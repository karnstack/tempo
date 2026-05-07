---
id: 0008
slug: migration-identity-config
title: Migration 0001 — identity & config tables
status: done
depends_on: [0007]
owner: ""
est_minutes: 25
tags: [storage, migration]
autonomy: full
skills: []
---

## Goal

Land the v1 baseline migration (`migrations/0001_identity_config.sql` — single file with `-- +goose Up` / `-- +goose Down` sections) covering the spec's "Identity & config" section: `tenants`, `users`, `sessions`, `gh_tokens`, `connections`, `repos`, `gh_users`. After this task, `make migrate-up` against a fresh DB creates exactly these tables, and `make migrate-down` drops them cleanly.

Spec reference: `docs/superpowers/specs/2026-05-08-tempo-design.md` lines 86–95.

## Acceptance criteria

- [ ] `migrations/0001_identity_config.up.sql` exists with `-- +goose Up` markers and creates the seven tables plus their indices.
- [ ] `migrations/0001_identity_config.down.sql` exists with `-- +goose Down` markers and drops the tables.
- [ ] **No foreign keys, no CHECK constraints, no cascades.** Cross-table refs are plain `xxx_id INTEGER NOT NULL` columns. Enum-like values (`kind`, `role`, `status`) are plain `TEXT NOT NULL`; validation lives in Go.
- [ ] NOT NULL columns where data is truly required.
- [ ] Unique indices: `users(tenant_id, email)`, `repos(tenant_id, gh_id)`, `gh_users(tenant_id, gh_id)`, partial-unique `connections(tenant_id, owner, name) WHERE kind='repo'`, partial-unique `connections(tenant_id, owner) WHERE kind='org'`.
- [ ] `go run ./cmd/migrate up` succeeds against an empty DB; all seven tables visible in `sqlite_master`.
- [ ] `go run ./cmd/migrate down` rolls everything back; `sqlite_master` no longer lists any of the seven.
- [ ] Re-running `up` after `down` succeeds (idempotency).
- [ ] `go test ./internal/storage/...` still passes (no schema-dependent test breakage).
- [ ] `verify.sh` exits 0.

## Files to touch

- `migrations/0001_identity_config.sql` (new — single file, both Up and Down sections)
- `migrations/README.md` (correct the naming-convention line)
- `.plans/upnext/0008-migration-identity-config/verify.sh` (replace stub)

## Schema decisions (rationale up front)

Per the project's "no DB-level constraints" rule: this migration uses **no foreign keys, no CHECK, no cascades**. Cross-table relationships are plain `xxx_id INTEGER NOT NULL` columns; referential integrity, enum validation, and cascade-on-delete semantics are all enforced in Go (handlers, repos, transactions). Migrations stay reorderable, deletes stay explicit.

- **IDs**: `INTEGER PRIMARY KEY` for everything except `sessions.id`. Sessions use `TEXT PRIMARY KEY` because the id *is* the cookie token (random 32-byte base64). Integer rowids keep joins compact and sqlc-friendly.
- **Cross-table refs**: plain `tenant_id INTEGER NOT NULL`, `user_id INTEGER NOT NULL`, etc. No `REFERENCES` clauses. No `ON DELETE` clauses. The Go repo layer is responsible for cascade deletes (e.g. `tenants.Delete(id)` runs `DELETE FROM users WHERE tenant_id=?` etc. inside a tx).
- **Enum-like fields** (`users.role`, `connections.kind`, `connections.status`): plain `TEXT NOT NULL`, no CHECK. A Go type like `type ConnectionKind string` with `KindRepo`, `KindOrg` constants and a `Valid()` method handles validation in handlers.
- **Timestamps**: `TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP` for `created_at`/`added_at`. modernc.org/sqlite scans `TIMESTAMP` into `time.Time`. Nullable timestamps (`expires_at`, `last_sync_at`, `last_seen_at`) are plain `TIMESTAMP` (no default).
- **Booleans**: `archived INTEGER NOT NULL DEFAULT 0` — SQLite has no bool type; sqlc treats this as int64 which we wrap in app code if needed.
- **Encrypted PAT**: `encrypted_pat BLOB NOT NULL` — opaque bytes. Encryption happens in the auth layer (0015/0039), not here.
- **Partial unique indices** for connections: `(tenant_id, owner, name)` is only unique when `kind='repo'`; `(tenant_id, owner)` only when `kind='org'`. Unique indices are kept (they prevent duplicate-row races, distinct from FK/CHECK).

## Steps

### 1. Write the up migration

Create `migrations/0001_identity_config.up.sql`:

```sql
-- +goose Up

CREATE TABLE tenants (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE users (
  id INTEGER PRIMARY KEY,
  tenant_id INTEGER NOT NULL,
  email TEXT NOT NULL,
  password_hash TEXT NOT NULL,
  role TEXT NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX users_tenant_email_uniq ON users(tenant_id, email);

CREATE TABLE sessions (
  id TEXT PRIMARY KEY,
  user_id INTEGER NOT NULL,
  expires_at TIMESTAMP NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX sessions_user_idx ON sessions(user_id);
CREATE INDEX sessions_expires_idx ON sessions(expires_at);

CREATE TABLE gh_tokens (
  id INTEGER PRIMARY KEY,
  tenant_id INTEGER NOT NULL,
  label TEXT NOT NULL,
  encrypted_pat BLOB NOT NULL,
  scopes TEXT NOT NULL DEFAULT '',
  expires_at TIMESTAMP,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX gh_tokens_tenant_idx ON gh_tokens(tenant_id);

CREATE TABLE connections (
  id INTEGER PRIMARY KEY,
  tenant_id INTEGER NOT NULL,
  kind TEXT NOT NULL,
  owner TEXT NOT NULL,
  name TEXT,
  token_id INTEGER NOT NULL,
  backfill_from TIMESTAMP NOT NULL,
  status TEXT NOT NULL DEFAULT 'active',
  last_sync_at TIMESTAMP,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX connections_repo_uniq
  ON connections(tenant_id, owner, name) WHERE kind = 'repo';
CREATE UNIQUE INDEX connections_org_uniq
  ON connections(tenant_id, owner) WHERE kind = 'org';
CREATE INDEX connections_token_idx ON connections(token_id);

CREATE TABLE repos (
  id INTEGER PRIMARY KEY,
  tenant_id INTEGER NOT NULL,
  connection_id INTEGER NOT NULL,
  gh_id INTEGER NOT NULL,
  owner TEXT NOT NULL,
  name TEXT NOT NULL,
  default_branch TEXT NOT NULL DEFAULT 'main',
  archived INTEGER NOT NULL DEFAULT 0,
  added_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX repos_tenant_gh_uniq ON repos(tenant_id, gh_id);
CREATE INDEX repos_connection_idx ON repos(connection_id);
CREATE INDEX repos_owner_name_idx ON repos(owner, name);

CREATE TABLE gh_users (
  id INTEGER PRIMARY KEY,
  tenant_id INTEGER NOT NULL,
  gh_id INTEGER NOT NULL,
  login TEXT NOT NULL,
  name TEXT,
  avatar_url TEXT,
  last_seen_at TIMESTAMP
);
CREATE UNIQUE INDEX gh_users_tenant_gh_uniq ON gh_users(tenant_id, gh_id);
CREATE INDEX gh_users_login_idx ON gh_users(login);
```

Commit: `chore(migrations): 0001 identity & config tables (up)`.

### 2. Write the down migration

Create `migrations/0001_identity_config.down.sql`:

```sql
-- +goose Down

DROP TABLE IF EXISTS gh_users;
DROP TABLE IF EXISTS repos;
DROP TABLE IF EXISTS connections;
DROP TABLE IF EXISTS gh_tokens;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS tenants;
```

Indices drop with their tables in SQLite, no need to drop them separately. Order doesn't matter (no FKs).

Commit: `chore(migrations): 0001 identity & config tables (down)`.

### 3. Manually smoke-test

```sh
rm -f /tmp/tempo-0008.db
TEMPO_DB="sqlite:///tmp/tempo-0008.db" go run ./cmd/migrate up
sqlite3 /tmp/tempo-0008.db ".tables"
TEMPO_DB="sqlite:///tmp/tempo-0008.db" go run ./cmd/migrate down
sqlite3 /tmp/tempo-0008.db ".tables"   # should print only goose_db_version
TEMPO_DB="sqlite:///tmp/tempo-0008.db" go run ./cmd/migrate up   # idempotent
```

If anything looks wrong, fix the SQL and amend — no commit yet.

### 4. Run `verify.sh`

`./.plans/upnext/0008-migration-identity-config/verify.sh` should exit 0.

### 5. Final wrap-up commit

If steps 1–2 produced two commits already, the `verify.sh` rewrite is its own small commit: `chore(plans): 0008 verify.sh`. Then the `/next-task` workflow handles `RESULT.md`, status=done, and the `git mv` to `completed/`.

## Notes

- Goose splits statements by `;`; no need for `StatementBegin/End` for plain CREATEs.
- The driver name in `cmd/migrate/main.go` is `"sqlite3"` (goose dialect) but the `database/sql` driver registered is `"sqlite"` from modernc.org/sqlite — already wired correctly, no change needed.
- Don't seed a tenants row here. Auth/register (0017) will insert the single v1 tenant on first run.
- `goose_db_version` is goose's bookkeeping table; verify.sh excludes it from the "tables removed" check.
- **No FKs / no CHECK** — enum validation and cascade-on-delete will be enforced in Go (handlers and the repository layer landing in 0012). This is the project-wide rule, not a 0008-specific call.
