-- name: UpsertCommit :exec
INSERT INTO commits (
  repo_id, sha, author_gh_user_id, committer_gh_user_id,
  authored_at, additions, deletions, message
) VALUES (
  @repo_id, @sha, @author_gh_user_id, @committer_gh_user_id,
  @authored_at, @additions, @deletions, @message
)
ON CONFLICT (repo_id, sha) DO UPDATE SET
  author_gh_user_id = excluded.author_gh_user_id,
  committer_gh_user_id = excluded.committer_gh_user_id,
  authored_at = excluded.authored_at,
  additions = excluded.additions,
  deletions = excluded.deletions,
  message = excluded.message;

-- name: GetCommit :one
SELECT * FROM commits WHERE repo_id = @repo_id AND sha = @sha;

-- name: ListCommitsByRepoBetween :many
SELECT * FROM commits
WHERE repo_id = @repo_id
  AND authored_at >= @from_ts
  AND authored_at < @to_ts
ORDER BY authored_at;

-- name: ListCommitsByAuthorBetween :many
SELECT * FROM commits
WHERE author_gh_user_id = @author_gh_user_id
  AND authored_at >= @from_ts
  AND authored_at < @to_ts
ORDER BY authored_at;
