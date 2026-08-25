import type { Scope } from '@/api/types'

/**
 * The root scope, as it lives in the URL (v9, PRD D177/D185).
 *
 * Poznámky and Dokumenty each have TWO roots, and which one you are standing in
 * is part of the address rather than a piece of component state:
 *
 *     /poznamky/recepty/gulas             → shared
 *     /poznamky/soukrome/denik            → private
 *
 * Putting it in the URL is what makes the two trees survive a reload, a
 * back-button, a bookmark and a shared link — and it is why the switcher can be a
 * link rather than a toggle with state to lose.
 *
 * ⚠ `soukrome` is therefore a RESERVED SLUG at both shared roots (D185). The
 * backend enforces it: a shared root folder named "Soukromé" is given `soukrome-2`
 * so this parse can never be ambiguous. If that ever stops being true, a folder
 * could shadow the private tree and this file would be the last thing to notice.
 */
export const PRIVATE_SEGMENT = 'soukrome'

/** parseScopedPath splits a route splat into its scope and its slug path. */
export function parseScopedPath(splat: string): { scope: Scope; path: string } {
  const clean = splat.replace(/^\/+/, '')
  if (clean === PRIVATE_SEGMENT) return { scope: 'private', path: '' }
  if (clean.startsWith(`${PRIVATE_SEGMENT}/`)) {
    return { scope: 'private', path: clean.slice(PRIVATE_SEGMENT.length + 1) }
  }
  return { scope: 'shared', path: clean }
}

/**
 * scopedRoute builds a URL for one root of one module.
 *
 * Always go through this rather than concatenating: the private prefix appears in
 * a dozen places across two pages, and a single one that forgets it navigates the
 * user out of the private tree without saying so.
 */
export function scopedRoute(base: string, scope: Scope, path = ''): string {
  const prefix = scope === 'private' ? `${base}/${PRIVATE_SEGMENT}` : base
  return path ? `${prefix}/${path}` : prefix
}

/**
 * resolvedKey identifies "which item is currently resolved" for the deep-link
 * effects in Poznámky and Dokumenty.
 *
 * ⚠ IT INCLUDES THE SCOPE, and that is the whole reason it exists. Those effects
 * skip re-resolving when the path has not changed, and they used to compare the
 * bare slug path — so moving between /poznamky/soukrome/denik and /poznamky/denik
 * (both trees may hold a `denik`) looked like "nothing changed". The effect
 * returned early, the PREVIOUS root's item stayed selected, and a private note
 * could render under the shared heading, the shared tint and the shared
 * breadcrumb — every one of the five "which tree am I in" carriers saying shared.
 * The user also never reached the shared item they navigated to.
 *
 * A slug path without a scope does not identify anything; neither does this key.
 */
export function resolvedKey(scope: Scope, path: string): string {
  return `${scope}:${path}`
}

/** isPrivate is the predicate the lock mark keys off. */
export const isPrivate = (visibility: string): boolean => visibility === 'private'
