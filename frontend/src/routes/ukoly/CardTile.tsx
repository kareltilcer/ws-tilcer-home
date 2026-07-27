import { useSortable } from '@dnd-kit/sortable'
import { CSS } from '@dnd-kit/utilities'
import { ArrowLeftRight, GripVertical } from 'lucide-react'
import type { Card, Label } from '@/api/types'
import { cn } from '@/lib/utils'
import { fmtDate } from '@/i18n/format'

// CardTile — the board card (design/v1/CardTile.dc.html). A dnd-kit sortable:
// drag is gated to the grip so touch scrolling and card taps stay intact, while
// the grip carries the sortable listeners (pointer + keyboard). The explicit
// "Přesunout do…" (⇄) button is the primary keyboard/touch move path.
export function CardTile({
  card,
  labels,
  canWrite,
  done,
  pending = false,
  onOpen,
  onMove,
}: {
  card: Card
  labels: Label[]
  canWrite: boolean
  done: boolean
  pending?: boolean
  onOpen: () => void
  onMove: () => void
}) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({
    id: card.id,
    data: { type: 'card', columnId: card.column_id },
    disabled: !canWrite,
  })

  const prog = card.checklist_progress
  const pct = prog.total ? Math.round((prog.done / prog.total) * 100) : 0
  const chips = card.label_ids
    .map((id) => labels.find((l) => l.id === id))
    .filter((l): l is Label => Boolean(l))
  // label_ids come with the card; the labels list is fetched separately and may
  // still be loading/erroring. Show a count for any ids we can't resolve yet so a
  // labelled card never renders as unlabelled.
  const unresolvedLabels = card.label_ids.length - chips.length

  return (
    <div
      ref={setNodeRef}
      style={{ transform: CSS.Transform.toString(transform), transition }}
      className={cn(
        'rounded-md border border-border bg-s2 p-2.5 transition-colors hover:border-border-strong',
        isDragging && 'opacity-40',
        pending && 'opacity-60',
      )}
    >
      <div className="flex items-start gap-1.5">
        {canWrite && (
          <button
            type="button"
            className="mt-0.5 flex-none cursor-grab touch-none rounded text-subtle hover:text-muted focus-visible:outline focus-visible:outline-2 focus-visible:outline-focus active:cursor-grabbing"
            aria-label={`Přetáhnout kartu ${card.title}`}
            {...attributes}
            {...listeners}
          >
            <GripVertical size={14} aria-hidden />
          </button>
        )}
        <button type="button" onClick={onOpen} className="block min-w-0 flex-1 text-left">
          <span className={cn('text-sm font-semibold leading-snug text-fg [word-break:break-word]', done && 'text-muted line-through')}>
            {card.title}
          </span>
        </button>
        {canWrite && (
          <button
            type="button"
            onClick={onMove}
            aria-label={`Přesunout do… — ${card.title}`}
            title="Přesunout do…"
            className="-mr-0.5 -mt-0.5 grid h-8 w-8 flex-none place-items-center rounded-md border border-border bg-s3 text-muted hover:text-fg"
          >
            <ArrowLeftRight size={14} aria-hidden />
          </button>
        )}
      </div>

      {/* Chips + metadata also open the card, restoring the whole-body tap target.
          tabIndex -1 keeps it out of the tab order — the title button above is the
          keyboard/AT open control. */}
      <button type="button" onClick={onOpen} tabIndex={-1} aria-label={`Otevřít kartu ${card.title}`} className="block w-full text-left">
        {(chips.length > 0 || unresolvedLabels > 0) && (
          <div className="mt-2 flex flex-wrap gap-1">
            {chips.map((l) => (
              <span
                key={l.id}
                className="inline-flex items-center gap-1 rounded-[5px] px-1.5 py-0.5 text-[11px] font-semibold"
                style={{ backgroundColor: `color-mix(in oklch, ${l.color} 16%, transparent)`, color: l.color }}
              >
                <span className="h-[7px] w-[7px] flex-none rounded-full" style={{ backgroundColor: l.color }} />
                {l.name}
              </span>
            ))}
            {unresolvedLabels > 0 && (
              <span className="inline-flex items-center gap-1 rounded-[5px] bg-s3 px-1.5 py-0.5 text-[11px] font-semibold text-subtle" title="Štítky">
                🏷 {unresolvedLabels}
              </span>
            )}
          </div>
        )}

        <div className="mt-2 flex min-h-[14px] flex-wrap items-center gap-3 font-mono text-[11px] text-subtle">
          {prog.total > 0 && (
            <span className="inline-flex items-center gap-1.5" title="Postup checklistu">
              <span className="inline-block h-1 w-8 overflow-hidden rounded-full bg-s4">
                <span className="block h-full bg-good" style={{ width: `${pct}%` }} />
              </span>
              {prog.done}/{prog.total}
            </span>
          )}
          {card.notes && <span title="Má poznámky">≡</span>}
          {card.link_count > 0 && <span title="Odkazy">↗ {card.link_count}</span>}
          {card.done_at && <span className="text-good">✓ hotovo {fmtDate(new Date(card.done_at))}</span>}
          {pending && <span className="text-warn">ukládám…</span>}
        </div>
      </button>
    </div>
  )
}
