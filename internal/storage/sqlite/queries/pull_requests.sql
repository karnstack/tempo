-- name: UpsertPullRequest :exec
INSERT INTO pull_requests (
  repo_id, number, gh_id, author_gh_user_id, state, title,
  created_at, merged_at, closed_at, additions, deletions,
  base_ref, head_ref, draft
) VALUES (
  @repo_id, @number, @gh_id, @author_gh_user_id, @state, @title,
  @created_at, @merged_at, @closed_at, @additions, @deletions,
  @base_ref, @head_ref, @draft
)
ON CONFLICT (repo_id, number) DO UPDATE SET
  gh_id = excluded.gh_id,
  author_gh_user_id = excluded.author_gh_user_id,
  state = excluded.state,
  title = excluded.title,
  merged_at = excluded.merged_at,
  closed_at = excluded.closed_at,
  additions = excluded.additions,
  deletions = excluded.deletions,
  base_ref = excluded.base_ref,
  head_ref = excluded.head_ref,
  draft = excluded.draft;

-- name: GetPullRequest :one
SELECT * FROM pull_requests WHERE repo_id = @repo_id AND number = @number;

-- name: ListPullRequestsByRepoBetween :many
SELECT * FROM pull_requests
WHERE repo_id = @repo_id
  AND created_at >= @from_ts
  AND created_at < @to_ts
ORDER BY created_at;

-- name: ListMergedPullRequestsByRepoBetween :many
SELECT * FROM pull_requests
WHERE repo_id = @repo_id
  AND merged_at IS NOT NULL
  AND merged_at >= @from_ts
  AND merged_at < @to_ts
ORDER BY merged_at;
