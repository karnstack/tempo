# 0025 — Org repos enumerator — RESULT

Added `internal/github/orgrepos`, a slim REST list fetcher over
`GET /orgs/{org}/repos`. Same shape as
`internal/github/{commits,releases,deployments}`: page-number
pagination via Link header, conditional `If-None-Match` ETag,
stateless, no DB access, hermetic VCR replay tests with hand-authored
cassettes gated behind `//go:build gen`.

## Files added

- `internal/github/orgrepos/doc.go` — package preamble (purpose,
  pipeline position, ETag-as-cursor rule, slim-struct rationale,
  cassette conventions).
- `internal/github/orgrepos/fetcher.go` — `Repo`, `FetchOptions`,
  `Page`, `Fetcher`, `New`, `Fetch`, `hasNextLink`, `rawRepo`,
  `rawRepo.toRepo`.
- `internal/github/orgrepos/fetcher_test.go` — `TestFetch_Page`,
  `TestFetch_NotModified`, `TestFetch_HTTPError`, `newReplayClient`.
- `internal/github/orgrepos/cassettes_gen_test.go` (`//go:build gen`)
  — `TestGen_Cassettes` with three subtests.
- `internal/github/orgrepos/testdata/list_page.json`,
  `list_not_modified.json`, `list_http_error.json`.
- `.plans/upnext/0025-org-repos-enumerator/verify.sh`.

## Commits

```
a8503dc test(github/orgrepos): replay tests for happy/304/HTTPError paths
7f09fc5 test(github/orgrepos): VCR cassettes + gated authoring entrypoint
443f94a feat(github/orgrepos): types + REST fetcher with ETag/page
4a11f32 chore(github/orgrepos): scaffold package
```

## Verify output

```
==> go vet ./internal/github/...
==> go build ./...
==> go test ./internal/github/orgrepos/... -race -count=1
ok  	github.com/karnstack/tempo/internal/github/orgrepos	1.608s
==> go test ./internal/github/... -race -count=1 (no regressions)
ok  	github.com/karnstack/tempo/internal/github	1.674s
ok  	github.com/karnstack/tempo/internal/github/commits	2.689s
ok  	github.com/karnstack/tempo/internal/github/deployments	3.911s
ok  	github.com/karnstack/tempo/internal/github/orgrepos	3.230s
ok  	github.com/karnstack/tempo/internal/github/prconvo	5.142s
ok  	github.com/karnstack/tempo/internal/github/prs	5.703s
ok  	github.com/karnstack/tempo/internal/github/releases	4.575s
ok  	github.com/karnstack/tempo/internal/github/vcr	6.356s
==> compile check: -tags=record ./internal/github/orgrepos/...
==> compile check: -tags=gen ./internal/github/orgrepos/...
VERIFY OK
```

## Followups

- 0026 (ingest scheduler) wires this fetcher in: when iterating an
  `org` connection, page through `orgrepos.Fetch`, upsert `repos`
  rows, then dispatch the per-repo fetchers.
- Type-filter passthrough is exposed but untested with non-empty
  values (the cassette uses the default). When 0026 actually selects
  a non-default type, add a fourth cassette covering that path.
- `pushed_at`, `Disabled`, and user-repos enumeration
  (`GET /users/{user}/repos`) are intentionally deferred until an
  ingest consumer needs them.
