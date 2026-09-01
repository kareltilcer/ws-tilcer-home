// The rule the Vizuální editor asks before it moves the caret on an empty note, kept in
// its own module for the same reason noteFormat.ts is: MilkdownEditor evaluates Crepe at
// import and jsdom cannot load it, so anything left in there cannot be exercised by a
// test. This half is pure DOM predicate — no ProseMirror, no Crepe — so out here it can be.

// isTextEntry: a control the user could be WRITING IN right now. Tag names rather than
// `instanceof HTMLInputElement`, so an element from another document (the Nástěnka
// overlay renders through a portal) is judged by what it is, not by which realm minted it —
// and `closest` rather than `isContentEditable` for the same reason plus one more: editable
// is INHERITED, so the caret can sit on a child that carries no attribute of its own, and
// jsdom implements the attribute but not the property (an `isContentEditable` branch here
// could never be tested). Deliberately coarse — a checkbox is not text entry but is caught
// anyway — because the two ways of being wrong are not equal: refusing costs the user one
// extra tap, taking the caret out of a field they are typing in costs them what they typed.
const isTextEntry = (el: Element): boolean =>
  el.tagName === 'INPUT' ||
  el.tagName === 'TEXTAREA' ||
  el.closest('[contenteditable]:not([contenteditable="false"])') !== null

// mayTakeCaret answers the one question the editor has to ask before it takes the caret:
// has anyone claimed it since we started mounting? There are TWO waits stacked between the
// decision and the caret — the lazy chunk (ProseMirror + CodeMirror, seconds on a phone)
// and then create(), which is a promise — and the header is interactive through both, so
// the user has every chance to put the caret somewhere themselves first.
//
// `root` is the editor's host element, `opened` whatever held focus when it mounted, and
// `active` whatever holds it now. Unclaimed means:
//   - nothing focused at all (a page load, or a tap that focused nothing, leaves body);
//   - an ANCESTOR of the root — the Nástěnka overlay's Radix dialog focuses itself on open;
//   - anything already inside the editor;
//   - the same element we mounted under — Chrome leaves the "Vizuální" tab button focused
//     after the tap that opened us, and taking the caret off a button costs nothing.
//
// …EXCEPT when that last one is a field the user could be writing in. The rename pencil one
// header up opens a field that takes the caret itself, and it opens during the wait: tap it
// while the editor chunk is still loading and the field, not the tab button, is the element
// the editor mounts under. "The same element we mounted under" would then hand the caret
// straight out of a half-typed title — and that field commits on blur, so the theft lands on
// a write path. Same theft as a tap during create(), through the other door.
export function mayTakeCaret(root: HTMLElement, opened: Element | null, active: Element | null): boolean {
  if (active === null || active === root.ownerDocument.body) return true
  if (active.contains(root) || root.contains(active)) return true
  return active === opened && !isTextEntry(active)
}
