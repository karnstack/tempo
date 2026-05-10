package auth_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/karnstack/tempo/internal/auth"
)

func TestIsFirstRun_EmptyDB_True(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	r := auth.NewRegistrar(q)

	first, err := r.IsFirstRun(context.Background())
	if err != nil {
		t.Fatalf("IsFirstRun: %v", err)
	}
	if !first {
		t.Fatal("IsFirstRun = false on empty DB, want true")
	}
}

func TestRegister_FirstRun_CreatesTenantAndUser(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	r := auth.NewRegistrar(q)
	ctx := context.Background()

	user, err := r.Register(ctx, "admin@acme.test", "hunter22hunter22")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if user.Email != "admin@acme.test" {
		t.Errorf("Email = %q, want admin@acme.test", user.Email)
	}
	if user.Role != auth.AdminRole {
		t.Errorf("Role = %q, want %q", user.Role, auth.AdminRole)
	}
	if user.PasswordHash == "" {
		t.Error("PasswordHash is empty")
	}
	if user.PasswordHash == "hunter22hunter22" {
		t.Error("PasswordHash is the plaintext password")
	}

	// Password verifies.
	ok, err := auth.Verify("hunter22hunter22", user.PasswordHash)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !ok {
		t.Error("Verify returned false for correct password")
	}

	// Tenant got bootstrapped.
	tenants, err := q.ListTenants(ctx)
	if err != nil {
		t.Fatalf("ListTenants: %v", err)
	}
	if len(tenants) != 1 {
		t.Fatalf("ListTenants len = %d, want 1", len(tenants))
	}
	if tenants[0].Name != auth.DefaultTenantName {
		t.Errorf("Tenant.Name = %q, want %q", tenants[0].Name, auth.DefaultTenantName)
	}
	if user.TenantID != tenants[0].ID {
		t.Errorf("user.TenantID = %d, want %d", user.TenantID, tenants[0].ID)
	}
}

func TestIsFirstRun_AfterRegister_False(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	r := auth.NewRegistrar(q)
	ctx := context.Background()

	if _, err := r.Register(ctx, "admin@acme.test", "hunter22hunter22"); err != nil {
		t.Fatalf("Register: %v", err)
	}

	first, err := r.IsFirstRun(ctx)
	if err != nil {
		t.Fatalf("IsFirstRun: %v", err)
	}
	if first {
		t.Fatal("IsFirstRun = true after a user exists, want false")
	}
}

func TestRegister_SecondCall_ReturnsErrNotFirstRun(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	r := auth.NewRegistrar(q)
	ctx := context.Background()

	if _, err := r.Register(ctx, "admin@acme.test", "hunter22hunter22"); err != nil {
		t.Fatalf("first Register: %v", err)
	}

	_, err := r.Register(ctx, "second@acme.test", "anotherlongpw")
	if !errors.Is(err, auth.ErrNotFirstRun) {
		t.Fatalf("err = %v, want ErrNotFirstRun", err)
	}

	// DB unchanged: still 1 user.
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

func TestRegister_LowercasesAndTrimsEmail(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	r := auth.NewRegistrar(q)

	user, err := r.Register(context.Background(), "  Admin@Acme.Test  ", "hunter22hunter22")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if user.Email != "admin@acme.test" {
		t.Errorf("Email = %q, want admin@acme.test", user.Email)
	}
}

func TestRegister_InvalidEmail(t *testing.T) {
	t.Parallel()
	cases := []string{
		"",
		"   ",
		"no-at-sign",
		"@nolocal.test",
		"trailing@nodot",
		"two@@at.test",
		"has space@dom.test",
	}
	for _, e := range cases {
		t.Run(strings.ReplaceAll(e, " ", "_"), func(t *testing.T) {
			q := newIntegrationStore(t)
			r := auth.NewRegistrar(q)
			_, err := r.Register(context.Background(), e, "hunter22hunter22")
			if !errors.Is(err, auth.ErrInvalidEmail) {
				t.Fatalf("err = %v, want ErrInvalidEmail (input %q)", err, e)
			}
		})
	}
}

func TestRegister_ShortPassword(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	r := auth.NewRegistrar(q)

	_, err := r.Register(context.Background(), "admin@acme.test", "short")
	if !errors.Is(err, auth.ErrPasswordTooShort) {
		t.Fatalf("err = %v, want ErrPasswordTooShort", err)
	}
}

func TestRegister_FailsBeforeAnyDBWrite(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	r := auth.NewRegistrar(q)
	ctx := context.Background()

	if _, err := r.Register(ctx, "bad", "short"); err == nil {
		t.Fatal("Register succeeded with bad input")
	}

	// No tenant should have been created on the validation failure path.
	n, err := q.CountTenants(ctx)
	if err != nil {
		t.Fatalf("CountTenants: %v", err)
	}
	if n != 0 {
		t.Errorf("CountTenants = %d, want 0", n)
	}
}
