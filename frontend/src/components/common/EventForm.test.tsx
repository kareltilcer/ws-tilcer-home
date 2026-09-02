import { describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { EventForm } from './EventForm'
import { LEAD_LABEL } from './reminderLead'

// ⚠ THE LEAD SELECTOR IS LAID OUT WHETHER OR NOT THE BOX IS TICKED. It used to be
// unmounted behind a min-height reserve that held one row of chips while the phone
// drew two, so ticking the box shoved the rest of the form down. The assertion
// below is on the DOM rather than on pixels, because that is the part jsdom can
// actually see: the chips exist unticked, and they are hidden rather than unmounted.
//
// ⚠ AND JSDOM SEES ONLY HALF OF THE HIDING. vitest.config.ts sets `css: false`, so
// Tailwind's `invisible` is a bare class name here with no computed style behind
// it — naming the class is the most this environment can say about it. The
// queryByRole assertion below therefore rests on the `aria-hidden` beside that
// class, not on visibility: in a browser both apply, in jsdom only one of them
// exists, and that is why the component carries both.

// Only the create call is stubbed: the form is rendered in create mode, so the
// getEvent query never enables and reconcileLinks walks two empty lists.
const createEvent = vi.hoisted(() => vi.fn())
vi.mock('@/modules/events/api/endpoints', async (orig) => ({
  ...(await orig<Record<string, unknown>>()),
  createEvent,
}))

function renderForm() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <EventForm open onOpenChange={() => {}} />
    </QueryClientProvider>,
  )
}

describe('EventForm reminder lead', () => {
  it('lays the lead chips out while the box is unticked, hidden rather than unmounted', () => {
    renderForm()
    const sameDay = screen.getByRole('button', { name: LEAD_LABEL['0d'], hidden: true })
    expect(sameDay.parentElement).toHaveClass('invisible')
    // Laid out, and at the same time genuinely hidden: the chips are not in the
    // accessibility tree (and so, in a browser, not in the tab order) until the box
    // is ticked — what unmounting used to buy and opacity would not have.
    expect(screen.queryByRole('button', { name: LEAD_LABEL['0d'] })).toBeNull()
  })

  it('offers every lead in the shared table, worded as the detail view words them', async () => {
    const user = userEvent.setup()
    renderForm()
    await user.click(screen.getByLabelText('Připomenout'))
    for (const label of Object.values(LEAD_LABEL)) {
      expect(screen.getByRole('button', { name: label })).toBeInTheDocument()
    }
  })

  it('sends reminder_lead 0d when the same-day chip is chosen', async () => {
    const user = userEvent.setup()
    createEvent.mockResolvedValue({ id: 'e1' })
    renderForm()

    await user.type(screen.getByPlaceholderText('Např. Zaplatit plyn'), 'Vynést koš')
    // Two date inputs are laid out now (the end-of-recurrence one is hidden, not
    // unmounted); the first is the event's own date. userEvent cannot type into a
    // jsdom date field, so the value is set the way the browser would deliver it.
    const date = document.querySelectorAll('input[type="date"]')[0] as HTMLInputElement
    fireEvent.change(date, { target: { value: '2026-07-15' } })
    await user.click(screen.getByLabelText('Připomenout'))
    await user.click(screen.getByRole('button', { name: LEAD_LABEL['0d'] }))
    await user.click(screen.getByRole('button', { name: 'Vytvořit' }))

    await waitFor(() => expect(createEvent).toHaveBeenCalled())
    expect(createEvent.mock.calls[0][0]).toMatchObject({
      title: 'Vynést koš',
      starts_on: '2026-07-15',
      reminder_enabled: true,
      reminder_lead: '0d',
    })
  })

  it('marks the chosen lead pressed, which is the only thing that carries the selection', async () => {
    const user = userEvent.setup()
    renderForm()
    await user.click(screen.getByLabelText('Připomenout'))
    await user.click(screen.getByRole('button', { name: LEAD_LABEL['2w'] }))
    expect(screen.getByRole('button', { name: LEAD_LABEL['2w'] })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('button', { name: LEAD_LABEL['0d'] })).toHaveAttribute('aria-pressed', 'false')
  })
})
