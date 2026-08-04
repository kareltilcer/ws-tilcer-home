import { useEffect, useRef } from 'react'
import { Crepe } from '@milkdown/crepe'
import '@milkdown/crepe/theme/common/style.css'
import './crepe-theme.css'

// MilkdownEditor is the WYSIWYG ("Vizuální") surface — Milkdown's batteries-
// included Crepe editor (ProseMirror), the Markdown-backed rich editor named in
// the v3 design. It serializes to Markdown, so it round-trips the one canonical
// body_md (D30) — no HTML is persisted.
//
// The editor is created once from `defaultValue`; to load a different note (or to
// re-seed after "load their version"), the parent passes a changing `key` so the
// component remounts. Content changes are streamed to onChange as Markdown; the
// parent debounces the save (last-write-wins, D38).
export function MilkdownEditor({
  defaultValue,
  onChange,
}: {
  defaultValue: string
  onChange: (markdown: string) => void
}) {
  const hostRef = useRef<HTMLDivElement>(null)
  // Keep the latest onChange without re-running the create effect.
  const onChangeRef = useRef(onChange)
  onChangeRef.current = onChange

  useEffect(() => {
    const root = hostRef.current
    if (!root) return
    const crepe = new Crepe({ root, defaultValue })
    let disposed = false
    // Crepe re-serializes defaultValue on load — normalized (list markers, spacing,
    // escaping) so it differs textually from the stored body_md even though nothing
    // was edited. We must not forward that normalization (it would dirty the draft
    // and trigger a no-op autosave) but must forward every genuine edit. So we seed
    // `baseline` from the editor's OWN serialization once created (getMarkdown) and
    // forward only emissions that differ. Seeding from getMarkdown — rather than
    // adopting the first markdownUpdated emission — means the user's first real edit
    // is never swallowed, even if Crepe emits no initial markdownUpdated at all.
    let baseline: string | null = null
    // Register the listener BEFORE create() so no emission during editor build is
    // missed; while baseline is still null (pre-seed) any emission is the initial
    // normalization and is ignored — the editor isn't interactive yet, so it can't
    // be a user edit.
    crepe.on((listener) => {
      listener.markdownUpdated((_ctx, markdown) => {
        if (disposed || baseline === null) return
        if (markdown === baseline) return
        onChangeRef.current(markdown)
      })
    })
    crepe
      .create()
      .then(() => {
        if (disposed) return
        baseline = crepe.getMarkdown()
      })
      .catch(() => {
        /* editor failed to mount — the raw Markdown tab remains the escape hatch */
      })
    return () => {
      disposed = true
      void crepe.destroy()
    }
    // Intentionally create once; remount via `key` to change the note/content.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  return <div className="note-crepe rounded-xl border border-accent bg-s1 px-3 py-2" ref={hostRef} />
}
