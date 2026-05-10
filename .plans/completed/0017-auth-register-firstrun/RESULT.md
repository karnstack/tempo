# 0017 — `/auth/register` + first-run gate

## What changed

- `internal/auth/register.go` (new) — `Registrar`, `UserStore` interface,
  `IsFirstRun`, `Register`, sentinel errors (`ErrNotFirstRun`,
  `ErrInvalidEmail`, `ErrPasswordTooShort`). Email is normalised
  (lowercased + trimmed); validation is loose-regex (non-empty local,
  non-empty domain with a dot). On first run the default tenant is
  bootstrapped lazily before the user row is inserted.
- `internal/auth/register_test.go` (new) — integration tests against
  real SQLite + applied migrations: empty-DB first-run, tenant
  bootstrap, role assignment, password hashing/verification, second
  call returns `ErrNotFirstRun` with DB unchanged, email
  case-insensitivity, invalid-email table, short-password rejection,
  validation-fails-before-write guarantee.
- `internal/auth/fx.go` (new) — `NewManagerFx(cfg, q)` and
  `NewRegistrarFx(q)` adapters so `cmd/tempo/main.go` can `fx.Provide`
  them directly. Cookie `Secure` flag follows
  `cfg.Env == "production"`.
- `internal/api/auth/router.go` (new) — `Configure(e, l, m, r)` mounts
  the `/api/v1/auth` group.
- `internal/api/auth/firstrun.go` (new) — `GET /api/v1/auth/firstrun`
  returns `{first_run: bool}`.
- `internal/api/auth/register.go` (new) — `POST /api/v1/auth/register`
  binds `{email, password}`, maps `Registrar` errors to 400/409,
  issues the session cookie via `auth.Manager` on success, returns
  `201` with `{user: {id, email, role}}`.
- `internal/api/auth/register_test.go` (new) — handler-level
  integration tests: 200 firstrun states, 201 happy path with
  cookie-validates-against-the-manager round-trip, 409 once a user
  exists (no cookie, DB unchanged), 400 paths for invalid email,
  short password, malformed JSON.
- `internal/api/run.go` — `Run` and `configureRoutes` now take
  `*auth.Manager` and `*auth.Registrar`; auth group mounted alongside
  health; SPA fallback stays last.
- `cmd/tempo/main.go` — `fx.Provide(sqlite.NewQueries,
  auth.NewManagerFx, auth.NewRegistrarFx)`.

## Verify output (last lines)

```
==> go vet ./internal/auth/... ./internal/api/...
==> go build ./...
==> go test ./internal/auth/... ./internal/api/...
ok  	github.com/karnstack/tempo/internal/auth	1.347s
ok  	github.com/karnstack/tempo/internal/api	1.440s
ok  	github.com/karnstack/tempo/internal/api/auth	0.493s
==> go test ./... (no regressions)
ok  	github.com/karnstack/tempo/internal/api	0.333s
ok  	github.com/karnstack/tempo/internal/api/auth	0.710s
ok  	github.com/karnstack/tempo/internal/auth	1.947s
ok  	github.com/karnstack/tempo/internal/config	1.976s
ok  	github.com/karnstack/tempo/internal/logger	1.182s
ok  	github.com/karnstack/tempo/internal/storage/sqlite	2.733s
VERIFY OK
```

## Followups

- 0018 (`/auth/login` + `/auth/logout` + applied middleware) reuses
  `*auth.Manager` and the `users` row created here. Login will need a
  `GetUserByEmail`-style query against the registrar's tenant; the
  generated `sqlitedb.GetUserByEmail` already exists.
- A `/api/v1/me` endpoint behind `requireSession` is not wired here —
  that's 0038's job.
- Session sweep ticker still deferred to 0026.
