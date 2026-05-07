#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../../.."

fail() { echo "VERIFY FAIL: $1" >&2; exit 1; }

for f in .mise.toml Makefile LICENSE .gitignore .editorconfig README.md .golangci.yml; do
  [ -s "$f" ] || fail "$f missing or empty"
done

# README is hand-written; just sanity check it has the title
grep -q '^# tempo' README.md || fail "README.md missing # tempo title"

# .mise.toml has the three tools
grep -q '^go ' .mise.toml || fail ".mise.toml missing go pin"
grep -q '^node ' .mise.toml || fail ".mise.toml missing node pin"
grep -q '^pnpm ' .mise.toml || fail ".mise.toml missing pnpm pin"

# Makefile has the canonical targets
for t in help dev build test lint fmt ci clean; do
  grep -qE "^$t:" Makefile || fail "Makefile missing target: $t"
done

# Make is happy with our Makefile
make -n help >/dev/null || fail "Makefile failed to parse (make -n help)"

# LICENSE looks like MIT
grep -qi "MIT License" LICENSE || fail "LICENSE doesn't look like MIT"
grep -q "karnstack" LICENSE || fail "LICENSE missing copyright holder"

# golangci config parses (only if golangci-lint is installed)
if command -v golangci-lint >/dev/null; then
  golangci-lint config -c .golangci.yml >/dev/null 2>&1 || fail ".golangci.yml is invalid"
fi

echo "verify ok"
