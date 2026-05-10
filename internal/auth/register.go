package auth

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/karnstack/tempo/internal/storage/sqlite/sqlitedb"
)

const (
	DefaultTenantName = "default"
	AdminRole         = "admin"
	MinPasswordLen    = 8
)

var (
	ErrNotFirstRun      = errors.New("auth: registration is closed")
	ErrInvalidEmail     = errors.New("auth: invalid email")
	ErrPasswordTooShort = errors.New("auth: password too short")
)

// emailRe is deliberately loose: non-empty local part, "@", non-empty
// domain with a dot. Strict RFC-5321 regexes reject real addresses in
// practice; the users unique index catches duplicates.
var emailRe = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

// UserStore is the persistence subset Registrar needs. *sqlitedb.Queries
// satisfies it natively; a future *pgdb.Queries will too.
type UserStore interface {
	CountTenants(ctx context.Context) (int64, error)
	ListTenants(ctx context.Context) ([]sqlitedb.Tenant, error)
	CreateTenant(ctx context.Context, name string) (sqlitedb.Tenant, error)
	CountUsersByTenant(ctx context.Context, tenantID int64) (int64, error)
	CreateUser(ctx context.Context, arg sqlitedb.CreateUserParams) (sqlitedb.User, error)
}

// Registrar handles first-run admin registration. After a user exists,
// Register returns ErrNotFirstRun; the route stays mounted so the SPA
// can probe state with /firstrun, but it cannot create a second admin.
type Registrar struct{ store UserStore }

func NewRegistrar(store UserStore) *Registrar { return &Registrar{store: store} }

// IsFirstRun reports whether zero users exist across all tenants. Walks
// tenants because in v1 a tenant is bootstrapped lazily on first
// register — there's no "default tenant id = 1" guarantee until then.
func (r *Registrar) IsFirstRun(ctx context.Context) (bool, error) {
	n, err := r.store.CountTenants(ctx)
	if err != nil {
		return false, fmt.Errorf("auth: count tenants: %w", err)
	}
	if n == 0 {
		return true, nil
	}
	tenants, err := r.store.ListTenants(ctx)
	if err != nil {
		return false, fmt.Errorf("auth: list tenants: %w", err)
	}
	for _, t := range tenants {
		c, err := r.store.CountUsersByTenant(ctx, t.ID)
		if err != nil {
			return false, fmt.Errorf("auth: count users: %w", err)
		}
		if c > 0 {
			return false, nil
		}
	}
	return true, nil
}

// Register creates the admin user during first run. Email is lowercased
// and trimmed before validation. Returns one of the sentinel errors on
// invalid input or when a user already exists; otherwise the persisted
// user row.
func (r *Registrar) Register(ctx context.Context, email, password string) (sqlitedb.User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if !emailRe.MatchString(email) {
		return sqlitedb.User{}, ErrInvalidEmail
	}
	if len(password) < MinPasswordLen {
		return sqlitedb.User{}, ErrPasswordTooShort
	}

	firstRun, err := r.IsFirstRun(ctx)
	if err != nil {
		return sqlitedb.User{}, err
	}
	if !firstRun {
		return sqlitedb.User{}, ErrNotFirstRun
	}

	hash, err := Hash(password)
	if err != nil {
		return sqlitedb.User{}, fmt.Errorf("auth: hash password: %w", err)
	}

	tenantID, err := r.ensureTenant(ctx)
	if err != nil {
		return sqlitedb.User{}, err
	}

	user, err := r.store.CreateUser(ctx, sqlitedb.CreateUserParams{
		TenantID:     tenantID,
		Email:        email,
		PasswordHash: hash,
		Role:         AdminRole,
	})
	if err != nil {
		return sqlitedb.User{}, fmt.Errorf("auth: create user: %w", err)
	}
	return user, nil
}

func (r *Registrar) ensureTenant(ctx context.Context) (int64, error) {
	tenants, err := r.store.ListTenants(ctx)
	if err != nil {
		return 0, fmt.Errorf("auth: list tenants: %w", err)
	}
	if len(tenants) > 0 {
		return tenants[0].ID, nil
	}
	t, err := r.store.CreateTenant(ctx, DefaultTenantName)
	if err != nil {
		return 0, fmt.Errorf("auth: create tenant: %w", err)
	}
	return t.ID, nil
}
