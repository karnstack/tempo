#!/usr/bin/env bash
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

echo "==> go vet ./internal/auth/..."
go vet ./internal/auth/...
echo "  ok"

echo "==> go build ./..."
go build ./...
echo "  ok"

echo "==> go test ./internal/auth/... -count=1"
go test ./internal/auth/... -count=1
echo "  ok"

echo "==> go test ./... (no regressions)"
go test ./... -count=1
echo "  ok"

echo "==> golang.org/x/crypto is a direct dep"
if grep -E '^\s*golang\.org/x/crypto\s+v[0-9]+\.[0-9]+\.[0-9]+\s*$' go.mod >/dev/null; then
  echo "  ok"
else
  echo "FAIL: golang.org/x/crypto is not a direct dep in go.mod" >&2
  grep -n 'golang.org/x/crypto' go.mod >&2 || true
  exit 1
fi

echo "VERIFY OK"
