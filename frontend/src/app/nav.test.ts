import { describe, expect, it } from 'vitest'
import shell from './AppShell.tsx?raw'
import { isFullBleedRoute, routes } from './routes'

// D260 — the first demotion in this app's history, asserted against the source.
//
// ⚠ IT READS AppShell.tsx AS TEXT rather than rendering it, and that is deliberate:
// the two nav tables are module-level constants that the component closes over, so
// rendering would need the whole provider stack (auth, router, query client, the
// theme) to assert a fact that is decided before any of it runs. What can regress is
// the TABLE, and the table is what this reads.
//
// ⚠ AND IT READS IT THROUGH VITE'S `?raw`, NOT node:fs. `tsconfig.app.json` declares
// `types: ["vite/client"]` and nothing else, so a `node:fs` import typechecks in
// vitest and fails `tsc -b` — which is the gate. `?raw` is typed by vite/client and
// resolved by the bundler, so both agree.

function block(name: string): string {
  const start = shell.indexOf(`const ${name}: NavItem[] = [`)
  expect(start, `${name} is missing from AppShell`).toBeGreaterThan(-1)
  const end = shell.indexOf('\n]', start)
  return shell.slice(start, end)
}

describe('D260 — Chat takes a thumb tab and Okno moves to the overflow', () => {
  const primary = block('PRIMARY')
  const overflow = block('OVERFLOW')

  it('gives Chat one of the four thumb tabs', () => {
    expect(primary).toContain('routes.chat')
  })

  // ⚠ FOUR, NOT FIVE. Four thumb-reachable tabs plus *Více* is the shape that works
  // at 375 px; a fifth makes six slots and every target shrinks below the touch-size
  // minimum. This is the count the demotion exists to preserve.
  it('keeps the tab bar at four entries', () => {
    const entries = primary.match(/\{ to: /g) ?? []
    expect(entries.length, 'four thumb tabs plus Více is the 375 px shape (D260)').toBe(4)
  })

  // ⚠ THE THIRD OF FOUR, WHICH IS THE MIDDLE OF FIVE once *Více* is appended. That
  // slot is the easiest one to hit with a thumb, and it is why the one tab in this
  // app carrying an unread count is the one that sits there. The desktop sidebar
  // orders the very same items differently, and that is not a contradiction — see
  // the block below.
  it('keeps Chat in the middle slot of the thumb bar', () => {
    const entries = primary.split('{ to: ').slice(1)
    expect(entries.findIndex((e) => e.startsWith('routes.chat'))).toBe(2)
  })

  it('moves Okno do budoucnosti into the overflow, and only there', () => {
    expect(primary).not.toContain('routes.okno')
    expect(overflow).toContain('routes.okno')
  })

  // A demotion, not a removal: Okno leads the sheet with its FULL name, rather than
  // being buried at the bottom beside the admin-only entries.
  it('puts Okno first in the sheet, with its full name', () => {
    const first = overflow.indexOf('{ to: ')
    expect(overflow.slice(first, first + 120)).toContain('routes.okno')
    expect(overflow).toContain('cs.nav.okno')
  })
})

describe('the chat routes', () => {
  it('addresses the clean-up page under the module it belongs to', () => {
    // ⚠ IT IS A CHAT SUB-ROUTE, NOT AN ADMINISTRACE TAB. The listing is
    // member-scoped, so it belongs to the module whose membership decides what it
    // can show — an admin outside every conversation has no business on it (D240).
    expect(routes.chatUklid).toBe('/chat/uklid')
    expect(routes.chatUklid.startsWith(routes.chat)).toBe(true)
  })
})

describe('the desktop side nav leads with Chat', () => {
  // ⚠ ALSO READ AS TEXT, for the reason at the top of this file: `desktopItems` is
  // assembled inside the component out of both module-level tables, so reading one
  // array's order back would mean standing up the whole provider stack.
  //
  // The sidebar is not the tab bar with a wider gutter. The phone bar is ordered by
  // REACH; a list has no reach to trade on, so it is ordered by REASON TO LOOK, and
  // chat is the only entry in it that can be waiting for you. Deriving one from the
  // other is what had Chat fourth, under three destinations that never ask for
  // anything.
  it('puts Chat above the other three thumb tabs', () => {
    const start = shell.indexOf('const desktopItems = [')
    expect(start, 'desktopItems is missing from AppShell').toBeGreaterThan(-1)
    const list = shell.slice(start, shell.indexOf('\n  ]', start))
    const chatFirst = list.indexOf('item.to === routes.chat')
    const theRest = list.indexOf('item.to !== routes.chat')
    expect(chatFirst, 'the side nav no longer leads with Chat').toBeGreaterThan(-1)
    expect(theRest, 'the other three thumb tabs are missing from the side nav').toBeGreaterThan(chatFirst)
  })
})

describe('isFullBleedRoute — the one module the shell does not pad', () => {
  it('hands the list and one thread the content box whole', () => {
    expect(isFullBleedRoute(routes.chat)).toBe(true)
    expect(isFullBleedRoute(`${routes.chat}/0198f4c2-6a1b-7000-8000-000000000000`)).toBe(true)
    expect(isFullBleedRoute(`${routes.chat}/`)).toBe(true)
  })

  // The clean-up screen is a document that scrolls, like the ten modules either side
  // of it — unpadding it would push its heading into the shell's edge.
  it('leaves the clean-up screen an ordinary padded page', () => {
    expect(isFullBleedRoute(routes.chatUklid)).toBe(false)
    // React Router reads `/chat/uklid/` as the same location, so this has to too.
    expect(isFullBleedRoute(`${routes.chatUklid}/`)).toBe(false)
  })

  it('pads every other destination', () => {
    for (const path of [routes.nastenka, routes.ukoly, routes.okno, routes.poznamky, routes.dokumenty, routes.log, routes.nastaveni]) {
      expect(isFullBleedRoute(path), path).toBe(false)
    }
  })

  // ⚠ A PATH PREFIX, NOT A STRING ONE: /chatbot shares five characters with the
  // module and none of its layout.
  it('does not match a path that merely starts with the same letters', () => {
    expect(isFullBleedRoute('/chatbot')).toBe(false)
  })
})
