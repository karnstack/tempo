---
id: 0031
slug: sync-runs-tracking
title: Sync runs + error tracking + status hook
status: done
depends_on: [0027, 0028, 0029, 0030]
owner: ""
est_minutes: 60
tags: [ingest]
autonomy: full
skills: []
---

## Goal

Finish the `sync_runs` story so that the future `/api/v1/sync/status` and
`/api/v1/system/health` endpoints (task 0044) can be implemented as a thin
HTTP shell over a typed Go function. The actual table, the
`StartSyncRun` / `FinishSyncRun` machinery, and per-tick error capture
were all wired up in 0026–0030 — what's missing is:

1. **Lookup queries** that distinguish "latest attempt" from "latest
   success" and "latest failure", plus a global aggregate for the health
   endpoint.
2. **A `StatusFor` helper** in `internal/ingest` that bundles those
   queries into a single `Status` snapshot. This is the "status hook"
   from the plan — `/api/v1/sync/status` (0044) will iterate active
   connections and call this per connection.
3. **Retention pruning** so `sync_runs` doesn't grow without bound
   (15-min ticks × N connections × forever = unbounded growth). The
   scheduler invokes `PruneSyncRunsByConnection(conn.ID, keep_n)` after
   each tick.
4. **A retention knob** in config (`TEMPO_SYNC_RUN_RETENTION`, default
   200 — ~2 days of history at the 15-min default cadence).

Spec refs:

- `docs/superpowers/specs/2026-05-08-tempo-design.md` line 116:
  `sync_runs(id, connection_id, started_at, finished_at, ok, items,
  rate_limit_remaining, error)`.
- `docs/superpowers/specs/2026-05-08-tempo-design.md` lines 227–228:
  `GET /sync/status` ("live ingest health"), `GET /system/health`.
- Master plan row 157: deps `0027–0030`, autonomy `full`.
- Master plan row 180: 0044 depends on 0031.

### Scope notes / non-goals

- **No HTTP wiring.** That's 0044's job. This task delivers the storage
  + Go helper layer only.
- **No new migration.** The existing `sync_runs` table from
  `migrations/0004_sync_state.sql` already covers everything. The
  current `sync_runs_connection_started_idx` (on
  `connection_id, started_at`) covers all four new queries — partial
  `WHERE ok=1`/`ok=0` indexes are over-engineering at this scale.
- **No status-cache layer.** `StatusFor` is called on-demand per HTTP
  request — three indexed `SELECT ... LIMIT 1`s per connection is
  cheap. Caching can be added if profiling demands it.
- **No retention by age.** We retain by count (keep last N), not by
  time window. Reason: count is easier to reason about for "show me the
  last 50 runs" UIs and survives long pauses (e.g. a paused connection
  shouldn't lose its history just because no new runs landed for 30
  days).
- **Failure to prune is non-fatal.** Logged at Warn, never blocks the
  tick or affects the run's recorded outcome. The scheduler's
  primary job is ingest, not housekeeping.

## Acceptance criteria

- [ ] `internal/storage/sqlite/queries/sync_runs.sql` declares four new
      sqlc queries:
      - `GetLastSuccessfulSyncRun(connection_id) :one` — most recent
        row with `ok = 1`. Returns `sql.ErrNoRows` if none.
      - `GetLastFailedSyncRun(connection_id) :one` — most recent row
        with `ok = 0 AND error != ''`. Returns `sql.ErrNoRows` if none.
      - `PruneSyncRunsByConnection(connection_id, keep_n) :exec` —
        deletes rows for the connection older than the most recent
        `keep_n` (i.e. keeps the `keep_n` newest by `started_at DESC`).
      - `CountFailedSyncRunsSince(started_at) :one` — global count of
        rows where `finished_at IS NOT NULL AND ok = 0 AND
        started_at >= ?`.
- [ ] `sqlc generate` produces matching Go bindings in
      `internal/storage/sqlite/sqlitedb/sync_runs.sql.go`. `sqlc diff`
      exits clean.
- [ ] `internal/ingest/status.go` exports:
      ```go
      type Status struct {
          ConnectionID int64
          Latest       *sqlitedb.SyncRun
          LastSuccess  *sqlitedb.SyncRun
          LastFailure  *sqlitedb.SyncRun
      }
      func StatusFor(ctx context.Context, q *sqlitedb.Queries,
          connectionID int64) (Status, error)
      ```
      Each pointer field is nil when the corresponding row doesn't exist
      (`sql.ErrNoRows`). Any other DB error short-circuits and is
      returned. The connection ID is stamped onto the returned Status
      even when all three rows are nil (so callers can map by ID).
- [ ] `internal/ingest/status_test.go` covers four scenarios on a
      seeded in-memory DB: no runs / only-successes / only-failures /
      mixed (newest-of-each-kind picked correctly).
- [ ] `internal/config/config.go` adds `SyncRunRetention int` to the
      `Poll` struct. Loader reads `TEMPO_SYNC_RUN_RETENTION` via
      `strconv.Atoi`; default 200; validation: must be `>= 1` (else
      aggregated error: `"config: TEMPO_SYNC_RUN_RETENTION must be >= 1
      (got %d)"`).
- [ ] `internal/config/config_test.go` asserts default `Poll.SyncRunRetention
      == 200` and an env override (e.g. `50`) is picked up. Also asserts
      that `TEMPO_SYNC_RUN_RETENTION=0` produces a validation error.
- [ ] `internal/ingest/scheduler.go` calls
      `PruneSyncRunsByConnection(conn.ID, cfg.Poll.SyncRunRetention)`
      after every `syncConnection` (regardless of success/failure — the
      pruning predicate is "keep N newest", so failed runs still get
      counted). A pruning error logs at Warn (`"ingest: prune
      sync_runs"`) and does NOT affect the run outcome or
      `last_sync_at`.
- [ ] `internal/ingest/scheduler_test.go` gains a test:
      `TestTick_PrunesSyncRunsToRetention`. Seed 5 sync_runs manually
      via `StartSyncRun` + `FinishSyncRun`. Build a scheduler with a
      `Poll.SyncRunRetention = 3` config override. Tick once. Assert
      `ListSyncRunsByConnection(... LIMIT 10)` returns exactly 3 rows,
      and that the **most recent** row is the new tick's run (i.e. the
      pruning kept newest, not oldest).
- [ ] Existing scheduler tests still pass — pruning is a no-op when
      retention exceeds row count.
- [ ] `./verify.sh` exits 0:
      1. `sqlc diff`
      2. `go vet ./internal/ingest/... ./internal/storage/... ./internal/config/...`
      3. `go build ./...`
      4. Focused tests: `go test ./internal/ingest/... ./internal/storage/... ./internal/config/... -race -count=1`
      5. Full suite: `go test ./... -race -count=1`

## Files to touch

- `internal/storage/sqlite/queries/sync_runs.sql` — append four queries.
- `internal/storage/sqlite/sqlitedb/sync_runs.sql.go` — regenerated by sqlc.
- `internal/storage/sqlite/sqlitedb/querier.go` — regenerated by sqlc.
- `internal/ingest/status.go` — new: `Status` struct + `StatusFor`.
- `internal/ingest/status_test.go` — new.
- `internal/ingest/scheduler.go` — invoke prune; thread retention from cfg.
- `internal/ingest/scheduler_test.go` — pruning test + small helper to
  configure retention.
- `internal/config/config.go` — extend `Poll`, parse env, validate.
- `internal/config/config_test.go` — default + override + validation.
- `.plans/upnext/0031-sync-runs-tracking/verify.sh` — verification script.

No migration changes. No main.go changes (fx wires everything off `Poll`
and `*sqlitedb.Queries`, both of which already exist).

## Steps

1. **Add queries.** Edit `internal/storage/sqlite/queries/sync_runs.sql`
   and append:

   ```sql
   -- name: GetLastSuccessfulSyncRun :one
   SELECT * FROM sync_runs
   WHERE connection_id = @connection_id
     AND ok = 1
   ORDER BY started_at DESC
   LIMIT 1;

   -- name: GetLastFailedSyncRun :one
   SELECT * FROM sync_runs
   WHERE connection_id = @connection_id
     AND ok = 0
     AND error != ''
   ORDER BY started_at DESC
   LIMIT 1;

   -- name: PruneSyncRunsByConnection :exec
   DELETE FROM sync_runs
   WHERE connection_id = @connection_id
     AND id NOT IN (
       SELECT id FROM sync_runs
       WHERE connection_id = @connection_id
       ORDER BY started_at DESC
       LIMIT @keep_n
     );

   -- name: CountFailedSyncRunsSince :one
   SELECT COUNT(*) FROM sync_runs
   WHERE finished_at IS NOT NULL
     AND ok = 0
     AND started_at >= @since;
   ```

   Run `sqlc generate` from repo root (uses `sqlc.yaml`). Verify
   `sqlc diff` exits clean. Commit:
   `feat(storage): sync_runs status + retention queries (#0031)`.

2. **Status helper.** Create `internal/ingest/status.go`:

   ```go
   package ingest

   import (
       "context"
       "database/sql"
       "errors"

       "github.com/karnstack/tempo/internal/storage/sqlite/sqlitedb"
   )

   // Status is the per-connection sync health snapshot consumed by
   // /api/v1/sync/status. Pointer fields are nil when no row of that
   // kind exists yet.
   type Status struct {
       ConnectionID int64
       Latest       *sqlitedb.SyncRun
       LastSuccess  *sqlitedb.SyncRun
       LastFailure  *sqlitedb.SyncRun
   }

   // StatusFor reads three indexed rows (latest, last success, last
   // failure) for a connection. Missing rows are reported as nil
   // pointers, not errors. Any other DB error short-circuits.
   func StatusFor(ctx context.Context, q *sqlitedb.Queries,
       connectionID int64) (Status, error) {
       st := Status{ConnectionID: connectionID}

       if r, err := q.GetLatestSyncRun(ctx, connectionID); err == nil {
           st.Latest = &r
       } else if !errors.Is(err, sql.ErrNoRows) {
           return Status{}, err
       }

       if r, err := q.GetLastSuccessfulSyncRun(ctx, connectionID); err == nil {
           st.LastSuccess = &r
       } else if !errors.Is(err, sql.ErrNoRows) {
           return Status{}, err
       }

       if r, err := q.GetLastFailedSyncRun(ctx, connectionID); err == nil {
           st.LastFailure = &r
       } else if !errors.Is(err, sql.ErrNoRows) {
           return Status{}, err
       }

       return st, nil
   }
   ```

   Add tests in `internal/ingest/status_test.go` covering:
   - empty: all three pointers nil, `ConnectionID` stamped.
   - only successes: `Latest == LastSuccess`, `LastFailure == nil`.
   - only failures: `Latest == LastFailure`, `LastSuccess == nil`.
   - mixed: `Latest` is whichever finished most recently; `LastSuccess`
     and `LastFailure` each point to the most recent of their kind.

   Use `newIntegrationStore` from `scheduler_test.go` — extract it into
   a small `helpers_test.go` if duplication itches, otherwise just
   reuse it via a package-level test helper.

   Commit: `feat(ingest): StatusFor helper for sync_runs (#0031)`.

3. **Config knob.** In `internal/config/config.go`:

   - Add `SyncRunRetention int` to `Poll`.
   - In `loadPoll`, read `TEMPO_SYNC_RUN_RETENTION` (default `200`),
     parse with `strconv.Atoi`, and validate `>= 1`.

   In `internal/config/config_test.go`:
   - Extend `TestLoadDefaults` (or equivalent) to assert
     `cfg.Poll.SyncRunRetention == 200`.
   - Add `TestLoadParsesSyncRunRetention` setting the env to `50` and
     asserting it.
   - Add `TestLoadRejectsZeroSyncRunRetention` setting it to `0` and
     asserting `Load` returns an error mentioning the variable.

   Commit: `feat(config): TEMPO_SYNC_RUN_RETENTION (#0031)`.

4. **Wire pruning into the scheduler.** In `internal/ingest/scheduler.go`,
   inside `syncConnection`, after the call to `s.finishRun(...)` at the
   end of the happy and error paths, call:

   ```go
   if err := s.q.PruneSyncRunsByConnection(writeCtx, sqlitedb.PruneSyncRunsByConnectionParams{
       ConnectionID: conn.ID,
       KeepN:        int64(s.cfg.Poll.SyncRunRetention),
   }); err != nil {
       s.log.Warn("ingest: prune sync_runs",
           zap.Int64("connection_id", conn.ID), zap.Error(err))
   }
   ```

   Hoist this into a small `pruneRuns(writeCtx, conn.ID)` method so all
   exit paths call it once. Make sure it runs on:
   - happy path (after `UpdateConnectionLastSync`),
   - runner-error path,
   - token-decrypt / token-missing path,
   - ctx-cancelled mid-runner path.

   In each case use the same `writeCtx` style as `finishRun`: if the
   parent ctx is cancelled, fall back to a 5-second-timeout
   `context.Background()` so the prune still lands. Refactor
   `finishRun` to return its `writeCtx` (or factor a tiny helper
   `closingCtx(ctx) (context.Context, context.CancelFunc)`), then both
   `finishRun` and `pruneRuns` share it.

   Commit: `feat(ingest): prune sync_runs to retention (#0031)`.

5. **Scheduler test.** In `internal/ingest/scheduler_test.go`, add a
   helper to override `Poll.SyncRunRetention` (extend `newScheduler` to
   accept a retention override, or add a new constructor wrapper). Add
   `TestTick_PrunesSyncRunsToRetention`:

   ```go
   func TestTick_PrunesSyncRunsToRetention(t *testing.T) {
       t.Parallel()
       q := newIntegrationStore(t)
       box := newBox(t)
       tn := seedTenant(t, q)
       tok := seedToken(t, q, box, tn, "ghp_secret")
       conn := seedConnection(t, q, tn, tok, "active", "octo", strPtr("hello"))

       // Seed 5 prior sync_runs spaced 1ms apart.
       base := time.Now().Add(-time.Hour).UTC()
       for i := 0; i < 5; i++ {
           r, err := q.StartSyncRun(context.Background(), sqlitedb.StartSyncRunParams{
               ConnectionID: conn.ID,
               StartedAt:    base.Add(time.Duration(i) * time.Millisecond),
           })
           if err != nil { t.Fatalf("StartSyncRun: %v", err) }
           finishedAt := r.StartedAt.Add(100 * time.Microsecond)
           if err := q.FinishSyncRun(context.Background(), sqlitedb.FinishSyncRunParams{
               ID: r.ID, FinishedAt: &finishedAt, Ok: 1, Items: 0,
               RateLimitRemaining: nil, Error: "",
           }); err != nil { t.Fatalf("FinishSyncRun: %v", err) }
       }

       s := newSchedulerWithRetention(t, q, box, nil, 3)
       s.Tick(context.Background())

       runs, err := q.ListSyncRunsByConnection(context.Background(), sqlitedb.ListSyncRunsByConnectionParams{
           ConnectionID: conn.ID, LimitN: 10,
       })
       if err != nil { t.Fatalf("ListSyncRunsByConnection: %v", err) }
       if len(runs) != 3 {
           t.Fatalf("len(runs) = %d, want 3 (retention)", len(runs))
       }
       // The new tick's run must survive — it's the newest.
       if runs[0].FinishedAt == nil || runs[0].Ok != 1 {
           t.Errorf("most recent row not the freshly-ticked run: %+v", runs[0])
       }
   }
   ```

   Commit: `test(ingest): scheduler prunes sync_runs to retention (#0031)`.

6. **Verify.** Write `.plans/upnext/0031-sync-runs-tracking/verify.sh`:

   ```bash
   #!/usr/bin/env bash
   set -euo pipefail
   cd "$(git rev-parse --show-toplevel)"

   echo "==> sqlc diff"
   sqlc diff

   echo "==> go vet (ingest, storage, config)"
   go vet ./internal/ingest/... ./internal/storage/... ./internal/config/...

   echo "==> go build ./..."
   go build ./...

   echo "==> go test focused"
   go test ./internal/ingest/... ./internal/storage/... ./internal/config/... -race -count=1

   echo "==> go test ./... -race -count=1"
   go test ./... -race -count=1

   echo "VERIFY OK"
   ```

   `chmod +x` it. Run it. Expect `VERIFY OK`.

## Notes

- **Why `error != ''` in `GetLastFailedSyncRun`.** `ok = 0` is set the
  moment a run starts (the schema default) and only flipped to `1` on
  success. So an in-flight run that hasn't been `FinishSyncRun`'d yet
  technically matches `ok = 0`. Adding `error != ''` filters those out
  — by the time a row has an error it's necessarily finished.
- **Why `LIMIT @keep_n` inside the `NOT IN` subquery.** SQLite's
  `DELETE ... NOT IN (SELECT ... LIMIT N)` is the idiomatic
  "keep newest N" pattern. The subquery returns the IDs to *retain*;
  everything else for that connection is deleted. The
  `(connection_id, started_at)` index makes both halves cheap.
- **Why prune after every tick, not on a schedule.** Pruning is O(rows
  in this connection only) and bounded by `keep_n`. Doing it inline
  removes a whole second-tier scheduler. The amortized cost is one
  `DELETE` per tick per connection — negligible.
- **Why `keep_n` (count) instead of `since` (age).** A paused or
  rarely-syncing connection shouldn't lose its history just because it
  wasn't ticked recently. Count-based retention also makes UI promises
  predictable: "the last N runs" maps cleanly to a paginated history
  view in 0055.
- **Why retention default 200.** At the default 15-min cadence,
  200 runs ≈ 50 hours ≈ 2 days. That's enough for the dashboard to
  show "recent activity" plus a comfortable buffer for diagnosing an
  overnight breakage. Operators can dial up with the env var.
- **Why no fail-fast on the prune error.** Pruning is housekeeping. A
  transient lock or disk-full doesn't change the truth of the just-
  recorded sync run. We log and move on — the next tick will retry.
- **Why no extra index.** Each of the new queries filters by
  `connection_id` first and orders by `started_at DESC LIMIT 1`. The
  existing `sync_runs_connection_started_idx` is a compound index on
  exactly `(connection_id, started_at)`, which SQLite walks in reverse
  for `ORDER BY ... DESC`. The trailing `ok = 1` / `ok = 0` and
  `error != ''` filters apply to a tiny slice (top-of-index per
  connection); a partial index is overkill.
- **Why no Go struct for `CountFailedSyncRunsSince` yet.** 0044
  consumes it directly — `q.CountFailedSyncRunsSince(ctx, since)` is
  already the cleanest API surface. We expose the query and stop.
