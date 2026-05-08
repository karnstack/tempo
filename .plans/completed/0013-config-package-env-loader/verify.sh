#!/usr/bin/env bash
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

echo "==> go vet ./internal/config/..."
go vet ./internal/config/...
echo "  ok"

echo "==> go build ./..."
go build ./...
echo "  ok"

echo "==> go test ./internal/config/... -count=1 (covers TEMPO_SECRET, validation aggregation, defaults, TZ, helpers)"
go test ./internal/config/... -count=1
echo "  ok"

echo "==> go test ./... (no regressions outside config)"
go test ./...
echo "  ok"

echo "==> dev boot smoke: tempo starts, surfaces SecretWarning, opens port 8080"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
LOG="$TMP/boot.log"
( go run ./cmd/tempo >"$LOG" 2>&1 & echo $! > "$TMP/pid" )
sleep 2
PID=$(cat "$TMP/pid")
kill "$PID" 2>/dev/null || true
wait "$PID" 2>/dev/null || true

if ! grep -q "TEMPO_SECRET unset" "$LOG"; then
  echo "FAIL: tempo dev boot did not surface SecretWarning" >&2
  tail -50 "$LOG" >&2
  exit 1
fi
if ! grep -q "starting tempo api" "$LOG"; then
  echo "FAIL: tempo dev boot did not reach api start" >&2
  tail -50 "$LOG" >&2
  exit 1
fi
echo "  ok"

echo "VERIFY OK"
