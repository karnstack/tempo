---
id: 0009
slug: migration-raw-events
title: Migration 0002 — raw event tables
status: done
depends_on: [0008]
owner: ""
est_minutes: 30
tags: [storage, migration]
autonomy: full
skills: []
---

## Goal

Land the v1 raw-event migration (`migrations/0002_raw_events.sql` — single file with `-- +goose Up` / `-- +goose Down` sections) covering the spec's "Raw events (append-only, source of truth)" section: `commits`, `pull_requests`, `pr_reviews`, `pr_review_comments`, `pr_issue_comments`, `deployments`. After this task, `make migrate-up` against a DB that already has 0001 applied creates exactly these six tables, and `make migrate-down` (one step) drops them cleanly without touching the 0001 tables.

Spec reference: `docs/superpowers/specs/2026-05-08-tempo-design.md` lines 97–105.

## Acceptance criteria

- [ ] `migrations/0002_raw_events.sql` exists with `-- +goose Up` and `-- +goose Down` markers and creates the six tables plus their indices.
- [ ] **No foreign keys, no CHECK constraints, no cascades.** Cross-table refs (`repo_id`, `pr_repo_id`, `*_gh_user_id`) are plain `INTEGER NOT NULL` columns. Enum-like values (`state`, `status`) are plain `TEXT NOT NULL`; validation lives in Go.
- [ ] `commits` PK is composite `(repo_id, sha)` — same SHA can legitimately appear across forks/mirrors, so per-repo composite is the safe choice (deviation from spec's shorthand `sha PK`, justified in rationale).
- [ ] `pull_requests` PK is composite `(repo_id, number)`.
- [ ] `pr_reviews`, `pr_review_comments`, `pr_issue_comments`, `deployments` use `gh_id INTEGER PRIMARY KEY` (GitHub's id is globally unique per resource).
- [ ] Booleans (`pull_requests.draft`) stored as `INTEGER NOT NULL DEFAULT 0`.
- [ ] Indices for the rollup read-path queries:
  - `pull_requests`: unique `(repo_id, gh_id)`; `(author_gh_user_id, created_at)`; `(repo_id, merged_at)`; `(repo_id, state)`.
  - `commits`: `(repo_id, authored_at)`; `(author_gh_user_id, authored_at)`.
  - `pr_reviews`: `(pr_repo_id, pr_number)`; `(reviewer_gh_user_id, submitted_at)`.
  - `pr_review_comments`: `(pr_repo_id, pr_number)`; `(author_gh_user_id, created_at)`.
  - `pr_issue_comments`: `(pr_repo_id, pr_number)`; `(author_gh_user_id, created_at)`.
  - `deployments`: `(repo_id, created_at)`; `(repo_id, environment)`.
- [ ] `go run ./cmd/migrate up` applies cleanly on top of 0001 against an empty DB; all six new tables visible in `sqlite_master`, plus the seven from 0001.
- [ ] `go run ./cmd/migrate down` rolls back exactly one step (drops only the six 0002 tables; the seven 0001 tables remain).
- [ ] `down` then `up` again is idempotent (no errors).
- [ ] `go test ./internal/storage/...` still passes.
- [ ] `verify.sh` exits 0.

## Files to touch

- `migrations/0002_raw_events.sql` (new — single file, both Up and Down sections)
- `.plans/upnext/0009-migration-raw-events/verify.sh` (replace stub)

## Schema decisions (rationale up front)

Per the project's "no DB-level constraints" rule (see `feedback_db_constraints.md`): no FKs, no CHECK, no cascades. Cross-table refs are plain `INTEGER NOT NULL` columns; referential integrity, enum validation, and cascade-on-delete semantics live in Go.

- **`commits` PK**: spec writes `sha PK`, but a SHA can legitimately appear in multiple repos (forks, mirrors, vendored history). Using `sha` alone as PK would silently drop those rows on insert. We use composite `PRIMARY KEY (repo_id, sha)` instead. Rollup queries already filter by `repo_id`, so the composite key matches the access pattern.
- **`pull_requests` PK**: composite `(repo_id, number)` — explicit in the spec. `gh_id` (GitHub's global PR id) gets a separate unique index on `(repo_id, gh_id)` so we can also look up by GitHub id when ingesting webhook payloads.
- **`pr_reviews`/`pr_review_comments`/`pr_issue_comments`/`deployments` PK**: `gh_id INTEGER PRIMARY KEY` — these GitHub ids are globally unique per resource type and are the natural dedupe key during ingest. We carry `pr_repo_id` + `pr_number` (or `repo_id`) as plain columns for join-free filtering.
- **`state` / `status` columns**: plain `TEXT NOT NULL` (`open`/`closed`/`merged` for PRs; `approved`/`commented`/`changes_requested`/`dismissed` for reviews; deployment states like `success`/`failure`/`pending`). Validation via Go enum types in the ingest layer.
- **Counts** (`additions`, `deletions`): `INTEGER NOT NULL DEFAULT 0`. GitHub may not return values for very large diffs; we default to 0 and let Go decide whether to backfill.
- **Nullable timestamps**: `merged_at`, `closed_at` on PRs are nullable (open PRs have neither). Everything else is `NOT NULL` because the event being ingested necessarily has a timestamp.
- **Authors**: `author_gh_user_id INTEGER NOT NULL` everywhere. Ghost / deleted GitHub users still have a stable id; the ingest layer ensures a `gh_users` row exists before inserting events.
- **Indices target the rollup read-path**: every index listed above corresponds to a query in the daily-rollup worker (engineer stats, repo stats, review latency/load) or the ingest cursor logic. We index for those access patterns now to keep rollup queries fast on day one; we can drop or add as the workers land.

## Steps

### 1. Write the migration

Create `migrations/0002_raw_events.sql`:

```sql
-- +goose Up

CREATE TABLE commits (
  repo_id INTEGER NOT NULL,
  sha TEXT NOT NULL,
  author_gh_user_id INTEGER NOT NULL,
  committer_gh_user_id INTEGER NOT NULL,
  authored_at TIMESTAMP NOT NULL,
  additions INTEGER NOT NULL DEFAULT 0,
  deletions INTEGER NOT NULL DEFAULT 0,
  message TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (repo_id, sha)
);
CREATE INDEX commits_repo_authored_idx ON commits(repo_id, authored_at);
CREATE INDEX commits_author_authored_idx ON commits(author_gh_user_id, authored_at);

CREATE TABLE pull_requests (
  repo_id INTEGER NOT NULL,
  number INTEGER NOT NULL,
  gh_id INTEGER NOT NULL,
  author_gh_user_id INTEGER NOT NULL,
  state TEXT NOT NULL,
  title TEXT NOT NULL,
  created_at TIMESTAMP NOT NULL,
  merged_at TIMESTAMP,
  closed_at TIMESTAMP,
  additions INTEGER NOT NULL DEFAULT 0,
  deletions INTEGER NOT NULL DEFAULT 0,
  base_ref TEXT NOT NULL,
  head_ref TEXT NOT NULL,
  draft INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (repo_id, number)
);
CREATE UNIQUE INDEX pull_requests_repo_gh_uniq ON pull_requests(repo_id, gh_id);
CREATE INDEX pull_requests_author_created_idx ON pull_requests(author_gh_user_id, created_at);
CREATE INDEX pull_requests_repo_merged_idx ON pull_requests(repo_id, merged_at);
CREATE INDEX pull_requests_repo_state_idx ON pull_requests(repo_id, state);

CREATE TABLE pr_reviews (
  gh_id INTEGER PRIMARY KEY,
  pr_repo_id INTEGER NOT NULL,
  pr_number INTEGER NOT NULL,
  reviewer_gh_user_id INTEGER NOT NULL,
  state TEXT NOT NULL,
  submitted_at TIMESTAMP NOT NULL
);
CREATE INDEX pr_reviews_pr_idx ON pr_reviews(pr_repo_id, pr_number);
CREATE INDEX pr_reviews_reviewer_submitted_idx ON pr_reviews(reviewer_gh_user_id, submitted_at);

CREATE TABLE pr_review_comments (
  gh_id INTEGER PRIMARY KEY,
  pr_repo_id INTEGER NOT NULL,
  pr_number INTEGER NOT NULL,
  author_gh_user_id INTEGER NOT NULL,
  created_at TIMESTAMP NOT NULL
);
CREATE INDEX pr_review_comments_pr_idx ON pr_review_comments(pr_repo_id, pr_number);
CREATE INDEX pr_review_comments_author_created_idx ON pr_review_comments(author_gh_user_id, created_at);

CREATE TABLE pr_issue_comments (
  gh_id INTEGER PRIMARY KEY,
  pr_repo_id INTEGER NOT NULL,
  pr_number INTEGER NOT NULL,
  author_gh_user_id INTEGER NOT NULL,
  created_at TIMESTAMP NOT NULL
);
CREATE INDEX pr_issue_comments_pr_idx ON pr_issue_comments(pr_repo_id, pr_number);
CREATE INDEX pr_issue_comments_author_created_idx ON pr_issue_comments(author_gh_user_id, created_at);

CREATE TABLE deployments (
  gh_id INTEGER PRIMARY KEY,
  repo_id INTEGER NOT NULL,
  environment TEXT NOT NULL,
  ref TEXT NOT NULL,
  sha TEXT NOT NULL,
  status TEXT NOT NULL,
  created_at TIMESTAMP NOT NULL
);
CREATE INDEX deployments_repo_created_idx ON deployments(repo_id, created_at);
CREATE INDEX deployments_repo_env_idx ON deployments(repo_id, environment);

-- +goose Down

DROP TABLE IF EXISTS deployments;
DROP TABLE IF EXISTS pr_issue_comments;
DROP TABLE IF EXISTS pr_review_comments;
DROP TABLE IF EXISTS pr_reviews;
DROP TABLE IF EXISTS pull_requests;
DROP TABLE IF EXISTS commits;
```

Commit: `chore(migrations): 0002 raw event tables`.

### 2. Manually smoke-test

```sh
rm -f /tmp/tempo-0009.db
TEMPO_DB="sqlite:///tmp/tempo-0009.db" go run ./cmd/migrate up
sqlite3 /tmp/tempo-0009.db ".tables"
TEMPO_DB="sqlite:///tmp/tempo-0009.db" go run ./cmd/migrate down   # one step → drops 0002
sqlite3 /tmp/tempo-0009.db ".tables"   # 0001 tables still present
TEMPO_DB="sqlite:///tmp/tempo-0009.db" go run ./cmd/migrate up     # idempotent
```

If something looks wrong, fix the SQL — no commit yet.

### 3. Write `verify.sh`

Replace the stub with a real script that:
- spins up a fresh sqlite db,
- runs `go run ./cmd/migrate up` (applies both 0001 and 0002),
- checks the six 0002 tables exist,
- checks the six expected indices exist,
- asserts no FKs, no CHECK constraints (project rule),
- runs `go run ./cmd/migrate down` and asserts only the six 0002 tables disappear (0001 tables remain),
- re-runs `up` and re-checks (idempotency),
- runs `go test ./internal/storage/...`.

Commit: `chore(plans): 0009 verify.sh`.

### 4. Run `verify.sh`

`./.plans/upnext/0009-migration-raw-events/verify.sh` should exit 0.

### 5. Wrap-up

`/next-task`'s post-implement workflow handles `RESULT.md`, status flip to `done`, and the `git mv` to `completed/`.

## Notes

- Goose splits statements by `;`; no need for `StatementBegin/End` for plain `CREATE`s.
- One `migrate down` step drops *only* 0002 because goose tracks versions; we don't need to manually fence the 0001 tables.
- `commits.message` uses `DEFAULT ''` so the ingest layer can elide it for the lightweight default-branch poll and only fill it on demand.
- These tables are append-only from the app's perspective. We use `INSERT … ON CONFLICT DO UPDATE` upserts (handled in repo layer in 0012) keyed on the PKs above for re-poll idempotency.
- We deliberately do **not** index `commits(sha)` alone — the PK already covers `(repo_id, sha)` lookups, and we rarely need cross-repo SHA lookups.
