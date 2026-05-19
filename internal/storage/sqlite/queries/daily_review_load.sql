-- name: UpsertDailyReviewLoad :exec
INSERT INTO daily_review_load (
  date, repo_id, reviewer_gh_user_id, reviews, response_minutes_p50
) VALUES (
  @date, @repo_id, @reviewer_gh_user_id, @reviews, @response_minutes_p50
)
ON CONFLICT (date, repo_id, reviewer_gh_user_id) DO UPDATE SET
  reviews = excluded.reviews,
  response_minutes_p50 = excluded.response_minutes_p50;

-- name: ListDailyReviewLoadByRepoBetween :many
SELECT * FROM daily_review_load
WHERE repo_id = @repo_id
  AND date >= @from_date
  AND date < @to_date
ORDER BY date, reviewer_gh_user_id;

-- name: ListDailyReviewLoadByReviewerBetween :many
SELECT * FROM daily_review_load
WHERE reviewer_gh_user_id = @reviewer_gh_user_id
  AND date >= @from_date
  AND date < @to_date
ORDER BY date, repo_id;

-- name: DeleteDailyReviewLoadByDateRepo :exec
DELETE FROM daily_review_load WHERE date = @date AND repo_id = @repo_id;

-- name: SumDailyReviewLoadByTenantOwnerBetween :many
--
-- SUM of "reviews" per (date, reviewer) across every repo with
-- (tenant_id, owner) for the date range. response_minutes_p50 is
-- intentionally omitted (cross-repo percentile aggregation is
-- statistically meaningless).
SELECT l.date AS date,
       l.reviewer_gh_user_id AS reviewer_gh_user_id,
       CAST(SUM(l.reviews) AS INTEGER) AS reviews
FROM daily_review_load l
JOIN repos r ON r.id = l.repo_id
WHERE r.tenant_id = @tenant_id AND r.owner = @owner
  AND l.date >= @from_date AND l.date < @to_date
GROUP BY l.date, l.reviewer_gh_user_id
ORDER BY l.date, l.reviewer_gh_user_id;
