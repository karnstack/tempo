---
id: 0040
slug: api-connections
title: /api/v1/connections CRUD
status: done
depends_on: [0039]
owner: ""
est_minutes: 60
tags: [api]
autonomy: full
skills: []
---

## Goal

Add the three connections endpoints listed in the spec
(`docs/superpowers/specs/2026-05-08-tempo-design.md:217-219`):

```
GET    /api/v1/connections
POST   /api/v1/connections
DELETE /api/v1/connections/:id
```

Connections are the wrapper around the `connections` table — each
row points one tenant at one GitHub source (a repo or an org) using
a tenant-scoped GH token (`gh_tokens.id`). The ingest worker walks
active connections every `cfg.Poll.Interval`; deleting a connection
stops further sync from that source.

Schema reminder (`migrations/0001_identity_config.sql:26-46`):

```sql
CREATE TABLE connections (
  id INTEGER PRIMARY KEY,
  tenant_id INTEGER NOT NULL,
  kind TEXT NOT NULL,                -- 'repo' | 'org'
  owner TEXT NOT NULL,
  name TEXT,                          -- required for kind='repo', nullable for 'org'
  token_id INTEGER NOT NULL,
  backfill_from TIMESTAMP NOT NULL,
  status TEXT NOT NULL DEFAULT 'active',
  last_sync_at TIMESTAMP,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX connections_repo_uniq ON connections(tenant_id, owner, name) WHERE kind = 'repo';
CREATE UNIQUE INDEX connections_org_uniq  ON connections(tenant_id, owner) WHERE kind = 'org';
```

### Authentication / authorisation

- All three routes mount behind `web.RequireSession(m)` — same
  pattern as `internal/api/tokens/tokens.go:51-56`.
- The handler resolves the caller's tenant via
  `tenantIDFromSession(ctx, q)` (lift the helper from
  `internal/api/tokens/tokens.go:165-179`; expose as a small shared
  helper in `internal/api/web/` so we don't duplicate it three more
  times in 0041–0044 — but if that's controversial, a local copy is
  fine for v1).
- Cross-tenant access is impossible: every query is filtered by
  `tenant_id`. Delete additionally re-fetches the row and verifies
  ownership before issuing the DELETE.

### Request shapes

`POST /api/v1/connections` body:

```jsonc
{
  "kind": "repo",                 // or "org"
  "owner": "octocat",
  "name": "hello-world",           // required when kind=repo; omit/null for org
  "token_id": 12,                  // gh_tokens.id, must belong to caller's tenant
  "backfill_from": "2025-08-01T00:00:00Z" // optional; defaults to now - cfg.Poll.BackfillDays
}
```

Validation, in order:

1. `kind` must be exactly `"repo"` or `"org"`. Anything else → 400.
2. `owner` is required (after trim). Empty → 400.
3. For `kind="repo"`, `name` is required (after trim). Empty / null → 400.
4. For `kind="org"`, `name` must be empty or null. Non-empty → 400.
5. `token_id` must reference a `gh_tokens` row owned by the caller's
   tenant. Lookup fails (`sql.ErrNoRows`) or cross-tenant → 400 with
   `"token_id is invalid"`. (Not 404 — the token may exist but
   belong to another tenant; we don't want to leak that.)
6. `backfill_from`, if present, must parse and be in the past. If
   absent, default to `time.Now().UTC().AddDate(0, 0,
   -cfg.Poll.BackfillDays)`.

If the partial-unique index trips on insert (duplicate
`(tenant_id, owner, name)` for `kind="repo"`, or duplicate
`(tenant_id, owner)` for `kind="org"`), respond 409 with body
`{"error":"connection already exists"}`. The sqlite driver returns
the error as a wrapped `*sqlite.Error` carrying
`extendedCode == 2067` (`SQLITE_CONSTRAINT_UNIQUE`); detect via
`strings.Contains(err.Error(), "UNIQUE constraint failed")` since
the modernc.org/sqlite Error type isn't a clean shape to type-assert
against (tokens.go doesn't ever face uniqueness; this is new
territory).

### Response shapes

`ConnectionDTO`:

```jsonc
{
  "id": 1,
  "kind": "repo",
  "owner": "octocat",
  "name": "hello-world",
  "token_id": 12,
  "backfill_from": "2025-08-01T00:00:00Z",
  "status": "active",
  "last_sync_at": "2026-05-19T13:00:00Z", // null if never synced
  "created_at": "2026-05-19T12:00:00Z"
}
```

`tenant_id` is never serialised on the wire — it's an internal
denormalisation column.

- `GET /api/v1/connections` → `200 {"connections": [...]}` ordered
  by `created_at` (sqlc query `ListConnectionsByTenant` already
  orders this way).
- `POST` → `201 {"connection": {...}}`.
- `DELETE /:id`:
  - `204` on success.
  - `404` if not found for this tenant (including cross-tenant).
  - `400` if `:id` is not an integer.
  - No conflict / referential check — deleting a connection orphans
    its `repos` / raw-event rows in v1, which is intentional: those
    rows are read-only historical data and removing them would
    silently destroy a tenant's metrics. A future task can add a
    "purge data" admin op.

### Scope notes / non-goals

- **No PATCH / update.** Editing a connection (e.g. swapping
  tokens, pausing) is a separate task; the master plan doesn't
  promise it in v1.
- **No `last_sync_at` writes.** That's the ingest worker's job (see
  `internal/storage/sqlite/queries/connections.sql:18-19`); this
  endpoint just surfaces the column.
- **No deep validation of `owner` / `name`.** GitHub will reject
  invalid handles at first sync — we don't replicate that regex
  here.
- **No webhook registration.** That's v1.1.
- **Token deletion conflict still belongs to the tokens handler**
  (existing `tokens.deleteHandler` already 409s when a connection
  references the token); nothing to change there.

## Acceptance criteria

- [ ] `internal/api/connections/connections.go` exposes a
      `Configure(e *echo.Echo, l *zap.Logger, m *intauth.Manager, q
      *sqlitedb.Queries, cfg *config.Config)` function that mounts:
      - `GET /api/v1/connections` (RequireSession)
      - `POST /api/v1/connections` (RequireSession)
      - `DELETE /api/v1/connections/:id` (RequireSession)
- [ ] DTO + request types live in the same package as the handlers.
      `ConnectionDTO` never includes `tenant_id`.
- [ ] `internal/api/run.go` wires `connections.Configure(...)` into
      `configureRoutes`. The Run signature gains `cfg *config.Config`
      access — already present, just pass it through.
- [ ] `internal/api/connections/connections_test.go` covers, using
      a real sqlite + the same `seedAndLogin` / `doJSON` pattern as
      `internal/api/tokens/tokens_test.go`:
      - `TestList_Empty` → `200 {"connections":[]}`.
      - `TestList_HappyPath` → after POSTing two, list returns both
        ordered by created_at.
      - `TestPost_Repo_HappyPath` → 201, body shape correct, no
        `tenant_id`, `backfill_from` set, `status="active"`.
      - `TestPost_Org_HappyPath` → 201, `name` is null in response.
      - `TestPost_DefaultsBackfill` → omit `backfill_from`, expect
        backfill_from ≈ `now - cfg.Poll.BackfillDays`.
      - `TestPost_BadKind_400`.
      - `TestPost_RepoWithoutName_400`.
      - `TestPost_OrgWithName_400`.
      - `TestPost_EmptyOwner_400`.
      - `TestPost_UnknownToken_400`.
      - `TestPost_CrossTenantToken_400` — seed a second tenant
        with its own token; POST referring to that token from the
        first tenant's session must 400, not leak existence.
      - `TestPost_DuplicateRepo_409`.
      - `TestPost_DuplicateOrg_409`.
      - `TestPost_NoCookie_401`.
      - `TestDelete_HappyPath_204`.
      - `TestDelete_NotFound_404`.
      - `TestDelete_BadID_400`.
      - `TestDelete_CrossTenant_404` — connection exists under
        tenant B; tenant A's session DELETEs by that id → 404,
        connection survives.
      - `TestDelete_NoCookie_401`.
- [ ] `go vet ./...`, `go build ./...`, `go test ./...` pass.
- [ ] `verify.sh` runs the standard four sections and exits 0.

## Files to touch

- `internal/api/connections/connections.go` (new).
- `internal/api/connections/connections_test.go` (new).
- `internal/api/run.go` — register the new Configure.
- `.plans/upnext/0040-api-connections/verify.sh`.

No sqlc changes — all required queries
(`CreateConnection`, `GetConnection`, `ListConnectionsByTenant`,
`DeleteConnection`) already exist
(`internal/storage/sqlite/queries/connections.sql`).

## Steps

### 1. Handler package

Create `internal/api/connections/connections.go` modelled on
`internal/api/tokens/tokens.go`. Lift the `tenantIDFromSession`
helper as a local function (duplication accepted — moving it to
`internal/api/web/` could happen in a follow-up after we see how
0041–0044 use it).

Key pieces:

- `ConnectionDTO` + `ListConnectionsResponse` /
  `CreateConnectionRequest` / `CreateConnectionResponse`.
- `Configure(e, l, m, q, cfg)`: mounts the three routes behind
  `web.RequireSession(m)`.
- `listHandler(q)`: filter by `tenantID`, map to DTO, return JSON.
- `createHandler(q, cfg)`: validate inputs, look up token, default
  backfill_from, INSERT, detect UNIQUE constraint and 409.
- `deleteHandler(q)`: ParseInt id, fetch+check tenant, DELETE,
  204.

Commit: `feat(api): connections CRUD handlers (#0040)`.

### 2. Wire into router

In `internal/api/run.go`:

```go
import "github.com/karnstack/tempo/internal/api/connections"
...
configureRoutes(e, l, m, r, a, q, box, cfg)
...
func configureRoutes(e *echo.Echo, l *zap.Logger, m *intauth.Manager, r *intauth.Registrar, a *intauth.Authenticator, q *sqlitedb.Queries, box *secret.Box, cfg *config.Config) {
    health.Configure(e, l)
    apiauth.Configure(e, l, m, r, a)
    me.Configure(e, l, m, q)
    tokens.Configure(e, l, m, q, box)
    connections.Configure(e, l, m, q, cfg)
    ...
}
```

Commit: `feat(api): mount connections routes (#0040)`.

### 3. Tests

Create `internal/api/connections/connections_test.go`. Lift the
test harness (`newIntegrationStore`, `seedAndLogin`, `doJSON`,
`tenantIDForLogin`) from `internal/api/tokens/tokens_test.go` —
duplication is the path of least pain here; if a shared package
emerges in 0041+ we can extract.

Each test case in acceptance criteria gets one func. Group by
verb for skimmability.

Commit: `test(api): connections coverage (#0040)`.

### 4. Verify

`./verify.sh` from the task dir. Same four-section format.

## Notes

- `backfill_from` precision: the spec stores TIMESTAMP. Go's
  `time.Time.UTC().Format(time.RFC3339Nano)` round-trips cleanly
  through modernc.org/sqlite, so just pass `time.Time` through —
  the driver handles serialisation.
- The 409 detection via `strings.Contains` is regrettable. The
  alternative is fishing into `*sqlite.Error.Code()` /
  `extendedCode`, but the modernc.org/sqlite error type lives in
  an internal struct field; the substring check is what
  `internal/auth/registrar.go` uses for the same constraint (worth
  checking — if there's a cleaner helper there, lift it).
- If a future change adds a `pending` initial status (e.g. for
  "queued for first sync"), this endpoint will need a body field
  too. v1 keeps it implicit: status is always "active" on create.
