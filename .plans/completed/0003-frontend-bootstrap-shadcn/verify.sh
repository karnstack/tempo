#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../../.."

fail() { echo "VERIFY FAIL: $1" >&2; exit 1; }

[ -d web ] || fail "web/ dir missing"
[ -f web/package.json ] || fail "web/package.json missing"
[ -f web/components.json ] || fail "web/components.json missing (shadcn init didn't run?)"
[ -f web/vite.config.ts ] || fail "web/vite.config.ts missing"

# components.json reflects our setup. shadcn 4.7+ encodes the base via the
# style field ("base-<name>") rather than a top-level "base" key, so we check
# style instead. The spec requires base=base — i.e. style must start with "base-".
grep -qE '"style": *"base-' web/components.json || fail "components.json style field is not a base-* style (spec requires base=base)"
grep -qE '"iconLibrary": *"lucide"' web/components.json || fail "components.json iconLibrary is not lucide"
grep -q '"@/components"' web/components.json || fail "components.json missing @/ alias"

# Make targets
for t in web-install web-dev web-build; do
  grep -qE "^$t:" Makefile || fail "Makefile missing target: $t"
done

# Frontend builds. CI=true so pnpm doesn't prompt for module-dir purge under
# non-interactive bash (no TTY).
CI=true pnpm -C web install --frozen-lockfile >/dev/null
pnpm -C web build >/dev/null
[ -d web/dist ] || fail "web/dist not produced by build"
[ -f web/dist/index.html ] || fail "web/dist/index.html missing"

echo "verify ok"
