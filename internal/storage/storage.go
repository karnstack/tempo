// Package storage hosts the persistence-layer seam. SQLite is the v1 backend;
// Postgres lives in internal/storage/postgres as a future stub.
package storage

import (
	"context"
	"database/sql"
)

// Storage is the seam between business code and a concrete database. Repository
// methods (Users, Connections, etc.) will hang off this interface as 0012 lands
// sqlc-generated query packages.
type Storage interface {
	// DB returns the underlying *sql.DB. Used by migrations and ad-hoc queries.
	DB() *sql.DB
	// Ping verifies the connection is alive.
	Ping(ctx context.Context) error
	// Close releases the connection pool.
	Close() error
}
