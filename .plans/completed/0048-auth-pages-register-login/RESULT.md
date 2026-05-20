# 0048 — Auth pages — READY FOR UI REVIEW

## What landed

- shadcn primitives: `field`, `alert`, `spinner` (plus `label` as a
  field dep).
- `web/src/lib/auth-redirect.ts` — `safeFromPath()` constrains the
  `?from` query param to same-origin relative paths so a malicious
  link can't bounce a freshly-authed user to a phishing site
  (`//evil.com` and `/login` self-references both reject).
- `web/src/lib/queries/firstrun.ts` — `firstRunQueryOptions` (60s
  staleTime; the SPA only needs to see the transition once per
  session). `web/src/lib/queries/me.ts` now exports `meQueryOptions`
  and `_app.tsx` consumes it so all three routes (`_app`, `/login`,
  `/register`) share one source of truth.
- `web/src/components/auth/auth-card.tsx` — shared centered shell
  with the same `t` brand mark + wordmark + eyebrow used in the
  sidebar header.
- `web/src/components/auth/auth-form.tsx` — single `AuthForm` taking
  `mode: "login" | "register"`. Owns its mutation, client-side
  validation, field errors, and a top-level destructive `Alert`.
  Submit button shows a `Spinner` (`data-icon="inline-start"`) and
  disables on pending.
  - Backend error mapping:
    - `register` 400 mentioning "email" → `FieldError` on email
    - `register` 400 mentioning "password" → `FieldError` on password
    - `register` 409 → Alert "Registration is closed. Please sign in
      instead."
    - `login` 401 → Alert "Email or password is incorrect."
    - anything else → Alert with the raw server message.
- `web/src/routes/login.tsx` and `web/src/routes/register.tsx` —
  rewritten. `beforeLoad` on each route:
  1. Ensures `firstRunQueryOptions` is in cache, and redirects to
     the sibling page if state disagrees (preserving `?from`).
  2. Tries `meQueryOptions` — a 200 means already-authed, so it
     short-circuits to `safeFromPath(from)`. A 401 falls through to
     the form.

`_app.tsx` still emits the `redirect({ to: "/login", search: { from:
location.pathname } })` it did in 0047 — `/login` now declares
`validateSearch` for `from?: string` so the search payload is typed.

## Verify output

```
== pnpm typecheck ==
> tsc --noEmit
== pnpm lint ==
> eslint .
== pnpm build ==
> tsc -b && vite build
✓ built in 323ms
```

## How to view it locally

Dev stack is already running (`mise run dev-api` + `mise run dev-web`).
Server's first-run state is **true** right now — no users yet.

Open `https://tempo.localhost/` in a fresh browser profile. The
redirect chain:

```
/  →  /dashboard  →  _app beforeLoad 401s  →  /login?from=%2Fdashboard
   →  /login beforeLoad sees first_run=true  →  /register?from=%2Fdashboard
```

Register `admin@example.com` / `hunter22hunter22` (≥ 8 chars). On
success you land on `/dashboard` with the sidebar shell from 0047.

## Smoke test the auth flows

| Step | Expected |
|------|----------|
| Hit `/` while logged out (first-run still true) | bounce to `/register` |
| Submit empty form | inline `Email is required.` + `Password is required.` |
| Submit `not-an-email` + 7-char password | inline FieldErrors on both fields (client-side) |
| Submit valid email + 7-char password | inline `Password must be at least 8 characters.` |
| Submit a valid email + 8+ char password | redirect to `/dashboard` |
| Log out (topbar user menu) | bounce back to `/login` |
| Submit valid email + wrong password | top `Alert`: "Email or password is incorrect." |
| Submit valid email + right password | redirect to `/dashboard` |
| Visit `/register` while logged out (post first-run) | redirect to `/login` |
| Visit `/login` while logged in | redirect to `/dashboard` |
| Visit `/dashboard` while logged out | bounce through `/login?from=%2Fdashboard`, sign in, land on `/dashboard` |
| Try `/login?from=//evil.com` then sign in | redirect to `/dashboard` (open-redirect blocked) |

## What to design-review

- **Auth card framing.** Centered card, max-w-sm, with brand mark
  block above. Looks balanced at desktop and mobile? Card padding
  (`p-6`) feel right?
- **Brand block.** Same `t` square + wordmark + "engineering
  insights" eyebrow as the sidebar. Reads consistent or feels
  like the sidebar fell on its side?
- **Form density.** `FieldGroup` defaults give `gap-5` between
  fields, with `FieldDescription` for the password hint on
  register. Comfortable, or want it tighter / looser?
- **Submit button.** Full-width `Button` with `Spinner` on
  pending. Color is `bg-primary` (zinc-ish). Want it to read more
  like a CTA, or leave conservative?
- **Error treatment.** Per-field `FieldError` (red text under the
  input) + a top `Alert` (destructive variant, soft card bg with
  red text) for server-level failures. Both showing at once is
  possible (e.g. an inline field error plus a 5xx); fine, or want
  one to suppress the other?
- **Copy.** "Sign in" / "Create the admin account" / the
  description sentences. Tweak if any of it feels off.
- **Auto-focus.** Email field auto-focuses on mount. Want that, or
  leave it to the browser?

## Notes / known limitations

- No "Forgot password?" affordance — v1 has no recovery flow. Admin
  rotates the password from the (still-stub) Settings page.
- No "Remember me" toggle — backend issues a long-lived cookie
  unconditionally.
- AuthForm is intentionally a single component with a `mode` prop
  rather than two near-duplicate files; the differences (endpoint,
  copy, password autocomplete, register-only hint) are narrow enough
  that the prop split reads cleaner than file split. Easy to tear
  apart later if behavior diverges.
- The TASK.md originally proposed separate `login-form.tsx` +
  `register-form.tsx`; the actual implementation is one
  `auth-form.tsx`. No other deviations from the plan.

When you're happy, run `/finish-task 0048` and we can move on to
0049 (Connections page).

## Final (2026-05-20)

User registered an admin account, exercised the login + logout
flows, then went through the shell. No follow-up edits to the auth
pages themselves — review polish landed against 0047 (topbar
separator, brand-mark centering, menu gap, favicon, per-route
titles, "Powered by karnstack" footer link).

### Final verify output

```
== pnpm typecheck ==
> tsc --noEmit
== pnpm lint ==
> eslint .
== pnpm build ==
> tsc -b && vite build
✓ built in 270ms
```

User-approved on 2026-05-20.
