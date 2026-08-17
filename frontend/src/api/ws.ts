import { useEffect, useRef } from 'react'
import { useQueryClient, type QueryClient } from '@tanstack/react-query'
import { useLocation } from 'react-router-dom'
import { toast } from 'sonner'
import { qk } from './keys'
import { clientId } from './clientId'
import { cs } from '@/i18n/cs'
import { routes } from '@/app/routes'

// useLiveSync opens the session-authenticated websocket and applies pushed
// changes by invalidating the affected query caches (refetch-on-focus is the
// reconnect fallback, configured on the QueryClient). Reconnects with capped
// backoff. In Mode B the browser sends the session cookie automatically on the
// same-origin upgrade — there is no bearer token.
//
// A change made ELSEWHERE (another device or tab) that touches the screen the
// user is currently on also raises a brief toast, so the live update isn't a
// silent surprise. Our own optimistic mutations echo back carrying our clientId
// as `origin` and are never toasted — they were already applied here. The cache
// invalidation runs for every push regardless, reconciling optimistic state.
export function useLiveSync(): void {
  const qc = useQueryClient()
  // Current path held in a ref so the long-lived socket handler always reads the
  // latest route without tearing the socket down and reconnecting on navigation.
  const pathname = useLocation().pathname
  const pathRef = useRef(pathname)
  pathRef.current = pathname

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
          const msg = JSON.parse(e.data) as { type?: string; origin?: string }
          if (!msg.type) return
          // Classify the change once and thread the module through cache
          // invalidation, route matching, and the toast — no repeated prefix scans.
          const mod = classify(msg.type)
          applyChange(qc, mod)
          // Toast only when the change carries a known origin that isn't us,
          // affects the route on screen, and this tab is actually visible — a
          // hidden tab picks the change up via refetch-on-focus on return. A push
          // with no origin (background job, cron, BE→BE, or any path that didn't
          // stamp our client id) is never toasted: we can't tell whether this very
          // tab made it, so we don't risk falsely reporting it as "changed elsewhere".
          if (
            msg.origin &&
            msg.origin !== clientId &&
            document.visibilityState === 'visible' &&
            affectsCurrentRoute(mod, pathRef.current)
          ) {
            notifyChanged(mod)
          }
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

// A live module groups the change types that surface together on one screen.
// The type prefix is classified once (see classify) and every consumer — cache
// invalidation, route matching, and the toast — reads from the same record, so
// a new change category or a renamed prefix is edited in exactly one place.
interface LiveModule {
  route: string
  // Query caches this module feeds, beyond the always-refreshed dashboard.
  keys: readonly (readonly unknown[])[]
  // Stable toast id + message, so a burst of pushes collapses into one notice.
  toast: { id: string; message: string }
}

const todoModule: LiveModule = {
  route: routes.ukoly,
  keys: [['boards'], ['board'], ['card']],
  toast: { id: 'live-todo', message: cs.live.tasksUpdated },
}

const eventsModule: LiveModule = {
  route: routes.okno,
  keys: [['events'], ['event']],
  toast: { id: 'live-events', message: cs.live.eventsUpdated },
}

const notesModule: LiveModule = {
  route: routes.poznamky,
  keys: [['notes']],
  toast: { id: 'live-notes', message: cs.live.notesUpdated },
}

const documentsModule: LiveModule = {
  route: routes.dokumenty,
  keys: [['documents']],
  toast: { id: 'live-documents', message: cs.live.documentsUpdated },
}

const financeModule: LiveModule = {
  route: routes.finance,
  keys: [['finance']],
  toast: { id: 'live-finance', message: cs.live.financeUpdated },
}

// classify maps a change type to the module it belongs to, or null for types no
// screen tracks (which then invalidate only the dashboard and never toast).
function classify(type: string): LiveModule | null {
  if (type.startsWith('event')) return eventsModule
  // Checked before the note/folder prefixes: "document_folder.changed" belongs to
  // documents, not to Poznámky's folders.
  if (type.startsWith('document')) return documentsModule
  if (type.startsWith('finance')) return financeModule
  if (type.startsWith('note') || type.startsWith('folder')) return notesModule
  if (
    type.startsWith('card') ||
    type.startsWith('column') ||
    type.startsWith('board') ||
    type.startsWith('label')
  ) {
    return todoModule
  }
  return null
}

function applyChange(qc: QueryClient, mod: LiveModule | null) {
  // The dashboard aggregates almost everything — always refresh it.
  void qc.invalidateQueries({ queryKey: qk.dashboard })
  if (!mod) return
  for (const key of mod.keys) {
    void qc.invalidateQueries({ queryKey: key })
  }
}

// affectsCurrentRoute reports whether a change owned by `mod` is visible on the
// route at `path`. The dashboard ("/") aggregates every module, so any change
// shows there; each other screen tracks its own module. Matching is anchored on
// a segment boundary so a route never matches a sibling that merely shares its
// prefix (e.g. "/okno" must not match a hypothetical "/oknoarchiv"). Routes with
// no module (e.g. the admin log) receive no live pushes, so never toast.
function affectsCurrentRoute(mod: LiveModule | null, path: string): boolean {
  if (path === routes.nastenka) return true
  if (!mod) return false
  return path === mod.route || path.startsWith(mod.route + '/')
}

// notifyChanged raises one toast per change category, reusing a stable id so a
// burst of pushes (e.g. someone reordering several cards elsewhere) collapses
// into a single, self-updating toast rather than a stack. A null module (a
// change no screen tracks) never toasts.
function notifyChanged(mod: LiveModule | null) {
  if (mod) toast(mod.toast.message, { id: mod.toast.id })
}
