#!/usr/bin/env bash
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

echo "==> go vet ./internal/auth/... ./internal/api/..."
go vet ./internal/auth/... ./internal/api/...

echo "==> go build ./..."
go build ./...

echo "==> go test ./internal/auth/... ./internal/api/..."
go test ./internal/auth/... ./internal/api/... -count=1

echo "==> go test ./... (no regressions)"
go test ./... -count=1

echo "VERIFY OK"
