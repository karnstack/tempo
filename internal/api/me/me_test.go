package me_test

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
	"github.com/karnstack/tempo/internal/api/me"
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
	path := filepath.Join(t.TempDir(), "me_integration.db")
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

func newMeEcho(t *testing.T, q *sqlitedb.Queries) (*echo.Echo, *intauth.Manager) {
	t.Helper()
	m := intauth.NewManager(q, time.Hour, false)
	r := intauth.NewRegistrar(q)
	a := intauth.NewAuthenticator(q)
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	l := zaptest.NewLogger(t)
	apiauth.Configure(e, l, m, r, a)
	me.Configure(e, l, m, q)
	return e, m
}

func seedAdmin(t *testing.T, q *sqlitedb.Queries) sqlitedb.User {
	t.Helper()
	user, err := intauth.NewRegistrar(q).Register(context.Background(), seedEmail, seedPassword)
	if err != nil {
		t.Fatalf("seed Register: %v", err)
	}
	return user
}

func loginAndCookie(t *testing.T, e http.Handler) *http.Cookie {
	t.Helper()
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

func TestMe_HappyPath_200(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e, _ := newMeEcho(t, q)
	user := seedAdmin(t, q)
	cookie := loginAndCookie(t, e)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp me.MeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.User.ID != user.ID {
		t.Errorf("user.id = %d, want %d", resp.User.ID, user.ID)
	}
	if resp.User.Email != seedEmail {
		t.Errorf("user.email = %q, want %q", resp.User.Email, seedEmail)
	}
	if resp.User.Role != intauth.AdminRole {
		t.Errorf("user.role = %q, want %q", resp.User.Role, intauth.AdminRole)
	}
}

func TestMe_NoCookie_401(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e, _ := newMeEcho(t, q)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", rec.Code, rec.Body.String())
	}
}

func TestMe_UnknownSession_401(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e, _ := newMeEcho(t, q)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req.AddCookie(&http.Cookie{Name: intauth.CookieName, Value: "nope"})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestMe_ExpiredSession_401(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e, _ := newMeEcho(t, q)
	user := seedAdmin(t, q)

	expiredID := "expired-session-id"
	if _, err := q.CreateSession(context.Background(), sqlitedb.CreateSessionParams{
		ID:        expiredID,
		UserID:    user.ID,
		ExpiresAt: time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatalf("seed expired session: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req.AddCookie(&http.Cookie{Name: intauth.CookieName, Value: expiredID})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestMe_OrphanUser_401_ClearsCookie(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e, _ := newMeEcho(t, q)
	user := seedAdmin(t, q)
	cookie := loginAndCookie(t, e)

	if err := q.DeleteUser(context.Background(), user.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", rec.Code, rec.Body.String())
	}
	var cleared *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == intauth.CookieName {
			cleared = c
		}
	}
	if cleared == nil {
		t.Fatal("expected clearing cookie on orphan-user 401")
	}
	if cleared.MaxAge >= 0 {
		t.Errorf("clearing cookie MaxAge = %d, want < 0", cleared.MaxAge)
	}
}
