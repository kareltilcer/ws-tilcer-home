// Browser crash reporting to status.tilcer.cz.
//
// It hooks `error` and `unhandledrejection` on window and posts each one to the
// site's ingest endpoint. React 19 hands an uncaught render error to
// `window.reportError`, which dispatches an ErrorEvent — so the white screen a
// broken component leaves behind arrives here too, without this app growing an
// error boundary it has deliberately never had.
//
// ⚠ EVERY PATH FAILS SAFE, exactly as the Go client does. Nothing here throws
// into the host page, an ingest failure is swallowed, and a page with no
// configuration installs no listeners at all. A reporter that can break the app
// it watches is worse than no reporter.
//
// The request is deliberately NOT credentialed. Ingest authenticates by key, and
// `credentials: "include"` would make the browser demand an
// Access-Control-Allow-Credentials header status never sends — turning every
// report into a CORS failure with nothing logged anywhere.

import { crashConfig, type CrashConfig } from '@/platform/status/config'

/** The reports one page load may send. status allows 60/min per site with a
 *  burst of 120; a browser is one of several reporters against that budget and a
 *  render loop can produce thousands of identical errors in a second, so the tab
 *  keeps a much tighter cap of its own and simply stops. */
const MAX_REPORTS_PER_LOAD = 20

/** Message caps, well inside the server's 64 KB body limit — which it enforces
 *  with a 413 that this client, being silent, would never surface. */
const MAX_MESSAGE_CHARS = 2000
const MAX_STACK_CHARS = 8000

interface ReportOptions {
  stack?: string
  context?: Record<string, unknown>
}

let config: CrashConfig | null = null
let sent = 0
/** Epoch ms before which nothing is sent, set from a 429's Retry-After. */
let mutedUntil = 0

/**
 * initCrashReporting installs the two window listeners. It is called once, from
 * main.tsx, BEFORE React mounts — a crash while the app is still booting is the
 * one nobody can report by hand, and it is also the one the login screen leaves
 * a member staring at.
 *
 * With no VITE_STATUS_INGEST_* build args (every local dev run) it returns
 * having done nothing.
 */
export function initCrashReporting(): void {
  if (config) return // already installed; a second call is a no-op
  config = crashConfig()
  if (!config) return

  window.addEventListener('error', (e: ErrorEvent) => {
    report(e.error ?? e.message, {
      context: { source: e.filename, line: e.lineno, column: e.colno },
    })
  })

  window.addEventListener('unhandledrejection', (e: PromiseRejectionEvent) => {
    report(e.reason, { context: { kind: 'unhandledrejection' } })
  })
}

/** report posts one error. Private on purpose: the two window listeners above
 *  are the whole surface, and a second way in would be a second thing to keep
 *  fail-safe. */
function report(error: unknown, options: ReportOptions = {}): void {
  try {
    if (!config) return
    if (sent >= MAX_REPORTS_PER_LOAD) return
    if (Date.now() < mutedUntil) return

    const { message, stack } = describe(error)
    if (!message) return
    sent += 1

    void post(config, {
      message: cut(message, MAX_MESSAGE_CHARS),
      level: 'error',
      stack: cut(options.stack ?? stack, MAX_STACK_CHARS) || undefined,
      environment: config.environment || undefined,
      release: config.release || undefined,
      context: { ...options.context, url: location.href, viewport: `${innerWidth}x${innerHeight}` },
    })
  } catch {
    // The reporter must never be the thing that throws.
  }
}

/** describe pulls a message and a stack out of whatever was thrown — which, in a
 *  browser, is not always an Error: a rejected promise carries any value at all,
 *  and `window.onerror` fires with a bare string for a cross-origin script. */
function describe(error: unknown): { message: string; stack: string } {
  if (error instanceof Error) {
    return { message: error.message || error.name || 'Error', stack: error.stack ?? '' }
  }
  if (typeof error === 'string') return { message: error, stack: '' }
  // An object with no message would stringify to "[object Object]", which groups
  // every such rejection together under one useless title; JSON says more.
  try {
    return { message: JSON.stringify(error) ?? String(error), stack: '' }
  } catch {
    return { message: String(error), stack: '' }
  }
}

interface CrashPayload {
  message: string
  level: string
  stack?: string
  environment?: string
  release?: string
  context?: Record<string, unknown>
}

async function post(cfg: CrashConfig, payload: CrashPayload): Promise<void> {
  try {
    const res = await fetch(cfg.url, {
      method: 'POST',
      // keepalive so a report raised during a page unload still leaves.
      keepalive: true,
      headers: { 'Content-Type': 'application/json', 'X-Ingest-Key': cfg.key },
      body: JSON.stringify(payload),
    })
    if (res.status === 429) {
      // Back off and DROP. The documented alternative is one retry after the
      // delay, and a queue is explicitly ruled out — a monitoring client must
      // never build an unbounded backlog. Holding the event would mean holding
      // it across a navigation that may never come back, so this tab simply goes
      // quiet for the window it was given.
      const retryAfter = Number(res.headers.get('Retry-After'))
      mutedUntil = Date.now() + (Number.isFinite(retryAfter) && retryAfter > 0 ? retryAfter : 60) * 1000
    }
  } catch {
    // Network failure, a CSP that blocked the connection, an origin missing from
    // STATUS_ALLOWED_ORIGINS — all of them drop. There is nowhere to report a
    // reporting failure to.
  }
}

function cut(value: string | undefined, max: number): string {
  if (!value) return ''
  return value.length <= max ? value : value.slice(0, max) + '…[truncated]'
}
