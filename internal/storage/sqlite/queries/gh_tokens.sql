-- name: CreateGhToken :one
INSERT INTO gh_tokens (tenant_id, label, encrypted_pat, scopes, expires_at)
VALUES (@tenant_id, @label, @encrypted_pat, @scopes, @expires_at)
RETURNING *;

-- name: GetGhToken :one
SELECT * FROM gh_tokens WHERE id = @id;

-- name: ListGhTokensByTenant :many
SELECT * FROM gh_tokens WHERE tenant_id = @tenant_id ORDER BY created_at;

-- name: DeleteGhToken :exec
DELETE FROM gh_tokens WHERE id = @id;
