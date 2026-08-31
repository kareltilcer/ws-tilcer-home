// How a push envelope becomes a notification (PRD D63/D67, D248).
//
// This is split out of sw.ts for one reason: sw.ts cannot be imported by a test.
// It registers global listeners and calls precacheAndRoute(self.__WB_MANIFEST) at
// module scope, so merely importing it in vitest runs the service worker. The
// mapping below is the part with rules worth pinning down, so it lives where a
// test can reach it and sw.ts keeps only the event plumbing.

/** Envelope is the payload platform/push sends (push.Envelope's wire half). */
export interface Envelope {
  module?: string
  type?: string
  title?: string
  body?: string
  url?: string
  tag?: string
  renotify?: boolean
  data?: Record<string, unknown>
}

/** lib.webworker's NotificationOptions omits `renotify` — TypeScript models the
 *  subset of the Notifications API that every target implements, and renotify is
 *  Android/Chrome's half. Declared here rather than cast away at the call site,
 *  so the field stays type-checked. */
export interface AlertingNotificationOptions extends NotificationOptions {
  renotify?: boolean
}

/** notificationFor maps one envelope onto showNotification's two arguments. */
export function notificationFor(envelope: Envelope): {
  title: string
  options: AlertingNotificationOptions
} {
  const tag = envelope.tag
  return {
    title: envelope.title || 'Home',
    options: {
      body: envelope.body ?? '',
      icon: '/icons/icon-192.png',
      badge: '/icons/badge-72.png',
      // The collapse tag lets a newer push of the same kind replace an older one
      // instead of stacking (a morning summary should not pile up).
      tag,
      // ⚠ AND renotify IS WHAT KEEPS THAT REPLACEMENT FROM BEING SILENT. A
      // notification that replaces a same-tag predecessor does not alert again
      // unless this is true: no sound, no vibration, no banner. Chat sent twenty
      // messages into one room and only the first was ever announced.
      //
      // The `&& Boolean(tag)` is not defensive padding: showNotification THROWS a
      // TypeError on renotify with an empty tag, and a push handler that throws
      // shows nothing at all. The server clears the same combination, and this is
      // the half that holds for an envelope written by an older build — the same
      // split as inAppTarget's origin check.
      renotify: envelope.renotify === true && Boolean(tag),
      data: {
        url: envelope.url ?? '/',
        module: envelope.module,
        type: envelope.type,
        ...envelope.data,
      },
    },
  }
}
