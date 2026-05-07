-- +goose Up

CREATE TABLE sync_runs (
  id INTEGER PRIMARY KEY,
  connection_id INTEGER NOT NULL,
  started_at TIMESTAMP NOT NULL,
  finished_at TIMESTAMP,
  ok INTEGER NOT NULL DEFAULT 0,
  items INTEGER NOT NULL DEFAULT 0,
  rate_limit_remaining INTEGER,
  error TEXT NOT NULL DEFAULT ''
);
CREATE INDEX sync_runs_connection_started_idx ON sync_runs(connection_id, started_at);

CREATE TABLE sync_cursors (
  connection_id INTEGER NOT NULL,
  resource TEXT NOT NULL,
  cursor TEXT NOT NULL,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (connection_id, resource)
);

-- +goose Down

DROP TABLE IF EXISTS sync_cursors;
DROP TABLE IF EXISTS sync_runs;
