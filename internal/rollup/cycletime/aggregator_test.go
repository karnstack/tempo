package cycletime_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/karnstack/tempo/internal/config"
	"github.com/karnstack/tempo/internal/rollup/cycletime"
	"github.com/karnstack/tempo/internal/storage"
	"github.com/karnstack/tempo/internal/storage/sqlite"
	"github.com/karnstack/tempo/internal/storage/sqlite/sqlitedb"
	"github.com/karnstack/tempo/migrations"
	"go.uber.org/fx/fxtest"
	"go.uber.org/zap/zaptest"
)

// --- harness ------------------------------------------------------

func newStorage(t *testing.T) (storage.Storage, *sqlitedb.Queries) {
	t.Helper()
	lc := fxtest.NewLifecycle(t)
	l := zaptest.NewLogger(t)
	path := filepath.Join(t.TempDir(), "cycletime.db")
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
	return s, sqlitedb.New(s.DB())
}

func seedRepo(t *testing.T, q *sqlitedb.Queries, archived bool) sqlitedb.Repo {
	t.Helper()
	ctx := context.Background()
	tenant, err := q.CreateTenant(ctx, "t")
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	a := int64(0)
	if archived {
		a = 1
	}
	repo, err := q.CreateRepo(ctx, sqlitedb.CreateRepoParams{
		TenantID:      tenant.ID,
		ConnectionID:  1,
		GhID:          int64(time.Now().UnixNano()),
		Owner:         "o",
		Name:          "r",
		DefaultBranch: "main",
		Archived:      a,
	})
	if err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}
	return repo
}

// seedMergedPR upserts a PR with the given created_at and merged_at.
// closed_at is set to merged_at (GitHub semantics on merge).
func seedMergedPR(t *testing.T, q *sqlitedb.Queries, repoID, prNum int64, createdAt, mergedAt time.Time) {
	t.Helper()
	m := mergedAt
	c := mergedAt
	if err := q.UpsertPullRequest(context.Background(), sqlitedb.UpsertPullRequestParams{
		RepoID:         repoID,
		Number:         prNum,
		GhID:           int64(time.Now().UnixNano()) + prNum,
		AuthorGhUserID: 1,
		State:          "merged",
		Title:          "t",
		CreatedAt:      createdAt,
		UpdatedAt:      mergedAt,
		MergedAt:       &m,
		ClosedAt:       &c,
		Additions:      0,
		Deletions:      0,
		BaseRef:        "main",
		HeadRef:        "f",
	}); err != nil {
		t.Fatalf("UpsertPullRequest #%d: %v", prNum, err)
	}
}

// seedOpenPR upserts an unmerged PR — used to make sure the aggregator
// ignores it.
func seedOpenPR(t *testing.T, q *sqlitedb.Queries, repoID, prNum int64, createdAt time.Time) {
	t.Helper()
	if err := q.UpsertPullRequest(context.Background(), sqlitedb.UpsertPullRequestParams{
		RepoID:         repoID,
		Number:         prNum,
		GhID:           int64(time.Now().UnixNano()) + prNum,
		AuthorGhUserID: 1,
		State:          "open",
		Title:          "t",
		CreatedAt:      createdAt,
		UpdatedAt:      createdAt,
		BaseRef:        "main",
		HeadRef:        "f",
	}); err != nil {
		t.Fatalf("UpsertPullRequest open #%d: %v", prNum, err)
	}
}

func readRow(t *testing.T, q *sqlitedb.Queries, repoID int64, date string) sqlitedb.DailyRepoStat {
	t.Helper()
	rows, err := q.ListDailyRepoStatsByRepoBetween(context.Background(), sqlitedb.ListDailyRepoStatsByRepoBetweenParams{
		RepoID:   repoID,
		FromDate: date,
		ToDate:   addDate(date, 1),
	})
	if err != nil {
		t.Fatalf("ListDailyRepoStatsByRepoBetween: %v", err)
	}
	switch len(rows) {
	case 0:
		return sqlitedb.DailyRepoStat{}
	case 1:
		return rows[0]
	default:
		t.Fatalf("expected <=1 row for (repo=%d, date=%s); got %d", repoID, date, len(rows))
		return sqlitedb.DailyRepoStat{}
	}
}

func addDate(date string, n int) string {
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		panic(err)
	}
	return t.AddDate(0, 0, n).Format("2006-01-02")
}

// --- tests --------------------------------------------------------

func TestAggregate_EmptyDatabase(t *testing.T) {
	s, _ := newStorage(t)
	a := cycletime.New(s, zaptest.NewLogger(t))
	date := time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)
	if err := a.Aggregate(context.Background(), date); err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
}

func TestAggregate_SingleMergedPR(t *testing.T) {
	s, q := newStorage(t)
	a := cycletime.New(s, zaptest.NewLogger(t))

	repo := seedRepo(t, q, false)
	date := time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)
	noon := date.Add(12 * time.Hour)

	// Open 6h before noon, merge at noon -> 21,600 seconds.
	seedMergedPR(t, q, repo.ID, 1, noon.Add(-6*time.Hour), noon)

	if err := a.Aggregate(context.Background(), date); err != nil {
		t.Fatalf("Aggregate: %v", err)
	}

	row := readRow(t, q, repo.ID, "2026-05-17")
	const want = int64(6 * 60 * 60)
	if row.LeadTimeSecondsP50 == nil || *row.LeadTimeSecondsP50 != want {
		t.Errorf("p50 = %v, want %d", row.LeadTimeSecondsP50, want)
	}
	if row.LeadTimeSecondsP90 == nil || *row.LeadTimeSecondsP90 != want {
		t.Errorf("p90 = %v, want %d", row.LeadTimeSecondsP90, want)
	}
}

func TestAggregate_PercentilesNearestRank(t *testing.T) {
	s, q := newStorage(t)
	a := cycletime.New(s, zaptest.NewLogger(t))

	repo := seedRepo(t, q, false)
	date := time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)
	noon := date.Add(12 * time.Hour)

	// Four PRs, all merged at noon (in window), with durations
	// 100s, 200s, 300s, 400s. Nearest-rank: p50 = sorted[1] = 200,
	// p90 = sorted[3] = 400.
	for i, dur := range []time.Duration{100 * time.Second, 200 * time.Second, 300 * time.Second, 400 * time.Second} {
		seedMergedPR(t, q, repo.ID, int64(i+1), noon.Add(-dur), noon)
	}
	// One open PR — must be ignored.
	seedOpenPR(t, q, repo.ID, 99, noon)

	if err := a.Aggregate(context.Background(), date); err != nil {
		t.Fatalf("Aggregate: %v", err)
	}

	row := readRow(t, q, repo.ID, "2026-05-17")
	if row.LeadTimeSecondsP50 == nil || *row.LeadTimeSecondsP50 != 200 {
		t.Errorf("p50 = %v, want 200", row.LeadTimeSecondsP50)
	}
	if row.LeadTimeSecondsP90 == nil || *row.LeadTimeSecondsP90 != 400 {
		t.Errorf("p90 = %v, want 400", row.LeadTimeSecondsP90)
	}
}

func TestAggregate_WindowBoundariesOnMergedAt(t *testing.T) {
	s, q := newStorage(t)
	a := cycletime.New(s, zaptest.NewLogger(t))

	repo := seedRepo(t, q, false)
	target := time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)
	afterDay := target.Add(24 * time.Hour)
	beforeDay := target.Add(-time.Nanosecond)
	atStart := target
	atEnd := afterDay.Add(-time.Nanosecond)

	// Four PRs merged at the four boundary points; only the middle
	// two should count. Each has duration = 60s for easy assertions.
	seedMergedPR(t, q, repo.ID, 1, beforeDay.Add(-60*time.Second), beforeDay)
	seedMergedPR(t, q, repo.ID, 2, atStart.Add(-60*time.Second), atStart)
	seedMergedPR(t, q, repo.ID, 3, atEnd.Add(-60*time.Second), atEnd)
	seedMergedPR(t, q, repo.ID, 4, afterDay.Add(-60*time.Second), afterDay)

	if err := a.Aggregate(context.Background(), target); err != nil {
		t.Fatalf("Aggregate: %v", err)
	}

	row := readRow(t, q, repo.ID, "2026-05-17")
	// Two samples, both 60s, so p50 = p90 = 60.
	if row.LeadTimeSecondsP50 == nil || *row.LeadTimeSecondsP50 != 60 {
		t.Errorf("p50 = %v, want 60 (atStart + atEnd only)", row.LeadTimeSecondsP50)
	}
	if row.LeadTimeSecondsP90 == nil || *row.LeadTimeSecondsP90 != 60 {
		t.Errorf("p90 = %v, want 60 (atStart + atEnd only)", row.LeadTimeSecondsP90)
	}
}

func TestAggregate_MultiRepoPartitioning(t *testing.T) {
	s, q := newStorage(t)
	a := cycletime.New(s, zaptest.NewLogger(t))

	r1 := seedRepo(t, q, false)
	r2 := seedRepo(t, q, false)
	date := time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)
	noon := date.Add(12 * time.Hour)

	// r1 PRs: 100s, 300s -> p50 = 100, p90 = 300.
	seedMergedPR(t, q, r1.ID, 1, noon.Add(-100*time.Second), noon)
	seedMergedPR(t, q, r1.ID, 2, noon.Add(-300*time.Second), noon)

	// r2 PRs: 500s -> p50 = p90 = 500.
	seedMergedPR(t, q, r2.ID, 1, noon.Add(-500*time.Second), noon)

	if err := a.Aggregate(context.Background(), date); err != nil {
		t.Fatalf("Aggregate: %v", err)
	}

	r1Row := readRow(t, q, r1.ID, "2026-05-17")
	if r1Row.LeadTimeSecondsP50 == nil || *r1Row.LeadTimeSecondsP50 != 100 {
		t.Errorf("r1 p50 = %v, want 100", r1Row.LeadTimeSecondsP50)
	}
	if r1Row.LeadTimeSecondsP90 == nil || *r1Row.LeadTimeSecondsP90 != 300 {
		t.Errorf("r1 p90 = %v, want 300", r1Row.LeadTimeSecondsP90)
	}

	r2Row := readRow(t, q, r2.ID, "2026-05-17")
	if r2Row.LeadTimeSecondsP50 == nil || *r2Row.LeadTimeSecondsP50 != 500 {
		t.Errorf("r2 p50 = %v, want 500", r2Row.LeadTimeSecondsP50)
	}
	if r2Row.LeadTimeSecondsP90 == nil || *r2Row.LeadTimeSecondsP90 != 500 {
		t.Errorf("r2 p90 = %v, want 500", r2Row.LeadTimeSecondsP90)
	}
}

func TestAggregate_ArchivedRepoSkipped(t *testing.T) {
	s, q := newStorage(t)
	a := cycletime.New(s, zaptest.NewLogger(t))

	repo := seedRepo(t, q, true)
	date := time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)
	noon := date.Add(12 * time.Hour)
	seedMergedPR(t, q, repo.ID, 1, noon.Add(-time.Hour), noon)

	if err := a.Aggregate(context.Background(), date); err != nil {
		t.Fatalf("Aggregate: %v", err)
	}

	row := readRow(t, q, repo.ID, "2026-05-17")
	if row.Date != "" {
		t.Errorf("archived repo: got row %+v, want none", row)
	}
}

func TestAggregate_NoMergedPRsClearsPercentiles(t *testing.T) {
	s, q := newStorage(t)
	a := cycletime.New(s, zaptest.NewLogger(t))

	repo := seedRepo(t, q, false)
	date := time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)

	// Seed a row that already has percentiles from a prior run, plus
	// non-zero counts owned by 0034. After this run with no merged PRs
	// in window, percentiles should clear to NULL but counts must stay.
	p50 := int64(999)
	p90 := int64(1999)
	if err := q.UpsertDailyRepoStats(context.Background(), sqlitedb.UpsertDailyRepoStatsParams{
		Date:               "2026-05-17",
		RepoID:             repo.ID,
		PrsOpened:          5,
		PrsMerged:          5,
		PrsClosed:          5,
		Deploys:            5,
		LeadTimeSecondsP50: &p50,
		LeadTimeSecondsP90: &p90,
	}); err != nil {
		t.Fatalf("UpsertDailyRepoStats seed: %v", err)
	}

	if err := a.Aggregate(context.Background(), date); err != nil {
		t.Fatalf("Aggregate: %v", err)
	}

	row := readRow(t, q, repo.ID, "2026-05-17")
	if row.LeadTimeSecondsP50 != nil {
		t.Errorf("p50 = %v, want nil", row.LeadTimeSecondsP50)
	}
	if row.LeadTimeSecondsP90 != nil {
		t.Errorf("p90 = %v, want nil", row.LeadTimeSecondsP90)
	}
	if row.PrsOpened != 5 || row.PrsMerged != 5 || row.PrsClosed != 5 || row.Deploys != 5 {
		t.Errorf("counts changed: %+v, want all 5", row)
	}
}

func TestAggregate_CountColumnsPreserved(t *testing.T) {
	s, q := newStorage(t)
	a := cycletime.New(s, zaptest.NewLogger(t))

	repo := seedRepo(t, q, false)
	date := time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)
	noon := date.Add(12 * time.Hour)

	// Seed counts via the full-row UpsertDailyRepoStats (as if 0034
	// had already run), with NULL percentiles.
	if err := q.UpsertDailyRepoStats(context.Background(), sqlitedb.UpsertDailyRepoStatsParams{
		Date:               "2026-05-17",
		RepoID:             repo.ID,
		PrsOpened:          11,
		PrsMerged:          7,
		PrsClosed:          3,
		Deploys:            2,
		LeadTimeSecondsP50: nil,
		LeadTimeSecondsP90: nil,
	}); err != nil {
		t.Fatalf("UpsertDailyRepoStats seed: %v", err)
	}

	// Now seed a merged PR and run the aggregator.
	seedMergedPR(t, q, repo.ID, 1, noon.Add(-300*time.Second), noon)
	if err := a.Aggregate(context.Background(), date); err != nil {
		t.Fatalf("Aggregate: %v", err)
	}

	row := readRow(t, q, repo.ID, "2026-05-17")
	// Counts must reflect the seeded values, not the merged-PR set.
	if row.PrsOpened != 11 || row.PrsMerged != 7 || row.PrsClosed != 3 || row.Deploys != 2 {
		t.Errorf("counts after aggregate: %+v, want opened=11 merged=7 closed=3 deploys=2", row)
	}
	// Percentiles must reflect the merged PR.
	if row.LeadTimeSecondsP50 == nil || *row.LeadTimeSecondsP50 != 300 {
		t.Errorf("p50 = %v, want 300", row.LeadTimeSecondsP50)
	}
	if row.LeadTimeSecondsP90 == nil || *row.LeadTimeSecondsP90 != 300 {
		t.Errorf("p90 = %v, want 300", row.LeadTimeSecondsP90)
	}
}

func TestAggregate_NegativeDurationFiltered(t *testing.T) {
	s, q := newStorage(t)
	a := cycletime.New(s, zaptest.NewLogger(t))

	repo := seedRepo(t, q, false)
	date := time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)
	noon := date.Add(12 * time.Hour)

	// One valid 100s PR.
	seedMergedPR(t, q, repo.ID, 1, noon.Add(-100*time.Second), noon)
	// One clock-skew PR: merged_at < created_at by 60s.
	seedMergedPR(t, q, repo.ID, 2, noon.Add(60*time.Second), noon)

	if err := a.Aggregate(context.Background(), date); err != nil {
		t.Fatalf("Aggregate: %v", err)
	}

	row := readRow(t, q, repo.ID, "2026-05-17")
	if row.LeadTimeSecondsP50 == nil || *row.LeadTimeSecondsP50 != 100 {
		t.Errorf("p50 = %v, want 100 (negative duration filtered)", row.LeadTimeSecondsP50)
	}
	if row.LeadTimeSecondsP90 == nil || *row.LeadTimeSecondsP90 != 100 {
		t.Errorf("p90 = %v, want 100 (negative duration filtered)", row.LeadTimeSecondsP90)
	}
}
