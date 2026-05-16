package deployments_test

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
	"github.com/karnstack/tempo/internal/ingest/deployments"
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
	path := filepath.Join(t.TempDir(), "deployments_runner.db")
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
	r := deployments.New(q, zaptest.NewLogger(t))

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

	ds, err := q.ListDeploymentsByRepoBetween(context.Background(), sqlitedb.ListDeploymentsByRepoBetweenParams{
		RepoID: repo.ID,
		FromTs: mustParse(t, "2026-04-01T00:00:00Z"),
		ToTs:   mustParse(t, "2026-05-01T00:00:00Z"),
	})
	if err != nil {
		t.Fatalf("ListDeploymentsByRepoBetween: %v", err)
	}
	if len(ds) != 3 {
		t.Fatalf("len(deployments) = %d, want 3", len(ds))
	}

	for _, d := range ds {
		if d.Status != "" {
			t.Errorf("deployment %d Status = %q, want \"\" (list endpoint omits status)", d.GhID, d.Status)
		}
	}

	// Cursor: since == max(CreatedAt) == 2026-04-12T10:00:00Z; etag pinned
	// to page-1 server etag.
	cur, err := q.GetSyncCursor(context.Background(), sqlitedb.GetSyncCursorParams{
		ConnectionID: conn.ID,
		Resource:     "deployments:karnstack/tempo",
	})
	if err != nil {
		t.Fatalf("GetSyncCursor: %v", err)
	}
	const wantCursor = `2026-04-12T10:00:00Z|W/"dep-abc"`
	if cur.Cursor != wantCursor {
		t.Errorf("cursor = %q, want %q", cur.Cursor, wantCursor)
	}

	// Sanity: cursor is parseable back via the split-on-`|` convention.
	sinceStr, etag, _ := strings.Cut(cur.Cursor, "|")
	if sinceStr != "2026-04-12T10:00:00Z" || etag != `W/"dep-abc"` {
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

	// Pre-seed cursor matching what happy_path would have written.
	const seededCursor = `2026-04-12T10:00:00Z|W/"dep-abc"`
	if err := q.UpsertSyncCursor(context.Background(), sqlitedb.UpsertSyncCursorParams{
		ConnectionID: conn.ID,
		Resource:     "deployments:karnstack/tempo",
		Cursor:       seededCursor,
		UpdatedAt:    mustParse(t, "2026-04-12T10:00:00Z"),
	}); err != nil {
		t.Fatalf("UpsertSyncCursor: %v", err)
	}

	gh := newReplayClient(t, "testdata/not_modified.json")
	r := deployments.New(q, zaptest.NewLogger(t))

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
		Resource:     "deployments:karnstack/tempo",
	})
	if err != nil {
		t.Fatalf("GetSyncCursor: %v", err)
	}
	if cur.Cursor != seededCursor {
		t.Errorf("cursor = %q, want unchanged %q", cur.Cursor, seededCursor)
	}

	// No deployment rows written.
	ds, err := q.ListDeploymentsByRepoBetween(context.Background(), sqlitedb.ListDeploymentsByRepoBetweenParams{
		RepoID: 1,
		FromTs: mustParse(t, "2000-01-01T00:00:00Z"),
		ToTs:   mustParse(t, "2099-01-01T00:00:00Z"),
	})
	if err != nil {
		t.Fatalf("ListDeploymentsByRepoBetween: %v", err)
	}
	if len(ds) != 0 {
		t.Errorf("len(deployments) = %d, want 0", len(ds))
	}
}

func TestRun_MultiPage_CursorAtMaxCreated(t *testing.T) {
	t.Parallel()
	q := newQueries(t)
	tn := seedTenant(t, q)
	tok := seedToken(t, q, tn)
	conn := seedConnection(t, q, tn, tok, "karnstack", strPtr("multi"),
		mustParse(t, "2026-04-01T00:00:00Z"))
	repo := seedRepo(t, q, tn, conn.ID, 6000003, "karnstack", "multi")

	gh := newReplayClient(t, "testdata/multi_page.json")
	r := deployments.New(q, zaptest.NewLogger(t))

	out, err := r.Run(context.Background(), conn, gh)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Items != 4 {
		t.Errorf("Items = %d, want 4 (2 + 2 across two pages)", out.Items)
	}

	ds, err := q.ListDeploymentsByRepoBetween(context.Background(), sqlitedb.ListDeploymentsByRepoBetweenParams{
		RepoID: repo.ID,
		FromTs: mustParse(t, "2026-04-01T00:00:00Z"),
		ToTs:   mustParse(t, "2026-05-01T00:00:00Z"),
	})
	if err != nil {
		t.Fatalf("ListDeploymentsByRepoBetween: %v", err)
	}
	if len(ds) != 4 {
		t.Errorf("len(deployments) = %d, want 4", len(ds))
	}

	// Cursor pinned to max(CreatedAt) across BOTH pages = 2026-04-14T09:30:00Z.
	// Etag = page1.ETag.
	cur, err := q.GetSyncCursor(context.Background(), sqlitedb.GetSyncCursorParams{
		ConnectionID: conn.ID,
		Resource:     "deployments:karnstack/multi",
	})
	if err != nil {
		t.Fatalf("GetSyncCursor: %v", err)
	}
	const wantCursor = `2026-04-14T09:30:00Z|W/"page1"`
	if cur.Cursor != wantCursor {
		t.Errorf("cursor = %q, want %q (max across pages, page1 etag)",
			cur.Cursor, wantCursor)
	}
}

func TestRun_EarlyStop_StopsBeforePage2(t *testing.T) {
	t.Parallel()
	q := newQueries(t)
	tn := seedTenant(t, q)
	tok := seedToken(t, q, tn)
	conn := seedConnection(t, q, tn, tok, "karnstack", strPtr("tempo"),
		mustParse(t, "2026-01-01T00:00:00Z"))
	repo := seedRepo(t, q, tn, conn.ID, 6000004, "karnstack", "tempo")

	// Pre-seed cursor: since = 2026-04-15 → deploys on 4-20 + 4-18 are
	// new, deploy on 4-10 is old → sawOld → break before page 2.
	const seededCursor = `2026-04-15T00:00:00Z|W/"dep-prior"`
	if err := q.UpsertSyncCursor(context.Background(), sqlitedb.UpsertSyncCursorParams{
		ConnectionID: conn.ID,
		Resource:     "deployments:karnstack/tempo",
		Cursor:       seededCursor,
		UpdatedAt:    mustParse(t, "2026-04-15T00:00:00Z"),
	}); err != nil {
		t.Fatalf("UpsertSyncCursor: %v", err)
	}

	// Cassette has ONLY a page-1 interaction even though Link says
	// rel="next" exists. If the runner fetches page 2, vcr.Done() will
	// return an "unmatched request" error in t.Cleanup.
	gh := newReplayClient(t, "testdata/early_stop.json")
	r := deployments.New(q, zaptest.NewLogger(t))

	out, err := r.Run(context.Background(), conn, gh)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Items != 2 {
		t.Errorf("Items = %d, want 2 (two new deploys; the 4-10 one is older than cursor)", out.Items)
	}

	ds, err := q.ListDeploymentsByRepoBetween(context.Background(), sqlitedb.ListDeploymentsByRepoBetweenParams{
		RepoID: repo.ID,
		FromTs: mustParse(t, "2026-01-01T00:00:00Z"),
		ToTs:   mustParse(t, "2026-05-01T00:00:00Z"),
	})
	if err != nil {
		t.Fatalf("ListDeploymentsByRepoBetween: %v", err)
	}
	if len(ds) != 2 {
		t.Errorf("len(deployments) = %d, want 2 (4-10 deploy must be filtered out)", len(ds))
	}
	for _, d := range ds {
		if d.GhID == 7003 {
			t.Errorf("deployment 7003 (created_at = 4-10) was upserted but should have been filtered as old")
		}
	}

	cur, err := q.GetSyncCursor(context.Background(), sqlitedb.GetSyncCursorParams{
		ConnectionID: conn.ID,
		Resource:     "deployments:karnstack/tempo",
	})
	if err != nil {
		t.Fatalf("GetSyncCursor: %v", err)
	}
	const wantCursor = `2026-04-20T10:00:00Z|W/"dep-stop"`
	if cur.Cursor != wantCursor {
		t.Errorf("cursor = %q, want %q (max of new deploys, page1 etag)",
			cur.Cursor, wantCursor)
	}
}

func TestRun_NoRepos_Noop(t *testing.T) {
	t.Parallel()
	q := newQueries(t)
	tn := seedTenant(t, q)
	tok := seedToken(t, q, tn)
	conn := seedConnection(t, q, tn, tok, "karnstack", strPtr("nothing"),
		mustParse(t, "2026-04-01T00:00:00Z"))
	// Note: no repos seeded.

	// Bare client with no transport — Runner must not make any HTTP calls.
	gh := github.New("test-token", github.WithBackoff(func(int) time.Duration { return 0 }))
	r := deployments.New(q, zaptest.NewLogger(t))

	out, err := r.Run(context.Background(), conn, gh)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Items != 0 {
		t.Errorf("Items = %d, want 0", out.Items)
	}
	if out.RateLimitRemaining != nil {
		t.Errorf("RateLimitRemaining = %v, want nil (no REST call made)", *out.RateLimitRemaining)
	}

	cursors, err := q.ListSyncCursorsByConnection(context.Background(), conn.ID)
	if err != nil {
		t.Fatalf("ListSyncCursorsByConnection: %v", err)
	}
	if len(cursors) != 0 {
		t.Errorf("len(cursors) = %d, want 0", len(cursors))
	}
}

func TestRun_EmptyResponse_RefreshesEtag(t *testing.T) {
	t.Parallel()
	q := newQueries(t)
	tn := seedTenant(t, q)
	tok := seedToken(t, q, tn)
	conn := seedConnection(t, q, tn, tok, "karnstack", strPtr("calm"),
		mustParse(t, "2026-01-01T00:00:00Z"))
	seedRepo(t, q, tn, conn.ID, 6000005, "karnstack", "calm")

	// Pre-seed cursor with empty etag — server returns 200 + new etag + 0 deploys.
	const seededCursor = "2026-04-15T00:00:00Z|"
	if err := q.UpsertSyncCursor(context.Background(), sqlitedb.UpsertSyncCursorParams{
		ConnectionID: conn.ID,
		Resource:     "deployments:karnstack/calm",
		Cursor:       seededCursor,
		UpdatedAt:    mustParse(t, "2026-04-15T00:00:00Z"),
	}); err != nil {
		t.Fatalf("UpsertSyncCursor: %v", err)
	}

	gh := newReplayClient(t, "testdata/empty_response.json")
	r := deployments.New(q, zaptest.NewLogger(t))

	out, err := r.Run(context.Background(), conn, gh)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Items != 0 {
		t.Errorf("Items = %d, want 0 (empty body)", out.Items)
	}

	cur, err := q.GetSyncCursor(context.Background(), sqlitedb.GetSyncCursorParams{
		ConnectionID: conn.ID,
		Resource:     "deployments:karnstack/calm",
	})
	if err != nil {
		t.Fatalf("GetSyncCursor: %v", err)
	}
	const wantCursor = `2026-04-15T00:00:00Z|W/"dep-new"`
	if cur.Cursor != wantCursor {
		t.Errorf("cursor = %q, want %q (since unchanged, etag refreshed)",
			cur.Cursor, wantCursor)
	}
}

func TestRun_LegacyCursor_ParsedAsBareSince(t *testing.T) {
	t.Parallel()
	q := newQueries(t)
	tn := seedTenant(t, q)
	tok := seedToken(t, q, tn)
	conn := seedConnection(t, q, tn, tok, "karnstack", strPtr("calm"),
		mustParse(t, "2026-01-01T00:00:00Z"))
	seedRepo(t, q, tn, conn.ID, 6000006, "karnstack", "calm")

	// Legacy hand-seeded cursor with no `|` separator. Must parse as
	// (since=that, etag=""). Cassette URL has no since= param either way,
	// so the runner uses the parsed since to filter client-side; the
	// cassette returns 0 deploys + fresh etag.
	const seededCursor = "2026-04-15T00:00:00Z"
	if err := q.UpsertSyncCursor(context.Background(), sqlitedb.UpsertSyncCursorParams{
		ConnectionID: conn.ID,
		Resource:     "deployments:karnstack/calm",
		Cursor:       seededCursor,
		UpdatedAt:    mustParse(t, "2026-04-15T00:00:00Z"),
	}); err != nil {
		t.Fatalf("UpsertSyncCursor: %v", err)
	}

	gh := newReplayClient(t, "testdata/empty_response.json")
	r := deployments.New(q, zaptest.NewLogger(t))

	out, err := r.Run(context.Background(), conn, gh)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Items != 0 {
		t.Errorf("Items = %d, want 0", out.Items)
	}

	cur, err := q.GetSyncCursor(context.Background(), sqlitedb.GetSyncCursorParams{
		ConnectionID: conn.ID,
		Resource:     "deployments:karnstack/calm",
	})
	if err != nil {
		t.Fatalf("GetSyncCursor: %v", err)
	}
	const wantCursor = `2026-04-15T00:00:00Z|W/"dep-new"`
	if cur.Cursor != wantCursor {
		t.Errorf("cursor = %q, want %q (legacy parse + refresh)",
			cur.Cursor, wantCursor)
	}
}

func TestRun_PerRepoFailureIsolation(t *testing.T) {
	t.Parallel()
	q := newQueries(t)
	tn := seedTenant(t, q)
	tok := seedToken(t, q, tn)
	conn := seedConnection(t, q, tn, tok, "karnstack", strPtr("multi"),
		mustParse(t, "2026-04-01T00:00:00Z"))
	// Seed two repos. ListReposByConnection orders by (owner, name), so
	// "aok" runs before "zfail" — matches cassette order.
	aok := seedRepo(t, q, tn, conn.ID, 6000010, "karnstack", "aok")
	zfail := seedRepo(t, q, tn, conn.ID, 6000011, "karnstack", "zfail")

	gh := newReplayClient(t, "testdata/repo_failure.json")
	r := deployments.New(q, zaptest.NewLogger(t))

	out, err := r.Run(context.Background(), conn, gh)
	if err == nil {
		t.Fatal("Run err = nil, want non-nil (zfail returned 404)")
	}
	if got := err.Error(); !strings.Contains(got, "karnstack/zfail") {
		t.Errorf("err = %v, want contains 'karnstack/zfail'", err)
	}

	// aok succeeded with 1 deploy.
	if out.Items != 1 {
		t.Errorf("Items = %d, want 1 (only aok succeeded)", out.Items)
	}

	aokDeploys, err := q.ListDeploymentsByRepoBetween(context.Background(), sqlitedb.ListDeploymentsByRepoBetweenParams{
		RepoID: aok.ID,
		FromTs: mustParse(t, "2026-04-01T00:00:00Z"),
		ToTs:   mustParse(t, "2026-05-01T00:00:00Z"),
	})
	if err != nil {
		t.Fatalf("ListDeploymentsByRepoBetween aok: %v", err)
	}
	if len(aokDeploys) != 1 {
		t.Errorf("len(aok deployments) = %d, want 1", len(aokDeploys))
	}

	zfailDeploys, err := q.ListDeploymentsByRepoBetween(context.Background(), sqlitedb.ListDeploymentsByRepoBetweenParams{
		RepoID: zfail.ID,
		FromTs: mustParse(t, "2026-04-01T00:00:00Z"),
		ToTs:   mustParse(t, "2026-05-01T00:00:00Z"),
	})
	if err != nil {
		t.Fatalf("ListDeploymentsByRepoBetween zfail: %v", err)
	}
	if len(zfailDeploys) != 0 {
		t.Errorf("len(zfail deployments) = %d, want 0", len(zfailDeploys))
	}

	// aok's cursor advanced; zfail's must not exist.
	if _, err := q.GetSyncCursor(context.Background(), sqlitedb.GetSyncCursorParams{
		ConnectionID: conn.ID,
		Resource:     "deployments:karnstack/aok",
	}); err != nil {
		t.Errorf("GetSyncCursor aok: %v (should have advanced)", err)
	}

	cursors, err := q.ListSyncCursorsByConnection(context.Background(), conn.ID)
	if err != nil {
		t.Fatalf("ListSyncCursorsByConnection: %v", err)
	}
	for _, c := range cursors {
		if c.Resource == "deployments:karnstack/zfail" {
			t.Errorf("zfail cursor unexpectedly exists: %+v", c)
		}
	}
}
