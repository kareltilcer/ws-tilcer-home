import { useEffect, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { ArrowUpRight, Check, Download, Link2, Pencil, Pin, Trash2, MoveRight, Upload } from 'lucide-react'
import { qk } from '@/api/keys'
import { ApiError } from '@/api/client'
import * as api from '@/api/endpoints'
import type { DocumentDetail, PinScope } from '@/api/types'
import { cs } from '@/i18n/cs'
import { cn } from '@/lib/utils'
import { fmtBytes, fmtDateTime } from '@/i18n/format'
import { useAuth } from '@/app/auth'
import { Button, Spinner, buttonClass } from '@/components/ui/ui'
import { MarkdownView } from '@/components/common/MarkdownView'
import { kindMeta, viewerMode } from './fileKind'

// DocumentView is the standalone document viewer (design/v4/DocumentView.dc.html).
// The Dokumenty pane renders it, and the Nástěnka overlay reuses it VERBATIM — the
// overlay only wraps it in a dialog and passes via="dashboard" so edits made from
// the dashboard are attributed there in the audit log.
//
// Preview rendering follows the untrusted-content rules (D48): PDFs (including a
// derived Office→PDF) go into a SANDBOXED iframe, images into <img>, text and
// Markdown are fetched and rendered by the app itself. Nothing else is ever
// rendered — download-only, pending, and failed states show a type card with
// Stáhnout, never a broken frame.
export function DocumentView({
  documentId,
  readOnly,
  via,
  onRename,
  onMove,
  onDelete,
  onPublish,
  onGone,
}: {
  documentId: string
  readOnly?: boolean
  via?: string
  onRename?: (d: DocumentDetail) => void
  onMove?: (d: DocumentDetail) => void
  onDelete?: (d: DocumentDetail) => void
  /** v9: owner-only publish of a private document (D182). Absent = no control. */
  onPublish?: (d: DocumentDetail) => void
  onGone?: () => void
}) {
  const { canWrite } = useAuth()
  const qc = useQueryClient()
  const [pinMenu, setPinMenu] = useState(false)
  const editable = canWrite && !readOnly

  const doc = useQuery({
    queryKey: qk.documentDetail(documentId),
    queryFn: () => api.getDocument(documentId),
  })

  const pin = useMutation({
    mutationFn: ({ scope, on }: { scope: PinScope; on: boolean }) =>
      on ? api.pinDocument(documentId, scope, via) : api.unpinDocument(documentId, scope, via),
    onSuccess: (_data, vars) => {
      // ONE call covers the detail, the tree and the search results: since v9 the
      // module prefix is qk.documentsAll (['documents']), which every documents key
      // hangs off. It used to be qk.documentsTree, a sibling of the search key —
      // hence the two extra invalidations that stood here, and a comment explaining
      // a prefix relationship that is now the opposite of the truth.
      //
      // Search results still matter for a PERSONAL pin specifically: personal pins
      // are deliberately not broadcast (D47), so nothing else refreshes them.
      void qc.invalidateQueries({ queryKey: qk.documentsAll })
      // A household pin changes what everyone sees, so the dashboard needs a refresh;
      // a personal pin only affects this user's widget, which the same key covers.
      void qc.invalidateQueries({ queryKey: qk.dashboard })
      setPinMenu(false)
      toast.success(vars.on ? cs.documents.pinned : cs.documents.unpinned)
    },
    onError: () => toast.error(cs.documents.saveError),
  })

  if (doc.isLoading) {
    return (
      <div className="grid min-h-[240px] flex-1 place-items-center">
        <Spinner />
      </div>
    )
  }
  // Only a 404 means the document is really gone. The 502 the backend returns while
  // object storage is unavailable, and a plain network drop, are transient — telling
  // the reader someone deleted it, with no way back, would be a lie. DokumentyPage
  // draws the same line when it resolves a slug path.
  if (doc.isError && !(doc.error instanceof ApiError && doc.error.status === 404)) {
    return (
      <div className="grid min-h-[240px] flex-1 place-items-center px-6 text-center">
        <div>
          <div className="mb-1.5 text-base font-bold">{cs.common.errorTitle}</div>
          <Button className="mt-3" onClick={() => void doc.refetch()}>
            {cs.common.retry}
          </Button>
        </div>
      </div>
    )
  }
  if (doc.isError || !doc.data) {
    return (
      <div className="grid min-h-[240px] flex-1 place-items-center px-6 text-center">
        <div>
          <div className="mb-1.5 text-base font-bold">{cs.documents.goneTitle}</div>
          <p className="mb-4 text-sm text-muted text-pretty">{cs.documents.goneBody}</p>
          {onGone && <Button onClick={onGone}>{cs.documents.cancel}</Button>}
        </div>
      </div>
    )
  }

  const d = doc.data
  const meta = kindMeta(d.content_type)
  const mode = viewerMode(d.content_type, d.preview_kind, d.preview_status)
  // Root-level fallback names the root the document actually lives in (carrier
  // discipline — see rootLabel in DokumentyPage): the document's own visibility
  // is the scope here, since this view has no page scope of its own.
  const folderLabel = d.path?.length
    ? d.path.map((p) => p.name).join(' / ')
    : d.visibility === 'private'
      ? cs.privacy.privateDocuments
      : cs.documents.root

  const copyLink = async () => {
    // The PERMANENT link (D42) — id-based, so it survives renames and moves. Absolute
    // so it works when pasted into a chat, and household-only either way.
    //
    // The server's own permalink is the source: a deploy that sets
    // HOME_DOCS_PUBLIC_BASE_URL returns it already absolute (and pointing at the
    // public host, which need not be this origin). Rebuilding the path here would
    // silently ignore that setting. `new URL` leaves an absolute value alone and
    // absolutises the plain "/d/{id}" against this origin.
    const url = new URL(d.urls.permalink, window.location.origin).toString()
    try {
      await navigator.clipboard.writeText(url)
      toast.success(cs.documents.copyLinkDone)
    } catch {
      toast.error(cs.documents.copyLinkError)
    }
    setPinMenu(false)
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {/* header: path, type, title, pins, actions */}
      <div className="flex-none border-b border-border px-4 py-3 md:px-5">
        <div className="flex flex-wrap items-start gap-3">
          <div className="min-w-[170px] flex-1">
            <div className="mb-1.5 truncate font-mono text-[11px] text-subtle">{folderLabel}</div>
            <div className="flex items-start gap-3">
              <span
                className={cn(
                  'grid h-[38px] w-[38px] flex-none place-items-center rounded-[9px] font-mono text-[10.5px] font-bold',
                  meta.tile,
                )}
                aria-hidden
              >
                {meta.icon}
              </span>
              <div className="min-w-0">
                <h2 className="text-lg font-extrabold leading-tight tracking-tight break-words text-pretty">{d.title}</h2>
                <div className="mt-0.5 font-mono text-[12px] text-muted">
                  {meta.label} · {fmtBytes(d.byte_size)} · {fmtDateTime(d.updated_at)}
                </div>
              </div>
            </div>
            <div className="mt-2.5 flex flex-wrap items-center gap-1.5">
              {d.pinned.household && (
                <span className="inline-flex h-6 items-center gap-1.5 rounded-full bg-accent-soft px-2.5 text-[11.5px] font-bold text-accent">
                  <span className="h-2 w-2 rounded-full bg-accent" aria-hidden />
                  {cs.documents.badgeHousehold}
                </span>
              )}
              {d.pinned.personal && (
                <span className="inline-flex h-6 items-center gap-1.5 rounded-full bg-s3 px-2.5 text-[11.5px] font-bold text-muted">
                  <span className="h-2 w-2 rounded-full border border-muted" aria-hidden />
                  {cs.documents.badgePersonal}
                </span>
              )}
            </div>
          </div>

          <div className="relative flex flex-none flex-wrap items-center justify-end gap-1.5">
            {/* An <a>, not a button: the browser must handle the attachment response
                itself (and the header decides the filename). */}
            <a href={d.urls.download} download className={buttonClass('primary', 'sm')}>
              <Download size={14} aria-hidden /> {cs.documents.download}
            </a>
            {editable && onRename && (
              <Button
                variant="secondary"
                size="sm"
                className="w-9 px-0"
                title={cs.documents.rename}
                aria-label={cs.documents.rename}
                onClick={() => onRename(d)}
              >
                <Pencil size={14} aria-hidden />
              </Button>
            )}
            <Button variant="secondary" size="sm" title={cs.documents.copyLinkTitle} onClick={copyLink}>
              <Link2 size={14} className="text-accent" aria-hidden /> {cs.documents.copyLink}
            </Button>
            <Button
              variant={d.pinned.household || d.pinned.personal ? 'primary' : 'secondary'}
              size="sm"
              aria-haspopup="true"
              aria-expanded={pinMenu}
              onClick={() => setPinMenu((o) => !o)}
            >
              <Pin size={14} aria-hidden /> {d.pinned.household || d.pinned.personal ? cs.documents.pinned : cs.documents.pin}
            </Button>

            {pinMenu && (
              <>
                <button
                  type="button"
                  className="fixed inset-0 z-20 cursor-default"
                  aria-label={cs.documents.cancel}
                  onClick={() => setPinMenu(false)}
                />
                <div className="absolute right-0 top-10 z-30 w-[236px] rounded-xl border border-border-strong bg-s1 p-1.5 shadow-[var(--shadow)]">
                  {editable && (
                    <PinOption
                      checked={d.pinned.household}
                      label={cs.documents.pinHousehold}
                      // ⚠ On a private document "pro všechny" is UNAVAILABLE AND
                      // EXPLAINED, never silently absent (D183). Hiding it leaves
                      // the member wondering what they did wrong.
                      hint={
                        d.visibility === 'private'
                          ? cs.privacy.householdPinUnavailable
                          : cs.documents.pinHouseholdHint
                      }
                      disabled={d.visibility === 'private'}
                      onClick={() => pin.mutate({ scope: 'household', on: !d.pinned.household })}
                    />
                  )}
                  {/* A personal pin is a view preference, so even a reader may set it. */}
                  <PinOption
                    checked={d.pinned.personal}
                    label={cs.documents.pinPersonal}
                    hint={cs.documents.pinPersonalHint}
                    onClick={() => pin.mutate({ scope: 'personal', on: !d.pinned.personal })}
                  />
                  {/* Publish, in the item's own menu beside the visibility
                      controls it belongs with (HANDOFF-design §v9 §3). */}
                  {onPublish && d.visibility === 'private' && editable && (
                    <>
                      <div className="my-1 h-px bg-border" />
                      <button
                        type="button"
                        onClick={() => {
                          setPinMenu(false)
                          onPublish?.(d)
                        }}
                        className="flex w-full items-center gap-2.5 rounded-lg border border-vis-private bg-vis-private-soft px-2.5 py-2 text-left text-[13px] font-bold hover:brightness-110"
                      >
                        <Upload size={14} className="flex-none" aria-hidden />
                        {cs.privacy.publish}
                      </button>
                    </>
                  )}
                  <p className="mt-1.5 border-t border-border px-1.5 pt-2 text-[10.5px] leading-snug text-subtle">
                    {cs.documents.copyLinkNote}
                  </p>
                </div>
              </>
            )}
          </div>
        </div>

        {d.description && <p className="mt-2.5 text-[13px] leading-relaxed text-muted text-pretty">{d.description}</p>}
      </div>

      {/* toolbar: immutability note + organise actions */}
      {editable ? (
        <div className="flex flex-none flex-wrap items-center gap-2.5 px-4 py-2.5 md:px-5">
          <span className="inline-flex h-7 items-center gap-1.5 rounded-full border border-border bg-s2 px-2.5 font-mono text-[11px] text-muted">
            ◈ {cs.documents.immutable}
          </span>
          <div className="min-w-2 flex-1" />
          {onMove && (
            <Button variant="secondary" size="sm" onClick={() => onMove(d)}>
              <MoveRight size={14} aria-hidden /> {cs.documents.move}
            </Button>
          )}
          {onDelete && (
            <Button
              variant="danger"
              size="sm"
              className="w-8 px-0"
              title={cs.documents.delete}
              aria-label={cs.documents.delete}
              onClick={() => onDelete(d)}
            >
              <Trash2 size={14} aria-hidden />
            </Button>
          )}
        </div>
      ) : (
        <div className="flex flex-none items-center gap-2.5 px-4 py-2.5 md:px-5">
          <span className="inline-flex h-7 items-center gap-1.5 rounded-full border border-border bg-s3 px-2.5 text-[12px] font-semibold text-muted">
            {cs.documents.readOnly}
          </span>
          <span className="font-mono text-[11px] text-subtle">{cs.documents.readOnlyHint}</span>
        </div>
      )}

      {/* preview region */}
      <div className="om-scroll min-h-0 flex-1 overflow-auto bg-bg px-4 pb-6 pt-4 md:px-5">
        <DocumentPreview doc={d} mode={mode} />
      </div>
    </div>
  )
}

function PinOption({
  checked,
  label,
  hint,
  onClick,
  disabled,
}: {
  checked: boolean
  label: string
  hint: string
  onClick: () => void
  /** v9: a household pin on a private document — disabled AND explained (D183). */
  disabled?: boolean
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={checked}
      disabled={disabled}
      className={cn(
        'flex w-full items-center gap-2.5 rounded-lg px-2.5 py-2 text-left text-[13px] text-fg',
        disabled ? 'cursor-not-allowed opacity-60' : 'hover:bg-s3',
      )}
    >
      <span
        className={cn(
          'grid h-5 w-5 flex-none place-items-center rounded-md border-[1.5px]',
          checked ? 'border-accent bg-accent text-accent-fg' : 'border-border-strong',
        )}
        aria-hidden
      >
        {checked && <Check size={12} />}
      </span>
      <span className="flex-1">
        <span className="block font-semibold">{label}</span>
        <span className="block text-[11px] text-subtle">{hint}</span>
      </span>
    </button>
  )
}

// ---- preview renderers ----

function DocumentPreview({ doc, mode }: { doc: DocumentDetail; mode: ReturnType<typeof viewerMode> }) {
  switch (mode) {
    case 'pdf':
      return <PdfPreview doc={doc} />
    case 'image':
      return (
        <div className="flex flex-col items-center gap-3">
          <img
            src={doc.urls.raw}
            alt={doc.title}
            className="max-h-[70vh] w-auto max-w-full rounded-[10px] border border-border-strong bg-s2 object-contain"
          />
          <p className="font-mono text-[11px] text-subtle md:hidden">{cs.documents.previewPinchHint}</p>
        </div>
      )
    case 'text':
      return <TextPreview doc={doc} />
    case 'pending':
      return (
        <div className="grid min-h-[280px] place-items-center">
          <div className="max-w-sm text-center">
            <Spinner className="mx-auto mb-4 h-10 w-10" />
            <div className="mb-1.5 text-[15px] font-bold">{cs.documents.previewPendingTitle}</div>
            <p className="text-[13px] text-muted text-pretty">{cs.documents.previewPendingBody}</p>
            <a href={doc.urls.download} download className={buttonClass('secondary', 'md', 'mt-4')}>
              <Download size={16} aria-hidden /> {cs.documents.downloadOriginal}
            </a>
          </div>
        </div>
      )
    default:
      return <TypeCard doc={doc} body={mode === 'failed' ? cs.documents.previewFailedBody : cs.documents.previewNoneBody} />
  }
}

// PdfPreview renders a PDF (or a derived Office→PDF) in a SANDBOXED iframe.
//
// The sandbox is `allow-scripts` and deliberately NOT `allow-same-origin`: the
// browser's built-in PDF viewer is script-driven and shows nothing without the
// former, while withholding the latter keeps the frame in an opaque origin, unable
// to touch home's cookies or DOM. The backend sets a matching CSP on the response.
//
// Some browsers still refuse to render a PDF in a sandboxed frame, so an explicit
// "open in a new tab" fallback sits under the frame — the user is never stuck.
function PdfPreview({ doc }: { doc: DocumentDetail }) {
  return (
    <div className="flex h-full min-h-[420px] flex-col gap-2">
      <iframe
        src={doc.urls.preview}
        title={`${cs.documents.previewFrameLabel}: ${doc.title}`}
        sandbox="allow-scripts"
        className="h-[65vh] min-h-[360px] w-full rounded-[10px] border border-border-strong bg-s2"
      />
      <div className="flex items-center justify-center gap-2">
        <a
          href={doc.urls.preview}
          target="_blank"
          rel="noreferrer"
          className="inline-flex items-center gap-1.5 font-mono text-[11px] text-muted underline hover:text-fg"
        >
          <ArrowUpRight size={12} aria-hidden />
          {cs.documents.previewOpenInTab}
        </a>
      </div>
    </div>
  )
}

// maxInlineTextBytes caps what the text viewer will pull into the page. Plain text,
// Markdown and CSV are all "natively previewable", so this path is taken for the
// whole family however big the file is — and a 40 MB CSV export is a perfectly legal
// upload, well under the 50 MB cap. Fetching it whole and laying it out in one node
// locks the tab up for the reader, who cannot even reach the Stáhnout button. Past
// the cap the type card is the honest answer, exactly as for a type we cannot render.
const maxInlineTextBytes = 2 * 1024 * 1024

// TextPreview fetches the bytes and renders them itself: Markdown through the
// shared sanitised renderer, plain text as escaped text. The bytes never become a
// document in home's origin.
function TextPreview({ doc }: { doc: DocumentDetail }) {
  const [text, setText] = useState<string | null>(null)
  const [failed, setFailed] = useState(false)
  const cancelled = useRef(false)
  const tooLarge = doc.byte_size > maxInlineTextBytes

  useEffect(() => {
    cancelled.current = false
    setText(null)
    setFailed(false)
    if (tooLarge) return
    fetch(doc.urls.raw, { credentials: 'include' })
      .then((r) => {
        if (!r.ok) throw new Error(String(r.status))
        return r.text()
      })
      .then((t) => {
        if (!cancelled.current) setText(t)
      })
      .catch(() => {
        if (!cancelled.current) setFailed(true)
      })
    return () => {
      cancelled.current = true
    }
  }, [doc.urls.raw, tooLarge])

  if (tooLarge) {
    return <TypeCard doc={doc} body={cs.documents.previewTooLargeBody(maxInlineTextBytes / (1024 * 1024))} />
  }
  if (failed) return <TypeCard doc={doc} body={cs.documents.previewFailedBody} />
  if (text === null) {
    return (
      <div className="grid min-h-[200px] place-items-center">
        <Spinner />
      </div>
    )
  }
  const isMarkdown = doc.content_type.startsWith('text/markdown')
  return (
    <div className="mx-auto max-w-[640px] rounded-[11px] border border-border bg-s1 px-5 py-4">
      {isMarkdown ? (
        <MarkdownView>{text}</MarkdownView>
      ) : (
        <pre className="whitespace-pre-wrap break-words font-mono text-[13px] leading-relaxed text-fg">{text}</pre>
      )}
    </div>
  )
}

// TypeCard is the honest "no preview" state: the type, the size, and a download
// button — never a broken frame.
function TypeCard({ doc, body }: { doc: DocumentDetail; body: string }) {
  const meta = kindMeta(doc.content_type)
  return (
    <div className="grid min-h-[280px] place-items-center">
      <div className="max-w-sm rounded-2xl border border-border bg-s1 px-6 py-7 text-center">
        <div className={cn('mx-auto mb-4 grid h-[60px] w-[60px] place-items-center rounded-[13px] font-mono text-sm font-bold', meta.tile)}>
          {meta.icon}
        </div>
        <div className="mb-1 break-words text-[15px] font-bold">{doc.title}</div>
        <div className="mb-3 font-mono text-[11.5px] text-muted">
          {meta.label} · {fmtBytes(doc.byte_size)}
        </div>
        <p className="mb-4 text-[13px] text-muted text-pretty">{body}</p>
        <a href={doc.urls.download} download className={buttonClass('primary')}>
          <Download size={16} aria-hidden /> {cs.documents.download}
        </a>
      </div>
    </div>
  )
}
