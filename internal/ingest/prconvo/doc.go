// Package prconvo is the PR-conversation ingest runner. For every PR whose
// `pull_requests.updated_at` is newer than the per-repo cursor, it pages
// three GraphQL sub-resources (via internal/github/prconvo) — reviews,
// review-thread comments, and conversation-tab issue comments — upserts
// the authors into gh_users, and writes rows into pr_reviews,
// pr_review_comments, pr_issue_comments.
//
// One Runner instance lives per process and is shared across connections.
// The per-tick *github.Client (carrying the connection's PAT and live rate
// limiter) is injected by the scheduler on each Run.
//
// Cursor convention: one sync_cursors row per (connection_id,
// "prconvo:<owner>/<name>"). Value is RFC3339Nano UTC. A single key
// covers all three sub-resources because they are co-fetched per PR;
// advancing them atomically keeps re-fetch behaviour predictable on
// partial failure (upserts are idempotent).
//
// Per-repo failure isolation: a failure mid-repo logs a warning and
// continues to the next repo. The first error is wrapped (with
// owner/name and the failing PR number) and returned at the end so the
// scheduler records it on sync_runs. Cursors for failed repos are NOT
// advanced; cursors for repos that completed earlier in the same Run
// ARE.
package prconvo
