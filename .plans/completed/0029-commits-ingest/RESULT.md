# 0029 Commits ingest — RESULT

## Summary

Per-repo, REST-based commits runner is live. Each tick walks the
connection's non-archived repos and issues a conditional
`GET .../commits?since=<cursor>&per_page=100&page=1` with the cached etag.
Composite `since|etag` cursor lets `(since, etag)` advance atomically;
304 short-circuits with no DB writes, 200 OK + 0 commits refreshes the
etag in place, 200 OK + N commits advances `since` to `max(authoredAt)`
and clears the etag. Per-repo failure isolation mirrors the prs/prconvo
runners — failed repos don't advance, succeeded ones do.

`runners=3` after wire-up: prs + prconvo + commits.

## Files changed

- `internal/github/client.go` — `Client.RESTRemaining()` method.
- `internal/ingest/commits/doc.go` — package doc.
- `internal/ingest/commits/run.go` — fx `Module` provider.
- `internal/ingest/commits/runner.go` — `Runner`, `New`, `Name`, `Run`,
  `syncRepo`, `processPage`, `upsertActor`, `loadCursor`,
  `formatCursor`.
- `internal/ingest/commits/runner_test.go` — six tests (happy path,
  304, multi-page, no-repos noop, empty response refreshes etag,
  legacy cursor, per-repo failure isolation).
- `internal/ingest/commits/cassettes_gen_test.go` — `gen`-tagged
  cassette author (5 cassettes).
- `internal/ingest/commits/testdata/{happy_path,not_modified,multi_page,empty_response,repo_failure}.json`.
- `cmd/tempo/main.go` — `commits.Module` registered.
- `.plans/upnext/0029-commits-ingest/verify.sh` — script.

## Verify output (last lines)

```
ok  	github.com/karnstack/tempo/internal/api/me	10.579s
ok  	github.com/karnstack/tempo/internal/api/tokens	18.854s
ok  	github.com/karnstack/tempo/internal/api/web	4.724s
ok  	github.com/karnstack/tempo/internal/auth	19.863s
ok  	github.com/karnstack/tempo/internal/config	4.516s
ok  	github.com/karnstack/tempo/internal/github	4.776s
ok  	github.com/karnstack/tempo/internal/github/commits	4.966s
ok  	github.com/karnstack/tempo/internal/github/deployments	5.078s
ok  	github.com/karnstack/tempo/internal/github/orgrepos	3.567s
ok  	github.com/karnstack/tempo/internal/github/prconvo	1.321s
ok  	github.com/karnstack/tempo/internal/github/prs	1.433s
ok  	github.com/karnstack/tempo/internal/github/releases	1.528s
ok  	github.com/karnstack/tempo/internal/github/vcr	1.622s
ok  	github.com/karnstack/tempo/internal/ingest	4.376s
ok  	github.com/karnstack/tempo/internal/ingest/commits	3.982s
ok  	github.com/karnstack/tempo/internal/ingest/prconvo	4.088s
ok  	github.com/karnstack/tempo/internal/ingest/prs	3.617s
ok  	github.com/karnstack/tempo/internal/logger	2.405s
ok  	github.com/karnstack/tempo/internal/secret	2.650s
ok  	github.com/karnstack/tempo/internal/storage/sqlite	2.709s
VERIFY OK
```

## Follow-ups (not in scope)

- Per-SHA detail call to populate `commits.additions/deletions` (the list
  endpoint omits them).
- Per-PR head-ref commit ingest, if cycle-time rollup (0035) needs
  branch-local commits beyond what reaches the default branch.
- `sync_runs` row recording per repo (covered by 0031).
