#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/../../.."

echo "== sqlc diff =="
sqlc diff

echo "== go vet =="
go vet ./...

echo "== go build =="
go build ./...

echo "== go test (api + storage) =="
go test ./internal/api/... ./internal/storage/...
