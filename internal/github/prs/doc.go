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
//   - REVIEWS / REVIEW COMMENTS / ISSUE COMMENTS live in the neighbouring
//     `internal/github/prconvo` package (task 0022) — three GraphQL
//     queries on `pullRequest.{reviews,reviewThreads,comments}`, paged
//     independently. Callers compose: list PRs here, fetch sub-resources
//     there for each PR whose `updatedAt` crossed the cursor.
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
