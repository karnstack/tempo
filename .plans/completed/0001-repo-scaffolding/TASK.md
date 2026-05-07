---
id: 0001
slug: repo-scaffolding
title: Repo scaffolding (mise, Makefile, LICENSE, basic README, ignore files)
status: done
depends_on: []
owner: ""
est_minutes: 25
tags: [bootstrap]
autonomy: full
skills: []
---

## Goal

Lay down the inert top-level files every contributor needs on day one: toolchain pinning via `mise`, an empty Makefile with the canonical targets, a permissive license, baseline ignore files, an `.editorconfig`, and a one-page README that makes the repo look credible to a first-time visitor.

## Acceptance criteria

- [ ] `.mise.toml` pins Go 1.24.x, Node 24.x, pnpm 10.x.
- [ ] `Makefile` has targets: `dev`, `build`, `test`, `lint`, `fmt`, `ci`, `clean`. Bodies may stub for now (echo "not implemented yet"); they get fleshed out as later tasks land. `make help` lists them.
- [ ] `LICENSE` file with MIT text, year 2026, copyright holder "karnstack contributors".
- [ ] `.gitignore` ignores Go binaries, `dist/`, `node_modules/`, `.DS_Store`, `*.db`, `*.db-journal`, `data/`, `.air-tmp/`.
- [ ] `.editorconfig` with sane defaults (utf-8, lf, 2-space indent for ts/tsx/json/yaml, tab indent for go, trim trailing whitespace, final newline).
- [ ] `README.md` already exists from an earlier hand-written commit; **don't overwrite it** — verify it's still present and unmodified.
- [ ] `.golangci.yml` with a sensible default (errcheck, govet, ineffassign, gosimple, unused, gofmt, goimports). Body can be terse.
- [ ] `verify.sh` runs and exits 0.

## Files to touch

- Create: `.mise.toml`
- Create: `Makefile`
- Create: `LICENSE`
- Create: `.gitignore`
- Create: `.editorconfig`
- Verify only (don't recreate): `README.md`
- Create: `.golangci.yml`

## Steps

- [ ] **Step 1 — Write `.mise.toml`**

  ```toml
  [tools]
  go = "1.24"
  node = "24"
  pnpm = "10"
  ```

- [ ] **Step 2 — Write `Makefile`**

  ```make
  .PHONY: help dev build test lint fmt ci clean

  help: ## Show this help
  	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?##/ {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

  dev: ## Run Go server + Vite dev server (filled in by Task 0006)
  	@echo "dev target not yet implemented — see Task 0006"

  build: ## Build SPA into web/dist then embed into the Go binary
  	@echo "build target not yet implemented — see Task 0005"

  test: ## Run all tests (Go + frontend)
  	@echo "test target not yet implemented"

  lint: ## Run all linters
  	@echo "lint target not yet implemented"

  fmt: ## Format Go + frontend
  	@echo "fmt target not yet implemented"

  ci: lint test build ## Run the same checks as CI

  clean: ## Remove build outputs
  	rm -rf tempo web/dist .air-tmp data/*.db data/*.db-journal
  ```

- [ ] **Step 3 — Write `LICENSE`** — standard MIT text. Year 2026, holder `karnstack contributors`.

- [ ] **Step 4 — Write `.gitignore`**

  ```
  # Go
  /tempo
  *.test
  *.out

  # Node
  node_modules/
  web/dist/
  .pnpm-store/

  # SQLite (dev)
  data/
  *.db
  *.db-journal
  *.db-wal
  *.db-shm

  # Tooling scratch
  .air-tmp/
  .vscode/
  .idea/
  .DS_Store

  # Local env
  .env
  .env.local
  ```

- [ ] **Step 5 — Write `.editorconfig`**

  ```ini
  root = true

  [*]
  charset = utf-8
  end_of_line = lf
  insert_final_newline = true
  trim_trailing_whitespace = true
  indent_style = space
  indent_size = 2

  [*.go]
  indent_style = tab

  [Makefile]
  indent_style = tab

  [*.md]
  trim_trailing_whitespace = false
  ```

- [ ] **Step 6 — Write `.golangci.yml`**

  ```yaml
  run:
    timeout: 3m

  linters:
    enable:
      - errcheck
      - govet
      - ineffassign
      - gosimple
      - unused
      - gofmt
      - goimports
      - staticcheck

  issues:
    exclude-use-default: false
  ```

- [ ] **Step 7 — Verify `README.md`** is still present (committed earlier by hand). Don't rewrite it.

- [ ] **Step 8 — Run `./verify.sh`** to confirm all files exist with non-empty content and the Makefile parses.

- [ ] **Step 9 — Commit**

  ```bash
  git add .mise.toml Makefile LICENSE .gitignore .editorconfig .golangci.yml
  git commit -m "chore: scaffold repo (mise, Makefile, LICENSE) (#0001)"
  ```

## Notes

- The Makefile stubs are intentional. Later tasks (0005, 0006) replace them with real bodies. `make ci` already chains `lint test build`, so when those targets become real, CI follows.
- `.golangci.yml` is conservative on purpose. Tighten when there's a baseline of code to lint against.
- Don't add `.github/` files yet — that's Task 0057.
