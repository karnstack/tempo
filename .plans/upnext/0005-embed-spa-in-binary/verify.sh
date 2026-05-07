#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../../.."

fail() { echo "VERIFY FAIL: $1" >&2; exit 1; }

[ -f internal/webui/embed.go ] || fail "internal/webui/embed.go missing"
grep -q "//go:embed all:dist" internal/webui/embed.go || fail "embed directive missing"
grep -q "\"/api/\"" internal/webui/embed.go || fail "API path guard missing in webui handler"

# Build chain
make build >/dev/null
[ -x ./tempo ] || fail "./tempo not built"
[ -f web/dist/index.html ] || fail "web/dist/index.html missing"
[ -f internal/webui/dist/index.html ] || fail "internal/webui/dist/index.html missing (embed-copy didn't run)"

# Boot it on a random port
PORT=18${RANDOM:0:3}
TEMPO_LISTEN=":$PORT" ./tempo &
PID=$!
trap 'kill $PID 2>/dev/null || true' EXIT

# Wait for server up
for i in $(seq 1 30); do
  if curl -fsS "http://127.0.0.1:$PORT/api/v1/system/health" >/dev/null 2>&1; then
    break
  fi
  sleep 0.1
done

# Root → SPA index
ROOT=$(curl -fsS "http://127.0.0.1:$PORT/" | head -c 100)
echo "$ROOT" | grep -qi '<!DOCTYPE html>' || fail "root should serve SPA index.html, got: $ROOT"

# Arbitrary SPA path → fallback to index.html
SPA_FALLBACK=$(curl -fsS "http://127.0.0.1:$PORT/repos/foo/bar" | head -c 100)
echo "$SPA_FALLBACK" | grep -qi '<!DOCTYPE html>' || fail "SPA fallback failed for /repos/foo/bar"

# Health endpoint still json
HEALTH=$(curl -fsS "http://127.0.0.1:$PORT/api/v1/system/health")
echo "$HEALTH" | grep -q '"status":"ok"' || fail "health endpoint broken: $HEALTH"

# Static asset (favicon or any asset under web/dist) served directly
# Find any asset under dist/assets/ if it exists
ASSET=$(find internal/webui/dist -type f -path '*/assets/*' | head -n1 || true)
if [ -n "$ASSET" ]; then
  REL=${ASSET#internal/webui/dist/}
  curl -fsS "http://127.0.0.1:$PORT/$REL" >/dev/null || fail "static asset 404: $REL"
fi

echo "verify ok"
