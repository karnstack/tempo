-- name: UpsertRollupRunStart :one
INSERT INTO rollup_runs (date, kind, started_at, finished_at, ok, error)
VALUES (@date, @kind, @started_at, NULL, 0, '')
ON CONFLICT(date, kind) DO UPDATE SET
  started_at = excluded.started_at,
  finished_at = NULL,
  ok = 0,
  error = ''
RETURNING *;

-- name: FinishRollupRun :exec
UPDATE rollup_runs
SET finished_at = @finished_at,
    ok = @ok,
    error = @error
WHERE date = @date AND kind = @kind;

-- name: GetRollupRun :one
SELECT * FROM rollup_runs WHERE date = @date AND kind = @kind;

-- name: ListSuccessfulRollupDates :many
SELECT date FROM rollup_runs
WHERE kind = @kind AND ok = 1 AND date >= @since
ORDER BY date DESC;
