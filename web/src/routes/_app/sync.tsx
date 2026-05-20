import { createFileRoute } from "@tanstack/react-router"
import { RefreshCwIcon } from "lucide-react"

import { SectionPlaceholder } from "@/components/app-shell/placeholder"

export const Route = createFileRoute("/_app/sync")({
  head: () => ({ meta: [{ title: "Sync · tempo" }] }),
  component: SyncPage,
})

function SyncPage() {
  return (
    <SectionPlaceholder
      icon={RefreshCwIcon}
      title="Sync status"
      description="Per-connection ingest health: last run, last success, last failure."
      taskId="0055"
    />
  )
}
