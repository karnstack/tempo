import { useState, type FormEvent } from "react"
import { useMutation, useQueryClient } from "@tanstack/react-query"

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
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
import { ME_QUERY_KEY } from "@/lib/queries/me"
import type { components } from "@/lib/openapi"

type AuthMode = "login" | "register"
type AuthResponse = components["schemas"]["MeResponse"]

type AuthFormProps = {
  mode: AuthMode
  onSuccess: (response: AuthResponse) => void
}

const MIN_PASSWORD_LENGTH = 8

const COPY = {
  login: {
    submit: "Sign in",
    passwordAutoComplete: "current-password",
    passwordHint: undefined,
  },
  register: {
    submit: "Create account",
    passwordAutoComplete: "new-password",
    passwordHint: `At least ${MIN_PASSWORD_LENGTH} characters.`,
  },
} as const

export function AuthForm({ mode, onSuccess }: AuthFormProps) {
  const queryClient = useQueryClient()
  const [email, setEmail] = useState("")
  const [password, setPassword] = useState("")
  const [emailError, setEmailError] = useState<string | null>(null)
  const [passwordError, setPasswordError] = useState<string | null>(null)
  const [topError, setTopError] = useState<string | null>(null)

  const mutation = useMutation({
    mutationFn: (input: { email: string; password: string }) =>
      apiPost(mode === "login" ? "/auth/login" : "/auth/register", {
        body: input,
      }),
    onSuccess: (response) => {
      queryClient.setQueryData(ME_QUERY_KEY, response)
      onSuccess(response as AuthResponse)
    },
    onError: (err) => {
      applyServerError(err, mode, {
        setEmailError,
        setPasswordError,
        setTopError,
      })
    },
  })

  const onSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    setEmailError(null)
    setPasswordError(null)
    setTopError(null)

    const trimmedEmail = email.trim()
    let clientInvalid = false
    if (!trimmedEmail) {
      setEmailError("Email is required.")
      clientInvalid = true
    }
    if (!password) {
      setPasswordError("Password is required.")
      clientInvalid = true
    } else if (mode === "register" && password.length < MIN_PASSWORD_LENGTH) {
      setPasswordError(
        `Password must be at least ${MIN_PASSWORD_LENGTH} characters.`,
      )
      clientInvalid = true
    }
    if (clientInvalid) return

    mutation.mutate({ email: trimmedEmail, password })
  }

  const copy = COPY[mode]
  const pending = mutation.isPending

  return (
    <form noValidate onSubmit={onSubmit}>
      <FieldGroup>
        {topError && (
          <Alert variant="destructive">
            <AlertTitle>Couldn’t {mode === "login" ? "sign in" : "create your account"}</AlertTitle>
            <AlertDescription>{topError}</AlertDescription>
          </Alert>
        )}

        <Field data-invalid={emailError ? true : undefined}>
          <FieldLabel htmlFor="email">Email</FieldLabel>
          <Input
            id="email"
            type="email"
            autoComplete="email"
            autoFocus
            aria-invalid={emailError ? true : undefined}
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            disabled={pending}
            required
          />
          {emailError && <FieldError>{emailError}</FieldError>}
        </Field>

        <Field data-invalid={passwordError ? true : undefined}>
          <FieldLabel htmlFor="password">Password</FieldLabel>
          <Input
            id="password"
            type="password"
            autoComplete={copy.passwordAutoComplete}
            aria-invalid={passwordError ? true : undefined}
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            disabled={pending}
            required
          />
          {passwordError ? (
            <FieldError>{passwordError}</FieldError>
          ) : (
            copy.passwordHint && <FieldDescription>{copy.passwordHint}</FieldDescription>
          )}
        </Field>

        <Button type="submit" disabled={pending} className="w-full">
          {pending && <Spinner data-icon="inline-start" />}
          {copy.submit}
        </Button>
      </FieldGroup>
    </form>
  )
}

type ErrorSetters = {
  setEmailError: (v: string | null) => void
  setPasswordError: (v: string | null) => void
  setTopError: (v: string | null) => void
}

function applyServerError(err: unknown, mode: AuthMode, setters: ErrorSetters) {
  if (!(err instanceof ApiError)) {
    setters.setTopError(
      err instanceof Error ? err.message : "Unexpected error, please try again.",
    )
    return
  }

  if (mode === "register") {
    if (err.status === 400) {
      const msg = err.message.toLowerCase()
      if (msg.includes("email")) {
        setters.setEmailError(err.message)
        return
      }
      if (msg.includes("password")) {
        setters.setPasswordError(err.message)
        return
      }
      setters.setTopError(err.message)
      return
    }
    if (err.status === 409) {
      setters.setTopError("Registration is closed. Please sign in instead.")
      return
    }
  }

  if (mode === "login" && err.status === 401) {
    setters.setTopError("Email or password is incorrect.")
    return
  }

  setters.setTopError(err.message || "Unexpected error, please try again.")
}
