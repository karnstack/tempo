---
id: 0017
slug: auth-register-firstrun
title: /auth/register + first-run gate
status: done
depends_on: [0016]
owner: ""
est_minutes: 0
tags: [auth]
autonomy: full
skills: []
---

## Goal

Land the first-run admin registration flow:

- `GET  /api/v1/auth/firstrun` — public; returns `{first_run: bool}` so the
  SPA knows whether to show `/register` or redirect to `/login`.
- `POST /api/v1/auth/register` — public; succeeds **only** when no users
  exist. Validates email + password, hashes via Argon2id (0015), creates
  the default tenant if absent, inserts the user with `role = "admin"`,
  issues a session cookie via the `auth.Manager` (0016), and returns the
  fresh user JSON. Rejects with **409 Conflict** when registration is
  closed (any user already exists).

Also wire `*auth.Manager` and a new `*auth.Registrar` into the fx graph
so 0018 (`/auth/login` + `/auth/logout`) can reuse them, and extend
`api.Run` to accept those deps.

## Acceptance criteria

- [ ] `GET  /api/v1/auth/firstrun` returns `200 {"first_run": true}` on a
      fresh database; `200 {"first_run": false}` once a user exists.
- [ ] `POST /api/v1/auth/register` with valid `{email, password}` on a
      fresh database returns `201` with the created user JSON, sets the
      `tempo_session` cookie (HttpOnly, Lax, MaxAge ≈ session duration),
      and persists a `sessions` row.
- [ ] Re-posting `/auth/register` after a user exists returns
      `409 {"message": "registration is closed"}`. No cookie is set, no
      DB row is added.
- [ ] Invalid email returns `400`. Password under 8 chars returns `400`.
      Empty/malformed JSON returns `400`. None of these paths create a
      user, tenant, or session.
- [ ] First successful register auto-creates the `default` tenant
      (`tenants` count goes 0 → 1) and assigns the new user to it.
- [ ] After register, the session cookie validates against
      `auth.Manager.Validate` (round-trip integration test against real
      SQLite + applied migrations).
- [ ] Email is case-insensitive: `Admin@Acme.test` → stored as
      `admin@acme.test`. Whitespace is trimmed before validation.
- [ ] `cmd/tempo/main.go` boots with the new fx providers; `go build` and
      `go vet` clean.
- [ ] `verify.sh` exits 0: `go vet` + `go build` + `go test` across
      `./internal/auth/...`, `./internal/api/...`, and the full module.

## Files to touch

- `internal/auth/register.go` (new) — `Registrar`, `UserStore` interface,
  `IsFirstRun`, `Register`, sentinel errors (`ErrNotFirstRun`,
  `ErrInvalidEmail`, `ErrPasswordTooShort`).
- `internal/auth/register_test.go` (new) — integration tests against a
  real `*sqlitedb.Queries` (reuse `newIntegrationStore` from
  `session_test.go`).
- `internal/auth/fx.go` (new) — `NewManagerFx(cfg, q)` and
  `NewRegistrarFx(q)` adapters so `cmd/tempo/main.go` can `fx.Provide`
  them without leaking constructor signatures.
- `internal/api/auth/router.go` (new) — `Configure(e, l, m, r)`
  registering `/api/v1/auth/firstrun` and `/api/v1/auth/register`.
- `internal/api/auth/firstrun.go` (new) — `GET` handler.
- `internal/api/auth/register.go` (new) — `POST` handler.
- `internal/api/auth/register_test.go` (new) — handler tests using a
  real fx-wired manager + integration store.
- `internal/api/run.go` — extend `Run` and `configureRoutes` to take
  `*auth.Manager` and `*auth.Registrar`; mount auth routes.
- `cmd/tempo/main.go` — `fx.Provide(sqlite.NewQueries,
  auth.NewManagerFx, auth.NewRegistrarFx)`.
- `verify.sh` — full vet/build/test run.

## Steps

### 1. Write `internal/auth/register.go`

The Registrar interface mirrors `SessionStore`: take a small
persistence subset that `*sqlitedb.Queries` already satisfies, so
production wiring is trivial and tests can swap in a fake later if
needed. For v1 we only ever expect 0 or 1 tenants; the implementation
is written to scale to N tenants without changing call sites.

```go
package auth

import (
    "context"
    "errors"
    "fmt"
    "regexp"
    "strings"

    "github.com/karnstack/tempo/internal/storage/sqlite/sqlitedb"
)

const (
    DefaultTenantName = "default"
    AdminRole         = "admin"
    MinPasswordLen    = 8
)

var (
    ErrNotFirstRun      = errors.New("auth: registration is closed")
    ErrInvalidEmail     = errors.New("auth: invalid email")
    ErrPasswordTooShort = errors.New("auth: password too short")
)

var emailRe = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

type UserStore interface {
    CountTenants(ctx context.Context) (int64, error)
    ListTenants(ctx context.Context) ([]sqlitedb.Tenant, error)
    CreateTenant(ctx context.Context, name string) (sqlitedb.Tenant, error)
    CountUsersByTenant(ctx context.Context, tenantID int64) (int64, error)
    CreateUser(ctx context.Context, arg sqlitedb.CreateUserParams) (sqlitedb.User, error)
}

type Registrar struct{ store UserStore }

func NewRegistrar(store UserStore) *Registrar { return &Registrar{store: store} }

func (r *Registrar) IsFirstRun(ctx context.Context) (bool, error) { /* see implementation */ }
func (r *Registrar) Register(ctx context.Context, email, password string) (sqlitedb.User, error) {
    /* normalize → validate → IsFirstRun → Hash → ensure tenant → CreateUser */
}
```

`IsFirstRun` short-circuits when `CountTenants == 0`. Otherwise it
walks `ListTenants` and sums per-tenant user counts (cheap; v1 has at
most one row).

`Register` order:
1. `email = strings.ToLower(strings.TrimSpace(email))`; reject via
   `emailRe`.
2. `len(password) < MinPasswordLen` → `ErrPasswordTooShort`.
3. `IsFirstRun` → false → `ErrNotFirstRun`.
4. `Hash(password)`.
5. Ensure tenant exists (create `DefaultTenantName` if absent).
6. `CreateUser(tenant, email, hash, "admin")`.

Commit: `feat(auth): registrar with first-run gate`.

### 2. Write `internal/auth/register_test.go`

`auth_test` package; reuse `newIntegrationStore` from
`session_test.go`. Cases:

- `TestIsFirstRun_EmptyDB_True`
- `TestIsFirstRun_AfterRegister_False`
- `TestRegister_FirstRun_CreatesTenantAndUser` — tenants 0 → 1, user
  has `Role == "admin"`, password verifies via `auth.Verify`.
- `TestRegister_SecondCall_ReturnsErrNotFirstRun` — DB unchanged.
- `TestRegister_LowercasesAndTrimsEmail`.
- `TestRegister_InvalidEmail_ReturnsErr`.
- `TestRegister_ShortPassword_ReturnsErr`.

Commit: `test(auth): registrar integration coverage`.

### 3. Write `internal/auth/fx.go`

```go
package auth

import (
    "github.com/karnstack/tempo/internal/config"
    "github.com/karnstack/tempo/internal/storage/sqlite/sqlitedb"
)

func NewManagerFx(cfg *config.Config, q *sqlitedb.Queries) *Manager {
    return NewManager(q, cfg.Session.Duration, cfg.Env == "production")
}

func NewRegistrarFx(q *sqlitedb.Queries) *Registrar { return NewRegistrar(q) }
```

Commit: `feat(auth): fx providers for manager + registrar`.

### 4. Write `internal/api/auth/` package

`router.go`:

```go
package auth

import (
    intauth "github.com/karnstack/tempo/internal/auth"
    "github.com/labstack/echo/v4"
    "go.uber.org/zap"
)

func Configure(e *echo.Echo, _ *zap.Logger, m *intauth.Manager, r *intauth.Registrar) {
    g := e.Group("/api/v1/auth")
    g.GET("/firstrun", firstRunHandler(r))
    g.POST("/register", registerHandler(m, r))
}
```

`firstrun.go` — wraps `web.WrapPublic`; calls `r.IsFirstRun`; returns
`{first_run}` as JSON.

`register.go` — wraps `web.WrapPublic`; binds `{email, password}`;
maps `Registrar` errors to status codes (400 for validation, 409 for
`ErrNotFirstRun`, 500 otherwise); calls `m.Issue`; returns `201` with
user DTO `{id, email, role}`.

Commit: `feat(api): /auth/firstrun + /auth/register handlers`.

### 5. Write `internal/api/auth/register_test.go`

Reuse the integration store pattern. For each case, build an
`echo.New()` and call `apiauth.Configure(e, l, m, r)` directly:

- `TestFirstRun_FreshDB_True`
- `TestFirstRun_AfterRegister_False`
- `TestRegister_HappyPath` — 201, response body matches user, cookie
  present, `sessions` table has 1 row whose id matches the cookie.
- `TestRegister_AlreadyClosed_409` — register once, second POST → 409,
  `users` count still 1.
- `TestRegister_InvalidEmail_400`.
- `TestRegister_ShortPassword_400`.
- `TestRegister_BadJSON_400`.

Commit: `test(api): /auth/firstrun + /auth/register coverage`.

### 6. Wire into `internal/api/run.go`

Extend `Run` and `configureRoutes` signatures:

```go
func Run(lc fx.Lifecycle, l *zap.Logger, cfg *config.Config,
    m *intauth.Manager, r *intauth.Registrar) error { … }

func configureRoutes(e *echo.Echo, l *zap.Logger,
    m *intauth.Manager, r *intauth.Registrar) {
    health.Configure(e, l)
    apiauth.Configure(e, l, m, r)
    e.GET("/*", echo.WrapHandler(webui.Handler())) // SPA fallback last
}
```

Use import aliases (`intauth`, `apiauth`) to avoid name clashes.

Commit: `feat(api): mount auth routes`.

### 7. Wire into `cmd/tempo/main.go`

Add to `fx.Provide`:

```go
sqlite.NewQueries,
auth.NewManagerFx,
auth.NewRegistrarFx,
```

Commit: `feat(cmd): wire auth manager + registrar`.

### 8. Refresh `verify.sh`

```bash
#!/usr/bin/env bash
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

echo "==> go vet ./internal/auth/... ./internal/api/..."
go vet ./internal/auth/... ./internal/api/...

echo "==> go build ./..."
go build ./...

echo "==> go test ./internal/auth/... ./internal/api/..."
go test ./internal/auth/... ./internal/api/... -count=1

echo "==> go test ./... (no regressions)"
go test ./... -count=1

echo "VERIFY OK"
```

Run it. Fix anything that breaks.

Commit (final): `chore(0017): verify script + RESULT.md`.

## Notes

- The `users` count check needs to walk all tenants because tenants
  are created lazily; checking `CountUsersByTenant(1)` would race with
  the bootstrap. Cheap in v1 (one tenant).
- Cookie `Secure` flag follows `cfg.Env == "production"` — matches
  the wiring `auth.Manager` already had implicitly.
- `web.WrapPublic` is used for both routes. Auth middleware lives on
  protected routes only (0038 onwards); these two are by definition
  reachable without a session.
- Email regex is deliberately loose. RFC 5321 is too permissive in
  practice and a strict regex is brittle; we want non-empty local +
  domain with a dot in the domain. The DB unique index catches the
  rest.
- No FK / CHECK constraints introduced (per the project's
  Go-enforced-constraints rule).
- Session sweep is still deferred to 0026 (the worker scheduler).
