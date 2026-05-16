package commits

import (
	"context"
	"time"

	"github.com/karnstack/tempo/internal/github"
	"github.com/karnstack/tempo/internal/ingest"
	"github.com/karnstack/tempo/internal/storage/sqlite/sqlitedb"
	"go.uber.org/zap"
)

// Runner ingests default-branch commits for one connection per call to Run.
type Runner struct {
	q   *sqlitedb.Queries
	log *zap.Logger
	now func() time.Time
}

// Option mutates a Runner after New. Used by tests to inject fakes.
type Option func(*Runner)

// WithClock overrides time.Now. Useful for deterministic cursor.updated_at
// timestamps in tests.
func WithClock(now func() time.Time) Option { return func(r *Runner) { r.now = now } }

// New builds a Runner with production defaults.
func New(q *sqlitedb.Queries, l *zap.Logger, opts ...Option) *Runner {
	r := &Runner{q: q, log: l, now: time.Now}
	for _, o := range opts {
		o(r)
	}
	return r
}

// Name implements ingest.Runner.
func (*Runner) Name() string { return "commits" }

// Run implements ingest.Runner. The scheduler hands us a per-connection
// *github.Client; we own the per-repo iteration.
func (r *Runner) Run(ctx context.Context, conn sqlitedb.Connection, gh *github.Client) (ingest.Outcome, error) {
	return ingest.Outcome{}, nil
}
