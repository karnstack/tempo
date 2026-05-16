# 0030 Deployments ingest — RESULT

## Summary

Per-repo, REST-based GitHub-Deployments runner is live. Each tick walks
the connection's non-archived repos and issues a conditional
`GET /repos/{owner}/{repo}/deployments?page=1&per_page=100` with the
cached etag. Composite `since|etag` cursor lets the runner short-
circuit on 304 (no DB writes), early-stop paging the moment a page
contains a deploy with `created_at <= since` (deploys come back DESC),
and always refresh the etag on 200 because the URL has no `since=`
parameter (the server-side etag is stable per URL). Per-repo failure
isolation mirrors the prs/commits runners — failed repos don't advance,
succeeded ones do.

`runners=4` after wire-up: prs + prconvo + commits + deployments.

### Scope decisions documented in `doc.go`

- **GitHub Deployments only** — Releases-as-deploys deferred to a
  follow-up task (mapping needs its own design: environment, ref→sha
  resolve, gh_id collision risk between Deployment and Release ID
  spaces).
- **`status = ""` for v1** — list endpoint omits status; the per-deploy
  statuses call is a future enrichment.

## Files changed

- `internal/ingest/deployments/doc.go` — package doc, cursor rules,
  scope notes.
- `internal/ingest/deployments/run.go` — fx `Module` provider.
- `internal/ingest/deployments/runner.go` — `Runner`, `New`, `Name`,
  `Run`, `syncRepo`, `processPage`, `loadCursor`, `formatCursor`.
- `internal/ingest/deployments/runner_test.go` — seven tests (happy
  path, 304, multi-page, early-stop, no-repos noop, empty response
  refreshes etag, legacy cursor, per-repo failure isolation).
- `internal/ingest/deployments/cassettes_gen_test.go` — `gen`-tagged
  cassette author (6 cassettes).
- `internal/ingest/deployments/testdata/{happy_path,not_modified,multi_page,early_stop,empty_response,repo_failure}.json`.
- `cmd/tempo/main.go` — `deployments.Module` registered.
- `.plans/upnext/0030-deploys-ingest/verify.sh` — script.

## Verify output (last lines)

```
ok  	github.com/karnstack/tempo/internal/ingest	3.557s
ok  	github.com/karnstack/tempo/internal/ingest/commits	3.428s
ok  	github.com/karnstack/tempo/internal/ingest/deployments	3.619s
ok  	github.com/karnstack/tempo/internal/ingest/prconvo	3.538s
ok  	github.com/karnstack/tempo/internal/ingest/prs	3.140s
ok  	github.com/karnstack/tempo/internal/logger	2.222s
ok  	github.com/karnstack/tempo/internal/secret	1.469s
ok  	github.com/karnstack/tempo/internal/storage/sqlite	2.456s
VERIFY OK
```

## Follow-ups (not in scope)

- Releases-as-deploys mapping into the `deployments` table (separate
  task; needs schema decision on `source` column + composite PK to avoid
  Deployment-ID/Release-ID collisions).
- Per-deploy `statuses` enrichment if the rollup (0034) wants
  "successful deploys only" granularity.
- `sync_runs` row recording per repo (covered by 0031).
