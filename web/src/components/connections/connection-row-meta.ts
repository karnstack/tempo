import type { components } from "@/lib/openapi"

export type ConnectionDTO = components["schemas"]["ConnectionDTO"]
export type TokenDTO = components["schemas"]["TokenDTO"]

const RELATIVE_UNITS: Array<[Intl.RelativeTimeFormatUnit, number]> = [
  ["year", 60 * 60 * 24 * 365],
  ["month", 60 * 60 * 24 * 30],
  ["week", 60 * 60 * 24 * 7],
  ["day", 60 * 60 * 24],
  ["hour", 60 * 60],
  ["minute", 60],
  ["second", 1],
]

const rtf = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" })

export function relativeTime(iso: string | null | undefined): string {
  if (!iso) return "—"
  const date = new Date(iso)
  const diffSec = Math.round((date.getTime() - Date.now()) / 1000)
  const absSec = Math.abs(diffSec)
  for (const [unit, secInUnit] of RELATIVE_UNITS) {
    if (absSec >= secInUnit || unit === "second") {
      const value = Math.round(diffSec / secInUnit)
      return rtf.format(value, unit)
    }
  }
  return "just now"
}

const dateFmt = new Intl.DateTimeFormat(undefined, {
  year: "numeric",
  month: "short",
  day: "numeric",
})

export function shortDate(iso: string | null | undefined): string {
  if (!iso) return "—"
  return dateFmt.format(new Date(iso))
}

const isoFmt = new Intl.DateTimeFormat(undefined, {
  dateStyle: "medium",
  timeStyle: "short",
})

export function tooltipDate(iso: string | null | undefined): string {
  if (!iso) return ""
  return isoFmt.format(new Date(iso))
}

export function connectionLabel(conn: ConnectionDTO): string {
  if (conn.kind === "org" || !conn.name) return conn.owner
  return `${conn.owner}/${conn.name}`
}

export function tokenLabel(tokens: TokenDTO[] | undefined, id: number): string {
  const t = tokens?.find((x) => x.id === id)
  return t?.label ?? `#${id}`
}
