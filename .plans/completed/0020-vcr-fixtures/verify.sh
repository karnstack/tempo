#!/usr/bin/env bash
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

echo "==> go vet ./internal/github/..."
go vet ./internal/github/...

echo "==> go build ./internal/github/..."
go build ./internal/github/...

echo "==> go test ./internal/github/vcr/... -count=1 -race"
go test ./internal/github/vcr/... -count=1 -race

echo "==> go test ./internal/github/... -count=1 -race"
go test ./internal/github/... -count=1 -race

echo "==> compile check: -tags=record"
go test -tags=record -run='^$' ./internal/github/vcr/... >/dev/null

echo "OK"
