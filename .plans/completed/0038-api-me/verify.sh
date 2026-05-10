#!/usr/bin/env bash
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

echo "==> go vet ./..."
go vet ./...
echo "  ok"

echo "==> go build ./..."
go build ./...
echo "  ok"

echo "==> go test ./internal/api/... -race -count=1"
go test ./internal/api/... -race -count=1
echo "  ok"

echo "VERIFY OK"
