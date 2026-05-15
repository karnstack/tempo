-- name: CreateConnection :one
INSERT INTO connections (tenant_id, kind, owner, name, token_id, backfill_from, status)
VALUES (@tenant_id, @kind, @owner, @name, @token_id, @backfill_from, @status)
RETURNING *;

-- name: GetConnection :one
SELECT * FROM connections WHERE id = @id;

-- name: ListConnectionsByTenant :many
SELECT * FROM connections WHERE tenant_id = @tenant_id ORDER BY created_at;

-- name: ListActiveConnections :many
SELECT * FROM connections WHERE status = 'active' ORDER BY id;

-- name: UpdateConnectionStatus :exec
UPDATE connections SET status = @status WHERE id = @id;

-- name: UpdateConnectionLastSync :exec
UPDATE connections SET last_sync_at = @last_sync_at WHERE id = @id;

-- name: DeleteConnection :exec
DELETE FROM connections WHERE id = @id;

-- name: CountConnectionsByToken :one
SELECT COUNT(*) FROM connections WHERE token_id = @token_id;
