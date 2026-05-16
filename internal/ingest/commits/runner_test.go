package commits_test

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/karnstack/tempo/internal/config"
	"github.com/karnstack/tempo/internal/github"
	"github.com/karnstack/tempo/internal/github/vcr"
	"github.com/karnstack/tempo/internal/ingest/commits"
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
	path := filepath.Join(t.TempDir(), "commits_runner.db")
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

func TestRun_HappyPath_SingleRepo(t *testing.T) {
	t.Parallel()
	q := newQueries(t)
	tn := seedTenant(t, q)
	tok := seedToken(t, q, tn)
	conn := seedConnection(t, q, tn, tok, "karnstack", strPtr("tempo"),
		mustParse(t, "2026-04-01T00:00:00Z"))
	repo := seedRepo(t, q, tn, conn.ID, 6000001, "karnstack", "tempo")

	gh := newReplayClient(t, "testdata/happy_path.json")
	r := commits.New(q, zaptest.NewLogger(t))

	out, err := r.Run(context.Background(), conn, gh)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Items != 3 {
		t.Errorf("Items = %d, want 3", out.Items)
	}
	if out.RateLimitRemaining == nil {
		t.Fatalf("RateLimitRemaining = nil, want non-nil")
	}
	if got := *out.RateLimitRemaining; got != 4999 {
		t.Errorf("RateLimitRemaining = %d, want 4999", got)
	}

	cs, err := q.ListCommitsByRepoBetween(context.Background(), sqlitedb.ListCommitsByRepoBetweenParams{
		RepoID: repo.ID,
		FromTs: mustParse(t, "2026-04-01T00:00:00Z"),
		ToTs:   mustParse(t, "2026-05-01T00:00:00Z"),
	})
	if err != nil {
		t.Fatalf("ListCommitsByRepoBetween: %v", err)
	}
	if len(cs) != 3 {
		t.Fatalf("len(commits) = %d, want 3", len(cs))
	}

	// Ghost commit's author and committer must both be 0.
	var foundGhost bool
	for _, c := range cs {
		if c.Sha == "c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3" {
			foundGhost = true
			if c.AuthorGhUserID != 0 || c.CommitterGhUserID != 0 {
				t.Errorf("ghost commit: author=%d committer=%d, want 0/0",
					c.AuthorGhUserID, c.CommitterGhUserID)
			}
		}
	}
	if !foundGhost {
		t.Error("ghost commit row not found")
	}

	// gh_users: alice + renovate[bot]. Ghost is skipped.
	users, err := q.ListGhUsersByTenant(context.Background(), tn)
	if err != nil {
		t.Fatalf("ListGhUsersByTenant: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("len(gh_users) = %d, want 2 (alice + renovate[bot])", len(users))
	}
	wantLogins := map[string]bool{"alice": false, "renovate[bot]": false}
	for _, u := range users {
		if _, ok := wantLogins[u.Login]; !ok {
			t.Errorf("unexpected gh_user %q", u.Login)
			continue
		}
		wantLogins[u.Login] = true
	}
	for login, seen := range wantLogins {
		if !seen {
			t.Errorf("missing gh_user %q", login)
		}
	}

	// Cursor: since == max(authoredAt) == 2026-04-12T10:00:00Z; etag cleared.
	cur, err := q.GetSyncCursor(context.Background(), sqlitedb.GetSyncCursorParams{
		ConnectionID: conn.ID,
		Resource:     "commits:karnstack/tempo",
	})
	if err != nil {
		t.Fatalf("GetSyncCursor: %v", err)
	}
	wantCursor := "2026-04-12T10:00:00Z|"
	if cur.Cursor != wantCursor {
		t.Errorf("cursor = %q, want %q", cur.Cursor, wantCursor)
	}

	// Sanity: cursor is parseable back via the split-on-`|` convention.
	sinceStr, etag, _ := strings.Cut(cur.Cursor, "|")
	if sinceStr != "2026-04-12T10:00:00Z" || etag != "" {
		t.Errorf("cursor split: since=%q etag=%q", sinceStr, etag)
	}
}

func TestRun_NotModified_LeavesCursorUntouched(t *testing.T) {
	t.Parallel()
	q := newQueries(t)
	tn := seedTenant(t, q)
	tok := seedToken(t, q, tn)
	conn := seedConnection(t, q, tn, tok, "karnstack", strPtr("tempo"),
		mustParse(t, "2026-01-01T00:00:00Z"))
	seedRepo(t, q, tn, conn.ID, 6000002, "karnstack", "tempo")

	// Pre-seed cursor matching what the happy_path test would have written.
	const seededCursor = `2026-04-12T10:00:00Z|W/"abc123"`
	if err := q.UpsertSyncCursor(context.Background(), sqlitedb.UpsertSyncCursorParams{
		ConnectionID: conn.ID,
		Resource:     "commits:karnstack/tempo",
		Cursor:       seededCursor,
		UpdatedAt:    mustParse(t, "2026-04-12T10:00:00Z"),
	}); err != nil {
		t.Fatalf("UpsertSyncCursor: %v", err)
	}

	gh := newReplayClient(t, "testdata/not_modified.json")
	r := commits.New(q, zaptest.NewLogger(t))

	out, err := r.Run(context.Background(), conn, gh)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Items != 0 {
		t.Errorf("Items = %d, want 0 (304 NotModified)", out.Items)
	}

	// Cursor unchanged.
	cur, err := q.GetSyncCursor(context.Background(), sqlitedb.GetSyncCursorParams{
		ConnectionID: conn.ID,
		Resource:     "commits:karnstack/tempo",
	})
	if err != nil {
		t.Fatalf("GetSyncCursor: %v", err)
	}
	if cur.Cursor != seededCursor {
		t.Errorf("cursor = %q, want unchanged %q", cur.Cursor, seededCursor)
	}

	// No commits written.
	cs, err := q.ListCommitsByRepoBetween(context.Background(), sqlitedb.ListCommitsByRepoBetweenParams{
		RepoID: 1,
		FromTs: mustParse(t, "2000-01-01T00:00:00Z"),
		ToTs:   mustParse(t, "2099-01-01T00:00:00Z"),
	})
	if err != nil {
		t.Fatalf("ListCommitsByRepoBetween: %v", err)
	}
	if len(cs) != 0 {
		t.Errorf("len(commits) = %d, want 0", len(cs))
	}
}
