import type { QueryClient } from "@tanstack/react-query"
import { HeadContent, Outlet, createRootRouteWithContext } from "@tanstack/react-router"
import { TanStackRouterDevtools } from "@tanstack/router-devtools"
import { ReactQueryDevtools } from "@tanstack/react-query-devtools"

import { TooltipProvider } from "@/components/ui/tooltip"

type RouterContext = {
  queryClient: QueryClient
}

export const Route = createRootRouteWithContext<RouterContext>()({
  head: () => ({
    meta: [
      { title: "tempo" },
      { name: "description", content: "Engineering insights for your GitHub repos and orgs." },
      { name: "viewport", content: "width=device-width, initial-scale=1.0" },
    ],
    links: [{ rel: "icon", type: "image/svg+xml", href: "/favicon.svg" }],
  }),
  component: RootComponent,
})

function RootComponent() {
  return (
    <TooltipProvider>
      <HeadContent />
      <Outlet />
      {import.meta.env.DEV && (
        <>
          <TanStackRouterDevtools position="bottom-right" />
          <ReactQueryDevtools buttonPosition="bottom-left" />
        </>
      )}
    </TooltipProvider>
  )
}
