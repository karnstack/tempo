import { createFileRoute } from "@tanstack/react-router"
import { SettingsIcon } from "lucide-react"

import { SectionPlaceholder } from "@/components/app-shell/placeholder"

export const Route = createFileRoute("/_app/settings")({
  head: () => ({ meta: [{ title: "Settings · tempo" }] }),
  component: SettingsPage,
})

function SettingsPage() {
  return (
    <SectionPlaceholder
      icon={SettingsIcon}
      title="Settings"
      description="Admin password, polling cadence, retention, danger zone."
      taskId="0050"
    />
  )
}
