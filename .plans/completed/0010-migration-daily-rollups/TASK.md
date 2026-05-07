---
id: 0010
slug: migration-daily-rollups
title: Migration 0003 — daily rollup tables
status: done
depends_on: [0009]
owner: ""
est_minutes: 25
tags: [storage, migration]
autonomy: full
skills: []
---

## Goal

Land the v1 daily-rollup migration (`migrations/0003_daily_rollups.sql` — single file, `-- +goose Up` / `-- +goose Down`) covering the spec's "Daily rollups (read path for the dashboard)" section: `daily_engineer_stats`, `daily_repo_stats`, `daily_review_latency`, `daily_review_load`.

These tables are the **only thing the dashboard reads from**. They are rewritten idempotently by the rollup worker every night (and on demand) from the raw events in 0002. Schema must support fast `(repo_id, date_range)` and `(gh_user_id, date_range)` scans.

Spec reference: `docs/superpowers/specs/2026-05-08-tempo-design.md` lines 107–112.

## Acceptance criteria

- [ ] `migrations/0003_daily_rollups.sql` exists with `-- +goose Up` and `-- +goose Down` markers and creates the four tables plus their indices.
- [ ] **No foreign keys, no CHECK constraints, no cascades.** Cross-table refs (`repo_id`, `gh_user_id`, `reviewer_gh_user_id`) are plain `INTEGER NOT NULL`.
- [ ] Composite PKs:
  - `daily_engineer_stats` → `(date, repo_id, gh_user_id)` (explicit in spec).
  - `daily_repo_stats` → `(date, repo_id)`.
  - `daily_review_latency` → `(date, repo_id)`.
  - `daily_review_load` → `(date, repo_id, reviewer_gh_user_id)`.
- [ ] `date TEXT NOT NULL` (SQLite-friendly `'YYYY-MM-DD'` strings; the rollup worker normalises to instance-local date in 0032).
- [ ] Counts: `INTEGER NOT NULL DEFAULT 0`. Percentile columns nullable (`NULL` when there is no signal that day).
- [ ] Secondary indices for the read-path queries:
  - `daily_engineer_stats(gh_user_id, date)` — engineer-profile time series.
  - `daily_engineer_stats(repo_id, date)` — repo-scoped engineer stats (already prefix-covered for `(date, repo_id)` lookups; this one inverts for repo-leaderboard scans).
  - `daily_repo_stats(repo_id, date)` — repo dashboard time series.
  - `daily_review_latency(repo_id, date)` — review-latency chart per repo.
  - `daily_review_load(reviewer_gh_user_id, date)` — review-load per engineer.
- [ ] `go run ./cmd/migrate up` applies cleanly on top of 0001+0002.
- [ ] `go run ./cmd/migrate down` (one step) drops only the 0003 tables; 0001 and 0002 tables remain.
- [ ] `down` then `up` is idempotent.
- [ ] `go test ./internal/storage/...` still passes.
- [ ] `verify.sh` exits 0.

## Files to touch

- `migrations/0003_daily_rollups.sql` (new — single file, both Up and Down)
- `.plans/upnext/0010-migration-daily-rollups/verify.sh` (replace stub)

## Schema decisions (rationale up front)

- **Why a single PK index isn't enough**: every rollup table's PK starts with `date`. Range queries the dashboard cares about are "repo X over the last 30 days" (`WHERE repo_id=? AND date BETWEEN ? AND ?`) and "engineer Y over the last 90 days" (`WHERE gh_user_id=? AND date BETWEEN ? AND ?`). Without a `(repo_id, date)` / `(gh_user_id, date)` index these become full scans. We add the inverted secondary indices up front; total table cardinality is `repos * days * engineers ≈ small` so the index overhead is fine.
- **Date as TEXT, not INTEGER unix epoch**: rollups are bucketed per **instance-local calendar date**, not per UTC instant. Storing `'2026-05-08'` makes `BETWEEN '2026-04-08' AND '2026-05-08'` a plain lexicographic comparison and keeps the SQL trivially debuggable. The rollup worker (0032) computes the bucket name in Go.
- **Percentiles nullable**: `lead_time_seconds_p50/p90`, `time_to_first_review_seconds_p50/p90`, `response_minutes_p50` are `INTEGER NULL`. A repo with zero merged PRs that day has `prs_merged = 0` and `lead_time_seconds_p50 = NULL` — surfaces as "no data" in the UI rather than a false zero.
- **`count` on `daily_review_latency`**: explicit denominator so the UI can show "p50 over 12 reviews" vs. "p50 over 1 review" — important for noisy days.
- **Idempotent rewrite**: the rollup worker uses `INSERT … ON CONFLICT (PK) DO UPDATE SET …` to overwrite a day's row when re-aggregating. The composite PKs above are exactly the dedupe keys.
- **No FK / CHECK** per project rule. Cascade-on-delete (e.g. when a connection is removed) is a Go-layer responsibility.

## Steps

### 1. Write the migration

Create `migrations/0003_daily_rollups.sql`:

```sql
-- +goose Up

CREATE TABLE daily_engineer_stats (
  date TEXT NOT NULL,
  repo_id INTEGER NOT NULL,
  gh_user_id INTEGER NOT NULL,
  commits INTEGER NOT NULL DEFAULT 0,
  prs_opened INTEGER NOT NULL DEFAULT 0,
  prs_merged INTEGER NOT NULL DEFAULT 0,
  reviews_given INTEGER NOT NULL DEFAULT 0,
  comments INTEGER NOT NULL DEFAULT 0,
  additions INTEGER NOT NULL DEFAULT 0,
  deletions INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (date, repo_id, gh_user_id)
);
CREATE INDEX daily_engineer_stats_user_date_idx ON daily_engineer_stats(gh_user_id, date);
CREATE INDEX daily_engineer_stats_repo_date_idx ON daily_engineer_stats(repo_id, date);

CREATE TABLE daily_repo_stats (
  date TEXT NOT NULL,
  repo_id INTEGER NOT NULL,
  prs_opened INTEGER NOT NULL DEFAULT 0,
  prs_merged INTEGER NOT NULL DEFAULT 0,
  prs_closed INTEGER NOT NULL DEFAULT 0,
  deploys INTEGER NOT NULL DEFAULT 0,
  lead_time_seconds_p50 INTEGER,
  lead_time_seconds_p90 INTEGER,
  PRIMARY KEY (date, repo_id)
);
CREATE INDEX daily_repo_stats_repo_date_idx ON daily_repo_stats(repo_id, date);

CREATE TABLE daily_review_latency (
  date TEXT NOT NULL,
  repo_id INTEGER NOT NULL,
  time_to_first_review_seconds_p50 INTEGER,
  time_to_first_review_seconds_p90 INTEGER,
  count INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (date, repo_id)
);
CREATE INDEX daily_review_latency_repo_date_idx ON daily_review_latency(repo_id, date);

CREATE TABLE daily_review_load (
  date TEXT NOT NULL,
  repo_id INTEGER NOT NULL,
  reviewer_gh_user_id INTEGER NOT NULL,
  reviews INTEGER NOT NULL DEFAULT 0,
  response_minutes_p50 INTEGER,
  PRIMARY KEY (date, repo_id, reviewer_gh_user_id)
);
CREATE INDEX daily_review_load_reviewer_date_idx ON daily_review_load(reviewer_gh_user_id, date);

-- +goose Down

DROP TABLE IF EXISTS daily_review_load;
DROP TABLE IF EXISTS daily_review_latency;
DROP TABLE IF EXISTS daily_repo_stats;
DROP TABLE IF EXISTS daily_engineer_stats;
```

Commit: `chore(migrations): 0003 daily rollup tables`.

### 2. Smoke test manually

```sh
rm -f /tmp/tempo-0010.db
TEMPO_DB="sqlite:///tmp/tempo-0010.db" go run ./cmd/migrate up
sqlite3 /tmp/tempo-0010.db ".tables"
TEMPO_DB="sqlite:///tmp/tempo-0010.db" go run ./cmd/migrate down
sqlite3 /tmp/tempo-0010.db ".tables"   # 0001 + 0002 still there
TEMPO_DB="sqlite:///tmp/tempo-0010.db" go run ./cmd/migrate up
```

### 3. Write `verify.sh`

Same shape as 0009: assert tables, indices, no-FK, no-CHECK, single-step down isolation, idempotency, storage tests.

Commit: `chore(plans): 0010 verify.sh`.

### 4. Run `verify.sh`

`./.plans/upnext/0010-migration-daily-rollups/verify.sh` exits 0.

## Notes

- `daily_review_load.response_minutes_p50` uses **minutes** (per spec) while the latency p50/p90 columns use **seconds**. This is deliberate — review-load is about responsiveness during a working day (minutes-resolution); lead time is about end-to-end ship velocity (seconds resolution / sub-day fidelity).
- We don't add a separate `(date)` index; SQLite uses the leftmost PK column for `BETWEEN date1 AND date2 AND repo_id=?` queries via the `(repo_id, date)` secondary index instead.
- The rollup worker (0032+) is responsible for `INSERT OR REPLACE`-style upserts; the migration just lays the schema.
