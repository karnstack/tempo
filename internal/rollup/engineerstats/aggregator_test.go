package engineerstats_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/karnstack/tempo/internal/config"
	"github.com/karnstack/tempo/internal/rollup/engineerstats"
	"github.com/karnstack/tempo/internal/storage"
	"github.com/karnstack/tempo/internal/storage/sqlite"
	"github.com/karnstack/tempo/internal/storage/sqlite/sqlitedb"
	"github.com/karnstack/tempo/migrations"
	"go.uber.org/fx/fxtest"
	"go.uber.org/zap/zaptest"
)

// --- harness ------------------------------------------------------

// newStorage builds an on-disk sqlite store with all migrations applied
// and returns the storage handle + a queries handle.
func newStorage(t *testing.T) (storage.Storage, *sqlitedb.Queries) {
	t.Helper()
	lc := fxtest.NewLifecycle(t)
	l := zaptest.NewLogger(t)
	path := filepath.Join(t.TempDir(), "engineerstats.db")
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
		GhID:          int64(time.Now().UnixNano()), // unique per call
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

// seedUser inserts a gh_users row scoped to the repo's tenant and
// returns its row id. The id (auto-incremented) is what every
// *_gh_user_id column references in this schema.
func seedUser(t *testing.T, q *sqlitedb.Queries, tenantID, ghID int64, login string) int64 {
	t.Helper()
	u, err := q.UpsertGhUser(context.Background(), sqlitedb.UpsertGhUserParams{
		TenantID: tenantID,
		GhID:     ghID,
		Login:    login,
	})
	if err != nil {
		t.Fatalf("UpsertGhUser %s: %v", login, err)
	}
	return u.ID
}

// --- seeders for raw events --------------------------------------

func seedCommit(t *testing.T, q *sqlitedb.Queries, repoID, authorID int64, sha string, authoredAt time.Time) {
	t.Helper()
	if err := q.UpsertCommit(context.Background(), sqlitedb.UpsertCommitParams{
		RepoID:            repoID,
		Sha:               sha,
		AuthorGhUserID:    authorID,
		CommitterGhUserID: authorID,
		AuthoredAt:        authoredAt,
	}); err != nil {
		t.Fatalf("UpsertCommit %s: %v", sha, err)
	}
}

// seedPR upserts a PR. mergedAt nil means the PR is not merged. adds
// and dels are the per-PR line counts; pass 0 if not exercising
// additions/deletions in the test.
func seedPR(t *testing.T, q *sqlitedb.Queries, repoID, prNum, authorID int64, createdAt time.Time, mergedAt *time.Time, adds, dels int64) {
	t.Helper()
	state := "open"
	if mergedAt != nil {
		state = "merged"
	}
	if err := q.UpsertPullRequest(context.Background(), sqlitedb.UpsertPullRequestParams{
		RepoID:         repoID,
		Number:         prNum,
		GhID:           int64(time.Now().UnixNano()) + prNum,
		AuthorGhUserID: authorID,
		State:          state,
		Title:          "t",
		CreatedAt:      createdAt,
		UpdatedAt:      createdAt,
		MergedAt:       mergedAt,
		Additions:      adds,
		Deletions:      dels,
		BaseRef:        "main",
		HeadRef:        "f",
	}); err != nil {
		t.Fatalf("UpsertPullRequest #%d: %v", prNum, err)
	}
}

func seedReview(t *testing.T, q *sqlitedb.Queries, ghID, repoID, prNum, reviewerID int64, submittedAt time.Time) {
	t.Helper()
	if err := q.UpsertPullRequestReview(context.Background(), sqlitedb.UpsertPullRequestReviewParams{
		GhID:             ghID,
		PrRepoID:         repoID,
		PrNumber:         prNum,
		ReviewerGhUserID: reviewerID,
		State:            "APPROVED",
		SubmittedAt:      submittedAt,
	}); err != nil {
		t.Fatalf("UpsertPullRequestReview gh=%d: %v", ghID, err)
	}
}

func seedReviewComment(t *testing.T, q *sqlitedb.Queries, ghID, repoID, prNum, authorID int64, createdAt time.Time) {
	t.Helper()
	if err := q.UpsertPullRequestReviewComment(context.Background(), sqlitedb.UpsertPullRequestReviewCommentParams{
		GhID:           ghID,
		PrRepoID:       repoID,
		PrNumber:       prNum,
		AuthorGhUserID: authorID,
		CreatedAt:      createdAt,
	}); err != nil {
		t.Fatalf("UpsertPullRequestReviewComment gh=%d: %v", ghID, err)
	}
}

func seedIssueComment(t *testing.T, q *sqlitedb.Queries, ghID, repoID, prNum, authorID int64, createdAt time.Time) {
	t.Helper()
	if err := q.UpsertPullRequestIssueComment(context.Background(), sqlitedb.UpsertPullRequestIssueCommentParams{
		GhID:           ghID,
		PrRepoID:       repoID,
		PrNumber:       prNum,
		AuthorGhUserID: authorID,
		CreatedAt:      createdAt,
	}); err != nil {
		t.Fatalf("UpsertPullRequestIssueComment gh=%d: %v", ghID, err)
	}
}

// readRows returns daily_engineer_stats for (date, repo_id) keyed by user id.
func readRows(t *testing.T, q *sqlitedb.Queries, repoID int64, date string) map[int64]sqlitedb.DailyEngineerStat {
	t.Helper()
	rows, err := q.ListDailyEngineerStatsByRepoBetween(context.Background(), sqlitedb.ListDailyEngineerStatsByRepoBetweenParams{
		RepoID:   repoID,
		FromDate: date,
		ToDate:   addDate(date, 1),
	})
	if err != nil {
		t.Fatalf("ListDailyEngineerStatsByRepoBetween: %v", err)
	}
	out := make(map[int64]sqlitedb.DailyEngineerStat, len(rows))
	for _, r := range rows {
		out[r.GhUserID] = r
	}
	return out
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
	a := engineerstats.New(s, zaptest.NewLogger(t))
	date := time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)
	if err := a.Aggregate(context.Background(), date); err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
}

func TestAggregate_AllSourcesPopulated(t *testing.T) {
	s, q := newStorage(t)
	a := engineerstats.New(s, zaptest.NewLogger(t))

	repo := seedRepo(t, q, false)
	alice := seedUser(t, q, repo.TenantID, 1001, "alice")

	date := time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)
	noon := date.Add(12 * time.Hour)

	// 2 commits, 1 opened PR, 1 merged PR (250+/50-), 3 reviews,
	// 2 review comments + 1 issue comment = 3 comments total.
	seedCommit(t, q, repo.ID, alice, "sha1", noon)
	seedCommit(t, q, repo.ID, alice, "sha2", noon.Add(time.Hour))
	seedPR(t, q, repo.ID, 100, alice, noon, nil, 0, 0)
	merged := noon.Add(2 * time.Hour)
	seedPR(t, q, repo.ID, 101, alice, noon.Add(-24*time.Hour), &merged, 250, 50)
	seedReview(t, q, 9001, repo.ID, 100, alice, noon)
	seedReview(t, q, 9002, repo.ID, 100, alice, noon.Add(time.Hour))
	seedReview(t, q, 9003, repo.ID, 100, alice, noon.Add(2*time.Hour))
	seedReviewComment(t, q, 8001, repo.ID, 100, alice, noon)
	seedReviewComment(t, q, 8002, repo.ID, 100, alice, noon)
	seedIssueComment(t, q, 7001, repo.ID, 100, alice, noon)

	if err := a.Aggregate(context.Background(), date); err != nil {
		t.Fatalf("Aggregate: %v", err)
	}

	rows := readRows(t, q, repo.ID, "2026-05-17")
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1; rows=%+v", len(rows), rows)
	}
	r := rows[alice]
	switch {
	case r.Commits != 2:
		t.Errorf("commits = %d, want 2", r.Commits)
	case r.PrsOpened != 1:
		t.Errorf("prs_opened = %d, want 1", r.PrsOpened)
	case r.PrsMerged != 1:
		t.Errorf("prs_merged = %d, want 1", r.PrsMerged)
	case r.ReviewsGiven != 3:
		t.Errorf("reviews_given = %d, want 3", r.ReviewsGiven)
	case r.Comments != 3:
		t.Errorf("comments = %d, want 3", r.Comments)
	case r.Additions != 250:
		t.Errorf("additions = %d, want 250", r.Additions)
	case r.Deletions != 50:
		t.Errorf("deletions = %d, want 50", r.Deletions)
	}
}

func TestAggregate_MultiUserMultiRepo(t *testing.T) {
	s, q := newStorage(t)
	a := engineerstats.New(s, zaptest.NewLogger(t))

	r1 := seedRepo(t, q, false)
	r2 := seedRepo(t, q, false)
	alice := seedUser(t, q, r1.TenantID, 1001, "alice")
	bob := seedUser(t, q, r1.TenantID, 1002, "bob")

	date := time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)
	noon := date.Add(12 * time.Hour)

	// repo1: alice commits, bob reviews
	seedCommit(t, q, r1.ID, alice, "r1-sha1", noon)
	seedReview(t, q, 9001, r1.ID, 1, bob, noon)

	// repo2: alice reviews, bob commits
	seedCommit(t, q, r2.ID, bob, "r2-sha1", noon)
	seedReview(t, q, 9002, r2.ID, 1, alice, noon)

	if err := a.Aggregate(context.Background(), date); err != nil {
		t.Fatalf("Aggregate: %v", err)
	}

	r1Rows := readRows(t, q, r1.ID, "2026-05-17")
	if got := r1Rows[alice].Commits; got != 1 {
		t.Errorf("r1 alice commits = %d, want 1", got)
	}
	if got := r1Rows[bob].ReviewsGiven; got != 1 {
		t.Errorf("r1 bob reviews_given = %d, want 1", got)
	}
	if got := r1Rows[alice].ReviewsGiven; got != 0 {
		t.Errorf("r1 alice reviews_given = %d, want 0", got)
	}

	r2Rows := readRows(t, q, r2.ID, "2026-05-17")
	if got := r2Rows[bob].Commits; got != 1 {
		t.Errorf("r2 bob commits = %d, want 1", got)
	}
	if got := r2Rows[alice].ReviewsGiven; got != 1 {
		t.Errorf("r2 alice reviews_given = %d, want 1", got)
	}
}

func TestAggregate_GhostAuthorFiltered(t *testing.T) {
	s, q := newStorage(t)
	a := engineerstats.New(s, zaptest.NewLogger(t))

	repo := seedRepo(t, q, false)
	alice := seedUser(t, q, repo.TenantID, 1001, "alice")

	date := time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)
	noon := date.Add(12 * time.Hour)

	// alice has a commit; Ghost (user_id=0) has another commit; only
	// alice should show up.
	seedCommit(t, q, repo.ID, alice, "alice-sha", noon)
	seedCommit(t, q, repo.ID, 0, "ghost-sha", noon)

	if err := a.Aggregate(context.Background(), date); err != nil {
		t.Fatalf("Aggregate: %v", err)
	}

	rows := readRows(t, q, repo.ID, "2026-05-17")
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1 (Ghost should be filtered); rows=%+v", len(rows), rows)
	}
	if _, ok := rows[0]; ok {
		t.Errorf("Ghost row (user_id=0) leaked into output")
	}
	if rows[alice].Commits != 1 {
		t.Errorf("alice commits = %d, want 1", rows[alice].Commits)
	}
}

func TestAggregate_ArchivedRepoSkipped(t *testing.T) {
	s, q := newStorage(t)
	a := engineerstats.New(s, zaptest.NewLogger(t))

	repo := seedRepo(t, q, true)
	alice := seedUser(t, q, repo.TenantID, 1001, "alice")

	date := time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)
	seedCommit(t, q, repo.ID, alice, "sha", date.Add(12*time.Hour))

	if err := a.Aggregate(context.Background(), date); err != nil {
		t.Fatalf("Aggregate: %v", err)
	}

	rows := readRows(t, q, repo.ID, "2026-05-17")
	if len(rows) != 0 {
		t.Errorf("rows = %d for archived repo, want 0", len(rows))
	}
}

func TestAggregate_WindowBoundaries(t *testing.T) {
	s, q := newStorage(t)
	a := engineerstats.New(s, zaptest.NewLogger(t))

	repo := seedRepo(t, q, false)
	alice := seedUser(t, q, repo.TenantID, 1001, "alice")

	target := time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)

	// just before and just after the target day → excluded.
	beforeDay := target.Add(-time.Nanosecond)
	afterDay := target.Add(24 * time.Hour)
	atStart := target               // inclusive start
	atEnd := afterDay.Add(-time.Nanosecond) // inclusive end

	seedCommit(t, q, repo.ID, alice, "before", beforeDay)
	seedCommit(t, q, repo.ID, alice, "atStart", atStart)
	seedCommit(t, q, repo.ID, alice, "atEnd", atEnd)
	seedCommit(t, q, repo.ID, alice, "after", afterDay)

	if err := a.Aggregate(context.Background(), target); err != nil {
		t.Fatalf("Aggregate: %v", err)
	}

	rows := readRows(t, q, repo.ID, "2026-05-17")
	if got := rows[alice].Commits; got != 2 {
		t.Errorf("commits = %d, want 2 (atStart + atEnd only)", got)
	}
}

func TestAggregate_IdempotentRerunAndStaleRowsCleared(t *testing.T) {
	s, q := newStorage(t)
	a := engineerstats.New(s, zaptest.NewLogger(t))

	repo := seedRepo(t, q, false)
	alice := seedUser(t, q, repo.TenantID, 1001, "alice")

	date := time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)
	noon := date.Add(12 * time.Hour)
	seedCommit(t, q, repo.ID, alice, "sha1", noon)

	if err := a.Aggregate(context.Background(), date); err != nil {
		t.Fatalf("first Aggregate: %v", err)
	}
	first := readRows(t, q, repo.ID, "2026-05-17")
	if first[alice].Commits != 1 {
		t.Fatalf("after first run: commits = %d, want 1", first[alice].Commits)
	}

	// Re-run with no source changes → identical state.
	if err := a.Aggregate(context.Background(), date); err != nil {
		t.Fatalf("second Aggregate: %v", err)
	}
	if readRows(t, q, repo.ID, "2026-05-17")[alice].Commits != 1 {
		t.Errorf("rerun changed commits count")
	}

	// Delete the underlying commit and re-run → daily row goes away,
	// not stale. Catches the "must DELETE before re-aggregate" bug.
	if _, err := s.DB().ExecContext(context.Background(),
		"DELETE FROM commits WHERE sha = ?", "sha1"); err != nil {
		t.Fatalf("DELETE commit: %v", err)
	}
	if err := a.Aggregate(context.Background(), date); err != nil {
		t.Fatalf("third Aggregate: %v", err)
	}
	rows := readRows(t, q, repo.ID, "2026-05-17")
	if len(rows) != 0 {
		t.Errorf("after source delete: rows = %d, want 0; rows=%+v", len(rows), rows)
	}
}

func TestAggregate_PRsOpenedYesterdayMergedTodayCountsOnce(t *testing.T) {
	s, q := newStorage(t)
	a := engineerstats.New(s, zaptest.NewLogger(t))

	repo := seedRepo(t, q, false)
	alice := seedUser(t, q, repo.TenantID, 1001, "alice")

	yesterday := time.Date(2026, 5, 16, 0, 0, 0, 0, time.UTC)
	today := yesterday.AddDate(0, 0, 1)
	openedAt := yesterday.Add(15 * time.Hour)
	mergedAt := today.Add(15 * time.Hour)

	seedPR(t, q, repo.ID, 1, alice, openedAt, &mergedAt, 10, 5)

	// On yesterday: prs_opened = 1, prs_merged = 0.
	if err := a.Aggregate(context.Background(), yesterday); err != nil {
		t.Fatalf("Aggregate yesterday: %v", err)
	}
	y := readRows(t, q, repo.ID, "2026-05-16")
	if y[alice].PrsOpened != 1 || y[alice].PrsMerged != 0 {
		t.Errorf("yesterday: opened=%d merged=%d, want 1/0", y[alice].PrsOpened, y[alice].PrsMerged)
	}

	// On today: prs_opened = 0, prs_merged = 1 + adds/dels.
	if err := a.Aggregate(context.Background(), today); err != nil {
		t.Fatalf("Aggregate today: %v", err)
	}
	tRows := readRows(t, q, repo.ID, "2026-05-17")
	if tRows[alice].PrsOpened != 0 || tRows[alice].PrsMerged != 1 {
		t.Errorf("today: opened=%d merged=%d, want 0/1", tRows[alice].PrsOpened, tRows[alice].PrsMerged)
	}
	if tRows[alice].Additions != 10 || tRows[alice].Deletions != 5 {
		t.Errorf("today: adds=%d dels=%d, want 10/5", tRows[alice].Additions, tRows[alice].Deletions)
	}
}

