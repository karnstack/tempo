# Result — 0026 ingest-worker-scheduler

## Summary

Landed the ingest worker scheduler — a single fx-managed goroutine that
ticks at `cfg.Poll.Interval` (default 15m, immediate first tick), iterates
every `connections.status='active'` row, opens a `sync_runs`, decrypts the
connection's PAT, builds a per-token `*github.Client`, dispatches to every
registered `Runner` in order, and closes the `sync_runs` row with the
outcome. `connections.last_sync_at` advances only on full success.
Runner errors short-circuit the connection (not the tick) and persist
onto `sync_runs.error`. v1 ships with zero runners wired — per-resource
fetchers (PRs/reviews/commits/deploys) plug in via the `"ingest.runners"`
fx value group across 0027–0030.

## What changed (file list)

- `internal/storage/sqlite/queries/connections.sql` — new
  `ListActiveConnections :many` query.
- `internal/storage/sqlite/sqlitedb/connections.sql.go`,
  `internal/storage/sqlite/sqlitedb/querier.go` — regenerated.
- `internal/ingest/ingest.go` — package doc tightened.
- `internal/ingest/runner.go` — new: `Runner` interface, `Outcome` struct,
  `NoopRunner`.
- `internal/ingest/runner_test.go` — new: interface + Noop assertions.
- `internal/ingest/scheduler.go` — new: `Scheduler` with `Tick(ctx)`,
  `Loop(ctx)`, `New(...)`, `WithClientBuilder/WithClock` options, plus
  helpers for sync_run lifecycle and per-connection client build.
- `internal/ingest/scheduler_test.go` — new: hermetic integration tests
  against real sqlite (no mocks). Covers happy/error/inactive paths plus
  Loop's immediate first tick and ctx-cancellation behavior.
- `internal/ingest/run.go` — new: fx `Run(RunParams)` hooks the loop into
  `fx.Lifecycle`.
- `cmd/tempo/main.go` — `fx.Invoke(ingest.Run)` next to `api.Run`.

## verify.sh output (tail)

```
==> sqlc diff (verify generated SQL bindings are in sync)
==> go vet ./internal/ingest/...
==> go build ./...
==> go test ./internal/ingest/... -race -count=1
ok  	github.com/karnstack/tempo/internal/ingest	2.341s
==> go test ./... -race -count=1 (no regressions)
... 21 packages, all pass ...
ok  	github.com/karnstack/tempo/internal/ingest	3.591s
VERIFY OK
```

## Smoke

`go run ./cmd/tempo` boots with the new scheduler:

```
INFO  ingest scheduler started  interval=30s runners=0
INFO  started
...
SIGTERM →
INFO  ingest scheduler stopping
INFO  OnStop hook executed (ingest.Run)
```

On a DB with no migrations applied, `ListActiveConnections` logs
`SQL logic error: no such table: connections` and the goroutine
continues — this is the same boot behavior as any other consumer of
`*sqlitedb.Queries` against an unmigrated DB. The real boot path applies
migrations before the lifecycle starts.

## Followups (out of scope for 0026)

- **0027–0030** plug per-resource Runners into the `"ingest.runners"`
  value group. They'll also populate `Outcome.RateLimitRemaining` by
  reading `X-RateLimit-Remaining` from their last response.
- **0031** adds `sync_status` aggregation on top of the `sync_runs`
  rows this task produces (per-connection latest state + error visibility
  for the `/sync/status` endpoint in 0044).
- **0044** surfaces the `RateLimitRemaining` field through the API for
  the sync-status panel.
- **0032+** add the daily rollup scheduler — separate goroutine, same
  fx lifecycle pattern as this one.
