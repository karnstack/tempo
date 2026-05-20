import { useMutation, useQueryClient } from "@tanstack/react-query"
import { toast } from "sonner"

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import { Spinner } from "@/components/ui/spinner"
import { ApiError, apiDelete } from "@/lib/api"
import { CONNECTIONS_QUERY_KEY } from "@/lib/queries/connections"
import {
  type ConnectionDTO,
  connectionLabel,
} from "@/components/connections/connection-row-meta"

type DeleteConnectionDialogProps = {
  connection: ConnectionDTO | null
  onOpenChange: (open: boolean) => void
}

export function DeleteConnectionDialog({
  connection,
  onOpenChange,
}: DeleteConnectionDialogProps) {
  const queryClient = useQueryClient()

  const mutation = useMutation({
    mutationFn: (id: number) => apiDelete("/connections/{id}", { path: { id } }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: CONNECTIONS_QUERY_KEY })
      toast.success("Connection removed.")
      onOpenChange(false)
    },
    onError: (err) => {
      const message =
        err instanceof ApiError
          ? err.message
          : err instanceof Error
            ? err.message
            : "Couldn’t remove connection."
      toast.error(message)
    },
  })

  const onConfirm = () => {
    if (!connection) return
    mutation.mutate(connection.id)
  }

  const pending = mutation.isPending
  const label = connection ? connectionLabel(connection) : ""

  return (
    <AlertDialog
      open={connection !== null}
      onOpenChange={(open) => {
        if (!open && !pending) onOpenChange(false)
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Remove this connection?</AlertDialogTitle>
          <AlertDialogDescription>
            <span className="font-medium text-foreground">{label}</span> will stop polling
            immediately. Already-ingested PRs, reviews, and deploys stay in tempo until you
            wipe data from Settings.
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={pending}>Cancel</AlertDialogCancel>
          <AlertDialogAction
            variant="destructive"
            onClick={(event) => {
              event.preventDefault()
              onConfirm()
            }}
            disabled={pending}
          >
            {pending && <Spinner data-icon="inline-start" />}
            Remove
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
