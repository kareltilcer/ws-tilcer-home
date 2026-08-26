import { useEffect, useRef } from 'react'
import { useQueryClient, type QueryClient } from '@tanstack/react-query'
import { useLocation } from 'react-router-dom'
import { toast } from 'sonner'
import { qk } from './keys'
import { clientId } from './clientId'
import { ApiError, reportUnauthorized } from './client'
import { getSession } from './endpoints'
import { cs } from '@/i18n/cs'
import { routes } from '@/app/routes'

// WS_CLOSE_POLICY_VIOLATION (RFC 6455 §7.4.1, code 1008) is what the hub answers
// with when it closes a socket because the session behind it no longer authorises
// it. Every other code — a deploy, a dropped network, a browser sleeping — is a
// reconnect.
//
// ⚠ Exported so ws.test.ts can pin BOTH halves of it: that the number is 1008 —
// the wire contract with the backend's websocket.StatusPolicyViolation, which
// this file cannot import — and that a close carrying it stops the reconnect
// loop. Nothing else links the two sides.
export const WS_CLOSE_POLICY_VIOLATION = 1008

// PROBE_AFTER_FAILED_DIALS is how many dials may fail IN A ROW WITHOUT THE SOCKET
// EVER OPENING before the tab stops guessing and asks the server directly whether
// the session is still there.
//
// ⚠ The close code above only reaches a tab that was CONNECTED when the session
// went. The common case is the opposite one: a phone asleep or offline when the
// revocation lands has no socket to close, so DisconnectSession finds nothing and
// the frame is never sent. On resume it dials, the server 401s the UPGRADE, and
// the WebSocket API deliberately hides the handshake response — the browser
// reports code 1006, indistinguishable from a dropped network. Without a second
// signal that tab redials every capped interval (25.6s) for as long as it stays
// open, with nothing on screen ever saying the session ended: exactly the loop
// the close code was added to stop.
//
// ⚠ AND IT COUNTS ONLY DIALS THAT NEVER OPENED. A tab that connects, works, and
// is dropped by a deploy or a flaky link has a healthy session and must never be
// probed; `opened` resets the counter for that reason. Three is past any single
// restart or handover and still under a minute of backoff.
const PROBE_AFTER_FAILED_DIALS = 3

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
    // Dials that closed WITHOUT ever opening, in a row. See PROBE_AFTER_FAILED_DIALS.
    let unopened = 0
    let timer: ReturnType<typeof setTimeout> | undefined

    // giveUp stops the reconnect loop for good and hands off to the login screen,
    // which is the same place a 401 goes. Both routes into it — the policy close
    // code, and the session probe below — are answers to "the session is gone",
    // and neither may leave a timer armed behind it.
    const giveUp = () => {
      closed = true
      if (timer) clearTimeout(timer)
      ws?.close()
      reportUnauthorized()
    }

    // probeSession asks the one question the WebSocket API cannot: is this tab
    // failing to connect because the network is bad, or because the session is
    // over? getSession carries skipAuthRedirect, so a 401 arrives here as a value
    // instead of routing itself — which keeps the handoff in one place, and keeps
    // every OTHER failure (offline, 5xx, the server still booting) a reconnect.
    const probeSession = async () => {
      try {
        await getSession()
      } catch (e) {
        if (e instanceof ApiError && e.status === 401) giveUp()
      }
    }

    const connect = () => {
      if (closed) return
      // Per-socket, deliberately: a dial that reached `open` proves the session
      // authorised it, whatever happens to the connection afterwards.
      let opened = false
      const proto = location.protocol === 'https:' ? 'wss' : 'ws'
      // The session cookie rides the same-origin upgrade automatically (Mode B) —
      // no token in the URL.
      ws = new WebSocket(`${proto}://${location.host}/ws`)
      ws.onopen = () => {
        opened = true
        attempt = 0
        unopened = 0
      }
      ws.onmessage = (e) => {
        try {
          const msg = JSON.parse(e.data) as {
            type?: string
            origin?: string
            payload?: { private?: string }
          }
          if (!msg.type) return
          // Classify the change once and thread the module through cache
          // invalidation, route matching, and the toast — no repeated prefix scans.
          const mod = classify(msg.type)
          applyChange(qc, mod)
          // ⚠ A PRIVATE MUTATION INVALIDATES BUT NEVER TOASTS (v9, D180/D187). The
          // frame carries no id and no owner — see notes.Service.notifyScoped — so
          // this tab cannot tell whether the change was ours; the refetch is what
          // keeps the OWNER's other tabs correct. For everybody else the row is in a
          // tree they cannot see, and "Poznámky byly změněny jinde" over a change
          // that never appears is both noise and a live activity indicator over
          // another member's private tree.
          const isPrivate = msg.payload?.private === '1'
          // Toast only when the change carries a known origin that isn't us,
          // affects the route on screen, and this tab is actually visible — a
          // hidden tab picks the change up via refetch-on-focus on return. A push
          // with no origin (background job, cron, BE→BE, or any path that didn't
          // stamp our client id) is never toasted: we can't tell whether this very
          // tab made it, so we don't risk falsely reporting it as "changed elsewhere".
          if (
            !isPrivate &&
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
      ws.onclose = (e) => {
        if (closed) return
        // ⚠ A REVOCATION IS NOT A RESTART. The server closes with the policy code
        // when the session behind this socket is gone — revoked by a logout
        // elsewhere, dropped by a mint that failed closed, or simply expired —
        // and reconnecting then is pointless: the upgrade is 401ed, and because
        // `open` resets the backoff this tab would redial every capped interval
        // for as long as it stays open, with nothing on screen ever saying the
        // session ended. Stop, and hand off to the same login screen a 401 does.
        if (e.code === WS_CLOSE_POLICY_VIOLATION) {
          giveUp()
          return
        }
        // ⚠ The close code above is only the half the server gets to SAY. A tab
        // that was asleep or offline when the session went never receives it and
        // lands here instead, with 1006 from a 401ed upgrade — identical, to the
        // browser, to a dropped network. After enough dials that never opened,
        // ask the server outright rather than redialling forever.
        if (!opened) {
          unopened += 1
          if (unopened >= PROBE_AFTER_FAILED_DIALS) {
            unopened = 0
            void probeSession()
          }
        }
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

// A garden change invalidates the plan, its CHECK and the affected tasks
// together — one prefix covers all three. A stale check is worse than no check,
// because it is a green tick that has stopped being true.
const gardenModule: LiveModule = {
  route: routes.zahrada,
  keys: [['garden']],
  toast: { id: 'live-garden', message: cs.live.gardenUpdated },
}

// Elektřina's three computed views — summary, intervals, history — are
// projections of the same five tables, so one prefix invalidates all of them
// together. A summary left stale beside a fresh reading list is a number that
// has silently stopped being true.
const electricityModule: LiveModule = {
  route: routes.elektrina,
  keys: [['electricity']],
  toast: { id: 'live-electricity', message: cs.live.electricityUpdated },
}

// classify maps a change type to the module it belongs to, or null for types no
// screen tracks (which then invalidate only the dashboard and never toast).
function classify(type: string): LiveModule | null {
  if (type.startsWith('event')) return eventsModule
  // Checked before the note/folder prefixes: "document_folder.changed" belongs to
  // documents, not to Poznámky's folders.
  if (type.startsWith('document')) return documentsModule
  if (type.startsWith('finance')) return financeModule
  // Checked before the note/folder and card prefixes: every garden push is
  // "garden_*", so one test covers plants, beds, seasons, plantings, tasks,
  // harvests and storage.
  if (type.startsWith('garden')) return gardenModule
  // Every electricity push is "electricity_*", so one test covers readings,
  // tariffs, advances, payments and periods.
  if (type.startsWith('electricity')) return electricityModule
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
