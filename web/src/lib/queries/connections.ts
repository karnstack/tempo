import { queryOptions, useQuery } from "@tanstack/react-query"

import { apiGet, ApiError } from "@/lib/api"

export const CONNECTIONS_QUERY_KEY = ["connections"] as const

export const connectionsQueryOptions = queryOptions({
  queryKey: CONNECTIONS_QUERY_KEY,
  queryFn: () => apiGet("/connections"),
  retry: (failureCount, error) => {
    if (error instanceof ApiError && error.status === 401) {
      return false
    }
    return failureCount < 2
  },
})

export function useConnectionsQuery() {
  return useQuery(connectionsQueryOptions)
}
