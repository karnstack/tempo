---
id: 0027
slug: pr-ingest-cursors
title: PR ingest end-to-end with cursor persistence
status: done
depends_on: [0021, 0026]
owner: ""
est_minutes: 90
tags: [ingest]
autonomy: full
skills: [systematic-debugging]
---

## Goal

Light up the first real ingest runner: pull requests. Walk every repo
attached to a connection, page GitHub's GraphQL `repository.pullRequests`
ordered by `UPDATED_AT DESC` (via `internal/github/prs`, task 0021),
upsert authors into `gh_users`, upsert PRs into `pull_requests`, and
persist a per-repo high-water `updatedAt` into `sync_cursors` so the
next tick fetches only what changed.

This task introduces the **runner-package convention** that 0028–0030
follow: a sub-package under `internal/ingest/<resource>/` that exports
a `*Runner` implementing `ingest.Runner`, wired into the scheduler via
the `"ingest.runners"` fx value group.

Architecture (matches spec lines 124–149 — Ingest strategy):

- **One runner instance per process**, shared across all connections.
  Stateless beyond `*sqlitedb.Queries` + `*zap.Logger`. The per-tick
  `*github.Client` is passed in by the scheduler — it carries the
  per-connection PAT and the live rate limiter.
- **Per-connection iteration:**
  1. `ListReposByConnection(conn.ID)` — non-archived repos owned by
     this connection. Empty list → `Outcome{}` and no error (a brand
     new org connection has zero repos until 0025's enumerator runs).
  2. For each repo: load cursor `prs:<owner>/<name>` from
     `sync_cursors`. If absent, `since = conn.BackfillFrom`. If present,
     parse the stored RFC3339Nano timestamp.
  3. Page-loop with `prs.Fetcher.Fetch(ctx, owner, name, after, 100,
     since)`. Stop on `!HasNext || ReachedSince`. `after` advances via
     `page.EndCursor` between pages.
  4. For each PR on each page:
     - Resolve `author_gh_user_id`: for actors with `GHID != 0`
       (`User`/`Bot`/`Mannequin`), `UpsertGhUser` with
       `tenant_id=conn.TenantID, gh_id=author.GHID, login, last_seen_at=PR.UpdatedAt`
       and use the returned row's `id`. For `Type == "Ghost"` (deleted
       account, `author == null` in GraphQL), use `0` — the schema has
       no FK and downstream rollups treat `0` as "unknown actor".
     - `UpsertPullRequest` with the full row.
     - Track `maxUpdated = max(maxUpdated, pr.UpdatedAt)`.
     - Increment `items`.
  5. After the page loop completes successfully (page-loop or
     `ReachedSince`), if `maxUpdated` advanced, write the new cursor:
     `UpsertSyncCursor(connection_id, "prs:<owner>/<name>", maxUpdated.UTC().Format(time.RFC3339Nano), now)`.
- **Per-repo error handling**: a failure on one repo logs a warning and
  continues to the next repo. The first error is returned (wrapped) at
  the end so the scheduler records it on `sync_runs`. Items keep
  accumulating from successful repos; cursors for failed repos are NOT
  advanced (so the next tick re-tries from the same `since`).
- **Rate-limit reporting**: at the end of `Run`, query
  `gh.GraphQLRemaining()` once. The GraphQL limiter's state is
  monotonically decreasing within a tick (each successful call
  refreshes `remaining` from response headers), so the post-run value
  IS the worst-observed remaining for this runner.

The **cursor convention** (per-repo, not per-connection): one
`sync_cursors` row per `(connection_id, "prs:<owner>/<name>")`. v1
keeps repo identity in the resource string rather than overloading the
table — keeps the schema flat and avoids a second column. Reviews/
commits/deploys runners (0028–0030) use the same convention with their
own resource prefix (`reviews:`, `commits:`, `deploys:`).

Spec refs:

- `docs/superpowers/specs/2026-05-08-tempo-design.md` Ingest strategy
  (lines 124–149) and Sync state (114–117).
- Plan row 153: deps `0021, 0026`, autonomy `full`, skill
  `systematic-debugging`.

## Acceptance criteria

- [ ] `internal/github/limiter.go` exposes `(*Limiter).Remaining() (int, bool)`.
      `*github.Client` exposes `GraphQLRemaining() (int, bool)`. `ok=false`
      until the limiter has seen at least one response.
- [ ] `internal/ingest/prs/runner.go` defines `Runner` (with
      `*sqlitedb.Queries`, `*zap.Logger`, optional clock), `New(...) *Runner`,
      `Name() string` returning `"prs"`, and
      `Run(ctx, conn, gh) (ingest.Outcome, error)` implementing the loop above.
- [ ] `internal/ingest/prs/run.go` exports an fx-friendly `Module`
      (`fx.Provide(fx.Annotate(New, fx.As(new(ingest.Runner)),
      fx.ResultTags(\`group:"ingest.runners"\`)))`) that subsequent
      runners can copy.
- [ ] Cursor key format `prs:<owner>/<name>`. Cursor value RFC3339Nano UTC.
- [ ] When no repos exist for the connection: `Outcome{Items:0, RateLimitRemaining:nil}`,
      no error, no DB writes.
- [ ] Ghost authors (`author == null`) become `author_gh_user_id = 0`,
      no `gh_users` row created.
- [ ] User/Bot/Mannequin authors get a `gh_users` row upserted with
      `last_seen_at = pr.UpdatedAt`; the row's `id` becomes
      `author_gh_user_id`.
- [ ] On per-repo failure, the cursor for the failing repo is NOT advanced;
      cursors for already-successful repos in the same Run ARE advanced.
- [ ] `Run` returns the first per-repo error wrapped with the failing repo's
      `owner/name`, after attempting all repos.
- [ ] Hermetic tests in `internal/ingest/prs/` cover: happy single page,
      multi-page (cursor advances after both pages), since-cutoff (existing
      cursor → fetcher passes it as `since`), empty repo list, ghost author,
      multi-repo with one failing (cursor advanced for the success, not for
      the failure, error propagated).
- [ ] `cmd/tempo/main.go` provides the PR runner into the
      `"ingest.runners"` group; the scheduler picks it up automatically
      (logged "ingest scheduler started ... runners=1").
- [ ] `./verify.sh` exits 0 (vet, build, full `go test ./... -race`).

## Files to touch

- `internal/github/limiter.go` — add `Remaining()` method.
- `internal/github/limiter_test.go` — cover the new method.
- `internal/github/client.go` — add `GraphQLRemaining()` accessor.
- `internal/github/client_test.go` — cover the accessor.
- `internal/ingest/prs/doc.go` — package overview (mirrors prs/doc.go style).
- `internal/ingest/prs/runner.go` — the `Runner` type + Run method.
- `internal/ingest/prs/run.go` — fx provider for the `"ingest.runners"` group.
- `internal/ingest/prs/runner_test.go` — hermetic integration tests
  (real sqlite, replayed VCR cassettes).
- `internal/ingest/prs/testdata/list_two_pages.json` — 2-page cassette
  (the multi-page test uses this; single-page test reuses
  `../../../github/prs/testdata/list_page.json` via a relative path).
- `internal/ingest/prs/testdata/list_since_recent.json` — cassette for
  the since-cutoff test (PRs newer than cursor on first page,
  ReachedSince true).
- `internal/ingest/prs/testdata/list_repo_failure.json` — cassette
  returning a `NOT_FOUND` GraphQL error for one repo.
- `cmd/tempo/main.go` — provide the PR runner into the value group.

No new sqlc queries needed: `UpsertGhUser`, `UpsertPullRequest`,
`GetSyncCursor`, `UpsertSyncCursor`, `ListReposByConnection` already exist.

## Steps

1. **Expose `Remaining()` on `Limiter` + `GraphQLRemaining()` on `Client`.**
   Two ~5-line additions. Tests assert: `false` before any Update,
   `(n, true)` after Update.
   Commit: `feat(github): expose graphql limiter remaining`.

2. **Scaffold `internal/ingest/prs/` package: `doc.go`, `runner.go`
   skeleton (Name only), `run.go` fx provider.**
   Make it compile. Confirm `go vet ./internal/ingest/...` passes and
   the value-group annotation is well-formed by adding it to main and
   running `go run ./cmd/tempo` briefly (or just `go build ./...`).
   Commit: `feat(ingest/prs): runner skeleton + fx wiring`.

3. **Implement single-repo, single-page Run logic** — list repos, for
   each repo: load cursor → fetch one page → upsert authors + PRs →
   persist max-updated cursor.
   Add the hermetic test `TestRun_HappyPath_SinglePage` reusing
   `../../../github/prs/testdata/list_page.json`. Seed tenant +
   connection + one repo (`karnstack/tempo`); assert 4 `pull_requests`
   rows, 3 `gh_users` rows (alice/renovate[bot]/old-bob; ghost is
   skipped), one `sync_cursors` row with `cursor =
   "2026-04-12T15:30:00Z"`, `Outcome.Items == 4`,
   `Outcome.RateLimitRemaining == nil` (cassette has no rate-limit
   headers).
   Commit: `feat(ingest/prs): single-page run + cursor persist`.

4. **Add page-loop.** Drive `Fetch` until `!HasNext || ReachedSince`.
   Author `testdata/list_two_pages.json` (page 1: 2 PRs, hasNext=true,
   endCursor="X"; page 2: 2 more PRs, hasNext=false). Add
   `TestRun_MultiPage_AdvancesCursor` — 4 `pull_requests` rows total,
   cursor pinned to the maximum `updatedAt` across both pages.
   Commit: `feat(ingest/prs): page loop`.

5. **Honour pre-existing cursor.** Add `TestRun_ExistingCursor_PassedAsSince`:
   pre-seed a `sync_cursors` row with `cursor =
   "2026-04-11T00:00:00Z"`, then run against `testdata/list_since_recent.json`
   (a fixture whose request body has `"first": 100` and where one PR's
   updatedAt is earlier than the cursor → fetcher drops it via
   `ReachedSince`). Assert exactly the newer PRs landed and the cursor
   advanced to the newest seen.
   Commit: `test(ingest/prs): existing cursor honoured`.

6. **Empty repo list + ghost author edge cases.**
   - `TestRun_NoRepos_Noop` — connection with zero repos → no DB writes,
     no errors, `Outcome.Items == 0`.
   - `TestRun_GhostAuthor` — verify the existing happy-path test already
     covers this; otherwise add a focused assertion that the PR with
     `author == null` (PR #98 in the existing fixture) lands with
     `author_gh_user_id = 0` and no `gh_users` row created for it.
   Commit: `test(ingest/prs): empty repo + ghost author`.

7. **Per-repo failure isolation.** Add `testdata/list_repo_failure.json`
   (a GraphQL `NOT_FOUND` error fixture identical in shape to
   `internal/github/prs/testdata/list_graphql_error.json`). Add
   `TestRun_OneRepoFails_OthersAdvance` — two repos, first one OK
   (cassette = list_page.json), second errors. Assert: first repo's
   PRs landed + cursor advanced; second repo has no `sync_cursors`
   row; `Run` returned a non-nil error wrapping the second repo's
   `owner/name`; `Outcome.Items` reflects the first repo's count.
   Commit: `feat(ingest/prs): isolate per-repo failures`.

8. **Wire into main.** Add the PR runner module to `cmd/tempo/main.go`'s
   `fx.Provide(...)`. `go build ./...` and a manual smoke
   (`go run ./cmd/tempo`) — log line should now say `runners=1`. Stop
   with Ctrl-C.
   Commit: `feat(ingest): wire prs runner into main`.

9. **Run `./verify.sh`.** Expect `VERIFY OK`.

## Notes

- **Why per-repo cursors, not per-connection.** PR `updatedAt`
  high-water marks differ across repos (a quiet repo's cursor stays
  old; a noisy repo's cursor advances daily). A single per-connection
  cursor would force quiet repos to redo work or noisy repos to miss
  updates, depending on which way the merge went. Per-repo is the
  natural granularity since the GraphQL query is per-repo anyway.
- **Why RFC3339Nano cursor values.** SQLite stores these as TEXT.
  Nano precision matches what GitHub returns and avoids round-trip
  drift. UTC normalisation makes lexicographic compare equivalent to
  chronological — a future v2 query like `WHERE cursor < '2026-...'`
  works without parsing.
- **Why Ghost = author_gh_user_id 0.** No FK in v1 (per repo memory:
  no DB-level constraints). `0` is a safe sentinel because gh_id is
  always positive in GitHub. Rollups (0033) bucket it as "unknown
  actor" without special-casing nullable columns.
- **Why GraphQL `rateLimit` is NOT added to the query.** We could add
  a `rateLimit { remaining }` selection to `prs.listQuery` and read it
  per-page, but that touches existing fixtures and the prs package's
  Page shape. The Limiter already tracks `X-RateLimit-Remaining` from
  response headers (it pauses on near-empty buckets); we're just
  exposing a read accessor. Adds zero query cost.
- **Why per-repo-failure tolerance, but error returned at end.** A
  flaky single repo (deleted, perms change, GraphQL hiccup) should not
  prevent ingest of the connection's other repos. But the connection's
  sync_run still needs to surface failure so /sync/status (0044) shows
  it red. So: best-effort across repos, then return the first error
  out.
- **Test isolation.** Tests open a real sqlite under `t.TempDir()`,
  apply migrations, and use the existing helpers from
  `internal/ingest/scheduler_test.go` (we duplicate a slim version
  rather than exporting them — keeping `_test.go` helpers test-private
  matches the existing pattern).
