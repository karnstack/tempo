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
LOG="$TMP/boot.log"
BIN="$TMP/tempo"

# Build a binary into TMP so we own a single PID — `go run` forks the
# compiled binary as a separate child, which makes clean teardown awkward.
go build -o "$BIN" ./cmd/tempo

cleanup() {
  if [[ -f "$TMP/pid" ]]; then
    PID=$(cat "$TMP/pid")
    kill "$PID" 2>/dev/null || true
    # Give it a moment to release the port, then SIGKILL if still alive.
    for _ in $(seq 1 10); do
      kill -0 "$PID" 2>/dev/null || break
      sleep 0.1
    done
    kill -9 "$PID" 2>/dev/null || true
    wait "$PID" 2>/dev/null || true
  fi
  rm -rf "$TMP"
}
trap cleanup EXIT

"$BIN" >"$LOG" 2>&1 &
echo $! >"$TMP/pid"

# Wait for the api banner before curl-ing.
for _ in $(seq 1 30); do
  if grep -q "starting tempo api" "$LOG"; then break; fi
  sleep 0.2
done

curl -sf http://127.0.0.1:8080/api/v1/system/health >/dev/null || true
sleep 0.3

if ! grep -q "starting tempo api" "$LOG"; then
  echo "FAIL: tempo dev boot did not reach api start" >&2
  tail -50 "$LOG" >&2
  exit 1
fi

# The access log line tags every request with method GET and path
# /api/v1/system/health. Console format (the dev default) uses
# "path": "...", so the regex tolerates whitespace around the colon.
if ! grep -E '"path"[[:space:]]*:[[:space:]]*"/api/v1/system/health"' "$LOG" >/dev/null; then
  echo "FAIL: no per-request access log line for /api/v1/system/health" >&2
  tail -80 "$LOG" >&2
  exit 1
fi

# And the trace_id field must appear on at least one entry.
if ! grep -q '"trace_id"' "$LOG"; then
  echo "FAIL: no trace_id field in boot log" >&2
  tail -80 "$LOG" >&2
  exit 1
fi
echo "  ok"

echo "VERIFY OK"
