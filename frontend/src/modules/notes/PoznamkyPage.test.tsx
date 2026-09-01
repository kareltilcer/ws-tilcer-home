import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cs } from '@/i18n/cs'
import { routes } from '@/app/routes'
import { PRIVATE_SEGMENT } from '@/lib/scope'
import type { Folder, NotesTree } from './api/types'

// AN EMPTY TREE'S ONLY OFFER WAS A NOTE, AND ON A PHONE THAT WAS THE WHOLE SCREEN.
// MobileView returns early for the empty root, so the header row carrying the
// folder button never rendered: the only way to a folder was to create a note
// first, purely to get the header back. The empty root is also the day-one screen
// of the private tree, so it was the likeliest place to meet the dead end. The
// cases below pin the folder action on both roots, inside a folder, and on the
// Dokumenty twin (D40) — and pin that a reader is still offered neither.

const getNotesTree = vi.hoisted(() => vi.fn())
const getDocumentsTree = vi.hoisted(() => vi.fn())
vi.mock('./api/endpoints', async (orig) => ({
  ...(await orig<Record<string, unknown>>()),
  getNotesTree,
}))
vi.mock('@/modules/documents/api/endpoints', async (orig) => ({
  ...(await orig<Record<string, unknown>>()),
  getDocumentsTree,
}))

const auth = vi.hoisted(() => ({ canWrite: true }))
vi.mock('@/app/auth', () => ({
  useAuth: () => ({ canWrite: auth.canWrite, isAdmin: false }),
}))

const folder = (over: Partial<Folder> = {}): Folder => ({
  id: 'f1',
  parent_id: null,
  name: 'Recepty',
  slug: 'recepty',
  icon: '',
  position: 'm',
  archived: false,
  visibility: 'shared',
  owner_id: null,
  created_by: 'u1',
  created_at: '2026-08-01T10:00:00Z',
  updated_at: '2026-08-01T10:00:00Z',
  ...over,
})

const EMPTY: NotesTree = { roots: [], root_notes: [] }

// jsdom's matchMedia stub reports matches:false, so useIsDesktop is false and the
// page renders MobileView — the viewport the dead end was on.
async function renderPoznamky(path: string = routes.poznamky) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const { PoznamkyPage } = await import('./PoznamkyPage')
  render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path={`${routes.poznamky}/*`} element={<PoznamkyPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

const createFolderDialog = () => screen.findByRole('dialog', { name: cs.notes.createFolderHeading })

beforeEach(() => {
  auth.canWrite = true
  getNotesTree.mockResolvedValue(EMPTY)
  getDocumentsTree.mockResolvedValue({ roots: [], root_documents: [] })
})

describe('the empty Poznámky root', () => {
  it('offers a folder beside the note, and the folder lands at the root', async () => {
    await renderPoznamky()

    expect(await screen.findByRole('button', { name: cs.notes.createNoteFull })).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: cs.notes.createFolderFull }))

    const dialog = await createFolderDialog()
    expect(within(dialog).getByText(`${cs.notes.locationLabel} ${cs.notes.rootLocation}`)).toBeInTheDocument()
  })

  it('offers it in the private root too — the screen a member meets on day one', async () => {
    await renderPoznamky(`${routes.poznamky}/${PRIVATE_SEGMENT}`)

    // The private copy, so this is the private root and not the shared one.
    expect(await screen.findByText(cs.privacy.emptyNotesTitle)).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: cs.notes.createFolderFull }))

    // ⚠ AND THE DIALOG NAMES THE PRIVATE ROOT. Both pages fell back to the shared
    // label whenever the parent was null, so the confirmation for a folder about to
    // land in the private tree read 'Poznámky (kořen)' — the other tree's name.
    const dialog = await createFolderDialog()
    expect(within(dialog).getByText(`${cs.notes.locationLabel} ${cs.privacy.rootLocationNotes}`)).toBeInTheDocument()
  })

  it('offers a reader neither — the empty state is not a way around canWrite', async () => {
    auth.canWrite = false
    await renderPoznamky()

    expect(await screen.findByText(cs.notes.emptyRootTitle)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: cs.notes.createFolderFull })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: cs.notes.createNoteFull })).not.toBeInTheDocument()
  })
})

describe('an empty Poznámky folder', () => {
  it('offers a subfolder, created inside the folder you are standing in', async () => {
    getNotesTree.mockResolvedValue({ roots: [{ folder: folder(), subfolders: [], notes: [] }], root_notes: [] })
    await renderPoznamky()

    await userEvent.click(await screen.findByRole('button', { name: /Recepty/ }))
    await userEvent.click(await screen.findByRole('button', { name: cs.notes.folderHere }))

    const dialog = await createFolderDialog()
    expect(within(dialog).getByText(/Recepty/)).toBeInTheDocument()
  })
})

describe('the Dokumenty twin (D40)', () => {
  it('offers a folder in its empty root as well', async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const { DokumentyPage } = await import('@/modules/documents/DokumentyPage')
    render(
      <QueryClientProvider client={qc}>
        <MemoryRouter initialEntries={[routes.dokumenty]}>
          <Routes>
            <Route path={`${routes.dokumenty}/*`} element={<DokumentyPage />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )

    await userEvent.click(await screen.findByRole('button', { name: cs.documents.createFolderFull }))
    expect(await screen.findByRole('dialog', { name: cs.documents.createFolderHeading })).toBeInTheDocument()
  })
})
