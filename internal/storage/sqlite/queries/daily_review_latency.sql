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

-- name: SumDailyReviewLatencyByTenantOwnerBetween :many
--
-- SUM of the "count" column across every repo with (tenant_id, owner)
-- for the date range. Percentile columns are intentionally not summed.
SELECT l.date AS date,
       CAST(SUM(l.count) AS INTEGER) AS count
FROM daily_review_latency l
JOIN repos r ON r.id = l.repo_id
WHERE r.tenant_id = @tenant_id AND r.owner = @owner
  AND l.date >= @from_date AND l.date < @to_date
GROUP BY l.date
ORDER BY l.date;
