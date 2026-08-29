// The per-device push subscription (PRD FR-P1–P3/P5, HANDOFF-design §v5 §8).
//
// The hard part here is not the API, it is the PERMISSION GAUNTLET. The browser
// prompt is OS-owned and effectively one-shot: if a member dismisses or blocks
// it, the app can never ask again. So this hook models every outcome explicitly
// — unsupported / default / granted / denied — and the UI primes for intent
// before the prompt is ever fired, so nobody spends their one prompt by accident.
//
// Everything here is per DEVICE, not per account: a phone and a laptop subscribe
// separately, and the panel says so.

import { useCallback, useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { qk } from '@/api/keys'
import { getPushPreferences, getVapidKey, registerPushSubscription, removePushSubscription, updatePushPreferences } from '@/api/push'
import type { PushCategories, PushPreferences } from '@/api/types'
import { ApiError } from '@/api/client'
import { cs } from '@/i18n/cs'

/** What this device can do about notifications right now. */
export type PushSupport =
  /** No Push API at all (an old browser, or iOS Safari outside an installed PWA). */
  | 'unsupported'
  /** Supported, permission not yet decided — the prompt is still available. */
  | 'prompt'
  /** Permission granted. */
  | 'granted'
  /** Blocked. The app CANNOT re-prompt; only browser settings can undo this. */
  | 'blocked'

export interface PushState {
  support: PushSupport
  /** True when this device has a subscription registered with the server. */
  subscribed: boolean
  /** The server has no VAPID keypair — push is off for the whole household. */
  serverDisabled: boolean
  busy: boolean
  /** Set when the last permission request was dismissed without a decision. */
  dismissed: boolean
  error: string | null
}

/** pushSupported reports whether this browser can do Web Push at all. */
export function pushSupported(): boolean {
  return (
    typeof window !== 'undefined' &&
    'serviceWorker' in navigator &&
    'PushManager' in window &&
    'Notification' in window
  )
}

function currentSupport(): PushSupport {
  if (!pushSupported()) return 'unsupported'
  switch (Notification.permission) {
    case 'granted':
      return 'granted'
    case 'denied':
      return 'blocked'
    default:
      return 'prompt'
  }
}

/** urlBase64ToUint8Array converts the VAPID public key to the byte array
 *  PushManager.subscribe demands. */
function urlBase64ToUint8Array(base64: string): Uint8Array<ArrayBuffer> {
  const padding = '='.repeat((4 - (base64.length % 4)) % 4)
  const normalized = (base64 + padding).replace(/-/g, '+').replace(/_/g, '/')
  const raw = atob(normalized)
  // Allocate the backing ArrayBuffer explicitly: applicationServerKey demands an
  // ArrayBuffer-backed view, and a plain `new Uint8Array(n)` widens to
  // ArrayBufferLike (which could be a SharedArrayBuffer) under TS 6.
  const buffer = new ArrayBuffer(raw.length)
  const out = new Uint8Array(buffer)
  for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i)
  return out
}

function bufferToBase64Url(buffer: ArrayBuffer | null): string {
  if (!buffer) return ''
  const bytes = new Uint8Array(buffer)
  let binary = ''
  for (const b of bytes) binary += String.fromCharCode(b)
  return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
}

/** currentSubscription reads this browser's push subscription, if any, without
 *  waiting forever on a service worker that may never register. */
async function currentSubscription(): Promise<PushSubscription | null> {
  if (!pushSupported()) return null
  const reg = await navigator.serviceWorker.getRegistration()
  return (await reg?.pushManager.getSubscription()) ?? null
}

/** How long to wait for the service worker to take control before deciding this
 *  browser is not going to give us one. */
const SW_READY_TIMEOUT_MS = 10000

/** serviceWorkerReady resolves the active registration, or null.
 *
 *  `navigator.serviceWorker.ready` is a trap: it never rejects and never settles
 *  at all when no worker is registered — which happens whenever registration was
 *  refused (a private-browsing window) or skipped (dev). Awaiting it bare leaves
 *  the caller hanging forever with no error to show, so every await goes through
 *  here and a browser that will not produce a worker says so. */
async function serviceWorkerReady(): Promise<ServiceWorkerRegistration | null> {
  if (!pushSupported()) return null
  let timer: ReturnType<typeof setTimeout> | undefined
  try {
    return await Promise.race([
      navigator.serviceWorker.ready,
      new Promise<null>((resolve) => {
        timer = setTimeout(() => resolve(null), SW_READY_TIMEOUT_MS)
      }),
    ])
  } catch {
    return null
  } finally {
    clearTimeout(timer)
  }
}

/** usePushKeepalive re-registers this device's existing endpoint with the server,
 *  once per app load. It is the repair path for an endpoint the browser rotated:
 *  the service worker tries first from `pushsubscriptionchange`, but it can only
 *  authenticate that call where CookieStore exists (no `document.cookie` in a
 *  worker), so on the browsers that lack it this is the ONLY thing that
 *  reconnects the device — and the failure it repairs is silent, so it cannot
 *  depend on the member opening the settings screen.
 *
 *  The upsert is idempotent and deliberately unaudited on refresh, so a no-op
 *  run costs one request. Mounted once, in AppShell. */
export function usePushKeepalive(): void {
  useEffect(() => {
    let cancelled = false
    void (async () => {
      try {
        const sub = await currentSubscription()
        if (!sub || cancelled) return
        const p256dh = bufferToBase64Url(sub.getKey('p256dh'))
        const auth = bufferToBase64Url(sub.getKey('auth'))
        if (!p256dh || !auth) return
        await registerPushSubscription({
          endpoint: sub.endpoint,
          keys: { p256dh, auth },
          user_agent: navigator.userAgent,
        })
      } catch {
        /* offline, or push disabled server-side — the next load tries again */
      }
    })()
    return () => {
      cancelled = true
    }
  }, [])
}

/** How long logout will wait for the server to drop this device. */
const RELEASE_TIMEOUT_MS = 3000

/** releasePushDevice unsubscribes this browser, server-side and locally. It is
 *  called on an EXPLICIT logout: a subscription that outlives the session keeps
 *  pushing the departing member's household notifications to a device somebody
 *  else may now be using, which is the same reason the persisted query cache is
 *  purged there.
 *
 *  It is deliberately NOT called on an involuntary 401 — an expired session is
 *  not a decision to leave this device, and silently turning notifications off
 *  would make the member re-run the whole permission flow to get them back. */
export async function releasePushDevice(): Promise<void> {
  const sub = await currentSubscription()
  if (!sub) return
  try {
    // Server first: the endpoint is authenticated, so it has to go before the
    // session does — which means logout waits on it. Bounded, so a slow or
    // unreachable backend cannot leave someone stuck on a screen they asked to
    // leave. Giving up costs nothing: the local unsubscribe below still stops
    // this browser receiving, and the orphaned row is pruned by the first send
    // that gets a 410 from the now-dead endpoint.
    await Promise.race([
      removePushSubscription(sub.endpoint),
      new Promise((_, reject) => setTimeout(() => reject(new Error('timeout')), RELEASE_TIMEOUT_MS)),
    ])
  } finally {
    await sub.unsubscribe()
  }
}

/** usePushDevice manages THIS device's subscription. */
export function usePushDevice() {
  const queryClient = useQueryClient()
  const [state, setState] = useState<PushState>(() => ({
    support: currentSupport(),
    subscribed: false,
    serverDisabled: false,
    busy: false,
    dismissed: false,
    error: null,
  }))

  // Read the existing subscription on mount so a returning member sees "zapnuto"
  // rather than being asked again. Re-registering it with the server is NOT done
  // here — usePushKeepalive does that app-wide, so the repair does not depend on
  // anyone opening this screen.
  useEffect(() => {
    let cancelled = false
    if (!pushSupported()) return
    void (async () => {
      // getRegistration() (via currentSubscription) rather than `ready`: this runs
      // on mount, and a panel that renders only once a service worker exists would
      // render never on a browser that refuses one.
      const sub = await currentSubscription()
      if (!cancelled) setState((s) => ({ ...s, subscribed: !!sub, support: currentSupport() }))
    })()
    return () => {
      cancelled = true
    }
  }, [])

  const subscribe = useCallback(async () => {
    setState((s) => ({ ...s, busy: true, error: null, dismissed: false }))
    try {
      // Ask the server FIRST: with no VAPID keypair there is nothing to subscribe
      // to, and burning the browser prompt to then fail would be unrecoverable.
      let key: string
      try {
        key = (await getVapidKey()).key
      } catch (err) {
        if (err instanceof ApiError && err.status === 503) {
          setState((s) => ({ ...s, busy: false, serverDisabled: true }))
          return false
        }
        throw err
      }

      // Then the service worker, and STILL before the prompt. There is no push
      // subscription without a worker, and the browser prompt is one-shot: asking
      // first and only then discovering we have nowhere to deliver would spend a
      // permission the app can never ask for again.
      const reg = await serviceWorkerReady()
      if (!reg) {
        setState((s) => ({ ...s, busy: false, error: cs.settings.swUnavailable }))
        return false
      }

      const permission = await Notification.requestPermission()
      if (permission !== 'granted') {
        setState((s) => ({
          ...s,
          busy: false,
          support: permission === 'denied' ? 'blocked' : 'prompt',
          dismissed: permission === 'default',
        }))
        return false
      }

      const sub =
        (await reg.pushManager.getSubscription()) ??
        (await reg.pushManager.subscribe({
          userVisibleOnly: true,
          applicationServerKey: urlBase64ToUint8Array(key),
        }))

      await registerPushSubscription({
        endpoint: sub.endpoint,
        keys: {
          p256dh: bufferToBase64Url(sub.getKey('p256dh')),
          auth: bufferToBase64Url(sub.getKey('auth')),
        },
        user_agent: navigator.userAgent,
      })

      setState((s) => ({ ...s, busy: false, subscribed: true, support: 'granted' }))
      void queryClient.invalidateQueries({ queryKey: qk.pushPrefs })
      return true
    } catch (err) {
      setState((s) => ({
        ...s,
        busy: false,
        error: err instanceof Error ? err.message : 'Nepodařilo se zapnout oznámení.',
      }))
      return false
    }
  }, [queryClient])

  const unsubscribe = useCallback(async () => {
    setState((s) => ({ ...s, busy: true, error: null }))
    try {
      const sub = await currentSubscription()
      if (sub) {
        await removePushSubscription(sub.endpoint)
        await sub.unsubscribe()
      }
      setState((s) => ({ ...s, busy: false, subscribed: false }))
      return true
    } catch (err) {
      setState((s) => ({
        ...s,
        busy: false,
        error: err instanceof Error ? err.message : 'Nepodařilo se vypnout oznámení.',
      }))
      return false
    }
  }, [])

  return { state, subscribe, unsubscribe }
}

/** usePushPreferences reads and patches the master + category mutes (D53a). */
export function usePushPreferences() {
  const queryClient = useQueryClient()
  const query = useQuery({ queryKey: qk.pushPrefs, queryFn: getPushPreferences })

  const mutation = useMutation({
    mutationFn: (patch: { enabled?: boolean; categories?: Partial<PushCategories> }) =>
      updatePushPreferences(patch),
    // Optimistic: a toggle must feel instant, and the server is the authority on
    // the result it returns.
    onSuccess: (prefs: PushPreferences) => queryClient.setQueryData(qk.pushPrefs, prefs),
  })

  return { query, update: mutation }
}
