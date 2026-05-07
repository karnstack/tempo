.PHONY: help dev build embed-copy test lint fmt ci clean web-install web-dev web-build

GO_LDFLAGS = -X github.com/karnstack/tempo/internal/version.Version=$(shell git rev-parse --short HEAD 2>/dev/null || echo dev)

help: ## Show this help
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?##/ {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

dev: ## Run Go server + Vite dev server (filled in by Task 0006)
	@echo "dev target not yet implemented — see Task 0006"

build: web-build embed-copy ## Build SPA, copy into Go embed dir, build binary
	go build -ldflags "$(GO_LDFLAGS)" -o tempo ./cmd/tempo

embed-copy: ## Copy web/dist into internal/webui/dist for //go:embed
	rm -rf internal/webui/dist
	mkdir -p internal/webui/dist
	cp -R web/dist/. internal/webui/dist/
	touch internal/webui/dist/.gitkeep

test: ## Run all tests (Go + frontend)
	go test ./...

lint: ## Run all linters
	golangci-lint run

fmt: ## Format Go + frontend
	go fmt ./...

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
