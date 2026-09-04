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
    // ⚠ NO `defer`, BECAUSE IT WOULD BE A LIE. `defer` only means anything on a
    // parser-inserted script; one built with createElement is force-async by
    // spec, so setting the property changes nothing and only tells the next
    // reader there is an ordering guarantee here. widget.md §1 recommends `defer`
    // for the pasted <script> embed — this is the injected path, which is already
    // non-blocking, and the load event below is what the trigger waits on.
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
 *
 * ⚠ `reporter` IS READ ONCE PER PAGE LOAD, whatever the dependency array below
 * suggests. The label reaches the widget as an attribute on its script element,
 * which the bundle parses when it executes; there is no API for changing it
 * afterwards, and re-injecting the script would put a second widget on the page.
 * So the effect re-running on a new label is a no-op, and the one case that
 * leaves is a second member signing in without a reload — their reports carry the
 * first member's label. It is a display label status never verifies, the sign-out
 * path a household of three actually takes is closing the tab, and the two
 * alternatives (a second widget, or dropping the dependency and lying to the
 * linter instead of to the reader) are both worse than saying so here.
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
