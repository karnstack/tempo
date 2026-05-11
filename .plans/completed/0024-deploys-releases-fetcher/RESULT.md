# 0024 — Deployments + Releases fetcher

Adds two sibling packages — `internal/github/deployments` and
`internal/github/releases` — each mirroring the `internal/github/commits`
shape exactly:

- REST list endpoints with conditional `If-None-Match` ETag (304 → no
  rate-limit cost) and page-number pagination derived from the `Link`
  header.
- Stateless, no DB access — persistence + cursor storage live in 0030.
- Hermetic VCR-replay tests via hand-authored cassettes gated behind a
  `gen` build tag.

The deployments endpoint exposes `SHA`/`Ref`/`Environment` filters
(useful for 0030's "production-only" cut). The releases endpoint has no
query filters; ETag alone carries the freshness signal. Deployment
**statuses** and per-release assets are deliberately out of scope (same
surgical scope-out the commits fetcher uses for additions/deletions);
0030 can compose detail calls if/when needed.

`Release.PublishedAt` is `*time.Time` in the raw decoder and zero-valued
when GitHub returns `null` (drafts), so callers see
`Release.PublishedAt == time.Time{}` for unpublished drafts. `Draft` and
`Prerelease` flags propagate verbatim.

## Files changed

- `internal/github/deployments/doc.go` (new)
- `internal/github/deployments/fetcher.go` (new — Fetcher, FetchOptions,
  Deployment, Page, Link parser, parseActor reusing `prs.Author`)
- `internal/github/deployments/fetcher_test.go` (new — three replay tests)
- `internal/github/deployments/cassettes_gen_test.go` (new — `//go:build gen`)
- `internal/github/deployments/testdata/list_page.json` (new — 200, three
  deployments, mixed creators, Link header rel="next")
- `internal/github/deployments/testdata/list_not_modified.json` (new — 304,
  no body, no ETag → caller-echo path)
- `internal/github/deployments/testdata/list_http_error.json` (new — 404)
- `internal/github/releases/doc.go` (new)
- `internal/github/releases/fetcher.go` (new — Fetcher, FetchOptions,
  Release, Page; `*time.Time` raw PublishedAt for null-tolerance)
- `internal/github/releases/fetcher_test.go` (new — three replay tests)
- `internal/github/releases/cassettes_gen_test.go` (new — `//go:build gen`)
- `internal/github/releases/testdata/list_page.json` (new — 200, three
  releases: stable / prerelease / draft, mixed author shapes)
- `internal/github/releases/testdata/list_not_modified.json` (new — 304)
- `internal/github/releases/testdata/list_http_error.json` (new — 404)
- `.plans/upnext/0024-deploys-releases-fetcher/TASK.md` (fleshed out from stub)
- `.plans/upnext/0024-deploys-releases-fetcher/verify.sh` (real verifier)

## Commits

```
0db0ec3 test(github/releases): replay tests for happy/304/HTTPError paths
2f6f352 test(github/releases): VCR cassettes + gated authoring entrypoint
a480160 feat(github/releases): types + REST fetcher with ETag/page
070ca17 chore(github/releases): scaffold package
18e0a8b test(github/deployments): replay tests for happy/304/HTTPError paths
08683d9 test(github/deployments): VCR cassettes + gated authoring entrypoint
475c60e feat(github/deployments): types + REST fetcher with ETag/page
bc0614b chore(github/deployments): scaffold package
```

## Verify output

```
==> go vet ./internal/github/...
==> go build ./...
==> go test ./internal/github/deployments/... -race -count=1
ok  	github.com/karnstack/tempo/internal/github/deployments	1.364s
==> go test ./internal/github/releases/... -race -count=1
ok  	github.com/karnstack/tempo/internal/github/releases	1.360s
==> go test ./internal/github/... -race -count=1 (no regressions)
ok  	github.com/karnstack/tempo/internal/github	2.145s
ok  	github.com/karnstack/tempo/internal/github/commits	1.586s
ok  	github.com/karnstack/tempo/internal/github/deployments	2.397s
ok  	github.com/karnstack/tempo/internal/github/prconvo	2.924s
ok  	github.com/karnstack/tempo/internal/github/prs	3.476s
ok  	github.com/karnstack/tempo/internal/github/releases	4.336s
ok  	github.com/karnstack/tempo/internal/github/vcr	4.050s
==> compile check: -tags=record ./internal/github/deployments/...
==> compile check: -tags=record ./internal/github/releases/...
==> compile check: -tags=gen ./internal/github/deployments/...
==> compile check: -tags=gen ./internal/github/releases/...
VERIFY OK
```

## Followups

- **0030 (Deployments ingest)** is the natural composer: pulls from both
  packages, merges into the unified `deployments(gh_id PK, repo_id,
  environment, ref, sha, status, created_at)` table, and decides whether
  to skip drafts / non-deploy releases. The merge key is `gh_id` (both
  endpoints return distinct ID spaces, so no collision risk).
- **Deployment statuses** — if 0030 needs the latest status, add a
  `FetchStatuses(ctx, owner, repo, deploymentID) (string, error)` to
  `internal/github/deployments` rather than rolling it into ingest. The
  status column in the spec's `deployments` table is the destination.
- **Recording mode** (`-tags=record`) is currently a compile-check only;
  if we ever want to re-record against real GitHub, write a `record_test.go`
  per the `vcr/record_test.go` pattern. Not blocking.
- **Releases assets** (the `assets` array on each release) are dropped
  in the raw decoder. If a future task wants asset names/download
  counts, add them to `rawRelease` and the public `Release` struct.
