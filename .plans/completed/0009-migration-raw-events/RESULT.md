# 0009 — raw event tables (RESULT)

## Files changed

- `migrations/0002_raw_events.sql` (new) — single-file goose migration with `-- +goose Up` / `-- +goose Down` sections covering `commits`, `pull_requests`, `pr_reviews`, `pr_review_comments`, `pr_issue_comments`, `deployments`, plus 14 indices.
- `.plans/upnext/0009-migration-raw-events/verify.sh` — replaced stub with a full check (table presence, no-FK / no-CHECK guard, index presence, single-step down, re-up idempotency, storage tests).
- `.plans/upnext/0009-migration-raw-events/TASK.md` — fleshed out from stub; status flipped to `done`.

## Schema notes

- `commits` PK is composite `(repo_id, sha)` (deviation from spec's shorthand `sha PK` to support forks/mirrors).
- `pull_requests` PK is composite `(repo_id, number)`; `(repo_id, gh_id)` is unique-indexed for GitHub-id lookups.
- All cross-table refs are plain `INTEGER NOT NULL`; no FKs, no CHECK, no cascades — per project rule (enforced in Go).

## Verify output (last lines)

```
==> migrate down (one step — drops only 0002)
OK   0002_raw_events.sql
==> verify 0002 tables removed
  ok: 0002 tables gone
==> verify 0001 tables NOT touched by single down step
  ok: 0001 tables intact
==> migrate up again (idempotency)
OK   0002_raw_events.sql
goose: successfully migrated database to version: 2
  ok: re-up recreated all 0002 tables
==> storage package tests still pass
ok  	github.com/karnstack/tempo/internal/storage/sqlite
VERIFY OK
```

## Followups

- 0012 will add sqlc query files keyed on the PKs / indices defined here. The `INSERT … ON CONFLICT DO UPDATE` upsert shape lives there.
- `commits.message` defaults to `''` so the lightweight default-branch poll can elide it; revisit when ingest lands.
