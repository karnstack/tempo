package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/karnstack/tempo/internal/storage/sqlite/sqlitedb"
)

// CookieName is the cookie that carries the server-issued session id.
const CookieName = "tempo_session"

// 32 random bytes → 256 bits, base64url-encoded to a 43-char cookie value.
const sessionIDBytes = 32

var (
	ErrNoSession      = errors.New("auth: no session cookie")
	ErrSessionUnknown = errors.New("auth: session not found")
	ErrSessionExpired = errors.New("auth: session expired")
)

// SessionStore is the persistence subset Manager needs. *sqlitedb.Queries
// satisfies it natively; a future *pgdb.Queries will too.
type SessionStore interface {
	CreateSession(ctx context.Context, arg sqlitedb.CreateSessionParams) (sqlitedb.Session, error)
	GetSession(ctx context.Context, id string) (sqlitedb.Session, error)
	DeleteSession(ctx context.Context, id string) error
	DeleteExpiredSessions(ctx context.Context, now time.Time) error
}

// Manager issues, validates, and revokes server-side sessions. The cookie
// carries a random id; the server is always the authority on validity.
type Manager struct {
	store    SessionStore
	duration time.Duration
	secure   bool
	now      func() time.Time
}

func NewManager(store SessionStore, duration time.Duration, secure bool) *Manager {
	return &Manager{
		store:    store,
		duration: duration,
		secure:   secure,
		now:      time.Now,
	}
}

// Issue creates a fresh session for userID, persists it, and writes the
// cookie to w. Returns the persisted row so callers can log id/expiry.
func (m *Manager) Issue(ctx context.Context, w http.ResponseWriter, userID int64) (sqlitedb.Session, error) {
	id, err := newSessionID()
	if err != nil {
		return sqlitedb.Session{}, err
	}
	expiresAt := m.now().Add(m.duration)
	sess, err := m.store.CreateSession(ctx, sqlitedb.CreateSessionParams{
		ID:        id,
		UserID:    userID,
		ExpiresAt: expiresAt,
	})
	if err != nil {
		return sqlitedb.Session{}, fmt.Errorf("auth: persist session: %w", err)
	}
	http.SetCookie(w, m.cookie(id, expiresAt))
	return sess, nil
}

// Validate reads the cookie from r and returns the session row, or one of
// the sentinel errors. Expired rows are best-effort deleted on the way out;
// the user is going to be 401'd either way and SweepExpired catches the rest.
func (m *Manager) Validate(ctx context.Context, r *http.Request) (sqlitedb.Session, error) {
	c, err := r.Cookie(CookieName)
	if err != nil || c.Value == "" {
		return sqlitedb.Session{}, ErrNoSession
	}
	sess, err := m.store.GetSession(ctx, c.Value)
	if err != nil {
		return sqlitedb.Session{}, ErrSessionUnknown
	}
	if !sess.ExpiresAt.After(m.now()) {
		_ = m.store.DeleteSession(ctx, sess.ID)
		return sqlitedb.Session{}, ErrSessionExpired
	}
	return sess, nil
}

// Revoke deletes the session row identified by the request's cookie (if
// present) and writes a clearing Set-Cookie header. Idempotent.
func (m *Manager) Revoke(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
	if c, err := r.Cookie(CookieName); err == nil && c.Value != "" {
		if err := m.store.DeleteSession(ctx, c.Value); err != nil {
			return fmt.Errorf("auth: delete session: %w", err)
		}
	}
	http.SetCookie(w, m.clearCookie())
	return nil
}

// SweepExpired deletes every session row whose expires_at is in the past.
func (m *Manager) SweepExpired(ctx context.Context) error {
	return m.store.DeleteExpiredSessions(ctx, m.now())
}

func (m *Manager) cookie(id string, expiresAt time.Time) *http.Cookie {
	return &http.Cookie{
		Name:     CookieName,
		Value:    id,
		Path:     "/",
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: http.SameSiteLaxMode,
		Expires:  expiresAt,
		MaxAge:   int(m.duration.Seconds()),
	}
}

func (m *Manager) clearCookie() *http.Cookie {
	return &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	}
}

func newSessionID() (string, error) {
	b := make([]byte, sessionIDBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("auth: read random: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

type ctxKey int

const sessionCtxKey ctxKey = 1

// IntoContext returns a child context carrying sess.
func IntoContext(ctx context.Context, sess sqlitedb.Session) context.Context {
	return context.WithValue(ctx, sessionCtxKey, sess)
}

// FromContext returns the session attached to ctx, if any.
func FromContext(ctx context.Context) (sqlitedb.Session, bool) {
	s, ok := ctx.Value(sessionCtxKey).(sqlitedb.Session)
	return s, ok
}
