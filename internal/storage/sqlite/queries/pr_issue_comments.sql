-- name: UpsertPullRequestIssueComment :exec
INSERT INTO pr_issue_comments (gh_id, pr_repo_id, pr_number, author_gh_user_id, created_at)
VALUES (@gh_id, @pr_repo_id, @pr_number, @author_gh_user_id, @created_at)
ON CONFLICT (gh_id) DO UPDATE SET
  pr_repo_id = excluded.pr_repo_id,
  pr_number = excluded.pr_number,
  author_gh_user_id = excluded.author_gh_user_id,
  created_at = excluded.created_at;

-- name: ListIssueCommentsByPullRequest :many
SELECT * FROM pr_issue_comments
WHERE pr_repo_id = @pr_repo_id AND pr_number = @pr_number
ORDER BY created_at;
