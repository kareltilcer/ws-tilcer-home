import { describe, expect, it, beforeEach } from 'vitest'
import { mayTakeCaret } from './noteFocus'

// WHO OWNS THE CARET WHILE THE EDITOR IS STILL MOUNTING. An empty note asks for the caret
// so a phone raises its keyboard, but the editor is behind a lazy chunk AND behind a
// create() promise, and the header stays interactive through both. Everything below is one
// question asked twice over: did the user claim the caret in that gap? If they did it stays
// with them, and the note costs one extra tap — the cheap half of being wrong.

// The header the note renders above the editor, which is what the user can reach during the
// wait: the rename field (a real control, and one that commits on blur) and the mode tabs.
function fixture() {
  document.body.innerHTML = ''
  const dialog = document.createElement('div') // stands in for the Nástěnka overlay's Radix content
  dialog.tabIndex = -1
  const rename = document.createElement('input')
  const tab = document.createElement('button')
  const root = document.createElement('div') // the Crepe host
  const inside = document.createElement('div') // ProseMirror, once create() has built it
  inside.contentEditable = 'true'
  root.append(inside)
  dialog.append(rename, tab, root)
  document.body.append(dialog)
  return { dialog, rename, tab, root, inside }
}

describe('mayTakeCaret', () => {
  let f: ReturnType<typeof fixture>
  beforeEach(() => {
    f = fixture()
  })

  // A page load leaves activeElement at body, and so does a tap on something that takes no
  // focus. Nobody is holding the caret, so the note the user opened may have it.
  it('takes the caret when nothing holds it', () => {
    expect(mayTakeCaret(f.root, null, null)).toBe(true)
    expect(mayTakeCaret(f.root, null, document.body)).toBe(true)
  })

  // Chrome leaves the tab button focused after the tap that opened us; Safari focuses no
  // button at all. Taking the caret off a button we were opened by costs nothing.
  it('takes the caret from the button it was opened under', () => {
    expect(mayTakeCaret(f.root, f.tab, f.tab)).toBe(true)
  })

  // The Nástěnka overlay's dialog focuses itself on open, and it wraps the editor — that is
  // the surface handing the note over, not a user claiming the caret.
  it('takes the caret from an ancestor of the editor', () => {
    expect(mayTakeCaret(f.root, f.dialog, f.dialog)).toBe(true)
  })

  it('takes the caret when it is already inside the editor', () => {
    expect(mayTakeCaret(f.root, f.tab, f.inside)).toBe(true)
  })

  // The mid-create tap: the editor mounted under the tab button, and the user reached the
  // rename field while create() was still running.
  it('leaves the caret in a control claimed after the editor mounted', () => {
    expect(mayTakeCaret(f.root, f.tab, f.rename)).toBe(false)
  })

  // THE SAME THEFT THROUGH THE OTHER DOOR, and the one "the element we mounted under"
  // used to wave through. The rename field opens on a tap and takes the caret itself, and
  // the editor's lazy chunk can still be loading when it does — so the field, not the tab
  // button, is what the editor mounts under. It commits the title on blur, so handing the
  // caret out of it lands a half-typed name on a write path.
  it('leaves the caret in a text field it was opened under', () => {
    expect(mayTakeCaret(f.root, f.rename, f.rename)).toBe(false)
  })

  it('leaves the caret in a textarea it was opened under', () => {
    const ta = document.createElement('textarea')
    f.dialog.append(ta)
    expect(mayTakeCaret(f.root, ta, ta)).toBe(false)
  })

  // A contenteditable OUTSIDE our root is somebody else's writing surface; only one inside
  // it is ours to take from (covered above). Its CHILD counts too — editable is inherited,
  // so the element actually holding the caret often carries no attribute of its own.
  it('leaves the caret in a contenteditable it was opened under', () => {
    const ce = document.createElement('div')
    ce.setAttribute('contenteditable', 'true')
    const line = document.createElement('p')
    ce.append(line)
    f.dialog.append(ce)
    expect(mayTakeCaret(f.root, ce, ce)).toBe(false)
    expect(mayTakeCaret(f.root, line, line)).toBe(false)
  })
})
