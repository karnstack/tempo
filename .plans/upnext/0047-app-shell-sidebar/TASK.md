---
id: 0047
slug: app-shell-sidebar
title: App shell (Sidebar nav + top bar + root layout)
status: in_progress
depends_on: [0046]
owner: ""
est_minutes: 90
tags: [ui, frontend]
autonomy: review
skills: [shadcn, refactoring-ui]
---

## Goal

App shell every other UI task (0048-0055) hangs off:

- Sidebar (left, collapsible) with Dashboard / Repos / Orgs /
  Engineers / Connections / Sync / Settings nav.
- Top bar with theme toggle + user menu (logout).
- TanStack Router `_app` layout that 401-redirects to `/login` via
  the `me` query.
- Placeholder routes for each nav item using shadcn `Empty`
  pointing at the task that will fill them in.

## Acceptance criteria

- [ ] shadcn components added: `sidebar`, `dropdown-menu`,
      `avatar`, `skeleton`, `tooltip`, `separator`, `empty`.
- [ ] `web/src/routes/_app.tsx` layout + per-section placeholders.
- [ ] `web/src/routes/login.tsx`, `register.tsx` stubs.
- [ ] `web/src/routes/index.tsx` redirects to dashboard/login.
- [ ] `app-shell/sidebar.tsx` + `app-shell/topbar.tsx`.
- [ ] `lib/queries/me.ts` shared.
- [ ] typecheck + lint + build pass.

## Files

See Steps. Verify.sh: typecheck + lint + build.

## Steps

1. `pnpm dlx shadcn@latest add sidebar dropdown-menu avatar skeleton tooltip separator empty`.
2. Author shell + routes.
3. Wire me query + 401 redirect.
4. Verify.
