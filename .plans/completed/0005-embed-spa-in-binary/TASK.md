---
id: 0005
slug: embed-spa-in-binary
title: Embed the SPA into the Go binary with `//go:embed`
status: done
depends_on: [0002, 0004]
owner: ""
est_minutes: 30
tags: [bootstrap, go, frontend]
autonomy: full
skills: []
---

## Goal

Wire the built SPA (`web/dist/`) into the Go binary via `//go:embed`. The Go server serves the SPA at `/` (and any non-`/api/*` route — SPA fallback to `index.html` so client-side router URLs work on hard reloads). After this task, `make build` produces a single binary that, when run, serves both the API and the full UI.

## Acceptance criteria

- [ ] `internal/webui/embed.go` declares `//go:embed all:dist` (after build copies `web/dist` to `internal/webui/dist`) **OR** uses a build-tag/symlink approach to embed `web/dist` directly. Pick one and document it.
- [ ] `internal/webui` exposes `Handler() http.Handler` that:
  - serves static files from the embedded FS,
  - falls back to `index.html` on 404 for any path that does not start with `/api/`.
- [ ] `internal/api.configureRoutes` mounts the webui handler under `/*` after registering all `/api/*` routes (echo + fx, not chi).
- [ ] Hitting the binary at `/` returns the SPA's `index.html`.
- [ ] Hitting the binary at `/some/spa/path` (a path with no static asset) also returns `index.html` (SPA fallback).
- [ ] Hitting the binary at `/api/v1/system/health` still returns the JSON health response.
- [ ] `make build` chains: `pnpm -C web build` → copy `web/dist` → `internal/webui/dist` → `go build`.
- [ ] `verify.sh` builds the binary, boots it, hits all three endpoints with the expected responses.

## Files to touch

- Create: `internal/webui/embed.go` (the `//go:embed` declaration + `Handler()`).
- Modify: `internal/api/run.go` — drop the placeholder `e.GET("/", "Hello, tempo")` and mount the webui handler at `/*` (after `health.Configure`).
- Modify: `Makefile` — `build` target chains web build + embed copy + go build.
- Update: `.gitignore` — ignore `internal/webui/dist/` (we don't commit the built artifacts).
- Create: `internal/webui/dist/.gitkeep` so the embed directive doesn't fail on a fresh clone before `make build`.

## Steps

- [ ] **Step 1 — Create `internal/webui/embed.go`**

  ```go
  // Package webui serves the embedded SPA. The dist/ directory is populated
  // by `make build` (from web/dist) before `go build` runs.
  package webui

  import (
  	"embed"
  	"io/fs"
  	"net/http"
  	"strings"
  )

  //go:embed all:dist
  var distFS embed.FS

  // Handler serves static SPA assets from the embedded FS. Any path that
  // does not start with /api/ and does not match a static file falls back
  // to /index.html so client-side router URLs work on hard reloads.
  func Handler() http.Handler {
  	sub, err := fs.Sub(distFS, "dist")
  	if err != nil {
  		panic(err) // build-time invariant
  	}
  	fileServer := http.FileServer(http.FS(sub))
  	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
  		if strings.HasPrefix(r.URL.Path, "/api/") {
  			http.NotFound(w, r)
  			return
  		}

  		// Try to serve the requested asset directly.
  		if r.URL.Path != "/" {
  			path := strings.TrimPrefix(r.URL.Path, "/")
  			if _, err := fs.Stat(sub, path); err == nil {
  				fileServer.ServeHTTP(w, r)
  				return
  			}
  		}

  		// Fall back to index.html.
  		r.URL.Path = "/"
  		fileServer.ServeHTTP(w, r)
  	})
  }
  ```

- [ ] **Step 2 — Add a `.gitkeep`** to `internal/webui/dist/` so the embed directive doesn't fail when the dir is empty on a fresh clone.

  ```bash
  mkdir -p internal/webui/dist
  touch internal/webui/dist/.gitkeep
  ```

- [ ] **Step 3 — Update `.gitignore`**

  ```
  /internal/webui/dist/
  !/internal/webui/dist/.gitkeep
  ```

- [ ] **Step 4 — Mount the handler in `internal/api/run.go`**

  Drop the placeholder `e.GET("/", "Hello, tempo")` and add the SPA fallback as the **last** thing `configureRoutes` does, so `/api/*` routes win:

  ```go
  func configureRoutes(e *echo.Echo, l *zap.Logger) {
  	health.Configure(e, l)

  	// SPA fallback — must be last so /api/* takes precedence.
  	e.GET("/*", echo.WrapHandler(webui.Handler()))
  }
  ```

  The webui handler itself defensively 404s on `/api/*` (so it never accidentally swallows API paths even if route registration order is changed later).

- [ ] **Step 5 — Update Makefile `build`**

  ```make
  GO_LDFLAGS = -X github.com/karnstack/tempo/internal/server.Version=$(shell git rev-parse --short HEAD 2>/dev/null || echo dev)

  build: web-build embed-copy ## Build SPA, copy into Go embed dir, build binary
  	go build -ldflags "$(GO_LDFLAGS)" -o tempo ./cmd/tempo

  embed-copy:
  	rm -rf internal/webui/dist
  	mkdir -p internal/webui/dist
  	cp -R web/dist/* internal/webui/dist/
  ```

- [ ] **Step 6 — Test the chain**

  ```bash
  make build
  ./tempo &
  curl -sS http://127.0.0.1:8080/                       | head -c 60
  curl -sS http://127.0.0.1:8080/some/spa/path          | head -c 60
  curl -sS http://127.0.0.1:8080/api/v1/system/health
  ```

  All three should respond. The first two should both be `index.html` (start with `<!DOCTYPE html>`). The third should be JSON.

- [ ] **Step 7 — Run `./verify.sh`.**

- [ ] **Step 8 — Commit**

  ```bash
  git add internal/webui Makefile .gitignore internal/server/server.go
  git commit -m "feat(server): embed SPA via //go:embed with SPA fallback (#0005)"
  ```

## Notes

- We embed from `internal/webui/dist/` rather than `web/dist/` directly so the embed path is stable and predictable, and so the embed directive only needs a relative path inside the package. The Makefile owns the copy.
- SPA fallback rule: any non-`/api/` path that doesn't match a static file → `index.html`. The router then handles client-side routing.
- Hard requirement: `/api/*` is registered BEFORE the webui mount. Don't reverse this — the chi router otherwise lets the SPA handler swallow `/api/v1/...`.
- `embed:all:dist` is required (vs. plain `dist`) so files starting with `_` (like Vite's `_app/` chunks if any) are included.
