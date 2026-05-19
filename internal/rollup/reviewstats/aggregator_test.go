package reviewstats_test

import (
	"context"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/karnstack/tempo/internal/config"
	"github.com/karnstack/tempo/internal/rollup/reviewstats"
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
	path := filepath.Join(t.TempDir(), "reviewstats.db")
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

// seedPR creates a PR with the given author. mergedAt/closedAt are nil
// (open) by default — the review aggregator doesn't read state.
func seedPR(t *testing.T, q *sqlitedb.Queries, repoID, prNum, authorUID int64, createdAt time.Time) {
	t.Helper()
	if err := q.UpsertPullRequest(context.Background(), sqlitedb.UpsertPullRequestParams{
		RepoID:         repoID,
		Number:         prNum,
		GhID:           int64(time.Now().UnixNano()) + prNum,
		AuthorGhUserID: authorUID,
		State:          "open",
		Title:          "t",
		CreatedAt:      createdAt,
		UpdatedAt:      createdAt,
		BaseRef:        "main",
		HeadRef:        "f",
	}); err != nil {
		t.Fatalf("UpsertPullRequest #%d: %v", prNum, err)
	}
}

// seedReview upserts one pr_reviews row. Caller provides gh_id so a
// (PR, reviewer) pair can carry multiple reviews on different dates.
func seedReview(t *testing.T, q *sqlitedb.Queries, ghID, prRepoID, prNumber, reviewerUID int64, submittedAt time.Time) {
	t.Helper()
	if err := q.UpsertPullRequestReview(context.Background(), sqlitedb.UpsertPullRequestReviewParams{
		GhID:             ghID,
		PrRepoID:         prRepoID,
		PrNumber:         prNumber,
		ReviewerGhUserID: reviewerUID,
		State:            "APPROVED",
		SubmittedAt:      submittedAt,
	}); err != nil {
		t.Fatalf("UpsertPullRequestReview gh=%d: %v", ghID, err)
	}
}

func readLatency(t *testing.T, q *sqlitedb.Queries, repoID int64, date string) sqlitedb.DailyReviewLatency {
	t.Helper()
	rows, err := q.ListDailyReviewLatencyByRepoBetween(context.Background(), sqlitedb.ListDailyReviewLatencyByRepoBetweenParams{
		RepoID:   repoID,
		FromDate: date,
		ToDate:   addDate(date, 1),
	})
	if err != nil {
		t.Fatalf("ListDailyReviewLatencyByRepoBetween: %v", err)
	}
	switch len(rows) {
	case 0:
		return sqlitedb.DailyReviewLatency{}
	case 1:
		return rows[0]
	default:
		t.Fatalf("expected <=1 row for (repo=%d, date=%s); got %d", repoID, date, len(rows))
		return sqlitedb.DailyReviewLatency{}
	}
}

func readLoad(t *testing.T, q *sqlitedb.Queries, repoID int64, date string) []sqlitedb.DailyReviewLoad {
	t.Helper()
	rows, err := q.ListDailyReviewLoadByRepoBetween(context.Background(), sqlitedb.ListDailyReviewLoadByRepoBetweenParams{
		RepoID:   repoID,
		FromDate: date,
		ToDate:   addDate(date, 1),
	})
	if err != nil {
		t.Fatalf("ListDailyReviewLoadByRepoBetween: %v", err)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ReviewerGhUserID < rows[j].ReviewerGhUserID })
	return rows
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
	a := reviewstats.New(s, zaptest.NewLogger(t))
	date := time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)
	if err := a.Aggregate(context.Background(), date); err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
}

func TestAggregate_SingleFirstReviewedPR(t *testing.T) {
	s, q := newStorage(t)
	a := reviewstats.New(s, zaptest.NewLogger(t))

	repo := seedRepo(t, q, false)
	date := time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)
	noon := date.Add(12 * time.Hour)

	// Author 1 opens PR 2h before noon; reviewer 2 reviews at noon.
	// Latency = 7200s. response_minutes = 120.
	seedPR(t, q, repo.ID, 1, 1, noon.Add(-2*time.Hour))
	seedReview(t, q, 1001, repo.ID, 1, 2, noon)

	if err := a.Aggregate(context.Background(), date); err != nil {
		t.Fatalf("Aggregate: %v", err)
	}

	lat := readLatency(t, q, repo.ID, "2026-05-17")
	if lat.Count != 1 {
		t.Errorf("latency count = %d, want 1", lat.Count)
	}
	const wantLat = int64(2 * 60 * 60)
	if lat.TimeToFirstReviewSecondsP50 == nil || *lat.TimeToFirstReviewSecondsP50 != wantLat {
		t.Errorf("latency p50 = %v, want %d", lat.TimeToFirstReviewSecondsP50, wantLat)
	}
	if lat.TimeToFirstReviewSecondsP90 == nil || *lat.TimeToFirstReviewSecondsP90 != wantLat {
		t.Errorf("latency p90 = %v, want %d", lat.TimeToFirstReviewSecondsP90, wantLat)
	}

	loads := readLoad(t, q, repo.ID, "2026-05-17")
	if len(loads) != 1 || loads[0].ReviewerGhUserID != 2 {
		t.Fatalf("loads = %+v, want one row for reviewer 2", loads)
	}
	if loads[0].Reviews != 1 {
		t.Errorf("reviewer 2 reviews = %d, want 1", loads[0].Reviews)
	}
	if loads[0].ResponseMinutesP50 == nil || *loads[0].ResponseMinutesP50 != 120 {
		t.Errorf("reviewer 2 p50 = %v, want 120", loads[0].ResponseMinutesP50)
	}
}

func TestAggregate_PercentilesNearestRank(t *testing.T) {
	s, q := newStorage(t)
	a := reviewstats.New(s, zaptest.NewLogger(t))

	repo := seedRepo(t, q, false)
	date := time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)
	noon := date.Add(12 * time.Hour)

	// Four PRs (authors 1..4) opened with offsets; reviewer 99 first-
	// reviews all of them at noon, latencies 60, 120, 180, 240.
	// Nearest-rank p50 = sorted[1] = 120, p90 = sorted[3] = 240.
	for i, secs := range []int64{60, 120, 180, 240} {
		prNum := int64(i + 1)
		author := int64(i + 1)
		seedPR(t, q, repo.ID, prNum, author, noon.Add(-time.Duration(secs)*time.Second))
		seedReview(t, q, 2000+prNum, repo.ID, prNum, 99, noon)
	}

	if err := a.Aggregate(context.Background(), date); err != nil {
		t.Fatalf("Aggregate: %v", err)
	}

	lat := readLatency(t, q, repo.ID, "2026-05-17")
	if lat.Count != 4 {
		t.Errorf("latency count = %d, want 4", lat.Count)
	}
	if lat.TimeToFirstReviewSecondsP50 == nil || *lat.TimeToFirstReviewSecondsP50 != 120 {
		t.Errorf("latency p50 = %v, want 120", lat.TimeToFirstReviewSecondsP50)
	}
	if lat.TimeToFirstReviewSecondsP90 == nil || *lat.TimeToFirstReviewSecondsP90 != 240 {
		t.Errorf("latency p90 = %v, want 240", lat.TimeToFirstReviewSecondsP90)
	}

	loads := readLoad(t, q, repo.ID, "2026-05-17")
	if len(loads) != 1 || loads[0].ReviewerGhUserID != 99 {
		t.Fatalf("loads = %+v, want single reviewer 99", loads)
	}
	if loads[0].Reviews != 4 {
		t.Errorf("reviewer 99 reviews = %d, want 4", loads[0].Reviews)
	}
	// Response minutes (truncating int division): 60s=1, 120s=2, 180s=3,
	// 240s=4. p50 nearest-rank with 4 samples = sorted[1] = 2.
	if loads[0].ResponseMinutesP50 == nil || *loads[0].ResponseMinutesP50 != 2 {
		t.Errorf("reviewer 99 p50 minutes = %v, want 2", loads[0].ResponseMinutesP50)
	}
}

func TestAggregate_SelfReviewExcluded(t *testing.T) {
	s, q := newStorage(t)
	a := reviewstats.New(s, zaptest.NewLogger(t))

	repo := seedRepo(t, q, false)
	date := time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)
	noon := date.Add(12 * time.Hour)

	// Author 7 opens PR, then author 7 reviews their own PR.
	seedPR(t, q, repo.ID, 1, 7, noon.Add(-time.Hour))
	seedReview(t, q, 3001, repo.ID, 1, 7, noon)

	if err := a.Aggregate(context.Background(), date); err != nil {
		t.Fatalf("Aggregate: %v", err)
	}

	lat := readLatency(t, q, repo.ID, "2026-05-17")
	if lat.Count != 0 {
		t.Errorf("latency count = %d, want 0 (self-review)", lat.Count)
	}
	if lat.TimeToFirstReviewSecondsP50 != nil || lat.TimeToFirstReviewSecondsP90 != nil {
		t.Errorf("latency percentiles must be nil, got p50=%v p90=%v",
			lat.TimeToFirstReviewSecondsP50, lat.TimeToFirstReviewSecondsP90)
	}
	loads := readLoad(t, q, repo.ID, "2026-05-17")
	if len(loads) != 0 {
		t.Errorf("loads = %+v, want none (self-review excluded)", loads)
	}
}

func TestAggregate_GhostReviewerExcluded(t *testing.T) {
	s, q := newStorage(t)
	a := reviewstats.New(s, zaptest.NewLogger(t))

	repo := seedRepo(t, q, false)
	date := time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)
	noon := date.Add(12 * time.Hour)

	seedPR(t, q, repo.ID, 1, 1, noon.Add(-time.Hour))
	// Ghost reviewer.
	seedReview(t, q, 4001, repo.ID, 1, 0, noon)

	if err := a.Aggregate(context.Background(), date); err != nil {
		t.Fatalf("Aggregate: %v", err)
	}

	lat := readLatency(t, q, repo.ID, "2026-05-17")
	if lat.Count != 0 {
		t.Errorf("latency count = %d, want 0 (ghost)", lat.Count)
	}
	loads := readLoad(t, q, repo.ID, "2026-05-17")
	if len(loads) != 0 {
		t.Errorf("loads = %+v, want none (ghost excluded)", loads)
	}
}

func TestAggregate_WindowBoundaries(t *testing.T) {
	s, q := newStorage(t)
	a := reviewstats.New(s, zaptest.NewLogger(t))

	repo := seedRepo(t, q, false)
	target := time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)
	afterDay := target.Add(24 * time.Hour)
	beforeDay := target.Add(-time.Nanosecond)
	atStart := target
	atEnd := afterDay.Add(-time.Nanosecond)

	// Four PRs by author 1, each reviewed by reviewer 2 at one of the
	// four boundary points. Only atStart + atEnd should count.
	for i, when := range []time.Time{beforeDay, atStart, atEnd, afterDay} {
		prNum := int64(i + 1)
		seedPR(t, q, repo.ID, prNum, 1, when.Add(-time.Minute))
		seedReview(t, q, 5000+prNum, repo.ID, prNum, 2, when)
	}

	if err := a.Aggregate(context.Background(), target); err != nil {
		t.Fatalf("Aggregate: %v", err)
	}

	lat := readLatency(t, q, repo.ID, "2026-05-17")
	if lat.Count != 2 {
		t.Errorf("latency count = %d, want 2 (atStart+atEnd)", lat.Count)
	}
	loads := readLoad(t, q, repo.ID, "2026-05-17")
	if len(loads) != 1 || loads[0].Reviews != 2 {
		t.Errorf("loads = %+v, want reviewer 2 with 2 reviews", loads)
	}
}

func TestAggregate_MultiRepoPartitioning(t *testing.T) {
	s, q := newStorage(t)
	a := reviewstats.New(s, zaptest.NewLogger(t))

	r1 := seedRepo(t, q, false)
	r2 := seedRepo(t, q, false)
	date := time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)
	noon := date.Add(12 * time.Hour)

	// r1: one PR/review with latency 60s.
	seedPR(t, q, r1.ID, 1, 1, noon.Add(-60*time.Second))
	seedReview(t, q, 6001, r1.ID, 1, 2, noon)
	// r2: one PR/review with latency 600s.
	seedPR(t, q, r2.ID, 1, 1, noon.Add(-600*time.Second))
	seedReview(t, q, 6002, r2.ID, 1, 3, noon)

	if err := a.Aggregate(context.Background(), date); err != nil {
		t.Fatalf("Aggregate: %v", err)
	}

	r1Lat := readLatency(t, q, r1.ID, "2026-05-17")
	if r1Lat.Count != 1 || r1Lat.TimeToFirstReviewSecondsP50 == nil || *r1Lat.TimeToFirstReviewSecondsP50 != 60 {
		t.Errorf("r1 latency = %+v, want count=1 p50=60", r1Lat)
	}
	r1Load := readLoad(t, q, r1.ID, "2026-05-17")
	if len(r1Load) != 1 || r1Load[0].ReviewerGhUserID != 2 {
		t.Errorf("r1 load = %+v, want reviewer 2", r1Load)
	}

	r2Lat := readLatency(t, q, r2.ID, "2026-05-17")
	if r2Lat.Count != 1 || r2Lat.TimeToFirstReviewSecondsP50 == nil || *r2Lat.TimeToFirstReviewSecondsP50 != 600 {
		t.Errorf("r2 latency = %+v, want count=1 p50=600", r2Lat)
	}
	r2Load := readLoad(t, q, r2.ID, "2026-05-17")
	if len(r2Load) != 1 || r2Load[0].ReviewerGhUserID != 3 {
		t.Errorf("r2 load = %+v, want reviewer 3", r2Load)
	}
}

func TestAggregate_ArchivedRepoSkipped(t *testing.T) {
	s, q := newStorage(t)
	a := reviewstats.New(s, zaptest.NewLogger(t))

	repo := seedRepo(t, q, true)
	date := time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)
	noon := date.Add(12 * time.Hour)
	seedPR(t, q, repo.ID, 1, 1, noon.Add(-time.Hour))
	seedReview(t, q, 7001, repo.ID, 1, 2, noon)

	if err := a.Aggregate(context.Background(), date); err != nil {
		t.Fatalf("Aggregate: %v", err)
	}

	if lat := readLatency(t, q, repo.ID, "2026-05-17"); lat.Date != "" {
		t.Errorf("archived repo: got latency %+v, want none", lat)
	}
	if loads := readLoad(t, q, repo.ID, "2026-05-17"); len(loads) != 0 {
		t.Errorf("archived repo: got loads %+v, want none", loads)
	}
}

func TestAggregate_MultiReviewerLoad(t *testing.T) {
	s, q := newStorage(t)
	a := reviewstats.New(s, zaptest.NewLogger(t))

	repo := seedRepo(t, q, false)
	date := time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)
	noon := date.Add(12 * time.Hour)

	// One PR by author 1. Reviewer 2 reviews 3 times today, reviewer
	// 3 reviews once today. Latency is anchored to whichever review
	// came first (chronologically) — reviewer 2 at noon.
	seedPR(t, q, repo.ID, 1, 1, noon.Add(-60*time.Minute))
	seedReview(t, q, 8001, repo.ID, 1, 2, noon)
	seedReview(t, q, 8002, repo.ID, 1, 2, noon.Add(time.Hour))
	seedReview(t, q, 8003, repo.ID, 1, 2, noon.Add(2*time.Hour))
	seedReview(t, q, 8004, repo.ID, 1, 3, noon.Add(30*time.Minute))

	if err := a.Aggregate(context.Background(), date); err != nil {
		t.Fatalf("Aggregate: %v", err)
	}

	// Latency: PR first-reviewed at noon, count=1, p50/p90=3600s.
	lat := readLatency(t, q, repo.ID, "2026-05-17")
	if lat.Count != 1 {
		t.Errorf("latency count = %d, want 1", lat.Count)
	}
	if lat.TimeToFirstReviewSecondsP50 == nil || *lat.TimeToFirstReviewSecondsP50 != 3600 {
		t.Errorf("latency p50 = %v, want 3600", lat.TimeToFirstReviewSecondsP50)
	}

	loads := readLoad(t, q, repo.ID, "2026-05-17")
	if len(loads) != 2 {
		t.Fatalf("loads = %+v, want 2 rows", loads)
	}
	// readLoad sorts by reviewer UID — so loads[0]=2, loads[1]=3.
	if loads[0].ReviewerGhUserID != 2 || loads[0].Reviews != 3 {
		t.Errorf("loads[0] = %+v, want reviewer=2 reviews=3", loads[0])
	}
	if loads[1].ReviewerGhUserID != 3 || loads[1].Reviews != 1 {
		t.Errorf("loads[1] = %+v, want reviewer=3 reviews=1", loads[1])
	}
}

func TestAggregate_IdempotentRerunAndStaleLoadCleared(t *testing.T) {
	s, q := newStorage(t)
	a := reviewstats.New(s, zaptest.NewLogger(t))

	repo := seedRepo(t, q, false)
	date := time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)
	noon := date.Add(12 * time.Hour)

	// Reviewer 2 does two reviews on day D. After first aggregate,
	// expect reviews=2 in load.
	seedPR(t, q, repo.ID, 1, 1, noon.Add(-time.Hour))
	seedPR(t, q, repo.ID, 2, 1, noon.Add(-time.Hour))
	seedReview(t, q, 9001, repo.ID, 1, 2, noon)
	seedReview(t, q, 9002, repo.ID, 2, 2, noon)

	if err := a.Aggregate(context.Background(), date); err != nil {
		t.Fatalf("first Aggregate: %v", err)
	}
	loads := readLoad(t, q, repo.ID, "2026-05-17")
	if len(loads) != 1 || loads[0].Reviews != 2 {
		t.Fatalf("after first: loads = %+v, want reviewer 2 with 2 reviews", loads)
	}

	// Re-run with no source changes — identical state.
	if err := a.Aggregate(context.Background(), date); err != nil {
		t.Fatalf("second Aggregate: %v", err)
	}
	loads = readLoad(t, q, repo.ID, "2026-05-17")
	if len(loads) != 1 || loads[0].Reviews != 2 {
		t.Errorf("after rerun: loads = %+v, want reviewer 2 with 2 reviews", loads)
	}

	// Delete one of the reviews and re-run. Reviews should drop to 1.
	if _, err := s.DB().ExecContext(context.Background(),
		"DELETE FROM pr_reviews WHERE gh_id = ?", 9002); err != nil {
		t.Fatalf("DELETE review: %v", err)
	}
	if err := a.Aggregate(context.Background(), date); err != nil {
		t.Fatalf("third Aggregate: %v", err)
	}
	loads = readLoad(t, q, repo.ID, "2026-05-17")
	if len(loads) != 1 || loads[0].Reviews != 1 {
		t.Errorf("after delete one: loads = %+v, want reviewer 2 with 1 review", loads)
	}

	// Delete the remaining review and re-run. The row should disappear
	// (DELETE+INSERT erases the now-orphan reviewer key).
	if _, err := s.DB().ExecContext(context.Background(),
		"DELETE FROM pr_reviews WHERE gh_id = ?", 9001); err != nil {
		t.Fatalf("DELETE review: %v", err)
	}
	if err := a.Aggregate(context.Background(), date); err != nil {
		t.Fatalf("fourth Aggregate: %v", err)
	}
	loads = readLoad(t, q, repo.ID, "2026-05-17")
	if len(loads) != 0 {
		t.Errorf("after delete all: loads = %+v, want empty", loads)
	}
}

func TestAggregate_FirstReviewIsEarliestAcrossReviewers(t *testing.T) {
	s, q := newStorage(t)
	a := reviewstats.New(s, zaptest.NewLogger(t))

	repo := seedRepo(t, q, false)
	date := time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)
	noon := date.Add(12 * time.Hour)

	// Author 1 opens PR 1 hour before noon. Alice (uid=2) reviews at
	// noon; Bob (uid=3) reviews 30 minutes later. The PR contributes
	// ONE latency sample to the daily count, anchored to Alice's
	// review (the earliest). Bob's review still counts in his load
	// row.
	seedPR(t, q, repo.ID, 1, 1, noon.Add(-time.Hour))
	seedReview(t, q, 10001, repo.ID, 1, 2, noon)
	seedReview(t, q, 10002, repo.ID, 1, 3, noon.Add(30*time.Minute))

	if err := a.Aggregate(context.Background(), date); err != nil {
		t.Fatalf("Aggregate: %v", err)
	}

	lat := readLatency(t, q, repo.ID, "2026-05-17")
	if lat.Count != 1 {
		t.Errorf("latency count = %d, want 1 (one PR first-reviewed today)", lat.Count)
	}
	if lat.TimeToFirstReviewSecondsP50 == nil || *lat.TimeToFirstReviewSecondsP50 != 3600 {
		t.Errorf("latency p50 = %v, want 3600 (Alice's review, not Bob's)", lat.TimeToFirstReviewSecondsP50)
	}

	loads := readLoad(t, q, repo.ID, "2026-05-17")
	if len(loads) != 2 {
		t.Fatalf("loads = %+v, want 2 rows (Alice + Bob)", loads)
	}
	if loads[0].ReviewerGhUserID != 2 || loads[0].Reviews != 1 {
		t.Errorf("loads[0] = %+v, want Alice reviews=1", loads[0])
	}
	if loads[1].ReviewerGhUserID != 3 || loads[1].Reviews != 1 {
		t.Errorf("loads[1] = %+v, want Bob reviews=1", loads[1])
	}
}
