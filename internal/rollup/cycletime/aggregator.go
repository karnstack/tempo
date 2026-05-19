// Package cycletime implements the rollup.Aggregator that populates the
// two lead_time_seconds_* columns of daily_repo_stats from per-PR
// merge-cycle durations.
//
// The metric for a (date, repo) row is the p50 / p90 of
// `merged_at - created_at` over every PR merged on that local date.
// "Cycle time" and "lead time" collapse to the same number in v1 — we
// don't yet link PRs to deployments, so the DORA commit-to-deploy lead
// time is out of scope.
//
// The two columns this aggregator owns are intentionally disjoint from
// repostats (0034): the underlying UpsertRepoLeadTime query INSERTs
// only the two lead-time columns (counts fall back to schema DEFAULT 0)
// and the ON CONFLICT DO UPDATE clause never touches the count columns.
// That lets the two sibling aggregators run in any order without
// trampling each other under rollup.Run.
package cycletime

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"

	"github.com/karnstack/tempo/internal/storage"
	"github.com/karnstack/tempo/internal/storage/sqlite/sqlitedb"
	"go.uber.org/zap"
)

const aggregatorName = "cycle_time"

// Aggregator wires UpsertRepoLeadTime through the rollup.Aggregator
// interface. Same skeleton as the engineerstats / repostats siblings;
// the per-repo body is the only thing that differs.
type Aggregator struct {
	db  *sql.DB
	q   *sqlitedb.Queries
	log *zap.Logger
}

// New builds an Aggregator from the shared Storage. fx wiring lives in
// module.go.
func New(s storage.Storage, l *zap.Logger) *Aggregator {
	db := s.DB()
	return &Aggregator{db: db, q: sqlitedb.New(db), log: l}
}

// Name implements rollup.Aggregator.
func (*Aggregator) Name() string { return aggregatorName }

// Aggregate rebuilds the lead-time percentiles on daily_repo_stats for
// `date` across every non-archived repo. Per-repo errors are logged and
// the first one is returned, but they never short-circuit sibling
// repos — matches the scheduler's policy.
//
// `date` is local-midnight in the scheduler's tz. The window is
// [date, date+24h) on merged_at.
func (a *Aggregator) Aggregate(ctx context.Context, date time.Time) error {
	dateStr := date.Format("2006-01-02")
	fromTS := date
	toTS := date.AddDate(0, 0, 1)

	repos, err := a.q.ListAllRepos(ctx)
	if err != nil {
		return fmt.Errorf("list repos: %w", err)
	}

	var firstErr error
	for _, r := range repos {
		if r.Archived != 0 {
			continue
		}
		if err := a.aggregateRepo(ctx, r.ID, dateStr, fromTS, toTS); err != nil {
			a.log.Warn("rollup/cycle_time: repo failed",
				zap.Int64("repo_id", r.ID),
				zap.String("date", dateStr),
				zap.Error(err))
			if firstErr == nil {
				firstErr = fmt.Errorf("repo %d: %w", r.ID, err)
			}
		}
	}
	return firstErr
}

// aggregateRepo computes the p50/p90 of merge cycle durations for one
// repo on one day and UPSERTs the result. A repo with zero merged PRs
// in the window UPSERTs both percentiles to NULL — this is the
// idempotency story when source data disappears (e.g. PR re-classified
// or deleted): a previous run that wrote percentiles must be cleared.
func (a *Aggregator) aggregateRepo(ctx context.Context, repoID int64, dateStr string, from, to time.Time) error {
	// FromTs/ToTs are *time.Time because they're compared against the
	// nullable `merged_at` column; sqlc inherits the column's nullability
	// for inferred param types.
	prs, err := a.q.ListMergedPullRequestsByRepoBetween(ctx, sqlitedb.ListMergedPullRequestsByRepoBetweenParams{
		RepoID: repoID,
		FromTs: &from,
		ToTs:   &to,
	})
	if err != nil {
		return fmt.Errorf("list merged prs: %w", err)
	}

	durations := make([]int64, 0, len(prs))
	for _, pr := range prs {
		// MergedAt is NOT NULL by query design, but defend against
		// driver-side surprises (and make this loop self-evidently
		// correct).
		if pr.MergedAt == nil {
			continue
		}
		d := int64(pr.MergedAt.Sub(pr.CreatedAt) / time.Second)
		if d < 0 {
			// Clock-skew rows would skew low percentiles; GitHub
			// shouldn't produce them but ingest is the wider contract.
			continue
		}
		durations = append(durations, d)
	}

	var p50, p90 *int64
	if len(durations) > 0 {
		sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
		v50 := percentile(durations, 50)
		v90 := percentile(durations, 90)
		p50 = &v50
		p90 = &v90
	}

	if err := a.q.UpsertRepoLeadTime(ctx, sqlitedb.UpsertRepoLeadTimeParams{
		Date:               dateStr,
		RepoID:             repoID,
		LeadTimeSecondsP50: p50,
		LeadTimeSecondsP90: p90,
	}); err != nil {
		return fmt.Errorf("upsert lead time: %w", err)
	}
	return nil
}

// percentile returns the nearest-rank percentile of a sorted slice.
// q is in [0, 100]. The caller must pre-sort ascending and guarantee
// len(sorted) > 0.
//
// Definition: idx = ceil(q*n/100) - 1, clamped to [0, n-1]. With one
// sample p50 = p90 = sorted[0]; with four samples [100,200,300,400]
// p50 = sorted[1] = 200 and p90 = sorted[3] = 400.
func percentile(sorted []int64, q int) int64 {
	n := len(sorted)
	idx := (q*n+99)/100 - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= n {
		idx = n - 1
	}
	return sorted[idx]
}
