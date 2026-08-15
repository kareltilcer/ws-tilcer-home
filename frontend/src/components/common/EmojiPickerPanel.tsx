import { useDeferredValue, useEffect, useMemo, useRef, useState } from 'react'
import { Search } from 'lucide-react'
import { cs } from '@/i18n/cs'
import { cn } from '@/lib/utils'
import { EMOJI_CATEGORIES, searchEmoji, type EmojiItem } from './emojiData'

// EmojiPickerPanel — the "browse all" body: a search box, a category quick-nav,
// and a scrollable, categorised grid of every emoji. Lazily imported by
// FolderIconPicker so its emoji dataset (~400 KB) only loads when actually opened.
// Native glyphs are rendered by the system font — no image sprites, no CDN.
export function EmojiPickerPanel({
  value,
  onSelect,
  autoFocus,
}: {
  value?: string
  onSelect: (emoji: string) => void
  /** Focus the search box on mount (desktop only — avoids popping the mobile keyboard). */
  autoFocus?: boolean
}) {
  const [query, setQuery] = useState('')
  const searchRef = useRef<HTMLInputElement>(null)
  const scrollRef = useRef<HTMLDivElement>(null)
  const sectionRefs = useRef<Record<string, HTMLElement | null>>({})

  useEffect(() => {
    if (autoFocus) searchRef.current?.focus()
  }, [autoFocus])

  // Filter off a deferred copy of the query so a broad match (a common single letter
  // can hit most of the ~1900-glyph dataset) renders its grid at low priority and the
  // keystrokes stay responsive. `searching` tracks the deferred value too, so the view
  // and the results it shows switch together.
  const deferredQuery = useDeferredValue(query)
  const results = useMemo(() => (deferredQuery.trim() ? searchEmoji(deferredQuery) : []), [deferredQuery])
  const searching = deferredQuery.trim().length > 0

  const jumpTo = (key: string) => {
    const section = sectionRefs.current[key]
    const container = scrollRef.current
    if (!section || !container) return
    // Scroll within the panel body (not the page) by aligning the section top.
    container.scrollTop = section.offsetTop - container.offsetTop
  }

  return (
    <div className="flex h-[360px] w-[min(20rem,calc(100vw-2rem))] flex-col">
      {/* Search */}
      <div className="flex-none border-b border-border p-2">
        <div className="relative">
          <Search size={15} className="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-subtle" aria-hidden />
          <input
            ref={searchRef}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={cs.common.emojiSearchPlaceholder}
            aria-label={cs.common.emojiSearchPlaceholder}
            className="h-9 w-full rounded-md border border-border bg-s1 pl-8 pr-3 text-sm text-fg placeholder:text-subtle focus-visible:outline-2 focus-visible:outline-focus"
          />
        </div>
      </div>

      {/* Category quick-nav — hidden while a search is active. */}
      {!searching && (
        <div className="flex flex-none items-center gap-0.5 border-b border-border px-1.5 py-1">
          {EMOJI_CATEGORIES.map((cat) => (
            <button
              key={cat.key}
              type="button"
              onClick={() => jumpTo(cat.key)}
              title={cat.label}
              aria-label={cat.label}
              className="grid h-7 w-7 flex-1 place-items-center rounded-md text-base leading-none hover:bg-s2"
            >
              {cat.icon}
            </button>
          ))}
        </div>
      )}

      {/* Body */}
      <div ref={scrollRef} className="om-scroll min-h-0 flex-1 overflow-y-auto p-2">
        {searching ? (
          results.length > 0 ? (
            <EmojiGrid items={results} value={value} onSelect={onSelect} />
          ) : (
            <p className="px-1 py-6 text-center text-sm text-subtle">{cs.common.emojiNoResults}</p>
          )
        ) : (
          EMOJI_CATEGORIES.map((cat) => (
            <section
              key={cat.key}
              ref={(el) => {
                sectionRefs.current[cat.key] = el
              }}
              // content-visibility lets the browser skip laying out offscreen
              // categories; the intrinsic-size estimate keeps the scrollbar stable.
              style={{ contentVisibility: 'auto', containIntrinsicSize: `${intrinsicHeight(cat.items.length)}px` } as React.CSSProperties}
              className="mb-1"
            >
              <h4 className="sticky top-0 bg-s1/95 px-1 py-1 text-[11px] font-semibold text-subtle backdrop-blur">{cat.label}</h4>
              <EmojiGrid items={cat.items} value={value} onSelect={onSelect} />
            </section>
          ))
        )}
      </div>
    </div>
  )
}

// A wrap grid that auto-fills the panel width with ~36px cells.
function EmojiGrid({ items, value, onSelect }: { items: EmojiItem[]; value?: string; onSelect: (emoji: string) => void }) {
  return (
    <div className="grid grid-cols-[repeat(auto-fill,minmax(2.25rem,1fr))] gap-0.5">
      {items.map((item) => {
        const active = item.char === value
        return (
          <button
            key={item.char}
            type="button"
            aria-pressed={active}
            title={item.name}
            aria-label={item.name}
            onClick={() => onSelect(item.char)}
            className={cn(
              'grid aspect-square place-items-center rounded-md text-xl leading-none',
              active ? 'bg-accent text-accent-fg' : 'hover:bg-s2',
            )}
          >
            {item.char}
          </button>
        )
      })}
    </div>
  )
}

// Rough height of a rendered category, so content-visibility can reserve space for
// collapsed sections. The grid auto-fills ~36px cells across the ~304px-wide panel
// (20rem minus p-2), which lands 8 columns; 38px is one 36px cell + the 2px gap.
const GRID_COLS = 8
function intrinsicHeight(count: number): number {
  const rows = Math.ceil(count / GRID_COLS)
  return 26 + rows * 38
}
