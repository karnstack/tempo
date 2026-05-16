#!/usr/bin/env bash
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

echo "==> sqlc diff"
sqlc diff

echo "==> go vet (ingest, storage, config)"
go vet ./internal/ingest/... ./internal/storage/... ./internal/config/...

echo "==> go build ./..."
go build ./...

echo "==> go test focused"
go test ./internal/ingest/... ./internal/storage/... ./internal/config/... -race -count=1

echo "==> go test ./... -race -count=1"
go test ./... -race -count=1

echo "VERIFY OK"
