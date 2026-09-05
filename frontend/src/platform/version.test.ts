import { describe, expect, it } from 'vitest'
import changelog from '../../../handoff/v10/CHANGELOG.md?raw'
import lockfileRaw from '../../package-lock.json?raw'
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

  it('carries the newest CHANGELOG release in minor.patch, under a constant major', () => {
    const newest = changelog.match(/^## v(\d+)(?:\.(\d+))?(\.\d+)?/m)
    expect(newest, 'no "## vX[.Y]" heading found in handoff/v10/CHANGELOG.md').not.toBeNull()
    // ⚠ A THIRD COMPONENT IS REFUSED, NOT DROPPED. `minor.patch` has nowhere to put it,
    // so a `## v10.2.1` heading would be satisfied by `1.10.2` — a label naming a release
    // that was never cut, which is the one thing this whole chain exists to prevent.
    expect(newest?.[3], 'a "## vX.Y.Z" heading has nowhere to go in minor.patch').toBeUndefined()

    const [major, minor, patch] = packageVersion.split('.')
    // ⚠ THE MAJOR IS ASSERTED TOO, and it is the half a release gets wrong: `2.10.2` is
    // three-part semver whose minor.patch still matches this heading, and it prints
    // `v2.10.2` in the side nav. The `1` is npm's demand for a third number and nothing
    // else — it carries no release meaning and it never moves.
    expect(major, 'the major is a constant 1, and no release ever bumps it').toBe('1')
    expect(minor, `package.json minor should be the CHANGELOG's ${newest?.[0]}`).toBe(
      newest?.[1],
    )
    // A heading with no second number is a `.0` release: `## v11` ⇔ `1.11.0`.
    expect(patch).toBe(newest?.[2] ?? '0')
  })

  // ⚠ `match` TAKES THE FIRST HEADING, NOT THE NEWEST, and those are the same fact only
  // while this file stays newest-first. Nothing else in the repo enforces that order, so
  // a reordered CHANGELOG would leave the assertion above reading an OLDER entry and
  // saying nothing about it. Asserted rather than assumed, for the same reason the
  // mapping is.
  it('reads the newest release, because the CHANGELOG is newest-first', () => {
    const releases = [...changelog.matchAll(/^## v(\d+)(?:\.(\d+))?/gm)].map((m) => [
      Number(m[1]),
      Number(m[2] ?? '0'),
    ])
    expect(
      releases.length,
      'no "## vX[.Y]" heading found in handoff/v10/CHANGELOG.md',
    ).toBeGreaterThan(0)
    const highest = [...releases].sort((a, b) => b[0] - a[0] || b[1] - a[1])[0]
    expect(
      releases[0],
      'the first "## vX[.Y]" heading is not the newest release the file names',
    ).toEqual(highest)
  })

  // ⚠ THE LOCKFILE IS THE THIRD FILE CARRYING THIS NUMBER, and it is the one with no
  // reader: D274 measured that `npm ci` ignores the lock's root `version` outright, so
  // a release that bumps the manifest and skips `npm install --package-lock-only` gets
  // a green suite, a green build and a correct label — and leaves the stale line to
  // surface in somebody else's diff about something else. That is the whole reason the
  // lock moved with the manifest, so it is asserted instead of written down.
  it('was bumped with package-lock.json, which nothing in npm checks', () => {
    const lock = JSON.parse(lockfileRaw) as {
      version?: string
      packages?: Record<string, { version?: string }>
    }
    expect(lock.version, 'package-lock.json root version').toBe(packageVersion)
    expect(lock.packages?.['']?.version, 'package-lock.json packages[""] version').toBe(
      packageVersion,
    )
  })
})
