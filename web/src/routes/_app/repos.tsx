import { createFileRoute } from "@tanstack/react-router"
import { BarChart3Icon } from "lucide-react"

import { SectionPlaceholder } from "@/components/app-shell/placeholder"

export const Route = createFileRoute("/_app/repos")({
  head: () => ({ meta: [{ title: "Repos · tempo" }] }),
  component: ReposPage,
})

function ReposPage() {
  return (
    <SectionPlaceholder
      icon={BarChart3Icon}
      title="Repos"
      description="Per-repo cycle time, deploys, lead time, review load."
      taskId="0052"
    />
  )
}
