package auth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/karnstack/tempo/internal/auth"
)

func TestAuthenticate_HappyPath(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	ctx := context.Background()

	registered, err := auth.NewRegistrar(q).Register(ctx, "admin@acme.test", "hunter22hunter22")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	got, err := auth.NewAuthenticator(q).Authenticate(ctx, "admin@acme.test", "hunter22hunter22")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if got.ID != registered.ID {
		t.Errorf("ID = %d, want %d", got.ID, registered.ID)
	}
	if got.Email != "admin@acme.test" {
		t.Errorf("Email = %q, want admin@acme.test", got.Email)
	}
	if got.Role != auth.AdminRole {
		t.Errorf("Role = %q, want %q", got.Role, auth.AdminRole)
	}
}

func TestAuthenticate_WrongPassword_ErrInvalidCredentials(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	ctx := context.Background()

	if _, err := auth.NewRegistrar(q).Register(ctx, "admin@acme.test", "hunter22hunter22"); err != nil {
		t.Fatalf("Register: %v", err)
	}

	_, err := auth.NewAuthenticator(q).Authenticate(ctx, "admin@acme.test", "wrongpassword12")
	if !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("Authenticate err = %v, want ErrInvalidCredentials", err)
	}
}

func TestAuthenticate_UnknownEmail_ErrInvalidCredentials(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	ctx := context.Background()

	// Fresh DB: no tenants, no users.
	_, err := auth.NewAuthenticator(q).Authenticate(ctx, "nobody@acme.test", "irrelevant1234")
	if !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("Authenticate err = %v, want ErrInvalidCredentials", err)
	}
}

func TestAuthenticate_UnknownEmail_RunsFakeVerify(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	ctx := context.Background()

	// Fresh DB → unknown-email branch. The fake-Verify call must run, so
	// the wall time needs to be at least one Argon2id pass. DefaultParams
	// (64 MiB, 3 iterations) takes tens of ms even on fast machines; pick
	// a generous floor that won't flake on slow CI.
	start := time.Now()
	_, err := auth.NewAuthenticator(q).Authenticate(ctx, "nobody@acme.test", "irrelevant1234")
	elapsed := time.Since(start)
	if !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("Authenticate err = %v, want ErrInvalidCredentials", err)
	}
	if elapsed < 5*time.Millisecond {
		t.Errorf("elapsed = %v, want ≥ 5ms (fake Verify should have run)", elapsed)
	}
}

func TestAuthenticate_MalformedEmail_ErrInvalidCredentials(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	ctx := context.Background()

	_, err := auth.NewAuthenticator(q).Authenticate(ctx, "not-an-email", "irrelevant1234")
	if !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("Authenticate err = %v, want ErrInvalidCredentials", err)
	}
}

func TestAuthenticate_EmptyPassword_ErrInvalidCredentials(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	ctx := context.Background()

	if _, err := auth.NewRegistrar(q).Register(ctx, "admin@acme.test", "hunter22hunter22"); err != nil {
		t.Fatalf("Register: %v", err)
	}

	_, err := auth.NewAuthenticator(q).Authenticate(ctx, "admin@acme.test", "")
	if !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("Authenticate err = %v, want ErrInvalidCredentials", err)
	}
}

func TestAuthenticate_CaseInsensitiveEmail(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	ctx := context.Background()

	registered, err := auth.NewRegistrar(q).Register(ctx, "admin@acme.test", "hunter22hunter22")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	got, err := auth.NewAuthenticator(q).Authenticate(ctx, "  Admin@Acme.TEST  ", "hunter22hunter22")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if got.ID != registered.ID {
		t.Errorf("ID = %d, want %d", got.ID, registered.ID)
	}
}
