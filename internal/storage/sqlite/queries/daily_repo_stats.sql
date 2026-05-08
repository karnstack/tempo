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
