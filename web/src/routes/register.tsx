import { createFileRoute, redirect, useNavigate } from "@tanstack/react-router"

import { AuthCard } from "@/components/auth/auth-card"
import { AuthForm } from "@/components/auth/auth-form"
import { ApiError } from "@/lib/api"
import { safeFromPath } from "@/lib/auth-redirect"
import { firstRunQueryOptions } from "@/lib/queries/firstrun"
import { meQueryOptions } from "@/lib/queries/me"

type RegisterSearch = {
  from?: string
}

export const Route = createFileRoute("/register")({
  validateSearch: (raw: Record<string, unknown>): RegisterSearch => ({
    from: typeof raw.from === "string" ? raw.from : undefined,
  }),
  beforeLoad: async ({ context, search }) => {
    const firstRun = await context.queryClient.ensureQueryData(firstRunQueryOptions)
    if (!firstRun.first_run) {
      throw redirect({ to: "/login", search: { from: search.from } })
    }

    try {
      await context.queryClient.ensureQueryData(meQueryOptions)
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) return
      throw err
    }
    throw redirect({ to: safeFromPath(search.from) })
  },
  component: RegisterPage,
})

function RegisterPage() {
  const navigate = useNavigate()
  const { from } = Route.useSearch()

  return (
    <AuthCard
      title="Create the admin account"
      description="This becomes the only user on this tempo instance. You can rotate the password from Settings later."
    >
      <AuthForm
        mode="register"
        onSuccess={() => {
          void navigate({ to: safeFromPath(from) })
        }}
      />
    </AuthCard>
  )
}
