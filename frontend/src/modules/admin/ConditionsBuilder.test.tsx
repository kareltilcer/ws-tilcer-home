import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import {
  ConditionsBuilder,
  conditionsForSave,
  conditionsPhrase,
  conditionsValid,
} from './ConditionsBuilder'
import type { NotificationCatalog, NotificationConditions } from './api/types'

const catalog: NotificationCatalog = {
  actions: [],
  metrics: [
    { key: 'events.pripominky_today', label: 'Připomínky na dnešek', unit: 'připomínek', scope: 'household' },
    { key: 'notes.pinned_count', label: 'Připnuté poznámky', unit: 'poznámek', scope: 'personal' },
  ],
  lists: [
    // A list-only key must be offerable too — its condition value is the length.
    { key: 'events.overdue', label: 'Po termínu', empty: 'nic po termínu', scope: 'household' },
    { key: 'events.pripominky_today', label: 'Připomínky na dnešek', empty: 'žádné připomínky', scope: 'household' },
  ],
  tokens: {},
  members: [],
}

function Harness({
  context = 'summary',
  onSaved,
}: {
  context?: 'trigger' | 'summary'
  onSaved?: (v: NotificationConditions | null) => void
}) {
  const [value, setValue] = useState<NotificationConditions | null>(null)
  return (
    <ConditionsBuilder
      context={context}
      catalog={catalog}
      value={value}
      onChange={(v) => {
        setValue(v)
        onSaved?.(v)
      }}
    />
  )
}

describe('ConditionsBuilder', () => {
  it('builds the motivating rule: any of two counts above zero', async () => {
    const user = userEvent.setup()
    const changes = vi.fn()
    render(<Harness onSaved={changes} />)

    // Each row renders two selects (key + operator); the key one is the one
    // whose first option is the placeholder.
    const keySelects = () =>
      screen
        .getAllByRole('combobox')
        .filter((el) => (el as HTMLSelectElement).options[0]?.text.includes('Vyber údaj'))

    await user.click(screen.getByRole('button', { name: /přidat podmínku/i }))
    await user.selectOptions(keySelects()[0], 'events.pripominky_today')
    await user.click(screen.getByRole('button', { name: /přidat podmínku/i }))
    await user.selectOptions(keySelects()[1], 'events.overdue')

    // Two rows expose the combinator; pick "stačí jedna" (any).
    await user.click(screen.getByRole('radio', { name: /stačí jedna/i }))

    const last = changes.mock.calls.at(-1)?.[0] as NotificationConditions
    expect(last.mode).toBe('any')
    expect(last.items).toEqual([
      { key: 'events.pripominky_today', op: 'gt', value: 0 },
      { key: 'events.overdue', op: 'gt', value: 0 },
    ])
  })

  it('hides personal keys from the trigger context', async () => {
    const user = userEvent.setup()
    render(<Harness context="trigger" />)
    await user.click(screen.getByRole('button', { name: /přidat podmínku/i }))
    expect(screen.queryByRole('option', { name: /Připnuté poznámky/ })).not.toBeInTheDocument()
    // …but a summary offers them, flagged as personal.
  })

  it('offers personal keys to summaries, flagged', async () => {
    const user = userEvent.setup()
    render(<Harness context="summary" />)
    await user.click(screen.getByRole('button', { name: /přidat podmínku/i }))
    expect(screen.getByRole('option', { name: /Připnuté poznámky · osobní/ })).toBeInTheDocument()
  })

  it('flags a row without a key and blocks validity', async () => {
    const user = userEvent.setup()
    render(<Harness />)
    await user.click(screen.getByRole('button', { name: /přidat podmínku/i }))
    expect(screen.getByText(/vyber údaj v každé podmínce/i)).toBeInTheDocument()
    expect(conditionsValid({ mode: 'all', items: [{ key: '', op: 'gt', value: 0 }] })).toBe(false)
    expect(conditionsValid(null)).toBe(true)
  })
})

describe('conditions helpers', () => {
  it('normalizes an empty block to null for the wire', () => {
    expect(conditionsForSave({ mode: 'all', items: [] })).toBeNull()
    expect(conditionsForSave(null)).toBeNull()
    const kept = { mode: 'any' as const, items: [{ key: 'a', op: 'gt' as const, value: 0 }] }
    expect(conditionsForSave(kept)).toBe(kept)
  })

  it('phrases a block with catalog labels and the combinator', () => {
    const phrase = conditionsPhrase(
      {
        mode: 'any',
        items: [
          { key: 'events.pripominky_today', op: 'gt', value: 0 },
          { key: 'events.overdue', op: 'gt', value: 0 },
        ],
      },
      catalog,
    )
    expect(phrase).toBe('Připomínky na dnešek je víc než 0 nebo Po termínu je víc než 0')
    expect(conditionsPhrase(null, catalog)).toBeNull()
  })
})
