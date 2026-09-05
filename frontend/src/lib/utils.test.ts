import { describe, expect, it } from 'vitest'
import { initial } from './utils'

// v10.2 moved `initial` out of chat's MembersPanel and into the shared module, because
// the side nav's user row draws the same mark. A helper with two callers and a stated
// invariant is a helper that needs the invariant written down as an assertion.
describe('initial — a person’s mark', () => {
  it('takes the first character, upper-cased', () => {
    expect(initial('karel')).toBe('K')
  })

  it('keeps Czech diacritics rather than folding them', () => {
    expect(initial('Šárka')).toBe('Š')
  })

  it('ignores leading whitespace, which a display name is free to carry', () => {
    expect(initial('  marie')).toBe('M')
  })

  // ⚠ THE REASON THIS SPREADS RATHER THAN INDEXES. `name[0]` takes one UTF-16 code
  // unit, so it cuts a surrogate pair in half and renders U+FFFD — the one way a
  // single character can be wrong. An emoji is the case that reaches it in practice:
  // display names come from auth and nothing there restricts the alphabet.
  it('does not cut a surrogate pair in half', () => {
    expect(initial('🌻 Zahrada')).toBe('🌻')
    expect(initial('🌻 Zahrada')).not.toContain('�')
  })

  // The empty case is a rendered empty circle, not a crash: `identity.label` is
  // `display_name || email` and cannot realistically be blank, but the avatar span is
  // sized by its classes and survives having nothing in it.
  it('returns nothing for a blank name rather than throwing', () => {
    expect(initial('')).toBe('')
    expect(initial('   ')).toBe('')
  })
})
