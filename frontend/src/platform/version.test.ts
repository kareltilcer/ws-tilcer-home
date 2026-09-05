import { describe, expect, it } from 'vitest'
import { shortCommit, versionLabel } from './version'

// ⚠ THE MODULE-LEVEL LABEL IS NOT WHAT IS TESTED HERE, and cannot usefully be:
// APP_VERSION_LABEL is composed once at import time out of `import.meta.env`, which
// vitest fixes to a dev build. What can regress is the composition, and the two
// functions it is composed from are pure.

describe('shortCommit — a sha, or nothing at all', () => {
  it('cuts a full sha to the seven characters the label shows', () => {
    expect(shortCommit('a3f9c2e1b4d5678901234567890abcdef1234567')).toBe('a3f9c2e')
  })

  it('takes a sha that is already short', () => {
    expect(shortCommit('a3f9c2e')).toBe('a3f9c2e')
  })

  it('lower-cases, because a label is copied into a bug report and compared by eye', () => {
    expect(shortCommit('A3F9C2E')).toBe('a3f9c2e')
  })

  it('trims, because a build arg arrives through a shell', () => {
    expect(shortCommit('  a3f9c2e  ')).toBe('a3f9c2e')
  })

  // ⚠ THE FAILURES TO EXPECT ARE NOT MALFORMED SHAS, they are a build arg that never
  // got a value: an unexpanded variable, a branch name, an empty string. Each one
  // prints something that LOOKS like an answer, which is the thing this drops.
  it.each([
    ['undefined', undefined],
    ['empty', ''],
    ['whitespace', '   '],
    ['an unexpanded variable', '$SOURCE_COMMIT'],
    ['a branch name', 'main'],
    ['a quoted empty string', '""'],
    ['too short to be a sha', 'a3f9c'],
  ])('drops %s rather than printing it', (_name, value) => {
    expect(shortCommit(value)).toBe('')
  })
})

describe('versionLabel — the version alone is still a label', () => {
  it('joins the two halves the way the design writes them', () => {
    expect(versionLabel('v10.2', 'a3f9c2e')).toBe('v10.2 · a3f9c2e')
  })

  // A build with no commit arg still says WHICH RELEASE it is, which is most of the
  // value: the alternative is a blank line where the label should be.
  it('falls back to the version when the build was given no commit', () => {
    expect(versionLabel('v10.2', '')).toBe('v10.2')
  })
})
