import { cs } from '@/i18n/cs'
import { cn } from '@/lib/utils'

// The 📁 shown wherever a folder has no icon of its own. Folders default to it in the
// picker, and every folder-display site falls back to it via `folder.icon || DEFAULT`.
export const DEFAULT_FOLDER_ICON = '📁'

// The picker seeds to 📁 (the default glyph), so leaving it there means "no custom
// icon". Normalize that back to '' before saving so the folder stores NULL and keeps
// falling back to DEFAULT_FOLDER_ICON at every display site — rather than persisting
// an explicit 📁 (and auditing a spurious ''→📁 change) on every create/rename.
export const iconToStore = (icon: string) => (icon === DEFAULT_FOLDER_ICON ? '' : icon)

// Preset palette offered in the create/rename dialogs (design v4.1). The custom-emoji
// input beside them accepts anything else.
export const FOLDER_ICON_PRESETS = [
  '📁', '🏠', '🏡', '💰', '🧾', '📄', '🔧', '🛒', '🍲', '✈️',
  '🎁', '🐾', '📷', '⚡', '🩺', '📘', '🚗', '🔑', '❤️', '🗂️',
] as const

// FolderIconPicker — a wrap grid of emoji presets plus a free-text custom-emoji
// input. `value` is the full current icon (a preset click and a custom entry both
// flow through the same string), so the highlighted preset always mirrors what will
// be saved. Shared by Poznámky and Dokumenty.
export function FolderIconPicker({ value, onChange }: { value: string; onChange: (icon: string) => void }) {
  return (
    <div>
      <div className="mb-2 text-[12px] font-semibold text-muted">
        {cs.common.folderIcon} <span className="font-normal text-subtle">({cs.common.optional})</span>
      </div>
      <div className="flex flex-wrap gap-1.5">
        {FOLDER_ICON_PRESETS.map((emoji) => {
          const active = emoji === value
          return (
            <button
              key={emoji}
              type="button"
              aria-pressed={active}
              onClick={() => onChange(emoji)}
              className={cn(
                'grid h-9 w-9 place-items-center rounded-lg border text-lg leading-none',
                active ? 'border-accent bg-accent text-accent-fg' : 'border-border bg-s2 hover:border-border-strong',
              )}
            >
              {emoji}
            </button>
          )
        })}
        <input
          value={value}
          onChange={(e) => onChange(e.target.value)}
          maxLength={8}
          aria-label={cs.common.customIcon}
          placeholder="＋"
          className="h-9 w-10 rounded-lg border border-dashed border-border-strong bg-s2 text-center text-base text-fg placeholder:text-subtle focus-visible:outline-2 focus-visible:outline-focus"
        />
      </div>
    </div>
  )
}
