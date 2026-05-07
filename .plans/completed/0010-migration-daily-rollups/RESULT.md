# 0010 — daily rollup tables (RESULT)

## Files changed

- `migrations/0003_daily_rollups.sql` (new) — 4 rollup tables (`daily_engineer_stats`, `daily_repo_stats`, `daily_review_latency`, `daily_review_load`) plus 5 read-path indices. Composite PKs match the upsert keys the rollup worker will use.
- `.plans/upnext/0010-migration-daily-rollups/verify.sh` — replaced stub.
- `.plans/upnext/0010-migration-daily-rollups/TASK.md` — fleshed out; status=done.

## Schema notes

- `date TEXT NOT NULL` (`'YYYY-MM-DD'` strings, instance-local) for trivial `BETWEEN` range scans.
- Percentile columns are nullable so days with zero signal show "no data" rather than a misleading 0.
- `(repo_id, date)` / `(gh_user_id, date)` secondary indices invert the PK ordering for the dashboard's "X over the last N days" queries.

## Verify output (last lines)

```
==> migrate down (one step — drops only 0003)
OK   0003_daily_rollups.sql
==> verify 0003 tables removed
  ok: 0003 tables gone
==> verify prior tables NOT touched by single down step
  ok: prior tables intact
==> migrate up again (idempotency)
goose: successfully migrated database to version: 3
  ok: re-up recreated all 0003 tables
==> storage package tests still pass
ok  	github.com/karnstack/tempo/internal/storage/sqlite
VERIFY OK
```

## Followups

- Rollup worker (0032+) is responsible for `INSERT … ON CONFLICT DO UPDATE` upserts.
- If future repos × days × engineers cardinality grows beyond ~10M rows, revisit whether `(repo_id, date)` should become a covering index.
