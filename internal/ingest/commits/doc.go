// Package commits is tempo's default-branch commits ingest runner. It is
// the bottom layer of the commits sync pipeline: per tick, per connection,
// per non-archived repo, the runner issues one REST
// `GET /repos/{owner}/{repo}/commits?since=<cursor>&per_page=100&page=1`
// with `If-None-Match: <etag>` and pages through `Link: rel="next"` until
// the listing is exhausted.
//
// Commits are upserted into the `commits` table; authors and committers
// are interned into `gh_users`. The cursor row in `sync_cursors` is keyed
// `commits:<owner>/<name>` with a composite value
// `"<RFC3339Nano since>|<etag>"`. Splitting on the first `|` is
// unambiguous — RFC 7232 forbids `|` in etag content. Legacy single-
// component (bare-timestamp, no `|`) cursors parse as `(since, etag="")`.
//
// # Cursor advance rules
//
//   - 304 NotModified: no upserts, no cursor write — the existing
//     (since, etag) pair stays valid for the next poll.
//   - 200 OK with N>0 commits: cursor advances to
//     (max(c.AuthoredAt), etag=""). Etag is cleared because the server-
//     side etag is keyed on the full URL, and the since-bump invalidates
//     it.
//   - 200 OK with 0 commits: cursor's since stays put, etag refreshes from
//     the page-1 response — the next poll's `If-None-Match` stays current.
//
// Per-repo failure isolation mirrors the prs runner: a failure on one
// repo logs a warning and continues to the next; the first error is
// wrapped with `owner/name` and returned at the end. Failed repos' cursors
// are NOT advanced.
//
// Additions and deletions on the `commits` table are not populated by
// this runner — GitHub's list endpoint omits them; see
// `internal/github/commits/doc.go`.
package commits
