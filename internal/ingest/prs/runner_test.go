package prs_test

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/karnstack/tempo/internal/config"
	"github.com/karnstack/tempo/internal/github"
	"github.com/karnstack/tempo/internal/github/vcr"
	"github.com/karnstack/tempo/internal/ingest/prs"
	"github.com/karnstack/tempo/internal/storage/sqlite"
	"github.com/karnstack/tempo/internal/storage/sqlite/sqlitedb"
	"github.com/karnstack/tempo/migrations"
	"go.uber.org/fx/fxtest"
	"go.uber.org/zap/zaptest"
)

// --- helpers ---

func newQueries(t *testing.T) *sqlitedb.Queries {
	t.Helper()
	lc := fxtest.NewLifecycle(t)
	l := zaptest.NewLogger(t)
	path := filepath.Join(t.TempDir(), "prs_runner.db")
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

func seedTenant(t *testing.T, q *sqlitedb.Queries) int64 {
	t.Helper()
	tn, err := q.CreateTenant(context.Background(), "acme")
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	return tn.ID
}

func seedToken(t *testing.T, q *sqlitedb.Queries, tenantID int64) int64 {
	t.Helper()
	tok, err := q.CreateGhToken(context.Background(), sqlitedb.CreateGhTokenParams{
		TenantID:     tenantID,
		Label:        "test",
		EncryptedPat: []byte("ignored-by-runner"),
		Scopes:       "repo",
	})
	if err != nil {
		t.Fatalf("CreateGhToken: %v", err)
	}
	return tok.ID
}

func seedConnection(t *testing.T, q *sqlitedb.Queries, tenantID, tokenID int64, owner string, name *string, backfillFrom time.Time) sqlitedb.Connection {
	t.Helper()
	c, err := q.CreateConnection(context.Background(), sqlitedb.CreateConnectionParams{
		TenantID:     tenantID,
		Kind:         "repo",
		Owner:        owner,
		Name:         name,
		TokenID:      tokenID,
		BackfillFrom: backfillFrom,
		Status:       "active",
	})
	if err != nil {
		t.Fatalf("CreateConnection: %v", err)
	}
	return c
}

func seedRepo(t *testing.T, q *sqlitedb.Queries, tenantID, connID int64, ghID int64, owner, name string) sqlitedb.Repo {
	t.Helper()
	r, err := q.CreateRepo(context.Background(), sqlitedb.CreateRepoParams{
		TenantID:      tenantID,
		ConnectionID:  connID,
		GhID:          ghID,
		Owner:         owner,
		Name:          name,
		DefaultBranch: "main",
		Archived:      0,
	})
	if err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}
	return r
}

func strPtr(s string) *string { return &s }

func newReplayClient(t *testing.T, cassettePath string) *github.Client {
	t.Helper()
	tr, err := vcr.NewTransport(cassettePath, vcr.ModeReplay)
	if err != nil {
		t.Fatalf("vcr.NewTransport(%s): %v", cassettePath, err)
	}
	t.Cleanup(func() {
		if err := tr.Close(); err != nil {
			t.Errorf("vcr.Close: %v", err)
		}
		if err := tr.Done(); err != nil {
			t.Errorf("vcr.Done: %v", err)
		}
	})
	return github.New("test-token",
		github.WithHTTPClient(&http.Client{Transport: tr}),
		github.WithBackoff(func(int) time.Duration { return 0 }),
	)
}

func mustParse(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return v
}

// --- tests ---

func TestRun_HappyPath_SinglePage(t *testing.T) {
	t.Parallel()
	q := newQueries(t)
	tn := seedTenant(t, q)
	tok := seedToken(t, q, tn)
	conn := seedConnection(t, q, tn, tok, "karnstack", strPtr("tempo"),
		mustParse(t, "2026-01-01T00:00:00Z"))
	repo := seedRepo(t, q, tn, conn.ID, 5000001, "karnstack", "tempo")

	gh := newReplayClient(t, "testdata/list_single_page.json")
	r := prs.New(q, zaptest.NewLogger(t))

	out, err := r.Run(context.Background(), conn, gh)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Items != 4 {
		t.Errorf("Items = %d, want 4", out.Items)
	}
	if out.RateLimitRemaining != nil {
		t.Errorf("RateLimitRemaining = %v, want nil (cassette has no rate-limit headers)", *out.RateLimitRemaining)
	}

	// 4 PRs landed for this repo.
	prsRows, err := q.ListPullRequestsByRepoBetween(context.Background(), sqlitedb.ListPullRequestsByRepoBetweenParams{
		RepoID: repo.ID,
		FromTs: mustParse(t, "2026-01-01T00:00:00Z"),
		ToTs:   mustParse(t, "2027-01-01T00:00:00Z"),
	})
	if err != nil {
		t.Fatalf("ListPullRequestsByRepoBetween: %v", err)
	}
	if len(prsRows) != 4 {
		t.Fatalf("len(prs) = %d, want 4", len(prsRows))
	}

	// 3 gh_users rows: alice (User), renovate[bot] (Bot), old-bob (Mannequin).
	// The Ghost author on PR #98 must NOT have a gh_users row.
	users, err := q.ListGhUsersByTenant(context.Background(), tn)
	if err != nil {
		t.Fatalf("ListGhUsersByTenant: %v", err)
	}
	if len(users) != 3 {
		t.Fatalf("len(gh_users) = %d, want 3", len(users))
	}
	wantLogins := map[string]int64{"alice": 2001, "renovate[bot]": 2002, "old-bob": 2003}
	for _, u := range users {
		want, ok := wantLogins[u.Login]
		if !ok {
			t.Errorf("unexpected gh_user %q", u.Login)
			continue
		}
		if u.GhID != want {
			t.Errorf("gh_user %q: GhID = %d, want %d", u.Login, u.GhID, want)
		}
		if u.LastSeenAt == nil {
			t.Errorf("gh_user %q: LastSeenAt = nil, want pr.UpdatedAt", u.Login)
		}
	}

	// PR #98's author was null in the cassette → author_gh_user_id must be 0,
	// and no gh_users row should have been created for it.
	pr98, err := q.GetPullRequest(context.Background(), sqlitedb.GetPullRequestParams{
		RepoID: repo.ID, Number: 98,
	})
	if err != nil {
		t.Fatalf("GetPullRequest #98: %v", err)
	}
	if pr98.AuthorGhUserID != 0 {
		t.Errorf("pr #98 author_gh_user_id = %d, want 0 (Ghost)", pr98.AuthorGhUserID)
	}

	// One sync_cursors row pinned to the max updatedAt in the page (PR #101).
	cur, err := q.GetSyncCursor(context.Background(), sqlitedb.GetSyncCursorParams{
		ConnectionID: conn.ID,
		Resource:     "prs:karnstack/tempo",
	})
	if err != nil {
		t.Fatalf("GetSyncCursor: %v", err)
	}
	if cur.Cursor != "2026-04-12T15:30:00Z" {
		t.Errorf("cursor = %q, want %q", cur.Cursor, "2026-04-12T15:30:00Z")
	}
}

func TestRun_MultiPage_AdvancesCursor(t *testing.T) {
	t.Parallel()
	q := newQueries(t)
	tn := seedTenant(t, q)
	tok := seedToken(t, q, tn)
	conn := seedConnection(t, q, tn, tok, "karnstack", strPtr("multi"),
		mustParse(t, "2026-01-01T00:00:00Z"))
	repo := seedRepo(t, q, tn, conn.ID, 5000002, "karnstack", "multi")

	gh := newReplayClient(t, "testdata/list_two_pages.json")
	r := prs.New(q, zaptest.NewLogger(t))

	out, err := r.Run(context.Background(), conn, gh)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Items != 4 {
		t.Errorf("Items = %d, want 4 (2 PRs per page × 2 pages)", out.Items)
	}

	prsRows, err := q.ListPullRequestsByRepoBetween(context.Background(), sqlitedb.ListPullRequestsByRepoBetweenParams{
		RepoID: repo.ID,
		FromTs: mustParse(t, "2026-01-01T00:00:00Z"),
		ToTs:   mustParse(t, "2027-01-01T00:00:00Z"),
	})
	if err != nil {
		t.Fatalf("ListPullRequestsByRepoBetween: %v", err)
	}
	if len(prsRows) != 4 {
		t.Fatalf("len(prs) = %d, want 4", len(prsRows))
	}

	cur, err := q.GetSyncCursor(context.Background(), sqlitedb.GetSyncCursorParams{
		ConnectionID: conn.ID,
		Resource:     "prs:karnstack/multi",
	})
	if err != nil {
		t.Fatalf("GetSyncCursor: %v", err)
	}
	// PR #200 has the latest updatedAt across both pages.
	if cur.Cursor != "2026-04-15T10:00:00Z" {
		t.Errorf("cursor = %q, want %q (max updatedAt across both pages)",
			cur.Cursor, "2026-04-15T10:00:00Z")
	}
}
