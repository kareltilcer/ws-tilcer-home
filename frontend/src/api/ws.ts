import { useEffect } from 'react'
import { useQueryClient, type QueryClient } from '@tanstack/react-query'
import { qk } from './keys'

// useLiveSync opens the session-authenticated websocket and applies pushed
// changes by invalidating the affected query caches (refetch-on-focus is the
// reconnect fallback, configured on the QueryClient). Reconnects with capped
// backoff. In Mode B the browser sends the session cookie automatically on the
// same-origin upgrade — there is no bearer token.
export function useLiveSync(): void {
  const qc = useQueryClient()
  useEffect(() => {
    let closed = false
    let ws: WebSocket | null = null
    let attempt = 0
    let timer: ReturnType<typeof setTimeout> | undefined

    const connect = () => {
      if (closed) return
      const proto = location.protocol === 'https:' ? 'wss' : 'ws'
      // The session cookie rides the same-origin upgrade automatically (Mode B) —
      // no token in the URL.
      ws = new WebSocket(`${proto}://${location.host}/ws`)
      ws.onopen = () => {
        attempt = 0
      }
      ws.onmessage = (e) => {
        try {
          const msg = JSON.parse(e.data) as { type?: string }
          if (msg.type) applyChange(qc, msg.type)
        } catch {
          // ignore malformed frames
        }
      }
      ws.onclose = () => {
        if (closed) return
        attempt = Math.min(attempt + 1, 6)
        timer = setTimeout(connect, 400 * 2 ** attempt)
      }
      ws.onerror = () => ws?.close()
    }
    connect()

    return () => {
      closed = true
      if (timer) clearTimeout(timer)
      ws?.close()
    }
  }, [qc])
}

function applyChange(qc: QueryClient, type: string) {
  // The dashboard aggregates almost everything — always refresh it.
  void qc.invalidateQueries({ queryKey: qk.dashboard })

  if (type.startsWith('card') || type.startsWith('column') || type.startsWith('board') || type.startsWith('label')) {
    void qc.invalidateQueries({ queryKey: ['boards'] })
    void qc.invalidateQueries({ queryKey: ['board'] })
    void qc.invalidateQueries({ queryKey: ['card'] })
  }
  if (type.startsWith('event')) {
    void qc.invalidateQueries({ queryKey: ['events'] })
    void qc.invalidateQueries({ queryKey: ['event'] })
  }
}
