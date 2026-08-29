import type { PreviewKind, PreviewStatus } from '@/api/types'
import { cs } from '@/i18n/cs'

// File-type presentation, ported from the v4 design bundle (docKindMeta /
// docPreviewKind / docStatusChip in design/v4/Home.dc.html). One place decides
// what a document looks like in the tree, the list, the grid, the widget, and the
// viewer, so a PDF is the same red "PDF" tile everywhere.

export type FileKind = 'pdf' | 'image' | 'doc' | 'sheet' | 'slides' | 'text' | 'md' | 'other'

export interface KindMeta {
  /** Short monospace badge shown in the type tile (design uses text, not icons). */
  icon: string
  /** Czech type name for the metadata line. */
  label: string
  /** Tailwind classes for the tile, from the theme tokens. */
  tile: string
}

const META: Record<FileKind, KindMeta> = {
  pdf: { icon: 'PDF', label: 'PDF', tile: 'bg-danger/15 text-danger' },
  image: { icon: 'IMG', label: 'Obrázek', tile: 'bg-good/15 text-good' },
  doc: { icon: 'DOCX', label: 'Word', tile: 'bg-accent/15 text-accent' },
  sheet: { icon: 'XLSX', label: 'Excel', tile: 'bg-good/15 text-good' },
  slides: { icon: 'PPTX', label: 'Prezentace', tile: 'bg-warn/15 text-warn' },
  text: { icon: 'TXT', label: 'Text', tile: 'bg-s3 text-muted' },
  md: { icon: 'MD', label: 'Markdown', tile: 'bg-s3 text-muted' },
  other: { icon: 'SOUB', label: 'Soubor', tile: 'bg-s3 text-subtle' },
}

/** fileKind classifies a content type for display. */
export function fileKind(contentType: string): FileKind {
  const base = contentType.split(';')[0].trim().toLowerCase()
  if (base === 'application/pdf') return 'pdf'
  if (base.startsWith('image/')) return 'image'
  if (base === 'text/markdown') return 'md'
  if (base.startsWith('text/')) return 'text'
  if (
    base.includes('wordprocessingml') ||
    base === 'application/msword' ||
    base === 'application/rtf' ||
    base.includes('opendocument.text')
  ) {
    return 'doc'
  }
  if (base.includes('spreadsheetml') || base === 'application/vnd.ms-excel' || base.includes('opendocument.spreadsheet')) return 'sheet'
  if (base.includes('presentationml') || base === 'application/vnd.ms-powerpoint' || base.includes('opendocument.presentation')) {
    return 'slides'
  }
  return 'other'
}

export function kindMeta(contentType: string): KindMeta {
  return META[fileKind(contentType)]
}

/** How the viewer should render a document. */
export type ViewerMode = 'pdf' | 'image' | 'text' | 'pending' | 'failed' | 'download'

/** viewerMode maps the server's preview_kind/preview_status onto a renderer.
 *
 *  The three non-rendering modes are distinct on purpose: "pending" is temporary
 *  and resolves itself over the websocket, "failed" is permanent but harmless, and
 *  "download" means the type never had a preview. Each gets its own copy so the
 *  user knows whether to wait. */
export function viewerMode(contentType: string, kind: PreviewKind, statusValue: PreviewStatus): ViewerMode {
  if (kind === 'none') return 'download'
  if (statusValue === 'pending') return 'pending'
  if (statusValue === 'failed') return 'failed'
  if (kind === 'pdf') return 'pdf' // a derived Office→PDF preview
  // native: the original previews directly
  const k = fileKind(contentType)
  if (k === 'pdf') return 'pdf'
  if (k === 'image') return 'image'
  if (k === 'text' || k === 'md') return 'text'
  return 'download'
}

/** statusChip is the small badge a list/grid row shows when a document has no
 *  usable preview (or not yet). Returns null when there is nothing to say. */
export function statusChip(
  contentType: string,
  kind: PreviewKind,
  statusValue: PreviewStatus,
): { label: string; className: string } | null {
  switch (viewerMode(contentType, kind, statusValue)) {
    case 'pending':
      return { label: cs.documents.chipPending, className: 'bg-warn/15 text-warn' }
    case 'failed':
      return { label: cs.documents.chipFailed, className: 'bg-s3 text-muted' }
    case 'download':
      return { label: cs.documents.chipDownloadOnly, className: 'bg-s3 text-muted' }
    default:
      return null
  }
}
