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
