package sync_test

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
	apisync "github.com/karnstack/tempo/internal/api/sync"
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
	path := filepath.Join(t.TempDir(), "sync_integration.db")
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

func newSyncEcho(t *testing.T, q *sqlitedb.Queries) *echo.Echo {
	t.Helper()
	m := intauth.NewManager(q, time.Hour, false)
	r := intauth.NewRegistrar(q)
	a := intauth.NewAuthenticator(q)
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	l := zaptest.NewLogger(t)
	apiauth.Configure(e, l, m, r, a)
	apisync.Configure(e, l, m, q)
	return e
}

func seedAndLogin(t *testing.T, e http.Handler, q *sqlitedb.Queries) *http.Cookie {
	t.Helper()
	if _, err := intauth.NewRegistrar(q).Register(context.Background(), seedEmail, seedPassword); err != nil {
		t.Fatalf("Register: %v", err)
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
	t.Fatal("no session")
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

func seedToken(t *testing.T, q *sqlitedb.Queries, tenantID int64) int64 {
	t.Helper()
	row, err := q.CreateGhToken(context.Background(), sqlitedb.CreateGhTokenParams{
		TenantID: tenantID, Label: "main", EncryptedPat: []byte("x"), Scopes: "repo",
	})
	if err != nil {
		t.Fatalf("CreateGhToken: %v", err)
	}
	return row.ID
}

func seedConnection(t *testing.T, q *sqlitedb.Queries, tenantID, tokenID int64, owner, name string) sqlitedb.Connection {
	t.Helper()
	n := name
	row, err := q.CreateConnection(context.Background(), sqlitedb.CreateConnectionParams{
		TenantID: tenantID, Kind: "repo", Owner: owner, Name: &n,
		TokenID: tokenID, BackfillFrom: time.Now().UTC(), Status: "active",
	})
	if err != nil {
		t.Fatalf("CreateConnection: %v", err)
	}
	return row
}

func seedRun(t *testing.T, q *sqlitedb.Queries, connectionID int64, startedAt time.Time, ok int64, errMsg string) sqlitedb.SyncRun {
	t.Helper()
	ctx := context.Background()
	r, err := q.StartSyncRun(ctx, sqlitedb.StartSyncRunParams{
		ConnectionID: connectionID, StartedAt: startedAt,
	})
	if err != nil {
		t.Fatalf("StartSyncRun: %v", err)
	}
	finishedAt := startedAt.Add(time.Millisecond)
	if err := q.FinishSyncRun(ctx, sqlitedb.FinishSyncRunParams{
		FinishedAt: &finishedAt, Ok: ok, Items: 0, Error: errMsg, ID: r.ID,
	}); err != nil {
		t.Fatalf("FinishSyncRun: %v", err)
	}
	r.FinishedAt = &finishedAt
	r.Ok = ok
	r.Error = errMsg
	return r
}

func TestStatus_Empty(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e := newSyncEcho(t, q)
	cookie := seedAndLogin(t, e, q)

	rec := doGET(t, e, "/api/v1/sync/status", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp apisync.SyncStatusResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Connections) != 0 {
		t.Errorf("connections = %d, want 0", len(resp.Connections))
	}
}

func TestStatus_HappyPath(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e := newSyncEcho(t, q)
	cookie := seedAndLogin(t, e, q)
	tenantID := tenantIDForLogin(t, q)
	tokenID := seedToken(t, q, tenantID)
	conn := seedConnection(t, q, tenantID, tokenID, "octo", "hello")

	now := time.Now().UTC()
	seedRun(t, q, conn.ID, now.Add(-2*time.Hour), 1, "")    // success
	seedRun(t, q, conn.ID, now.Add(-1*time.Hour), 0, "boom") // failure (latest)

	rec := doGET(t, e, "/api/v1/sync/status", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var resp apisync.SyncStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Connections) != 1 {
		t.Fatalf("connections = %d, want 1", len(resp.Connections))
	}
	c := resp.Connections[0]
	if c.LatestRun == nil || c.LatestRun.Ok != 0 || c.LatestRun.Error != "boom" {
		t.Errorf("latest_run = %+v, want failure with boom", c.LatestRun)
	}
	if c.LastSuccess == nil || c.LastSuccess.Ok != 1 {
		t.Errorf("last_success = %+v", c.LastSuccess)
	}
	if c.LastFailure == nil || c.LastFailure.Error != "boom" {
		t.Errorf("last_failure = %+v", c.LastFailure)
	}
}

func TestStatus_NoSyncRunsYet(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e := newSyncEcho(t, q)
	cookie := seedAndLogin(t, e, q)
	tenantID := tenantIDForLogin(t, q)
	tokenID := seedToken(t, q, tenantID)
	seedConnection(t, q, tenantID, tokenID, "octo", "hello")

	rec := doGET(t, e, "/api/v1/sync/status", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var resp apisync.SyncStatusResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Connections) != 1 {
		t.Fatalf("connections = %d", len(resp.Connections))
	}
	c := resp.Connections[0]
	if c.LatestRun != nil || c.LastSuccess != nil || c.LastFailure != nil {
		t.Errorf("expected all run pointers nil, got %+v", c)
	}
}

func TestStatus_CrossTenantIsolated(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e := newSyncEcho(t, q)
	cookie := seedAndLogin(t, e, q)

	// Another tenant has connections; caller has none.
	other, _ := q.CreateTenant(context.Background(), "other")
	otherToken := seedToken(t, q, other.ID)
	seedConnection(t, q, other.ID, otherToken, "ghost", "secret")

	rec := doGET(t, e, "/api/v1/sync/status", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var resp apisync.SyncStatusResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Connections) != 0 {
		t.Errorf("connections = %d, want 0 (other tenant's data leaked)", len(resp.Connections))
	}
}

func TestStatus_NoCookie_401(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e := newSyncEcho(t, q)
	rec := doGET(t, e, "/api/v1/sync/status", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
