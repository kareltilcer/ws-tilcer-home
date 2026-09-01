import { useEffect, useRef } from 'react'
import { Crepe } from '@milkdown/crepe'
import { $prose } from '@milkdown/kit/utils'
import { Plugin, PluginKey } from '@milkdown/kit/prose/state'
import type { EditorView } from '@milkdown/kit/prose/view'
import type { Node as ProseNode } from '@milkdown/kit/prose/model'
import { toast } from 'sonner'
import '@milkdown/crepe/theme/common/style.css'
import './crepe-theme.css'
import { uploadNoteImage } from './api/endpoints'
import { cs } from '@/i18n/cs'
import { MAX_INLINE_IMAGE_DATA_LEN } from './inlineImage'

// MilkdownEditor is the WYSIWYG ("Vizuální") surface — Milkdown's batteries-
// included Crepe editor (ProseMirror). It serializes to Markdown, so it round-trips
// the one canonical body_md (D30) — no HTML is persisted.
//
// Images never live inline as base64 (which blew the API body cap and froze the
// editor). Two paths funnel every image to object storage instead, so body_md holds
// only a small `![](/api/notes/images/{id})` reference:
//   - onUpload: a pasted/dropped/picked image FILE is streamed to R2 and the node is
//     inserted already pointing at the returned URL (Crepe awaits onUpload).
//   - the upload plugin below: an image pasted as HTML (copied from a web page) arrives
//     as a node whose src is a `data:`/`blob:` URI — onUpload never sees it. The plugin
//     resolves those bytes to a stored object and rewrites the node's src attribute
//     (which is what serializes).
//
// The plugin acts only on a src that CANNOT be persisted as it stands (see needsUpload),
// which is what makes its scan safe to run on mount as well as after a paste: the scan
// fires on every editor mount (switching to Vizuální remounts it). A small inline data:
// image the server accepts is left exactly where it is.
//
// WHERE THE SRC CAME FROM decides what a failed upload may do to it, because that decides
// whether the bytes exist anywhere else:
//   - PASTED this session — the host mints a placeholder (`![](uploading:…)`, the same
//     one the Markdown tab inserts) and runs the upload; the emission carries the
//     placeholder in the image's stead, so the multi-megabyte data URI never reaches
//     autosave or the localStorage mirror while the text typed around it still does, and
//     the swap for the real URL lands in the draft even if this editor is torn down
//     first. On failure the node is dropped: those bytes were never part of the note.
//   - ALREADY IN THE STORED BODY (found by the FIRST scan) — its bytes exist nowhere
//     else, so a failed upload must never edit it out. The node stays put and the whole
//     emission is held back instead, permanently if the upload failed: a note whose
//     inline image cannot be stored genuinely cannot be saved, and destroying the image
//     to make it saveable is not ours to do. The user is told to use the Markdown tab.

// needsUpload reports whether an image src must be replaced before the body can be
// persisted: a `blob:` URL (bytes that live only in this tab, meaningless to the server)
// or a `data:` image over the length the server accepts. A data: image UNDER the cap is
// deliberately NOT touched — the server stores it verbatim, so it can already be part of
// a note's stored body_md, and uploading it would rewrite (and autosave) a note the user
// did nothing but open. The Markdown tab's paste handler draws the line in the same
// place, so the two surfaces never disagree about one body.
function needsUpload(src: unknown): src is string {
  if (typeof src !== 'string') return false
  if (src.startsWith('blob:')) return true
  return src.startsWith('data:') && src.length > MAX_INLINE_IMAGE_DATA_LEN
}

// inlineImageRefRE matches a data:/blob: URI used as an IMAGE source in serialized
// markdown — the markdown image form `![alt](data:…)` / `![alt](blob:…)` (group 1) or a
// raw-HTML `<img src="data:…">` (group 2) — capturing the URI so needsUpload can weigh
// it. A plain LINK `[text](data:…)` is deliberately NOT matched: the upload plugin only
// ever rewrites (or removes) image NODES, so a data:/blob: URI that is not an image would
// never clear and would suppress autosave forever (silent data loss). Anchoring on the
// image syntax keeps the suppression scoped to exactly the uploads the plugin resolves.
//
// Each branch runs to its STRUCTURAL terminator — the `)` or the closing quote, never to
// the first whitespace — so a line-wrapped (MIME-style) base64 image is measured whole
// rather than as the short prefix before its first newline. The markdown branch also
// spans BALANCED inner parens (`(…)`, which CommonMark allows in an unwrapped
// destination), so a data URI carrying them — SVG path data, say — is captured whole
// instead of truncated at the first inner `)`. Same shapes, and the same reason, as the
// backend's inlineImageDataURIRE and the Markdown tab's imageRefDataURIRE.
const inlineImageRefRE =
  /!\[[^\]]*\]\(((?:data:|blob:)(?:[^()]|\([^()]*\))*)\)|<img\b[^>]*\bsrc=["']((?:data:|blob:)[^"']*)["'][^>]*>/gi

// PendingRef is one WHOLE image reference in serialized markdown whose src is still
// awaiting an upload — the emission fragment that must not reach autosave — paired with
// that src, which is what decides how the reference may be treated (see forward()).
type PendingRef = { ref: string; src: string }

function pendingInlineImageRefs(markdown: string): PendingRef[] {
  const refs: PendingRef[] = []
  for (const m of markdown.matchAll(inlineImageRefRE)) {
    const src = m[1] ?? m[2]
    if (needsUpload(src)) refs.push({ ref: m[0], src })
  }
  return refs
}

// replaceInlineImageRefs is how an emission mid-upload is made safe to forward: the bytes
// can't be persisted (the server rejects an oversized inlined data URI and it would blow
// the API body cap), but everything the user typed around them can. Each reference is
// swapped WHOLE — the entire `![alt](data:…)` or `<img … src="data:…" …>`, so no dangling
// `![alt]()` or `<img src="">` is left behind — for the host's placeholder, which the host
// later swaps for the real URL. A reference whose placeholder has not been minted yet is
// removed instead; the re-emission the mint triggers puts it back a tick later.
function replaceInlineImageRefs(
  markdown: string,
  pending: PendingRef[],
  tokenFor: (src: string) => string | undefined,
): string {
  return pending.reduce((text, p) => {
    const token = tokenFor(p.src)
    return text.split(p.ref).join(token ? `![](${token})` : '')
  }, markdown)
}

// findImagesBySrc locates EVERY image node whose src matches, at dispatch time
// (positions shift as the doc changes, so this is recomputed against the live state).
// All matches must be handled from the single upload: the same image pasted twice is
// two nodes sharing one data:/blob: src, and rewriting only the first would leave the
// second as a data: node that the scan re-queues and re-uploads (a duplicate object)
// and that keeps autosave suppressed until it happens to resolve.
function findImagesBySrc(view: EditorView, src: string): { pos: number; node: ProseNode }[] {
  const hits: { pos: number; node: ProseNode }[] = []
  view.state.doc.descendants((node, pos) => {
    if (node.attrs?.src === src) hits.push({ pos, node })
    return true
  })
  return hits
}

// rewriteImageSrc points every node that shared this src at the one uploaded URL.
// setNodeMarkup keeps node sizes stable, so all positions stay valid within the single
// transaction.
function rewriteImageSrc(view: EditorView, src: string, url: string) {
  const hits = findImagesBySrc(view, src)
  if (!hits.length) return
  const tr = view.state.tr
  for (const hit of hits) tr.setNodeMarkup(hit.pos, undefined, { ...hit.node.attrs, src: url })
  view.dispatch(tr)
}

// dropImages removes the nodes rather than leave an unpersistable src to serialize (which
// would re-trigger the freeze and be rejected by the server guard). Delete
// highest-position-first so each earlier position stays valid as the doc shrinks.
function dropImages(view: EditorView, src: string) {
  const hits = findImagesBySrc(view, src).sort((a, b) => b.pos - a.pos)
  if (!hits.length) return
  const tr = view.state.tr
  for (const hit of hits) tr.delete(hit.pos, hit.pos + hit.node.nodeSize)
  view.dispatch(tr)
}

// dispatchQuietly swallows a failed transaction. Every dispatch below is the last step of
// its branch with no recovery to attempt, and they run detached from any event handler —
// letting one throw would surface as an unhandled rejection.
function dispatchQuietly(dispatch: () => void) {
  try {
    dispatch()
  } catch {
    /* the doc is unchanged; the next scan gets another go */
  }
}

// storeInlineImage resolves the src's bytes and stores them, returning the object URL —
// or null when either step failed. Used only for an image that came from the STORED body;
// a pasted one is uploaded by the host so its placeholder shares the one pipeline.
async function storeInlineImage(noteId: string, src: string): Promise<string | null> {
  try {
    // fetch resolves both data: and blob: URLs to their bytes in the browser.
    const blob = await (await fetch(src)).blob()
    return (await uploadNoteImage(noteId, blob, 'obrazek')).url
  } catch {
    return null
  }
}

// InlineImageUpload is what the host hands back when asked to take over an image's bytes:
// the placeholder the emission must carry in the image's stead until the upload lands,
// and the URL it resolves to (null if it failed).
export type InlineImageUpload = { token: string; url: Promise<string | null> }

// inlineImageUploads owns the state the plugin and the emission gate must agree on about
// every src that can't be persisted: whether it is still uploading, whether it came from
// the note's STORED body or was pasted in this session, and which placeholder the host
// minted for it. `begin` delegates a pasted image to the host; `onTokenMinted` asks for a
// re-emission, because the placeholder can be minted after this transaction's
// markdownUpdated already fired — without it a paste whose emission lost that race would
// leave the image out of the draft until the next keystroke, or forever if none came.
function inlineImageUploads(opts: {
  noteId: string
  begin: (src: string) => InlineImageUpload
  onTokenMinted: () => void
}) {
  const inflight = new Set<string>()
  const stored = new Set<string>()
  const tokenBySrc = new Map<string, string>()
  let scanned = false

  // runStored uploads an image the note already holds. Failure leaves the doc untouched:
  // the node stays, the emission gate holds every emission back from there on, and the
  // note keeps the only copy of those bytes.
  const runStored = async (view: EditorView, src: string) => {
    let url: string | null = null
    try {
      url = await storeInlineImage(opts.noteId, src)
    } finally {
      inflight.delete(src)
    }
    // One id per outcome so a note holding several such images toasts once, not once each.
    if (url === null) toast.error(cs.notes.imageUploadBlocked, { id: 'note-image-blocked' })
    else toast.success(cs.notes.imageMigrated, { id: 'note-image-migrated' })
    // The editor can be torn down while the upload runs (mode switch, overlay close).
    // Dispatching into a destroyed view throws.
    if (url === null || view.isDestroyed) return
    dispatchQuietly(() => rewriteImageSrc(view, src, url))
  }

  // runPasted hands the bytes to the host and points the node at whatever comes back.
  // Storing the bytes and mutating the doc stay in separate failure domains on purpose:
  // once the object exists, a throw from the transaction (a foreign plugin's
  // appendTransaction, a stale position) must never be read as a failed upload and delete
  // an image whose bytes are safely stored.
  const runPasted = async (view: EditorView, src: string) => {
    const { token, url: pending } = opts.begin(src)
    tokenBySrc.set(src, token)
    opts.onTokenMinted()
    let url: string | null = null
    try {
      url = await pending
    } finally {
      inflight.delete(src)
      tokenBySrc.delete(src)
    }
    if (view.isDestroyed) return
    // Dropping the node rather than leaving an unpersistable src to serialize (which
    // would re-trigger the freeze and be rejected by the server guard) is safe here and
    // only here: the host already stripped its placeholder out of the draft, and the
    // bytes were never part of the saved note.
    dispatchQuietly(() => (url === null ? dropImages(view, src) : rewriteImageSrc(view, src, url)))
  }

  const scan = (view: EditorView) => {
    const pending: string[] = []
    view.state.doc.descendants((node) => {
      const src = node.attrs?.src
      // Mark as in-flight during the scan so two nodes sharing one src (the same image
      // pasted twice) don't both get queued for upload.
      if (needsUpload(src) && !inflight.has(src)) {
        inflight.add(src)
        pending.push(src)
      }
      return true
    })
    if (!scanned) {
      scanned = true
      // The first scan runs against the document Crepe parsed from the note's stored
      // body_md, so anything unpersistable in it was already saved. Only a data: src
      // carries its own bytes and is worth protecting — a blob: URL that somehow got
      // persisted points into a session that is long gone, so there is nothing to lose.
      for (const src of pending) if (src.startsWith('data:')) stored.add(src)
    }
    for (const src of pending) {
      const run = stored.has(src) ? runStored : runPasted
      void run(view, src).catch(() => {
        // Both paths already report whatever the user can act on; a throw that escapes
        // one is a bug in the host callback and helps nobody as an unhandled rejection.
        // Releasing the src lets the next scan try again.
        inflight.delete(src)
      })
    }
  }

  return {
    isStored: (src: string) => stored.has(src),
    tokenFor: (src: string) => tokenBySrc.get(src),
    // The plugin scans the doc after each change for image nodes pointing at a src that
    // can't be persisted and uploads them (once each, tracked by `inflight`).
    plugin: $prose(
      () =>
        new Plugin({
          key: new PluginKey('notes-inline-image-upload'),
          view: (view) => {
            scan(view)
            // Rescan only when the DOCUMENT actually changed. ProseMirror mints a new doc
            // node on every content edit but reuses it across selection-only updates
            // (cursor moves, focus), so this skips the full-doc walk on those — image srcs
            // can only change when the doc does.
            return {
              update: (updated, prev) => {
                if (prev.doc !== updated.state.doc) scan(updated)
              },
            }
          },
        }),
    ),
  }
}

export function MilkdownEditor({
  noteId,
  defaultValue,
  onChange,
  onInlineImage,
}: {
  noteId: string
  defaultValue: string
  onChange: (markdown: string) => void
  // onInlineImage delegates a PASTED image's bytes to the host, which owns the placeholder
  // and the upload (see InlineImageUpload). Required, not optional: a fallback that
  // uploaded here instead would put the image outside the host's autosave hold and lose it
  // whenever this editor is torn down mid-upload.
  onInlineImage: (src: string) => InlineImageUpload
}) {
  const hostRef = useRef<HTMLDivElement>(null)
  // Keep the latest callbacks without re-running the create effect.
  const onChangeRef = useRef(onChange)
  onChangeRef.current = onChange
  const onInlineImageRef = useRef(onInlineImage)
  onInlineImageRef.current = onInlineImage

  useEffect(() => {
    const root = hostRef.current
    if (!root) return
    const crepe = new Crepe({
      root,
      defaultValue,
      featureConfigs: {
        // Crepe ships an English placeholder ("Please enter..."), and an empty note now
        // opens straight into this editor — so that string is the first thing anyone
        // sees on a brand-new note, on the one screen of an otherwise Czech app.
        // `doc`, not Crepe's default `block`: this string is the empty-NOTE invitation.
        // Crepe's block mode decorates whichever empty block the caret is sitting in, so
        // it would reappear on a blank line the user opened up mid-note — telling someone
        // already writing to start writing. `doc` shows it only while the note is empty.
        [Crepe.Feature.Placeholder]: { text: cs.notes.visualPlaceholder, mode: 'doc' },
        // A pasted/dropped/picked image FILE goes straight to object storage; Crepe
        // awaits this and inserts the node already pointing at the returned URL.
        [Crepe.Feature.ImageBlock]: {
          onUpload: async (file: File) => {
            try {
              return (await uploadNoteImage(noteId, file, file.name || 'obrazek')).url
            } catch (e) {
              // Surface the failure (over-cap, network) — without this the file path
              // fails silently, unlike the HTML-paste path below. Re-throw so Crepe
              // aborts the insertion rather than embedding an empty src.
              toast.error(cs.notes.imageUploadError)
              throw e
            }
          },
        },
      },
    })
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

    // forward is the one gate every emission passes through. It runs on markdownUpdated
    // and again whenever a placeholder is minted (see onTokenMinted).
    const forward = (markdown: string) => {
      let out = markdown
      const pending = pendingInlineImageRefs(markdown)
      if (pending.length) {
        // An image that came from the note's STORED body is never edited out of an
        // emission: the forwarded body is what autosave persists, so handing over a body
        // without it would delete it from the note just as surely as deleting the node.
        // Hold the whole emission back instead — while its upload runs (the clean
        // emission after the rewrite carries the migrated body) and permanently if that
        // upload failed, because the note genuinely cannot be saved with those bytes
        // inline. The user is told, and the Markdown tab still shows the note verbatim.
        if (pending.some((p) => uploads.isStored(p.src))) return
        // A pasted image is still uploading (its node holds a data:/blob: src). Forward
        // the emission with the host's placeholder in its stead rather than dropping it
        // whole: the data URI must never reach autosave or the localStorage mirror, but
        // everything typed since the paste rides on these same emissions, and no clean
        // one ever follows when the upload outlives the editor (mode switch, overlay
        // close, tab killed) — the parent's draft would still hold the pre-paste text and
        // the edit would be silently lost. The placeholder is what lets the host swap in
        // the real URL even then.
        out = replaceInlineImageRefs(markdown, pending, uploads.tokenFor)
        // Belt and braces: if a reference outlives the swap (overlapping matches), drop
        // the whole emission rather than let the data URI through — the suppression is
        // the load-bearing half of this.
        if (pendingInlineImageRefs(out).length) return
      }
      if (out === baseline) return
      onChangeRef.current(out)
    }

    // HTML-pasted (data:/blob:) images: rewrite their src to an uploaded URL.
    const uploads = inlineImageUploads({
      noteId,
      begin: (src) => onInlineImageRef.current(src),
      onTokenMinted: () => {
        // Deferred: the mint happens inside a ProseMirror view update, and serializing the
        // doc re-enters Milkdown's ctx. A microtask still runs long before the autosave
        // debounce, so the placeholder reaches the draft just as promptly.
        queueMicrotask(() => {
          if (!disposed && baseline !== null) forward(crepe.getMarkdown())
        })
      },
    })
    crepe.editor.use(uploads.plugin)

    // Register the listener BEFORE create() so no emission during editor build is
    // missed; while baseline is still null (pre-seed) any emission is the initial
    // normalization and is ignored — the editor isn't interactive yet, so it can't
    // be a user edit.
    crepe.on((listener) => {
      listener.markdownUpdated((_ctx, markdown) => {
        if (disposed || baseline === null) return
        forward(markdown)
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
