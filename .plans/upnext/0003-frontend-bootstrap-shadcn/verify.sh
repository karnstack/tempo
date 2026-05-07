#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../../.."

fail() { echo "VERIFY FAIL: $1" >&2; exit 1; }

[ -d web ] || fail "web/ dir missing"
[ -f web/package.json ] || fail "web/package.json missing"
[ -f web/components.json ] || fail "web/components.json missing (shadcn init didn't run?)"
[ -f web/vite.config.ts ] || fail "web/vite.config.ts missing"

# components.json reflects our setup
grep -q '"base"' web/components.json || fail "components.json has no base field"
# Should be "base" not "radix"
if grep -q '"base": *"radix"' web/components.json; then
  fail "components.json has base=radix but spec requires base=base"
fi

# Make targets
for t in web-install web-dev web-build; do
  grep -qE "^$t:" Makefile || fail "Makefile missing target: $t"
done

# Frontend builds
pnpm -C web install --frozen-lockfile >/dev/null
pnpm -C web build >/dev/null
[ -d web/dist ] || fail "web/dist not produced by build"
[ -f web/dist/index.html ] || fail "web/dist/index.html missing"

echo "verify ok"
