#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../../.."

fail() { echo "VERIFY FAIL: $1" >&2; exit 1; }

[ -f .air.toml ] || fail ".air.toml missing"
grep -q "cmd/tempo" .air.toml || fail ".air.toml not pointing at cmd/tempo"
grep -q "internal/webui/dist" .air.toml || fail ".air.toml should exclude internal/webui/dist"

# Vite proxy
grep -q "proxy" web/vite.config.ts || fail "vite.config.ts missing proxy block"
grep -q "localhost:8080" web/vite.config.ts || fail "vite proxy not pointing at :8080"

# Make targets
for t in dev fmt lint test; do
  grep -qE "^$t:" Makefile || fail "Makefile missing target: $t"
done

# Dry-run dev (parses) — make -n shouldn't actually run servers
make -n dev >/dev/null || fail "make -n dev fails to parse"

echo "verify ok"
