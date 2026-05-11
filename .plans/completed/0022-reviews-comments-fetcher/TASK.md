---
id: 0022
slug: reviews-comments-fetcher
title: Reviews + review-comments + issue-comments fetchers
status: done
depends_on: [0021]
owner: ""
est_minutes: 120
tags: [github]
autonomy: full
skills: [systematic-debugging]
---

## Goal

Add the per-PR sub-resource fetchers in a new package `internal/github/prconvo`
("PR conversation"): reviews, review comments, and PR issue comments. Each is
its own GraphQL query, paginated cursor-by-cursor, returning flat node lists.
Mirrors the `internal/github/prs` (0021) pattern: stateless `Fetcher`, no DB
access, hermetic VCR-replay tests, hand-authored cassettes gated behind a
`gen` build tag.

This is what task 0028 ("Reviews/comments ingest") will call once per PR
whose `updatedAt` advanced. The package is the bottom layer — persistence,
upsert logic, and the per-PR loop live in `internal/ingest`, not here.

Design choices:

- **Three separate GraphQL queries, three fetcher methods, one package.**
  The spec (line 128) prefers one big query per PR, but bundling reviews +
  threads + issue comments into a single GraphQL query makes pagination
  awkward (each sub-connection has its own cursor, and threads contain a
  nested `comments` connection). Three small queries cost ~3 GraphQL points
  per touched PR instead of 1; at tempo's scale that's still well under the
  5,000-points-per-hour PAT budget. v2 can collapse into a single query if
  ingest profiles say it's worth it.
- **No `since` cutoff.** Sub-resources come back ASC; the only way to "stop
  early" is to compare each node's createdAt/submittedAt, which forces us to
  paginate the whole connection anyway (no DESC ordering option on these
  connections). The caller (0028) re-fetches all sub-resources for any PR
  whose parent `updatedAt` crossed the cursor and upserts by `gh_id`.
- **Reuse `prs.Author`.** The actor shape (`{GHID, Login, Type}`) is byte-
  for-byte identical to what's already in `internal/github/prs`. Import it
  rather than duplicate. A future refactor could lift it to a shared package
  if a third caller appears.
- **Review comments via `reviewThreads`.** GitHub's GraphQL exposes review
  comments as nested children of threads (no flat `reviewComments` connection
  on `PullRequest`). We paginate threads (cursor-driven) and inline their
  `comments(first: 100)` — adequate for tempo's small-team use case; a thread
  with >100 comments is exceptional and we explicitly drop the overflow with
  a Truncated flag on the page so callers can warn.

Spec refs:
- `docs/superpowers/specs/2026-05-08-tempo-design.md` data model (lines
  101–103): `pr_reviews(gh_id PK, …, reviewer_gh_user_id, state, submitted_at)`,
  `pr_review_comments(gh_id PK, …, author_gh_user_id, created_at)`,
  `pr_issue_comments(gh_id PK, …, author_gh_user_id, created_at)`.
- Ingest strategy (lines 123–149): GraphQL-first, cursor-based, polling
  cadence 15 min.
- Plan row 143: stub, dep 0021, autonomy `full`, skill `systematic-debugging`.

## Acceptance criteria

- [ ] New package `internal/github/prconvo` with `doc.go` + `fetcher.go`
      (or split per resource — `reviews.go`, `review_comments.go`,
      `issue_comments.go`, whichever stays readable).
- [ ] `prconvo.Fetcher` struct + `prconvo.New(c *github.Client) *Fetcher`
      ctor (mirrors `prs.New`).
- [ ] Three Fetch methods, each paginated by cursor:
    - `FetchReviews(ctx, owner, repo string, number int, after string, first int) (*ReviewsPage, error)`
    - `FetchReviewComments(ctx, owner, repo string, number int, after string, first int) (*ReviewCommentsPage, error)`
    - `FetchIssueComments(ctx, owner, repo string, number int, after string, first int) (*IssueCommentsPage, error)`
- [ ] `first` clamped to `[1,100]`; `first<=0` defaults to 50 (match `prs`).
- [ ] Types:
    - `Review{GHID int64, State string, SubmittedAt time.Time, Author prs.Author}`
      — `State` is the GraphQL enum verbatim: `APPROVED`, `CHANGES_REQUESTED`,
      `COMMENTED`, `DISMISSED`. `PENDING` reviews aren't visible via the API.
    - `ReviewComment{GHID int64, Author prs.Author, CreatedAt time.Time}`.
    - `IssueComment{GHID int64, Author prs.Author, CreatedAt time.Time}`.
- [ ] Page types:
    - `ReviewsPage{Reviews []Review, HasNext bool, EndCursor string}`.
    - `ReviewCommentsPage{Comments []ReviewComment, HasNext bool, EndCursor string, Truncated bool}`
      — `Truncated=true` when any thread had >100 inline comments and we
      dropped overflow.
    - `IssueCommentsPage{Comments []IssueComment, HasNext bool, EndCursor string}`.
- [ ] Author handling identical to `prs`: User / Bot / Mannequin each carry
      `databaseId`; `author == null` → `prs.Author{Type:"Ghost"}` with
      GHID=0, Login="". Local `parseAuthor` helper, not duplicating the
      one in `prs`; only the type is shared.
- [ ] GraphQL application errors propagate as `*github.GraphQLError` (no
      wrapping needed — `client.GraphQL` already returns them).
- [ ] Tests under `internal/github/prconvo/`, fully hermetic via VCR replay
      from `testdata/*.json`. At minimum one cassette + test per method:
    - `reviews_page.json` — multi-state reviews, mixed author types.
    - `review_comments_page.json` — two threads, one with multiple comments,
      mixed author types, optionally a Truncated case.
    - `issue_comments_page.json` — flat issue-comment page, mixed actors.
    - One GraphQL-error cassette (any one method is fine — exercises that
      `*github.GraphQLError` surfaces unchanged).
- [ ] Every replay test calls `tr.Done()` in `t.Cleanup` so stale cassettes
      fail loud (per VCR doc).
- [ ] Cassette authoring code lives in a `//go:build gen`-gated test file
      (`cassettes_gen_test.go`) — mirrors 0021. Re-runnable with
      `go test -tags=gen -run TestGen_Cassettes ./internal/github/prconvo/...`.
- [ ] `internal/github/prs/doc.go` updated: the "0022 will add" note in its
      Wiring section is now stale — point at `internal/github/prconvo`
      instead, or remove the forward-reference.
- [ ] `go vet ./internal/github/...` clean.
- [ ] `go build ./...` clean.
- [ ] `go test ./internal/github/... -race -count=1` passes (including all
      prior `prs/` and `vcr/` tests — no regressions).
- [ ] `go test -tags=record -run='^$' ./internal/github/prconvo/...` compiles
      (no record_*_test.go yet; we just need the package to build under the
      tag so future re-records work).

## Files to touch

- `internal/github/prconvo/doc.go` (new)
- `internal/github/prconvo/fetcher.go` (new — Fetcher type + three methods +
  three GraphQL queries + raw structs + author parser)
- `internal/github/prconvo/fetcher_test.go` (new — three replay tests
  covering happy paths)
- `internal/github/prconvo/fetcher_error_test.go` (new — one GraphQL error
  passthrough test, on whichever method)
- `internal/github/prconvo/cassettes_gen_test.go` (new — `//go:build gen`,
  authors all cassettes)
- `internal/github/prconvo/testdata/reviews_page.json` (new — hand-authored
  cassette, written by the `gen` test)
- `internal/github/prconvo/testdata/review_comments_page.json` (new)
- `internal/github/prconvo/testdata/issue_comments_page.json` (new)
- `internal/github/prconvo/testdata/<one>_error.json` (new — one cassette
  for the GraphQL-error case)
- `internal/github/prs/doc.go` (edit — remove or update the 0022 forward
  reference)
- `.plans/upnext/0022-reviews-comments-fetcher/verify.sh` (rewrite — matches
  0021 verify.sh shape, scoped to `prconvo`)

## Steps

Each step ends in a commit (small, frequent — matching the 0021 history).

### 1. Scaffold the package

Create `internal/github/prconvo/doc.go` describing the package's role and
how it composes with `internal/github`. Mirror the prose style of
`internal/github/prs/doc.go`.

Commit: `chore(github/prconvo): scaffold package`

### 2. Define types + GraphQL queries + Fetcher

Write `internal/github/prconvo/fetcher.go`. One Fetcher, three methods, three
const GraphQL query strings, three pairs of `raw*`/`*Page` types, a local
`parseAuthor` helper that returns `prs.Author`.

Sketch:

```go
package prconvo

import (
    "context"
    "fmt"
    "time"

    "github.com/karnstack/tempo/internal/github"
    "github.com/karnstack/tempo/internal/github/prs"
)

const (
    maxPageSize     = 100
    defaultPageSize = 50
    maxInlineComments = 100 // per-thread comments cap (reviewThreads pagination)
)

type Review struct {
    GHID        int64
    State       string
    SubmittedAt time.Time
    Author      prs.Author
}

type ReviewComment struct {
    GHID      int64
    Author    prs.Author
    CreatedAt time.Time
}

type IssueComment struct {
    GHID      int64
    Author    prs.Author
    CreatedAt time.Time
}

type ReviewsPage struct {
    Reviews   []Review
    HasNext   bool
    EndCursor string
}

type ReviewCommentsPage struct {
    Comments  []ReviewComment
    HasNext   bool
    EndCursor string
    Truncated bool
}

type IssueCommentsPage struct {
    Comments  []IssueComment
    HasNext   bool
    EndCursor string
}

type Fetcher struct{ c *github.Client }
func New(c *github.Client) *Fetcher { return &Fetcher{c: c} }

const reviewsQuery = `query($owner: String!, $repo: String!, $number: Int!, $first: Int!, $after: String) {
  repository(owner: $owner, name: $repo) {
    pullRequest(number: $number) {
      reviews(first: $first, after: $after) {
        pageInfo { hasNextPage endCursor }
        nodes {
          databaseId state submittedAt
          author {
            __typename login
            ... on User { databaseId }
            ... on Bot { databaseId }
            ... on Mannequin { databaseId }
          }
        }
      }
    }
  }
}`

const reviewCommentsQuery = `query($owner: String!, $repo: String!, $number: Int!, $first: Int!, $after: String) {
  repository(owner: $owner, name: $repo) {
    pullRequest(number: $number) {
      reviewThreads(first: $first, after: $after) {
        pageInfo { hasNextPage endCursor }
        nodes {
          comments(first: 100) {
            pageInfo { hasNextPage }
            nodes {
              databaseId createdAt
              author {
                __typename login
                ... on User { databaseId }
                ... on Bot { databaseId }
                ... on Mannequin { databaseId }
              }
            }
          }
        }
      }
    }
  }
}`

const issueCommentsQuery = `query($owner: String!, $repo: String!, $number: Int!, $first: Int!, $after: String) {
  repository(owner: $owner, name: $repo) {
    pullRequest(number: $number) {
      comments(first: $first, after: $after) {
        pageInfo { hasNextPage endCursor }
        nodes {
          databaseId createdAt
          author {
            __typename login
            ... on User { databaseId }
            ... on Bot { databaseId }
            ... on Mannequin { databaseId }
          }
        }
      }
    }
  }
}`
```

Each `Fetch*` method:
1. Clamps `first`.
2. Builds vars: `{owner, repo, number, first, after}` (after = nil when "").
3. Calls `f.c.GraphQL(ctx, query, vars, &raw)`.
4. Walks raw nodes → typed slice, fills page.
5. For review comments: walks `reviewThreads.nodes`, then nested
   `comments.nodes`, sets `Truncated` if any thread had `pageInfo.hasNextPage`.

Commit: `feat(github/prconvo): types + GraphQL queries + per-PR fetchers`

### 3. Cassette author (gen-tagged) + cassettes

Write `internal/github/prconvo/cassettes_gen_test.go` with
`//go:build gen` — authors the three happy-path cassettes plus one
error cassette. Closely model `prs/cassettes_gen_test.go`.

Run:

```sh
go test -tags=gen -run TestGen_Cassettes ./internal/github/prconvo/...
```

Verify the four `testdata/*.json` files are now on disk and parse via
`vcr.Cassette` (the save call already roundtrips).

Commit: `test(github/prconvo): VCR cassettes + gated authoring entrypoint`

### 4. Replay tests

Write `fetcher_test.go` + `fetcher_error_test.go`. Pattern after
`internal/github/prs/fetcher_test.go`:

- A `newReplayClient(t, cassettePath)` helper (or copy from prs — fine to
  duplicate; small).
- One subtest / function per cassette.
- Assertions: HasNext, EndCursor, length, individual node fields including
  Author shape (User / Bot / Mannequin / Ghost), Truncated flag for review
  comments.

Run `go test ./internal/github/prconvo/... -race -count=1`.

Commit: `test(github/prconvo): replay tests for reviews / review-comments / issue-comments`

### 5. Update `prs/doc.go`

Open `internal/github/prs/doc.go` and replace the "REVIEWS / REVIEW
COMMENTS / ISSUE COMMENTS will be added by task 0022, either by extending
this GraphQL query or by neighbouring fetchers" sentence with a pointer to
`internal/github/prconvo` (the neighbouring choice).

Commit: `docs(github/prs): point sub-resource note to prconvo package`

### 6. Verify

Rewrite `.plans/upnext/0022-reviews-comments-fetcher/verify.sh` (see below)
and run it. Capture last ~30 lines for RESULT.md.

Commit: `chore(plans/0022): verify.sh`

## Notes

- If a PR has >100 review threads OR a single thread has >100 inline
  comments, we'll under-report. Document this in `doc.go`. v2: bump the
  nested cap or paginate threads individually. Tracking debt for now is
  fine — tempo's small-team target rarely hits this.
- The `prs.Author` import creates an upward dependency from `prconvo` →
  `prs`. That's correct directionally (sub-resources depend on the PR
  module, not the other way around).
- We don't need a separate "since" parameter. The caller (0028) is the
  one applying since-semantics at the parent-PR level.
- Don't pre-emptively wire conditional ETag / `If-Modified-Since` here —
  this is GraphQL only, no ETags. That belongs to the commits fetcher
  (0023).
