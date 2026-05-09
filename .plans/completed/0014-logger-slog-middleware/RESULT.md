# 0014 — Logger (zap) + request/correlation middleware

## What changed

- `internal/logger/context.go` (new) — `IntoContext` / `FromContext` helpers so any goroutine (HTTP handlers, ingest worker, rollup worker) can pull the scoped `*zap.Logger` from `context.Context`. `FromContext` returns a `zap.NewNop()` fallback so callers don't need a nil check.
- `internal/logger/logger.go` — `New` now takes `*config.Config` and honors `cfg.Log.Level` (`debug|info|warn|error`) and `cfg.Log.Format` (`json|console`). `NewStandalone` is unchanged for `cmd/migrate` (it boots before config is loaded).
- `internal/api/middleware.go` (new) — `requestLogger` middleware: builds a per-request `*zap.Logger` tagged with `trace_id`, injects it into `c.Request().Context()`, calls `c.Error(err)` inline so the response status is final by log time, and emits one structured zap entry per request with `method`, `path`, `status`, `bytes`, `latency_ms`, `ip`, plus the err on non-nil. Severity follows status: `info` for 2xx/3xx, `warn` for 4xx, `error` for 5xx.
- `internal/api/run.go` — registers `requestLogger` between `middleware.RequestID` and `middleware.RecoverWithConfig`. Recover's panic line still emits separately; access log captures the resulting 500 from the response side.
- `internal/api/web/context.go` — `WrapPublic` no longer rebuilds the request-scoped logger. Pulls it from `logger.FromContext(c.Request().Context())` instead. Drops the `*zap.Logger` parameter.
- `internal/api/health/router.go` — call site updated; `*zap.Logger` parameter on `Configure` retained (will be useful when more routes register sub-paths or want to log at registration time).
- `internal/logger/logger_test.go` (new) — table-driven coverage for level honoring, console/json format selection, invalid-level error path, fx lifecycle Sync hook, IntoContext/FromContext round-trip, no-op fallback.
- `internal/api/middleware_test.go` (new) — 2xx info, 4xx warn, handler error error, panic error, and ctx-propagation cases. Uses `zaptest/observer` to capture zap entries and asserts field values + level + same-trace_id correlation between handler logs and the access log.

## Naming decision

Field name is `trace_id`, not `request_id`. The user picked this from a side question — see `feedback_log_correlation.md` in the auto-memory. Tempo runs three log-emitting goroutines (HTTP, ingest, rollup); a single field name across all three lets you grep one ID for an end-to-end logical operation.

## Verify output (last 30 lines)

```
ok
==> go test ./internal/api/... -count=1 (covers request logger middleware)
ok  	github.com/karnstack/tempo/internal/api	0.485s
?   	github.com/karnstack/tempo/internal/api/health	[no test files]
?   	github.com/karnstack/tempo/internal/api/web	[no test files]
  ok
==> go test ./... (no regressions)
?   	github.com/karnstack/tempo/cmd/migrate	[no test files]
?   	github.com/karnstack/tempo/cmd/tempo	[no test files]
ok  	github.com/karnstack/tempo/internal/api	(cached)
?   	github.com/karnstack/tempo/internal/api/health	[no test files]
?   	github.com/karnstack/tempo/internal/api/web	[no test files]
?   	github.com/karnstack/tempo/internal/auth	[no test files]
ok  	github.com/karnstack/tempo/internal/config	(cached)
?   	github.com/karnstack/tempo/internal/github	[no test files]
?   	github.com/karnstack/tempo/internal/ingest	[no test files]
ok  	github.com/karnstack/tempo/internal/logger	(cached)
?   	github.com/karnstack/tempo/internal/metrics	[no test files]
?   	github.com/karnstack/tempo/internal/rollup	[no test files]
?   	github.com/karnstack/tempo/internal/storage	[no test files]
?   	github.com/karnstack/tempo/internal/storage/postgres	[no test files]
ok  	github.com/karnstack/tempo/internal/storage/sqlite	(cached)
?   	github.com/karnstack/tempo/internal/storage/sqlite/sqlitedb	[no test files]
?   	github.com/karnstack/tempo/internal/version	[no test files]
?   	github.com/karnstack/tempo/internal/webui	[no test files]
?   	github.com/karnstack/tempo/migrations	[no test files]
  ok
==> dev boot smoke: tempo starts and emits an access log on /api/v1/system/health
  ok
VERIFY OK
```

## Sample access log line (console format, dev default)

```
2026-05-10T02:35:03.027+0530  INFO  api/middleware.go:56  request  {"service": "tempo", "trace_id": "VXpIVvZWDlGkUehPcamUGnybRBwChRhP", "method": "GET", "path": "/api/v1/system/health", "status": 200, "bytes": 32, "latency_ms": 0, "ip": "127.0.0.1"}
```

## Followups

- When the ingest worker (0026) and rollup worker (0032) land, mint a fresh ULID per cycle and stuff it into the worker context as `trace_id` via `logger.IntoContext`. Same field name, same grep.
- `health.Configure(e, l)` keeps an unused `l` parameter for now — drop it whenever the second route is added and we have a better view of the registration surface.
- Verify.sh's smoke test now builds a binary into a temp dir instead of using `go run`. Reason: `go run` forks the compiled binary as a separate child, making `kill`-by-captured-PID unreliable. The new teardown owns a single PID and SIGKILLs after a soft-exit grace window. Worth standardizing across future verify scripts that boot tempo.
