# 0008 — Migration 0001 (identity & config) — done

## What changed

- `migrations/0001_identity_config.sql` (new, single-file goose convention) — 7 tables: `tenants`, `users`, `sessions`, `gh_tokens`, `connections`, `repos`, `gh_users`. Plain `INTEGER PRIMARY KEY` IDs (sessions uses `TEXT` for the cookie token); `TIMESTAMP DEFAULT CURRENT_TIMESTAMP` for created_at/added_at. **Zero foreign keys, zero CHECK constraints** — referential integrity and enum validation will live in the Go repo layer (lands in 0012). Unique indices kept for true-uniqueness invariants. Partial unique indices on `connections` distinguish `kind='repo'` (uniqueness on `tenant_id, owner, name`) from `kind='org'` (uniqueness on `tenant_id, owner`).
- `migrations/README.md` — corrected the naming convention from split `.up.sql`/`.down.sql` to single-file `NNNN_<slug>.sql` with `-- +goose Up` / `-- +goose Down` sections (goose v3 default).
- `.plans/upnext/0008-migration-identity-config/verify.sh` — replaced stub. Asserts up creates the 7 tables + indices, down drops them, re-up is idempotent, schema has zero FKs and zero CHECK constraints (regression guard for the no-DB-constraints rule), and `go test ./internal/storage/...` still passes.
- `.plans/upnext/0008-migration-identity-config/TASK.md` — fleshed out from stub before starting.

## Schema decisions worth remembering

- **No FKs / no CHECK** is project-wide. Verify.sh now polices this — any future migration that re-introduces them will fail verify.
- Cascades-on-delete will be implemented in Go (handlers/repos), inside transactions.
- Enum-like fields (`role`, `kind`, `status`) will be Go-typed (`type ConnectionKind string` + constants + `Valid()`).
- A tenants row is **not** seeded here; auth/register (0017) creates it on first run.

## verify output (last lines)

```
  ok: re-up recreated all tables
==> storage package tests still pass
?   	github.com/karnstack/tempo/internal/storage	[no test files]
?   	github.com/karnstack/tempo/internal/storage/postgres	[no test files]
ok  	github.com/karnstack/tempo/internal/storage/sqlite	(cached)
VERIFY OK
```

## Followups

- 0009 (raw events), 0010 (rollups), 0011 (sync state) will follow the same conventions: single-file goose migration, no FK/CHECK, plain INTEGER ids, TIMESTAMP for time columns. Verify-style FK/CHECK guards should be copied into each new task's verify.sh.
- 0012 will introduce sqlc-generated query packages for these tables, plus the Go-side validation/cascade logic that compensates for the absent DB constraints.
