import { createFileRoute } from "@tanstack/react-router"
import { PlugIcon } from "lucide-react"

import { SectionPlaceholder } from "@/components/app-shell/placeholder"

export const Route = createFileRoute("/_app/connections")({
  component: ConnectionsPage,
})

function ConnectionsPage() {
  return (
    <SectionPlaceholder
      icon={PlugIcon}
      title="Connections"
      description="Add, remove, and watch GitHub repo or org connections."
      taskId="0049"
    />
  )
}
