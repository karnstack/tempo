package prconvo_test

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/karnstack/tempo/internal/config"
	"github.com/karnstack/tempo/internal/github"
	"github.com/karnstack/tempo/internal/github/vcr"
	"github.com/karnstack/tempo/internal/ingest/prconvo"
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
	path := filepath.Join(t.TempDir(), "prconvo_runner.db")
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

func seedPR(t *testing.T, q *sqlitedb.Queries, repoID int64, number int64, ghID int64, updatedAt time.Time) {
	t.Helper()
	if err := q.UpsertPullRequest(context.Background(), sqlitedb.UpsertPullRequestParams{
		RepoID:         repoID,
		Number:         number,
		GhID:           ghID,
		AuthorGhUserID: 0,
		State:          "OPEN",
		Title:          "seed",
		CreatedAt:      updatedAt.Add(-24 * time.Hour),
		UpdatedAt:      updatedAt,
		Additions:      0,
		Deletions:      0,
		BaseRef:        "main",
		HeadRef:        "feature",
		Draft:          0,
	}); err != nil {
		t.Fatalf("UpsertPullRequest #%d: %v", number, err)
	}
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

func TestRun_TwoPRs_CursorAtMaxUpdated(t *testing.T) {
	t.Parallel()
	q := newQueries(t)
	tn := seedTenant(t, q)
	tok := seedToken(t, q, tn)
	conn := seedConnection(t, q, tn, tok, "karnstack", strPtr("multi"),
		mustParse(t, "2026-01-01T00:00:00Z"))
	repo := seedRepo(t, q, tn, conn.ID, 6000002, "karnstack", "multi")
	older := mustParse(t, "2026-04-12T10:30:00Z")
	newer := mustParse(t, "2026-04-13T09:30:00Z")
	seedPR(t, q, repo.ID, 101, 7100101, older)
	seedPR(t, q, repo.ID, 102, 7100102, newer)

	gh := newReplayClient(t, "testdata/two_prs.json")
	r := prconvo.New(q, zaptest.NewLogger(t))

	out, err := r.Run(context.Background(), conn, gh)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// PR #101: 1 review + 1 review comment + 1 issue comment = 3.
	// PR #102: 1 review + 0 review comments + 1 issue comment = 2.
	if out.Items != 5 {
		t.Errorf("Items = %d, want 5", out.Items)
	}

	// Both PRs got their reviews.
	for _, n := range []int64{101, 102} {
		reviews, err := q.ListReviewsByPullRequest(context.Background(), sqlitedb.ListReviewsByPullRequestParams{
			PrRepoID: repo.ID, PrNumber: n,
		})
		if err != nil {
			t.Fatalf("ListReviewsByPullRequest(#%d): %v", n, err)
		}
		if len(reviews) != 1 {
			t.Errorf("reviews on #%d = %d, want 1", n, len(reviews))
		}
	}

	// Cursor pinned to the NEWER PR's UpdatedAt (max across both PRs).
	cur, err := q.GetSyncCursor(context.Background(), sqlitedb.GetSyncCursorParams{
		ConnectionID: conn.ID,
		Resource:     "prconvo:karnstack/multi",
	})
	if err != nil {
		t.Fatalf("GetSyncCursor: %v", err)
	}
	if cur.Cursor != newer.UTC().Format(time.RFC3339Nano) {
		t.Errorf("cursor = %q, want %q (max of two PRs' updated_at)",
			cur.Cursor, newer.UTC().Format(time.RFC3339Nano))
	}
}

func TestRun_HappyPath_SinglePR(t *testing.T) {
	t.Parallel()
	q := newQueries(t)
	tn := seedTenant(t, q)
	tok := seedToken(t, q, tn)
	conn := seedConnection(t, q, tn, tok, "karnstack", strPtr("tempo"),
		mustParse(t, "2026-01-01T00:00:00Z"))
	repo := seedRepo(t, q, tn, conn.ID, 6000001, "karnstack", "tempo")
	prUpdated := mustParse(t, "2026-04-12T12:00:00Z")
	seedPR(t, q, repo.ID, 101, 7000101, prUpdated)

	gh := newReplayClient(t, "testdata/happy_path_single_pr.json")
	r := prconvo.New(q, zaptest.NewLogger(t))

	out, err := r.Run(context.Background(), conn, gh)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// 2 reviews (1 user + 1 ghost) + 2 review comments (1 user + 1 ghost) + 1 issue comment = 5.
	if out.Items != 5 {
		t.Errorf("Items = %d, want 5", out.Items)
	}

	// pr_reviews: 2 rows. The ghost-dismissed review must have reviewer=0.
	reviews, err := q.ListReviewsByPullRequest(context.Background(), sqlitedb.ListReviewsByPullRequestParams{
		PrRepoID: repo.ID, PrNumber: 101,
	})
	if err != nil {
		t.Fatalf("ListReviewsByPullRequest: %v", err)
	}
	if len(reviews) != 2 {
		t.Fatalf("len(reviews) = %d, want 2", len(reviews))
	}
	var sawAlice, sawGhost bool
	for _, rv := range reviews {
		switch rv.GhID {
		case 3000001:
			sawAlice = true
			if rv.State != "APPROVED" || rv.ReviewerGhUserID == 0 {
				t.Errorf("alice review: state=%q reviewer=%d", rv.State, rv.ReviewerGhUserID)
			}
		case 3000002:
			sawGhost = true
			if rv.State != "DISMISSED" || rv.ReviewerGhUserID != 0 {
				t.Errorf("ghost review: state=%q reviewer=%d, want DISMISSED/0", rv.State, rv.ReviewerGhUserID)
			}
		}
	}
	if !sawAlice || !sawGhost {
		t.Errorf("missing review rows: sawAlice=%v sawGhost=%v", sawAlice, sawGhost)
	}

	// pr_review_comments: 2 rows. Ghost comment has author=0.
	rcs, err := q.ListReviewCommentsByPullRequest(context.Background(), sqlitedb.ListReviewCommentsByPullRequestParams{
		PrRepoID: repo.ID, PrNumber: 101,
	})
	if err != nil {
		t.Fatalf("ListReviewCommentsByPullRequest: %v", err)
	}
	if len(rcs) != 2 {
		t.Fatalf("len(review comments) = %d, want 2", len(rcs))
	}
	for _, c := range rcs {
		if c.GhID == 4000002 && c.AuthorGhUserID != 0 {
			t.Errorf("ghost review comment: author=%d, want 0", c.AuthorGhUserID)
		}
	}

	// pr_issue_comments: 1 row.
	ics, err := q.ListIssueCommentsByPullRequest(context.Background(), sqlitedb.ListIssueCommentsByPullRequestParams{
		PrRepoID: repo.ID, PrNumber: 101,
	})
	if err != nil {
		t.Fatalf("ListIssueCommentsByPullRequest: %v", err)
	}
	if len(ics) != 1 {
		t.Fatalf("len(issue comments) = %d, want 1", len(ics))
	}
	if ics[0].GhID != 5000001 || ics[0].AuthorGhUserID == 0 {
		t.Errorf("issue comment: gh_id=%d author=%d, want 5000001 / non-zero", ics[0].GhID, ics[0].AuthorGhUserID)
	}

	// gh_users: alice (review + comment author) + ci-bot (issue comment author). Ghost is skipped.
	users, err := q.ListGhUsersByTenant(context.Background(), tn)
	if err != nil {
		t.Fatalf("ListGhUsersByTenant: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("len(gh_users) = %d, want 2 (alice + ci-bot, ghost skipped)", len(users))
	}
	wantLogins := map[string]bool{"alice": false, "ci-bot": false}
	for _, u := range users {
		if _, ok := wantLogins[u.Login]; !ok {
			t.Errorf("unexpected gh_user %q", u.Login)
			continue
		}
		wantLogins[u.Login] = true
		if u.LastSeenAt == nil || !u.LastSeenAt.Equal(prUpdated) {
			t.Errorf("gh_user %q last_seen_at = %v, want %v", u.Login, u.LastSeenAt, prUpdated)
		}
	}
	for login, seen := range wantLogins {
		if !seen {
			t.Errorf("missing gh_user %q", login)
		}
	}

	// Cursor pinned to the PR's UpdatedAt.
	cur, err := q.GetSyncCursor(context.Background(), sqlitedb.GetSyncCursorParams{
		ConnectionID: conn.ID,
		Resource:     "prconvo:karnstack/tempo",
	})
	if err != nil {
		t.Fatalf("GetSyncCursor: %v", err)
	}
	if cur.Cursor != prUpdated.UTC().Format(time.RFC3339Nano) {
		t.Errorf("cursor = %q, want %q", cur.Cursor, prUpdated.UTC().Format(time.RFC3339Nano))
	}
}
