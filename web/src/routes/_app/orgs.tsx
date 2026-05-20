import { createFileRoute } from "@tanstack/react-router"
import { Building2Icon } from "lucide-react"

import { SectionPlaceholder } from "@/components/app-shell/placeholder"

export const Route = createFileRoute("/_app/orgs")({
  head: () => ({ meta: [{ title: "Orgs · tempo" }] }),
  component: OrgsPage,
})

function OrgsPage() {
  return (
    <SectionPlaceholder
      icon={Building2Icon}
      title="Orgs"
      description="Org-level summaries; drill down by repo."
      taskId="0053"
    />
  )
}
