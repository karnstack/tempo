package auth_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apiauth "github.com/karnstack/tempo/internal/api/auth"
	intauth "github.com/karnstack/tempo/internal/auth"
	"github.com/karnstack/tempo/internal/storage/sqlite/sqlitedb"
)

const seedEmail = "admin@acme.test"
const seedPassword = "hunter22hunter22"

// seedAdmin registers the admin via the registrar so /login has a row to
// authenticate against.
func seedAdmin(t *testing.T, q *sqlitedb.Queries) {
	t.Helper()
	if _, err := intauth.NewRegistrar(q).Register(context.Background(), seedEmail, seedPassword); err != nil {
		t.Fatalf("seed Register: %v", err)
	}
}

func postJSON(t *testing.T, e http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func TestLogin_HappyPath(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e, m := newAuthEcho(t, q)
	seedAdmin(t, q)

	rec := postJSON(t, e, "/api/v1/auth/login",
		`{"email":"admin@acme.test","password":"hunter22hunter22"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp apiauth.LoginResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.User.Email != seedEmail {
		t.Errorf("user.email = %q, want %q", resp.User.Email, seedEmail)
	}
	if resp.User.Role != intauth.AdminRole {
		t.Errorf("user.role = %q, want %q", resp.User.Role, intauth.AdminRole)
	}
	if resp.User.ID == 0 {
		t.Error("user.id = 0")
	}

	c := cookieByName(rec, intauth.CookieName)
	if c == nil {
		t.Fatal("no session cookie set on 200")
	}
	if !c.HttpOnly {
		t.Error("session cookie should be HttpOnly")
	}

	// Cookie validates against the same manager.
	validateReq := httptest.NewRequest(http.MethodGet, "/", nil)
	validateReq.AddCookie(c)
	sess, err := m.Validate(context.Background(), validateReq)
	if err != nil {
		t.Fatalf("Validate after login: %v", err)
	}
	if sess.UserID != resp.User.ID {
		t.Errorf("session.UserID = %d, want %d", sess.UserID, resp.User.ID)
	}
}

func TestLogin_WrongPassword_401(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e, _ := newAuthEcho(t, q)
	seedAdmin(t, q)

	rec := postJSON(t, e, "/api/v1/auth/login",
		`{"email":"admin@acme.test","password":"wrongpassword12"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", rec.Code, rec.Body.String())
	}
	if c := cookieByName(rec, intauth.CookieName); c != nil && c.MaxAge >= 0 && c.Value != "" {
		t.Errorf("session cookie should not be set on 401, got %+v", c)
	}
}

func TestLogin_UnknownEmail_401(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e, _ := newAuthEcho(t, q)
	// No seedAdmin — DB is empty.

	rec := postJSON(t, e, "/api/v1/auth/login",
		`{"email":"nobody@acme.test","password":"irrelevant1234"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestLogin_MalformedEmail_401(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e, _ := newAuthEcho(t, q)
	seedAdmin(t, q)

	rec := postJSON(t, e, "/api/v1/auth/login",
		`{"email":"not-an-email","password":"irrelevant1234"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestLogin_EmptyPassword_401(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e, _ := newAuthEcho(t, q)
	seedAdmin(t, q)

	rec := postJSON(t, e, "/api/v1/auth/login",
		`{"email":"admin@acme.test","password":""}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestLogin_CaseInsensitiveEmail_200(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e, _ := newAuthEcho(t, q)
	seedAdmin(t, q)

	rec := postJSON(t, e, "/api/v1/auth/login",
		`{"email":"  Admin@Acme.TEST  ","password":"hunter22hunter22"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

func TestLogin_BadJSON_400(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e, _ := newAuthEcho(t, q)
	seedAdmin(t, q)

	rec := postJSON(t, e, "/api/v1/auth/login", `{not-json`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// loginAndCookie performs a successful login and returns the session cookie.
func loginAndCookie(t *testing.T, e http.Handler) *http.Cookie {
	t.Helper()
	rec := postJSON(t, e, "/api/v1/auth/login",
		`{"email":"admin@acme.test","password":"hunter22hunter22"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("login: status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	c := cookieByName(rec, intauth.CookieName)
	if c == nil {
		t.Fatal("login did not set session cookie")
	}
	return c
}

func TestLogout_WithSession_204_DeletesRow(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e, m := newAuthEcho(t, q)
	seedAdmin(t, q)

	cookie := loginAndCookie(t, e)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body=%s", rec.Code, rec.Body.String())
	}
	cleared := cookieByName(rec, intauth.CookieName)
	if cleared == nil {
		t.Fatal("logout did not write a clearing Set-Cookie")
	}
	if cleared.MaxAge >= 0 {
		t.Errorf("clearing cookie MaxAge = %d, want < 0", cleared.MaxAge)
	}

	// Replaying the original cookie should now look unknown.
	validateReq := httptest.NewRequest(http.MethodGet, "/", nil)
	validateReq.AddCookie(cookie)
	if _, err := m.Validate(context.Background(), validateReq); !errors.Is(err, intauth.ErrSessionUnknown) {
		t.Errorf("Validate after logout err = %v, want ErrSessionUnknown", err)
	}
}

func TestLogout_NoCookie_204(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e, _ := newAuthEcho(t, q)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	cleared := cookieByName(rec, intauth.CookieName)
	if cleared == nil {
		t.Fatal("logout should still write a clearing cookie even without a session")
	}
	if cleared.MaxAge >= 0 {
		t.Errorf("clearing cookie MaxAge = %d, want < 0", cleared.MaxAge)
	}
}

func TestLogout_UnknownSession_204(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e, _ := newAuthEcho(t, q)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: intauth.CookieName, Value: "no-such-session"})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	cleared := cookieByName(rec, intauth.CookieName)
	if cleared == nil {
		t.Fatal("logout should still write a clearing cookie")
	}
	if cleared.MaxAge >= 0 {
		t.Errorf("clearing cookie MaxAge = %d, want < 0", cleared.MaxAge)
	}
}
