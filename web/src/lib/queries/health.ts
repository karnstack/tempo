import { useQuery } from "@tanstack/react-query"

import { apiGet } from "@/lib/api"

export const HEALTH_QUERY_KEY = ["system", "health"] as const

export function useHealthQuery() {
  return useQuery({
    queryKey: HEALTH_QUERY_KEY,
    queryFn: () => apiGet("/system/health"),
    staleTime: Infinity,
    retry: false,
  })
}
