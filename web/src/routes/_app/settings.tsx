import { useState } from "react"
import { createFileRoute } from "@tanstack/react-router"
import { ClockIcon, KeyRoundIcon, ShieldAlertIcon, UserIcon } from "lucide-react"

import {
  Card,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { AddTokenDialog } from "@/components/settings/add-token-dialog"
import { DeleteTokenDialog } from "@/components/settings/delete-token-dialog"
import { TokenList } from "@/components/settings/token-list"
import type { TokenDTO } from "@/components/connections/connection-row-meta"
import { useTokensQuery } from "@/lib/queries/tokens"

type TabValue = "profile" | "tokens" | "polling" | "danger"

type SettingsSearch = {
  tab?: TabValue
}

const TABS: TabValue[] = ["profile", "tokens", "polling", "danger"]

export const Route = createFileRoute("/_app/settings")({
  head: () => ({ meta: [{ title: "Settings · tempo" }] }),
  validateSearch: (raw: Record<string, unknown>): SettingsSearch => {
    const tab = typeof raw.tab === "string" && TABS.includes(raw.tab as TabValue)
      ? (raw.tab as TabValue)
      : undefined
    return { tab }
  },
  component: SettingsPage,
})

function SettingsPage() {
  const search = Route.useSearch()
  const navigate = Route.useNavigate()
  const activeTab: TabValue = search.tab ?? "tokens"

  return (
    <Tabs
      value={activeTab}
      onValueChange={(value) => {
        if (typeof value !== "string") return
        void navigate({ search: { tab: value as TabValue }, replace: true })
      }}
      className="flex flex-col gap-6"
    >
      <TabsList>
        <TabsTrigger value="profile">
          <UserIcon data-icon="inline-start" />
          Profile
        </TabsTrigger>
        <TabsTrigger value="tokens">
          <KeyRoundIcon data-icon="inline-start" />
          Tokens
        </TabsTrigger>
        <TabsTrigger value="polling">
          <ClockIcon data-icon="inline-start" />
          Polling
        </TabsTrigger>
        <TabsTrigger value="danger">
          <ShieldAlertIcon data-icon="inline-start" />
          Danger
        </TabsTrigger>
      </TabsList>

      <TabsContent value="profile">
        <ComingSoonCard
          icon={UserIcon}
          title="Profile"
          description="Change admin email and password."
        />
      </TabsContent>

      <TabsContent value="tokens">
        <TokensTab />
      </TabsContent>

      <TabsContent value="polling">
        <ComingSoonCard
          icon={ClockIcon}
          title="Polling"
          description="Tune ingest cadence and retention windows."
        />
      </TabsContent>

      <TabsContent value="danger">
        <ComingSoonCard
          icon={ShieldAlertIcon}
          title="Danger zone"
          description="Wipe ingested data, reset rollups, delete the tenant."
        />
      </TabsContent>
    </Tabs>
  )
}

function TokensTab() {
  const tokensQuery = useTokensQuery()
  const [addOpen, setAddOpen] = useState(false)
  const [addKey, setAddKey] = useState(0)
  const [pendingDelete, setPendingDelete] = useState<TokenDTO | null>(null)

  const openAdd = () => {
    setAddKey((k) => k + 1)
    setAddOpen(true)
  }

  return (
    <>
      <TokenList
        tokens={tokensQuery.data?.tokens}
        isLoading={tokensQuery.isLoading}
        onAddClick={openAdd}
        onRemoveClick={(token) => setPendingDelete(token)}
      />

      <AddTokenDialog key={addKey} open={addOpen} onOpenChange={setAddOpen} />

      <DeleteTokenDialog
        token={pendingDelete}
        onOpenChange={(open) => {
          if (!open) setPendingDelete(null)
        }}
      />
    </>
  )
}

function ComingSoonCard({
  icon: Icon,
  title,
  description,
}: {
  icon: React.ComponentType
  title: string
  description: string
}) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>{title}</CardTitle>
        <CardDescription>{description}</CardDescription>
      </CardHeader>
      <div className="px-6 pb-6">
        <Empty className="border-dashed">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <Icon />
            </EmptyMedia>
            <EmptyTitle>Coming soon</EmptyTitle>
            <EmptyDescription>This tab is wired up but not implemented yet.</EmptyDescription>
          </EmptyHeader>
        </Empty>
      </div>
    </Card>
  )
}
