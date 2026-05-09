---
id: 0014
slug: logger-slog-middleware
title: Logger (zap) + request/correlation middleware
status: in_progress
depends_on: [0013]
owner: ""
est_minutes: 35
tags: [server]
autonomy: full
skills: []
---

## Goal

Wire tempo's logging surface end-to-end so every request emits exactly one structured log line and any downstream code (storage, ingest, rollup) can pull a request-scoped `*zap.Logger` from `context.Context`.

The bootstrap from earlier tasks already gives us:

- `logger.New` returning a zap logger with an fx lifecycle hook (dev/prod mode picked by `IsDev()`).
- `logger.NewStandalone` for short-lived CLI tools (e.g. `cmd/migrate`).
- echo `middleware.RequestID()` wired in `internal/api/run.go`.
- echo `middleware.Recover` wired with a zap-backed `LogErrorFunc` that records the panic, stack, and `trace_id`.
- A request-scoped `web.Context` carrying a `*zap.Logger` with `trace_id` attached.

What's missing — and what this task delivers:

1. `logger.New` ignores `cfg.Log.Level` and `cfg.Log.Format` from 0013. We thread the Config through and honor both.
2. There is no per-request access log. We add one structured zap line per request: `method`, `path`, `status`, `bytes`, `latency_ms`, `trace_id`, `ip`, with severity scaling by status code.
3. The request-scoped logger lives only on `web.Context.L` — code that doesn't reach for `web.Context` (storage, ingest, rollup helpers invoked via `ctx context.Context`) can't pull it. We add `logger.IntoContext` / `logger.FromContext` and have the new middleware inject the per-request logger into `c.Request().Context()`.
4. Tests for level/format honoring, context round-trip, and the request-log middleware behavior under success / 4xx / 5xx / panic.

> **Plan vs reality:** The master plan row 0014 names "slog" as the logger. Tempo runs on `uber-go/zap` per the user's saved `feedback_zuzoto_pattern` memory, and the codebase has been zap since 0002. Treat the plan's "slog" wording as stale; this task uses zap. Title in frontmatter updated to match.

Spec reference: `docs/superpowers/specs/2026-05-08-tempo-design.md` line 290 (`TEMPO_LOG_LEVEL`), line 454 ("Structured logs from day 1").

## Acceptance criteria

- [ ] `logger.New(lc, cfg)` honors `cfg.Log.Level` (`debug|info|warn|error`) and `cfg.Log.Format` (`json|console`). Dev defaults still produce console + colored levels; prod defaults still produce JSON.
- [ ] `logger.NewStandalone()` keeps its current zero-arg signature for `cmd/migrate` (it boots before config is loaded). Behavior unchanged: dev = console, prod = JSON, info level.
- [ ] `logger.IntoContext(ctx, l) context.Context` and `logger.FromContext(ctx) *zap.Logger` exist. `FromContext` returns a `zap.NewNop()` when the context has no logger so callers don't need a nil check.
- [ ] A new echo middleware in `internal/api` builds a per-request logger (`trace_id` field), injects it into `c.Request().Context()`, and emits one zap entry per request with `method`, `path`, `status`, `bytes`, `latency_ms`, `trace_id`, `ip`. Severity: `info` for 1xx/2xx/3xx, `warn` for 4xx, `error` for 5xx or non-nil handler error.
- [ ] Middleware order in `configureMiddleware`: `RequestID` → request logger → `Recover`. The Recover hook still records its panic line — the access log line emits afterwards with `status=500`.
- [ ] `web.WrapPublic` pulls the request-scoped logger via `logger.FromContext(c.Request().Context())` instead of rebuilding it. Stop threading `*zap.Logger` through `WrapPublic`'s signature.
- [ ] `health.Configure` still receives `*zap.Logger` (kept for future logging during registration) but no longer passes it to `WrapPublic`.
- [ ] `internal/logger/logger_test.go` covers: `Level`/`Format` honored (table-driven); `IntoContext`/`FromContext` round-trip; `FromContext` on a bare context returns a non-nil logger that does not panic on use.
- [ ] `internal/api/middleware_test.go` covers: middleware emits one entry per request; entry contains `trace_id`/`method`/`path`/`status`/`latency_ms`; severity scales with status; handler can pull logger from `c.Request().Context()` and that logger has `trace_id` attached.
- [ ] `go vet ./...`, `go build ./...`, `go test ./...` all pass.
- [ ] Dev boot smoke (the same one 0013 already exercises) still surfaces `starting tempo api`, and now also surfaces a per-request access log line when we curl `/api/v1/system/health`.

## Files to touch

- `internal/logger/logger.go` (signature change: `New(lc, *config.Config)`, level/format wiring)
- `internal/logger/context.go` (new: `IntoContext`/`FromContext` + unexported context key)
- `internal/logger/logger_test.go` (new)
- `internal/api/middleware.go` (new: `requestLogger` middleware factory)
- `internal/api/run.go` (wire request logger into the chain; pass `*zap.Logger` to it)
- `internal/api/middleware_test.go` (new)
- `internal/api/web/context.go` (drop `*zap.Logger` parameter from `WrapPublic`; pull from ctx)
- `internal/api/health/router.go` (call site update for the new `WrapPublic` shape)
- `cmd/tempo/main.go` (no code change expected — fx wires `*config.Config` into `logger.New` automatically once the signature changes; update only if compile fails)
- `.plans/upnext/0014-logger-slog-middleware/verify.sh` (replace stub)

## Steps

### 1. Add `logger.IntoContext` / `logger.FromContext`

Create `internal/logger/context.go` with an unexported context key and the two helpers:

```go
package logger

import (
	"context"

	"go.uber.org/zap"
)

type ctxKey struct{}

// IntoContext returns a copy of ctx carrying l. FromContext on the result
// returns l. Use this when starting a request, scheduling a job, or otherwise
// minting a context that should propagate a scoped logger.
func IntoContext(ctx context.Context, l *zap.Logger) context.Context {
	return context.WithValue(ctx, ctxKey{}, l)
}

// FromContext returns the logger previously attached via IntoContext, or a
// no-op logger if none is present. Callers never need a nil check.
func FromContext(ctx context.Context) *zap.Logger {
	if l, ok := ctx.Value(ctxKey{}).(*zap.Logger); ok && l != nil {
		return l
	}
	return zap.NewNop()
}
```

Commit: `feat(logger): context propagation helpers`

### 2. Honor `cfg.Log.Level` / `cfg.Log.Format` in `logger.New`

Change the signature to `New(lc fx.Lifecycle, cfg *config.Config) (*zap.Logger, error)`. Build a `zap.Config` whose `Encoding` is `cfg.Log.Format` (`"json"` or `"console"`) and whose `Level` is parsed from `cfg.Log.Level`. Keep dev's colored capital-level encoder when `Format == "console"`. Production still gets ISO timestamps + lowercase levels. `NewStandalone` is unchanged.

Sketch:

```go
func New(lc fx.Lifecycle, cfg *config.Config) (*zap.Logger, error) {
	lvl, err := zapcore.ParseLevel(cfg.Log.Level)
	if err != nil {
		return nil, fmt.Errorf("logger: parse level: %w", err)
	}
	zc := zap.NewProductionConfig()
	if cfg.Log.Format == "console" {
		zc = zap.NewDevelopmentConfig()
		zc.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	}
	zc.Level = zap.NewAtomicLevelAt(lvl)
	zc.Encoding = cfg.Log.Format
	l, err := zc.Build()
	if err != nil { return nil, err }
	lc.Append(fx.Hook{ OnStop: ... })
	return l, nil
}
```

Verify `cmd/tempo/main.go` still compiles — fx supplies `*config.Config` via the existing `config.Load` provider, so the new dependency resolves automatically.

Commit: `feat(logger): honor TEMPO_LOG_LEVEL and TEMPO_LOG_FORMAT`

### 3. Tests for logger

Add `internal/logger/logger_test.go`. Use `zaptest/observer` is overkill here — the test target is the configuration plumbing. Build a fake `*config.Config` per case and assert:

- Each level string yields a logger that enables the matching `zapcore.Level` (`l.Core().Enabled(level)`).
- `Format == "json"` and `Format == "console"` both produce non-nil loggers (build success is the contract).
- Invalid level surfaces an error from `New`.
- `IntoContext`/`FromContext` round-trip the same `*zap.Logger`.
- `FromContext(context.Background())` returns a non-nil no-op logger that doesn't panic on `Info("x")`.

Commit: `test(logger): level/format and context helpers`

### 4. Add request logger middleware

Create `internal/api/middleware.go`:

```go
package api

import (
	"time"

	"github.com/karnstack/tempo/internal/logger"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

// requestLogger builds a per-request *zap.Logger tagged with trace_id,
// injects it into c.Request().Context(), and emits one access log entry
// after the handler returns. Severity scales with response status; handler
// errors are logged at error level.
func requestLogger(l *zap.Logger) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			start := time.Now()
			req := c.Request()
			rid := req.Header.Get(echo.HeaderXRequestID)
			if rid == "" {
				rid = c.Response().Header().Get(echo.HeaderXRequestID)
			}
			rl := l.With(zap.String("trace_id", rid))
			c.SetRequest(req.WithContext(logger.IntoContext(req.Context(), rl)))

			err := next(c)

			res := c.Response()
			fields := []zap.Field{
				zap.String("method", req.Method),
				zap.String("path", req.URL.Path),
				zap.Int("status", res.Status),
				zap.Int64("bytes", res.Size),
				zap.Int64("latency_ms", time.Since(start).Milliseconds()),
				zap.String("ip", c.RealIP()),
			}
			switch {
			case err != nil:
				rl.Error("request", append(fields, zap.Error(err))...)
			case res.Status >= 500:
				rl.Error("request", fields...)
			case res.Status >= 400:
				rl.Warn("request", fields...)
			default:
				rl.Info("request", fields...)
			}
			return err
		}
	}
}
```

Wire it in `configureMiddleware` between `RequestID` and `Recover`:

```go
e.Use(middleware.RequestID())
e.Use(requestLogger(l))
e.Use(middleware.RecoverWithConfig(...))
```

Commit: `feat(api): per-request access log middleware with context propagation`

### 5. Simplify `WrapPublic`

`internal/api/web/context.go`:

```go
func WrapPublic(h HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		return h(&Context{
			Context: c,
			L:       logger.FromContext(c.Request().Context()),
		})
	}
}
```

Update `internal/api/health/router.go`:

```go
func Configure(e *echo.Echo, l *zap.Logger) {
	e.GET("/api/v1/system/health", web.WrapPublic(Get))
	_ = l // reserved for registration-time logging
}
```

(Or just drop the unused parameter from `Configure` later when more routes land. For this task, keep the signature stable to avoid churn — leaving `l` unused is fine for one route.)

Commit: `refactor(api): pull per-request logger from context in WrapPublic`

### 6. Tests for middleware

Add `internal/api/middleware_test.go`. Use `zap/zaptest/observer` to capture entries, `httptest.NewRequest` + `httptest.NewRecorder` to drive echo. Cases:

- 200: one observed entry, level Info, fields include `trace_id`, `method=GET`, `path=/ok`, `status=200`, `latency_ms` ≥ 0.
- 404: one observed entry at Warn.
- 500 (handler returns echo.NewHTTPError): one observed entry at Error with `error` field.
- Context propagation: handler reads `logger.FromContext(c.Request().Context())`, asserts the returned logger is not nop, and writes an entry — observer sees it carry `trace_id`.

Commit: `test(api): request logger middleware emits one entry per request`

### 7. Verify

`./.plans/upnext/0014-logger-slog-middleware/verify.sh` exits 0.

## Notes

- We do not rename `internal/logger` to `internal/log` or anything fancy — keeps imports stable.
- We deliberately do NOT register a goroutine-safe logger swap for `TEMPO_LOG_LEVEL` reload at runtime. v1 doesn't ship hot-reload of config; if it ever does, that's a separate task.
- Echo's `RequestID` middleware sets the header before our logger runs, so `req.Header.Get(echo.HeaderXRequestID)` works even though the value is technically generated server-side. The fallback to the response header covers the case where echo writes it there first; both forms exist defensively because echo's behavior across versions has been inconsistent.
- We don't replace echo's `Recover` middleware. Its `LogErrorFunc` already emits a structured panic record. The access log we emit afterwards captures the resulting 500 from a separate angle — the two are complementary, not duplicative.
- Severity scaling on the access log (info/warn/error by status) is intentional: it makes `grep level=warn` a useful triage tool against a self-hosted instance with no APM.
