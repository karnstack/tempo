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
import type { TokenDTO } from "@/components/connections/connection-row-meta"
import { TOKENS_QUERY_KEY } from "@/lib/queries/tokens"

type DeleteTokenDialogProps = {
  token: TokenDTO | null
  onOpenChange: (open: boolean) => void
}

type ConflictBody = {
  error?: string
  connection_count?: number
}

export function DeleteTokenDialog({ token, onOpenChange }: DeleteTokenDialogProps) {
  const queryClient = useQueryClient()

  const mutation = useMutation({
    mutationFn: (id: number) => apiDelete("/tokens/{id}", { path: { id } }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: TOKENS_QUERY_KEY })
      toast.success("Token removed.")
      onOpenChange(false)
    },
    onError: (err) => {
      if (err instanceof ApiError && err.status === 409) {
        const body = (err.body as ConflictBody) ?? {}
        const n = body.connection_count ?? 0
        toast.error(
          n > 0
            ? `Can't remove — ${n} connection${n === 1 ? "" : "s"} still use this token.`
            : "Can't remove — token is in use.",
        )
        return
      }
      const message =
        err instanceof ApiError
          ? err.message
          : err instanceof Error
            ? err.message
            : "Couldn’t remove token."
      toast.error(message)
    },
  })

  const onConfirm = () => {
    if (!token) return
    mutation.mutate(token.id)
  }

  const pending = mutation.isPending

  return (
    <AlertDialog
      open={token !== null}
      onOpenChange={(open) => {
        if (!open && !pending) onOpenChange(false)
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Remove this token?</AlertDialogTitle>
          <AlertDialogDescription>
            <span className="font-medium text-foreground">{token?.label}</span> will be
            destroyed. Any connections still attached to it must be removed first.
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
