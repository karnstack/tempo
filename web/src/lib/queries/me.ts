import { useQuery } from "@tanstack/react-query"

import { apiGet, ApiError } from "@/lib/api"

export const ME_QUERY_KEY = ["me"] as const

export function useMeQuery() {
  return useQuery({
    queryKey: ME_QUERY_KEY,
    queryFn: () => apiGet("/me"),
    retry: (failureCount, error) => {
      if (error instanceof ApiError && error.status === 401) {
        return false
      }
      return failureCount < 2
    },
  })
}

export function isUnauthorized(error: unknown): boolean {
  return error instanceof ApiError && error.status === 401
}
