-- name: StartSyncRun :one
INSERT INTO sync_runs (connection_id, started_at)
VALUES (@connection_id, @started_at)
RETURNING *;

-- name: FinishSyncRun :exec
UPDATE sync_runs
SET finished_at = @finished_at,
    ok = @ok,
    items = @items,
    rate_limit_remaining = @rate_limit_remaining,
    error = @error
WHERE id = @id;

-- name: GetSyncRun :one
SELECT * FROM sync_runs WHERE id = @id;

-- name: ListSyncRunsByConnection :many
SELECT * FROM sync_runs
WHERE connection_id = @connection_id
ORDER BY started_at DESC
LIMIT @limit_n;

-- name: GetLatestSyncRun :one
SELECT * FROM sync_runs
WHERE connection_id = @connection_id
ORDER BY started_at DESC
LIMIT 1;
