import { describe, expect, it, vi, beforeEach } from 'vitest'
import { commandsCtx, editorViewCtx, schemaCtx } from '@milkdown/kit/core'
import { Schema, type Node as ProseNode } from '@milkdown/kit/prose/model'
import { EditorState, TextSelection, type Transaction } from '@milkdown/kit/prose/state'
import type { Ctx } from '@milkdown/kit/ctx'
import {
  blockquoteSchema,
  bulletListSchema,
  headingSchema,
  inlineCodeSchema,
  liftListItemCommand,
  linkSchema,
  paragraphSchema,
  setBlockTypeCommand,
  toggleEmphasisCommand,
  toggleInlineCodeCommand,
  toggleStrongCommand,
  wrapInBlockTypeCommand,
} from '@milkdown/kit/preset/commonmark'
import { toggleLinkCommand } from '@milkdown/kit/component/link-tooltip'
import { applyFormat } from './noteFormat'

// applyFormat is what every toolbar button ends up in, and it is the one part of the bar
// with real branching: which press turns a heading back into a paragraph, which one lifts
// a bullet out instead of nesting another one, which one arms a mark for the next
// keystroke instead of marking a range. NoteView's own suite stubs the whole editor out,
// so none of that is covered there.
//
// The editor itself is not built here — milkdown reads everything it needs out of the
// ctx, so a ctx carrying a schema, a command recorder and a fake view is enough to drive
// the mapping. The node/mark NAMES below are milkdown's own ids ($nodeSchema('heading'),
// $markSchema('inlineCode'), …); `headingSchema.type(ctx)` is literally
// `ctx.get(schemaCtx).nodes.heading`, so a schema using those names resolves the same
// types the real editor would hand it.
const schema = new Schema({
  nodes: {
    doc: { content: 'block+' },
    paragraph: { group: 'block', content: 'inline*' },
    heading: { group: 'block', content: 'inline*', attrs: { level: { default: 1 } } },
    blockquote: { group: 'block', content: 'block+' },
    bullet_list: { group: 'block', content: 'list_item+' },
    list_item: { content: 'paragraph block*' },
    text: { group: 'inline' },
  },
  marks: {
    link: { attrs: { href: { default: '' } } },
    inlineCode: {},
  },
})

const nodes = schema.nodes
const para = (text: string) => nodes.paragraph.create(null, text ? schema.text(text) : null)
const heading = (level: number, text: string) => nodes.heading.create({ level }, schema.text(text))
const quote = (...children: ProseNode[]) => nodes.blockquote.create(null, children)
const list = (...items: ProseNode[]) => nodes.bullet_list.create(null, items)
const item = (...children: ProseNode[]) => nodes.list_item.create(null, children)
const doc = (...children: ProseNode[]) => nodes.doc.create(null, children)

// caretAt finds a position inside the text node holding `needle`, so the tests can say
// where the caret is in words rather than in hand-counted offsets.
function caretAt(root: ProseNode, needle: string): number {
  let found = -1
  root.descendants((node, pos) => {
    if (found === -1 && node.isText && node.text?.includes(needle)) found = pos + 1
    return found === -1
  })
  if (found === -1) throw new Error(`no text node containing ${needle}`)
  return found
}

const call = vi.fn()
const dispatch = vi.fn<(tr: Transaction) => void>()
const focus = vi.fn()

// mount hands applyFormat a ctx around one document and one selection.
function mount(root: ProseNode, anchor: number, head = anchor): Ctx {
  return mountState(EditorState.create({ doc: root, selection: TextSelection.create(root, anchor, head) }))
}

// mountState is the same ctx around a state assembled elsewhere — for the cases where the
// STORED-mark set is the thing under test and has to come from a real transaction.
function mountState(state: EditorState): Ctx {
  const view = { state, dispatch, focus }
  const values = new Map<unknown, unknown>([
    [schemaCtx, schema],
    [commandsCtx, { call }],
    [editorViewCtx, view],
  ])
  return { get: (slice: unknown) => values.get(slice) } as unknown as Ctx
}

const dispatched = () => dispatch.mock.calls.at(-1)?.[0] as Transaction

beforeEach(() => {
  call.mockReset()
  dispatch.mockReset()
  focus.mockReset()
})

describe('applyFormat headings', () => {
  it('turns a paragraph into the heading level pressed', () => {
    const root = doc(para('mléko'))
    applyFormat(mount(root, caretAt(root, 'mléko')), 'h2')
    expect(call).toHaveBeenCalledWith(setBlockTypeCommand.key, {
      nodeType: schema.nodes.heading,
      attrs: { level: 2 },
    })
  })

  // There is no "normal text" button beside them, so the heading button is also the only
  // way back out of a heading.
  it('turns a heading of the pressed level back into a paragraph', () => {
    const root = doc(heading(1, 'Nákup'))
    applyFormat(mount(root, caretAt(root, 'Nákup')), 'h1')
    expect(call).toHaveBeenCalledWith(setBlockTypeCommand.key, { nodeType: schema.nodes.paragraph })
  })

  it('re-levels a heading of a different level rather than clearing it', () => {
    const root = doc(heading(1, 'Nákup'))
    applyFormat(mount(root, caretAt(root, 'Nákup')), 'h2')
    expect(call).toHaveBeenCalledWith(setBlockTypeCommand.key, {
      nodeType: schema.nodes.heading,
      attrs: { level: 2 },
    })
  })

  // setBlockTypeCommand rewrites every block in the range, so "is it already a heading"
  // has to be asked of the whole range. Asked of the first block only, a selection running
  // out of a heading into the text below it reads as "already H1" and flattens both.
  it('promotes a selection running from a heading into the paragraph below it', () => {
    const root = doc(heading(1, 'Nákup'), para('mléko'))
    applyFormat(mount(root, caretAt(root, 'Nákup'), caretAt(root, 'mléko')), 'h1')
    expect(call).toHaveBeenCalledWith(setBlockTypeCommand.key, {
      nodeType: schema.nodes.heading,
      attrs: { level: 1 },
    })
  })

  it('clears a selection whose every block is already the pressed level', () => {
    const root = doc(heading(1, 'Nákup'), heading(1, 'Úklid'))
    applyFormat(mount(root, caretAt(root, 'Nákup'), caretAt(root, 'Úklid')), 'h1')
    expect(call).toHaveBeenCalledWith(setBlockTypeCommand.key, { nodeType: schema.nodes.paragraph })
  })
})

describe('applyFormat lists and quotes', () => {
  it('wraps a loose paragraph in a bullet list', () => {
    const root = doc(para('mléko'))
    applyFormat(mount(root, caretAt(root, 'mléko')), 'bulletList')
    expect(call).toHaveBeenCalledWith(wrapInBlockTypeCommand.key, { nodeType: schema.nodes.bullet_list })
  })

  // Without the lift this press wraps, and wrapping inside a list item finds no valid
  // wrapping at all — the button simply does nothing on the content it is named after.
  it('lifts an item out instead of wrapping it again', () => {
    const root = doc(list(item(para('mléko'))))
    applyFormat(mount(root, caretAt(root, 'mléko')), 'bulletList')
    expect(call).toHaveBeenCalledWith(liftListItemCommand.key)
    expect(call).not.toHaveBeenCalledWith(wrapInBlockTypeCommand.key, expect.anything())
  })

  it('wraps a loose paragraph in a blockquote', () => {
    const root = doc(para('cituji'))
    applyFormat(mount(root, caretAt(root, 'cituji')), 'quote')
    expect(call).toHaveBeenCalledWith(wrapInBlockTypeCommand.key, { nodeType: schema.nodes.blockquote })
  })

  it('lifts out of a blockquote instead of nesting a second one', () => {
    const root = doc(quote(para('cituji')))
    applyFormat(mount(root, caretAt(root, 'cituji')), 'quote')
    expect(call).not.toHaveBeenCalledWith(wrapInBlockTypeCommand.key, expect.anything())
    expect(dispatch).toHaveBeenCalledTimes(1)
    // The lift really removed the blockquote, not merely dispatched something.
    expect(dispatched().doc.firstChild?.type).toBe(schema.nodes.paragraph)
  })

  // A bare `lift` unwraps the NEAREST liftable parent, which inside `> - mléko` is the
  // list item — so the quote button used to un-bullet and leave the quote standing. The
  // ancestor has to be named, or the button undoes something other than what it applies.
  it('lifts a quoted bullet out of the quote, leaving the bullet alone', () => {
    const root = doc(quote(list(item(para('mléko')))))
    applyFormat(mount(root, caretAt(root, 'mléko')), 'quote')
    const lifted = dispatched().doc
    expect(lifted.firstChild?.type).toBe(schema.nodes.bullet_list)
    expect(lifted.firstChild?.textContent).toBe('mléko')
  })

  // The mirror image, and the reason the list branch never needed this: liftListItem finds
  // its own range by naming the list, so `•` inside a quoted bullet stays on the bullet.
  it('lifts the item, not the quote, when the bullet button is pressed inside a quote', () => {
    const root = doc(quote(list(item(para('mléko')))))
    applyFormat(mount(root, caretAt(root, 'mléko')), 'bulletList')
    expect(call).toHaveBeenCalledWith(liftListItemCommand.key)
  })
})

describe('applyFormat marks', () => {
  it('maps bold and italic straight through', () => {
    const root = doc(para('mléko'))
    applyFormat(mount(root, caretAt(root, 'mléko')), 'bold')
    expect(call).toHaveBeenCalledWith(toggleStrongCommand.key)
    applyFormat(mount(root, caretAt(root, 'mléko')), 'italic')
    expect(call).toHaveBeenCalledWith(toggleEmphasisCommand.key)
  })

  it('marks the selected range with code', () => {
    const root = doc(para('mléko'))
    const from = caretAt(root, 'mléko')
    applyFormat(mount(root, from, from + 5), 'code')
    expect(call).toHaveBeenCalledWith(toggleInlineCodeCommand.key)
  })

  // An empty caret has no range to mark, so code has to arm the next keystroke instead —
  // otherwise the button does nothing exactly where someone reaches for it.
  it('arms code as a stored mark on an empty caret', () => {
    const root = doc(para('mléko'))
    applyFormat(mount(root, caretAt(root, 'mléko')), 'code')
    expect(call).not.toHaveBeenCalledWith(toggleInlineCodeCommand.key)
    expect(dispatched().storedMarks?.map((m) => m.type)).toEqual([schema.marks.inlineCode])
  })

  it('disarms code when the caret already carries it', () => {
    const coded = nodes.paragraph.create(null, schema.text('mléko', [schema.marks.inlineCode.create()]))
    const root = doc(coded)
    applyFormat(mount(root, caretAt(root, 'mléko')), 'code')
    expect(dispatched().storedMarks).toEqual([])
  })
})

// linked builds a paragraph whose whole text carries one link mark.
const linkedPara = (text: string, href = 'https://example.test') =>
  nodes.paragraph.create(null, schema.text(text, [schema.marks.link.create({ href })]))

describe('applyFormat link', () => {
  it('opens the URL editor over a selection and leaves focus to it', () => {
    const root = doc(para('mléko'))
    const from = caretAt(root, 'mléko')
    applyFormat(mount(root, from, from + 5), 'link')
    expect(call).toHaveBeenCalledWith(toggleLinkCommand.key)
    // The tooltip focuses its own input a frame later; pulling the caret back into the
    // doc here would take it straight off that input again.
    expect(focus).not.toHaveBeenCalled()
  })

  // An empty caret inside a link has no range to give a new href to. Opening the editor
  // there splices a SECOND link into the middle of the first one on confirm.
  it('disarms the link mark on an empty caret inside a link', () => {
    const root = doc(linkedPara('mléko'))
    applyFormat(mount(root, caretAt(root, 'mléko')), 'link')
    expect(call).not.toHaveBeenCalledWith(toggleLinkCommand.key)
    expect(dispatched().storedMarks).toEqual([])
  })

  // The press above leaves an EMPTY stored-mark set behind, and nothing moves the caret
  // between two clicks on the same button — so a guard that reads the stored set first
  // answers "no link here" the second time and opens the URL editor over the very range
  // it just refused. The guard has to ask the document.
  it('still refuses on a second press, when the first press emptied the stored marks', () => {
    const root = doc(linkedPara('mléko'))
    const at = caretAt(root, 'mléko')
    const state = EditorState.create({ doc: root, selection: TextSelection.create(root, at) })
    // Exactly the state the first press leaves: a cursor still inside the link, carrying a
    // stored set that no longer holds it.
    const after = state.apply(state.tr.removeStoredMark(schema.marks.link))
    expect(after.storedMarks).toEqual([])

    applyFormat(mountState(after), 'link')
    expect(call).not.toHaveBeenCalledWith(toggleLinkCommand.key)
  })
})

describe('applyFormat focus', () => {
  // A keyboard activation (Tab to the button, Enter) really did move focus out of the
  // editor, so every branch but the link one puts the caret back.
  it('returns the caret to the editor after a block or mark command', () => {
    const root = doc(para('mléko'))
    applyFormat(mount(root, caretAt(root, 'mléko')), 'bold')
    expect(focus).toHaveBeenCalledTimes(1)
  })
})

describe('applyFormat schema resolution', () => {
  // The mapping resolves schema types by milkdown's own ids; if one of them ever stopped
  // matching, every test above would still pass against the wrong node.
  it('resolves the milkdown schema ids the mapping depends on', () => {
    const root = doc(para('mléko'))
    const ctx = mount(root, caretAt(root, 'mléko'))
    expect(headingSchema.type(ctx)).toBe(schema.nodes.heading)
    expect(paragraphSchema.type(ctx)).toBe(schema.nodes.paragraph)
    expect(blockquoteSchema.type(ctx)).toBe(schema.nodes.blockquote)
    expect(bulletListSchema.type(ctx)).toBe(schema.nodes.bullet_list)
    expect(inlineCodeSchema.type(ctx)).toBe(schema.marks.inlineCode)
    expect(linkSchema.type(ctx)).toBe(schema.marks.link)
  })
})
