---
id: 0030
slug: deploys-ingest
title: Deployments ingest
status: done
depends_on: [0024, 0026]
owner: ""
est_minutes: 120
tags: [ingest]
autonomy: full
skills: [systematic-debugging]
---

## Goal

Light up the fourth ingest runner: GitHub Deployments per repo. Wires the
REST fetcher from task 0024 (`internal/github/deployments`) into the
worker scheduler with conditional `If-None-Match` ETag polling so 304
responses don't consume rate-limit budget. Each tick walks every
non-archived repo on the connection, sends one ETag'd
`GET /repos/{owner}/{repo}/deployments?per_page=100&page=1`, pages through
`Link: rel="next"` until either history is exhausted or it crosses the
cursor's `since` horizon (deploys come back newest-first), and upserts
`deployments` rows keyed on `gh_id`.

Architecture mirrors `internal/ingest/commits/`:

- **One runner instance per process**, shared across all connections.
  Stateless beyond `*sqlitedb.Queries` + `*zap.Logger`. The per-tick
  `*github.Client` is passed in by the scheduler.
- **Per-connection iteration:**
  1. `ListReposByConnection(conn.ID)` — non-archived repos. Empty → no-op.
  2. For each repo: load cursor `deployments:<owner>/<name>` from
     `sync_cursors`. Cursor value is composite
     `"<RFC3339Nano since>|<etag>"`. Split on the first `|` (etags can
     contain `"` and `W/` but never `|` per RFC 7232 etagc). Legacy
     bare-timestamp form parses as `(since=that, etag="")`. Missing row
     → `(conn.BackfillFrom, "")`.
  3. **Page 1 fetch** `FetchOptions{ETag: etag, PerPage: 100, Page: 1}`.
     No `SHA`/`Ref`/`Environment` filter — we want all deploys for this
     repo. Capture `page1Etag = page.ETag` for the cursor write.
  4. **304 NotModified**: no upserts, no further pages, no cursor write.
     `items = 0` for this repo. Continue to next repo.
  5. **200 OK**: process deploys (deploys are returned by GitHub in
     `created_at` DESC):
     - For each deploy `d` in the page:
       - If `d.CreatedAt <= since` → mark `sawOld = true`, skip
         (already seen on a previous poll).
       - Else: `UpsertDeployment{gh_id, repo_id, environment, ref, sha,
         status: "", created_at}`. `status = ""` because the list
         endpoint omits it (see `internal/github/deployments/doc.go`).
         Increment `items`. Track `maxCreated = max(maxCreated,
         d.CreatedAt)`.
     - If `sawOld` → break out of the paging loop (older pages have
       only-older deploys).
     - Else if `page.HasNext` → fetch the next page (no etag on
       `page > 1`) and repeat.
  6. After paging for this repo, write the cursor:
     - `newSince = since` (unchanged) if no new deploys landed
       (`maxCreated.IsZero()`); else `newSince = maxCreated`.
     - `newEtag = page1Etag` either way. Unlike commits, the URL has no
       `since=` query param, so the server-side etag is keyed on the
       same URL across polls — saving the freshest etag tightens the
       next poll's `If-None-Match`.
     - Cursor value = `newSince.UTC().Format(time.RFC3339Nano) + "|" +
       newEtag`. Call `UpsertSyncCursor(... resource =
       "deployments:<owner>/<name>", cursor = value, updated_at =
       r.now().UTC())`.
- **Per-repo failure isolation**: a failure on one repo logs a warning
  and continues to the next. First error is wrapped with `owner/name` and
  returned at the end. Cursors for failed repos are NOT advanced. Within
  a repo, a mid-page failure stalls that repo for this tick — page-1
  deploys we already upserted stay (idempotent on `gh_id`), and the
  cursor doesn't move so next tick re-fetches from the same `since`.
  Mirrors the prs/commits runner behaviour.
- **Rate-limit reporting**: end of `Run`, query `gh.RESTRemaining()`
  once (already added in 0029).

Cursor **convention**: per-repo, key `deployments:<owner>/<name>`.
Composite `since|etag` lets `(since, etag)` advance atomically.

Spec refs:

- `docs/superpowers/specs/2026-05-08-tempo-design.md` data model (line
  104): `deployments(gh_id PK, repo_id, environment, ref, sha, status,
  created_at)`. Ingest strategy (lines 124–149). Sync state (114–117).
- Plan row 156: deps `0024, 0026`, autonomy `full`, skill
  `systematic-debugging`.

### Scope decision: GitHub Deployments only (no Releases-as-deploys)

Spec line 104 says `deployments` is "sourced from GitHub Deployments +
Releases". This task implements GitHub Deployments only; Releases-as-
deploys is deferred to a follow-up task. Reasoning:

- Mapping a Release into a `deployments` row needs a design decision
  (what `environment`? `published_at` vs `created_at`? `tag_name` vs
  `target_commitish` for `ref`? `sha` not directly available — needs a
  ref→sha resolve). That decision lives in a fresh task with its own
  acceptance criteria, not bolted onto this one.
- `gh_id` is a single PK in the current schema. Mixing Deployment IDs
  and Release IDs in the same column could collide (separate GitHub ID
  spaces). Adding a `source` column + composite PK is an in-place
  migration edit (allowed pre-launch per CLAUDE memory) but expands
  scope.
- Most teams using tempo will either use the Deployments API directly
  or via a CI tool that posts to it. Releases-as-deploys is the
  fallback path; v1 is useful without it.
- Mirrors 0029's scope discipline: it deferred per-PR head-ref commits
  even though spec line 145 mentioned them, because the metric-bearing
  data was reachable via default-branch alone.

Documented in `doc.go` with the follow-up.

### Scope decision: list endpoint only, no per-deploy statuses

The list `/deployments` endpoint omits `status`; getting it needs a
separate `GET /deployments/{id}/statuses` per deploy. v1 stores `status
= ""` and the rollup (0034) counts deploy rows regardless of status.
Documented in `internal/github/deployments/doc.go`. Same surgical-scope
choice as commits' additions/deletions in 0029.

## Acceptance criteria

- [ ] `internal/ingest/deployments/runner.go` defines `Runner` (with
      `*sqlitedb.Queries`, `*zap.Logger`, optional clock), `New(...) *Runner`,
      `Name() string` returning `"deployments"`, and
      `Run(ctx, conn, gh) (ingest.Outcome, error)` implementing the loop above.
- [ ] `internal/ingest/deployments/run.go` exports an fx-friendly
      `Module` (same `fx.Annotate` shape as commits/prconvo/prs).
- [ ] `internal/ingest/deployments/doc.go` documents cursor format,
      advance rules, and the Releases-deferred + statuses-deferred
      scope decisions.
- [ ] Cursor key format `deployments:<owner>/<name>`. Cursor value
      `"<RFC3339Nano>|<etag>"`. Empty-etag form: `"<RFC3339Nano>|"`.
      Legacy single-component (no `|`) parses as `(since=that, etag="")`.
- [ ] On 304 NotModified for a repo: zero upserts, zero cursor writes
      for that repo. Items = 0 contribution.
- [ ] On 200 OK with 0 new deploys (empty body OR all `created_at <=
      since`): cursor written with `since` unchanged, `etag` refreshed
      from page-1 response.
- [ ] On 200 OK with N>0 new deploys across one or more pages: cursor
      written with `since = max(d.CreatedAt)` and `etag = page1.ETag`.
- [ ] Early-stop: when a page contains a deploy whose `created_at <=
      since`, the runner does NOT fetch subsequent pages (cassette
      without a page-2 interaction proves this — vcr replay fails on
      unmatched request).
- [ ] On per-page failure mid-repo: that repo's cursor NOT advanced;
      the first error is wrapped with `owner/name` and returned. Other
      repos that succeeded before the failure DO advance their cursors.
- [ ] When the connection has no repos: zero HTTP calls, zero cursor
      writes, `Outcome.Items = 0`, `Outcome.RateLimitRemaining = nil`.
- [ ] Hermetic tests in `internal/ingest/deployments/` cover:
      - happy path: 3 deploys (User creator + Bot creator + Ghost null
        creator); cursor advanced to max CreatedAt; etag pinned to
        page-1 etag; `Outcome.Items=3`;
        `*Outcome.RateLimitRemaining=4999`.
      - 304 NotModified: pre-seeded cursor with etag; cassette returns
        304; cursor unchanged after Run.
      - multi-page: page 1 (HasNext, all new) + page 2 (terminal, all
        new); cursor pinned to max CreatedAt across BOTH pages.
      - early-stop: page 1 has 2 new + 1 old (`created_at <= since`);
        page 2 NOT in cassette — Run must not fetch it. `Outcome.Items
        = 2`. Cursor at max(new).
      - 200 OK with empty body + new etag: cursor's since unchanged,
        etag refreshed.
      - legacy bare-timestamp cursor parsed correctly (reuses
        empty_response cassette).
      - empty repos for connection: no HTTP calls, no cursor writes.
      - per-repo failure isolation: two repos, "aok" succeeds + "zfail"
        returns 404 → aok's cursor advances, zfail's does not, first
        error wraps `karnstack/zfail`.
- [ ] `cmd/tempo/main.go` provides `deployments.Module`; the
      scheduler's boot log shows `runners=4`.
- [ ] `./verify.sh` exits 0 (sqlc diff, vet, build, focused tests,
      full `go test ./... -race`).

## Files to touch

- `internal/ingest/deployments/doc.go` — package overview, cursor
  rules, scope decisions.
- `internal/ingest/deployments/runner.go` — `Runner` + `Run` + helpers.
- `internal/ingest/deployments/run.go` — fx provider for the
  `"ingest.runners"` group.
- `internal/ingest/deployments/runner_test.go` — hermetic tests.
- `internal/ingest/deployments/cassettes_gen_test.go` —
  `//go:build gen` cassette author (mirrors commits/prconvo pattern).
- `internal/ingest/deployments/testdata/*.json` — committed VCR
  cassettes (6 files: happy_path, not_modified, multi_page, early_stop,
  empty_response, repo_failure).
- `cmd/tempo/main.go` — add `deployments.Module`.
- `.plans/upnext/0030-deploys-ingest/verify.sh` — verification script.

No schema, sqlc, or migration changes — `deployments` table,
`UpsertDeployment`, `ListReposByConnection`, `GetSyncCursor`,
`UpsertSyncCursor`, and `Client.RESTRemaining()` all exist already.

## Steps

1. **Scaffold `internal/ingest/deployments/` package.** Create:
   - `doc.go` — short package overview (per-repo REST deploys with
     ETag-based polling, since-driven early-stop, releases-deferred and
     statuses-deferred scope notes).
   - `run.go` — fx `Module` provider, copy shape from
     `internal/ingest/commits/run.go`.
   - `runner.go` — `Runner` struct, `New`, `Option` + `WithClock`,
     `Name() = "deployments"`, empty `Run` returning zero outcome.

   `go build ./...` + `go vet ./internal/ingest/deployments/...` pass.
   Commit: `feat(ingest/deployments): runner skeleton + fx wiring`.

2. **Implement single-repo happy path** (1 page, 3 deploys incl. ghost):
   - Implement the cursor parse helper (`strings.Cut(v, "|")`).
   - Implement `syncRepo`: load cursor, page-1 fetch, handle 304
     short-circuit, iterate deploys with upserts + since-filter, compute
     `maxCreated`, write cursor.
   - Write `cassettes_gen_test.go` mirroring
     `internal/ingest/commits/cassettes_gen_test.go`. First cassette
     `happy_path.json` — URL
     `https://api.github.com/repos/karnstack/tempo/deployments?page=1&per_page=100`,
     response 200 with `Etag: W/"dep-abc"`, `X-RateLimit-Remaining:
     4999`, no `Link` header (terminal). Body: 3 deploys in DESC
     created_at order, mixed creator shapes (alice@User, deploybot@Bot,
     null=Ghost). Generate with `go test -tags=gen -run
     TestGen_Cassettes ./internal/ingest/deployments/...`.
   - Write `TestRun_HappyPath_SingleRepo`: tenant + token + connection
     (BackfillFrom `2026-04-01T00:00:00Z`) + repo `karnstack/tempo`.
     Run. Assert:
     - 3 rows in `deployments` for `repo.ID`.
     - Cursor `deployments:karnstack/tempo` parses back to (since =
       max CreatedAt = `2026-04-12T10:00:00Z`, etag = `W/"dep-abc"`).
     - `Outcome.Items = 3`.
     - `*Outcome.RateLimitRemaining == 4999`.

   Commit: `feat(ingest/deployments): single-repo happy path + cursor`.

3. **304 NotModified.** Author `not_modified.json` — URL with
   `?page=1&per_page=100`, response status 304 with `Etag:
   W/"dep-def"`.
   Add `TestRun_NotModified_LeavesCursorUntouched`: pre-seed cursor
   `"2026-04-12T10:00:00Z|W/\"dep-abc\""`. Run. Assert:
   - 0 deployments rows, 0 new sync_cursors writes.
   - Cursor value unchanged after Run.
   - `Outcome.Items = 0`.

   Commit: `test(ingest/deployments): 304 leaves cursor untouched`.

4. **Multi-page.** Author `multi_page.json` — two interactions:
   - Page 1: URL `?page=1&per_page=100`, response 200, headers include
     `Link: <...page=2&per_page=100>; rel="next", <...page=3...>;
     rel="last"`, body 2 deploys (all `created_at` > `BackfillFrom`).
   - Page 2: URL `?page=2&per_page=100`, response 200, no `Link`
     rel="next", body 2 more deploys (also all newer than
     `BackfillFrom`, e.g. dates `2026-04-09T...` & `2026-04-08T...`).

   Add `TestRun_MultiPage_CursorAtMaxCreated`: empty cursor. Run.
   Assert 4 deploys upserted, cursor `since == max(created_at across
   both pages)`, etag = page1.ETag.

   Commit: `feat(ingest/deployments): multi-page pagination`.

5. **Early-stop.** Author `early_stop.json` — ONE interaction only
   (page 1 with `Link: rel="next"` but the runner must NOT fetch it):
   - URL `?page=1&per_page=100`, response 200 with Link header
     suggesting a page 2 exists, body 3 deploys in DESC order:
     `created_at = 2026-04-20T...`, `2026-04-18T...`, `2026-04-10T...`.

   Pre-seed cursor `"2026-04-15T00:00:00Z|W/\"dep-old\""`. So deploys
   on 4-20 and 4-18 are new; the 4-10 one is old → sawOld → break.

   Add `TestRun_EarlyStop_StopsBeforePage2`: Run. Assert:
   - `Outcome.Items = 2` (only the two new ones).
   - 2 rows in `deployments` (the 4-20 + 4-18 ones).
   - Cursor `since == 2026-04-20T...`, etag = page1.ETag.
   - VCR `tr.Done()` returns nil (no unconsumed interactions, no
     missed interactions — the runner did NOT try to fetch page 2).

   Commit: `feat(ingest/deployments): early-stop on cursor crossover`.

6. **Empty-response + legacy cursor edge cases.** Author
   `empty_response.json`: one interaction with `?page=1&per_page=100`,
   response 200, headers include `Etag: W/"dep-new"`, body `[]`.

   Add three tests:
   - `TestRun_NoRepos_Noop`: no repos seeded. Bare client with no
     transport (zero HTTP calls). Assert items=0, RateLimitRemaining
     nil, no cursor write.
   - `TestRun_EmptyResponse_RefreshesEtag`: pre-seed cursor
     `"2026-04-15T00:00:00Z|"` (empty etag). Run with
     `empty_response.json`. Assert cursor value after =
     `"2026-04-15T00:00:00Z|W/\"dep-new\""`, items=0.
   - `TestRun_LegacyCursor_ParsedAsBareSince`: pre-seed cursor
     `"2026-04-15T00:00:00Z"` (no `|`). Use the same cassette. Assert
     cursor after Run = `"2026-04-15T00:00:00Z|W/\"dep-new\""`.

   Commit: `test(ingest/deployments): empty + cursor format cases`.

7. **Per-repo failure isolation.** Author `repo_failure.json`: two
   interactions:
   - aok page 1: URL `repos/karnstack/aok/deployments?...`, response
     200 + 1 deploy.
   - zfail page 1: URL `repos/karnstack/zfail/deployments?...`,
     response 404 with body `{"message":"Not Found"}` (404 surfaces as
     `*github.HTTPError`).

   Seed two repos `karnstack/aok` and `karnstack/zfail` on the same
   connection. (Note: `ListReposByConnection` returns repos sorted by
   `(owner, name)` — "aok" < "zfail" alphabetically, so cassette order
   matches.)

   Add `TestRun_PerRepoFailureIsolation`: Run. Assert:
   - err != nil, err.Error() contains `karnstack/zfail`.
   - `Outcome.Items = 1` (only aok's deploy landed).
   - 1 row in `deployments` (aok's only).
   - Cursor `deployments:karnstack/aok` exists (advanced).
   - Cursor `deployments:karnstack/zfail` does NOT exist.

   Commit: `feat(ingest/deployments): isolate per-repo failures`.

8. **Wire into main.** Add `deployments.Module` to `cmd/tempo/main.go`
   (next to `commits.Module`). Build, optionally smoke `make dev` to
   confirm scheduler boots with `runners=4`.
   Commit: `feat(ingest): wire deployments runner into main`.

9. **Run `./verify.sh`.** Expect `VERIFY OK`.

## Notes

- **Why no `since=` query param.** GitHub's `/deployments` endpoint
  doesn't accept one. Our cursor's `since` is a client-side filter
  applied while iterating the (newest-first) results. The ETag handles
  "did anything change?"; the since handles "of what did change, what's
  new to us?". They compose cleanly.
- **Why early-stop on `created_at <= since`.** Without it, every poll
  walks all historical pages forever. With it, polls that surface 0 new
  deploys terminate after page 1 (the oldest deploy on page 1 is
  guaranteed to be <= since after the first successful run); polls with
  a few new deploys terminate as soon as the page boundary crosses the
  cursor. Cheap and correct because deploys come back DESC by
  created_at.
- **Why we always refresh the etag on 200 OK** (whether new deploys or
  not). The endpoint URL is identical across polls (no since=), so the
  server-side etag is stable per URL+state. Saving the freshest etag
  tightens the next poll's `If-None-Match` — a deploy event that
  changes server state will rotate the etag, and we want our copy to
  match. (Contrast with commits where since-advance rotates the URL
  and therefore invalidates the etag.)
- **Why `status = ""` for v1.** The list endpoint omits status. A
  detail call per deploy is wasteful; the rollup (0034) just counts
  rows. If a future task needs "successful deploys only", add a
  per-deploy statuses fetch and split the column. Documented in
  `internal/github/deployments/doc.go` already.
- **Why we don't intern `creator` into `gh_users`.** The `deployments`
  schema has no deployer column. Tracking the deployer is unscored in
  v1 metrics. Skipping the upsert keeps the runner surgically simple
  vs. commits (which DOES intern because its schema has
  `author_gh_user_id` + `committer_gh_user_id`).
- **Why composite `since|etag` even though `since` isn't sent.** Same
  atomicity argument as commits: one `sync_cursors` row holds both
  pieces so they advance together. Future improvements (e.g. tracking
  status_url-based etags per deploy) slot in by extending the value
  format. The unambiguous `|`-split parse rule continues to work.
- **Why GitHub Deployments only (defer Releases).** See "Scope
  decision" above. A separate task can design Release→deploy mapping
  cleanly without entangling this PR.
- **VCR cassettes per test.** Same rationale as commits: each test
  needs a specific conversation shape. The `gen`-tagged author writes
  them deterministically; CI replays via the no-tag build.
