import { commandsCtx, editorViewCtx } from '@milkdown/kit/core'
import { lift } from '@milkdown/kit/prose/commands'
import type { Ctx } from '@milkdown/kit/ctx'
import type { EditorState } from '@milkdown/kit/prose/state'
import type { MarkType, NodeType } from '@milkdown/kit/prose/model'
import {
  blockquoteSchema,
  bulletListSchema,
  headingSchema,
  inlineCodeSchema,
  liftListItemCommand,
  linkSchema,
  listItemSchema,
  paragraphSchema,
  setBlockTypeCommand,
  toggleEmphasisCommand,
  toggleInlineCodeCommand,
  toggleStrongCommand,
  wrapInBlockTypeCommand,
} from '@milkdown/kit/preset/commonmark'
import { toggleLinkCommand } from '@milkdown/kit/component/link-tooltip'

// The Vizuální toolbar's contract with the editor, kept in its own module so the three
// files that share it — the bar, its host, and the editor — do not all have to reach into
// MilkdownEditor (and so the mapping below can be exercised without evaluating Crepe).
// The bar and the host import ONLY the types from here, which erase at build, so
// ProseMirror stays out of the landing bundle exactly as it does today.

// NoteFormatCommand is the closed set the Vizuální toolbar can ask for: the minimal bar
// the design draws (headings, bold/italic, list, quote, code, link) and no more — this is
// a household notes app, not a CMS.
export type NoteFormatCommand = 'h1' | 'h2' | 'bold' | 'italic' | 'bulletList' | 'quote' | 'code' | 'link'

// NoteEditorHandle is the toolbar's whole reach into the editor.
export type NoteEditorHandle = { format: (command: NoteFormatCommand) => void }

// markArmed reports what the NEXT keystroke at an empty caret would carry: the stored-mark
// set once anything has been toggled, and the marks on the text the caret is in until then.
// That precedence is what makes the code button a toggle — its own removeStoredMark leaves
// an empty stored set, which the next press correctly reads as "not armed" and re-arms.
function markArmed(state: EditorState, type: MarkType): boolean {
  return (state.storedMarks ?? state.selection.$from.marks()).some((m) => m.type === type)
}

// markAtCaret is the question the LINK guard has to ask instead: is there a link here AT
// ALL — in the document, or armed. Asking markArmed makes the guard leaky in exactly two
// clicks: it answers out of the stored set whenever there is one, and the guard's own
// removeStoredMark leaves an EMPTY stored set behind, so a second press on the caret the
// first press just refused reads "no link here" and opens the URL editor over the empty
// range — which is the mangling the guard exists to prevent (see the link branch). Nothing
// moves the caret between two clicks on the same button, so that is a press away, not an
// edge case. The two helpers cannot be merged: this rule would stop the code button from
// ever re-arming inside code text.
function markAtCaret(state: EditorState, type: MarkType): boolean {
  const inDoc = state.selection.$from.marks().some((m) => m.type === type)
  return inDoc || (state.storedMarks?.some((m) => m.type === type) ?? false)
}

// insideNode reports whether the block holding the caret sits anywhere inside `type` —
// how the list and quote buttons tell "wrap this" from "this is already wrapped".
function insideNode(state: EditorState, type: NodeType): boolean {
  const { $from } = state.selection
  for (let depth = $from.depth; depth > 0; depth--) if ($from.node(depth).type === type) return true
  return false
}

// wholeRangeIsHeading answers the question setBlockTypeCommand actually acts on: is EVERY
// textblock in the selection already a heading of this level? Asking only the block the
// selection starts in ($from.parent) reads a selection running from a heading down into
// the paragraph below it as "already a heading" and flattens both, which is the opposite
// of what was asked for.
function wholeRangeIsHeading(ctx: Ctx, state: EditorState, level: number): boolean {
  const heading = headingSchema.type(ctx)
  const { from, to } = state.selection
  let blocks = 0
  let all = true
  state.doc.nodesBetween(from, to, (node) => {
    if (!node.isTextblock) return true
    blocks += 1
    if (node.type !== heading || node.attrs.level !== level) all = false
    return false
  })
  return blocks > 0 && all
}

// applyFormat maps one toolbar command onto milkdown's commands. The mapping mirrors
// Crepe's own `top-bar` feature (which ships disabled, and English) command for command,
// so the toolbar, the slash menu and the keymap can never disagree about what a given
// piece of formatting means. It departs from that reference in one deliberate place: the
// bar has no "normal text" entry, so each button that CHANGES a block is also the way
// back out of it (see the headings, list and quote branches).
export function applyFormat(ctx: Ctx, command: NoteFormatCommand) {
  const commands = ctx.get(commandsCtx)
  const view = ctx.get(editorViewCtx)
  switch (command) {
    case 'h1':
    case 'h2': {
      // Two buttons and no "normal text" entry beside them, so a heading button is also
      // the only way back OUT of a heading: on a block that already is this level, return
      // it to a paragraph rather than re-applying the level it has, which would look like
      // a dead button.
      const level = command === 'h1' ? 1 : 2
      const active = wholeRangeIsHeading(ctx, view.state, level)
      commands.call(
        setBlockTypeCommand.key,
        active ? { nodeType: paragraphSchema.type(ctx) } : { nodeType: headingSchema.type(ctx), attrs: { level } },
      )
      break
    }
    case 'bold':
      commands.call(toggleStrongCommand.key)
      break
    case 'italic':
      commands.call(toggleEmphasisCommand.key)
      break
    case 'bulletList':
      // Wrapping is only half a bullet button. Pressed on a line that ALREADY is one,
      // `wrapIn` finds no valid wrapping (list_item's content is `paragraph block*`, so a
      // list cannot be its first child) and the command just returns false — a button
      // that visibly does nothing on the very content it is about. Lift the item out
      // instead: the same "the button is also the way back out" rule the headings follow.
      if (insideNode(view.state, listItemSchema.type(ctx))) commands.call(liftListItemCommand.key)
      else commands.call(wrapInBlockTypeCommand.key, { nodeType: bulletListSchema.type(ctx) })
      break
    case 'quote':
      // The quote button has the sharper end of the same problem: blockquote's content is
      // `block+`, so wrapping a quote in a quote IS valid and a second press silently
      // indents again instead of undoing. `lift` is the unwrap milkdown ships no command
      // for — it takes the block range out of the blockquote around it.
      if (insideNode(view.state, blockquoteSchema.type(ctx))) lift(view.state, view.dispatch)
      else commands.call(wrapInBlockTypeCommand.key, { nodeType: blockquoteSchema.type(ctx) })
      break
    case 'code': {
      // With nothing selected there is no range to mark, so toggling code has to arm the
      // NEXT characters typed instead — a stored mark. Without this the button does
      // nothing at all on an empty caret, which is exactly where someone reaches for it.
      const { state } = view
      if (!state.selection.empty) {
        commands.call(toggleInlineCodeCommand.key)
        break
      }
      const markType = inlineCodeSchema.type(ctx)
      view.dispatch(
        markArmed(state, markType) ? state.tr.removeStoredMark(markType) : state.tr.addStoredMark(markType.create()),
      )
      break
    }
    case 'link': {
      const { state } = view
      const linkType = linkSchema.type(ctx)
      // An empty caret inside an existing link has no range to give a new href to, and
      // ToggleLink would open the URL editor over that empty range — confirming there
      // inserts the URL as text inside the existing link and re-marks it, splicing a
      // SECOND href into the middle of the first. Disarm the mark instead, so what is
      // typed next is plain text. (Crepe's own bar guards the same case.)
      //
      // markAtCaret, not markArmed: the guard must not be blinded by the empty stored set
      // its own dispatch leaves behind, or the second press does the very thing the first
      // one refused — and autosave persists the mangled link. (`break` rather than the
      // `return` below, so the caret still goes back into the doc afterwards.)
      if (state.selection.empty && markAtCaret(state, linkType)) {
        view.dispatch(state.tr.removeStoredMark(linkType))
        break
      }
      // The LINK TOOLTIP's ToggleLink, not commonmark's: it opens the URL editor over the
      // selection and focuses that input a frame later, so this branch returns without
      // pulling focus back into the doc. Commonmark's would apply a link mark with an
      // empty href and leave no way to fill one in.
      commands.call(toggleLinkCommand.key)
      return
    }
  }
  // A keyboard activation (Tab to the button, Enter) genuinely moved focus out of the
  // editor — the pointer path never does, since the button prevents its own mousedown.
  // Put the caret back either way, so typing continues where the formatting just landed.
  view.focus()
}
