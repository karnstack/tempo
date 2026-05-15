package ingest_test

import (
	"context"
	"testing"

	"github.com/karnstack/tempo/internal/ingest"
	"github.com/karnstack/tempo/internal/storage/sqlite/sqlitedb"
)

// Compile-time check: NoopRunner implements Runner.
var _ ingest.Runner = ingest.NoopRunner{}

func TestNoopRunner_Name(t *testing.T) {
	t.Parallel()
	if got := (ingest.NoopRunner{}).Name(); got != "noop" {
		t.Errorf("Name() = %q, want %q", got, "noop")
	}
}

func TestNoopRunner_Run_ReturnsZeroOutcome(t *testing.T) {
	t.Parallel()
	out, err := (ingest.NoopRunner{}).Run(context.Background(), sqlitedb.Connection{}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Items != 0 {
		t.Errorf("Items = %d, want 0", out.Items)
	}
	if out.RateLimitRemaining != nil {
		t.Errorf("RateLimitRemaining = %v, want nil", *out.RateLimitRemaining)
	}
}
