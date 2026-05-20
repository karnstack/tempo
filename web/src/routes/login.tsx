import { createFileRoute, redirect, useNavigate } from "@tanstack/react-router"

import { AuthCard } from "@/components/auth/auth-card"
import { AuthForm } from "@/components/auth/auth-form"
import { ApiError } from "@/lib/api"
import { safeFromPath } from "@/lib/auth-redirect"
import { firstRunQueryOptions } from "@/lib/queries/firstrun"
import { meQueryOptions } from "@/lib/queries/me"

type LoginSearch = {
  from?: string
}

export const Route = createFileRoute("/login")({
  validateSearch: (raw: Record<string, unknown>): LoginSearch => ({
    from: typeof raw.from === "string" ? raw.from : undefined,
  }),
  beforeLoad: async ({ context, search }) => {
    const firstRun = await context.queryClient.ensureQueryData(firstRunQueryOptions)
    if (firstRun.first_run) {
      throw redirect({ to: "/register", search: { from: search.from } })
    }

    try {
      await context.queryClient.ensureQueryData(meQueryOptions)
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) return
      throw err
    }
    throw redirect({ to: safeFromPath(search.from) })
  },
  component: LoginPage,
})

function LoginPage() {
  const navigate = useNavigate()
  const { from } = Route.useSearch()

  return (
    <AuthCard title="Sign in" description="Use the admin credentials you set up at first run.">
      <AuthForm
        mode="login"
        onSuccess={() => {
          void navigate({ to: safeFromPath(from) })
        }}
      />
    </AuthCard>
  )
}
