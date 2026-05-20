# 0049 — connections-page · RESULT

Status at write: `in_progress` (review-mode). Ready for design review.

## What changed

**Routes**

- `web/src/routes/_app/connections.tsx` — replaced 0049 stub placeholder with real CRUD page.
- `web/src/routes/__root.tsx` — mounted `<Toaster />` from sonner once.

**Components**

- `web/src/components/connections/connection-list.tsx` — Card + Table + Skeleton/Empty, per-row DropdownMenu.
- `web/src/components/connections/add-connection-dialog.tsx` — Dialog form (kind / owner / name / token / backfill_from). Mutation, toast, 400/409 error mapping.
- `web/src/components/connections/delete-connection-dialog.tsx` — AlertDialog confirm + destructive action.
- `web/src/components/connections/connection-row-meta.ts` — shared DTO aliases + date/label formatters.

**Queries**

- `web/src/lib/queries/connections.ts` — `connectionsQueryOptions`, `useConnectionsQuery`.
- `web/src/lib/queries/tokens.ts` — `tokensQueryOptions`, `useTokensQuery` (read-only for the picker; token CRUD is 0050).

**Shadcn primitives added**

`card`, `badge`, `dialog`, `select`, `alert-dialog`, `table`, `sonner`. Sonner Toaster wired to project's local `useTheme` instead of `next-themes`.

## Verify output (tail)

```
== pnpm typecheck ==
> tsc --noEmit
== pnpm lint ==
> eslint .
== pnpm build ==
> tsc -b && vite build
…
dist/assets/connections-D4n4hOi4.js     51.48 kB │ gzip: 16.65 kB
dist/assets/index-DFHmmn_T.js          478.29 kB │ gzip: 151.04 kB
✓ built in 347ms
```

All three steps passed clean. Re-run: `./.plans/upnext/0049-connections-page/verify.sh`.

## How to view locally

```bash
make dev        # backend on :8080 + vite on :5173 with proxy
```

Visit `http://localhost:5173/connections`. Sign in first if needed. Inspect:

- **Empty / no-token state** — fresh DB with zero tokens. Expect the muted Alert above the Card and a disabled "Add connection" CTA inside the `Empty` block.
- **Empty / has-token state** — add a token via API (no UI yet — that's 0050) or seed one, then revisit. CTA enabled.
- **Populated state** — add 1–3 connections via the dialog. Table renders kind Badge, target, token label, status Badge, relative last-sync (`—` for never), short Added date.
- **Add dialog** — flip kind between Repo and Org; name field hides on Org. Submit empty → client errors. Submit dup → top Alert "Connection already exists.".
- **Remove flow** — row → "…" → "Remove" → AlertDialog. Confirm → row disappears, toast bottom-right.

## Expected review concerns

- **Header layout.** The CardAction sits inside CardHeader — when the description wraps, the "Add" button can feel detached. Worth a look on narrow viewports.
- **Status Badge variants.** Only `active` uses `default` (filled). Anything else falls back to `secondary` (muted). If the backend grows error/paused/syncing statuses, color encoding will need refinement.
- **Table density.** No sub-text, no avatars yet. May look a touch barren when only one row exists.
- **Token column.** Shows token *label*; could combine with scope or a small chip in the future.
- **Empty state.** Uses dashed border `Empty`. Tone aligns with `SectionPlaceholder` elsewhere, but copy is more functional.
- **Toast position.** Bottom-right, rich colors. Easy to flip later if the sidebar overlaps on small screens.
- **Backfill input.** Native `<input type="date">` — styling will inherit browser-native. If we want consistency across browsers, swap for a calendar Popover later.

## Followups (not in scope here)

- Token CRUD on Settings page (task 0050) so the no-token Alert links to a real action.
- Live sync-status streaming for the `Last sync` column (task 0055).
- Show ingest error details inline when a connection's last `sync_run` failed.

## Next

User reviews UI, iterates conversationally if needed, then runs `/finish-task 0049` once satisfied.
