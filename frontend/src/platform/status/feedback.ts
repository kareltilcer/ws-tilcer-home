// The feedback widget: the button that lets the people USING home write to
// Karel, with a screenshot or a clip attached. It is a different key and a
// different endpoint from crash reporting — a spammed widget can be revoked
// without silencing the crash board, which is why status issues two.
//
// ⚠ THE WIDGET'S OWN FLOATING LAUNCHER IS SUPPRESSED (`data-launcher="none"`),
// and home's shell supplies the trigger instead. The launcher sits 16 px from a
// corner and does not try to guess the height of a host app's bottom
// navigation — and home has one: a 56 px thumb-tab bar across the whole width at
// every width below 768. Moving it to a top corner only trades that collision
// for the mobile header's theme and sign-out buttons. `StatusFeedback.open()` is
// unaffected by the suppression and is the documented public API.
//
// ⚠ WHAT THAT COSTS, since it is the one thing this arrangement gives up. The
// widget renders no launcher when feedback is switched OFF for the site, so its
// own button can never be dead; ours is rendered on the strength of the script
// having LOADED, which is a weaker claim. It covers the failures that actually
// happen — no key baked in, a blocked script, an offline load — and leaves one:
// feedback disabled in the dashboard after this build shipped with a key. Then
// the trigger opens nothing. Rotating the key is the fix, and it is the same
// action that would have to reach this build anyway.
//
// ⚠ CONSOLE CAPTURE MUST STAY OFF for this site (it is off by default). home's
// privacy model makes a member's private notes unreadable by anyone, admins
// included; a console line carrying a note title into status would be read by
// Karel's admin session — a side door with a different lock. That is a dashboard
// setting, not a build arg, so nothing in this file can enforce it.

import { useEffect, useState } from 'react'
import { widgetConfig } from '@/platform/status/config'

declare global {
  interface Window {
    /** Defined by the widget bundle from the moment it executes — before its
     *  configuration round-trip finishes — so a trigger wired to it never has to
     *  care about timing. */
    StatusFeedback?: { open: () => void }
  }
}

/** The in-flight (or settled) load, so React StrictMode's double effect and two
 *  mounts of the shell all share one script element. */
let loading: Promise<boolean> | null = null

function ensureWidget(reporter: string): Promise<boolean> {
  if (loading) return loading
  const cfg = widgetConfig()
  if (!cfg) {
    loading = Promise.resolve(false)
    return loading
  }
  loading = new Promise<boolean>((resolve) => {
    const script = document.createElement('script')
    script.src = cfg.src
    script.defer = true
    script.dataset.site = cfg.site
    script.dataset.key = cfg.key
    script.dataset.lang = 'cs'
    script.dataset.launcher = 'none'
    // A display LABEL for whoever is signed in, not an identity — status never
    // verifies it. It saves Karel guessing which of three people wrote in.
    if (reporter) script.dataset.reporter = reporter
    if (cfg.release) script.dataset.release = cfg.release
    script.addEventListener('load', () => resolve(true))
    script.addEventListener('error', () => resolve(false))
    document.head.appendChild(script)
  })
  return loading
}

/**
 * useFeedbackWidget loads the widget once and reports whether the trigger should
 * be rendered. False means: this build has no widget key, or the script did not
 * load — and in both cases the shell shows nothing rather than a control that
 * fails when pressed.
 */
export function useFeedbackWidget(reporter: string): boolean {
  const [ready, setReady] = useState(false)
  useEffect(() => {
    let live = true
    void ensureWidget(reporter).then((ok) => {
      if (live) setReady(ok)
    })
    return () => {
      live = false
    }
  }, [reporter])
  return ready
}

/** openFeedback opens the report dialog. Guarded twice over — the global is
 *  absent when the script was blocked, and the widget itself never throws into
 *  the host app, so neither does this. */
export function openFeedback(): void {
  try {
    window.StatusFeedback?.open()
  } catch {
    // Nothing to do and nowhere to say it: a failure here must not become the
    // host app's exception.
  }
}
