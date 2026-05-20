---
id: 0048
slug: auth-pages-register-login
title: Auth pages — register (first-run) + login
status: in_progress
depends_on: [0047]
owner: ""
est_minutes: 60
tags: [ui, frontend, auth]
autonomy: review
skills: [shadcn]
---

## Goal

Replace the placeholder `/login` and `/register` route stubs with real
forms wired against the backend's `/auth/firstrun`, `/auth/register`,
and `/auth/login` endpoints. The page that's reachable depends on the
server's first-run state, and once authenticated the user lands at
`/dashboard` (or `from` if it was supplied by the protected-route
redirect in `_app.tsx`).

This unblocks UI review of the app shell — reviewers can sign up /
log in entirely through the SPA instead of curl-ing a session cookie.

## Acceptance criteria

- [ ] `npx shadcn@latest add field alert spinner` adds the primitives
  we need; no new manual `<Label>` / `<div className="space-y-...">`
  form markup.
- [ ] `/register` shows only when `GET /auth/firstrun` returns
  `first_run: true`; otherwise redirects to `/login`. On success the
  user lands at `from` if present (and safe) or `/dashboard`.
- [ ] `/login` shows only when `first_run: false`; otherwise redirects
  to `/register`. Accepts a `?from=<path>` search param and forwards to
  it after auth. Rejects `from` values that don't start with `/` or
  start with `//`.
- [ ] Both forms validate inline using shadcn `Field` + `data-invalid`
  / `aria-invalid` (email present, password ≥ 8 chars on register).
- [ ] Server errors (409 registration closed, 401 invalid creds, 400
  invalid email, etc.) surface in an `Alert` above the form, not as a
  thrown query error.
- [ ] Submit button shows `Spinner` while the mutation is pending and
  is `disabled` so double-submit is impossible.
- [ ] Already-authenticated users visiting `/login` or `/register`
  redirect to `/dashboard` (the firstrun query and a `/me` probe
  happen in `beforeLoad`).
- [ ] `pnpm typecheck && pnpm lint && pnpm build` all pass.

## Files to touch

- `web/src/components/ui/field.tsx` (added by shadcn CLI)
- `web/src/components/ui/alert.tsx` (added by shadcn CLI)
- `web/src/components/ui/spinner.tsx` (added by shadcn CLI)
- `web/src/components/auth/auth-card.tsx` (new — shared centered shell)
- `web/src/components/auth/login-form.tsx` (new)
- `web/src/components/auth/register-form.tsx` (new)
- `web/src/lib/queries/firstrun.ts` (new — `firstRunQueryOptions`)
- `web/src/lib/queries/me.ts` (helper export `meQueryOptions`)
- `web/src/routes/login.tsx` (rewrite)
- `web/src/routes/register.tsx` (rewrite)
- `web/src/lib/auth-redirect.ts` (new — `safeFromPath`)

## Steps

### 1. Add shadcn primitives

```bash
cd web && pnpm dlx shadcn@latest add field alert spinner
```

Commit: `feat(web): add shadcn field/alert/spinner primitives`.

### 2. Add `firstRunQueryOptions` + extract `meQueryOptions`

Centralize the query options so the routes' `beforeLoad` can call
`queryClient.ensureQueryData(meQueryOptions)` without inlining the
fetch. Both routes plus `_app.tsx` end up using the same options
object.

- `web/src/lib/queries/me.ts` — export `meQueryOptions`. Refactor
  `useMeQuery` to use it. Update `_app.tsx` import.
- `web/src/lib/queries/firstrun.ts` — export
  `firstRunQueryOptions` and `useFirstRunQuery`.

Commit: `feat(web): centralize me + firstrun query options`.

### 3. `safeFromPath` helper

A `from` query string can be attacker-supplied. Only allow
same-origin relative paths starting with a single `/`. Anything else
falls back to `/dashboard`.

```ts
// web/src/lib/auth-redirect.ts
export const DEFAULT_AUTHED_DESTINATION = "/dashboard"
export function safeFromPath(raw: unknown): string {
  if (typeof raw !== "string") return DEFAULT_AUTHED_DESTINATION
  if (!raw.startsWith("/")) return DEFAULT_AUTHED_DESTINATION
  if (raw.startsWith("//")) return DEFAULT_AUTHED_DESTINATION
  return raw
}
```

Commit: `feat(web): add safeFromPath helper for auth redirects`.

### 4. `AuthCard` shell

Shared visual frame for both auth pages so they read as a system:
brand mark + wordmark + subtitle slot + form slot. Centered, max
`w-sm`. Uses the same `t` mark from the sidebar.

```tsx
// web/src/components/auth/auth-card.tsx
type Props = {
  title: string
  description?: string
  children: React.ReactNode
  footer?: React.ReactNode
}
```

Commit: `feat(web): add AuthCard shell for /login and /register`.

### 5. `RegisterForm` + `LoginForm`

Each is a self-contained component that owns its mutation, validation
state, and error rendering. Submitting calls
`apiPost("/auth/register" | "/auth/login", { body })`, then on success
seeds `["me"]` with the returned user and calls a `onSuccess`
callback from the parent route (which handles navigation).

Form layout:

```tsx
<FieldGroup>
  <Field data-invalid={emailError ? true : undefined}>
    <FieldLabel htmlFor="email">Email</FieldLabel>
    <Input id="email" type="email" autoComplete="email"
      aria-invalid={emailError ? true : undefined}
      value={email} onChange={(e) => setEmail(e.target.value)} />
    {emailError && <FieldError>{emailError}</FieldError>}
  </Field>
  <Field data-invalid={passwordError ? true : undefined}>
    <FieldLabel htmlFor="password">Password</FieldLabel>
    <Input id="password" type="password" autoComplete={isRegister
      ? "new-password" : "current-password"}
      aria-invalid={passwordError ? true : undefined}
      value={password} onChange={(e) => setPassword(e.target.value)} />
    {passwordError && <FieldError>{passwordError}</FieldError>}
    {isRegister && !passwordError && (
      <FieldDescription>At least 8 characters.</FieldDescription>
    )}
  </Field>
  {topError && <Alert variant="destructive">…</Alert>}
  <Button type="submit" disabled={pending}>
    {pending && <Spinner data-icon="inline-start" />}
    {isRegister ? "Create account" : "Sign in"}
  </Button>
</FieldGroup>
```

Map backend errors:
- `register` 400 "invalid email" → `setEmailError(message)`
- `register` 400 "password must be at least 8 characters" →
  `setPasswordError(message)`
- `register` 409 → `setTopError("Registration is closed. Please log
  in instead.")`
- `login` 401 → `setTopError("Email or password is incorrect.")`
- Anything else → `setTopError(error.message || "Unexpected error,
  please try again.")`

Commit: `feat(web): wire register and login forms`.

### 6. Rewrite `/register` and `/login` route files

Both have `beforeLoad` that ensures the firstrun query is fresh,
short-circuits to the appropriate sibling page if state mismatches,
and probes `/me` to redirect authenticated users straight to their
destination.

```tsx
// web/src/routes/login.tsx
export const Route = createFileRoute("/login")({
  validateSearch: (s) => ({
    from: typeof s.from === "string" ? s.from : undefined,
  }),
  beforeLoad: async ({ context, search }) => {
    const firstRun = await context.queryClient.ensureQueryData(
      firstRunQueryOptions,
    )
    if (firstRun.first_run) throw redirect({ to: "/register" })

    try {
      await context.queryClient.ensureQueryData(meQueryOptions)
      throw redirect({ to: safeFromPath(search.from) })
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) return
      throw err
    }
  },
  component: LoginPage,
})
```

`register.tsx` mirrors this with `if (!firstRun.first_run) → /login`.

`LoginPage` / `RegisterPage` render `<AuthCard>` + the matching form
and pass an `onSuccess` callback that seeds `["me"]` and navigates.

Commit: `feat(web): wire /login and /register pages`.

### 7. Verify

- Run `./.plans/upnext/0048-auth-pages-register-login/verify.sh` from
  repo root.
- Manual smoke (documented in `RESULT.md` for review):
  1. Fresh DB → `https://tempo.localhost/` redirects to `/register`.
  2. Register `admin@example.com` / `hunter22hunter22` → lands on
     `/dashboard`.
  3. Log out via topbar → returns to `/login`.
  4. Wrong password → 401 surfaces in Alert; right password → back to
     `/dashboard`.
  5. Visit `/register` while logged out (after first user exists) →
     redirected to `/login`.
  6. Visit `/login` while logged in → redirected to `/dashboard`.
  7. Open `/dashboard` while logged out → redirected to
     `/login?from=%2Fdashboard`; sign in → land on `/dashboard`.

## Notes

- Don't add a "Forgot password?" link — v1 has no recovery flow
  (admin runs a CLI / manual SQL).
- The "remember me" toggle is also out of scope. Session lifetime is
  whatever the backend picks (currently long-lived cookie).
- Keep `from` strictly relative to defend against open-redirect.
  We're already constraining it in `safeFromPath`.
- Use semantic tokens only (`text-muted-foreground`, `bg-card`,
  `text-destructive`). No raw colors.
- Auth pages don't use the protected `_app` layout — they're
  top-level routes that intentionally bypass the sidebar shell.
