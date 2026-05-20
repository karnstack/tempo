import { MoreHorizontalIcon, PlusIcon, PlugIcon, Trash2Icon } from "lucide-react"

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
  type ConnectionDTO,
  type TokenDTO,
  connectionLabel,
  relativeTime,
  shortDate,
  tokenLabel,
  tooltipDate,
} from "@/components/connections/connection-row-meta"

type ConnectionListProps = {
  connections: ConnectionDTO[] | undefined
  tokens: TokenDTO[] | undefined
  isLoading: boolean
  canAdd: boolean
  onAddClick: () => void
  onRemoveClick: (conn: ConnectionDTO) => void
}

export function ConnectionList({
  connections,
  tokens,
  isLoading,
  canAdd,
  onAddClick,
  onRemoveClick,
}: ConnectionListProps) {
  const hasConnections = !isLoading && (connections?.length ?? 0) > 0

  return (
    <Card>
      <CardHeader>
        <CardTitle>Connections</CardTitle>
        <CardDescription>
          Repos and orgs tempo polls for events. Removing a connection stops polling
          but keeps already-ingested data.
        </CardDescription>
        {hasConnections && (
          <CardAction>
            <Button onClick={onAddClick} disabled={!canAdd} size="sm">
              <PlusIcon data-icon="inline-start" />
              Add connection
            </Button>
          </CardAction>
        )}
      </CardHeader>
      <CardContent className="px-0 pb-0">
        {isLoading ? (
          <ConnectionListSkeleton />
        ) : hasConnections ? (
          <ConnectionTable
            connections={connections!}
            tokens={tokens}
            onRemoveClick={onRemoveClick}
          />
        ) : (
          <div className="px-6 pb-6">
            <Empty className="border-dashed">
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <PlugIcon />
                </EmptyMedia>
                <EmptyTitle>No connections yet</EmptyTitle>
                <EmptyDescription>
                  Add a GitHub repo or org to start ingesting PRs, reviews, and deploys.
                </EmptyDescription>
              </EmptyHeader>
              <EmptyContent>
                <Button onClick={onAddClick} disabled={!canAdd}>
                  <PlusIcon data-icon="inline-start" />
                  Add connection
                </Button>
              </EmptyContent>
            </Empty>
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function ConnectionTable({
  connections,
  tokens,
  onRemoveClick,
}: {
  connections: ConnectionDTO[]
  tokens: TokenDTO[] | undefined
  onRemoveClick: (conn: ConnectionDTO) => void
}) {
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead className="pl-6">Kind</TableHead>
          <TableHead>Target</TableHead>
          <TableHead>Token</TableHead>
          <TableHead>Status</TableHead>
          <TableHead>Last sync</TableHead>
          <TableHead>Added</TableHead>
          <TableHead className="w-12 pr-6 text-right" />
        </TableRow>
      </TableHeader>
      <TableBody>
        {connections.map((conn) => (
          <TableRow key={conn.id}>
            <TableCell className="pl-6">
              <Badge variant="outline" className="font-mono text-[10px] uppercase">
                {conn.kind}
              </Badge>
            </TableCell>
            <TableCell className="font-medium">{connectionLabel(conn)}</TableCell>
            <TableCell className="text-muted-foreground">
              {tokenLabel(tokens, conn.token_id)}
            </TableCell>
            <TableCell>
              <StatusBadge status={conn.status} />
            </TableCell>
            <TableCell className="text-muted-foreground">
              <span title={tooltipDate(conn.last_sync_at)}>
                {relativeTime(conn.last_sync_at)}
              </span>
            </TableCell>
            <TableCell className="text-muted-foreground">
              <span title={tooltipDate(conn.created_at)}>
                {shortDate(conn.created_at)}
              </span>
            </TableCell>
            <TableCell className="w-12 pr-6 text-right">
              <DropdownMenu>
                <DropdownMenuTrigger
                  render={
                    <Button
                      variant="ghost"
                      size="icon-sm"
                      aria-label={`Open actions for ${connectionLabel(conn)}`}
                    />
                  }
                >
                  <MoreHorizontalIcon />
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end">
                  <DropdownMenuGroup>
                    <DropdownMenuItem
                      variant="destructive"
                      onClick={() => onRemoveClick(conn)}
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

function StatusBadge({ status }: { status: string }) {
  const variant = status === "active" ? "default" : "secondary"
  return <Badge variant={variant}>{status}</Badge>
}

function ConnectionListSkeleton() {
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead className="pl-6">Kind</TableHead>
          <TableHead>Target</TableHead>
          <TableHead>Token</TableHead>
          <TableHead>Status</TableHead>
          <TableHead>Last sync</TableHead>
          <TableHead>Added</TableHead>
          <TableHead className="w-12 pr-6 text-right" />
        </TableRow>
      </TableHeader>
      <TableBody>
        {[0, 1, 2].map((i) => (
          <TableRow key={i}>
            <TableCell className="pl-6">
              <Skeleton className="h-5 w-12" />
            </TableCell>
            <TableCell>
              <Skeleton className="h-4 w-40" />
            </TableCell>
            <TableCell>
              <Skeleton className="h-4 w-20" />
            </TableCell>
            <TableCell>
              <Skeleton className="h-5 w-16" />
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
