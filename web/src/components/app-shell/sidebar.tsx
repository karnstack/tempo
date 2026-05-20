import { Link, useRouterState } from "@tanstack/react-router"
import {
  BarChart3Icon,
  Building2Icon,
  GaugeIcon,
  PlugIcon,
  RefreshCwIcon,
  SettingsIcon,
  UsersIcon,
} from "lucide-react"
import type { ComponentType, SVGProps } from "react"

import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
} from "@/components/ui/sidebar"
import { useHealthQuery } from "@/lib/queries/health"

type NavItem = {
  title: string
  to: string
  icon: ComponentType<SVGProps<SVGSVGElement>>
}

const PRIMARY_NAV: NavItem[] = [
  { title: "Dashboard", to: "/dashboard", icon: GaugeIcon },
  { title: "Repos", to: "/repos", icon: BarChart3Icon },
  { title: "Orgs", to: "/orgs", icon: Building2Icon },
  { title: "Engineers", to: "/engineers", icon: UsersIcon },
]

const SECONDARY_NAV: NavItem[] = [
  { title: "Connections", to: "/connections", icon: PlugIcon },
  { title: "Sync", to: "/sync", icon: RefreshCwIcon },
  { title: "Settings", to: "/settings", icon: SettingsIcon },
]

export function AppSidebar() {
  const location = useRouterState({ select: (s) => s.location.pathname })
  const health = useHealthQuery()

  const isActive = (to: string) => {
    if (to === "/dashboard") {
      return location === "/" || location.startsWith("/dashboard")
    }
    return location.startsWith(to)
  }

  return (
    <Sidebar collapsible="icon">
      <SidebarHeader>
        <div className="flex items-center gap-2 px-2 py-1.5 group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:gap-0 group-data-[collapsible=icon]:px-0">
          <div className="bg-primary text-primary-foreground flex size-7 shrink-0 items-center justify-center rounded-md font-mono text-sm font-semibold">
            t
          </div>
          <div className="flex flex-col leading-tight group-data-[collapsible=icon]:hidden">
            <span className="font-mono text-sm font-semibold">tempo</span>
            <span className="text-muted-foreground text-[10px] uppercase tracking-wider">
              engineering insights
            </span>
          </div>
        </div>
      </SidebarHeader>

      <SidebarContent>
        <SidebarGroup>
          <SidebarGroupLabel>Insights</SidebarGroupLabel>
          <SidebarGroupContent>
            <SidebarMenu className="gap-0.5">
              {PRIMARY_NAV.map((item) => (
                <SidebarMenuItem key={item.to}>
                  <SidebarMenuButton
                    render={<Link to={item.to} />}
                    isActive={isActive(item.to)}
                    tooltip={item.title}
                  >
                    <item.icon />
                    <span>{item.title}</span>
                  </SidebarMenuButton>
                </SidebarMenuItem>
              ))}
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>

        <SidebarGroup>
          <SidebarGroupLabel>Workspace</SidebarGroupLabel>
          <SidebarGroupContent>
            <SidebarMenu className="gap-0.5">
              {SECONDARY_NAV.map((item) => (
                <SidebarMenuItem key={item.to}>
                  <SidebarMenuButton
                    render={<Link to={item.to} />}
                    isActive={isActive(item.to)}
                    tooltip={item.title}
                  >
                    <item.icon />
                    <span>{item.title}</span>
                  </SidebarMenuButton>
                </SidebarMenuItem>
              ))}
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
      </SidebarContent>

      <SidebarFooter>
        <div className="text-muted-foreground flex flex-col gap-0.5 px-2 py-1 text-[10px] group-data-[collapsible=icon]:hidden">
          <span className="font-mono">{health.data ? `v${health.data.version}` : "—"}</span>
          <span>
            Powered by{" "}
            <a
              href="https://karnstack.com"
              target="_blank"
              rel="noreferrer"
              className="hover:text-foreground underline-offset-2 hover:underline"
            >
              karnstack
            </a>
          </span>
        </div>
      </SidebarFooter>
    </Sidebar>
  )
}
