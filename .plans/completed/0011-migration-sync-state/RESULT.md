# 0011 — sync state tables (RESULT)

## Files changed

- `migrations/0004_sync_state.sql` (new) — `sync_runs` (id PK + connection/started_at index) and `sync_cursors` (composite PK `(connection_id, resource)`).
- `.plans/upnext/0011-migration-sync-state/verify.sh` — replaced stub.
- `.plans/upnext/0011-migration-sync-state/TASK.md` — fleshed out; status=done.

## Schema notes

- `sync_runs.ok INTEGER NOT NULL DEFAULT 0` + `error TEXT NOT NULL DEFAULT ''`: failure rate computable without joins; running rows surface as `finished_at IS NULL`.
- `rate_limit_remaining INTEGER` (nullable) distinguishes "we don't know" (e.g. 304 cached) from "0 remaining".
- `sync_cursors.cursor TEXT NOT NULL` is opaque — accommodates GraphQL cursors, ISO timestamps, ETags without schema churn.

## Verify output (last lines)

```
==> verify 0004 tables removed
  ok: 0004 tables gone
==> verify prior tables NOT touched by single down step
  ok: prior tables intact
==> migrate up again (idempotency)
goose: successfully migrated database to version: 4
  ok: re-up recreated all 0004 tables
==> storage package tests still pass
ok  	github.com/karnstack/tempo/internal/storage/sqlite
VERIFY OK
```

## Followups

- Phase-2 storage migrations are now complete. 0012 lands sqlc query files + repository unit tests over all four migrations.
