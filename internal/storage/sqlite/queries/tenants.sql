-- name: CreateTenant :one
INSERT INTO tenants (name)
VALUES (@name)
RETURNING *;

-- name: GetTenant :one
SELECT * FROM tenants WHERE id = @id;

-- name: ListTenants :many
SELECT * FROM tenants ORDER BY id;

-- name: CountTenants :one
SELECT COUNT(*) FROM tenants;
