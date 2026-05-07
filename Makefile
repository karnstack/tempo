.PHONY: help dev build test lint fmt ci clean

GO_LDFLAGS = -X github.com/karnstack/tempo/internal/version.Version=$(shell git rev-parse --short HEAD 2>/dev/null || echo dev)

help: ## Show this help
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?##/ {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

dev: ## Run Go server + Vite dev server (filled in by Task 0006)
	@echo "dev target not yet implemented — see Task 0006"

build: ## Build the Go binary (SPA embed lands in 0005)
	go build -ldflags "$(GO_LDFLAGS)" -o tempo ./cmd/tempo

test: ## Run all tests (Go + frontend)
	go test ./...

lint: ## Run all linters
	golangci-lint run

fmt: ## Format Go + frontend
	go fmt ./...

ci: lint test build ## Run the same checks as CI

clean: ## Remove build outputs
	rm -rf tempo web/dist .air-tmp data/*.db data/*.db-journal
