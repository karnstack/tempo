---
id: 0032
slug: rollup-scheduler
title: Rollup scheduler (daily 02:00 instance-local)
status: done
depends_on: [0010, 0031]
owner: ""
est_minutes: 90
tags: [rollup]
autonomy: full
skills: []
---

## Goal

Stand up the daily rollup worker. Today, `internal/rollup/rollup.go` is
just a doc comment. By the end of this task we have:

1. A `Scheduler` that ticks once a minute, fires at the configured local
   hour (default 02:00, controlled by `TEMPO_ROLLUP_HOUR` + `TEMPO_TZ`),
   and runs one rollup per local date.
2. An `Aggregator` interface (`Name() string`, `Aggregate(ctx, date) error`)
   that 0033–0036 will implement and plug in via the
   `"rollup.aggregators"` fx value group — same pattern as
   `"ingest.runners"`.
3. A `rollup_runs(date, kind, started_at, finished_at, ok, error)` table
   so the scheduler can detect already-done days and recover after the
   process was offline at fire time.
4. Catch-up on boot: scan the last 7 days, run any without a successful
   rollup. Bounded so we don't accidentally retro-rebuild the entire
   history on a long downtime — that's 0037's job (manual range
   re-aggregation).
5. fx wiring + `rollup.Run` invoked from `cmd/tempo/main.go` so the
   worker starts with the rest of the app.

This is purely the **framework**. The aggregator slice will be empty
for now: 0033 (engineer stats), 0034 (repo stats), 0035 (cycle time),
0036 (review latency) will populate it. Tests use a fake aggregator to
exercise the wiring.

Spec refs:

- `docs/superpowers/specs/2026-05-08-tempo-design.md` lines 64–66
  (rollup worker overview), lines 141–142 (daily 02:00, `TEMPO_TZ`),
  lines 107–112 (`daily_*` schema — already migrated).
- Master plan row 163: deps `0010, 0031`, autonomy `full`.

### Scope notes / non-goals

- **No aggregator implementations.** Just the interface + plumbing.
  0033–0036 ship the actual SQL.
- **No manual rerun API.** That's 0037 (idempotent re-aggregation
  hook) — exposes a function the future admin UI can call.
- **No retention pruning on `rollup_runs`.** One row per date — at
  365 rows/year per instance the table never matters. If it ever
  does, 0037 can grow a prune step.
- **No retries.** A failed rollup writes `ok=0` + error to
  `rollup_runs` and the next day's tick proceeds with that day's
  bucket. The day before remains marked failed; CatchUp ignores
  failed-but-recorded rows (rerun behavior comes in 0037). The
  scheduler will, however, redrive a missing row on the next boot
  — so the "process died mid-rollup before writing the row" case
  self-heals.
- **No second-level cron.** A check-every-minute ticker + a fire-hour
  predicate is simpler than computing next-fire and sleeping. Adds at
  most ~1m latency, which is irrelevant for daily aggregation.
  Survives sleep/wake and clock skew.
- **Migration edit-in-place** (per CLAUDE memory): the `rollup_runs`
  table goes into `migrations/0003_daily_rollups.sql` alongside the
  `daily_*` tables it tracks. No new migration file.

## Acceptance criteria

- [ ] `migrations/0003_daily_rollups.sql` adds, in-place:

      ```sql
      CREATE TABLE rollup_runs (
        date TEXT NOT NULL,
        kind TEXT NOT NULL,
        started_at TIMESTAMP NOT NULL,
        finished_at TIMESTAMP,
        ok INTEGER NOT NULL DEFAULT 0,
        error TEXT NOT NULL DEFAULT '',
        PRIMARY KEY (date, kind)
      );
      CREATE INDEX rollup_runs_date_idx ON rollup_runs(date);
      ```

      Plus the matching `DROP TABLE IF EXISTS rollup_runs;` in the
      down section (before the daily_* drops so a teardown is clean).

- [ ] `internal/storage/sqlite/queries/rollup_runs.sql` declares:
      - `UpsertRollupRunStart(date, kind, started_at) :one` — inserts
        or resets a row (`ok=0`, `error=''`, `finished_at=NULL`,
        new `started_at`) and returns it. Pattern:
        `INSERT ... ON CONFLICT(date, kind) DO UPDATE SET started_at = ...,
        finished_at = NULL, ok = 0, error = '' RETURNING *`.
      - `FinishRollupRun(date, kind, finished_at, ok, error) :exec` —
        updates an existing row.
      - `GetRollupRun(date, kind) :one` — returns `sql.ErrNoRows` if
        absent. Used by tests + catch-up.
      - `ListSuccessfulRollupDates(kind, since) :many` — returns
        date strings where `kind = ?1 AND ok = 1 AND date >= ?2`.
        Catch-up uses this to know which days NOT to re-run.

- [ ] `sqlc generate` produces matching Go bindings.
      `sqlc diff` exits clean.

- [ ] `internal/rollup/aggregator.go` declares:

      ```go
      type Aggregator interface {
          Name() string
          Aggregate(ctx context.Context, date time.Time) error
      }
      ```

      `date` is local-midnight of the day to roll up
      (`time.Date(y,m,d,0,0,0,0,tz)`). Implementations format it however
      they need.

- [ ] `internal/rollup/scheduler.go` exports `Scheduler` with:
      - `New(l, cfg, q, aggregators, opts...) *Scheduler`.
      - `Option`: `WithClock(now func() time.Time)`,
        `WithCheckInterval(d time.Duration)` (default 1m; tests use
        smaller).
      - `Loop(ctx context.Context)` — calls `CatchUp` then ticks on
        the check interval, running `Tick` each time.
      - `CatchUp(ctx)` — for each of the last 7 local dates,
        if no successful rollup_runs row exists, runs the rollup.
        Bounded window; doesn't crawl indefinitely.
      - `Tick(ctx)` — if `now` is past today's fire time AND
        yesterday's local date has no successful rollup_runs row,
        runs the rollup for yesterday.
      - `RunDate(ctx, date time.Time)` — idempotent. Upserts a
        rollup_runs row (status `started_at = now, ok = 0`), invokes
        every aggregator (continuing past individual failures),
        writes finished_at + final ok/error. The "kind" column is
        always `"all"`.
      - Pure helpers: `nextFire(now)`, `pastFireTime(now)`,
        `bucketDate(t) string` (YYYY-MM-DD in scheduler tz),
        `localMidnight(date string) (time.Time, error)`.

- [ ] `internal/rollup/run.go` exports `Run(p RunParams) error`
      mirroring `ingest.Run`. `RunParams` carries fx.Lifecycle,
      logger, config, queries, and the `[]Aggregator` group via
      `group:"rollup.aggregators"`. The lifecycle hook spins
      `s.Loop(ctx)` in a goroutine and stops it cleanly on shutdown.

- [ ] `cmd/tempo/main.go` adds `fx.Invoke(rollup.Run)` next to
      `fx.Invoke(ingest.Run)`. No Module needed at the call site
      until 0033+ start providing aggregators.

- [ ] Boot log: `rollup scheduler started` includes
      `zap.Int("aggregators", n)` and the configured hour + tz.

- [ ] Hermetic tests in `internal/rollup/`:
      - `TestNextFire_*` — pure function, no DB. Cases: now before
        today's fire → today; now after → tomorrow; tz-shifted now
        does not skip a day.
      - `TestBucketDate_HonoursTimezone` — `t = 2026-05-16T01:30:00Z`
        with tz `Asia/Kolkata` → `"2026-05-16"` (already next day in
        IST); tz `UTC` → `"2026-05-16"`; tz `America/Los_Angeles` →
        `"2026-05-15"`.
      - `TestRunDate_NoAggregators_WritesOkRollupRun` — Scheduler
        with empty aggregator slice. `RunDate(ctx, midnight)` writes
        a row with `ok=1`, `error=""`, `finished_at` non-nil.
      - `TestRunDate_AggregatorError_RecordsFailure` — two fake
        aggregators; second returns "boom". Row has `ok=0`,
        `error` contains the aggregator name + message. First
        aggregator still observed (failures don't short-circuit).
      - `TestRunDate_AggregatorOrderPreserved` — given two fakes in
        slice order [a, b], both are called and we record the call
        order.
      - `TestRunDate_Idempotent` — call twice for the same date.
        Second call upserts a fresh row (`started_at` advances,
        `finished_at` advances), aggregators called twice, only one
        row remains in DB.
      - `TestTick_BeforeFireTime_NoOp` — `now = today fire-hour - 1h`.
        Tick. No rollup_runs row created.
      - `TestTick_AfterFireTime_RunsYesterday` — `now = today
        fire-hour + 10m`. Tick. One rollup_runs row for yesterday's
        date string, `ok=1`. Subsequent Tick at the same `now` does
        nothing (idempotency via the existence check).
      - `TestCatchUp_RunsLast7MissingDays` — set `now` to 4 days
        past the fire time, pre-seed a successful rollup_runs row
        for "today-2 days". CatchUp. Expect rollup_runs rows for
        the other six days (today-1, today-3, today-4, today-5,
        today-6, today-7) but NOT today-2 (already done) and NOT
        today (the day in progress). Aggregator counter == 6.
      - `TestCatchUp_IgnoresFailedRows` — pre-seed today-1 with
        `ok=0` (failed). CatchUp does not re-run it. Reason:
        avoid hot-loop on a deterministically-failing aggregator;
        rerun is 0037's job.
      - `TestLoop_StartsCatchUpThenTicks` — spin Loop with
        `WithCheckInterval(5*time.Millisecond)` and a clock past
        the fire hour. After ~50ms, observe at least one
        rollup_runs row for yesterday + at least one aggregator
        call. Cancel ctx, assert clean exit.

- [ ] `cmd/tempo/main.go` builds and boots, log line confirms
      `aggregators=0`.

- [ ] `./verify.sh` exits 0:
      1. `sqlc diff`
      2. `go vet ./internal/rollup/... ./internal/storage/... ./internal/config/...`
      3. `go build ./...`
      4. Focused tests: `go test ./internal/rollup/... ./internal/storage/... -race -count=1`
      5. Full suite: `go test ./... -race -count=1`

## Files to touch

- `migrations/0003_daily_rollups.sql` — append `rollup_runs` table +
  index; add matching drop in the down section.
- `internal/storage/sqlite/queries/rollup_runs.sql` — new file with
  four queries.
- `internal/storage/sqlite/sqlitedb/rollup_runs.sql.go` — generated.
- `internal/storage/sqlite/sqlitedb/querier.go` — regenerated.
- `internal/storage/sqlite/sqlitedb/models.go` — regenerated (new
  `RollupRun` model).
- `internal/rollup/aggregator.go` — new: `Aggregator` interface +
  a small `NoopAggregator` for log clarity.
- `internal/rollup/scheduler.go` — new: `Scheduler`, `Option`,
  `New`, `Loop`, `CatchUp`, `Tick`, `RunDate`, pure helpers.
- `internal/rollup/run.go` — new: `RunParams` + `Run` (fx).
- `internal/rollup/rollup.go` — keep the package doc, add a one-line
  pointer to scheduler.go.
- `internal/rollup/scheduler_test.go` — new: every test above.
- `cmd/tempo/main.go` — add `fx.Invoke(rollup.Run)`.
- `.plans/upnext/0032-rollup-scheduler/verify.sh` — verify script.

No config changes — `Rollup.Hour` and `Rollup.Timezone` are already in
`internal/config/config.go`.

## Steps

1. **Add `rollup_runs` table.** Edit `migrations/0003_daily_rollups.sql`
   in place. Append the `CREATE TABLE rollup_runs` + index after the
   `daily_review_load` index, before `-- +goose Down`. In the down
   section, add `DROP TABLE IF EXISTS rollup_runs;` as the first drop.

   Commit: `feat(migrations): rollup_runs tracking table (#0032)`.

2. **Add sqlc queries.** Create
   `internal/storage/sqlite/queries/rollup_runs.sql` with the four
   queries from the acceptance criteria. Run `sqlc generate` and
   `sqlc diff`. Verify the new `RollupRun` model + four query
   functions appear in `sqlitedb/`.

   Commit: `feat(storage): rollup_runs queries (#0032)`.

3. **Aggregator interface.** Create `internal/rollup/aggregator.go`
   with the `Aggregator` interface and `NoopAggregator` sentinel.

   Commit: `feat(rollup): Aggregator interface (#0032)`.

4. **Scheduler skeleton + pure helpers.** Create
   `internal/rollup/scheduler.go` with `Scheduler`, `Option` (with
   `WithClock`, `WithCheckInterval`), `New`, and pure helpers:
   `tz`, `nextFire`, `pastFireTime`, `bucketDate`, `localMidnight`.
   Add `Loop`, `Tick`, `CatchUp`, `RunDate` as compiling stubs.

   Default `now = time.Now`, `checkInterval = time.Minute`.

   Add pure-function tests for `nextFire` (3 cases) and `bucketDate`
   (3 timezone cases). No DB needed for these.

   Commit: `feat(rollup): scheduler skeleton + pure helpers (#0032)`.

5. **`RunDate`.** Implement the full RunDate flow:
   - `UpsertRollupRunStart(date="YYYY-MM-DD", kind="all", started_at=now)`.
   - Iterate aggregators; track first error but continue past
     individual failures (log Warn).
   - `FinishRollupRun(..., ok, errMsg)` using a `closingCtx` helper
     copied from the ingest pattern.

   Add tests: NoAggregators, AggregatorError, AggregatorOrderPreserved,
   Idempotent.

   Commit: `feat(rollup): RunDate + per-aggregator error tracking (#0032)`.

6. **`Tick` + `CatchUp`.** Implement both with the helpers
   `hasSuccessfulRun`, `hasFailedRun`, `successfulDateSet`. Tick is a
   single-day gate; CatchUp walks last 7 local days.

   Tests: Tick_BeforeFireTime_NoOp, Tick_AfterFireTime_RunsYesterday,
   CatchUp_RunsLast7MissingDays, CatchUp_IgnoresFailedRows.

   Commit: `feat(rollup): Tick + CatchUp with 7-day window (#0032)`.

7. **`Loop` + fx wiring.** Implement Loop (CatchUp once + ticker).
   Create `internal/rollup/run.go` mirroring `internal/ingest/run.go`.
   Add `fx.Invoke(rollup.Run)` to `cmd/tempo/main.go`.

   Test `TestLoop_StartsCatchUpThenTicks` with
   `WithCheckInterval(5*time.Millisecond)`.

   Commit: `feat(rollup): Loop + fx wiring (#0032)`.

8. **Verify.** Write `.plans/upnext/0032-rollup-scheduler/verify.sh`.
   Run it. Expect `VERIFY OK`.

## Notes

- **Why daily-cron-style tick.** We don't need millisecond precision
  for a daily job. Ticking every minute is simpler than
  computing-next-fire / re-sleeping after wake-from-sleep / handling
  DST transitions. The fire-hour predicate makes the schedule
  declarative: "if it's past `Rollup.Hour` and yesterday isn't done,
  do it now."
- **Why kind="all" for now.** Per-aggregator rollup_runs rows would
  let an operator rerun just one slice (engineer stats, say). Useful
  but premature. 0037 can grow this. Today: one row per date keeps
  catch-up trivial.
- **Why 7-day catch-up window.** Long enough to survive a one-week
  outage (the practical limit for a self-hosted app) and short
  enough that a fresh install doesn't churn through historical
  empty days. Beyond 7 days, the operator uses 0037 to schedule a
  bulk re-aggregation explicitly.
- **Why CatchUp ignores failed rows.** A deterministically-failing
  aggregator (bad data, bug) shouldn't hot-loop the scheduler on
  every restart. The failed row stays as a beacon; rerun is an
  explicit operator action via 0037.
- **Why local midnight, not UTC.** Engineering dashboards are
  cognitively local. A repo's "Tuesday" should match what humans see
  in their working hours. `TEMPO_TZ` lets the operator pin a
  specific zone (e.g. team HQ tz) for stability across globally
  distributed clusters; default is system local.
- **Why no `fx.Module` for rollup itself.** Unlike ingest runners,
  the scheduler has no providers of its own (no `Aggregator`-shaped
  group entries). The aggregator packages 0033–0036 will declare
  their own `fx.Module`s that export into `"rollup.aggregators"`,
  exactly like the ingest packages.
- **Why ctx propagated through `RunDate`.** A long aggregator that
  outlives shutdown should observe ctx cancellation and bail
  cleanly. The `FinishRollupRun` write uses `closingCtx` to still
  land an `ok=0, error="ctx canceled"` row, matching the ingest
  pattern.
- **Sqlite TEXT date format.** `daily_*` tables already use `TEXT
  NOT NULL` for `date`. `rollup_runs.date` matches. We always
  format with `time.Format("2006-01-02")` in the scheduler's tz so
  the join key is stable.
