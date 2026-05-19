---
id: 0035
slug: cycle-time-rollup
title: Cycle time + lead time rollup p50/p90
status: done
depends_on: [0033, 0034]
owner: ""
est_minutes: 45
tags: [rollup]
autonomy: full
skills: []
---

## Goal

Wire the third concrete `rollup.Aggregator` — cycle / lead time — into
the scheduler's `"rollup.aggregators"` fx value group. For a given
local date `D` in the scheduler's tz, it populates the two
`lead_time_seconds_*` columns of `daily_repo_stats` for every
non-archived repo by computing percentiles over per-PR
`merged_at - created_at` durations for PRs **merged** on `D`.

Schema reminder
(`migrations/0003_daily_rollups.sql:19-29`,
`docs/superpowers/specs/2026-05-08-tempo-design.md:110`):

```
daily_repo_stats(date, repo_id,
  prs_opened, prs_merged, prs_closed, deploys,     -- owned by 0034
  lead_time_seconds_p50, lead_time_seconds_p90)    -- owned by 0035
```

There is no `cycle_time_*` column in v1 — the master plan title says
"Cycle time + lead time" because in PR terms they collapse to the same
metric: time from PR open to merge. The DORA "lead time for changes"
(commit → deploy) is out of scope for v1; we don't yet associate
deployments with the commits/PRs they ship.

Attribution rules:

| Column                  | Source                                | Time bucket   |
| ----------------------- | ------------------------------------- | ------------- |
| `lead_time_seconds_p50` | `merged_at - created_at` over merged PRs (per repo) | `merged_at`   |
| `lead_time_seconds_p90` | same                                  | `merged_at`   |

Archived repos are skipped. A repo with **zero** merged PRs on `D` gets
`NULL` p50 and `NULL` p90 — but the aggregator still UPSERTs the row
so a previous-run-with-data → next-run-without-data transition clears
the stale percentiles. (Same idempotency story as 0034's counts.)

**Disjoint-columns contract with 0034.** This aggregator's UPSERT
INSERTs only `(date, repo_id, lead_time_seconds_p50,
lead_time_seconds_p90)` and the count columns rely on their schema
DEFAULT 0. The `ON CONFLICT (date, repo_id) DO UPDATE SET` clause
**must not** mention `prs_opened/prs_merged/prs_closed/deploys` —
those belong to `repostats` (0034). Mirrors 0034's contract in the
opposite direction; `RESULT.md` for 0034 enshrines the same rule.

The aggregator order between siblings is undefined in `rollup.Run`,
so this disjoint-columns discipline is the contract that lets either
run first without trampling the other.

**No transaction needed.** Like 0034, this is a single
`INSERT … ON CONFLICT DO UPDATE` per repo with no companion DELETE —
SQLite handles atomicity itself. (0033 needed a tx for DELETE +
INSERT; here there's only INSERT.)

**Percentile method: nearest-rank.** For a sorted list of `n`
durations, p_k = sorted[ceil(k·n/100) − 1] (clamped to `[0, n−1]`).
With one sample, p50 = p90 = sorted[0]; with two samples, p50 =
sorted[0] and p90 = sorted[1]. Integer seconds throughout — we
truncate `time.Duration / time.Second`. Two-pass float math (50.0
quantile via linear interpolation) would change values by ≤1s and
make the test cases hard to spell out; nearest-rank is what dashboards
typically want anyway ("the kth-worst PR's wait").

### Scope notes / non-goals

- **Only this aggregator.** Engineer stats ships (0033); repo counts
  ship (0034); review latency is 0036.
- **Only `daily_repo_stats.lead_time_seconds_*`.** No new tables, no
  new columns.
- **Negative durations are filtered out.** `merged_at < created_at`
  shouldn't happen (GitHub doesn't allow it) but a clock-skew row
  would skew low percentiles otherwise.
- **No DORA "commit → deploy" lead time.** That would need a
  PR-to-deploy join we don't track in v1.
- **No retention pruning.** Old days stay forever.
- **No public re-run API.** 0037 owns that.

## Acceptance criteria

- [ ] `internal/storage/sqlite/queries/daily_repo_stats.sql` adds
      `-- name: UpsertRepoLeadTime :exec`:

      ```sql
      INSERT INTO daily_repo_stats (
        date, repo_id, lead_time_seconds_p50, lead_time_seconds_p90
      ) VALUES (
        @date, @repo_id, @lead_time_seconds_p50, @lead_time_seconds_p90
      )
      ON CONFLICT (date, repo_id) DO UPDATE SET
        lead_time_seconds_p50 = excluded.lead_time_seconds_p50,
        lead_time_seconds_p90 = excluded.lead_time_seconds_p90;
      ```

      The `ON CONFLICT` clause **must not** mention `prs_*` or
      `deploys` — those are owned by 0034.

- [ ] `sqlc generate` succeeds and `sqlc diff` exits clean.

- [ ] `internal/rollup/cycletime/aggregator.go` declares:

      ```go
      type Aggregator struct {
          db  *sql.DB
          q   *sqlitedb.Queries
          log *zap.Logger
      }

      func New(s storage.Storage, l *zap.Logger) *Aggregator
      func (*Aggregator) Name() string                       // "cycle_time"
      func (a *Aggregator) Aggregate(ctx context.Context, date time.Time) error
      ```

      `Aggregate` walks every non-archived repo. Per repo:

      1. `ListMergedPullRequestsByRepoBetween` to fetch PRs merged in
         `[date, date+24h)`.
      2. Collect `int64(MergedAt.Sub(CreatedAt) / time.Second)`,
         skipping any with `merged_at == nil` (defensive) or negative
         duration.
      3. If `len(durations) == 0`, UPSERT with both percentile params
         set to `nil` (`*int64(nil)` → SQL `NULL`).
      4. Otherwise sort ascending and compute nearest-rank p50/p90.
      5. `UpsertRepoLeadTime` with the computed pointers.

      Per-repo errors don't short-circuit sibling repos — log + record
      the first error and continue, mirroring 0033 and 0034 and the
      scheduler's aggregator-error policy.

- [ ] `internal/rollup/cycletime/module.go` exports a `fx.Module` that
      provides `*Aggregator` into the `"rollup.aggregators"` value
      group, same shape as `internal/rollup/repostats/module.go`.

- [ ] `cmd/tempo/main.go` includes `cycletime.Module` alongside
      `engineerstats.Module` and `repostats.Module`.

- [ ] `internal/rollup/cycletime/aggregator_test.go` covers, with a
      real on-disk sqlite via the same harness pattern as
      `internal/rollup/repostats/aggregator_test.go`:
      - Empty database → no error, no rows.
      - Single merged PR → both percentiles equal that PR's duration.
      - Multi-PR percentile math (4 PRs with distinct durations →
        nearest-rank p50 = sorted[1], p90 = sorted[3]).
      - Window boundaries: PRs with `merged_at` just before/after the
        day boundary are excluded; PRs at `date` and at
        `date + 24h − 1ns` are included.
      - Multi-repo partitioning — each repo's row uses only its own
        merged PRs.
      - Archived repo is skipped (no row written).
      - No merged PRs in window for a repo with prior counts → p50/p90
        UPSERT to NULL; counts are untouched (idempotent clearing).
      - **Disjoint-columns contract:** seed a row with non-NULL
        `prs_opened/prs_merged/prs_closed/deploys` via
        `UpsertDailyRepoStats`, then run `Aggregate` with merged-PR
        data. The two lead-time columns must update; the four count
        columns must remain at the seeded values. This locks in the
        ON-CONFLICT-DO-UPDATE clause's column list.
      - Negative duration is filtered (merged_at < created_at). One
        valid PR + one negative PR → percentiles reflect the valid PR
        only.

- [ ] `go test ./internal/rollup/...` and `go vet ./...` pass.

- [ ] `verify.sh` runs: `sqlc diff`, `go vet ./...`, `go build ./...`,
      `go test ./internal/rollup/...` and exits 0.

## Files to touch

- `internal/storage/sqlite/queries/daily_repo_stats.sql` — add
  `UpsertRepoLeadTime`.
- `internal/storage/sqlite/sqlitedb/daily_repo_stats.sql.go`,
  `internal/storage/sqlite/sqlitedb/querier.go` — regenerated by sqlc.
- `internal/rollup/cycletime/aggregator.go` (new).
- `internal/rollup/cycletime/module.go` (new).
- `internal/rollup/cycletime/aggregator_test.go` (new).
- `cmd/tempo/main.go` — `cycletime.Module` registered.
- `.plans/upnext/0035-cycle-time-rollup/verify.sh`.

## Steps

### 1. sqlc query

Append `UpsertRepoLeadTime` to
`internal/storage/sqlite/queries/daily_repo_stats.sql` (see acceptance
criteria for the exact SQL). Run `make sqlc-generate`.

Commit: `feat(storage): repo lead-time UPSERT query (#0035)`.

### 2. Aggregator

Create `internal/rollup/cycletime/aggregator.go`. Use the
`repostats.Aggregator` skeleton as a baseline — same struct, same
constructor signature, same per-repo error policy. The only departure
is the per-repo body: fetch merged PRs, compute durations in Go, sort,
compute nearest-rank percentiles, UPSERT (with `*int64` for NULL
support).

```go
const aggregatorName = "cycle_time"

// percentile returns the nearest-rank percentile of a sorted slice.
// q ∈ [0, 100]. Caller must pre-sort ascending and guarantee len > 0.
func percentile(sorted []int64, q int) int64 {
    idx := (q*len(sorted) + 99) / 100 - 1 // ceil(q*n/100) - 1
    if idx < 0 { idx = 0 }
    if idx >= len(sorted) { idx = len(sorted) - 1 }
    return sorted[idx]
}
```

Commit: `feat(rollup): cycle_time aggregator (#0035)`.

### 3. fx Module

Create `internal/rollup/cycletime/module.go` mirroring
`internal/rollup/repostats/module.go`. Wire `cycletime.Module` into
`cmd/tempo/main.go` alongside the existing rollup modules.

Commit: `feat(rollup): fx wiring for cycle_time (#0035)`.

### 4. Tests

Create `internal/rollup/cycletime/aggregator_test.go`. Reuse the same
on-disk sqlite + `migrations.Apply` harness as
`internal/rollup/repostats/aggregator_test.go`. Lift `newStorage`,
`seedRepo`, `seedPR`, `readRow`, `addDate`. No deployment seeders
needed.

Test cases (one func each):

- `TestAggregate_EmptyDatabase`
- `TestAggregate_SingleMergedPR` — p50 == p90 == duration.
- `TestAggregate_PercentilesNearestRank` — 4 PRs with durations
  `[100, 200, 300, 400]` → p50 = sorted[1] = 200, p90 = sorted[3] = 400.
- `TestAggregate_WindowBoundariesOnMergedAt`
- `TestAggregate_MultiRepoPartitioning`
- `TestAggregate_ArchivedRepoSkipped`
- `TestAggregate_NoMergedPRsClearsPercentiles` — seed a row with
  non-NULL percentiles via `UpsertDailyRepoStats`; run with no merged
  PRs in window; assert both p50/p90 become NULL and counts untouched.
- `TestAggregate_CountColumnsPreserved` — disjoint-columns contract
  in the opposite direction from 0034.
- `TestAggregate_NegativeDurationFiltered`

Commit: `test(rollup): cycle_time aggregator coverage (#0035)`.

### 5. Verify

`./verify.sh` from the task dir. Should output the four section
headers (sqlc diff, go vet, go build, go test) with the tests in
`internal/rollup/...` passing.

## Notes

- The aggregator does not consult `gh_users` — lead time is per-repo,
  not per-author. Author-level analyses can join on `pull_requests`
  directly when needed.
- Nearest-rank vs linear interpolation: the dashboard rendering can
  switch to a histogram-style display later without touching the
  rollup. For now, integer-second percentiles are stable, easy to
  test, and good enough for v1's small daily samples.
- 0037 hook is free: same `Aggregate(ctx, date)` shape, idempotent
  by UPSERT (including the no-data → NULL transition).
