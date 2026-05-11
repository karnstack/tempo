// Package prconvo is tempo's per-PR conversation fetcher: pull-request
// reviews, review comments (inline diff comments), and PR issue comments
// (the conversation tab). Each resource is a separate GraphQL query on
// `repository.pullRequest(number)` — paginated cursor-by-cursor, returning
// flat slices of typed nodes. Reuses `internal/github/prs.Author` so the
// actor shape (User / Bot / Mannequin / Ghost) is identical across the
// PR pipeline.
//
// It is the bottom layer of the reviews/comments ingest pipeline, sitting
// beside `internal/github/prs` (task 0021):
//
//   - PERSISTENCE and per-PR LOOP STATE live in `internal/ingest` (task
//     0028). This package does not touch the database and does not know
//     which PR is "next".
//   - PAGING THE WHOLE CONNECTION is the caller's loop. We page one slice;
//     the caller decides whether to fetch the next page.
//   - NO `since` CUTOFF. GitHub's review/comment connections come back ASC
//     with no `orderBy` knob, so a since-based short-circuit doesn't help.
//     The intended use is: ingest sees a PR whose parent `updatedAt`
//     crossed the cursor, re-fetches all sub-resources for that PR, and
//     upserts by `gh_id`.
//
// # Wiring
//
//	c := github.New(token)
//	f := prconvo.New(c)
//
//	for after := ""; ; {
//	    page, err := f.FetchReviews(ctx, owner, repo, number, after, 100)
//	    if err != nil { return err }
//	    // ... upsert page.Reviews ...
//	    if !page.HasNext { break }
//	    after = page.EndCursor
//	}
//
// FetchReviewComments and FetchIssueComments follow the same shape.
//
// # Reviews
//
// FetchReviews paginates `pullRequest.reviews`. Each Review carries the
// GraphQL `state` enum verbatim — `APPROVED`, `CHANGES_REQUESTED`,
// `COMMENTED`, `DISMISSED`. PENDING reviews (drafts) aren't visible via
// the API for tokens that don't own them; if one does come back, its
// `SubmittedAt` is nil.
//
// # Review comments
//
// GitHub's GraphQL schema has no flat `PullRequest.reviewComments`
// connection. We paginate `pullRequest.reviewThreads` and inline each
// thread's `comments(first: 100)` into a flat slice. If any thread has
// more than 100 inline comments, the page's `Truncated` flag is set so
// the caller can warn or backfill via REST. For tempo's small-team
// target this almost never happens.
//
// # Issue comments
//
// FetchIssueComments paginates `pullRequest.comments` — the same
// connection the GitHub web UI calls "Conversation". Despite the name,
// these aren't issue records; they're comments attached to the PR.
//
// # Tests
//
// All tests in this package are hermetic VCR replays from
// `testdata/*.json`. Cassettes are hand-authored by a build-tag-gated
// `TestGen_Cassettes` (run `go test -tags=gen ./internal/github/prconvo/...`)
// — never recorded against the real GitHub API. CI replays them via the
// default (no-tag) build.
package prconvo
