---
id: 0028
slug: reviews-ingest
title: Reviews/comments ingest
status: in_progress
depends_on: [0022, 0027]
owner: ""
est_minutes: 120
tags: [ingest]
autonomy: full
skills: [systematic-debugging]
---

## Goal

Light up the second ingest runner: PR reviews, inline review comments, and
issue (conversation) comments — wiring the per-PR fetcher from task 0022
(`internal/github/prconvo`) into the worker scheduler.

This runner is **per-PR** (unlike `prs`, which is per-repo): each tick walks
the PRs that GitHub has touched since our cursor, and for each one pages
three GraphQL connections — `reviews`, `reviewThreads.comments`,
`pullRequest.comments` — upserting rows into `pr_reviews`,
`pr_review_comments`, and `pr_issue_comments`. Authors are interned into
`gh_users`.

Architecture (matches spec lines 124–149 — Ingest strategy):

- **One runner instance per process**, shared across all connections.
  Stateless beyond `*sqlitedb.Queries` + `*zap.Logger`. The per-tick
  `*github.Client` is passed in by the scheduler.
- **Per-connection iteration:**
  1. `ListReposByConnection(conn.ID)` — non-archived repos. Empty → no-op.
  2. For each repo: load cursor `prconvo:<owner>/<name>` from `sync_cursors`.
     If absent, `since = conn.BackfillFrom`. If present, parse the RFC3339Nano
     timestamp.
  3. `ListPullRequestsByRepoUpdatedSince(repo.ID, since)` — PRs whose
     `updated_at > since` (new query, see Schema change below). Empty → 0 calls
     for this repo.
  4. For each PR (sorted by `updated_at` ASC):
     - **FetchReviews** loop: page until `!HasNext`. For each review, upsert
       the reviewer into `gh_users` (Ghost → `0`, same convention as 0027),
       then `UpsertPullRequestReview`. Skip reviews with `SubmittedAt == nil`
       (PENDING reviews that haven't been submitted — the column is NOT NULL).
     - **FetchReviewComments** loop: page until `!HasNext`. For each comment,
       upsert author + `UpsertPullRequestReviewComment`. If
       `page.Truncated == true`, log a warning (`zap.Warn`) and continue —
       v1 accepts that PRs with >100 comments in a single thread will drop
       overflow; rare in practice.
     - **FetchIssueComments** loop: page until `!HasNext`. For each comment,
       upsert author + `UpsertPullRequestIssueComment`.
     - If all three succeed, track `maxUpdated = max(maxUpdated, pr.updated_at)`.
       (`pr.updated_at` is the DB row — it's what bumps when GitHub touches a
       sub-resource, so our cursor correctly captures "everything up to this
       PR's UpdatedAt has been synced".)
     - Increment `items` by `len(reviews) + len(review_comments) + len(issue_comments)`.
  5. After all PRs for this repo succeed: if `maxUpdated` advanced, write
     `UpsertSyncCursor(connection_id, "prconvo:<owner>/<name>",
     maxUpdated.UTC().Format(time.RFC3339Nano), now)`.
- **Per-repo failure isolation**: a failure on one repo logs a warning and
  continues to the next repo. First error is wrapped with `owner/name` and
  returned at the end. Cursors for failed repos are NOT advanced. Within a
  repo, a single failing PR stalls that repo for this tick (mirrors the
  PR runner's mid-page failure behavior — keeps writes idempotent).
- **Rate-limit reporting**: end of `Run`, query `gh.GraphQLRemaining()` once.

The **cursor convention**: per-repo, key `prconvo:<owner>/<name>`. A single
key covers all three sub-resources because they're always co-fetched per PR;
advancing them atomically avoids re-fetching reviews when only issue comments
landed last tick. (Same per-repo / per-resource pattern 0027 established.)

### Why we need a schema change

`pull_requests` currently has no `updated_at` column — task 0027 stores
`created_at`, `merged_at`, `closed_at` but the GitHub `updatedAt` from the
fetcher's `prs.PR.UpdatedAt` was dropped on the floor. Reviews/comments
ingest needs an incremental "which PRs changed since X" filter, and
`updated_at` is the natural column.

Alternative considered: a per-PR `sync_cursors` row (e.g.
`prconvo:<owner>/<name>#<number>`). Rejected — would explode the table size
to one row per PR (potentially millions), and `pull_requests.updated_at` is
intrinsically useful for the rollup phase (0033–0036) anyway.

So this task edits the existing `migrations/0002_raw_events.sql` to add the
column + index to the `pull_requests` table (pre-launch — no shipped data,
no need for an additive migration), updates the `UpsertPullRequest` query
and the prs runner to populate it, and adds a new
`ListPullRequestsByRepoUpdatedSince` query.

Spec refs:

- `docs/superpowers/specs/2026-05-08-tempo-design.md` Ingest strategy
  (lines 124–149), Sync state (114–117), Raw events tables (97–105).
- Plan row 154: deps `0022, 0027`, autonomy `full`, skill
  `systematic-debugging`.

## Acceptance criteria

- [ ] `migrations/0002_raw_events.sql` — `pull_requests` table grows an
      `updated_at TIMESTAMP NOT NULL` column (placed between `created_at`
      and `merged_at`), plus index `pull_requests_repo_updated_idx(repo_id,
      updated_at)`. Edit in place — no new migration file (pre-launch).
- [ ] `internal/storage/sqlite/queries/pull_requests.sql` — `UpsertPullRequest`
      accepts a new `@updated_at` parameter (INSERT + `excluded.updated_at` in
      conflict). New `ListPullRequestsByRepoUpdatedSince(repo_id, since)`
      returns PRs with `updated_at > @since` ordered by `updated_at ASC`.
- [ ] `internal/ingest/prs/runner.go` — `upsertPR` populates the new
      `UpdatedAt: pr.UpdatedAt` field on `UpsertPullRequestParams`. Existing
      tests still pass (cassettes already provide `updatedAt`).
- [ ] `internal/ingest/prconvo/runner.go` defines `Runner` (with
      `*sqlitedb.Queries`, `*zap.Logger`, optional clock), `New(...) *Runner`,
      `Name() string` returning `"prconvo"`, and
      `Run(ctx, conn, gh) (ingest.Outcome, error)` implementing the loop above.
- [ ] `internal/ingest/prconvo/run.go` exports an fx-friendly `Module`
      (same `fx.Annotate` shape as the prs runner).
- [ ] Cursor key format `prconvo:<owner>/<name>`. Cursor value RFC3339Nano UTC.
- [ ] Reviews with `SubmittedAt == nil` are skipped (PENDING reviews don't have
      a submission time and `pr_reviews.submitted_at` is NOT NULL).
- [ ] Ghost authors → `reviewer_gh_user_id = 0` (or `author_gh_user_id = 0`
      for comments). No `gh_users` row created.
- [ ] User/Bot/Mannequin authors get a `gh_users` row upserted with
      `last_seen_at = pr.UpdatedAt` (the parent PR's UpdatedAt — the runner
      doesn't have a per-review timestamp on hand, and using the PR's
      UpdatedAt keeps the field monotonic).
- [ ] Truncated review threads (`prconvo.ReviewCommentsPage.Truncated == true`)
      log a `zap.Warn` with `owner/name/pr_number` but do NOT fail the PR.
- [ ] When `ListPullRequestsByRepoUpdatedSince` returns zero PRs for a repo:
      no GraphQL calls made for that repo, no cursor write.
- [ ] On per-PR failure, the repo's cursor is NOT advanced; the first error
      is wrapped with `owner/name/#number` and returned. Within a Run, repos
      that succeeded before the failure DO advance their cursors.
- [ ] Hermetic tests in `internal/ingest/prconvo/` cover:
      - happy path: 2 PRs, reviews + both kinds of comments land; cursor
        advanced to max PR.UpdatedAt;
      - ghost author across all 3 sub-resources;
      - truncated thread logs warning but Run succeeds;
      - existing cursor honoured as `since` (older PRs filtered);
      - empty PR list (no HTTP calls, no cursor write);
      - one repo's PR fails mid-way: that repo's cursor not written, other
        repo's cursor written, error returned.
- [ ] `cmd/tempo/main.go` provides `prconvo.Module`; the scheduler logs
      `runners=2` at boot.
- [ ] `./verify.sh` exits 0 (vet, build, full `go test ./... -race`).

## Files to touch

- `migrations/0002_raw_events.sql` — add `updated_at` column + index to
  `pull_requests` (edit in place; no new migration file).
- `internal/storage/sqlite/queries/pull_requests.sql` — add `@updated_at`
  param to upsert, add `ListPullRequestsByRepoUpdatedSince`.
- `internal/storage/sqlite/sqlitedb/*.go` — regenerated by `make sqlc-generate`.
- `internal/ingest/prs/runner.go` — pass `UpdatedAt` into `UpsertPullRequest`.
- `internal/ingest/prconvo/doc.go` — package overview.
- `internal/ingest/prconvo/runner.go` — `Runner` type + `Run` method.
- `internal/ingest/prconvo/run.go` — fx provider for the `"ingest.runners"` group.
- `internal/ingest/prconvo/runner_test.go` — hermetic tests.
- `internal/ingest/prconvo/cassettes_gen_test.go` — `//go:build gen`-tagged
  cassette author (mirrors `internal/github/prconvo/cassettes_gen_test.go`).
- `internal/ingest/prconvo/testdata/*.json` — committed VCR cassettes.
- `cmd/tempo/main.go` — add `prconvo.Module`.

No changes needed to existing sqlc queries for `pr_reviews`,
`pr_review_comments`, `pr_issue_comments` — `UpsertPullRequestReview`,
`UpsertPullRequestReviewComment`, `UpsertPullRequestIssueComment` already
exist (added in 0009).

## Steps

1. **Extend schema + `UpsertPullRequest` + new list query.**
   Edit `migrations/0002_raw_events.sql`'s `pull_requests` CREATE TABLE:
   add `updated_at TIMESTAMP NOT NULL` between `created_at` and `merged_at`,
   plus `CREATE INDEX pull_requests_repo_updated_idx ON pull_requests(repo_id,
   updated_at);`. Edit in place — no additive file, no down change needed
   beyond mirroring the index in the existing DROP block.

   Edit `internal/storage/sqlite/queries/pull_requests.sql`:

   ```sql
   -- name: UpsertPullRequest :exec
   INSERT INTO pull_requests (
     repo_id, number, gh_id, author_gh_user_id, state, title,
     created_at, updated_at, merged_at, closed_at, additions, deletions,
     base_ref, head_ref, draft
   ) VALUES (
     @repo_id, @number, @gh_id, @author_gh_user_id, @state, @title,
     @created_at, @updated_at, @merged_at, @closed_at, @additions, @deletions,
     @base_ref, @head_ref, @draft
   )
   ON CONFLICT (repo_id, number) DO UPDATE SET
     gh_id = excluded.gh_id,
     author_gh_user_id = excluded.author_gh_user_id,
     state = excluded.state,
     title = excluded.title,
     updated_at = excluded.updated_at,
     merged_at = excluded.merged_at,
     closed_at = excluded.closed_at,
     additions = excluded.additions,
     deletions = excluded.deletions,
     base_ref = excluded.base_ref,
     head_ref = excluded.head_ref,
     draft = excluded.draft;

   -- name: ListPullRequestsByRepoUpdatedSince :many
   SELECT * FROM pull_requests
   WHERE repo_id = @repo_id AND updated_at > @since
   ORDER BY updated_at;
   ```

   Run `make sqlc-generate`. Update `internal/ingest/prs/runner.go`'s
   `upsertPR` to pass `UpdatedAt: pr.UpdatedAt`.
   Verify existing prs tests still pass: `go test ./internal/ingest/prs/... -race`.
   Note: editing migration 0002 in place is the pre-launch convention —
   the migrations tree is reapplied fresh on every dev/test run.
   Commit: `feat(storage): pull_requests.updated_at column`.

2. **Scaffold `internal/ingest/prconvo/` package.** Create `doc.go`,
   `run.go` (fx Module, copy shape from prs), `runner.go` skeleton
   (`Runner` struct, `New`, `Name() = "prconvo"`, empty `Run` returning
   zero outcome). `go build ./...` passes.
   Commit: `feat(ingest/prconvo): runner skeleton + fx wiring`.

3. **Implement single-PR happy path.** In `Run`:
   - List repos, for each: load cursor → list PRs → for each PR fetch
     reviews, then review_comments, then issue_comments.
   - Author the upsert + cursor logic.
   - Skip reviews with `SubmittedAt == nil`.

   Write `cassettes_gen_test.go` mirroring `internal/github/prconvo/cassettes_gen_test.go`:
   one cassette `happy_path.json` with three interactions for PR #101 — a
   reviews response (2 reviews: alice approved, ghost dismissed), a
   review_comments response (1 thread, 2 comments), and an issue_comments
   response (1 comment). Generate the cassette with
   `go test -tags=gen -run TestGen_Cassettes ./internal/ingest/prconvo/...`.

   Write `TestRun_HappyPath_SinglePR`: seed tenant, token, connection
   (BackfillFrom 2026-01-01), repo `karnstack/tempo`, and pre-seed one
   PR row with `updated_at = 2026-04-12T12:00:00Z`. Run, then assert:
   - 2 rows in `pr_reviews` (alice + ghost — ghost has reviewer=0);
   - 2 rows in `pr_review_comments`;
   - 1 row in `pr_issue_comments`;
   - `gh_users` contains alice + comment authors (NOT ghost);
   - cursor `prconvo:karnstack/tempo` = `2026-04-12T12:00:00Z`;
   - `Outcome.Items = 5`.
   Commit: `feat(ingest/prconvo): single-PR happy path + cursor`.

4. **Multi-PR + multi-repo iteration.** Author `multi_pr.json` with
   six interactions (two PRs × three queries each). Add
   `TestRun_TwoPRs_CursorAtMaxUpdated`: PR #101 updated_at
   2026-04-12, PR #102 updated_at 2026-04-13. Assert both PRs'
   sub-resources land and the cursor pins to `2026-04-13T...`.
   Commit: `feat(ingest/prconvo): multi-PR iteration`.

5. **Truncated review threads.** Author `truncated_thread.json`
   where the review_comments response has `Truncated: true`. Add
   `TestRun_TruncatedThread_LogsWarn_DoesNotFail`: use a
   `zaptest.NewLogger` with a `zaptest.WrapOptions` observer to
   assert one `Warn` was logged with the expected fields. Run
   succeeds.
   Commit: `test(ingest/prconvo): truncated thread warning`.

6. **Existing cursor honoured.** Add `TestRun_ExistingCursor_FiltersOlderPRs`:
   pre-seed cursor `prconvo:owner/name = 2026-04-12T00:00:00Z`. Seed two
   PR rows — #101 with updated_at 2026-04-10 (older, must be skipped),
   #102 with updated_at 2026-04-13. Cassette has only #102's three
   interactions. Run, assert only #102's data landed and cursor advanced.
   Commit: `test(ingest/prconvo): existing cursor honoured`.

7. **Empty cases.** Add `TestRun_NoRepos_Noop` and `TestRun_NoUpdatedPRs_Noop`
   (repo exists but cursor is newer than every PR's updated_at). Assert
   zero HTTP calls (use a bare client with no transport like the prs
   tests do), zero items, no cursor write.
   Commit: `test(ingest/prconvo): empty cases`.

8. **Per-repo failure isolation.** Author `repo_failure.json` cassette:
   six interactions for repo "aok" (PR #1 succeeds — 3 calls; PR #2 first
   call returns GraphQL `NOT_FOUND`). Seed two repos, "aok" and "zfail"
   (alphabetical order matters: aok runs first). Variant A: "aok" PR #2
   fails → assert aok's cursor NOT written, zfail's cursor advances after
   its own PRs land. Or simpler: seed only "aok" with two PRs (one OK,
   one failing) → assert that repo's cursor not advanced, error wraps
   `owner/name/#PR_number`.
   Commit: `feat(ingest/prconvo): isolate per-repo failures`.

9. **Wire into main.** Add `prconvo.Module` to `cmd/tempo/main.go`. Build,
   smoke. Logs should show `runners=2`.
   Commit: `feat(ingest): wire prconvo runner into main`.

10. **Run `./verify.sh`.** Expect `VERIFY OK`.

## Notes

- **Why `prconvo:` over three keys.** The three sub-resources (reviews,
  review_comments, issue_comments) are always co-fetched per PR. A single
  cursor advances them atomically and avoids the failure mode where reviews
  cursor races ahead of comments, causing stale comment data to look
  authoritative. The downside is that any of the three failing forces all
  three to re-fetch next tick — acceptable since upserts are idempotent.
- **Why `pr.updated_at` as the cursor, not review/comment timestamps.**
  GitHub's `pullRequest.updatedAt` is bumped whenever ANY sub-resource is
  touched (new review, new comment, label change, etc.). Using it as the
  reviews cursor means "I've processed every sub-resource on every PR up
  to updated_at <= X" — exactly the right invariant.
- **Why skip PENDING reviews.** GraphQL returns `submittedAt: null` for
  reviews the token's user has authored but not yet submitted. They're
  not real to anyone but the author. The `pr_reviews.submitted_at` column
  is NOT NULL so we can't store them anyway. We're not the reviewing token
  in practice (PATs ingest other people's reviews) but the GraphQL schema
  permits this and we should not panic.
- **Why VCR cassettes per test, not record-once.** Different tests need
  different conversation shapes (truncation, errors, varying review
  counts). The `gen`-tagged author writes them deterministically; CI
  replays.
- **Why prconvo is per-PR but prs is per-repo.** GitHub's GraphQL has a
  repo-scoped `repository.pullRequests(orderBy: UPDATED_AT)` query — that's
  what task 0021's fetcher uses. There is NO equivalent
  `repository.reviews` connection ordered by submission time; reviews live
  under `pullRequest.reviews`. So we have to iterate PRs, then fetch
  per-PR. This is the GraphQL shape forcing the per-PR strategy, not a
  design choice.
- **Why we set `last_seen_at` to `pr.UpdatedAt` for comment/review authors.**
  The runner has the PR's UpdatedAt but not the individual review/comment
  timestamp at the moment of UpsertGhUser. Using the PR's UpdatedAt is
  monotonic-enough: even if we miss the exact comment time by a few hours,
  the user definitely was "alive on GitHub" at the PR's UpdatedAt.
