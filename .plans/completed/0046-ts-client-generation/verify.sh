#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/../../.."

echo "== sqlc diff =="
sqlc diff

echo "== go vet =="
go vet ./...

echo "== go build =="
go build ./...

echo "== go test (api) =="
go test ./internal/api/...

echo "== pnpm typecheck =="
pnpm -C web run typecheck

echo "== openapi:check =="
pnpm -C web run openapi:check
