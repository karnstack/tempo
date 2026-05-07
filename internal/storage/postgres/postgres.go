// Package postgres is a placeholder for the v1.x Postgres backend. The seam
// exists today so call sites can be written against storage.Storage; the real
// implementation arrives when Postgres parity is in scope.
package postgres

import (
	"errors"

	"github.com/karnstack/tempo/internal/storage"
)

// Open is a stub. Returns an error in v1.
func Open() (storage.Storage, error) {
	return nil, errors.New("postgres backend not implemented in v1")
}
