package auth_test

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
	intauth "github.com/karnstack/tempo/internal/auth"
	"github.com/karnstack/tempo/internal/config"
	"github.com/karnstack/tempo/internal/storage/sqlite"
	"github.com/karnstack/tempo/internal/storage/sqlite/sqlitedb"
	"github.com/karnstack/tempo/migrations"
	"github.com/labstack/echo/v4"
	"go.uber.org/fx/fxtest"
	"go.uber.org/zap/zaptest"
)

// newIntegrationStore boots a real on-disk SQLite, applies migrations,
// and returns the sqlc handle. Mirrors the helper in
// internal/auth/session_test.go but lives here to avoid an
// internal-package import cycle.
func newIntegrationStore(t *testing.T) *sqlitedb.Queries {
	t.Helper()
	lc := fxtest.NewLifecycle(t)
	l := zaptest.NewLogger(t)
	path := filepath.Join(t.TempDir(), "register_integration.db")
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

// newAuthEcho returns a fresh echo with the auth route group mounted
// against q. Mirrors the production wiring without the rest of the
// middleware chain (we don't need access logs in handler tests).
func newAuthEcho(t *testing.T, q *sqlitedb.Queries) (*echo.Echo, *intauth.Manager) {
	t.Helper()
	m := intauth.NewManager(q, time.Hour, false)
	r := intauth.NewRegistrar(q)
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	apiauth.Configure(e, zaptest.NewLogger(t), m, r)
	return e, m
}

func cookieByName(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func TestFirstRun_FreshDB_True(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e, _ := newAuthEcho(t, q)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/firstrun", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp apiauth.FirstRunResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.FirstRun {
		t.Errorf("first_run = false, want true on empty DB")
	}
}

func TestFirstRun_AfterRegister_False(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e, _ := newAuthEcho(t, q)

	// Register a user via the registrar directly.
	if _, err := intauth.NewRegistrar(q).Register(context.Background(), "admin@acme.test", "hunter22hunter22"); err != nil {
		t.Fatalf("seed Register: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/firstrun", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp apiauth.FirstRunResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.FirstRun {
		t.Errorf("first_run = true after register, want false")
	}
}

func TestRegister_HappyPath(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e, m := newAuthEcho(t, q)

	body := strings.NewReader(`{"email":"admin@acme.test","password":"hunter22hunter22"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var resp apiauth.RegisterResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.User.Email != "admin@acme.test" {
		t.Errorf("user.email = %q", resp.User.Email)
	}
	if resp.User.Role != intauth.AdminRole {
		t.Errorf("user.role = %q, want %q", resp.User.Role, intauth.AdminRole)
	}
	if resp.User.ID == 0 {
		t.Error("user.id = 0")
	}

	// Cookie present, validates against the same manager.
	c := cookieByName(rec, intauth.CookieName)
	if c == nil {
		t.Fatal("no session cookie set")
	}
	if c.Value == "" {
		t.Error("session cookie value is empty")
	}
	if !c.HttpOnly {
		t.Error("session cookie should be HttpOnly")
	}

	validateReq := httptest.NewRequest(http.MethodGet, "/", nil)
	validateReq.AddCookie(c)
	sess, err := m.Validate(context.Background(), validateReq)
	if err != nil {
		t.Fatalf("Validate after register: %v", err)
	}
	if sess.UserID != resp.User.ID {
		t.Errorf("session.UserID = %d, want %d", sess.UserID, resp.User.ID)
	}
}

func TestRegister_AlreadyClosed_409(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e, _ := newAuthEcho(t, q)
	ctx := context.Background()

	// First registration via the same handler.
	first := strings.NewReader(`{"email":"admin@acme.test","password":"hunter22hunter22"}`)
	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", first)
	req1.Header.Set("Content-Type", "application/json")
	rec1 := httptest.NewRecorder()
	e.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusCreated {
		t.Fatalf("first register status = %d", rec1.Code)
	}

	// Second attempt → 409.
	second := strings.NewReader(`{"email":"second@acme.test","password":"anotherlongpw"}`)
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", second)
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	e.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusConflict {
		t.Fatalf("second register status = %d, want 409; body=%s", rec2.Code, rec2.Body.String())
	}
	if c := cookieByName(rec2, intauth.CookieName); c != nil {
		t.Errorf("session cookie should not be set on 409, got %+v", c)
	}

	// Still exactly one user.
	tenants, err := q.ListTenants(ctx)
	if err != nil {
		t.Fatalf("ListTenants: %v", err)
	}
	if len(tenants) != 1 {
		t.Fatalf("len(tenants) = %d, want 1", len(tenants))
	}
	c, err := q.CountUsersByTenant(ctx, tenants[0].ID)
	if err != nil {
		t.Fatalf("CountUsersByTenant: %v", err)
	}
	if c != 1 {
		t.Errorf("user count = %d, want 1", c)
	}
}

func TestRegister_InvalidEmail_400(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e, _ := newAuthEcho(t, q)

	body := strings.NewReader(`{"email":"not-an-email","password":"hunter22hunter22"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if c := cookieByName(rec, intauth.CookieName); c != nil {
		t.Errorf("session cookie should not be set on 400")
	}
}

func TestRegister_ShortPassword_400(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e, _ := newAuthEcho(t, q)

	body := strings.NewReader(`{"email":"admin@acme.test","password":"short"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestRegister_BadJSON_400(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e, _ := newAuthEcho(t, q)

	body := strings.NewReader(`{not-json`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
