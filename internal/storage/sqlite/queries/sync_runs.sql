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

-- name: GetLastSuccessfulSyncRun :one
SELECT * FROM sync_runs
WHERE connection_id = @connection_id
  AND ok = 1
ORDER BY started_at DESC
LIMIT 1;

-- name: GetLastFailedSyncRun :one
SELECT * FROM sync_runs
WHERE connection_id = @connection_id
  AND ok = 0
  AND error != ''
ORDER BY started_at DESC
LIMIT 1;

-- name: PruneSyncRunsByConnection :exec
DELETE FROM sync_runs AS old
WHERE old.connection_id = @connection_id
  AND old.id NOT IN (
    SELECT keep.id FROM sync_runs AS keep
    WHERE keep.connection_id = @connection_id
    ORDER BY keep.started_at DESC
    LIMIT @keep_n
  );

-- name: CountFailedSyncRunsSince :one
SELECT COUNT(*) FROM sync_runs
WHERE finished_at IS NOT NULL
  AND ok = 0
  AND started_at >= @since;
