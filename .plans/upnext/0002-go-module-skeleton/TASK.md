---
id: 0002
slug: go-module-skeleton
title: Go module + skeleton package layout
status: pending
depends_on: [0001]
owner: ""
est_minutes: 30
tags: [bootstrap, go]
autonomy: full
skills: []
---

## Goal

Initialize the Go module under `github.com/karnstack/tempo`, create the package skeleton matching the spec's repo layout, and add a `main.go` that boots the HTTP server with `Hello, tempo` at `/` and `{"status":"ok"}` at `/api/v1/system/health`. No business logic yet — just the smallest thing that proves the binary builds and runs.

## Acceptance criteria

- [ ] `go.mod` declares module `github.com/karnstack/tempo`, Go 1.24.
- [ ] Package skeletons exist (with at least one `.go` file each so the package compiles): `internal/{config,server,api,auth,storage,github,ingest,rollup,metrics,webui}`.
- [ ] `cmd/tempo/main.go` boots a `chi` HTTP server on `:8080` (configurable via `TEMPO_LISTEN`).
- [ ] `GET /` returns `200 Hello, tempo`.
- [ ] `GET /api/v1/system/health` returns `200 application/json {"status":"ok","version":"<git-sha-or-dev>"}`.
- [ ] `make build` produces a `./tempo` binary in repo root.
- [ ] `verify.sh` builds the binary, starts it, hits both endpoints, kills it, asserts both 200s.

## Files to touch

- Create: `go.mod`, `go.sum`
- Create: `cmd/tempo/main.go`
- Create: `internal/config/config.go` (stub: `type Config struct { Listen string }` + `Load()` reading `TEMPO_LISTEN` with default `:8080`)
- Create: `internal/server/server.go` (`New(cfg *config.Config) *http.Server` returning a chi-based server with the two routes)
- Create: `internal/server/health.go` (`HealthHandler` returning the JSON above)
- Create: stub files in `internal/{api,auth,storage,github,ingest,rollup,metrics,webui}` — each a one-line `package X` so the dir compiles.
- Modify: `Makefile` — fill in `build`, `test`, `lint`, `fmt`, `clean` targets for the Go side.

## Steps

- [ ] **Step 1 — `go mod init github.com/karnstack/tempo`** and add deps:

  ```bash
  go mod init github.com/karnstack/tempo
  go get github.com/go-chi/chi/v5
  ```

- [ ] **Step 2 — Write `internal/config/config.go`**

  ```go
  package config

  import "os"

  type Config struct {
  	Listen string
  }

  func Load() *Config {
  	return &Config{
  		Listen: getenv("TEMPO_LISTEN", ":8080"),
  	}
  }

  func getenv(key, fallback string) string {
  	if v := os.Getenv(key); v != "" {
  		return v
  	}
  	return fallback
  }
  ```

- [ ] **Step 3 — Write `internal/server/health.go`**

  ```go
  package server

  import (
  	"encoding/json"
  	"net/http"
  )

  // Version is overridden at build time via -ldflags.
  var Version = "dev"

  type healthResponse struct {
  	Status  string `json:"status"`
  	Version string `json:"version"`
  }

  func HealthHandler(w http.ResponseWriter, _ *http.Request) {
  	w.Header().Set("content-type", "application/json")
  	_ = json.NewEncoder(w).Encode(healthResponse{Status: "ok", Version: Version})
  }
  ```

- [ ] **Step 4 — Write `internal/server/server.go`**

  ```go
  package server

  import (
  	"net/http"

  	"github.com/go-chi/chi/v5"
  	"github.com/go-chi/chi/v5/middleware"
  	"github.com/karnstack/tempo/internal/config"
  )

  func New(cfg *config.Config) *http.Server {
  	r := chi.NewRouter()
  	r.Use(middleware.RequestID)
  	r.Use(middleware.Recoverer)

  	r.Get("/", func(w http.ResponseWriter, _ *http.Request) {
  		_, _ = w.Write([]byte("Hello, tempo"))
  	})
  	r.Get("/api/v1/system/health", HealthHandler)

  	return &http.Server{
  		Addr:    cfg.Listen,
  		Handler: r,
  	}
  }
  ```

- [ ] **Step 5 — Write `cmd/tempo/main.go`**

  ```go
  package main

  import (
  	"context"
  	"errors"
  	"log/slog"
  	"net/http"
  	"os"
  	"os/signal"
  	"syscall"
  	"time"

  	"github.com/karnstack/tempo/internal/config"
  	"github.com/karnstack/tempo/internal/server"
  )

  func main() {
  	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
  	cfg := config.Load()
  	srv := server.New(cfg)

  	go func() {
  		logger.Info("tempo starting", "listen", cfg.Listen)
  		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
  			logger.Error("server error", "err", err)
  			os.Exit(1)
  		}
  	}()

  	stop := make(chan os.Signal, 1)
  	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
  	<-stop

  	logger.Info("shutting down")
  	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
  	defer cancel()
  	_ = srv.Shutdown(ctx)
  }
  ```

- [ ] **Step 6 — Stub the remaining internal packages.** For each of `internal/{api,auth,storage,github,ingest,rollup,metrics,webui}/`, create a single `<pkg>.go` with `package <name>` and a TODO doc comment. Example for `internal/api/api.go`:

  ```go
  // Package api hosts the REST handlers under /api/v1.
  package api
  ```

- [ ] **Step 7 — Replace Makefile Go targets** with real bodies:

  ```make
  GO_LDFLAGS = -X github.com/karnstack/tempo/internal/server.Version=$(shell git rev-parse --short HEAD 2>/dev/null || echo dev)

  build: ## Build SPA into web/dist then embed into the Go binary
  	go build -ldflags "$(GO_LDFLAGS)" -o tempo ./cmd/tempo

  test: ## Run all tests (Go + frontend)
  	go test ./...

  lint: ## Run all linters
  	golangci-lint run

  fmt: ## Format Go + frontend
  	go fmt ./...
  	goimports -w .
  ```

  (Frontend portions of these targets land in 0006.)

- [ ] **Step 8 — Run `./verify.sh`** from the task dir.

- [ ] **Step 9 — Commit**

  ```bash
  git add go.mod go.sum cmd internal Makefile
  git commit -m "feat(server): minimal Go server with /system/health (#0002)"
  ```

## Notes

- We're not embedding the SPA yet — `/` returns plain text. Task 0005 adds the embed.
- `chi` is the only HTTP dep for now; we'll add `argon2` etc. in later tasks.
- The version sha trick in `Makefile` is harmless if there's no git history — `git rev-parse` fails, falling back to `dev`.
