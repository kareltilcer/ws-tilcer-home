import { describe, expect, it } from 'vitest'
import changelog from '../../../handoff/v10/CHANGELOG.md?raw'
import dockerfile from '../../Dockerfile?raw'
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

// ⚠ THE COMMIT HALF HAS AN INVARIANT WITH NO READER TOO, and it is one line of a
// Dockerfile that looks like a typo. `ARG SOURCE_COMMIT` is BARE on purpose: a default
// written in the Dockerfile beats the `ARG SOURCE_COMMIT=<sha>` line Coolify splices in,
// so `ARG SOURCE_COMMIT=""` — the spelling the nine args above it use — blanks the commit
// on every deploy that reaches the build by that splice. That shipped once already. The
// comment above the line says so in capitals, which is exactly the kind of rule an editor
// normalising a file does not read, so it is asserted here for the same reason the
// CHANGELOG mapping and the lockfile are.
describe('frontend/Dockerfile keeps the commit arg inheritable', () => {
  const lines = dockerfile.split('\n').map((line) => line.trim())
  const firstFrom = lines.findIndex((line) => /^FROM\s/i.test(line))
  const buildStep = lines.findIndex((line) => /^RUN\s+npm\s+run\s+build\b/.test(line))
  const sourceArg = lines.findIndex((line) => /^ARG\s+SOURCE_COMMIT\b/.test(line))
  // `${SOURCE_COMMIT}`, surrounding quotes, and a trailing `# comment` — which that file's
  // own `RUN npm run build` line already carries — are all the SAME INSTRUCTION to Docker,
  // measured. A guard that reddens on an edit Docker cannot tell apart is a guard the next
  // person deletes, and then the line it was protecting is unprotected.
  const viteArg = lines.findIndex((line) =>
    /^ARG\s+VITE_APP_COMMIT="?\$\{?SOURCE_COMMIT\}?"?(\s+#.*)?$/.test(line),
  )

  it('declares SOURCE_COMMIT bare, with no default to beat the injected value', () => {
    const declarations = lines.filter((line) => /^ARG\s+SOURCE_COMMIT\b/.test(line))
    expect(declarations, 'no `ARG SOURCE_COMMIT` line in frontend/Dockerfile').toHaveLength(
      1,
    )
    // `ARG SOURCE_COMMIT=""`, `ARG SOURCE_COMMIT=` and `ARG SOURCE_COMMIT=x` all lose.
    // Extra spacing and a trailing `# comment` do NOT — measured, they build to the same
    // instruction, and this line gets the same tolerance as `viteArg` above for the same
    // reason: a guard that reddens on an edit Docker cannot see is a guard that gets cut.
    expect(
      declarations[0],
      'ARG SOURCE_COMMIT must stay BARE — a default here beats what Coolify injects and blanks the label',
    ).toMatch(/^ARG\s+SOURCE_COMMIT(\s+#.*)?$/)
  })

  it('still chains VITE_APP_COMMIT off it, which is what reaches the bundle', () => {
    expect(
      viteArg,
      'frontend/Dockerfile no longer defaults VITE_APP_COMMIT from SOURCE_COMMIT',
    ).toBeGreaterThanOrEqual(0)
  })

  // ⚠ A DEFAULT IS NOT THE ONLY EDIT THAT BLANKS THE LABEL, and the other three leave both
  // lines spelled exactly the way the assertions above want them.
  //
  // Moved ABOVE the first FROM, this declaration becomes a GLOBAL, and a global reaches a
  // stage only through a bare re-declaration inside it — which there would no longer be:
  // measured, that shape bakes an empty commit even under `--build-arg SOURCE_COMMIT=<sha>`,
  // so it breaks the STRONGEST supply path, not the weakest.
  //
  // Moved BELOW the line that reads it, `$SOURCE_COMMIT` expands before the name is
  // declared and yields empty — measured, again even under `--build-arg`. ⚠ THAT ONE IS
  // INVISIBLE ON A COOLIFY DEPLOY, because the `ARG SOURCE_COMMIT=<sha>` line Coolify
  // splices in after the FROM declares the name first and the expansion then works
  // (measured both ways). It breaks local builds and the `--build-arg`-only path instead,
  // which is precisely why it is asserted here: the next deploy would not notice.
  //
  // Moved past `RUN npm run build` — into the Nginx stage, say — both lines are still in
  // the file, still in order, and reach nothing: the ENV block above the build would bake
  // an empty commit. That is why the build step is an anchor here and not just the FROM.
  it('keeps both args inside the build stage, in the order the expansion needs', () => {
    expect(firstFrom, 'no FROM instruction in frontend/Dockerfile').toBeGreaterThanOrEqual(
      0,
    )
    expect(buildStep, 'no `RUN npm run build` in frontend/Dockerfile').toBeGreaterThan(
      firstFrom,
    )
    expect(
      sourceArg,
      'ARG SOURCE_COMMIT must sit AFTER the first FROM — above it, it is a global and nothing here re-declares it',
    ).toBeGreaterThan(firstFrom)
    expect(
      viteArg,
      'ARG VITE_APP_COMMIT=$SOURCE_COMMIT must come AFTER ARG SOURCE_COMMIT, or it expands to nothing',
    ).toBeGreaterThan(sourceArg)
    expect(
      viteArg,
      'both ARGs must sit ABOVE `RUN npm run build` — below it they are read by nothing',
    ).toBeLessThan(buildStep)
    // A FROM between them and the build step would put them in an earlier stage, which
    // the ordering assertions above cannot see.
    const stageBreak = lines.findIndex(
      (line, i) => i > sourceArg && i < buildStep && /^FROM\s/i.test(line),
    )
    expect(
      stageBreak,
      'a FROM between the ARGs and `RUN npm run build` leaves them in a different stage',
    ).toBe(-1)
  })
})
