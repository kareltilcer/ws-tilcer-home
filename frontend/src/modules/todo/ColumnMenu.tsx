import { useEffect, useState } from 'react'
import type { Column, ColumnKind } from './api/types'
import { ResponsiveModal } from '@/components/ui/modal'
import { Button, Input } from '@/components/ui/ui'
import { KINDS, PRIORITIES } from './columnFields'
import { Segmented } from './Segmented'

// ColumnMenu — the per-column editor (v1 column context menu): rename, kind,
// priority, reorder, delete. Purely presentational; the board owns the mutations
// (delete confirms cascade when the column still holds cards).
export function ColumnMenu({
  open,
  onOpenChange,
  column,
  cardCount,
  canReorder,
  canMoveEarlier,
  canMoveLater,
  onRename,
  onSetKind,
  onSetPriority,
  onNudge,
  onDelete,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  column: Column
  cardCount: number
  canReorder: boolean
  canMoveEarlier: boolean
  canMoveLater: boolean
  onRename: (name: string) => void
  onSetKind: (kind: ColumnKind) => void
  onSetPriority: (priority: number) => void
  onNudge: (dir: -1 | 1) => void
  onDelete: () => void
}) {
  const [name, setName] = useState(column.name)
  useEffect(() => setName(column.name), [column.name])

  const saveName = () => {
    const t = name.trim()
    if (t && t !== column.name) onRename(t)
  }

  return (
    <ResponsiveModal open={open} onOpenChange={onOpenChange} title={column.name}>
      <div className="space-y-5">
        <Field label="Přejmenovat">
          <form
            className="flex gap-1.5"
            onSubmit={(e) => {
              e.preventDefault()
              saveName()
            }}
          >
            <Input value={name} onChange={(e) => setName(e.target.value)} aria-label="Název sloupce" className="h-9" />
            <Button size="sm" type="submit" variant="primary" className="flex-none">
              Uložit
            </Button>
          </form>
        </Field>

        <Field label="Druh">
          <Segmented options={KINDS} value={column.kind} onPick={onSetKind} />
        </Field>

        <Field label="Priorita">
          <Segmented options={PRIORITIES} value={column.priority} onPick={onSetPriority} />
        </Field>

        <Field label="Pořadí">
          {canReorder ? (
            <div className="flex gap-1.5">
              <Button size="sm" variant="secondary" className="flex-1" disabled={!canMoveEarlier} onClick={() => onNudge(-1)}>
                ◀ Vlevo
              </Button>
              <Button size="sm" variant="secondary" className="flex-1" disabled={!canMoveLater} onClick={() => onNudge(1)}>
                Vpravo ▶
              </Button>
            </div>
          ) : (
            <p className="text-[12.5px] text-muted">Dostupné jen v ručním řazení. Přepni řazení sloupců na „Ruční“.</p>
          )}
        </Field>

        <div className="border-t border-border pt-4">
          <Button variant="danger" className="w-full" onClick={onDelete}>
            Smazat sloupec{cardCount > 0 ? ` (${cardCount})` : ''}
          </Button>
        </div>
      </div>
    </ResponsiveModal>
  )
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <div className="mb-2 font-mono text-[10px] uppercase tracking-wide text-subtle">{label}</div>
      {children}
    </div>
  )
}
