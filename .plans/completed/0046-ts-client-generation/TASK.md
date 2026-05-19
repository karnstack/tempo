---
id: 0046
slug: ts-client-generation
title: Generate TS client into web/src/lib/api.ts
status: done
depends_on: [0045]
owner: ""
est_minutes: 45
tags: [frontend, openapi]
autonomy: full
skills: []
---

## Goal

Wire `openapi-typescript` into the frontend toolchain. Generated
types at `web/src/lib/openapi.d.ts`; thin typed `apiClient` wrapper
at `web/src/lib/api.ts` over fetch.

`pnpm run openapi:check` regenerates and diffs against the
committed copy so spec drift fails CI.

## Acceptance criteria

- [ ] `web/package.json` adds `openapi-typescript@^7` devDep +
      `openapi:generate` / `openapi:check` scripts.
- [ ] `web/src/lib/openapi.d.ts` committed.
- [ ] `web/src/lib/api.ts` exposes typed fetch helpers.
- [ ] `pnpm -C web run openapi:check` exits 0 when in sync.
- [ ] `pnpm -C web run typecheck` passes.
- [ ] `Makefile` adds `openapi-check-frontend` target.
- [ ] verify.sh: backend + typecheck + openapi:check.

## Files

- `web/package.json`, `web/pnpm-lock.yaml`.
- `web/src/lib/openapi.d.ts` (generated, committed).
- `web/src/lib/api.ts` (new).
- `Makefile`.

## Steps

1. Install + scripts.
2. Typed fetch wrapper.
3. Makefile target.
4. Verify.

## Notes

- No runtime validation; we trust the spec.
- Session cookie auth via `credentials: 'same-origin'`.
