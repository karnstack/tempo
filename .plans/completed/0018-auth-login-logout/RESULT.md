# 0018 — `/auth/login` + `/auth/logout` + middleware

## What changed

- `internal/auth/login.go` (new) — `Authenticator`, `LoginUserStore`
  interface, `Authenticate(email, password)`, `ErrInvalidCredentials`.
  Walks tenants because users are unique per `(tenant_id, email)`; v1
  has at most one tenant so this is one query in the happy path.
  Failure cases (wrong password, unknown email, malformed email,
  empty password) all collapse to `ErrInvalidCredentials` so a probing
  client can't enumerate accounts. Unknown-email and bad-input
  branches run `Verify` against a fake hash precomputed at init so
  the wall time is comparable to a wrong-password verify.
- `internal/auth/login_test.go` (new) — integration tests against
  real SQLite + applied migrations: happy path, wrong password,
  unknown email, malformed email, empty password,
  case-insensitive-email, plus a wall-time floor on the unknown-email
  branch to assert the fake-`Verify` actually ran.
- `internal/auth/fx.go` — adds `NewAuthenticatorFx(q)` so
  `cmd/tempo/main.go` can `fx.Provide` it directly.
- `internal/api/web/middleware.go` (new) — exported
  `RequireSession(m *auth.Manager) echo.MiddlewareFunc`. Behaviour
  identical to the old private version: 401 on miss/expired/unknown,
  attach session to the request context via `auth.IntoContext` on
  success.
- `internal/api/web/middleware_test.go` (new) — moved
  `TestRequireSession_*` cases (happy path, no cookie, unknown id,
  expired row) plus the in-test `fakeSessionStore`.
- `internal/api/middleware.go` — drops the private `requireSession`
  and the now-unused imports (`auth`, `net/http`).
- `internal/api/middleware_test.go` — drops `fakeSessionStore`,
  `newRequireSessionEcho`, and the `TestRequireSession_*` cases.
  Cleans up imports.
- `internal/api/auth/login.go` (new) — `POST /api/v1/auth/login`
  handler. Maps `ErrInvalidCredentials` → 401, bind failure → 400,
  issues the session cookie via `auth.Manager.Issue` on success,
  returns `200 {user: {id, email, role}}`.
- `internal/api/auth/logout.go` (new) — `POST /api/v1/auth/logout`
  handler. Always 204; relies on `auth.Manager.Revoke` being
  idempotent against missing/unknown cookies.
- `internal/api/auth/router.go` — `Configure` now takes
  `*intauth.Authenticator` and mounts `/login` + `/logout`.
- `internal/api/auth/login_test.go` (new) — handler-level
  integration tests covering all eight login cases (happy, wrong
  password, unknown email, malformed email, empty password,
  case-insensitive, bad JSON) and three logout cases (with session,
  no cookie, unknown session). Asserts the post-logout cookie is
  cleared and replaying the original cookie returns
  `ErrSessionUnknown`.
- `internal/api/run.go` — `Run` and `configureRoutes` now take
  `*intauth.Authenticator` and pass it through to `apiauth.Configure`.
- `cmd/tempo/main.go` — adds `auth.NewAuthenticatorFx` to
  `fx.Provide`.

## Verify output (last lines)

```
==> go vet ./internal/auth/... ./internal/api/...
==> go build ./...
==> go test ./internal/auth/... ./internal/api/...
ok  	github.com/karnstack/tempo/internal/auth	1.405s
ok  	github.com/karnstack/tempo/internal/api	1.015s
ok  	github.com/karnstack/tempo/internal/api/auth	1.825s
ok  	github.com/karnstack/tempo/internal/api/web	1.660s
==> go test ./... (no regressions)
ok  	github.com/karnstack/tempo/internal/api	0.406s
ok  	github.com/karnstack/tempo/internal/api/auth	1.290s
ok  	github.com/karnstack/tempo/internal/api/web	1.678s
ok  	github.com/karnstack/tempo/internal/auth	2.640s
ok  	github.com/karnstack/tempo/internal/config	0.869s
ok  	github.com/karnstack/tempo/internal/logger	2.430s
ok  	github.com/karnstack/tempo/internal/storage/sqlite	1.335s
VERIFY OK
```

## Followups

- 0038 (`/api/v1/me`) is the first protected feature route. It will
  mount `web.RequireSession(m)` on its group and pull the session
  via `auth.FromContext(ctx)`.
- Session sweep ticker is still deferred to 0026.
- The fake hash is package-private and computed once at init
  (~50ms one-time cost). If test wall time becomes a concern, swap
  it out via a private constructor; not needed today.
