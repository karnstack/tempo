---
id: 0038
slug: api-me
title: /api/v1/me + auth middleware
status: done
depends_on: [0018]
owner: ""
est_minutes: 45
tags: [api]
autonomy: full
skills: []
---

## Goal

Wire the existing `web.RequireSession` middleware onto the first protected route group and ship the inaugural authenticated endpoint: `GET /api/v1/me`. This task is the template that 0039 (tokens), 0040 (connections), 0041–0044 (metrics + sync) will copy: a feature package under `internal/api/<feature>/` with a `Configure(e, ...)` function that mounts a protected `/api/v1` subroute.

Out of scope: tokens, connections, metrics. Those have their own tasks. We also do not introduce a shared `protected` group helper — each feature `Configure` mounts its own group with `web.RequireSession(m)` so feature wiring stays one indirection deep.

Spec reference: `docs/superpowers/specs/2026-05-08-tempo-design.md` line 211 (`GET /me` in the API surface). Master plan: line 174.

## Design decisions

- **`UserDTO` moves to `internal/api/web`.** It currently lives in `internal/api/auth/register.go` but `me`, `tokens`, and friends will all return user-shaped payloads. Hoisting to `web` (which both register/login and me already import) avoids a duplicate definition and keeps the response shape canonical.
- **`me.Configure` owns the protected `/api/v1` group itself.** No shared group helper. Pattern matches `apiauth.Configure(e, ...)` exactly. When 0039+ land they each mount `e.Group("/api/v1", web.RequireSession(m))` again — Echo treats these as independent groups, no collision.
- **Orphan session → 401 + clear cookie.** If `RequireSession` validates a row whose `user_id` no longer exists (admin deleted the user manually), `GetUser` returns `sql.ErrNoRows`. Returning 500 would be a lie — the request is unauthenticated, just for an unusual reason. Return 401 and clear the cookie so the SPA bounces to login. The orphan session row is left alone (deleting it in a GET feels surprising; the next sweep cleans it up via expiry anyway).
- **`*sqlitedb.Queries` flows into `api.Run` via fx.** It's already provided by `sqlite.NewQueries` (see `cmd/tempo/main.go:24`). Adding the param makes it available for me + the rest of Phase 7.
- **No middleware on the `e.Use` level.** Auth is per-group, not global, because `/api/v1/auth/*` and the SPA fallback must remain public.

## Acceptance criteria

- [ ] `internal/api/web/dto.go` exports `UserDTO{ID, Email, Role}` with the same JSON shape as today.
- [ ] `internal/api/auth/register.go` and `login.go` import `UserDTO` from `web`; the duplicate definition in `register.go` is gone.
- [ ] `internal/api/me/me.go` exports `Configure(e *echo.Echo, l *zap.Logger, m *intauth.Manager, q *sqlitedb.Queries)` that mounts `GET /api/v1/me` behind `web.RequireSession(m)`.
- [ ] `MeResponse{User: web.UserDTO}` is the response shape.
- [ ] `internal/api/run.go` takes `*sqlitedb.Queries` and calls `me.Configure(e, l, m, q)`.
- [ ] Behavioural tests in `internal/api/me/me_test.go` against a real SQLite + migrations:
  - **200 happy path**: register seeds an admin, login writes the cookie, `GET /me` with the cookie returns `{user:{id,email,role}}`.
  - **401 no cookie**: `GET /me` with no cookie returns 401.
  - **401 unknown session**: `GET /me` with a bogus cookie returns 401.
  - **401 expired session**: pre-insert a session row whose `expires_at` is in the past → 401 (the existing `RequireSession` test already covers this branch but we re-assert through the real handler stack).
  - **401 orphan user**: register, login, then delete the user row directly via `q.DeleteUser`, replay the cookie → 401, response carries a clearing `Set-Cookie`.
- [ ] `go vet ./...`, `go build ./...`, `go test ./internal/api/... -race -count=1` all pass.
- [ ] `verify.sh` exits 0.

## Files to touch

- `internal/api/web/dto.go` (new)
- `internal/api/auth/register.go` (drop UserDTO; import from web)
- `internal/api/auth/login.go` (use web.UserDTO)
- `internal/api/me/me.go` (new)
- `internal/api/me/me_test.go` (new)
- `internal/api/run.go` (add `*sqlitedb.Queries` param + me.Configure call)
- `cmd/tempo/main.go` (no change — fx already provides `*sqlitedb.Queries`)
- `.plans/upnext/0038-api-me/verify.sh` (replace stub)

## Steps

### 1. Hoist UserDTO into `internal/api/web`

Create `internal/api/web/dto.go`:

```go
package web

// UserDTO is the canonical JSON shape for a user row in API responses. The
// password hash is intentionally never serialised.
type UserDTO struct {
	ID    int64  `json:"id"`
	Email string `json:"email"`
	Role  string `json:"role"`
}
```

Edit `internal/api/auth/register.go`: delete the local `UserDTO` definition, change `RegisterResponse.User` to `web.UserDTO`, change the construction site to `web.UserDTO{...}`.

Edit `internal/api/auth/login.go`: same — `LoginResponse.User` becomes `web.UserDTO`, construction site too.

Build to make sure nothing dangles:

```
go build ./...
```

Commit: `refactor(api): lift UserDTO into internal/api/web for reuse`

### 2. Create the me handler

`internal/api/me/me.go`:

```go
// Package me hosts the GET /api/v1/me handler — the smallest authenticated
// endpoint, used by the SPA to bootstrap the current-user state.
package me

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/karnstack/tempo/internal/api/web"
	intauth "github.com/karnstack/tempo/internal/auth"
	"github.com/karnstack/tempo/internal/storage/sqlite/sqlitedb"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

// MeResponse is the wire shape returned by GET /me.
type MeResponse struct {
	User web.UserDTO `json:"user"`
}

// Configure mounts GET /api/v1/me behind RequireSession.
func Configure(e *echo.Echo, _ *zap.Logger, m *intauth.Manager, q *sqlitedb.Queries) {
	g := e.Group("/api/v1", web.RequireSession(m))
	g.GET("/me", meHandler(m, q))
}

func meHandler(m *intauth.Manager, q *sqlitedb.Queries) echo.HandlerFunc {
	return web.WrapPublic(func(ctx *web.Context) error {
		sess, ok := intauth.FromContext(ctx.Request().Context())
		if !ok {
			// RequireSession should have ensured this; defence in depth.
			return echo.NewHTTPError(http.StatusUnauthorized, "unauthorized")
		}
		user, err := q.GetUser(ctx.Request().Context(), sess.UserID)
		if errors.Is(err, sql.ErrNoRows) {
			// Session row points at a deleted user. Clear the cookie so
			// the SPA bounces to login on the next request.
			_ = m.Revoke(ctx.Request().Context(), ctx.Response(), ctx.Request())
			return echo.NewHTTPError(http.StatusUnauthorized, "unauthorized")
		}
		if err != nil {
			ctx.L.Error("get user failed", zap.Error(err))
			return echo.NewHTTPError(http.StatusInternalServerError, "lookup failed")
		}
		return ctx.JSON(http.StatusOK, MeResponse{User: web.UserDTO{
			ID:    user.ID,
			Email: user.Email,
			Role:  user.Role,
		}})
	})
}
```

Commit: `feat(api): GET /api/v1/me behind RequireSession`

### 3. Wire into `internal/api/run.go`

Add the `*sqlitedb.Queries` param to `Run` and `configureRoutes`, and call `me.Configure(e, l, m, q)`. Imports: add `"github.com/karnstack/tempo/internal/api/me"` and `"github.com/karnstack/tempo/internal/storage/sqlite/sqlitedb"`.

Build:

```
go build ./...
```

Commit: `feat(api): wire me.Configure into router`

### 4. Write integration tests

`internal/api/me/me_test.go`. Mirror the harness in `internal/api/auth/register_test.go` (real SQLite + migrations + a small Echo with both auth and me mounted). Key paths:

```go
package me_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	apiauth "github.com/karnstack/tempo/internal/api/auth"
	"github.com/karnstack/tempo/internal/api/me"
	intauth "github.com/karnstack/tempo/internal/auth"
	"github.com/karnstack/tempo/internal/config"
	"github.com/karnstack/tempo/internal/storage/sqlite"
	"github.com/karnstack/tempo/internal/storage/sqlite/sqlitedb"
	"github.com/karnstack/tempo/migrations"
	"github.com/labstack/echo/v4"
	"go.uber.org/fx/fxtest"
	"go.uber.org/zap/zaptest"
)

const seedEmail = "admin@acme.test"
const seedPassword = "hunter22hunter22"

func newIntegrationStore(t *testing.T) *sqlitedb.Queries {
	t.Helper()
	lc := fxtest.NewLifecycle(t)
	l := zaptest.NewLogger(t)
	path := filepath.Join(t.TempDir(), "me_integration.db")
	cfg := &config.Config{Database: config.Database{
		Driver: "sqlite", DSN: path, Raw: "sqlite://" + path,
	}}
	s, err := sqlite.New(lc, l, cfg)
	if err != nil {
		t.Fatalf("sqlite.New: %v", err)
	}
	if err := migrations.Apply(context.Background(), s.DB()); err != nil {
		t.Fatalf("migrations.Apply: %v", err)
	}
	lc.RequireStart()
	t.Cleanup(lc.RequireStop)
	return sqlitedb.New(s.DB())
}

func newMeEcho(t *testing.T, q *sqlitedb.Queries) (*echo.Echo, *intauth.Manager) {
	t.Helper()
	m := intauth.NewManager(q, time.Hour, false)
	r := intauth.NewRegistrar(q)
	a := intauth.NewAuthenticator(q)
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	l := zaptest.NewLogger(t)
	apiauth.Configure(e, l, m, r, a)
	me.Configure(e, l, m, q)
	return e, m
}

func loginAndCookie(t *testing.T, e http.Handler) *http.Cookie {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
		strings.NewReader(`{"email":"`+seedEmail+`","password":"`+seedPassword+`"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login: status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == intauth.CookieName {
			return c
		}
	}
	t.Fatal("login did not set session cookie")
	return nil
}

func seedAdmin(t *testing.T, q *sqlitedb.Queries) sqlitedb.User {
	t.Helper()
	user, err := intauth.NewRegistrar(q).Register(context.Background(), seedEmail, seedPassword)
	if err != nil {
		t.Fatalf("seed Register: %v", err)
	}
	return user
}

func TestMe_HappyPath_200(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e, _ := newMeEcho(t, q)
	user := seedAdmin(t, q)
	cookie := loginAndCookie(t, e)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp me.MeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.User.ID != user.ID {
		t.Errorf("user.id = %d, want %d", resp.User.ID, user.ID)
	}
	if resp.User.Email != seedEmail {
		t.Errorf("user.email = %q, want %q", resp.User.Email, seedEmail)
	}
	if resp.User.Role != intauth.AdminRole {
		t.Errorf("user.role = %q, want %q", resp.User.Role, intauth.AdminRole)
	}
}

func TestMe_NoCookie_401(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e, _ := newMeEcho(t, q)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", rec.Code, rec.Body.String())
	}
}

func TestMe_UnknownSession_401(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e, _ := newMeEcho(t, q)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req.AddCookie(&http.Cookie{Name: intauth.CookieName, Value: "nope"})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestMe_ExpiredSession_401(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e, _ := newMeEcho(t, q)
	user := seedAdmin(t, q)

	expiredID := "expired-session-id"
	if _, err := q.CreateSession(context.Background(), sqlitedb.CreateSessionParams{
		ID:        expiredID,
		UserID:    user.ID,
		ExpiresAt: time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatalf("seed expired session: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req.AddCookie(&http.Cookie{Name: intauth.CookieName, Value: expiredID})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestMe_OrphanUser_401_ClearsCookie(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e, _ := newMeEcho(t, q)
	user := seedAdmin(t, q)
	cookie := loginAndCookie(t, e)

	if err := q.DeleteUser(context.Background(), user.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", rec.Code, rec.Body.String())
	}
	var cleared *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == intauth.CookieName {
			cleared = c
		}
	}
	if cleared == nil {
		t.Fatal("expected clearing cookie on orphan-user 401")
	}
	if cleared.MaxAge >= 0 {
		t.Errorf("clearing cookie MaxAge = %d, want < 0", cleared.MaxAge)
	}
}
```

Run:

```
go test ./internal/api/... -race -count=1
```

Commit: `test(api): /me coverage incl. orphan user + expired session`

### 5. Replace verify.sh

```bash
#!/usr/bin/env bash
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

echo "==> go vet ./..."
go vet ./...
echo "  ok"

echo "==> go build ./..."
go build ./...
echo "  ok"

echo "==> go test ./internal/api/... -race -count=1"
go test ./internal/api/... -race -count=1
echo "  ok"

echo "VERIFY OK"
```

### 6. Run verify

```
./.plans/upnext/0038-api-me/verify.sh
```

## Notes

- We deliberately do not delete the orphan session row inside the GET handler — sweeping is the `Manager.SweepExpired` path's job, and side-effecting from a read endpoint surprises the next reader of this code. The cleared cookie is enough to keep the SPA flow correct.
- Future Phase 7 features (`tokens`, `connections`, etc.) repeat the `e.Group("/api/v1", web.RequireSession(m))` line in their own `Configure`. Echo deduplicates middleware per route, so this is fine and keeps each feature's wiring local.
- `*sqlitedb.Queries` flowing into `api.Run` is the moment the API layer stops being storage-agnostic at the wiring level. That's intentional — we still have the `Storage` seam at the boundary, but Phase 7 endpoints all want sqlc-typed reads, so the convenience wins.
