export const DEFAULT_AUTHED_DESTINATION = "/dashboard"

export function safeFromPath(raw: unknown): string {
  if (typeof raw !== "string") return DEFAULT_AUTHED_DESTINATION
  if (!raw.startsWith("/")) return DEFAULT_AUTHED_DESTINATION
  if (raw.startsWith("//")) return DEFAULT_AUTHED_DESTINATION
  if (raw.startsWith("/login") || raw.startsWith("/register")) {
    return DEFAULT_AUTHED_DESTINATION
  }
  return raw
}
