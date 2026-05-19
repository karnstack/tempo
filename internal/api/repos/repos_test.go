package repos_test

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
	"github.com/karnstack/tempo/internal/api/repos"
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
	path := filepath.Join(t.TempDir(), "repos_integration.db")
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

func newReposEcho(t *testing.T, q *sqlitedb.Queries, cfg *config.Config) *echo.Echo {
	t.Helper()
	m := intauth.NewManager(q, time.Hour, false)
	r := intauth.NewRegistrar(q)
	a := intauth.NewAuthenticator(q)
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	l := zaptest.NewLogger(t)
	apiauth.Configure(e, l, m, r, a)
	repos.Configure(e, l, m, q, cfg)
	return e
}

// defaultCfg pins tz to UTC so the date defaulting in tests is
// deterministic regardless of the host clock.
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
		t.Fatalf("login: status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == intauth.CookieName {
			return c
		}
	}
	t.Fatal("login did not set session cookie")
	return nil
}

func doJSON(t *testing.T, e http.Handler, method, path string, cookie *http.Cookie, body string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
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

func seedDailyRepoStats(t *testing.T, q *sqlitedb.Queries, repoID int64, date string, p50, p90 *int64) {
	t.Helper()
	if err := q.UpsertDailyRepoStats(context.Background(), sqlitedb.UpsertDailyRepoStatsParams{
		Date:               date,
		RepoID:             repoID,
		PrsOpened:          3,
		PrsMerged:          2,
		PrsClosed:          1,
		Deploys:            4,
		LeadTimeSecondsP50: p50,
		LeadTimeSecondsP90: p90,
	}); err != nil {
		t.Fatalf("UpsertDailyRepoStats %s: %v", date, err)
	}
}

func seedDailyReviewLatency(t *testing.T, q *sqlitedb.Queries, repoID int64, date string, p50, p90 *int64, count int64) {
	t.Helper()
	if err := q.UpsertDailyReviewLatency(context.Background(), sqlitedb.UpsertDailyReviewLatencyParams{
		Date:                        date,
		RepoID:                      repoID,
		TimeToFirstReviewSecondsP50: p50,
		TimeToFirstReviewSecondsP90: p90,
		Count:                       count,
	}); err != nil {
		t.Fatalf("UpsertDailyReviewLatency %s: %v", date, err)
	}
}

func seedDailyReviewLoad(t *testing.T, q *sqlitedb.Queries, repoID int64, date string, reviewerUID, reviews int64, p50 *int64) {
	t.Helper()
	if err := q.UpsertDailyReviewLoad(context.Background(), sqlitedb.UpsertDailyReviewLoadParams{
		Date:               date,
		RepoID:             repoID,
		ReviewerGhUserID:   reviewerUID,
		Reviews:            reviews,
		ResponseMinutesP50: p50,
	}); err != nil {
		t.Fatalf("UpsertDailyReviewLoad %s/%d: %v", date, reviewerUID, err)
	}
}

func i64(v int64) *int64 { return &v }

// --- LIST ---

func TestList_Empty(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e := newReposEcho(t, q, defaultCfg())
	cookie := seedAndLogin(t, e, q)

	rec := doJSON(t, e, http.MethodGet, "/api/v1/repos", cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp repos.ListReposResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Repos) != 0 {
		t.Errorf("repos = %d, want 0", len(resp.Repos))
	}
}

func TestList_HappyPath(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e := newReposEcho(t, q, defaultCfg())
	cookie := seedAndLogin(t, e, q)
	tenantID := tenantIDForLogin(t, q)

	// Seed two repos for caller's tenant + one for another tenant.
	seedRepo(t, q, tenantID, "octo", "hello", false)
	seedRepo(t, q, tenantID, "acme", "world", false)
	other, err := q.CreateTenant(context.Background(), "other")
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	seedRepo(t, q, other.ID, "ghost", "hidden", false)

	rec := doJSON(t, e, http.MethodGet, "/api/v1/repos", cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp repos.ListReposResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Repos) != 2 {
		t.Fatalf("repos = %d, want 2", len(resp.Repos))
	}
	// ListReposByTenant orders by (owner, name) → acme before octo.
	if resp.Repos[0].Owner != "acme" || resp.Repos[1].Owner != "octo" {
		t.Errorf("owners = %s/%s, want acme/octo",
			resp.Repos[0].Owner, resp.Repos[1].Owner)
	}
	// tenant_id must not leak.
	if strings.Contains(rec.Body.String(), "tenant_id") {
		t.Errorf("body leaks tenant_id: %s", rec.Body.String())
	}
}

func TestList_NoCookie_401(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e := newReposEcho(t, q, defaultCfg())

	rec := doJSON(t, e, http.MethodGet, "/api/v1/repos", nil, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

// --- METRICS ---

func TestMetrics_HappyPath(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e := newReposEcho(t, q, defaultCfg())
	cookie := seedAndLogin(t, e, q)
	tenantID := tenantIDForLogin(t, q)
	repo := seedRepo(t, q, tenantID, "octo", "hello", false)

	seedDailyRepoStats(t, q, repo.ID, "2026-05-16", i64(3600), i64(7200))
	seedDailyRepoStats(t, q, repo.ID, "2026-05-17", nil, nil)
	seedDailyReviewLatency(t, q, repo.ID, "2026-05-16", i64(1800), i64(3600), 4)
	seedDailyReviewLoad(t, q, repo.ID, "2026-05-16", 12, 5, i64(30))
	seedDailyReviewLoad(t, q, repo.ID, "2026-05-16", 13, 2, i64(45))

	rec := doJSON(t, e, http.MethodGet,
		"/api/v1/repos/octo/hello/metrics?from=2026-05-15&to=2026-05-18", cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp repos.MetricsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Repo.Owner != "octo" || resp.Repo.Name != "hello" {
		t.Errorf("repo wrong: %+v", resp.Repo)
	}
	if resp.From != "2026-05-15" || resp.To != "2026-05-18" {
		t.Errorf("range = %s..%s, want 2026-05-15..2026-05-18", resp.From, resp.To)
	}
	if len(resp.DailyStats) != 2 {
		t.Errorf("daily_stats = %d, want 2 rows", len(resp.DailyStats))
	}
	if len(resp.DailyReviewLatency) != 1 {
		t.Errorf("daily_review_latency = %d, want 1", len(resp.DailyReviewLatency))
	}
	if len(resp.DailyReviewLoad) != 2 {
		t.Errorf("daily_review_load = %d, want 2 (one per reviewer)", len(resp.DailyReviewLoad))
	}
}

func TestMetrics_DefaultsTo30Days(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e := newReposEcho(t, q, defaultCfg())
	cookie := seedAndLogin(t, e, q)
	tenantID := tenantIDForLogin(t, q)
	seedRepo(t, q, tenantID, "octo", "hello", false)

	rec := doJSON(t, e, http.MethodGet,
		"/api/v1/repos/octo/hello/metrics", cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp repos.MetricsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	today := time.Now().UTC().Format("2006-01-02")
	wantFrom := time.Now().UTC().AddDate(0, 0, -29).Format("2006-01-02")
	if resp.To != today {
		t.Errorf("to = %q, want today=%q", resp.To, today)
	}
	if resp.From != wantFrom {
		t.Errorf("from = %q, want today-29=%q", resp.From, wantFrom)
	}
}

func TestMetrics_RespectsRange(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e := newReposEcho(t, q, defaultCfg())
	cookie := seedAndLogin(t, e, q)
	tenantID := tenantIDForLogin(t, q)
	repo := seedRepo(t, q, tenantID, "octo", "hello", false)

	// Out of range (before) + in range + out of range (after).
	seedDailyRepoStats(t, q, repo.ID, "2026-05-10", i64(100), i64(200))
	seedDailyRepoStats(t, q, repo.ID, "2026-05-15", i64(300), i64(400))
	seedDailyRepoStats(t, q, repo.ID, "2026-05-20", i64(500), i64(600))

	rec := doJSON(t, e, http.MethodGet,
		"/api/v1/repos/octo/hello/metrics?from=2026-05-15&to=2026-05-15", cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp repos.MetricsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.DailyStats) != 1 {
		t.Fatalf("daily_stats = %d, want 1", len(resp.DailyStats))
	}
	if resp.DailyStats[0].Date != "2026-05-15" {
		t.Errorf("date = %s, want 2026-05-15", resp.DailyStats[0].Date)
	}
}

func TestMetrics_InvalidFrom_400(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e := newReposEcho(t, q, defaultCfg())
	cookie := seedAndLogin(t, e, q)
	tenantID := tenantIDForLogin(t, q)
	seedRepo(t, q, tenantID, "octo", "hello", false)

	rec := doJSON(t, e, http.MethodGet,
		"/api/v1/repos/octo/hello/metrics?from=not-a-date", cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestMetrics_InvalidTo_400(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e := newReposEcho(t, q, defaultCfg())
	cookie := seedAndLogin(t, e, q)
	tenantID := tenantIDForLogin(t, q)
	seedRepo(t, q, tenantID, "octo", "hello", false)

	rec := doJSON(t, e, http.MethodGet,
		"/api/v1/repos/octo/hello/metrics?to=2026/05/17", cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestMetrics_FromAfterTo_400(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e := newReposEcho(t, q, defaultCfg())
	cookie := seedAndLogin(t, e, q)
	tenantID := tenantIDForLogin(t, q)
	seedRepo(t, q, tenantID, "octo", "hello", false)

	rec := doJSON(t, e, http.MethodGet,
		"/api/v1/repos/octo/hello/metrics?from=2026-05-20&to=2026-05-15", cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestMetrics_RangeTooLong_400(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e := newReposEcho(t, q, defaultCfg())
	cookie := seedAndLogin(t, e, q)
	tenantID := tenantIDForLogin(t, q)
	seedRepo(t, q, tenantID, "octo", "hello", false)

	rec := doJSON(t, e, http.MethodGet,
		"/api/v1/repos/octo/hello/metrics?from=2024-05-15&to=2026-05-15", cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestMetrics_RepoNotFound_404(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e := newReposEcho(t, q, defaultCfg())
	cookie := seedAndLogin(t, e, q)

	rec := doJSON(t, e, http.MethodGet,
		"/api/v1/repos/nonexistent/ghost/metrics", cookie, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestMetrics_CrossTenantRepo_404(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e := newReposEcho(t, q, defaultCfg())
	cookie := seedAndLogin(t, e, q)

	// Seed a repo under another tenant.
	other, err := q.CreateTenant(context.Background(), "other")
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	seedRepo(t, q, other.ID, "octo", "hello", false)

	rec := doJSON(t, e, http.MethodGet,
		"/api/v1/repos/octo/hello/metrics", cookie, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (cross-tenant)", rec.Code)
	}
}

func TestMetrics_ArchivedRepoStillVisible(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e := newReposEcho(t, q, defaultCfg())
	cookie := seedAndLogin(t, e, q)
	tenantID := tenantIDForLogin(t, q)
	repo := seedRepo(t, q, tenantID, "octo", "hello", true)
	seedDailyRepoStats(t, q, repo.ID, "2026-05-17", i64(3600), i64(7200))

	rec := doJSON(t, e, http.MethodGet,
		"/api/v1/repos/octo/hello/metrics?from=2026-05-17&to=2026-05-17", cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp repos.MetricsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Repo.Archived {
		t.Error("archived = false, want true")
	}
	if len(resp.DailyStats) != 1 {
		t.Errorf("daily_stats = %d, want 1 (archived repo keeps history)",
			len(resp.DailyStats))
	}
}

func TestMetrics_NoCookie_401(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e := newReposEcho(t, q, defaultCfg())

	rec := doJSON(t, e, http.MethodGet,
		"/api/v1/repos/octo/hello/metrics", nil, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

