# 0021 — PR fetcher (GraphQL with cursors) — RESULT

## Summary

Added `internal/github/prs` — a stateless GraphQL pull-request fetcher that
pages `repository.pullRequests` ordered by `UPDATED_AT DESC`, surfaces the
GraphQL cursor (`hasNextPage` / `endCursor`), and short-circuits when the
page crosses a caller-supplied `since` cutoff. No DB, no looping — those are
0027's job.

Persistence-side ingest (0027) can now drive the loop:

```go
f := prs.New(githubClient)
for after := ""; ; {
    page, err := f.Fetch(ctx, "owner", "repo", after, 100, lastSeenUpdatedAt)
    // ... persist page.PRs, commit page.EndCursor ...
    if !page.HasNext || page.ReachedSince { break }
    after = page.EndCursor
}
```

## Files added

- `internal/github/prs/doc.go` — package overview, wiring example, test
  policy (hermetic replay via the existing `internal/github/vcr` layer).
- `internal/github/prs/fetcher.go` — `PR`, `Author`, `Page`, `Fetcher`,
  `New`, `Fetch`. `listQuery` const carries the GraphQL query string. Raw
  envelope types parse JSON; `toPR()` + `parse()` map to public types.
- `internal/github/prs/fetcher_test.go` — three replay tests:
  - `TestFetch_Page` — single page, all four author shapes (User, Bot,
    Mannequin, Ghost), `MergedAt`/`ClosedAt` set vs nil, `Draft` true/false,
    `HasNext` + `EndCursor` propagation. Also exercises the upper page-size
    clamp by calling with `first=999` against a cassette recorded at
    `first=100`.
  - `TestFetch_SinceCutoff` — three PRs ordered DESC by `updatedAt`; the
    cassette's third PR has `updatedAt == since`, exercising the strict-
    greater rule (dropped + `ReachedSince=true`).
  - `TestFetch_GraphQLError` — error envelope surfaces as
    `*github.GraphQLError` unchanged.
- `internal/github/prs/cassettes_gen_test.go` (`//go:build gen`) — gated
  authoring entrypoint. Builds `vcr.Cassette`s directly so the request body
  stays in sync with `listQuery` automatically. Mirrors the pattern in
  `internal/github/vcr/record_demo_test.go`.
- `internal/github/prs/testdata/list_page.json` — 4-node page, mixed
  author types, `hasNextPage=true`.
- `internal/github/prs/testdata/list_since_cutoff.json` — 3-node page,
  one node exactly on the cutoff.
- `internal/github/prs/testdata/list_graphql_error.json` — NOT_FOUND
  error envelope.

## Design highlights

- **Per-page, not iterator.** 0027 wants to persist the cursor after every
  successful page; iterators hide that boundary. `Page` exposes `HasNext`
  + `EndCursor` and a `ReachedSince` signal for stopping.
- **Client-side `since` filter.** GitHub's `pullRequests` connection has no
  native `updatedAt` filter, only `orderBy`. We order DESC and stop on the
  first node at-or-before the cutoff.
- **Strict-greater cutoff.** `UpdatedAt <= since` drops the PR. Equality
  means "caller already has this PR's current state".
- **Author union compresses to one struct.** `Author{GHID, Login, Type}` —
  `Type` is GraphQL `__typename` ("User"|"Bot"|"Mannequin"); `null` author
  becomes `Author{Type:"Ghost"}` with `GHID=0`. Unhandled actor types
  (EnterpriseUserAccount, Organization) degrade to `Login`+`Type` with
  `GHID=0` — 0027 can flag for review.
- **Page-size clamping.** `first <= 0` → 50; `first > 100` → 100 (GraphQL
  hard limit). Cassettes are recorded at the clamped value, so the test
  passing `first=999` is the upper-bound assertion.
- **Cassette authoring is reproducible.** The `gen` build tag is the
  established pattern (sibling: `internal/github/vcr/record_demo_test.go`
  with `record`). Re-run after any `listQuery` change.

## Verify output (last lines)

```
==> go vet ./internal/github/...
==> go build ./...
==> go test ./internal/github/prs/... -race -count=1
ok  	github.com/karnstack/tempo/internal/github/prs	1.349s
==> go test ./internal/github/... -race -count=1 (no regressions)
ok  	github.com/karnstack/tempo/internal/github	1.614s
ok  	github.com/karnstack/tempo/internal/github/prs	1.897s
ok  	github.com/karnstack/tempo/internal/github/vcr	2.472s
==> compile check: -tags=record ./internal/github/prs/...
VERIFY OK
```

## Followups (out of scope here)

- **0022** will extend the GraphQL query (or add neighbour fetchers) for
  reviews, review comments, and issue comments. The `PR` struct + `listQuery`
  are designed to grow inline — add nested connections, extend `rawPR`,
  surface new public fields.
- **0026/0027** will provide `fx.Provide(prs.New)` once the worker scheduler
  + PR ingest land. No fx wiring shipped here (no consumer yet).
- The PR-author union currently parses three concrete types (User, Bot,
  Mannequin). If real fixture data turns up `EnterpriseUserAccount` or
  `Organization` PRs in v1, add inline fragments and re-run the `gen` tag.
- Re-recording against the real GitHub GraphQL endpoint isn't wired up —
  the cassettes are hand-authored. If/when we want a live-recorded variant,
  follow the template in `internal/github/vcr/record_demo_test.go` and swap
  the fake upstream for `github.New(token, WithHTTPClient(vcrTransport))`.
