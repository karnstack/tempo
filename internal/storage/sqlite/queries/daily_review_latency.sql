-- name: UpsertDailyReviewLatency :exec
INSERT INTO daily_review_latency (
  date, repo_id,
  time_to_first_review_seconds_p50, time_to_first_review_seconds_p90, count
) VALUES (
  @date, @repo_id,
  @time_to_first_review_seconds_p50, @time_to_first_review_seconds_p90, @count
)
ON CONFLICT (date, repo_id) DO UPDATE SET
  time_to_first_review_seconds_p50 = excluded.time_to_first_review_seconds_p50,
  time_to_first_review_seconds_p90 = excluded.time_to_first_review_seconds_p90,
  count = excluded.count;

-- name: ListDailyReviewLatencyByRepoBetween :many
SELECT * FROM daily_review_latency
WHERE repo_id = @repo_id
  AND date >= @from_date
  AND date < @to_date
ORDER BY date;
