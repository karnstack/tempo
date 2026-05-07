---
id: 0002
slug: go-module-skeleton
title: Go module + skeleton package layout
status: done
depends_on: [0001]
owner: ""
est_minutes: 45
tags: [bootstrap, go]
autonomy: full
skills: []
---

## Goal

Initialize the Go module under `github.com/karnstack/tempo` and stand up the smallest possible echo + fx + zap server matching the zuzoto-services pattern. `cmd/tempo/main.go` boots an fx app that wires zap and runs the API. `GET /` returns `Hello, tempo` and `GET /api/v1/system/health` returns `{"status":"ok","version":"<sha>"}`. No business logic — this proves the binary builds, starts, and serves both endpoints.

## Stack decisions (zuzoto-services parity)

- HTTP: **echo/v4** (not chi).
- DI: **uber-go/fx**.
- Logger: **uber-go/zap**, with an fx-aware `internal/logger` factory ported from zuzoto.
- Config: minimal env-var loader for now (`TEMPO_LISTEN`, `TEMPO_ENV`). Task 0013 adopts cfgx + TOML.
- Layout: API code lives under `internal/api/` (monolith adaptation of zuzoto's `<svc>/api/`).
- Handler shape: `web.Context` wrapping `echo.Context` + `*zap.Logger`, registered via `Configure(e, l)` per feature.

## Acceptance criteria

- [ ] `go.mod` declares module `github.com/karnstack/tempo`, `go 1.26.3` (matches `.mise.toml` pin).
- [ ] Deps include: `github.com/labstack/echo/v4`, `go.uber.org/fx`, `go.uber.org/zap`. **No chi.**
- [ ] Package skeletons exist (each compiles): `internal/{config,logger,api,auth,storage,github,ingest,rollup,metrics,webui}` plus `internal/api/{health,web}`.
- [ ] `cmd/tempo/main.go` boots an `fx.New(...)` app that provides `*zap.Logger` and invokes `api.Run`. The fx logger uses zap.
- [ ] `internal/api/run.go` exposes `Run(lc fx.Lifecycle, l *zap.Logger) error`, configures middleware (RequestID, Recover), calls `configureRoutes(e, l)`, and registers an `fx.Hook` that starts the server in a goroutine and shuts it down on stop.
- [ ] `GET /` returns `200 Hello, tempo`.
- [ ] `GET /api/v1/system/health` returns `200 application/json` with body containing `"status":"ok"` and `"version":"<sha-or-dev>"`.
- [ ] Server listens on `:8080` by default, overridable via `TEMPO_LISTEN`.
- [ ] `make build` produces a `./tempo` binary in repo root.
- [ ] `verify.sh` passes.

## Files to touch

Create:

- `go.mod`, `go.sum`
- `cmd/tempo/main.go`
- `internal/config/config.go` — env loader (`Listen`, `Env`).
- `internal/logger/logger.go` — `New(lc fx.Lifecycle) (*zap.Logger, error)` + `NewStandalone()`.
- `internal/api/run.go` — `Run` + `configureMiddleware` + `configureRoutes`.
- `internal/api/web/context.go` — `Context` (embeds echo.Context, holds `L *zap.Logger`), `HandlerFunc`, `WrapPublic`.
- `internal/api/health/router.go` — `Configure(e, l)` registering `/api/v1/system/health`.
- `internal/api/health/get.go` — health handler returning `{status, version}`.
- `internal/api/version.go` — `var Version = "dev"` (overridden via `-ldflags`).
- Stub `<pkg>.go` files in `internal/{auth,storage,github,ingest,rollup,metrics,webui}` — each `package <name>` + one-line doc comment.

Modify:

- `Makefile` — fill in `build`, `test`, `lint`, `fmt`, `clean` targets for the Go side.

## Steps

- [ ] **Step 1 — Init module and add deps**

  ```bash
  go mod init github.com/karnstack/tempo
  go get github.com/labstack/echo/v4 go.uber.org/fx go.uber.org/zap
  ```

  Then bump the `go` directive in `go.mod` to `1.26.3` (matches `.mise.toml`).

- [ ] **Step 2 — `internal/config/config.go`**

  ```go
  // Package config loads tempo's runtime configuration from environment variables.
  package config

  import "os"

  type Config struct {
      Listen string
      Env    string
  }

  func Load() *Config {
      return &Config{
          Listen: getenv("TEMPO_LISTEN", ":8080"),
          Env:    getenv("TEMPO_ENV", "development"),
      }
  }

  func IsDev() bool { return Load().Env == "development" }

  func getenv(key, fallback string) string {
      if v := os.Getenv(key); v != "" {
          return v
      }
      return fallback
  }
  ```

- [ ] **Step 3 — `internal/logger/logger.go`** (port from zuzoto, drop pgx tracelog for now)

  ```go
  // Package logger builds the application zap logger and wires its lifecycle into fx.
  package logger

  import (
      "context"
      "errors"
      "syscall"

      "github.com/karnstack/tempo/internal/config"
      "go.uber.org/fx"
      "go.uber.org/zap"
      "go.uber.org/zap/zapcore"
  )

  func New(lc fx.Lifecycle) (*zap.Logger, error) {
      cfg := zap.NewProductionConfig()
      if config.IsDev() {
          cfg = zap.NewDevelopmentConfig()
          cfg.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
      }

      l, err := cfg.Build()
      if err != nil {
          return nil, err
      }

      lc.Append(fx.Hook{
          OnStop: func(_ context.Context) error {
              if err := l.Sync(); err != nil &&
                  !errors.Is(err, syscall.ENOTTY) &&
                  !errors.Is(err, syscall.EINVAL) &&
                  !errors.Is(err, syscall.EBADF) {
                  return err
              }
              return nil
          },
      })

      return l, nil
  }

  func NewStandalone() *zap.Logger {
      cfg := zap.NewProductionConfig()
      if config.IsDev() {
          cfg = zap.NewDevelopmentConfig()
          cfg.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
      }
      l, err := cfg.Build()
      if err != nil {
          panic(err)
      }
      return l
  }
  ```

- [ ] **Step 4 — `internal/api/web/context.go`**

  ```go
  // Package web provides the request context and handler wrappers used by API endpoints.
  package web

  import (
      "github.com/labstack/echo/v4"
      "go.uber.org/zap"
  )

  // Context wraps echo.Context with the request-scoped logger.
  // Auth and tenant fields will be added when the auth tasks land.
  type Context struct {
      echo.Context
      L *zap.Logger
  }

  type HandlerFunc func(ctx *Context) error

  func WrapPublic(h HandlerFunc, l *zap.Logger) echo.HandlerFunc {
      return func(c echo.Context) error {
          rid := c.Response().Header().Get(echo.HeaderXRequestID)
          ctx := &Context{
              Context: c,
              L:       l.With(zap.String("request_id", rid)),
          }
          return h(ctx)
      }
  }
  ```

- [ ] **Step 5 — `internal/api/version.go`**

  ```go
  package api

  // Version is overridden at build time via -ldflags.
  var Version = "dev"
  ```

- [ ] **Step 6 — `internal/api/health/get.go`**

  ```go
  package health

  import (
      "net/http"

      "github.com/karnstack/tempo/internal/api"
      "github.com/karnstack/tempo/internal/api/web"
  )

  type Response struct {
      Status  string `json:"status"`
      Version string `json:"version"`
  }

  func Get(ctx *web.Context) error {
      return ctx.JSON(http.StatusOK, Response{
          Status:  "ok",
          Version: api.Version,
      })
  }
  ```

- [ ] **Step 7 — `internal/api/health/router.go`**

  ```go
  package health

  import (
      "github.com/labstack/echo/v4"
      "github.com/karnstack/tempo/internal/api/web"
      "go.uber.org/zap"
  )

  func Configure(e *echo.Echo, l *zap.Logger) {
      e.GET("/api/v1/system/health", web.WrapPublic(Get, l))
  }
  ```

- [ ] **Step 8 — `internal/api/run.go`**

  ```go
  // Package api hosts the echo server and route registration for tempo's REST API.
  package api

  import (
      "context"
      "errors"
      "fmt"
      "net/http"
      "time"

      "github.com/labstack/echo/v4"
      "github.com/labstack/echo/v4/middleware"
      "github.com/karnstack/tempo/internal/api/health"
      "github.com/karnstack/tempo/internal/config"
      "go.uber.org/fx"
      "go.uber.org/zap"
  )

  func Run(lc fx.Lifecycle, l *zap.Logger) error {
      cfg := config.Load()

      e := echo.New()
      if !config.IsDev() {
          e.HideBanner = true
          e.HidePort = true
      }

      configureMiddleware(e, l)
      configureRoutes(e, l)

      server := &http.Server{
          Addr:              cfg.Listen,
          Handler:           e,
          ReadTimeout:       30 * time.Second,
          ReadHeaderTimeout: 5 * time.Second,
          WriteTimeout:      30 * time.Second,
          IdleTimeout:       60 * time.Second,
          MaxHeaderBytes:    1 << 20,
      }

      lc.Append(fx.Hook{
          OnStart: func(_ context.Context) error {
              go func() {
                  l.Info("starting tempo api", zap.String("addr", server.Addr))
                  if err := e.StartServer(server); err != nil && !errors.Is(err, http.ErrServerClosed) {
                      l.Error("error starting echo server", zap.Error(err))
                  }
              }()
              return nil
          },
          OnStop: func(ctx context.Context) error {
              l.Info("shutdown signal received")
              return e.Shutdown(ctx)
          },
      })

      return nil
  }

  func configureMiddleware(e *echo.Echo, l *zap.Logger) {
      e.Use(middleware.RequestID())
      e.Use(middleware.RecoverWithConfig(middleware.RecoverConfig{
          StackSize: 1 << 12,
          LogErrorFunc: func(c echo.Context, err error, stack []byte) error {
              l.Error("recovered from panic",
                  zap.Error(err),
                  zap.ByteString("stack", stack),
                  zap.String("request_id", c.Response().Header().Get(echo.HeaderXRequestID)),
              )
              return nil
          },
      }))
  }

  func configureRoutes(e *echo.Echo, l *zap.Logger) {
      e.GET("/", func(c echo.Context) error {
          return c.String(http.StatusOK, "Hello, tempo")
      })
      health.Configure(e, l)

      // Suppress unused-var lint for the format helper.
      _ = fmt.Sprintf
  }
  ```

  (The `_ = fmt.Sprintf` line is removed in the actual file — just delete the `fmt` import if it's not used.)

- [ ] **Step 9 — Stub the remaining internal packages.** For each of `internal/{auth,storage,github,ingest,rollup,metrics,webui}/`, create a single `<pkg>.go` with `package <name>` plus a one-line doc comment.

- [ ] **Step 10 — `cmd/tempo/main.go`**

  ```go
  package main

  import (
      "github.com/karnstack/tempo/internal/api"
      "github.com/karnstack/tempo/internal/logger"
      "go.uber.org/fx"
      "go.uber.org/fx/fxevent"
      "go.uber.org/zap"
  )

  func main() {
      fx.New(
          fx.Provide(logger.New),
          fx.Decorate(func(l *zap.Logger) *zap.Logger {
              return l.With(zap.String("service", "tempo"))
          }),
          fx.Invoke(api.Run),
          fx.WithLogger(func(l *zap.Logger) fxevent.Logger {
              return &fxevent.ZapLogger{Logger: l}
          }),
      ).Run()
  }
  ```

- [ ] **Step 11 — Update Makefile Go targets**

  ```make
  GO_LDFLAGS = -X github.com/karnstack/tempo/internal/api.Version=$(shell git rev-parse --short HEAD 2>/dev/null || echo dev)

  build: ## Build the Go binary (SPA embed lands in 0005)
  	go build -ldflags "$(GO_LDFLAGS)" -o tempo ./cmd/tempo

  test: ## Run all tests (Go + frontend)
  	go test ./...

  lint: ## Run all linters
  	golangci-lint run

  fmt: ## Format Go + frontend
  	go fmt ./...
  ```

- [ ] **Step 12 — `go mod tidy`** to clean up `go.sum`.

- [ ] **Step 13 — Run `./verify.sh`** from the task dir.

- [ ] **Step 14 — Commit**

  ```bash
  git add go.mod go.sum cmd internal Makefile
  git commit -m "feat(server): minimal echo+fx+zap server with /system/health (#0002)"
  ```

## Notes

- We're not embedding the SPA yet — `/` returns plain text. Task 0005 adds the embed.
- `cfgx`-based typed config lands in task 0013; for now `internal/config` is a tiny env loader so we can ship a working binary.
- Pgx `tracelog` integration in `internal/logger` is deferred to the storage tasks.
- `Version` lives on the `api` package (`internal/api`) rather than `server` because there is no `server` package — the http server is built inside `api.Run`.
