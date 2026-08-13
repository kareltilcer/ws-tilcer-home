import { useCallback, useRef, useState } from 'react'
import { Check, X } from 'lucide-react'
import { ApiError } from '@/api/client'
import * as api from '@/api/endpoints'
import { cs } from '@/i18n/cs'
import { cn } from '@/lib/utils'
import { fmtBytes } from '@/i18n/format'
import { kindMeta } from './fileKind'

// The upload cap is the SERVER's (HOME_DOCS_MAX_UPLOAD_MB) and arrives on the tree
// response; checking it on the client too means a 60 MB file is refused instantly
// instead of after a long upload that ends in a 413. This constant is only the
// fallback for the moment before the tree has loaded — hardcoding the real limit
// would silently refuse valid files as soon as an operator raised the server's.
export const DEFAULT_MAX_UPLOAD_MB = 50

type ItemState = 'uploading' | 'done' | 'error'

export interface UploadItem {
  id: string
  name: string
  size: number
  contentType: string
  state: ItemState
  progress: number
  error?: string
  /** preview_status of the created document, so a queued row can say "náhled se připravuje". */
  previewPending?: boolean
}

let seq = 0
const nextId = () => `u${++seq}`

/** useUploadQueue owns the multi-file upload queue: per-file progress, the
 *  client-side size pre-check, and the tree refresh once a file lands. */
export function useUploadQueue(opts: { folderId: string | null; onUploaded: () => void; maxUploadMB?: number }) {
  const [items, setItems] = useState<UploadItem[]>([])
  const [open, setOpen] = useState(false)
  // Kept in a ref so a queue started in one folder keeps uploading there even if the
  // user navigates away mid-upload.
  const folderRef = useRef(opts.folderId)
  folderRef.current = opts.folderId
  const capRef = useRef(opts.maxUploadMB ?? DEFAULT_MAX_UPLOAD_MB)
  capRef.current = opts.maxUploadMB ?? DEFAULT_MAX_UPLOAD_MB

  const patch = useCallback((id: string, changes: Partial<UploadItem>) => {
    setItems((prev) => prev.map((it) => (it.id === id ? { ...it, ...changes } : it)))
  }, [])

  const enqueue = useCallback(
    (files: File[]) => {
      if (files.length === 0) return
      setOpen(true)
      const targetFolder = folderRef.current
      const maxMB = capRef.current
      for (const file of files) {
        const id = nextId()
        const item: UploadItem = {
          id,
          name: file.name,
          size: file.size,
          contentType: file.type,
          state: 'uploading',
          progress: 0,
        }
        // Refuse an over-cap file up front: the server would 413 it anyway, and this
        // way the user is told immediately, before waiting through the transfer.
        if (file.size > maxMB * 1024 * 1024) {
          setItems((prev) => [...prev, { ...item, state: 'error', error: cs.documents.uploadTooLarge(maxMB) }])
          continue
        }
        setItems((prev) => [...prev, item])
        void api
          .uploadDocument(file, { folder_id: targetFolder }, (fraction) => patch(id, { progress: fraction }))
          .then((doc) => {
            patch(id, { state: 'done', progress: 1, previewPending: doc.preview_status === 'pending' })
            opts.onUploaded()
          })
          .catch((err: unknown) => {
            // 415 is the type allowlist; everything else gets the generic message plus
            // whatever detail the server supplied.
            let message: string = cs.documents.uploadFailed
            if (err instanceof ApiError) {
              if (err.status === 413) message = cs.documents.uploadTooLarge(maxMB)
              else if (err.status === 415) message = cs.documents.uploadRejectedType
              else if (err.detail) message = `${cs.documents.uploadFailed}: ${err.detail}`
            }
            patch(id, { state: 'error', error: message })
          })
      }
    },
    [opts, patch],
  )

  const remove = useCallback((id: string) => setItems((prev) => prev.filter((it) => it.id !== id)), [])
  const close = useCallback(() => {
    setOpen(false)
    // Everything goes, failures included. The panel is only mounted while `open` is
    // true, so a retained error row would not be readable anyway — it would just sit
    // there until the next enqueue reopened the panel and pinned a stale "nepodařilo
    // se nahrát" next to a file that is uploading fine. The X is an explicit dismissal
    // of rows the user has just been looking at, and each error row has its own X for
    // clearing one at a time.
    setItems([])
  }, [])

  const active = items.filter((it) => it.state === 'uploading').length
  return { items, open: open && items.length > 0, active, enqueue, remove, close }
}

/** UploadPanel is the floating queue: one row per file with progress, success, or
 *  an error explaining itself. */
export function UploadPanel({
  items,
  active,
  onRemove,
  onClose,
}: {
  items: UploadItem[]
  active: number
  onRemove: (id: string) => void
  onClose: () => void
}) {
  const done = items.filter((it) => it.state === 'done').length
  const failed = items.filter((it) => it.state === 'error').length
  const summary = active > 0 ? `${done}/${items.length}` : failed > 0 ? `${failed}×` : cs.documents.uploadDone

  return (
    <div className="fixed inset-x-4 bottom-24 z-30 overflow-hidden rounded-2xl border border-border-strong bg-s1 shadow-[var(--shadow)] md:inset-x-auto md:bottom-5 md:right-5 md:w-[360px]">
      <div className="flex items-center gap-2.5 border-b border-border px-3.5 py-3">
        <div className="flex-1">
          <div className="text-[13.5px] font-bold">{cs.documents.uploadHeading}</div>
          <div className="mt-0.5 font-mono text-[11px] text-subtle">{summary}</div>
        </div>
        <button
          type="button"
          onClick={onClose}
          aria-label={cs.documents.uploadCloseWhenDone}
          className="grid h-8 w-8 place-items-center rounded-md text-muted hover:bg-s2 hover:text-fg"
        >
          <X size={16} aria-hidden />
        </button>
      </div>
      <ul className="om-scroll max-h-[280px] space-y-2 overflow-y-auto p-3">
        {items.map((it) => {
          const meta = kindMeta(it.contentType || 'application/octet-stream')
          return (
            <li key={it.id} className="rounded-xl border border-border bg-s2 px-3 py-2.5">
              <div className="flex items-center gap-2.5">
                <span
                  className={cn('grid h-[30px] w-[30px] flex-none place-items-center rounded-lg font-mono text-[8.5px] font-bold', meta.tile)}
                  aria-hidden
                >
                  {meta.icon}
                </span>
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-[12.5px] font-semibold">{it.name}</span>
                  <span className="block font-mono text-[10.5px] text-subtle">{fmtBytes(it.size)}</span>
                </span>
                {it.state === 'done' && <Check size={16} className="flex-none text-good" aria-label={cs.documents.uploadDone} />}
                {it.state === 'error' && (
                  <button
                    type="button"
                    onClick={() => onRemove(it.id)}
                    aria-label={cs.documents.uploadRemove}
                    className="grid h-7 w-7 flex-none place-items-center rounded-md text-muted hover:bg-s3 hover:text-fg"
                  >
                    <X size={14} aria-hidden />
                  </button>
                )}
              </div>
              {it.state === 'uploading' && (
                <div
                  className="mt-2 h-1.5 overflow-hidden rounded-full bg-s3"
                  role="progressbar"
                  aria-valuenow={Math.round(it.progress * 100)}
                  aria-valuemin={0}
                  aria-valuemax={100}
                  aria-label={it.name}
                >
                  {/* motion-safe keeps the bar still for prefers-reduced-motion users */}
                  <span
                    className="block h-full bg-accent motion-safe:transition-[width] motion-safe:duration-300"
                    style={{ width: `${Math.round(it.progress * 100)}%` }}
                  />
                </div>
              )}
              {it.state === 'done' && it.previewPending && (
                <div className="mt-1.5 text-[11px] text-muted">{cs.documents.uploadPreviewPending}</div>
              )}
              {it.state === 'error' && <div className="mt-1.5 text-[11px] text-danger text-pretty">{it.error}</div>}
            </li>
          )
        })}
      </ul>
    </div>
  )
}

/** useFileDrop wires desktop drag-and-drop onto a container. Mobile uses the
 *  picker button instead, which the same enqueue feeds. */
export function useFileDrop(onFiles: (files: File[]) => void) {
  const [dragging, setDragging] = useState(false)
  const depth = useRef(0)

  return {
    dragging,
    handlers: {
      onDragEnter: (e: React.DragEvent) => {
        if (!e.dataTransfer.types.includes('Files')) return
        e.preventDefault()
        depth.current += 1
        setDragging(true)
      },
      onDragOver: (e: React.DragEvent) => {
        if (!e.dataTransfer.types.includes('Files')) return
        e.preventDefault() // required, or the browser navigates to the dropped file
      },
      onDragLeave: () => {
        depth.current = Math.max(0, depth.current - 1)
        if (depth.current === 0) setDragging(false)
      },
      onDrop: (e: React.DragEvent) => {
        if (!e.dataTransfer.types.includes('Files')) return
        e.preventDefault()
        depth.current = 0
        setDragging(false)
        onFiles(Array.from(e.dataTransfer.files))
      },
    },
  }
}
