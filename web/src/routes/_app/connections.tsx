import { useState } from "react"
import { createFileRoute, Link } from "@tanstack/react-router"
import { AlertCircleIcon } from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { AddConnectionDialog } from "@/components/connections/add-connection-dialog"
import { ConnectionList } from "@/components/connections/connection-list"
import { DeleteConnectionDialog } from "@/components/connections/delete-connection-dialog"
import type { ConnectionDTO } from "@/components/connections/connection-row-meta"
import { useConnectionsQuery } from "@/lib/queries/connections"
import { useTokensQuery } from "@/lib/queries/tokens"

export const Route = createFileRoute("/_app/connections")({
  head: () => ({ meta: [{ title: "Connections · tempo" }] }),
  component: ConnectionsPage,
})

function ConnectionsPage() {
  const connectionsQuery = useConnectionsQuery()
  const tokensQuery = useTokensQuery()

  const tokens = tokensQuery.data?.tokens
  const connections = connectionsQuery.data?.connections
  const tokensReady = tokensQuery.isSuccess
  const hasTokens = (tokens?.length ?? 0) > 0

  const [addOpen, setAddOpen] = useState(false)
  const [addKey, setAddKey] = useState(0)
  const [pendingDelete, setPendingDelete] = useState<ConnectionDTO | null>(null)

  const openAdd = () => {
    if (!hasTokens) return
    setAddKey((k) => k + 1)
    setAddOpen(true)
  }

  return (
    <>
      {tokensReady && !hasTokens && (
        <Alert>
          <AlertCircleIcon />
          <AlertTitle>You need a GitHub PAT first.</AlertTitle>
          <AlertDescription>
            Connections poll GitHub with one of your stored tokens. Add one under{" "}
            <Link to="/settings" className="underline underline-offset-4">
              Settings → Tokens
            </Link>
            , then come back to add a connection.
          </AlertDescription>
        </Alert>
      )}

      <ConnectionList
        connections={connections}
        tokens={tokens}
        isLoading={connectionsQuery.isLoading}
        canAdd={hasTokens}
        onAddClick={openAdd}
        onRemoveClick={(conn) => setPendingDelete(conn)}
      />

      <AddConnectionDialog
        key={addKey}
        open={addOpen}
        onOpenChange={setAddOpen}
        tokens={tokens}
      />

      <DeleteConnectionDialog
        connection={pendingDelete}
        onOpenChange={(open) => {
          if (!open) setPendingDelete(null)
        }}
      />
    </>
  )
}
