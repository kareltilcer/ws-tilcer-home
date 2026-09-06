import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { AuditEvent, AuditEventDetail } from './api/types'
import { LogPage } from './LogPage'

// A FAILED REQUEST IS NOT AN EMPTY LOG. Every view here falls back to `?? []`,
// so without an isError branch a 500 renders as "nothing matches your filter" —
// the opposite of what happened, on the one screen whose whole job is to say
// what did happen. Each case below pins one view's error branch ahead of its
// empty state; reordering the ternary must fail here.

const listLogs = vi.hoisted(() => vi.fn())
const getLog = vi.hoisted(() => vi.fn())
const getEntityTimeline = vi.hoisted(() => vi.fn())
const getLogStats = vi.hoisted(() => vi.fn())
vi.mock('./api/endpoints', () => ({ listLogs, getLog, getEntityTimeline, getLogStats }))

const event = (): AuditEvent => ({
  redacted: false,
  id: 'ev1',
  ts: '2026-08-20T10:00:00Z',
  actor_user_id: 'marie',
  actor_type: 'user',
  actor_label: 'Marie',
  module: 'todo',
  action: 'card.create',
  entity_type: 'card',
  entity_id: 'c1',
  summary: 'vytvořena karta',
  level: 'info',
  request_id: null,
  site: 'home',
  meta: null,
  change_count: 0,
  via: null,
  via_token_id: null,
})

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <LogPage />
    </QueryClientProvider>,
  )
}

const retryButton = () => screen.findByRole('button', { name: /zkusit znovu/i })

describe('LogPage', () => {
  it('offers a retry when the log list fails, never the empty state', async () => {
    listLogs.mockRejectedValueOnce(new Error('boom'))
    renderPage()

    expect(await retryButton()).toBeInTheDocument()
    expect(screen.queryByText('Žádné záznamy pro tento filtr.')).not.toBeInTheDocument()
  })

  it('still shows the empty state when the list succeeds with nothing to draw', async () => {
    listLogs.mockResolvedValue({ items: [], next_cursor: null })
    renderPage()

    expect(await screen.findByText('Žádné záznamy pro tento filtr.')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /zkusit znovu/i })).not.toBeInTheDocument()
  })

  it('offers a retry when the stats request fails, never the empty state', async () => {
    listLogs.mockResolvedValue({ items: [], next_cursor: null })
    getLogStats.mockRejectedValueOnce(new Error('boom'))
    renderPage()
    await userEvent.click(screen.getByRole('tab', { name: 'Analytika' }))

    expect(await retryButton()).toBeInTheDocument()
    expect(screen.queryByText('Žádná data.')).not.toBeInTheDocument()
  })

  it('offers a retry when an event detail fails, never "no field changes"', async () => {
    listLogs.mockResolvedValue({ items: [event()], next_cursor: null })
    getLog.mockRejectedValueOnce(new Error('boom'))
    renderPage()
    await userEvent.click(await screen.findByRole('button', { name: /vytvořena karta/i }))

    expect(await retryButton()).toBeInTheDocument()
    expect(screen.queryByText('Bez změn polí.')).not.toBeInTheDocument()
  })

  it('offers a retry when the entity timeline fails, never the empty history', async () => {
    const detail: AuditEventDetail = { ...event(), changes: [] }
    listLogs.mockResolvedValue({ items: [event()], next_cursor: null })
    getLog.mockResolvedValue(detail)
    getEntityTimeline.mockRejectedValueOnce(new Error('boom'))
    renderPage()
    await userEvent.click(await screen.findByRole('button', { name: /vytvořena karta/i }))
    await userEvent.click(await screen.findByRole('button', { name: /historie této entity/i }))

    expect(await retryButton()).toBeInTheDocument()
    expect(screen.queryByText('Žádná historie.')).not.toBeInTheDocument()
  })
})

// v11 — the Log's eighth filtering dimension, and the chip that goes with it
// (D290/D291/D292).
describe('the origin of a change', () => {
  it('draws a neutral chip on a row written through an assistant', async () => {
    listLogs.mockResolvedValue({
      items: [{ ...event(), actor_label: 'Marie · Claude (notebook)', via: 'mcp', via_token_id: 'tok-1' }],
      next_cursor: null,
    })
    renderPage()

    const row = await screen.findByRole('button', { name: /vytvořena karta/i })
    expect(row.textContent).toContain('Přes asistenta')
  })

  it('draws no chip on an ordinary browser row', async () => {
    listLogs.mockResolvedValue({ items: [event()], next_cursor: null })
    renderPage()

    // ⚠ SCOPED TO THE ROW, not to the screen: the FILTER chip carries the same
    // words, so a page-wide `queryByText` would find it and the absence would
    // mean nothing.
    const row = await screen.findByRole('button', { name: /vytvořena karta/i })
    expect(row.textContent).not.toContain('Přes asistenta')
  })

  // ⚠ THE CHIP CARRIES NO TOKEN NAME (D291). The actor beside it already reads
  // "Marie · Claude (notebook)" — the backend builds that label from the token at
  // write time — so a chip repeating it would say the same thing twice in one row,
  // and would need a join this response does not carry.
  it('leaves the token name to the actor label', async () => {
    listLogs.mockResolvedValue({
      items: [{ ...event(), actor_label: 'Marie · Claude (notebook)', via: 'mcp', via_token_id: 'tok-1' }],
      next_cursor: null,
    })
    renderPage()

    const row = await screen.findByRole('button', { name: /vytvořena karta/i })
    expect(row.textContent).toContain('Marie · Claude (notebook)')
    expect(row.textContent?.split('Claude (notebook)')).toHaveLength(2)
  })

  it('sends via=mcp once Přes asistenta is chosen and Filtrovat pressed', async () => {
    listLogs.mockResolvedValue({ items: [], next_cursor: null })
    renderPage()
    await screen.findByText('Žádné záznamy pro tento filtr.')

    await userEvent.click(screen.getByRole('button', { name: 'Přes asistenta' }))
    await userEvent.click(screen.getByRole('button', { name: 'Filtrovat' }))

    await vi.waitFor(() => {
      expect(listLogs.mock.calls.at(-1)?.[0]?.via).toBe('mcp')
    })
  })

  // ⚠ *Vše* CLEARS THE PARAMETER RATHER THAN SENDING AN EMPTY ONE. `via=` outside
  // the enum is a 422 on this route — the one filter here that is a switch rather
  // than an equality bind — so a "clear" that sent a blank would turn the default
  // choice into an error the member cannot read.
  it('omits the parameter entirely when Vše is chosen', async () => {
    listLogs.mockResolvedValue({ items: [], next_cursor: null })
    renderPage()
    await screen.findByText('Žádné záznamy pro tento filtr.')

    await userEvent.click(screen.getByRole('button', { name: 'Přes asistenta' }))
    await userEvent.click(screen.getByRole('button', { name: 'Filtrovat' }))
    await vi.waitFor(() => {
      expect(listLogs.mock.calls.at(-1)?.[0]?.via).toBe('mcp')
    })

    await userEvent.click(screen.getByRole('button', { name: 'Vše' }))
    await userEvent.click(screen.getByRole('button', { name: 'Filtrovat' }))
    await vi.waitFor(() => {
      expect(listLogs.mock.calls.at(-1)?.[0]?.via).toBeUndefined()
    })
  })
})
