import { Outlet, createFileRoute, redirect, useRouterState } from "@tanstack/react-router"

import { AppSidebar } from "@/components/app-shell/sidebar"
import { AppTopbar } from "@/components/app-shell/topbar"
import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar"
import { ApiError } from "@/lib/api"
import { meQueryOptions } from "@/lib/queries/me"

const SECTION_TITLES: Record<string, string> = {
  "/dashboard": "Dashboard",
  "/repos": "Repos",
  "/orgs": "Orgs",
  "/engineers": "Engineers",
  "/connections": "Connections",
  "/sync": "Sync",
  "/settings": "Settings",
}

function pickTitle(pathname: string): string {
  for (const [prefix, title] of Object.entries(SECTION_TITLES)) {
    if (pathname.startsWith(prefix)) {
      return title
    }
  }
  return "tempo"
}

export const Route = createFileRoute("/_app")({
  beforeLoad: async ({ context, location }) => {
    try {
      await context.queryClient.ensureQueryData(meQueryOptions)
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        throw redirect({
          to: "/login",
          search: { from: location.pathname },
        })
      }
      throw err
    }
  },
  component: AppLayout,
})

function AppLayout() {
  const pathname = useRouterState({ select: (s) => s.location.pathname })
  const title = pickTitle(pathname)

  return (
    <SidebarProvider>
      <AppSidebar />
      <SidebarInset>
        <AppTopbar title={title} />
        <main className="flex flex-1 flex-col gap-6 p-6">
          <Outlet />
        </main>
      </SidebarInset>
    </SidebarProvider>
  )
}
