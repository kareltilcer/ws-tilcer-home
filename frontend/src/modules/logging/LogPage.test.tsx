import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { AuditEvent, AuditEventDetail } from '@/api/types'
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
vi.mock('@/api/endpoints', () => ({ listLogs, getLog, getEntityTimeline, getLogStats }))

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
