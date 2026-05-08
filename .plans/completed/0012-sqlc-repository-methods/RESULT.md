# 0012 — sqlc repository methods (RESULT)

## Files changed

- `internal/storage/sqlite/queries/*.sql` × 19 — one query file per table covering CRUD / upserts / range scans the downstream tasks will need.
- `internal/storage/sqlite/sqlitedb/*.go` — sqlc-generated bindings (21 files: 19 `<table>.sql.go` + `db.go` + `models.go` + `querier.go`). Do not hand-edit.
- `internal/storage/sqlite/sqlite.go` — added `Store.Querier()` method and `NewQueries(storage.Storage)` fx provider.
- `internal/storage/sqlite/repo_test.go` — round-trip tests (identity, connection/repo, PR upsert idempotency, daily-engineer-stats range, sync run/cursor lifecycle, gh_user upsert returning, missing-row → `sql.ErrNoRows`).
- `migrations/migrations.go` — needed a `sync.Mutex` after the first parallel test run; goose's `SetBaseFS`/`SetDialect` are package-globals and the deferred `SetBaseFS(nil)` was racing concurrent callers' `Up`.

## Notes / decisions captured during execution

- Used `@name` SQLite parameter syntax → sqlc emits clean `*Params` structs (no positional `?` confusion for 14-column inserts).
- `commits` upsert is `:exec` not `:one` because the table has no autoincrement id (composite PK) and we don't need the row back after an event ingest.
- `sqlc.yaml` was already configured with `emit_pointers_for_null_types: true`; nullable columns surface as `*string`/`*time.Time`/`*int64` which keeps the test code's `ptr()` helper trivial.

## Verify output (last lines)

```
==> migrate up against fresh DB (smoke — embedded FS path)
OK   0001_identity_config.sql
OK   0002_raw_events.sql
OK   0003_daily_rollups.sql
OK   0004_sync_state.sql
goose: successfully migrated database to version: 4
  ok: all 19 tables created from embedded migrations
VERIFY OK
```

## Followups

- Phase-2 storage is now fully wired. Phase-3 (config / logger / auth) starts at 0013 and will exercise the `users` / `sessions` / `tenants` queries.
- The fx graph in `internal/server` does not yet call `sqlite.NewQueries`. Each downstream task that needs `*sqlitedb.Queries` injected adds the provider line. (Not adding it now would have meant adding it speculatively without a consumer.)
- If a feature task needs a new query, it adds the `.sql` snippet and runs `make sqlc-generate`; verify.sh's git-diff guard ensures committed bindings stay in sync.
