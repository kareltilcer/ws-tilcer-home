import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { qk } from '@/api/keys'
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
const updateNote = vi.hoisted(() => vi.fn())
vi.mock('./api/endpoints', () => ({
  getNote,
  updateNote,
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
// Vizuální surface, echoes what it was seeded with, and offers a button that emits an
// edit — the one thing a test cannot do by typing into a ProseMirror that isn't there.
const EMITTED = vi.hoisted(() => 'napsáno ve Vizuálním')
vi.mock('./MilkdownEditor', () => ({
  MilkdownEditor: ({ defaultValue, onChange }: { defaultValue: string; onChange: (md: string) => void }) => (
    <div data-testid="visual-editor">
      {defaultValue}
      <button type="button" onClick={() => onChange(EMITTED)}>
        emit
      </button>
    </div>
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

// updateNote has to RESOLVE A NOTE: the component's onSuccess reads `d.body_md` off
// whatever comes back, so a bare vi.fn() (resolving undefined) would throw in there and
// send every autosave a test happens to trigger down the save-FAILURE path — toast,
// retry backoff and all — while looking like it saved.
function resetApi() {
  auth.canWrite = true
  localStorage.clear()
  getNote.mockReset()
  updateNote.mockReset()
  updateNote.mockImplementation(async (_id: string, patch: { body_md?: string }) => note(patch.body_md ?? ''))
}

function renderNote() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const view = render(
    <QueryClientProvider client={qc}>
      <NoteView noteId={NOTE_ID} />
    </QueryClientProvider>,
  )
  // The client comes back so a test can play the part of a refetch landing under an
  // already-open editor.
  return { ...view, qc }
}

const markdownTab = () => screen.getByRole('button', { name: cs.notes.modeMarkdown })
const readTab = () => screen.getByRole('button', { name: cs.notes.modeRead })
const emit = () => screen.getByRole('button', { name: 'emit' })
// The "změněno jinde" advisory, addressed by its one action rather than its sentence:
// the banner's text node sits beside the button, so the button is the unambiguous probe.
const conflictBanner = () => screen.queryByRole('button', { name: cs.notes.reloadTheirs })

describe('NoteView opening mode', () => {
  beforeEach(resetApi)

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

// The editor this opens by itself is a real edit session and has to be watched like
// one. Crepe is seeded once and never re-reads the query, so an empty editor standing
// over a note that has since gained text will save its empty doc back over that text on
// the first keystroke unless something intervenes. Nothing was typed into it, so the
// note's own body is taken silently; once something HAS been typed, the "změněno jinde"
// advisory puts the choice to the user instead.
describe('NoteView auto-opened editor and the changed-elsewhere advisory', () => {
  beforeEach(resetApi)

  // Opening a note must not, by itself, write anything: the seeded draft IS the note's
  // body, so autosave has nothing to persist and the durable mirror stays empty. The
  // teardown flush is where an unwanted save would surface, so unmount and look.
  it('saves nothing just by opening an empty note', async () => {
    getNote.mockResolvedValue(note('\n\n   \n'))
    const { unmount } = renderNote()
    await screen.findByTestId('visual-editor')
    unmount()
    expect(updateNote).not.toHaveBeenCalled()
    expect(localStorage.getItem(`poznamky:draft:${NOTE_ID}`)).toBeNull()
  })

  // A body arriving after the surface was chosen: a persisted cache catching up on
  // reconnect, or another member's edit refetched off the WS echo.
  it('adopts a body that arrives while the auto-opened editor sits on the empty one', async () => {
    getNote.mockResolvedValue(note(''))
    const { qc } = renderNote()
    await screen.findByTestId('visual-editor')
    expect(conflictBanner()).not.toBeInTheDocument()
    // The query observer notifies on a timer, not a microtask, so the assertion has to
    // wait for the render rather than assume act() already produced it.
    qc.setQueryData(qk.noteDetail(NOTE_ID), note('nákup: mléko'))
    // Re-seeded, not just noticed: the editor is showing their text, so the next
    // keystroke lands on top of it instead of replacing it.
    expect(await screen.findByText('nákup: mléko')).toBeInTheDocument()
    expect(conflictBanner()).not.toBeInTheDocument()
  })

  // The adopt is only ever silent while there is nothing to lose. Once the user has
  // typed, their text and the arriving one are a real conflict and it goes to them.
  it('still warns when a body arrives after the user has typed', async () => {
    getNote.mockResolvedValue(note(''))
    const { qc } = renderNote()
    await screen.findByTestId('visual-editor')
    await userEvent.click(emit())
    qc.setQueryData(qk.noteDetail(NOTE_ID), note('nákup: mléko'))
    expect(await screen.findByRole('button', { name: cs.notes.reloadTheirs })).toBeInTheDocument()
    expect(screen.getByText(EMITTED)).toBeInTheDocument() // their version is offered, not imposed
  })

  // The blank-but-not-empty body is the case the conflict baseline is taken for: with
  // the advisory armed from the start, a stored "\n" that nobody else touched must not
  // read as someone else's edit and offer to reload a version identical to this one.
  it('stays quiet when a whitespace-only note is typed into', async () => {
    getNote.mockResolvedValue(note('\n\n   \n'))
    renderNote()
    await screen.findByTestId('visual-editor')
    await userEvent.click(emit())
    expect(await screen.findByText(EMITTED)).toBeInTheDocument()
    expect(conflictBanner()).not.toBeInTheDocument()
  })
})
