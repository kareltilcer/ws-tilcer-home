import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { Composer, type ComposerContext } from './Composer'
import type { NotificationCatalog } from './api/types'

const catalog: NotificationCatalog = {
  actions: [{ key: 'card.move', module: 'todo', label: 'Když někdo přesune úkol' }],
  metrics: [
    { key: 'todo.pravedelam_count', label: 'Úkoly v Právě dělám', unit: 'úkolů', scope: 'household' },
    { key: 'notes.pinned_count', label: 'Připnuté poznámky', unit: 'poznámek', scope: 'personal' },
    // Every real list shares its label with the metric of the same key, so the
    // fixture does too — the chips have to stay tellable apart.
    { key: 'events.pripominky_today', label: 'Připomínky na dnešek', unit: 'připomínek', scope: 'household' },
  ],
  lists: [
    {
      key: 'events.pripominky_today',
      label: 'Připomínky na dnešek',
      empty: 'žádné připomínky',
      scope: 'household',
    },
  ],
  tokens: {
    broadcast: { time: ['now', 'date'] },
    trigger: {
      time: ['now', 'date'],
      event: ['event.summary', 'event.actor_label'],
      change: ['change.<pole>.old', 'change.<pole>.new'],
    },
    summary: {
      time: ['now', 'date'],
      metric: [
        'metric.todo.pravedelam_count',
        'metric.notes.pinned_count',
        'metric.events.pripominky_today',
      ],
      list: ['list.events.pripominky_today'],
    },
  },
  members: [],
}

/** Harness keeps the composer controlled, the way the real editors do. */
function Harness({
  context = 'summary',
  initialBody = '',
}: {
  context?: ComposerContext
  initialBody?: string
}) {
  const [title, setTitle] = useState('Dobré ráno')
  const [body, setBody] = useState(initialBody)
  return (
    <Composer
      context={context}
      catalog={catalog}
      title={title}
      body={body}
      onTitleChange={setTitle}
      onBodyChange={setBody}
    />
  )
}

describe('Composer', () => {
  it('offers only the palette its context allows', async () => {
    const user = userEvent.setup()
    const { unmount } = render(<Harness context="broadcast" />)
    await user.click(screen.getByRole('button', { name: /vložit údaj/i }))

    // A broadcast can only reference time — no event or metric tokens exist for it.
    expect(screen.getByText('Čas')).toBeInTheDocument()
    expect(screen.queryByText(/Úkoly v Právě dělám/)).not.toBeInTheDocument()
    expect(screen.queryByText('Kdo to udělal')).not.toBeInTheDocument()
    unmount()

    render(<Harness context="trigger" />)
    await user.click(screen.getByRole('button', { name: /vložit údaj/i }))
    expect(screen.getByText('Kdo to udělal')).toBeInTheDocument()
  })

  it('inserts a picked token into the body instead of making the admin type it', async () => {
    const user = userEvent.setup()
    render(<Harness />)

    await user.click(screen.getByRole('button', { name: /vložit údaj/i }))
    await user.click(screen.getByRole('button', { name: /Úkoly v Právě dělám/ }))

    const body = screen.getByLabelText('Text') as HTMLTextAreaElement
    expect(body.value).toBe('{{metric.todo.pravedelam_count}}')
  })

  it('marks a personal metric so the admin knows it differs per recipient', async () => {
    const user = userEvent.setup()
    render(<Harness />)
    await user.click(screen.getByRole('button', { name: /vložit údaj/i }))

    expect(screen.getByRole('button', { name: /Připnuté poznámky · osobní/ })).toBeInTheDocument()
  })

  it('resolves tokens to sample values in the preview — never raw {{…}}', async () => {
    render(<Harness initialBody="Právě dělám: {{metric.todo.pravedelam_count}} úkolů" />)

    const preview = screen.getByText(/Právě dělám: \d+ úkolů/)
    expect(preview).toBeInTheDocument()
    expect(preview.textContent).not.toContain('{{')
  })

  it('renders an unknown token as a placeholder rather than leaking it', () => {
    render(<Harness initialBody="Hodnota: {{metric.neexistuje}}" />)

    const preview = screen.getByText(/Hodnota: —/)
    expect(preview).toBeInTheDocument()
  })

  it('shows an unedited <pole> placeholder as the raw text the server would send', () => {
    // The server's substitution pattern excludes < and >, so `{{change.<pole>.old}}`
    // is not a token there — it would be delivered verbatim to a phone. The preview
    // must show that, not paper over it with a sample value (the save is refused
    // with a 422 naming <pole>, and the two have to agree).
    render(<Harness context="trigger" initialBody="Bylo: {{change.<pole>.old}}" />)

    // Matched in both the field and the preview — what matters is that the
    // preview is one of them, and that no sample value was substituted.
    expect(screen.getAllByText(/Bylo: \{\{change\.<pole>\.old\}\}/).length).toBeGreaterThan(1)
    expect(screen.queryByText(/Bylo: Vynést koš/)).not.toBeInTheDocument()
  })

  it('selects the <pole> placeholder on insert so the next keystroke names the field', async () => {
    const user = userEvent.setup()
    render(<Harness context="trigger" />)

    await user.click(screen.getByRole('button', { name: /vložit údaj/i }))
    await user.click(screen.getByRole('button', { name: 'Původní hodnota' }))

    const body = screen.getByLabelText('Text') as HTMLTextAreaElement
    expect(body.value).toBe('{{change.<pole>.old}}')
    // "{{change." is 9 characters; the selection covers "<pole>".
    expect(body.value.slice(body.selectionStart, body.selectionEnd)).toBe('<pole>')
  })

  it('offers module lists beside the numbers, and inserts one as a token', async () => {
    const user = userEvent.setup()
    render(<Harness />)

    await user.click(screen.getByRole('button', { name: /vložit údaj/i }))
    expect(screen.getByText('Seznamy z modulů')).toBeInTheDocument()

    // The metric of the same key carries the same Czech label, so the two chips
    // must not answer to one name — the group heading is not part of it.
    const chip = screen.getByRole('button', { name: 'Připomínky na dnešek (seznam)' })
    expect(screen.getByRole('button', { name: 'Připomínky na dnešek' })).not.toBe(chip)
    // The empty case is the one the sample preview cannot show, so it is on the chip.
    expect(chip).toHaveAttribute('title', expect.stringContaining('žádné připomínky'))

    await user.click(chip)
    const body = screen.getByLabelText('Text') as HTMLTextAreaElement
    expect(body.value).toBe('{{list.events.pripominky_today}}')
  })

  it('sends a list chip to the body even when the title had focus, then aims back at the title', async () => {
    const user = userEvent.setup()
    render(<Harness />)

    await user.click(screen.getByLabelText('Nadpis'))
    await user.click(screen.getByRole('button', { name: /vložit údaj/i }))
    await user.click(screen.getByRole('button', { name: 'Připomínky na dnešek (seznam)' }))

    // The server refuses a list in a title — bullets and newlines in a headline —
    // so the composer must not be able to put one there.
    expect((screen.getByLabelText('Nadpis') as HTMLInputElement).value).toBe('Dobré ráno')
    expect((screen.getByLabelText('Text') as HTMLTextAreaElement).value).toBe(
      '{{list.events.pripominky_today}}',
    )

    // The redirect is about that one token. A metric chip picked next belongs
    // where the admin was actually writing.
    await user.click(screen.getByRole('button', { name: 'Úkoly v Právě dělám' }))
    expect((screen.getByLabelText('Nadpis') as HTMLInputElement).value).toContain(
      '{{metric.todo.pravedelam_count}}',
    )
    expect((screen.getByLabelText('Text') as HTMLTextAreaElement).value).toBe(
      '{{list.events.pripominky_today}}',
    )
  })

  it('aims at the body once the admin types there after a redirected list chip', async () => {
    const user = userEvent.setup()
    render(<Harness />)

    await user.click(screen.getByLabelText('Nadpis'))
    await user.click(screen.getByRole('button', { name: /vložit údaj/i }))
    await user.click(screen.getByRole('button', { name: 'Připomínky na dnešek (seznam)' }))

    // The redirect left keyboard focus in the body; the admin keeps writing
    // there. Typing fires no focus event, so it is the change itself that must
    // retake the insert target — the next chip belongs where they are writing,
    // not in the title they abandoned.
    await user.keyboard(' a dál')
    await user.click(screen.getByRole('button', { name: 'Úkoly v Právě dělám' }))

    expect((screen.getByLabelText('Nadpis') as HTMLInputElement).value).toBe('Dobré ráno')
    expect((screen.getByLabelText('Text') as HTMLTextAreaElement).value).toContain(
      '{{metric.todo.pravedelam_count}}',
    )
  })

  it('previews a list as the bulleted block that will actually be sent', () => {
    render(<Harness initialBody={'Dnes:\n{{list.events.pripominky_today}}'} />)

    // One bullet per item — the preview has to show the shape, not a comma run.
    const preview = screen.getByText(/• Vynést koš/)
    expect(preview.textContent).toContain('• Zalít kytky')
    expect(preview.textContent).not.toContain('{{')
  })

  it('offers no list tokens outside a summary', async () => {
    const user = userEvent.setup()
    render(<Harness context="trigger" />)
    await user.click(screen.getByRole('button', { name: /vložit údaj/i }))

    expect(screen.queryByText('Seznamy z modulů')).not.toBeInTheDocument()
  })

  it('shows the default-body placeholder so a blank body does not read as broken', () => {
    render(
      <Composer
        context="trigger"
        catalog={catalog}
        title="Úkoly"
        body=""
        onTitleChange={vi.fn()}
        onBodyChange={vi.fn()}
        bodyPlaceholder="Necháš-li prázdné, pošle se popis akce z logu."
      />,
    )
    // The placeholder appears both on the field and in the preview, so an admin
    // who leaves the body empty still sees what will be sent.
    expect(screen.getAllByText(/pošle se popis akce z logu/).length).toBeGreaterThan(0)
  })
})
