package rollup_test

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/karnstack/tempo/internal/config"
	"github.com/karnstack/tempo/internal/rollup"
	"github.com/karnstack/tempo/internal/storage/sqlite"
	"github.com/karnstack/tempo/internal/storage/sqlite/sqlitedb"
	"github.com/karnstack/tempo/migrations"
	"go.uber.org/fx/fxtest"
	"go.uber.org/zap/zaptest"
)

// --- helpers ---

func newStore(t *testing.T) *sqlitedb.Queries {
	t.Helper()
	lc := fxtest.NewLifecycle(t)
	l := zaptest.NewLogger(t)
	path := filepath.Join(t.TempDir(), "rollup.db")
	cfg := &config.Config{Database: config.Database{
		Driver: "sqlite", DSN: path, Raw: "sqlite://" + path,
	}}
	s, err := sqlite.New(lc, l, cfg)
	if err != nil {
		t.Fatalf("sqlite.New: %v", err)
	}
	if err := migrations.Apply(context.Background(), s.DB()); err != nil {
		t.Fatalf("migrations.Apply: %v", err)
	}
	lc.RequireStart()
	t.Cleanup(lc.RequireStop)
	return sqlitedb.New(s.DB())
}

// newScheduler builds a scheduler pinned to a tz with the given fire
// hour, a fixed clock, and an aggregator list. tz nil means UTC.
func newScheduler(t *testing.T, q *sqlitedb.Queries, tz *time.Location, hour int, now time.Time, aggs []rollup.Aggregator, opts ...rollup.Option) *rollup.Scheduler {
	t.Helper()
	cfg := &config.Config{Rollup: config.Rollup{Timezone: tz, Hour: hour}}
	clock := rollup.WithClock(func() time.Time { return now })
	all := append([]rollup.Option{clock}, opts...)
	return rollup.New(zaptest.NewLogger(t), cfg, q, aggs, all...)
}

// --- fake aggregator ---

type fakeAgg struct {
	name     string
	err      error
	calls    atomic.Int32
	mu       sync.Mutex
	gotDates []time.Time
	gotOrder *sharedOrder // optional; tests share a *sharedOrder across aggs
}

// sharedOrder is a goroutine-safe call-order log used across multiple
// fakeAgg instances in TestRunDate_AggregatorOrderPreserved.
type sharedOrder struct {
	mu    sync.Mutex
	names []string
}

func (s *sharedOrder) record(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.names = append(s.names, name)
}

func (s *sharedOrder) snapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.names))
	copy(out, s.names)
	return out
}

func (f *fakeAgg) Name() string { return f.name }
func (f *fakeAgg) Aggregate(_ context.Context, date time.Time) error {
	f.calls.Add(1)
	f.mu.Lock()
	f.gotDates = append(f.gotDates, date)
	f.mu.Unlock()
	if f.gotOrder != nil {
		f.gotOrder.record(f.name)
	}
	return f.err
}

func (f *fakeAgg) Calls() int32 { return f.calls.Load() }

// --- pure-helper tests ---

func TestBucketDate_HonoursTimezone(t *testing.T) {
	t.Parallel()
	q := newStore(t)
	ist, _ := time.LoadLocation("Asia/Kolkata")
	pst, _ := time.LoadLocation("America/Los_Angeles")
	t0 := time.Date(2026, 5, 16, 1, 30, 0, 0, time.UTC) // 01:30 UTC

	tests := []struct {
		name string
		tz   *time.Location
		want string
	}{
		{"utc", time.UTC, "2026-05-16"},
		{"ist_already_next_day", ist, "2026-05-16"},  // 07:00 IST same date in this case
		{"pst_still_previous_day", pst, "2026-05-15"}, // 18:30 prior-day PST
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newScheduler(t, q, tc.tz, 2, t0, nil)
			got := s.BucketDate(t0)
			if got != tc.want {
				t.Errorf("BucketDate = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNextFire_BeforeTodayFire(t *testing.T) {
	t.Parallel()
	q := newStore(t)
	now := time.Date(2026, 5, 16, 1, 30, 0, 0, time.UTC) // before 02:00
	s := newScheduler(t, q, time.UTC, 2, now, nil)
	got := s.NextFire(now)
	want := time.Date(2026, 5, 16, 2, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("NextFire = %v, want %v", got, want)
	}
}

func TestNextFire_AfterTodayFire(t *testing.T) {
	t.Parallel()
	q := newStore(t)
	now := time.Date(2026, 5, 16, 3, 0, 0, 0, time.UTC) // after 02:00
	s := newScheduler(t, q, time.UTC, 2, now, nil)
	got := s.NextFire(now)
	want := time.Date(2026, 5, 17, 2, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("NextFire = %v, want %v", got, want)
	}
}

func TestNextFire_TzShiftDoesNotSkipDay(t *testing.T) {
	t.Parallel()
	q := newStore(t)
	ist, _ := time.LoadLocation("Asia/Kolkata")
	// 21:00 UTC on May 15 is 02:30 IST on May 16 — already past today's
	// IST 02:00. Next fire must be May 17 02:00 IST, not May 16 02:00.
	now := time.Date(2026, 5, 15, 21, 0, 0, 0, time.UTC)
	s := newScheduler(t, q, ist, 2, now, nil)
	got := s.NextFire(now)
	want := time.Date(2026, 5, 17, 2, 0, 0, 0, ist)
	if !got.Equal(want) {
		t.Errorf("NextFire = %v, want %v", got, want)
	}
}

// --- RunDate ---

func TestRunDate_NoAggregators_WritesOkRollupRun(t *testing.T) {
	t.Parallel()
	q := newStore(t)
	now := time.Date(2026, 5, 16, 3, 0, 0, 0, time.UTC)
	s := newScheduler(t, q, time.UTC, 2, now, nil)

	date := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)
	s.RunDate(context.Background(), date)

	row, err := q.GetRollupRun(context.Background(), sqlitedb.GetRollupRunParams{
		Date: "2026-05-15", Kind: "all",
	})
	if err != nil {
		t.Fatalf("GetRollupRun: %v", err)
	}
	if row.Ok != 1 {
		t.Errorf("ok = %d, want 1", row.Ok)
	}
	if row.Error != "" {
		t.Errorf("error = %q, want empty", row.Error)
	}
	if row.FinishedAt == nil {
		t.Error("finished_at is nil")
	}
}

func TestRunDate_AggregatorError_RecordsFailure(t *testing.T) {
	t.Parallel()
	q := newStore(t)
	now := time.Date(2026, 5, 16, 3, 0, 0, 0, time.UTC)
	a := &fakeAgg{name: "engineer-stats"}
	b := &fakeAgg{name: "repo-stats", err: errBoom}
	s := newScheduler(t, q, time.UTC, 2, now, []rollup.Aggregator{a, b})

	date := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)
	s.RunDate(context.Background(), date)

	if a.Calls() != 1 {
		t.Errorf("aggregator a calls = %d, want 1 (no short-circuit)", a.Calls())
	}
	if b.Calls() != 1 {
		t.Errorf("aggregator b calls = %d, want 1", b.Calls())
	}
	row, err := q.GetRollupRun(context.Background(), sqlitedb.GetRollupRunParams{
		Date: "2026-05-15", Kind: "all",
	})
	if err != nil {
		t.Fatalf("GetRollupRun: %v", err)
	}
	if row.Ok != 0 {
		t.Errorf("ok = %d, want 0", row.Ok)
	}
	wantSub := "repo-stats: boom"
	if !contains(row.Error, wantSub) {
		t.Errorf("error = %q, want substring %q", row.Error, wantSub)
	}
}

func TestRunDate_AggregatorOrderPreserved(t *testing.T) {
	t.Parallel()
	q := newStore(t)
	now := time.Date(2026, 5, 16, 3, 0, 0, 0, time.UTC)
	order := &sharedOrder{}
	a := &fakeAgg{name: "alpha", gotOrder: order}
	b := &fakeAgg{name: "beta", gotOrder: order}
	s := newScheduler(t, q, time.UTC, 2, now, []rollup.Aggregator{a, b})

	date := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)
	s.RunDate(context.Background(), date)

	got := order.snapshot()
	if len(got) != 2 || got[0] != "alpha" || got[1] != "beta" {
		t.Errorf("order = %v, want [alpha beta]", got)
	}
}

func TestRunDate_Idempotent(t *testing.T) {
	t.Parallel()
	q := newStore(t)
	clock := time.Date(2026, 5, 16, 3, 0, 0, 0, time.UTC)
	a := &fakeAgg{name: "engineer-stats"}
	cfg := &config.Config{Rollup: config.Rollup{Timezone: time.UTC, Hour: 2}}
	s := rollup.New(zaptest.NewLogger(t), cfg, q, []rollup.Aggregator{a},
		rollup.WithClock(func() time.Time { return clock }),
	)

	date := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)
	s.RunDate(context.Background(), date)
	firstRow, err := q.GetRollupRun(context.Background(), sqlitedb.GetRollupRunParams{
		Date: "2026-05-15", Kind: "all",
	})
	if err != nil {
		t.Fatalf("GetRollupRun (1st): %v", err)
	}

	// Advance the clock so the second run's started_at is observably later.
	clock = clock.Add(time.Hour)
	s.RunDate(context.Background(), date)

	if a.Calls() != 2 {
		t.Errorf("aggregator calls = %d, want 2", a.Calls())
	}
	secondRow, err := q.GetRollupRun(context.Background(), sqlitedb.GetRollupRunParams{
		Date: "2026-05-15", Kind: "all",
	})
	if err != nil {
		t.Fatalf("GetRollupRun (2nd): %v", err)
	}
	if !secondRow.StartedAt.After(firstRow.StartedAt) {
		t.Errorf("second StartedAt %v not after first %v", secondRow.StartedAt, firstRow.StartedAt)
	}

	// Only one row should exist for this (date, kind).
	since := "2026-05-01"
	dates, err := q.ListSuccessfulRollupDates(context.Background(), sqlitedb.ListSuccessfulRollupDatesParams{
		Kind:  "all",
		Since: since,
	})
	if err != nil {
		t.Fatalf("ListSuccessfulRollupDates: %v", err)
	}
	count := 0
	for _, d := range dates {
		if d == "2026-05-15" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("rows for 2026-05-15 = %d, want 1 (idempotent upsert)", count)
	}
}

// --- Tick ---

func TestTick_BeforeFireTime_NoOp(t *testing.T) {
	t.Parallel()
	q := newStore(t)
	now := time.Date(2026, 5, 16, 1, 0, 0, 0, time.UTC) // before 02:00
	a := &fakeAgg{name: "engineer-stats"}
	s := newScheduler(t, q, time.UTC, 2, now, []rollup.Aggregator{a})
	s.Tick(context.Background())

	if a.Calls() != 0 {
		t.Errorf("aggregator calls = %d, want 0 (no fire yet)", a.Calls())
	}
	if _, err := q.GetRollupRun(context.Background(), sqlitedb.GetRollupRunParams{
		Date: "2026-05-15", Kind: "all",
	}); err == nil {
		t.Error("expected no rollup_runs row before fire time")
	}
}

func TestTick_AfterFireTime_RunsYesterday(t *testing.T) {
	t.Parallel()
	q := newStore(t)
	now := time.Date(2026, 5, 16, 2, 10, 0, 0, time.UTC) // 10m past fire
	a := &fakeAgg{name: "engineer-stats"}
	s := newScheduler(t, q, time.UTC, 2, now, []rollup.Aggregator{a})

	s.Tick(context.Background())
	if a.Calls() != 1 {
		t.Fatalf("aggregator calls = %d, want 1 after first tick", a.Calls())
	}
	row, err := q.GetRollupRun(context.Background(), sqlitedb.GetRollupRunParams{
		Date: "2026-05-15", Kind: "all",
	})
	if err != nil {
		t.Fatalf("GetRollupRun: %v", err)
	}
	if row.Ok != 1 {
		t.Errorf("ok = %d, want 1", row.Ok)
	}

	// Second tick at same clock — must be a no-op.
	s.Tick(context.Background())
	if a.Calls() != 1 {
		t.Errorf("aggregator calls = %d, want 1 (idempotency)", a.Calls())
	}
}

// --- CatchUp ---

func TestCatchUp_RunsLast7MissingDays(t *testing.T) {
	t.Parallel()
	q := newStore(t)
	now := time.Date(2026, 5, 16, 4, 0, 0, 0, time.UTC) // past fire
	a := &fakeAgg{name: "engineer-stats"}
	s := newScheduler(t, q, time.UTC, 2, now, []rollup.Aggregator{a})

	// Pre-seed a successful row for today-2 days = 2026-05-14.
	pre, err := q.UpsertRollupRunStart(context.Background(), sqlitedb.UpsertRollupRunStartParams{
		Date: "2026-05-14", Kind: "all", StartedAt: now.Add(-48 * time.Hour),
	})
	if err != nil {
		t.Fatalf("UpsertRollupRunStart: %v", err)
	}
	finishedAt := now.Add(-48 * time.Hour).Add(time.Second)
	if err := q.FinishRollupRun(context.Background(), sqlitedb.FinishRollupRunParams{
		Date: pre.Date, Kind: pre.Kind,
		FinishedAt: &finishedAt, Ok: 1, Error: "",
	}); err != nil {
		t.Fatalf("FinishRollupRun: %v", err)
	}

	s.CatchUp(context.Background())

	// We expect runs for today-1..today-7 except today-2 — that's 6 calls.
	if a.Calls() != 6 {
		t.Errorf("aggregator calls = %d, want 6", a.Calls())
	}

	// today-2 (2026-05-14) row must still reflect the pre-seeded run
	// (not a fresh one) — its started_at should match the pre value.
	row, err := q.GetRollupRun(context.Background(), sqlitedb.GetRollupRunParams{
		Date: "2026-05-14", Kind: "all",
	})
	if err != nil {
		t.Fatalf("GetRollupRun: %v", err)
	}
	if !row.StartedAt.Equal(pre.StartedAt) {
		t.Errorf("today-2 row was overwritten by CatchUp (started_at = %v, want %v)",
			row.StartedAt, pre.StartedAt)
	}

	// today (2026-05-16) must not have a row; CatchUp covers yesterday and back.
	if _, err := q.GetRollupRun(context.Background(), sqlitedb.GetRollupRunParams{
		Date: "2026-05-16", Kind: "all",
	}); err == nil {
		t.Error("expected no rollup_runs row for today")
	}
}

func TestCatchUp_IgnoresFailedRows(t *testing.T) {
	t.Parallel()
	q := newStore(t)
	now := time.Date(2026, 5, 16, 4, 0, 0, 0, time.UTC)
	a := &fakeAgg{name: "engineer-stats"}
	s := newScheduler(t, q, time.UTC, 2, now, []rollup.Aggregator{a})

	// Seed today-1 as a finished failure.
	pre, err := q.UpsertRollupRunStart(context.Background(), sqlitedb.UpsertRollupRunStartParams{
		Date: "2026-05-15", Kind: "all", StartedAt: now.Add(-2 * time.Hour),
	})
	if err != nil {
		t.Fatalf("UpsertRollupRunStart: %v", err)
	}
	finishedAt := now.Add(-time.Hour)
	if err := q.FinishRollupRun(context.Background(), sqlitedb.FinishRollupRunParams{
		Date: pre.Date, Kind: pre.Kind,
		FinishedAt: &finishedAt, Ok: 0, Error: "agg: boom",
	}); err != nil {
		t.Fatalf("FinishRollupRun: %v", err)
	}

	s.CatchUp(context.Background())

	// today-1 must NOT re-run; that's 6 calls (today-2..today-7).
	if a.Calls() != 6 {
		t.Errorf("aggregator calls = %d, want 6 (today-1 skipped)", a.Calls())
	}
	row, err := q.GetRollupRun(context.Background(), sqlitedb.GetRollupRunParams{
		Date: "2026-05-15", Kind: "all",
	})
	if err != nil {
		t.Fatalf("GetRollupRun: %v", err)
	}
	if row.Ok != 0 || row.Error == "" {
		t.Errorf("today-1 row mutated: ok=%d error=%q", row.Ok, row.Error)
	}
}

// --- Loop ---

func TestLoop_StartsCatchUpThenTicks(t *testing.T) {
	t.Parallel()
	q := newStore(t)
	now := time.Date(2026, 5, 16, 4, 0, 0, 0, time.UTC)
	a := &fakeAgg{name: "engineer-stats"}
	s := newScheduler(t, q, time.UTC, 2, now, []rollup.Aggregator{a},
		rollup.WithCheckInterval(5*time.Millisecond),
	)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); s.Loop(ctx) }()

	// Catch-up alone runs 7 days (yesterday..today-7). Wait for that
	// plus at least one tick (which is a no-op because all 7 days are
	// already done, but the tick still happens).
	deadline := time.After(2 * time.Second)
	for a.Calls() < 7 {
		select {
		case <-deadline:
			t.Fatalf("aggregator calls = %d after 2s, want >= 7", a.Calls())
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("loop did not exit after cancel")
	}

	row, err := q.GetRollupRun(context.Background(), sqlitedb.GetRollupRunParams{
		Date: "2026-05-15", Kind: "all",
	})
	if err != nil {
		t.Fatalf("GetRollupRun: %v", err)
	}
	if row.Ok != 1 {
		t.Errorf("yesterday's row ok = %d, want 1", row.Ok)
	}
}

// --- shared helpers ---

var errBoom = &boomErr{}

type boomErr struct{}

func (*boomErr) Error() string { return "boom" }

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
