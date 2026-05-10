package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/karnstack/tempo/internal/storage/sqlite/sqlitedb"
)

// ErrInvalidCredentials is returned for any login failure visible to a
// probing client: wrong password, unknown email, malformed email, empty
// password. Collapsing all four prevents account enumeration.
var ErrInvalidCredentials = errors.New("auth: invalid credentials")

// LoginUserStore is the persistence subset Authenticator needs.
// *sqlitedb.Queries satisfies it natively; a future *pgdb.Queries will too.
type LoginUserStore interface {
	ListTenants(ctx context.Context) ([]sqlitedb.Tenant, error)
	GetUserByEmail(ctx context.Context, arg sqlitedb.GetUserByEmailParams) (sqlitedb.User, error)
}

// Authenticator validates email + password against the stored user row.
// Lookup walks tenants because users are unique per (tenant_id, email);
// v1 has at most one tenant so this is one query in the happy path.
type Authenticator struct {
	store    LoginUserStore
	fakeHash string
}

// NewAuthenticator builds an Authenticator and precomputes a fake hash
// once. Callers that need a custom fake hash (tests) can use
// NewAuthenticatorWithFakeHash.
func NewAuthenticator(store LoginUserStore) *Authenticator {
	return &Authenticator{store: store, fakeHash: defaultFakeHash}
}

// defaultFakeHash burns one Verify worth of CPU on the unknown-user path
// so the success and failure branches take comparable wall time.
// Computed at package init.
var defaultFakeHash = mustFakeHash()

func mustFakeHash() string {
	h, err := Hash("placeholder-for-timing-equivalence")
	if err != nil {
		panic(fmt.Errorf("auth: precompute fake hash: %w", err))
	}
	return h
}

// Authenticate returns the matching user when email + password verify,
// or ErrInvalidCredentials for any failure a client could probe with.
// Storage errors bubble up as wrapped errors.
func (a *Authenticator) Authenticate(ctx context.Context, email, password string) (sqlitedb.User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if !emailRe.MatchString(email) || password == "" {
		_, _ = Verify(password, a.fakeHash)
		return sqlitedb.User{}, ErrInvalidCredentials
	}

	tenants, err := a.store.ListTenants(ctx)
	if err != nil {
		return sqlitedb.User{}, fmt.Errorf("auth: list tenants: %w", err)
	}

	var (
		user  sqlitedb.User
		found bool
	)
	for _, t := range tenants {
		u, err := a.store.GetUserByEmail(ctx, sqlitedb.GetUserByEmailParams{
			TenantID: t.ID,
			Email:    email,
		})
		if err == nil {
			user, found = u, true
			break
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return sqlitedb.User{}, fmt.Errorf("auth: get user: %w", err)
		}
	}

	if !found {
		_, _ = Verify(password, a.fakeHash)
		return sqlitedb.User{}, ErrInvalidCredentials
	}

	ok, err := Verify(password, user.PasswordHash)
	if err != nil {
		return sqlitedb.User{}, fmt.Errorf("auth: verify: %w", err)
	}
	if !ok {
		return sqlitedb.User{}, ErrInvalidCredentials
	}
	return user, nil
}
