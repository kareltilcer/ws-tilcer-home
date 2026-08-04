import { useEffect } from 'react'
import { cs } from '@/i18n/cs'

const SEP = ' · '

// Drives the browser-tab title for the mounted screen. Segments are ordered
// most-specific-first and the app name is always appended last, so the tab stays
// legible when the browser truncates it and when many tabs are open
// (e.g. "Můj úkol · Poznámky · home"). Falsy segments are dropped, so a deeper
// context can be passed as undefined until it resolves —
// `useDocumentTitle(note?.title, cs.notes.title)` shows "Poznámky · home" while
// loading, then "<title> · Poznámky · home" once the note is known.
export function useDocumentTitle(...segments: Array<string | null | undefined>) {
  const title = [...segments, cs.app.name].filter(Boolean).join(SEP)
  useEffect(() => {
    document.title = title
  }, [title])
}
