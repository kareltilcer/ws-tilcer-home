// App-wide offline state (PRD D71/D73, HANDOFF-design §v5 §10).
//
// Offline in v5 is READ-ONLY and it must feel deliberate, not broken: one calm
// neutral indicator (never error-red — being offline is a state, not a failure),
// every write control DISABLED rather than hidden, and honest "needs a
// connection" states for the few things that genuinely cannot work.
//
// There is deliberately no queue, no sync status and no conflict UI. Writes fail
// closed, which is exactly what D71 asks for.

import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from 'react'

interface OnlineContextValue {
  online: boolean
  /** The install prompt, when the browser has offered one. */
  canInstall: boolean
  install: () => Promise<void>
}

const OnlineContext = createContext<OnlineContextValue>({
  online: true,
  canInstall: false,
  install: async () => {},
})

/** BeforeInstallPromptEvent is Chromium-only and not in lib.dom. */
interface BeforeInstallPromptEvent extends Event {
  prompt: () => Promise<void>
  userChoice: Promise<{ outcome: 'accepted' | 'dismissed' }>
}

export function OnlineProvider({ children }: { children: ReactNode }) {
  const [online, setOnline] = useState(() => (typeof navigator === 'undefined' ? true : navigator.onLine))
  const [installEvent, setInstallEvent] = useState<BeforeInstallPromptEvent | null>(null)

  useEffect(() => {
    const goOnline = () => setOnline(true)
    const goOffline = () => setOnline(false)
    window.addEventListener('online', goOnline)
    window.addEventListener('offline', goOffline)

    // Chromium fires this when the app qualifies for installation; other browsers
    // never do, which is why the affordance HIDES rather than showing a dead
    // button (Safari/iOS installs through the share sheet instead).
    const onBeforeInstall = (e: Event) => {
      e.preventDefault()
      setInstallEvent(e as BeforeInstallPromptEvent)
    }
    window.addEventListener('beforeinstallprompt', onBeforeInstall)
    const onInstalled = () => setInstallEvent(null)
    window.addEventListener('appinstalled', onInstalled)

    return () => {
      window.removeEventListener('online', goOnline)
      window.removeEventListener('offline', goOffline)
      window.removeEventListener('beforeinstallprompt', onBeforeInstall)
      window.removeEventListener('appinstalled', onInstalled)
    }
  }, [])

  const install = useCallback(async () => {
    if (!installEvent) return
    await installEvent.prompt()
    await installEvent.userChoice
    setInstallEvent(null)
  }, [installEvent])

  return (
    <OnlineContext.Provider value={{ online, canInstall: !!installEvent, install }}>
      {children}
    </OnlineContext.Provider>
  )
}

/** useOnline reports connectivity. Write controls disable when it is false. */
export function useOnline(): boolean {
  return useContext(OnlineContext).online
}

/** useInstallPrompt exposes the platform install flow where it exists. */
export function useInstallPrompt() {
  const { canInstall, install } = useContext(OnlineContext)
  return { canInstall, install }
}

/** offlineWriteProps returns the disabled state + the ONE standard message every
 *  write control shows offline, so the app never invents a second wording. */
export function offlineWriteProps(online: boolean): { disabled: boolean; title?: string } {
  if (online) return { disabled: false }
  return { disabled: true, title: 'Změny nelze uložit offline' }
}
