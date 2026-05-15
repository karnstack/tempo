# Result — 0027 PR ingest end-to-end with cursor persistence

## What changed

### `internal/github/`
- `limiter.go` — added `(*Limiter).Remaining() (int, bool)`. Returns
  `(_, false)` until the first `Update` seeds the bucket from response
  headers.
- `client.go` — added `Client.GraphQLRemaining() (int, bool)` thin
  delegate so the ingest scheduler can read the worst-observed GraphQL
  bucket headroom at the end of a tick.
- `limiter_test.go`, `client_test.go` — coverage for both accessors
  (unknown-before-first-call, post-call value).

### `internal/ingest/prs/` (new package)
- `doc.go` — package overview, runner-package convention, cursor
  convention, per-repo failure isolation policy.
- `runner.go` — `Runner` type with `*sqlitedb.Queries`, `*zap.Logger`,
  optional `WithClock` for deterministic cursor timestamps.
  - `Name() == "prs"`.
  - `Run` lists non-archived repos for the connection, builds a
    `ghprs.Fetcher`, iterates per-repo with `syncRepo`, returns
    `Outcome{Items, RateLimitRemaining}` (the latter sourced from
    `gh.GraphQLRemaining()` once at end).
  - `syncRepo` loads `sync_cursors` (RFC3339Nano UTC), pages
    `Fetch` until `!HasNext || ReachedSince`, upserts authors
    (Ghost → `0`; User/Bot/Mannequin → `gh_users` row with
    `last_seen_at = pr.UpdatedAt`), upserts PR rows, and writes
    `prs:<owner>/<name>` cursor at the max updatedAt seen.
  - Per-repo failures: log warning, continue; first error wrapped
    with `owner/name` and returned at end.
- `run.go` — `Module = fx.Module("ingest.prs", ...)` registering
  `*Runner` as `ingest.Runner` into the `"ingest.runners"` value group
  via `fx.Annotate(New, fx.As(new(ingest.Runner)),
  fx.ResultTags(`group:"ingest.runners"`))`. The runner-package
  convention 0028–0030 will copy.
- `runner_test.go` — six hermetic tests, all green:
  - `TestRun_HappyPath_SinglePage` — 4 PRs land, 3 gh_users (Ghost
    excluded), PR #98 has `author_gh_user_id = 0`, cursor =
    `2026-04-12T15:30:00Z`, `Outcome.RateLimitRemaining = nil`.
  - `TestRun_MultiPage_AdvancesCursor` — 4 PRs across 2 pages,
    cursor = `2026-04-15T10:00:00Z`.
  - `TestRun_NoRepos_Noop` — connection with zero repos, no DB
    writes, no HTTP calls (bare client survives because the early
    return short-circuits before the fetcher).
  - `TestRun_ExistingCursor_PassedAsSince` — pre-seed cursor
    `2026-04-11T00:00:00Z`, oldest PR (#48 at `2026-04-10`) is
    dropped via `ReachedSince`, cursor advances to newest kept.
  - `TestRun_OneRepoFails_OthersAdvance` — 2 repos, first OK +
    cursor advanced, second NOT_FOUND, error wraps owner/name with
    `*github.GraphQLError` reachable via `errors.As`,
    `Outcome.Items` reflects only the success.
- `testdata/list_single_page.json` — 4-PR coverage mirror of
  `internal/github/prs/testdata/list_page.json` but with
  `hasNextPage=false` (the existing fixture is shared with
  `TestFetch_Page` which still requires `hasNext=true`).
- `testdata/list_two_pages.json` — 2-page cassette (2 PRs each).
- `testdata/list_since_recent.json` — single-page cassette with one
  PR older than the seeded cursor.
- `testdata/list_repo_failure.json` — 2-interaction cassette: page
  for `karnstack/aok` (success), then NOT_FOUND for `karnstack/zfail`.

### `cmd/tempo/main.go`
- Added `prs.Module` to `fx.New(...)`. Smoke run logs `ingest scheduler
  started ... runners=1`.

## verify.sh output (last lines)

```
ok  	github.com/karnstack/tempo/internal/ingest	3.917s
ok  	github.com/karnstack/tempo/internal/ingest/prs	3.081s
ok  	github.com/karnstack/tempo/internal/logger	2.143s
?   	github.com/karnstack/tempo/internal/metrics	[no test files]
?   	github.com/karnstack/tempo/internal/rollup	[no test files]
ok  	github.com/karnstack/tempo/internal/secret	2.308s
?   	github.com/karnstack/tempo/internal/storage	[no test files]
?   	github.com/karnstack/tempo/internal/storage/postgres	[no test files]
ok  	github.com/karnstack/tempo/internal/storage/sqlite	4.070s
?   	github.com/karnstack/tempo/internal/storage/sqlite/sqlitedb	[no test files]
?   	github.com/karnstack/tempo/internal/version	[no test files]
?   	github.com/karnstack/tempo/internal/webui	[no test files]
?   	github.com/karnstack/tempo/migrations	[no test files]
VERIFY OK
```

## Followups

- 0028 (reviews ingest), 0029 (commits ingest), 0030 (deploys ingest)
  should copy `internal/ingest/prs/run.go` verbatim, swapping the
  package import. Each gets its own `<resource>:<owner>/<name>` cursor
  prefix.
- `cmd/tempo` smoke run logs a "no such table: connections" error from
  the ticker because the local smoke DB hasn't been migrated. Not a
  regression of this task; resolve when migrations are wired into the
  binary's startup path (separate task).
- The "reuse `../../../github/prs/testdata/list_page.json`" guidance in
  the original TASK.md was written before the page loop existed; the
  fixture's `hasNextPage=true` made the loop walk off the cassette. The
  happy-path test now uses an owned `testdata/list_single_page.json`
  with the same 4-PR coverage but `hasNextPage=false`.
