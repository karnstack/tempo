#!/usr/bin/env bash
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

echo "==> go vet ./internal/github/..."
go vet ./internal/github/...

echo "==> go build ./..."
go build ./...

echo "==> go test ./internal/github/prs/... -race -count=1"
go test ./internal/github/prs/... -race -count=1

echo "==> go test ./internal/github/... -race -count=1 (no regressions)"
go test ./internal/github/... -race -count=1

echo "==> compile check: -tags=record ./internal/github/prs/..."
go test -tags=record -run='^$' ./internal/github/prs/... >/dev/null

echo "VERIFY OK"
