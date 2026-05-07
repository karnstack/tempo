// Command migrate runs goose migrations against the configured TEMPO_DB.
// It links modernc.org/sqlite directly so no CGo is required.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"github.com/karnstack/tempo/internal/config"
	"github.com/karnstack/tempo/internal/logger"
	"github.com/pressly/goose/v3"
	"go.uber.org/zap"
	_ "modernc.org/sqlite"
)

const migrationsDir = "migrations"

func main() {
	l := logger.NewStandalone()
	defer func() { _ = l.Sync() }()

	if len(os.Args) < 2 {
		l.Fatal("usage: migrate <up|down|status|version>")
	}
	cmd := os.Args[1]

	cfg := config.Load()
	if cfg.Database.Driver != "sqlite" {
		l.Fatal("migrate: only sqlite is supported in v1", zap.String("driver", cfg.Database.Driver))
	}

	if dir := filepath.Dir(cfg.Database.DSN); dir != "" && dir != "." && cfg.Database.DSN != ":memory:" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			l.Fatal("migrate: ensure data dir", zap.Error(err))
		}
	}

	db, err := sql.Open("sqlite", cfg.Database.DSN)
	if err != nil {
		l.Fatal("migrate: open db", zap.Error(err))
	}
	defer db.Close()

	goose.SetLogger(zapGooseLogger{l: l})
	if err := goose.SetDialect("sqlite3"); err != nil {
		l.Fatal("migrate: set dialect", zap.Error(err))
	}

	ctx := context.Background()
	switch cmd {
	case "up":
		err = goose.UpContext(ctx, db, migrationsDir)
	case "down":
		err = goose.DownContext(ctx, db, migrationsDir)
	case "status":
		err = goose.StatusContext(ctx, db, migrationsDir)
	case "version":
		err = goose.VersionContext(ctx, db, migrationsDir)
	default:
		l.Fatal("migrate: unknown command", zap.String("cmd", cmd))
	}
	if err != nil {
		l.Fatal("migrate: run", zap.String("cmd", cmd), zap.Error(err))
	}
	fmt.Println("migrate", cmd, "ok")
}

type zapGooseLogger struct{ l *zap.Logger }

func (z zapGooseLogger) Fatalf(format string, v ...any) { z.l.Sugar().Fatalf(format, v...) }
func (z zapGooseLogger) Printf(format string, v ...any) { z.l.Sugar().Infof(format, v...) }
