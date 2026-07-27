import type { BoardTreeColumn } from '@/api/types'
import { comparePositions } from '@/lib/lexorank'

export type SortMode = 'manual' | 'priority'

// Column display order. `manual` follows the lexorank `position` (the drag order,
// which the backend already returns). `priority` re-sorts high→low, tie-broken by
// position so the manual order is the stable secondary key — matching the v1
// prototype's `(priority desc) || (original index)`.
export function orderColumns(columns: BoardTreeColumn[], mode: SortMode): BoardTreeColumn[] {
  const byPosition = columns.slice().sort((a, b) => comparePositions(a.column.position, b.column.position))
  if (mode === 'manual') return byPosition
  return byPosition
    .map((c, i) => [c, i] as const)
    .sort((a, b) => b[0].column.priority - a[0].column.priority || a[1] - b[1])
    .map(([c]) => c)
}

// `now` columns pinned to the top, preserving the incoming order within each group
// (mobile accordion, PRD §7 / v1). Applied on top of orderColumns.
export function pinNowFirst(columns: BoardTreeColumn[]): BoardTreeColumn[] {
  const now = columns.filter((c) => c.column.kind === 'now')
  const rest = columns.filter((c) => c.column.kind !== 'now')
  return [...now, ...rest]
}
