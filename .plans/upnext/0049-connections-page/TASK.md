---
id: 0049
slug: connections-page
title: Connections page (list/add/delete)
status: in_progress
depends_on: [0046, 0047]
owner: ""
est_minutes: 90
tags: [ui, frontend]
autonomy: review
skills: [shadcn]
---

## Goal

Replace the `/connections` placeholder with a real CRUD page that lists current
GitHub connections, lets the operator add a new repo/org connection, and
delete an existing one. Backend `/api/v1/connections` already exists (0040) and
TS types are generated (0046).

A connection requires picking a `token_id`, so the page also surfaces the list
of tokens — but token CRUD lives on the Settings page (0050). When the tenant
has zero tokens we render a guidance state instead of opening the add dialog.

## Acceptance criteria

- [ ] `/connections` route renders connections in a table (kind, owner/name, token, status, last sync, created, row-action).
- [ ] Loading state uses `Skeleton`; empty state uses `Empty` with an "Add connection" CTA.
- [ ] "Add connection" opens a `Dialog` form with: kind (Select repo/org), owner (Input), name (Input — required when kind=repo, hidden when kind=org), token_id (Select sourced from `GET /tokens`), backfill_from (Input type=date, optional).
- [ ] Successful add closes the dialog, refetches the list, fires a `sonner` toast `"Connection added."`.
- [ ] Server validation errors (400) map to field-level errors; duplicate (409) shows top-of-form Alert "Connection already exists.".
- [ ] Row action menu (DropdownMenu) → "Remove" opens `AlertDialog` confirmation. Confirm calls `DELETE /connections/{id}`, refetches, toasts `"Connection removed."`.
- [ ] If no tokens exist, the "Add connection" CTA renders a tooltip/disabled state explaining a PAT is required first (point at Settings → Tokens once that page exists).
- [ ] `pnpm typecheck`, `pnpm lint`, `pnpm build` all pass.

## Files to touch

- `web/src/routes/_app/connections.tsx` — replace placeholder with real page.
- `web/src/lib/queries/connections.ts` — new: `connectionsQueryOptions`, `useConnectionsQuery`.
- `web/src/lib/queries/tokens.ts` — new: `tokensQueryOptions`, `useTokensQuery` (just `GET /tokens`, since the Add dialog needs the list; full token CRUD waits for 0050).
- `web/src/components/connections/connection-list.tsx` — Table with rows, row-action menu.
- `web/src/components/connections/add-connection-dialog.tsx` — controlled Dialog + form (kind/owner/name/token_id/backfill_from).
- `web/src/components/connections/delete-connection-dialog.tsx` — AlertDialog confirm.
- `web/src/routes/__root.tsx` — add `<Toaster />` from `sonner` once at root.
- `web/src/components/ui/{card,badge,dialog,select,alert-dialog,table,sonner}.tsx` — added via `pnpm dlx shadcn@latest add ...`.

## Steps

1. **Add missing shadcn components.** From `web/`:
   ```bash
   pnpm dlx shadcn@latest add card badge dialog select alert-dialog table sonner
   ```
   Verify the files landed under `web/src/components/ui/`. `sonner` adds the `Toaster` wrapper.
   Commit: `feat(web): add shadcn primitives for connections page (#0049)`.

2. **Wire `<Toaster />` at root.** Edit `web/src/routes/__root.tsx` to import `Toaster` from `@/components/ui/sonner` and render once inside `TooltipProvider`. Run `pnpm typecheck` to confirm.
   Commit: `feat(web): mount sonner toaster at root (#0049)`.

3. **Add data hooks.** Create `web/src/lib/queries/connections.ts` and `web/src/lib/queries/tokens.ts`. Mirror the `me.ts` pattern: export a `queryOptions(...)` const and a `useXQuery()` hook. The query key for connections is `["connections"]`, tokens is `["tokens"]`.
   Commit: `feat(web): connections + tokens query hooks (#0049)`.

4. **Build `ConnectionList`.** Renders a `Card` with `Table` inside. Columns: Kind (Badge `repo`/`org`), Repo (`{owner}/{name}` or `{owner}` for org), Token (token label looked up by `token_id`), Status (Badge — `active` → default, others → secondary), Last sync (relative formatter or `—`), Added, Row actions (DropdownMenu with `Remove`). Loading state: `Skeleton` rows × 3. Empty state: `Empty` with a "Add connection" button. Use `Spinner` only inside the action button when mutating.
   Commit: `feat(web): connection list table (#0049)`.

5. **Build `AddConnectionDialog`.** Controlled `Dialog` with `DialogTitle` "Add connection". `FieldGroup` containing kind `Select` (`repo`/`org`), owner `Input`, name `Input` (only when kind=`repo`), token `Select` (options from `useTokensQuery`), backfill_from `Input type=date` (optional). On submit, call `apiPost("/connections", { body })`, invalidate `["connections"]`, close dialog, toast `Connection added.`. Map error → 400 to field-level via simple substring sniff (`owner`/`name`/`token`), 409 → top Alert "Connection already exists.".
   Commit: `feat(web): add-connection dialog (#0049)`.

6. **Build `DeleteConnectionDialog`.** `AlertDialog` with `AlertDialogTitle` "Remove this connection?" and `AlertDialogDescription` warning about data retention (raw events stay, polling stops). Confirm → `apiDelete("/connections/{id}", { path: { id } })`, invalidate `["connections"]`, toast `Connection removed.`, close.
   Commit: `feat(web): delete-connection confirm dialog (#0049)`.

7. **Wire the route.** Rewrite `web/src/routes/_app/connections.tsx`: use `useConnectionsQuery` + `useTokensQuery`, render `ConnectionList`, mount the two dialogs as controlled state. If `useTokensQuery().data?.tokens.length === 0`, render an `Alert` above the list pointing users to Settings → Tokens (once 0050 lands; for now copy says "Add a PAT in Settings first.").
   Commit: `feat(web): /connections page (#0049)`.

8. **Run verify.** From the task dir: `./verify.sh`. Fix any lint/type/build errors, then proceed to RESULT.md write-up.

## Notes

- Token list endpoint is only used here for the picker; token *management* is 0050's scope. Don't add CreateToken/DeleteToken UI in this task.
- The backend returns `echo`-style `{"message": "..."}` errors; `ApiError.message` is already the string we need.
- `kind=org` means `name` must be null/empty in the request body — the dialog should send `name: null` (or omit it) when kind is `org`.
- Keep the column count tight (≤7). If we add more later we can move to a horizontally scrollable Card.
- This is `autonomy: review` — once verify passes, write `RESULT.md` with screenshots paths and stop for design review.
