import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import type { ElHistory, ElPeriod } from '../api/types'
import { HistorieTab } from './HistorieTab'

// A FAILED REQUEST IS NOT AN EMPTY HISTORY, and the two look identical unless
// the component says otherwise: `history.data?.months ?? []` turns any error
// into "not enough history yet", which claims the opposite of what happened and
// would hide a year of readings behind a matter-of-fact sentence.

const getHistory = vi.hoisted(() => vi.fn())
vi.mock('../api/endpoints', () => ({ getHistory }))

const period = (id: string, starts: string, ends: string): ElPeriod => ({
  id,
  starts_on: starts,
  ends_on: ends,
  ends_on_confirmed: false,
  invoiced_total_haler: null,
  invoiced_balance_haler: null,
  invoiced_vt_dkwh: null,
  invoiced_nt_dkwh: null,
  invoiced_at: null,
  note: null,
  created_by: null,
  created_at: '',
  updated_at: '',
})

function renderTab(periods: ElPeriod[], currentId: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <HistorieTab periods={periods} currentId={currentId} />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('HistorieTab', () => {
  it('offers a retry when the history request fails, never the empty state', async () => {
    getHistory.mockRejectedValueOnce(new Error('boom'))
    renderTab([period('p1', '2026-01-01', '2026-06-30')], 'p1')

    expect(await screen.findByRole('button', { name: /zkusit|znovu/i })).toBeInTheDocument()
    expect(screen.queryByText('Historie zatím není')).not.toBeInTheDocument()
  })

  it('still shows the empty state when the request succeeds with nothing to draw', async () => {
    const empty: ElHistory = { months: [] }
    getHistory.mockResolvedValueOnce(empty)
    renderTab([period('p1', '2026-01-01', '2026-06-30')], 'p1')

    expect(await screen.findByText('Historie zatím není')).toBeInTheDocument()
  })

  // The head of the list is merely the newest by starts_on, so dropping it would
  // file the period actually running under "closed" the moment one is created
  // ahead of its start.
  it('lists every period except the one on screen, matched by id', async () => {
    const drawn: ElHistory = {
      months: [
        {
          month: '2026-03',
          vt_dkwh: 3790,
          nt_dkwh: 8200,
          energy_haler: 99200,
          fees_haler: 35000,
          total_haler: 134200,
          is_approximate: true,
        },
      ],
    }
    getHistory.mockResolvedValueOnce(drawn)
    renderTab(
      [
        period('future', '2027-01-01', '2027-12-31'),
        period('running', '2026-07-01', '2026-12-31'),
        period('past', '2026-01-01', '2026-06-30'),
      ],
      'running',
    )

    expect(await screen.findByText('Uzavřená období')).toBeInTheDocument()
    expect(screen.getByText('1. 1. 2027 – 31. 12. 2027')).toBeInTheDocument()
    expect(screen.getByText('1. 1. 2026 – 30. 6. 2026')).toBeInTheDocument()
    // The period being charted above must not also appear as a closed one.
    expect(screen.queryByText('1. 7. 2026 – 31. 12. 2026')).not.toBeInTheDocument()
  })
})
