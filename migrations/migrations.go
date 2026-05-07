// Package migrations embeds the goose SQL migration files so they can be
// applied without depending on the working directory at runtime. The same
// embed.FS is shared by `cmd/migrate` and the storage tests.
package migrations

import (
	"context"
	"database/sql"
	"embed"
	"fmt"

	"github.com/pressly/goose/v3"
)

//go:embed *.sql
var FS embed.FS

// Apply runs all up migrations against db using the embedded FS. The dialect
// is pinned to sqlite3 (goose's name for SQLite).
func Apply(ctx context.Context, db *sql.DB) error {
	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("migrations.Apply: dialect: %w", err)
	}
	goose.SetBaseFS(FS)
	defer goose.SetBaseFS(nil)
	if err := goose.UpContext(ctx, db, "."); err != nil {
		return fmt.Errorf("migrations.Apply: up: %w", err)
	}
	return nil
}
