---
id: 0011
slug: migration-sync-state
title: Migration 0004 — sync state tables
status: done
depends_on: [0009]
owner: ""
est_minutes: 15
tags: [storage, migration]
autonomy: full
skills: []
---

## Goal

Land the v1 sync-state migration (`migrations/0004_sync_state.sql` — single file, `-- +goose Up` / `-- +goose Down`) covering the spec's "Sync state" section: `sync_runs` (history of polling runs) and `sync_cursors` (per-connection-per-resource resume points).

These two small tables let the ingest worker (0026+) resume incremental syncs from the last cursor and surface "last sync at / ok / items" status to the dashboard's sync panel (0055).

Spec reference: `docs/superpowers/specs/2026-05-08-tempo-design.md` lines 114–117.

## Acceptance criteria

- [ ] `migrations/0004_sync_state.sql` exists with `-- +goose Up` and `-- +goose Down` markers.
- [ ] `sync_runs(id INTEGER PK, connection_id, started_at, finished_at?, ok bool-as-int, items, rate_limit_remaining?, error TEXT)` plus index `(connection_id, started_at)`.
- [ ] `sync_cursors(connection_id, resource, cursor, updated_at)` with composite PK `(connection_id, resource)`. No additional index needed (PK covers the only access pattern).
- [ ] **No foreign keys, no CHECK constraints, no cascades.** `connection_id` is plain `INTEGER NOT NULL`; resource is `TEXT NOT NULL` (validated in Go).
- [ ] Booleans (`ok`) stored as `INTEGER NOT NULL DEFAULT 0`.
- [ ] `error TEXT NOT NULL DEFAULT ''` (empty string when run succeeded; non-empty contains the error message).
- [ ] `go run ./cmd/migrate up` applies cleanly on top of 0001+0002+0003.
- [ ] `go run ./cmd/migrate down` (one step) drops only the 0004 tables.
- [ ] `down`+`up` idempotent.
- [ ] `go test ./internal/storage/...` still passes.
- [ ] `verify.sh` exits 0.

## Files to touch

- `migrations/0004_sync_state.sql` (new — single file)
- `.plans/upnext/0011-migration-sync-state/verify.sh` (replace stub)

## Schema decisions (rationale up front)

- **`sync_runs.id INTEGER PRIMARY KEY`**: a sync run is just an event log row; an autoincrement rowid is the simplest unique key. We never look up a run by anything other than its connection or recency.
- **`sync_runs.finished_at` nullable**: a run row is inserted at start; the ingest worker updates `finished_at`, `ok`, `items`, `error` when the run terminates. A still-running row has `finished_at IS NULL` — useful for "is sync currently running?" queries and for surfacing zombie runs.
- **`sync_runs.ok INTEGER NOT NULL DEFAULT 0`**: 0 until the run completes successfully; 1 after. Lets us count failure rate without joining `error != ''`.
- **`sync_runs.rate_limit_remaining INTEGER`** (nullable): the GitHub API doesn't always return a rate-limit header (e.g., from cached 304 responses), and `0` is a meaningful value (we hit the limit). Distinguish "we don't know" with NULL.
- **`sync_runs.error TEXT NOT NULL DEFAULT ''`**: keep the column NOT NULL so row scans don't have to NULL-check; empty string conveys "no error" cleanly.
- **`sync_runs(connection_id, started_at)` index**: every read path is "the last N runs for this connection, newest first" — supports it directly.
- **`sync_cursors` composite PK**: `(connection_id, resource)` is the natural key. The cursor value itself is opaque to SQLite (could be a GraphQL cursor base64 string, an ISO timestamp for `since=`, or an ETag) — we just store and return it. `updated_at` is bookkeeping for "when did we last advance this cursor".
- **No FK / CHECK** per project rule. Cascade-on-delete (when a connection is removed) lives in the Go repo layer's `Connections.Delete` method.

## Steps

### 1. Write the migration

Create `migrations/0004_sync_state.sql`:

```sql
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
```

Commit: `chore(migrations): 0004 sync state tables`.

### 2. Smoke-test manually, then write `verify.sh`

Same shape as 0009/0010 — table presence, no-FK / no-CHECK, single-step down isolation, idempotency, storage tests.

### 3. Run `verify.sh`

`./.plans/upnext/0011-migration-sync-state/verify.sh` exits 0.

## Notes

- `resource` values used by the ingest layer (informative, validated in Go): `pull_requests`, `commits`, `pr_reviews`, `pr_review_comments`, `pr_issue_comments`, `deployments`. Keeping the column free-form leaves room for v1.1 additions (e.g. `releases`).
- `rate_limit_remaining` is captured per *run*, not per *call*. The worker writes the last-observed value at run end; for finer-grained backoff history we'd need a separate table — out of scope for v1.
- `sync_cursors.cursor` is opaque text to keep the schema indifferent to GraphQL vs REST cursors. Each fetcher knows how to interpret its own resource's value.
