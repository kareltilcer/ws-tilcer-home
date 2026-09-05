import { describe, expect, it } from 'vitest'
import changelog from '../../../handoff/v10/CHANGELOG.md?raw'
import { version as packageVersion } from '../../package.json'
import { APP_VERSION, shortCommit, versionLabel } from './version'

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
    expect(versionLabel('v1.10.2', 'a3f9c2e')).toBe('v1.10.2 · a3f9c2e')
  })

  // A build with no commit arg still says WHICH RELEASE it is, which is most of the
  // value: the alternative is a blank line where the label should be.
  it('falls back to the version when the build was given no commit', () => {
    expect(versionLabel('v1.10.2', '')).toBe('v1.10.2')
  })
})

// ⚠ THE ONE THING version.ts CALLS AN INVARIANT WAS THE ONE THING NOTHING READ BACK.
// Its comment says APP_VERSION is bumped with the CHANGELOG's newest heading and that
// "a label that disagrees with the changelog is worse than no label at all, because it
// is the string a bug report gets filed under" — which is a rule a release forgets in
// exactly the way nav.test.ts exists to catch for the nav order. Same idiom: read the
// other file and assert against it, so the release that forgets fails here rather than
// on a screenshot from Karel six weeks later.
//
// ⚠ THE CHAIN IS TWO HOPS LONG SINCE D274, and each one can break on its own: the label
// is `package.json`'s version, and `package.json`'s version is the CHANGELOG's newest
// release. The second hop is NOT string equality — npm demands three-part semver and
// the releases are numbered `v10`, `v10.1`, `v10.2`, so the release lives in
// `minor.patch` and the major is a constant `1` carrying no meaning. `1.10.2` is v10.2;
// `1.11.0` is v11. That mapping is exactly the kind of rule a release applies wrongly
// at 11pm, which is why it is asserted rather than written down.
describe('APP_VERSION agrees with the CHANGELOG it is bumped with', () => {
  it('prints the package version verbatim, under the v the design writes', () => {
    expect(APP_VERSION).toBe(`v${packageVersion}`)
  })

  it('is three-part semver, because that is the whole reason for the leading 1', () => {
    expect(packageVersion).toMatch(/^\d+\.\d+\.\d+$/)
  })

  it('carries the newest CHANGELOG release in minor.patch', () => {
    const newest = changelog.match(/^## v(\d+)(?:\.(\d+))?/m)
    expect(newest, 'no "## vX[.Y]" heading found in handoff/v10/CHANGELOG.md').not.toBeNull()

    const [, minor, patch] = packageVersion.split('.')
    expect(minor, `package.json minor should be the CHANGELOG's ${newest?.[0]}`).toBe(
      newest?.[1],
    )
    // A heading with no second number is a `.0` release: `## v11` ⇔ `1.11.0`.
    expect(patch).toBe(newest?.[2] ?? '0')
  })
})
