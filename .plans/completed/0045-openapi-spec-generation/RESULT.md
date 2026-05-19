# 0045 — OpenAPI 3.1 spec generation

## Files changed

- `internal/api/openapi.yaml` — canonical OpenAPI 3.1.0 contract
  covering 18 routes (auth firstrun/register/login/logout, me,
  tokens CRUD, connections CRUD, repos list + metrics, orgs
  metrics, engineers list + metrics, sync status, system health)
  plus a full `components/schemas` map for every DTO.
- `internal/api/openapi.go` — embeds the YAML via `//go:embed` and
  mounts `GET /api/openapi.yaml` publicly with
  `Content-Type: application/yaml`.
- `internal/api/openapi_test.go` — validates the YAML via
  `getkin/kin-openapi` and checks every echo `/api/v1/...` route
  has a matching path+method in the spec.
- `internal/api/run.go` — wires `configureOpenAPI(e)`.
- `Makefile` — adds `openapi-validate` target.
- `go.mod`/`go.sum` — `github.com/getkin/kin-openapi`.

## Verify output

```
== sqlc diff ==
== go vet ==
== go build ==
== go test (api) ==
... all api tests OK
== openapi-validate ==
ok  internal/api (TestOpenAPISpec_*)
```

## Notes / followups

- **Hand-rolled, not swaggo.** Cleaner handlers, shorter total
  surface (~860 lines of YAML), better round-trip with
  openapi-typescript. The route-coverage test is the drift guard.
- **OpenAPI 3.1 over 3.0** — direct nullable union types
  (`type: [integer, "null"]`) instead of the `nullable: true` shim
  3.0 needed.
- **/api/openapi.yaml is public.** No auth so codegen tools and
  docs renderers (Redocly, swagger-ui) can pull it without a
  session.
- **Test caught a missed route.** The first run of
  `CoversAllRoutes` flagged `/auth/firstrun` missing from the
  spec; added it before committing. This is the value of the
  drift check.
- **`*` route filter.** Echo registers a 404 fallback under
  `/api/v1/*` for unmatched sub-routes; the test filters those
  via `strings.HasSuffix(path, "/*")`.
