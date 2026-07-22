import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  DndContext,
  DragOverlay,
  PointerSensor,
  useDraggable,
  useDroppable,
  useSensor,
  useSensors,
  type DragEndEvent,
  type DragStartEvent,
} from '@dnd-kit/core'
import * as Popover from '@radix-ui/react-popover'
import { toast } from 'sonner'
import { ChevronDown, ChevronLeft, GripVertical, Plus } from 'lucide-react'
import { qk } from '@/api/keys'
import * as api from '@/api/endpoints'
import type { BoardTree, Card, Label } from '@/api/types'
import { between } from '@/lib/lexorank'
import { useIsDesktop } from '@/hooks/useMediaQuery'
import { useAuth } from '@/app/auth'
import { cn } from '@/lib/utils'
import { count, PLURAL } from '@/i18n/plural'
import { Button, Input, Spinner } from '@/components/ui/ui'
import { ScreenHeader } from '@/components/common/states'
import { CardDetail } from '@/components/common/CardDetail'
import { useCollapse } from './useCollapse'

export function UkolyPage() {
  const { canWrite } = useAuth()
  const boards = useQuery({ queryKey: qk.boards, queryFn: api.listBoards })
  const [boardId, setBoardId] = useState<string | null>(null)
  const activeBoardId = boardId ?? boards.data?.[0]?.id ?? null

  return (
    <div className="mx-auto max-w-6xl">
      <ScreenHeader title="Úkoly" subtitle="Trello-style nástěnka úkolů" />
      {boards.isLoading ? (
        <div className="grid place-items-center py-16">
          <Spinner />
        </div>
      ) : !activeBoardId ? (
        <p className="text-sm text-muted">Zatím žádná nástěnka.</p>
      ) : (
        <>
          <BoardSwitcher boards={boards.data ?? []} activeId={activeBoardId} onSelect={setBoardId} />
          <BoardView key={activeBoardId} boardId={activeBoardId} canWrite={canWrite} />
        </>
      )}
    </div>
  )
}

function BoardSwitcher({ boards, activeId, onSelect }: { boards: { id: string; name: string }[]; activeId: string; onSelect: (id: string) => void }) {
  if (boards.length <= 1) return null
  return (
    <div className="mb-4 flex flex-wrap gap-1.5">
      {boards.map((b) => (
        <button
          key={b.id}
          type="button"
          onClick={() => onSelect(b.id)}
          className={cn(
            'rounded-md border px-3 py-1.5 text-sm font-semibold',
            b.id === activeId ? 'border-accent bg-accent-soft text-fg' : 'border-border bg-s2 text-muted hover:text-fg',
          )}
        >
          {b.name}
        </button>
      ))}
    </div>
  )
}

function BoardView({ boardId, canWrite }: { boardId: string; canWrite: boolean }) {
  const qc = useQueryClient()
  const desktop = useIsDesktop()
  const { collapsed, toggle } = useCollapse(boardId)
  const [q, setQ] = useState('')
  const [labelFilter, setLabelFilter] = useState<string[]>([])
  const [includeArchived, setIncludeArchived] = useState(false)
  const [openCardId, setOpenCardId] = useState<string | null>(null)
  const [dragCard, setDragCard] = useState<Card | null>(null)

  const filters = { q: q || undefined, label: labelFilter.length ? labelFilter : undefined, include_archived: includeArchived || undefined }
  const treeKey = qk.boardTree(boardId, filters)
  const tree = useQuery({ queryKey: treeKey, queryFn: () => api.getBoardTree(boardId, filters) })
  const labels = useQuery({ queryKey: qk.boardLabels(boardId), queryFn: () => api.listLabels(boardId) })

  const allColumns = useMemo(() => tree.data?.columns.map((c) => c.column) ?? [], [tree.data])
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 6 } }))

  const moveMut = useMutation({
    mutationFn: (v: { cardId: string; columnId: string; position?: string }) =>
      api.moveCard(v.cardId, { column_id: v.columnId, position: v.position }),
    onMutate: async (v) => {
      await qc.cancelQueries({ queryKey: treeKey })
      const prev = qc.getQueryData<BoardTree>(treeKey)
      qc.setQueryData<BoardTree>(treeKey, (old) => (old ? relocateCard(old, v.cardId, v.columnId) : old))
      return { prev }
    },
    onError: (_e, _v, ctx) => {
      if (ctx?.prev) qc.setQueryData(treeKey, ctx.prev)
      toast.error('Přesun se nezdařil', { description: 'Změna byla vrácena.' })
    },
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: ['board', boardId] })
      void qc.invalidateQueries({ queryKey: qk.dashboard })
    },
  })

  const addMut = useMutation({
    mutationFn: (v: { columnId: string; title: string }) => api.createCard(v.columnId, { title: v.title }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['board', boardId] })
      void qc.invalidateQueries({ queryKey: qk.dashboard })
    },
    onError: () => toast.error('Kartu se nepodařilo vytvořit'),
  })

  function onDragStart(e: DragStartEvent) {
    const id = String(e.active.id)
    const card = tree.data?.columns.flatMap((c) => c.cards).find((c) => c.id === id) ?? null
    setDragCard(card)
  }
  function onDragEnd(e: DragEndEvent) {
    setDragCard(null)
    const { active, over } = e
    if (!over) return
    const cardId = String(active.id)
    const targetColumnId = String(over.id)
    const fromColumnId = (active.data.current as { columnId?: string } | undefined)?.columnId
    if (!fromColumnId || fromColumnId === targetColumnId) return // cross-column moves only (reorder via drag is a future enhancement)
    const targetCards = tree.data?.columns.find((c) => c.column.id === targetColumnId)?.cards ?? []
    const lastPos = targetCards.length ? targetCards[targetCards.length - 1].position : ''
    moveMut.mutate({ cardId, columnId: targetColumnId, position: between(lastPos, '') })
  }

  if (tree.isLoading) {
    return (
      <div className="grid place-items-center py-16">
        <Spinner />
      </div>
    )
  }
  if (tree.isError || !tree.data) {
    return <p className="text-sm text-danger">Nepodařilo se načíst nástěnku.</p>
  }

  const orderedColumns = tree.data.columns.slice().sort((a, b) => a.column.priority - b.column.priority)

  return (
    <>
      <FilterBar
        q={q}
        onQ={setQ}
        labels={labels.data ?? []}
        selected={labelFilter}
        onToggleLabel={(id) => setLabelFilter((s) => (s.includes(id) ? s.filter((x) => x !== id) : [...s, id]))}
        includeArchived={includeArchived}
        onToggleArchived={() => setIncludeArchived((v) => !v)}
      />

      <DndContext sensors={sensors} onDragStart={onDragStart} onDragEnd={onDragEnd}>
        <div className={cn(desktop ? 'flex gap-4 overflow-x-auto pb-2 om-scroll' : 'flex flex-col gap-4')}>
          {orderedColumns.map(({ column, cards }) => (
            <ColumnView
              key={column.id}
              column={column}
              cards={cards}
              desktop={desktop}
              canWrite={canWrite}
              columns={allColumns}
              collapsed={collapsed.has(column.id)}
              onToggleCollapse={() => toggle(column.id)}
              onOpenCard={setOpenCardId}
              onMove={(cardId, columnId) => moveMut.mutate({ cardId, columnId })}
              onQuickAdd={(title) => addMut.mutate({ columnId: column.id, title })}
            />
          ))}
        </div>
        <DragOverlay>
          {dragCard ? <div className="rounded-md border border-accent bg-s3 p-2.5 text-sm font-medium shadow-lg">{dragCard.title}</div> : null}
        </DragOverlay>
      </DndContext>

      {openCardId && (
        <CardDetail cardId={openCardId} boardId={boardId} open onOpenChange={(o) => !o && setOpenCardId(null)} readOnly={!canWrite} />
      )}
    </>
  )
}

function FilterBar({
  q,
  onQ,
  labels,
  selected,
  onToggleLabel,
  includeArchived,
  onToggleArchived,
}: {
  q: string
  onQ: (v: string) => void
  labels: Label[]
  selected: string[]
  onToggleLabel: (id: string) => void
  includeArchived: boolean
  onToggleArchived: () => void
}) {
  return (
    <div className="mb-4 space-y-2">
      <div className="flex flex-wrap items-center gap-2">
        <Input value={q} onChange={(e) => onQ(e.target.value)} placeholder="Hledat karty…" className="max-w-xs" aria-label="Hledat karty" />
        <label className="flex items-center gap-2 text-sm text-muted">
          <input type="checkbox" checked={includeArchived} onChange={onToggleArchived} className="h-4 w-4" />
          Zobrazit archivované
        </label>
      </div>
      {labels.length > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {labels.map((l) => (
            <button
              key={l.id}
              type="button"
              onClick={() => onToggleLabel(l.id)}
              className={cn(
                'inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-[12px] font-semibold',
                selected.includes(l.id) ? 'border-transparent text-fg' : 'border-border text-muted hover:text-fg',
              )}
              style={selected.includes(l.id) ? { backgroundColor: `color-mix(in oklch, ${l.color} 22%, var(--s2))` } : undefined}
            >
              <span className="h-2 w-2 rounded-full" style={{ backgroundColor: l.color }} />
              {l.name}
            </button>
          ))}
        </div>
      )}
    </div>
  )
}

function ColumnView({
  column,
  cards,
  desktop,
  canWrite,
  columns,
  collapsed,
  onToggleCollapse,
  onOpenCard,
  onMove,
  onQuickAdd,
}: {
  column: { id: string; name: string; kind: string }
  cards: Card[]
  desktop: boolean
  canWrite: boolean
  columns: { id: string; name: string }[]
  collapsed: boolean
  onToggleCollapse: () => void
  onOpenCard: (id: string) => void
  onMove: (cardId: string, columnId: string) => void
  onQuickAdd: (title: string) => void
}) {
  const isNow = column.kind === 'now'
  const isDone = column.kind === 'done'
  const { setNodeRef, isOver } = useDroppable({ id: column.id })

  if (collapsed && desktop) {
    return (
      <button
        type="button"
        onClick={onToggleCollapse}
        className={cn('flex w-10 flex-none flex-col items-center gap-2 rounded-lg border bg-s1 py-3', isNow ? 'border-now/60' : 'border-border')}
        aria-label={`Rozbalit sloupec ${column.name}`}
      >
        <span className="text-[11px] text-subtle">{cards.length}</span>
        <span className="[writing-mode:vertical-rl] rotate-180 text-sm font-bold">{column.name}</span>
      </button>
    )
  }

  return (
    <section
      ref={setNodeRef}
      className={cn(
        'flex flex-col rounded-lg border bg-s1',
        desktop ? 'max-h-[calc(100vh-18rem)] w-80 flex-none' : 'w-full',
        isNow ? 'border-now/60' : 'border-border',
        isOver && 'ring-2 ring-accent',
      )}
    >
      <header className="flex items-center gap-2 px-3 py-2.5">
        {isNow && <span className="h-2 w-2 rounded-full bg-now" aria-hidden />}
        <h2 className="flex-1 truncate text-sm font-bold">{column.name}</h2>
        <span className="text-[12px] text-subtle">{count(cards.length, PLURAL.cards)}</span>
        <button type="button" onClick={onToggleCollapse} className="text-subtle hover:text-fg" aria-label={`Sbalit sloupec ${column.name}`}>
          <ChevronLeft size={15} aria-hidden />
        </button>
      </header>
      {!collapsed && (
        <div className="min-h-0 flex-1 space-y-2 overflow-y-auto px-3 pb-3 om-scroll">
          {cards.map((card) => (
            <CardTile
              key={card.id}
              card={card}
              canWrite={canWrite}
              columns={columns}
              currentColumnId={column.id}
              onOpen={() => onOpenCard(card.id)}
              onMove={onMove}
              done={isDone}
            />
          ))}
          {cards.length === 0 && <p className="py-3 text-center text-[13px] text-subtle">Prázdné</p>}
          {canWrite && <QuickAdd onAdd={onQuickAdd} />}
        </div>
      )}
    </section>
  )
}

function CardTile({
  card,
  canWrite,
  columns,
  currentColumnId,
  onOpen,
  onMove,
  done,
}: {
  card: Card
  canWrite: boolean
  columns: { id: string; name: string }[]
  currentColumnId: string
  onOpen: () => void
  onMove: (cardId: string, columnId: string) => void
  done: boolean
}) {
  const prog = card.checklist_progress
  const { attributes, listeners, setNodeRef, transform, isDragging } = useDraggable({
    id: card.id,
    data: { columnId: currentColumnId },
    disabled: !canWrite,
  })
  return (
    <div
      ref={setNodeRef}
      style={transform ? { transform: `translate3d(${transform.x}px, ${transform.y}px, 0)` } : undefined}
      className={cn('rounded-md border border-border bg-s2 p-2.5 hover:border-border-strong', isDragging && 'opacity-40')}
    >
      <div className="flex items-start gap-1.5">
        {canWrite && (
          <button
            type="button"
            className="mt-0.5 flex-none cursor-grab text-subtle hover:text-muted active:cursor-grabbing"
            aria-label="Přetáhnout kartu"
            {...attributes}
            {...listeners}
          >
            <GripVertical size={14} aria-hidden />
          </button>
        )}
        <button type="button" onClick={onOpen} className="block min-w-0 flex-1 text-left">
          <div className={cn('text-sm font-medium text-fg', done && 'text-muted line-through')}>{card.title}</div>
          <div className="mt-1 flex flex-wrap items-center gap-2 text-[11px] text-subtle">
            {card.label_ids.length > 0 && <span>🏷 {card.label_ids.length}</span>}
            {prog.total > 0 && (
              <span>
                ☑ {prog.done}/{prog.total}
              </span>
            )}
            {card.notes && <span>📝</span>}
          </div>
        </button>
      </div>
      {canWrite && (
        <div className="mt-2 pl-5">
          <MoveTo columns={columns} currentColumnId={currentColumnId} onPick={(colId) => onMove(card.id, colId)} />
        </div>
      )}
    </div>
  )
}

function MoveTo({ columns, currentColumnId, onPick }: { columns: { id: string; name: string }[]; currentColumnId: string; onPick: (columnId: string) => void }) {
  const [open, setOpen] = useState(false)
  const targets = columns.filter((c) => c.id !== currentColumnId)
  if (targets.length === 0) return null
  return (
    <Popover.Root open={open} onOpenChange={setOpen}>
      <Popover.Trigger asChild>
        <button type="button" className="inline-flex items-center gap-1 rounded-md border border-border bg-s1 px-2 py-1 text-[12px] font-semibold text-muted hover:text-fg">
          Přesunout do… <ChevronDown size={13} aria-hidden />
        </button>
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Content sideOffset={4} className="z-50 w-56 rounded-md border border-border bg-s3 p-1 shadow-[var(--shadow)]">
          {targets.map((t) => (
            <button
              key={t.id}
              type="button"
              onClick={() => {
                onPick(t.id)
                setOpen(false)
              }}
              className="block w-full truncate rounded px-2 py-1.5 text-left text-sm text-fg hover:bg-accent-soft"
            >
              {t.name}
            </button>
          ))}
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  )
}

function QuickAdd({ onAdd }: { onAdd: (title: string) => void }) {
  const [adding, setAdding] = useState(false)
  const [title, setTitle] = useState('')
  if (!adding) {
    return (
      <button
        type="button"
        onClick={() => setAdding(true)}
        className="flex w-full items-center gap-1.5 rounded-md px-2 py-1.5 text-[13px] font-semibold text-subtle hover:bg-s2 hover:text-fg"
      >
        <Plus size={14} aria-hidden /> Přidat kartu
      </button>
    )
  }
  return (
    <form
      onSubmit={(e) => {
        e.preventDefault()
        const t = title.trim()
        if (t) {
          onAdd(t)
          setTitle('')
        }
      }}
      className="space-y-1.5"
    >
      <Input value={title} onChange={(e) => setTitle(e.target.value)} placeholder="Název karty…" autoFocus onBlur={() => !title && setAdding(false)} className="h-9" />
      <div className="flex gap-1.5">
        <Button size="sm" type="submit" variant="primary">
          Přidat
        </Button>
        <Button size="sm" variant="ghost" onClick={() => { setAdding(false); setTitle('') }}>
          Zrušit
        </Button>
      </div>
    </form>
  )
}

function relocateCard(tree: BoardTree, cardId: string, targetColumnId: string): BoardTree {
  let moved: Card | undefined
  const columns = tree.columns.map((col) => {
    const idx = col.cards.findIndex((c) => c.id === cardId)
    if (idx >= 0) {
      moved = col.cards[idx]
      return { ...col, cards: col.cards.filter((c) => c.id !== cardId) }
    }
    return col
  })
  if (!moved) return tree
  return {
    ...tree,
    columns: columns.map((col) =>
      col.column.id === targetColumnId ? { ...col, cards: [...col.cards, { ...moved!, column_id: targetColumnId }] } : col,
    ),
  }
}
