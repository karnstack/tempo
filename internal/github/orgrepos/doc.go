// Package orgrepos is tempo's org-repos enumerator. It calls GitHub's
// REST `GET /orgs/{org}/repos` with conditional `If-None-Match` ETag
// (a 304 response does NOT consume the rate-limit budget) and
// page-number pagination derived from the `Link` response header.
//
// It is the bottom layer of the org-connection ingest path. When a
// user adds an `org` connection (spec line 92: `connections.kind ∈
// {repo, org}`, `name` nullable for orgs), the ingest worker
// (0026 / 0031) needs the current set of repos under that org so it
// can upsert `repos` rows (spec line 94) and dispatch per-repo
// fetchers (prs, commits, deployments, releases) against them. That
// enumeration is what this package provides.
//
//   - PERSISTENCE, dedup, and worker scheduling live in
//     `internal/ingest`, not here. This package does not touch the
//     database. The caller stores `etag` in `sync_cursors`; we just
//     pass it through.
//   - PAGING THE WHOLE LIST is the caller's loop. We page one slice;
//     the caller decides whether to fetch the next page and when to
//     commit the cursor.
//
// # Wiring
//
//	c := github.New(token)
//	f := orgrepos.New(c)
//
//	page, err := f.Fetch(ctx, "karnstack", orgrepos.FetchOptions{
//	    ETag:    cachedETag, // honoured only on Page<=1
//	    PerPage: 100,
//	})
//	if err != nil { return err }
//	if page.NotModified { /* nothing new since last poll */ return nil }
//	for {
//	    // ... upsert page.Repos ...
//	    if !page.HasNext { break }
//	    page, err = f.Fetch(ctx, "karnstack", orgrepos.FetchOptions{
//	        PerPage: 100,
//	        Page:    page.NextPage,
//	    })
//	    if err != nil { return err }
//	}
//	cachedETag = page.ETag // refresh cursor
//
// # ETag is the cursor
//
// The `/orgs/{org}/repos` endpoint does NOT accept a `since` query
// param or any other date filter. We rely on conditional ETag polling
// alone for the "did anything new arrive?" signal. Org repo lists are
// small and change rarely; a 304 per poll keeps the cost off the
// rate-limit budget entirely.
//
// On 304: `Page.NotModified=true`, `Page.Repos` empty,
// `Page.HasNext=false`, and `Page.ETag` echoes the caller's input
// when the server omits its own ETag.
//
// ETag is silently dropped when `opts.Page > 1` — each paginated URL
// has its own ETag on the server, and the first page is the one we
// care about caching.
//
// # Type filter
//
// `FetchOptions.Type` maps to the `type` query param: `all` (default
// when empty), `public`, `private`, `forks`, `sources`, `member`. We
// don't validate — GitHub returns 422 for invalid values and that
// propagates as `*github.HTTPError`.
//
// # Slim Repo struct
//
// Only fields the ingest path actually needs:
//
//   - `GHID`, `Owner`, `Name`, `DefaultBranch`, `Archived` — the
//     spec's `repos` table columns (line 94).
//   - `Fork`, `Private` — ubiquitous filter inputs for the worker
//     (skip forks; surface private).
//
// `pushed_at`, `Disabled`, and the full `owner` object are skipped
// (YAGNI). Add them only when an ingest task actually needs them.
//
// # Tests
//
// All tests in this package are hermetic VCR replays from
// `testdata/*.json`. Cassettes are hand-authored by a build-tag-gated
// `TestGen_Cassettes` (run `go test -tags=gen ./internal/github/orgrepos/...`)
// — never recorded against the real GitHub API. CI replays them via
// the default (no-tag) build.
package orgrepos
