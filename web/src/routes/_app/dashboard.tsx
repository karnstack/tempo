import { createFileRoute } from "@tanstack/react-router"
import { GaugeIcon } from "lucide-react"

import { SectionPlaceholder } from "@/components/app-shell/placeholder"

export const Route = createFileRoute("/_app/dashboard")({
  component: DashboardPage,
})

function DashboardPage() {
  return (
    <SectionPlaceholder
      icon={GaugeIcon}
      title="Global dashboard"
      description="PR throughput, top contributors, review backlog, recent deploys."
      taskId="0051"
    />
  )
}
