import { describe, expect, it } from 'vitest'
import shell from './AppShell.tsx?raw'
import settings from '../platform/settings/NastaveniPage.tsx?raw'
import { APP_VERSION, APP_VERSION_LABEL } from '@/platform/version'

/**
 * What the shell puts around every screen — v10.2 (D271, D272, D273).
 *
 * ⚠ IT READS THE TWO FILES AS TEXT, for the reason nav.test.ts and
 * shellKeyboard.test.ts both give: rendering AppShell needs the whole provider stack
 * (auth, router, query client, theme, the live-sync socket and an unread query) to
 * assert facts about which chrome exists, and jsdom has no layout engine to tell a
 * `md:hidden` header from a visible one anyway. What can regress is whether the
 * markup is there at all, and that is what this reads.
 *
 * ⚠ AND THROUGH VITE'S `?raw`, NOT node:fs: `tsconfig.app.json` declares
 * `types: ["vite/client"]` and nothing else, so a `node:fs` import typechecks under
 * vitest and fails `tsc -b`, which is the gate.
 */
describe('D272 — the phone has no app header', () => {
  // ⚠ THE ONLY <header> IN THIS FILE WAS THE PHONE'S. 61 px carrying the word "home",
  // a theme toggle and a sign-out button sat above every screen at 375 px and appears
  // in no artboard of any version. The first row under the status bar belongs to the
  // screen somebody is standing on.
  it('renders no app header element at all', () => {
    expect(shell).not.toMatch(/<header[\s>]/)
  })

  // The two controls it carried are the reason it existed, so their absence is
  // asserted separately: a header removed while its buttons were re-hung somewhere
  // else in the shell would pass the check above and miss the point.
  //
  // ⚠ THE HANDLERS, NOT THE WORDS. Both names appear in that file's prose — the
  // comments say where each control went, which is the paragraph a reader needs most
  // — so a bare /\btoggle\b/ would be a check that fires on its own explanation.
  it('wires neither a theme toggle nor a sign-out control', () => {
    expect(shell).not.toContain('cs.app.signOut')
    expect(shell).not.toContain('onClick={toggle}')
    expect(shell).not.toContain('onClick={logout}')
    expect(shell).not.toMatch(/const \{[^}]*\blogout\b[^}]*\} = useAuth\(\)/)
    expect(shell).toContain('const { theme } = useTheme()')
  })

  // ⚠ THE THEME IS STILL READ HERE, and that is not a leftover. Sonner's toaster is
  // mounted by the shell and has to be told which palette to draw in; what moved is
  // the CONTROL, not the value.
  it('still hands the theme to the toaster it mounts', () => {
    expect(shell).toContain('<Toaster theme={theme}')
  })
})

describe('D273 — sign-out and the theme live in Nastavení', () => {
  it('gives the settings screen the sign-out action', () => {
    expect(settings).toContain('cs.app.signOut')
    expect(settings).toContain('onClick={logout}')
  })

  it('keeps the theme toggle on the same screen', () => {
    expect(settings).toContain('onClick={toggle}')
    expect(settings).toContain('cs.settings.appearanceAccount')
  })
})

describe('D271 — the version is printed where a bug report can find it', () => {
  // Two places, per the artboards: the foot of the side nav and the bottom of the
  // "Více" sheet. One would leave whichever device hit the bug unable to name it.
  it('renders the label in both the side nav and the phone sheet', () => {
    expect([...shell.matchAll(/<VersionLabel\b/g)]).toHaveLength(2)
  })

  it('renders it from the shared constant rather than a second string', () => {
    expect(shell).toContain('{APP_VERSION_LABEL}')
  })

  // ⚠ IT IS A LABEL, NOT A CONTROL, and it says what it is. `v1.10.2 · a3f9c2e` on its
  // own is a token; the accessible name is what makes it a sentence, and a `title`
  // alone is not announced on a non-interactive element.
  // ⚠ MATCHED, NOT QUOTED. The first spelling of this asserted the whole JSX line
  // verbatim, which is a test that fails when Prettier rewraps an attribute and
  // passes when the name is deleted from some OTHER element — the two ways an
  // assertion can be wrong at once. What has to hold is that the string is rendered
  // inside an sr-only span, and nothing about how that span is formatted.
  it('names the string for a screen reader', () => {
    expect(shell).toMatch(/className="sr-only"[^>]*>\s*\{cs\.app\.version\}/)
  })

  it('leads with the version this bundle is', () => {
    expect(APP_VERSION_LABEL.startsWith(APP_VERSION)).toBe(true)
  })
})
