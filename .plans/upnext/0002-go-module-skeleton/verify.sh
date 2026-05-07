#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../../.."

fail() { echo "VERIFY FAIL: $1" >&2; exit 1; }

[ -f go.mod ] || fail "go.mod missing"
grep -q '^module github.com/karnstack/tempo$' go.mod || fail "go.mod has wrong module path"

# All expected packages exist and compile
for pkg in config server api auth storage github ingest rollup metrics webui; do
  ls internal/"$pkg"/*.go >/dev/null 2>&1 || fail "internal/$pkg has no .go files"
done

# Build
make build >/dev/null
[ -x ./tempo ] || fail "./tempo not built"

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

# Hit endpoints
ROOT=$(curl -fsS "http://127.0.0.1:$PORT/")
[ "$ROOT" = "Hello, tempo" ] || fail "root response wrong: $ROOT"

HEALTH=$(curl -fsS "http://127.0.0.1:$PORT/api/v1/system/health")
echo "$HEALTH" | grep -q '"status":"ok"' || fail "health response wrong: $HEALTH"

echo "verify ok"
