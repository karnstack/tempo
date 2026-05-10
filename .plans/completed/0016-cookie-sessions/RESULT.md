# 0016 — Server-validated cookie sessions

## What changed

- `internal/auth/session.go` (new) — `Manager`, `SessionStore` interface, sentinel errors (`ErrNoSession`/`ErrSessionUnknown`/`ErrSessionExpired`), `Issue`/`Validate`/`Revoke`/`SweepExpired`, and `IntoContext`/`FromContext` helpers. Cookie carries a base64url-encoded 32-byte random id; server is the only authority on validity.
- `internal/auth/session_test.go` (new) — fake-store unit cases plus one in-memory SQLite integration test that round-trips `Issue → Validate → Revoke → Validate(ErrSessionUnknown)` against a real `*sqlitedb.Queries`.
- `internal/api/middleware.go` — added `requireSession(m *auth.Manager) echo.MiddlewareFunc`. On success it injects the session into the request context; on failure it returns `echo.NewHTTPError(401, "unauthorized")`.
- `internal/api/middleware_test.go` — happy path + three 401 cases (no cookie, unknown id, expired row).

Not wired into any route yet — that's 0017/0018's job.

## Verify output (last lines)

```
==> go vet ./internal/auth/... ./internal/api/...
  ok
==> go build ./...
  ok
==> go test ./internal/auth/... -count=1
ok  	github.com/karnstack/tempo/internal/auth	0.601s
  ok
==> go test ./internal/api/... -count=1
ok  	github.com/karnstack/tempo/internal/api	0.349s
  ok
==> go test ./... (no regressions)
ok  	github.com/karnstack/tempo/internal/api	0.360s
ok  	github.com/karnstack/tempo/internal/auth	2.417s
ok  	github.com/karnstack/tempo/internal/config	0.817s
ok  	github.com/karnstack/tempo/internal/logger	1.335s
ok  	github.com/karnstack/tempo/internal/storage/sqlite	1.905s
  ok
VERIFY OK
```

## Followups

- 0017 (`/auth/register` + first-run gate) and 0018 (`/auth/login` + `/auth/logout` + applied middleware) will fx-wire `*auth.Manager` and call `Issue`/`Revoke`. They'll also be the first place to mount `requireSession` on a real handler group.
- A daily ticker calling `Manager.SweepExpired` is intentionally not wired here. Defer until we have a worker scheduler (0026) — sessions accumulating for a few days in the meantime is harmless because `Validate` already rejects expired rows.
