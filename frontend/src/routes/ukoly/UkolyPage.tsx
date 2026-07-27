import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  DndContext,
  DragOverlay,
  KeyboardSensor,
  PointerSensor,
  closestCorners,
  useSensor,
  useSensors,
  type DragEndEvent,
  type DragStartEvent,
} from '@dnd-kit/core'
import {
  SortableContext,
  arrayMove,
  horizontalListSortingStrategy,
  sortableKeyboardCoordinates,
  useSortable,
  verticalListSortingStrategy,
} from '@dnd-kit/sortable'
import { CSS } from '@dnd-kit/utilities'
import { toast } from 'sonner'
import { ChevronDown, ChevronUp, GripVertical, MoreHorizontal, Plus } from 'lucide-react'
import { qk } from '@/api/keys'
import * as api from '@/api/endpoints'
import type { Board, BoardTree, Card, Column, ColumnKind, Label } from '@/api/types'
import { between, comparePositions } from '@/lib/lexorank'
import { useIsDesktop } from '@/hooks/useMediaQuery'
import { useAuth } from '@/app/auth'
import { cn } from '@/lib/utils'
import { count, PLURAL } from '@/i18n/plural'
import { Button, Input, Spinner } from '@/components/ui/ui'
import { ScreenHeader } from '@/components/common/states'
import { CardDetail } from '@/components/common/CardDetail'
import { CardTile } from './CardTile'
import { MoveSheet } from './MoveSheet'
import { ColumnMenu } from './ColumnMenu'
import { BoardToolbar } from './BoardToolbar'
import { BoardDialog } from './BoardDialog'
import { AddColumnModal } from './AddColumnModal'
import { LabelManager } from './LabelManager'
import { useCollapse } from './useCollapse'
import { orderColumns, pinNowFirst } from './ordering'
import { useBoardSort } from './useBoardSort'
import { useColumnMove } from './useColumnMove'

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
      ) : (
        <BoardView
          key={activeBoardId ?? 'none'}
          boards={boards.data ?? []}
          activeBoardId={activeBoardId}
          onSelectBoard={setBoardId}
          canWrite={canWrite}
        />
      )}
    </div>
  )
}

function BoardView({
  boards,
  activeBoardId,
  onSelectBoard,
  canWrite,
}: {
  boards: Board[]
  activeBoardId: string | null
  onSelectBoard: (id: string | null) => void
  canWrite: boolean
}) {
  const qc = useQueryClient()
  const desktop = useIsDesktop()
  const boardId = activeBoardId
  const keyBoardId = boardId ?? ''
  const isReader = !canWrite
  const { collapsed, toggle } = useCollapse(keyBoardId)
  const { mode: sortMode, setMode: setSortMode } = useBoardSort(keyBoardId)
  const [q, setQ] = useState('')
  const [labelFilter, setLabelFilter] = useState<string[]>([])
  const [includeArchived, setIncludeArchived] = useState(false)
  const [openCardId, setOpenCardId] = useState<string | null>(null)
  const [moveCardId, setMoveCardId] = useState<string | null>(null)
  const [menuColId, setMenuColId] = useState<string | null>(null)
  const [dragColId, setDragColId] = useState<string | null>(null)
  const [dragCard, setDragCard] = useState<Card | null>(null)
  const [boardDialog, setBoardDialog] = useState<'create' | 'rename' | null>(null)
  const [addColOpen, setAddColOpen] = useState(false)
  const [labelMgrOpen, setLabelMgrOpen] = useState(false)

  const filters = { q: q || undefined, label: labelFilter.length ? labelFilter : undefined, include_archived: includeArchived || undefined }
  const treeKey = qk.boardTree(keyBoardId, filters)
  const tree = useQuery({ queryKey: treeKey, queryFn: () => api.getBoardTree(boardId!, filters), enabled: !!boardId })
  const labels = useQuery({ queryKey: qk.boardLabels(keyBoardId), queryFn: () => api.listLabels(boardId!), enabled: !!boardId })

  const allColumns = useMemo(() => tree.data?.columns.map((c) => c.column) ?? [], [tree.data])
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 6 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  )

  const moveMut = useMutation({
    // position is the optimistic key for the local reorder. What the server receives:
    // beforeCardId (when set) anchors the insert before that card so the server derives
    // the key from the true stored order — correct under a filter that hides cards in
    // the gap; else serverPosition '' appends to the column's true tail; else the
    // client-computed position is used as-is.
    mutationFn: (v: { cardId: string; columnId: string; position: string; serverPosition?: string; beforeCardId?: string }) =>
      api.moveCard(v.cardId, { column_id: v.columnId, position: v.serverPosition ?? v.position, before_card_id: v.beforeCardId }),
    onMutate: async (v) => {
      await qc.cancelQueries({ queryKey: treeKey })
      const prev = qc.getQueryData<BoardTree>(treeKey)
      qc.setQueryData<BoardTree>(treeKey, (old) => (old ? relocateCard(old, v.cardId, v.columnId, v.position) : old))
      return { prev }
    },
    onError: (_e, _v, ctx) => {
      if (ctx?.prev) qc.setQueryData(treeKey, ctx.prev)
      toast.error('Přesun se nezdařil', { description: 'Změna byla vrácena.' })
    },
    onSettled: (_d, _e, v) => {
      // Tree only (all filter variants) — a move never changes label membership, so
      // don't refetch the labels list on the drag hot path. ['board', id] would also
      // match ['board', id, 'labels'].
      void qc.invalidateQueries({ queryKey: ['board', keyBoardId, 'tree'] })
      void qc.invalidateQueries({ queryKey: qk.dashboard })
      void qc.invalidateQueries({ queryKey: qk.card(v.cardId) }) // refresh an open CardDetail's column
    },
  })
  const pendingCardId = moveMut.isPending ? (moveMut.variables?.cardId ?? null) : null

  const addMut = useMutation({
    mutationFn: (v: { columnId: string; title: string }) => api.createCard(v.columnId, { title: v.title }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['board', boardId] })
      void qc.invalidateQueries({ queryKey: qk.dashboard })
    },
    onError: () => toast.error('Kartu se nepodařilo vytvořit'),
  })

  const invalidateBoard = () => {
    void qc.invalidateQueries({ queryKey: ['board', boardId] })
    void qc.invalidateQueries({ queryKey: qk.dashboard })
  }

  const updateColMut = useMutation({
    mutationFn: (v: { id: string; body: Partial<{ name: string; priority: number; kind: ColumnKind }> }) => api.updateColumn(v.id, v.body),
    onSuccess: invalidateBoard,
    onError: () => toast.error('Sloupec se nepodařilo upravit'),
  })
  const deleteColMut = useMutation({
    mutationFn: (v: { id: string; cascade: boolean }) => api.deleteColumn(v.id, v.cascade),
    onSuccess: () => {
      setMenuColId(null)
      invalidateBoard()
    },
    onError: () => toast.error('Sloupec se nepodařilo smazat'),
  })

  // Column reorder (drag handle on desktop, ▲▼ on mobile) — one lexorank write,
  // flipping the board back to manual sort so the user sees their arrangement.
  const columnMove = useColumnMove(keyBoardId, treeKey, () => setSortMode('manual'))

  const createBoardMut = useMutation({
    mutationFn: (name: string) => api.createBoard({ name }),
    onSuccess: (b) => {
      void qc.invalidateQueries({ queryKey: qk.boards })
      onSelectBoard(b.id)
    },
    onError: () => toast.error('Nástěnku se nepodařilo vytvořit'),
  })
  const renameBoardMut = useMutation({
    mutationFn: (v: { id: string; name: string }) => api.updateBoard(v.id, { name: v.name }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: qk.boards }),
    onError: () => toast.error('Nástěnku se nepodařilo přejmenovat'),
  })
  const deleteBoardMut = useMutation({
    mutationFn: (id: string) => api.deleteBoard(id),
    onSuccess: (_r, id) => {
      void qc.invalidateQueries({ queryKey: qk.boards })
      onSelectBoard(boards.find((b) => b.id !== id)?.id ?? null)
    },
    onError: () => toast.error('Nástěnku se nepodařilo smazat'),
  })
  const addColMut = useMutation({
    mutationFn: (v: { name: string; kind: ColumnKind; priority: number }) => api.createColumn(boardId!, v),
    onSuccess: invalidateBoard,
    onError: () => toast.error('Sloupec se nepodařilo vytvořit'),
  })

  // Append a card to the end of a target column — the explicit "Přesunout do…"
  // path, which always drops at the tail (v1). Drag computes precise positions.
  // serverPosition '' makes the backend compute the true tail: the last *visible*
  // card may not be the real last card when a filter is active, so we can't trust a
  // client-computed key here (the local one is only for optimistic ordering).
  function moveToColumnEnd(cardId: string, columnId: string) {
    const target = (qc.getQueryData<BoardTree>(treeKey) ?? tree.data)?.columns.find((c) => c.column.id === columnId)
    const last = target?.cards.length ? target.cards[target.cards.length - 1].position : ''
    moveMut.mutate({ cardId, columnId, position: between(last, ''), serverPosition: '' })
  }

  function onDragStart(e: DragStartEvent) {
    const d = e.active.data.current as { type?: string } | undefined
    if (d?.type === 'column') {
      setDragColId(String(e.active.id))
      return
    }
    const id = String(e.active.id)
    const card = tree.data?.columns.flatMap((c) => c.cards).find((c) => c.id === id) ?? null
    setDragCard(card)
  }
  function onDragCancel() {
    setDragCard(null)
    setDragColId(null)
  }
  function onDragEnd(e: DragEndEvent) {
    setDragCard(null)
    setDragColId(null)
    const { active, over } = e
    const data = tree.data
    if (!over || !data) return
    const activeData = active.data.current as { type?: string; columnId?: string } | undefined
    const overData = over.data.current as { type?: string; columnId?: string } | undefined

    // Column reorder: src is a column; `over` may resolve to a card, whose columnId
    // still names the target column. One lexorank write via useColumnMove (which
    // renumbers only when the displayed order diverges from the stored order).
    if (activeData?.type === 'column') {
      const srcColId = String(active.id)
      const targetColId = overData?.columnId
      if (!targetColId || targetColId === srcColId) return
      columnMove.moveTo(displayColumns, srcColId, targetColId)
      return
    }

    // Card move.
    const cardId = String(active.id)
    const fromColumnId = activeData?.columnId
    const toColumnId = overData?.columnId
    if (!fromColumnId || !toColumnId) return
    const overCardId = overData?.type === 'card' ? String(over.id) : null

    const toCol = data.columns.find((c) => c.column.id === toColumnId)
    if (!toCol) return

    // `next` is the position of the card the moved card lands before, or undefined
    // when it lands last; `anchorId` is that card's id. A last drop can't trust the
    // visible tail under a filter, so serverPosition '' defers to the backend's true
    // tail; a mid-insert sends the anchor so the backend computes the slot in true
    // stored order — correct even when the filter hides cards in the gap (see moveMut).
    let prev: string
    let next: string | undefined
    let anchorId: string | undefined
    if (fromColumnId === toColumnId) {
      // Reorder within the column — arrayMove gives the intuitive drop slot.
      const cards = toCol.cards
      const oldIndex = cards.findIndex((c) => c.id === cardId)
      const newIndex = overCardId ? cards.findIndex((c) => c.id === overCardId) : cards.length - 1
      if (oldIndex < 0 || newIndex < 0 || oldIndex === newIndex) return
      const order = arrayMove(cards, oldIndex, newIndex)
      const k = order.findIndex((c) => c.id === cardId)
      prev = order[k - 1]?.position ?? ''
      next = order[k + 1]?.position
      anchorId = order[k + 1]?.id
    } else {
      // Cross-column — insert before the hovered card, else append to the tail.
      const cards = toCol.cards
      const idx = overCardId ? cards.findIndex((c) => c.id === overCardId) : cards.length
      const at = idx < 0 ? cards.length : idx
      prev = cards[at - 1]?.position ?? ''
      next = cards[at]?.position
      anchorId = cards[at]?.id
    }
    const position = between(prev, next ?? '')
    moveMut.mutate({
      cardId,
      columnId: toColumnId,
      position,
      serverPosition: next === undefined ? '' : undefined,
      beforeCardId: next === undefined ? undefined : anchorId,
    })
  }

  const moveCard = moveCardId ? tree.data?.columns.flatMap((c) => c.cards).find((c) => c.id === moveCardId) ?? null : null

  const orderedColumns = tree.data ? orderColumns(tree.data.columns, sortMode) : []
  // displayColumns is what the user actually sees. Column indexing and reorder must
  // key off it, not orderedColumns, or the ▲▼/◀▶ controls act on a different order
  // than what is shown. Mobile pins `now` columns to the top only in the *priority*
  // view; in manual mode we respect the user's exact arrangement so the displayed
  // order equals the stored position order — otherwise a single nudge would look
  // like a full reorder to useColumnMove and renumber every column (pinning `now`
  // to the front board-wide, desktop included).
  const displayColumns = desktop || sortMode === 'manual' ? orderedColumns : pinNowFirst(orderedColumns)
  const menuIndex = menuColId ? displayColumns.findIndex((c) => c.column.id === menuColId) : -1
  const menuEntry = menuIndex >= 0 ? displayColumns[menuIndex] : null
  const activeBoardName = boards.find((b) => b.id === boardId)?.name ?? ''
  const hasFilter = !!q || labelFilter.length > 0 || includeArchived

  // Column reorder is a manual-order operation. In priority sort the displayed order
  // diverges from the stored order (and on mobile pins `now` columns first), so a
  // reorder there would renumber every column and bake that view into the persisted
  // order board-wide — including desktop. Gate reordering to manual sort and steer the
  // user to switch (#6). canReorder drives the drag handle, the mobile ▲▼ nudge, and
  // the ColumnMenu ◀▶ buttons.
  const canReorder = canWrite && sortMode === 'manual'
  const columnIds = displayColumns.map((c) => c.column.id)
  const dragColumn = dragColId ? displayColumns.find((c) => c.column.id === dragColId) ?? null : null

  const addColTile = canWrite ? (
    desktop ? (
      <div className="w-[266px] flex-none self-start">
        <button
          type="button"
          onClick={() => setAddColOpen(true)}
          className="flex h-[46px] w-full items-center justify-center gap-2 rounded-xl border border-dashed border-border-strong text-[13px] font-semibold text-muted hover:border-accent hover:text-fg"
        >
          <Plus size={15} aria-hidden /> Přidat sloupec
        </button>
      </div>
    ) : (
      <button
        type="button"
        onClick={() => setAddColOpen(true)}
        className="flex h-12 w-full items-center justify-center gap-2 rounded-xl border border-dashed border-border-strong text-[13.5px] font-semibold text-muted hover:border-accent hover:text-fg"
      >
        <Plus size={16} aria-hidden /> Přidat sloupec
      </button>
    )
  ) : null

  return (
    <>
      <BoardToolbar
        boards={boards}
        activeBoardId={boardId}
        canWrite={canWrite}
        isReader={isReader}
        sortMode={sortMode}
        onSelectBoard={onSelectBoard}
        onCreateBoard={() => setBoardDialog('create')}
        onRenameBoard={() => setBoardDialog('rename')}
        onManageLabels={() => setLabelMgrOpen(true)}
        onDeleteBoard={() => {
          if (boardId && window.confirm(`Smazat tabuli „${activeBoardName}“? Tuto akci nelze vzít zpět.`)) deleteBoardMut.mutate(boardId)
        }}
        onSetSort={setSortMode}
        onAddColumn={() => setAddColOpen(true)}
      />

      {boardId && (
        <FilterBar
          q={q}
          onQ={setQ}
          labels={labels.data ?? []}
          selected={labelFilter}
          onToggleLabel={(id) => setLabelFilter((s) => (s.includes(id) ? s.filter((x) => x !== id) : [...s, id]))}
          includeArchived={includeArchived}
          onToggleArchived={() => setIncludeArchived((v) => !v)}
          hasFilter={hasFilter}
          onClear={() => {
            setQ('')
            setLabelFilter([])
            setIncludeArchived(false)
          }}
          desktop={desktop}
        />
      )}

      {!boardId ? (
        <EmptyState
          title="Zatím žádná nástěnka"
          body="Vytvoř první tabuli a začni přidávat úkoly."
          action={canWrite ? { label: 'Nová nástěnka', onClick: () => setBoardDialog('create') } : undefined}
        />
      ) : tree.isLoading ? (
        <div className="grid place-items-center py-16">
          <Spinner />
        </div>
      ) : tree.isError || !tree.data ? (
        <div className="py-12 text-center">
          <p className="mb-3 text-sm text-danger">Nepodařilo se načíst nástěnku.</p>
          <Button variant="secondary" size="sm" onClick={() => void tree.refetch()}>
            Zkusit znovu
          </Button>
        </div>
      ) : orderedColumns.length === 0 ? (
        <EmptyState
          title={`Tabule „${activeBoardName}“ je zatím prázdná`}
          body="Začni prvním sloupcem — třeba „Zásobník“, „Právě dělám“ a „Hotovo“."
          action={canWrite ? { label: 'Přidat sloupec', onClick: () => setAddColOpen(true) } : undefined}
        />
      ) : (
        <>
          {canWrite && sortMode === 'priority' && (
            <p className="mb-3 rounded-lg border border-border bg-s1 px-3 py-2 text-[12.5px] text-muted">
              Sloupce jsou seřazené podle priority. Pro ruční uspořádání přepni řazení na <span className="font-semibold text-fg">Ruční</span>.
            </p>
          )}
          <DndContext sensors={sensors} collisionDetection={closestCorners} onDragStart={onDragStart} onDragEnd={onDragEnd} onDragCancel={onDragCancel}>
            <div className={cn(desktop ? 'flex items-stretch gap-4 overflow-x-auto pb-2 om-scroll' : 'flex flex-col gap-4')}>
              <SortableContext items={columnIds} strategy={desktop ? horizontalListSortingStrategy : verticalListSortingStrategy}>
                {displayColumns.map(({ column, cards }) => {
                  const oi = displayColumns.findIndex((c) => c.column.id === column.id)
                  return (
                    <ColumnView
                      key={column.id}
                      column={column}
                      cards={cards}
                      desktop={desktop}
                      canWrite={canWrite}
                      canReorder={canReorder}
                      labels={labels.data ?? []}
                      pendingCardId={pendingCardId}
                      collapsed={collapsed.has(column.id)}
                      isFirst={oi === 0}
                      isLast={oi === displayColumns.length - 1}
                      onToggleCollapse={() => toggle(column.id)}
                      onOpenCard={setOpenCardId}
                      onMoveCard={setMoveCardId}
                      onOpenMenu={() => setMenuColId(column.id)}
                      onQuickAdd={(title) => addMut.mutate({ columnId: column.id, title })}
                      onColNudge={(dir) => columnMove.nudge(displayColumns, column.id, dir)}
                    />
                  )
                })}
              </SortableContext>
              {addColTile}
            </div>
            <DragOverlay>
              {dragColumn ? (
                <div className="rounded-xl border border-accent bg-s2 px-3 py-2 text-sm font-bold shadow-lg">{dragColumn.column.name}</div>
              ) : dragCard ? (
                <div className="rounded-md border border-accent bg-s3 p-2.5 text-sm font-semibold shadow-lg">{dragCard.title}</div>
              ) : null}
            </DragOverlay>
          </DndContext>
        </>
      )}

      {openCardId && boardId && (
        <CardDetail cardId={openCardId} boardId={boardId} open onOpenChange={(o) => !o && setOpenCardId(null)} readOnly={!canWrite} />
      )}

      {moveCard && (
        <MoveSheet
          open
          onOpenChange={(o) => !o && setMoveCardId(null)}
          cardTitle={moveCard.title}
          columns={allColumns}
          currentColumnId={moveCard.column_id}
          onPick={(columnId) => moveToColumnEnd(moveCard.id, columnId)}
        />
      )}

      {menuEntry && (
        <ColumnMenu
          open
          onOpenChange={(o) => !o && setMenuColId(null)}
          column={menuEntry.column}
          cardCount={menuEntry.card_count}
          canReorder={canReorder}
          canMoveEarlier={menuIndex > 0}
          canMoveLater={menuIndex >= 0 && menuIndex < displayColumns.length - 1}
          onRename={(name) => updateColMut.mutate({ id: menuEntry.column.id, body: { name } })}
          onSetKind={(kind) => updateColMut.mutate({ id: menuEntry.column.id, body: { kind } })}
          onSetPriority={(priority) => updateColMut.mutate({ id: menuEntry.column.id, body: { priority } })}
          onNudge={(dir) => columnMove.nudge(displayColumns, menuEntry.column.id, dir)}
          onDelete={() => {
            // Use the column's true card count (card_count), not the filtered
            // cards array — otherwise an active filter would under-report the
            // cascade (or send cascade=false when cards are hidden, which the
            // backend rejects with 409).
            const n = menuEntry.card_count
            const msg =
              n > 0
                ? `Sloupec „${menuEntry.column.name}“ obsahuje ${count(n, PLURAL.cards)}. Smazat i s kartami?`
                : `Smazat sloupec „${menuEntry.column.name}“?`
            if (!window.confirm(msg)) return
            deleteColMut.mutate({ id: menuEntry.column.id, cascade: n > 0 })
          }}
        />
      )}

      {boardDialog && (
        <BoardDialog
          open
          onOpenChange={(o) => !o && setBoardDialog(null)}
          mode={boardDialog}
          initialName={boardDialog === 'rename' ? activeBoardName : ''}
          onSubmit={(name) => {
            if (boardDialog === 'create') createBoardMut.mutate(name)
            else if (boardId) renameBoardMut.mutate({ id: boardId, name })
          }}
        />
      )}
      {addColOpen && <AddColumnModal open onOpenChange={setAddColOpen} onSubmit={(v) => addColMut.mutate(v)} />}
      {labelMgrOpen && boardId && (
        <LabelManager open onOpenChange={setLabelMgrOpen} boardId={boardId} labels={labels.data ?? []} />
      )}
    </>
  )
}

function EmptyState({ title, body, action }: { title: string; body: string; action?: { label: string; onClick: () => void } }) {
  return (
    <div className="grid place-items-center py-16">
      <div className="max-w-md text-center">
        <div className="mx-auto mb-4 grid h-[52px] w-[52px] place-items-center rounded-xl border border-dashed border-border-strong bg-s2 text-2xl text-subtle">＋</div>
        <div className="mb-1.5 text-[17px] font-bold">{title}</div>
        <p className="mb-4 text-[13.5px] text-pretty text-muted">{body}</p>
        {action && (
          <Button variant="primary" onClick={action.onClick}>
            {action.label}
          </Button>
        )}
      </div>
    </div>
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
  hasFilter,
  onClear,
  desktop,
}: {
  q: string
  onQ: (v: string) => void
  labels: Label[]
  selected: string[]
  onToggleLabel: (id: string) => void
  includeArchived: boolean
  onToggleArchived: () => void
  hasFilter: boolean
  onClear: () => void
  desktop: boolean
}) {
  const [mOpen, setMOpen] = useState(false)
  const activeCount = (q ? 1 : 0) + selected.length + (includeArchived ? 1 : 0)

  const labelChips =
    labels.length > 0 ? (
      <div className="flex flex-wrap gap-1.5">
        {labels.map((l) => (
          <button
            key={l.id}
            type="button"
            onClick={() => onToggleLabel(l.id)}
            className={cn(
              'inline-flex items-center gap-1.5 rounded-lg border px-2.5 py-1 text-[12.5px] font-semibold',
              selected.includes(l.id) ? 'border-transparent text-fg' : 'border-border text-muted hover:text-fg',
            )}
            style={selected.includes(l.id) ? { backgroundColor: `color-mix(in oklch, ${l.color} 22%, var(--s2))` } : undefined}
          >
            <span className="h-2 w-2 rounded-full" style={{ backgroundColor: l.color }} />
            {l.name}
          </button>
        ))}
      </div>
    ) : null

  const archivedBtn = (
    <button
      type="button"
      onClick={onToggleArchived}
      className={cn(
        'h-9 rounded-lg border px-3 text-[12.5px] font-semibold',
        includeArchived ? 'border-accent bg-accent-soft text-fg' : 'border-border text-muted hover:text-fg',
      )}
    >
      {includeArchived ? '✓ ' : ''}Archivované
    </button>
  )

  if (desktop) {
    return (
      <div className="mb-4 flex flex-wrap items-center gap-2">
        <Input value={q} onChange={(e) => onQ(e.target.value)} placeholder="Hledat úkoly…" className="h-9 max-w-xs" aria-label="Hledat úkoly" />
        {labelChips}
        {archivedBtn}
        {hasFilter && (
          <button type="button" onClick={onClear} className="h-9 rounded-lg border border-border px-3 text-[12.5px] text-muted hover:text-fg">
            ✕ Zrušit filtr
          </button>
        )}
      </div>
    )
  }

  return (
    <div className="mb-4 space-y-2">
      <div className="flex gap-2">
        <Input value={q} onChange={(e) => onQ(e.target.value)} placeholder="Hledat…" className="h-10 flex-1" aria-label="Hledat úkoly" />
        <button
          type="button"
          onClick={() => setMOpen((v) => !v)}
          className="relative h-10 flex-none rounded-lg border border-border bg-s2 px-3.5 text-[13px] font-semibold text-fg"
        >
          Filtr
          {activeCount > 0 && (
            <span className="absolute -right-1.5 -top-1.5 grid h-[18px] min-w-[18px] place-items-center rounded-full bg-accent px-1 text-[10px] font-bold text-accent-fg">
              {activeCount}
            </span>
          )}
        </button>
      </div>
      {mOpen && (
        <div className="space-y-2.5 rounded-xl border border-border bg-s1 p-3">
          {labelChips}
          <div className="flex items-center gap-2">
            {archivedBtn}
            {hasFilter && (
              <button type="button" onClick={onClear} className="h-9 rounded-lg border border-border px-3 text-[12.5px] text-muted hover:text-fg">
                ✕ Zrušit filtr
              </button>
            )}
          </div>
        </div>
      )}
    </div>
  )
}

const PRIORITY_LABELS = ['nízká', 'střední', 'vysoká']

function ColumnView({
  column,
  cards,
  desktop,
  canWrite,
  canReorder,
  labels,
  pendingCardId,
  collapsed,
  isFirst,
  isLast,
  onToggleCollapse,
  onOpenCard,
  onMoveCard,
  onOpenMenu,
  onQuickAdd,
  onColNudge,
}: {
  column: Column
  cards: Card[]
  desktop: boolean
  canWrite: boolean
  canReorder: boolean
  labels: Label[]
  pendingCardId: string | null
  collapsed: boolean
  isFirst: boolean
  isLast: boolean
  onToggleCollapse: () => void
  onOpenCard: (id: string) => void
  onMoveCard: (id: string) => void
  onOpenMenu: () => void
  onQuickAdd: (title: string) => void
  onColNudge: (dir: -1 | 1) => void
}) {
  const isNow = column.kind === 'now'
  const isDone = column.kind === 'done'
  const cardIds = cards.map((c) => c.id)
  // The column is a dnd-kit sortable: dragging is gated to the header grip (only when
  // canReorder), while the droppable stays live at all times so cards can be dropped
  // onto the column even in priority view where column reordering is disabled.
  const { attributes, listeners, setNodeRef, setActivatorNodeRef, transform, transition, isDragging, isOver } = useSortable({
    id: column.id,
    data: { type: 'column', columnId: column.id },
    disabled: { draggable: !canReorder, droppable: false },
  })
  const sortableStyle = { transform: CSS.Transform.toString(transform), transition }

  const accent = isNow ? 'var(--now)' : isDone ? 'var(--good)' : 'var(--border-strong)'
  const nowTint = isNow
    ? { borderColor: 'color-mix(in oklch, var(--now) 45%, var(--border))', backgroundColor: 'color-mix(in oklch, var(--now) 7%, var(--s1))' }
    : undefined

  if (collapsed && desktop) {
    return (
      <button
        ref={setNodeRef}
        type="button"
        onClick={onToggleCollapse}
        style={{ ...nowTint, ...sortableStyle }}
        className={cn(
          'flex w-12 flex-none flex-col items-center gap-2.5 rounded-xl border border-border bg-s1 py-2.5',
          isOver && 'ring-2 ring-accent',
          isDragging && 'opacity-50',
        )}
        aria-label={`Rozbalit sloupec ${column.name}`}
      >
        <span className="h-2 w-2 flex-none rounded-full" style={{ background: accent }} />
        <span className="max-h-[280px] overflow-hidden text-ellipsis whitespace-nowrap text-sm font-bold [writing-mode:vertical-rl] rotate-180">{column.name}</span>
        <span className="grid h-[22px] min-w-[22px] place-items-center rounded-full bg-s3 px-1 font-mono text-[12px] text-muted">{cards.length}</span>
      </button>
    )
  }

  return (
    <section
      ref={setNodeRef}
      style={{ ...nowTint, ...sortableStyle }}
      className={cn(
        'flex flex-col overflow-hidden rounded-xl border border-border bg-s1',
        desktop ? 'max-h-[calc(100vh-16rem)] w-[304px] flex-none' : 'w-full',
        isOver && 'ring-2 ring-accent',
        isDragging && 'opacity-50',
      )}
    >
      <div className="h-[3px] flex-none" style={{ background: accent }} aria-hidden />

      <div className="flex items-center gap-1.5 px-2.5 pt-2.5">
        {desktop && canReorder && (
          <button
            type="button"
            ref={setActivatorNodeRef}
            {...attributes}
            {...listeners}
            title="Přetáhni pro změnu pořadí sloupce"
            aria-label={`Přesunout sloupec ${column.name}`}
            className="flex-none cursor-grab touch-none rounded text-subtle hover:text-fg focus-visible:outline focus-visible:outline-2 focus-visible:outline-focus active:cursor-grabbing"
          >
            <GripVertical size={14} aria-hidden />
          </button>
        )}
        <button
          type="button"
          onClick={onToggleCollapse}
          className="grid h-7 w-7 flex-none place-items-center rounded-md text-muted hover:bg-s3 hover:text-fg"
          aria-label={collapsed ? `Rozbalit sloupec ${column.name}` : `Sbalit sloupec ${column.name}`}
        >
          <ChevronDown size={15} aria-hidden className={cn('transition-transform', collapsed && '-rotate-90')} />
        </button>
        <h2 className="min-w-0 flex-1 truncate text-sm font-bold [word-break:break-word]">{column.name}</h2>
        {isNow && (
          <span
            className="flex-none rounded-full px-2 py-0.5 text-[10.5px] font-bold"
            style={{ backgroundColor: 'color-mix(in oklch, var(--now) 22%, transparent)', color: 'var(--now)' }}
          >
            TEĎ
          </span>
        )}
        <span className="grid h-5 min-w-5 flex-none place-items-center rounded-full bg-s3 px-1.5 font-mono text-[11px] text-muted">{cards.length}</span>
        {canWrite && (
          <button
            type="button"
            onClick={onOpenMenu}
            aria-label={`Možnosti sloupce ${column.name}`}
            title="Přejmenovat · priorita · druh · smazat"
            className="grid h-7 w-7 flex-none place-items-center rounded-md text-muted hover:bg-s3 hover:text-fg"
          >
            <MoreHorizontal size={16} aria-hidden />
          </button>
        )}
      </div>

      {!collapsed && (
        <>
          <div className="flex items-center gap-2 px-2.5 pb-1 pt-1.5 font-mono text-[11px] text-muted">
            <span>{count(cards.length, PLURAL.cards)}</span>
            {column.priority > 0 && (
              <span className="rounded-[5px] bg-s3 px-1.5 py-0.5 text-subtle">priorita: {PRIORITY_LABELS[column.priority]}</span>
            )}
          </div>

          {canWrite && (
            <div className="space-y-2 px-2.5 pb-2">
              {!desktop && canReorder && (
                <div className="flex items-center gap-1.5">
                  <span className="flex-1 font-mono text-[10px] uppercase tracking-wide text-subtle">Pořadí sloupce</span>
                  <button
                    type="button"
                    disabled={isFirst}
                    onClick={() => onColNudge(-1)}
                    aria-label={`Posunout sloupec ${column.name} nahoru`}
                    className="grid h-11 w-11 place-items-center rounded-lg border border-border bg-s3 text-fg disabled:opacity-40"
                  >
                    <ChevronUp size={16} aria-hidden />
                  </button>
                  <button
                    type="button"
                    disabled={isLast}
                    onClick={() => onColNudge(1)}
                    aria-label={`Posunout sloupec ${column.name} dolů`}
                    className="grid h-11 w-11 place-items-center rounded-lg border border-border bg-s3 text-fg disabled:opacity-40"
                  >
                    <ChevronDown size={16} aria-hidden />
                  </button>
                </div>
              )}
              <QuickAdd onAdd={onQuickAdd} />
            </div>
          )}

          <div className="min-h-[44px] flex-1 space-y-2 overflow-y-auto px-2.5 pb-3 om-scroll">
            <SortableContext items={cardIds} strategy={verticalListSortingStrategy}>
              {cards.map((card) => (
                <CardTile
                  key={card.id}
                  card={card}
                  labels={labels}
                  canWrite={canWrite}
                  done={isDone}
                  pending={pendingCardId === card.id}
                  onOpen={() => onOpenCard(card.id)}
                  onMove={() => onMoveCard(card.id)}
                />
              ))}
            </SortableContext>
            {cards.length === 0 && (
              <div className="rounded-lg border border-dashed border-border py-4 text-center text-[12px] text-subtle">
                Prázdný sloupec — přetáhni sem kartu
              </div>
            )}
          </div>
        </>
      )}
    </section>
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

// Optimistic move: pull the card from its column, restamp column + position, and
// reinsert into the target sorted by position (cards render in position order).
// card_count is adjusted alongside so the delete-cascade count stays honest during
// the optimistic window (a same-column reorder nets to zero: -1 then +1).
function relocateCard(tree: BoardTree, cardId: string, targetColumnId: string, position: string): BoardTree {
  let moved: Card | undefined
  const columns = tree.columns.map((col) => {
    const idx = col.cards.findIndex((c) => c.id === cardId)
    if (idx >= 0) {
      moved = col.cards[idx]
      return { ...col, cards: col.cards.filter((c) => c.id !== cardId), card_count: Math.max(0, col.card_count - 1) }
    }
    return col
  })
  if (!moved) return tree
  // Mirror the backend's done stamping (store.MoveCard): entering a done column
  // stamps done_at (keeping any existing stamp), leaving one clears it. Without
  // this the optimistic card sits in a done column with no "✓ hotovo" line until
  // the refetch lands.
  const targetKind = tree.columns.find((c) => c.column.id === targetColumnId)?.column.kind
  const done_at = targetKind === 'done' ? (moved.done_at ?? new Date().toISOString()) : null
  const updated: Card = { ...moved, column_id: targetColumnId, position, done_at }
  return {
    ...tree,
    columns: columns.map((col) =>
      col.column.id === targetColumnId
        ? {
            ...col,
            cards: [...col.cards, updated].sort((a, b) => comparePositions(a.position, b.position)),
            card_count: col.card_count + 1,
          }
        : col,
    ),
  }
}
