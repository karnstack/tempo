import type { ComponentType, SVGProps } from "react"

import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"

type PlaceholderProps = {
  icon: ComponentType<SVGProps<SVGSVGElement>>
  title: string
  description: string
  taskId: string
}

export function SectionPlaceholder({
  icon: Icon,
  title,
  description,
  taskId,
}: PlaceholderProps) {
  return (
    <div className="flex flex-1 items-center justify-center">
      <Empty className="border-dashed">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <Icon />
          </EmptyMedia>
          <EmptyTitle>{title}</EmptyTitle>
          <EmptyDescription>{description}</EmptyDescription>
        </EmptyHeader>
        <EmptyContent>
          <span className="text-muted-foreground font-mono text-xs">
            placeholder · {taskId}
          </span>
        </EmptyContent>
      </Empty>
    </div>
  )
}
