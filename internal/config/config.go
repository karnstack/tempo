// Package config loads tempo's runtime configuration from TEMPO_* environment
// variables. Load() parses every variable, applies defaults, validates the
// resulting struct, and returns aggregated errors so a misconfigured deploy
// surfaces everything wrong in a single boot attempt.
package config

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the parsed, validated runtime config. fx-injectable.
type Config struct {
	Listen   string
	Env      string
	Database Database
	Secret   Secret
	Poll     Poll
	Log      Log
	Rollup   Rollup
	Session  Session

	// SecretWarning, when non-empty, signals that the secret key was derived
	// from a dev-only fallback and the deploy is not production-safe. The
	// logger surfaces it at WARN. Always empty in production (production
	// without TEMPO_SECRET is a Load error, not a warning).
	SecretWarning string
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

// Secret holds the 32-byte symmetric key used for session signing and PAT
// encryption. Loaded from TEMPO_SECRET (base64 of 32 raw bytes). In dev/test
// without TEMPO_SECRET, derived from sha256("tempo-dev") so dev sessions
// survive process restarts; SecretWarning is set in that case.
type Secret struct {
	Key []byte
}

// Poll governs the GitHub ingest worker.
type Poll struct {
	Interval         time.Duration
	BackfillDays     int
	SyncRunRetention int
}

// Log governs zap initialisation.
type Log struct {
	Level  string // debug | info | warn | error
	Format string // json | console
}

// Rollup governs when the daily rollup job fires.
type Rollup struct {
	// Timezone is nil when TEMPO_TZ is unset (use system local). When set,
	// time.Now().In(Timezone) is the basis for the daily date bucket and
	// the fire-time computation.
	Timezone *time.Location
	// Hour is the instance-local hour at which the rollup runs (0–23).
	Hour int
}

// Session governs how long a successful login is good for.
type Session struct {
	Duration time.Duration
}

// Load parses every env var, applies defaults, validates, and returns an
// aggregated error if anything is wrong.
func Load() (*Config, error) {
	cfg := &Config{
		Listen: resolveListen(),
		Env:    getenv("TEMPO_ENV", "development"),
	}

	var errs []error
	push := func(err error) {
		if err != nil {
			errs = append(errs, err)
		}
	}

	if !validEnv(cfg.Env) {
		push(fmt.Errorf("config: TEMPO_ENV=%q must be one of development|production|test", cfg.Env))
	}

	db, err := parseDB(getenv("TEMPO_DB", "sqlite://./data/tempo.db"))
	push(err)
	cfg.Database = db

	secret, warning, err := loadSecret(cfg.Env)
	push(err)
	cfg.Secret = Secret{Key: secret}
	cfg.SecretWarning = warning

	cfg.Poll, err = loadPoll()
	push(err)

	cfg.Log, err = loadLog(cfg.Env)
	push(err)

	cfg.Rollup, err = loadRollup()
	push(err)

	cfg.Session, err = loadSession()
	push(err)

	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return cfg, nil
}

// IsDev reports whether the current TEMPO_ENV is development.
func IsDev() bool { return getenv("TEMPO_ENV", "development") == "development" }

// IsProd reports whether the current TEMPO_ENV is production.
func IsProd() bool { return os.Getenv("TEMPO_ENV") == "production" }

// IsTest reports whether the current TEMPO_ENV is test.
func IsTest() bool { return os.Getenv("TEMPO_ENV") == "test" }

func validEnv(env string) bool {
	switch env {
	case "development", "production", "test":
		return true
	}
	return false
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

// loadSecret returns (key, warning, error). On production with no/invalid
// TEMPO_SECRET, error is non-nil. On dev/test with no TEMPO_SECRET, key is
// derived from a deterministic fallback and warning is set.
func loadSecret(env string) ([]byte, string, error) {
	raw := os.Getenv("TEMPO_SECRET")
	if raw == "" {
		if env == "production" {
			return nil, "", errors.New("config: TEMPO_SECRET is required in production (base64 of 32 random bytes)")
		}
		// Deterministic dev fallback so dev sessions survive restarts.
		sum := sha256.Sum256([]byte("tempo-dev"))
		return sum[:], "TEMPO_SECRET unset — using insecure deterministic dev key. Set TEMPO_SECRET (base64 32 bytes) for any non-throwaway deployment.", nil
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, "", fmt.Errorf("config: TEMPO_SECRET is not valid base64: %w", err)
	}
	if len(key) != 32 {
		return nil, "", fmt.Errorf("config: TEMPO_SECRET decodes to %d bytes, want exactly 32", len(key))
	}
	return key, "", nil
}

func loadPoll() (Poll, error) {
	var errs []error
	intervalStr := getenv("TEMPO_POLL_INTERVAL", "15m")
	interval, err := time.ParseDuration(intervalStr)
	if err != nil {
		errs = append(errs, fmt.Errorf("config: TEMPO_POLL_INTERVAL=%q is not a Go duration: %w", intervalStr, err))
	} else if interval <= 0 {
		errs = append(errs, fmt.Errorf("config: TEMPO_POLL_INTERVAL=%q must be positive", intervalStr))
	}

	daysStr := getenv("TEMPO_BACKFILL_DAYS", "90")
	days, err := strconv.Atoi(daysStr)
	if err != nil {
		errs = append(errs, fmt.Errorf("config: TEMPO_BACKFILL_DAYS=%q is not an integer: %w", daysStr, err))
	} else if days <= 0 {
		errs = append(errs, fmt.Errorf("config: TEMPO_BACKFILL_DAYS=%q must be positive", daysStr))
	}

	retentionStr := getenv("TEMPO_SYNC_RUN_RETENTION", "200")
	retention, err := strconv.Atoi(retentionStr)
	if err != nil {
		errs = append(errs, fmt.Errorf("config: TEMPO_SYNC_RUN_RETENTION=%q is not an integer: %w", retentionStr, err))
	} else if retention < 1 {
		errs = append(errs, fmt.Errorf("config: TEMPO_SYNC_RUN_RETENTION must be >= 1 (got %d)", retention))
	}

	return Poll{Interval: interval, BackfillDays: days, SyncRunRetention: retention}, errors.Join(errs...)
}

func loadLog(env string) (Log, error) {
	var errs []error
	level := strings.ToLower(getenv("TEMPO_LOG_LEVEL", "info"))
	if !validLogLevel(level) {
		errs = append(errs, fmt.Errorf("config: TEMPO_LOG_LEVEL=%q must be one of debug|info|warn|error", level))
	}

	defaultFormat := "json"
	if env == "development" {
		defaultFormat = "console"
	}
	format := strings.ToLower(getenv("TEMPO_LOG_FORMAT", defaultFormat))
	if !validLogFormat(format) {
		errs = append(errs, fmt.Errorf("config: TEMPO_LOG_FORMAT=%q must be one of json|console", format))
	}
	return Log{Level: level, Format: format}, errors.Join(errs...)
}

func validLogLevel(s string) bool {
	switch s {
	case "debug", "info", "warn", "error":
		return true
	}
	return false
}

func validLogFormat(s string) bool { return s == "json" || s == "console" }

func loadRollup() (Rollup, error) {
	var errs []error
	var loc *time.Location
	if tz := os.Getenv("TEMPO_TZ"); tz != "" {
		l, err := time.LoadLocation(tz)
		if err != nil {
			errs = append(errs, fmt.Errorf("config: TEMPO_TZ=%q: %w", tz, err))
		} else {
			loc = l
		}
	}

	hourStr := getenv("TEMPO_ROLLUP_HOUR", "2")
	hour, err := strconv.Atoi(hourStr)
	if err != nil {
		errs = append(errs, fmt.Errorf("config: TEMPO_ROLLUP_HOUR=%q is not an integer: %w", hourStr, err))
	} else if hour < 0 || hour > 23 {
		errs = append(errs, fmt.Errorf("config: TEMPO_ROLLUP_HOUR=%q must be in [0, 23]", hourStr))
	}

	return Rollup{Timezone: loc, Hour: hour}, errors.Join(errs...)
}

func loadSession() (Session, error) {
	durStr := getenv("TEMPO_SESSION_DURATION", "720h")
	dur, err := time.ParseDuration(durStr)
	if err != nil {
		return Session{}, fmt.Errorf("config: TEMPO_SESSION_DURATION=%q is not a Go duration: %w", durStr, err)
	}
	if dur <= 0 {
		return Session{}, fmt.Errorf("config: TEMPO_SESSION_DURATION=%q must be positive", durStr)
	}
	return Session{Duration: dur}, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// resolveListen picks the HTTP listen address in priority order:
//
//  1. TEMPO_LISTEN  — explicit "host:port" or ":port" override.
//  2. PORT          — bare port number, prefixed with ":". This is the
//                     convention dev wrappers like portless.sh use to
//                     inject an assigned port (4000-4999 range), and the
//                     PaaS contract (Heroku, Cloud Run, Fly).
//  3. ":4811"       — local default. Picked to sit inside portless's
//                     assignment range so the experience is consistent
//                     with or without portless, and well clear of the
//                     usual 3000/5173/8080 dev-port pile-up.
func resolveListen() string {
	if v := os.Getenv("TEMPO_LISTEN"); v != "" {
		return v
	}
	if p := os.Getenv("PORT"); p != "" {
		return ":" + p
	}
	return ":4811"
}
