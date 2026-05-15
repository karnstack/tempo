---
id: 0026
slug: ingest-worker-scheduler
title: Worker scheduler (ticker per-connection iteration)
status: done
depends_on: [0019, 0011]
owner: ""
est_minutes: 75
tags: [ingest]
autonomy: full
skills: [systematic-debugging]
---

## Goal

Lay down the ingest worker scheduler — the goroutine that, every
`TEMPO_POLL_INTERVAL` (default 15m), iterates every active `connection`,
opens a `sync_runs` row, decrypts the connection's PAT, builds a per-token
`*github.Client`, dispatches to a list of registered `Runner`s, and closes
the `sync_runs` row with the outcome. **Per-resource fetching arrives in
0027–0030**: this task ships only the loop, lifecycle, sync-run accounting,
and the `Runner` interface those tasks plug into.

Architecture (matches spec lines 59–66 and 124–142):

- One goroutine, owned by fx `Lifecycle`. `OnStart` launches it; `OnStop`
  cancels its context and waits (bounded by `OnStop`'s ctx deadline).
- `time.Ticker` at `cfg.Poll.Interval`. First tick fires immediately on
  boot (90-day backfill shouldn't wait 15 min). Subsequent ticks at the
  configured interval.
- Each tick: list active connections (single `tenant_id` in v1, but the
  query is tenant-agnostic — `ListActiveConnections`). Iterate serially
  per connection — no fan-out in v1. Per-connection sync is itself the
  unit of progress; we don't parallelise across connections because (a)
  GitHub's 5k/hour budget is per-token and most v1 instances have one
  token, (b) serial is debuggable and the 90-day backfill window fits
  easily in 15 min.
- Per-connection sequence:
  1. `StartSyncRun(connection_id, started_at)` → returns the new row.
  2. Resolve the connection's `gh_tokens` row, decrypt `encrypted_pat`
     via `*secret.Box`.
  3. Build a fresh `*github.Client` per connection (each connection has
     its own token; sharing a client across tokens would mix limiter
     state).
  4. Run every registered `Runner` in order, accumulating
     `Outcome.Items` and tracking the minimum observed
     `RateLimitRemaining` (the worst-case is the meaningful number).
  5. On any runner error: short-circuit, record `error`, set `ok=0`,
     **do not** bump `last_sync_at`. Continue to the next connection.
  6. On all-clean: set `ok=1`, write `items` and `rate_limit_remaining`,
     and `UpdateConnectionLastSync` with the start time.
- Graceful shutdown: when ctx is cancelled mid-tick, the current
  connection's sync_run is closed with `ok=0`, `error="canceled"`, and
  the loop exits without starting more connections.

Runner abstraction (the contract subsequent tasks plug into):

```go
package ingest

type Outcome struct {
    Items              int64  // items written or upserted
    RateLimitRemaining *int64 // optional; reported into sync_runs.rate_limit_remaining
}

type Runner interface {
    // Name is used for structured logging and error wrapping. Must be
    // a stable short identifier ("prs", "reviews", "commits", "deploys").
    Name() string
    // Run executes one resource sync for one connection. Returning an
    // error short-circuits the rest of this connection's runners but
    // does not abort the tick.
    Run(ctx context.Context, conn sqlitedb.Connection, gh *github.Client) (Outcome, error)
}
```

Runners are wired into fx as a value group:

```go
fx.Provide(fx.Annotate(NewPRRunner, fx.ResultTags(`group:"ingest.runners"`)))
```

v1 of this task ships **zero runners** wired — the scheduler boots and
ticks but performs no work per connection. Tests register fake runners to
exercise the loop. Subsequent tasks 0027–0030 each add their runner.

Spec refs:

- `docs/superpowers/specs/2026-05-08-tempo-design.md` Architecture
  (lines 59–66), Ingest strategy (124–142), Sync state model (114–117).
- Plan row 152: deps `0019, 0011`, autonomy `full`, skill
  `systematic-debugging`.

## Acceptance criteria

- [ ] New sqlc query `ListActiveConnections` returns all connections with
      `status = 'active'`, generated into `sqlitedb`.
- [ ] `internal/ingest/runner.go` defines `Runner` interface + `Outcome`
      struct.
- [ ] `internal/ingest/scheduler.go` defines `Scheduler` with `Tick(ctx)`
      and `Loop(ctx)` methods.
- [ ] `Tick`: lists active connections, for each opens + closes a sync_run,
      decrypts PAT, builds per-token client, invokes every runner in order,
      records aggregated `items` + min `rate_limit_remaining`.
- [ ] On runner error: `sync_runs.ok = 0`, `error` is the runner-prefixed
      message, `last_sync_at` is NOT advanced.
- [ ] On success: `sync_runs.ok = 1`, `connections.last_sync_at` advances
      to the tick's start time.
- [ ] On ctx cancellation mid-tick: in-flight sync_run is closed with
      `ok=0, error="canceled"`; loop exits.
- [ ] `internal/ingest/run.go` exports `Run(p RunParams) error` for
      `fx.Invoke`. Hooks register goroutine start/stop with the
      lifecycle. OnStop waits for the loop to exit, bounded by ctx.
- [ ] `cmd/tempo/main.go` wires `fx.Invoke(ingest.Run)` after the api Run.
- [ ] Scheduler is hermetic-testable: injectable clock (`now func() time.Time`)
      and client builder (`func(token string) *github.Client`).
- [ ] Tests cover: happy-path tick (sync_run row + last_sync_at
      advance + runner ordering), runner error path, zero-runners
      no-op path, ctx-cancellation mid-tick, token-decrypt failure,
      missing gh_tokens row.
- [ ] `./verify.sh` exits 0.

## Files to touch

- `internal/storage/sqlite/queries/connections.sql` — add
  `ListActiveConnections :many`.
- `internal/storage/sqlite/sqlitedb/connections.sql.go` — regenerated by sqlc.
- `internal/storage/sqlite/sqlitedb/querier.go` — regenerated by sqlc.
- `internal/ingest/ingest.go` — keep, update doc comment.
- `internal/ingest/runner.go` — new.
- `internal/ingest/scheduler.go` — new.
- `internal/ingest/scheduler_test.go` — new (hermetic, in-memory sqlite).
- `internal/ingest/runner_test.go` — new (Noop + interface compliance).
- `internal/ingest/run.go` — new (fx wiring).
- `cmd/tempo/main.go` — add `fx.Invoke(ingest.Run)`.

## Steps

1. **Add `ListActiveConnections` sqlc query + regenerate.**
   - Edit `internal/storage/sqlite/queries/connections.sql`:

     ```sql
     -- name: ListActiveConnections :many
     SELECT * FROM connections WHERE status = 'active' ORDER BY id;
     ```
   - Run `make sqlc-generate` (or `sqlc generate`).
   - `go build ./...` to confirm the regenerated files compile.
   - Commit: `feat(storage): list-active-connections query`.

2. **Define `Runner` interface + `Outcome` + `NoopRunner`.**
   - Create `internal/ingest/runner.go` with the `Runner` interface,
     `Outcome` struct, and a `NoopRunner` (returns `Outcome{}, nil`).
   - Create `internal/ingest/runner_test.go` asserting `NoopRunner`
     implements `Runner` and returns zero outcome.
   - `go test ./internal/ingest/... -race -count=1`.
   - Commit: `feat(ingest): runner interface + noop`.

3. **Implement the `Scheduler` (single-tick logic).**
   - Create `internal/ingest/scheduler.go`:
     - Fields: `q *sqlitedb.Queries`, `box *secret.Box`, `log *zap.Logger`,
       `cfg *config.Config`, `runners []Runner`,
       `newClient func(token string, l *zap.Logger) *github.Client`,
       `now func() time.Time`.
     - Constructor `New(...)` with sensible defaults
       (`newClient = defaultClientBuilder`, `now = time.Now`).
     - `Tick(ctx)`: list active connections; for each, call
       `syncConnection(ctx, conn)`.
     - `syncConnection(ctx, conn)`: open sync_run, decrypt PAT, build
       client, iterate runners, finalize sync_run, bump
       `last_sync_at` on success.
   - Helper `finishRun(ctx, runID, ok, items, remaining, errMsg, finishedAt)`
     wraps the sqlc `FinishSyncRun` call (keeps the error path tidy).
   - Commit: `feat(ingest): scheduler tick core`.

4. **Add scheduler tests — happy path + error path.**
   - `internal/ingest/scheduler_test.go` with helpers:
     - `newIntegrationStore(t)` — copy from `internal/auth/session_test.go`.
     - `seedToken(t, q, box, plaintext)` → returns `gh_tokens.id`.
     - `seedConnection(t, q, tokenID, status, owner, name)` → returns
       `connections.id`.
   - Tests:
     - `TestTick_NoConnections` — empty DB → no sync_runs.
     - `TestTick_OneConnection_NoRunners` — sync_run opened+closed with
       `ok=1`, `items=0`, `last_sync_at` advances.
     - `TestTick_OneConnection_HappyPathRunner` — fake runner returns
       `Outcome{Items: 42, RateLimitRemaining: ptr(4999)}` → sync_run
       captures both; runner called exactly once with the right
       connection.
     - `TestTick_MultipleRunners_Order` — two fake runners; verify order
       of invocation and that `items` is the sum.
     - `TestTick_RunnerError_StopsAtFirstFailure` — second runner errors;
       sync_run.ok=0, error message has runner name prefix, third runner
       NOT called, `last_sync_at` NOT updated.
     - `TestTick_TokenMissing_ErrorPath` — connection's `token_id`
       doesn't exist → sync_run.ok=0, error string contains "token".
     - `TestTick_SkipsInactiveConnections` — inactive connection is
       ignored (no sync_run created).
   - `go test ./internal/ingest/... -race -count=1`.
   - Commit: `test(ingest): scheduler tick happy/error/inactive`.

5. **Add `Loop` + ctx-cancellation test.**
   - In `scheduler.go` add `Loop(ctx)`:
     - First call: `s.Tick(ctx)`.
     - Then `time.NewTicker(cfg.Poll.Interval)` loop. On each `<-ticker.C`,
       call `s.Tick(ctx)`. On `<-ctx.Done()` return.
   - In `scheduler_test.go` add:
     - `TestLoop_TicksImmediately_AndOnInterval` — short interval (50ms),
       counting fake runner; assert ≥2 calls within 200ms; cancel; loop
       exits cleanly.
     - `TestLoop_CtxCancelMidTick` — runner blocks until ctx cancelled,
       then returns ctx.Err. Verify sync_run closed with
       `error="context canceled"`.
   - Commit: `feat(ingest): scheduler loop + cancellation`.

6. **Add fx wiring (`run.go`).**
   - Create `internal/ingest/run.go`:

     ```go
     package ingest

     import (
         "context"

         "github.com/karnstack/tempo/internal/config"
         "github.com/karnstack/tempo/internal/secret"
         "github.com/karnstack/tempo/internal/storage/sqlite/sqlitedb"
         "go.uber.org/fx"
         "go.uber.org/zap"
     )

     type RunParams struct {
         fx.In
         Lifecycle fx.Lifecycle
         Logger    *zap.Logger
         Config    *config.Config
         Queries   *sqlitedb.Queries
         Box       *secret.Box
         Runners   []Runner `group:"ingest.runners"`
     }

     func Run(p RunParams) error {
         s := New(p.Logger, p.Config, p.Queries, p.Box, p.Runners)
         ctx, cancel := context.WithCancel(context.Background())
         done := make(chan struct{})
         p.Lifecycle.Append(fx.Hook{
             OnStart: func(_ context.Context) error {
                 go func() {
                     defer close(done)
                     s.Loop(ctx)
                 }()
                 p.Logger.Info("ingest scheduler started",
                     zap.Duration("interval", p.Config.Poll.Interval),
                     zap.Int("runners", len(p.Runners)))
                 return nil
             },
             OnStop: func(stopCtx context.Context) error {
                 cancel()
                 select {
                 case <-done:
                     return nil
                 case <-stopCtx.Done():
                     return stopCtx.Err()
                 }
             },
         })
         return nil
     }
     ```
   - Commit: `feat(ingest): fx lifecycle wiring`.

7. **Wire into `cmd/tempo/main.go`.**
   - Add `fx.Invoke(ingest.Run)` after the existing `fx.Invoke(api.Run)`.
   - `go build ./...` → confirm binary still builds.
   - Manual smoke: `go run ./cmd/tempo` should log "ingest scheduler
     started" and not crash. Stop with Ctrl-C; should log shutdown
     cleanly. (Document this as a manual smoke; verify.sh stays
     deterministic.)
   - Commit: `feat(ingest): wire scheduler into main`.

8. **Run `./verify.sh`.** Expect `VERIFY OK`.

## Notes

- **Why no rate-limit accounting in the scheduler itself.** The client's
  `Limiter` already pauses on near-empty buckets (`internal/github/limiter.go`).
  The scheduler's only rate-limit job is reporting — recording the
  worst-observed `Remaining` for each connection's sync_run for the
  `/sync/status` endpoint (0044). The Runner returns it via `Outcome`;
  the scheduler doesn't read `Client` state.
- **Why per-connection client.** Each connection's PAT is a separate
  bucket. Sharing a `*github.Client` across tokens would mix limiter
  state across buckets — a polite token would get throttled because a
  noisy token tripped the floor.
- **Why no fan-out.** Single goroutine per scheduler keeps `sync_runs`
  ordering deterministic and per-connection rate-limit budgets honest.
  v2 may add a worker pool keyed on `token_id`.
- **Connection.last_sync_at semantics.** Bumped only on full success.
  Partial-failure connections retain the old timestamp so the next tick
  knows to retry from the same `since=`. Cursor advancement (0027+) is
  separate from `last_sync_at`.
- **Test isolation.** Tests open a real in-memory-style sqlite (file
  under `t.TempDir()`), run `migrations.Apply`, then talk to the real
  `*sqlitedb.Queries`. No mocks for the store. Runners are tiny fakes
  defined in `scheduler_test.go`.
- **Avoid DB-level constraints (per repo memory).** No CHECK constraint
  on `connections.status` — the scheduler filters in SQL by value, Go
  handles validation.
