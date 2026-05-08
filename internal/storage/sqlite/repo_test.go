package sqlite_test

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/karnstack/tempo/internal/config"
	"github.com/karnstack/tempo/internal/storage/sqlite"
	"github.com/karnstack/tempo/internal/storage/sqlite/sqlitedb"
	"github.com/karnstack/tempo/migrations"
	"go.uber.org/fx/fxtest"
	"go.uber.org/zap/zaptest"
)

func ptr[T any](v T) *T { return &v }

func newTestStore(t *testing.T) (*sqlitedb.Queries, func()) {
	t.Helper()
	lc := fxtest.NewLifecycle(t)
	l := zaptest.NewLogger(t)
	path := filepath.Join(t.TempDir(), "repo_test.db")
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
	return sqlitedb.New(s.DB()), func() { lc.RequireStop() }
}

func randID(t *testing.T) string {
	t.Helper()
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return hex.EncodeToString(b[:])
}

func TestIdentityRoundtrip(t *testing.T) {
	t.Parallel()
	q, stop := newTestStore(t)
	defer stop()
	ctx := context.Background()

	tenant, err := q.CreateTenant(ctx, "acme")
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	if tenant.ID == 0 || tenant.Name != "acme" {
		t.Fatalf("unexpected tenant: %+v", tenant)
	}

	user, err := q.CreateUser(ctx, sqlitedb.CreateUserParams{
		TenantID: tenant.ID, Email: "a@b.com", PasswordHash: "x", Role: "admin",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	got, err := q.GetUserByEmail(ctx, sqlitedb.GetUserByEmailParams{
		TenantID: tenant.ID, Email: "a@b.com",
	})
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	if got.ID != user.ID {
		t.Fatalf("GetUserByEmail id = %d, want %d", got.ID, user.ID)
	}

	count, err := q.CountUsersByTenant(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("CountUsersByTenant: %v", err)
	}
	if count != 1 {
		t.Fatalf("CountUsersByTenant = %d, want 1", count)
	}

	sid := randID(t)
	if _, err := q.CreateSession(ctx, sqlitedb.CreateSessionParams{
		ID: sid, UserID: user.ID, ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := q.GetSession(ctx, sid); err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if err := q.DeleteSession(ctx, sid); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
}

func TestConnectionAndRepoRoundtrip(t *testing.T) {
	t.Parallel()
	q, stop := newTestStore(t)
	defer stop()
	ctx := context.Background()

	tenant, err := q.CreateTenant(ctx, "acme")
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}

	tok, err := q.CreateGhToken(ctx, sqlitedb.CreateGhTokenParams{
		TenantID: tenant.ID, Label: "ci", EncryptedPat: []byte("opaque"), Scopes: "repo",
	})
	if err != nil {
		t.Fatalf("CreateGhToken: %v", err)
	}

	conn, err := q.CreateConnection(ctx, sqlitedb.CreateConnectionParams{
		TenantID: tenant.ID, Kind: "repo", Owner: "karnstack",
		Name: ptr("tempo"), TokenID: tok.ID, BackfillFrom: time.Now().Add(-30 * 24 * time.Hour),
		Status: "active",
	})
	if err != nil {
		t.Fatalf("CreateConnection: %v", err)
	}

	repo, err := q.CreateRepo(ctx, sqlitedb.CreateRepoParams{
		TenantID: tenant.ID, ConnectionID: conn.ID, GhID: 12345,
		Owner: "karnstack", Name: "tempo", DefaultBranch: "main", Archived: 0,
	})
	if err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}

	conns, err := q.ListConnectionsByTenant(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("ListConnectionsByTenant: %v", err)
	}
	if len(conns) != 1 || conns[0].ID != conn.ID {
		t.Fatalf("ListConnectionsByTenant = %+v", conns)
	}

	now := time.Now()
	if err := q.UpdateConnectionLastSync(ctx, sqlitedb.UpdateConnectionLastSyncParams{
		LastSyncAt: &now, ID: conn.ID,
	}); err != nil {
		t.Fatalf("UpdateConnectionLastSync: %v", err)
	}
	got, err := q.GetConnection(ctx, conn.ID)
	if err != nil {
		t.Fatalf("GetConnection: %v", err)
	}
	if got.LastSyncAt == nil {
		t.Fatal("GetConnection.LastSyncAt nil after update")
	}

	repos, err := q.ListReposByConnection(ctx, conn.ID)
	if err != nil {
		t.Fatalf("ListReposByConnection: %v", err)
	}
	if len(repos) != 1 || repos[0].ID != repo.ID {
		t.Fatalf("ListReposByConnection = %+v", repos)
	}
}

func TestPullRequestUpsertIdempotent(t *testing.T) {
	t.Parallel()
	q, stop := newTestStore(t)
	defer stop()
	ctx := context.Background()

	createdAt := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	params := sqlitedb.UpsertPullRequestParams{
		RepoID: 1, Number: 42, GhID: 999, AuthorGhUserID: 7,
		State: "open", Title: "first", CreatedAt: createdAt,
		Additions: 10, Deletions: 2, BaseRef: "main", HeadRef: "feat/x", Draft: 0,
	}
	if err := q.UpsertPullRequest(ctx, params); err != nil {
		t.Fatalf("UpsertPullRequest insert: %v", err)
	}

	// Re-poll: same PK, updated fields. Should not error and should reflect
	// the new values.
	mergedAt := createdAt.Add(2 * time.Hour)
	params.State = "merged"
	params.Title = "first (merged)"
	params.MergedAt = &mergedAt
	params.Additions = 12
	if err := q.UpsertPullRequest(ctx, params); err != nil {
		t.Fatalf("UpsertPullRequest re-poll: %v", err)
	}

	got, err := q.GetPullRequest(ctx, sqlitedb.GetPullRequestParams{RepoID: 1, Number: 42})
	if err != nil {
		t.Fatalf("GetPullRequest: %v", err)
	}
	if got.State != "merged" {
		t.Fatalf("State = %q, want merged", got.State)
	}
	if got.MergedAt == nil || !got.MergedAt.Equal(mergedAt) {
		t.Fatalf("MergedAt = %v, want %v", got.MergedAt, mergedAt)
	}
	if got.Additions != 12 {
		t.Fatalf("Additions = %d, want 12", got.Additions)
	}
}

func TestDailyEngineerStatsRange(t *testing.T) {
	t.Parallel()
	q, stop := newTestStore(t)
	defer stop()
	ctx := context.Background()

	for i, date := range []string{"2026-05-01", "2026-05-02", "2026-05-03"} {
		if err := q.UpsertDailyEngineerStats(ctx, sqlitedb.UpsertDailyEngineerStatsParams{
			Date: date, RepoID: 1, GhUserID: 7,
			Commits: int64(i + 1), PrsOpened: int64(i),
		}); err != nil {
			t.Fatalf("UpsertDailyEngineerStats[%d]: %v", i, err)
		}
	}

	rows, err := q.ListDailyEngineerStatsByUserBetween(ctx, sqlitedb.ListDailyEngineerStatsByUserBetweenParams{
		GhUserID: 7, FromDate: "2026-05-01", ToDate: "2026-05-04",
	})
	if err != nil {
		t.Fatalf("ListDailyEngineerStatsByUserBetween: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("len(rows) = %d, want 3", len(rows))
	}
	if rows[0].Date != "2026-05-01" || rows[2].Date != "2026-05-03" {
		t.Fatalf("unexpected ordering: %+v", rows)
	}

	// Re-aggregation should overwrite, not append.
	if err := q.UpsertDailyEngineerStats(ctx, sqlitedb.UpsertDailyEngineerStatsParams{
		Date: "2026-05-02", RepoID: 1, GhUserID: 7, Commits: 99,
	}); err != nil {
		t.Fatalf("UpsertDailyEngineerStats overwrite: %v", err)
	}
	rows, err = q.ListDailyEngineerStatsByUserBetween(ctx, sqlitedb.ListDailyEngineerStatsByUserBetweenParams{
		GhUserID: 7, FromDate: "2026-05-01", ToDate: "2026-05-04",
	})
	if err != nil {
		t.Fatalf("re-list: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("len(rows) after upsert = %d, want 3", len(rows))
	}
	for _, r := range rows {
		if r.Date == "2026-05-02" && r.Commits != 99 {
			t.Fatalf("overwrite did not take: row = %+v", r)
		}
	}
}

func TestSyncRunAndCursor(t *testing.T) {
	t.Parallel()
	q, stop := newTestStore(t)
	defer stop()
	ctx := context.Background()

	started := time.Now()
	run, err := q.StartSyncRun(ctx, sqlitedb.StartSyncRunParams{ConnectionID: 1, StartedAt: started})
	if err != nil {
		t.Fatalf("StartSyncRun: %v", err)
	}
	if run.Ok != 0 || run.FinishedAt != nil {
		t.Fatalf("unfinished run should have ok=0 and FinishedAt=nil: %+v", run)
	}

	finished := started.Add(30 * time.Second)
	if err := q.FinishSyncRun(ctx, sqlitedb.FinishSyncRunParams{
		FinishedAt: &finished, Ok: 1, Items: 42,
		RateLimitRemaining: ptr(int64(4500)), Error: "", ID: run.ID,
	}); err != nil {
		t.Fatalf("FinishSyncRun: %v", err)
	}

	got, err := q.GetLatestSyncRun(ctx, 1)
	if err != nil {
		t.Fatalf("GetLatestSyncRun: %v", err)
	}
	if got.Ok != 1 || got.Items != 42 || got.FinishedAt == nil {
		t.Fatalf("FinishSyncRun did not update row: %+v", got)
	}

	if err := q.UpsertSyncCursor(ctx, sqlitedb.UpsertSyncCursorParams{
		ConnectionID: 1, Resource: "pull_requests", Cursor: "abc", UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("UpsertSyncCursor insert: %v", err)
	}
	if err := q.UpsertSyncCursor(ctx, sqlitedb.UpsertSyncCursorParams{
		ConnectionID: 1, Resource: "pull_requests", Cursor: "def", UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("UpsertSyncCursor update: %v", err)
	}
	cur, err := q.GetSyncCursor(ctx, sqlitedb.GetSyncCursorParams{ConnectionID: 1, Resource: "pull_requests"})
	if err != nil {
		t.Fatalf("GetSyncCursor: %v", err)
	}
	if cur.Cursor != "def" {
		t.Fatalf("cursor = %q, want def", cur.Cursor)
	}
}

func TestGhUserUpsertReturnsRow(t *testing.T) {
	t.Parallel()
	q, stop := newTestStore(t)
	defer stop()
	ctx := context.Background()

	tenant, err := q.CreateTenant(ctx, "acme")
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}

	now := time.Now()
	first, err := q.UpsertGhUser(ctx, sqlitedb.UpsertGhUserParams{
		TenantID: tenant.ID, GhID: 7, Login: "alice", Name: ptr("Alice"), LastSeenAt: &now,
	})
	if err != nil {
		t.Fatalf("UpsertGhUser insert: %v", err)
	}
	later := now.Add(time.Hour)
	second, err := q.UpsertGhUser(ctx, sqlitedb.UpsertGhUserParams{
		TenantID: tenant.ID, GhID: 7, Login: "alice2", Name: ptr("Alice 2"), LastSeenAt: &later,
	})
	if err != nil {
		t.Fatalf("UpsertGhUser update: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("upsert produced new id: first=%d second=%d", first.ID, second.ID)
	}
	if second.Login != "alice2" {
		t.Fatalf("Login = %q, want alice2", second.Login)
	}
}

func TestGetSessionMissing(t *testing.T) {
	t.Parallel()
	q, stop := newTestStore(t)
	defer stop()
	ctx := context.Background()

	_, err := q.GetSession(ctx, "does-not-exist")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("err = %v, want sql.ErrNoRows", err)
	}
}
