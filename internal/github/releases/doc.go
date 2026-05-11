// Package releases is tempo's releases fetcher. It calls GitHub's REST
// `GET /repos/{owner}/{repo}/releases` with conditional `If-None-Match`
// ETag (a 304 response does NOT consume the rate-limit budget) and
// page-number pagination derived from the `Link` response header.
//
// It is the bottom layer of the deploys ingest pipeline alongside
// `internal/github/deployments`:
//
//   - PERSISTENCE and cursor STORAGE live in `internal/ingest` (task 0030),
//     not here. This package does not touch the database. The caller stores
//     the `etag` in `sync_cursors`; we just pass it through.
//   - PAGING THE WHOLE LIST is the caller's loop. We page one slice; the
//     caller decides whether to fetch the next page and when to commit
//     the cursor.
//   - 0030 will fold this fetcher's output together with deployments into
//     the unified `deployments` table (spec line 104). Releases that map
//     to a "release-as-deploy" event get one row; published releases
//     without a corresponding GitHub Deployment carry their own row.
//     Merging is 0030's job, not ours.
//
// # Wiring
//
//	c := github.New(token)
//	f := releases.New(c)
//
//	page, err := f.Fetch(ctx, "owner", "repo", releases.FetchOptions{
//	    ETag:    cachedETag,    // honoured only on Page<=1
//	    PerPage: 100,
//	})
//	if err != nil { return err }
//	if page.NotModified { /* nothing new since last poll */ return nil }
//	for {
//	    // ... persist page.Releases ...
//	    if !page.HasNext { break }
//	    page, err = f.Fetch(ctx, owner, repo, releases.FetchOptions{
//	        PerPage: 100,
//	        Page:    page.NextPage,
//	    })
//	    if err != nil { return err }
//	}
//	cachedETag = page.ETag // refresh cursor
//
// # ETag is the cursor
//
// The releases endpoint does NOT accept a `since` query param or any
// other filter. We rely on conditional ETag polling alone for the "did
// anything new arrive?" signal. Releases are extremely low-volume
// (typically one per deploy at most); a 304 per poll keeps the cost off
// the rate-limit budget entirely.
//
// On 304: `Page.NotModified=true`, `Page.Releases` empty,
// `Page.HasNext=false`, and `Page.ETag` echoes the caller's input.
//
// ETag is silently dropped when `opts.Page > 1` — each paginated URL has
// its own ETag on the server, and the first page is the one we care
// about caching.
//
// # Drafts and published_at
//
// A draft release has `published_at: null` in GitHub's JSON. The raw
// struct uses `*time.Time` and zero-values it on null, so callers see a
// zero `time.Time` for unpublished drafts. The `Draft` and `Prerelease`
// flags propagate verbatim.
//
// # Tests
//
// All tests in this package are hermetic VCR replays from
// `testdata/*.json`. Cassettes are hand-authored by a build-tag-gated
// `TestGen_Cassettes` (run `go test -tags=gen ./internal/github/releases/...`)
// — never recorded against the real GitHub API. CI replays them via the
// default (no-tag) build.
package releases
