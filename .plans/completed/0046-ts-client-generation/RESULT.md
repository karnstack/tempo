# 0046 — TS client generation

## Files changed

- `web/package.json` + `web/pnpm-lock.yaml` — adds
  `openapi-typescript@^7` devDep + `openapi:generate` /
  `openapi:check` scripts.
- `web/src/lib/openapi.d.ts` — generated `paths` / `components`
  namespaces (committed; regenerated via the script).
- `web/src/lib/api.ts` — typed fetch wrapper. Exposes `apiFetch`,
  `apiGet`, `apiPost`, `apiDelete` with compile-time-checked path,
  query, body, and response types parameterised on the
  `openapi.d.ts` paths union.
- `Makefile` — `openapi-check-frontend` target wrapping
  `pnpm -C web run openapi:check`.

## Verify output

```
== sqlc diff ==
== go vet ==
== go build ==
== go test (api) ==
== pnpm typecheck ==
== openapi:check ==
✨ openapi-typescript 7.13.0
🚀 ../internal/api/openapi.yaml → /tmp/tempo-openapi-check.d.ts [36ms]
```

(diff is empty when in sync; the script exits 0)

## Notes / followups

- **Single source of truth.** `internal/api/openapi.yaml` is the
  contract. Go-side coverage test (0045) guards routes; TS-side
  drift check guards types. Any new endpoint needs both files
  updated.
- **No runtime validation.** Zod/io-ts would double the bundle for
  no benefit on an internal app. TanStack Query handles retry +
  staleness one layer up; the typed wrapper handles compile-time
  correctness.
- **`ApiError`** carries `status` + parsed body so handlers can
  branch on 4xx semantics without re-parsing.
- **Generic helper boilerplate.** The `apiGet` / `apiPost` /
  `apiDelete` wrappers cast through `"get" & Methods<P>` so TS
  narrows the method to the path's declared verbs. Without that
  intersection, the inferred result type widens to `unknown`.
- **CI plumbing.** 0058 (GitHub Actions) will run
  `make openapi-check-frontend` + `make openapi-validate` to lock
  both ends.
