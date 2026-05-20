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
# Terminal 1 — backend on https://api.tempo.localhost (auto-migrates first):
mise run dev-api

# Terminal 2 — frontend on https://tempo.localhost, proxies /api → api.tempo.localhost:
mise run dev-web
```

Then in your browser:

- **`https://tempo.localhost/`** — should redirect to `/dashboard`,
  which 401s (no session) and lands you on `/login` (stub).
- **`https://tempo.localhost/register`** — stub form placeholder
  (0048 fills it in).
- **Manually create an account** via `curl` so you can poke the
  shell with a real session:

  ```bash
  curl -X POST https://api.tempo.localhost/api/v1/auth/register \
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

## Final (2026-05-20)

User reviewed against the live SPA after the 0048 login flow landed
(so the shell could actually be exercised end-to-end). Two shell
bugs surfaced during review and were folded into 0047:

- `da45d53` fix(web): wrap user-menu label in DropdownMenuGroup — the
  topbar avatar menu was throwing "MenuGroupRootContext is missing"
  because `DropdownMenuLabel` (Base UI's `Menu.GroupLabel`) was a
  bare child of `DropdownMenuContent`. Wrapped it in its own
  `DropdownMenuGroup`.
- `5c4abd3` fix(web): topbar separator stretches; sidebar brand mark
  shrink-0 — dropped the explicit `h-5` cap on the topbar separator
  so Base UI's `data-vertical:self-stretch` takes over and renders a
  full-height divider; added `shrink-0` to the size-7 brand mark so
  the collapsed sidebar (icon mode, ~48px wide) couldn't squeeze it.
- `44b2b8d` feat(web): per-route titles, t-in-a-box favicon, sidebar
  polish — bundled the rest of the review polish:
  - Per-route `head()` titles wired via TanStack Router; root sets
    "tempo" + description + viewport, leaves override with
    "<Page> · tempo".
  - `/favicon.svg` — t-in-a-box mark matching the sidebar brand,
    with a `prefers-color-scheme: dark` variant. `vite.svg` deleted.
  - `SidebarMenu` gets `gap-0.5` so hovered/active pills don't butt
    up against each other.
  - Sidebar header centers the brand block in icon-collapsed mode
    (`justify-center` + zero padding/gap) instead of clipping left.
  - Sidebar footer adds a "Powered by karnstack" link below the
    version, hidden in icon-collapsed mode.

### Final verify output

```
== pnpm typecheck ==
> tsc --noEmit
== pnpm lint ==
> eslint .
== pnpm build ==
> tsc -b && vite build
✓ built in 274ms
```

User-approved on 2026-05-20.
