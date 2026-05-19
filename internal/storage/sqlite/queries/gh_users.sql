-- name: UpsertGhUser :one
INSERT INTO gh_users (tenant_id, gh_id, login, name, avatar_url, last_seen_at)
VALUES (@tenant_id, @gh_id, @login, @name, @avatar_url, @last_seen_at)
ON CONFLICT (tenant_id, gh_id) DO UPDATE SET
  login = excluded.login,
  name = excluded.name,
  avatar_url = excluded.avatar_url,
  last_seen_at = excluded.last_seen_at
RETURNING *;

-- name: GetGhUser :one
SELECT * FROM gh_users WHERE id = @id;

-- name: GetGhUserByGhID :one
SELECT * FROM gh_users WHERE tenant_id = @tenant_id AND gh_id = @gh_id;

-- name: ListGhUsersByTenant :many
SELECT * FROM gh_users WHERE tenant_id = @tenant_id ORDER BY login;

-- name: GetGhUserByTenantLogin :one
SELECT * FROM gh_users WHERE tenant_id = @tenant_id AND login = @login LIMIT 1;
