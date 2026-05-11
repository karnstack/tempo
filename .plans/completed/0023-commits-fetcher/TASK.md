---
id: 0023
slug: commits-fetcher
title: Commits fetcher (REST since cursor + ETag)
status: done
depends_on: [0020]
owner: ""
est_minutes: 100
tags: [github]
autonomy: full
skills: [systematic-debugging]
---

## Goal

Add a default-branch commits fetcher in a new package `internal/github/commits`.
It calls `GET /repos/{owner}/{repo}/commits` via REST with `since=<RFC3339>`
incremental cursoring, conditional `If-None-Match` ETag (304 → no rate-limit
cost), page-number pagination derived from the `Link` header, and returns one
typed `Page` per call. Stateless. No DB access. Hermetic VCR-replay tests with
hand-authored cassettes gated behind a `gen` build tag.

This is what task 0029 ("Commits ingest") will call once per repo per tick,
persisting `commits` rows and updating `sync_cursors`. Per-commit stats
(additions/deletions) are explicitly **out of scope** for this task — the list
endpoint doesn't return them; if 0029 needs them, it can detail-fetch
`GET /repos/{owner}/{repo}/commits/{sha}` per SHA. We keep this fetcher
surgical: one endpoint, one call per page, cheapest possible polling.

Design choices:

- **REST not GraphQL, on purpose.** Spec line 132 calls out conditional REST
  with ETag specifically because 304 responses do not consume the rate-limit
  budget. For commits — the single highest-volume signal — that matters more
  than the per-call cost difference. We pay one full GET on first-touch, then
  cheap 304s for every poll until the branch advances.
- **`since=` for incremental, ETag for "nothing changed".** They compose: the
  caller stores `(since, etag)` together in `sync_cursors`. Same `since`
  query keeps the cached ETag stable on the server, so 304s flow until a new
  commit lands.
- **Reuse `prs.Author`.** REST returns `{login, id, type}` (type ∈ `User`,
  `Bot`, …) — byte-compatible with the GraphQL-derived `prs.Author{GHID,
  Login, Type}` once we read REST's `type` field where GraphQL fed
  `__typename`. Null author → `prs.Author{Type:"Ghost"}` (account deleted or
  email unmatched).
- **Page-number cursors over Link-URL following.** GitHub's REST list uses
  `Link: <...>; rel="next"` for pagination; we honour `next` as the
  HasNext signal but expose `NextPage int` rather than the raw URL. Simpler
  to reason about, simpler to test, and survives base-URL rewrites the way
  the rest of `internal/github` already does (matching ignores host/scheme).
- **ETag only honoured on Page 1.** Each (URL+query) has its own ETag on the
  server. The first page's ETag is the "did anything new arrive?" signal; for
  subsequent pages we just stream through. The fetcher silently drops the
  caller-supplied ETag when `Page > 1` to keep the API hard to misuse.
- **No additions/deletions.** The list endpoint omits these. If 0029 needs
  them, it composes a per-SHA detail call — *or* we add a `FetchStats`
  method here later. Either way, not this task. Documented in `doc.go`.

Spec refs:
- `docs/superpowers/specs/2026-05-08-tempo-design.md` data model (line 99):
  `commits(repo_id, sha PK, author_gh_user_id, committer_gh_user_id,
  authored_at, additions, deletions, message)`. Additions/deletions are
  columns; this fetcher leaves them at 0 (caller's problem).
- Ingest strategy (lines 130–135): `since=<last_sync_at>` for commits,
  ETag / `If-Modified-Since`, 304s outside rate-limit budget.
- Plan row 144: stub, dep 0020, autonomy `full`, skill
  `systematic-debugging`.

## Acceptance criteria

- [ ] New package `internal/github/commits` with `doc.go` + `fetcher.go`.
- [ ] `commits.Fetcher` struct + `commits.New(c *github.Client) *Fetcher`
      ctor (mirrors `prs.New` / `prconvo.New`).
- [ ] `FetchOptions` struct: `SHA`, `Since time.Time`, `Page int`,
      `PerPage int`, `ETag string`.
- [ ] One method:
      `Fetch(ctx context.Context, owner, repo string, opts FetchOptions) (*Page, error)`.
- [ ] `PerPage` clamped to `[1,100]`; `PerPage<=0` defaults to 50 (match `prs`).
- [ ] `Page<=0` becomes 1. `ETag` is sent as `If-None-Match` **only** when
      `Page<=1` (silently dropped otherwise).
- [ ] `Since` non-zero formats as RFC3339 in the `since` query param;
      zero omits the param.
- [ ] `SHA` non-empty sets the `sha` query param (branch / ref / commit);
      empty defers to GitHub's default branch (omit param).
- [ ] Types:
    - `Commit{SHA string, Message string, Author prs.Author, Committer prs.Author,
      AuthoredAt time.Time, CommittedAt time.Time}`. No additions/deletions
      fields — document why in `doc.go`.
    - `Page{Commits []Commit, HasNext bool, NextPage int, NotModified bool, ETag string}`.
      `NextPage == 0` iff `HasNext == false`. On 304: `NotModified=true`,
      `Commits` empty, `HasNext=false`, `ETag` echoes the caller's input
      (server doesn't send ETag on 304 in practice, but we tolerate either).
- [ ] `HasNext` is derived from the `Link` response header's `rel="next"`
      entry (case-insensitive scan); when present, `NextPage = opts.Page + 1`
      (with `opts.Page` already normalised to ≥1).
- [ ] HTTP errors propagate as `*github.HTTPError` (no wrapping needed —
      `client.REST` already returns them for 4xx).
- [ ] Author parsing: when the JSON `author` / `committer` object is null →
      `prs.Author{Type:"Ghost"}`. When present, populate `GHID = id`,
      `Login = login`, `Type = type`. Local `parseAuthor` helper in this
      package (don't reach into `prs` for it).
- [ ] Tests under `internal/github/commits/`, fully hermetic via VCR replay
      from `testdata/*.json`. Minimum cassettes:
    - `list_page.json` — 200, three commits with mixed actor types
      (User author, Bot committer, Ghost — null author), `Link` header with
      `rel="next"`. Server-returned ETag. Validates HasNext, NextPage,
      ETag passthrough, all three author shapes.
    - `list_not_modified.json` — 304, no body, ETag header optional.
      Validates NotModified=true, Commits empty, HasNext=false, NextPage=0.
    - `list_http_error.json` — 404, JSON error body. Validates
      `errors.As(err, *github.HTTPError)` and status == 404.
- [ ] Every replay test calls `tr.Done()` in `t.Cleanup` so stale cassettes
      fail loud (per VCR doc) — same pattern as `prs` / `prconvo`.
- [ ] Cassette-authoring code lives in `//go:build gen`-gated file
      (`cassettes_gen_test.go`). Re-runnable with:
      `go test -tags=gen -run TestGen_Cassettes ./internal/github/commits/...`.
- [ ] `go vet ./internal/github/...` clean.
- [ ] `go build ./...` clean.
- [ ] `go test ./internal/github/... -race -count=1` passes (no regressions
      in `prs`/`prconvo`/`vcr`).
- [ ] `go test -tags=record -run='^$' ./internal/github/commits/...` compiles
      (so future re-records work).
- [ ] `go test -tags=gen -run='^$' ./internal/github/commits/...` compiles.

## Files to touch

- `internal/github/commits/doc.go` (new — package overview, REST contract,
  scope-out note for additions/deletions).
- `internal/github/commits/fetcher.go` (new — Fetcher, FetchOptions, Commit,
  Page, raw struct, Link-header parser, parseAuthor).
- `internal/github/commits/fetcher_test.go` (new — three replay subtests +
  `newReplayClient` helper).
- `internal/github/commits/cassettes_gen_test.go` (new — `//go:build gen`,
  authors all three cassettes).
- `internal/github/commits/testdata/list_page.json` (new — written by gen).
- `internal/github/commits/testdata/list_not_modified.json` (new).
- `internal/github/commits/testdata/list_http_error.json` (new).
- `.plans/upnext/0023-commits-fetcher/verify.sh` (new — mirrors 0022's shape,
  scoped to `commits`).

## Steps

Each step ends in a commit (small, frequent — match the 0021/0022 history).

### 1. Scaffold the package

Create `internal/github/commits/doc.go`. Mirror prose style of
`internal/github/prs/doc.go` and `internal/github/prconvo/doc.go`. Cover:
- Purpose (default-branch commits via REST list with cursor + ETag).
- Where it sits in the ingest pipeline (0029 will compose; persistence /
  cursor storage are in `internal/ingest`, not here).
- The since/ETag/page composition rules.
- The explicit scope-out for additions/deletions.

Commit: `chore(github/commits): scaffold package`

### 2. Types + Fetcher implementation

Write `internal/github/commits/fetcher.go`.

Sketch:

```go
package commits

import (
    "context"
    "fmt"
    "encoding/json"
    "net/http"
    "net/url"
    "strings"
    "time"

    "github.com/karnstack/tempo/internal/github"
    "github.com/karnstack/tempo/internal/github/prs"
)

const (
    maxPageSize     = 100
    defaultPageSize = 50
)

type Commit struct {
    SHA         string
    Message     string
    Author      prs.Author
    Committer   prs.Author
    AuthoredAt  time.Time
    CommittedAt time.Time
}

type Page struct {
    Commits     []Commit
    HasNext     bool
    NextPage    int
    NotModified bool
    ETag        string
}

type FetchOptions struct {
    SHA     string
    Since   time.Time
    Page    int
    PerPage int
    ETag    string
}

type Fetcher struct{ c *github.Client }

func New(c *github.Client) *Fetcher { return &Fetcher{c: c} }

func (f *Fetcher) Fetch(ctx context.Context, owner, repo string, opts FetchOptions) (*Page, error) {
    page := opts.Page
    if page <= 0 {
        page = 1
    }
    perPage := opts.PerPage
    switch {
    case perPage <= 0:
        perPage = defaultPageSize
    case perPage > maxPageSize:
        perPage = maxPageSize
    }

    q := url.Values{}
    q.Set("per_page", fmt.Sprintf("%d", perPage))
    q.Set("page", fmt.Sprintf("%d", page))
    if !opts.Since.IsZero() {
        q.Set("since", opts.Since.UTC().Format(time.RFC3339))
    }
    if opts.SHA != "" {
        q.Set("sha", opts.SHA)
    }

    path := fmt.Sprintf("/repos/%s/%s/commits?%s", owner, repo, q.Encode())
    var headers http.Header
    if page <= 1 && opts.ETag != "" {
        headers = http.Header{"If-None-Match": []string{opts.ETag}}
    }

    resp, err := f.c.REST(ctx, http.MethodGet, path, nil, headers)
    if err != nil {
        return nil, fmt.Errorf("commits: fetch %s/%s: %w", owner, repo, err)
    }

    out := &Page{ETag: resp.ETag}
    if resp.Status == http.StatusNotModified {
        out.NotModified = true
        if out.ETag == "" {
            out.ETag = opts.ETag // server may omit on 304; echo caller's value
        }
        return out, nil
    }

    var raw []rawCommit
    if len(resp.Body) > 0 {
        if err := json.Unmarshal(resp.Body, &raw); err != nil {
            return nil, fmt.Errorf("commits: decode %s/%s: %w", owner, repo, err)
        }
    }
    out.Commits = make([]Commit, 0, len(raw))
    for _, n := range raw {
        out.Commits = append(out.Commits, n.toCommit())
    }
    if hasNextLink(resp.Headers.Get("Link")) {
        out.HasNext = true
        out.NextPage = page + 1
    }
    return out, nil
}

// hasNextLink scans a GitHub Link header for a rel="next" entry.
// Header form: `<url1>; rel="next", <url2>; rel="last"`.
func hasNextLink(h string) bool {
    if h == "" {
        return false
    }
    for _, part := range strings.Split(h, ",") {
        if strings.Contains(part, `rel="next"`) {
            return true
        }
    }
    return false
}

type rawCommit struct {
    SHA    string `json:"sha"`
    Commit struct {
        Message string `json:"message"`
        Author  struct {
            Date time.Time `json:"date"`
        } `json:"author"`
        Committer struct {
            Date time.Time `json:"date"`
        } `json:"committer"`
    } `json:"commit"`
    Author    *rawActor `json:"author"`
    Committer *rawActor `json:"committer"`
}

type rawActor struct {
    Login string `json:"login"`
    ID    int64  `json:"id"`
    Type  string `json:"type"`
}

func (r rawCommit) toCommit() Commit {
    return Commit{
        SHA:         r.SHA,
        Message:     r.Commit.Message,
        Author:      parseActor(r.Author),
        Committer:   parseActor(r.Committer),
        AuthoredAt:  r.Commit.Author.Date,
        CommittedAt: r.Commit.Committer.Date,
    }
}

func parseActor(r *rawActor) prs.Author {
    if r == nil {
        return prs.Author{Type: "Ghost"}
    }
    return prs.Author{GHID: r.ID, Login: r.Login, Type: r.Type}
}
```

Run `go vet ./internal/github/...` and `go build ./...` to catch typos.

Commit: `feat(github/commits): types + REST fetcher with since/ETag/page`

### 3. Cassette author (gen-tagged) + cassettes

Write `internal/github/commits/cassettes_gen_test.go` with `//go:build gen`.
Model on `internal/github/prs/cassettes_gen_test.go` — pure
hand-authored, no real network. Three subtests:

- `list_page`: builds a `vcr.Cassette` with one interaction. Request:
  `GET /repos/karnstack/tempo/commits?page=1&per_page=100&since=...`.
  Response: 200, JSON array of three rawCommit-shaped objects:
  - SHA `aaaa...1`, User author (alice), User committer (alice),
    AuthoredAt 2026-04-12T10:00:00Z.
  - SHA `bbbb...2`, Bot author (renovate[bot]), User committer (alice),
    CommittedAt different from AuthoredAt to prove they're separate.
  - SHA `cccc...3`, null author + null committer → Ghost on both.
  Response headers: `ETag: W/"abc123"`, `Link: <https://...?page=2&per_page=100>; rel="next", <https://...?page=4&per_page=100>; rel="last"`.

- `list_not_modified`: request matches a follow-up poll with the same
  `since` (caller stored ETag). Response: 304, empty body, optional
  `ETag: W/"abc123"` header (test both presence and absence — pick one and
  comment).

- `list_http_error`: request `GET /repos/ghost-org/missing-repo/commits?...`.
  Response: 404 with body `{"message":"Not Found","documentation_url":"..."}`.

Helper functions (parallel to prs):
- `commitsRequestURL(owner, repo, page, perPage int, since string, sha string) string`
- `mustMarshal(t, v) json.RawMessage`

Run: `go test -tags=gen -run TestGen_Cassettes ./internal/github/commits/...`.
Verify three `testdata/*.json` files exist and parse via `vcr.LoadCassette`
(the `Save` path already roundtrips, but a sanity check is fine).

Commit: `test(github/commits): VCR cassettes + gated authoring entrypoint`

### 4. Replay tests

Write `internal/github/commits/fetcher_test.go`. Model on
`internal/github/prs/fetcher_test.go`:

- `newReplayClient(t, cassettePath)` helper — copy from prs (small;
  duplication is fine).
- `TestFetch_Page`: replay `list_page.json`. Assert HasNext=true,
  NextPage=2, ETag=`W/"abc123"`, len(Commits)==3, every Commit's
  fields. Validate all three author/committer shapes (User, Bot, Ghost).
- `TestFetch_NotModified`: replay `list_not_modified.json`. Assert
  NotModified=true, len(Commits)==0, HasNext=false, NextPage=0,
  ETag passthrough.
- `TestFetch_HTTPError`: replay `list_http_error.json`. Assert
  `errors.As(err, **github.HTTPError)` and `httpErr.Status == 404`.

Run `go test ./internal/github/commits/... -race -count=1`.

Commit: `test(github/commits): replay tests for happy/304/HTTPError paths`

### 5. Verify

Write `.plans/upnext/0023-commits-fetcher/verify.sh` (see below) and run it.
Capture last ~30 lines for `RESULT.md`.

Commit: `chore(plans/0023): verify.sh`

## Notes

- The list endpoint's `commit.author.date` and `commit.committer.date` may
  differ when a commit is rebased / cherry-picked. Storing both gives 0033
  (engineer stats rollup) flexibility — it can pick "authored_at" for
  attribution. The spec data model uses `authored_at` only, but we surface
  both at the fetcher layer and let the ingest decide which to persist.
- `commit.message` can be multi-megabyte for some unusual commits. We
  pass it through unchanged — storage truncation is 0029's call.
- ETags on REST are weak (`W/"..."`); GitHub treats `If-None-Match` against
  weak ETags correctly. Pass them through verbatim.
- Do NOT pre-emptively add a per-commit detail fetcher (`GET .../commits/{sha}`)
  in this task. It belongs to a future task or 0029's implementation. Keep
  this surgical.
- The `prs.Author` import creates an upward dependency from `commits` →
  `prs`. Same directional flow `prconvo` uses; fine.
- The github client's `applyHeaders` adds caller-supplied headers via
  `req.Header.Add` (not Set). For `If-None-Match` we want Add semantics
  anyway — one value, no overwrite worry.
