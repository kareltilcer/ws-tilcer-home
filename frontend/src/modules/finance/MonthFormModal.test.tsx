import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { FinanceMonth } from './api/types'
import { MonthFormModal } from './MonthFormModal'

const recorded: FinanceMonth = {
  id: 'm1',
  month: '2026-07',
  income_kaja: 60000,
  income_andy: 40000,
  rates: { personal: 20, operational: 60, fun: 10, no_fun: 10 },
  split: {
    total_income: 100000,
    personal_kaja: 12000,
    personal_andy: 8000,
    to_operational_kaja: 48000,
    to_operational_andy: 32000,
    operational_received: 80000,
    fun_savings: 10000,
    no_fun_savings: 10000,
    needs: 60000,
  },
  created_by: null,
  created_at: '2026-07-01T00:00:00Z',
  updated_at: '2026-07-01T00:00:00Z',
}

function renderForm(overrides: Partial<Parameters<typeof MonthFormModal>[0]> = {}) {
  const onSubmit = vi.fn()
  render(
    <MonthFormModal
      open
      onOpenChange={() => {}}
      editing={null}
      months={[recorded]}
      defaultMonth="2026-08"
      saving={false}
      error={null}
      onSubmit={onSubmit}
      {...overrides}
    />,
  )
  return { onSubmit }
}

describe('MonthFormModal', () => {
  it('pre-fills the rates from the most recent month (FR-F6)', () => {
    renderForm()
    expect(screen.getByLabelText(/^Osobní$/)).toHaveValue('20')
    expect(screen.getByLabelText(/^Provozní$/)).toHaveValue('60')
  })

  it('blocks submit until the rates total 100, showing the running remainder', async () => {
    const user = userEvent.setup()
    const { onSubmit } = renderForm()

    const submit = screen.getByRole('button', { name: 'Přidat měsíc' })
    expect(submit).toBeEnabled()

    // Knock the rates off 100: the chip says how far off, and submit locks.
    await user.clear(screen.getByLabelText(/^Osobní$/))
    await user.type(screen.getByLabelText(/^Osobní$/), '15')

    expect(screen.getByText(/zbývá 5 %/)).toBeInTheDocument()
    expect(submit).toBeDisabled()
    expect(screen.getByText('Sazby musí dát dohromady 100 %.')).toBeInTheDocument()

    // The balance button pours the remainder into no-fun savings.
    await user.click(screen.getByRole('button', { name: /Doplnit do 100 %/ }))
    expect(screen.getByText(/sedí — 100 %/)).toBeInTheDocument()
    expect(submit).toBeEnabled()

    await user.click(submit)
    expect(onSubmit).toHaveBeenCalledWith({
      month: '2026-08',
      income_kaja: 0,
      income_andy: 0,
      rates: { personal: 15, operational: 60, fun: 10, no_fun: 15 },
    })
  })

  it('reports the rates as over 100 as well as under', async () => {
    const user = userEvent.setup()
    renderForm()
    await user.clear(screen.getByLabelText(/^Osobní$/))
    await user.type(screen.getByLabelText(/^Osobní$/), '30')
    expect(screen.getByText(/přebývá 10 %/)).toBeInTheDocument()
  })

  it('disables months that are already recorded', () => {
    renderForm()
    const taken = screen.getByRole('option', { name: /červenec 2026/ })
    expect(taken).toBeDisabled()
    expect(taken).toHaveTextContent('už zadaný')
    // The month being EDITED is still selectable — it is its own row.
    render(
      <MonthFormModal
        open
        onOpenChange={() => {}}
        editing={recorded}
        months={[recorded]}
        defaultMonth="2026-08"
        saving={false}
        error={null}
        onSubmit={() => {}}
      />,
    )
    expect(screen.getAllByRole('option', { name: /červenec 2026/ }).at(-1)).toBeEnabled()
  })

  it('previews the split live and shows 0 Kč with a footnote when needs goes negative', async () => {
    const user = userEvent.setup()
    renderForm()

    await user.type(screen.getByLabelText('Příjem Kája'), '60000')
    await user.type(screen.getByLabelText('Příjem Andy'), '40000')
    // The preview follows the inputs — the whole reason the form is worth using.
    expect(screen.getByText('100 000 Kč')).toBeInTheDocument()

    // operational = 0 at a realistic income drives needs to −2 Kč. The DATA is
    // never clamped; the screen shows 0 Kč and states the rounding.
    for (const [label, value] of [
      ['Osobní', '90'],
      ['Provozní', '0'],
      ['Zábavné spoření', '5'],
      ['Nezábavné spoření', '5'],
    ] as const) {
      const field = screen.getByLabelText(new RegExp(`^${label}$`))
      await user.clear(field)
      await user.type(field, value)
    }
    await user.clear(screen.getByLabelText('Příjem Kája'))
    await user.type(screen.getByLabelText('Příjem Kája'), '6887385')
    await user.clear(screen.getByLabelText('Příjem Andy'))
    await user.type(screen.getByLabelText('Příjem Andy'), '2030905')

    expect(screen.getByText(/Potřeby vyjdou na .*2 Kč/)).toBeInTheDocument()
  })
})
