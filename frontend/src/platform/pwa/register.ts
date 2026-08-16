// Service-worker registration (PRD D72).
//
// Registration is explicit rather than plugin-injected so the one SW the app has
// is visibly owned by this file. Updates are SILENT: a new build activates on the
// next load with no prompt and no toast. With no offline write queue there is
// nothing in flight that a version change could strand, so an "aktualizovat?"
// dialog would be a question with only one sensible answer.

export function registerServiceWorker(): void {
  if (!('serviceWorker' in navigator)) return
  // Dev runs without a service worker: a stale precache during HMR is a much
  // worse debugging experience than simply not having offline support locally.
  if (import.meta.env.DEV) return

  window.addEventListener('load', () => {
    void navigator.serviceWorker
      .register('/sw.js', { scope: '/' })
      .then((reg) => {
        // Take over as soon as a new build is installed, without asking.
        reg.addEventListener('updatefound', () => {
          const incoming = reg.installing
          if (!incoming) return
          incoming.addEventListener('statechange', () => {
            if (incoming.state === 'installed' && navigator.serviceWorker.controller) {
              incoming.postMessage({ type: 'SKIP_WAITING' })
            }
          })
        })
      })
      .catch(() => {
        // No service worker means no offline reads and no push — the settings
        // panel already explains the second, and the app itself still works.
      })
  })
}
