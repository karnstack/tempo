#!/usr/bin/env bash
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

echo "==> sqlc diff (verify generated SQL bindings are in sync)"
sqlc diff

echo "==> go vet ./internal/ingest/..."
go vet ./internal/ingest/...

echo "==> go build ./..."
go build ./...

echo "==> go test ./internal/ingest/... -race -count=1"
go test ./internal/ingest/... -race -count=1

echo "==> go test ./... -race -count=1 (no regressions)"
go test ./... -race -count=1

echo "VERIFY OK"
