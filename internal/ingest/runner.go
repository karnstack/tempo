package ingest

import (
	"context"

	"github.com/karnstack/tempo/internal/github"
	"github.com/karnstack/tempo/internal/storage/sqlite/sqlitedb"
)

// Outcome is what a Runner reports back to the scheduler after syncing one
// resource for one connection. Items is the count of rows written or
// upserted in this run; RateLimitRemaining is the worst-observed
// X-RateLimit-Remaining seen during the run, or nil if the runner made no
// HTTP calls (e.g. nothing was due to refresh).
type Outcome struct {
	Items              int64
	RateLimitRemaining *int64
}

// Runner ingests one resource (PRs, reviews, commits, deploys, ...) for one
// connection. Implementations live in their own packages and are wired into
// the scheduler via the fx value group `"ingest.runners"`.
//
// Returning an error short-circuits the rest of this connection's runners
// for the current tick. It does NOT abort the tick — the scheduler moves on
// to the next connection. The error is persisted onto the connection's
// sync_runs row, prefixed with the runner's Name.
type Runner interface {
	Name() string
	Run(ctx context.Context, conn sqlitedb.Connection, gh *github.Client) (Outcome, error)
}

// NoopRunner is a Runner that does nothing. It exists for tests and to make
// the scheduler's "zero runners wired" state observable (Name() shows up in
// logs).
type NoopRunner struct{}

func (NoopRunner) Name() string { return "noop" }

func (NoopRunner) Run(context.Context, sqlitedb.Connection, *github.Client) (Outcome, error) {
	return Outcome{}, nil
}
