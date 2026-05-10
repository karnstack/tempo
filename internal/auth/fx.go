package auth

import (
	"github.com/karnstack/tempo/internal/config"
	"github.com/karnstack/tempo/internal/storage/sqlite/sqlitedb"
)

// NewManagerFx is the fx adapter that constructs a *Manager from the
// already-provided *config.Config and *sqlitedb.Queries. Cookie Secure
// flag tracks production-vs-dev so dev over plain http "just works".
func NewManagerFx(cfg *config.Config, q *sqlitedb.Queries) *Manager {
	return NewManager(q, cfg.Session.Duration, cfg.Env == "production")
}

// NewRegistrarFx is the fx adapter for *Registrar.
func NewRegistrarFx(q *sqlitedb.Queries) *Registrar {
	return NewRegistrar(q)
}

// NewAuthenticatorFx is the fx adapter for *Authenticator.
func NewAuthenticatorFx(q *sqlitedb.Queries) *Authenticator {
	return NewAuthenticator(q)
}
