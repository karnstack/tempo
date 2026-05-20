import { useState, type FormEvent } from "react"
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { toast } from "sonner"

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Spinner } from "@/components/ui/spinner"
import { ApiError, apiPost } from "@/lib/api"
import type { components } from "@/lib/openapi"
import { TOKENS_QUERY_KEY } from "@/lib/queries/tokens"

type CreateRequest = components["schemas"]["CreateTokenRequest"]

type AddTokenDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
}

type FieldErrors = {
  label?: string
  pat?: string
  scopes?: string
  expires_at?: string
}

export function AddTokenDialog({ open, onOpenChange }: AddTokenDialogProps) {
  const queryClient = useQueryClient()
  const [label, setLabel] = useState("")
  const [pat, setPat] = useState("")
  const [scopes, setScopes] = useState("repo,read:org")
  const [expiresAt, setExpiresAt] = useState("")
  const [fieldErrors, setFieldErrors] = useState<FieldErrors>({})
  const [topError, setTopError] = useState<string | null>(null)

  const mutation = useMutation({
    mutationFn: (body: CreateRequest) => apiPost("/tokens", { body }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: TOKENS_QUERY_KEY })
      toast.success("Token added.")
      onOpenChange(false)
    },
    onError: (err) => {
      applyServerError(err, setFieldErrors, setTopError)
    },
  })

  const onSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    setFieldErrors({})
    setTopError(null)

    const clientErrors: FieldErrors = {}
    const trimmedLabel = label.trim()
    const trimmedPat = pat.trim()
    if (!trimmedLabel) clientErrors.label = "Label is required."
    if (!trimmedPat) clientErrors.pat = "Token is required."

    if (Object.keys(clientErrors).length > 0) {
      setFieldErrors(clientErrors)
      return
    }

    const body: CreateRequest = {
      label: trimmedLabel,
      pat: trimmedPat,
      scopes: scopes.trim(),
      expires_at: expiresAt ? new Date(expiresAt).toISOString() : null,
    }

    mutation.mutate(body)
  }

  const pending = mutation.isPending

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Add GitHub token</DialogTitle>
          <DialogDescription>
            Tempo encrypts and stores the PAT. It's never displayed again after creation.
          </DialogDescription>
        </DialogHeader>

        <form noValidate onSubmit={onSubmit} className="contents">
          <FieldGroup>
            {topError && (
              <Alert variant="destructive">
                <AlertTitle>Couldn’t add token</AlertTitle>
                <AlertDescription>{topError}</AlertDescription>
              </Alert>
            )}

            <Field data-invalid={fieldErrors.label ? true : undefined}>
              <FieldLabel htmlFor="label">Label</FieldLabel>
              <Input
                id="label"
                autoComplete="off"
                placeholder="ci-bot or my-laptop"
                aria-invalid={fieldErrors.label ? true : undefined}
                value={label}
                onChange={(e) => setLabel(e.target.value)}
                disabled={pending}
                required
              />
              {fieldErrors.label ? (
                <FieldError>{fieldErrors.label}</FieldError>
              ) : (
                <FieldDescription>Shown when picking a token for a connection.</FieldDescription>
              )}
            </Field>

            <Field data-invalid={fieldErrors.pat ? true : undefined}>
              <FieldLabel htmlFor="pat">Personal access token</FieldLabel>
              <Input
                id="pat"
                type="password"
                autoComplete="off"
                placeholder="ghp_… or github_pat_…"
                aria-invalid={fieldErrors.pat ? true : undefined}
                value={pat}
                onChange={(e) => setPat(e.target.value)}
                disabled={pending}
                required
              />
              {fieldErrors.pat ? (
                <FieldError>{fieldErrors.pat}</FieldError>
              ) : (
                <FieldDescription>
                  Fine-grained or classic. Scopes below should match.
                </FieldDescription>
              )}
            </Field>

            <Field data-invalid={fieldErrors.scopes ? true : undefined}>
              <FieldLabel htmlFor="scopes">Scopes</FieldLabel>
              <Input
                id="scopes"
                autoComplete="off"
                placeholder="repo,read:org"
                aria-invalid={fieldErrors.scopes ? true : undefined}
                value={scopes}
                onChange={(e) => setScopes(e.target.value)}
                disabled={pending}
              />
              {fieldErrors.scopes ? (
                <FieldError>{fieldErrors.scopes}</FieldError>
              ) : (
                <FieldDescription>Comma-separated. Informational only.</FieldDescription>
              )}
            </Field>

            <Field data-invalid={fieldErrors.expires_at ? true : undefined}>
              <FieldLabel htmlFor="expires_at">Expires</FieldLabel>
              <Input
                id="expires_at"
                type="date"
                aria-invalid={fieldErrors.expires_at ? true : undefined}
                value={expiresAt}
                onChange={(e) => setExpiresAt(e.target.value)}
                disabled={pending}
              />
              {fieldErrors.expires_at ? (
                <FieldError>{fieldErrors.expires_at}</FieldError>
              ) : (
                <FieldDescription>Leave blank for no expiry tracking.</FieldDescription>
              )}
            </Field>
          </FieldGroup>

          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={() => onOpenChange(false)}
              disabled={pending}
            >
              Cancel
            </Button>
            <Button type="submit" disabled={pending}>
              {pending && <Spinner data-icon="inline-start" />}
              Add token
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function applyServerError(
  err: unknown,
  setFieldErrors: (e: FieldErrors) => void,
  setTopError: (v: string | null) => void,
) {
  if (!(err instanceof ApiError)) {
    setTopError(err instanceof Error ? err.message : "Unexpected error, please try again.")
    return
  }

  if (err.status === 400) {
    const msg = err.message
    const lower = msg.toLowerCase()
    if (lower.includes("label")) {
      setFieldErrors({ label: msg })
      return
    }
    if (lower.includes("pat") || lower.includes("token")) {
      setFieldErrors({ pat: msg })
      return
    }
    setTopError(msg)
    return
  }

  setTopError(err.message || "Unexpected error, please try again.")
}
