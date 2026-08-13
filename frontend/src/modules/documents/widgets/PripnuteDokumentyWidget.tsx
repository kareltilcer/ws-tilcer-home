import type { PripnuteDokumentyWidget as PripnuteData } from '@/api/types'
import { WidgetEmpty, type WidgetComponentProps } from '@/platform/widgets/shared'
import { cs } from '@/i18n/cs'
import { cn } from '@/lib/utils'
import { fmtBytes } from '@/i18n/format'
import { kindMeta, statusChip } from '@/routes/dokumenty/fileKind'

// PripnuteDokumentyWidget renders the documents.pripnute payload: household pins ∪
// the caller's personal pins, de-duplicated (FR-DOC11). A row opens the document in
// a PREVIEW OVERLAY on Nástěnka (onOpenDocument) — it never navigates to Dokumenty,
// which is the explicit requirement. There is no done gesture: documents are not
// completed.
export function PripnuteDokumentyWidget({ data, onOpenDocument }: WidgetComponentProps) {
  const documents = (data as PripnuteData | undefined)?.documents ?? []
  if (documents.length === 0) return <WidgetEmpty text={cs.documents.widgetEmpty} />
  return (
    <ul className="space-y-2">
      {documents.map((p) => {
        const meta = kindMeta(p.content_type)
        const chip = statusChip(p.content_type, p.preview_kind, p.preview_status)
        const personal = p.scope === 'personal'
        // Three distinct scopes: personal-only, household-only, and both. Collapsing
        // household into "both" would discard the backend's scope distinction.
        const badge =
          p.scope === 'personal'
            ? cs.documents.badgePersonal
            : p.scope === 'household'
              ? cs.documents.badgeHousehold
              : cs.documents.badgeBoth
        return (
          <li key={p.document_id}>
            <button
              type="button"
              onClick={() => onOpenDocument(p.document_id)}
              className="flex w-full items-center gap-3 rounded-lg border border-border bg-s2 p-3 text-left hover:border-border-strong"
            >
              {p.thumbnail_url ? (
                <img
                  src={p.thumbnail_url}
                  alt=""
                  className="h-9 w-9 flex-none rounded-lg border border-border object-cover"
                  loading="lazy"
                />
              ) : (
                <span
                  className={cn('grid h-9 w-9 flex-none place-items-center rounded-lg font-mono text-[9px] font-bold', meta.tile)}
                  aria-hidden
                >
                  {meta.icon}
                </span>
              )}
              <span className="min-w-0 flex-1">
                <span className="block truncate text-sm font-semibold text-fg">{p.title}</span>
                <span className="mt-0.5 flex flex-wrap items-center gap-x-2 font-mono text-[11px] text-subtle">
                  <span>{fmtBytes(p.byte_size)}</span>
                  {chip && (
                    <>
                      <span aria-hidden>·</span>
                      <span>{chip.label}</span>
                    </>
                  )}
                </span>
              </span>
              <span
                className={cn(
                  'inline-flex h-5 flex-none items-center rounded-full px-2 text-[10.5px] font-bold',
                  personal ? 'bg-s3 text-muted' : 'bg-accent-soft text-accent',
                )}
              >
                {badge}
              </span>
            </button>
          </li>
        )
      })}
    </ul>
  )
}
