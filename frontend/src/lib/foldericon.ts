// Frontend mirror of the backend's folder-icon length policy
// (backend/internal/platform/foldericon/foldericon.go). An icon is capped at 8
// code points server-side and silently truncated beyond that, so the client
// enforces the same cap up front — the user sees exactly what will be stored,
// with no silent server-side truncation.
//
// This lives in lib/ (not emojiData.ts) so the eager FolderIconPicker can import
// the cap without dragging in the lazily-loaded ~400 KB emoji dataset.
export const MAX_ICON_RUNES = 8

// clampIconRunes bounds an icon to MAX_ICON_RUNES, counting by code point (via the
// string iterator) rather than UTF-16 unit, so a multi-codepoint emoji is measured
// the same way the backend measures runes.
export const clampIconRunes = (s: string): string => {
  const runes = [...s]
  return runes.length > MAX_ICON_RUNES ? runes.slice(0, MAX_ICON_RUNES).join('') : s
}
