import type { ColumnKind } from '@/api/types'

// Column kind/priority options shared by AddColumnModal (create) and ColumnMenu
// (edit) so the labels stay in lockstep across both UIs.
export const KINDS: [ColumnKind, string][] = [
  ['now', 'Právě dělám'],
  ['normal', 'Běžný'],
  ['done', 'Hotovo'],
]

export const PRIORITIES: [number, string][] = [
  [2, 'Vysoká'],
  [1, 'Střední'],
  [0, 'Nízká'],
]
