#!/usr/bin/env bash
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

echo "==> go vet ./internal/github/..."
go vet ./internal/github/...
echo "  ok"

echo "==> go build ./..."
go build ./...
echo "  ok"

echo "==> go test ./internal/github/... -race -count=1"
go test ./internal/github/... -race -count=1
echo "  ok"

echo "==> go test ./... (no regressions)"
go test ./... -race -count=1
echo "  ok"

echo "VERIFY OK"
