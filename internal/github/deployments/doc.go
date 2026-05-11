// Package deployments is tempo's deployments fetcher. It calls GitHub's
// REST `GET /repos/{owner}/{repo}/deployments` with conditional
// `If-None-Match` ETag (a 304 response does NOT consume the rate-limit
// budget) and page-number pagination derived from the `Link` response
// header.
//
// It is the bottom layer of the deploys ingest pipeline:
//
//   - PERSISTENCE and cursor STORAGE live in `internal/ingest` (task 0030),
//     not here. This package does not touch the database. The caller stores
//     the `etag` in `sync_cursors`; we just pass it through.
//   - PAGING THE WHOLE LIST is the caller's loop. We page one slice; the
//     caller decides whether to fetch the next page and when to commit the
//     cursor.
//   - 0030 will also fetch from `internal/github/releases` (the other half
//     of the spec's "Deployments + Releases" source) and merge both shapes
//     into the unified `deployments` table.
//
// # Wiring
//
//	c := github.New(token)
//	f := deployments.New(c)
//
//	page, err := f.Fetch(ctx, "owner", "repo", deployments.FetchOptions{
//	    Environment: "production",  // optional filter
//	    ETag:        cachedETag,    // honoured only on Page<=1
//	    PerPage:     100,
//	})
//	if err != nil { return err }
//	if page.NotModified { /* nothing new since last poll */ return nil }
//	for {
//	    // ... persist page.Deployments ...
//	    if !page.HasNext { break }
//	    page, err = f.Fetch(ctx, owner, repo, deployments.FetchOptions{
//	        Environment: "production",
//	        PerPage:     100,
//	        Page:        page.NextPage,
//	    })
//	    if err != nil { return err }
//	}
//	cachedETag = page.ETag // refresh cursor
//
// # ETag is the cursor
//
// The deployments endpoint does NOT accept a `since` query param, so we
// rely on conditional ETag polling alone for the "did anything new
// arrive?" signal. Deploys are low-volume; a 304 per poll keeps the cost
// off the rate-limit budget entirely.
//
// On 304: `Page.NotModified=true`, `Page.Deployments` empty,
// `Page.HasNext=false`, and `Page.ETag` echoes the caller's input (the
// server sometimes omits ETag on 304).
//
// ETag is silently dropped when `opts.Page > 1` — each paginated URL has
// its own ETag on the server, and the first page is the one we actually
// care about caching.
//
// # No deployment statuses
//
// GitHub's list-deployments endpoint does NOT return the latest
// deployment_status (success / failure / in_progress / inactive). The
// `status` column in the spec's `deployments` table is populated, if at
// all, by a per-deployment call to
// `GET /repos/{owner}/{repo}/deployments/{id}/statuses` — which is the
// caller's choice. This package stays surgical: one endpoint, one
// request per page.
//
// # Tests
//
// All tests in this package are hermetic VCR replays from
// `testdata/*.json`. Cassettes are hand-authored by a build-tag-gated
// `TestGen_Cassettes` (run `go test -tags=gen ./internal/github/deployments/...`)
// — never recorded against the real GitHub API. CI replays them via the
// default (no-tag) build.
package deployments
