// The token list's derivations — v11 (HANDOFF-design §v11 items 5 and 6).
//
// ⚠ ONE FILE FOR BOTH SCREENS, and that is the point rather than a convenience.
// Nastavení → Asistenti shows a member their own tokens; Administrace → Asistenti
// → Tokeny shows an admin everybody's. The COLUMNS differ (the admin one adds an
// owner, a created date and a last IP) and not one rule behind them does: the same
// four states, the same sort, the same idea of what "expiring soon" means. Two
// copies would disagree within a version.
//
// ⚠ AND THEY ARE PURE FUNCTIONS OVER A CLOCK PASSED IN. "Vyprší brzy" is a
// comparison against now, which is the one thing a test cannot assert about a
// component that reads `new Date()` itself.

import { fmtDate, fmtDateTime, sinceLabel } from '@/i18n/format'
import { count, czPlural, PLURAL } from '@/i18n/plural'
import { cs } from '@/i18n/cs'
import type { McpModule, McpToken } from '@/api/types'

/** Tokens within this many days of expiry are marked *vyprší brzy*. */
export const EXPIRING_SOON_DAYS = 14

/**
 * The four states a token can be in.
 *
 * ⚠ AND THEY ARE NOT FOUR COLOURS. `soon` is deliberately NOT the attention
 * register and never the danger family: an expiry a member chose for themselves is
 * not a problem, it is the thing working — §v9's rule that *private is not a
 * warning*, applied to *expiring*. `unused` is not an error either; it is a useful
 * fact, and usually means the config never landed.
 */
export type TokenState = 'active' | 'unused' | 'soon' | 'expired' | 'revoked'

export function tokenState(t: McpToken, now: Date = new Date()): TokenState {
  if (t.revoked_at) return 'revoked'
  if (t.expires_at) {
    const expires = new Date(t.expires_at).getTime()
    if (expires <= now.getTime()) return 'expired'
    if (expires - now.getTime() <= EXPIRING_SOON_DAYS * 86_400_000) return 'soon'
  }
  if (!t.last_used_at) return 'unused'
  return 'active'
}

/** A dead token is revoked or expired: kept in the list, dimmed, never removed. */
export function isDead(state: TokenState): boolean {
  return state === 'revoked' || state === 'expired'
}

/**
 * sortTokens orders by `last_used_at` descending, **nulls last**.
 *
 * ⚠ NOT BY CREATION DATE, WHICH IS THE ORDER THE API RETURNS. The row a member is
 * looking for is almost always the one that has just done something — or the one
 * that has never done anything at all, which is why the never-used tokens go to
 * the bottom as a group rather than being scattered by age. Ties among them fall
 * back to the name, in Czech collation, so the order is stable across renders.
 */
export function sortTokens<T extends McpToken>(tokens: readonly T[]): T[] {
  return [...tokens].sort((a, b) => {
    if (!a.last_used_at && !b.last_used_at) return a.name.localeCompare(b.name, 'cs')
    if (!a.last_used_at) return 1
    if (!b.last_used_at) return -1
    return b.last_used_at.localeCompare(a.last_used_at)
  })
}

/**
 * scopeSummary renders the *Rozsah* cell: *Všechny moduly*, or a count whose
 * `title` names them.
 *
 * ⚠ AN EMPTY LIST MEANS EVERYTHING, not nothing (D288). It is the field's most
 * common value and the one most easily read backwards, so it gets words rather
 * than "0 modulů".
 */
export function scopeSummary(modules: readonly McpModule[]): { label: string; title: string } {
  if (modules.length === 0) return { label: cs.mcp.scopeAll, title: cs.mcp.scopeAllTitle }
  return {
    label: count(modules.length, PLURAL.modules),
    title: modules.map(moduleLabel).join(' · '),
  }
}

/** moduleLabel is the screen's name for a wire module — "Okno", never "events". */
export function moduleLabel(m: McpModule): string {
  return cs.mcp.modules[m] ?? m
}

/** expiryLabel is a date, or *bez omezení*. */
export function expiryLabel(t: McpToken): string {
  return t.expires_at ? fmtDate(new Date(t.expires_at)) : cs.mcp.expiryNone
}

/**
 * lastUsedLabel is the most important cell on either screen.
 *
 * ⚠ RELATIVE IN THE CELL, ABSOLUTE IN THE TITLE. `last_used_at` is accurate to
 * within an hour rather than to the call (D286), so a clock time would claim a
 * precision the column does not have — and the question this cell answers is *"do
 * I still need this?"*, which *před 3 dny* answers and `14:07` does not.
 */
export function lastUsedLabel(t: McpToken): { label: string; title: string; never: boolean } {
  if (!t.last_used_at) {
    return { label: cs.mcp.lastUsedNever, title: cs.mcp.lastUsedNeverTitle, never: true }
  }
  return { label: sinceLabel(t.last_used_at), title: fmtDateTime(t.last_used_at), never: false }
}

/**
 * markLabel is the row's state marker, or null when there is nothing to say.
 *
 * ⚠ `active` AND `unused` BOTH RETURN NULL, and `unused` is the interesting one:
 * the *naposledy použito* column already says *nepoužito* in its own words, and a
 * chip repeating it puts the same word twice in one row — which at 375 px costs a
 * line for nothing. The state is still rendered; it is rendered where it belongs.
 */
export function markLabel(t: McpToken, state: TokenState): string | null {
  switch (state) {
    case 'revoked':
      return cs.mcp.markRevoked(t.revoked_at ? fmtDate(new Date(t.revoked_at)) : '')
    case 'expired':
      return cs.mcp.markExpired(t.expires_at ? fmtDate(new Date(t.expires_at)) : '')
    case 'soon':
      return cs.mcp.markSoon
    default:
      return null
  }
}

/** tokenCountLabel renders "3 tokeny" for a list header. */
export function tokenCountLabel(n: number): string {
  return `${n} ${czPlural(n, PLURAL.tokens)}`
}
