import { cs } from '@/i18n/cs'
import type { NoteFormatCommand } from './noteFormat'

// VisualToolbar is the formatting bar of the Vizuální mode — the one thing the design
// has drawn since v3 that the WYSIWYG surface shipped without, leaving every heading,
// bold and list to Markdown syntax the visual editor exists to spare people.
//
// It renders in NoteView's column rather than inside the editor, because the design puts
// it OUTSIDE the scrolling body: a formatting bar that scrolls away halfway down a note
// is a bar you have to go looking for. What the commands DO lives in noteFormat's
// applyFormat, beside the milkdown imports; this file takes nothing from there but the
// type naming them, which erases at build, so ProseMirror stays out of the landing bundle.
//
// Crepe ships a bar of its own (`top-bar`, off by default) and it is not this one: it is
// English-labelled, reaches well past the minimal set — tables, task lists, LaTeX — and
// renders inside the editor root, where the scroll problem above applies.
const ITEMS: { command: NoteFormatCommand; label: string; title: string }[] = [
  { command: 'h1', label: 'H1', title: cs.notes.toolbarH1 },
  { command: 'h2', label: 'H2', title: cs.notes.toolbarH2 },
  { command: 'bold', label: 'B', title: cs.notes.toolbarBold },
  { command: 'italic', label: 'I', title: cs.notes.toolbarItalic },
  { command: 'bulletList', label: '•', title: cs.notes.toolbarBullet },
  { command: 'quote', label: '“', title: cs.notes.toolbarQuote },
  { command: 'code', label: '</>', title: cs.notes.toolbarCode },
  { command: 'link', label: '↗', title: cs.notes.toolbarLink },
]

export function VisualToolbar({ onCommand }: { onCommand: (command: NoteFormatCommand) => void }) {
  return (
    <div
      role="group"
      aria-label={cs.notes.toolbar}
      className="mx-5 mb-3.5 flex flex-none flex-wrap gap-[3px] rounded-[9px] border border-border bg-s2 p-[5px]"
    >
      {ITEMS.map((item) => (
        <button
          key={item.command}
          type="button"
          title={item.title}
          // The face is a glyph, so it is the tooltip's words that have to be the
          // accessible name — "bullet, button" tells a screen-reader user nothing.
          aria-label={item.title}
          // Keep the caret where it is: a mousedown landing on the button blurs the
          // editor, and the user loses sight of the very selection they are formatting.
          onMouseDown={(e) => e.preventDefault()}
          onClick={() => onCommand(item.command)}
          className="h-[30px] min-w-8 rounded-md px-2 font-mono text-[12.5px] font-semibold text-muted hover:bg-s3 hover:text-fg"
        >
          {item.label}
        </button>
      ))}
    </div>
  )
}
