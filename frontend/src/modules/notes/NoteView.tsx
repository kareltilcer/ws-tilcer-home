import { lazy, Suspense, useEffect, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { ArrowRightLeft, Check, Link2, Pencil, Pin, RotateCcw, Trash2, Upload, X } from 'lucide-react'
import { qk } from '@/api/keys'
import { ApiError } from '@/api/client'
import * as api from './api/endpoints'
import type { PinScope } from '@/api/types'
import type { NoteDetail } from './api/types'
import { cs } from '@/i18n/cs'
import { cn } from '@/lib/utils'
import { useAuth } from '@/app/auth'
import { routes } from '@/app/routes'
import { Spinner } from '@/components/ui/ui'
import { MarkdownView } from '@/components/common/MarkdownView'
import { VisualToolbar } from './VisualToolbar'
import { MAX_INLINE_IMAGE_DATA_LEN } from './inlineImage'
import type { NoteEditorHandle } from './noteFormat'

// Milkdown (Crepe) is heavy (ProseMirror + CodeMirror). Lazy-load it so it stays
// out of the landing bundle and is fetched only when someone opens the WYSIWYG
// ("Vizuální") mode — the read/Markdown modes need none of it. VisualToolbar is
// deliberately NOT part of that chunk: it only names commands (a type-only import,
// erased at build), so the bar can render while Crepe is still being fetched.
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

// ---- Inline base64 images pasted into the Markdown surface ----
//
// The Markdown tab is a plain textarea with no image pipeline. Copying markdown/HTML
// source that embeds an image as an oversized base64 `data:` URI would land it inline —
// which blows the API body cap and the server rejects (the same guard the Vizuální editor
// sidesteps by uploading). So before such an image reference reaches the draft we upload
// it and swap in the small reference URL — the text equivalent of the Vizuální editor's
// upload plugin. (A pasted image FILE is handled separately, on the same event.)
//
// The match is scoped to IMAGE-REFERENCE position only — `![alt](data:…)` or a raw-HTML
// `<img … src="data:…">` — mirroring the Vizuální editor's "image nodes only" rule. A
// bare `data:image…` token sitting in prose or a fenced code block (e.g. a base64 sample
// a user is documenting) is deliberately left verbatim, never silently rewritten into an
// upload reference — even though the server still rejects an oversized one, which is the
// intended "no megabyte blobs inline" behavior, surfaced as an error rather than as a
// silent content rewrite.

// imageRefDataURIRE captures the data:image URI (group 1 markdown, group 2 raw HTML) only
// where it is used as an image source. Global so matchAll walks every occurrence. Each
// branch stops at its STRUCTURAL terminator — the `)` or the closing quote, never at
// whitespace — so a line-wrapped (MIME-style) base64 image is captured whole, matching
// the backend's inlineImageDataURIRE character for character. Stopping at the first
// newline instead meant such an image was never uploaded: it stayed in the draft, every
// autosave 422'd on the server's guard, and the 4xx branch of save.onError wiped the
// durable mirror along with the pending body. The markdown branch also spans BALANCED
// inner parens (`(…)`, which CommonMark allows in an unwrapped destination), so a data
// URI carrying them is measured whole rather than truncated at the first inner `)`.
const imageRefDataURIRE =
  /!\[[^\]]*\]\((data:image\/(?:[^()]|\([^()]*\))+)\)|<img[^>]+src=["'](data:image\/[^"']+)["']/gi

// wrappedBase64RE is the sanity check the backend needs no equivalent of: it only
// MEASURES what it matches, whereas we REWRITE it. An unclosed `![](data:image/…`
// would otherwise let the capture run over prose up to the next `)` anywhere below,
// and that prose would be uploaded and then stripped out of the note. So a URI
// carrying whitespace must actually look like wrapped base64 to be touched; one
// without whitespace cannot span more than a single token and is taken as-is.
const wrappedBase64RE = /^data:image\/[\w.+-]+;base64,[A-Za-z0-9+/=\s]+$/i
const isUploadableDataURI = (uri: string) => !/\s/.test(uri) || wrappedBase64RE.test(uri)

// oversizedInlineImages splits the oversized data:image references in `text` into the ones
// we can safely migrate to object storage and whether any were left alone. Both halves
// matter: the server's guard has NO equivalent of the isUploadableDataURI exemption, so a
// URI we decline to rewrite is one every save of this note will be rejected for. Saying so
// at paste time beats letting the first autosave fail with an error the user can't place.
const oversizedInlineImages = (text: string): { uploadable: string[]; rejected: boolean } => {
  const uploadable = new Set<string>()
  let rejected = false
  for (const m of text.matchAll(imageRefDataURIRE)) {
    const uri = m[1] ?? m[2]
    if (!uri || uri.length <= MAX_INLINE_IMAGE_DATA_LEN) continue
    if (isUploadableDataURI(uri)) uploadable.add(uri)
    else rejected = true
  }
  return { uploadable: [...uploadable], rejected }
}

// uploadingTokenSeq mints a unique, temporary placeholder src for an image whose upload
// is in flight. It is inserted at the caret synchronously and later swapped for the real
// URL (or removed) via a functional setDraft — so anything the user types WHILE the
// upload runs is preserved instead of being clobbered by a stale full-body snapshot.
let uploadingTokenSeq = 0
const nextUploadingToken = () => `uploading:${Date.now().toString(36)}-${(uploadingTokenSeq++).toString(36)}`

// stripUploadToken removes an in-flight placeholder from the draft when its upload
// fails. It deletes the WHOLE enclosing image reference — a markdown `![alt](token)`
// with any alt text, or a raw-HTML `<img … src="token" …>` — and then any bare leftover,
// so no dangling `![alt]()` or `<img src="">` is persisted (a plain token-substring strip
// would leave those broken remnants behind).
const stripUploadToken = (text: string, token: string): string => {
  const esc = token.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  return text
    .replace(new RegExp(String.raw`!\[[^\]]*\]\(${esc}\)`, 'g'), '')
    .replace(new RegExp(String.raw`<img\b[^>]*\bsrc=["']?${esc}["']?[^>]*>`, 'gi'), '')
    .split(token)
    .join('')
}

// isBlank is the module's one spelling of "a body with nothing in it". Blank counts as
// empty throughout this view — a body of "\n" renders as nothing and the user cannot tell
// the two apart — and two rules turn on that same reading (which surface a note opens on,
// and whether the surface being entered takes the caret), so they read it from here.
const isBlank = (text: string) => text.trim() === ''

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
  onPublish,
}: {
  noteId: string
  readOnly?: boolean
  via?: string
  onMove?: (note: NoteDetail) => void
  onDelete?: (note: NoteDetail) => void
  /** v9: owner-only publish of a private note (D182). Absent = no control. */
  onPublish?: (note: NoteDetail) => void
}) {
  const { canWrite } = useAuth()
  const editable = canWrite && !readOnly
  const qc = useQueryClient()

  const note = useQuery({ queryKey: qk.noteDetail(noteId), queryFn: () => api.getNote(noteId) })

  const [mode, setMode] = useState<Mode>('read')
  const [draft, setDraft] = useState<string | null>(null)
  const [reseed, setReseed] = useState(0) // bump to remount the WYSIWYG editor
  const [autoFocus, setAutoFocus] = useState(false) // the surface being entered should take the caret
  const [titleDraft, setTitleDraft] = useState<string | null>(null)
  const [pinMenu, setPinMenu] = useState(false)
  const [changedElsewhere, setChangedElsewhere] = useState(false)
  const [justSaved, setJustSaved] = useState(false)
  const [uploadsInFlight, setUploadsInFlight] = useState(0) // image uploads still resolving; autosave holds while > 0
  const syncedBodyRef = useRef<string>('') // stored body_md we're in sync with (conflict baseline)
  const saveTimer = useRef<ReturnType<typeof setTimeout>>(undefined)
  const pendingSave = useRef<string | null>(null) // draft awaiting the debounced save
  const savingRef = useRef<string | null>(null) // body currently in flight (null = idle; enforces single-flight)
  const saveFailed = useRef(false) // last save errored → back off the auto-retry
  const failCount = useRef(0) // consecutive transient failures → cap the auto-retry
  const justSavedTimer = useRef<ReturnType<typeof setTimeout>>(undefined) // clears the "Uloženo" flag
  const opened = useRef(false) // the opening surface has been settled (see the mount effect)
  const untouched = useRef(false) // no input of the user's has reached this session's draft yet
  const pendingTokens = useRef(new Set<string>()) // placeholders of the uploads still resolving
  const staleSeed = useRef(false) // the WYSIWYG editor was seeded from a draft holding placeholders
  const draftRef = useRef<string | null>(null) // latest draft, readable from the []-deps unmount flush
  draftRef.current = draft
  const editorRef = useRef<NoteEditorHandle>(null) // the Vizuální toolbar's way into the editor

  // openEditor is the ONE way an edit session starts, so the three ways in — the tab
  // click, a draft rescued on mount, and the empty note that opens itself — cannot
  // drift apart. `seed` sets the two things a session is made of: the DRAFT the editor
  // renders and autosave watches, and the conflict BASELINE the "změněno jinde"
  // advisory measures the stored body against. Omit it to re-enter a session that is
  // already open (just switching tabs), which must not re-baseline anything.
  const openEditor = (m: Exclude<Mode, 'read'>, seed?: { stored: string; initial: string }) => {
    if (seed) {
      syncedBodyRef.current = seed.stored
      setDraft(seed.initial)
      // A session seeded with the note's OWN body holds nothing of the user's yet, and
      // stays that way until they type (see editDraft and the conflict effect). A
      // RESCUED draft is the opposite: it is text a previous session failed to persist,
      // so it counts as touched from the first render and is never adopted away silently.
      untouched.current = seed.initial === seed.stored
    }
    // An empty note is an invitation to write, so the surface being entered takes the
    // caret — which is what raises the keyboard on a phone, where tapping "Vizuální"
    // otherwise opens an editor you still have to tap a second time to type a word in.
    // Only when it IS empty: taking the caret on a note that already has text would
    // slide half of it under a keyboard nobody asked for. A tab click into a session
    // that is already open carries no seed, so its own draft is the body being entered.
    setAutoFocus(isBlank(seed ? seed.initial : (draft ?? '')))
    if (m === 'visual') {
      // Crepe reads `defaultValue` once per `reseed` value and never again, so a new
      // seed needs a new editor. Seeding it from a draft that still holds an upload
      // placeholder gives it a token it cannot resolve; remember that so the re-seed
      // effect below re-seeds it once the uploads land. (Entering Vizuální with
      // nothing in flight seeds clean — the common case.)
      setReseed((r) => r + 1)
      staleSeed.current = pendingTokens.current.size > 0
    }
    setMode(m)
  }

  // editDraft is every path where the USER's own input reaches the draft: typing in
  // either editor, a paste, and the image-upload swaps that finish a paste. It marks the
  // session touched, and nothing ever marks it untouched again — from the first keystroke
  // on, an incoming body is a real conflict that goes to the user through the banner.
  // Seeding a session (openEditor) and adopting the stored body (adoptStored) put the
  // NOTE's text in the draft, not the user's, so they deliberately bypass this.
  const editDraft: typeof setDraft = (next) => {
    untouched.current = false
    setDraft(next)
  }

  // adoptStored discards our draft and takes the stored body instead, re-seeding the
  // WYSIWYG editor from it (same reseed reason as above). Both the user-driven "načíst
  // jejich verzi" and the silent adopt in the conflict effect go through here.
  const adoptStored = (stored: string) => {
    setDraft(stored)
    syncedBodyRef.current = stored
    setReseed((r) => r + 1)
    staleSeed.current = false // re-seeded from the stored body, which holds no placeholders
    // Their text arriving is not the user entering anything — and this re-seed remounts
    // the WYSIWYG editor, so a request left standing from the open would pull the caret
    // back into it out of wherever the user actually is (renaming the note, say) on
    // someone else's save. This is one half of the rule; the upload re-seed below is the
    // other: ONLY the re-seed openEditor itself makes carries a focus request, because
    // openEditor is the only one the user asked for.
    setAutoFocus(false)
  }

  // structural=true when the change moves the note's tree row or its pinned-notes
  // entry (rename, pin): it invalidates the tree and dashboard queries so both hosts
  // (Poznámky pane and Nástěnka overlay) refetch from this one place. A body-only
  // autosave changes neither the tree (title/slug/pin dot) nor most widgets, and the
  // note.changed WS echo still reconciles the dashboard excerpt — so it skips both
  // refetches, avoiding a full reload on every debounced keystroke-batch.
  const applyDetail = (d: NoteDetail, structural: boolean) => {
    qc.setQueryData(qk.noteDetail(noteId), d)
    if (structural) {
      void qc.invalidateQueries({ queryKey: qk.notesAll })
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
      // A permanent (4xx) failure fails identically on every retry. Don't spin: drop this
      // body from the pending marker so onSettled won't reschedule it, and reset the
      // transient counter.
      if (err instanceof ApiError && err.status >= 400 && err.status < 500) {
        saveFailed.current = false
        failCount.current = 0
        if (pendingSave.current === body_md) {
          pendingSave.current = null
          // Only a body doomed NO MATTER WHAT IT CONTAINS is worth dropping the durable
          // mirror for: the note was archived/deleted elsewhere (404) or the write role
          // was revoked (403), so resurrecting it on the next open would just fail again
          // forever. Every other 4xx rejects this particular body while the text in it is
          // still the user's unsaved work — 422 (an image inlined larger than the server
          // stores), 413 (over the API body cap). Wiping the mirror there destroyed the
          // whole editing session behind one generic toast, which is exactly the failure
          // the mirror exists to prevent.
          if (err.status === 403 || err.status === 404) clearDraftMirror(noteId)
        }
        // 422 is the one the user can actually act on, and "nepodařilo se uložit" gives
        // them nothing to go on — name it, and say the draft is still there.
        toast.error(err.status === 422 ? cs.notes.saveRejected : cs.notes.saveError)
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
      void qc.invalidateQueries({ queryKey: qk.notesAll })
      void qc.invalidateQueries({ queryKey: qk.dashboard })
    },
    onError: () => toast.error(cs.notes.saveError),
  })

  // Autosave the body draft (debounced) — last-write-wins (D38). pendingSave holds
  // the not-yet-persisted draft; flushSave sends it single-flight (so concurrent
  // PATCHes can't reorder) and the unmount flush below rescues a draft still pending.
  useEffect(() => {
    if (draft === null || !note.data) return
    // An image upload is still resolving, so the draft holds a throwaway placeholder
    // token. Hold the save until every upload settles (each upload's timeout guarantees
    // it eventually resolves or is stripped), so the token never reaches the server or
    // the durable mirror. Gating on this counter — not on the token TEXT — means a stale
    // or coincidental `uploading:` substring in the body can never wedge autosave shut.
    // The counter is in the deps so the effect re-runs (and saves) the moment it hits 0.
    if (uploadsInFlight > 0) {
      // The SAVE waits, but the durable mirror must not: an upload can run for as long as
      // the request timeout allows, and everything typed in the meantime rides on this
      // effect. Without this, a tab killed mid-upload (crash, reload, closed window) is
      // recovered from a mirror still holding the body from BEFORE the paste — the
      // unmount flush covers an orderly teardown, nothing covers an abrupt one. Mirror
      // the body the unmount path would rescue: the draft minus the placeholders, which
      // is exactly what is safe to persist right now.
      const body = [...pendingTokens.current].reduce((text, token) => stripUploadToken(text, token), draft)
      if (body === (note.data.body_md ?? '')) clearDraftMirror(noteId)
      else writeDraftMirror(noteId, body)
      return
    }
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
  }, [draft, uploadsInFlight])

  // Flush a pending debounced save on unmount (overlay close, note switch, back
  // navigation) so edits made within the last 800ms aren't silently lost. This is a
  // best-effort optimistic PATCH: the component is gone, so its onSuccess/onError
  // may never fire and there is no mounted retry loop to recover a transient failure.
  // The durable net is the localStorage mirror (still holding pendingSave's body) —
  // NOT cleared here, so a failed teardown save is recovered on the note's next open.
  useEffect(() => {
    const inFlightTokens = pendingTokens.current // the Set itself never changes; its contents do
    return () => {
      if (saveTimer.current) clearTimeout(saveTimer.current) // cancel any pending flush/retry
      if (justSavedTimer.current) clearTimeout(justSavedTimer.current)
      // Rescue a pending draft the debounce timer hadn't flushed yet — but ONLY when no
      // save is currently in flight. Firing a PATCH while an earlier one is still running
      // bypasses the single-flight guard (savingRef); React Query does not serialize the
      // two, so the older body can commit last and clobber the newer (a last-write-wins
      // loss). When a save IS in flight, the latest draft is already in the localStorage
      // mirror, so it's recovered on the note's next open instead of risking that race.
      if (savingRef.current === null) {
        const stranded = [...inFlightTokens]
        if (stranded.length > 0 && draftRef.current !== null) {
          // An upload was still resolving, so the autosave effect never ran for the
          // current draft: pendingSave AND the mirror still hold the body from BEFORE
          // the paste, and flushing that would persist it over everything typed since.
          // Take the live draft instead, minus the placeholders whose uploads can no
          // longer land (the component is gone, so nothing will swap them for a URL) —
          // the text survives and only the un-uploaded image is dropped. Re-mirror it
          // too: the durable net is stale for exactly the same reason.
          const body = stranded.reduce((text, token) => stripUploadToken(text, token), draftRef.current)
          if (body !== syncedBodyRef.current) {
            writeDraftMirror(noteId, body)
            save.mutate(body)
          }
        } else if (pendingSave.current !== null) {
          save.mutate(pendingSave.current)
        }
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
    if (stored === syncedBodyRef.current || stored === draft) return
    // Their edit landed over an editor holding NOTHING of ours: nothing the user typed
    // has ever reached this draft, so adopting their version can lose nothing of theirs.
    // Take it silently instead of raising a banner that asks them to choose between the
    // note's own stale text and the same note's new text.
    //
    // Load-bearing, not cosmetic: the WYSIWYG editor is seeded once per `reseed` and
    // never re-reads the query, so without the re-seed it would keep showing the body
    // from before their edit — and the first keystroke into that stale doc would
    // autosave it straight back over what they wrote. That matters most for the
    // session the mount effect below opens BY ITSELF on an empty note: the user never
    // asked for an editor, so an empty draft they never typed into must not become
    // the thing that wipes a body someone else filled in meanwhile.
    //
    // The gate is `untouched` — a flag the user's first input clears for good — and NOT
    // `draft === syncedBodyRef.current`. That equality is not "never typed into": a
    // successful save advances the baseline to the body it just wrote (save.onSuccess),
    // so someone who has been writing for ten minutes satisfies it again the moment
    // autosave catches up, and their text would be replaced under the caret with no
    // banner at all. The other two conditions stay, and uploadsInFlight in particular is
    // not redundant: a pasted image reaches the draft only when its emission or its URL
    // swap lands, so between the paste and that swap the flag is still true and this
    // counter is the only thing standing between an incoming body and the paste.
    if (untouched.current && pendingSave.current === null && uploadsInFlight === 0) {
      adoptStored(stored)
      return
    }
    setChangedElsewhere(true)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [note.data?.body_md, draft, save.isPending, uploadsInFlight])

  // WHICH SURFACE A NOTE OPENS ON, settled ONCE per mount. One effect, not two, so the
  // two candidates below can't race across a declaration order nobody would think to
  // preserve — the earlier split needed a paragraph of comment to explain why it was
  // safe, which is the tell.
  //
  // 1. A draft a previous session couldn't persist outranks everything: a teardown
  //    flush that failed transiently (its onSettled retry couldn't run once unmounted),
  //    or a tab killed before it ran. The mirror outlives the unmount in localStorage;
  //    if it still differs from the stored body, restore it — in MARKDOWN, so the
  //    rescued text is shown verbatim — and let autosave re-attempt the write. A mirror
  //    equal to the stored body is a stale leftover from a save that DID land: drop it.
  // 2. Otherwise an empty note opens in VIZUÁLNÍ, not Číst. Číst has nothing to render
  //    for a note with no body, and an empty note is nearly always one just created from
  //    the "nová poznámka" dialog — which lands the user on a read-only tab, one click
  //    away from writing the thing they opened the dialog to write. Blank counts as
  //    empty: a body of "\n" renders as nothing and the user can't tell the two apart.
  //
  // Once, because a deliberate switch back to Číst on a still-empty note must stick, and
  // a note emptied by deleting its text must not yank the tab out from under the person
  // deleting it. Never for a reader (no editor to be dropped into), never over an
  // archived note, and never over an active in-memory draft.
  useEffect(() => {
    if (opened.current || !editable || !note.data || note.data.archived || draft !== null) return
    opened.current = true
    const stored = note.data.body_md ?? ''
    const mirror = readDraftMirror(noteId)
    if (mirror !== null && mirror !== stored) {
      openEditor('markdown', { stored, initial: mirror }) // arms autosave, which re-persists and re-mirrors it
      toast(cs.notes.draftRecovered)
      return
    }
    if (mirror !== null) clearDraftMirror(noteId)
    if (!isBlank(stored)) return
    // The baseline AND the draft, exactly as a tab click would set them: this IS an edit
    // session, the user just didn't have to ask for it. The baseline keeps a body that is
    // blank but not literally empty ("\n") from reading as someone else's edit and
    // offering to reload a version identical to this one. The draft is what puts the
    // conflict effect above on watch — it early-returns while the draft is null — so a
    // body arriving after this runs (a persisted cache catching up on reconnect, another
    // member's edit over the WS echo) is adopted into the editor instead of being sat on
    // by a surface that never re-reads the query.
    openEditor('visual', { stored, initial: stored })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [note.data, editable])

  // Re-seed the WYSIWYG editor once every placeholder it was seeded with has resolved.
  // Crepe is created ONCE per `reseed` value from `defaultValue`, so a seed taken while an
  // upload was in flight bakes the throwaway `uploading:` token into the editor: it is not
  // a data:/blob: src, so the upload plugin ignores it, and the real URL swapped into the
  // draft afterwards never reaches the editor — its next emission would overwrite the
  // draft with the dead token and strand the uploaded object until the 24h sweep.
  useEffect(() => {
    if (!staleSeed.current || uploadsInFlight > 0 || mode !== 'visual') return
    staleSeed.current = false
    // The other half of adoptStored's rule: a re-seed the user did not ask for drops any
    // standing focus request. A note CAN be blank with an upload still in flight — paste
    // an image, then delete the node again while it uploads — and re-entering Vizuální
    // there arms the request and this re-seed at once, so without this the remount would
    // pull the caret back out of wherever the wait had left the user.
    setAutoFocus(false)
    setReseed((r) => r + 1)
  }, [uploadsInFlight, mode])

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
  // Root-level fallback names the root the note actually lives in (carrier
  // discipline — see rootLabel in PoznamkyPage): the note's own visibility is
  // the scope here, since this view has no page scope of its own.
  const folderLabel = n.path?.length
    ? n.path.map((p) => p.name).join(' / ')
    : n.visibility === 'private'
      ? cs.privacy.privateNotes
      : cs.notes.root
  const body = draft ?? n.body_md ?? ''

  // A tab click into an editor. A null draft means no session is open yet, so this one
  // starts it from the stored body; otherwise we're just moving between two views of a
  // session that is already running and its draft/baseline stay untouched.
  const enterEdit = (m: Exclude<Mode, 'read'>) =>
    openEditor(m, draft === null ? { stored: n.body_md ?? '', initial: n.body_md ?? '' } : undefined)

  // Swap `from` for `to` in the LIVE draft (a functional update, not a stale snapshot),
  // so edits typed while an upload was in flight survive. A null draft means it was
  // discarded meanwhile (e.g. "load their version") — leave it discarded.
  const swapInDraft = (from: string, to: string) =>
    editDraft((prev) => (prev === null ? prev : prev.split(from).join(to)))

  // resolveUploadToken uploads `source()` and replaces its in-flight placeholder token
  // with the real reference URL — or strips the placeholder image on failure. Runs
  // detached from the paste event and only ever touches the draft through swapInDraft.
  // It brackets the upload with the uploadsInFlight counter that gates autosave: bumped
  // synchronously here (before the first await, so the gate is up before the placeholder
  // draft can trigger a save) and released in `finally`, whichever way the upload ends.
  // The token is tracked alongside the counter so the unmount flush can strip the
  // placeholders that will never resolve out of the body it rescues. It returns the URL
  // so the Vizuální editor can point its own node at it (see beginInlineImageUpload).
  const resolveUploadToken = async (
    token: string,
    source: () => Promise<File | Blob>,
    filename = 'obrazek',
  ): Promise<string | null> => {
    pendingTokens.current.add(token)
    setUploadsInFlight((n) => n + 1)
    try {
      const { url } = await api.uploadNoteImage(noteId, await source(), filename)
      swapInDraft(token, url)
      return url
    } catch {
      // Remove the whole enclosing image reference — `![alt](token)` (any alt) or a
      // raw-HTML `<img … src="token" …>` — plus any bare leftover, so no dangling
      // `![alt]()` or `<img src="">` is persisted.
      editDraft((prev) => (prev === null ? prev : stripUploadToken(prev, token)))
      toast.error(cs.notes.imageUploadError)
      return null
    } finally {
      pendingTokens.current.delete(token)
      setUploadsInFlight((n) => Math.max(0, n - 1))
    }
  }

  // beginInlineImageUpload is the Vizuální editor's way into this SAME pipeline for an
  // image pasted as HTML (a data:/blob: node its own onUpload never sees). Routing it
  // through here rather than letting the editor upload on its own is what makes the two
  // surfaces behave alike: one autosave hold, one durable mirror, one unmount rescue. It
  // also means the placeholder lives in the DRAFT, so the swap for the real URL still
  // lands when the editor is torn down mid-upload (mode switch, overlay close) — the
  // image used to be dropped silently there while its bytes sat in the bucket.
  const beginInlineImageUpload = (src: string) => {
    const token = nextUploadingToken()
    // fetch resolves both data: and blob: URLs to their bytes in the browser.
    return { token, url: resolveUploadToken(token, async () => (await fetch(src)).blob()) }
  }

  // The Markdown surface is a plain textarea with no image pipeline, so a pasted image
  // would land as a multi-megabyte base64 data: URI — which the server rejects (the
  // body-cap guard the Vizuální editor sidesteps by uploading). Give it the same path
  // for both vectors: a pasted image FILE (clipboard image / copied from a page), and
  // markdown/HTML TEXT that embeds an oversized `data:image…` image reference. Either way
  // we insert a placeholder at the caret NOW (so the caret and any concurrent typing are
  // honored) and swap in the small reference URL once the upload lands; anything else
  // falls through to the browser.
  const onMarkdownPaste = (e: React.ClipboardEvent<HTMLTextAreaElement>) => {
    // Snapshot value + caret synchronously; the async upload must not read `e` later.
    const { value, selectionStart, selectionEnd } = e.currentTarget
    const splice = (snippet: string) =>
      `${value.slice(0, selectionStart)}${snippet}${value.slice(selectionEnd)}`

    // 1. A pasted image FILE (clipboard image / copied from a page) — but ONLY when the
    // clipboard carries no text to paste instead. Copying from Excel, Word, Outlook or
    // Google Docs puts a text/plain + text/html flavor AND a bitmap rendition of the
    // selection on the clipboard; taking the file there would replace the spreadsheet
    // range (or the formatted paragraph) the user copied with a screenshot of it. A real
    // image paste — a screenshot, or "Copy image" from a page — carries no usable text.
    const text = e.clipboardData.getData('text/plain')
    if (!text.trim()) {
      const item = Array.from(e.clipboardData.items).find(
        (it) => it.kind === 'file' && it.type.startsWith('image/'),
      )
      const file = item?.getAsFile()
      if (file) {
        e.preventDefault()
        const token = nextUploadingToken()
        editDraft(splice(`![](${token})`)) // reserve the caret spot immediately
        void resolveUploadToken(token, () => Promise.resolve(file), file.name || 'obrazek')
        return
      }
    }

    // 2. Pasted TEXT carrying an oversized base64 data:image reference (e.g. copied
    // markdown source). Ordinary text — and short data: snippets the server accepts, and
    // bare base64 in code blocks — fall through to the browser's default paste.
    const { uploadable: uris, rejected } = oversizedInlineImages(text)
    // An oversized reference we won't rewrite still lands in the draft and the server
    // will refuse every save of it. Warn now; the paste itself is left alone rather than
    // silently rewritten (the whole point of the isUploadableDataURI guard).
    if (rejected) toast.error(cs.notes.imageNotEmbeddable)
    if (!text || uris.length === 0) return
    e.preventDefault()
    // Replace each oversized data: URI with a placeholder token BEFORE it enters the
    // draft (so no megabyte data: URI is ever held or autosaved), insert the staged text
    // at the caret, then upload each and swap its token for the real URL.
    const tokenByURI = new Map(uris.map((uri) => [uri, nextUploadingToken()]))
    let staged = text
    for (const [uri, token] of tokenByURI) staged = staged.split(uri).join(token)
    editDraft(splice(staged))
    for (const [uri, token] of tokenByURI) {
      // fetch resolves a data: URI to its bytes in the browser.
      void resolveUploadToken(token, async () => (await fetch(uri)).blob())
    }
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
    adoptStored(n.body_md ?? '')
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
              aria-label={cs.notes.copyLinkLabel}
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
                <div className="absolute right-0 top-10 z-30 w-64 rounded-xl border border-border-strong bg-s1 p-1.5 shadow-[var(--shadow)]">
                  {editable && (
                    <PinOption
                      checked={n.pinned.household}
                      title={cs.notes.pinHousehold}
                      // ⚠ On a private note "pro všechny" is UNAVAILABLE AND
                      // EXPLAINED, never silently absent (D183). Hiding it leaves
                      // the member wondering what they did wrong; Home's standing
                      // discipline is that disabled controls stay visible.
                      hint={
                        n.visibility === 'private'
                          ? cs.privacy.householdPinUnavailable
                          : cs.notes.pinHouseholdHint
                      }
                      disabled={n.visibility === 'private'}
                      onClick={() => togglePin('household')}
                    />
                  )}
                  <PinOption
                    checked={n.pinned.personal}
                    title={cs.notes.pinPersonal}
                    hint={cs.notes.pinPersonalHint}
                    onClick={() => togglePin('personal')}
                  />
                  {/* Publish lives in the item's OWN menu (HANDOFF-design §v9 §3),
                      beside the visibility controls it belongs with — not on a
                      toolbar where a bulk action would sit. Owner-only: a private
                      note is only ever loaded by its owner, so rendering it here
                      cannot expose it to anyone else. */}
                  {onPublish && n.visibility === 'private' && editable && (
                    <>
                      <div className="my-1 h-px bg-border" />
                      <button
                        type="button"
                        onClick={() => {
                          setPinMenu(false)
                          onPublish?.(n)
                        }}
                        className="flex w-full items-center gap-2.5 rounded-md border border-vis-private bg-vis-private-soft px-2.5 py-2 text-left text-[13px] font-bold hover:brightness-110"
                      >
                        <Upload size={14} className="flex-none" />
                        {cs.privacy.publish}
                      </button>
                    </>
                  )}
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

      {/* Formatting bar — Vizuální only, and outside the scroller below so it stays put
          while a long note scrolls under it. */}
      {mode === 'visual' && <VisualToolbar onCommand={(command) => editorRef.current?.format(command)} />}

      {/* Body */}
      <div className="min-h-0 flex-1 overflow-y-auto px-5 pb-6">
        {mode === 'read' && <MarkdownView>{body}</MarkdownView>}
        {mode === 'markdown' && (
          <textarea
            // Empty note: the caret starts here, so a phone raises its keyboard on the
            // tab tap itself — React focuses an autoFocus element during the same commit,
            // which keeps the tap and the focus one gesture as far as iOS is concerned.
            // The Vizuální surface cannot promise that (see MilkdownEditor).
            autoFocus={autoFocus}
            value={body}
            spellCheck={false}
            onChange={(e) => editDraft(e.target.value)}
            onPaste={onMarkdownPaste}
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
            <MilkdownEditor
              key={`${noteId}-${reseed}`}
              ref={editorRef}
              noteId={noteId}
              defaultValue={body}
              autoFocus={autoFocus}
              onChange={editDraft}
              onInlineImage={beginInlineImageUpload}
            />
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
  disabled,
}: {
  checked: boolean
  title: string
  hint: string
  onClick: () => void
  /**
   * v9: a HOUSEHOLD pin on a PRIVATE note (D183).
   *
   * ⚠ Disabled AND EXPLAINED, never hidden. The hint carries the reason —
   * "ostatní ji nevidí" — because a control that silently vanishes leaves the
   * member wondering what they did wrong, and Home's standing discipline is that
   * disabled controls stay visible.
   */
  disabled?: boolean
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      className={cn(
        'flex w-full items-center gap-2.5 rounded-md px-2.5 py-2 text-left',
        disabled ? 'cursor-not-allowed opacity-60' : 'hover:bg-s3',
      )}
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
