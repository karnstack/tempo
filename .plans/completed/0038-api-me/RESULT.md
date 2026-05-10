# 0038 — /api/v1/me + auth middleware

## What changed

- `internal/api/web/dto.go` (new) — canonical `UserDTO{ID,Email,Role}` JSON shape, hoisted out of the auth package so Phase 7 endpoints share one definition.
- `internal/api/auth/register.go` — drops local `UserDTO`, imports `web.UserDTO`.
- `internal/api/auth/login.go` — uses `web.UserDTO` in `LoginResponse`.
- `internal/api/me/me.go` (new) — `Configure(e, l, m, q)` mounts `GET /api/v1/me` behind `web.RequireSession(m)`. Handler reads session from ctx → `q.GetUser` → `MeResponse{User: web.UserDTO}`. Orphan-user (session points to deleted user row) → 401 with clearing cookie.
- `internal/api/me/me_test.go` (new) — 5 integration tests against real SQLite + migrations: 200 happy path, 401 no cookie, 401 unknown session, 401 expired row, 401 orphan user (asserts clearing `Set-Cookie`).
- `internal/api/run.go` — `Run` and `configureRoutes` now take `*sqlitedb.Queries`; calls `me.Configure(e, l, m, q)`. fx already provides `*sqlitedb.Queries` via `sqlite.NewQueries`.

## Commits

- `7adc28e` refactor(api): lift UserDTO into internal/api/web for reuse
- `66829df` feat(api): GET /api/v1/me behind RequireSession
- `2ce98af` feat(api): wire me.Configure into router
- `263165c` test(api): /me coverage incl. orphan user + expired session

## Verify output

```
==> go vet ./...
  ok
==> go build ./...
  ok
==> go test ./internal/api/... -race -count=1
ok  	github.com/karnstack/tempo/internal/api	3.716s
ok  	github.com/karnstack/tempo/internal/api/auth	7.849s
?   	github.com/karnstack/tempo/internal/api/health	[no test files]
ok  	github.com/karnstack/tempo/internal/api/me	7.544s
ok  	github.com/karnstack/tempo/internal/api/web	4.721s
  ok
VERIFY OK
```

## Followups

- Phase 7 follow-ons (0039 tokens, 0040 connections, 0041–0044 metrics + sync) repeat the `e.Group("/api/v1", web.RequireSession(m))` line in their own `Configure`. No shared protected-group helper introduced — pattern is explicit and one-indirection-deep per feature.
- Orphan session row left in place on the 401 path; `Manager.SweepExpired` cleans it up via expiry. If admin deletion of users becomes common we can add a targeted cleanup query later.
