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
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Spinner } from "@/components/ui/spinner"
import { ApiError, apiPost } from "@/lib/api"
import type { components } from "@/lib/openapi"
import { CONNECTIONS_QUERY_KEY } from "@/lib/queries/connections"
import type { TokenDTO } from "@/components/connections/connection-row-meta"

type CreateRequest = components["schemas"]["CreateConnectionRequest"]
type Kind = "repo" | "org"

type AddConnectionDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  tokens: TokenDTO[] | undefined
}

type FieldErrors = {
  owner?: string
  name?: string
  token?: string
  backfill_from?: string
}

export function AddConnectionDialog({
  open,
  onOpenChange,
  tokens,
}: AddConnectionDialogProps) {
  const queryClient = useQueryClient()
  const [kind, setKind] = useState<Kind>("repo")
  const [owner, setOwner] = useState("")
  const [name, setName] = useState("")
  const [tokenId, setTokenId] = useState<string>(
    tokens?.[0]?.id != null ? String(tokens[0].id) : "",
  )
  const [backfillFrom, setBackfillFrom] = useState("")
  const [fieldErrors, setFieldErrors] = useState<FieldErrors>({})
  const [topError, setTopError] = useState<string | null>(null)

  const mutation = useMutation({
    mutationFn: (body: CreateRequest) => apiPost("/connections", { body }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: CONNECTIONS_QUERY_KEY })
      toast.success("Connection added.")
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
    const trimmedOwner = owner.trim()
    const trimmedName = name.trim()

    if (!trimmedOwner) clientErrors.owner = "Owner is required."
    if (kind === "repo" && !trimmedName) clientErrors.name = "Repo name is required."
    if (!tokenId) clientErrors.token = "Pick a token."

    if (Object.keys(clientErrors).length > 0) {
      setFieldErrors(clientErrors)
      return
    }

    const body: CreateRequest = {
      kind,
      owner: trimmedOwner,
      name: kind === "repo" ? trimmedName : null,
      token_id: Number(tokenId),
      backfill_from: backfillFrom ? new Date(backfillFrom).toISOString() : null,
    }

    mutation.mutate(body)
  }

  const pending = mutation.isPending

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Add connection</DialogTitle>
          <DialogDescription>
            Pick a target and the token that grants access. Backfill defaults to 90 days.
          </DialogDescription>
        </DialogHeader>

        <form noValidate onSubmit={onSubmit} className="contents">
          <FieldGroup>
            {topError && (
              <Alert variant="destructive">
                <AlertTitle>Couldn’t add connection</AlertTitle>
                <AlertDescription>{topError}</AlertDescription>
              </Alert>
            )}

            <Field>
              <FieldLabel htmlFor="kind">Kind</FieldLabel>
              <Select
                value={kind}
                onValueChange={(value) => {
                  if (value === "repo" || value === "org") setKind(value)
                }}
              >
                <SelectTrigger id="kind" className="w-full" disabled={pending}>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    <SelectItem value="repo">Repository</SelectItem>
                    <SelectItem value="org">Organization</SelectItem>
                  </SelectGroup>
                </SelectContent>
              </Select>
              <FieldDescription>
                Repos sync one project. Orgs enumerate every non-archived repo.
              </FieldDescription>
            </Field>

            <Field data-invalid={fieldErrors.owner ? true : undefined}>
              <FieldLabel htmlFor="owner">Owner</FieldLabel>
              <Input
                id="owner"
                autoComplete="off"
                placeholder={kind === "repo" ? "vercel" : "vercel"}
                aria-invalid={fieldErrors.owner ? true : undefined}
                value={owner}
                onChange={(e) => setOwner(e.target.value)}
                disabled={pending}
                required
              />
              {fieldErrors.owner && <FieldError>{fieldErrors.owner}</FieldError>}
            </Field>

            {kind === "repo" && (
              <Field data-invalid={fieldErrors.name ? true : undefined}>
                <FieldLabel htmlFor="name">Repo</FieldLabel>
                <Input
                  id="name"
                  autoComplete="off"
                  placeholder="next.js"
                  aria-invalid={fieldErrors.name ? true : undefined}
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  disabled={pending}
                  required
                />
                {fieldErrors.name && <FieldError>{fieldErrors.name}</FieldError>}
              </Field>
            )}

            <Field data-invalid={fieldErrors.token ? true : undefined}>
              <FieldLabel htmlFor="token">Token</FieldLabel>
              <Select value={tokenId} onValueChange={setTokenId}>
                <SelectTrigger id="token" className="w-full" disabled={pending}>
                  <SelectValue placeholder="Pick a token…" />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    {tokens?.map((t) => (
                      <SelectItem key={t.id} value={String(t.id)}>
                        {t.label}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
              {fieldErrors.token && <FieldError>{fieldErrors.token}</FieldError>}
            </Field>

            <Field data-invalid={fieldErrors.backfill_from ? true : undefined}>
              <FieldLabel htmlFor="backfill_from">Backfill from</FieldLabel>
              <Input
                id="backfill_from"
                type="date"
                aria-invalid={fieldErrors.backfill_from ? true : undefined}
                value={backfillFrom}
                onChange={(e) => setBackfillFrom(e.target.value)}
                disabled={pending}
              />
              {fieldErrors.backfill_from ? (
                <FieldError>{fieldErrors.backfill_from}</FieldError>
              ) : (
                <FieldDescription>Leave blank for the default 90-day window.</FieldDescription>
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
              Add connection
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

  if (err.status === 409) {
    setTopError("Connection already exists.")
    return
  }

  if (err.status === 400) {
    const msg = err.message
    const lower = msg.toLowerCase()
    if (lower.includes("token")) {
      setFieldErrors({ token: msg })
      return
    }
    if (lower.includes("owner")) {
      setFieldErrors({ owner: msg })
      return
    }
    if (lower.includes("name")) {
      setFieldErrors({ name: msg })
      return
    }
    if (lower.includes("backfill")) {
      setFieldErrors({ backfill_from: msg })
      return
    }
    setTopError(msg)
    return
  }

  setTopError(err.message || "Unexpected error, please try again.")
}
