---
id: 0037
slug: idempotent-reaggregation
title: Idempotent re-aggregation hook
status: done
depends_on: [0033, 0034, 0035, 0036]
owner: ""
est_minutes: 30
tags: [rollup]
autonomy: full
skills: []
---

## Goal

Expose an explicit "rebuild a date range" entry point on the rollup
`Scheduler`. The four sibling aggregators (0033–0036) are already
idempotent — each `Aggregate(ctx, date)` either UPSERTs by
`(date, repo_id[, gh_user_id])` or DELETE+INSERTs a per-repo slice
inside a tx. The piece missing is a programmatic hook that walks a
date range and calls `RunDate(ctx, d)` for every local date in the
range, refreshing both `rollup_runs` and the per-aggregator output.

Master plan row 168 names this "retro-rebuild a date range". v1 keeps
the hook *internal* — no HTTP endpoint, no CLI flag yet. The public
API is one method on `*Scheduler` so existing consumers (tests,
future admin tooling, 0044's `/sync/status` if it grows a manual
trigger) can wire it in deliberately.

### How it composes with existing scheduling

- The daily fire path (`Loop` → `Tick`) and the boot catch-up path
  (`CatchUp`) already lean on `RunDate` and the
  `kind="all"` `rollup_runs` rows.
- `Rebuild(ctx, from, to)` reuses the same `RunDate` machinery, so:
  - rollup_runs gets a fresh row per rebuilt date (UPSERT semantics),
  - per-aggregator failures get logged + recorded but never
    short-circuit sibling dates,
  - re-running a date that's already successful overwrites it with a
    new start/finish timestamp pair.
- The aggregators themselves are unchanged — 0033/0036 use
  DELETE+INSERT in a tx, 0034/0035 use disjoint-column UPSERTs.
  Calling them again for date `D` correctly replaces the
  `(date=D, ...)` slice in every output table.

### Scope notes / non-goals

- **No HTTP / CLI surface yet.** Future tasks (admin settings page,
  ops CLI) can layer on top. v1 callers are tests and any internal
  consumer that imports `internal/rollup`.
- **No new `rollup_runs.kind`** for manual reruns. The scheduler's
  rollup.go comment hinted at it ("Per-aggregator kinds may be
  added in 0037 when manual reruns land"), but adding it would
  silently change the meaning of `kind="all"` rows. Defer.
- **No retention pruning** ahead of the rebuild — if the caller
  wants to truncate a date first, they call `Delete*` queries
  themselves. The aggregators already handle the
  "source-data-shrank" case by writing zeros / NULLs.
- **Snap to tz-local midnight.** Inputs are interpreted as local
  *dates* in the scheduler's tz, not absolute timestamps. Passing
  noon UTC for an Asia/Tokyo scheduler still rebuilds the JST date
  that contains that instant.

## Acceptance criteria

- [ ] `internal/rollup/scheduler.go` adds an exported method
      `func (s *Scheduler) Rebuild(ctx context.Context, from, to time.Time) error`.
      The method:
      - Returns an error if either input is the zero value.
      - Snaps `from` / `to` to local-midnight in the scheduler's tz.
      - Returns an error if `to` (snapped) is before `from` (snapped).
      - Iterates inclusive `[from, to]` in chronological order.
      - Honours ctx cancellation: stops at the start of the next
        date and returns `ctx.Err()` rather than recording a
        synthetic failure.
      - Per iteration, calls `s.RunDate(ctx, d)`. Returns nil if all
        dates ran (regardless of per-aggregator outcomes — those are
        already recorded in rollup_runs).
- [ ] A small helper `snapToLocalMidnight(t time.Time) time.Time`
      (unexported) is added next to `localMidnight` for the snap
      operation. Keeps the public surface clean.
- [ ] `internal/rollup/scheduler_test.go` covers (using `fakeAgg`
      from the existing test file):
      - `TestRebuild_SingleDay` — one date, one RunDate call, one
        rollup_runs row.
      - `TestRebuild_RangeChronological` — three-day range, exactly
        three RunDate calls in ascending date order; each aggregator
        sees each date once; rollup_runs has all three rows.
      - `TestRebuild_FromAfterToErrors` — returns a non-nil error,
        no aggregator calls, no rollup_runs writes.
      - `TestRebuild_ZeroTimeErrors` — both `from.IsZero()` and
        `to.IsZero()` independently produce errors.
      - `TestRebuild_SnapsToLocalMidnight` — pass non-midnight
        `time.Time` inputs in a non-UTC tz; the resulting RunDate
        calls receive local-midnight values matching the bucket
        date.
      - `TestRebuild_IdempotentRerun` — run Rebuild twice for the
        same range; rollup_runs still has the same number of rows
        (UPSERT collapses re-runs), and each aggregator's call count
        doubles (because RunDate calls it again).
      - `TestRebuild_AggregatorFailureRecordedButContinues` — a
        fakeAgg that returns an error doesn't stop sibling dates;
        the rollup_runs row for the failing date has `ok=0` and a
        non-empty error message, but the next date's row is `ok=1`.
      - `TestRebuild_CtxCancelStopsRange` — start a 5-day rebuild,
        cancel the ctx after the first date's RunDate completes;
        subsequent dates have no rollup_runs row.
- [ ] `internal/rollup/scheduler.go` exports a brief doc-comment on
      `Rebuild` describing the snap behavior and the fact that this
      is the intended entry point for manual retro-rebuilds.
- [ ] `go vet ./...`, `go build ./...`, `go test ./internal/rollup/...`
      all pass.
- [ ] `verify.sh` runs: `sqlc diff` (should be clean — no SQL
      changes), `go vet ./...`, `go build ./...`,
      `go test ./internal/rollup/...` and exits 0.

## Files to touch

- `internal/rollup/scheduler.go` — add `Rebuild` + `snapToLocalMidnight`.
- `internal/rollup/scheduler_test.go` — add eight test funcs.
- `.plans/upnext/0037-idempotent-reaggregation/verify.sh` (rewrite
  from stub).

## Steps

### 1. Implement Rebuild

Add the method and helper to `internal/rollup/scheduler.go`. Reuse
`s.tz()`, `s.now()`, and `s.RunDate(ctx, d)`. Keep the docstring
honest about what "idempotent" means: rollup_runs UPSERTs collapse
duplicate runs, and aggregators replace their (date, ...) slices.

```go
// Rebuild iterates inclusive [from, to] local-date by local-date in
// the scheduler's tz, calling RunDate for each. Use this to
// retro-rebuild after a data fix or to backfill a fresh instance.
// Per-aggregator outcomes are still recorded in rollup_runs; this
// method returns nil unless one of (a) the inputs are invalid or
// (b) ctx is cancelled.
func (s *Scheduler) Rebuild(ctx context.Context, from, to time.Time) error { ... }
```

Commit: `feat(rollup): scheduler Rebuild method (#0037)`.

### 2. Tests

Add the eight test funcs to `scheduler_test.go`. The existing
`fakeAgg`, `newScheduler`, `newStore`, `sharedOrder` types are
sufficient.

Commit: `test(rollup): Rebuild coverage (#0037)`.

### 3. verify.sh

Rewrite the stub to run sqlc diff + go vet + go build + go test on
`internal/rollup/...` (matches the 0033/0035 pattern).

Commit (combined with task completion).

### 4. Verify

`./verify.sh`. Should output the four section headers cleanly.

## Notes

- The rollup_runs row for each date is identified by
  `(date, kind="all")`. Re-running with `Rebuild` UPSERTs in place —
  the start/finish timestamps reflect the most recent run, and the
  `ok` flag reflects the most recent outcome. Historical run
  outcomes are intentionally not preserved in v1.
- If we later need a "rebuild only one aggregator's output for a
  range" mode, this hook is the right place to grow a `WithAggregators`
  filter — but defer until there's a use case.
- The catch-up path skips dates with `ok=0` (failed) rollup_runs
  rows. After a failure-then-rebuild, the rebuild's success flips the
  row to `ok=1`, which means the next CatchUp boot won't try to
  rerun it. That's the intended interaction.
