#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
cd "$REPO_ROOT/web"

echo "== pnpm typecheck =="
pnpm typecheck

echo "== pnpm lint =="
pnpm lint

echo "== pnpm build =="
pnpm build
