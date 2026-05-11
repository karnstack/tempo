#!/usr/bin/env bash
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

required=(Dockerfile .dockerignore docker-compose.yml .env.example)
for f in "${required[@]}"; do
  if [[ ! -s "$f" ]]; then
    echo "FAIL: missing or empty: $f" >&2
    exit 1
  fi
  echo "  $f ok"
done

for tgt in docker-build docker-up docker-down; do
  if ! grep -q "^${tgt}:" Makefile; then
    echo "FAIL: Makefile missing target: $tgt" >&2
    exit 1
  fi
  echo "  Makefile target $tgt present"
done

if command -v docker >/dev/null && docker compose version >/dev/null 2>&1; then
  docker compose -f docker-compose.yml config >/dev/null \
    || { echo "FAIL: docker-compose.yml does not parse" >&2; exit 1; }
  echo "  docker-compose.yml parses"
else
  echo "  WARN: docker compose not available; skipping compose-config check"
fi

if command -v docker >/dev/null && docker buildx version >/dev/null 2>&1; then
  echo "==> docker buildx build (this takes ~1-2 minutes)"
  docker buildx build --load -t tempo:verify . >/tmp/tempo-docker-build.log 2>&1 \
    || { echo "FAIL: docker build — see /tmp/tempo-docker-build.log" >&2; tail -40 /tmp/tempo-docker-build.log >&2; exit 1; }
  echo "  build ok"
  size=$(docker image inspect tempo:verify --format '{{.Size}}')
  echo "  image size: $((size / 1024 / 1024)) MiB"
  docker image rm tempo:verify >/dev/null 2>&1 || true
else
  echo "  WARN: docker buildx not available; skipping image build"
fi

echo "==> go build ./..."
go build ./...
echo "  ok"

echo "VERIFY OK"
