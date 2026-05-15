// Package prs is the pull-request ingest runner. It walks every non-archived
// repo on a connection, pages GitHub's GraphQL `repository.pullRequests`
// (via internal/github/prs) ordered by UPDATED_AT DESC, upserts authors
// into gh_users and rows into pull_requests, and persists a per-repo
// high-water `updatedAt` cursor into sync_cursors so the next tick is
// incremental.
//
// One Runner instance lives per process and is shared across connections.
// The per-tick *github.Client (carrying the connection's PAT and live rate
// limiter) is injected by the scheduler on each Run.
//
// Cursor convention: one sync_cursors row per (connection_id,
// "prs:<owner>/<name>"). Value is RFC3339Nano UTC. v1 keeps repo identity
// in the resource string rather than a separate column; reviews/commits/
// deploys runners use the same convention with their own resource prefix.
//
// Per-repo failure isolation: a failure on one repo logs a warning and
// continues to the next. The first error is wrapped and returned at the
// end so the scheduler records it on sync_runs. Cursors for failed repos
// are NOT advanced; cursors for successful repos in the same Run ARE.
package prs
