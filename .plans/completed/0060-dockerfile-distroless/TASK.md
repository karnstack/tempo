---
id: 0060
slug: dockerfile-distroless
title: Dockerfile (multi-stage distroless) + docker-compose
status: done
depends_on: [0005]
owner: ""
est_minutes: 45
tags: [deploy, docker]
autonomy: full
skills: []
---

## Goal

Ship a production-grade container image and a `docker compose` flow for local trial. Three deliverables:

1. **Multi-stage `Dockerfile`** — node-builder for the SPA, go-builder for the binary, distroless runtime. Both `tempo` and `migrate` binaries land in the final image. CGo-free (we already use `modernc.org/sqlite`), so the final image runs on `gcr.io/distroless/static-debian12:nonroot` and weighs ~25 MB per the spec.
2. **`docker-compose.yml`** — runs migrations as a one-shot service, then `tempo` with a named `tempo-data` volume mounted at `/data`. Requires `TEMPO_SECRET` in the env (with a friendly error if missing). Companion `.env.example` for the operator.
3. **`.dockerignore`** — keeps the build context lean (no `node_modules`, no `data/`, no built `tempo` binary, no `.git`, no `.plans`).

This task does **not** add CI image-build pipelines (0058/0061) and does **not** push to a registry. It also does **not** change application code — both binaries already exist.

Spec reference: `docs/superpowers/specs/2026-05-08-tempo-design.md`
- line 298 ("a single tempo binary"),
- lines 299–300 ("Docker: multi-stage. Final stage `gcr.io/distroless/static-debian12`, ~25MB image. Volume for `/data` (SQLite + WAL)").

Master-plan row: line 213 of `docs/superpowers/plans/2026-05-08-tempo-implementation.md`.

## Design decisions

- **Base images pinned to exact patch level** (per the user's standing rule on tool versions).
  - `node:24.15.0-alpine3.20` — matches `.mise.toml` (`node = "24.15.0"`).
  - `golang:1.26.3-alpine3.20` — matches `.mise.toml` (`go = "1.26.3"`).
  - `gcr.io/distroless/static-debian12:nonroot` — distroless tags are channel-based, not patch-pinned (Google rebuilds them frequently with security fixes); `:nonroot` is the standard production tag. Pinning to a digest happens in 0061 when we cut the first release.
- **`pnpm` from corepack inside the node stage.** Matches `.mise.toml` (`pnpm = "10.33.4"`). `corepack prepare pnpm@10.33.4 --activate` keeps the pin tight.
- **`CGO_ENABLED=0`, `GOFLAGS=-trimpath`, `-ldflags="-s -w …"`.** Pure-Go SQLite means CGo is off; trimpath + strip flags shave the binary. We keep the existing `-X .../version.Version=…` ldflag (set from `git rev-parse --short HEAD` in the Makefile, defaulted to `docker` inside the container so the image works without a `.git` in context).
- **Both `tempo` and `migrate` binaries shipped in the final image.** Distroless has no shell, so we cannot run an entrypoint script that does `migrate up && tempo`. The supported flows are:
  1. **Compose**: `migrate` runs as a one-shot `service_completed_successfully` dependency of `tempo`.
  2. **Bare `docker run`**: the operator runs `docker run … /migrate up` once, then `docker run … /tempo` as the long-lived container.
  Auto-migration on tempo startup is a future cleanup (touches Go code, not in scope here).
- **`USER nonroot:nonroot` (uid 65532)** in the runtime stage. `/data` is created in the go-builder stage as an empty directory and `COPY --chown=nonroot:nonroot`'d into the final image so SQLite can write it without a chown shim. Named volumes inherit this ownership on first mount.
- **`docker-compose.yml` enforces `TEMPO_SECRET` via shell parameter expansion**: `${TEMPO_SECRET:?Set TEMPO_SECRET — generate with: openssl rand -base64 32}`. Compose aborts with that message if the variable is unset. Companion `.env.example` shows the operator how to provide it.
- **No multi-arch builds in this task.** `docker buildx` and `linux/amd64,linux/arm64` are 0058/CI's job. The Dockerfile is arch-agnostic — `buildx` will work over it later.
- **No `HEALTHCHECK` in the image itself** — distroless lacks `wget` / `curl` and we don't want to ship a healthcheck binary. Compose-level healthchecks (using `restart: unless-stopped` plus an external probe) are sufficient for v1.
- **`.dockerignore` is aggressive.** Excluding `data/`, `tempo` (built binary), `web/dist`, `web/node_modules`, `internal/webui/dist/*`, `.plans`, `.claude`, `.git`, `*.db*` keeps the build context small and prevents stale state from leaking into the image. The Dockerfile rebuilds `web/dist` and `internal/webui/dist` from source inside the build, so excluding them is correct.

## Acceptance criteria

- [ ] `Dockerfile` at repo root with three stages: `web-builder` (node), `go-builder` (golang-alpine), `runtime` (distroless/static).
- [ ] `web-builder` runs `pnpm install --frozen-lockfile` then `pnpm run build`, output at `/app/web/dist`.
- [ ] `go-builder` copies the built SPA into `internal/webui/dist/` (matching `make embed-copy`), runs `go build` for both `./cmd/tempo` and `./cmd/migrate` with `CGO_ENABLED=0`, ldflags `-s -w -X github.com/karnstack/tempo/internal/version.Version=docker`.
- [ ] `runtime` is `gcr.io/distroless/static-debian12:nonroot`. Image contains `/tempo`, `/migrate`, and an empty `/data` directory chowned to nonroot. `WORKDIR /data`, `EXPOSE 8080`, `ENTRYPOINT ["/tempo"]`.
- [ ] `.dockerignore` excludes the items listed in Design decisions.
- [ ] `docker-compose.yml` defines two services (`migrate`, `tempo`) sharing a named volume `tempo-data` at `/data`. `tempo` `depends_on` `migrate` with `condition: service_completed_successfully`. `TEMPO_SECRET` is required via `${VAR:?msg}`. Default env: `TEMPO_DB=sqlite:///data/tempo.db`, `TEMPO_LISTEN=:8080`, `TEMPO_ENV=production`.
- [ ] `.env.example` at repo root with `TEMPO_SECRET=` placeholder + a one-line comment on how to fill it.
- [ ] `Makefile` gains 3 targets: `docker-build`, `docker-up`, `docker-down`.
- [ ] `verify.sh`:
  1. Confirms each file exists.
  2. If `docker buildx` is available, builds the image and asserts the tag exists.
  3. If `docker buildx` is unavailable, prints a WARN line and still exits 0 (so CI without docker doesn't fail this verify).
- [ ] No application code changes. `go build ./...` and the existing test suite still pass (sanity-checked by verify).

## Files to touch

- `Dockerfile` (new)
- `.dockerignore` (new)
- `docker-compose.yml` (new)
- `.env.example` (new)
- `Makefile` (add 3 targets, update `.PHONY`)
- `.plans/upnext/0060-dockerfile-distroless/verify.sh` (replace stub)

## Steps

### 1. `.dockerignore`

```
# Build artifacts
tempo
web/dist/
web/node_modules/
internal/webui/dist/*
!internal/webui/dist/.gitkeep

# Runtime data
data/
*.db
*.db-journal
*.db-wal

# Tooling state
.air-tmp/
.git/
.github/
.plans/
.claude/
.mise.toml
.editorconfig
.golangci.yml

# Docs (not needed at runtime)
docs/

# Local secrets
.env
.env.local
```

### 2. `Dockerfile`

```dockerfile
# syntax=docker/dockerfile:1.7

# ---- stage 1: build the SPA ----
FROM node:24.15.0-alpine3.20 AS web-builder
WORKDIR /app/web

# Activate the pnpm version pinned in .mise.toml
RUN corepack enable && corepack prepare pnpm@10.33.4 --activate

# Copy lockfile first for layer caching
COPY web/package.json web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile

# Build the SPA
COPY web/ ./
RUN pnpm run build

# ---- stage 2: build the Go binaries ----
FROM golang:1.26.3-alpine3.20 AS go-builder
WORKDIR /src

# go.mod / go.sum first for caching
COPY go.mod go.sum ./
RUN go mod download

# Source
COPY . .

# Drop the prebuilt SPA into the embed location
RUN rm -rf internal/webui/dist && mkdir -p internal/webui/dist
COPY --from=web-builder /app/web/dist/ internal/webui/dist/
RUN touch internal/webui/dist/.gitkeep

# Build both binaries. CGo is off (modernc.org/sqlite is pure Go).
ENV CGO_ENABLED=0 GOOS=linux GOFLAGS=-trimpath
RUN go build \
      -ldflags "-s -w -X github.com/karnstack/tempo/internal/version.Version=docker" \
      -o /out/tempo ./cmd/tempo && \
    go build \
      -ldflags "-s -w" \
      -o /out/migrate ./cmd/migrate

# Empty /data dir to be COPY'd into the final image with the right owner.
RUN mkdir -p /out/data

# ---- stage 3: runtime ----
FROM gcr.io/distroless/static-debian12:nonroot AS runtime
WORKDIR /data

COPY --from=go-builder --chown=nonroot:nonroot /out/tempo /tempo
COPY --from=go-builder --chown=nonroot:nonroot /out/migrate /migrate
COPY --from=go-builder --chown=nonroot:nonroot /out/data /data

USER nonroot:nonroot
EXPOSE 8080
VOLUME ["/data"]

ENTRYPOINT ["/tempo"]
```

### 3. `docker-compose.yml`

```yaml
# tempo — local Docker stack.
# Usage:
#   cp .env.example .env       # then set TEMPO_SECRET
#   docker compose up --build
#
# The migrate service runs once on each `up` and exits 0 before tempo starts.

services:
  migrate:
    build: .
    image: tempo:dev
    command: ["/migrate", "up"]
    environment:
      TEMPO_DB: sqlite:///data/tempo.db
      TEMPO_ENV: production
      TEMPO_SECRET: "${TEMPO_SECRET:?Set TEMPO_SECRET — generate with: openssl rand -base64 32}"
    volumes:
      - tempo-data:/data
    restart: "no"

  tempo:
    build: .
    image: tempo:dev
    depends_on:
      migrate:
        condition: service_completed_successfully
    environment:
      TEMPO_DB: sqlite:///data/tempo.db
      TEMPO_LISTEN: ":8080"
      TEMPO_ENV: production
      TEMPO_SECRET: "${TEMPO_SECRET:?Set TEMPO_SECRET — generate with: openssl rand -base64 32}"
      TEMPO_LOG_FORMAT: json
    ports:
      - "8080:8080"
    volumes:
      - tempo-data:/data
    restart: unless-stopped

volumes:
  tempo-data:
```

### 4. `.env.example`

```
# Copy to .env and fill in. docker compose reads .env automatically.
#
# 32 random bytes, base64-encoded. Generate with:
#   openssl rand -base64 32
TEMPO_SECRET=
```

### 5. Makefile additions

Append these targets (and add their names to `.PHONY` at the top):

```makefile
docker-build: ## Build the docker image (tag tempo:dev)
	docker buildx build --load -t tempo:dev .

docker-up: ## Run tempo via docker compose (requires .env with TEMPO_SECRET)
	docker compose up --build

docker-down: ## Stop the docker compose stack and drop the named volume
	docker compose down -v
```

Update the `.PHONY` line at the top to include `docker-build docker-up docker-down`.

### 6. `verify.sh`

```bash
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

# Sanity: Makefile has the new targets
for tgt in docker-build docker-up docker-down; do
  if ! grep -q "^${tgt}:" Makefile; then
    echo "FAIL: Makefile missing target: $tgt" >&2
    exit 1
  fi
  echo "  Makefile target $tgt present"
done

# docker compose syntax check (only if docker compose available)
if command -v docker >/dev/null && docker compose version >/dev/null 2>&1; then
  docker compose -f docker-compose.yml config >/dev/null \
    || { echo "FAIL: docker-compose.yml does not parse" >&2; exit 1; }
  echo "  docker-compose.yml parses"
else
  echo "  WARN: docker compose not available; skipping compose-config check"
fi

# Optional: image build (only if docker buildx is available — keeps CI flexible)
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

# Make sure we did not break the rest of the repo
echo "==> go build ./..."
go build ./...
echo "  ok"

echo "VERIFY OK"
```

`chmod +x verify.sh`.

### 7. Commits

Group cleanly:

1. `feat(docker): multi-stage distroless Dockerfile + .dockerignore`
   - `Dockerfile`, `.dockerignore`
2. `feat(docker): docker compose stack with migrate one-shot`
   - `docker-compose.yml`, `.env.example`
3. `chore(make): docker-build / docker-up / docker-down targets`
   - `Makefile`

Then run verify.

## Notes

- We can't pin the distroless tag to a SemVer because Google maintains them as channels (rebuilt with patches under the same tag). When 0061 cuts a release we should pin to the digest (`gcr.io/distroless/static-debian12@sha256:…`); for now `:nonroot` is correct and idiomatic.
- The `web-builder` stage doesn't share Go's `go.sum` cache, but the `pnpm install` layer is cached on `pnpm-lock.yaml` so SPA-only changes don't reinstall deps.
- `version.Version` is set to the literal string `docker` in the image. A future task can pass it in via `--build-arg VERSION=$(git describe --tags)` and update both the Dockerfile and the Makefile target accordingly. Out of scope here.
- `restart: unless-stopped` on the `tempo` service means a manual `docker compose down` won't auto-restart it (correct), but a docker daemon restart will (also correct). The `migrate` service has `restart: "no"` because it's a one-shot — restarting it would re-run the migration loop pointlessly.
- `.env` (without `.example`) is gitignored already via the `*.env` pattern's absence — verify with `git check-ignore -v .env` before committing if paranoid. We add `.env` to `.dockerignore` regardless.
- The verify script's image-build step takes ~1–2 minutes on a cold cache. Subsequent runs hit the buildx cache and are much faster. CI without docker (some self-hosted runners) silently skips this and still passes.
