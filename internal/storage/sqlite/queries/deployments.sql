-- name: UpsertDeployment :exec
INSERT INTO deployments (gh_id, repo_id, environment, ref, sha, status, created_at)
VALUES (@gh_id, @repo_id, @environment, @ref, @sha, @status, @created_at)
ON CONFLICT (gh_id) DO UPDATE SET
  repo_id = excluded.repo_id,
  environment = excluded.environment,
  ref = excluded.ref,
  sha = excluded.sha,
  status = excluded.status,
  created_at = excluded.created_at;

-- name: ListDeploymentsByRepoBetween :many
SELECT * FROM deployments
WHERE repo_id = @repo_id
  AND created_at >= @from_ts
  AND created_at < @to_ts
ORDER BY created_at;
