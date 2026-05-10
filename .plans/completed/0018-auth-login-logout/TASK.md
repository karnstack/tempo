---
id: 0018
slug: auth-login-logout
title: /auth/login + /auth/logout + middleware
status: done
depends_on: [0017]
owner: ""
est_minutes: 0
tags: [auth]
autonomy: full
skills: []
---

## Goal

Land the admin login + logout flow and expose the session-required
middleware so future protected feature packages can mount it directly:

- `POST /api/v1/auth/login` — public; validates `{email, password}` against
  the stored `users` row, verifies the Argon2id hash (0015), issues a
  session cookie via `auth.Manager.Issue` (0016), and returns the user
  JSON. **All credential failures collapse to `401`** (wrong password,
  unknown email, malformed email) so a probing client cannot enumerate
  accounts. Bad/empty JSON body still returns `400`. Login also runs a
  constant-time fake-`Verify` when the email is unknown so the wall-clock
  for a successful and failed lookup is comparable.
- `POST /api/v1/auth/logout` — public + idempotent; deletes the server
  session row identified by the cookie (if any) and writes a clearing
  `Set-Cookie`. Returns `204 No Content` regardless of whether a session
  was found, so a stale browser tab can call it safely.
- Move `requireSession` middleware out of the private `internal/api`
  package into `internal/api/web` (the leaf package every feature
  imports anyway) and **export it as `web.RequireSession`**. `internal/api`
  no longer owns it; future feature `Configure(e, l, m, ...)` calls
  attach it to their protected groups. No production routes are
  protected yet — that's 0038's job.

## Acceptance criteria

- [ ] `POST /api/v1/auth/login` with the registered email + correct
      password returns `200`, sets `tempo_session` (HttpOnly, Lax),
      persists a `sessions` row whose id matches the cookie, and the
      cookie validates against the same `auth.Manager`.
- [ ] `POST /api/v1/auth/login` with the registered email + wrong
      password returns `401 {"message": "invalid credentials"}`. No
      cookie set, no session row created.
- [ ] `POST /api/v1/auth/login` with an unknown email returns `401`
      (same body) and runs Argon2 once against a fixed fake hash so the
      response time is comparable to a wrong-password 401.
- [ ] `POST /api/v1/auth/login` with a malformed email or empty
      password returns `401` (collapsed into the same response — no
      400 hint that would let a client probe accounts).
- [ ] `POST /api/v1/auth/login` with malformed JSON returns `400`.
- [ ] Login is case-insensitive on email: `Admin@Acme.test` matches
      the `admin@acme.test` row.
- [ ] `POST /api/v1/auth/logout` with a valid session cookie returns
      `204`, deletes the matching `sessions` row, and emits a clearing
      `Set-Cookie` (`MaxAge=-1`).
- [ ] `POST /api/v1/auth/logout` with no cookie returns `204` and
      still emits the clearing `Set-Cookie` (no error).
- [ ] `POST /api/v1/auth/logout` with an unknown session id returns
      `204` and clears the cookie. No 500.
- [ ] After a login → logout round trip, replaying the original cookie
      against `auth.Manager.Validate` returns `ErrSessionUnknown`.
- [ ] `web.RequireSession(m)` exists, returns 401 on missing/unknown/
      expired cookies, and on success places the validated session in
      the request context (handlers can read it via `auth.FromContext`).
      The old private `requireSession` in `internal/api/middleware.go`
      is removed; its tests live alongside the new public middleware.
- [ ] `cmd/tempo/main.go` boots with the new `auth.NewAuthenticatorFx`
      provider; `go build` and `go vet` clean.
- [ ] `verify.sh` exits 0: `go vet` + `go build` + `go test` over
      `./internal/auth/...`, `./internal/api/...`, and the full module.

## Files to touch

- `internal/auth/login.go` (new) — `Authenticator`, `LoginUserStore`
  interface, `Authenticate`, `ErrInvalidCredentials`. Constant-time
  fake-`Verify` when the email is missing.
- `internal/auth/login_test.go` (new) — integration tests against a
  real `*sqlitedb.Queries` covering happy path, wrong password,
  unknown email, malformed email, case-insensitivity, fake-verify
  branch (assert `Verify` was called by checking it returned cleanly
  even with no user — via a counting fake or by checking total wall
  time is non-trivial).
- `internal/auth/fx.go` (modify) — add `NewAuthenticatorFx(q)`.
- `internal/api/web/middleware.go` (new) — exported
  `RequireSession(m *auth.Manager) echo.MiddlewareFunc`. Behaviour
  identical to the old private version: 401 on miss/expired, attach
  session to ctx via `auth.IntoContext` on success.
- `internal/api/web/middleware_test.go` (new) — moved
  `TestRequireSession_*` cases (happy path, no cookie, unknown id,
  expired row) plus the in-test `fakeSessionStore`.
- `internal/api/middleware.go` (modify) — drop the private
  `requireSession` function and the now-unused `auth` import.
- `internal/api/middleware_test.go` (modify) — drop `fakeSessionStore`
  and `TestRequireSession_*` cases (now in `web/`); leave
  `requestLogger` cases intact.
- `internal/api/auth/login.go` (new) — `POST /login` handler.
  Constant-time error mapping: `ErrInvalidCredentials` → 401; bind
  failure → 400.
- `internal/api/auth/logout.go` (new) — `POST /logout` handler.
  Always 204 after `m.Revoke`.
- `internal/api/auth/router.go` (modify) — register new routes; take
  `*intauth.Authenticator` in `Configure`.
- `internal/api/auth/login_test.go` (new) — handler integration tests
  for the eight bullets above.
- `internal/api/run.go` — extend `Run` and `configureRoutes` to take
  `*intauth.Authenticator`; pass through to `apiauth.Configure`.
- `cmd/tempo/main.go` — `fx.Provide(auth.NewAuthenticatorFx)`.
- `verify.sh` — full vet/build/test run.

## Steps

### 1. Write `internal/auth/login.go`

Mirror the `Registrar` shape: small persistence interface, walk
tenants in v1 since email uniqueness is per-tenant. The fake hash is
computed once at init from a constant password — its only job is to
spend ~Argon2id-equivalent time inside `Verify`.

```go
package auth

import (
    "context"
    "errors"
    "fmt"
    "strings"

    "github.com/karnstack/tempo/internal/storage/sqlite/sqlitedb"
)

// ErrInvalidCredentials is returned for any login failure visible to
// a probing client: wrong password, unknown email, malformed email.
// Collapsing all three prevents account enumeration.
var ErrInvalidCredentials = errors.New("auth: invalid credentials")

// LoginUserStore is the persistence subset Authenticator needs.
type LoginUserStore interface {
    ListTenants(ctx context.Context) ([]sqlitedb.Tenant, error)
    GetUserByEmail(ctx context.Context, arg sqlitedb.GetUserByEmailParams) (sqlitedb.User, error)
}

type Authenticator struct{ store LoginUserStore }

func NewAuthenticator(store LoginUserStore) *Authenticator { return &Authenticator{store: store} }

// fakeHash burns one Verify worth of CPU when no user is found, so the
// success and failure paths take comparable wall time. Computed once.
var fakeHash = mustFakeHash()

func mustFakeHash() string {
    h, err := Hash("placeholder-for-timing-equivalence")
    if err != nil {
        panic(fmt.Errorf("auth: precompute fake hash: %w", err))
    }
    return h
}

func (a *Authenticator) Authenticate(ctx context.Context, email, password string) (sqlitedb.User, error) {
    email = strings.ToLower(strings.TrimSpace(email))
    if !emailRe.MatchString(email) || password == "" {
        // Burn the fake-verify budget so timing matches the unknown-user path.
        _, _ = Verify(password, fakeHash)
        return sqlitedb.User{}, ErrInvalidCredentials
    }

    tenants, err := a.store.ListTenants(ctx)
    if err != nil {
        return sqlitedb.User{}, fmt.Errorf("auth: list tenants: %w", err)
    }

    var (
        user  sqlitedb.User
        found bool
    )
    for _, t := range tenants {
        u, err := a.store.GetUserByEmail(ctx, sqlitedb.GetUserByEmailParams{TenantID: t.ID, Email: email})
        if err == nil {
            user, found = u, true
            break
        }
        // sql.ErrNoRows is the no-match signal; surface anything else.
        if !errors.Is(err, sql.ErrNoRows) {
            return sqlitedb.User{}, fmt.Errorf("auth: get user: %w", err)
        }
    }

    if !found {
        _, _ = Verify(password, fakeHash)
        return sqlitedb.User{}, ErrInvalidCredentials
    }

    ok, err := Verify(password, user.PasswordHash)
    if err != nil {
        return sqlitedb.User{}, fmt.Errorf("auth: verify: %w", err)
    }
    if !ok {
        return sqlitedb.User{}, ErrInvalidCredentials
    }
    return user, nil
}
```

Add `database/sql` to imports.

Commit: `feat(auth): authenticator with constant-time login`.

### 2. Write `internal/auth/login_test.go`

`auth_test` package; reuse `newIntegrationStore` from
`session_test.go`. Cases:

- `TestAuthenticate_HappyPath` — register a user, then
  `Authenticate(email, pw)` returns the user.
- `TestAuthenticate_WrongPassword_ErrInvalidCredentials`.
- `TestAuthenticate_UnknownEmail_ErrInvalidCredentials` — even on a
  fresh DB with no tenants.
- `TestAuthenticate_UnknownEmail_RunsFakeVerify` — assert wall time
  is at least, say, ~10ms (Argon2id baseline) so the fake-verify
  branch ran. Cheap timing assertion; not flaky-tight.
- `TestAuthenticate_MalformedEmail_ErrInvalidCredentials` — input
  `"not-an-email"`.
- `TestAuthenticate_CaseInsensitive` — register `admin@acme.test`,
  authenticate with `Admin@Acme.test`.
- `TestAuthenticate_EmptyPassword_ErrInvalidCredentials`.

Commit: `test(auth): authenticator integration coverage`.

### 3. Update `internal/auth/fx.go`

Add:

```go
func NewAuthenticatorFx(q *sqlitedb.Queries) *Authenticator { return NewAuthenticator(q) }
```

Commit: `feat(auth): fx provider for authenticator`.

### 4. Move `requireSession` → `internal/api/web/middleware.go`

Create the new file:

```go
package web

import (
    "net/http"

    "github.com/karnstack/tempo/internal/auth"
    "github.com/labstack/echo/v4"
)

// RequireSession returns middleware that 401s any request without a
// valid session cookie. On success the validated session is attached
// to the request context so handlers can pull it via auth.FromContext.
func RequireSession(m *auth.Manager) echo.MiddlewareFunc {
    return func(next echo.HandlerFunc) echo.HandlerFunc {
        return func(c echo.Context) error {
            sess, err := m.Validate(c.Request().Context(), c.Request())
            if err != nil {
                return echo.NewHTTPError(http.StatusUnauthorized, "unauthorized")
            }
            r := c.Request()
            c.SetRequest(r.WithContext(auth.IntoContext(r.Context(), sess)))
            return next(c)
        }
    }
}
```

Then create `internal/api/web/middleware_test.go` with a
`web_test` package, the `fakeSessionStore` helper, and the
`TestRequireSession_*` cases pulled from
`internal/api/middleware_test.go` (renamed to call `web.RequireSession`).

Edit `internal/api/middleware.go`: delete the `requireSession` function
and remove the `auth` import (still need `logger`, `echo`, `zap`,
`time`, `net/http` for `requestLogger`).

Edit `internal/api/middleware_test.go`: delete `fakeSessionStore`,
`newRequireSessionEcho`, and all `TestRequireSession_*` cases. Drop
the now-unused imports (`auth`, `sqlitedb`, `database/sql`, `fmt`,
`time`).

Commit: `refactor(api): expose RequireSession via web package`.

### 5. Write `internal/api/auth/login.go`

```go
package auth

import (
    "errors"
    "net/http"

    "github.com/karnstack/tempo/internal/api/web"
    intauth "github.com/karnstack/tempo/internal/auth"
    "github.com/labstack/echo/v4"
    "go.uber.org/zap"
)

type LoginRequest struct {
    Email    string `json:"email"`
    Password string `json:"password"`
}

type LoginResponse struct {
    User UserDTO `json:"user"`
}

func loginHandler(a *intauth.Authenticator, m *intauth.Manager) echo.HandlerFunc {
    return web.WrapPublic(func(ctx *web.Context) error {
        var req LoginRequest
        if err := ctx.Bind(&req); err != nil {
            return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
        }

        user, err := a.Authenticate(ctx.Request().Context(), req.Email, req.Password)
        switch {
        case errors.Is(err, intauth.ErrInvalidCredentials):
            return echo.NewHTTPError(http.StatusUnauthorized, "invalid credentials")
        case err != nil:
            ctx.L.Error("login failed", zap.Error(err))
            return echo.NewHTTPError(http.StatusInternalServerError, "login failed")
        }

        if _, err := m.Issue(ctx.Request().Context(), ctx.Response(), user.ID); err != nil {
            ctx.L.Error("issue session failed", zap.Error(err))
            return echo.NewHTTPError(http.StatusInternalServerError, "session issue failed")
        }
        return ctx.JSON(http.StatusOK, LoginResponse{User: UserDTO{
            ID:    user.ID,
            Email: user.Email,
            Role:  user.Role,
        }})
    })
}
```

Commit: `feat(api): /auth/login handler`.

### 6. Write `internal/api/auth/logout.go`

```go
package auth

import (
    "net/http"

    "github.com/karnstack/tempo/internal/api/web"
    intauth "github.com/karnstack/tempo/internal/auth"
    "github.com/labstack/echo/v4"
    "go.uber.org/zap"
)

func logoutHandler(m *intauth.Manager) echo.HandlerFunc {
    return web.WrapPublic(func(ctx *web.Context) error {
        if err := m.Revoke(ctx.Request().Context(), ctx.Response(), ctx.Request()); err != nil {
            ctx.L.Error("revoke session failed", zap.Error(err))
            return echo.NewHTTPError(http.StatusInternalServerError, "logout failed")
        }
        return ctx.NoContent(http.StatusNoContent)
    })
}
```

Commit: `feat(api): /auth/logout handler`.

### 7. Update `internal/api/auth/router.go`

```go
func Configure(e *echo.Echo, _ *zap.Logger, m *intauth.Manager, r *intauth.Registrar, a *intauth.Authenticator) {
    g := e.Group("/api/v1/auth")
    g.GET("/firstrun", firstRunHandler(r))
    g.POST("/register", registerHandler(m, r))
    g.POST("/login", loginHandler(a, m))
    g.POST("/logout", logoutHandler(m))
}
```

Commit: `feat(api): mount /auth/login + /auth/logout`.

### 8. Write `internal/api/auth/login_test.go`

Reuse the integration-store + `newAuthEcho` pattern from
`register_test.go`. Update `newAuthEcho` to also build the
`Authenticator` and pass it to `Configure`. Cases:

- `TestLogin_HappyPath` — seed via `Registrar.Register`, POST
  `/login` → 200, body has user, cookie set, `Validate` succeeds.
- `TestLogin_WrongPassword_401`.
- `TestLogin_UnknownEmail_401`.
- `TestLogin_MalformedEmail_401`.
- `TestLogin_EmptyPassword_401`.
- `TestLogin_CaseInsensitiveEmail_200`.
- `TestLogin_BadJSON_400`.
- `TestLogout_WithSession_204_DeletesRow` — login first, capture
  cookie, POST `/logout` with cookie → 204, replaying cookie against
  `m.Validate` returns `ErrSessionUnknown`.
- `TestLogout_NoCookie_204` — fresh request, no session, → 204, no
  panic, response carries the clearing `Set-Cookie`.
- `TestLogout_UnknownSession_204` — POST with a fake cookie value
  not in DB → 204, clearing cookie present.

Commit: `test(api): /auth/login + /auth/logout coverage`.

### 9. Wire `Authenticator` into `internal/api/run.go`

Extend `Run` and `configureRoutes` signatures to take
`*intauth.Authenticator`, pass through to `apiauth.Configure`.

Commit: `feat(api): wire authenticator into router`.

### 10. Wire into `cmd/tempo/main.go`

Add `auth.NewAuthenticatorFx` to `fx.Provide`.

Commit: `feat(cmd): wire authenticator`.

### 11. Refresh `verify.sh`

Identical structure to 0017 — `go vet` + `go build` + targeted tests
+ full module tests. See the file written in step 12.

### 12. Run `verify.sh` and fix anything red

Commit (final): `chore(0018): verify script + RESULT.md`.

## Notes

- Login intentionally collapses 400-style cred validation into 401 so
  a probing client can't tell "format is bad" from "user is unknown"
  from "password is wrong". Bad JSON is the only 400 because that's
  about the protocol, not the credentials.
- The fake hash is computed once at package init. It costs one
  Argon2id pass at startup (~50ms with `DefaultParams`), which is
  acceptable for a single-binary admin tool. Tests may want to
  replace `Authenticator.fakeHash` if they care about speed — keep
  it package-private for now and revisit only if test wall-time
  becomes an issue.
- The `LoginUserStore` walks tenants. v1 has at most one row, so
  this is one query in the happy path. The shape leaves room for
  multi-tenant lookup later (e.g. a `GetUserByEmailGlobal` query) by
  swapping the iteration for a single call without changing the
  authenticator's signature.
- `m.Revoke` is already safe against missing/unknown cookies — it
  no-ops the delete branch and always writes the clearing cookie. So
  the logout handler is just `Revoke` + 204.
- Moving `requireSession` into `web` is the smallest change that
  unblocks future feature packages from mounting it themselves
  without dragging in `internal/api`. This keeps the package import
  graph: `feature → web → auth`, with `internal/api` only at the
  top of the tree.
- No FK / CHECK constraints introduced (per the project's
  Go-enforced-constraints rule).
- Session sweep ticker is still deferred to 0026.
