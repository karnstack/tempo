package sqlite_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/karnstack/tempo/internal/config"
	"github.com/karnstack/tempo/internal/storage/sqlite"
	"go.uber.org/fx/fxtest"
	"go.uber.org/zap/zaptest"
)

func TestNew_InMemory(t *testing.T) {
	t.Parallel()
	lc := fxtest.NewLifecycle(t)
	l := zaptest.NewLogger(t)
	cfg := &config.Config{Database: config.Database{
		Driver: "sqlite", DSN: ":memory:", Raw: "sqlite://:memory:",
	}}

	s, err := sqlite.New(lc, l, cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := s.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}

	var fk int
	if err := s.DB().QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil {
		t.Fatalf("read PRAGMA foreign_keys: %v", err)
	}
	if fk != 1 {
		t.Fatalf("foreign_keys = %d, want 1", fk)
	}

	lc.RequireStart().RequireStop()
}

func TestNew_TempFile_WAL(t *testing.T) {
	t.Parallel()
	lc := fxtest.NewLifecycle(t)
	l := zaptest.NewLogger(t)
	path := filepath.Join(t.TempDir(), "tempo.db")
	cfg := &config.Config{Database: config.Database{
		Driver: "sqlite", DSN: path, Raw: "sqlite://" + path,
	}}

	s, err := sqlite.New(lc, l, cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var mode string
	if err := s.DB().QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("read PRAGMA journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Fatalf("journal_mode = %q, want wal", mode)
	}

	var bt int
	if err := s.DB().QueryRow("PRAGMA busy_timeout").Scan(&bt); err != nil {
		t.Fatalf("read PRAGMA busy_timeout: %v", err)
	}
	if bt != 5000 {
		t.Fatalf("busy_timeout = %d, want 5000", bt)
	}

	lc.RequireStart().RequireStop()
}

func TestNew_RejectsWrongDriver(t *testing.T) {
	t.Parallel()
	lc := fxtest.NewLifecycle(t)
	l := zaptest.NewLogger(t)
	cfg := &config.Config{Database: config.Database{
		Driver: "postgres", DSN: "postgres://x", Raw: "postgres://x",
	}}
	if _, err := sqlite.New(lc, l, cfg); err == nil {
		t.Fatal("expected error for non-sqlite driver")
	}
}
