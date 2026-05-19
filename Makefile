.PHONY: help dev build embed-copy test lint fmt ci clean web-install web-dev web-build migrate-up migrate-down migrate-status sqlc-generate openapi-validate openapi-check-frontend docker-build docker-up docker-down pre-commit-install

GO_LDFLAGS = -X github.com/karnstack/tempo/internal/version.Version=$(shell git rev-parse --short HEAD 2>/dev/null || echo dev)

help: ## Show this help
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?##/ {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

# Resolve `mise` so this Makefile works in subshells that don't
# inherit zsh/bash's PATH (e.g. when wrappers like portless.sh spawn
# `make` directly). Prefers `mise` on PATH; falls back to the canonical
# install paths if the parent shell only had it via a shell function.
MISE := $(shell \
	if command -v mise >/dev/null 2>&1; then echo mise; \
	elif [ -x "$$HOME/.local/bin/mise" ]; then echo "$$HOME/.local/bin/mise"; \
	elif [ -x /usr/local/bin/mise ]; then echo /usr/local/bin/mise; \
	elif [ -x /opt/homebrew/bin/mise ]; then echo /opt/homebrew/bin/mise; \
	fi)

dev: ## Run Go (air) + Vite dev servers concurrently
	@[ -n "$(MISE)" ] || (echo "install mise: https://mise.jdx.dev/installing-mise.html" && exit 1)
	@echo "  Go    → http://localhost:4811"
	@echo "  Vite  → http://localhost:4810 (proxies /api → :4811)"
	@echo ""
	@echo "  (override via PORT=... / TEMPO_LISTEN=... / VITE_API_TARGET=...)"
	@trap 'kill 0' INT TERM; \
		$(MISE) exec -- air & \
		$(MISE) exec -- pnpm -C web dev & \
		wait

build: web-build embed-copy ## Build SPA, copy into Go embed dir, build binary
	go build -ldflags "$(GO_LDFLAGS)" -o tempo ./cmd/tempo

embed-copy: ## Copy web/dist into internal/webui/dist for //go:embed
	rm -rf internal/webui/dist
	mkdir -p internal/webui/dist
	cp -R web/dist/. internal/webui/dist/
	touch internal/webui/dist/.gitkeep

test: ## Run all tests (Go + frontend)
	go test ./...
	@if pnpm -C web run 2>/dev/null | grep -qE '^[[:space:]]+test'; then pnpm -C web test; else echo "(no frontend tests yet)"; fi

lint: ## Run all linters
	golangci-lint run
	pnpm -C web run lint || true
	pnpm -C web run typecheck || true

fmt: ## Format Go + frontend
	go fmt ./...
	command -v goimports >/dev/null && goimports -w . || true
	pnpm -C web exec prettier --write . 2>/dev/null || true

ci: lint test build ## Run the same checks as CI

clean: ## Remove build outputs
	rm -rf tempo web/dist .air-tmp data/*.db data/*.db-journal
	find internal/webui/dist -mindepth 1 ! -name .gitkeep -delete 2>/dev/null || true

web-install: ## Install frontend deps
	pnpm -C web install --frozen-lockfile

web-dev: ## Run Vite dev server
	pnpm -C web dev

web-build: ## Build SPA into web/dist
	pnpm -C web build

migrate-up: ## Apply all pending DB migrations
	go run ./cmd/migrate up

migrate-down: ## Roll back the latest DB migration
	go run ./cmd/migrate down

migrate-status: ## Show migration status
	go run ./cmd/migrate status

sqlc-generate: ## Regenerate sqlc-typed query bindings
	@command -v sqlc >/dev/null || (echo "install sqlc via 'mise install' (pinned in .mise.toml) or 'go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1'" && exit 1)
	sqlc generate

openapi-validate: ## Validate internal/api/openapi.yaml + check route coverage against the live router
	go test -run TestOpenAPISpec ./internal/api/...

openapi-check-frontend: ## Regenerate the TS client types and diff against the committed copy
	pnpm -C web run openapi:check

docker-build: ## Build the docker image (tag tempo:dev)
	docker buildx build --load -t tempo:dev .

docker-up: ## Run tempo via docker compose (requires .env with TEMPO_SECRET)
	docker compose up --build

docker-down: ## Stop the docker compose stack and drop the named volume
	docker compose down -v

pre-commit-install: ## Install the pre-commit framework's git hook in this clone
	@command -v pre-commit >/dev/null || (echo "install pre-commit: 'pip install pre-commit' or 'brew install pre-commit'" && exit 1)
	pre-commit install
	@echo "  pre-commit hook installed. Run 'pre-commit run --all-files' to check the whole tree."
