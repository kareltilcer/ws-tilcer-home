import { describe, expect, it } from 'vitest'
import shell from './AppShell.tsx?raw'
import { routes } from './routes'

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
