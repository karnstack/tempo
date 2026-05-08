-- name: CreateUser :one
INSERT INTO users (tenant_id, email, password_hash, role)
VALUES (@tenant_id, @email, @password_hash, @role)
RETURNING *;

-- name: GetUser :one
SELECT * FROM users WHERE id = @id;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE tenant_id = @tenant_id AND email = @email;

-- name: ListUsersByTenant :many
SELECT * FROM users WHERE tenant_id = @tenant_id ORDER BY created_at;

-- name: CountUsersByTenant :one
SELECT COUNT(*) FROM users WHERE tenant_id = @tenant_id;

-- name: UpdateUserPassword :exec
UPDATE users SET password_hash = @password_hash WHERE id = @id;

-- name: DeleteUser :exec
DELETE FROM users WHERE id = @id;
