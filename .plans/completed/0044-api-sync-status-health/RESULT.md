# 0044 — /api/v1/sync/status + system/health

## Files changed

- `internal/api/sync/sync.go` — new package; mounts
  `GET /api/v1/sync/status` behind RequireSession. For each
  tenant connection it returns the connection's identifier fields
  plus `ingest.StatusFor`'s three snapshots
  (latest_run / last_success / last_failure), with nil-safe pointer
  mapping so absent run kinds emit JSON null.
- `internal/api/sync/sync_test.go` — 5 integration tests.
- `internal/api/run.go` — `apisync.Configure(...)` wired (aliased
  to avoid the `sync` stdlib name collision).

`/api/v1/system/health` was already public via
`internal/api/health/` — no change there.

## Verify output

```
== sqlc diff ==
== go vet ==
== go build ==
== go test (api) ==
... all api packages OK
```

## Notes / followups

- **Stdlib import collision.** `internal/api/sync` collides with
  Go's `sync` package; imported as `apisync` everywhere it's
  referenced. The package's own name stays `sync` for URL/path
  consistency with the route.
- **Tenant isolation via connections list.** `sync_runs` rows are
  joined to connections, which are tenant-scoped. Listing the
  tenant's connections and then per-connection `StatusFor` is the
  isolation boundary; no extra WHERE clause needed.
- **No live updates.** v1 returns a snapshot; the dashboard
  refetches on a timer. SSE / websockets are a later concern.
- **`tenantIDFromSession` duplicated for the fifth time.** No
  extraction in this task; will be cleaner to lift after the test
  expectations stabilise across all five callers.
