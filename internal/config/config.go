// Package config loads tempo's runtime configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"strings"
)

type Config struct {
	Listen   string
	Env      string
	Database Database
}

// Database holds the parsed TEMPO_DB value. Driver is the database/sql driver
// name (today: "sqlite"; future: "postgres"). DSN is the driver-specific
// connection string — a filesystem path (or ":memory:") for sqlite, the libpq
// URL for postgres. Raw preserves the original env value for logs.
type Database struct {
	Driver string
	DSN    string
	Raw    string
}

func Load() *Config {
	raw := getenv("TEMPO_DB", "sqlite://./data/tempo.db")
	db, err := parseDB(raw)
	if err != nil {
		// Match the existing minimal-config style: panic at boot rather than
		// smuggle a half-built Config to callers. cfgx in 0013 turns this into
		// a typed error path.
		panic(err)
	}
	return &Config{
		Listen:   getenv("TEMPO_LISTEN", ":8080"),
		Env:      getenv("TEMPO_ENV", "development"),
		Database: db,
	}
}

func IsDev() bool {
	return getenv("TEMPO_ENV", "development") == "development"
}

func parseDB(raw string) (Database, error) {
	switch {
	case strings.HasPrefix(raw, "sqlite://"):
		return Database{Driver: "sqlite", DSN: strings.TrimPrefix(raw, "sqlite://"), Raw: raw}, nil
	case strings.HasPrefix(raw, "postgres://"), strings.HasPrefix(raw, "postgresql://"):
		return Database{Driver: "postgres", DSN: raw, Raw: raw}, nil
	default:
		return Database{}, fmt.Errorf("config: unsupported TEMPO_DB scheme %q", raw)
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
