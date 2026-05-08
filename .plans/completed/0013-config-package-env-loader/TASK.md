---
id: 0013
slug: config-package-env-loader
title: Config package + env loader + validation
status: done
depends_on: [0002]
owner: ""
est_minutes: 35
tags: [config]
autonomy: full
skills: []
---

## Goal

Expand `internal/config` from the bootstrap stub (just `Listen`, `Env`, `Database`) to the full v1 surface the spec documents. Cover every `TEMPO_*` env var, parse + validate them, and make the resulting `*config.Config` the single source of truth for the rest of the app to inject via fx.

Spec reference: `docs/superpowers/specs/2026-05-08-tempo-design.md` lines 281–292 (env table).

### Note on cfgx

The user's saved feedback says "mirror zuzoto-services Go stack: cfgx + …". On inspection, `github.com/gomantics/cfgx` v0.0.8 hardcodes the env-var prefix to `CONFIG_*` (see `internal/envoverride/envoverride.go` and `internal/generator/struct_gen.go`), with no flag to override. The tempo spec explicitly documents `TEMPO_*` env vars as the public interface. Adopting cfgx as-is would either (a) break the spec's documented env names or (b) require a parallel shim that reads `TEMPO_*` first and falls through to cfgx's `CONFIG_*` defaults — net new complexity for no win.

Decision for this task: stay hand-written. cfgx adoption (or a fork that supports prefix override) is worth surfacing as a separate decision with the user. Documented in RESULT.md.

## Acceptance criteria

- [ ] `Config` struct grows nested config sub-structs: `Secret`, `Poll`, `Log`, `Rollup`, `Session`. `Database` and the top-level `Listen` / `Env` stay where they are.
- [ ] All env vars from spec lines 281–292 are read with documented defaults:
  - `TEMPO_LISTEN` (`:8080`)
  - `TEMPO_ENV` (`development`)
  - `TEMPO_DB` (`sqlite://./data/tempo.db`)
  - `TEMPO_SECRET` (base64 32-byte key — required in production; dev fallback generates an ephemeral key with a warning recorded on the Config so logger.New can surface it)
  - `TEMPO_POLL_INTERVAL` (`15m`, parsed via `time.ParseDuration`)
  - `TEMPO_BACKFILL_DAYS` (`90`, int)
  - `TEMPO_LOG_LEVEL` (`info`; one of debug/info/warn/error)
  - `TEMPO_TZ` (empty = system; otherwise `time.LoadLocation`)
- [ ] Plus two tempo-internal-but-useful additions: `TEMPO_LOG_FORMAT` (`json` in prod, `console` in dev), `TEMPO_ROLLUP_HOUR` (`2`, 0–23), `TEMPO_SESSION_DURATION` (`720h`). Document in code comments + spec follow-up.
- [ ] `Load()` signature changes to `Load() (*Config, error)`. Validation errors are returned, not panicked. fx providers natively handle this.
- [ ] Validation rules:
  - `Env` in `{development, production, test}`.
  - In `production`, `TEMPO_SECRET` must be set and decode to exactly 32 bytes; `Config.SecretWarning` empty.
  - In `development`/`test` with no `TEMPO_SECRET`, generate a deterministic dev key (e.g. SHA-256 of `"tempo-dev"`) and set `Config.SecretWarning` to a human-readable string for the logger.
  - `LogLevel ∈ {debug, info, warn, error}`. Invalid → error.
  - `LogFormat ∈ {json, console}`. Invalid → error.
  - `Poll.Interval > 0`, `Poll.BackfillDays > 0`. Invalid → error.
  - `Rollup.Hour ∈ [0, 23]`. Invalid → error.
  - `Session.Duration > 0`. Invalid → error.
  - `TEMPO_TZ`: if non-empty, must `time.LoadLocation` cleanly.
- [ ] Multiple errors are aggregated via `errors.Join` so a misconfigured deploy surfaces *all* problems on one boot, not just the first.
- [ ] Existing call sites continue to work: `cmd/tempo/main.go` registers `config.Load` as an fx provider (already does), `cmd/migrate/main.go` calls `config.Load()` directly — update it to handle the error return (`l.Fatal` on failure).
- [ ] `IsDev()` keeps the same shape (no breaking change). Add `IsProd()` and `IsTest()` for symmetry.
- [ ] Tests cover: each parse/default path; each validation rule; production-without-secret error; dev-without-secret warning; multiple-errors aggregation.
- [ ] `go build ./...` and `go test ./...` pass.
- [ ] `verify.sh` exits 0.

## Files to touch

- `internal/config/config.go` (expand)
- `internal/config/config_test.go` (add coverage)
- `cmd/migrate/main.go` (update for `(*Config, error)` return)
- `.plans/upnext/0013-config-package-env-loader/verify.sh` (replace stub)

## Steps

### 1. Expand `Config`

Add sub-structs and a `SecretWarning string` field on `Config`. Keep field order stable for diff readability.

### 2. Implement `Load() (*Config, error)`

Parse each env var, accumulate errors via `errors.Join`. For `TEMPO_SECRET`:

```go
secret, secretWarning, err := loadSecret(env)
```

`loadSecret` returns the 32-byte key, an optional warning, and a validation error. In production: require base64 of exactly 32 bytes. In dev/test: if unset, derive `sha256.Sum256([]byte("tempo-dev"))[:]` and emit a warning string.

### 3. Update call sites

- `cmd/migrate/main.go`: `cfg, err := config.Load(); if err != nil { l.Fatal(...) }`. Already calls `Load()`; just thread through the error.
- `cmd/tempo/main.go`: no change needed — fx handles the error return.
- `internal/api/run.go`: switch to taking `*config.Config` as a parameter from fx instead of calling `config.Load()` inside Run. Tighter DI.
- `internal/logger/logger.go`: surface `cfg.SecretWarning` if non-empty (at WARN level).

### 4. Tests

Table-driven tests for each parser, validator, and error-aggregation path. Use `t.Setenv` for env-var manipulation. Verify dev-fallback secret is deterministic and 32 bytes.

### 5. Verify

`./.plans/upnext/0013-config-package-env-loader/verify.sh` exits 0.

## Notes

- Production `TEMPO_SECRET` *must* be a 32-byte base64. We don't accept hex to avoid format-confusion bugs. `openssl rand -base64 32` is the documented generator.
- The dev-fallback derived key (`sha256("tempo-dev")`) is deterministic on purpose so rebooting doesn't invalidate dev sessions / require re-login. It must NEVER be considered safe — `Config.SecretWarning` calls this out, and the production check ensures real deployments fail fast without a real key.
- We don't depend on cfgx in this task. See the "Note on cfgx" section above and the followups in RESULT.md.
- Keeping `Listen`, `Env`, `Database` flat (not nested under `Server` etc.) preserves existing call sites verbatim. Only new fields are nested.
