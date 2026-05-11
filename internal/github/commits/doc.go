// Package commits is tempo's default-branch commits fetcher. It calls
// GitHub's REST `GET /repos/{owner}/{repo}/commits` with a `since=<RFC3339>`
// cursor, conditional `If-None-Match` ETag (a 304 response does NOT
// consume the rate-limit budget), and page-number pagination derived from
// the `Link` response header.
//
// It is the bottom layer of the commits ingest pipeline:
//
//   - PERSISTENCE and cursor STORAGE live in `internal/ingest` (task 0029),
//     not here. This package does not touch the database. The caller stores
//     `(since, etag)` together in `sync_cursors`; we just pass them
//     through.
//   - PAGING THE WHOLE BRANCH is the caller's loop. We page one slice; the
//     caller decides whether to fetch the next page and how/when to commit
//     the cursor.
//
// # Wiring
//
//	c := github.New(token)
//	f := commits.New(c)
//
//	page, err := f.Fetch(ctx, "owner", "repo", commits.FetchOptions{
//	    Since:   lastSync,
//	    ETag:    cachedETag, // honoured only on Page<=1
//	    PerPage: 100,
//	})
//	if err != nil { return err }
//	if page.NotModified { /* nothing new since last poll */ return nil }
//	for {
//	    // ... persist page.Commits ...
//	    if !page.HasNext { break }
//	    page, err = f.Fetch(ctx, owner, repo, commits.FetchOptions{
//	        Since:   lastSync,
//	        PerPage: 100,
//	        Page:    page.NextPage,
//	    })
//	    if err != nil { return err }
//	}
//	cachedETag = page.ETag // refresh cursor
//
// # since + ETag compose
//
// The server keys its ETag on the full request URL, which includes the
// `since` query param. Keeping `since` stable across polls keeps the
// cached ETag valid; 304s flow until a new commit lands on the branch. On
// 304: `Page.NotModified=true`, `Page.Commits` empty, `Page.HasNext=false`,
// and `Page.ETag` echoes the caller's input (the server sometimes omits
// ETag on 304).
//
// ETag is silently dropped when `opts.Page > 1` — each paginated URL has
// its own ETag on the server, and the first page is the one we actually
// care about caching for "did anything new arrive?".
//
// # No additions / deletions
//
// GitHub's list-commits endpoint does NOT return per-commit
// additions/deletions. Those columns in the `commits` table (see the
// spec's data model) are populated, if at all, by a per-SHA detail call
// on top of this fetcher — `GET /repos/{owner}/{repo}/commits/{sha}` —
// which is the caller's choice to make. This package stays surgical:
// one endpoint, one request per page.
//
// # Tests
//
// All tests in this package are hermetic VCR replays from
// `testdata/*.json`. Cassettes are hand-authored by a build-tag-gated
// `TestGen_Cassettes` (run `go test -tags=gen ./internal/github/commits/...`)
// — never recorded against the real GitHub API. CI replays them via the
// default (no-tag) build.
package commits
