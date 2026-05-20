import { queryOptions, useQuery } from "@tanstack/react-query"

import { apiGet, ApiError } from "@/lib/api"

export const TOKENS_QUERY_KEY = ["tokens"] as const

export const tokensQueryOptions = queryOptions({
  queryKey: TOKENS_QUERY_KEY,
  queryFn: () => apiGet("/tokens"),
  retry: (failureCount, error) => {
    if (error instanceof ApiError && error.status === 401) {
      return false
    }
    return failureCount < 2
  },
})

export function useTokensQuery() {
  return useQuery(tokensQueryOptions)
}
