import { lazy, Suspense, useEffect, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { ArrowRightLeft, Check, Link2, Pencil, Pin, RotateCcw, Trash2, X } from 'lucide-react'
import { qk } from '@/api/keys'
import { ApiError } from '@/api/client'
import * as api from '@/api/endpoints'
import type { NoteDetail, PinScope } from '@/api/types'
import { cs } from '@/i18n/cs'
import { cn } from '@/lib/utils'
import { useAuth } from '@/app/auth'
import { routes } from '@/app/routes'
import { Spinner } from '@/components/ui/ui'
import { MarkdownView } from '@/components/common/MarkdownView'

// Milkdown (Crepe) is heavy (ProseMirror + CodeMirror). Lazy-load it so it stays
// out of the landing bundle and is fetched only when someone opens the WYSIWYG
// ("Vizuální") mode — the read/Markdown modes need none of it.
const MilkdownEditor = lazy(() => import('./MilkdownEditor').then((m) => ({ default: m.MilkdownEditor })))

type Mode = 'read' | 'visual' | 'markdown'

// A transient save failure (network / 5xx) is retried, but only this many times in a
// row — beyond that a persistently unreachable server would just be hammered.
const MAX_SAVE_RETRIES = 5

// While an edit is pending (not yet confirmed persisted) it is mirrored to
// localStorage, keyed by note id. This is the safety net for the one save the
// mounted retry loop can't cover: the best-effort flush fired as the editor
// unmounts (overlay close / note switch / back nav). If that final PATCH fails
// transiently — or the tab is killed before it runs — the mirror survives and the
// edit is recovered into the editor the next time the note is opened (see the
// recovery effect). The mirror is cleared the instant a save lands; a mirror that
// still equals the stored body on reopen is a stale leftover from a *successful*
// teardown save and is simply dropped.
const draftKey = (id: string) => `poznamky:draft:${id}`
const readDraftMirror = (id: string): string | null => {
  try {
    return localStorage.getItem(draftKey(id))
  } catch {
    return null // private mode / storage disabled — the mirror is best-effort
  }
}
const writeDraftMirror = (id: string, body: string) => {
  try {
    localStorage.setItem(draftKey(id), body)
  } catch {
    /* quota / disabled — best-effort */
  }
}
const clearDraftMirror = (id: string) => {
  try {
    localStorage.removeItem(draftKey(id))
  } catch {
    /* best-effort */
  }
}

// NoteView is the standalone note read/edit surface reused verbatim by both the
// Poznámky pane and the Nástěnka overlay (FR-P8). It owns the note fetch, the
// three-mode editor (Číst · Vizuální · Markdown over one body_md), autosave
// (last-write-wins), the two-scope pin control, copy-link, and the "změněno
// jinde" advisory. Structural actions (move/delete) are delegated to the host
// via callbacks so the same component works inside an overlay that has no tree.
export function NoteView({
  noteId,
  readOnly = false,
  via,
  onMove,
  onDelete,
}: {
  noteId: string
  readOnly?: boolean
  via?: string
  onMove?: (note: NoteDetail) => void
  onDelete?: (note: NoteDetail) => void
}) {
  const { canWrite } = useAuth()
  const editable = canWrite && !readOnly
  const qc = useQueryClient()

  const note = useQuery({ queryKey: qk.noteDetail(noteId), queryFn: () => api.getNote(noteId) })

  const [mode, setMode] = useState<Mode>('read')
  const [draft, setDraft] = useState<string | null>(null)
  const [reseed, setReseed] = useState(0) // bump to remount the WYSIWYG editor
  const [titleDraft, setTitleDraft] = useState<string | null>(null)
  const [pinMenu, setPinMenu] = useState(false)
  const [changedElsewhere, setChangedElsewhere] = useState(false)
  const [justSaved, setJustSaved] = useState(false)
  const syncedBodyRef = useRef<string>('') // stored body_md we're in sync with (conflict baseline)
  const saveTimer = useRef<ReturnType<typeof setTimeout>>(undefined)
  const pendingSave = useRef<string | null>(null) // draft awaiting the debounced save
  const savingRef = useRef<string | null>(null) // body currently in flight (null = idle; enforces single-flight)
  const saveFailed = useRef(false) // last save errored → back off the auto-retry
  const failCount = useRef(0) // consecutive transient failures → cap the auto-retry
  const justSavedTimer = useRef<ReturnType<typeof setTimeout>>(undefined) // clears the "Uloženo" flag
  const recovered = useRef(false) // guards the one-shot draft recovery on mount

  // structural=true when the change moves the note's tree row or its pinned-notes
  // entry (rename, pin): it invalidates the tree and dashboard queries so both hosts
  // (Poznámky pane and Nástěnka overlay) refetch from this one place. A body-only
  // autosave changes neither the tree (title/slug/pin dot) nor most widgets, and the
  // note.changed WS echo still reconciles the dashboard excerpt — so it skips both
  // refetches, avoiding a full reload on every debounced keystroke-batch.
  const applyDetail = (d: NoteDetail, structural: boolean) => {
    qc.setQueryData(qk.noteDetail(noteId), d)
    if (structural) {
      void qc.invalidateQueries({ queryKey: qk.notesTree })
      void qc.invalidateQueries({ queryKey: qk.dashboard })
    }
  }

  const save = useMutation({
    mutationFn: (body_md: string) => api.updateNote(noteId, { body_md }, via),
    onSuccess: (d, body_md) => {
      syncedBodyRef.current = d.body_md ?? ''
      saveFailed.current = false
      failCount.current = 0
      // Drop the pending marker only now that the write has landed — and only if no
      // newer edit arrived while this save was in flight, so a queued draft isn't
      // lost. (A failed save keeps pendingSave set so it's retried below / rescued by
      // the unmount flush.) The durable mirror clears together with the marker.
      if (pendingSave.current === body_md) {
        pendingSave.current = null
        clearDraftMirror(noteId)
      }
      applyDetail(d, false)
      setJustSaved(true)
      setChangedElsewhere(false)
      if (justSavedTimer.current) clearTimeout(justSavedTimer.current)
      justSavedTimer.current = setTimeout(() => setJustSaved(false), 1500)
    },
    onError: (err, body_md) => {
      // A 401 means the session expired mid-edit. apiFetch routes to login AND rethrows
      // the ApiError, so it DOES land here — it is NOT a doomed body. Drop only the
      // in-memory pending marker (so we don't retry against a dead session) but KEEP the
      // localStorage mirror, so the unsaved edit is recovered on the note's next open
      // after re-login. This must precede the 4xx branch below, which would wipe the
      // mirror and silently lose the edit.
      if (err instanceof ApiError && err.status === 401) {
        if (pendingSave.current === body_md) pendingSave.current = null
        return
      }
      // A permanent (4xx) failure fails identically on every retry — the note was
      // archived/deleted elsewhere (404) or the write role was revoked (403). Don't
      // spin: drop this doomed body from the pending marker so onSettled won't
      // reschedule it, and reset the transient counter.
      if (err instanceof ApiError && err.status >= 400 && err.status < 500) {
        saveFailed.current = false
        failCount.current = 0
        if (pendingSave.current === body_md) {
          pendingSave.current = null
          clearDraftMirror(noteId) // doomed body (note gone / role revoked) — don't resurrect it
        }
        toast.error(cs.notes.saveError)
        return
      }
      // Transient (network / 5xx): keep pendingSave so onSettled retries with backoff,
      // but count consecutive failures so the cap below can stop a persistently-down
      // server from being hammered.
      failCount.current += 1
      saveFailed.current = true
      toast.error(cs.notes.saveError)
    },
    onSettled: () => {
      savingRef.current = null
      // Single-flight serialization: at most one PATCH is ever in flight, so a slow
      // older save can't land after a newer one and clobber it (last-write-wins). If
      // newer edits queued while this one ran — or this one failed transiently —
      // persist the latest draft, debounced (longer after a failure so a flaky network
      // doesn't spin). This is also the retry path that recovers a failed save while
      // the editor stays mounted. A permanent 4xx already cleared pendingSave (above),
      // and once consecutive transient failures hit the cap we stop rescheduling — so
      // neither a doomed save nor a dead server can loop here forever.
      if (pendingSave.current !== null && failCount.current < MAX_SAVE_RETRIES) {
        if (saveTimer.current) clearTimeout(saveTimer.current)
        saveTimer.current = setTimeout(flushSave, saveFailed.current ? 3000 : 800)
      }
    },
  })

  // flushSave persists the latest pending draft, but only when no save is already in
  // flight — the single-flight guard that keeps writes ordered (see onSettled). It
  // reads pendingSave/savingRef live, so it always sends the newest queued body.
  const flushSave = () => {
    if (savingRef.current !== null) return
    const body = pendingSave.current
    if (body === null) return
    savingRef.current = body
    save.mutate(body)
  }

  const rename = useMutation({
    mutationFn: (title: string) => api.updateNote(noteId, { title }, via),
    onSuccess: (d) => {
      // A rename bumps updated_at but leaves body_md untouched, and the conflict
      // guard now compares the stored body (not updated_at) against syncedBodyRef —
      // so our own rename (or its WS echo) can't trip a false "changed elsewhere".
      // Nothing to advance here; just apply the new title/slug.
      applyDetail(d, true)
      setTitleDraft(null)
    },
    onError: () => toast.error(cs.notes.saveError),
  })

  const pin = useMutation({
    mutationFn: ({ scope, on }: { scope: PinScope; on: boolean }) =>
      on ? api.pinNote(noteId, scope, via) : api.unpinNote(noteId, scope, via),
    onSuccess: (pinned) => {
      qc.setQueryData<NoteDetail>(qk.noteDetail(noteId), (d) => (d ? { ...d, pinned } : d))
      void qc.invalidateQueries({ queryKey: qk.notesTree })
      void qc.invalidateQueries({ queryKey: qk.dashboard })
    },
    onError: () => toast.error(cs.notes.saveError),
  })

  // Autosave the body draft (debounced) — last-write-wins (D38). pendingSave holds
  // the not-yet-persisted draft; flushSave sends it single-flight (so concurrent
  // PATCHes can't reorder) and the unmount flush below rescues a draft still pending.
  useEffect(() => {
    if (draft === null || !note.data) return
    if (draft === (note.data.body_md ?? '')) {
      // Reverted back to the stored body within the debounce window — cancel the
      // queued save and drop the stale pending marker (and its mirror), otherwise the
      // retry path or the unmount flush would persist an intermediate value the user
      // already undid.
      if (saveTimer.current) clearTimeout(saveTimer.current)
      pendingSave.current = null
      clearDraftMirror(noteId)
      return
    }
    pendingSave.current = draft
    writeDraftMirror(noteId, draft) // durable safety net until the save is confirmed
    if (saveTimer.current) clearTimeout(saveTimer.current)
    saveTimer.current = setTimeout(flushSave, 800)
    return () => {
      if (saveTimer.current) clearTimeout(saveTimer.current)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [draft])

  // Flush a pending debounced save on unmount (overlay close, note switch, back
  // navigation) so edits made within the last 800ms aren't silently lost. This is a
  // best-effort optimistic PATCH: the component is gone, so its onSuccess/onError
  // may never fire and there is no mounted retry loop to recover a transient failure.
  // The durable net is the localStorage mirror (still holding pendingSave's body) —
  // NOT cleared here, so a failed teardown save is recovered on the note's next open.
  useEffect(() => {
    return () => {
      if (saveTimer.current) clearTimeout(saveTimer.current) // cancel any pending flush/retry
      if (justSavedTimer.current) clearTimeout(justSavedTimer.current)
      // Rescue a pending draft the debounce timer hadn't flushed yet — but ONLY when no
      // save is currently in flight. Firing a PATCH while an earlier one is still running
      // bypasses the single-flight guard (savingRef); React Query does not serialize the
      // two, so the older body can commit last and clobber the newer (a last-write-wins
      // loss). When a save IS in flight, the latest draft is already in the localStorage
      // mirror, so it's recovered on the note's next open instead of risking that race.
      if (savingRef.current === null && pendingSave.current !== null) {
        save.mutate(pendingSave.current)
      }
      pendingSave.current = null
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // Detect a concurrent edit while we're editing. We compare the stored BODY (not
  // updated_at) against the body we synced from: only a body divergence means
  // someone else's edit would be lost by keeping our draft. A rename/move/pin bumps
  // updated_at but not body_md — whether triggered here or arriving as the WS echo
  // of our own structural action — so body comparison never raises a false
  // "changed elsewhere". `stored !== draft` skips the case where they happened to
  // save exactly our text (nothing to lose).
  useEffect(() => {
    if (draft === null || !note.data || save.isPending) return
    const stored = note.data.body_md ?? ''
    if (stored !== syncedBodyRef.current && stored !== draft) {
      setChangedElsewhere(true)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [note.data?.body_md, draft, save.isPending])

  // Recover an autosaved draft a previous session couldn't persist — a teardown flush
  // that failed transiently (its onSettled retry couldn't run once unmounted), or a
  // tab killed before it ran. The mirror outlives the unmount in localStorage; on
  // reopen, if it still differs from the stored body, restore it into the editor (in
  // Markdown mode) and let autosave re-attempt the write. A mirror equal to the stored
  // body is a stale leftover from a save that did land — just drop it. Runs once, only
  // for editors, and never over an active in-memory draft.
  useEffect(() => {
    if (recovered.current || !editable || !note.data || note.data.archived || draft !== null) return
    recovered.current = true
    const mirror = readDraftMirror(noteId)
    if (mirror === null) return
    const stored = note.data.body_md ?? ''
    if (mirror === stored) {
      clearDraftMirror(noteId)
      return
    }
    syncedBodyRef.current = stored
    setDraft(mirror) // arms the autosave effect, which re-persists and re-mirrors it
    setMode('markdown')
    toast(cs.notes.draftRecovered)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [note.data, editable])

  // The note was archived/deleted elsewhere while open (the detail query refetches on
  // the note.* WS echo and returns the tombstone row — GetNoteDetail doesn't filter
  // archived). Abandon any queued/pending save: it would 404 against the backend's
  // archived guard. Clearing the timer, the pending marker, and the durable mirror
  // stops both the retry loop and the teardown flush from firing a doomed PATCH (and
  // from recovering the doomed draft on next open). The gone-state render below then
  // withdraws the editable surface entirely.
  useEffect(() => {
    if (!note.data?.archived) return
    if (saveTimer.current) clearTimeout(saveTimer.current)
    pendingSave.current = null
    clearDraftMirror(noteId)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [note.data?.archived, noteId])

  if (note.isLoading) {
    return (
      <div className="grid flex-1 place-items-center p-8">
        <Spinner />
      </div>
    )
  }
  if (note.isError || !note.data) {
    return (
      <div className="grid flex-1 place-items-center p-8 text-center">
        <div>
          <p className="mb-2 font-bold">{cs.notes.errorTitle}</p>
          <button
            type="button"
            onClick={() => note.refetch()}
            className="h-9 rounded-md bg-accent px-4 text-sm font-semibold text-accent-fg"
          >
            {cs.common.retry}
          </button>
        </div>
      </div>
    )
  }

  // A note archived/deleted elsewhere still resolves via GetNoteDetail (no archived
  // filter), but it is unreachable and every edit PATCH would 404 against the backend
  // guard. Refuse the editable surface — the Poznámky pane's tree reconcile navigates
  // away on the next refetch, but the Nástěnka overlay has no tree, so this inline
  // guard is what keeps it from spamming save-error toasts and silently dropping edits.
  if (note.data.archived) return <NoteGone />

  const n = note.data
  const folderLabel = n.path.length ? n.path.map((p) => p.name).join(' / ') : cs.notes.root
  const body = draft ?? n.body_md ?? ''

  const enterEdit = (m: Exclude<Mode, 'read'>) => {
    if (draft === null) {
      setDraft(n.body_md ?? '')
      syncedBodyRef.current = n.body_md ?? ''
    }
    if (m === 'visual') setReseed((r) => r + 1)
    setMode(m)
  }

  const reloadTheirs = () => {
    // Discard our local draft and adopt their version. Cancel any queued debounced
    // save AND clear the pending marker (and its durable mirror) first — otherwise
    // the unmount flush (or a late timer, or the next-open recovery) would resurrect
    // the draft we just discarded and clobber the version the user chose to keep.
    // Setting draft to body_md next makes the draft-change effect early-return, so
    // pendingSave stays null.
    if (saveTimer.current) clearTimeout(saveTimer.current)
    pendingSave.current = null
    clearDraftMirror(noteId)
    saveFailed.current = false
    setDraft(n.body_md ?? '')
    syncedBodyRef.current = n.body_md ?? ''
    setReseed((r) => r + 1)
    setChangedElsewhere(false)
  }

  // commitTitle PATCHes only when the title actually changed; a no-op rename would
  // write a spurious audit event, bump updated_at, and trip every other open
  // editor's "changed elsewhere" guard. Empty or unchanged just closes the field.
  const commitTitle = () => {
    // In-flight guard (mirrors NotesDialogs' `if (pending) return`): a held/double Enter,
    // or Enter immediately followed by onBlur, must not fire a second rename while the
    // first is still pending (n.title hasn't updated yet, so `t !== n.title` still holds).
    if (rename.isPending) return
    const t = (titleDraft ?? '').trim()
    if (t && t !== n.title) rename.mutate(t)
    else setTitleDraft(null)
  }

  const copyLink = async () => {
    const url = `${location.origin}${routes.poznamky}/${n.slug_path}`
    try {
      if (!navigator.clipboard) throw new Error('clipboard unavailable')
      await navigator.clipboard.writeText(url)
      toast.success(cs.notes.copyLinkDone)
    } catch {
      toast.error(cs.notes.copyLinkError)
    }
  }

  const togglePin = (scope: PinScope) => {
    const on = scope === 'household' ? !n.pinned.household : !n.pinned.personal
    pin.mutate({ scope, on })
    setPinMenu(false)
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {/* Header */}
      <div className="flex-none border-b border-border px-5 pb-3 pt-4">
        <div className="flex flex-wrap items-start gap-3">
          <div className="min-w-0 flex-1">
            <div className="mb-1 truncate font-mono text-[11px] text-subtle">{folderLabel}</div>
            {titleDraft !== null ? (
              <input
                autoFocus
                value={titleDraft}
                onChange={(e) => setTitleDraft(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') commitTitle()
                  if (e.key === 'Escape') setTitleDraft(null)
                }}
                onBlur={commitTitle}
                className="w-full rounded-md border border-accent bg-s2 px-2 py-1 text-xl font-extrabold tracking-tight text-fg outline-none"
              />
            ) : (
              <h2 className="break-words text-xl font-extrabold leading-snug tracking-tight text-pretty">{n.title}</h2>
            )}
            <div className="mt-2 flex flex-wrap items-center gap-1.5">
              {n.pinned.household && (
                <span className="inline-flex h-6 items-center gap-1.5 rounded-full bg-accent-soft px-2.5 text-[11.5px] font-bold text-accent">
                  <span className="h-2 w-2 rounded-full bg-accent" /> {cs.notes.badgeHousehold}
                </span>
              )}
              {n.pinned.personal && (
                <span className="inline-flex h-6 items-center gap-1.5 rounded-full bg-s3 px-2.5 text-[11.5px] font-bold text-muted">
                  <span className="h-2 w-2 rounded-full border border-muted" /> {cs.notes.badgePersonal}
                </span>
              )}
            </div>
          </div>
          <div className="relative flex flex-none items-center gap-1.5">
            {editable && (
              <IconBtn label={cs.notes.rename} onClick={() => setTitleDraft(n.title)}>
                <Pencil size={14} />
              </IconBtn>
            )}
            <button
              type="button"
              onClick={copyLink}
              title={cs.notes.copyLinkTitle}
              className="inline-flex h-[34px] items-center gap-1.5 rounded-md border border-border bg-s2 px-3 text-[12.5px] font-semibold text-fg hover:bg-s3"
            >
              <Link2 size={14} className="text-accent" /> {cs.notes.copyLink}
            </button>
            <button
              type="button"
              onClick={() => setPinMenu((o) => !o)}
              aria-haspopup="true"
              aria-expanded={pinMenu}
              className={cn(
                'inline-flex h-[34px] items-center gap-1.5 rounded-md border px-3 text-[12.5px] font-semibold',
                n.pinned.household || n.pinned.personal
                  ? 'border-accent bg-accent-soft text-accent'
                  : 'border-border bg-s2 text-fg hover:bg-s3',
              )}
            >
              <Pin size={14} /> {n.pinned.household || n.pinned.personal ? cs.notes.pinned : cs.notes.pin}
            </button>
            {pinMenu && (
              <>
                <div className="fixed inset-0 z-20" onClick={() => setPinMenu(false)} />
                <div className="absolute right-0 top-10 z-30 w-60 rounded-xl border border-border-strong bg-s1 p-1.5 shadow-[var(--shadow)]">
                  {editable && (
                    <PinOption
                      checked={n.pinned.household}
                      title={cs.notes.pinHousehold}
                      hint={cs.notes.pinHouseholdHint}
                      onClick={() => togglePin('household')}
                    />
                  )}
                  <PinOption
                    checked={n.pinned.personal}
                    title={cs.notes.pinPersonal}
                    hint={cs.notes.pinPersonalHint}
                    onClick={() => togglePin('personal')}
                  />
                </div>
              </>
            )}
          </div>
        </div>
      </div>

      {/* "změněno jinde" banner */}
      {changedElsewhere && (
        <div className="mx-5 mt-3 flex flex-none items-center gap-3 rounded-xl border border-warn/40 bg-warn/10 px-3 py-2.5">
          <RotateCcw size={15} className="flex-none text-warn" />
          <div className="flex-1 text-[12.5px] leading-snug">
            {cs.notes.changedElsewhere}{' '}
            <button type="button" onClick={reloadTheirs} className="font-bold text-accent underline">
              {cs.notes.reloadTheirs}
            </button>
          </div>
          <button
            type="button"
            onClick={() => setChangedElsewhere(false)}
            aria-label="Zavřít upozornění"
            className="grid h-6 w-6 flex-none place-items-center rounded-md text-muted hover:bg-s2"
          >
            <X size={13} />
          </button>
        </div>
      )}

      {/* Mode toggle + status + actions */}
      <div className="flex flex-none flex-wrap items-center gap-2.5 px-5 py-3">
        {editable ? (
          <div className="flex gap-0.5 rounded-md border border-border bg-s2 p-0.5">
            <ModeTab active={mode === 'read'} onClick={() => setMode('read')}>
              {cs.notes.modeRead}
            </ModeTab>
            <ModeTab active={mode === 'visual'} onClick={() => enterEdit('visual')}>
              {cs.notes.modeVisual}
            </ModeTab>
            <ModeTab active={mode === 'markdown'} onClick={() => enterEdit('markdown')}>
              {cs.notes.modeMarkdown}
            </ModeTab>
          </div>
        ) : (
          <span className="inline-flex h-7 items-center gap-2 rounded-full border border-border bg-s3 px-3 text-[12px] font-semibold text-muted">
            {cs.notes.readOnly}
          </span>
        )}
        <span aria-live="polite" className="text-[11.5px] text-subtle">
          {save.isPending ? cs.notes.saving : justSaved ? cs.notes.saved : ''}
        </span>
        <div className="flex-1" />
        {editable && (onMove || onDelete) && (
          <div className="flex gap-2">
            {onMove && (
              <button
                type="button"
                onClick={() => onMove(n)}
                className="inline-flex h-8 items-center gap-1.5 rounded-md border border-border bg-s2 px-3 text-[12.5px] font-semibold text-fg hover:bg-s3"
              >
                <ArrowRightLeft size={13} /> {cs.notes.move}
              </button>
            )}
            {onDelete && (
              <button
                type="button"
                onClick={() => onDelete(n)}
                aria-label={cs.notes.delete}
                className="grid h-8 w-8 place-items-center rounded-md border border-border bg-s2 text-danger hover:bg-danger/10"
              >
                <Trash2 size={14} />
              </button>
            )}
          </div>
        )}
      </div>

      {/* Body */}
      <div className="min-h-0 flex-1 overflow-y-auto px-5 pb-6">
        {mode === 'read' && <MarkdownView>{body}</MarkdownView>}
        {mode === 'markdown' && (
          <textarea
            value={body}
            spellCheck={false}
            onChange={(e) => setDraft(e.target.value)}
            placeholder={cs.notes.bodyPlaceholder}
            className="h-full min-h-[300px] w-full resize-y rounded-xl border border-border bg-s2 px-4 py-3.5 font-mono text-sm leading-relaxed text-fg outline-none focus:border-accent"
          />
        )}
        {mode === 'visual' && (
          <Suspense
            fallback={
              <div className="grid place-items-center py-10">
                <Spinner />
              </div>
            }
          >
            <MilkdownEditor key={`${noteId}-${reseed}`} defaultValue={body} onChange={setDraft} />
          </Suspense>
        )}
      </div>
    </div>
  )
}

function NoteGone() {
  return (
    <div className="grid flex-1 place-items-center p-8 text-center">
      <div>
        <p className="mb-1 font-bold">{cs.notes.goneTitle}</p>
        <p className="text-sm text-muted">{cs.notes.goneBody}</p>
      </div>
    </div>
  )
}

function ModeTab({ active, onClick, children }: { active: boolean; onClick: () => void; children: React.ReactNode }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        'h-7 rounded px-3 text-[12.5px] font-semibold',
        active ? 'bg-accent text-accent-fg' : 'text-muted hover:text-fg',
      )}
    >
      {children}
    </button>
  )
}

function IconBtn({ label, onClick, children }: { label: string; onClick: () => void; children: React.ReactNode }) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-label={label}
      title={label}
      className="grid h-[34px] w-[34px] place-items-center rounded-md border border-border bg-s2 text-muted hover:bg-s3 hover:text-fg"
    >
      {children}
    </button>
  )
}

function PinOption({
  checked,
  title,
  hint,
  onClick,
}: {
  checked: boolean
  title: string
  hint: string
  onClick: () => void
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="flex w-full items-center gap-2.5 rounded-md px-2.5 py-2 text-left hover:bg-s3"
    >
      <span
        className={cn(
          'grid h-5 w-5 flex-none place-items-center rounded-md border',
          checked ? 'border-accent bg-accent text-accent-fg' : 'border-border-strong text-transparent',
        )}
      >
        <Check size={12} />
      </span>
      <span className="flex-1">
        <span className="block text-[13px] font-semibold text-fg">{title}</span>
        <span className="block text-[11px] text-subtle">{hint}</span>
      </span>
    </button>
  )
}
