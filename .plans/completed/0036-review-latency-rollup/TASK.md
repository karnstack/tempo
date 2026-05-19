---
id: 0036
slug: review-latency-rollup
title: Review latency + load rollup
status: done
depends_on: [0033]
owner: ""
est_minutes: 60
tags: [rollup]
autonomy: full
skills: []
---

## Goal

Wire the fourth concrete `rollup.Aggregator` — review stats — into
the scheduler's `"rollup.aggregators"` fx value group. For a given
local date `D` in the scheduler's tz, it rebuilds **two** tables from
the raw `pr_reviews` + `pull_requests` data:

1. `daily_review_latency(date, repo_id,
   time_to_first_review_seconds_p50, p90, count)` — one row per
   (date, repo). The percentiles describe seconds-to-first-review
   over PRs whose **first non-self review** was submitted on `D`.

2. `daily_review_load(date, repo_id, reviewer_gh_user_id, reviews,
   response_minutes_p50)` — one row per (date, repo, reviewer).
   `reviews` is the count of reviews that reviewer submitted on `D`
   for this repo; `response_minutes_p50` is the p50 of
   `(submitted_at - PR.created_at)` in minutes over those reviews.

Both tables are populated by the same `Aggregator.Aggregate` pass so
they share a per-repo loop, per-repo error handling, and a per-repo
transaction (review_load needs DELETE+INSERT because it fans out per
reviewer; review_latency is a single UPSERT but joins the same tx for
atomic visibility).

Schema reminder
(`migrations/0003_daily_rollups.sql:32-50`,
`docs/superpowers/specs/2026-05-08-tempo-design.md:111-112`):

```
daily_review_latency(date, repo_id,
  time_to_first_review_seconds_p50,
  time_to_first_review_seconds_p90,
  count)                          PK (date, repo_id)

daily_review_load(date, repo_id, reviewer_gh_user_id,
  reviews, response_minutes_p50)  PK (date, repo_id, reviewer_gh_user_id)
```

Attribution rules:

| Output column                              | Source                                                                            | Bucket            |
| ------------------------------------------ | --------------------------------------------------------------------------------- | ----------------- |
| `time_to_first_review_seconds_p50/p90`     | per-PR `first_review.submitted_at - pr.created_at`, repo-scoped                   | first review's `submitted_at` falls in [from, to) |
| `count` (in review_latency)                | number of PRs first-reviewed in the window                                       | same              |
| `reviews` (in review_load)                 | count of reviews per reviewer                                                    | review's `submitted_at` falls in [from, to)       |
| `response_minutes_p50`                     | per-review `(submitted_at - pr.created_at) / 60`                                  | same              |

**Self-reviews and ghost reviewers are filtered.** A reviewer with
`reviewer_gh_user_id = 0` (the commits-ingest Ghost sentinel — see
`internal/ingest/commits/runner.go:186`) or
`reviewer_gh_user_id = pr.author_gh_user_id` is excluded from both
metrics. Self-reviews can technically happen on GitHub
(state="COMMENT" on your own PR) but they aren't what people mean by
"review latency" or "review load" — they skew low and they obscure
who's actually doing the reviewing.

Archived repos are skipped.

**Idempotency.** Review latency: single UPSERT per repo (counts +
percentiles all in the ON CONFLICT clause). Review load: per-repo
DELETE + INSERT inside a tx, same shape as 0033's engineerstats.
The DELETE is necessary because per-reviewer rows would leak stale
counts otherwise (a reviewer who reviewed on D, then their data was
re-classified into another window, would otherwise keep a stale row
at the old count).

Sibling-aggregator interaction: neither of these tables is shared
with another aggregator (0034/0035 only touch `daily_repo_stats`), so
no disjoint-columns contract applies here. Each aggregator owns its
own table(s) end-to-end.

### Scope notes / non-goals

- **One aggregator for two tables.** Master plan row 167 is a single
  task; we keep them in one package since they share the same source
  data, the same per-repo loop, and the same tx.
- **No "review response time from request_at".** The schema and the
  ingest only track `submitted_at`, not when a reviewer was first
  requested. response_minutes is relative to PR open, not relative
  to "you became a reviewer".
- **No retention pruning.**
- **No public re-run API.** 0037 owns that.

## Acceptance criteria

- [ ] `internal/storage/sqlite/queries/pr_reviews.sql` adds
      `-- name: ListFirstReviewLatenciesForRepo :many` — returns
      `(pr_created_at, first_review_at)` pairs for every PR in the
      repo whose earliest non-self-non-ghost review falls in the
      window. (If sqlc-sqlite chokes on the `GROUP BY ... HAVING
      MIN(...) BETWEEN`, fall back to a Go-resident `const querySQL`
      executed via `db.QueryContext` — same escape hatch as 0033.)
- [ ] `internal/storage/sqlite/queries/pr_reviews.sql` adds
      `-- name: ListReviewsForRepoBetween :many` — returns
      `(reviewer_gh_user_id, submitted_at, pr_created_at)` for every
      review submitted in the window in the repo, excluding ghost
      reviewers and self-reviews. Same fallback applies.
- [ ] `internal/storage/sqlite/queries/daily_review_load.sql` adds
      `-- name: DeleteDailyReviewLoadByDateRepo :exec`.
- [ ] `sqlc generate` succeeds and `sqlc diff` exits clean.
- [ ] `internal/rollup/reviewstats/aggregator.go` declares:

      ```go
      type Aggregator struct {
          db  *sql.DB
          q   *sqlitedb.Queries
          log *zap.Logger
      }

      func New(s storage.Storage, l *zap.Logger) *Aggregator
      func (*Aggregator) Name() string                       // "review_stats"
      func (a *Aggregator) Aggregate(ctx context.Context, date time.Time) error
      ```

      Per non-archived repo:

      1. Fetch first-review pairs and reviews via the two new
         queries.
      2. Compute latency seconds (in Go) → sort → nearest-rank
         p50/p90, plus count.
      3. Group reviews by reviewer; for each reviewer compute count
         + p50 of response_minutes.
      4. Open a tx. DELETE existing daily_review_load rows for
         (date, repo). For each reviewer with activity, INSERT one
         row. UPSERT the daily_review_latency row (count=0,
         p50/p90=NULL when there were no first-reviews on D). Commit.

      Per-repo errors don't short-circuit sibling repos.

- [ ] `internal/rollup/reviewstats/module.go` exports a `fx.Module`
      that provides `*Aggregator` into the `"rollup.aggregators"`
      value group.
- [ ] `cmd/tempo/main.go` includes `reviewstats.Module`.
- [ ] `internal/rollup/reviewstats/aggregator_test.go` covers, with
      a real on-disk sqlite via the same harness as the sibling
      packages:
      - Empty database → no error, no rows in either table.
      - Single first-reviewed PR → review_latency p50=p90=that
        latency, count=1; review_load has one reviewer row with
        reviews=1 and p50=that PR's response_minutes.
      - Multi-PR / multi-review percentile math, nearest-rank
        (4 first-reviews with seconds [60, 120, 180, 240] →
        review_latency p50=sorted[1]=120, p90=sorted[3]=240,
        count=4).
      - Self-review excluded — a PR whose only review is by the
        author contributes nothing to either table.
      - Ghost reviewer (`reviewer_gh_user_id=0`) excluded from both.
      - Window boundaries: first reviews and per-day reviews at
        before/at-start/at-end/after-day points; only at-start and
        at-end count.
      - Multi-repo partitioning.
      - Archived repo skipped (neither table writes a row).
      - Multi-reviewer review_load: two reviewers in same repo,
        different counts, both correctly bucketed.
      - Idempotent rerun + stale review_load row cleared: seed two
        reviews by reviewer A on D, run, delete one, rerun → row
        stays with reviews=1; delete the other → row disappears
        (DELETE-then-INSERT erases the now-orphan reviewer key).
      - "first review" must be the chronologically earliest review
        across reviewers, not per reviewer — i.e. a PR with two
        reviews on D (Alice first, then Bob) counts once in
        review_latency, anchored to Alice's submitted_at.
- [ ] `go test ./internal/rollup/...` and `go vet ./...` pass.
- [ ] `verify.sh` runs: `sqlc diff`, `go vet ./...`, `go build ./...`,
      `go test ./internal/rollup/...` and exits 0.

## Files to touch

- `internal/storage/sqlite/queries/pr_reviews.sql` — add two list
  queries.
- `internal/storage/sqlite/queries/daily_review_load.sql` — add
  `DeleteDailyReviewLoadByDateRepo`.
- Regenerated sqlc files.
- `internal/rollup/reviewstats/aggregator.go` (new).
- `internal/rollup/reviewstats/module.go` (new).
- `internal/rollup/reviewstats/aggregator_test.go` (new).
- `cmd/tempo/main.go` — `reviewstats.Module` registered.
- `.plans/upnext/0036-review-latency-rollup/verify.sh`.

## Steps

### 1. sqlc queries

Append to `internal/storage/sqlite/queries/pr_reviews.sql`:

```sql
-- name: ListFirstReviewLatenciesForRepo :many
--
-- For each PR in the repo, returns (pr_created_at, first_review_at)
-- when the earliest non-self non-ghost review falls in
-- [from_ts, to_ts). The aggregator computes latency in Go.
SELECT pr.created_at AS pr_created_at,
       MIN(r.submitted_at) AS first_review_at
FROM pull_requests pr
JOIN pr_reviews r
  ON r.pr_repo_id = pr.repo_id AND r.pr_number = pr.number
WHERE pr.repo_id = @repo_id
  AND r.reviewer_gh_user_id != 0
  AND r.reviewer_gh_user_id != pr.author_gh_user_id
GROUP BY pr.repo_id, pr.number
HAVING MIN(r.submitted_at) >= @from_ts AND MIN(r.submitted_at) < @to_ts;

-- name: ListReviewsForRepoBetween :many
--
-- Reviews submitted in [from_ts, to_ts) in the repo, joined to the
-- target PR so the aggregator can compute response_minutes. Excludes
-- ghost reviewers and self-reviews.
SELECT r.reviewer_gh_user_id AS reviewer_gh_user_id,
       r.submitted_at AS submitted_at,
       pr.created_at AS pr_created_at
FROM pr_reviews r
JOIN pull_requests pr
  ON pr.repo_id = r.pr_repo_id AND pr.number = r.pr_number
WHERE r.pr_repo_id = @repo_id
  AND r.reviewer_gh_user_id != 0
  AND r.reviewer_gh_user_id != pr.author_gh_user_id
  AND r.submitted_at >= @from_ts AND r.submitted_at < @to_ts;
```

Append to `internal/storage/sqlite/queries/daily_review_load.sql`:

```sql
-- name: DeleteDailyReviewLoadByDateRepo :exec
DELETE FROM daily_review_load WHERE date = @date AND repo_id = @repo_id;
```

Run `make sqlc-generate`. If sqlc-sqlite chokes on either join+HAVING
shape, port the offending query into a Go `const` in the aggregator
and run via `tx.QueryContext` / `db.QueryContext` with `?1..?n`
positional args.

Commit: `feat(storage): review stats source + cleanup queries (#0036)`.

### 2. Aggregator

Create `internal/rollup/reviewstats/aggregator.go`. Lift the
percentile helper from `internal/rollup/cycletime/aggregator.go`
(copy is fine — two callers don't justify a shared package yet) and
add the per-repo logic for both tables. Per repo:

```go
func (a *Aggregator) aggregateRepo(ctx context.Context, repoID int64, dateStr string, from, to time.Time) error {
    firstReviews, err := a.q.ListFirstReviewLatenciesForRepo(ctx, ...)
    if err != nil { ... }
    reviews, err := a.q.ListReviewsForRepoBetween(ctx, ...)
    if err != nil { ... }

    // Compute review_latency.
    latencies := ...    // []int64 seconds, sorted
    var p50, p90 *int64
    count := int64(len(latencies))
    if count > 0 { p50, p90 = percentile pointers ... }

    // Compute review_load per reviewer.
    type loadAgg struct { count int64; durations []int64 }
    perReviewer := map[int64]*loadAgg{}
    for _, r := range reviews { ... }

    // Tx.
    tx, _ := a.db.BeginTx(ctx, nil)
    qtx := a.q.WithTx(tx)
    qtx.DeleteDailyReviewLoadByDateRepo(...)
    for uid, agg := range perReviewer {
        sort agg.durations
        p50min := percentile(agg.durations, 50)
        qtx.UpsertDailyReviewLoad(... reviewer=uid, reviews=agg.count, response_minutes_p50=&p50min)
    }
    qtx.UpsertDailyReviewLatency(... p50, p90, count)
    tx.Commit()
}
```

Commit: `feat(rollup): review_stats aggregator (#0036)`.

### 3. fx Module

Create `internal/rollup/reviewstats/module.go` mirroring
`internal/rollup/cycletime/module.go`. Wire `reviewstats.Module` into
`cmd/tempo/main.go`.

Commit: `feat(rollup): fx wiring for review_stats (#0036)`.

### 4. Tests

Create `internal/rollup/reviewstats/aggregator_test.go`. New helpers:
- `seedReview(t, q, ghID, prRepoID, prNumber, reviewerUID, submittedAt)`.
- `seedPR` (lifted from repostats test).
- `seedRepo`, `newStorage` (same shape).
- `readLatency` / `readLoadRows` helpers.

See acceptance criteria for the test list. One func per case.

Commit: `test(rollup): review_stats aggregator coverage (#0036)`.

### 5. Verify

`./verify.sh` from the task dir. Same four-section format as 0034/0035.

## Notes

- Both new queries use `MIN(r.submitted_at)` (review_latency) or join
  to the PR via `(repo_id, number)` (review_load). sqlc-sqlite has
  parsed simpler JOINs before — this is more aggressive but worth
  trying before the Go-const fallback.
- Self-review filter is `r.reviewer_gh_user_id != pr.author_gh_user_id`
  applied at the SQL level. The aggregator doesn't re-check.
- Percentile uses the same nearest-rank definition as 0035 (and
  matches what the dashboard will render).
- The per-repo tx wraps the load DELETE + load INSERTs + the latency
  UPSERT so the (date, repo) slice is either fully old or fully new.
  Visibility under WAL is the contract that lets the dashboard read
  consistently mid-rollup.
- 0037's manual re-run hook stays free: `Aggregate(ctx, date)` per
  date, idempotent by DELETE+INSERT (load) and UPSERT (latency).
