import type { ReactNode } from "react"

type AuthCardProps = {
  title: string
  description?: ReactNode
  children: ReactNode
  footer?: ReactNode
}

export function AuthCard({ title, description, children, footer }: AuthCardProps) {
  return (
    <main className="bg-background flex min-h-svh items-center justify-center p-6">
      <div className="flex w-full max-w-sm flex-col gap-6">
        <div className="flex items-center gap-2">
          <div className="bg-primary text-primary-foreground flex size-7 items-center justify-center rounded-md font-mono text-sm font-semibold">
            t
          </div>
          <div className="flex flex-col leading-tight">
            <span className="font-mono text-sm font-semibold">tempo</span>
            <span className="text-muted-foreground text-[10px] uppercase tracking-wider">
              engineering insights
            </span>
          </div>
        </div>

        <div className="bg-card text-card-foreground border-border flex flex-col gap-5 rounded-lg border p-6 shadow-sm">
          <div className="flex flex-col gap-1.5">
            <h1 className="text-base font-semibold leading-tight">{title}</h1>
            {description && (
              <p className="text-muted-foreground text-sm leading-snug">{description}</p>
            )}
          </div>
          {children}
        </div>

        {footer && (
          <p className="text-muted-foreground text-center text-xs">{footer}</p>
        )}
      </div>
    </main>
  )
}
