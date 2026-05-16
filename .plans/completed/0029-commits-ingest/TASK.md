---
id: 0029
slug: commits-ingest
title: Commits ingest
status: done
depends_on: [0023, 0026]
owner: ""
est_minutes: 120
tags: [ingest]
autonomy: full
skills: [systematic-debugging]
---

## Goal

Light up the third ingest runner: default-branch commits per repo. Wires the
REST fetcher from task 0023 (`internal/github/commits`) into the worker
scheduler, with `since=<RFC3339>` for incremental polling and conditional
`If-None-Match` ETag so 304 responses don't consume the rate-limit budget.

This runner is **per-repo** (like `prs`, unlike `prconvo`): each tick walks
every non-archived repo in the connection, sends one ETag'd `GET
/repos/{owner}/{repo}/commits?since=<cursor>&per_page=100&page=1`, and pages
through if the response carries a `Link: rel="next"`. Authors and committers
are interned into `gh_users`; commit rows are upserted into `commits`.

Architecture:

- **One runner instance per process**, shared across all connections.
  Stateless beyond `*sqlitedb.Queries` + `*zap.Logger`. The per-tick
  `*github.Client` is passed in by the scheduler.
- **Per-connection iteration:**
  1. `ListReposByConnection(conn.ID)` — non-archived repos. Empty → no-op.
  2. For each repo (alphabetical by `(owner, name)` from the query): load
     cursor `commits:<owner>/<name>` from `sync_cursors`.
     - Cursor value is composite: `"<RFC3339Nano since>|<etag>"`. Split on
       the first `|` (etag may contain `"` and `W/` but never `|` per RFC
       7232 etagc). If no `|` present (legacy / hand-seeded), the whole
       string is the since; etag is empty.
     - If absent: `since = conn.BackfillFrom`, `etag = ""`.
  3. **Page 1 fetch** with `FetchOptions{Since: since, ETag: etag,
     PerPage: 100}`. Capture `page1Etag = page.ETag` for later cursor
     write (only page 1's etag is meaningful — the fetcher silently drops
     etag for `page > 1`, see `internal/github/commits/doc.go`).
  4. **304 NotModified**: nothing changed. Don't write a cursor row. Don't
     fetch further pages. `items = 0` for this repo. Continue to next repo.
  5. **200 OK**: process commits. For each:
     - Upsert author into `gh_users` (Ghost → `0`, same convention as 0027/0028).
       `last_seen_at = c.AuthoredAt`.
     - Upsert committer into `gh_users` (Ghost → `0`). `last_seen_at =
       c.CommittedAt`. If author == committer (same `GHID`) we upsert twice
       harmlessly — `UpsertGhUser`'s `excluded.last_seen_at` overwrites
       deterministically; we don't dedup to keep the code one-liner-simple.
     - `UpsertCommit` with `repo_id = repo.ID`, `sha = c.SHA`,
       `authored_at = c.AuthoredAt`, `additions = 0`, `deletions = 0`,
       `message = c.Message`. (Additions/deletions stay 0 in v1 — the
       list-commits endpoint omits them; per-SHA detail call is a future
       follow-up. The commits table has no `committed_at` column per
       spec.)
     - Track `maxAuthored = max(maxAuthored, c.AuthoredAt)`.
     - Increment `items` by 1 per commit.
  6. While `page.HasNext`: fetch the next page (no etag on page>1) and
     repeat step 5 for its commits.
  7. After all pages succeed for this repo, write cursor:
     - If commits were upserted (`!maxAuthored.IsZero()`): `newSince =
       maxAuthored`, `newEtag = ""` (the old etag's URL keyed on the old
       `?since=` value; advancing since invalidates it).
     - Else (200 OK with 0 commits): `newSince = since` (unchanged), `newEtag
       = page1Etag` (refresh the cached etag for next poll's
       `If-None-Match`).
     - Either way, cursor value = `newSince.UTC().Format(time.RFC3339Nano)
       + "|" + newEtag`. Call `UpsertSyncCursor(... resource =
       "commits:<owner>/<name>", cursor = value, updated_at = r.now().UTC())`.
- **Per-repo failure isolation**: a failure on one repo logs a warning and
  continues to the next repo. First error is wrapped with `owner/name` and
  returned at the end. Cursors for failed repos are NOT advanced. Within a
  repo, a mid-page failure stalls that repo for this tick — but the
  page-1 commits we already upserted stay (idempotent), and the cursor
  doesn't move so next tick re-fetches from the same `since`. Mirrors the
  prs runner's mid-page failure behavior.
- **Rate-limit reporting**: end of `Run`, query `gh.RESTRemaining()` once.
  This method doesn't exist yet on `*github.Client` — Step 2 adds it as a
  three-line mirror of `GraphQLRemaining`.

Cursor **convention**: per-repo, key `commits:<owner>/<name>`. The
composite `since|etag` value lets `(since, etag)` advance atomically; if a
poll lands a new etag but no commits, we save the new etag against the same
`since` so the NEXT poll's `If-None-Match` is current.

Spec refs:

- `docs/superpowers/specs/2026-05-08-tempo-design.md` Ingest strategy
  (lines 124–149), Sync state (114–117), Raw events tables (97–105).
- Plan row 155: deps `0023, 0026`, autonomy `full`, skill
  `systematic-debugging`.

### Scope decision: default branch only (not per-PR head ref)

Spec line 145 mentions "Commits (per default branch + per PR head ref)" as
v1 scope. Per-PR head-ref commits are deferred — they multiply REST load by
N PRs and the metrics-relevant commits (the ones that ship) reach the
default branch eventually. Default branch only is the right size for this
task; the per-PR head-ref enrichment is a future follow-up if the cycle-time
rollup (0035) requires it.

The fetcher's `FetchOptions.SHA` is left empty — GitHub server-side defaults
to the repo's default branch, so we get the right commits without sending an
explicit `sha=<branch>` query param. Tests verify this by recording cassettes
without a `sha=` URL param.

## Acceptance criteria

- [ ] `internal/github/client.go` — new `Client.RESTRemaining() (int, bool)`
      method mirroring `GraphQLRemaining`; thin wrapper around `c.rest.Remaining()`.
- [ ] `internal/ingest/commits/runner.go` defines `Runner` (with
      `*sqlitedb.Queries`, `*zap.Logger`, optional clock), `New(...) *Runner`,
      `Name() string` returning `"commits"`, and
      `Run(ctx, conn, gh) (ingest.Outcome, error)` implementing the loop above.
- [ ] `internal/ingest/commits/run.go` exports an fx-friendly `Module`
      (same `fx.Annotate` shape as the prs/prconvo runners).
- [ ] Cursor key format `commits:<owner>/<name>`. Cursor value
      `"<RFC3339Nano>|<etag>"`. Empty-etag form: `"<RFC3339Nano>|"`.
      Legacy single-component (no `|`) parses as `(since=that, etag="")`.
- [ ] Ghost authors / committers → `0`. No `gh_users` row created.
      User/Bot/Mannequin actors get a row upserted with `last_seen_at =
      c.AuthoredAt` (author) or `c.CommittedAt` (committer).
- [ ] On 304 NotModified for a repo: zero upserts, zero cursor writes for
      that repo. Items = 0 contribution.
- [ ] On 200 OK with 0 commits: cursor written with `since` unchanged but
      `etag` refreshed from page-1 response.
- [ ] On 200 OK with N commits across one or more pages: cursor written
      with `since = max(c.AuthoredAt)` and `etag = ""` (since-advance
      invalidates old etag URL).
- [ ] On per-page failure mid-repo: that repo's cursor NOT advanced; the
      first error is wrapped with `owner/name` and returned. Other repos
      that succeeded before the failure DO advance their cursors.
- [ ] When the connection has no repos: zero HTTP calls, zero cursor
      writes, `Outcome.Items = 0`, `Outcome.RateLimitRemaining = nil`.
- [ ] Hermetic tests in `internal/ingest/commits/` cover:
      - happy path: 3 commits (User author + same-user committer; Bot
        author + User committer; Ghost author + Ghost committer); cursor
        advanced to max AuthoredAt; etag cleared; 2 gh_users rows.
      - 304 NotModified: pre-seeded cursor with etag; cassette returns
        304; cursor unchanged after Run.
      - multi-page: page 1 (HasNext) + page 2 (terminal); cursor pinned to
        max AuthoredAt across BOTH pages.
      - 200 OK with 0 commits + new etag: cursor's since unchanged, etag
        refreshed.
      - composite cursor parsed correctly: pre-seed `"<ts>|W/\"x\""`,
        assert the fetcher request URL includes that `since` (cassette
        URL match enforces this).
      - legacy bare-timestamp cursor parsed correctly.
      - empty repos for connection: no HTTP calls, no cursor writes.
      - per-repo failure isolation: two repos, "aok" succeeds + "zfail"
        returns 404 → aok's cursor advances, zfail's does not, first
        error wraps `karnstack/zfail`.
      - `RESTRemaining` plumbed through `Outcome.RateLimitRemaining` when
        the response carries `X-RateLimit-Remaining`.
- [ ] `cmd/tempo/main.go` provides `commits.Module`; the scheduler's boot
      log shows `runners=3`.
- [ ] `./verify.sh` exits 0 (sqlc diff, vet, build, focused tests,
      full `go test ./... -race`).

## Files to touch

- `internal/github/client.go` — add `RESTRemaining()`.
- `internal/ingest/commits/doc.go` — package overview.
- `internal/ingest/commits/runner.go` — `Runner` type + `Run` method.
- `internal/ingest/commits/run.go` — fx provider for the `"ingest.runners"` group.
- `internal/ingest/commits/runner_test.go` — hermetic tests.
- `internal/ingest/commits/cassettes_gen_test.go` — `//go:build gen`-tagged
  cassette author (mirrors the prconvo pattern).
- `internal/ingest/commits/testdata/*.json` — committed VCR cassettes.
- `cmd/tempo/main.go` — add `commits.Module`.
- `.plans/upnext/0029-commits-ingest/verify.sh` — verification script.

No schema or sqlc changes — `commits` table, `UpsertCommit`,
`UpsertGhUser`, `ListReposByConnection`, `GetSyncCursor`, `UpsertSyncCursor`
all exist (added in 0009/0012).

## Steps

1. **Add `Client.RESTRemaining()`.**
   In `internal/github/client.go`, just below `GraphQLRemaining`:

   ```go
   // RESTRemaining reports the most recent X-RateLimit-Remaining observed on
   // a REST response. ok=false until the first REST call has completed.
   func (c *Client) RESTRemaining() (int, bool) { return c.rest.Remaining() }
   ```

   `go build ./internal/github/...` + `go vet ./internal/github/...` pass.
   Commit: `feat(github): expose Client.RESTRemaining()`.

2. **Scaffold `internal/ingest/commits/` package.** Create:
   - `doc.go` — short package overview (default-branch commits, REST,
     `since=` + `If-None-Match`).
   - `run.go` — fx `Module` provider, copy shape from
     `internal/ingest/prconvo/run.go`.
   - `runner.go` — `Runner` struct, `New`, `Option` + `WithClock`,
     `Name() = "commits"`, empty `Run` returning zero outcome.

   `go build ./...` passes. Commit: `feat(ingest/commits): runner skeleton + fx wiring`.

3. **Implement single-repo happy path** (1 page, 3 commits incl. ghost):
   - Implement the cursor parse helper (`strings.Cut(v, "|")`).
   - Implement `syncRepo`: load cursor, page-1 fetch, handle 304 short-circuit,
     iterate commits with upserts, compute `maxAuthored`, write cursor.
   - Write `cassettes_gen_test.go` mirroring `internal/ingest/prconvo/cassettes_gen_test.go`.
     One cassette `happy_path.json` matching URL
     `https://api.github.com/repos/karnstack/tempo/commits?page=1&per_page=100&since=2026-04-01T00%3A00%3A00Z`,
     response 200 with `Etag: W/"abc123"`, `X-RateLimit-Remaining: 4999`, no
     `Link` header (terminal), body 3 commits: alice@User both sides (same
     authored/committed time), renovate[bot]@Bot author + alice@User
     committer (different times — rebase), Ghost/Ghost.
     Generate: `go test -tags=gen -run TestGen_Cassettes ./internal/ingest/commits/...`.
   - Write `TestRun_HappyPath_SingleRepo`: tenant + token + connection
     (BackfillFrom `2026-04-01T00:00:00Z`) + repo `karnstack/tempo`. Run.
     Assert:
     - 3 rows in `commits` (ghost commit has both `author=0` and `committer=0`).
     - 2 gh_users rows (alice + renovate[bot]; ghost author/committer skipped).
     - Cursor `commits:karnstack/tempo` parses back to (max AuthoredAt = `2026-04-12T10:00:00Z`, etag = `""`).
     - `Outcome.Items = 3`.
     - `Outcome.RateLimitRemaining != nil` and `*Outcome.RateLimitRemaining == 4999`.

   Commit: `feat(ingest/commits): single-repo happy path + cursor`.

4. **304 NotModified.** Author `not_modified.json`: one interaction matching
   URL `?since=2026-04-12T10%3A00%3A00Z` (the post-happy-path cursor's
   since), response status 304 with `Etag: W/"def456"`.
   Add `TestRun_NotModified_NoCursorWrite`: pre-seed cursor
   `"2026-04-12T10:00:00Z|W/\"abc123\""`. Run. Assert:
   - 0 new commits, 0 new gh_users.
   - Cursor value unchanged after Run.
   - `Outcome.Items = 0`.

   Commit: `test(ingest/commits): 304 not modified leaves cursor untouched`.

5. **Multi-page.** Author `multi_page.json`: two interactions.
   - Page 1: URL `?page=1&per_page=100&since=...`, response 200, headers
     include `Link: <...page=2&per_page=100>; rel="next", <...page=3...>; rel="last"`,
     body 2 commits.
   - Page 2: URL `?page=2&per_page=100&since=...`, response 200, no `Link`
     rel="next", body 2 commits.

   Add `TestRun_MultiPage_CursorAtMaxAuthored`: empty cursor. Run. Assert
   4 commits, cursor `since == max(authored across both pages)`, etag `""`.

   Commit: `feat(ingest/commits): multi-page pagination`.

6. **Empty + composite cursor edge cases.** Author `empty_response.json`:
   one interaction with `?since=2026-04-15T00%3A00%3A00Z`, response 200,
   headers include `Etag: W/"new-etag"`, body `[]`.
   Add three tests:
   - `TestRun_NoRepos_Noop`: no repos seeded. Bare client with no transport
     (zero HTTP calls). Assert items=0, RateLimitRemaining nil, no cursor write.
   - `TestRun_EmptyResponse_RefreshesEtag`: pre-seed cursor
     `"2026-04-15T00:00:00Z|"` (empty etag). Run with `empty_response.json`.
     Assert cursor value after = `"2026-04-15T00:00:00Z|W/\"new-etag\""`,
     items=0.
   - `TestRun_LegacyCursor_ParsedAsBareSince`: pre-seed cursor
     `"2026-04-15T00:00:00Z"` (no `|`). Use the same cassette. Assert the
     runner queried with that since (cassette match proves it), cursor
     after Run is `"2026-04-15T00:00:00Z|W/\"new-etag\""`.

   Commit: `test(ingest/commits): empty + cursor format cases`.

7. **Per-repo failure isolation.** Author `repo_failure.json`: two
   interactions.
   - aok page 1: URL `repos/karnstack/aok/commits?...`, response 200 + 1 commit.
   - zfail page 1: URL `repos/karnstack/zfail/commits?...`, response 404
     with body `{"message":"Not Found"}` (404 is not retried by the inner
     client, surfaces as `*github.HTTPError`).

   Seed two repos `karnstack/aok` and `karnstack/zfail` on the same
   connection. (Note: `ListReposByConnection` returns repos sorted by
   `(owner, name)` — "aok" < "zfail" alphabetically, so cassette order
   matches.)

   Add `TestRun_PerRepoFailureIsolation`: Run. Assert:
   - err != nil, err.Error() contains `karnstack/zfail`.
   - `Outcome.Items = 1` (only aok's commit landed).
   - 1 row in `commits` (aok's only).
   - Cursor `commits:karnstack/aok` exists (advanced).
   - Cursor `commits:karnstack/zfail` does NOT exist.

   Commit: `feat(ingest/commits): isolate per-repo failures`.

8. **Wire into main.** Add `commits.Module` to `cmd/tempo/main.go` (next
   to `prs.Module` / `prconvo.Module`). Build, optionally smoke `make dev`
   to confirm scheduler boots with `runners=3`.
   Commit: `feat(ingest): wire commits runner into main`.

9. **Run `./verify.sh`.** Expect `VERIFY OK`.

## Notes

- **Why composite `since|etag`.** The fetcher needs both inputs to perform
  conditional polling. A single `sync_cursors` row keeps the read/write
  atomic — there is no window where `since` is advanced but `etag` is stale
  or vice versa. The split-on-`|` parse is unambiguous because RFC 7232
  forbids `|` in etag content. We don't gain anything from a separate row
  per component.
- **Why clear the etag on `since`-advance.** Server-side ETags are keyed on
  the full URL including `?since=`. Once we move `since` forward, the old
  etag is meaningless for the new URL — saving it would mislead the next
  poll into sending an `If-None-Match` that never matches. Clearing is the
  conservative right thing.
- **Why preserve the etag on `200 OK + 0 commits`.** This is the "drift"
  case: server confirmed nothing new, but possibly bumped its internal
  etag (different hash because of header changes etc). Saving the new
  etag tightens the next poll's `If-None-Match` to the freshest server
  state, restoring 304-on-no-change.
- **Why default-branch only, not per-PR head ref.** See "Scope decision"
  above. The metric-bearing commits (merged work that ships) all surface
  on the default branch eventually. Per-PR head ref commits would 5×–20×
  REST load with marginal v1 value.
- **Why `AuthoredAt` (not `CommittedAt`) for the cursor.** GitHub's
  `since=` filter is keyed on author-date per the fetcher doc. So
  `max(authoredAt)` is what excludes already-seen commits next poll.
  `committed_at` is rebase-shifted and not used by the filter.
- **Why VCR cassettes per test.** Same rationale as prconvo: each test
  needs a specific conversation shape (304 vs 200+empty vs 200+N+next,
  ghosts present/absent, failure vs success). The `gen`-tagged author
  writes them deterministically; CI replays via the no-tag build.
- **Why we re-upsert gh_users for author+committer when they're the same.**
  Saves a per-commit branch and a comparison; the `UpsertGhUser` query is
  a single SQLite roundtrip per call. With typical repo cardinality (10s
  of distinct authors over months) this is noise. Avoiding the branch
  keeps the code shape symmetric with the prs/prconvo runners.

