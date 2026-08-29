import { useRef } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import * as api from '@/api/endpoints'
import type { BoardTree, BoardTreeColumn } from '@/api/types'
import { between, canInsertBetween, comparePositions } from '@/lib/lexorank'

type Ordered = BoardTreeColumn[]
type Write = { columnId: string; position: string }

// useColumnMove reorders columns by writing lexorank `position` keys and calling
// POST /api/columns/{id}/move, then flips the board back to manual sort (onManual).
//
// It operates on the *displayed* order passed in. When that order already matches
// the stored position order (manual view), a single row is rewritten between the
// moved column's new neighbours — never a renumber. In the priority view the
// displayed order diverges from the position order, so a single write can't
// reproduce the arrangement the user saw once the board flips to manual (the
// neighbours' stored positions may be inverted, and untouched columns would snap
// back to position order). There we renumber every column to the target order so
// the manual view shows exactly what was on screen.
export function useColumnMove(boardId: string, treeKey: readonly unknown[], onManual: () => void) {
  const qc = useQueryClient()

  // Serialize moves: rapid nudge/drag clicks would otherwise fire overlapping
  // moveColumn requests that can commit out of order on the server. A synchronous
  // ref (not mut.isPending, which only flips on the next render) drops any new move
  // while one is in flight; the user retries once the board settles.
  const busy = useRef(false)

  const mut = useMutation({
    // A single write hits the per-column move; a multi-column renumber goes to the
    // atomic batch endpoint so it applies all-or-nothing (no half-applied order on
    // a partial failure — the optimistic rollback then always matches the server).
    mutationFn: async (writes: Write[]): Promise<void> => {
      if (writes.length === 1) await api.moveColumn(writes[0].columnId, writes[0].position)
      else await api.reorderColumns(boardId, writes.map((w) => ({ id: w.columnId, position: w.position })))
    },
    onMutate: async (writes) => {
      await qc.cancelQueries({ queryKey: treeKey })
      const prev = qc.getQueryData<BoardTree>(treeKey)
      const posByCol = new Map(writes.map((w) => [w.columnId, w.position]))
      qc.setQueryData<BoardTree>(treeKey, (old) =>
        old
          ? {
              ...old,
              columns: old.columns.map((c) =>
                posByCol.has(c.column.id) ? { ...c, column: { ...c.column, position: posByCol.get(c.column.id)! } } : c,
              ),
            }
          : old,
      )
      return { prev }
    },
    onError: (_e, _v, ctx) => {
      if (ctx?.prev) qc.setQueryData(treeKey, ctx.prev)
      toast.error('Přesun sloupce se nezdařil', { description: 'Změna byla vrácena.' })
    },
    onSettled: () => {
      busy.current = false
      void qc.invalidateQueries({ queryKey: ['board', boardId] })
    },
  })

  const posMapOf = (cols: Ordered) => {
    const m = new Map(cols.map((c) => [c.column.id, c.column.position]))
    return (id: string) => m.get(id) ?? ''
  }

  // Renumber the whole target order to strictly increasing keys, emitting a write
  // only for columns whose position actually changes.
  const renumberWrites = (targetIds: string[], posOf: (id: string) => string): Write[] => {
    const writes: Write[] = []
    let prev = ''
    for (const id of targetIds) {
      const pos = between(prev, '')
      if (posOf(id) !== pos) writes.push({ columnId: id, position: pos })
      prev = pos
    }
    return writes
  }

  // Realise `targetIds` (the desired left-to-right order after the move) as
  // position writes. `cols` is the currently displayed order.
  const commit = (targetIds: string[], cols: Ordered, srcId: string) => {
    if (busy.current) return // a move is already in flight — ignore until it settles
    const posOf = posMapOf(cols)
    const displayedIds = cols.map((c) => c.column.id)
    const positionOrder = displayedIds.slice().sort((a, b) => comparePositions(posOf(a), posOf(b)))
    const matchesPositionOrder = displayedIds.every((id, i) => id === positionOrder[i])

    const k = targetIds.indexOf(srcId)
    if (k < 0) return // src not in the target order — nothing to move
    const prev = k > 0 ? posOf(targetIds[k - 1]) : ''
    const next = k < targetIds.length - 1 ? posOf(targetIds[k + 1]) : ''

    if (matchesPositionOrder && canInsertBetween(prev, next)) {
      // Manual view: one row rewritten between the moved column's new neighbours.
      busy.current = true
      mut.mutate([{ columnId: srcId, position: between(prev, next) }])
    } else {
      // Priority view — or degenerate manual neighbours that share an equal/inverted
      // position key (corrupted data), where no single strictly-between key exists.
      // Renumber every column to the target order so the manual view reproduces what
      // the user saw. A reorder that resolves to zero writes still falls through to
      // onManual() so the board leaves priority sort and shows the dragged order.
      const writes = renumberWrites(targetIds, posOf)
      if (writes.length > 0) {
        busy.current = true
        mut.mutate(writes)
      }
    }
    onManual() // a user-initiated reorder always flips the board to manual sort
  }

  // Drop `srcId` onto `targetId` — v1 splice semantics: remove src, insert at
  // target's original index (lands after the target when dragging downward).
  const moveTo = (cols: Ordered, srcId: string, targetId: string) => {
    if (srcId === targetId) return
    const ids = cols.map((c) => c.column.id)
    const from = ids.indexOf(srcId)
    const to = ids.indexOf(targetId)
    if (from < 0 || to < 0) return
    ids.splice(from, 1)
    ids.splice(to, 0, srcId)
    commit(ids, cols, srcId)
  }

  // Nudge `srcId` one slot (mobile ▲▼ / menu ◀▶): dir -1 = earlier, +1 = later.
  const nudge = (cols: Ordered, srcId: string, dir: -1 | 1) => {
    const ids = cols.map((c) => c.column.id)
    const i = ids.indexOf(srcId)
    const j = i + dir
    if (i < 0 || j < 0 || j >= ids.length) return
    ;[ids[i], ids[j]] = [ids[j], ids[i]]
    commit(ids, cols, srcId)
  }

  return { moveTo, nudge, isPending: mut.isPending }
}
