# Result — 0005 embed-spa-in-binary

Status: **done** (autonomy: full).

## What changed

### Added

- `internal/webui/embed.go` — `//go:embed all:dist` plus `Handler() http.Handler`. SPA-fallback rule: any non-`/api/*` path that doesn't match a static file → serve `dist/index.html`. Defensive 404 on `/api/*` so the handler never accidentally swallows API paths even if route order is changed.
- `internal/webui/dist/.gitkeep` — keeps the embed directive valid on a fresh clone before `make build` runs.

### Modified

- `internal/api/run.go` — dropped the placeholder `e.GET("/", "Hello, tempo")`; mounted `e.GET("/*", echo.WrapHandler(webui.Handler()))` **after** `health.Configure`. `/api/*` routes win because they're more specific in echo's radix tree.
- `Makefile` — `build` now chains `web-build → embed-copy → go build`. Added `embed-copy` target (`rm -rf` + `mkdir -p` + `cp -R web/dist/. internal/webui/dist/` + retain `.gitkeep`). `clean` wipes embedded dist contents (preserving `.gitkeep`).
- `.gitignore` — ignore `/internal/webui/dist/` build output, but keep `.gitkeep` tracked.

### Stack note

The original TASK body assumed a `chi` server. tempo runs on `echo/v4 + fx + zap` (per memory + 0002), so Step 4 was rewritten to mount via `e.GET("/*", echo.WrapHandler(...))`. The TASK.md was updated in-place so the record matches reality.

## Verify output (last ~30 lines)

```
…
INFO    fxevent/zap.go:51   started   {"service": "tempo"}
INFO    api/run.go:44       starting tempo api  {"addr": ":18299"}
http server started on [::]:18299
verify ok
INFO    fxevent/zap.go:51   received signal   {"signal": "TERMINATED"}
INFO    api/run.go:52       shutdown signal received
```

`./verify.sh` exercises:
- `make build` (web build + embed copy + go build).
- Boots `./tempo` on a random port, polls until ready.
- `GET /` → starts with `<!DOCTYPE html>` (SPA root).
- `GET /repos/foo/bar` → starts with `<!DOCTYPE html>` (SPA fallback for client-side route).
- `GET /api/v1/system/health` → JSON `"status":"ok"`.
- `GET /assets/<first-asset>` → 200 (static asset served directly).

## Followups

- 0006 (`dev-tooling-air`) layers `air` for Go hot reload + concurrent Vite dev server with `/api/*` proxy.
- The webui handler currently sets no caching headers; tune `Cache-Control` for hashed `assets/*` later if SPA cold-load latency matters.
