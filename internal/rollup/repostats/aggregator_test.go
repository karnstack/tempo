package repostats_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/karnstack/tempo/internal/config"
	"github.com/karnstack/tempo/internal/rollup/repostats"
	"github.com/karnstack/tempo/internal/storage"
	"github.com/karnstack/tempo/internal/storage/sqlite"
	"github.com/karnstack/tempo/internal/storage/sqlite/sqlitedb"
	"github.com/karnstack/tempo/migrations"
	"go.uber.org/fx/fxtest"
	"go.uber.org/zap/zaptest"
)

// --- harness ------------------------------------------------------

// newStorage builds an on-disk sqlite store with all migrations applied
// and returns the storage handle + a queries handle. Mirrors the
// engineerstats test harness so future rollup packages can share the
// same shape.
func newStorage(t *testing.T) (storage.Storage, *sqlitedb.Queries) {
	t.Helper()
	lc := fxtest.NewLifecycle(t)
	l := zaptest.NewLogger(t)
	path := filepath.Join(t.TempDir(), "repostats.db")
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

// seedRepo creates a tenant + repo and returns the repo. archived=true
// sets repos.archived=1.
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

// seedPR upserts a PR. mergedAt nil + closedAt nil means open; mergedAt
// non-nil implies the PR is also closed (GitHub semantics) — pass the
// same value for closedAt in that case. The author is a fixed sentinel
// because repo_stats doesn't read author_gh_user_id.
func seedPR(t *testing.T, q *sqlitedb.Queries, repoID, prNum int64, createdAt time.Time, mergedAt, closedAt *time.Time) {
	t.Helper()
	state := "open"
	switch {
	case mergedAt != nil:
		state = "merged"
	case closedAt != nil:
		state = "closed"
	}
	if err := q.UpsertPullRequest(context.Background(), sqlitedb.UpsertPullRequestParams{
		RepoID:         repoID,
		Number:         prNum,
		GhID:           int64(time.Now().UnixNano()) + prNum,
		AuthorGhUserID: 1,
		State:          state,
		Title:          "t",
		CreatedAt:      createdAt,
		UpdatedAt:      createdAt,
		MergedAt:       mergedAt,
		ClosedAt:       closedAt,
		Additions:      0,
		Deletions:      0,
		BaseRef:        "main",
		HeadRef:        "f",
	}); err != nil {
		t.Fatalf("UpsertPullRequest #%d: %v", prNum, err)
	}
}

// seedDeployment writes one row with status="" to match what the
// production ingest writes (see internal/ingest/deployments/doc.go:42).
func seedDeployment(t *testing.T, q *sqlitedb.Queries, ghID, repoID int64, createdAt time.Time) {
	t.Helper()
	if err := q.UpsertDeployment(context.Background(), sqlitedb.UpsertDeploymentParams{
		GhID:        ghID,
		RepoID:      repoID,
		Environment: "production",
		Ref:         "main",
		Sha:         "deadbeef",
		Status:      "",
		CreatedAt:   createdAt,
	}); err != nil {
		t.Fatalf("UpsertDeployment gh=%d: %v", ghID, err)
	}
}

// readRow returns the daily_repo_stats row for (repo_id, date), or a
// zero DailyRepoStat with Date == "" if none exists. The ListByRepoBetween
// query's [from, to) shape is used with a single-day window.
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
		t.Fatalf("expected ≤1 row for (repo=%d, date=%s); got %d", repoID, date, len(rows))
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
	a := repostats.New(s, zaptest.NewLogger(t))
	date := time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)
	if err := a.Aggregate(context.Background(), date); err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
}

func TestAggregate_AllSourcesPopulated(t *testing.T) {
	s, q := newStorage(t)
	a := repostats.New(s, zaptest.NewLogger(t))

	repo := seedRepo(t, q, false)
	date := time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)
	noon := date.Add(12 * time.Hour)

	// 1 opened, 1 merged (also has closed_at), 1 closed-not-merged,
	// 2 deploys — all anchored to noon on the target day.
	seedPR(t, q, repo.ID, 100, noon, nil, nil)
	merged := noon.Add(time.Hour)
	seedPR(t, q, repo.ID, 101, noon.Add(-24*time.Hour), &merged, &merged)
	closed := noon.Add(2 * time.Hour)
	seedPR(t, q, repo.ID, 102, noon.Add(-24*time.Hour), nil, &closed)
	seedDeployment(t, q, 9001, repo.ID, noon)
	seedDeployment(t, q, 9002, repo.ID, noon.Add(3*time.Hour))

	if err := a.Aggregate(context.Background(), date); err != nil {
		t.Fatalf("Aggregate: %v", err)
	}

	row := readRow(t, q, repo.ID, "2026-05-17")
	switch {
	case row.Date != "2026-05-17":
		t.Errorf("date = %q, want 2026-05-17", row.Date)
	case row.PrsOpened != 1:
		t.Errorf("prs_opened = %d, want 1", row.PrsOpened)
	case row.PrsMerged != 1:
		t.Errorf("prs_merged = %d, want 1", row.PrsMerged)
	case row.PrsClosed != 1:
		t.Errorf("prs_closed = %d, want 1", row.PrsClosed)
	case row.Deploys != 2:
		t.Errorf("deploys = %d, want 2", row.Deploys)
	case row.LeadTimeSecondsP50 != nil:
		t.Errorf("lead_time_p50 = %v, want nil", row.LeadTimeSecondsP50)
	case row.LeadTimeSecondsP90 != nil:
		t.Errorf("lead_time_p90 = %v, want nil", row.LeadTimeSecondsP90)
	}
}

func TestAggregate_MergedPRNotDoubleCounted(t *testing.T) {
	s, q := newStorage(t)
	a := repostats.New(s, zaptest.NewLogger(t))

	repo := seedRepo(t, q, false)
	date := time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)
	noon := date.Add(12 * time.Hour)

	// Opened and merged the same day. Per GitHub semantics, closed_at
	// is also set on merge — the prs_closed subquery filters that out.
	merged := noon.Add(2 * time.Hour)
	seedPR(t, q, repo.ID, 1, noon, &merged, &merged)

	if err := a.Aggregate(context.Background(), date); err != nil {
		t.Fatalf("Aggregate: %v", err)
	}

	row := readRow(t, q, repo.ID, "2026-05-17")
	if row.PrsMerged != 1 || row.PrsClosed != 0 {
		t.Errorf("merged=%d closed=%d, want merged=1 closed=0", row.PrsMerged, row.PrsClosed)
	}
	if row.PrsOpened != 1 {
		t.Errorf("opened=%d, want 1", row.PrsOpened)
	}
}

func TestAggregate_MultiRepoPartitioning(t *testing.T) {
	s, q := newStorage(t)
	a := repostats.New(s, zaptest.NewLogger(t))

	r1 := seedRepo(t, q, false)
	r2 := seedRepo(t, q, false)
	date := time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)
	noon := date.Add(12 * time.Hour)

	// r1: 2 opened, 1 deploy.
	seedPR(t, q, r1.ID, 1, noon, nil, nil)
	seedPR(t, q, r1.ID, 2, noon, nil, nil)
	seedDeployment(t, q, 9001, r1.ID, noon)

	// r2: 1 merged, 2 deploys.
	merged := noon
	seedPR(t, q, r2.ID, 1, noon.Add(-24*time.Hour), &merged, &merged)
	seedDeployment(t, q, 9002, r2.ID, noon)
	seedDeployment(t, q, 9003, r2.ID, noon.Add(time.Hour))

	if err := a.Aggregate(context.Background(), date); err != nil {
		t.Fatalf("Aggregate: %v", err)
	}

	r1Row := readRow(t, q, r1.ID, "2026-05-17")
	if r1Row.PrsOpened != 2 || r1Row.Deploys != 1 || r1Row.PrsMerged != 0 {
		t.Errorf("r1: opened=%d merged=%d deploys=%d, want 2/0/1",
			r1Row.PrsOpened, r1Row.PrsMerged, r1Row.Deploys)
	}

	r2Row := readRow(t, q, r2.ID, "2026-05-17")
	if r2Row.PrsMerged != 1 || r2Row.Deploys != 2 || r2Row.PrsOpened != 0 {
		t.Errorf("r2: opened=%d merged=%d deploys=%d, want 0/1/2",
			r2Row.PrsOpened, r2Row.PrsMerged, r2Row.Deploys)
	}
}

func TestAggregate_ArchivedRepoSkipped(t *testing.T) {
	s, q := newStorage(t)
	a := repostats.New(s, zaptest.NewLogger(t))

	repo := seedRepo(t, q, true)
	date := time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)
	noon := date.Add(12 * time.Hour)
	seedPR(t, q, repo.ID, 1, noon, nil, nil)
	seedDeployment(t, q, 9001, repo.ID, noon)

	if err := a.Aggregate(context.Background(), date); err != nil {
		t.Fatalf("Aggregate: %v", err)
	}

	row := readRow(t, q, repo.ID, "2026-05-17")
	if row.Date != "" {
		t.Errorf("archived repo: got row %+v, want none", row)
	}
}

func TestAggregate_WindowBoundaries(t *testing.T) {
	s, q := newStorage(t)
	a := repostats.New(s, zaptest.NewLogger(t))

	repo := seedRepo(t, q, false)
	target := time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)
	afterDay := target.Add(24 * time.Hour)
	beforeDay := target.Add(-time.Nanosecond)
	atStart := target
	atEnd := afterDay.Add(-time.Nanosecond)

	// PRs straddling the boundary. Only atStart + atEnd should count.
	seedPR(t, q, repo.ID, 1, beforeDay, nil, nil)
	seedPR(t, q, repo.ID, 2, atStart, nil, nil)
	seedPR(t, q, repo.ID, 3, atEnd, nil, nil)
	seedPR(t, q, repo.ID, 4, afterDay, nil, nil)

	// Deployments at the same four points.
	seedDeployment(t, q, 9001, repo.ID, beforeDay)
	seedDeployment(t, q, 9002, repo.ID, atStart)
	seedDeployment(t, q, 9003, repo.ID, atEnd)
	seedDeployment(t, q, 9004, repo.ID, afterDay)

	if err := a.Aggregate(context.Background(), target); err != nil {
		t.Fatalf("Aggregate: %v", err)
	}

	row := readRow(t, q, repo.ID, "2026-05-17")
	if row.PrsOpened != 2 {
		t.Errorf("prs_opened = %d, want 2 (atStart + atEnd only)", row.PrsOpened)
	}
	if row.Deploys != 2 {
		t.Errorf("deploys = %d, want 2 (atStart + atEnd only)", row.Deploys)
	}
}

func TestAggregate_IdempotentRerunAndStaleCountsCleared(t *testing.T) {
	s, q := newStorage(t)
	a := repostats.New(s, zaptest.NewLogger(t))

	repo := seedRepo(t, q, false)
	date := time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)
	noon := date.Add(12 * time.Hour)
	seedPR(t, q, repo.ID, 1, noon, nil, nil)
	seedDeployment(t, q, 9001, repo.ID, noon)

	if err := a.Aggregate(context.Background(), date); err != nil {
		t.Fatalf("first Aggregate: %v", err)
	}
	first := readRow(t, q, repo.ID, "2026-05-17")
	if first.PrsOpened != 1 || first.Deploys != 1 {
		t.Fatalf("after first run: opened=%d deploys=%d, want 1/1",
			first.PrsOpened, first.Deploys)
	}

	// Re-run with no source changes → identical counts.
	if err := a.Aggregate(context.Background(), date); err != nil {
		t.Fatalf("second Aggregate: %v", err)
	}
	second := readRow(t, q, repo.ID, "2026-05-17")
	if second != first {
		t.Errorf("rerun changed row: before=%+v after=%+v", first, second)
	}

	// Delete the source PR + deployment and re-run → counts drop to 0
	// in the same row. Catches a missing UPSERT path.
	if _, err := s.DB().ExecContext(context.Background(),
		"DELETE FROM pull_requests WHERE repo_id = ? AND number = ?", repo.ID, 1); err != nil {
		t.Fatalf("DELETE PR: %v", err)
	}
	if _, err := s.DB().ExecContext(context.Background(),
		"DELETE FROM deployments WHERE gh_id = ?", 9001); err != nil {
		t.Fatalf("DELETE deployment: %v", err)
	}
	if err := a.Aggregate(context.Background(), date); err != nil {
		t.Fatalf("third Aggregate: %v", err)
	}
	third := readRow(t, q, repo.ID, "2026-05-17")
	if third.PrsOpened != 0 || third.Deploys != 0 {
		t.Errorf("after source delete: opened=%d deploys=%d, want 0/0",
			third.PrsOpened, third.Deploys)
	}
}

func TestAggregate_LeadTimeColumnsPreserved(t *testing.T) {
	s, q := newStorage(t)
	a := repostats.New(s, zaptest.NewLogger(t))

	repo := seedRepo(t, q, false)
	date := time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)
	noon := date.Add(12 * time.Hour)

	// Seed a row with non-NULL lead-time columns as if 0035's
	// aggregator had already run. UpsertDailyRepoStats sets all eight
	// columns explicitly.
	p50 := int64(123456)
	p90 := int64(789012)
	if err := q.UpsertDailyRepoStats(context.Background(), sqlitedb.UpsertDailyRepoStatsParams{
		Date:               "2026-05-17",
		RepoID:             repo.ID,
		PrsOpened:          99,
		PrsMerged:          99,
		PrsClosed:          99,
		Deploys:            99,
		LeadTimeSecondsP50: &p50,
		LeadTimeSecondsP90: &p90,
	}); err != nil {
		t.Fatalf("UpsertDailyRepoStats seed: %v", err)
	}

	// Now seed real activity and run the aggregator.
	seedPR(t, q, repo.ID, 1, noon, nil, nil)
	seedDeployment(t, q, 9001, repo.ID, noon)
	if err := a.Aggregate(context.Background(), date); err != nil {
		t.Fatalf("Aggregate: %v", err)
	}

	row := readRow(t, q, repo.ID, "2026-05-17")
	// The four count columns must reflect the aggregator's view, not
	// the seeded 99s.
	if row.PrsOpened != 1 || row.PrsMerged != 0 || row.PrsClosed != 0 || row.Deploys != 1 {
		t.Errorf("counts after aggregate: %+v, want opened=1 merged=0 closed=0 deploys=1", row)
	}
	// Lead-time columns must be untouched.
	if row.LeadTimeSecondsP50 == nil || *row.LeadTimeSecondsP50 != p50 {
		t.Errorf("lead_time_p50 changed: got %v, want %d", row.LeadTimeSecondsP50, p50)
	}
	if row.LeadTimeSecondsP90 == nil || *row.LeadTimeSecondsP90 != p90 {
		t.Errorf("lead_time_p90 changed: got %v, want %d", row.LeadTimeSecondsP90, p90)
	}
}
