# 0047 — App shell — READY FOR UI REVIEW

## What landed

- shadcn primitives added under the existing `base-nova` Base UI
  preset: `sidebar`, `dropdown-menu`, `avatar`, `skeleton`,
  `tooltip`, `separator`, `empty` (and `sheet`, `input` as deps).
- `web/src/routes/_app.tsx` — protected layout. `beforeLoad` calls
  `queryClient.ensureQueryData({ queryKey: ["me"], queryFn:
  apiGet("/me") })`; an `ApiError(401)` throws `redirect({ to:
  "/login" })`. The guard runs before render so there's no
  flicker.
- `app-shell/sidebar.tsx` — collapsible-icon Sidebar with two
  groups:
  - Insights: Dashboard, Repos, Orgs, Engineers
  - Workspace: Connections, Sync, Settings
  Active state via TanStack Router's pathname + the Sidebar
  component's `isActive`. Footer shows version from
  `/api/v1/system/health`.
- `app-shell/topbar.tsx` — SidebarTrigger + section title + theme
  menu (light / dark / system) + user dropdown (email/role label,
  logout button).
- `app-shell/placeholder.tsx` — shared shadcn `Empty` wrapper used
  by every placeholder route, with a task-id chip pointing at the
  task that will fill it in.
- Stub `/login` and `/register` routes (0048 wires the forms).
- Root `/` redirects to `/dashboard`.
- ESLint: `react-refresh/only-export-components` is scoped off for
  `src/components/ui/**` (shadcn-generated) and `src/routes/**`
  (TanStack file routes export `Route` + component by design).

## Verify output

```
== pnpm typecheck ==
> tsc --noEmit
== pnpm lint ==
> eslint .
== pnpm build ==
> tsc -b && vite build
✓ built in 1.72s
```

## How to view it locally

From the repo root:

```bash
# Terminal 1 — backend on :4811 (auto-migrates first):
mise run dev-api

# Terminal 2 — frontend on :4810, proxies /api → :4811:
mise run dev-web
```

Then in your browser:

- **`http://localhost:4810/`** — should redirect to `/dashboard`,
  which 401s (no session) and lands you on `/login` (stub).
- **`http://localhost:4810/register`** — stub form placeholder
  (0048 fills it in).
- **Manually create an account** via `curl` so you can poke the
  shell with a real session:

  ```bash
  curl -X POST http://localhost:4810/api/v1/auth/register \
    -H "Content-Type: application/json" \
    -c /tmp/tempo.cookies \
    -d '{"email":"admin@example.com","password":"hunter22hunter22"}'
  ```

  Then drop `/tmp/tempo.cookies` into the browser via the devtools
  Application → Cookies panel (cookie name: `tempo_session`), or
  hit `/api/v1/auth/login` from a tab that already has the cookie.

- **With a session**, visit `/dashboard` — you should see the
  sidebar (collapsible via `cmd-B` or the trigger), the topbar
  with theme menu + user dropdown, and the placeholder card.
  Click through Repos / Orgs / Engineers / Connections / Sync /
  Settings — each shows its own placeholder with a "coming in
  00XX" chip.

## What to design-review

Conservative defaults — I kept the visual language close to the
`base-nova` preset that's already locked in. Things to evaluate:

- **Sidebar branding.** Header has a square `t` mark + "tempo"
  wordmark + "engineering insights" eyebrow. Footer shows the
  build version. Adjust mark / wordmark style if it doesn't read
  right.
- **Nav grouping.** Insights vs Workspace split — does that feel
  natural, or should everything be one group?
- **Icons** (lucide): Gauge / BarChart3 / Building2 / Users /
  Plug / RefreshCw / Settings. Swap any that don't fit your
  mental model.
- **Topbar density.** h-14 with sidebar-trigger + title + theme +
  avatar. Comfortable, or too sparse / too dense?
- **Theme toggle.** Three options (light/dark/system); inline
  with the menu rather than a one-click toggle. The existing
  `d` keyboard shortcut still works.
- **User menu.** Just Account (disabled, "coming soon") + Log
  out. Anything you want surfaced here?
- **Placeholder treatment.** Card-like `Empty` centered in the
  main area. Want them more compact / more prominent?
- **Spacing and motion.** Default shadcn animations on dropdowns;
  no custom motion yet.

## Notes / known limitations

- `/login` and `/register` are placeholder cards. 0048 wires the
  actual forms; the redirect chain already routes there.
- The version pulled from `/system/health` is the Go-side
  `version.Version` (currently `dev` or commit SHA, `docker` in
  the container image).
- "Account (coming soon)" item in the user menu is a hook for a
  future profile page; remove it if we never plan one.
- Mobile: the sidebar collapses into a Sheet on small viewports
  (`use-mobile.ts` from shadcn handles the breakpoint). Worth a
  quick check at viewport widths < 768px.

When you're happy, run `/finish-task 0047` and I'll move on to
0048 (auth pages).
