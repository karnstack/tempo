---
id: 0004
slug: tanstack-router-query
title: Layer TanStack Router + Query on top of the shadcn starter
status: in_progress
depends_on: [0003]
owner: ""
est_minutes: 30
tags: [bootstrap, frontend, tanstack]
autonomy: review
skills: [shadcn]
---

## Goal

Add TanStack Router (file-based) and TanStack Query to the `web/` project, replacing the default starter with a `RouterProvider` driven by an auto-generated `routeTree.gen.ts`. After this, navigating to `/`, `/about`, etc. resolves through TanStack Router; React Query's QueryClientProvider wraps the tree; one minimal `index` route renders so we can confirm routing works.

This stays `autonomy: review` because the user should sanity-check the running app before moving on to embedding (0005).

## Acceptance criteria

- [ ] `web/package.json` has runtime deps `@tanstack/react-router`, `@tanstack/react-query`.
- [ ] `web/package.json` has dev deps `@tanstack/router-plugin`, `@tanstack/router-devtools`, `@tanstack/react-query-devtools`.
- [ ] `web/vite.config.ts` includes `tanstackRouter()` plugin (auto-generates `web/src/routeTree.gen.ts`).
- [ ] `web/src/routes/__root.tsx` defines the root route (`createRootRoute`) with `<Outlet/>` and devtools.
- [ ] `web/src/routes/index.tsx` defines a minimal index route that renders "tempo" + a Button from shadcn (proves the chain: shadcn → router → query).
- [ ] `web/src/main.tsx` builds a router from the generated tree and wraps it in `QueryClientProvider`.
- [ ] `pnpm -C web dev` serves the index route at `/`. No console errors.
- [ ] `pnpm -C web build` succeeds.
- [ ] `verify.sh` runs install + build + checks for the generated `routeTree.gen.ts` and the plugin in `vite.config.ts`.

## Files to touch

- Modify: `web/package.json` (deps via `pnpm add`)
- Modify: `web/vite.config.ts` (add the router plugin)
- Modify: `web/src/main.tsx` (replace App with RouterProvider + QueryClientProvider)
- Create: `web/src/routes/__root.tsx`
- Create: `web/src/routes/index.tsx`
- Add the Button component: `pnpm -C web dlx shadcn@latest add button` (managed via shadcn CLI).
- Generated (do not commit by hand): `web/src/routeTree.gen.ts` — produced by the plugin. Add to `.eslintignore`/`.prettierignore` if those exist.

## Steps

- [ ] **Step 1 — Install deps**

  ```bash
  pnpm -C web add @tanstack/react-router @tanstack/react-query
  pnpm -C web add -D @tanstack/router-plugin @tanstack/router-devtools @tanstack/react-query-devtools
  ```

- [ ] **Step 2 — Update `web/vite.config.ts`** to include the router plugin BEFORE `react()`:

  ```ts
  import { defineConfig } from 'vite'
  import react from '@vitejs/plugin-react'
  import tailwindcss from '@tailwindcss/vite'
  import { tanstackRouter } from '@tanstack/router-plugin/vite'
  import path from 'node:path'

  export default defineConfig({
    plugins: [
      tanstackRouter({ target: 'react', autoCodeSplitting: true }),
      react(),
      tailwindcss(),
    ],
    resolve: {
      alias: { '@': path.resolve(__dirname, './src') },
    },
  })
  ```

  (Adjust to match whatever the shadcn-init-vite config already has — keep the existing tailwind plugin and alias; just add `tanstackRouter()` first.)

- [ ] **Step 3 — Add the Button component**

  ```bash
  pnpm -C web dlx shadcn@latest add button
  ```

- [ ] **Step 4 — Create `web/src/routes/__root.tsx`**

  ```tsx
  import { Outlet, createRootRoute } from '@tanstack/react-router'
  import { TanStackRouterDevtools } from '@tanstack/router-devtools'
  import { ReactQueryDevtools } from '@tanstack/react-query-devtools'

  export const Route = createRootRoute({
    component: () => (
      <>
        <Outlet />
        {import.meta.env.DEV && (
          <>
            <TanStackRouterDevtools position="bottom-right" />
            <ReactQueryDevtools buttonPosition="bottom-left" />
          </>
        )}
      </>
    ),
  })
  ```

- [ ] **Step 5 — Create `web/src/routes/index.tsx`**

  ```tsx
  import { createFileRoute } from '@tanstack/react-router'
  import { Button } from '@/components/ui/button'

  export const Route = createFileRoute('/')({
    component: Home,
  })

  function Home() {
    return (
      <main className="flex min-h-screen flex-col items-center justify-center gap-6">
        <h1 className="text-4xl font-semibold tracking-tight">tempo</h1>
        <p className="text-muted-foreground">engineering metrics for github</p>
        <Button>Hello</Button>
      </main>
    )
  }
  ```

- [ ] **Step 6 — Replace `web/src/main.tsx`**

  ```tsx
  import { StrictMode } from 'react'
  import { createRoot } from 'react-dom/client'
  import { RouterProvider, createRouter } from '@tanstack/react-router'
  import { QueryClient, QueryClientProvider } from '@tanstack/react-query'

  import { routeTree } from './routeTree.gen'
  import './index.css'

  const queryClient = new QueryClient({
    defaultOptions: {
      queries: {
        staleTime: 60_000,
        refetchOnWindowFocus: false,
      },
    },
  })

  const router = createRouter({
    routeTree,
    defaultPreload: 'intent',
    context: { queryClient },
  })

  declare module '@tanstack/react-router' {
    interface Register {
      router: typeof router
    }
  }

  createRoot(document.getElementById('root')!).render(
    <StrictMode>
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </StrictMode>,
  )
  ```

  Delete the old `App.tsx` and any starter assets that are no longer referenced.

- [ ] **Step 7 — Run dev**

  ```bash
  pnpm -C web dev
  ```

  Confirm `http://localhost:5173/` shows "tempo" + the Button. The router plugin generates `web/src/routeTree.gen.ts` automatically; `pnpm dev` keeps it fresh.

- [ ] **Step 8 — Run `./verify.sh`.**

- [ ] **Step 9 — Commit**

  ```bash
  git add web/package.json web/pnpm-lock.yaml web/vite.config.ts web/src
  git commit -m "feat(web): TanStack Router + Query layered on shadcn starter (#0004)"
  ```

## Notes

- The `routeTree.gen.ts` file is generated. It's fine to commit it (recommended) so that fresh clones can `pnpm build` without first running `pnpm dev`.
- shadcn skill rule: use semantic tokens (`text-muted-foreground`) — the index route already does. No raw colors.
- This task is `review` — once it builds & dev-server-loads, surface the screen to the user and wait for `/finish-task 0004` before promoting.

## Review checklist for the user

- [ ] Index page renders the Button with the preset's button styling.
- [ ] TanStack Router devtools panel opens (bottom-right).
- [ ] React Query devtools button is present (bottom-left).
- [ ] No console errors.
