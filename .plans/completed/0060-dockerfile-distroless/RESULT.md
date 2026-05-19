# 0060 — Dockerfile (multi-stage distroless) + docker-compose

## Files changed

- `Dockerfile` — three-stage build (`web-builder` → `go-builder` →
  `gcr.io/distroless/static-debian12:nonroot`).
- `.dockerignore` — keeps the build context lean.
- `docker-compose.yml` — `migrate` one-shot + `tempo` long-lived,
  sharing a `tempo-data` named volume at `/data`.
- `.env.example` — `TEMPO_SECRET=` placeholder with the openssl
  generation one-liner.
- `Makefile` — `docker-build` / `docker-up` / `docker-down`
  targets.

## Verify output

```
  Dockerfile ok
  .dockerignore ok
  docker-compose.yml ok
  .env.example ok
  Makefile target docker-build present
  Makefile target docker-up present
  Makefile target docker-down present
  docker-compose.yml parses
==> docker buildx build (this takes ~1-2 minutes)
  build ok
  image size: 22 MiB
==> go build ./...
  ok
VERIFY OK
```

## Notes / followups

- **Image size: 22 MiB**, under the spec's "~25 MB" target. Both
  `/tempo` and `/migrate` binaries fit because we strip symbols
  (`-s -w`) and stay CGo-free.
- **Base-image pin caveat.** The TASK body assumed
  `node:24.15.0-alpine3.20` / `golang:1.26.3-alpine3.20` would
  exist; Docker Hub only publishes the alpine-version-suffixed
  tags for some patch combinations. Dropped to
  `node:24.15.0-alpine` / `golang:1.26.3-alpine` (still pins the
  language patch level, lets Alpine track its own latest). When
  0061 cuts a release we should pin to image digests.
- **compose-config check needs a dummy `TEMPO_SECRET`.** The
  `${TEMPO_SECRET:?...}` guard fires during `compose config`
  interpolation too, so verify.sh sets `TEMPO_SECRET=dummy` only
  for the parse check.
- **No multi-arch yet.** `docker buildx` will work over the same
  Dockerfile when 0058/0061 adds `--platform linux/amd64,linux/arm64`
  to the CI build.
- **No HEALTHCHECK in the image** — distroless lacks the tools to
  implement one. `restart: unless-stopped` plus an external probe
  is sufficient for v1.
- **Auto-migration on tempo startup** is a future cleanup; for now
  the compose flow runs `/migrate up` as a one-shot before tempo
  starts.
