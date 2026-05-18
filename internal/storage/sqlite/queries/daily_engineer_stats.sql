-- name: UpsertDailyEngineerStats :exec
INSERT INTO daily_engineer_stats (
  date, repo_id, gh_user_id,
  commits, prs_opened, prs_merged, reviews_given, comments,
  additions, deletions
) VALUES (
  @date, @repo_id, @gh_user_id,
  @commits, @prs_opened, @prs_merged, @reviews_given, @comments,
  @additions, @deletions
)
ON CONFLICT (date, repo_id, gh_user_id) DO UPDATE SET
  commits = excluded.commits,
  prs_opened = excluded.prs_opened,
  prs_merged = excluded.prs_merged,
  reviews_given = excluded.reviews_given,
  comments = excluded.comments,
  additions = excluded.additions,
  deletions = excluded.deletions;

-- name: ListDailyEngineerStatsByRepoBetween :many
SELECT * FROM daily_engineer_stats
WHERE repo_id = @repo_id
  AND date >= @from_date
  AND date < @to_date
ORDER BY date, gh_user_id;

-- name: ListDailyEngineerStatsByUserBetween :many
SELECT * FROM daily_engineer_stats
WHERE gh_user_id = @gh_user_id
  AND date >= @from_date
  AND date < @to_date
ORDER BY date, repo_id;

-- name: DeleteDailyEngineerStatsByDateRepo :exec
DELETE FROM daily_engineer_stats WHERE date = @date AND repo_id = @repo_id;

-- The per-day aggregation query lives in
-- internal/rollup/engineerstats/aggregator.go because sqlc-sqlite's parser
-- can't resolve CTE references in the chained WITH ... INSERT statement
-- it produces. It's a single Exec executed inside the same tx as the
-- DELETE above.
