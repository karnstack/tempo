#!/usr/bin/env bash
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

echo "==> sqlc diff"
sqlc diff

echo "==> go vet (rollup, storage, config)"
go vet ./internal/rollup/... ./internal/storage/... ./internal/config/...

echo "==> go build ./..."
go build ./...

echo "==> go test focused"
go test ./internal/rollup/... ./internal/storage/... -race -count=1

echo "==> go test ./... -race -count=1"
go test ./... -race -count=1

echo "VERIFY OK"
