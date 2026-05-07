# Result — 0003 Frontend bootstrap (shadcn preset bcivVNFh)

Status: **ready for UI review** (autonomy: review). Verify passes; the SPA
builds and serves cleanly. Hand off to user to eyeball the preset and run
`/finish-task 0003` when satisfied.

## What changed

New `web/` Vite + React 19 + TS + Tailwind v4 + shadcn project (commit `fa3b0dd`).

- `web/components.json` — `style: "base-nova"`, `base: base` (derived),
  `iconLibrary: lucide`, aliases under `@/`.
- `web/src/index.css` — preset `@theme inline` block (129 lines) with Geist
  variable font, zinc base color, neutral chart/menu palette.
- `web/src/components/theme-provider.tsx` — light/dark/system theme provider
  (preset adds the `d` keyboard shortcut to toggle).
- `web/src/components/ui/button.tsx` — `Button` primitive (CVA + Base UI),
  added because the starter `App.tsx` references it.
- `web/src/lib/utils.ts` — `cn()` helper.
- `web/pnpm-workspace.yaml` — `allowBuilds: { esbuild: true }` so `pnpm install`
  doesn't exit with `[ERR_PNPM_IGNORED_BUILDS]`.
- `Makefile` — added `web-install`, `web-dev`, `web-build` targets.
- `.plans/upnext/0003-frontend-bootstrap-shadcn/verify.sh` — replaced the
  literal `"base"` field grep with checks against `style: "base-*"`,
  `iconLibrary: "lucide"`, and `@/` alias presence (shadcn 4.7+ derives base
  from `style`, no top-level `base` key).

## Verify output (last lines)

```
$ tsc -b && vite build
verify ok
```

Build summary:

```
dist/index.html                                           0.46 kB │ gzip:  0.29 kB
dist/assets/index-Mm6Msy92.css                           20.77 kB │ gzip:  4.46 kB
dist/assets/index-r9bah29Z.js                           233.64 kB │ gzip: 73.93 kB
dist/assets/geist-*.woff2                                ~58 kB total
✓ built in 599ms
```

## How to view locally

```bash
make web-dev
# or: pnpm -C web dev
```

Then open <http://localhost:5173>. You'll see the default shadcn "Project
ready!" starter. Press **`d`** to toggle dark mode.

## Review checklist (for the user)

Eyeball before running `/finish-task 0003`:

- [ ] Background and foreground colors match the `bcivVNFh` preset (zinc base,
      Geist sans).
- [ ] Typography is Geist Variable, not Vite/Inter default.
- [ ] `Button` primitive matches the preset's nova treatment.
- [ ] Dark mode toggle works (press `d`).
- [ ] No console errors in DevTools.

## Known follow-ups (not blockers)

- 0004 — TanStack Router + Query layered on top.
- The `@base-ui/react` 1.4 + `lucide-react` 1.14 pin came from the preset
  install; revisit if 0004's TanStack pulls in conflicting peers.
- `shadcn` package was pulled in as a runtime dep by the CLI; harmless, used
  for the `shadcn/tailwind.css` import in `index.css`.
