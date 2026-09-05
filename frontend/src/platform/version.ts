// The version label the shell prints in two quiet places: the foot of the desktop
// side nav, and the bottom of the mobile "Více" sheet (D271).
//
// ⚠ IT EXISTS SO A BUG CAN BE REPORTED, and that is the whole of its job. A member
// who says "it broke" has told us nothing a rebuild can be checked against; a member
// who copies `v10.2 · a3f9c2e` into the feedback widget has named the exact bundle
// that broke. It is NOT a changelog and NOT an update notice — D26 still holds: the
// service worker updates the app on its own and nobody is asked to do anything about
// a new version.
//
// ⚠ THE TWO HALVES HAVE TWO DIFFERENT SOURCES, on purpose. The VERSION is a property
// of the CODE, so it lives here in the repo and is bumped with
// `handoff/v10/CHANGELOG.md`; the COMMIT is a property of the BUILD, which the repo
// cannot know, so it arrives as a Vite build arg (frontend/Dockerfile). That is why a
// build given no commit prints the version alone rather than printing nothing.

const env = import.meta.env as Record<string, string | undefined>

/** The release this bundle is. ⚠ Bump it with the CHANGELOG's newest heading — the
 *  two are one fact, and a label that disagrees with the changelog is worse than no
 *  label at all, because it is the string a bug report gets filed under. */
export const APP_VERSION = 'v10.2'

/**
 * shortCommit reduces a build arg to the seven characters the label shows.
 *
 * ⚠ ANYTHING THAT IS NOT A SHA IS DROPPED RATHER THAN PRINTED. The value reaches us
 * through a Dockerfile and a deploy platform, so the failures to expect are a literal
 * unexpanded `$SOURCE_COMMIT`, a branch name, or an empty string with its quotes
 * still on — and a label nobody can trust is worth less than a label with one half
 * missing, since the whole point is that it can be copied into a bug report.
 */
export function shortCommit(raw: string | undefined): string {
  const value = (raw ?? '').trim()
  return /^[0-9a-f]{7,40}$/i.test(value) ? value.slice(0, 7).toLowerCase() : ''
}

/** `version · commit`, or the version alone when this build was given no commit. */
export function versionLabel(version: string, commit: string): string {
  return commit ? `${version} · ${commit}` : version
}

/** What the shell renders. `dev` stands in for the sha under `npm run dev`, so the
 *  label is never silently half-absent in the one place it is looked at daily. */
export const APP_VERSION_LABEL = versionLabel(
  APP_VERSION,
  shortCommit(env.VITE_APP_COMMIT) || (import.meta.env.DEV ? 'dev' : ''),
)
