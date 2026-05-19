---
id: 0034
slug: repo-stats-rollup
title: Repo stats rollup
status: done
depends_on: [0032]
owner: ""
est_minutes: 45
tags: [rollup]
autonomy: full
skills: []
---

## Goal

Wire the second concrete `rollup.Aggregator` — repo stats — into the
scheduler's `"rollup.aggregators"` fx value group. For a given local
date `D` in the scheduler's tz, it rebuilds the counts columns of
`daily_repo_stats` for every non-archived repo by aggregating from
`pull_requests` and `deployments`.

`daily_repo_stats(date, repo_id, prs_opened, prs_merged, prs_closed,
deploys, lead_time_seconds_p50, lead_time_seconds_p90)` is keyed by
`(date, repo_id)`. This aggregator owns **only the four count columns**
(`prs_opened`, `prs_merged`, `prs_closed`, `deploys`). The two
`lead_time_seconds_*` columns are owned by 0035 and must be left
untouched on UPSERT.

Attribution rules:

| Column        | Source                                                                  | Time column   |
| ------------- | ----------------------------------------------------------------------- | ------------- |
| `prs_opened`  | `pull_requests`                                                         | `created_at`  |
| `prs_merged`  | `pull_requests WHERE merged_at IS NOT NULL`                             | `merged_at`   |
| `prs_closed`  | `pull_requests WHERE merged_at IS NULL AND closed_at IS NOT NULL`       | `closed_at`   |
| `deploys`     | `deployments` (all rows; status is empty in v1 — see deployments doc)   | `created_at`  |

`prs_closed` is "closed without merging" — a merged PR has both
`merged_at` and `closed_at` populated by GitHub, so excluding merged
rows avoids double-counting against `prs_merged`. The dashboard wants
abandoned-PR counts, not "any close event including merges".

Archived repos are skipped. There is no per-user grouping here — repo
stats are one row per `(date, repo_id)`.

**Idempotency is via UPSERT, not DELETE + INSERT.** Because the row is
keyed only by `(date, repo_id)` (no per-user fan-out), there is no
stale-row leakage when source data shrinks — the four count subqueries
just return 0. And because lead-time columns belong to a sibling
aggregator (0035), we MUST NOT clear the row before re-aggregating:
`ON CONFLICT (date, repo_id) DO UPDATE SET` lists only this
aggregator's four columns, leaving `lead_time_seconds_p50/p90`
untouched. The aggregator order between siblings is undefined in
`rollup.Run`, so this disjoint-columns discipline is the contract.

No per-repo transaction either: the whole aggregation is a single
`INSERT … ON CONFLICT DO UPDATE`, which SQLite executes atomically on
its own. (0033 needed a tx for DELETE + INSERT; here there's only
INSERT.)

Spec refs:

- `docs/superpowers/specs/2026-05-08-tempo-design.md` line 110 — column
  set for `daily_repo_stats`.
- `internal/ingest/deployments/doc.go:42-44` — explicit note that this
  rollup just counts deploy rows regardless of status.
- Master plan row 165 (Phase 6) — autonomy `full`, deps `0032`, scope
  hint "(counts, deploys)" confirms lead-time is 0035, not here.

### Scope notes / non-goals

- **Only this aggregator.** Engineer stats already ships (0033);
  cycle/lead time is 0035; review latency is 0036.
- **No lead-time columns.** Both `lead_time_seconds_p50` and
  `lead_time_seconds_p90` stay NULL after this run (0035 populates
  them).
- **No retention pruning.** Old days stay forever.
- **No public re-run API.** That's 0037, which will loop
  `Aggregate(ctx, date)` per local date.
- **No releases-as-deploys.** The deployments ingest only sources
  GitHub Deployments (per `internal/ingest/deployments/doc.go:34-40`);
  this rollup counts whatever's in the `deployments` table.
- **`prs_closed` excludes merged PRs** by definition (see attribution
  table). If we ever need "any close event" the column name in the
  spec is ambiguous enough that we'd add a sibling column rather than
  change this one.

## Acceptance criteria

- [ ] `internal/storage/sqlite/queries/daily_repo_stats.sql` adds
      `-- name: AggregateRepoStatsForDay :exec` — a single
      `INSERT INTO daily_repo_stats (date, repo_id, prs_opened,
      prs_merged, prs_closed, deploys) VALUES (@date, @repo_id, (…
      COUNT subqueries …)) ON CONFLICT (date, repo_id) DO UPDATE SET
      prs_opened = excluded.prs_opened, prs_merged =
      excluded.prs_merged, prs_closed = excluded.prs_closed, deploys =
      excluded.deploys;`. The `ON CONFLICT` clause **must not** mention
      `lead_time_seconds_p50` or `lead_time_seconds_p90` — 0035 owns
      those.

      The existing `UpsertDailyRepoStats` query stays as-is; the
      future write path for `lead_time_seconds_*` is 0035's design
      decision.

- [ ] `sqlc generate` succeeds and `sqlc diff` exits clean.

      Fallback note: if sqlc-sqlite chokes on multi-use named params
      (`@repo_id`, `@from_ts`, `@to_ts` each appear in four
      subqueries), follow 0033's escape hatch — keep a doc-only stub
      in the `.sql` file pointing at a Go-resident `const aggregateSQL`
      in `aggregator.go` and run it via `db.ExecContext` with
      positional `?1..?4` args. Either path is acceptable as long as
      the generated code compiles and the tests pass.

- [ ] `internal/rollup/repostats/aggregator.go` declares:

      ```go
      type Aggregator struct {
          db  *sql.DB
          q   *sqlitedb.Queries
          log *zap.Logger
      }

      func New(s storage.Storage, l *zap.Logger) *Aggregator
      func (*Aggregator) Name() string                       // "repo_stats"
      func (a *Aggregator) Aggregate(ctx context.Context, date time.Time) error
      ```

      `Aggregate` does, for each repo in `ListAllRepos` order:

      1. Skip if `r.Archived != 0`.
      2. Run the single aggregation statement (sqlc-typed or raw,
         depending on the fallback path).

      `date_str = date.Format("2006-01-02")` in the date's location.
      `from_ts = date` (already local-midnight from the scheduler).
      `to_ts = date.AddDate(0, 0, 1)`.

      Per-repo errors don't short-circuit sibling repos — log + record
      the first error and continue, mirroring 0033 and the scheduler's
      aggregator-error policy.

- [ ] `internal/rollup/repostats/module.go` exports a `fx.Module` that
      provides `*Aggregator` into the `"rollup.aggregators"` value
      group, same shape as `internal/rollup/engineerstats/module.go`.

- [ ] `cmd/tempo/main.go` includes `repostats.Module` alongside
      `engineerstats.Module`.

- [ ] `internal/rollup/repostats/aggregator_test.go` covers, with a
      real on-disk sqlite via the same harness as
      `internal/rollup/engineerstats/aggregator_test.go`:
      - Empty database → no-op, no error.
      - Single repo with PRs opened/merged/closed and deployments on
        the target day → counts match the expected single row.
      - Multi-repo partitioning → each repo gets its own row with
        only its own counts.
      - Merged PR is counted as `prs_merged`, not `prs_closed`, even
        though `closed_at` is populated.
      - Archived repo is skipped (no row written).
      - Window boundaries: rows just before/after the day boundary are
        excluded; rows at `date` and at `date + 24h − 1ns` are
        included.
      - Idempotency: running twice produces identical state; deleting
        a source PR and re-running drives the count back to 0 without
        leaving a stale row at the old count.
      - **Disjoint-columns contract:** seed a row with non-NULL
        `lead_time_seconds_p50/p90` via `UpsertDailyRepoStats`, then
        run `Aggregate`. The four count columns must update; the two
        lead-time columns must remain untouched. This locks in the
        ON-CONFLICT-DO-UPDATE clause's column list.

- [ ] `go test ./internal/rollup/...` and `go vet ./...` pass.

- [ ] `verify.sh` runs: `sqlc diff`, `go vet ./...`, `go build ./...`,
      `go test ./internal/rollup/...` and exits 0.

## Files to touch

- `internal/storage/sqlite/queries/daily_repo_stats.sql` — add
  `AggregateRepoStatsForDay`.
- `internal/storage/sqlite/sqlitedb/daily_repo_stats.sql.go`,
  `internal/storage/sqlite/sqlitedb/querier.go` — regenerated by sqlc.
- `internal/rollup/repostats/aggregator.go` (new).
- `internal/rollup/repostats/module.go` (new).
- `internal/rollup/repostats/aggregator_test.go` (new).
- `cmd/tempo/main.go` — `repostats.Module` registered.
- `.plans/upnext/0034-repo-stats-rollup/verify.sh`.

## Steps

### 1. sqlc query

Append to `internal/storage/sqlite/queries/daily_repo_stats.sql`:

```sql
-- name: AggregateRepoStatsForDay :exec
INSERT INTO daily_repo_stats (
  date, repo_id,
  prs_opened, prs_merged, prs_closed, deploys
) VALUES (
  @date, @repo_id,
  (SELECT COUNT(*) FROM pull_requests
     WHERE repo_id = @repo_id
       AND created_at >= @from_ts AND created_at < @to_ts),
  (SELECT COUNT(*) FROM pull_requests
     WHERE repo_id = @repo_id
       AND merged_at IS NOT NULL
       AND merged_at >= @from_ts AND merged_at < @to_ts),
  (SELECT COUNT(*) FROM pull_requests
     WHERE repo_id = @repo_id
       AND merged_at IS NULL
       AND closed_at IS NOT NULL
       AND closed_at >= @from_ts AND closed_at < @to_ts),
  (SELECT COUNT(*) FROM deployments
     WHERE repo_id = @repo_id
       AND created_at >= @from_ts AND created_at < @to_ts)
)
ON CONFLICT (date, repo_id) DO UPDATE SET
  prs_opened = excluded.prs_opened,
  prs_merged = excluded.prs_merged,
  prs_closed = excluded.prs_closed,
  deploys    = excluded.deploys;
```

Run `make sqlc-generate`. If sqlc balks at multi-use named params,
delete this block, replace it with a doc-only `-- repo_stats
aggregation SQL lives in internal/rollup/repostats/aggregator.go` note
(same pattern as 0033's `daily_engineer_stats.sql`), and carry the SQL
into Step 2 as a Go `const` with positional `?1=date_str, ?2=repo_id,
?3=from_ts, ?4=to_ts` placeholders.

Commit: `feat(storage): repo_stats aggregation query (#0034)`.

### 2. Aggregator

Create `internal/rollup/repostats/aggregator.go`:

```go
// Package repostats implements the rollup.Aggregator that rebuilds
// the count columns of daily_repo_stats from pull_requests and
// deployments.
//
// The four count columns (prs_opened, prs_merged, prs_closed, deploys)
// belong to this aggregator. The two lead_time_seconds_* columns
// belong to the cycle-time aggregator (0035) and must be left
// untouched on UPSERT — see the ON CONFLICT clause in the underlying
// query.
package repostats

import (
    "context"
    "database/sql"
    "fmt"
    "time"

    "github.com/karnstack/tempo/internal/storage"
    "github.com/karnstack/tempo/internal/storage/sqlite/sqlitedb"
    "go.uber.org/zap"
)

const aggregatorName = "repo_stats"

type Aggregator struct {
    db  *sql.DB
    q   *sqlitedb.Queries
    log *zap.Logger
}

func New(s storage.Storage, l *zap.Logger) *Aggregator {
    db := s.DB()
    return &Aggregator{db: db, q: sqlitedb.New(db), log: l}
}

func (*Aggregator) Name() string { return aggregatorName }

func (a *Aggregator) Aggregate(ctx context.Context, date time.Time) error {
    dateStr := date.Format("2006-01-02")
    fromTS := date
    toTS := date.AddDate(0, 0, 1)

    repos, err := a.q.ListAllRepos(ctx)
    if err != nil {
        return fmt.Errorf("list repos: %w", err)
    }

    var firstErr error
    for _, r := range repos {
        if r.Archived != 0 {
            continue
        }
        if err := a.aggregateRepo(ctx, r.ID, dateStr, fromTS, toTS); err != nil {
            a.log.Warn("rollup/repo_stats: repo failed",
                zap.Int64("repo_id", r.ID),
                zap.String("date", dateStr),
                zap.Error(err))
            if firstErr == nil {
                firstErr = fmt.Errorf("repo %d: %w", r.ID, err)
            }
        }
    }
    return firstErr
}

func (a *Aggregator) aggregateRepo(ctx context.Context, repoID int64, dateStr string, from, to time.Time) error {
    // Single-statement UPSERT — SQLite handles atomicity. Disjoint
    // columns from 0035 mean no DELETE is needed, and ON CONFLICT DO
    // UPDATE only touches the four count columns this aggregator owns.
    if err := a.q.AggregateRepoStatsForDay(ctx, sqlitedb.AggregateRepoStatsForDayParams{
        Date:   dateStr,
        RepoID: repoID,
        FromTs: from,
        ToTs:   to,
    }); err != nil {
        return fmt.Errorf("aggregate: %w", err)
    }
    return nil
}
```

If Step 1 fell back to Go-resident SQL, replace the body of
`aggregateRepo` with `_, err := a.db.ExecContext(ctx, aggregateSQL,
dateStr, repoID, from, to)` and add the `const aggregateSQL` at the
top of the file (mirroring `internal/rollup/engineerstats/aggregator.go`'s
shape).

Commit: `feat(rollup): repo_stats aggregator (#0034)`.

### 3. fx Module

Create `internal/rollup/repostats/module.go`:

```go
package repostats

import (
    "github.com/karnstack/tempo/internal/rollup"
    "go.uber.org/fx"
)

// Module provides *Aggregator into the "rollup.aggregators" value
// group consumed by rollup.Run. Mirrors engineerstats.Module.
var Module = fx.Module("rollup.repo_stats",
    fx.Provide(
        fx.Annotate(
            New,
            fx.As(new(rollup.Aggregator)),
            fx.ResultTags(`group:"rollup.aggregators"`),
        ),
    ),
)
```

Wire into `cmd/tempo/main.go` alongside `engineerstats.Module`:

```go
import (
    ...
    "github.com/karnstack/tempo/internal/rollup/repostats"
)
...
    engineerstats.Module,
    repostats.Module,
```

Commit: `feat(rollup): fx wiring for repo_stats (#0034)`.

### 4. Tests

Create `internal/rollup/repostats/aggregator_test.go`. Reuse the same
on-disk sqlite + `migrations.Apply` harness as
`internal/rollup/engineerstats/aggregator_test.go`. Lift the
`newStorage`, `seedRepo`, and `seedPR` helpers; add a new
`seedDeployment` helper (one `UpsertDeployment` call with `status=""`
to match what the ingest writes). No need for the gh_users / commits /
reviews / comments seeders — repo stats doesn't read those.

Test cases (one func each):

- `TestAggregate_EmptyDatabase` — `Aggregate` on a fresh DB does not
  error and writes no rows.
- `TestAggregate_AllSourcesPopulated` — single repo, on the target
  day: 1 opened, 1 merged, 1 closed-not-merged, 2 deploys → exactly one
  row with counts (1, 1, 1, 2) and lead-time columns NULL.
- `TestAggregate_MergedPRNotDoubleCounted` — single PR opened and
  merged on the target day (so `closed_at` is also set by the
  upserter). `prs_merged` = 1, `prs_closed` = 0.
- `TestAggregate_MultiRepoPartitioning` — two repos, different
  activity, both produce correctly-scoped rows.
- `TestAggregate_ArchivedRepoSkipped` — repo with `archived=1` and
  in-window deployments produces zero rows.
- `TestAggregate_WindowBoundaries` — events at
  `target - 1ns`, `target` (inclusive start), `target + 24h - 1ns`
  (inclusive end), and `target + 24h` (excluded end); only the
  middle two count.
- `TestAggregate_IdempotentRerunAndStaleCountsCleared` — run, delete
  a source PR, re-run, expect the count to drop to 0 in the same row.
- `TestAggregate_LeadTimeColumnsPreserved` — call
  `UpsertDailyRepoStats` first to seed a row with non-NULL
  `lead_time_seconds_p50/p90`; then `Aggregate`; assert the count
  columns updated and the lead-time columns are still the seeded
  non-NULL values. This is the disjoint-columns contract test.

Run `go test ./internal/rollup/repostats/...` to confirm.

Commit: `test(rollup): repo_stats aggregator coverage (#0034)`.

### 5. Verify

`./verify.sh` from the task dir. Should output the four section
headers (sqlc diff, go vet, go build, go test) with the tests in
`internal/rollup/...` passing.

## Notes

- The aggregator does not consult `gh_users` — none of the four count
  metrics is per-user. The Ghost-user filter that engineer_stats
  needs doesn't apply here.
- Deployment `status` is the empty string in v1 (per the deployments
  ingest doc); the rollup counts every row regardless. If we ever
  start populating `status` from `GET …/deployments/{id}/statuses`,
  this rollup can be revisited to filter to `status = "success"`.
- The 0035 cycle-time aggregator will UPSERT the same `(date,
  repo_id)` row but with its own column list in `ON CONFLICT DO
  UPDATE` — never with a leading DELETE. Both aggregators are free
  to run in either order under the scheduler's `for _, a := range
  s.aggregators` loop.
- If 0037's manual re-run lands while this aggregator is in
  production, the entrypoint stays `Aggregate(ctx, date)` — the
  scheduler already supports re-running a date (`RunDate`), so the
  re-aggregation hook is free.
