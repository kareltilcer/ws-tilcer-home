import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

/** Listener removers for the module instances a test loaded, run in afterEach. */
const teardown: Array<() => void> = []

// The reporter reads its configuration and keeps its counters at MODULE scope —
// it is installed once per page load and never reset — so every test imports a
// fresh copy after stubbing the build args it would have been given.
//
// jsdom's window OUTLIVES vi.resetModules(), so the listeners each fresh copy
// installs are recorded here and removed after the test. Without that, the
// second test is reported by two modules at once and every count below is wrong
// by however many tests ran first — which is exactly how the per-load cap
// appeared to be broken when it was not.
async function load(env: Record<string, string> = {}) {
  vi.resetModules()
  for (const [key, value] of Object.entries(env)) vi.stubEnv(key, value)

  const added: Array<[string, EventListenerOrEventListenerObject]> = []
  // The real method is bound BEFORE the spy replaces it — both to avoid
  // recursing and because jsdom's Window fails EventTarget.prototype's brand
  // check, so borrowing the prototype's method is not an option. The cast is to
  // a plain (type: string, …): the app pulls the webworker lib in through
  // src/sw.ts, which leaves `window.addEventListener` an overload set typed
  // against two different global event maps.
  const realAdd = window.addEventListener.bind(window) as (
    type: string,
    listener: EventListenerOrEventListenerObject,
    options?: boolean | AddEventListenerOptions,
  ) => void
  const spy = vi.spyOn(window, 'addEventListener').mockImplementation((type, listener, options) => {
    added.push([type, listener as EventListenerOrEventListenerObject])
    realAdd(type, listener as EventListenerOrEventListenerObject, options)
  })

  const mod = await import('@/platform/status/report')
  mod.initCrashReporting()

  spy.mockRestore()
  teardown.push(() => added.forEach(([type, fn]) => window.removeEventListener(type, fn)))
  return mod
}

const CONFIGURED = {
  VITE_STATUS_INGEST_URL: 'https://status.tilcer.cz/api/ingest/home',
  VITE_STATUS_INGEST_KEY: 'ik_public_browser_key',
  VITE_STATUS_ENVIRONMENT: 'prod',
  VITE_STATUS_RELEASE: 'home@2026.36.1',
}

let fetchMock: ReturnType<typeof vi.fn>

/** A synthetic ErrorEvent is still an error event: left uncancelled, jsdom
 *  reports it as an uncaught exception and fails the run. A real page's uncaught
 *  errors are cancelable for exactly this reason, so the test cancels its own. */
const swallow = (e: Event) => e.preventDefault()

beforeEach(() => {
  fetchMock = vi.fn().mockResolvedValue(new Response(null, { status: 202 }))
  vi.stubGlobal('fetch', fetchMock)
  window.addEventListener('error', swallow)
})

afterEach(() => {
  window.removeEventListener('error', swallow)
  teardown.splice(0).forEach((fn) => fn())
  vi.unstubAllEnvs()
  vi.unstubAllGlobals()
  // jsdom's location outlives a test, so a pushState below would otherwise leak
  // its path into whatever runs next.
  window.history.pushState({}, '', '/')
})

function throwError(error: unknown, init: Partial<ErrorEventInit> = {}) {
  window.dispatchEvent(
    new ErrorEvent('error', { error, message: String(error), cancelable: true, ...init }),
  )
}

function rejectPromise(reason: unknown) {
  window.dispatchEvent(Object.assign(new Event('unhandledrejection'), { reason }))
}

function lastBody(): Record<string, unknown> {
  const [, init] = fetchMock.mock.calls.at(-1) as [string, RequestInit]
  return JSON.parse(init.body as string) as Record<string, unknown>
}

describe('initCrashReporting', () => {
  it('installs nothing without build args — every local dev run', async () => {
    await load()
    throwError(new Error('boom'))
    rejectPromise(new Error('boom'))
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('posts an uncaught error as the documented CrashReport', async () => {
    await load(CONFIGURED)
    throwError(new Error('cannot read properties of undefined'), {
      filename: 'https://home.tilcer.cz/assets/index.js',
      lineno: 42,
      colno: 7,
    })

    expect(fetchMock).toHaveBeenCalledTimes(1)
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe(CONFIGURED.VITE_STATUS_INGEST_URL)
    expect(init.method).toBe('POST')
    // keepalive so a report raised during an unload still leaves the page.
    expect(init.keepalive).toBe(true)
    // Ingest authenticates by KEY. Sending credentials would demand an
    // Access-Control-Allow-Credentials header status never returns, turning
    // every report into a CORS failure with nothing logged anywhere.
    expect(init.credentials).toBeUndefined()
    expect(init.headers).toEqual({
      'Content-Type': 'application/json',
      'X-Ingest-Key': CONFIGURED.VITE_STATUS_INGEST_KEY,
    })

    const body = lastBody()
    expect(body.message).toBe('cannot read properties of undefined')
    expect(body.level).toBe('error')
    expect(body.environment).toBe('prod')
    expect(body.release).toBe('home@2026.36.1')
    expect(body.stack).toEqual(expect.stringContaining('Error'))
    expect(body.context).toMatchObject({
      source: 'https://home.tilcer.cz/assets/index.js',
      line: 42,
      column: 7,
      url: expect.stringContaining('http') as unknown as string,
    })
  })

  // ⚠ home's URLs carry SLUGGED TITLES — /poznamky/soukrome/<a private note's
  // title> — and status is read by Karel's admin session, a different lock from
  // the one on a member's private note. The module is what a crash report needs;
  // the page is not something a crash gets to decide to send.
  it('names the module, never the page — a private note title must not travel', async () => {
    await load(CONFIGURED)
    window.history.pushState({}, '', '/poznamky/soukrome/rozvod-s-manzelkou?edit=1#nadpis')
    throwError(new Error('boom'))

    const url = (lastBody().context as Record<string, unknown>).url as string
    expect(url).toBe(`${location.origin}/poznamky/…`)
    expect(url).not.toContain('rozvod')
    expect(url).not.toContain('soukrome')
    // No query and no hash either: both are page state, and neither is worth a
    // second place a title could hide.
    expect(url).not.toContain('?')
    expect(url).not.toContain('#')

    // A one-segment route keeps its whole path — there is nothing to elide.
    window.history.pushState({}, '', '/nastenka')
    throwError(new Error('boom again'))
    expect((lastBody().context as Record<string, unknown>).url).toBe(`${location.origin}/nastenka`)

    // …and the root is the root, not a bare origin with no slash.
    window.history.pushState({}, '', '/')
    throwError(new Error('boom at the root'))
    expect((lastBody().context as Record<string, unknown>).url).toBe(`${location.origin}/`)
  })

  // ⚠ context.source is the SCRIPT a crash came from, and only ever that. The
  // browser fills ErrorEvent.filename from the topmost JS stack frame and falls
  // back to the DOCUMENT when there is no frame to name — and home's documents
  // are the slugged titles the test above keeps off the board. One rule, both
  // fields, rather than a rule about the field somebody thought of first.
  it('drops a source that is the page rather than a script', async () => {
    await load(CONFIGURED)
    window.history.pushState({}, '', '/poznamky/soukrome/rozvod-s-manzelkou?edit=1')
    throwError(new Error('boom'), { filename: location.href })

    const context = lastBody().context as Record<string, unknown>
    expect(context.source).toBeUndefined()
    expect(JSON.stringify(context)).not.toContain('rozvod')

    // A real script URL is reported whole — the query and hash are stripped for
    // the comparison only.
    throwError(new Error('boom'), { filename: `${location.origin}/assets/index-a1b2.js?v=3` })
    expect((lastBody().context as Record<string, unknown>).source).toBe(
      `${location.origin}/assets/index-a1b2.js?v=3`,
    )
  })

  // ⚠ THE TWO HALVES OF ONE DEPLOYMENT MUST REACH ONE BOARD UNDER ONE NAME. The
  // Go process maps HOME_ENV onto status's prod/dev convention (config.StatusEnv)
  // precisely so that it sends the same word this default does; pinning it here
  // is the other half of that agreement.
  it('tags events with status\'s own environment vocabulary when unconfigured', async () => {
    await load({
      VITE_STATUS_INGEST_URL: CONFIGURED.VITE_STATUS_INGEST_URL,
      VITE_STATUS_INGEST_KEY: CONFIGURED.VITE_STATUS_INGEST_KEY,
    })
    throwError(new Error('boom'))
    // `dev` under vitest, `prod` in a production build — never Vite's own
    // "development"/"production", and never home's HOME_ENV spelling.
    expect(lastBody().environment).toBe('dev')
  })

  it('reports an unhandled rejection', async () => {
    await load(CONFIGURED)
    rejectPromise(new Error('network request failed'))
    expect(lastBody()).toMatchObject({
      message: 'network request failed',
      context: expect.objectContaining({ kind: 'unhandledrejection' }) as unknown as object,
    })
  })

  // A rejected promise carries any value at all, and window.onerror fires with a
  // bare string for a cross-origin script — so neither path may assume an Error.
  it('describes a thrown value that is not an Error', async () => {
    await load(CONFIGURED)
    rejectPromise('plain string reason')
    expect(lastBody().message).toBe('plain string reason')

    rejectPromise({ code: 418, why: 'teapot' })
    // Not "[object Object]", which would file every such rejection under one
    // useless group title.
    expect(lastBody().message).toBe('{"code":418,"why":"teapot"}')
  })

  it('stops after the per-window cap so a render loop cannot flood the board', async () => {
    await load(CONFIGURED)
    for (let i = 0; i < 60; i++) throwError(new Error(`boom ${i}`))
    expect(fetchMock).toHaveBeenCalledTimes(20)
  })

  // …and the allowance comes back. home is an installed PWA whose tab is not
  // reloaded for days, so a counter that only went up would make a device
  // permanently deaf after twenty errors spread across a week, with nothing
  // anywhere saying it had stopped listening.
  it('reopens the allowance an hour later', async () => {
    await load(CONFIGURED)
    for (let i = 0; i < 60; i++) throwError(new Error(`boom ${i}`))
    expect(fetchMock).toHaveBeenCalledTimes(20)

    vi.useFakeTimers()
    vi.setSystemTime(Date.now() + 61 * 60_000)
    throwError(new Error('an hour later'))
    vi.useRealTimers()

    expect(fetchMock).toHaveBeenCalledTimes(21)
    expect(lastBody().message).toBe('an hour later')
  })

  // ⚠ This exercises the parsing, not production: `Retry-After` is not a
  // CORS-safelisted response header and status exposes none, so a real
  // cross-origin read returns null and the 60 s default is what actually runs.
  it('goes quiet for Retry-After on a 429 and drops rather than queueing', async () => {
    fetchMock.mockResolvedValue(
      new Response(null, { status: 429, headers: { 'Retry-After': '120' } }),
    )
    await load(CONFIGURED)

    throwError(new Error('first'))
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1))

    throwError(new Error('second'))
    expect(fetchMock).toHaveBeenCalledTimes(1)

    // …and comes back once the window has passed. Nothing was held over: the
    // event raised while muted is gone, not queued.
    vi.useFakeTimers()
    vi.setSystemTime(Date.now() + 121_000)
    fetchMock.mockResolvedValue(new Response(null, { status: 202 }))
    throwError(new Error('third'))
    vi.useRealTimers()
    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(lastBody().message).toBe('third')
  })

  it('never throws into the page when the request fails', async () => {
    fetchMock.mockRejectedValue(new TypeError('Failed to fetch'))
    await load(CONFIGURED)
    expect(() => throwError(new Error('boom'))).not.toThrow()
    // Unhandled rejections out of the reporter would be reported BY the reporter.
    await expect(vi.waitFor(() => expect(fetchMock).toHaveBeenCalled())).resolves.not.toThrow()
  })

  it('caps the message and the stack well inside the server body limit', async () => {
    await load(CONFIGURED)
    const huge = new Error('x'.repeat(5000))
    huge.stack = 's'.repeat(20000)
    throwError(huge)

    const body = lastBody()
    expect((body.message as string).length).toBeLessThanOrEqual(2000 + '…[truncated]'.length)
    expect((body.stack as string).length).toBeLessThanOrEqual(8000 + '…[truncated]'.length)
  })
})
