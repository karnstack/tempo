import { createFileRoute } from "@tanstack/react-router"

export const Route = createFileRoute("/login")({
  component: LoginPage,
})

function LoginPage() {
  return (
    <main className="flex min-h-svh items-center justify-center p-6">
      <div className="border-border bg-card text-card-foreground w-full max-w-sm rounded-lg border p-8 shadow-sm">
        <h1 className="font-mono text-lg font-semibold tracking-tight">tempo</h1>
        <p className="text-muted-foreground mt-1 text-sm">
          Log in to continue.
        </p>
        <p className="text-muted-foreground mt-6 font-mono text-xs">
          placeholder · 0048 will wire the form
        </p>
      </div>
    </main>
  )
}
