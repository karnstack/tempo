package connections_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	apiauth "github.com/karnstack/tempo/internal/api/auth"
	"github.com/karnstack/tempo/internal/api/connections"
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
	path := filepath.Join(t.TempDir(), "connections_integration.db")
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

// newConnectionsEcho returns an echo instance with the auth +
// connections routes mounted. cfg is the config the create handler
// reads BackfillDays from.
func newConnectionsEcho(t *testing.T, q *sqlitedb.Queries, cfg *config.Config) *echo.Echo {
	t.Helper()
	m := intauth.NewManager(q, time.Hour, false)
	r := intauth.NewRegistrar(q)
	a := intauth.NewAuthenticator(q)
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	l := zaptest.NewLogger(t)
	apiauth.Configure(e, l, m, r, a)
	connections.Configure(e, l, m, q, cfg)
	return e
}

func defaultCfg() *config.Config {
	return &config.Config{Poll: config.Poll{BackfillDays: 90}}
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

// seedToken inserts a gh_tokens row for the given tenant and returns
// its id. The PAT itself doesn't matter for these tests — the
// connections handler never decrypts it.
func seedToken(t *testing.T, q *sqlitedb.Queries, tenantID int64, label string) int64 {
	t.Helper()
	row, err := q.CreateGhToken(context.Background(), sqlitedb.CreateGhTokenParams{
		TenantID:     tenantID,
		Label:        label,
		EncryptedPat: []byte("encrypted-placeholder"),
		Scopes:       "repo",
	})
	if err != nil {
		t.Fatalf("CreateGhToken(%s): %v", label, err)
	}
	return row.ID
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

// --- LIST ---

func TestList_Empty(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e := newConnectionsEcho(t, q, defaultCfg())
	cookie := seedAndLogin(t, e, q)

	rec := doJSON(t, e, http.MethodGet, "/api/v1/connections", cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp connections.ListConnectionsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Connections) != 0 {
		t.Errorf("connections = %d, want 0", len(resp.Connections))
	}
}

func TestList_HappyPath(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e := newConnectionsEcho(t, q, defaultCfg())
	cookie := seedAndLogin(t, e, q)
	tenantID := tenantIDForLogin(t, q)
	tokenID := seedToken(t, q, tenantID, "main")

	for i, owner := range []string{"alpha", "beta"} {
		body := fmt.Sprintf(`{"kind":"repo","owner":"%s","name":"r","token_id":%d}`, owner, tokenID)
		rec := doJSON(t, e, http.MethodPost, "/api/v1/connections", cookie, body)
		if rec.Code != http.StatusCreated {
			t.Fatalf("POST #%d: status = %d, want 201; body=%s", i, rec.Code, rec.Body.String())
		}
	}

	rec := doJSON(t, e, http.MethodGet, "/api/v1/connections", cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET: status = %d, want 200", rec.Code)
	}
	var resp connections.ListConnectionsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Connections) != 2 {
		t.Fatalf("connections = %d, want 2", len(resp.Connections))
	}
	if resp.Connections[0].Owner != "alpha" || resp.Connections[1].Owner != "beta" {
		t.Errorf("owners = %s/%s, want alpha/beta (created_at order)",
			resp.Connections[0].Owner, resp.Connections[1].Owner)
	}
}

// --- POST ---

func TestPost_Repo_HappyPath(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e := newConnectionsEcho(t, q, defaultCfg())
	cookie := seedAndLogin(t, e, q)
	tokenID := seedToken(t, q, tenantIDForLogin(t, q), "main")

	body := fmt.Sprintf(`{"kind":"repo","owner":"octocat","name":"hello","token_id":%d}`, tokenID)
	rec := doJSON(t, e, http.MethodPost, "/api/v1/connections", cookie, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var resp connections.CreateConnectionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Connection.ID == 0 {
		t.Error("id = 0")
	}
	if resp.Connection.Kind != "repo" || resp.Connection.Owner != "octocat" {
		t.Errorf("got kind=%q owner=%q", resp.Connection.Kind, resp.Connection.Owner)
	}
	if resp.Connection.Name == nil || *resp.Connection.Name != "hello" {
		t.Errorf("name = %v, want hello", resp.Connection.Name)
	}
	if resp.Connection.Status != "active" {
		t.Errorf("status = %q, want active", resp.Connection.Status)
	}
	if resp.Connection.BackfillFrom.IsZero() {
		t.Error("backfill_from is zero")
	}
	// tenant_id must not leak on the wire.
	if strings.Contains(rec.Body.String(), "tenant_id") {
		t.Errorf("body leaks tenant_id: %s", rec.Body.String())
	}
}

func TestPost_Org_HappyPath(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e := newConnectionsEcho(t, q, defaultCfg())
	cookie := seedAndLogin(t, e, q)
	tokenID := seedToken(t, q, tenantIDForLogin(t, q), "main")

	body := fmt.Sprintf(`{"kind":"org","owner":"acmeorg","token_id":%d}`, tokenID)
	rec := doJSON(t, e, http.MethodPost, "/api/v1/connections", cookie, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var resp connections.CreateConnectionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Connection.Kind != "org" {
		t.Errorf("kind = %q, want org", resp.Connection.Kind)
	}
	if resp.Connection.Name != nil {
		t.Errorf("name = %v, want nil for org", resp.Connection.Name)
	}
}

func TestPost_DefaultsBackfill(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	cfg := &config.Config{Poll: config.Poll{BackfillDays: 30}}
	e := newConnectionsEcho(t, q, cfg)
	cookie := seedAndLogin(t, e, q)
	tokenID := seedToken(t, q, tenantIDForLogin(t, q), "main")

	before := time.Now().UTC()
	body := fmt.Sprintf(`{"kind":"repo","owner":"o","name":"r","token_id":%d}`, tokenID)
	rec := doJSON(t, e, http.MethodPost, "/api/v1/connections", cookie, body)
	after := time.Now().UTC()
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var resp connections.CreateConnectionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	expectMin := before.AddDate(0, 0, -30)
	expectMax := after.AddDate(0, 0, -30)
	if resp.Connection.BackfillFrom.Before(expectMin.Add(-time.Second)) ||
		resp.Connection.BackfillFrom.After(expectMax.Add(time.Second)) {
		t.Errorf("backfill_from = %v, want between %v and %v (now - 30d)",
			resp.Connection.BackfillFrom, expectMin, expectMax)
	}
}

func TestPost_RespectsExplicitBackfill(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e := newConnectionsEcho(t, q, defaultCfg())
	cookie := seedAndLogin(t, e, q)
	tokenID := seedToken(t, q, tenantIDForLogin(t, q), "main")

	want := time.Date(2025, 8, 1, 0, 0, 0, 0, time.UTC)
	body, _ := json.Marshal(map[string]any{
		"kind": "repo", "owner": "o", "name": "r",
		"token_id": tokenID, "backfill_from": want,
	})
	rec := doJSON(t, e, http.MethodPost, "/api/v1/connections", cookie, string(body))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var resp connections.CreateConnectionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Connection.BackfillFrom.Equal(want) {
		t.Errorf("backfill_from = %v, want %v", resp.Connection.BackfillFrom, want)
	}
}

func TestPost_BadKind_400(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e := newConnectionsEcho(t, q, defaultCfg())
	cookie := seedAndLogin(t, e, q)
	tokenID := seedToken(t, q, tenantIDForLogin(t, q), "main")

	body := fmt.Sprintf(`{"kind":"team","owner":"o","name":"r","token_id":%d}`, tokenID)
	rec := doJSON(t, e, http.MethodPost, "/api/v1/connections", cookie, body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestPost_RepoWithoutName_400(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e := newConnectionsEcho(t, q, defaultCfg())
	cookie := seedAndLogin(t, e, q)
	tokenID := seedToken(t, q, tenantIDForLogin(t, q), "main")

	body := fmt.Sprintf(`{"kind":"repo","owner":"o","token_id":%d}`, tokenID)
	rec := doJSON(t, e, http.MethodPost, "/api/v1/connections", cookie, body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestPost_OrgWithName_400(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e := newConnectionsEcho(t, q, defaultCfg())
	cookie := seedAndLogin(t, e, q)
	tokenID := seedToken(t, q, tenantIDForLogin(t, q), "main")

	body := fmt.Sprintf(`{"kind":"org","owner":"o","name":"oops","token_id":%d}`, tokenID)
	rec := doJSON(t, e, http.MethodPost, "/api/v1/connections", cookie, body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestPost_EmptyOwner_400(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e := newConnectionsEcho(t, q, defaultCfg())
	cookie := seedAndLogin(t, e, q)
	tokenID := seedToken(t, q, tenantIDForLogin(t, q), "main")

	body := fmt.Sprintf(`{"kind":"repo","owner":"   ","name":"r","token_id":%d}`, tokenID)
	rec := doJSON(t, e, http.MethodPost, "/api/v1/connections", cookie, body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestPost_UnknownToken_400(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e := newConnectionsEcho(t, q, defaultCfg())
	cookie := seedAndLogin(t, e, q)

	body := `{"kind":"repo","owner":"o","name":"r","token_id":99999}`
	rec := doJSON(t, e, http.MethodPost, "/api/v1/connections", cookie, body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestPost_CrossTenantToken_400(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e := newConnectionsEcho(t, q, defaultCfg())
	cookie := seedAndLogin(t, e, q)

	// Seed a separate tenant with its own token; the logged-in user
	// must not be able to use it.
	other, err := q.CreateTenant(context.Background(), "other")
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	otherToken := seedToken(t, q, other.ID, "other-main")

	body := fmt.Sprintf(`{"kind":"repo","owner":"o","name":"r","token_id":%d}`, otherToken)
	rec := doJSON(t, e, http.MethodPost, "/api/v1/connections", cookie, body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (cross-tenant token)", rec.Code)
	}
}

func TestPost_DuplicateRepo_409(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e := newConnectionsEcho(t, q, defaultCfg())
	cookie := seedAndLogin(t, e, q)
	tokenID := seedToken(t, q, tenantIDForLogin(t, q), "main")

	body := fmt.Sprintf(`{"kind":"repo","owner":"o","name":"r","token_id":%d}`, tokenID)
	if rec := doJSON(t, e, http.MethodPost, "/api/v1/connections", cookie, body); rec.Code != http.StatusCreated {
		t.Fatalf("first POST: status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	rec := doJSON(t, e, http.MethodPost, "/api/v1/connections", cookie, body)
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate POST: status = %d, want 409; body=%s", rec.Code, rec.Body.String())
	}
}

func TestPost_DuplicateOrg_409(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e := newConnectionsEcho(t, q, defaultCfg())
	cookie := seedAndLogin(t, e, q)
	tokenID := seedToken(t, q, tenantIDForLogin(t, q), "main")

	body := fmt.Sprintf(`{"kind":"org","owner":"acmeorg","token_id":%d}`, tokenID)
	if rec := doJSON(t, e, http.MethodPost, "/api/v1/connections", cookie, body); rec.Code != http.StatusCreated {
		t.Fatalf("first POST: status = %d, want 201", rec.Code)
	}
	rec := doJSON(t, e, http.MethodPost, "/api/v1/connections", cookie, body)
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate POST: status = %d, want 409; body=%s", rec.Code, rec.Body.String())
	}
}

func TestPost_BadJSON_400(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e := newConnectionsEcho(t, q, defaultCfg())
	cookie := seedAndLogin(t, e, q)

	rec := doJSON(t, e, http.MethodPost, "/api/v1/connections", cookie, `{not-json`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestPost_NoCookie_401(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e := newConnectionsEcho(t, q, defaultCfg())

	body := `{"kind":"repo","owner":"o","name":"r","token_id":1}`
	rec := doJSON(t, e, http.MethodPost, "/api/v1/connections", nil, body)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

// --- DELETE ---

func TestDelete_HappyPath_204(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e := newConnectionsEcho(t, q, defaultCfg())
	cookie := seedAndLogin(t, e, q)
	tokenID := seedToken(t, q, tenantIDForLogin(t, q), "main")

	body := fmt.Sprintf(`{"kind":"repo","owner":"o","name":"r","token_id":%d}`, tokenID)
	rec := doJSON(t, e, http.MethodPost, "/api/v1/connections", cookie, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST: status = %d, want 201", rec.Code)
	}
	var resp connections.CreateConnectionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	delRec := doJSON(t, e, http.MethodDelete,
		fmt.Sprintf("/api/v1/connections/%d", resp.Connection.ID), cookie, "")
	if delRec.Code != http.StatusNoContent {
		t.Fatalf("DELETE: status = %d, want 204; body=%s", delRec.Code, delRec.Body.String())
	}
	// And it's gone from the list.
	listRec := doJSON(t, e, http.MethodGet, "/api/v1/connections", cookie, "")
	var listResp connections.ListConnectionsResponse
	_ = json.Unmarshal(listRec.Body.Bytes(), &listResp)
	if len(listResp.Connections) != 0 {
		t.Errorf("after delete: %d connections, want 0", len(listResp.Connections))
	}
}

func TestDelete_NotFound_404(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e := newConnectionsEcho(t, q, defaultCfg())
	cookie := seedAndLogin(t, e, q)

	rec := doJSON(t, e, http.MethodDelete, "/api/v1/connections/9999", cookie, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestDelete_BadID_400(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e := newConnectionsEcho(t, q, defaultCfg())
	cookie := seedAndLogin(t, e, q)

	rec := doJSON(t, e, http.MethodDelete, "/api/v1/connections/abc", cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestDelete_CrossTenant_404(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e := newConnectionsEcho(t, q, defaultCfg())
	cookie := seedAndLogin(t, e, q)

	// Seed another tenant + its token + a connection owned by it.
	other, err := q.CreateTenant(context.Background(), "other")
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	otherToken := seedToken(t, q, other.ID, "other-main")
	name := "r"
	otherConn, err := q.CreateConnection(context.Background(), sqlitedb.CreateConnectionParams{
		TenantID:     other.ID,
		Kind:         "repo",
		Owner:        "o",
		Name:         &name,
		TokenID:      otherToken,
		BackfillFrom: time.Now().UTC(),
		Status:       "active",
	})
	if err != nil {
		t.Fatalf("CreateConnection (other): %v", err)
	}

	// Tenant A's session attempts to DELETE the other tenant's
	// connection by id.
	rec := doJSON(t, e, http.MethodDelete,
		fmt.Sprintf("/api/v1/connections/%d", otherConn.ID), cookie, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (cross-tenant)", rec.Code)
	}
	// The other tenant's connection still exists.
	if _, err := q.GetConnection(context.Background(), otherConn.ID); err != nil {
		t.Errorf("other tenant's connection vanished: %v", err)
	}
}

func TestDelete_NoCookie_401(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e := newConnectionsEcho(t, q, defaultCfg())

	rec := doJSON(t, e, http.MethodDelete, "/api/v1/connections/1", nil, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
