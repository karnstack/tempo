package rollup

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/karnstack/tempo/internal/config"
	"github.com/karnstack/tempo/internal/storage/sqlite/sqlitedb"
	"go.uber.org/zap"
)

// rollupKind is the value stored in rollup_runs.kind for whole-day rows.
// Per-aggregator kinds may be added in 0037 when manual reruns land.
const rollupKind = "all"

// Scheduler runs aggregators once a day at cfg.Rollup.Hour (instance-local
// by default, or in cfg.Rollup.Timezone). It catches up missed days on
// boot and ticks every checkInterval (default 1m) to fire the daily run.
type Scheduler struct {
	log           *zap.Logger
	cfg           *config.Config
	q             *sqlitedb.Queries
	aggregators   []Aggregator
	now           func() time.Time
	checkInterval time.Duration
}

// Option mutates a Scheduler after New. Used by tests to inject fakes.
type Option func(*Scheduler)

// WithClock overrides time.Now. Tests use this to fix the fire-time
// predicate's reference point.
func WithClock(now func() time.Time) Option {
	return func(s *Scheduler) { s.now = now }
}

// WithCheckInterval overrides the Loop's ticker cadence. Production
// uses the default 1m; tests use ~5ms to drive the loop quickly.
func WithCheckInterval(d time.Duration) Option {
	return func(s *Scheduler) { s.checkInterval = d }
}

// New builds a Scheduler with production defaults.
func New(l *zap.Logger, cfg *config.Config, q *sqlitedb.Queries, aggregators []Aggregator, opts ...Option) *Scheduler {
	s := &Scheduler{
		log:           l,
		cfg:           cfg,
		q:             q,
		aggregators:   aggregators,
		now:           time.Now,
		checkInterval: time.Minute,
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// tz returns the scheduler's effective timezone. nil cfg.Rollup.Timezone
// means "system local".
func (s *Scheduler) tz() *time.Location {
	if s.cfg.Rollup.Timezone != nil {
		return s.cfg.Rollup.Timezone
	}
	return time.Local
}

// bucketDate formats t as YYYY-MM-DD in the scheduler's tz. This is the
// join key against daily_*.date and rollup_runs.date.
func (s *Scheduler) bucketDate(t time.Time) string {
	return t.In(s.tz()).Format("2006-01-02")
}

// localMidnight parses a YYYY-MM-DD date string and returns local-midnight
// in the scheduler's tz.
func (s *Scheduler) localMidnight(date string) (time.Time, error) {
	t, err := time.ParseInLocation("2006-01-02", date, s.tz())
	if err != nil {
		return time.Time{}, fmt.Errorf("rollup: parse date %q: %w", date, err)
	}
	return t, nil
}

// nextFire returns the next time the daily rollup should fire — the
// next occurrence of cfg.Rollup.Hour in the scheduler's tz. If now is
// before today's fire time, returns today's; else tomorrow's.
func (s *Scheduler) nextFire(now time.Time) time.Time {
	tz := s.tz()
	nowLocal := now.In(tz)
	today := time.Date(nowLocal.Year(), nowLocal.Month(), nowLocal.Day(),
		s.cfg.Rollup.Hour, 0, 0, 0, tz)
	if !nowLocal.Before(today) {
		return today.AddDate(0, 0, 1)
	}
	return today
}

// pastFireTime reports whether today's fire-hour has already passed in
// the scheduler's tz.
func (s *Scheduler) pastFireTime(now time.Time) bool {
	tz := s.tz()
	nowLocal := now.In(tz)
	today := time.Date(nowLocal.Year(), nowLocal.Month(), nowLocal.Day(),
		s.cfg.Rollup.Hour, 0, 0, 0, tz)
	return !nowLocal.Before(today)
}

// Loop runs CatchUp at boot, then ticks on checkInterval until ctx is
// cancelled, running Tick each time.
func (s *Scheduler) Loop(ctx context.Context) {
	s.CatchUp(ctx)
	if ctx.Err() != nil {
		return
	}
	t := time.NewTicker(s.checkInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.Tick(ctx)
		}
	}
}

// Tick runs yesterday's rollup if today's fire-hour has passed and
// yesterday isn't already done. Cheap idempotency via a DB lookup.
func (s *Scheduler) Tick(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	now := s.now()
	if !s.pastFireTime(now) {
		return
	}
	yesterday := s.yesterdayMidnight(now)
	dateStr := s.bucketDate(yesterday)
	if s.hasSuccessfulRun(ctx, dateStr) {
		return
	}
	s.RunDate(ctx, yesterday)
}

// CatchUp walks the last 7 local days and runs any without a successful
// rollup_runs row. Failed rows are left alone — manual rerun is 0037's job.
func (s *Scheduler) CatchUp(ctx context.Context) {
	now := s.now()
	done := s.successfulDateSet(ctx, 7)
	for i := 1; i <= 7; i++ {
		if ctx.Err() != nil {
			return
		}
		d := s.daysAgoMidnight(now, i)
		dateStr := s.bucketDate(d)
		if _, ok := done[dateStr]; ok {
			continue
		}
		if s.hasFailedRun(ctx, dateStr) {
			continue
		}
		s.RunDate(ctx, d)
	}
}

// RunDate aggregates one local date. Idempotent — re-running upserts a
// fresh rollup_runs row and re-invokes every aggregator. Aggregator
// errors are logged and recorded but never short-circuit siblings.
func (s *Scheduler) RunDate(ctx context.Context, date time.Time) {
	dateStr := s.bucketDate(date)
	startedAt := s.now().UTC()
	if _, err := s.q.UpsertRollupRunStart(ctx, sqlitedb.UpsertRollupRunStartParams{
		Date:      dateStr,
		Kind:      rollupKind,
		StartedAt: startedAt,
	}); err != nil {
		s.log.Error("rollup: upsert run start",
			zap.String("date", dateStr), zap.Error(err))
		return
	}

	var firstErr error
	for _, a := range s.aggregators {
		if ctx.Err() != nil {
			if firstErr == nil {
				firstErr = ctx.Err()
			}
			break
		}
		if err := a.Aggregate(ctx, date); err != nil {
			wrapped := fmt.Errorf("%s: %w", a.Name(), err)
			s.log.Warn("rollup: aggregator failed",
				zap.String("date", dateStr),
				zap.String("aggregator", a.Name()),
				zap.Error(err))
			if firstErr == nil {
				firstErr = wrapped
			}
		}
	}

	finishedAt := s.now().UTC()
	writeCtx, cancel := closingCtx(ctx)
	defer cancel()
	ok := int64(1)
	errMsg := ""
	if firstErr != nil {
		ok = 0
		errMsg = firstErr.Error()
	}
	if err := s.q.FinishRollupRun(writeCtx, sqlitedb.FinishRollupRunParams{
		FinishedAt: &finishedAt,
		Ok:         ok,
		Error:      errMsg,
		Date:       dateStr,
		Kind:       rollupKind,
	}); err != nil {
		s.log.Error("rollup: finish run",
			zap.String("date", dateStr), zap.Error(err))
	}
}

// hasSuccessfulRun returns true if rollup_runs has a row for (date, "all")
// with ok=1.
func (s *Scheduler) hasSuccessfulRun(ctx context.Context, dateStr string) bool {
	row, err := s.q.GetRollupRun(ctx, sqlitedb.GetRollupRunParams{
		Date: dateStr,
		Kind: rollupKind,
	})
	if err != nil {
		return false
	}
	return row.Ok == 1
}

// hasFailedRun returns true if rollup_runs has a finished row for
// (date, "all") with ok=0. Used by CatchUp to skip known-bad days.
func (s *Scheduler) hasFailedRun(ctx context.Context, dateStr string) bool {
	row, err := s.q.GetRollupRun(ctx, sqlitedb.GetRollupRunParams{
		Date: dateStr,
		Kind: rollupKind,
	})
	if err != nil {
		return false
	}
	return row.FinishedAt != nil && row.Ok == 0
}

// successfulDateSet returns a set of date strings (last `days` days)
// where kind="all" has a successful rollup row. Used by CatchUp for
// O(1) lookup per candidate day.
func (s *Scheduler) successfulDateSet(ctx context.Context, days int) map[string]struct{} {
	since := s.bucketDate(s.daysAgoMidnight(s.now(), days))
	dates, err := s.q.ListSuccessfulRollupDates(ctx, sqlitedb.ListSuccessfulRollupDatesParams{
		Kind:  rollupKind,
		Since: since,
	})
	if err != nil {
		s.log.Warn("rollup: list successful dates",
			zap.String("since", since), zap.Error(err))
		return map[string]struct{}{}
	}
	out := make(map[string]struct{}, len(dates))
	for _, d := range dates {
		out[d] = struct{}{}
	}
	return out
}

// yesterdayMidnight returns local-midnight of the day before now in tz.
func (s *Scheduler) yesterdayMidnight(now time.Time) time.Time {
	return s.daysAgoMidnight(now, 1)
}

// daysAgoMidnight returns local-midnight of n days before now in tz.
func (s *Scheduler) daysAgoMidnight(now time.Time, n int) time.Time {
	tz := s.tz()
	d := now.In(tz).AddDate(0, 0, -n)
	return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, tz)
}

// closingCtx returns ctx unchanged when it has no error, or a fresh
// 5-second-timeout context.Background()-derived context when ctx has
// already been cancelled or deadline-exceeded. Mirrors the ingest
// scheduler's helper so the rollup_runs finish write still lands when
// the parent ctx dies mid-aggregation.
func closingCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return context.WithTimeout(context.Background(), 5*time.Second)
	}
	return ctx, func() {}
}
