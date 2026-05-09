#!/usr/bin/env bash
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

echo "==> go vet ./internal/logger/... ./internal/api/..."
go vet ./internal/logger/... ./internal/api/...
echo "  ok"

echo "==> go build ./..."
go build ./...
echo "  ok"

echo "==> go test ./internal/logger/... -count=1"
go test ./internal/logger/... -count=1
echo "  ok"

echo "==> go test ./internal/api/... -count=1 (covers request logger middleware)"
go test ./internal/api/... -count=1
echo "  ok"

echo "==> go test ./... (no regressions)"
go test ./...
echo "  ok"

echo "==> dev boot smoke: tempo starts and emits an access log on /api/v1/system/health"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
LOG="$TMP/boot.log"
( go run ./cmd/tempo >"$LOG" 2>&1 & echo $! > "$TMP/pid" )

# Wait for the api banner before curl-ing.
for _ in $(seq 1 30); do
  if grep -q "starting tempo api" "$LOG"; then break; fi
  sleep 0.2
done

curl -sf http://127.0.0.1:8080/api/v1/system/health >/dev/null || true
sleep 0.3

PID=$(cat "$TMP/pid")
kill "$PID" 2>/dev/null || true
wait "$PID" 2>/dev/null || true

if ! grep -q "starting tempo api" "$LOG"; then
  echo "FAIL: tempo dev boot did not reach api start" >&2
  tail -50 "$LOG" >&2
  exit 1
fi

# The access log line tags every request with method GET and path /api/v1/system/health.
if ! grep -E '"path":"/api/v1/system/health"|path=/api/v1/system/health' "$LOG" >/dev/null; then
  if ! grep -E 'GET\s+/api/v1/system/health' "$LOG" >/dev/null; then
    echo "FAIL: no per-request access log line for /api/v1/system/health" >&2
    tail -80 "$LOG" >&2
    exit 1
  fi
fi
echo "  ok"

echo "VERIFY OK"
