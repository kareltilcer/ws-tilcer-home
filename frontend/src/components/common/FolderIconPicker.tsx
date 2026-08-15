import { lazy, Suspense, useState } from 'react'
import * as Popover from '@radix-ui/react-popover'
import { Loader2, Smile } from 'lucide-react'
import { cs } from '@/i18n/cs'
import { cn } from '@/lib/utils'
import { clampIconRunes } from '@/lib/foldericon'
import { Button } from '@/components/ui/ui'
import { useIsDesktop } from '@/hooks/useMediaQuery'

// The full "browse all emoji" panel is a lazily-loaded chunk: its ~400 KB emoji
// dataset only downloads the first time someone opens the picker, keeping it off
// the main bundle and the critical path.
const EmojiPickerPanel = lazy(() => import('./EmojiPickerPanel').then((m) => ({ default: m.EmojiPickerPanel })))

// The 📁 shown wherever a folder has no icon of its own. Folders default to it in the
// picker, and every folder-display site falls back to it via `folder.icon || DEFAULT`.
export const DEFAULT_FOLDER_ICON = '📁'

// The picker seeds to 📁 (the default glyph), so leaving it there means "no custom
// icon". Normalize that back to '' before saving so the folder stores NULL and keeps
// falling back to DEFAULT_FOLDER_ICON at every display site — rather than persisting
// an explicit 📁 (and auditing a spurious ''→📁 change) on every create/rename.
export const iconToStore = (icon: string) => (icon === DEFAULT_FOLDER_ICON ? '' : icon)

// Preset palette offered in the create/rename dialogs (design v4.1). The custom-emoji
// input and the "browse all" picker beside them reach any other glyph.
export const FOLDER_ICON_PRESETS = [
  '📁', '🏠', '🏡', '💰', '🧾', '📄', '🔧', '🛒', '🍲', '✈️',
  '🎁', '🐾', '📷', '⚡', '🩺', '📘', '🚗', '🔑', '❤️', '🗂️',
] as const

// FolderIconPicker — quick presets + a full searchable emoji picker + a free-text
// custom-emoji field. `value` is the full current icon (a preset click, a picker
// selection, and a typed entry all flow through the same string), so the highlighted
// preset always mirrors what will be saved. Shared by Poznámky and Dokumenty.
//
// Three ways in, by design: the presets are one-tap common choices; the "browse all"
// popover is a full grid with search (the primary path on desktop); and the text
// field stays so a phone can drop in an emoji straight from its own keyboard.
export function FolderIconPicker({ value, onChange }: { value: string; onChange: (icon: string) => void }) {
  const [browseOpen, setBrowseOpen] = useState(false)
  const desktop = useIsDesktop()

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
          // Clamp to the backend's rune cap here (counting code points, not UTF-16
          // units) so what's shown is exactly what's stored — no silent server-side
          // truncation. maxLength stays as a coarse UTF-16 backstop against huge pastes.
          onChange={(e) => onChange(clampIconRunes(e.target.value))}
          maxLength={16}
          aria-label={cs.common.customIcon}
          placeholder="＋"
          className="h-9 w-10 rounded-lg border border-dashed border-border-strong bg-s2 text-center text-base text-fg placeholder:text-subtle focus-visible:outline-2 focus-visible:outline-focus"
        />
      </div>

      <Popover.Root open={browseOpen} onOpenChange={setBrowseOpen}>
        <Popover.Trigger asChild>
          <Button variant="secondary" size="sm" className="mt-2">
            <Smile size={15} aria-hidden />
            {cs.common.browseEmoji}
          </Button>
        </Popover.Trigger>
        <Popover.Portal>
          <Popover.Content
            align="start"
            sideOffset={8}
            collisionPadding={8}
            // Above the dialog it opens inside (overlay z-40 / content z-50).
            className="z-[60] overflow-hidden rounded-lg border border-border bg-s1 text-fg shadow-[var(--shadow)]"
            // On mobile, don't yank focus into the search box (it would pop the
            // keyboard over the grid); on desktop the panel focuses it itself.
            onOpenAutoFocus={(e) => {
              if (!desktop) e.preventDefault()
            }}
          >
            <Suspense
              fallback={
                <div className="grid h-[360px] w-[min(20rem,calc(100vw-2rem))] place-items-center text-subtle">
                  <Loader2 className="animate-spin" size={20} aria-label={cs.common.loading} />
                </div>
              }
            >
              <EmojiPickerPanel
                value={value}
                autoFocus={desktop}
                onSelect={(emoji) => {
                  onChange(emoji)
                  setBrowseOpen(false)
                }}
              />
            </Suspense>
          </Popover.Content>
        </Popover.Portal>
      </Popover.Root>
    </div>
  )
}
