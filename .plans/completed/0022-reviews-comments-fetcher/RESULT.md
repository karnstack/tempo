# 0022 — Reviews + review-comments + issue-comments fetchers

## Summary

New package `internal/github/prconvo` with one stateless `Fetcher` and
three per-PR sub-resource methods, mirroring the `internal/github/prs`
(0021) pattern: hand-authored VCR cassettes gated behind `//go:build
gen`, hermetic replay tests in CI.

- `FetchReviews` → `pullRequest.reviews` (cursor + flat nodes).
- `FetchReviewComments` → `pullRequest.reviewThreads { comments(first:
  100) }`, flattened. `Truncated` flag set when any thread overflowed
  the 100-comment inline cap.
- `FetchIssueComments` → `pullRequest.comments` (cursor + flat nodes).

All three reuse `prs.Author` for the actor shape (User / Bot /
Mannequin / Ghost). Local raw structs + a tiny `parseAuthor` helper
unmarshal the GraphQL `author` union without dragging unexported names
out of `prs`.

`SubmittedAt` on `Review` is `*time.Time` (nil for the rare PENDING
review that's visible to the token holder); `CreatedAt` on
`ReviewComment` / `IssueComment` is non-nullable.

Updated `internal/github/prs/doc.go` to point its forward-reference at
the new `prconvo` package.

## Files changed

- `internal/github/prconvo/doc.go` (new)
- `internal/github/prconvo/fetcher.go` (new — types, queries, three
  Fetch methods, raw structs, author parser)
- `internal/github/prconvo/fetcher_test.go` (new — three happy-path
  replay tests)
- `internal/github/prconvo/fetcher_error_test.go` (new — GraphQL error
  passthrough)
- `internal/github/prconvo/cassettes_gen_test.go` (new — gen-tagged
  cassette author)
- `internal/github/prconvo/testdata/reviews_page.json` (new)
- `internal/github/prconvo/testdata/review_comments_page.json` (new)
- `internal/github/prconvo/testdata/issue_comments_page.json` (new)
- `internal/github/prconvo/testdata/reviews_graphql_error.json` (new)
- `internal/github/prs/doc.go` (edit — replace 0022 forward-reference)
- `.plans/upnext/0022-reviews-comments-fetcher/{TASK.md, verify.sh}`
  (rewrite)

## Verify

```
==> go vet ./internal/github/...
==> go build ./...
==> go test ./internal/github/prconvo/... -race -count=1
ok  	github.com/karnstack/tempo/internal/github/prconvo	1.380s
==> go test ./internal/github/... -race -count=1 (no regressions)
ok  	github.com/karnstack/tempo/internal/github	1.371s
ok  	github.com/karnstack/tempo/internal/github/prconvo	1.630s
ok  	github.com/karnstack/tempo/internal/github/prs	1.902s
ok  	github.com/karnstack/tempo/internal/github/vcr	2.209s
==> compile check: -tags=record ./internal/github/prconvo/...
==> compile check: -tags=gen ./internal/github/prconvo/...
VERIFY OK
```

## Followups (not blockers)

- **Per-thread overflow handling**: when `ReviewCommentsPage.Truncated`
  is set, the caller (0028 ingest) currently has no way to backfill the
  >100 comments on the overflowing thread. v2 could add
  `FetchThreadComments(threadID, after, first)` if real-world ingest
  shows this matters.
- **Author shape lift**: `prs.Author` is now imported by two sibling
  packages. If a third caller emerges (0023 commits fetcher? 0024
  deploys?), lift to `internal/github/actor` and remove the cross-sibling
  dependency.
- **Query fusion**: spec line 128 prefers a single PR-detail query
  pulling reviews + review comments + issue comments + commits + labels
  together. We chose three small queries (~3 GraphQL points/PR vs ~1)
  for clarity. If ingest profiling later flags rate limit pressure,
  collapse into one fused query.
