import { createFileRoute } from "@tanstack/react-router"
import { UsersIcon } from "lucide-react"

import { SectionPlaceholder } from "@/components/app-shell/placeholder"

export const Route = createFileRoute("/_app/engineers")({
  component: EngineersPage,
})

function EngineersPage() {
  return (
    <SectionPlaceholder
      icon={UsersIcon}
      title="Engineers"
      description="Per-engineer commits, PRs, reviews, response times."
      taskId="0054"
    />
  )
}
