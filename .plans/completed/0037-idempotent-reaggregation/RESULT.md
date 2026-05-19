# 0037 — Idempotent re-aggregation hook

## Files changed

- `internal/rollup/scheduler.go` — added `Scheduler.Rebuild(ctx,
  from, to)` and an unexported `snapToLocalMidnight` helper. Rebuild
  walks `[from, to]` inclusive in chronological order, snaps inputs
  to local-midnight in the scheduler's tz, calls `RunDate` per date,
  and surfaces ctx cancellation by returning early without writing a
  synthetic failure row.
- `internal/rollup/scheduler_test.go` — added 8 tests plus a
  `cancelOnceAgg` helper for deterministic ctx-cancel testing.

## Verify output

```
== sqlc diff ==
== go vet ==
== go build ==
== go test (rollup) ==
ok  	github.com/karnstack/tempo/internal/rollup	0.374s
ok  	github.com/karnstack/tempo/internal/rollup/cycletime	(cached)
ok  	github.com/karnstack/tempo/internal/rollup/engineerstats	(cached)
ok  	github.com/karnstack/tempo/internal/rollup/repostats	(cached)
ok  	github.com/karnstack/tempo/internal/rollup/reviewstats	(cached)
```

## Notes / followups

- **No new sqlc queries, no migration.** Idempotency was already a
  per-aggregator contract in 0033–0036 — Rebuild just composes
  RunDate over a range. The existing `rollup_runs` UPSERT semantics
  give us "one row per date, latest outcome wins" for free.
- **No new `rollup_runs.kind`.** The original rollup.go comment
  hinted at per-aggregator `kind` rows for manual reruns, but
  introducing them now would silently change the meaning of the
  existing `kind="all"` rows used by CatchUp / Tick. Deferred.
- **Race-free ctx cancel test.** The naive "spawn goroutine, sleep,
  cancel" pattern is racy when aggregators don't block; the test
  uses a `cancelOnceAgg` that cancels its own ctx inside Aggregate
  so the next loop iteration's `ctx.Err()` check fires
  deterministically.
- **Async-safe public surface.** Rebuild only reads the same fields
  RunDate already touches (`s.cfg`, `s.q`, `s.aggregators`,
  `s.log`), so callers can invoke it concurrently with the
  scheduler's Loop. SQLite WAL handles writer serialisation; the
  worst case is a brief lock-wait on rollup_runs.
- **No HTTP / CLI surface yet.** Future tasks (admin settings page,
  ops CLI) can layer on top — `Rebuild` is the right shape for that
  to import.
