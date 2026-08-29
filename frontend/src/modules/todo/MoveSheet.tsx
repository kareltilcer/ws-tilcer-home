import type { ColumnKind } from '@/api/types'
import { ResponsiveModal } from '@/components/ui/modal'

type Target = { id: string; name: string; kind: ColumnKind }

const dotColor = (k: ColumnKind) => (k === 'now' ? 'var(--now)' : k === 'done' ? 'var(--good)' : 'var(--border-strong)')
const kindLabel = (k: ColumnKind) => (k === 'now' ? 'právě dělám' : k === 'done' ? 'hotovo' : '')

// MoveSheet — the explicit "Přesunout kartu do…" control (v1, PRD §7). `now` and
// `done` columns surface first as "Rychlé cíle"; the rest follow. This is the
// keyboard/touch-primary alternative to dragging.
export function MoveSheet({
  open,
  onOpenChange,
  cardTitle,
  columns,
  currentColumnId,
  onPick,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  cardTitle: string
  columns: Target[]
  currentColumnId: string
  onPick: (columnId: string) => void
}) {
  const others = columns.filter((c) => c.id !== currentColumnId)
  const quick = others.filter((c) => c.kind === 'now' || c.kind === 'done')
  const rest = others.filter((c) => c.kind === 'normal')

  const Row = ({ t, showKind }: { t: Target; showKind?: boolean }) => (
    <button
      type="button"
      onClick={() => {
        onPick(t.id)
        onOpenChange(false)
      }}
      className="flex w-full items-center gap-2.5 rounded-md px-2.5 py-2.5 text-left hover:bg-accent-soft"
    >
      <span className="h-2.5 w-2.5 flex-none rounded-[3px]" style={{ backgroundColor: dotColor(t.kind) }} />
      <span className="min-w-0 flex-1 truncate text-sm font-semibold text-fg">{t.name}</span>
      {showKind && kindLabel(t.kind) && (
        <span aria-hidden className="flex-none font-mono text-[11px] text-muted">
          {kindLabel(t.kind)}
        </span>
      )}
    </button>
  )

  return (
    <ResponsiveModal open={open} onOpenChange={onOpenChange} title="Přesunout kartu do…">
      <p className="mb-2 truncate text-sm font-semibold text-fg">{cardTitle}</p>
      {others.length === 0 ? (
        <p className="py-4 text-center text-sm text-subtle">Žádný jiný sloupec.</p>
      ) : (
        <div className="space-y-0.5">
          {quick.length > 0 && (
            <>
              <p className="px-1 pb-1 pt-1 font-mono text-[10px] uppercase tracking-wide text-subtle">Rychlé cíle</p>
              {quick.map((t) => (
                <Row key={t.id} t={t} showKind />
              ))}
              {rest.length > 0 && <div className="my-1.5 h-px bg-border" />}
            </>
          )}
          {rest.map((t) => (
            <Row key={t.id} t={t} />
          ))}
        </div>
      )}
    </ResponsiveModal>
  )
}
