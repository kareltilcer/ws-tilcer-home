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
//
// ⚠ AND IT NEVER SENDS `location.href`, from EITHER of the two fields that could
// carry it: see pageRef and scriptRef below. home's URLs carry SLUGGED TITLES,
// private ones included, and a crash report is not a place a member's note title
// may travel to.

import { crashConfig, type CrashConfig } from '@/platform/status/config'

/** The reports one tab may send per hour. status allows 60/min per site with a
 *  burst of 120; a browser is one of several reporters against that budget and a
 *  render loop can produce thousands of identical errors in a second, so the tab
 *  keeps a much tighter cap of its own.
 *
 *  ⚠ IT IS A WINDOW THAT REOPENS, NOT A PER-LOAD COUNTER, because home is an
 *  installed PWA whose tab is not reloaded for days. A counter that only went up
 *  would make a device permanently deaf after twenty errors accumulated across a
 *  week — with nothing anywhere saying it had stopped — while doing nothing extra
 *  about the case the cap exists for: a render loop spends the whole allowance in
 *  one second and then waits out the hour either way. (It is a tumbling window,
 *  not a sliding one: the hour starts at the first report of a quiet period and
 *  the whole allowance comes back at once. Cheaper by twenty timestamps, and
 *  indistinguishable for the burst it is here to stop.) */
const MAX_REPORTS_PER_WINDOW = 20
const REPORT_WINDOW_MS = 60 * 60 * 1000

/** Message caps, well inside the server's 64 KB body limit — which it enforces
 *  with a 413 that this client, being silent, would never surface. */
const MAX_MESSAGE_CHARS = 2000
const MAX_STACK_CHARS = 8000

/** The 429 back-off, in the same two numbers the Go client carries.
 *
 *  DEFAULT_MUTE_MS is what runs today, because `Retry-After` is not readable
 *  cross-origin (see the 429 branch below). MAX_MUTE_MS is the clamp, and it is
 *  here for the same reason it is in `statusreport.maxMute`: a header this client
 *  cannot verify must not be able to silence it for a day. It matters MORE in the
 *  browser than on the server — home is an installed PWA whose tab is not
 *  reloaded for days, so an unclamped back-off is the permanently-deaf device the
 *  per-window cap above is written to avoid, arriving through the other door. */
const DEFAULT_MUTE_MS = 60 * 1000
const MAX_MUTE_MS = 5 * 60 * 1000

interface ReportOptions {
  stack?: string
  context?: Record<string, unknown>
}

let config: CrashConfig | null = null
/** Reports sent in the window that opened at windowStart. */
let sent = 0
let windowStart = 0
/** Epoch ms before which nothing is sent, set when a 429 comes back. */
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
      context: { source: scriptRef(e.filename), line: e.lineno, column: e.colno },
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
    const now = Date.now()
    if (now - windowStart >= REPORT_WINDOW_MS) {
      windowStart = now
      sent = 0
    }
    if (sent >= MAX_REPORTS_PER_WINDOW) return
    if (now < mutedUntil) return

    const { message, stack } = describe(error)
    if (!message) return
    sent += 1

    void post(config, {
      message: cut(message, MAX_MESSAGE_CHARS),
      level: 'error',
      stack: cut(options.stack ?? stack, MAX_STACK_CHARS) || undefined,
      environment: config.environment || undefined,
      release: config.release || undefined,
      context: { ...options.context, url: pageRef(), viewport: `${innerWidth}x${innerHeight}` },
    })
  } catch {
    // The reporter must never be the thing that throws.
  }
}

/** pageRef is the page a report names, and it is deliberately NOT
 *  `location.href`.
 *
 *  ⚠ HOME'S URLs CARRY SLUGGED TITLES. `/poznamky/soukrome/rozvod` is one
 *  member's PRIVATE note, slugged from its title by the backend; `/dokumenty/…`
 *  is the same for a filename. home's privacy model makes a private note
 *  unreadable by anyone, admins included — and status is read by Karel's ADMIN
 *  session, which is a different lock. Sending the full URL would be exactly the
 *  side door this feature keeps "Send console output" switched off to avoid, and
 *  it would open on every uncaught error rather than on an opt-in.
 *
 *  So: the origin and the FIRST path segment only — the module, which is what a
 *  crash report needs — with an ellipsis when anything was dropped, and no query
 *  or hash. First segment only rather than a list of the modules that slug their
 *  paths, because a module added later would not be on that list. What is lost is
 *  which sub-page of Zahrada or Elektřina it was; the stack trace names the
 *  component, which is the better answer to that question anyway.
 *
 *  The feedback WIDGET does send the whole URL (widget.md §3) and home cannot
 *  change that — but it shows the reporter everything before they press send, so
 *  it is a member's own decision about their own page. A crash report is not. */
function pageRef(): string {
  const segments = location.pathname.split('/').filter(Boolean)
  if (segments.length === 0) return `${location.origin}/`
  const head = `${location.origin}/${segments[0]}`
  return segments.length > 1 ? `${head}/…` : head
}

/** scriptRef is the SOURCE FILE a crash came from — and it exists because
 *  `ErrorEvent.filename` is not always one. The browser fills it from the topmost
 *  JavaScript stack frame; with no frame to attribute the error to, it falls back
 *  to the DOCUMENT, and home's documents are the slugged titles pageRef() above
 *  exists to keep off the board. One rule, applied to both places a URL can leave
 *  this file, rather than a rule about the field somebody happened to think of.
 *
 *  home ships every line of itself in `/assets` modules and index.html carries no
 *  inline script, so the fallback should never fire here — this is the guard, not
 *  a fix for something observed. Dropping the field when it does costs nothing:
 *  it would be naming the page a second time, and `context.url` already names the
 *  module. The query and hash are stripped only for the COMPARISON; a real script
 *  URL is reported whole. */
function scriptRef(filename: string | undefined): string | undefined {
  if (!filename) return undefined
  const bare = filename.split(/[?#]/)[0]
  return bare === location.origin + location.pathname ? undefined : filename
}

/** describe pulls a message and a stack out of whatever was thrown — which, in a
 *  browser, is not always an Error: a rejected promise carries any value at all,
 *  and `window.onerror` fires with a bare string for a cross-origin script. */
function describe(error: unknown): { message: string; stack: string } {
  if (error instanceof Error) {
    return { message: error.message || error.name || 'Error', stack: error.stack ?? '' }
  }
  if (typeof error === 'string') return { message: error, stack: '' }
  // ⚠ A REJECTION CAN CARRY NOTHING AT ALL — `Promise.reject()`, or an abort that
  // rejects with undefined — and the JSON path below renders that as the literal
  // string "undefined" (JSON.stringify(undefined) is undefined, so the ?? falls
  // through to String()). "undefined" is TRUTHY, so report()'s empty-message drop
  // never sees it, and the board grows a permanent group titled `undefined`
  // collecting every reasonless rejection in the app under a name that points at
  // nothing. There is no stack to add either, so the title is all such an event
  // will ever have: it may as well say what happened.
  if (error === undefined || error === null) {
    return { message: `empty error value: ${String(error)}`, stack: '' }
  }
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
      // never build an unbounded backlog. Holding the event would mean holding it
      // across a navigation that may never come back, so this tab simply goes
      // quiet.
      //
      // ⚠ IN A BROWSER THE 60 s FALLBACK IS THE NORMAL PATH, not the exception.
      // `Retry-After` is not a CORS-safelisted response header, and status's
      // documented CORS surface (integration.md, "Cross-origin reporting") lists
      // no Access-Control-Expose-Headers — so `headers.get` returns null here even
      // though the server sent the header. The read stays because it costs nothing
      // and becomes correct the day status exposes it; the default is what
      // actually runs.
      const retryAfter = Number(res.headers.get('Retry-After'))
      const asked =
        Number.isFinite(retryAfter) && retryAfter > 0 ? retryAfter * 1000 : DEFAULT_MUTE_MS
      mutedUntil = Date.now() + Math.min(asked, MAX_MUTE_MS)
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
