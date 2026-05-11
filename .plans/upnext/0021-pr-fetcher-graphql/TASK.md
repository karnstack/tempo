---
id: 0021
slug: pr-fetcher-graphql
title: PR fetcher (GraphQL with cursors)
status: in_progress
depends_on: [0020]
owner: ""
est_minutes: 90
tags: [github]
autonomy: full
skills: [systematic-debugging]
---

## Goal

Add the first GitHub resource fetcher: `internal/github/prs`. It queries the
`repository.pullRequests` GraphQL connection ordered by `UPDATED_AT DESC`,
returns one page at a time with the GraphQL `endCursor` + `hasNextPage` flag,
and short-circuits when the page reaches a caller-supplied `since` cutoff so
incremental polls stop walking the history.

This is the bottom layer of the PR pipeline. Persistence + cursor storage +
paging-the-whole-history is 0027's job (`PR ingest end-to-end with cursor
persistence`). 0022 will extend this query (or this package) with reviews,
review comments, and issue comments. 0021 stays pure: a stateless fetcher
that takes a `*github.Client`, returns parsed data, and never touches the DB.

Spec refs:

- `docs/superpowers/specs/2026-05-08-tempo-design.md` §"Ingest strategy"
  (lines 123–149): GraphQL-first, cursor on `updatedAt`, page-by-page.
- Data model PRs (line 100): `pull_requests(repo_id, number, gh_id,
  author_gh_user_id, state, title, created_at, merged_at, closed_at,
  additions, deletions, base_ref, head_ref, draft)`.
- Plan row 142: stub, dep 0020, autonomy `full`, skill `systematic-debugging`.

## Acceptance criteria

- [ ] New package `internal/github/prs` with `doc.go` + `fetcher.go`.
- [ ] `prs.Fetcher` struct + `prs.New(c *github.Client) *Fetcher` ctor.
- [ ] `Fetcher.Fetch(ctx, owner, repo, after string, first int, since time.Time) (*Page, error)`
      pages through `repository.pullRequests` ordered by `UPDATED_AT DESC`.
- [ ] `Page{PRs, HasNext, EndCursor, ReachedSince}` — `ReachedSince` is true
      when at least one returned node had `UpdatedAt <= since`; PRs strictly
      older than `since` are dropped from `Page.PRs` (we still see them, we
      just don't surface them, and we set `ReachedSince` to tell the caller
      to stop after this page).
- [ ] `since == time.Time{}` (zero) disables the cutoff: all PRs are returned
      and `ReachedSince` is always false.
- [ ] `PR` struct mirrors the schema: `GHID int64`, `Number int`, `Title string`,
      `State string` (OPEN|CLOSED|MERGED), `Author Author`, `CreatedAt`,
      `UpdatedAt`, `MergedAt *time.Time`, `ClosedAt *time.Time`, `Additions`,
      `Deletions`, `BaseRef`, `HeadRef`, `Draft bool`.
- [ ] `Author{GHID int64, Login string, Type string}` — `Type` is GraphQL
      `__typename` ("User", "Bot", "Mannequin", or "Ghost" when the GraphQL
      `author` field is null because the account was deleted).
- [ ] Author parser handles: User, Bot, Mannequin (each with `databaseId`),
      and null (Ghost → `Author{Type:"Ghost"}`, GHID=0, Login="").
- [ ] GraphQL application errors come back unchanged as `*github.GraphQLError`
      (the client already does this).
- [ ] `first` is clamped to `[1,100]` (GitHub's GraphQL page-size limit);
      `first <= 0` defaults to 50.
- [ ] Tests under `internal/github/prs/`, all hermetic via VCR replay
      (`internal/github/vcr`). At least one cassette per scenario:
      `list_page.json` (mixed PR states + author types in one page),
      `list_since_cutoff.json` (`ReachedSince` + filtering),
      `list_graphql_error.json` (GraphQL error envelope passthrough).
- [ ] Every replay test calls `tr.Done()` in `t.Cleanup` so stale cassettes
      fail loud (per the VCR doc).
- [ ] `go vet ./internal/github/...` clean.
- [ ] `go test ./internal/github/... -race -count=1` passes.
- [ ] `go test -tags=record -run='^$' ./internal/github/prs/...` compiles
      (we don't ship a record_*_test.go yet, but the package must build under
      the tag for future re-records).

## Files to touch

- `internal/github/prs/doc.go` (new)
- `internal/github/prs/fetcher.go` (new)
- `internal/github/prs/fetcher_test.go` (new)
- `internal/github/prs/testdata/list_page.json` (new — hand-authored cassette)
- `internal/github/prs/testdata/list_since_cutoff.json` (new — cassette)
- `internal/github/prs/testdata/list_graphql_error.json` (new — cassette)
- `.plans/upnext/0021-pr-fetcher-graphql/verify.sh` (rewrite)

## Steps

Each step ends in a commit. Keep them small.

### 1. Scaffold the package

Create `internal/github/prs/doc.go` describing the package's job and how it
composes with `internal/github` (it's a fetcher; persistence is elsewhere).

Commit: `chore(github/prs): scaffold package`

### 2. Define the types + query

In `internal/github/prs/fetcher.go`:

```go
package prs

import (
    "context"
    "encoding/json"
    "fmt"
    "time"

    "github.com/karnstack/tempo/internal/github"
)

const (
    maxPageSize     = 100
    defaultPageSize = 50
)

type PR struct {
    GHID      int64
    Number    int
    Title     string
    State     string
    Author    Author
    CreatedAt time.Time
    UpdatedAt time.Time
    MergedAt  *time.Time
    ClosedAt  *time.Time
    Additions int
    Deletions int
    BaseRef   string
    HeadRef   string
    Draft     bool
}

type Author struct {
    GHID  int64
    Login string
    Type  string
}

type Page struct {
    PRs          []PR
    HasNext      bool
    EndCursor    string
    ReachedSince bool
}

type Fetcher struct{ c *github.Client }

func New(c *github.Client) *Fetcher { return &Fetcher{c: c} }

const listQuery = `query($owner: String!, $repo: String!, $first: Int!, $after: String) {
  repository(owner: $owner, name: $repo) {
    pullRequests(first: $first, after: $after, orderBy: {field: UPDATED_AT, direction: DESC}) {
      pageInfo { hasNextPage endCursor }
      nodes {
        databaseId number title state
        createdAt updatedAt mergedAt closedAt
        additions deletions
        baseRefName headRefName
        isDraft
        author {
          __typename
          login
          ... on User { databaseId }
          ... on Bot { databaseId }
          ... on Mannequin { databaseId }
        }
      }
    }
  }
}`

type rawPage struct {
    Repository struct {
        PullRequests struct {
            PageInfo struct {
                HasNextPage bool   `json:"hasNextPage"`
                EndCursor   string `json:"endCursor"`
            } `json:"pageInfo"`
            Nodes []rawPR `json:"nodes"`
        } `json:"pullRequests"`
    } `json:"repository"`
}

type rawPR struct {
    DatabaseID  int64      `json:"databaseId"`
    Number      int        `json:"number"`
    Title       string     `json:"title"`
    State       string     `json:"state"`
    CreatedAt   time.Time  `json:"createdAt"`
    UpdatedAt   time.Time  `json:"updatedAt"`
    MergedAt    *time.Time `json:"mergedAt"`
    ClosedAt    *time.Time `json:"closedAt"`
    Additions   int        `json:"additions"`
    Deletions   int        `json:"deletions"`
    BaseRefName string     `json:"baseRefName"`
    HeadRefName string     `json:"headRefName"`
    IsDraft     bool       `json:"isDraft"`
    Author      *rawAuthor `json:"author"`
}

type rawAuthor struct {
    Typename   string `json:"__typename"`
    Login      string `json:"login"`
    DatabaseID int64  `json:"databaseId"`
}
```

Commit: `feat(github/prs): types + GraphQL query`

### 3. Implement Fetch + parser

Add to `fetcher.go`:

```go
func (f *Fetcher) Fetch(ctx context.Context, owner, repo, after string, first int, since time.Time) (*Page, error) {
    if first <= 0 {
        first = defaultPageSize
    } else if first > maxPageSize {
        first = maxPageSize
    }
    vars := map[string]any{
        "owner": owner,
        "repo":  repo,
        "first": first,
    }
    if after != "" {
        vars["after"] = after
    } else {
        vars["after"] = nil
    }
    var raw rawPage
    if err := f.c.GraphQL(ctx, listQuery, vars, &raw); err != nil {
        return nil, fmt.Errorf("prs: fetch %s/%s: %w", owner, repo, err)
    }
    page := &Page{
        HasNext:   raw.Repository.PullRequests.PageInfo.HasNextPage,
        EndCursor: raw.Repository.PullRequests.PageInfo.EndCursor,
    }
    page.PRs = make([]PR, 0, len(raw.Repository.PullRequests.Nodes))
    for _, n := range raw.Repository.PullRequests.Nodes {
        if !since.IsZero() && !n.UpdatedAt.After(since) {
            page.ReachedSince = true
            continue
        }
        page.PRs = append(page.PRs, n.toPR())
    }
    return page, nil
}

func (r rawPR) toPR() PR {
    return PR{
        GHID:      r.DatabaseID,
        Number:    r.Number,
        Title:     r.Title,
        State:     r.State,
        Author:    r.Author.parse(),
        CreatedAt: r.CreatedAt,
        UpdatedAt: r.UpdatedAt,
        MergedAt:  r.MergedAt,
        ClosedAt:  r.ClosedAt,
        Additions: r.Additions,
        Deletions: r.Deletions,
        BaseRef:   r.BaseRefName,
        HeadRef:   r.HeadRefName,
        Draft:     r.IsDraft,
    }
}

func (r *rawAuthor) parse() Author {
    if r == nil {
        return Author{Type: "Ghost"}
    }
    return Author{GHID: r.DatabaseID, Login: r.Login, Type: r.Typename}
}

// Compile-time check to keep json import in use even if rawPR fields drift.
var _ = json.Marshaler(nil)
```

(Drop the `json.Marshaler` line — only added to silence an unused-import; if
`encoding/json` ends up unused after step 4, remove the import instead.)

Commit: `feat(github/prs): page fetch with cursor + since cutoff`

### 4. Author the cassettes

Three cassettes, hand-crafted JSON. Cassette format is documented in
`internal/github/vcr/cassette.go` (`Cassette`, `Interaction`, `Request`,
`Response`). All cassettes:

- `request.method = "POST"`, `request.url = "https://api.github.com/graphql"`.
- `request.body` = an object with the canonical GraphQL envelope:
  `{"query": <listQuery>, "variables": {"owner": "...", "repo": "...", "first": 50, "after": null}}`.
  (Matching canonicalises JSON keys/whitespace — see
  `internal/github/vcr/cassette.go:171–183`.)
- `response.status = 200`, `response.body = {"data": {...}}`.

Files:

- `testdata/list_page.json` — single page, `hasNextPage=true`, `endCursor="Y3Vyc29yOjE="`,
  4 nodes mixing: merged PR (User author), closed-not-merged PR (Bot author),
  open draft PR (Mannequin author), open PR with `author: null` (Ghost).
- `testdata/list_since_cutoff.json` — `hasNextPage=false`, 3 nodes ordered
  by updatedAt DESC: two with `updatedAt` > since cutoff (2026-04-01T00:00:00Z),
  one with `updatedAt` < cutoff. Test asserts `ReachedSince=true`, `len(PRs)==2`,
  older PR dropped.
- `testdata/list_graphql_error.json` — response body:
  `{"data": null, "errors": [{"message": "Could not resolve to a Repository with the name 'x/y'.", "type": "NOT_FOUND"}]}`.

Commit: `test(github/prs): VCR cassettes for page/since/error`

### 5. Tests

In `internal/github/prs/fetcher_test.go`:

```go
package prs

import (
    "context"
    "errors"
    "net/http"
    "testing"
    "time"

    "github.com/karnstack/tempo/internal/github"
    "github.com/karnstack/tempo/internal/github/vcr"
)

func newReplayClient(t *testing.T, cassettePath string) *github.Client {
    t.Helper()
    tr, err := vcr.NewTransport(cassettePath, vcr.ModeReplay)
    if err != nil {
        t.Fatal(err)
    }
    t.Cleanup(func() {
        if err := tr.Close(); err != nil {
            t.Errorf("vcr.Close: %v", err)
        }
        if err := tr.Done(); err != nil {
            t.Errorf("vcr.Done: %v", err)
        }
    })
    return github.New("test-token",
        github.WithHTTPClient(&http.Client{Transport: tr}),
        github.WithBackoff(func(int) time.Duration { return 0 }),
    )
}

func TestFetch_Page(t *testing.T) { /* asserts all 4 author types, MergedAt/ClosedAt, Draft, fields */ }
func TestFetch_SinceCutoff(t *testing.T) { /* ReachedSince=true, older PR dropped */ }
func TestFetch_GraphQLError(t *testing.T) {
    f := New(newReplayClient(t, "testdata/list_graphql_error.json"))
    _, err := f.Fetch(context.Background(), "x", "y", "", 50, time.Time{})
    var ge *github.GraphQLError
    if !errors.As(err, &ge) {
        t.Fatalf("err = %v, want *github.GraphQLError", err)
    }
}
func TestFetch_PageSizeClamping(t *testing.T) { /* use list_page.json; pass first=999, assert no error (cassette matches first=100) */ }
```

Note on page-size clamping test: the cassette must be recorded with the
*clamped* `first` value (100). We can keep a separate cassette
`list_clamped.json`, or just fold this into the page test by recording it at
`first=100`. Simplest: record `list_page.json` at `first=100`, and in tests
call with `first=999` to exercise the clamp; recording `list_since_cutoff.json`
at `first=100` too. Skip the dedicated clamp test and just assert clamping
inline.

Commit: `test(github/prs): replay tests for fetch behaviour`

### 6. Update verify.sh

Replace the stub `verify.sh` with:

```bash
#!/usr/bin/env bash
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

echo "==> go vet ./internal/github/..."
go vet ./internal/github/...

echo "==> go build ./..."
go build ./...

echo "==> go test ./internal/github/prs/... -race -count=1"
go test ./internal/github/prs/... -race -count=1

echo "==> go test ./internal/github/... -race -count=1 (no regressions)"
go test ./internal/github/... -race -count=1

echo "==> compile check: -tags=record ./internal/github/prs/..."
go test -tags=record -run='^$' ./internal/github/prs/... >/dev/null

echo "VERIFY OK"
```

Commit: `chore(github/prs): verify.sh`

### 7. Run verify, then move to completed (autonomy: full)

- `./.plans/upnext/0021-pr-fetcher-graphql/verify.sh`
- Write `RESULT.md` per the next-task workflow.
- Update frontmatter `status: done`.
- `git mv` to `.plans/completed/`.
- `git add -A && git commit -m "feat(github): PR fetcher (GraphQL with cursors) (#0021)"`.

## Notes

- **Why pages, not an iterator.** Persistence + cursor storage (0027) wants
  to commit the GraphQL `endCursor` after every successful page; an iterator
  hides that boundary. Page-at-a-time keeps the contract explicit.
- **Why filter `since` client-side.** GitHub's `pullRequests` connection
  has no native `updatedAt` filter — only `orderBy`. We order DESC and stop
  when the page crosses the cutoff. This is also why `ReachedSince` is per-
  page (not per-PR): the caller stops the loop, not us.
- **Strict-greater vs.  greater-or-equal.** We drop PRs where
  `updatedAt <= since`, i.e. the caller's `since` should be the
  `last_seen_updated_at` they already have. Equality means "we already
  have this PR's current state".
- **GraphQL author union.** Five concrete types in GitHub's schema can be
  PR authors (User, Bot, Mannequin, EnterpriseUserAccount, Organization).
  `EnterpriseUserAccount` is GHE-only and `Organization` is rare. We only
  inline-fragment three, but a future type still parses with `Login` and
  `Type` set and `GHID=0`. That's a degradation we can live with — 0027
  can flag `Author.GHID == 0` as "look up via Login or treat as ghost".
- **No fx wiring here.** No consumer of `prs.Fetcher` exists yet. 0026/0027
  will wire `fx.Provide(prs.New)`.
- **Future-proofing for 0022.** When 0022 lands reviews/review-comments/
  issue-comments, the natural extension is to grow the GraphQL query (add
  nested `reviews(first:N) { nodes {...} }` etc.) and grow the `PR` struct.
  No restructure required.
