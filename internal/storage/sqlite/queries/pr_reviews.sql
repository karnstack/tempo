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

-- name: ListReviewsWithPRMetaForRepo :many
--
-- Every non-self non-ghost review in the repo, joined to its PR's
-- number and created_at. No time filter; the aggregator does both
-- the "first review per PR" reduction and the [from, to) bucketing
-- in Go. Doing the MIN()/HAVING in SQL fights the modernc.org/sqlite
-- driver: aggregate functions strip the column's TIMESTAMP type tag
-- and the result comes back as a free-form string (YYYY-MM-DD
-- HH:MM:SS +ZZZZ TZN), so Scan cannot unmarshal it into time.Time
-- and unixepoch() refuses to parse it either.
SELECT pr.number AS pr_number,
       r.reviewer_gh_user_id AS reviewer_gh_user_id,
       r.submitted_at AS submitted_at,
       pr.created_at AS pr_created_at
FROM pr_reviews r
JOIN pull_requests pr
  ON pr.repo_id = r.pr_repo_id AND pr.number = r.pr_number
WHERE r.pr_repo_id = @repo_id
  AND r.reviewer_gh_user_id != 0
  AND r.reviewer_gh_user_id != pr.author_gh_user_id;
