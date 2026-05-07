# Result — 0002 Go module + skeleton package layout

## What changed

Adopted the zuzoto-services stack (echo + fx + zap) as the baseline for tempo's Go side. Original task called for chi + slog; user asked to mirror `/Users/karn/code/zuzoto/services` while keeping tempo as a single binary, so the plan was rewritten before implementation.

### New files

- `go.mod`, `go.sum` — module `github.com/karnstack/tempo`, `go 1.26.3` (matches `.mise.toml` pin). Direct deps: `github.com/labstack/echo/v4`, `go.uber.org/fx`, `go.uber.org/zap`.
- `cmd/tempo/main.go` — `fx.New()` providing `logger.New`, decorating zap with `service=tempo`, invoking `api.Run`, using `fxevent.ZapLogger`.
- `internal/config/config.go` — env-var loader (`TEMPO_LISTEN`, `TEMPO_ENV`) + `IsDev()`. cfgx adoption deferred to task 0013.
- `internal/logger/logger.go` — fx-aware zap factory ported from zuzoto (`logger.New(lc) (*zap.Logger, error)` + `NewStandalone()`); pgx tracelog integration deferred until storage tasks.
- `internal/api/run.go` — `Run(lc fx.Lifecycle, l *zap.Logger) error` builds echo, configures `RequestID` + `Recover` middleware, registers routes, attaches an `fx.Hook` that starts the server in a goroutine and shuts it down on stop.
- `internal/api/web/context.go` — `web.Context` (echo.Context + `*zap.Logger`) and `WrapPublic`. Auth/scope wrappers will land with the auth tasks.
- `internal/api/health/get.go` + `router.go` — handler returning `{"status":"ok","version":"<sha>"}` and `Configure(e, l)`.
- `internal/version/version.go` — `var Version = "dev"`, lives in its own leaf package to avoid an `api → health → api` import cycle (Version is overridden at build time via `-ldflags`).
- Stub one-line `package` files in `internal/{auth,storage,github,ingest,rollup,metrics,webui}`.

### Modified

- `Makefile` — `build`, `test`, `lint`, `fmt` Go targets implemented. `GO_LDFLAGS` injects the git short SHA into `internal/version.Version`.
- `.plans/upnext/0002-go-module-skeleton/TASK.md` — full rewrite of stack/layout sections to reflect echo + fx + zap.
- `.plans/upnext/0002-go-module-skeleton/verify.sh` — checks for echo/fx/zap deps, asserts no chi, walks the new package layout (`internal/api/{web,health}` etc.).

## Verify output (last lines)

```
INFO  api/run.go:43  starting tempo api  {"service": "tempo", "addr": ":18519"}
INFO  fxevent/zap.go:51  started  {"service": "tempo"}
⇨ http server started on [::]:18519
verify ok
INFO  fxevent/zap.go:51  received signal  {"service": "tempo", "signal": "TERMINATED"}
INFO  api/run.go:51  shutdown signal received  {"service": "tempo"}
INFO  fxevent/zap.go:51  OnStop hook executed
```

`go vet ./...` and `go test ./...` both clean (no test files yet).

## Followups

- Task 0013 will replace `internal/config`'s env loader with cfgx + TOML.
- Task 0014 will extend `internal/logger` with the request logger middleware (zuzoto's `RequestLoggerWithConfig` block) and the pgx tracelog adapter once storage lands.
- The version short-SHA shows as `d0ae0ee` in the build output because that's `HEAD` at build time; once this task commits it will roll forward.
