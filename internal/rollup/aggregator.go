package rollup

import (
	"context"
	"time"
)

// Aggregator rolls up one slice of data (engineer stats, repo stats,
// etc.) for a single local date. The date argument is local-midnight in
// the scheduler's configured timezone — implementations format it as
// they need. Returning an error logs the failure and records a failed
// rollup_runs row but does not abort sibling aggregators.
type Aggregator interface {
	Name() string
	Aggregate(ctx context.Context, date time.Time) error
}

// NoopAggregator is an Aggregator that does nothing. It exists for
// tests and so the "no aggregators wired" state stays observable in
// logs without special-casing the empty-slice path.
type NoopAggregator struct{}

func (NoopAggregator) Name() string                                { return "noop" }
func (NoopAggregator) Aggregate(context.Context, time.Time) error { return nil }
