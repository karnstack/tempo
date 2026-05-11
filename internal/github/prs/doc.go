// Package prs is tempo's pull-request fetcher. It calls GitHub's GraphQL
// `repository.pullRequests` connection ordered by `UPDATED_AT DESC`, returns
// one page at a time with the GraphQL cursor (`endCursor` / `hasNextPage`),
// and short-circuits when the page reaches a caller-supplied `since` cutoff
// so incremental polls stop walking the history.
//
// It is the bottom layer of the PR ingest pipeline:
//
//   - PERSISTENCE and cursor STORAGE live in `internal/ingest` (task 0027),
//     not here. This package does not touch the database.
//   - PAGING THE WHOLE HISTORY is the caller's loop. We page one slice; the
//     caller decides whether to fetch the next page and how/when to commit
//     the cursor.
//   - REVIEWS / REVIEW COMMENTS / ISSUE COMMENTS will be added by task 0022,
//     either by extending this GraphQL query or by neighbouring fetchers.
//
// # Wiring
//
//	c := github.New(token)
//	f := prs.New(c)
//	for after, since := "", lastSeenUpdatedAt; ; {
//	    page, err := f.Fetch(ctx, "owner", "repo", after, 100, since)
//	    if err != nil { return err }
//	    // ... persist page.PRs ...
//	    if !page.HasNext || page.ReachedSince { break }
//	    after = page.EndCursor
//	}
//
// # Tests
//
// Tests under this package are hermetic — they replay hand-authored cassettes
// from `testdata/*.json` via `internal/github/vcr`. CI never hits the network.
// To re-record (rare; only when GitHub's schema or our query changes), follow
// the workflow documented in `internal/github/vcr/doc.go`.
package prs
