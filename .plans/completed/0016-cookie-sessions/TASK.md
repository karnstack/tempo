---
id: 0016
slug: cookie-sessions
title: Server-validated cookie sessions
status: done
depends_on: [0012, 0015]
owner: ""
est_minutes: 50
tags: [auth]
autonomy: full
skills: []
---

## Goal

Ship the cookie-session primitive that 0017 (`/auth/register`) and 0018 (`/auth/login` + `/auth/logout` + applied middleware) will sit on top of. Concretely, deliver a small `auth.Manager` with three operations — `Issue`, `Validate`, `Revoke` — plus an echo middleware constructor that turns a successful `Validate` into an `unauthorized → handler` gate.

The session backing store is the existing `sessions(id, user_id, expires_at)` table created in 0008 and the sqlc-generated `CreateSession`/`GetSession`/`DeleteSession`/`DeleteUserSessions`/`DeleteExpiredSessions` queries from 0012. No JWTs: the cookie carries a 32-byte random id, the server looks the row up, the row's `expires_at` is the only authority on whether the session is still good.

This task does **not** wire the middleware into any routes (no `/api/v1/*` namespace exists yet) and does **not** create the register/login handlers (those are 0017/0018). It delivers the building block plus the echo middleware constructor.

Spec reference: `docs/superpowers/specs/2026-05-08-tempo-design.md`
- line 62 ("Cookie sessions, server-validated"),
- line 79 ("Sessions are server-side rows; cookie carries a random session id. No JWTs"),
- line 90 (`sessions(id, user_id, expires_at)`),
- line 287 (`TEMPO_SECRET` powers session encryption — but for this task we use it only as a sanity dependency; the session id itself is just a 32-byte random value, no signing needed because the server validates against the DB row).

## Design decisions

- **Random id, no signing.** The session cookie is a base64url-encoded 32-byte random value (256 bits of entropy). Because every request validates against the DB row, an attacker who guesses an id has to guess all 256 bits to win — no HMAC needed. We deliberately do **not** mint a signed token: the server is always the authority, and signed tokens add a second invalidation path (key rotation) we don't want to manage.
- **Cookie attributes.** `HttpOnly` (no JS access), `SameSite=Lax` (the SPA is same-origin so Lax is sufficient and avoids the SameSite=None+Secure CSRF surface), `Secure` only when `cfg.Env == "production"` (otherwise dev-on-localhost can't set the cookie over plain HTTP), `Path=/`, `MaxAge` and `Expires` both set to the configured `cfg.Session.Duration` so old browsers and modern ones both honour it.
- **`Validate` deletes expired rows opportunistically.** When `Validate` finds a row whose `expires_at` is in the past, it best-effort `DeleteSession`s it before returning `ErrSessionExpired`. Errors from the delete are swallowed — the user is going to be 401'd either way, and a sweeper (`SweepExpired`) is exposed so a future ticker can catch the rest. We do **not** wire that ticker in this task.
- **`Revoke` is idempotent.** Missing cookie → still write the clearing `Set-Cookie` and return nil. Cookie present but row already gone → still clear. The caller (`/auth/logout` in 0018) shouldn't have to care about the prior state.
- **No "remember me" toggle, no rolling expiry.** v1 keeps the session model boring: one duration, fixed at issue time. If we want sliding-window renewal later, it's a one-method addition (`Touch`) — but it's not needed for the admin-only first version.
- **`SessionStore` interface.** The Manager depends on a small interface (`CreateSession`/`GetSession`/`DeleteSession`/`DeleteExpiredSessions`) that `*sqlitedb.Queries` satisfies. This keeps tests fast (a fake store needs ~30 lines) and makes a future postgres `*pgdb.Queries` a drop-in. Importing `sqlitedb` types in the interface signature is fine — these are POD structs and a parallel `pgdb` package can be generated to mirror them when postgres lands.
- **No fx wiring of the manager into `api.Run` yet.** 0017/0018 will plumb `*auth.Manager` into the feature `Configure`s as they need it. Adding a no-op consumer now is dead code.

## Acceptance criteria

- [ ] `internal/auth/session.go` exports:
  - `CookieName = "tempo_session"`.
  - `SessionStore` interface with the four methods listed above (signatures match `*sqlitedb.Queries`).
  - `Manager` struct with `NewManager(store SessionStore, duration time.Duration, secure bool) *Manager`.
  - `(*Manager).Issue(ctx, w http.ResponseWriter, userID int64) (sqlitedb.Session, error)` — generates a fresh id, writes the row, sets the cookie.
  - `(*Manager).Validate(ctx, r *http.Request) (sqlitedb.Session, error)` — reads the cookie, looks up the row, checks `expires_at > now`. Returns sentinel errors for the three failure modes.
  - `(*Manager).Revoke(ctx, w, r) error` — deletes the row (if any) and writes the clearing cookie.
  - `(*Manager).SweepExpired(ctx) error` — calls `DeleteExpiredSessions(ctx, now)`.
  - Sentinel errors: `ErrNoSession`, `ErrSessionUnknown`, `ErrSessionExpired`.
  - Context helpers: `IntoContext(ctx, sqlitedb.Session) context.Context`, `FromContext(ctx) (sqlitedb.Session, bool)`.
- [ ] `internal/auth/session_test.go` covers (against a fake `SessionStore` unless noted):
  - `Issue` writes a row with the right `user_id` + `expires_at` (= `now + duration`), sets a `Set-Cookie` header with `HttpOnly`, `SameSite=Lax`, `Secure=false` for `secure=false` and `Secure=true` for `secure=true`, and a `MaxAge` matching the duration.
  - `Issue` produces a different cookie value across calls (entropy check — call twice, assert `≠`).
  - `Validate` happy path: round-trip an `Issue`d cookie back through `Validate`, get the same row.
  - `Validate` with no cookie returns `ErrNoSession`.
  - `Validate` with an unknown id returns `ErrSessionUnknown`.
  - `Validate` with an expired row returns `ErrSessionExpired` **and** the fake store sees a `DeleteSession(id)` call (opportunistic cleanup).
  - `Revoke` deletes the matching row and writes a clearing cookie (`MaxAge=-1`).
  - `Revoke` with no cookie still writes the clearing cookie and returns nil.
  - `SweepExpired` calls through to the store with `now`.
  - One **integration** test: open an in-memory SQLite via the existing test helper, apply migrations, get a real `*sqlitedb.Queries`, create a tenant + user, run `Issue → Validate → Revoke → Validate(ErrSessionUnknown)` end-to-end. Proves the interface lines up with the generated code.
- [ ] `internal/api/middleware.go` gains `requireSession(m *auth.Manager) echo.MiddlewareFunc`. On success it injects the session into `c.Request().Context()` (via `auth.IntoContext`) and calls `next`. On failure it returns `echo.NewHTTPError(401, "unauthorized")` (let the existing requestLogger log it as a warn).
- [ ] `internal/api/middleware_test.go` covers `requireSession`:
  - Happy path: a request carrying a valid cookie reaches the inner handler, and the inner handler can pull the session out via `auth.FromContext`.
  - 401s for: no cookie, unknown id, expired row.
- [ ] `go vet ./...`, `go build ./...`, `go test ./... -count=1` all pass.
- [ ] `verify.sh` exits 0.

## Files to touch

- `internal/auth/session.go` (new)
- `internal/auth/session_test.go` (new)
- `internal/api/middleware.go` (add `requireSession` + the `auth` import)
- `internal/api/middleware_test.go` (add the new test cases — leave existing requestLogger tests untouched)
- `.plans/upnext/0016-cookie-sessions/verify.sh` (replace stub)

## Steps

### 1. Add the session module

Create `internal/auth/session.go`:

```go
package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/karnstack/tempo/internal/storage/sqlite/sqlitedb"
)

// CookieName is the cookie that carries the server-issued session id.
const CookieName = "tempo_session"

// sessionIDBytes is the entropy of a freshly minted session id. 32 bytes →
// 256 bits, base64url-encoded to a 43-character cookie value. Server-side
// validation makes this overkill, which is exactly what we want.
const sessionIDBytes = 32

// Sentinel errors returned by Validate.
var (
	ErrNoSession      = errors.New("auth: no session cookie")
	ErrSessionUnknown = errors.New("auth: session not found")
	ErrSessionExpired = errors.New("auth: session expired")
)

// SessionStore is the persistence subset Manager needs. *sqlitedb.Queries
// satisfies it natively; a future *pgdb.Queries will too.
type SessionStore interface {
	CreateSession(ctx context.Context, arg sqlitedb.CreateSessionParams) (sqlitedb.Session, error)
	GetSession(ctx context.Context, id string) (sqlitedb.Session, error)
	DeleteSession(ctx context.Context, id string) error
	DeleteExpiredSessions(ctx context.Context, now time.Time) error
}

// Manager issues, validates, and revokes server-side sessions backed by the
// sessions table. The cookie carries a random id; the server is always the
// authority on whether the session is still valid.
type Manager struct {
	store    SessionStore
	duration time.Duration
	secure   bool
	now      func() time.Time
}

// NewManager builds a Manager. secure should be true in production (sets the
// Secure cookie attribute so the browser only ships it over HTTPS) and false
// in dev so localhost-over-HTTP works.
func NewManager(store SessionStore, duration time.Duration, secure bool) *Manager {
	return &Manager{
		store:    store,
		duration: duration,
		secure:   secure,
		now:      time.Now,
	}
}

// Issue creates a new session row for userID and writes the cookie to w.
// Returns the persisted Session so callers (login handler) can log id/expiry.
func (m *Manager) Issue(ctx context.Context, w http.ResponseWriter, userID int64) (sqlitedb.Session, error) {
	id, err := newSessionID()
	if err != nil {
		return sqlitedb.Session{}, err
	}
	expiresAt := m.now().Add(m.duration)
	sess, err := m.store.CreateSession(ctx, sqlitedb.CreateSessionParams{
		ID:        id,
		UserID:    userID,
		ExpiresAt: expiresAt,
	})
	if err != nil {
		return sqlitedb.Session{}, fmt.Errorf("auth: persist session: %w", err)
	}
	http.SetCookie(w, m.cookie(id, expiresAt))
	return sess, nil
}

// Validate reads the cookie from r, looks up the session row, and checks that
// it has not expired. Expired rows are best-effort deleted on the way out.
func (m *Manager) Validate(ctx context.Context, r *http.Request) (sqlitedb.Session, error) {
	c, err := r.Cookie(CookieName)
	if err != nil || c.Value == "" {
		return sqlitedb.Session{}, ErrNoSession
	}
	sess, err := m.store.GetSession(ctx, c.Value)
	if err != nil {
		return sqlitedb.Session{}, ErrSessionUnknown
	}
	if !sess.ExpiresAt.After(m.now()) {
		_ = m.store.DeleteSession(ctx, sess.ID)
		return sqlitedb.Session{}, ErrSessionExpired
	}
	return sess, nil
}

// Revoke deletes the session row identified by the request's cookie (if any)
// and writes a clearing Set-Cookie header. Idempotent: a missing cookie or a
// row that's already gone still results in a clean clearing cookie + nil.
func (m *Manager) Revoke(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
	if c, err := r.Cookie(CookieName); err == nil && c.Value != "" {
		if err := m.store.DeleteSession(ctx, c.Value); err != nil {
			return fmt.Errorf("auth: delete session: %w", err)
		}
	}
	http.SetCookie(w, m.clearCookie())
	return nil
}

// SweepExpired deletes every session row whose expires_at is in the past.
// Intended for a low-frequency ticker (daily); not wired in this task.
func (m *Manager) SweepExpired(ctx context.Context) error {
	return m.store.DeleteExpiredSessions(ctx, m.now())
}

func (m *Manager) cookie(id string, expiresAt time.Time) *http.Cookie {
	return &http.Cookie{
		Name:     CookieName,
		Value:    id,
		Path:     "/",
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: http.SameSiteLaxMode,
		Expires:  expiresAt,
		MaxAge:   int(m.duration.Seconds()),
	}
}

func (m *Manager) clearCookie() *http.Cookie {
	return &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	}
}

func newSessionID() (string, error) {
	b := make([]byte, sessionIDBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("auth: read random: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// --- context plumbing ---

type ctxKey int

const sessionCtxKey ctxKey = 1

// IntoContext returns a child context carrying sess. Used by middleware so
// downstream handlers can pull the session out without re-validating.
func IntoContext(ctx context.Context, sess sqlitedb.Session) context.Context {
	return context.WithValue(ctx, sessionCtxKey, sess)
}

// FromContext returns the session attached to ctx, if any.
func FromContext(ctx context.Context) (sqlitedb.Session, bool) {
	s, ok := ctx.Value(sessionCtxKey).(sqlitedb.Session)
	return s, ok
}
```

Commit: `feat(auth): cookie session manager (issue/validate/revoke)`

### 2. Add the session tests

Create `internal/auth/session_test.go`. Use a fake `SessionStore` for the unit cases and the in-memory SQLite helper from `internal/storage/sqlite/repo_test.go` for the one integration case. The fake should be ~30 lines: a map keyed by id, plus a `deleted []string` slice so tests can assert opportunistic cleanup.

Skeleton:

```go
package auth_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/karnstack/tempo/internal/auth"
	"github.com/karnstack/tempo/internal/storage/sqlite/sqlitedb"
)

type fakeStore struct {
	rows    map[string]sqlitedb.Session
	deleted []string
}

func newFakeStore() *fakeStore { return &fakeStore{rows: map[string]sqlitedb.Session{}} }

func (f *fakeStore) CreateSession(_ context.Context, arg sqlitedb.CreateSessionParams) (sqlitedb.Session, error) {
	s := sqlitedb.Session{ID: arg.ID, UserID: arg.UserID, ExpiresAt: arg.ExpiresAt, CreatedAt: time.Now()}
	f.rows[arg.ID] = s
	return s, nil
}

func (f *fakeStore) GetSession(_ context.Context, id string) (sqlitedb.Session, error) {
	s, ok := f.rows[id]
	if !ok {
		return sqlitedb.Session{}, sql.ErrNoRows // or any non-nil error; Validate maps it to ErrSessionUnknown
	}
	return s, nil
}

func (f *fakeStore) DeleteSession(_ context.Context, id string) error {
	f.deleted = append(f.deleted, id)
	delete(f.rows, id)
	return nil
}

func (f *fakeStore) DeleteExpiredSessions(_ context.Context, now time.Time) error {
	for id, s := range f.rows {
		if !s.ExpiresAt.After(now) {
			delete(f.rows, id)
		}
	}
	return nil
}
```

Then write the cases listed in Acceptance criteria. Use `httptest.NewRecorder()` and `httptest.NewRequest()` for cookie round-tripping. To exercise the expired-row path, seed the store directly with a row whose `ExpiresAt` is in the past, then build a request carrying that id as a cookie.

For the integration case, mirror the `newTestStore` pattern from `internal/storage/sqlite/repo_test.go` (read it first to find the helper signature). Steps inside the test:
1. Open in-memory DB + apply migrations + grab `*sqlitedb.Queries`.
2. Create a tenant + user (use the existing `CreateTenant` / `CreateUser` queries).
3. `m := auth.NewManager(q, time.Hour, false)`.
4. `Issue` → assert returned `Session.UserID == user.ID` and a cookie was set on the recorder.
5. New request carrying that cookie → `Validate` → assert returned id matches.
6. `Revoke` → assert another `Validate` against the same cookie returns `ErrSessionUnknown`.

Commit: `test(auth): cookie session manager unit + integration`

### 3. Add the echo `requireSession` middleware

Edit `internal/api/middleware.go`. Add this below `requestLogger`:

```go
// requireSession returns echo middleware that 401s any request without a valid
// session cookie. On success the validated session is attached to the request
// context so handlers can pull it via auth.FromContext.
func requireSession(m *auth.Manager) echo.MiddlewareFunc {
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

Add the imports (`net/http`, `github.com/karnstack/tempo/internal/auth`).

Commit: `feat(api): requireSession echo middleware`

### 4. Test the middleware

Edit `internal/api/middleware_test.go`. Build a tiny per-test echo app, mount a stub handler that pulls the session via `auth.FromContext` and writes the user id, wrap it in `requireSession(manager)`, then exercise the four cases (happy path + 3 failures). Use the same `fakeStore` shape as in step 2 — copy or extract.

Commit: `test(api): requireSession middleware happy path + 401s`

### 5. Replace verify.sh

Replace the stub with a script that runs vet/build/test on the touched packages and then the whole tree:

```bash
#!/usr/bin/env bash
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

echo "==> go vet ./internal/auth/... ./internal/api/..."
go vet ./internal/auth/... ./internal/api/...
echo "  ok"

echo "==> go build ./..."
go build ./...
echo "  ok"

echo "==> go test ./internal/auth/... -count=1"
go test ./internal/auth/... -count=1
echo "  ok"

echo "==> go test ./internal/api/... -count=1"
go test ./internal/api/... -count=1
echo "  ok"

echo "==> go test ./... (no regressions)"
go test ./... -count=1
echo "  ok"

echo "VERIFY OK"
```

### 6. Run verify

```
./.plans/upnext/0016-cookie-sessions/verify.sh
```

## Notes

- The opportunistic delete inside `Validate` swallows errors on purpose — the user is going to be 401'd either way and a daily `SweepExpired` will catch any rows we miss. Don't propagate the delete error.
- `MaxAge` and `Expires` are both set on the cookie. `MaxAge` wins on modern browsers but `Expires` keeps very old user agents honest. They agree, so neither one lies.
- We don't bind the session to the IP or User-Agent. That historically locks users out when they switch networks (laptop → phone hotspot) and the entropy of the random id already makes hijack-by-guessing infeasible. If we ever need that, it's a `validate_extra(*http.Request) error` hook on Manager.
- `auth.FromContext` returns the zero `sqlitedb.Session` and `false` when no session is attached. Handlers gated behind `requireSession` can assume `ok == true`.
- The `sql.ErrNoRows` import in the fake store skeleton above is just one option — any non-nil error works; `Validate` maps every store error to `ErrSessionUnknown`. Pick whatever keeps the fake honest.
