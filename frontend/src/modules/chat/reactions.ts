import { cs } from '@/i18n/cs'
import type { Reaction, ReactionActor } from './api/types'

/**
 * The seven emoji (v10.1, D265), in the order the chips and the picker draw them.
 *
 * ⚠ IT IS THE SAME SEVEN THE SERVER ENFORCES, and it is duplicated here on purpose
 * rather than fetched: the picker has to draw before any request, and a palette
 * that arrived over the wire would make the bar empty on a cold load. The server is
 * the authority — an emoji absent from ITS list is a 422 — so the failure mode of
 * the two drifting is a control that is refused, not a value that gets stored.
 *
 * ⚠ ❤️ IS TWO CODE POINTS (U+2764 U+FE0F). The variation selector is part of the
 * value the server compares byte-for-byte, so this string must never be "tidied" to
 * a bare ❤ — a chip written that way could never be matched, and therefore never
 * removed.
 */
export const REACTION_PALETTE = ['❤️', '👍', '😂', '😮', '😢', '🙏', '✅'] as const

/** The double tap's emoji. Named, because two files reach for it. */
export const HEART = REACTION_PALETTE[0]

/** Whether this chip is one the caller has left. */
export function isMine(reaction: Reaction, me: string): boolean {
  return reaction.by.some((a) => a.user_id === me)
}

/** Whether the caller has already reacted to this message with `emoji`. */
export function hasReacted(reactions: Reaction[], emoji: string, me: string): boolean {
  const chip = reactions.find((r) => r.emoji === emoji)
  return !!chip && isMine(chip, me)
}

/**
 * The chip's tooltip and its accessible name: who reacted, with the caller as *vy*.
 *
 * ⚠ WHO REACTED IS UNDER THE CURSOR, NOT IN THE ROW — the design's rule. A row of
 * names beside every chip is unreadable at 375 px in a room of six, and the count is
 * what the eye is actually scanning for.
 *
 * ⚠ AND IT IS THE ACCESSIBLE NAME TOO, because a `title` is not one. The chip
 * renders an emoji and a numeral, so without this a screen reader announces
 * "❤️ 3, button" and the three people are unreachable.
 */
export function reactionLabel(reaction: Reaction, me: string): string {
  const names = reaction.by.map((a: ReactionActor) => (a.user_id === me ? cs.chat.membersYou : a.label))
  return `${reaction.emoji} ${names.join(', ')}`
}
