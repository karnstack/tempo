package tokens_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	apiauth "github.com/karnstack/tempo/internal/api/auth"
	"github.com/karnstack/tempo/internal/api/tokens"
	intauth "github.com/karnstack/tempo/internal/auth"
	"github.com/karnstack/tempo/internal/config"
	"github.com/karnstack/tempo/internal/secret"
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
	path := filepath.Join(t.TempDir(), "tokens_integration.db")
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

func newTokensEcho(t *testing.T, q *sqlitedb.Queries) (*echo.Echo, *intauth.Manager, *secret.Box) {
	t.Helper()
	m := intauth.NewManager(q, time.Hour, false)
	r := intauth.NewRegistrar(q)
	a := intauth.NewAuthenticator(q)
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	box, err := secret.NewBox(key)
	if err != nil {
		t.Fatalf("NewBox: %v", err)
	}
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	l := zaptest.NewLogger(t)
	apiauth.Configure(e, l, m, r, a)
	tokens.Configure(e, l, m, q, box)
	return e, m, box
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

func TestPostTokens_HappyPath_201(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e, _, box := newTokensEcho(t, q)
	cookie := seedAndLogin(t, e, q)

	rec := doJSON(t, e, http.MethodPost, "/api/v1/tokens", cookie,
		`{"label":"main","pat":"ghp_secrettoken1234567890","scopes":"repo,read:org"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var resp tokens.CreateTokenResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Token.Label != "main" {
		t.Errorf("label = %q, want %q", resp.Token.Label, "main")
	}
	if resp.Token.Scopes != "repo,read:org" {
		t.Errorf("scopes = %q", resp.Token.Scopes)
	}
	if resp.Token.ID == 0 {
		t.Error("id = 0")
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("ghp_")) {
		t.Errorf("response body leaks PAT: %s", rec.Body.String())
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("encrypted_pat")) {
		t.Errorf("response body leaks encrypted_pat: %s", rec.Body.String())
	}

	row, err := q.GetGhToken(context.Background(), resp.Token.ID)
	if err != nil {
		t.Fatalf("GetGhToken: %v", err)
	}
	plain, err := box.Decrypt(row.EncryptedPat)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if string(plain) != "ghp_secrettoken1234567890" {
		t.Errorf("decrypted = %q", string(plain))
	}
}

func TestPostTokens_TrimsWhitespace(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e, _, box := newTokensEcho(t, q)
	cookie := seedAndLogin(t, e, q)

	rec := doJSON(t, e, http.MethodPost, "/api/v1/tokens", cookie,
		`{"label":"  main  ","pat":"  ghp_secrettoken1234567890\n","scopes":"repo"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var resp tokens.CreateTokenResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Token.Label != "main" {
		t.Errorf("label = %q, want trimmed %q", resp.Token.Label, "main")
	}
	row, err := q.GetGhToken(context.Background(), resp.Token.ID)
	if err != nil {
		t.Fatalf("GetGhToken: %v", err)
	}
	plain, err := box.Decrypt(row.EncryptedPat)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if string(plain) != "ghp_secrettoken1234567890" {
		t.Errorf("decrypted = %q, want trimmed PAT", string(plain))
	}
}

func TestPostTokens_EmptyLabel_400(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e, _, _ := newTokensEcho(t, q)
	cookie := seedAndLogin(t, e, q)

	rec := doJSON(t, e, http.MethodPost, "/api/v1/tokens", cookie,
		`{"label":"   ","pat":"ghp_x"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestPostTokens_EmptyPAT_400(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e, _, _ := newTokensEcho(t, q)
	cookie := seedAndLogin(t, e, q)

	rec := doJSON(t, e, http.MethodPost, "/api/v1/tokens", cookie,
		`{"label":"main","pat":""}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestPostTokens_BadJSON_400(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e, _, _ := newTokensEcho(t, q)
	cookie := seedAndLogin(t, e, q)

	rec := doJSON(t, e, http.MethodPost, "/api/v1/tokens", cookie, `{not-json`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestPostTokens_NoCookie_401(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e, _, _ := newTokensEcho(t, q)

	rec := doJSON(t, e, http.MethodPost, "/api/v1/tokens", nil,
		`{"label":"main","pat":"ghp_x"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestPostTokens_WithExpiresAt(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e, _, _ := newTokensEcho(t, q)
	cookie := seedAndLogin(t, e, q)

	exp := time.Date(2027, 1, 15, 0, 0, 0, 0, time.UTC)
	body, _ := json.Marshal(map[string]any{
		"label":      "main",
		"pat":        "ghp_x",
		"scopes":     "",
		"expires_at": exp,
	})
	rec := doJSON(t, e, http.MethodPost, "/api/v1/tokens", cookie, string(body))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var resp tokens.CreateTokenResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Token.ExpiresAt == nil || !resp.Token.ExpiresAt.Equal(exp) {
		t.Errorf("expires_at = %v, want %v", resp.Token.ExpiresAt, exp)
	}
}

func TestGetTokens_Multiple_200(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e, _, _ := newTokensEcho(t, q)
	cookie := seedAndLogin(t, e, q)

	for _, label := range []string{"alpha", "bravo", "charlie"} {
		rec := doJSON(t, e, http.MethodPost, "/api/v1/tokens", cookie,
			`{"label":"`+label+`","pat":"ghp_`+label+`"}`)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create %s: status = %d; body=%s", label, rec.Code, rec.Body.String())
		}
	}

	rec := doJSON(t, e, http.MethodGet, "/api/v1/tokens", cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp tokens.ListTokensResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Tokens) != 3 {
		t.Fatalf("got %d tokens, want 3", len(resp.Tokens))
	}
	for _, tok := range resp.Tokens {
		if tok.Label == "" {
			t.Error("token has empty label")
		}
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("ghp_")) {
		t.Errorf("list leaks PAT: %s", rec.Body.String())
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("encrypted_pat")) {
		t.Errorf("list leaks encrypted_pat: %s", rec.Body.String())
	}
}

func TestGetTokens_NoCookie_401(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e, _, _ := newTokensEcho(t, q)

	rec := doJSON(t, e, http.MethodGet, "/api/v1/tokens", nil, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestDeleteToken_Happy_204(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e, _, _ := newTokensEcho(t, q)
	cookie := seedAndLogin(t, e, q)

	createRec := doJSON(t, e, http.MethodPost, "/api/v1/tokens", cookie,
		`{"label":"main","pat":"ghp_x"}`)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create: status = %d", createRec.Code)
	}
	var created tokens.CreateTokenResponse
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}

	idStr := strconv.FormatInt(created.Token.ID, 10)
	rec := doJSON(t, e, http.MethodDelete, "/api/v1/tokens/"+idStr, cookie, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body=%s", rec.Code, rec.Body.String())
	}

	if _, err := q.GetGhToken(context.Background(), created.Token.ID); err == nil {
		t.Error("token row still present after delete")
	}
}

func TestDeleteToken_Missing_404(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e, _, _ := newTokensEcho(t, q)
	cookie := seedAndLogin(t, e, q)

	rec := doJSON(t, e, http.MethodDelete, "/api/v1/tokens/9999", cookie, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestDeleteToken_OtherTenant_404(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e, _, _ := newTokensEcho(t, q)
	cookie := seedAndLogin(t, e, q)

	other, err := q.CreateGhToken(context.Background(), sqlitedb.CreateGhTokenParams{
		TenantID:     999,
		Label:        "intruder",
		EncryptedPat: []byte{0xde, 0xad},
		Scopes:       "",
	})
	if err != nil {
		t.Fatalf("seed other tenant token: %v", err)
	}

	rec := doJSON(t, e, http.MethodDelete, "/api/v1/tokens/"+strconv.FormatInt(other.ID, 10), cookie, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if _, err := q.GetGhToken(context.Background(), other.ID); err != nil {
		t.Errorf("other-tenant token was deleted: %v", err)
	}
}

func TestDeleteToken_InUse_409(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e, _, _ := newTokensEcho(t, q)
	cookie := seedAndLogin(t, e, q)
	tenantID := tenantIDForLogin(t, q)

	createRec := doJSON(t, e, http.MethodPost, "/api/v1/tokens", cookie,
		`{"label":"main","pat":"ghp_x"}`)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create: status = %d", createRec.Code)
	}
	var created tokens.CreateTokenResponse
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if _, err := q.CreateConnection(context.Background(), sqlitedb.CreateConnectionParams{
		TenantID:     tenantID,
		Kind:         "repo",
		Owner:        "acme",
		Name:         strPtr("widget"),
		TokenID:      created.Token.ID,
		BackfillFrom: time.Now().Add(-90 * 24 * time.Hour),
		Status:       "active",
	}); err != nil {
		t.Fatalf("seed connection: %v", err)
	}

	rec := doJSON(t, e, http.MethodDelete, "/api/v1/tokens/"+strconv.FormatInt(created.Token.ID, 10), cookie, "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", rec.Code, rec.Body.String())
	}
	var conf tokens.DeleteConflictResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &conf); err != nil {
		t.Fatalf("decode conflict: %v", err)
	}
	if conf.Error != "token in use" {
		t.Errorf("error = %q", conf.Error)
	}
	if conf.ConnectionCount != 1 {
		t.Errorf("connection_count = %d, want 1", conf.ConnectionCount)
	}
	if _, err := q.GetGhToken(context.Background(), created.Token.ID); err != nil {
		t.Errorf("token row was deleted on 409: %v", err)
	}
}

func TestDeleteToken_NoCookie_401(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e, _, _ := newTokensEcho(t, q)

	rec := doJSON(t, e, http.MethodDelete, "/api/v1/tokens/1", nil, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func strPtr(s string) *string { return &s }
