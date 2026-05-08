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
