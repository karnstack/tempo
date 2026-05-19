// Package repostats implements the rollup.Aggregator that rebuilds the
// count columns of daily_repo_stats from pull_requests and deployments.
//
// The four columns this aggregator owns are prs_opened, prs_merged,
// prs_closed, and deploys. The two lead_time_seconds_* columns belong
// to the cycle-time aggregator (0035) and are intentionally absent from
// the underlying query's ON CONFLICT DO UPDATE clause — siblings can
// run in any order without trampling each other's columns.
//
// Idempotency is via UPSERT alone. Because daily_repo_stats is keyed by
// (date, repo_id) with no per-user fan-out, a re-run after deleting
// source rows naturally drives the counts back to 0; no DELETE step is
// needed (and a DELETE would clobber sibling-owned columns).
package repostats

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/karnstack/tempo/internal/storage"
	"github.com/karnstack/tempo/internal/storage/sqlite/sqlitedb"
	"go.uber.org/zap"
)

const aggregatorName = "repo_stats"

// Aggregator wires the AggregateRepoStatsForDay query through the
// rollup.Aggregator interface. It holds *sql.DB only so the constructor
// signature matches sibling aggregators; the actual write goes through
// the sqlc-typed query.
type Aggregator struct {
	db  *sql.DB
	q   *sqlitedb.Queries
	log *zap.Logger
}

// New builds an Aggregator from the shared Storage. The fx wiring is in
// module.go.
func New(s storage.Storage, l *zap.Logger) *Aggregator {
	db := s.DB()
	return &Aggregator{db: db, q: sqlitedb.New(db), log: l}
}

// Name implements rollup.Aggregator.
func (*Aggregator) Name() string { return aggregatorName }

// Aggregate rebuilds the count columns of daily_repo_stats for `date`
// across every non-archived repo. Per-repo errors are logged and the
// first one is returned, but they never short-circuit sibling repos —
// matches the scheduler's policy for aggregator-level errors and the
// engineer_stats aggregator's per-repo policy.
//
// `date` is local-midnight in the scheduler's tz. We treat it as the
// inclusive start of a 24h window and compare against the UTC TIMESTAMP
// columns directly — Go's time.Time carries the location and the driver
// normalises at the SQL boundary.
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
			a.log.Warn("rollup/repo_stats: repo failed",
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

// aggregateRepo runs the single-statement UPSERT for one (date, repo).
// SQLite executes the INSERT atomically on its own; no explicit
// transaction is needed because there is no companion DELETE.
func (a *Aggregator) aggregateRepo(ctx context.Context, repoID int64, dateStr string, from, to time.Time) error {
	if err := a.q.AggregateRepoStatsForDay(ctx, sqlitedb.AggregateRepoStatsForDayParams{
		Date:   dateStr,
		RepoID: repoID,
		FromTs: from,
		ToTs:   to,
	}); err != nil {
		return fmt.Errorf("aggregate: %w", err)
	}
	return nil
}
