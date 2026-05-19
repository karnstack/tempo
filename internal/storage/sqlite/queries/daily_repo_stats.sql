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

-- name: UpsertRepoLeadTime :exec
--
-- Writes only the two lead_time_seconds_* columns of daily_repo_stats.
-- The count columns rely on their schema DEFAULT 0 on INSERT and are
-- intentionally absent from the ON CONFLICT DO UPDATE clause; they
-- belong to the repo_stats aggregator (0034). Mirrors the
-- disjoint-columns contract that AggregateRepoStatsForDay enshrines
-- from the opposite direction. See internal/rollup/cycletime/aggregator.go
-- and .plans/completed/0035-cycle-time-rollup/TASK.md.
INSERT INTO daily_repo_stats (
  date, repo_id, lead_time_seconds_p50, lead_time_seconds_p90
) VALUES (
  @date, @repo_id, @lead_time_seconds_p50, @lead_time_seconds_p90
)
ON CONFLICT (date, repo_id) DO UPDATE SET
  lead_time_seconds_p50 = excluded.lead_time_seconds_p50,
  lead_time_seconds_p90 = excluded.lead_time_seconds_p90;

-- name: SumDailyRepoStatsByTenantOwnerBetween :many
--
-- Aggregates counts across every repo with (tenant_id, owner) for the
-- given date range. Used by /api/v1/orgs/:org/metrics. Percentile
-- columns are intentionally not summed (they do not aggregate
-- statistically without raw samples). CAST forces the int64 scan
-- target for sqlc-sqlite.
SELECT s.date AS date,
       CAST(SUM(s.prs_opened) AS INTEGER) AS prs_opened,
       CAST(SUM(s.prs_merged) AS INTEGER) AS prs_merged,
       CAST(SUM(s.prs_closed) AS INTEGER) AS prs_closed,
       CAST(SUM(s.deploys) AS INTEGER) AS deploys
FROM daily_repo_stats s
JOIN repos r ON r.id = s.repo_id
WHERE r.tenant_id = @tenant_id AND r.owner = @owner
  AND s.date >= @from_date AND s.date < @to_date
GROUP BY s.date
ORDER BY s.date;
