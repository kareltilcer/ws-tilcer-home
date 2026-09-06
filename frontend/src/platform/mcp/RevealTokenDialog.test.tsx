import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { McpTokenCreated } from '@/api/types'
import { RevealTokenDialog } from './RevealTokenDialog'

// ⚠⚠ THE ONE SCREEN IN THIS APPLICATION THAT CANNOT BE RE-OPENED (v11, D315).
//
// Only the SHA-256 of the secret is stored, so a member who dismisses this dialog
// without copying has lost the token. Every assertion below is about a dismissal
// that must NOT happen, or about a copy that must be confirmed — and none of it is
// visible in a screenshot, which is why it is pinned here rather than left to the
// artboard.

const SECRET = 'hmcp_9tKpR2vX8mQ4wZ7nB1sL6dF3gH0jY5cA9eU2iO4kT8r'

const token: McpTokenCreated = {
  id: 't-1',
  name: 'Claude na notebooku',
  prefix: 'hmcp_9tK',
  modules: [],
  created_at: '2026-09-06T14:20:00Z',
  expires_at: null,
  last_used_at: null,
  last_used_ip: null,
  revoked_at: null,
  secret: SECRET,
}

/** A clipboard the test can read back. jsdom ships none. */
function stubClipboard(impl: (text: string) => Promise<void> = () => Promise.resolve()) {
  const writeText = vi.fn(impl)
  Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })
  return writeText
}

describe('the reveal dialog refuses every dismissal but Hotovo', () => {
  // ⚠ ESCAPE IS MUSCLE MEMORY, AND HERE MUSCLE MEMORY COSTS THE TOKEN. Every other
  // dialog in Home closes on it; this is the single deviation, and it is the whole
  // reason this component is not `ResponsiveModal`.
  it('stays open on Escape', async () => {
    const onDone = vi.fn()
    render(<RevealTokenDialog token={token} onDone={onDone} />)
    await userEvent.keyboard('{Escape}')
    expect(onDone).not.toHaveBeenCalled()
    expect(screen.getByRole('dialog')).toBeInTheDocument()
  })

  it('stays open when the overlay behind it is clicked', async () => {
    const onDone = vi.fn()
    const { baseElement } = render(<RevealTokenDialog token={token} onDone={onDone} />)
    // Radix's overlay is the dialog's sibling in the portal; a pointer-down on it
    // is what "clicking outside" actually is.
    const overlay = baseElement.querySelector('[data-radix-popper-content-wrapper], .fixed.inset-0')
    expect(overlay).not.toBeNull()
    await userEvent.click(overlay as Element)
    expect(onDone).not.toHaveBeenCalled()
    expect(screen.getByRole('dialog')).toBeInTheDocument()
  })

  // ⚠ AND THERE IS NO ✕. `ResponsiveModal` carries one in its header, which is the
  // third dismissal a member reaches for without reading anything.
  it('offers no close button in its header', () => {
    render(<RevealTokenDialog token={token} onDone={() => {}} />)
    expect(screen.queryByRole('button', { name: 'Zavřít' })).not.toBeInTheDocument()
  })

  it('closes on Hotovo, which is the only exit', async () => {
    const onDone = vi.fn()
    render(<RevealTokenDialog token={token} onDone={onDone} />)
    await userEvent.click(screen.getByRole('button', { name: 'Hotovo' }))
    expect(onDone).toHaveBeenCalledOnce()
  })
})

describe('the secret is rendered once, inside the snippet', () => {
  // ⚠ ONE RENDERING, NOT TWO. The bare token is deliberately NOT shown above the
  // connect snippet: two copies of one secret on one screen double the chance a
  // copy ends up somewhere it should not, and at 375 px they double the height of
  // the thing a member is trying to read. The two buttons are two ways to copy ONE
  // rendering.
  it('puts the secret in exactly one place in the DOM', () => {
    const { baseElement } = render(<RevealTokenDialog token={token} onDone={() => {}} />)
    const occurrences = (baseElement.textContent ?? '').split(SECRET).length - 1
    expect(occurrences).toBe(1)
  })

  it('renders it as a ready-to-paste client configuration', () => {
    const { baseElement } = render(<RevealTokenDialog token={token} onDone={() => {}} />)
    const text = baseElement.textContent ?? ''
    expect(text).toContain('mcpServers')
    expect(text).toContain('/mcp')
    expect(text).toContain(`Bearer ${SECRET}`)
  })

  // ⚠ THE WARNING IS ABOVE THE VALUE, NOT BELOW IT. A caution read after the thing
  // it cautions about is decoration — so the order is asserted rather than trusted
  // to whoever next edits the JSX.
  it('places the warning before the secret', () => {
    const { baseElement } = render(<RevealTokenDialog token={token} onDone={() => {}} />)
    const text = baseElement.textContent ?? ''
    expect(text.indexOf('zobrazí jen jednou')).toBeGreaterThanOrEqual(0)
    expect(text.indexOf('zobrazí jen jednou')).toBeLessThan(text.indexOf(SECRET))
  })
})

describe('copying', () => {
  // ⚠ FOCUS LANDS ON THE COPY BUTTON AND NOT ON *Hotovo*. Radix focuses the first
  // focusable child by default, and here that would put the one irreversible exit
  // under an Enter press somebody makes on reflex.
  it('focuses the copy button rather than the exit', async () => {
    render(<RevealTokenDialog token={token} onDone={() => {}} />)
    await vi.waitFor(() => {
      expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Kopírovat konfiguraci' }))
    })
  })

  it('copies the whole configuration, and announces that it did', async () => {
    const writeText = stubClipboard()
    render(<RevealTokenDialog token={token} onDone={() => {}} />)
    await userEvent.click(screen.getByRole('button', { name: 'Kopírovat konfiguraci' }))
    expect(writeText).toHaveBeenCalledOnce()
    expect(writeText.mock.calls[0][0]).toContain('mcpServers')
    expect(await screen.findByText('Zkopírováno — celá konfigurace')).toBeInTheDocument()
  })

  it('copies the bare token for a client that wants it in an env var', async () => {
    const writeText = stubClipboard()
    render(<RevealTokenDialog token={token} onDone={() => {}} />)
    await userEvent.click(screen.getByRole('button', { name: 'Kopírovat jen token' }))
    expect(writeText).toHaveBeenCalledWith(SECRET)
    expect(await screen.findByText('Zkopírováno — jen token')).toBeInTheDocument()
  })

  // ⚠ A COPY THAT SILENTLY FAILED IS INDISTINGUISHABLE FROM ONE THAT WORKED until
  // the paste — and here the paste is the only chance there will ever be. A
  // clipboard write can be refused outright (an insecure context, a permission
  // policy), and the member has to be told while the dialog is still open.
  it('says so when the clipboard refuses, and names the fallback', async () => {
    stubClipboard(() => Promise.reject(new Error('denied')))
    render(<RevealTokenDialog token={token} onDone={() => {}} />)
    await userEvent.click(screen.getByRole('button', { name: 'Kopírovat konfiguraci' }))
    const message = await screen.findByText(/Zkopírovat se nepodařilo/)
    expect(message.textContent).toContain('ručně')
  })

  // ⚠ ANNOUNCED, NOT ONLY SHOWN. The live region is the whole of what a
  // screen-reader user gets that a sighted one gets from the button flashing.
  it('puts the result in a polite live region', () => {
    const { baseElement } = render(<RevealTokenDialog token={token} onDone={() => {}} />)
    expect(baseElement.querySelector('[aria-live="polite"]')).not.toBeNull()
  })
})
