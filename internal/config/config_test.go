package config

import (
	"crypto/sha256"
	"encoding/base64"
	"testing"
	"time"
)

// envMap clears the listed keys for the test, then sets the entries from kv.
// t.Setenv handles per-test cleanup.
func envMap(t *testing.T, clear []string, kv map[string]string) {
	t.Helper()
	for _, k := range clear {
		t.Setenv(k, "")
	}
	for k, v := range kv {
		t.Setenv(k, v)
	}
}

var allEnvKeys = []string{
	"TEMPO_LISTEN", "TEMPO_ENV", "TEMPO_DB", "TEMPO_SECRET",
	"TEMPO_POLL_INTERVAL", "TEMPO_BACKFILL_DAYS", "TEMPO_SYNC_RUN_RETENTION",
	"TEMPO_LOG_LEVEL", "TEMPO_LOG_FORMAT",
	"TEMPO_TZ", "TEMPO_ROLLUP_HOUR", "TEMPO_SESSION_DURATION",
	"PORT",
}

func TestParseDB(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		raw       string
		wantDrv   string
		wantDSN   string
		wantError bool
	}{
		{name: "sqlite_filesystem", raw: "sqlite://./data/tempo.db", wantDrv: "sqlite", wantDSN: "./data/tempo.db"},
		{name: "sqlite_memory", raw: "sqlite://:memory:", wantDrv: "sqlite", wantDSN: ":memory:"},
		{name: "postgres_url", raw: "postgres://u:p@h/db", wantDrv: "postgres", wantDSN: "postgres://u:p@h/db"},
		{name: "postgresql_alias", raw: "postgresql://u:p@h/db", wantDrv: "postgres", wantDSN: "postgresql://u:p@h/db"},
		{name: "unsupported", raw: "mysql://x", wantError: true},
		{name: "empty", raw: "", wantError: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseDB(tc.raw)
			if tc.wantError {
				if err == nil {
					t.Fatalf("parseDB(%q): expected error, got %+v", tc.raw, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseDB(%q): unexpected error %v", tc.raw, err)
			}
			if got.Driver != tc.wantDrv || got.DSN != tc.wantDSN || got.Raw != tc.raw {
				t.Errorf("got %+v", got)
			}
		})
	}
}

func TestLoadDefaults(t *testing.T) {
	envMap(t, allEnvKeys, nil)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Listen != ":4811" {
		t.Errorf("Listen = %q, want :4811", cfg.Listen)
	}
	if cfg.Env != "development" {
		t.Errorf("Env = %q, want development", cfg.Env)
	}
	if cfg.Database.Driver != "sqlite" {
		t.Errorf("Database.Driver = %q, want sqlite", cfg.Database.Driver)
	}
	if cfg.Poll.Interval != 15*time.Minute {
		t.Errorf("Poll.Interval = %v, want 15m", cfg.Poll.Interval)
	}
	if cfg.Poll.BackfillDays != 90 {
		t.Errorf("Poll.BackfillDays = %d, want 90", cfg.Poll.BackfillDays)
	}
	if cfg.Poll.SyncRunRetention != 200 {
		t.Errorf("Poll.SyncRunRetention = %d, want 200", cfg.Poll.SyncRunRetention)
	}
	if cfg.Log.Level != "info" {
		t.Errorf("Log.Level = %q, want info", cfg.Log.Level)
	}
	if cfg.Log.Format != "console" {
		t.Errorf("Log.Format = %q, want console (dev default)", cfg.Log.Format)
	}
	if cfg.Rollup.Hour != 2 {
		t.Errorf("Rollup.Hour = %d, want 2", cfg.Rollup.Hour)
	}
	if cfg.Rollup.Timezone != nil {
		t.Errorf("Rollup.Timezone = %v, want nil (system local)", cfg.Rollup.Timezone)
	}
	if cfg.Session.Duration != 720*time.Hour {
		t.Errorf("Session.Duration = %v, want 720h", cfg.Session.Duration)
	}
	if len(cfg.Secret.Key) != 32 {
		t.Errorf("Secret.Key length = %d, want 32", len(cfg.Secret.Key))
	}
	if cfg.SecretWarning == "" {
		t.Error("expected SecretWarning to be non-empty in dev with no TEMPO_SECRET")
	}
	// Dev fallback is deterministic.
	want := sha256.Sum256([]byte("tempo-dev"))
	if string(cfg.Secret.Key) != string(want[:]) {
		t.Error("dev fallback key is not the documented sha256(\"tempo-dev\")")
	}
}

func TestLoadListenHonoursPORTWhenTempoListenUnset(t *testing.T) {
	envMap(t, allEnvKeys, map[string]string{"PORT": "4567"})

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Listen != ":4567" {
		t.Errorf("Listen = %q, want :4567 (from PORT)", cfg.Listen)
	}
}

func TestLoadListenTempoListenOverridesPORT(t *testing.T) {
	envMap(t, allEnvKeys, map[string]string{
		"TEMPO_LISTEN": ":9999",
		"PORT":         "4567",
	})

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Listen != ":9999" {
		t.Errorf("Listen = %q, want :9999 (TEMPO_LISTEN wins over PORT)", cfg.Listen)
	}
}

func TestLoadProductionRequiresSecret(t *testing.T) {
	envMap(t, allEnvKeys, map[string]string{"TEMPO_ENV": "production"})

	_, err := Load()
	if err == nil {
		t.Fatal("Load: expected error for production without TEMPO_SECRET")
	}
}

func TestLoadProductionWithSecret(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	envMap(t, allEnvKeys, map[string]string{
		"TEMPO_ENV":    "production",
		"TEMPO_SECRET": base64.StdEncoding.EncodeToString(key),
	})

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Log.Format != "json" {
		t.Errorf("Log.Format = %q, want json (prod default)", cfg.Log.Format)
	}
	if cfg.SecretWarning != "" {
		t.Errorf("SecretWarning = %q, want empty in production", cfg.SecretWarning)
	}
	if string(cfg.Secret.Key) != string(key) {
		t.Error("Secret.Key did not round-trip from TEMPO_SECRET base64")
	}
}

func TestLoadInvalidSecretBase64(t *testing.T) {
	envMap(t, allEnvKeys, map[string]string{
		"TEMPO_ENV":    "production",
		"TEMPO_SECRET": "not-base64!!!!",
	})
	if _, err := Load(); err == nil {
		t.Fatal("expected error for invalid base64")
	}
}

func TestLoadSecretWrongLength(t *testing.T) {
	envMap(t, allEnvKeys, map[string]string{
		"TEMPO_ENV":    "production",
		"TEMPO_SECRET": base64.StdEncoding.EncodeToString([]byte("short")),
	})
	if _, err := Load(); err == nil {
		t.Fatal("expected error for non-32-byte secret")
	}
}

func TestLoadValidationAggregatesErrors(t *testing.T) {
	envMap(t, allEnvKeys, map[string]string{
		"TEMPO_ENV":              "garbage",
		"TEMPO_DB":               "mysql://x",
		"TEMPO_POLL_INTERVAL":    "not-a-duration",
		"TEMPO_BACKFILL_DAYS":    "-1",
		"TEMPO_LOG_LEVEL":        "fatal",
		"TEMPO_LOG_FORMAT":       "yaml",
		"TEMPO_TZ":               "Mars/Olympus",
		"TEMPO_ROLLUP_HOUR":      "27",
		"TEMPO_SESSION_DURATION": "0",
	})
	_, err := Load()
	if err == nil {
		t.Fatal("expected aggregated error")
	}
	msg := err.Error()
	for _, want := range []string{
		"TEMPO_ENV", "TEMPO_DB", "TEMPO_POLL_INTERVAL", "TEMPO_BACKFILL_DAYS",
		"TEMPO_LOG_LEVEL", "TEMPO_LOG_FORMAT", "TEMPO_TZ",
		"TEMPO_ROLLUP_HOUR", "TEMPO_SESSION_DURATION",
	} {
		if !contains(msg, want) {
			t.Errorf("error missing reference to %s. err=%v", want, err)
		}
	}
}

func TestLoadParsesTimezone(t *testing.T) {
	envMap(t, allEnvKeys, map[string]string{"TEMPO_TZ": "America/Los_Angeles"})
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Rollup.Timezone == nil || cfg.Rollup.Timezone.String() != "America/Los_Angeles" {
		t.Errorf("Rollup.Timezone = %v", cfg.Rollup.Timezone)
	}
}

func TestLoadParsesPollInterval(t *testing.T) {
	envMap(t, allEnvKeys, map[string]string{"TEMPO_POLL_INTERVAL": "30s"})
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Poll.Interval != 30*time.Second {
		t.Errorf("Poll.Interval = %v, want 30s", cfg.Poll.Interval)
	}
}

func TestLoadParsesSyncRunRetention(t *testing.T) {
	envMap(t, allEnvKeys, map[string]string{"TEMPO_SYNC_RUN_RETENTION": "50"})
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Poll.SyncRunRetention != 50 {
		t.Errorf("Poll.SyncRunRetention = %d, want 50", cfg.Poll.SyncRunRetention)
	}
}

func TestLoadRejectsZeroSyncRunRetention(t *testing.T) {
	envMap(t, allEnvKeys, map[string]string{"TEMPO_SYNC_RUN_RETENTION": "0"})
	_, err := Load()
	if err == nil {
		t.Fatal("expected error for TEMPO_SYNC_RUN_RETENTION=0")
	}
	if !contains(err.Error(), "TEMPO_SYNC_RUN_RETENTION") {
		t.Errorf("err missing reference to TEMPO_SYNC_RUN_RETENTION: %v", err)
	}
}

func TestEnvHelpers(t *testing.T) {
	t.Run("dev", func(t *testing.T) {
		t.Setenv("TEMPO_ENV", "development")
		if !IsDev() || IsProd() || IsTest() {
			t.Fatal("dev env helpers wrong")
		}
	})
	t.Run("prod", func(t *testing.T) {
		t.Setenv("TEMPO_ENV", "production")
		if IsDev() || !IsProd() || IsTest() {
			t.Fatal("prod env helpers wrong")
		}
	})
	t.Run("test", func(t *testing.T) {
		t.Setenv("TEMPO_ENV", "test")
		if IsDev() || IsProd() || !IsTest() {
			t.Fatal("test env helpers wrong")
		}
	})
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
