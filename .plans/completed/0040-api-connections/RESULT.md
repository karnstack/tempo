# 0040 — /api/v1/connections CRUD

## Files changed

- `internal/api/connections/connections.go` — new package. Three
  routes mounted under `/api/v1` behind `web.RequireSession`:
  - `GET /connections` → `ListConnectionsResponse`
  - `POST /connections` → `CreateConnectionResponse`
  - `DELETE /connections/:id` → 204
  The handler validates `kind in {repo, org}` with the
  corresponding name rule, looks up `token_id` and verifies tenant
  ownership (returning a generic 400 to avoid leaking existence),
  defaults `backfill_from` to `now - cfg.Poll.BackfillDays`, and
  maps SQLite UNIQUE constraint violations to 409 via substring
  match on the error text.
- `internal/api/connections/connections_test.go` — 19 integration
  tests against a real sqlite + auth chain.
- `internal/api/run.go` — wires `connections.Configure(...)` into
  `configureRoutes`; threads `*config.Config` through so the create
  handler can read `cfg.Poll.BackfillDays`.

## Verify output

```
== sqlc diff ==
== go vet ==
== go build ==
== go test (api) ==
ok  	github.com/karnstack/tempo/internal/api	0.669s
ok  	github.com/karnstack/tempo/internal/api/auth	1.631s
ok  	github.com/karnstack/tempo/internal/api/connections	(cached)
?   	github.com/karnstack/tempo/internal/api/health	[no test files]
ok  	github.com/karnstack/tempo/internal/api/me	1.862s
ok  	github.com/karnstack/tempo/internal/api/tokens	3.330s
ok  	github.com/karnstack/tempo/internal/api/web	2.262s
```

## Notes / followups

- **`tenantIDFromSession` duplicated from `tokens.go`.** Three
  handlers will likely need the same helper (0041–0043). Lift to a
  shared package (probably `internal/api/web/session.go`) once a
  second consumer lands; pre-extracting now would risk an awkward
  API.
- **409 via substring match.** The modernc.org/sqlite Error type
  doesn't expose a stable extended-code field, so unique-violation
  detection uses `strings.Contains(err.Error(), "UNIQUE constraint
  failed")`. Fragile but acceptable for v1; if modernc.org/sqlite
  ever exposes `*sqlite.Error.Code()` as a public method we can
  swap to that.
- **Cross-tenant token returns 400, not 404.** Deliberate — 404
  would let an attacker discover that a token id exists by
  comparing 400 ("invalid") vs 404 ("not found") responses across
  tenants. Both bad inputs collapse to the same 400.
- **No PATCH / pause-and-resume.** Out of scope for v1; the master
  plan doesn't list it. The ingest worker already skips
  `status != 'active'` connections so the building blocks are
  there.
- **Delete is destructive but doesn't cascade.** Removing a
  connection orphans its `repos` / raw-event rows. Intentional:
  cascade would silently destroy a tenant's history. A future
  "purge data" admin op can opt into that explicitly.
- **`backfill_from` round-trip via modernc.org/sqlite is clean.**
  The driver formats `time.Time` as RFC3339Nano text on write and
  parses it back on read, so the wire-shape JSON matches what got
  stored.
