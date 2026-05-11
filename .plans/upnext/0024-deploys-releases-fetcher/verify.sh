#!/usr/bin/env bash
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

echo "==> go vet ./internal/github/..."
go vet ./internal/github/...

echo "==> go build ./..."
go build ./...

echo "==> go test ./internal/github/deployments/... -race -count=1"
go test ./internal/github/deployments/... -race -count=1

echo "==> go test ./internal/github/releases/... -race -count=1"
go test ./internal/github/releases/... -race -count=1

echo "==> go test ./internal/github/... -race -count=1 (no regressions)"
go test ./internal/github/... -race -count=1

echo "==> compile check: -tags=record ./internal/github/deployments/..."
go test -tags=record -run='^$' ./internal/github/deployments/... >/dev/null

echo "==> compile check: -tags=record ./internal/github/releases/..."
go test -tags=record -run='^$' ./internal/github/releases/... >/dev/null

echo "==> compile check: -tags=gen ./internal/github/deployments/..."
go test -tags=gen -run='^$' ./internal/github/deployments/... >/dev/null

echo "==> compile check: -tags=gen ./internal/github/releases/..."
go test -tags=gen -run='^$' ./internal/github/releases/... >/dev/null

echo "VERIFY OK"
