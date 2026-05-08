-- name: UpsertPullRequestReview :exec
INSERT INTO pr_reviews (gh_id, pr_repo_id, pr_number, reviewer_gh_user_id, state, submitted_at)
VALUES (@gh_id, @pr_repo_id, @pr_number, @reviewer_gh_user_id, @state, @submitted_at)
ON CONFLICT (gh_id) DO UPDATE SET
  pr_repo_id = excluded.pr_repo_id,
  pr_number = excluded.pr_number,
  reviewer_gh_user_id = excluded.reviewer_gh_user_id,
  state = excluded.state,
  submitted_at = excluded.submitted_at;

-- name: ListReviewsByPullRequest :many
SELECT * FROM pr_reviews
WHERE pr_repo_id = @pr_repo_id AND pr_number = @pr_number
ORDER BY submitted_at;

-- name: ListReviewsByReviewerBetween :many
SELECT * FROM pr_reviews
WHERE reviewer_gh_user_id = @reviewer_gh_user_id
  AND submitted_at >= @from_ts
  AND submitted_at < @to_ts
ORDER BY submitted_at;
