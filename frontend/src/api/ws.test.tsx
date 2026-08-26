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

  // ⚠ The close code only reaches a tab that was CONNECTED when the session went.
  // A phone asleep or offline at that moment has no socket to close, so nothing is
  // ever sent to it; on resume its upgrade is 401ed and the browser reports 1006,
  // which is indistinguishable from a dropped network. These three pin the second
  // signal that closes that hole.
  describe('a session that ended while the tab was disconnected', () => {
    /** dialAndFail runs `n` dial→close(1006) cycles, none of which ever open. */
    const dialAndFail = async (n: number) => {
      for (let i = 0; i < n; i++) {
        closeWith(1006)
        await act(async () => {
          await vi.advanceTimersByTimeAsync(400 * 2 ** Math.min(i + 1, 6))
        })
      }
    }
    const response = (status: number, body: unknown = {}) => ({
      ok: status >= 200 && status < 300,
      status,
      json: async () => body,
    })

    it('probes the session after repeated dials that never open, and hands off on a 401', async () => {
      const fetchMock = vi.fn().mockResolvedValue(response(401, { error: 'unauthorized' }))
      vi.stubGlobal('fetch', fetchMock)
      const onUnauthorized = vi.fn()
      setUnauthorizedHandler(onUnauthorized)
      renderProbe()

      // Two failures are still just a bad network: no question asked.
      await dialAndFail(2)
      expect(fetchMock).not.toHaveBeenCalled()
      expect(onUnauthorized).not.toHaveBeenCalled()

      // The third asks the server outright.
      await dialAndFail(1)
      expect(fetchMock).toHaveBeenCalledTimes(1)
      expect(fetchMock.mock.calls[0][0]).toBe('/api/auth/session')
      expect(onUnauthorized).toHaveBeenCalledTimes(1)

      // And the loop is over — no redial at the cap, or ever.
      const sockets = FakeSocket.instances.length
      await act(async () => {
        await vi.advanceTimersByTimeAsync(120_000)
      })
      expect(FakeSocket.instances).toHaveLength(sockets)
    })

    it('keeps reconnecting when the probe says the session is fine', async () => {
      const fetchMock = vi.fn().mockResolvedValue(response(200, { user: { id: 'u1' } }))
      vi.stubGlobal('fetch', fetchMock)
      const onUnauthorized = vi.fn()
      setUnauthorizedHandler(onUnauthorized)
      renderProbe()

      await dialAndFail(3)
      expect(fetchMock).toHaveBeenCalledTimes(1)
      // A server that is merely restarting must not sign anybody out.
      expect(onUnauthorized).not.toHaveBeenCalled()
      // The backoff is at its cap by now, so give it more than that: the point is
      // that a redial still comes at all.
      const sockets = FakeSocket.instances.length
      closeWith(1006)
      await act(async () => {
        await vi.advanceTimersByTimeAsync(30_000)
      })
      expect(FakeSocket.instances.length).toBeGreaterThan(sockets)
    })

    it('never probes a socket that opened — a deploy is not a revocation', async () => {
      const fetchMock = vi.fn().mockResolvedValue(response(401))
      vi.stubGlobal('fetch', fetchMock)
      renderProbe()

      // Every dial connects and is then dropped: the session authorised each one,
      // so there is nothing to ask about however often the link breaks.
      for (let i = 0; i < 5; i++) {
        act(() => FakeSocket.last.onopen?.())
        closeWith(1006)
        await act(async () => {
          await vi.advanceTimersByTimeAsync(800)
        })
      }
      expect(fetchMock).not.toHaveBeenCalled()
    })
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
