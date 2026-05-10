package web_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/karnstack/tempo/internal/api/web"
	"github.com/karnstack/tempo/internal/auth"
	"github.com/karnstack/tempo/internal/storage/sqlite/sqlitedb"
	"github.com/labstack/echo/v4"
)

// fakeSessionStore is a minimal auth.SessionStore for middleware tests.
type fakeSessionStore struct {
	rows map[string]sqlitedb.Session
}

func newFakeSessionStore() *fakeSessionStore {
	return &fakeSessionStore{rows: map[string]sqlitedb.Session{}}
}

func (f *fakeSessionStore) CreateSession(_ context.Context, arg sqlitedb.CreateSessionParams) (sqlitedb.Session, error) {
	s := sqlitedb.Session{ID: arg.ID, UserID: arg.UserID, ExpiresAt: arg.ExpiresAt, CreatedAt: time.Now()}
	f.rows[arg.ID] = s
	return s, nil
}

func (f *fakeSessionStore) GetSession(_ context.Context, id string) (sqlitedb.Session, error) {
	s, ok := f.rows[id]
	if !ok {
		return sqlitedb.Session{}, sql.ErrNoRows
	}
	return s, nil
}

func (f *fakeSessionStore) DeleteSession(_ context.Context, id string) error {
	delete(f.rows, id)
	return nil
}

func (f *fakeSessionStore) DeleteExpiredSessions(_ context.Context, now time.Time) error {
	for id, s := range f.rows {
		if !s.ExpiresAt.After(now) {
			delete(f.rows, id)
		}
	}
	return nil
}

// newRequireSessionEcho builds a fresh echo with web.RequireSession mounted on
// a /protected handler that writes the session's user_id back as the body.
func newRequireSessionEcho(t *testing.T, m *auth.Manager) *echo.Echo {
	t.Helper()
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	e.GET("/protected", func(c echo.Context) error {
		sess, ok := auth.FromContext(c.Request().Context())
		if !ok {
			return c.String(http.StatusInternalServerError, "no session in context")
		}
		return c.String(http.StatusOK, fmt.Sprintf("user=%d", sess.UserID))
	}, web.RequireSession(m))
	return e
}

func TestRequireSession_HappyPath(t *testing.T) {
	store := newFakeSessionStore()
	m := auth.NewManager(store, time.Hour, false)

	// Issue a session and capture the cookie.
	issueRec := httptest.NewRecorder()
	if _, err := m.Issue(context.Background(), issueRec, 123); err != nil {
		t.Fatalf("Issue: %v", err)
	}
	var cookie *http.Cookie
	for _, c := range issueRec.Result().Cookies() {
		if c.Name == auth.CookieName {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("Issue did not set the session cookie")
	}

	e := newRequireSessionEcho(t, m)
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%q", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); got != "user=123" {
		t.Errorf("body = %q, want %q", got, "user=123")
	}
}

func TestRequireSession_NoCookieReturns401(t *testing.T) {
	m := auth.NewManager(newFakeSessionStore(), time.Hour, false)
	e := newRequireSessionEcho(t, m)

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestRequireSession_UnknownIDReturns401(t *testing.T) {
	m := auth.NewManager(newFakeSessionStore(), time.Hour, false)
	e := newRequireSessionEcho(t, m)

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "no-such-session"})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestRequireSession_ExpiredRowReturns401(t *testing.T) {
	store := newFakeSessionStore()
	expiredID := "expired-id"
	store.rows[expiredID] = sqlitedb.Session{
		ID: expiredID, UserID: 1, ExpiresAt: time.Now().Add(-time.Minute),
	}
	m := auth.NewManager(store, time.Hour, false)
	e := newRequireSessionEcho(t, m)

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: expiredID})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	// The opportunistic delete inside Validate ran.
	if _, ok := store.rows[expiredID]; ok {
		t.Errorf("expired row still in store after request")
	}
}
