import type { PripnutePoznamkyWidget as PripnuteData } from '@/api/types'
import { PinScopeBadge, WidgetEmpty, type WidgetComponentProps } from '@/platform/widgets/shared'
import { PrivateMark } from '@/components/common/RootSwitcher'
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
        return (
          <li key={p.note_id}>
            <button
              type="button"
              onClick={() => onOpenNote(p.note_id)}
              className="flex w-full items-start gap-3 rounded-lg border border-border bg-s2 p-3 text-left hover:border-border-strong"
            >
              <span className={cn('mt-1 h-2.5 w-2.5 flex-none rounded-full', personal ? 'border border-muted' : 'bg-accent')} />
              <span className="min-w-0 flex-1">
                <span className="flex items-center gap-2">
                  <span className="min-w-0 break-words text-sm font-semibold text-fg">{p.title}</span>
                  {/* A private row carries the lock AND the word (D183). The
                      widget sits on Nástěnka, outside either tree, so nothing
                      else on the row says the note is only visible to its
                      owner. */}
                  {p.visibility === 'private' && <PrivateMark />}
                </span>
                {p.excerpt && <span className="mt-0.5 block truncate text-[12px] text-muted">{p.excerpt}</span>}
              </span>
              <PinScopeBadge
                scope={p.scope}
                labels={{
                  personal: cs.notes.badgePersonal,
                  household: cs.notes.badgeHousehold,
                  both: cs.notes.badgeBoth,
                }}
              />
            </button>
          </li>
        )
      })}
    </ul>
  )
}
