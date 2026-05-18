---
id: 0033
slug: engineer-stats-rollup
title: Engineer stats rollup
status: done
depends_on: [0032]
owner: ""
est_minutes: 60
tags: [rollup]
autonomy: full
skills: []
---

## Goal

Wire the first concrete `rollup.Aggregator` — engineer stats — into the
scheduler's `"rollup.aggregators"` fx value group. For a given local
date `D` in the scheduler's tz, it rebuilds `daily_engineer_stats`
rows for every non-archived repo by aggregating from
`commits`, `pull_requests`, `pr_reviews`, `pr_review_comments`, and
`pr_issue_comments`.

`daily_engineer_stats(date, repo_id, gh_user_id, …)` is keyed by
`(date, repo_id, gh_user_id)` and the column set (per the spec) is
`commits, prs_opened, prs_merged, reviews_given, comments, additions,
deletions`.

Attribution rules:

| Column          | Source                                            | Author column          | Time column   |
| --------------- | ------------------------------------------------- | ---------------------- | ------------- |
| `commits`       | `commits`                                         | `author_gh_user_id`    | `authored_at` |
| `prs_opened`    | `pull_requests`                                   | `author_gh_user_id`    | `created_at`  |
| `prs_merged`    | `pull_requests WHERE merged_at IS NOT NULL`       | `author_gh_user_id`    | `merged_at`   |
| `additions`     | `SUM(pull_requests.additions)` on merged day      | `author_gh_user_id`    | `merged_at`   |
| `deletions`     | `SUM(pull_requests.deletions)` on merged day      | `author_gh_user_id`    | `merged_at`   |
| `reviews_given` | `pr_reviews`                                      | `reviewer_gh_user_id`  | `submitted_at`|
| `comments`      | `pr_review_comments` + `pr_issue_comments`        | `author_gh_user_id`    | `created_at`  |

`gh_user_id = 0` (the Ghost sentinel from commits ingest — see
`internal/ingest/commits/runner.go:204`) is filtered out everywhere.
Archived repos are skipped.

The aggregator is **idempotent**: for each `(date, repo_id)` it
`DELETE`s any existing rows, then re-aggregates. The whole per-day
sweep runs in a single `sql.Tx` so a mid-run failure leaves the day's
slice unchanged. This is what makes 0037's manual re-run free.

Spec refs:

- `docs/superpowers/specs/2026-05-08-tempo-design.md` line 109 — column
  set for `daily_engineer_stats`.
- Master plan row 164 (Phase 6) — autonomy `full`, deps `0032`.

### Scope notes / non-goals

- **Only this aggregator.** Repo stats / cycle time / review latency
  are 0034–0036, each populating its own `daily_*` table behind the
  same `Aggregator` interface.
- **No retention pruning.** Old days stay forever.
- **No public re-run API.** That's 0037 — it'll reuse the same
  `Aggregate(ctx, date)` entrypoint, possibly via a range loop.
- **No additions/deletions from commits.** `commits.additions` /
  `commits.deletions` are written as 0 by the commits ingest
  (`runner.go:186-187`), so we attribute lines-changed to the PR's
  author on the day it merges. This matches what the dashboard wants
  ("X shipped 250+ / 50- today").
- **Use sqlc `WithTx`** for the per-day transaction. No new tx helper.
- **Single aggregating SQL.** One `INSERT INTO daily_engineer_stats
  ... SELECT ... ON CONFLICT DO UPDATE` query with CTEs that the
  driver's UPSERT path makes idempotent (and the leading `DELETE`
  inside the same tx makes truly idempotent — a user who had a row
  and now has zero activity should disappear).

## Acceptance criteria

- [ ] `internal/storage/sqlite/queries/repos.sql` adds
      `-- name: ListAllRepos :many` returning every row from `repos`
      ordered by `id`. (Cross-tenant; rollups are system-wide.)

- [ ] `internal/storage/sqlite/queries/daily_engineer_stats.sql` adds
      `-- name: AggregateEngineerStatsForDay :exec` — a single
      `INSERT INTO daily_engineer_stats (...) SELECT ... ON CONFLICT
      (date, repo_id, gh_user_id) DO UPDATE SET ...` parameterised by
      `@date_str, @repo_id, @from_ts, @to_ts`. CTEs aggregate each
      source table, a `users` CTE `UNION`s the user-id sets, and the
      final `SELECT` `LEFT JOIN`s the per-source CTEs onto it with
      `COALESCE(..., 0)`. Excludes `gh_user_id = 0` everywhere.

- [ ] `sqlc generate` succeeds and `sqlc diff` exits clean.

- [ ] `internal/rollup/engineerstats/aggregator.go` declares:

      ```go
      type Aggregator struct {
          q   *sqlitedb.Queries
          db  *sql.DB
          log *zap.Logger
      }

      func New(s storage.Storage, l *zap.Logger) *Aggregator
      func (a *Aggregator) Name() string                     // "engineer_stats"
      func (a *Aggregator) Aggregate(ctx context.Context, date time.Time) error
      ```

      `Aggregate` does, for each non-archived repo (in repo-id order):

      1. `BEGIN`
      2. `DeleteDailyEngineerStatsByDateRepo(date_str, repo_id)`
      3. `AggregateEngineerStatsForDay(date_str, repo_id, from_ts, to_ts)`
      4. `COMMIT` (rollback on either error)

      `date_str = date.Format("2006-01-02")` in the date's location.
      `from_ts = date` (it's already local-midnight from the scheduler).
      `to_ts = date.AddDate(0, 0, 1)`. The `*_at` columns are stored
      as UTC `TIMESTAMP`s; comparing them to local-midnight `time.Time`
      values works directly (Go normalises to UTC at the driver
      boundary).

      Per-repo errors don't short-circuit sibling repos — log + record
      first error and continue, mirroring the scheduler's
      aggregator-error policy.

- [ ] `internal/rollup/engineerstats/module.go` exports a `fx.Module`
      that provides `*Aggregator` into the `"rollup.aggregators"`
      value group, same shape as `internal/ingest/commits/run.go`.

- [ ] `cmd/tempo/main.go` includes `engineerstats.Module` (next to the
      ingest modules).

- [ ] `internal/rollup/engineerstats/aggregator_test.go` covers, with a
      real in-memory sqlite:
      - Empty repo set → no-op, no error.
      - Single repo, single user, all five source tables populated on
        the target day → one expected row.
      - Multi-user, multi-repo → correct partition by `(repo_id, user)`.
      - Ghost author (`gh_user_id = 0`) is filtered out.
      - Archived repo (`archived = 1`) is skipped.
      - Idempotency: running twice produces identical state; running
        after deleting source rows wipes the stale daily row.
      - Out-of-window rows (just before/just after the day boundary in
        tz) are excluded.

- [ ] `go test ./internal/rollup/...` and `go vet ./...` pass.

- [ ] `verify.sh` runs: `sqlc diff`, `go build ./...`, `go vet ./...`,
      `go test ./internal/rollup/...` and exits 0.

## Files to touch

- `internal/storage/sqlite/queries/repos.sql` — add `ListAllRepos`.
- `internal/storage/sqlite/queries/daily_engineer_stats.sql` — add
  `AggregateEngineerStatsForDay`.
- `internal/storage/sqlite/sqlitedb/repos.sql.go`,
  `internal/storage/sqlite/sqlitedb/daily_engineer_stats.sql.go` —
  regenerated by sqlc.
- `internal/rollup/engineerstats/aggregator.go` (new).
- `internal/rollup/engineerstats/module.go` (new).
- `internal/rollup/engineerstats/aggregator_test.go` (new).
- `cmd/tempo/main.go` — `engineerstats.Module` registered.
- `.plans/upnext/0033-engineer-stats-rollup/verify.sh`.

## Steps

### 1. sqlc queries

Append to `internal/storage/sqlite/queries/repos.sql`:

```sql
-- name: ListAllRepos :many
SELECT * FROM repos ORDER BY id;
```

Append to `internal/storage/sqlite/queries/daily_engineer_stats.sql`:

```sql
-- name: AggregateEngineerStatsForDay :exec
WITH
  commits_agg AS (
    SELECT author_gh_user_id AS uid, COUNT(*) AS n
    FROM commits
    WHERE repo_id = @repo_id
      AND author_gh_user_id != 0
      AND authored_at >= @from_ts AND authored_at < @to_ts
    GROUP BY author_gh_user_id
  ),
  prs_opened_agg AS (
    SELECT author_gh_user_id AS uid, COUNT(*) AS n
    FROM pull_requests
    WHERE repo_id = @repo_id
      AND author_gh_user_id != 0
      AND created_at >= @from_ts AND created_at < @to_ts
    GROUP BY author_gh_user_id
  ),
  prs_merged_agg AS (
    SELECT author_gh_user_id AS uid,
           COUNT(*)        AS n,
           SUM(additions)  AS adds,
           SUM(deletions)  AS dels
    FROM pull_requests
    WHERE repo_id = @repo_id
      AND merged_at IS NOT NULL
      AND author_gh_user_id != 0
      AND merged_at >= @from_ts AND merged_at < @to_ts
    GROUP BY author_gh_user_id
  ),
  reviews_agg AS (
    SELECT reviewer_gh_user_id AS uid, COUNT(*) AS n
    FROM pr_reviews
    WHERE pr_repo_id = @repo_id
      AND reviewer_gh_user_id != 0
      AND submitted_at >= @from_ts AND submitted_at < @to_ts
    GROUP BY reviewer_gh_user_id
  ),
  comments_agg AS (
    SELECT uid, SUM(n) AS n FROM (
      SELECT author_gh_user_id AS uid, COUNT(*) AS n
      FROM pr_review_comments
      WHERE pr_repo_id = @repo_id
        AND author_gh_user_id != 0
        AND created_at >= @from_ts AND created_at < @to_ts
      GROUP BY author_gh_user_id
      UNION ALL
      SELECT author_gh_user_id AS uid, COUNT(*) AS n
      FROM pr_issue_comments
      WHERE pr_repo_id = @repo_id
        AND author_gh_user_id != 0
        AND created_at >= @from_ts AND created_at < @to_ts
      GROUP BY author_gh_user_id
    ) GROUP BY uid
  ),
  users AS (
    SELECT uid FROM commits_agg
    UNION SELECT uid FROM prs_opened_agg
    UNION SELECT uid FROM prs_merged_agg
    UNION SELECT uid FROM reviews_agg
    UNION SELECT uid FROM comments_agg
  )
INSERT INTO daily_engineer_stats (
  date, repo_id, gh_user_id,
  commits, prs_opened, prs_merged, reviews_given, comments,
  additions, deletions
)
SELECT
  @date_str, @repo_id, u.uid,
  COALESCE(c.n, 0),
  COALESCE(po.n, 0),
  COALESCE(pm.n, 0),
  COALESCE(rv.n, 0),
  COALESCE(cm.n, 0),
  COALESCE(pm.adds, 0),
  COALESCE(pm.dels, 0)
FROM users u
LEFT JOIN commits_agg     c  ON c.uid  = u.uid
LEFT JOIN prs_opened_agg  po ON po.uid = u.uid
LEFT JOIN prs_merged_agg  pm ON pm.uid = u.uid
LEFT JOIN reviews_agg     rv ON rv.uid = u.uid
LEFT JOIN comments_agg    cm ON cm.uid = u.uid
ON CONFLICT (date, repo_id, gh_user_id) DO UPDATE SET
  commits       = excluded.commits,
  prs_opened    = excluded.prs_opened,
  prs_merged    = excluded.prs_merged,
  reviews_given = excluded.reviews_given,
  comments      = excluded.comments,
  additions     = excluded.additions,
  deletions     = excluded.deletions;
```

Run `make sqlc-generate`. Commit:
`feat(storage): engineer_stats aggregation query + ListAllRepos (#0033)`.

### 2. Aggregator skeleton + tests scaffolding

Create `internal/rollup/engineerstats/aggregator.go`:

```go
// Package engineerstats implements the rollup.Aggregator that rebuilds
// daily_engineer_stats from commits, PRs, reviews, and comments.
package engineerstats

import (
    "context"
    "database/sql"
    "errors"
    "fmt"
    "time"

    "github.com/karnstack/tempo/internal/storage"
    "github.com/karnstack/tempo/internal/storage/sqlite/sqlitedb"
    "go.uber.org/zap"
)

const aggregatorName = "engineer_stats"

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
            a.log.Warn("rollup/engineer_stats: repo failed",
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
    tx, err := a.db.BeginTx(ctx, nil)
    if err != nil {
        return fmt.Errorf("begin tx: %w", err)
    }
    defer func() { _ = tx.Rollback() }()

    qtx := a.q.WithTx(tx)
    if err := qtx.DeleteDailyEngineerStatsByDateRepo(ctx, sqlitedb.DeleteDailyEngineerStatsByDateRepoParams{
        Date: dateStr, RepoID: repoID,
    }); err != nil {
        return fmt.Errorf("delete existing: %w", err)
    }
    if err := qtx.AggregateEngineerStatsForDay(ctx, sqlitedb.AggregateEngineerStatsForDayParams{
        RepoID:  repoID,
        FromTs:  from,
        ToTs:    to,
        DateStr: dateStr,
    }); err != nil {
        return fmt.Errorf("aggregate: %w", err)
    }
    if err := tx.Commit(); err != nil {
        if errors.Is(err, sql.ErrTxDone) {
            return nil
        }
        return fmt.Errorf("commit: %w", err)
    }
    return nil
}
```

Commit: `feat(rollup): engineer_stats aggregator (#0033)`.

### 3. fx Module

Create `internal/rollup/engineerstats/module.go`:

```go
package engineerstats

import (
    "github.com/karnstack/tempo/internal/rollup"
    "go.uber.org/fx"
)

var Module = fx.Module("rollup.engineer_stats",
    fx.Provide(
        fx.Annotate(
            New,
            fx.As(new(rollup.Aggregator)),
            fx.ResultTags(`group:"rollup.aggregators"`),
        ),
    ),
)
```

Wire into `cmd/tempo/main.go` alongside the ingest modules:

```go
import (
    ...
    "github.com/karnstack/tempo/internal/rollup/engineerstats"
)
...
        engineerstats.Module,
```

Commit: `feat(rollup): wire engineer_stats into fx (#0033)`.

### 4. Tests

Create `internal/rollup/engineerstats/aggregator_test.go`. Use the
same in-memory sqlite + `migrations.Apply` pattern as
`internal/rollup/scheduler_test.go`. Seed via raw `db.Exec` or via
the generated `Upsert*` helpers — whichever reads cleanest. One table-
driven test with sub-tests for each case in the acceptance list.

Skeleton:

```go
package engineerstats_test

import (
    "context"
    "path/filepath"
    "testing"
    "time"

    "github.com/karnstack/tempo/internal/config"
    "github.com/karnstack/tempo/internal/rollup/engineerstats"
    "github.com/karnstack/tempo/internal/storage/sqlite"
    "github.com/karnstack/tempo/internal/storage/sqlite/sqlitedb"
    "github.com/karnstack/tempo/migrations"
    "go.uber.org/fx/fxtest"
    "go.uber.org/zap/zaptest"
)

func newStorage(t *testing.T) (*sqlite.Store, *sqlitedb.Queries) { /* ... */ }
// + seeders for tenants/repos/users/commits/PRs/reviews/comments.
```

Run `go test ./internal/rollup/engineerstats/...` to confirm.

Commit: `test(rollup): engineer_stats aggregator coverage (#0033)`.

### 5. Verify

Write `verify.sh` (see below). Run `./verify.sh` from the task dir.

## Notes

- The aggregator does NOT consult `gh_users` — every `*_gh_user_id`
  column already references a real `gh_users.id` (sentinel 0 aside),
  so we save a JOIN.
- The CTE pattern is unusual enough that we'll trust the test matrix
  rather than try to formally analyse the SQL.
- `time.Time` comparison against TIMESTAMP columns works because the
  modernc.org/sqlite driver stores them as RFC3339 UTC; Go's
  `time.Time.UnmarshalText` round-trip is the same path sqlc uses to
  hydrate the row, so `from_ts/to_ts` filters compose without manual
  formatting.
- If `gh_user_id = 0` ever needs to be re-attributed (e.g. an
  "anonymous" bucket on the dashboard), that's a UI decision, not a
  rollup decision.
