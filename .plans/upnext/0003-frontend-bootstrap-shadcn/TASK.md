---
id: 0003
slug: frontend-bootstrap-shadcn
title: Frontend bootstrap via `shadcn init` with preset bcivVNFh
status: pending
depends_on: [0001]
owner: ""
est_minutes: 25
tags: [bootstrap, frontend, shadcn]
autonomy: review
skills: [shadcn]
---

## Goal

Scaffold the `web/` frontend by running shadcn's CLI with the user's preset code. This produces a Vite + React + TS + Tailwind v4 project pre-wired with the shadcn config (`components.json`), the preset's visual tokens, the Base UI primitive library, and lucide-react for icons. After this task, `pnpm -C web dev` should boot the default shadcn starter on `http://localhost:5173`.

This is `autonomy: review` because the visual shell of the app is born here. The user should look at the running starter and confirm the preset looks right before TanStack is layered on (Task 0004).

## Acceptance criteria

- [ ] `web/package.json` exists with Vite + React deps installed via shadcn init.
- [ ] `web/components.json` exists, with `style` and tokens reflecting preset `bcivVNFh`, `base: "base"`, `iconLibrary: "lucide"`, alias `@/`.
- [ ] `web/src/index.css` (or equivalent) carries the Tailwind v4 `@theme inline` block from the preset.
- [ ] `pnpm -C web dev` boots without errors and serves the default shadcn starter at `http://localhost:5173`.
- [ ] `pnpm -C web build` produces `web/dist/`.
- [ ] No node_modules committed; `web/.gitignore` (created by the init) excludes them and dist.
- [ ] `verify.sh` confirms the install + build + presence of `components.json` with the expected `base` field.

## Files to touch

- Create (via CLI): everything under `web/`.
- Modify: `Makefile` — add a `web-install` and `web-dev` target.

## Steps

- [ ] **Step 1 — Run shadcn init** from the repo root:

  ```bash
  pnpm dlx shadcn@latest init --preset bcivVNFh --base base --template vite --name web
  ```

  This creates `web/` with Vite + React + TS + Tailwind v4 + shadcn pre-wired and the preset's tokens applied.

- [ ] **Step 2 — Confirm `web/components.json`** has:
  - `"base": "base"` (or whatever `base: base` produced; should not be `radix`)
  - `"iconLibrary": "lucide"` (default for the preset, but verify)
  - aliases `"components": "@/components"`, `"ui": "@/components/ui"`, `"utils": "@/lib/utils"`, etc.
  - `"tailwind"` block with the v4 `cssVariables: true` setting.

  If anything looks off, re-run init with `--force` or open an issue and pause.

- [ ] **Step 3 — Sanity install + build**

  ```bash
  pnpm -C web install
  pnpm -C web dev   # in another shell
  pnpm -C web build
  ```

  Visit `http://localhost:5173`. The default shadcn starter should render. Stop the dev server.

- [ ] **Step 4 — Update `Makefile`**

  ```make
  web-install: ## Install frontend deps
  	pnpm -C web install --frozen-lockfile

  web-dev: ## Run Vite dev server
  	pnpm -C web dev

  web-build: ## Build SPA into web/dist
  	pnpm -C web build

  # update test target later in 0006 to also run pnpm test if/when added
  ```

- [ ] **Step 5 — Run `./verify.sh`.**

- [ ] **Step 6 — Commit**

  ```bash
  git add web Makefile
  git commit -m "feat(web): scaffold shadcn frontend with preset bcivVNFh (#0003)"
  ```

## Notes

- **Do not decode the preset code.** Pass it directly to `shadcn init` per the shadcn skill rules.
- The shadcn CLI manages everything inside `web/`. Don't hand-write `components.json` or the Tailwind config — let the CLI own them.
- Don't add components yet. They get added on demand in later UI tasks via `pnpm dlx shadcn@latest add <name>`.
- TanStack is **not** in this task. It's layered in 0004.
- Since this is `autonomy: review`, after verify passes, leave the task in `upnext/` and tell the user "open `http://localhost:5173` and confirm the preset looks right". Wait for `/finish-task 0003` before moving to completed.

## Review checklist for the user

When you're looking at the running starter, eyeball:
- [ ] Background and foreground colors look right (dark/light mode toggle if the preset has one).
- [ ] Typography is the preset's, not the Vite default.
- [ ] Component surfaces (buttons, cards) match what `bcivVNFh` is supposed to look like.
- [ ] No console errors.
