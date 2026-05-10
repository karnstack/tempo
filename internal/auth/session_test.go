package auth_test

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/karnstack/tempo/internal/auth"
	"github.com/karnstack/tempo/internal/config"
	"github.com/karnstack/tempo/internal/storage/sqlite"
	"github.com/karnstack/tempo/internal/storage/sqlite/sqlitedb"
	"github.com/karnstack/tempo/migrations"
	"go.uber.org/fx/fxtest"
	"go.uber.org/zap/zaptest"
)

// fakeStore satisfies auth.SessionStore for unit tests. It records
// DeleteSession calls so we can assert opportunistic cleanup.
type fakeStore struct {
	rows    map[string]sqlitedb.Session
	deleted []string
}

func newFakeStore() *fakeStore {
	return &fakeStore{rows: map[string]sqlitedb.Session{}}
}

func (f *fakeStore) CreateSession(_ context.Context, arg sqlitedb.CreateSessionParams) (sqlitedb.Session, error) {
	s := sqlitedb.Session{
		ID:        arg.ID,
		UserID:    arg.UserID,
		ExpiresAt: arg.ExpiresAt,
		CreatedAt: time.Now(),
	}
	f.rows[arg.ID] = s
	return s, nil
}

func (f *fakeStore) GetSession(_ context.Context, id string) (sqlitedb.Session, error) {
	s, ok := f.rows[id]
	if !ok {
		return sqlitedb.Session{}, sql.ErrNoRows
	}
	return s, nil
}

func (f *fakeStore) DeleteSession(_ context.Context, id string) error {
	f.deleted = append(f.deleted, id)
	delete(f.rows, id)
	return nil
}

func (f *fakeStore) DeleteExpiredSessions(_ context.Context, now time.Time) error {
	for id, s := range f.rows {
		if !s.ExpiresAt.After(now) {
			delete(f.rows, id)
		}
	}
	return nil
}

func extractCookie(t *testing.T, rec *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no cookie named %q in response: %+v", name, rec.Result().Cookies())
	return nil
}

func TestIssue_PersistsRowAndSetsCookie(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	m := auth.NewManager(store, time.Hour, false)

	rec := httptest.NewRecorder()
	before := time.Now()
	sess, err := m.Issue(context.Background(), rec, 42)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if sess.UserID != 42 {
		t.Fatalf("UserID = %d, want 42", sess.UserID)
	}
	// expires_at is roughly now + 1h.
	wantMin := before.Add(time.Hour - time.Second)
	wantMax := time.Now().Add(time.Hour + time.Second)
	if sess.ExpiresAt.Before(wantMin) || sess.ExpiresAt.After(wantMax) {
		t.Fatalf("ExpiresAt = %v, want in [%v, %v]", sess.ExpiresAt, wantMin, wantMax)
	}
	if _, ok := store.rows[sess.ID]; !ok {
		t.Fatalf("Issue did not persist row id=%q", sess.ID)
	}

	c := extractCookie(t, rec, auth.CookieName)
	if c.Value != sess.ID {
		t.Errorf("cookie value = %q, want %q", c.Value, sess.ID)
	}
	if !c.HttpOnly {
		t.Error("cookie should be HttpOnly")
	}
	if c.Secure {
		t.Error("cookie should not be Secure when manager secure=false")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax", c.SameSite)
	}
	if c.Path != "/" {
		t.Errorf("Path = %q, want /", c.Path)
	}
	if c.MaxAge != int(time.Hour.Seconds()) {
		t.Errorf("MaxAge = %d, want %d", c.MaxAge, int(time.Hour.Seconds()))
	}
}

func TestIssue_SecureFlagFollowsConstructor(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	m := auth.NewManager(store, time.Hour, true)

	rec := httptest.NewRecorder()
	if _, err := m.Issue(context.Background(), rec, 1); err != nil {
		t.Fatalf("Issue: %v", err)
	}
	c := extractCookie(t, rec, auth.CookieName)
	if !c.Secure {
		t.Error("cookie should be Secure when manager secure=true")
	}
}

func TestIssue_ProducesUniqueIDs(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	m := auth.NewManager(store, time.Hour, false)

	rec1 := httptest.NewRecorder()
	rec2 := httptest.NewRecorder()
	a, err := m.Issue(context.Background(), rec1, 1)
	if err != nil {
		t.Fatalf("Issue 1: %v", err)
	}
	b, err := m.Issue(context.Background(), rec2, 1)
	if err != nil {
		t.Fatalf("Issue 2: %v", err)
	}
	if a.ID == b.ID {
		t.Fatalf("two consecutive Issues produced the same id: %q", a.ID)
	}
}

func TestValidate_HappyPath(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	m := auth.NewManager(store, time.Hour, false)

	rec := httptest.NewRecorder()
	issued, err := m.Issue(context.Background(), rec, 7)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(extractCookie(t, rec, auth.CookieName))
	got, err := m.Validate(context.Background(), req)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if got.ID != issued.ID || got.UserID != issued.UserID {
		t.Fatalf("Validate returned %+v, want %+v", got, issued)
	}
}

func TestValidate_NoCookieReturnsErrNoSession(t *testing.T) {
	t.Parallel()
	m := auth.NewManager(newFakeStore(), time.Hour, false)
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	if _, err := m.Validate(context.Background(), req); !errors.Is(err, auth.ErrNoSession) {
		t.Fatalf("err = %v, want ErrNoSession", err)
	}
}

func TestValidate_UnknownIDReturnsErrSessionUnknown(t *testing.T) {
	t.Parallel()
	m := auth.NewManager(newFakeStore(), time.Hour, false)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "nope"})

	if _, err := m.Validate(context.Background(), req); !errors.Is(err, auth.ErrSessionUnknown) {
		t.Fatalf("err = %v, want ErrSessionUnknown", err)
	}
}

func TestValidate_ExpiredRowReturnsErrSessionExpiredAndDeletes(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	// Seed an already-expired row directly.
	expiredID := "expired-id"
	store.rows[expiredID] = sqlitedb.Session{
		ID:        expiredID,
		UserID:    9,
		ExpiresAt: time.Now().Add(-time.Minute),
		CreatedAt: time.Now().Add(-time.Hour),
	}
	m := auth.NewManager(store, time.Hour, false)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: expiredID})
	if _, err := m.Validate(context.Background(), req); !errors.Is(err, auth.ErrSessionExpired) {
		t.Fatalf("err = %v, want ErrSessionExpired", err)
	}
	if len(store.deleted) != 1 || store.deleted[0] != expiredID {
		t.Fatalf("deleted = %v, want [%q]", store.deleted, expiredID)
	}
	if _, ok := store.rows[expiredID]; ok {
		t.Errorf("expired row still present after Validate")
	}
}

func TestRevoke_DeletesRowAndClearsCookie(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	m := auth.NewManager(store, time.Hour, false)

	rec := httptest.NewRecorder()
	issued, err := m.Issue(context.Background(), rec, 5)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/logout", nil)
	req.AddCookie(extractCookie(t, rec, auth.CookieName))
	revRec := httptest.NewRecorder()
	if err := m.Revoke(context.Background(), revRec, req); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if _, ok := store.rows[issued.ID]; ok {
		t.Errorf("Revoke did not delete the row")
	}
	c := extractCookie(t, revRec, auth.CookieName)
	if c.MaxAge != -1 {
		t.Errorf("clearing cookie MaxAge = %d, want -1", c.MaxAge)
	}
	if c.Value != "" {
		t.Errorf("clearing cookie value = %q, want \"\"", c.Value)
	}
}

func TestRevoke_NoCookieStillWritesClearingCookie(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	m := auth.NewManager(store, time.Hour, false)

	req := httptest.NewRequest(http.MethodPost, "/logout", nil)
	rec := httptest.NewRecorder()
	if err := m.Revoke(context.Background(), rec, req); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	c := extractCookie(t, rec, auth.CookieName)
	if c.MaxAge != -1 {
		t.Errorf("clearing cookie MaxAge = %d, want -1", c.MaxAge)
	}
}

func TestSweepExpired_CallsThroughToStore(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	// Two rows: one expired, one fresh.
	store.rows["dead"] = sqlitedb.Session{ID: "dead", UserID: 1, ExpiresAt: time.Now().Add(-time.Hour)}
	store.rows["live"] = sqlitedb.Session{ID: "live", UserID: 1, ExpiresAt: time.Now().Add(time.Hour)}

	m := auth.NewManager(store, time.Hour, false)
	if err := m.SweepExpired(context.Background()); err != nil {
		t.Fatalf("SweepExpired: %v", err)
	}
	if _, ok := store.rows["dead"]; ok {
		t.Errorf("expired row not swept")
	}
	if _, ok := store.rows["live"]; !ok {
		t.Errorf("fresh row got swept")
	}
}

func TestContextRoundtrip(t *testing.T) {
	t.Parallel()
	sess := sqlitedb.Session{ID: "abc", UserID: 11}
	ctx := auth.IntoContext(context.Background(), sess)
	got, ok := auth.FromContext(ctx)
	if !ok {
		t.Fatal("FromContext ok = false")
	}
	if got.ID != "abc" || got.UserID != 11 {
		t.Fatalf("FromContext = %+v, want %+v", got, sess)
	}

	if _, ok := auth.FromContext(context.Background()); ok {
		t.Errorf("FromContext on bare context returned ok=true")
	}
}

// --- integration: real sqlite + real *sqlitedb.Queries ---

func newIntegrationStore(t *testing.T) *sqlitedb.Queries {
	t.Helper()
	lc := fxtest.NewLifecycle(t)
	l := zaptest.NewLogger(t)
	path := filepath.Join(t.TempDir(), "session_integration.db")
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

func TestManager_AgainstRealQueries(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	ctx := context.Background()

	tenant, err := q.CreateTenant(ctx, "acme")
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := q.CreateUser(ctx, sqlitedb.CreateUserParams{
		TenantID: tenant.ID, Email: "admin@acme.test", PasswordHash: "x", Role: "admin",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	m := auth.NewManager(q, time.Hour, false)

	// Issue.
	issueRec := httptest.NewRecorder()
	sess, err := m.Issue(ctx, issueRec, user.ID)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if sess.UserID != user.ID {
		t.Fatalf("Session.UserID = %d, want %d", sess.UserID, user.ID)
	}

	// Validate (round-trip the cookie).
	cookie := extractCookie(t, issueRec, auth.CookieName)
	validateReq := httptest.NewRequest(http.MethodGet, "/", nil)
	validateReq.AddCookie(cookie)
	got, err := m.Validate(ctx, validateReq)
	if err != nil {
		t.Fatalf("Validate after Issue: %v", err)
	}
	if got.ID != sess.ID {
		t.Fatalf("Validate id = %q, want %q", got.ID, sess.ID)
	}

	// Revoke.
	revRec := httptest.NewRecorder()
	revReq := httptest.NewRequest(http.MethodPost, "/logout", nil)
	revReq.AddCookie(cookie)
	if err := m.Revoke(ctx, revRec, revReq); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	// Validate again — row is gone.
	postReq := httptest.NewRequest(http.MethodGet, "/", nil)
	postReq.AddCookie(cookie)
	if _, err := m.Validate(ctx, postReq); !errors.Is(err, auth.ErrSessionUnknown) {
		t.Fatalf("err after Revoke = %v, want ErrSessionUnknown", err)
	}
}
