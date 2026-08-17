import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import type { RozpocetWidget as RozpocetData } from '@/api/types'
import type { WidgetComponentProps } from '@/platform/widgets/shared'
import { RozpocetWidget } from './RozpocetWidget'

const noop = () => {}
const props: Omit<WidgetComponentProps, 'data' | 'canWrite'> = {
  onOpenCard: noop,
  onOpenReminder: noop,
  onOpenNote: noop,
  onOpenDocument: noop,
  onCompleteTask: noop,
  onCompleteReminder: noop,
}

function renderWidget(data: RozpocetData, canWrite = true) {
  return render(
    <MemoryRouter>
      <RozpocetWidget {...props} data={data} canWrite={canWrite} />
    </MemoryRouter>,
  )
}

describe('RozpocetWidget', () => {
  // The missing state is the one that matters: it is the only place the household
  // is told a month went unrecorded.
  it('prompts to enter the month when it is missing', () => {
    renderWidget({ state: 'missing', month: '2026-08' })
    expect(screen.getByRole('button', { name: 'Zadat srpen 2026' })).toBeEnabled()
    expect(screen.getByText(/srpen 2026 ještě nikdo nezadal/)).toBeInTheDocument()
  })

  it('leaves the prompt visible but disabled for a reader', () => {
    renderWidget({ state: 'missing', month: '2026-08' }, false)
    // Visible and disabled, never hidden — the screen reads the same for everyone.
    expect(screen.getByRole('button', { name: 'Zadat srpen 2026' })).toBeDisabled()
  })

  it('shows the headline numbers when the month is recorded', () => {
    renderWidget({
      state: 'recorded',
      month: '2026-08',
      total_income: 100000,
      personal_kaja: 12000,
      personal_andy: 8000,
      needs: 60000,
      savings: 20000,
    })
    expect(screen.getByText('100 000 Kč')).toBeInTheDocument()
    expect(screen.getByText('Kája si nechá')).toBeInTheDocument()
    expect(screen.getByText('12 000 Kč')).toBeInTheDocument()
    expect(screen.getByText('20 000 Kč')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /Zadat/ })).not.toBeInTheDocument()
  })

  // A negative `needs` is correct arithmetic; the widget renders 0 Kč rather than
  // "−2 Kč", which would read as a bug on the household's landing page.
  it('renders a negative needs as 0 Kč', () => {
    renderWidget({
      state: 'recorded',
      month: '2026-08',
      total_income: 8918290,
      personal_kaja: 6198647,
      personal_andy: 1827815,
      needs: -2,
      savings: 891830,
    })
    expect(screen.getByText('0 Kč')).toBeInTheDocument()
  })
})
