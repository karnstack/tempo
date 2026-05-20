import { KeyRoundIcon, MoreHorizontalIcon, PlusIcon, Trash2Icon } from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Skeleton } from "@/components/ui/skeleton"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import {
  type TokenDTO,
  shortDate,
  tooltipDate,
} from "@/components/connections/connection-row-meta"

type TokenListProps = {
  tokens: TokenDTO[] | undefined
  isLoading: boolean
  onAddClick: () => void
  onRemoveClick: (token: TokenDTO) => void
}

export function TokenList({
  tokens,
  isLoading,
  onAddClick,
  onRemoveClick,
}: TokenListProps) {
  const hasTokens = !isLoading && (tokens?.length ?? 0) > 0

  return (
    <Card>
      <CardHeader>
        <CardTitle>GitHub tokens</CardTitle>
        <CardDescription>
          Personal access tokens tempo uses to poll GitHub. Stored encrypted; never
          shown again after creation.
        </CardDescription>
        {hasTokens && (
          <CardAction>
            <Button onClick={onAddClick} size="sm">
              <PlusIcon data-icon="inline-start" />
              Add token
            </Button>
          </CardAction>
        )}
      </CardHeader>
      <CardContent className="px-0 pb-0">
        {isLoading ? (
          <TokenListSkeleton />
        ) : hasTokens ? (
          <TokenTable tokens={tokens!} onRemoveClick={onRemoveClick} />
        ) : (
          <div className="px-6 pb-6">
            <Empty className="border-dashed">
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <KeyRoundIcon />
                </EmptyMedia>
                <EmptyTitle>No tokens yet</EmptyTitle>
                <EmptyDescription>
                  Add a GitHub PAT with <span className="font-mono">repo</span> and{" "}
                  <span className="font-mono">read:org</span> scopes so tempo can poll.
                </EmptyDescription>
              </EmptyHeader>
              <EmptyContent>
                <Button onClick={onAddClick}>
                  <PlusIcon data-icon="inline-start" />
                  Add token
                </Button>
              </EmptyContent>
            </Empty>
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function TokenTable({
  tokens,
  onRemoveClick,
}: {
  tokens: TokenDTO[]
  onRemoveClick: (token: TokenDTO) => void
}) {
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead className="pl-6">Label</TableHead>
          <TableHead>Scopes</TableHead>
          <TableHead>Expires</TableHead>
          <TableHead>Added</TableHead>
          <TableHead className="w-12 pr-6 text-right" />
        </TableRow>
      </TableHeader>
      <TableBody>
        {tokens.map((token) => (
          <TableRow key={token.id}>
            <TableCell className="pl-6 font-medium">{token.label}</TableCell>
            <TableCell>
              <ScopesCell scopes={token.scopes} />
            </TableCell>
            <TableCell className="text-muted-foreground">
              <span title={tooltipDate(token.expires_at)}>
                {token.expires_at ? shortDate(token.expires_at) : "Never"}
              </span>
            </TableCell>
            <TableCell className="text-muted-foreground">
              <span title={tooltipDate(token.created_at)}>
                {shortDate(token.created_at)}
              </span>
            </TableCell>
            <TableCell className="w-12 pr-6 text-right">
              <DropdownMenu>
                <DropdownMenuTrigger
                  render={
                    <Button
                      variant="ghost"
                      size="icon-sm"
                      aria-label={`Open actions for ${token.label}`}
                    />
                  }
                >
                  <MoreHorizontalIcon />
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end">
                  <DropdownMenuGroup>
                    <DropdownMenuItem
                      variant="destructive"
                      onClick={() => onRemoveClick(token)}
                    >
                      <Trash2Icon data-icon="inline-start" />
                      Remove
                    </DropdownMenuItem>
                  </DropdownMenuGroup>
                </DropdownMenuContent>
              </DropdownMenu>
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}

function ScopesCell({ scopes }: { scopes: string }) {
  const parts = scopes
    .split(",")
    .map((s) => s.trim())
    .filter(Boolean)
  if (parts.length === 0) return <span className="text-muted-foreground">—</span>
  return (
    <div className="flex flex-wrap gap-1">
      {parts.map((s) => (
        <Badge key={s} variant="outline" className="font-mono text-[10px]">
          {s}
        </Badge>
      ))}
    </div>
  )
}

function TokenListSkeleton() {
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead className="pl-6">Label</TableHead>
          <TableHead>Scopes</TableHead>
          <TableHead>Expires</TableHead>
          <TableHead>Added</TableHead>
          <TableHead className="w-12 pr-6 text-right" />
        </TableRow>
      </TableHeader>
      <TableBody>
        {[0, 1].map((i) => (
          <TableRow key={i}>
            <TableCell className="pl-6">
              <Skeleton className="h-4 w-32" />
            </TableCell>
            <TableCell>
              <Skeleton className="h-5 w-24" />
            </TableCell>
            <TableCell>
              <Skeleton className="h-4 w-20" />
            </TableCell>
            <TableCell>
              <Skeleton className="h-4 w-20" />
            </TableCell>
            <TableCell className="w-12 pr-6 text-right">
              <Skeleton className="ml-auto size-8" />
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}
