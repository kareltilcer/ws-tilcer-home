import { describe, expect, it } from 'vitest'
import { ALL_EMOJI, EMOJI_CATEGORIES, MAX_ICON_RUNES, searchEmoji } from './emojiData'

describe('emojiData', () => {
  it('loads the full dataset grouped into 9 categories', () => {
    expect(EMOJI_CATEGORIES).toHaveLength(9)
    expect(ALL_EMOJI.length).toBeGreaterThan(1500)
    // Every category carries a Czech label and a nav glyph.
    for (const cat of EMOJI_CATEGORIES) {
      expect(cat.label).toMatch(/\p{L}/u)
      expect(cat.icon.length).toBeGreaterThan(0)
    }
  })

  it('offers no glyph the backend would truncate (foldericon.maxRunes)', () => {
    for (const item of ALL_EMOJI) {
      expect([...item.char].length).toBeLessThanOrEqual(MAX_ICON_RUNES)
    }
  })

  it('finds emoji by English name', () => {
    const chars = searchEmoji('cat').map((e) => e.char)
    expect(chars).toContain('🐱')
    expect(searchEmoji('house').map((e) => e.char)).toContain('🏠')
  })

  it('finds emoji by Czech alias', () => {
    expect(searchEmoji('srdce').map((e) => e.char)).toContain('❤️')
    expect(searchEmoji('auto').map((e) => e.char)).toContain('🚗')
    expect(searchEmoji('zdravi').map((e) => e.char)).toContain('🩺')
  })

  it('folds diacritics in the query', () => {
    // "kůň" → "kun" matches the horse's Czech alias.
    expect(searchEmoji('kůň').map((e) => e.char)).toContain('🐴')
  })

  it('requires every token to match (AND semantics)', () => {
    const chars = searchEmoji('cerne srdce').map((e) => e.char)
    expect(chars).toContain('🖤')
    expect(chars).not.toContain('❤️')
  })

  it('returns everything for an empty query and nothing for gibberish', () => {
    // A fresh copy (not the live ALL_EMOJI reference) with the same contents.
    const all = searchEmoji('   ')
    expect(all).not.toBe(ALL_EMOJI)
    expect(all).toEqual(ALL_EMOJI)
    expect(searchEmoji('zzqxnotarealthing')).toHaveLength(0)
  })
})
