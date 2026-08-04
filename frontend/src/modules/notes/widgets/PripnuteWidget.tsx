import type { PripnutePoznamkyWidget as PripnuteData } from '@/api/types'
import { WidgetEmpty, type WidgetComponentProps } from '@/platform/widgets/shared'
import { cs } from '@/i18n/cs'
import { cn } from '@/lib/utils'

// PripnuteWidget renders the notes.pripnute payload: household pins ∪ the caller's
// personal pins, de-duplicated (FR-P8). A row opens the note in an overlay ON
// Nástěnka (onOpenNote) — it never navigates to Poznámky. There is NO done gesture
// — notes aren't completed.
export function PripnuteWidget({ data, onOpenNote }: WidgetComponentProps) {
  const notes = (data as PripnuteData | undefined)?.notes ?? []
  if (notes.length === 0) return <WidgetEmpty text={cs.notes.widgetEmpty} />
  return (
    <ul className="space-y-2">
      {notes.map((p) => {
        const personal = p.scope === 'personal'
        // Three distinct scopes: personal-only, household-only, and both. Collapsing
        // household into the "both" badge discards the backend's scope distinction.
        const badge =
          p.scope === 'personal' ? cs.notes.badgePersonal : p.scope === 'household' ? cs.notes.badgeHousehold : cs.notes.badgeBoth
        return (
          <li key={p.note_id}>
            <button
              type="button"
              onClick={() => onOpenNote(p.note_id)}
              className="flex w-full items-start gap-3 rounded-lg border border-border bg-s2 p-3 text-left hover:border-border-strong"
            >
              <span className={cn('mt-1 h-2.5 w-2.5 flex-none rounded-full', personal ? 'border border-muted' : 'bg-accent')} />
              <span className="min-w-0 flex-1">
                <span className="block break-words text-sm font-semibold text-fg">{p.title}</span>
                {p.excerpt && <span className="mt-0.5 block truncate text-[12px] text-muted">{p.excerpt}</span>}
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
