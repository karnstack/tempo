---
id: 0006
slug: dev-tooling-air
title: Dev tooling — air for Go hot reload + concurrent dev script
status: done
depends_on: [0005]
owner: ""
est_minutes: 25
tags: [bootstrap, dx]
autonomy: full
skills: []
---

## Goal

Make `make dev` run both the Go server (with `air` for hot reload) and the Vite dev server in parallel, with Vite proxying `/api/*` to Go. Also wire up `make fmt`, `make lint`, `make test` for the frontend so contributors have one entry point.

## Acceptance criteria

- [ ] `.air.toml` in repo root tells air to watch `cmd/`, `internal/`, and rebuild `tempo` on change.
- [ ] `web/vite.config.ts` proxies `/api` → `http://localhost:8080`.
- [ ] `make dev` runs `air` and `pnpm -C web dev` concurrently, prints a banner with both URLs (Go: `:8080`, Vite: `:5173`), and shuts both down on Ctrl-C.
- [ ] `make fmt` formats Go (`gofmt`, `goimports`) and frontend (`pnpm -C web fmt` if defined; otherwise prettier directly).
- [ ] `make lint` runs `golangci-lint run` and `pnpm -C web lint` (typecheck + ESLint).
- [ ] `make test` runs `go test ./...` and `pnpm -C web test` if a test script exists (skip gracefully if not).
- [ ] `verify.sh` checks file presence and runs `make -n dev` (dry run) without error.

## Files to touch

- Create: `.air.toml`
- Modify: `web/vite.config.ts` (add `server.proxy`).
- Modify: `Makefile` (`dev`, `fmt`, `lint`, `test` targets).
- Add: `web/package.json` scripts — `lint`, `format`, `typecheck` if not already present (shadcn init usually adds some).

## Steps

- [ ] **Step 1 — `.air.toml`**

  ```toml
  root = "."
  tmp_dir = ".air-tmp"

  [build]
    bin = "./.air-tmp/tempo"
    cmd = "go build -o ./.air-tmp/tempo ./cmd/tempo"
    delay = 500
    exclude_dir = ["web", "node_modules", ".plans", "docs", "data", ".air-tmp", "internal/webui/dist"]
    exclude_regex = ["_test.go$"]
    include_ext = ["go"]
    kill_delay = 500
    log = "build-errors.log"
    send_interrupt = true
    stop_on_error = true

  [color]
    app = "white"
    build = "yellow"
    main = "magenta"
    runner = "green"
    watcher = "cyan"

  [log]
    time = false

  [misc]
    clean_on_exit = true
  ```

  **Note:** in dev, the binary serves whatever is currently in `internal/webui/dist/` — which may be empty. That's fine: in dev, the user opens the Vite URL (5173), not the Go URL. The Vite proxy forwards API calls.

- [ ] **Step 2 — Vite proxy** in `web/vite.config.ts`:

  ```ts
  export default defineConfig({
    plugins: [/* …existing… */],
    resolve: { alias: { '@': path.resolve(__dirname, './src') } },
    server: {
      proxy: {
        '/api': { target: 'http://localhost:8080', changeOrigin: true },
      },
    },
  })
  ```

- [ ] **Step 3 — Update Makefile dev/fmt/lint/test**

  ```make
  dev: ## Run Go (air) + Vite dev servers concurrently
  	@command -v air >/dev/null || (echo "install air: go install github.com/air-verse/air@latest" && exit 1)
  	@echo "  Go    → http://localhost:8080"
  	@echo "  Vite  → http://localhost:5173 (proxies /api → :8080)"
  	@trap 'kill 0' INT TERM; \
  		air & \
  		pnpm -C web dev & \
  		wait

  fmt: ## Format Go + frontend
  	go fmt ./...
  	command -v goimports >/dev/null && goimports -w . || true
  	pnpm -C web exec prettier --write . 2>/dev/null || true

  lint: ## Run all linters
  	golangci-lint run
  	pnpm -C web run lint || true
  	pnpm -C web run typecheck || true

  test: ## Run all tests
  	go test ./...
  	@if pnpm -C web run | grep -q '^  test'; then pnpm -C web test; else echo "(no frontend tests yet)"; fi
  ```

  Note: the `2>/dev/null || true` and conditional `pnpm test` keep this graceful while the frontend test setup hasn't been added yet.

- [ ] **Step 4 — Run `make -n dev`** (dry-run) to confirm the recipe parses and tools are referenced. Then actually run `make dev` interactively to confirm both servers boot.

- [ ] **Step 5 — Run `./verify.sh`.**

- [ ] **Step 6 — Commit**

  ```bash
  git add .air.toml web/vite.config.ts Makefile
  git commit -m "feat(dx): air-based hot reload + concurrent dev script (#0006)"
  ```

## Notes

- `air` is not bundled — contributors install it via `go install github.com/air-verse/air@latest`. The Makefile prints the install command on a missing-tool error.
- The Vite proxy is essential: in dev mode users hit `:5173` for hot frontend reload, but `/api` requests hit the Go backend on `:8080`. In prod they're both the same binary.
- `clean_on_exit = true` deletes `.air-tmp/` on shutdown so we don't accumulate build artifacts.
