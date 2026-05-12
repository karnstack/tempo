---
id: 0025
slug: org-repos-enumerator
title: Org repos enumerator
status: in_progress
depends_on: [0019]
owner: ""
est_minutes: 75
tags: [github]
autonomy: full
skills: [systematic-debugging]
---

## Goal

Add `internal/github/orgrepos` — a REST list fetcher over
`GET /orgs/{org}/repos`. It mirrors the established commits / releases /
deployments shape exactly: conditional `If-None-Match` ETag (a 304 does
NOT consume the rate-limit budget), page-number pagination derived from
the `Link` response header, stateless, no DB access, hermetic VCR-replay
tests with hand-authored cassettes gated behind a `gen` build tag.

It is the bottom layer of the org-connection ingest path. The spec calls
out two relevant rows:

- `connections(id, tenant_id, kind, owner, name, ...)` where
  `kind ∈ {repo, org}` and `name` is nullable for orgs.
- `repos(id, tenant_id, connection_id, gh_id, owner, name,
  default_branch, archived, added_at)`.

When a user adds an `org` connection (or on each periodic poll), the
ingest worker (0026 / 0031) needs the current set of repos under that
org so it can upsert `repos` rows and dispatch the per-repo fetchers
(prs, commits, etc.) against them. That enumeration is what this
fetcher provides. Persistence, dedup, and the worker scheduling that
calls it all live in `internal/ingest` — this package stays surgical.

Design choices (parallel to 0023 / 0024):

- **One package, single endpoint.** Matches the one-package-per-endpoint
  convention (`commits`, `prs`, `prconvo`, `releases`, `deployments`).
- **Package name `orgrepos`.** Distinguishes it from a future user-repos
  enumerator (`GET /users/{user}/repos`) and avoids colliding with the
  `repos` table or any future `internal/repos` storage package.
- **REST + ETag, on purpose.** Same as 0024: spec line 132–133 says
  conditional REST with ETag because 304s do not consume the rate-limit
  budget. Org repo lists are small and change rarely; ETag carries the
  freshness signal cheaply.
- **No `since=` parameter — ETag is the cursor.** The endpoint does not
  accept a date filter. Caller stores `etag` in `sync_cursors`; if 304,
  nothing changed.
- **Expose `type` filter, default to `all`.** GitHub's own default for
  org-owner authenticated requests is `all`, but it's worth exposing
  because callers may want `public` / `sources` etc. for tighter scopes.
- **Slim Repo struct.** Spec-required fields (`gh_id`, `owner`, `name`,
  `default_branch`, `archived`) plus `Fork` and `Private` — both are
  ubiquitous filter inputs the worker will want (skip forks; surface
  private). `Disabled` and `pushed_at` deferred (YAGNI).
- **Page-number cursors via Link header.** Identical to commits /
  releases / deployments: parse `rel="next"` for `HasNext`, expose
  `NextPage int`.
- **ETag only honoured on Page 1.** Each (URL+query) has its own ETag
  on the server; only the first page is worth caching. The fetcher
  silently drops the caller-supplied ETag when `Page > 1`.
- **No actor parsing.** Org repos don't have a `creator`/`author` field
  worth surfacing here — the `owner` JSON object is just `{login, id,
  type}` for the parent org, which is redundant with the `org` argument.
  Keep the type small.

Spec refs:
- `docs/superpowers/specs/2026-05-08-tempo-design.md` data model (lines
  92–94): `connections` / `repos` tables.
- Ingest strategy (lines 130–135): conditional REST with ETag.
- Plan row 146: stub, dep 0020, autonomy `full`, skill
  `systematic-debugging`.

## Acceptance criteria

- [ ] Package `internal/github/orgrepos` with `doc.go`, `fetcher.go`,
      `fetcher_test.go`, `cassettes_gen_test.go`, and a `testdata/`
      directory holding three cassettes (parallel to
      `internal/github/releases/`).
- [ ] `Fetcher` struct + `New(c *github.Client) *Fetcher` ctor.
- [ ] Single method:
      `Fetch(ctx context.Context, org string, opts FetchOptions) (*Page, error)`.
- [ ] `Repo` type with fields:
      `{GHID int64, Owner, Name, DefaultBranch string, Archived, Fork, Private bool}`.
- [ ] `FetchOptions{Type string, Page, PerPage int, ETag string}`.
      `Type` empty → omit (server defaults to `all`).
- [ ] `PerPage` clamped to `[1,100]`; `PerPage<=0` defaults to 50.
      `Page<=0` becomes 1. `ETag` honoured only when `Page<=1`.
- [ ] `Page{Repos []Repo, HasNext bool, NextPage int, NotModified bool,
      ETag string}`. On 304: `NotModified=true`, `Repos` empty,
      `HasNext=false`, `NextPage=0`, `ETag` echoes caller's input when
      server omits.
- [ ] `HasNext` derived from response Link header's `rel="next"` (shared
      `hasNextLink` logic — duplicated per package, as commits / releases
      / deployments already do).
- [ ] HTTP errors propagate as `*github.HTTPError` (no extra wrapping
      beyond `fmt.Errorf("orgrepos: fetch ...: %w", err)`).
- [ ] Three cassettes:
    - `list_page.json` — 200, three repos (regular public, archived,
      fork). Link header `rel="next"`. Server-returned ETag.
    - `list_not_modified.json` — 304, no body, no ETag header (caller-
      echo path).
    - `list_http_error.json` — 404, JSON error body (unknown org).
- [ ] Replay test calls `tr.Done()` in `t.Cleanup` so stale cassettes
      fail loud.
- [ ] Cassette-authoring code lives in `//go:build gen`-gated file.
      Re-runnable with
      `go test -tags=gen -run TestGen_Cassettes ./internal/github/orgrepos/...`.

### Verification

- [ ] `go vet ./internal/github/...` clean.
- [ ] `go build ./...` clean.
- [ ] `go test ./internal/github/orgrepos/... -race -count=1` passes.
- [ ] `go test ./internal/github/... -race -count=1` passes (no regressions
      in prs / prconvo / commits / releases / deployments / vcr).
- [ ] `go test -tags=record -run='^$' ./internal/github/orgrepos/...`
      compiles.
- [ ] `go test -tags=gen -run='^$' ./internal/github/orgrepos/...`
      compiles.

## Files to touch

- `internal/github/orgrepos/doc.go` (new)
- `internal/github/orgrepos/fetcher.go` (new)
- `internal/github/orgrepos/fetcher_test.go` (new)
- `internal/github/orgrepos/cassettes_gen_test.go` (new — `//go:build gen`)
- `internal/github/orgrepos/testdata/list_page.json` (new — by gen)
- `internal/github/orgrepos/testdata/list_not_modified.json` (new — by gen)
- `internal/github/orgrepos/testdata/list_http_error.json` (new — by gen)
- `.plans/upnext/0025-org-repos-enumerator/verify.sh` (new)
- `.plans/upnext/0025-org-repos-enumerator/TASK.md` (this file, after each step)

## Steps

Each step ends in a small commit. History should look like 0024's.

### 1. Scaffold package (`doc.go`) ✓

Create `internal/github/orgrepos/doc.go` modelled on `releases/doc.go`.
Cover purpose, pipeline position (ingest at 0026/0031 composes
persistence; this package is REST-only), the ETag-as-cursor composition
rule, and the cassette test conventions.

Commit: `chore(github/orgrepos): scaffold package`

### 2. Types + fetcher ✓

Write `internal/github/orgrepos/fetcher.go`. Same skeleton as
`releases/fetcher.go`. Sketch:

```go
package orgrepos

const (
    maxPageSize     = 100
    defaultPageSize = 50
)

type Repo struct {
    GHID          int64
    Owner         string
    Name          string
    DefaultBranch string
    Archived      bool
    Fork          bool
    Private       bool
}

type FetchOptions struct {
    Type    string
    Page    int
    PerPage int
    ETag    string
}

type Page struct {
    Repos       []Repo
    HasNext     bool
    NextPage    int
    NotModified bool
    ETag        string
}

type Fetcher struct{ c *github.Client }

func New(c *github.Client) *Fetcher { return &Fetcher{c: c} }

func (f *Fetcher) Fetch(ctx context.Context, org string, opts FetchOptions) (*Page, error) {
    // clamp Page / PerPage, build query (page, per_page, optional type),
    // ETag only on page<=1, c.REST(...), branch on 304, decode []rawRepo,
    // hasNextLink scan.
}

type rawRepo struct {
    ID            int64  `json:"id"`
    Name          string `json:"name"`
    DefaultBranch string `json:"default_branch"`
    Archived      bool   `json:"archived"`
    Fork          bool   `json:"fork"`
    Private       bool   `json:"private"`
    Owner         struct {
        Login string `json:"login"`
    } `json:"owner"`
}

func (r rawRepo) toRepo() Repo { /* ... */ }

func hasNextLink(h string) bool { /* same as releases */ }
```

Endpoint path: `/orgs/{org}/repos?page=&per_page=[&type=]`.

Commit: `feat(github/orgrepos): types + REST fetcher with ETag/page`

### 3. Cassette author (`cassettes_gen_test.go`) + cassettes

Create `cassettes_gen_test.go` with `//go:build gen`. Three subtests
mirroring `releases/cassettes_gen_test.go`:

- `list_page` — three repos:
    1. `tempo` — regular public repo, default_branch=main, archived=false,
       fork=false, private=false.
    2. `legacy-archive` — archived=true, default_branch=master,
       private=false.
    3. `forked-toy` — fork=true, default_branch=main, private=false.
  Link header `rel="next"` + `rel="last"`. ETag `W/"orgrepos-abc"`.

- `list_not_modified` — 304, no headers (caller-echo path).

- `list_http_error` — 404, JSON `{message: "Not Found",
  documentation_url: ...}`. Org `ghost-org`.

Run `go test -tags=gen -run TestGen_Cassettes ./internal/github/orgrepos/...`
to materialise `testdata/*.json`. Commit the JSON.

Commit: `test(github/orgrepos): VCR cassettes + gated authoring entrypoint`

### 4. Replay tests (`fetcher_test.go`)

Create `fetcher_test.go` with `newReplayClient` helper (copy from
`releases/fetcher_test.go`). Three tests:

- `TestFetch_Page` — verifies the three repo shapes, Link → HasNext +
  NextPage=2, server-returned ETag, PerPage=999 → clamped to 100.
- `TestFetch_NotModified` — 304 → NotModified=true, Repos empty, ETag
  echoes caller input.
- `TestFetch_HTTPError` — 404 → `*github.HTTPError`, Status=404.

Commit: `test(github/orgrepos): replay tests for happy/304/HTTPError paths`

### 5. verify.sh

Write `.plans/upnext/0025-org-repos-enumerator/verify.sh` modelled on
0024's: go vet, go build, package-scoped go test, full-tree go test,
record-tag compile, gen-tag compile. `chmod +x`.

Commit: included in step 4 or in step 6.

### 6. Run verify, wrap up

Run `./.plans/upnext/0025-org-repos-enumerator/verify.sh` end-to-end.
On success: write `RESULT.md`, flip `status: done`, `git mv` to
`.plans/completed/`, final commit
`feat(github): Org repos enumerator (#0025)`.

## Notes

- The org connection's `name` column is nullable per spec line 93 ("name
  nullable for orgs"). This fetcher takes `org` as the connection's
  `owner` field — the org login itself.
- We deliberately omit `pushed_at` and `Disabled`. Add them only when an
  ingest task actually needs them.
- We could in principle reuse `prs.Author` for `owner`, but
  `Repo.Owner` is just a string (login). No null case; the owner is the
  parent org. Keep it surgical.
- The `type` filter when sent is one of `all`, `public`, `private`,
  `forks`, `sources`, `member`. We don't validate — GitHub will return
  422 for invalid values and that propagates as `*github.HTTPError`.
