import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cs } from '@/i18n/cs'
import type { NoteDetail } from './api/types'
import { NoteView } from './NoteView'

// WHICH TAB A NOTE OPENS ON. Číst has nothing to render for a note with no body, and
// an empty note is nearly always one just created — so it opens in Vizuální instead,
// and the note that already has text still opens in Číst. The cases below pin both
// halves plus the three things the default must not run over: a reader (who has no
// editor to be dropped into), a rescued draft (Markdown, so the recovered text is
// visible verbatim), and a deliberate switch back to Číst.

const getNote = vi.hoisted(() => vi.fn())
vi.mock('./api/endpoints', () => ({
  getNote,
  updateNote: vi.fn(),
  pinNote: vi.fn(),
  unpinNote: vi.fn(),
  uploadNoteImage: vi.fn(),
}))

const auth = vi.hoisted(() => ({ canWrite: true }))
vi.mock('@/app/auth', () => ({
  useAuth: () => ({
    canWrite: auth.canWrite,
    isAdmin: false,
    identity: { userId: 'u1', email: 'k@example.com', label: 'Kája', roles: ['write'] },
    logout: () => {},
  }),
}))

// Crepe is lazy-loaded and needs a real DOM/ProseMirror; the stub stands in for the
// Vizuální surface and echoes what it was seeded with.
vi.mock('./MilkdownEditor', () => ({
  MilkdownEditor: ({ defaultValue }: { defaultValue: string }) => (
    <div data-testid="visual-editor">{defaultValue}</div>
  ),
}))

const NOTE_ID = 'n1'

const note = (body_md: string | null): NoteDetail => ({
  id: NOTE_ID,
  folder_id: null,
  title: 'Nápady',
  slug: 'napady',
  body_md,
  position: 'a0',
  archived: false,
  visibility: 'shared',
  owner_id: null,
  created_by: 'u1',
  created_at: '2026-08-30T10:00:00Z',
  updated_at: '2026-08-30T10:00:00Z',
  path: [],
  slug_path: 'napady',
  pinned: { household: false, personal: false },
})

function renderNote() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <NoteView noteId={NOTE_ID} />
    </QueryClientProvider>,
  )
}

const markdownTab = () => screen.getByRole('button', { name: cs.notes.modeMarkdown })
const readTab = () => screen.getByRole('button', { name: cs.notes.modeRead })

describe('NoteView opening mode', () => {
  beforeEach(() => {
    auth.canWrite = true
    localStorage.clear()
    getNote.mockReset()
  })

  it('opens an empty note in Vizuální', async () => {
    getNote.mockResolvedValue(note(''))
    renderNote()
    expect(await screen.findByTestId('visual-editor')).toBeInTheDocument()
  })

  // A note the API returns with a null body is the same empty note, and the create
  // endpoint is free to send either.
  it('opens a null-bodied note in Vizuální', async () => {
    getNote.mockResolvedValue(note(null))
    renderNote()
    expect(await screen.findByTestId('visual-editor')).toBeInTheDocument()
  })

  // Blank is not the same as empty, and the user cannot tell the two apart: a body of
  // whitespace renders as nothing in Číst, so it opens in Vizuální too.
  it('opens a whitespace-only note in Vizuální', async () => {
    getNote.mockResolvedValue(note('\n\n   \n'))
    renderNote()
    expect(await screen.findByTestId('visual-editor')).toBeInTheDocument()
  })

  it('opens a note that has text in Číst', async () => {
    getNote.mockResolvedValue(note('# Seznam\n\nmléko'))
    renderNote()
    expect(await screen.findByText('Seznam')).toBeInTheDocument()
    expect(screen.queryByTestId('visual-editor')).not.toBeInTheDocument()
  })

  // A reader has no mode tabs at all — dropping them into an editor they cannot save
  // from would be worse than the empty read view.
  it('leaves an empty note in the read surface for a reader', async () => {
    auth.canWrite = false
    getNote.mockResolvedValue(note(''))
    renderNote()
    expect(await screen.findByText(cs.notes.readOnly)).toBeInTheDocument()
    expect(screen.queryByTestId('visual-editor')).not.toBeInTheDocument()
  })

  // The rescued text is shown verbatim in Markdown so the user can see exactly what
  // was recovered; the empty-note default must not steal that tab.
  it('keeps a recovered draft in Markdown even though the stored note is empty', async () => {
    localStorage.setItem(`poznamky:draft:${NOTE_ID}`, 'rozepsaná věta')
    getNote.mockResolvedValue(note(''))
    renderNote()
    expect(await screen.findByDisplayValue('rozepsaná věta')).toBeInTheDocument()
    expect(screen.queryByTestId('visual-editor')).not.toBeInTheDocument()
  })

  // The default is a starting point, not a rule the tabs have to fight: switching to
  // Číst on a note that is still empty has to stick.
  it('does not pull the user back out of Číst on a still-empty note', async () => {
    getNote.mockResolvedValue(note(''))
    renderNote()
    await screen.findByTestId('visual-editor')
    await userEvent.click(readTab())
    expect(screen.queryByTestId('visual-editor')).not.toBeInTheDocument()
  })

  // Emptying a note in the editor must not yank the tab out from under the person
  // doing the emptying: the default decides once, on open.
  it('does not snap back to Vizuální when a note is emptied from Markdown', async () => {
    getNote.mockResolvedValue(note('mléko'))
    renderNote()
    await screen.findByText('mléko')
    await userEvent.click(markdownTab())
    await userEvent.clear(screen.getByPlaceholderText(cs.notes.bodyPlaceholder))
    expect(screen.getByPlaceholderText(cs.notes.bodyPlaceholder)).toHaveValue('')
    expect(screen.queryByTestId('visual-editor')).not.toBeInTheDocument()
  })
})
