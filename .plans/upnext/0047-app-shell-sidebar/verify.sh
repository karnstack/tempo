#!/usr/bin/env bash
set -euo pipefail
cd "$(git rev-parse --show-toplevel)/web"

echo "== pnpm typecheck =="
pnpm run typecheck

echo "== pnpm lint =="
pnpm run lint

echo "== pnpm build =="
pnpm run build
