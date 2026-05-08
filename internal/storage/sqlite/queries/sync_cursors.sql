-- name: UpsertSyncCursor :exec
INSERT INTO sync_cursors (connection_id, resource, cursor, updated_at)
VALUES (@connection_id, @resource, @cursor, @updated_at)
ON CONFLICT (connection_id, resource) DO UPDATE SET
  cursor = excluded.cursor,
  updated_at = excluded.updated_at;

-- name: GetSyncCursor :one
SELECT * FROM sync_cursors
WHERE connection_id = @connection_id AND resource = @resource;

-- name: ListSyncCursorsByConnection :many
SELECT * FROM sync_cursors
WHERE connection_id = @connection_id
ORDER BY resource;
