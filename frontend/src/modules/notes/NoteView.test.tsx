import { useImperativeHandle, type Ref } from 'react'
import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen, waitFor, within } from '@testing-library/react'
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
// It also records the formatting commands the toolbar sends down its handle, and reports
// whether the caret was asked for — which is as far as either can be proven without a
// real editor under it: putting the caret in a document is milkdown's half of that.
const EMITTED = vi.hoisted(() => 'napsáno ve Vizuálním')
const formatted = vi.hoisted(() => [] as string[])
vi.mock('./MilkdownEditor', () => ({
  MilkdownEditor: ({
    defaultValue,
    autoFocus,
    onChange,
    ref,
  }: {
    defaultValue: string
    autoFocus?: boolean
    onChange: (md: string) => void
    ref?: Ref<{ format: (command: string) => void }>
  }) => {
    useImperativeHandle(ref, () => ({ format: (command: string) => formatted.push(command) }), [])
    return (
      <div data-testid="visual-editor" data-autofocus={String(autoFocus === true)}>
        {defaultValue}
        <button type="button" onClick={() => onChange(EMITTED)}>
          emit
        </button>
      </div>
    )
  },
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
  formatted.length = 0
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
const visualTab = () => screen.getByRole('button', { name: cs.notes.modeVisual })
const readTab = () => screen.getByRole('button', { name: cs.notes.modeRead })
const bodyTextarea = () => screen.getByPlaceholderText(cs.notes.bodyPlaceholder)
const emit = () => screen.getByRole('button', { name: 'emit' })
// The "změněno jinde" advisory, addressed by its one action rather than its sentence:
// the banner's text node sits beside the button, so the button is the unambiguous probe.
const conflictBanner = () => screen.queryByRole('button', { name: cs.notes.reloadTheirs })
const formatBar = () => screen.queryByRole('group', { name: cs.notes.toolbar })

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

  // The same conflict, one autosave later — and this is the one an equality check gets
  // wrong. A successful save advances the conflict baseline to the body it just wrote, so
  // "the draft equals the baseline" goes back to being TRUE for someone who has been
  // writing all along. Their text is still theirs: the arriving body has to go to the
  // banner, not silently into the editor under their caret.
  it('still warns when a body arrives after the user typed AND the autosave landed', async () => {
    getNote.mockResolvedValue(note(''))
    const { qc } = renderNote()
    await screen.findByTestId('visual-editor')
    await userEvent.click(emit())
    // Wait for the real debounced save to land — the write reaching the server is what
    // moves the baseline; short-circuiting it would test a state the app never reaches.
    await waitFor(() => expect(updateNote).toHaveBeenCalledWith(NOTE_ID, { body_md: EMITTED }, undefined), {
      timeout: 3000,
    })
    await waitFor(() => expect(qc.getQueryData(qk.noteDetail(NOTE_ID))).toMatchObject({ body_md: EMITTED }))
    expect(conflictBanner()).not.toBeInTheDocument() // our own save is not someone else's edit

    qc.setQueryData(qk.noteDetail(NOTE_ID), note('nákup: mléko'))
    expect(await screen.findByRole('button', { name: cs.notes.reloadTheirs })).toBeInTheDocument()
    expect(screen.getByText(EMITTED)).toBeInTheDocument() // not replaced under them
    expect(screen.queryByText('nákup: mléko')).not.toBeInTheDocument()
  })

  // Typing is typing on either surface: the Markdown textarea feeds the same draft, so it
  // has to mark the session touched too — and again this only bites once the save has
  // landed, because until then the queued write is what holds the adopt off.
  it('still warns when a body arrives after the user typed in Markdown and it saved', async () => {
    getNote.mockResolvedValue(note(''))
    const { qc } = renderNote()
    await screen.findByTestId('visual-editor')
    await userEvent.click(markdownTab())
    await userEvent.type(screen.getByPlaceholderText(cs.notes.bodyPlaceholder), 'moje věta')
    await waitFor(() => expect(qc.getQueryData(qk.noteDetail(NOTE_ID))).toMatchObject({ body_md: 'moje věta' }), {
      timeout: 3000,
    })

    qc.setQueryData(qk.noteDetail(NOTE_ID), note('nákup: mléko'))
    expect(await screen.findByRole('button', { name: cs.notes.reloadTheirs })).toBeInTheDocument()
    expect(screen.getByPlaceholderText(cs.notes.bodyPlaceholder)).toHaveValue('moje věta')
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

// The Vizuální formatting bar. What the commands DO belongs to milkdown and is stubbed
// out here; what this view owns is which tab the bar belongs to and that pressing a
// button reaches the editor at all — the two halves that were simply missing.
describe('NoteView Vizuální toolbar', () => {
  beforeEach(resetApi)

  it('shows the formatting bar on the Vizuální tab', async () => {
    getNote.mockResolvedValue(note(''))
    renderNote()
    await screen.findByTestId('visual-editor')
    const bar = formatBar()
    expect(bar).toBeInTheDocument()
    // The whole minimal set the design draws, and nothing beyond it.
    expect(within(bar as HTMLElement).getAllByRole('button')).toHaveLength(8)
    expect(within(bar as HTMLElement).getByRole('button', { name: cs.notes.toolbarH1 })).toBeInTheDocument()
  })

  // Číst has nothing to format and Markdown is the raw text itself — a bar that applied
  // to neither would be a control that does nothing on two of the three tabs.
  it('is absent on Číst and on Markdown', async () => {
    getNote.mockResolvedValue(note(''))
    renderNote()
    await screen.findByTestId('visual-editor')
    await userEvent.click(markdownTab())
    expect(formatBar()).not.toBeInTheDocument()
    await userEvent.click(readTab())
    expect(formatBar()).not.toBeInTheDocument()
  })

  // A reader never reaches an editor, so they must not be shown its controls either.
  it('is absent for a reader', async () => {
    auth.canWrite = false
    getNote.mockResolvedValue(note('mléko'))
    renderNote()
    expect(await screen.findByText(cs.notes.readOnly)).toBeInTheDocument()
    expect(formatBar()).not.toBeInTheDocument()
  })

  // The bar renders outside the editor and reaches it through a handle, so the wiring
  // between the two is the thing that can silently come apart.
  it('sends the pressed command down to the editor', async () => {
    getNote.mockResolvedValue(note(''))
    renderNote()
    await screen.findByTestId('visual-editor')
    await userEvent.click(screen.getByRole('button', { name: cs.notes.toolbarBold }))
    await userEvent.click(screen.getByRole('button', { name: cs.notes.toolbarLink }))
    expect(formatted).toEqual(['bold', 'link'])
  })
})

// WHERE THE CARET GOES. Someone who opens a blank page means to write on it, and on a
// phone the caret is what raises the keyboard — without it the Vizuální tab lands you in
// an editor that needs a second tap before a single letter can be typed. A note that
// already HAS text is the opposite case: it was opened to be read, and pulling the caret
// into it would slide half of it under a keyboard nobody asked for.
describe('NoteView autofocus on an empty note', () => {
  beforeEach(resetApi)

  it('asks for the caret in the editor an empty note opens by itself', async () => {
    getNote.mockResolvedValue(note(''))
    renderNote()
    expect(await screen.findByTestId('visual-editor')).toHaveAttribute('data-autofocus', 'true')
  })

  it('focuses the Markdown textarea when an empty note is taken to that tab', async () => {
    getNote.mockResolvedValue(note(''))
    renderNote()
    await screen.findByTestId('visual-editor')
    await userEvent.click(markdownTab())
    expect(bodyTextarea()).toHaveFocus()
  })

  // Blank is empty everywhere else in this view (it renders as nothing and the user
  // cannot tell the two apart), so the caret follows it here too.
  it('focuses the Markdown textarea on a whitespace-only note', async () => {
    getNote.mockResolvedValue(note('\n\n   \n'))
    renderNote()
    await screen.findByTestId('visual-editor')
    await userEvent.click(markdownTab())
    expect(bodyTextarea()).toHaveFocus()
  })

  it('leaves the caret alone on a note that already has text', async () => {
    getNote.mockResolvedValue(note('mléko'))
    renderNote()
    await screen.findByText('mléko')
    await userEvent.click(markdownTab())
    expect(bodyTextarea()).not.toHaveFocus()
    await userEvent.click(visualTab())
    expect(screen.getByTestId('visual-editor')).toHaveAttribute('data-autofocus', 'false')
  })

  // A rescued draft opens in Markdown carrying text the user has not saved. That is a
  // session already under way, not a blank page — the caret stays where it is.
  it('leaves the caret alone on a recovered draft', async () => {
    localStorage.setItem(`poznamky:draft:${NOTE_ID}`, 'rozepsaná věta')
    getNote.mockResolvedValue(note(''))
    renderNote()
    expect(await screen.findByDisplayValue('rozepsaná věta')).not.toHaveFocus()
  })

  // Once the note has been written in, moving between the tabs is moving between two
  // views of a note in progress — the caret belongs wherever the user left it.
  it('stops asking for the caret once the note has been written in', async () => {
    getNote.mockResolvedValue(note(''))
    renderNote()
    await screen.findByTestId('visual-editor')
    await userEvent.click(emit())
    await userEvent.click(markdownTab())
    expect(bodyTextarea()).not.toHaveFocus()
  })

  // Someone else's body arriving re-seeds the auto-opened editor, which remounts it. The
  // user never asked for that and need not even have the caret in the note — they could
  // be renaming it — so the re-seed must not carry the request the open made.
  it('does not grab the caret when a body arrives from elsewhere', async () => {
    getNote.mockResolvedValue(note(''))
    const { qc } = renderNote()
    expect(await screen.findByTestId('visual-editor')).toHaveAttribute('data-autofocus', 'true')
    qc.setQueryData(qk.noteDetail(NOTE_ID), note('nákup: mléko'))
    expect(await screen.findByText('nákup: mléko')).toBeInTheDocument()
    expect(screen.getByTestId('visual-editor')).toHaveAttribute('data-autofocus', 'false')
  })
})
