import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, render } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { useLiveSync, WS_CLOSE_POLICY_VIOLATION } from './ws'
import { setUnauthorizedHandler } from './client'

// The frontend half of the revocation handoff (v10). The backend pins its side
// hard — TestRevokedSocketClosesWithAPolicyCode asserts the close status — and
// this is the half that turns that status into a login screen.
//
// ⚠ Without it the branch fails SILENTLY in every direction: change the
// constant, move the check below the backoff lines, or return early on the wrong
// condition, and a revoked tab goes back to redialing an upgrade that 401s every
// time, once per capped backoff, for as long as the tab stays open, with nothing
// on screen ever saying the session ended.

/** FakeSocket stands in for the browser's WebSocket: it never dials, and it
 *  hands the test the handlers useLiveSync registered so a close can be fired
 *  with a chosen code. */
class FakeSocket {
  static instances: FakeSocket[] = []
  static get last(): FakeSocket {
    const s = FakeSocket.instances[FakeSocket.instances.length - 1]
    if (!s) throw new Error('no socket was opened')
    return s
  }
  onopen: (() => void) | null = null
  onmessage: ((e: MessageEvent) => void) | null = null
  onclose: ((e: CloseEvent) => void) | null = null
  onerror: (() => void) | null = null
  close = vi.fn()
  url: string
  constructor(url: string) {
    this.url = url
    FakeSocket.instances.push(this)
  }
}

/** closeWith fires onclose the way the browser does, carrying the server's code. */
function closeWith(code: number) {
  const socket = FakeSocket.last
  act(() => {
    socket.onclose?.({ code } as CloseEvent)
  })
}

function Probe() {
  useLiveSync()
  return null
}

function renderProbe() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <Probe />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('useLiveSync close handling', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    FakeSocket.instances = []
    vi.stubGlobal('WebSocket', FakeSocket)
  })

  afterEach(() => {
    setUnauthorizedHandler(null)
    vi.unstubAllGlobals()
    vi.useRealTimers()
  })

  // The wire contract with the Go side, which this file cannot import.
  it('uses RFC 6455 1008, the code the hub closes a revoked socket with', () => {
    expect(WS_CLOSE_POLICY_VIOLATION).toBe(1008)
  })

  it('stops reconnecting and hands off to the login screen on a policy close', () => {
    const onUnauthorized = vi.fn()
    setUnauthorizedHandler(onUnauthorized)
    renderProbe()
    expect(FakeSocket.instances).toHaveLength(1)
    act(() => FakeSocket.last.onopen?.())

    closeWith(WS_CLOSE_POLICY_VIOLATION)

    expect(onUnauthorized).toHaveBeenCalledTimes(1)
    // And no redial, ever — not at the first backoff step, not at the cap.
    act(() => void vi.advanceTimersByTime(120_000))
    expect(FakeSocket.instances).toHaveLength(1)
  })

  it('still reconnects on every other close code, without a login handoff', () => {
    const onUnauthorized = vi.fn()
    setUnauthorizedHandler(onUnauthorized)
    renderProbe()
    act(() => FakeSocket.last.onopen?.())

    // 1006: the deploy / dropped-network case, which MUST stay a reconnect.
    closeWith(1006)

    expect(onUnauthorized).not.toHaveBeenCalled()
    expect(FakeSocket.instances).toHaveLength(1)
    act(() => void vi.advanceTimersByTime(800)) // 400 * 2 ** 1
    expect(FakeSocket.instances).toHaveLength(2)
  })

  it('does not redial after the hook unmounts', () => {
    const { unmount } = renderProbe()
    act(() => FakeSocket.last.onopen?.())
    unmount()

    closeWith(1006)
    act(() => void vi.advanceTimersByTime(120_000))
    expect(FakeSocket.instances).toHaveLength(1)
  })
})
