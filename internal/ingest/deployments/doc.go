// Package deployments is tempo's GitHub-Deployments ingest runner. It is
// the bottom layer of the deploys sync pipeline: per tick, per
// connection, per non-archived repo, the runner issues one REST
// `GET /repos/{owner}/{repo}/deployments?per_page=100&page=1` with
// `If-None-Match: <etag>` and pages through `Link: rel="next"` until
// either history is exhausted or the page boundary crosses the cursor's
// `since` horizon (deploys come back newest-first).
//
// Deploy rows are upserted into the `deployments` table keyed on `gh_id`.
// The cursor row in `sync_cursors` is keyed `deployments:<owner>/<name>`
// with a composite value `"<RFC3339Nano since>|<etag>"`. Splitting on the
// first `|` is unambiguous — RFC 7232 forbids `|` in etag content. Legacy
// single-component (bare-timestamp, no `|`) cursors parse as
// `(since, etag="")`.
//
// # Cursor advance rules
//
//   - 304 NotModified: no upserts, no cursor write — the existing
//     (since, etag) pair stays valid for the next poll.
//   - 200 OK with N>0 new deploys: cursor advances to
//     (max(d.CreatedAt), page1.ETag). "New" means `d.CreatedAt > since`.
//   - 200 OK with 0 new deploys (empty body OR all `created_at <= since`):
//     since stays put, etag refreshes from the page-1 response — the next
//     poll's `If-None-Match` stays current. Because the request URL has
//     no `since=` query param, the server-side etag is keyed on a stable
//     URL across polls, so always saving the freshest etag is the right
//     thing.
//
// Per-repo failure isolation mirrors the prs/commits runners: a failure
// on one repo logs a warning and continues to the next; the first error
// is wrapped with `owner/name` and returned at the end. Failed repos'
// cursors are NOT advanced.
//
// # Scope decisions
//
// Spec line 104 says the `deployments` table is "sourced from GitHub
// Deployments + Releases". This runner ingests GitHub Deployments only;
// Releases-as-deploys is deferred to a follow-up task because the
// mapping (environment, ref→sha resolution, gh_id collision risk between
// Deployment and Release ID spaces) deserves its own design decision.
//
// Deployment `status` is stored as the empty string in v1 — the list
// endpoint omits it (see `internal/github/deployments/doc.go`). The
// rollup (0034) just counts deploy rows regardless of status.
package deployments
