import type { ReactNode } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { render, screen, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cs } from '@/i18n/cs'
import { routes } from '@/app/routes'
import { PRIVATE_SEGMENT } from '@/lib/scope'
import { RootSwitcher } from './RootSwitcher'

// ⚠ ON MOBILE THE SWITCHER IS THE ONLY WAY IN AND OUT OF THE PRIVATE TREE. It was
// once rendered in DesktopView alone, so a member who reached /poznamky/soukrome
// at 375 px could leave only by editing the address bar — and the empty private
// root, the screen somebody meets on day one, was the likeliest place to be
// stranded. Both mobile branches are pinned below, for both twins (D40), together
// with the reserved `soukrome` slug the private link has to carry.

const getNotesTree = vi.hoisted(() => vi.fn())
const getDocumentsTree = vi.hoisted(() => vi.fn())
vi.mock('@/api/endpoints', async (orig) => ({
  ...(await orig<Record<string, unknown>>()),
  getNotesTree,
  getDocumentsTree,
}))
vi.mock('@/app/auth', () => ({ useAuth: () => ({ canWrite: true, isAdmin: false }) }))

// jsdom's matchMedia stub reports matches:false, so useIsDesktop is false and the
// pages render MobileView — the viewport this control exists to solve.
async function renderAt(path: string, routePath: string, element: ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path={routePath} element={element} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
  return within(await screen.findByRole('navigation', { name: cs.privacy.switcherLabel })).getAllByRole('link')
}

describe('RootSwitcher', () => {
  it('offers both roots, with the private one on the reserved slug', () => {
    render(
      <MemoryRouter>
        <RootSwitcher
          scope="shared"
          base={routes.poznamky}
          sharedLabel={cs.notes.title}
          privateLabel={cs.privacy.privateNotes}
        />
      </MemoryRouter>,
    )
    const links = within(screen.getByRole('navigation', { name: cs.privacy.switcherLabel })).getAllByRole('link')
    expect(links.map((a) => a.getAttribute('href'))).toEqual([
      routes.poznamky,
      `${routes.poznamky}/${PRIVATE_SEGMENT}`,
    ])
    expect(links[1]).toHaveAccessibleName(new RegExp(cs.privacy.privateNotes))
  })

  it('marks the root you are standing in, and only that one', () => {
    const { rerender } = render(
      <MemoryRouter>
        <RootSwitcher
          scope="private"
          base={routes.poznamky}
          sharedLabel={cs.notes.title}
          privateLabel={cs.privacy.privateNotes}
        />
      </MemoryRouter>,
    )
    const current = () =>
      within(screen.getByRole('navigation', { name: cs.privacy.switcherLabel }))
        .getAllByRole('link')
        .map((a) => a.getAttribute('aria-current'))

    expect(current()).toEqual([null, 'page'])
    rerender(
      <MemoryRouter>
        <RootSwitcher
          scope="shared"
          base={routes.poznamky}
          sharedLabel={cs.notes.title}
          privateLabel={cs.privacy.privateNotes}
        />
      </MemoryRouter>,
    )
    expect(current()).toEqual(['page', null])
  })
})

describe('the mobile root switcher', () => {
  it('gives the empty private Poznámky root a way back to the shared tree', async () => {
    getNotesTree.mockResolvedValue({ roots: [], root_notes: [] })
    const { PoznamkyPage } = await import('@/routes/poznamky/PoznamkyPage')

    const links = await renderAt(
      `${routes.poznamky}/${PRIVATE_SEGMENT}`,
      `${routes.poznamky}/*`,
      <PoznamkyPage />,
    )
    expect(links.map((a) => a.getAttribute('href'))).toEqual([
      routes.poznamky,
      `${routes.poznamky}/${PRIVATE_SEGMENT}`,
    ])
    expect(links[1]).toHaveAttribute('aria-current', 'page')
  })

  it('gives the empty private Dokumenty root the same way back (D40)', async () => {
    getDocumentsTree.mockResolvedValue({ roots: [], root_documents: [] })
    const { DokumentyPage } = await import('@/routes/dokumenty/DokumentyPage')

    const links = await renderAt(
      `${routes.dokumenty}/${PRIVATE_SEGMENT}`,
      `${routes.dokumenty}/*`,
      <DokumentyPage />,
    )
    expect(links.map((a) => a.getAttribute('href'))).toEqual([
      routes.dokumenty,
      `${routes.dokumenty}/${PRIVATE_SEGMENT}`,
    ])
    expect(links[1]).toHaveAttribute('aria-current', 'page')
  })

  it('still offers the private root from the shared tree', async () => {
    getNotesTree.mockResolvedValue({ roots: [], root_notes: [] })
    const { PoznamkyPage } = await import('@/routes/poznamky/PoznamkyPage')

    const links = await renderAt(routes.poznamky, `${routes.poznamky}/*`, <PoznamkyPage />)
    expect(links[0]).toHaveAttribute('aria-current', 'page')
    expect(links[1]).toHaveAttribute('href', `${routes.poznamky}/${PRIVATE_SEGMENT}`)
  })
})
