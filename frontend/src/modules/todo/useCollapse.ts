import { useCallback, useState } from 'react'

// Column collapse is client-side, per device (PRD D3) — persisted in
// localStorage keyed by board, never sent to the server.
export function useCollapse(boardId: string) {
  const key = `home-collapse-${boardId}`
  const [collapsed, setCollapsed] = useState<Set<string>>(() => load(key))

  const toggle = useCallback(
    (columnId: string) => {
      setCollapsed((prev) => {
        const next = new Set(prev)
        if (next.has(columnId)) next.delete(columnId)
        else next.add(columnId)
        try {
          localStorage.setItem(key, JSON.stringify([...next]))
        } catch {
          // ignore storage errors (private mode / quota)
        }
        return next
      })
    },
    [key],
  )

  return { collapsed, toggle }
}

function load(key: string): Set<string> {
  try {
    const raw = localStorage.getItem(key)
    if (raw) return new Set(JSON.parse(raw) as string[])
  } catch {
    // ignore
  }
  return new Set()
}
