/// <reference lib="webworker" />
//
// The ONE service worker (PRD D63/D67/D71/D72, HANDOFF-7 §12).
//
// It has two jobs that share nothing but this file:
//
//   1. PUSH — show a notification from the envelope, and route a click into the
//      right in-app screen. This is what makes `platform/push` visible at all.
//   2. PWA — precache the app shell so a cold offline load renders, and let a
//      new build activate silently.
//
// What it deliberately does NOT do is cache /api responses. Offline READS come
// from the persisted TanStack Query cache instead, which is namespaced per user
// and cleared on logout — an SW response cache is precisely the thing that would
// survive a logout and show one member another member's data.

import { precacheAndRoute, cleanupOutdatedCaches, createHandlerBoundToURL } from 'workbox-precaching'
import { NavigationRoute, registerRoute } from 'workbox-routing'
import { notificationFor, type Envelope } from './platform/push/notification'

declare const self: ServiceWorkerGlobalScope

// ---- PWA: precache + navigation fallback ----

precacheAndRoute(self.__WB_MANIFEST)
cleanupOutdatedCaches()

// Any navigation falls back to the cached shell so a deep link (/ukoly, /d/{id})
// opens offline; the SPA router then renders from the persisted query cache.
// /api and /ws are excluded — they must fail honestly rather than return a shell.
registerRoute(
  new NavigationRoute(createHandlerBoundToURL('/index.html'), {
    denylist: [/^\/api\//, /^\/ws$/],
  }),
)

// Silent auto-update (D72): a new build takes over on the next load with no
// prompt. There is no offline write queue, so there is no half-finished work a
// version change could strand — which is exactly why no "nová verze" toast is
// designed, and why building one would be adding a question nobody needs asked.
self.addEventListener('install', () => {
  void self.skipWaiting()
})
self.addEventListener('activate', (event) => {
  event.waitUntil(self.clients.claim())
})

// The app tells the SW to take over immediately after a fresh registration.
self.addEventListener('message', (event: ExtendableMessageEvent) => {
  if ((event.data as { type?: string } | undefined)?.type === 'SKIP_WAITING') {
    void self.skipWaiting()
  }
})

// ---- Push ----

self.addEventListener('push', (event: PushEvent) => {
  let envelope: Envelope = {}
  try {
    envelope = (event.data?.json() as Envelope) ?? {}
  } catch {
    // A payload that is not our JSON still deserves to be shown: a silent push
    // is worse than a generic one, and some browsers require userVisibleOnly.
    envelope = { title: 'Home', body: event.data?.text() ?? '' }
  }

  const { title, options } = notificationFor(envelope)
  event.waitUntil(self.registration.showNotification(title, options))
})

/** inAppTarget resolves a notification's click target, and refuses to leave the
 *  app. `new URL(value, base)` IGNORES the base whenever the value carries its
 *  own scheme, so an envelope url of "https://elsewhere.example" would navigate
 *  a household member off-origin. The server validates this field too; this is
 *  the half that holds even for an envelope written by an older build. */
function inAppTarget(url: string | undefined): URL {
  const target = new URL(url || '/', self.location.origin)
  if (target.origin !== self.location.origin) return new URL('/', self.location.origin)
  return target
}

self.addEventListener('notificationclick', (event: NotificationEvent) => {
  event.notification.close()
  const data = (event.notification.data ?? {}) as { url?: string }
  const target = inAppTarget(data.url)

  event.waitUntil(
    (async () => {
      const clients = await self.clients.matchAll({ type: 'window', includeUncontrolled: true })
      // Focus an existing tab where possible — opening a second copy of the app
      // is the most annoying thing a notification can do.
      for (const client of clients) {
        if ('focus' in client) {
          await client.focus()
          if ('navigate' in client) {
            try {
              await client.navigate(target.href)
            } catch {
              /* navigation can be refused; the focused tab is still the right result */
            }
          }
          return
        }
      }
      await self.clients.openWindow(target.href)
    })(),
  )
})

// Every mutation on /api is guarded by the double-submit CSRF check: an
// `X-CSRF-Token` header that must equal the (JS-readable) `csrf` cookie. A
// service worker has no `document.cookie`, so the token has to come from the
// asynchronous CookieStore API — without it these requests are answered with a
// 403 and the re-subscribe below can never succeed.
//
// CookieStore is not universal, so this returns '' where it is missing and the
// caller degrades to the page-side repair path (usePush re-registers this
// device's subscription on every load).
interface CookieStoreLike {
  get(name: string): Promise<{ value: string } | null | undefined>
}

async function csrfToken(): Promise<string> {
  const store = (self as unknown as { cookieStore?: CookieStoreLike }).cookieStore
  if (!store) return ''
  try {
    return (await store.get('csrf'))?.value ?? ''
  } catch {
    return ''
  }
}

/** apiWrite issues an authenticated mutation, or resolves false when this worker
 *  cannot obtain a CSRF token (in which case the page repairs it later). */
async function apiWrite(method: 'POST' | 'DELETE', body: unknown): Promise<boolean> {
  const token = await csrfToken()
  if (!token) return false
  const res = await fetch('/api/push/subscriptions', {
    method,
    credentials: 'include',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': token },
    body: JSON.stringify(body),
  })
  return res.ok
}

// A browser may rotate an endpoint at any time. Re-registering keeps the device
// subscribed without the member ever seeing it happen; failing silently would
// leave a device that believes it is subscribed but receives nothing.
// `pushsubscriptionchange` is not in lib.webworker's event map, so the handler
// takes the base Event and narrows it here.
self.addEventListener('pushsubscriptionchange', ((event: Event) => {
  const e = event as ExtendableEvent & {
    oldSubscription?: PushSubscription
    newSubscription?: PushSubscription
  }
  e.waitUntil(
    (async () => {
      try {
        if (e.oldSubscription?.endpoint) {
          await apiWrite('DELETE', { endpoint: e.oldSubscription.endpoint })
        }
        const fresh = e.newSubscription ?? (await self.registration.pushManager.getSubscription())
        if (!fresh) return
        const json = fresh.toJSON() as { endpoint?: string; keys?: { p256dh?: string; auth?: string } }
        if (!json.endpoint || !json.keys?.p256dh || !json.keys.auth) return
        await apiWrite('POST', {
          endpoint: json.endpoint,
          keys: { p256dh: json.keys.p256dh, auth: json.keys.auth },
        })
      } catch {
        /* usePush re-registers this device's endpoint on the next visit */
      }
    })(),
  )
}) as EventListener)

export {}
