# syntax=docker/dockerfile:1.7

# ---- stage 1: build the SPA ----
FROM node:24.15.0-alpine AS web-builder
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
FROM golang:1.26.3-alpine AS go-builder
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
