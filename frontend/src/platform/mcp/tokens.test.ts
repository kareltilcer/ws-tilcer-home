import { describe, expect, it } from 'vitest'
import { qk } from '@/api/keys'
import { mayPersistKey } from '@/platform/pwa/persist'
import type { McpToken } from '@/api/types'
import {
  EXPIRING_SOON_DAYS,
  expiryLabel,
  isDead,
  lastUsedLabel,
  markLabel,
  scopeSummary,
  sortTokens,
  tokenState,
} from './tokens'

// v11 — the token list's rules, asserted where they are decisions rather than
// rendering (PRD §V11-6, HANDOFF-design §v11 items 5 and 6).

const NOW = new Date('2026-09-06T14:20:00Z')

function token(over: Partial<McpToken> = {}): McpToken {
  return {
    id: 't-1',
    name: 'Claude na notebooku',
    prefix: 'hmcp_9tK',
    modules: [],
    created_at: '2026-08-01T09:00:00Z',
    expires_at: null,
    last_used_at: '2026-09-06T08:00:00Z',
    last_used_ip: '192.168.1.24',
    revoked_at: null,
    ...over,
  }
}

function daysFromNow(n: number): string {
  return new Date(NOW.getTime() + n * 86_400_000).toISOString()
}

describe('the four states, and they are not four colours', () => {
  it('names each one', () => {
    expect(tokenState(token(), NOW)).toBe('active')
    expect(tokenState(token({ last_used_at: null }), NOW)).toBe('unused')
    expect(tokenState(token({ expires_at: daysFromNow(3) }), NOW)).toBe('soon')
    expect(tokenState(token({ expires_at: daysFromNow(-1) }), NOW)).toBe('expired')
    expect(tokenState(token({ revoked_at: daysFromNow(-2) }), NOW)).toBe('revoked')
  })

  // ⚠ A REVOKED TOKEN THAT HAS ALSO EXPIRED READS AS REVOKED. Revocation is
  // something a person DID; expiry is something that happened. The row is a
  // record of the household's own actions first.
  it('prefers revoked over expired when a token is both', () => {
    expect(tokenState(token({ revoked_at: daysFromNow(-5), expires_at: daysFromNow(-1) }), NOW)).toBe('revoked')
  })

  it('draws the soon boundary at fourteen days', () => {
    expect(tokenState(token({ expires_at: daysFromNow(EXPIRING_SOON_DAYS - 1) }), NOW)).toBe('soon')
    expect(tokenState(token({ expires_at: daysFromNow(EXPIRING_SOON_DAYS + 1) }), NOW)).toBe('active')
  })

  // ⚠ AN EXPIRY IN THE FUTURE DOES NOT MAKE A NEVER-USED TOKEN "ACTIVE". The
  // never-used state is the useful one — it usually means the config never landed
  // — and it must survive a token that also happens to be expiring.
  it('still reports a never-used token as unused', () => {
    expect(tokenState(token({ last_used_at: null, expires_at: daysFromNow(300) }), NOW)).toBe('unused')
  })

  it('counts revoked and expired as dead, and nothing else', () => {
    expect(isDead('revoked')).toBe(true)
    expect(isDead('expired')).toBe(true)
    expect(isDead('soon')).toBe(false)
    expect(isDead('unused')).toBe(false)
    expect(isDead('active')).toBe(false)
  })
})

describe('sorting: last used descending, nulls last', () => {
  // ⚠ NOT THE ORDER THE API RETURNS, which is newest-created first. The row a
  // member is looking for is almost always the one that has just done something —
  // or the one that has never done anything at all.
  it('puts the most recently used first and the never-used at the bottom', () => {
    const rows = sortTokens([
      token({ id: 'never', name: 'Záloha', last_used_at: null }),
      token({ id: 'old', last_used_at: '2026-01-01T00:00:00Z' }),
      token({ id: 'fresh', last_used_at: '2026-09-06T08:00:00Z' }),
    ])
    expect(rows.map((t) => t.id)).toEqual(['fresh', 'old', 'never'])
  })

  it('orders the never-used ones by name, in Czech', () => {
    const rows = sortTokens([
      token({ id: 'z', name: 'Židle', last_used_at: null }),
      token({ id: 'c', name: 'Čtečka', last_used_at: null }),
      token({ id: 'a', name: 'Asistent', last_used_at: null }),
    ])
    expect(rows.map((t) => t.id)).toEqual(['a', 'c', 'z'])
  })

  it('does not mutate its input', () => {
    const input = [token({ id: 'a', last_used_at: null }), token({ id: 'b' })]
    sortTokens(input)
    expect(input.map((t) => t.id)).toEqual(['a', 'b'])
  })
})

describe('the scope cell', () => {
  // ⚠ AN EMPTY LIST MEANS EVERYTHING, NOT NOTHING (D288). It is the field's most
  // common value and the one most easily read backwards, so it gets words rather
  // than "0 modulů".
  it('reads an empty scope as every module', () => {
    expect(scopeSummary([]).label).toBe('Všechny moduly')
  })

  it('counts a narrowed scope and names the modules in its title', () => {
    const summary = scopeSummary(['todo', 'events', 'garden'])
    expect(summary.label).toBe('3 moduly')
    // ⚠ The SCREEN's names, not the wire's: a member picks "Okno", never "events".
    expect(summary.title).toBe('Úkoly · Okno · Zahrada')
  })
})

describe('the cells that carry a state', () => {
  it('says bez omezení rather than an empty date', () => {
    expect(expiryLabel(token({ expires_at: null }))).toBe('bez omezení')
    expect(expiryLabel(token({ expires_at: '2026-12-24T00:00:00Z' }))).toBe('24. 12. 2026')
  })

  it('renders a never-used token as a fact rather than a blank', () => {
    const last = lastUsedLabel(token({ last_used_at: null }))
    expect(last.never).toBe(true)
    expect(last.label).toBe('nepoužito')
    // The title is where the reason lives: it usually means the config never landed.
    expect(last.title).toContain('konfigurace')
  })

  // ⚠ RELATIVE IN THE CELL, ABSOLUTE IN THE TITLE. `last_used_at` is accurate to
  // within an hour rather than to the call (D286), so a clock time in the cell
  // would claim a precision the column does not have.
  it('keeps the exact stamp in the title and a relative label in the cell', () => {
    const last = lastUsedLabel(token({ last_used_at: '2026-08-01T09:30:00Z' }))
    expect(last.label.startsWith('před ')).toBe(true)
    expect(last.title).toContain('2026')
  })

  // ⚠ `unused` HAS NO MARK, and that is the interesting case. The naposledy
  // použito column already says "nepoužito" in its own words; a chip repeating it
  // would put the same word twice in one row, which at 375 px costs a line for
  // nothing.
  it('marks only the states the columns do not already carry', () => {
    expect(markLabel(token(), 'active')).toBeNull()
    expect(markLabel(token({ last_used_at: null }), 'unused')).toBeNull()
    expect(markLabel(token({ expires_at: daysFromNow(3) }), 'soon')).toBe('vyprší brzy')
    expect(markLabel(token({ expires_at: '2026-09-01T00:00:00Z' }), 'expired')).toBe('vypršel 1. 9. 2026')
    expect(markLabel(token({ revoked_at: '2026-09-02T00:00:00Z' }), 'revoked')).toBe('odvolán 2. 9. 2026')
  })
})

// ⚠ NEITHER TOKEN LIST MAY REACH THE DISK (v11). Neither is secret — no read route
// in the contract returns a secret or a hash — but a token list answers "who in
// this household has connected an assistant, and when did it last do something",
// and the admin one carries every member's. That is v9's purge-listing reasoning
// applied to credentials, on the same shared kitchen laptop.
describe('the token lists are excluded from the PWA persister', () => {
  it('refuses both keys', () => {
    expect(mayPersistKey(qk.mcpTokens)).toBe(false)
    expect(mayPersistKey(qk.adminMcpTokens)).toBe(false)
  })

  // The whole `mcp` prefix, matching the chat rule: a key added later must not
  // reach disk because nobody remembered to extend a list.
  it('refuses an mcp key that does not exist yet', () => {
    expect(mayPersistKey(['mcp', 'usage', 't-1'])).toBe(false)
  })

  // The counterpart — without it a persister that refused everything would pass.
  it('still persists the modules meant to work offline', () => {
    expect(mayPersistKey(qk.dashboard)).toBe(true)
    expect(mayPersistKey(qk.adminStorage)).toBe(true)
  })
})

// ⚠ THE HAZARD IS SEGMENT NESTING, NOT A STRING PREFIX (the v10 lesson, §V10-12).
// If the member's own key were a prefix of the admin one, revoking a personal
// token would invalidate every member's list — a request a non-admin answers with
// a 403 the screen has no way to explain.
describe('the two token keys are siblings, not nested', () => {
  it('leaves neither key a prefix of the other', () => {
    const mine = qk.mcpTokens as readonly unknown[]
    const admin = qk.adminMcpTokens as readonly unknown[]
    const isPrefix = (a: readonly unknown[], b: readonly unknown[]) =>
      a.length <= b.length && a.every((segment, i) => segment === b[i])
    expect(isPrefix(mine, admin)).toBe(false)
    expect(isPrefix(admin, mine)).toBe(false)
  })
})
