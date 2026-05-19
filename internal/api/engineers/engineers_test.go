package engineers_test

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
	"github.com/karnstack/tempo/internal/api/engineers"
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
	path := filepath.Join(t.TempDir(), "engineers_integration.db")
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

func newEngineersEcho(t *testing.T, q *sqlitedb.Queries, cfg *config.Config) *echo.Echo {
	t.Helper()
	m := intauth.NewManager(q, time.Hour, false)
	r := intauth.NewRegistrar(q)
	a := intauth.NewAuthenticator(q)
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	l := zaptest.NewLogger(t)
	apiauth.Configure(e, l, m, r, a)
	engineers.Configure(e, l, m, q, cfg)
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
		t.Fatalf("login: %d", rec.Code)
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

func seedGhUser(t *testing.T, q *sqlitedb.Queries, tenantID int64, login string) sqlitedb.GhUser {
	t.Helper()
	row, err := q.UpsertGhUser(context.Background(), sqlitedb.UpsertGhUserParams{
		TenantID: tenantID,
		GhID:     int64(time.Now().UnixNano()) + int64(len(login)),
		Login:    login,
	})
	if err != nil {
		t.Fatalf("UpsertGhUser(%s): %v", login, err)
	}
	return row
}

func seedDailyEngineerStat(t *testing.T, q *sqlitedb.Queries, repoID, uid int64, date string, commits, opened, merged int64) {
	t.Helper()
	if err := q.UpsertDailyEngineerStats(context.Background(), sqlitedb.UpsertDailyEngineerStatsParams{
		Date: date, RepoID: repoID, GhUserID: uid,
		Commits: commits, PrsOpened: opened, PrsMerged: merged,
	}); err != nil {
		t.Fatalf("UpsertDailyEngineerStats: %v", err)
	}
}

func seedDailyReviewLoad(t *testing.T, q *sqlitedb.Queries, repoID, uid int64, date string, reviews int64) {
	t.Helper()
	if err := q.UpsertDailyReviewLoad(context.Background(), sqlitedb.UpsertDailyReviewLoadParams{
		Date: date, RepoID: repoID, ReviewerGhUserID: uid, Reviews: reviews,
	}); err != nil {
		t.Fatalf("UpsertDailyReviewLoad: %v", err)
	}
}

func TestList_Empty(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e := newEngineersEcho(t, q, defaultCfg())
	cookie := seedAndLogin(t, e, q)
	rec := doGET(t, e, "/api/v1/engineers", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp engineers.ListEngineersResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Engineers) != 0 {
		t.Errorf("engineers = %d, want 0", len(resp.Engineers))
	}
}

func TestList_OrdersByLogin(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e := newEngineersEcho(t, q, defaultCfg())
	cookie := seedAndLogin(t, e, q)
	tenantID := tenantIDForLogin(t, q)
	seedGhUser(t, q, tenantID, "zeta")
	seedGhUser(t, q, tenantID, "alpha")
	other, _ := q.CreateTenant(context.Background(), "other")
	seedGhUser(t, q, other.ID, "ghost") // must not appear

	rec := doGET(t, e, "/api/v1/engineers", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var resp engineers.ListEngineersResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Engineers) != 2 {
		t.Fatalf("engineers = %d, want 2", len(resp.Engineers))
	}
	if resp.Engineers[0].Login != "alpha" || resp.Engineers[1].Login != "zeta" {
		t.Errorf("logins = %s/%s, want alpha/zeta", resp.Engineers[0].Login, resp.Engineers[1].Login)
	}
	if strings.Contains(rec.Body.String(), "tenant_id") {
		t.Errorf("body leaks tenant_id: %s", rec.Body.String())
	}
}

func TestList_NoCookie_401(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e := newEngineersEcho(t, q, defaultCfg())
	rec := doGET(t, e, "/api/v1/engineers", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestMetrics_HappyPath(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e := newEngineersEcho(t, q, defaultCfg())
	cookie := seedAndLogin(t, e, q)
	tenantID := tenantIDForLogin(t, q)
	u := seedGhUser(t, q, tenantID, "octo")

	repo1, _ := q.CreateRepo(context.Background(), sqlitedb.CreateRepoParams{
		TenantID: tenantID, ConnectionID: 1, GhID: 101,
		Owner: "a", Name: "x", DefaultBranch: "main",
	})
	repo2, _ := q.CreateRepo(context.Background(), sqlitedb.CreateRepoParams{
		TenantID: tenantID, ConnectionID: 1, GhID: 102,
		Owner: "a", Name: "y", DefaultBranch: "main",
	})
	seedDailyEngineerStat(t, q, repo1.ID, u.ID, "2026-05-17", 3, 1, 1)
	seedDailyEngineerStat(t, q, repo2.ID, u.ID, "2026-05-17", 1, 0, 0)
	seedDailyReviewLoad(t, q, repo1.ID, u.ID, "2026-05-17", 5)

	rec := doGET(t, e, "/api/v1/engineers/octo/metrics?from=2026-05-17&to=2026-05-17", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp engineers.MetricsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Engineer.Login != "octo" {
		t.Errorf("login = %q, want octo", resp.Engineer.Login)
	}
	if len(resp.DailyStats) != 2 {
		t.Errorf("daily_stats = %d, want 2 rows (one per repo)", len(resp.DailyStats))
	}
	if len(resp.DailyReviewLoad) != 1 || resp.DailyReviewLoad[0].Reviews != 5 {
		t.Errorf("review_load = %+v", resp.DailyReviewLoad)
	}
}

func TestMetrics_DefaultRange30Days(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e := newEngineersEcho(t, q, defaultCfg())
	cookie := seedAndLogin(t, e, q)
	tenantID := tenantIDForLogin(t, q)
	seedGhUser(t, q, tenantID, "octo")

	rec := doGET(t, e, "/api/v1/engineers/octo/metrics", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var resp engineers.MetricsResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	today := time.Now().UTC().Format("2006-01-02")
	wantFrom := time.Now().UTC().AddDate(0, 0, -29).Format("2006-01-02")
	if resp.To != today || resp.From != wantFrom {
		t.Errorf("range = %s..%s, want %s..%s", resp.From, resp.To, wantFrom, today)
	}
}

func TestMetrics_InvalidFrom_400(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e := newEngineersEcho(t, q, defaultCfg())
	cookie := seedAndLogin(t, e, q)
	tenantID := tenantIDForLogin(t, q)
	seedGhUser(t, q, tenantID, "octo")

	rec := doGET(t, e, "/api/v1/engineers/octo/metrics?from=bogus", cookie)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestMetrics_FromAfterTo_400(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e := newEngineersEcho(t, q, defaultCfg())
	cookie := seedAndLogin(t, e, q)
	tenantID := tenantIDForLogin(t, q)
	seedGhUser(t, q, tenantID, "octo")

	rec := doGET(t, e, "/api/v1/engineers/octo/metrics?from=2026-05-20&to=2026-05-10", cookie)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestMetrics_RangeTooLong_400(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e := newEngineersEcho(t, q, defaultCfg())
	cookie := seedAndLogin(t, e, q)
	tenantID := tenantIDForLogin(t, q)
	seedGhUser(t, q, tenantID, "octo")

	rec := doGET(t, e, "/api/v1/engineers/octo/metrics?from=2024-01-01&to=2026-05-01", cookie)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestMetrics_RespectsRange(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e := newEngineersEcho(t, q, defaultCfg())
	cookie := seedAndLogin(t, e, q)
	tenantID := tenantIDForLogin(t, q)
	u := seedGhUser(t, q, tenantID, "octo")
	repo, _ := q.CreateRepo(context.Background(), sqlitedb.CreateRepoParams{
		TenantID: tenantID, ConnectionID: 1, GhID: 101,
		Owner: "a", Name: "x", DefaultBranch: "main",
	})
	seedDailyEngineerStat(t, q, repo.ID, u.ID, "2026-05-10", 5, 0, 0)
	seedDailyEngineerStat(t, q, repo.ID, u.ID, "2026-05-15", 10, 0, 0)
	seedDailyEngineerStat(t, q, repo.ID, u.ID, "2026-05-20", 15, 0, 0)

	rec := doGET(t, e, "/api/v1/engineers/octo/metrics?from=2026-05-15&to=2026-05-15", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var resp engineers.MetricsResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.DailyStats) != 1 || resp.DailyStats[0].Commits != 10 {
		t.Errorf("stats = %+v, want one row commits=10", resp.DailyStats)
	}
}

func TestMetrics_LoginNotFound_404(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e := newEngineersEcho(t, q, defaultCfg())
	cookie := seedAndLogin(t, e, q)
	rec := doGET(t, e, "/api/v1/engineers/ghost/metrics", cookie)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestMetrics_CrossTenantEngineer_404(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e := newEngineersEcho(t, q, defaultCfg())
	cookie := seedAndLogin(t, e, q)
	other, _ := q.CreateTenant(context.Background(), "other")
	seedGhUser(t, q, other.ID, "octo")

	rec := doGET(t, e, "/api/v1/engineers/octo/metrics", cookie)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (cross-tenant)", rec.Code)
	}
}

func TestMetrics_NoCookie_401(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e := newEngineersEcho(t, q, defaultCfg())
	rec := doGET(t, e, "/api/v1/engineers/octo/metrics", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
