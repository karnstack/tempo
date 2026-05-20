import { queryOptions, useQuery } from "@tanstack/react-query"

import { apiGet } from "@/lib/api"

export const FIRSTRUN_QUERY_KEY = ["firstrun"] as const

export const firstRunQueryOptions = queryOptions({
  queryKey: FIRSTRUN_QUERY_KEY,
  queryFn: () => apiGet("/auth/firstrun"),
  staleTime: 60_000,
})

export function useFirstRunQuery() {
  return useQuery(firstRunQueryOptions)
}
