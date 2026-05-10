#!/usr/bin/env bash
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

echo "==> go vet ./internal/auth/... ./internal/api/..."
go vet ./internal/auth/... ./internal/api/...
echo "  ok"

echo "==> go build ./..."
go build ./...
echo "  ok"

echo "==> go test ./internal/auth/... -count=1"
go test ./internal/auth/... -count=1
echo "  ok"

echo "==> go test ./internal/api/... -count=1"
go test ./internal/api/... -count=1
echo "  ok"

echo "==> go test ./... (no regressions)"
go test ./... -count=1
echo "  ok"

echo "VERIFY OK"
