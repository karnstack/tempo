-- name: UpsertDailyRepoStats :exec
INSERT INTO daily_repo_stats (
  date, repo_id,
  prs_opened, prs_merged, prs_closed, deploys,
  lead_time_seconds_p50, lead_time_seconds_p90
) VALUES (
  @date, @repo_id,
  @prs_opened, @prs_merged, @prs_closed, @deploys,
  @lead_time_seconds_p50, @lead_time_seconds_p90
)
ON CONFLICT (date, repo_id) DO UPDATE SET
  prs_opened = excluded.prs_opened,
  prs_merged = excluded.prs_merged,
  prs_closed = excluded.prs_closed,
  deploys = excluded.deploys,
  lead_time_seconds_p50 = excluded.lead_time_seconds_p50,
  lead_time_seconds_p90 = excluded.lead_time_seconds_p90;

-- name: ListDailyRepoStatsByRepoBetween :many
SELECT * FROM daily_repo_stats
WHERE repo_id = @repo_id
  AND date >= @from_date
  AND date < @to_date
ORDER BY date;

-- name: DeleteDailyRepoStatsByDateRepo :exec
DELETE FROM daily_repo_stats WHERE date = @date AND repo_id = @repo_id;

-- name: AggregateRepoStatsForDay :exec
--
-- Rebuilds the four count columns of daily_repo_stats for (date, repo_id)
-- from pull_requests + deployments. The two lead_time_seconds_* columns
-- belong to the cycle-time aggregator (0035) and are intentionally absent
-- from the ON CONFLICT DO UPDATE clause so a sibling rerun does not clobber
-- them. See internal/rollup/repostats/aggregator.go for the wrapping
-- Aggregator and .plans/completed/0034-repo-stats-rollup/TASK.md for the
-- disjoint-columns contract.
INSERT INTO daily_repo_stats (
  date, repo_id,
  prs_opened, prs_merged, prs_closed, deploys
) VALUES (
  @date, @repo_id,
  (SELECT COUNT(*) FROM pull_requests pr
     WHERE pr.repo_id = @repo_id
       AND pr.created_at >= @from_ts AND pr.created_at < @to_ts),
  (SELECT COUNT(*) FROM pull_requests pr
     WHERE pr.repo_id = @repo_id
       AND pr.merged_at IS NOT NULL
       AND pr.merged_at >= @from_ts AND pr.merged_at < @to_ts),
  (SELECT COUNT(*) FROM pull_requests pr
     WHERE pr.repo_id = @repo_id
       AND pr.merged_at IS NULL
       AND pr.closed_at IS NOT NULL
       AND pr.closed_at >= @from_ts AND pr.closed_at < @to_ts),
  (SELECT COUNT(*) FROM deployments d
     WHERE d.repo_id = @repo_id
       AND d.created_at >= @from_ts AND d.created_at < @to_ts)
)
ON CONFLICT (date, repo_id) DO UPDATE SET
  prs_opened = excluded.prs_opened,
  prs_merged = excluded.prs_merged,
  prs_closed = excluded.prs_closed,
  deploys    = excluded.deploys;
