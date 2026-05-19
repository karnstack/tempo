// Package reviewstats implements the rollup.Aggregator that rebuilds
// daily_review_latency (one row per (date, repo)) and
// daily_review_load (one row per (date, repo, reviewer)) from
// pr_reviews + pull_requests.
//
// review_latency answers "how long did it take to get a first review
// today?" — for each PR whose earliest non-self non-ghost review was
// submitted on D, the latency is (first review.submitted_at -
// pr.created_at). The aggregator gathers those latencies repo-scoped
// and writes p50/p90 + a count.
//
// review_load answers "who reviewed how much today, and how fast?" —
// per reviewer-on-repo on D, count of reviews + p50 of
// response_minutes = (submitted_at - pr.created_at) / 60.
//
// Both metrics derive from a single bulk fetch of all non-self
// non-ghost reviews for the repo. Doing the MIN/HAVING in SQL fights
// the modernc.org/sqlite driver — aggregate functions on TIMESTAMP
// columns return the Go time.Time.String() format, which neither
// Scan nor unixepoch() will parse. The reduction is small enough
// (one repo's lifetime of reviews) that Go-side grouping is the
// right call for v1.
//
// Idempotency: review_latency is a single UPSERT per repo;
// review_load needs DELETE + INSERT because per-reviewer rows would
// otherwise leak stale counts when source data changes. Both happen
// inside one tx per repo so the (date, repo) slice is atomically
// swapped under WAL.
package reviewstats

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

const aggregatorName = "review_stats"

// Aggregator owns daily_review_latency and daily_review_load end to
// end. Holds *sql.DB so it can manage per-repo txs (review_load is
// DELETE + INSERT, like 0033).
type Aggregator struct {
	db  *sql.DB
	q   *sqlitedb.Queries
	log *zap.Logger
}

// New builds an Aggregator from the shared Storage. fx wiring lives
// in module.go.
func New(s storage.Storage, l *zap.Logger) *Aggregator {
	db := s.DB()
	return &Aggregator{db: db, q: sqlitedb.New(db), log: l}
}

// Name implements rollup.Aggregator.
func (*Aggregator) Name() string { return aggregatorName }

// Aggregate rebuilds both review tables for `date` across every
// non-archived repo. Per-repo errors are logged and the first one is
// returned, but they never short-circuit sibling repos.
//
// `date` is local-midnight in the scheduler's tz. Window is
// [date, date+24h) on `submitted_at` for both tables.
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
			a.log.Warn("rollup/review_stats: repo failed",
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

// firstReview tracks the per-PR reduction we use to compute
// review_latency.
type firstReview struct {
	prCreatedAt time.Time
	submittedAt time.Time
}

// loadAgg gathers per-reviewer state across the daily window.
type loadAgg struct {
	count     int64
	durations []int64 // response_minutes per review
}

func (a *Aggregator) aggregateRepo(ctx context.Context, repoID int64, dateStr string, from, to time.Time) error {
	rows, err := a.q.ListReviewsWithPRMetaForRepo(ctx, repoID)
	if err != nil {
		return fmt.Errorf("list reviews: %w", err)
	}

	// 1. Per-PR reduction for review_latency: track the earliest review
	// per PR (with its pr_created_at carried along).
	firstByPR := make(map[int64]firstReview, 64)
	for _, r := range rows {
		cur, ok := firstByPR[r.PrNumber]
		if !ok || r.SubmittedAt.Before(cur.submittedAt) {
			firstByPR[r.PrNumber] = firstReview{
				prCreatedAt: r.PrCreatedAt,
				submittedAt: r.SubmittedAt,
			}
		}
	}

	// 2. Filter to PRs first-reviewed in [from, to) and compute
	// integer-second latencies.
	var latencies []int64
	for _, fr := range firstByPR {
		if fr.submittedAt.Before(from) || !fr.submittedAt.Before(to) {
			continue
		}
		secs := int64(fr.submittedAt.Sub(fr.prCreatedAt) / time.Second)
		if secs < 0 {
			// Clock-skew defense — same logic as 0035's negative
			// duration filter.
			continue
		}
		latencies = append(latencies, secs)
	}

	// 3. Group reviews-in-window by reviewer for review_load.
	perReviewer := make(map[int64]*loadAgg)
	for _, r := range rows {
		if r.SubmittedAt.Before(from) || !r.SubmittedAt.Before(to) {
			continue
		}
		mins := int64(r.SubmittedAt.Sub(r.PrCreatedAt) / time.Minute)
		if mins < 0 {
			continue
		}
		agg, ok := perReviewer[r.ReviewerGhUserID]
		if !ok {
			agg = &loadAgg{}
			perReviewer[r.ReviewerGhUserID] = agg
		}
		agg.count++
		agg.durations = append(agg.durations, mins)
	}

	// 4. Latency percentiles + count.
	var p50, p90 *int64
	count := int64(len(latencies))
	if count > 0 {
		sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
		v50 := percentile(latencies, 50)
		v90 := percentile(latencies, 90)
		p50 = &v50
		p90 = &v90
	}

	// 5. Atomically swap the (date, repo) slice. The latency UPSERT
	// and the load DELETE + INSERTs all run inside the same tx so a
	// reader under WAL never sees a half-rebuilt day.
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	qtx := a.q.WithTx(tx)

	if err := qtx.DeleteDailyReviewLoadByDateRepo(ctx, sqlitedb.DeleteDailyReviewLoadByDateRepoParams{
		Date:   dateStr,
		RepoID: repoID,
	}); err != nil {
		return fmt.Errorf("delete review_load: %w", err)
	}

	for uid, agg := range perReviewer {
		sort.Slice(agg.durations, func(i, j int) bool { return agg.durations[i] < agg.durations[j] })
		p := percentile(agg.durations, 50)
		if err := qtx.UpsertDailyReviewLoad(ctx, sqlitedb.UpsertDailyReviewLoadParams{
			Date:               dateStr,
			RepoID:             repoID,
			ReviewerGhUserID:   uid,
			Reviews:            agg.count,
			ResponseMinutesP50: &p,
		}); err != nil {
			return fmt.Errorf("upsert review_load reviewer=%d: %w", uid, err)
		}
	}

	if err := qtx.UpsertDailyReviewLatency(ctx, sqlitedb.UpsertDailyReviewLatencyParams{
		Date:                        dateStr,
		RepoID:                      repoID,
		TimeToFirstReviewSecondsP50: p50,
		TimeToFirstReviewSecondsP90: p90,
		Count:                       count,
	}); err != nil {
		return fmt.Errorf("upsert review_latency: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// percentile returns the nearest-rank percentile of a sorted slice.
// q is in [0, 100]. The caller must pre-sort ascending and guarantee
// len(sorted) > 0. Same definition as 0035's cycletime.percentile;
// duplicated here rather than imported because two callers don't
// justify a shared package yet.
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
