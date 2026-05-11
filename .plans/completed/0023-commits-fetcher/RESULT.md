# 0023 — Commits fetcher (REST since cursor + ETag)

Adds `internal/github/commits` — a default-branch commits fetcher over
`GET /repos/{owner}/{repo}/commits` with `since=<RFC3339>` cursoring,
conditional `If-None-Match` ETag (304 → no rate-limit cost), and
page-number pagination derived from the `Link` response header. Stateless,
no DB. Hermetic VCR-replay tests via hand-authored cassettes gated behind
a `gen` build tag.

Per-commit additions/deletions are deliberately out of scope — the list
endpoint omits them; 0029 can compose `GET /repos/{owner}/{repo}/commits/{sha}`
if/when it needs them. Documented in `doc.go`.

## Files changed

- `internal/github/commits/doc.go` (new)
- `internal/github/commits/fetcher.go` (new — Fetcher, FetchOptions, Commit,
  Page, Link parser, parseActor reusing `prs.Author`)
- `internal/github/commits/fetcher_test.go` (new — three replay tests)
- `internal/github/commits/cassettes_gen_test.go` (new — `//go:build gen`)
- `internal/github/commits/testdata/list_page.json` (new — 200, three
  commits, mixed authors, Link header rel="next")
- `internal/github/commits/testdata/list_not_modified.json` (new — 304,
  no body, no ETag header → exercises caller-echo path)
- `internal/github/commits/testdata/list_http_error.json` (new — 404)
- `.plans/upnext/0023-commits-fetcher/TASK.md` (fleshed out from stub)
- `.plans/upnext/0023-commits-fetcher/verify.sh` (real verifier)

## Commits

```
c2adfff test(github/commits): replay tests for happy/304/HTTPError paths
45a1358 test(github/commits): VCR cassettes + gated authoring entrypoint
689eb19 feat(github/commits): types + REST fetcher with since/ETag/page
3c5d029 chore(github/commits): scaffold package
```

## Verify output

```
==> go vet ./internal/github/...
==> go build ./...
==> go test ./internal/github/commits/... -race -count=1
ok  	github.com/karnstack/tempo/internal/github/commits	1.332s
==> go test ./internal/github/... -race -count=1 (no regressions)
ok  	github.com/karnstack/tempo/internal/github	1.628s
ok  	github.com/karnstack/tempo/internal/github/commits	1.829s
ok  	github.com/karnstack/tempo/internal/github/prconvo	3.427s
ok  	github.com/karnstack/tempo/internal/github/prs	2.396s
ok  	github.com/karnstack/tempo/internal/github/vcr	4.007s
==> compile check: -tags=record ./internal/github/commits/...
==> compile check: -tags=gen ./internal/github/commits/...
VERIFY OK
```

## Followups

- 0029 (Commits ingest) will compose this fetcher with `sync_cursors`
  persistence + decide whether to detail-fetch additions/deletions per SHA.
- If profiling shows the per-SHA detail call cost matters, consider adding
  a `FetchStats(ctx, owner, repo, sha) (additions, deletions int, err)`
  method to this package rather than rolling it into ingest.
- Recording mode (`-tags=record`) is currently a compile-check only; if we
  ever want to re-record against real GitHub, write a `record_test.go`
  similar to the `vcr/record_test.go` pattern. Not blocking.
