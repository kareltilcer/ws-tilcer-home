import { describe, expect, it } from 'vitest'
import shell from './AppShell.tsx?raw'
import { DESKTOP_NAV_ORDER, isFullBleedRoute, routes } from './routes'

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
  // ⚠ THIS ONE IS STRUCTURAL, NOT TEXTUAL, and that is why the order was moved out
  // of the component into DESKTOP_NAV_ORDER. The `?raw` convention at the top of
  // this file is for the two nav TABLES, which are still module-level constants
  // inside AppShell; an order that lives in routes.ts can simply be read back, and
  // an assertion on the real array cannot be satisfied by rearranging source text.
  //
  // The sidebar is not the tab bar with a wider gutter. The phone bar is ordered by
  // REACH; a list has no reach to trade on, so the artboards order it by REASON TO
  // LOOK, and chat is the only entry in it that can be waiting for you.
  it('matches the artboard order (design/v10 Home.dc.html, lines 828–842)', () => {
    expect([...DESKTOP_NAV_ORDER]).toEqual([
      routes.chat,
      routes.nastenka,
      routes.ukoly,
      // ⚠ OKNO KEEPS THE FOURTH SLOT IT HAS ALWAYS HAD HERE. D260 took its THUMB
      // TAB, not its place in a list with room for every destination — the demotion
      // is about the four slots that have to fit under a thumb at 375 px.
      routes.okno,
      routes.poznamky,
      routes.dokumenty,
      routes.finance,
      routes.zahrada,
      routes.elektrina,
      routes.log,
      routes.administrace,
      routes.nastaveni,
    ])
  })

  // ⚠ THE ORDER HAS TO NAME EVERY NAV ENTRY. AppShell sorts by it and ranks anything
  // unnamed last, so a module added to PRIMARY or OVERFLOW and forgotten here would
  // land silently at the bottom of the sidebar rather than where it belongs. This is
  // where that gets caught.
  it('covers every entry in both nav tables', () => {
    const keys = [...(block('PRIMARY') + block('OVERFLOW')).matchAll(/\{ to: routes\.(\w+)/g)].map(
      (m) => m[1] as keyof typeof routes,
    )
    expect(keys.length, 'no nav entries found in AppShell').toBe(DESKTOP_NAV_ORDER.length)
    for (const key of keys) {
      expect(DESKTOP_NAV_ORDER, `routes.${key} is missing from DESKTOP_NAV_ORDER`).toContain(routes[key])
    }
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
    // ⚠ AND ANYTHING UNDER IT. `uklid` has no sub-routes today; the exclusion is
    // written for the day it grows one, because an exact match would hand that page
    // the full-bleed box and put its heading against the shell's edge.
    expect(isFullBleedRoute(`${routes.chatUklid}/2026-08`)).toBe(false)
  })

  // ⚠ CASE-INSENSITIVELY, BECAUSE THE ROUTER IS. No route in App.tsx sets
  // `caseSensitive`, so /CHAT really does render the module — and a predicate
  // stricter than the router's matching pads a viewport-sized pane, which is the one
  // thing it exists to prevent.
  it('matches the way the router matches, regardless of case', () => {
    expect(isFullBleedRoute('/CHAT')).toBe(true)
    expect(isFullBleedRoute('/Chat/0198F4C2-6A1B-7000-8000-000000000000')).toBe(true)
    expect(isFullBleedRoute('/Chat/Uklid')).toBe(false)
  })

  it('pads every other destination', () => {
    for (const path of [
      routes.nastenka,
      routes.ukoly,
      routes.okno,
      routes.poznamky,
      routes.dokumenty,
      routes.finance,
      routes.log,
      routes.administrace,
      routes.nastaveni,
      // ⚠ THE TWO MODULES WITH SUB-ROUTES, base AND sub-path: they are the ones a
      // predicate that widened past /chat would catch out first.
      routes.zahrada,
      `${routes.zahrada}/plodiny`,
      routes.elektrina,
      `${routes.elektrina}/cenik`,
    ]) {
      expect(isFullBleedRoute(path), path).toBe(false)
    }
  })

  // ⚠ A PATH PREFIX, NOT A STRING ONE: /chatbot shares five characters with the
  // module and none of its layout.
  it('does not match a path that merely starts with the same letters', () => {
    expect(isFullBleedRoute('/chatbot')).toBe(false)
  })
})
