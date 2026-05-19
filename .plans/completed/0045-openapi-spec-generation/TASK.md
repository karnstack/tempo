---
id: 0045
slug: openapi-spec-generation
title: OpenAPI 3 spec (hand-rolled)
status: done
depends_on: [0038, 0039, 0040, 0041, 0042, 0043, 0044]
owner: ""
est_minutes: 75
tags: [api, openapi]
autonomy: full
skills: []
---

## Goal

Produce a canonical OpenAPI 3.1 contract for tempo's REST API at
`internal/api/openapi.yaml`, embed it into the binary, and serve it
publicly at `GET /api/openapi.yaml` so the frontend's
`openapi-typescript` generator (0046) can pull it without auth.

Hand-rolled YAML over swaggo annotations: shorter, cleaner
handlers, better round-trip with TS codegen.

## Acceptance criteria

- [ ] `internal/api/openapi.yaml` exists, OpenAPI 3.1.0, covers
      every implemented route:
      - POST /api/v1/auth/{register,login,logout}
      - GET /api/v1/me
      - {GET,POST} /api/v1/tokens, DELETE /api/v1/tokens/{id}
      - {GET,POST} /api/v1/connections, DELETE
        /api/v1/connections/{id}
      - GET /api/v1/repos
      - GET /api/v1/repos/{owner}/{name}/metrics
      - GET /api/v1/orgs/{org}/metrics
      - GET /api/v1/engineers
      - GET /api/v1/engineers/{login}/metrics
      - GET /api/v1/sync/status
      - GET /api/v1/system/health
- [ ] `components/schemas` covers every DTO emitted today, with
      `format: date-time` for all time.Time fields.
- [ ] `internal/api/openapi.go` embeds the YAML and serves
      `GET /api/openapi.yaml` with `Content-Type: application/yaml`,
      public (no auth).
- [ ] `internal/api/openapi_test.go`:
      - Loads the YAML via `getkin/kin-openapi/openapi3`, asserts
        Validate() passes.
      - Builds the real echo router and verifies every
        `/api/v1/...` route has a matching path + method in the
        spec.
- [ ] `internal/api/run.go` wires `configureOpenAPI(e)` before the
      SPA fallback.
- [ ] `Makefile` adds `openapi-validate` target running
      `go test -run TestOpenAPISpec ./internal/api/...`.
- [ ] `go vet`, `go build`, `go test`, `make openapi-validate` pass.
- [ ] `verify.sh` clean.

## Files

- `internal/api/openapi.yaml` (new).
- `internal/api/openapi.go` (new) — embed + serve + route-converter helper.
- `internal/api/openapi_test.go` (new) — validate + route coverage.
- `internal/api/run.go` — wire.
- `go.mod` / `go.sum` — `getkin/kin-openapi`.
- `Makefile`.
- `.plans/upnext/0045-openapi-spec-generation/verify.sh`.

## Steps

1. Author `internal/api/openapi.yaml`. Commit
   `feat(api): hand-rolled OpenAPI 3.1 spec (#0045)`.
2. `internal/api/openapi.go` with `//go:embed openapi.yaml` and
   handler. Commit
   `feat(api): embed and serve openapi.yaml (#0045)`.
3. `go get github.com/getkin/kin-openapi/openapi3`. Write the test.
   Commit `test(api): openapi validates + covers routes (#0045)`.
4. Wire `configureOpenAPI(e)` + Makefile target. Commit.
5. Verify.

## Notes

- Path conversion: echo uses `:param`, OpenAPI uses `{param}`. The
  test's converter handles that.
- `echo.Routes()` includes a couple of framework-internal entries;
  filter to `strings.HasPrefix(path, "/api/v1/")`.
- Schemas hand-written (not reflected): reflection-based codegen
  for OpenAPI 3.1 is brittle on nullable pointers and not worth
  the dep.
