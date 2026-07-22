import { QueryClient } from '@tanstack/react-query'

// refetchOnWindowFocus is the websocket-reconnect fallback (PRD §7). Auth
// failures (401/403) are not retried — the fetch wrapper handles refresh/redirect.
export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      refetchOnWindowFocus: true,
      retry: (failureCount, error) => {
        const status = (error as { status?: number } | null)?.status
        if (status === 401 || status === 403) return false
        return failureCount < 2
      },
      staleTime: 10_000,
    },
  },
})
