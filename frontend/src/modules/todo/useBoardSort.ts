import { useCallback, useState } from 'react'
import type { SortMode } from './ordering'

// Column sort mode is a client-side view preference, per device and per board —
// persisted in localStorage like collapse (D3), never sent to the server. A
// manual drag reorder flips this back to 'manual' (see useColumnMove).
export function useBoardSort(boardId: string) {
  const key = `home-sort-${boardId}`
  const [mode, setMode] = useState<SortMode>(() => load(key))

  const set = useCallback(
    (m: SortMode) => {
      setMode(m)
      try {
        localStorage.setItem(key, m)
      } catch {
        // ignore storage errors (private mode / quota)
      }
    },
    [key],
  )

  return { mode, setMode: set }
}

function load(key: string): SortMode {
  try {
    if (localStorage.getItem(key) === 'priority') return 'priority'
  } catch {
    // ignore
  }
  return 'manual'
}
