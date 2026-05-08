# 0013 — Config package + env loader + validation (RESULT)

## Files changed

- `internal/config/config.go` — expanded from `{Listen, Env, Database}` to also cover `Secret`, `Poll`, `Log`, `Rollup`, `Session`. `Load()` signature is now `(*Config, error)`. Validation is field-level and aggregated via `errors.Join` so a misconfigured boot reports every problem at once.
- `internal/config/config_test.go` — table-driven coverage: defaults, production-secret-required, production-with-valid-secret, invalid-base64, wrong-length, aggregated-validation, TZ parse, env helpers.
- `cmd/migrate/main.go` — threads the new `(cfg, err)` return.
- `cmd/tempo/main.go` — adds an `fx.Invoke` that surfaces `cfg.SecretWarning` at WARN.
- `internal/api/run.go` — takes `*config.Config` from fx instead of calling `config.Load()` inside `Run`. Tighter DI; no functional change.

## Decision: stay hand-written, do not adopt cfgx in this task

The user's saved feedback ("mirror zuzoto-services Go stack: cfgx + …") points at cfgx. On inspection, `github.com/gomantics/cfgx` v0.0.8 hardcodes `CONFIG_*` as the env-var prefix (see `internal/envoverride/envoverride.go` line 15 and `internal/generator/struct_gen.go` line 589). The tempo spec documents `TEMPO_*` env vars as the public interface (spec lines 281–292). Adopting cfgx as-is would either break the spec's documented env names or require a parallel `TEMPO_* → CONFIG_*` shim — net new complexity for no win.

Surfaced as a followup decision to make with the user:
1. Stay hand-written (status quo after this task) — small per-feature cost as new env vars are added.
2. Adopt cfgx and rename env vars to `CONFIG_*` — touches docs but unifies tooling with zuzoto.
3. Fork cfgx to support a configurable prefix flag and upstream — best-of-both, but largest scope.

## Notes / decisions captured during execution

- `loadLog` was originally returning early on the first error, which masked a `TEMPO_LOG_FORMAT` problem when both LEVEL and FORMAT were invalid. Switched to `errors.Join` so the validation-aggregation test could reliably see all 9 errors in one boot.
- Dev fallback for `TEMPO_SECRET` is deterministic on purpose (`sha256("tempo-dev")`) so dev sessions survive process restart. `Config.SecretWarning` is the load-bearing signal that the deploy is unsafe; production fails Load instead of warning.
- Default `TEMPO_LOG_FORMAT` is `console` in development and `json` in production. The spec doesn't mention it directly; documented in code comments.
- Added `TEMPO_ROLLUP_HOUR` (0–23, default 2) and `TEMPO_SESSION_DURATION` (default 720h). Spec line 141 mentions "02:00 instance-local" without an env var — folding it into `TEMPO_ROLLUP_HOUR` makes it tunable. Spec doesn't define a session duration env at all; this fills the obvious gap.

## Verify output (last lines)

```
==> dev boot smoke: tempo starts, surfaces SecretWarning, opens port 8080
  ok
VERIFY OK
```

The smoke test boots `cmd/tempo`, asserts the `TEMPO_SECRET unset` warning is emitted, and asserts the API starts before the script kills it.

## Followups

- Make the cfgx-vs-hand-written decision with the user.
- 0014 (Logger middleware) is the next task and will replace the dev/prod logger config with one that reads `cfg.Log.{Level,Format}` instead of hardcoding `zap.NewProductionConfig` / `zap.NewDevelopmentConfig`.
- The `TEMPO_LOG_FORMAT`, `TEMPO_ROLLUP_HOUR`, and `TEMPO_SESSION_DURATION` additions should be reflected in `docs/superpowers/specs/2026-05-08-tempo-design.md`'s env table when that doc is next refreshed.
