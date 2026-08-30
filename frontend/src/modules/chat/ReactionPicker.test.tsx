import { useRef, useState } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ReactionPicker } from './ThreadView'
import { HEART } from './reactions'
import { cs } from '@/i18n/cs'
import type { ChatMessage } from './api/types'

/**
 * The picker bar's FOCUS CONTRACT (v10.1).
 *
 * ⚠ THIS IS THE HALF OF THE OVERLAY THAT CAN BE TESTED, and the half the bugs were
 * in. Moving the bar out of the bubble put the whole footer between the ☺ and the
 * palette it opens, so the bar has to take focus on open and give it back on close —
 * three branches of behaviour written in plain DOM, which jsdom does implement.
 * Stacking contexts, containing blocks and `scrollIntoView` are the other half and
 * are not asserted here: there is no layout engine under these tests to see them, and
 * a test that pretended otherwise would be measuring zeroes.
 *
 * ⚠ AND IT IS `user-event`, NOT `fireEvent`, ON PURPOSE. Focus-on-press is a DEFAULT
 * ACTION of mousedown — the scrim suppresses it, which is the whole of the fix that
 * keeps a tap outside from dropping focus on `<body>`. `fireEvent.click` does not
 * model default actions at all, so it would pass with the fix reverted and guard
 * nothing.
 */

const message: ChatMessage = {
  id: 'm1',
  conversation_id: 'c1',
  author_id: 'u1',
  author_label: 'Karel',
  body: 'ok',
  attachments: [],
  reactions: [],
  created_at: '2026-08-30T07:41:00.000Z',
  edited_at: null,
  deleted: false,
}

/** The wiring `LiveBubble` gives it: the row owns the open state and the ☺'s ref. */
function Harness({ onReact = vi.fn() }: { onReact?: (emoji: string, reacted: boolean) => void }) {
  const [picking, setPicking] = useState(false)
  const smile = useRef<HTMLButtonElement>(null)
  return (
    <div className="relative flex">
      {/* Stands in for the footer verbs the bar is no longer adjacent to. */}
      <button type="button">{cs.chat.word.reply}</button>
      <button ref={smile} type="button" onClick={() => setPicking((v) => !v)}>
        {cs.chat.reactionAdd}
      </button>
      {picking && (
        <ReactionPicker
          id="picker-1"
          message={message}
          me="u1"
          mine
          trigger={smile}
          onPicking={setPicking}
          onReact={onReact}
        />
      )}
    </div>
  )
}

const smile = () => screen.getByRole('button', { name: cs.chat.reactionAdd })
const bar = () => screen.queryByRole('group', { name: cs.chat.reactionPickerLabel })
/** Not reachable by role: it is `aria-hidden`, which is the point of it. */
const scrim = () => document.querySelector<HTMLButtonElement>('button[aria-hidden="true"]')

describe('ReactionPicker focus contract', () => {
  afterEach(cleanup)

  it('takes focus on open and hands it back to the ☺ on close', async () => {
    const user = userEvent.setup()
    render(<Harness />)

    await user.click(smile())
    expect(bar()).toBeInTheDocument()
    expect(document.activeElement).toBe(screen.getByRole('button', { name: HEART }))

    await user.click(screen.getByRole('button', { name: cs.chat.reactionPickerClose }))
    expect(bar()).not.toBeInTheDocument()
    expect(document.activeElement).toBe(smile())
  })

  it('hands focus back after a dismiss on the scrim, rather than dropping it on the body', async () => {
    const user = userEvent.setup()
    render(<Harness />)
    await user.click(smile())

    const out = scrim()
    expect(out).not.toBeNull()
    await user.click(out as HTMLButtonElement)

    expect(bar()).not.toBeInTheDocument()
    // ⚠ THE REGRESSION THIS FILE EXISTS FOR. The scrim is the bar's SIBLING, so if a
    // press focuses it the restore's `bar.contains(activeElement)` is false and the
    // ☺ never gets its focus back — the way out the scrim was added to provide was
    // the one way out that lost the member's place.
    expect(document.activeElement).toBe(smile())
    expect(document.activeElement).not.toBe(document.body)
  })

  it('refuses the focus a press would otherwise put on the aria-hidden scrim', async () => {
    const user = userEvent.setup()
    render(<Harness />)
    await user.click(smile())

    const heart = screen.getByRole('button', { name: HEART })
    await user.pointer({ keys: '[MouseLeft>]', target: scrim() as HTMLButtonElement })

    // Pressed but not released: focus has not moved off the emoji the bar gave it to,
    // and is not sitting on a node screen readers have been told to ignore.
    expect(document.activeElement).toBe(heart)
  })

  it('closes on Escape', async () => {
    const user = userEvent.setup()
    render(<Harness />)
    await user.click(smile())
    expect(bar()).toBeInTheDocument()

    await user.keyboard('{Escape}')
    expect(bar()).not.toBeInTheDocument()
  })

  it('leaves focus where the member deliberately moved it', async () => {
    const user = userEvent.setup()
    render(<Harness />)
    await user.click(smile())

    const reply = screen.getByRole('button', { name: cs.chat.word.reply })
    reply.focus()
    // Escape rather than a click, so the close itself moves nothing.
    await user.keyboard('{Escape}')

    expect(bar()).not.toBeInTheDocument()
    expect(document.activeElement).toBe(reply)
  })

  it('reports the picked emoji and closes', async () => {
    const onReact = vi.fn()
    const user = userEvent.setup()
    render(<Harness onReact={onReact} />)
    await user.click(smile())

    await user.click(screen.getByRole('button', { name: HEART }))

    expect(onReact).toHaveBeenCalledWith(HEART, true)
    expect(bar()).not.toBeInTheDocument()
    expect(document.activeElement).toBe(smile())
  })
})
