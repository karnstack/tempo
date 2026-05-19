package orgs_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	apiauth "github.com/karnstack/tempo/internal/api/auth"
	"github.com/karnstack/tempo/internal/api/orgs"
	intauth "github.com/karnstack/tempo/internal/auth"
	"github.com/karnstack/tempo/internal/config"
	"github.com/karnstack/tempo/internal/storage/sqlite"
	"github.com/karnstack/tempo/internal/storage/sqlite/sqlitedb"
	"github.com/karnstack/tempo/migrations"
	"github.com/labstack/echo/v4"
	"go.uber.org/fx/fxtest"
	"go.uber.org/zap/zaptest"
)

const (
	seedEmail    = "admin@acme.test"
	seedPassword = "hunter22hunter22"
)

func newIntegrationStore(t *testing.T) *sqlitedb.Queries {
	t.Helper()
	lc := fxtest.NewLifecycle(t)
	l := zaptest.NewLogger(t)
	path := filepath.Join(t.TempDir(), "orgs_integration.db")
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

func newOrgsEcho(t *testing.T, q *sqlitedb.Queries, cfg *config.Config) *echo.Echo {
	t.Helper()
	m := intauth.NewManager(q, time.Hour, false)
	r := intauth.NewRegistrar(q)
	a := intauth.NewAuthenticator(q)
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	l := zaptest.NewLogger(t)
	apiauth.Configure(e, l, m, r, a)
	orgs.Configure(e, l, m, q, cfg)
	return e
}

func defaultCfg() *config.Config {
	return &config.Config{Rollup: config.Rollup{Timezone: time.UTC, Hour: 2}}
}

func seedAndLogin(t *testing.T, e http.Handler, q *sqlitedb.Queries) *http.Cookie {
	t.Helper()
	if _, err := intauth.NewRegistrar(q).Register(context.Background(), seedEmail, seedPassword); err != nil {
		t.Fatalf("seed Register: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
		strings.NewReader(`{"email":"`+seedEmail+`","password":"`+seedPassword+`"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login: %d body=%s", rec.Code, rec.Body.String())
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == intauth.CookieName {
			return c
		}
	}
	t.Fatal("no session cookie")
	return nil
}

func doGET(t *testing.T, e http.Handler, path string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func tenantIDForLogin(t *testing.T, q *sqlitedb.Queries) int64 {
	t.Helper()
	user, err := q.GetUserByEmail(context.Background(), sqlitedb.GetUserByEmailParams{
		TenantID: 1, Email: seedEmail,
	})
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	return user.TenantID
}

func seedRepo(t *testing.T, q *sqlitedb.Queries, tenantID int64, owner, name string, archived bool) sqlitedb.Repo {
	t.Helper()
	a := int64(0)
	if archived {
		a = 1
	}
	repo, err := q.CreateRepo(context.Background(), sqlitedb.CreateRepoParams{
		TenantID:      tenantID,
		ConnectionID:  1,
		GhID:          int64(time.Now().UnixNano()) + int64(len(owner)+len(name)),
		Owner:         owner,
		Name:          name,
		DefaultBranch: "main",
		Archived:      a,
	})
	if err != nil {
		t.Fatalf("CreateRepo(%s/%s): %v", owner, name, err)
	}
	return repo
}

func seedDailyRepoStats(t *testing.T, q *sqlitedb.Queries, repoID int64, date string, opened, merged, closed, deploys int64) {
	t.Helper()
	if err := q.UpsertDailyRepoStats(context.Background(), sqlitedb.UpsertDailyRepoStatsParams{
		Date:      date,
		RepoID:    repoID,
		PrsOpened: opened, PrsMerged: merged, PrsClosed: closed, Deploys: deploys,
	}); err != nil {
		t.Fatalf("UpsertDailyRepoStats: %v", err)
	}
}

func seedDailyReviewLatency(t *testing.T, q *sqlitedb.Queries, repoID int64, date string, count int64) {
	t.Helper()
	if err := q.UpsertDailyReviewLatency(context.Background(), sqlitedb.UpsertDailyReviewLatencyParams{
		Date: date, RepoID: repoID, Count: count,
	}); err != nil {
		t.Fatalf("UpsertDailyReviewLatency: %v", err)
	}
}

func seedDailyReviewLoad(t *testing.T, q *sqlitedb.Queries, repoID int64, date string, reviewerUID, reviews int64) {
	t.Helper()
	if err := q.UpsertDailyReviewLoad(context.Background(), sqlitedb.UpsertDailyReviewLoadParams{
		Date: date, RepoID: repoID, ReviewerGhUserID: reviewerUID, Reviews: reviews,
	}); err != nil {
		t.Fatalf("UpsertDailyReviewLoad: %v", err)
	}
}

func TestMetrics_HappyPath(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e := newOrgsEcho(t, q, defaultCfg())
	cookie := seedAndLogin(t, e, q)
	tenantID := tenantIDForLogin(t, q)

	r1 := seedRepo(t, q, tenantID, "acme", "alpha", false)
	r2 := seedRepo(t, q, tenantID, "acme", "beta", false)
	// Another tenant w/ same owner — must not bleed.
	other, _ := q.CreateTenant(context.Background(), "other")
	otherRepo := seedRepo(t, q, other.ID, "acme", "secret", false)

	// On 2026-05-17: r1=(2,1,0,1) + r2=(3,2,1,4) → sum=(5,3,1,5).
	// Other tenant: (99,99,99,99) — must not show up.
	seedDailyRepoStats(t, q, r1.ID, "2026-05-17", 2, 1, 0, 1)
	seedDailyRepoStats(t, q, r2.ID, "2026-05-17", 3, 2, 1, 4)
	seedDailyRepoStats(t, q, otherRepo.ID, "2026-05-17", 99, 99, 99, 99)

	// Latency: r1 count=2, r2 count=3 → 5.
	seedDailyReviewLatency(t, q, r1.ID, "2026-05-17", 2)
	seedDailyReviewLatency(t, q, r2.ID, "2026-05-17", 3)

	// Load: reviewer 12 has r1=4, r2=2 → 6 total.
	seedDailyReviewLoad(t, q, r1.ID, "2026-05-17", 12, 4)
	seedDailyReviewLoad(t, q, r2.ID, "2026-05-17", 12, 2)

	rec := doGET(t, e, "/api/v1/orgs/acme/metrics?from=2026-05-17&to=2026-05-17", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp orgs.MetricsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Org != "acme" {
		t.Errorf("org = %q, want acme", resp.Org)
	}
	if len(resp.Repos) != 2 {
		t.Errorf("repos = %d, want 2 (acme's two; other tenant excluded)", len(resp.Repos))
	}
	if len(resp.DailyStats) != 1 {
		t.Fatalf("daily_stats = %d, want 1", len(resp.DailyStats))
	}
	got := resp.DailyStats[0]
	if got.PrsOpened != 5 || got.PrsMerged != 3 || got.PrsClosed != 1 || got.Deploys != 5 {
		t.Errorf("stats = %+v, want opened=5 merged=3 closed=1 deploys=5", got)
	}
	if len(resp.DailyReviewLatency) != 1 || resp.DailyReviewLatency[0].Count != 5 {
		t.Errorf("review_latency = %+v, want count=5", resp.DailyReviewLatency)
	}
	if len(resp.DailyReviewLoad) != 1 || resp.DailyReviewLoad[0].Reviews != 6 {
		t.Errorf("review_load = %+v, want reviews=6 for uid=12", resp.DailyReviewLoad)
	}
}

func TestMetrics_NoSuchOrg_404(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e := newOrgsEcho(t, q, defaultCfg())
	cookie := seedAndLogin(t, e, q)

	rec := doGET(t, e, "/api/v1/orgs/ghost/metrics", cookie)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestMetrics_DefaultRange30Days(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e := newOrgsEcho(t, q, defaultCfg())
	cookie := seedAndLogin(t, e, q)
	tenantID := tenantIDForLogin(t, q)
	seedRepo(t, q, tenantID, "acme", "r", false)

	rec := doGET(t, e, "/api/v1/orgs/acme/metrics", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp orgs.MetricsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	today := time.Now().UTC().Format("2006-01-02")
	wantFrom := time.Now().UTC().AddDate(0, 0, -29).Format("2006-01-02")
	if resp.To != today || resp.From != wantFrom {
		t.Errorf("range = %s..%s, want %s..%s", resp.From, resp.To, wantFrom, today)
	}
}

func TestMetrics_InvalidFrom_400(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e := newOrgsEcho(t, q, defaultCfg())
	cookie := seedAndLogin(t, e, q)
	tenantID := tenantIDForLogin(t, q)
	seedRepo(t, q, tenantID, "acme", "r", false)

	rec := doGET(t, e, "/api/v1/orgs/acme/metrics?from=zzz", cookie)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestMetrics_FromAfterTo_400(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e := newOrgsEcho(t, q, defaultCfg())
	cookie := seedAndLogin(t, e, q)
	tenantID := tenantIDForLogin(t, q)
	seedRepo(t, q, tenantID, "acme", "r", false)

	rec := doGET(t, e, "/api/v1/orgs/acme/metrics?from=2026-05-20&to=2026-05-10", cookie)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestMetrics_RangeTooLong_400(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e := newOrgsEcho(t, q, defaultCfg())
	cookie := seedAndLogin(t, e, q)
	tenantID := tenantIDForLogin(t, q)
	seedRepo(t, q, tenantID, "acme", "r", false)

	rec := doGET(t, e, "/api/v1/orgs/acme/metrics?from=2024-01-01&to=2026-05-20", cookie)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestMetrics_RespectsRange(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e := newOrgsEcho(t, q, defaultCfg())
	cookie := seedAndLogin(t, e, q)
	tenantID := tenantIDForLogin(t, q)
	repo := seedRepo(t, q, tenantID, "acme", "r", false)

	seedDailyRepoStats(t, q, repo.ID, "2026-05-10", 1, 1, 1, 1)
	seedDailyRepoStats(t, q, repo.ID, "2026-05-15", 2, 2, 2, 2)
	seedDailyRepoStats(t, q, repo.ID, "2026-05-20", 3, 3, 3, 3)

	rec := doGET(t, e, "/api/v1/orgs/acme/metrics?from=2026-05-15&to=2026-05-15", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp orgs.MetricsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.DailyStats) != 1 || resp.DailyStats[0].PrsOpened != 2 {
		t.Errorf("expected only May 15 sum=2: %+v", resp.DailyStats)
	}
}

func TestMetrics_CrossTenantOrgIsolated(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e := newOrgsEcho(t, q, defaultCfg())
	cookie := seedAndLogin(t, e, q)

	// Caller has no repos. Another tenant has acme repos.
	other, _ := q.CreateTenant(context.Background(), "other")
	seedRepo(t, q, other.ID, "acme", "r1", false)

	rec := doGET(t, e, "/api/v1/orgs/acme/metrics", cookie)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (cross-tenant isolation)", rec.Code)
	}
}

func TestMetrics_ReposListIncludesArchived(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e := newOrgsEcho(t, q, defaultCfg())
	cookie := seedAndLogin(t, e, q)
	tenantID := tenantIDForLogin(t, q)
	seedRepo(t, q, tenantID, "acme", "live", false)
	seedRepo(t, q, tenantID, "acme", "old", true)

	rec := doGET(t, e, "/api/v1/orgs/acme/metrics", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var resp orgs.MetricsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Repos) != 2 {
		t.Errorf("repos = %d, want 2 (archived included)", len(resp.Repos))
	}
	archivedCount := 0
	for _, r := range resp.Repos {
		if r.Archived {
			archivedCount++
		}
	}
	if archivedCount != 1 {
		t.Errorf("archived count = %d, want 1", archivedCount)
	}
}

func TestMetrics_NoCookie_401(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e := newOrgsEcho(t, q, defaultCfg())

	rec := doGET(t, e, "/api/v1/orgs/acme/metrics", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
