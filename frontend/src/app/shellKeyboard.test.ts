import { describe, expect, it } from 'vitest'
import shell from './AppShell.tsx?raw'
import chat from '../modules/chat/ChatPage.tsx?raw'

/**
 * The soft-keyboard layout is an AGREEMENT BETWEEN TWO FILES THAT NEVER IMPORT EACH
 * OTHER, and nothing was checking it.
 *
 * AppShell overwrites two custom properties on the shell root while a keyboard is up
 * and hides the thumb-tab bar over the same span; ChatPage reads those properties,
 * and a third, in its one height calc. Every joint is a STRING. Rename a token on one
 * side and tsc, oxlint and knip all stay green while the chat box quietly loses its
 * height on every phone — `var()` on an undeclared property is not an error, it is a
 * dropped declaration.
 *
 * ⚠ IT READS THE TWO FILES AS TEXT rather than rendering them, for the reason
 * nav.test.ts already gives about AppShell: rendering needs the whole provider stack
 * (auth, router, query client, theme) to assert something the cascade decides, and
 * jsdom has no layout engine to see a `display` swap or a custom property either way.
 * What can regress is the SPELLING, and the spelling is what this reads.
 *
 * ⚠ AND IT READS THROUGH VITE'S `?raw`, NOT node:fs, for the reason given there too:
 * `tsconfig.app.json` declares `types: ["vite/client"]` and nothing else, so a
 * `node:fs` import typechecks under vitest and fails `tsc -b`, which is the gate.
 *
 * ⚠ WHICH IS ALSO WHY theme/globals.css IS NOT THE THIRD FILE HERE. vitest.config.ts
 * sets `css: false`, so a `?raw` import of a stylesheet arrives as an empty string —
 * asserting against it would pass on anything. The declarations live there; what is
 * asserted here is that the two READERS spell them the same way.
 */
describe('the chat viewport tokens — overridden in one file, read in another', () => {
  // ⚠ THE ADDITION HAPPENS WHERE THE TOKENS ARE READ, AND MUST NOT MOVE BACK. A
  // custom property's var()s are substituted where the property is DECLARED, so a
  // --chat-chrome adding the two up on :root goes on resolving against :root's 57 px
  // however the shell root overrides the bottom term — measured, the box came out
  // 57 px short of the keyboard with the override plainly applied.
  it('reads all three tokens in one calc, adding the chrome up there', () => {
    expect(chat).toContain(
      'h-[calc(var(--chat-viewport)-var(--chat-chrome-top)-var(--chat-chrome-bottom))]',
    )
  })

  // ⚠ ONE DECISION IN TWO PLACES. Zeroing the bar's term is only honest while the bar
  // is off screen: both have to hang off the same flag, or the composer ends up 57 px
  // under a bar that is still there.
  it('hides the thumb bar and zeroes its term on the same flag', () => {
    expect(shell).toContain("keyboard.open && 'hidden'")
    expect(shell).toContain("'--chat-chrome-bottom': '0px'")
    expect(shell).toMatch(/keyboard\.open\s*\?/)
  })

  it('hands the visual viewport to the token ChatPage measures against', () => {
    expect(shell).toContain("'--chat-viewport': `${keyboard.viewport}px`")
  })

  // ⚠ AND THE HEADER'S TERM IS STILL NEVER OVERRIDDEN HERE, for a stronger reason
  // than before: v10.2 deleted the phone's app header outright (D272), so the term is
  // 0 in theme/globals.css and there is nothing above the thread for a keyboard to
  // change. It stays declared because that stylesheet is where the shell's chrome is
  // counted; a shell that started zeroing it would be a shell hiding something it no
  // longer renders.
  // ⚠ EITHER QUOTE, because the assertion is a NEGATIVE one and a negative that names
  // one spelling is a guard the next writer walks past without noticing. The quotes
  // stay in the pattern all the same: unquoted, it would also fire on the prose above
  // the style object, which names the token on purpose.
  it('never overrides the header term, which is not there to hide', () => {
    expect(shell).not.toMatch(/['"]--chat-chrome-top['"]/)
  })
})
