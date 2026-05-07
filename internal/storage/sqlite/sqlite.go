// Package sqlite is the SQLite implementation of storage.Storage. It uses the
// pure-Go modernc.org/sqlite driver so the binary stays CGo-free.
package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/karnstack/tempo/internal/config"
	"github.com/karnstack/tempo/internal/storage"
	"go.uber.org/fx"
	"go.uber.org/zap"
	_ "modernc.org/sqlite" // register "sqlite" driver
)

// pragmas applied at open time, in order. WAL gives concurrent readers; NORMAL
// sync is the recommended pairing with WAL; busy_timeout avoids "database is
// locked" under contention; temp_store=MEMORY moves temp tables off disk.
var pragmas = []string{
	"journal_mode=WAL",
	"synchronous=NORMAL",
	"foreign_keys=ON",
	"busy_timeout=5000",
	"temp_store=MEMORY",
}

// Store implements storage.Storage.
type Store struct {
	db *sql.DB
}

func (s *Store) DB() *sql.DB                    { return s.db }
func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }
func (s *Store) Close() error                   { return s.db.Close() }

// New is the fx provider. It opens the database, applies PRAGMAs, pings it,
// and registers an OnStop hook that closes the pool.
func New(lc fx.Lifecycle, l *zap.Logger, cfg *config.Config) (storage.Storage, error) {
	if cfg.Database.Driver != "sqlite" {
		return nil, fmt.Errorf("sqlite.New: expected driver=sqlite, got %q", cfg.Database.Driver)
	}
	if err := ensureParentDir(cfg.Database.DSN); err != nil {
		return nil, err
	}

	dsn := buildDSN(cfg.Database.DSN)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("sqlite.New: open: %w", err)
	}
	// WAL allows concurrent readers but only one writer at a time. Capping the
	// pool keeps writers serialised through database/sql instead of through
	// SQLITE_BUSY retries.
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)
	db.SetConnMaxIdleTime(5 * time.Minute)

	// Verify PRAGMAs took effect. _pragma= query params are silently ignored
	// on unknown keys; failing fast at boot is friendlier than mysterious bugs.
	if err := verifyPragmas(db); err != nil {
		_ = db.Close()
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sqlite.New: ping: %w", err)
	}

	l.Info("sqlite db ready",
		zap.String("path", cfg.Database.DSN),
		zap.String("raw", cfg.Database.Raw),
	)

	s := &Store{db: db}
	lc.Append(fx.Hook{
		OnStop: func(_ context.Context) error {
			l.Info("closing sqlite db")
			return s.Close()
		},
	})
	return s, nil
}

// buildDSN appends pragma query params so the driver applies them on every
// connection in the pool (per-connection state).
func buildDSN(path string) string {
	if path == ":memory:" || strings.HasPrefix(path, "file::memory:") {
		// In-memory test DBs need a shared cache so all connections in the
		// pool see the same database.
		return fmt.Sprintf("file::memory:?cache=shared&%s", pragmaQuery())
	}
	return fmt.Sprintf("file:%s?%s", path, pragmaQuery())
}

func pragmaQuery() string {
	parts := make([]string, 0, len(pragmas))
	for _, p := range pragmas {
		parts = append(parts, "_pragma="+p)
	}
	return strings.Join(parts, "&")
}

func verifyPragmas(db *sql.DB) error {
	checks := map[string]string{
		"foreign_keys": "1",
	}
	for k, want := range checks {
		var got string
		if err := db.QueryRow("PRAGMA " + k).Scan(&got); err != nil {
			return fmt.Errorf("sqlite.New: read PRAGMA %s: %w", k, err)
		}
		if !strings.EqualFold(got, want) {
			return fmt.Errorf("sqlite.New: PRAGMA %s = %q, want %q", k, got, want)
		}
	}
	return nil
}

func ensureParentDir(path string) error {
	if path == ":memory:" || strings.HasPrefix(path, "file::memory:") {
		return nil
	}
	dir := filepath.Dir(path)
	if dir == "" || dir == "." {
		return nil
	}
	return os.MkdirAll(dir, 0o755)
}
