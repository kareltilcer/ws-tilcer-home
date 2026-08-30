import { describe, expect, it } from 'vitest'
import { applyReaction } from './api/hooks'
import type { ChatMessage, Reaction } from './api/types'
import { hasReacted, HEART, isMine, reactionLabel, REACTION_PALETTE } from './reactions'

// Reactions (v10.1, D265) — the two pure pieces: what a chip means to the caller,
// and what the optimistic patch does before the server answers.
//
// ⚠ AN OPTIMISTIC UPDATE THAT IS WRONG IS WORSE THAN NONE. It puts a chip on screen
// that the next frame takes away, which reads as the app losing the tap it just
// acknowledged — in the one module where "did that go through" is the whole
// question the interface exists to answer.

const ME = { user_id: 'u-kaja', label: 'Kája' }
const THEM = { user_id: 'u-andy', label: 'Andy' }

function message(reactions: Reaction[]): ChatMessage {
  return {
    id: 'm-1',
    conversation_id: 'c-1',
    author_id: THEM.user_id,
    author_label: THEM.label,
    body: 'Klíče jsou pod květináčem.',
    attachments: [],
    reactions,
    created_at: '2026-08-29T10:00:00.000Z',
    edited_at: null,
    deleted: false,
  }
}

describe('the palette', () => {
  it('is the seven the server enforces', () => {
    expect(REACTION_PALETTE).toEqual(['❤️', '👍', '😂', '😮', '😢', '🙏', '✅'])
  })

  // ⚠ ❤️ IS U+2764 U+FE0F AND THE VARIATION SELECTOR IS PART OF THE VALUE. The
  // server compares byte-for-byte, so a "tidied" bare ❤ would be refused with 422 —
  // and if one were ever stored, no chip built from this constant could match it,
  // which means nobody could ever remove it.
  it('keeps the heart at two code points', () => {
    expect([...HEART]).toHaveLength(2)
    expect(HEART.codePointAt(1)).toBe(0xfe0f)
  })
})

describe('whose chip is it', () => {
  const chip: Reaction = { emoji: HEART, by: [THEM, ME] }

  it('is mine when I am among the reactors', () => {
    expect(isMine(chip, ME.user_id)).toBe(true)
    expect(isMine(chip, 'u-somebody-else')).toBe(false)
  })

  it('answers hasReacted per emoji, not per message', () => {
    const m = message([chip, { emoji: '👍', by: [THEM] }])
    expect(hasReacted(m.reactions, HEART, ME.user_id)).toBe(true)
    expect(hasReacted(m.reactions, '👍', ME.user_id)).toBe(false)
    expect(hasReacted(m.reactions, '😂', ME.user_id)).toBe(false)
  })

  // The design puts who reacted under the cursor rather than in the row — and the
  // same string is the accessible name, because a `title` is not one.
  it('names the reactors with the caller as "vy"', () => {
    expect(reactionLabel(chip, ME.user_id)).toBe(`${HEART} Andy, vy`)
    expect(reactionLabel(chip, 'u-nobody')).toBe(`${HEART} Andy, Kája`)
  })
})

describe('the optimistic patch', () => {
  it('adds me to an existing chip', () => {
    const out = applyReaction(message([{ emoji: HEART, by: [THEM] }]), HEART, true, ME)
    expect(out.reactions).toHaveLength(1)
    expect(out.reactions[0].by).toEqual([THEM, ME])
  })

  it('creates a chip that did not exist', () => {
    const out = applyReaction(message([]), '👍', true, ME)
    expect(out.reactions).toEqual([{ emoji: '👍', by: [ME] }])
  })

  // ⚠ THE DOUBLE TAP IS WHY. A gesture fires twice far more easily than a button
  // does, and a patch that pushed the member under the same chip twice would render
  // "❤️ 2" from one person until the server's answer corrected it.
  it('is idempotent when I am already there', () => {
    const before = message([{ emoji: HEART, by: [ME] }])
    const after = applyReaction(before, HEART, true, ME)
    expect(after.reactions[0].by).toEqual([ME])
  })

  it('removes me and leaves the others', () => {
    const out = applyReaction(message([{ emoji: HEART, by: [THEM, ME] }]), HEART, false, ME)
    expect(out.reactions[0].by).toEqual([THEM])
  })

  // A chip nobody is left holding stops existing, rather than reading zero.
  it('drops a chip whose last reactor left it', () => {
    const out = applyReaction(message([{ emoji: HEART, by: [ME] }]), HEART, false, ME)
    expect(out.reactions).toEqual([])
  })

  it('never touches the other emoji on the same message', () => {
    const before = message([
      { emoji: HEART, by: [ME] },
      { emoji: '🙏', by: [THEM] },
    ])
    const out = applyReaction(before, HEART, false, ME)
    expect(out.reactions).toEqual([{ emoji: '🙏', by: [THEM] }])
  })

  // ⚠ IT DOES NOT MUTATE. The cached message is the object every other observer of
  // this thread is holding; patching it in place would move other bubbles' state
  // without a re-render and make the rollback restore an object that had already
  // been changed underneath it.
  it('leaves the message it was given alone', () => {
    const before = message([{ emoji: HEART, by: [THEM] }])
    const snapshot = JSON.stringify(before)
    applyReaction(before, HEART, true, ME)
    expect(JSON.stringify(before)).toBe(snapshot)
  })
})
