import { useEffect, useState } from 'react'

/**
 * The on-screen keyboard — measured, because nothing reports it.
 *
 * ⚠ NO BROWSER TELLS A PAGE THAT THE KEYBOARD IS UP. What it does tell the page is
 * that the VISUAL viewport — the part actually on screen — got shorter while the
 * LAYOUT viewport, which is what `dvh` and `position: fixed` are measured against,
 * did not. That difference IS the strip the keyboard is covering, and it is the only
 * signal there is. The `interactive-widget` viewport property would move the layout
 * viewport instead and take the arithmetic away entirely, but Safari does not
 * implement it — so it would fix one half of the household's phones and leave the
 * other half exactly as broken.
 *
 * ⚠ A GAP ON ITS OWN IS NOT A KEYBOARD, WHICH IS WHY THIS IS NOT ONE SUBTRACTION.
 * Chrome on Android holds `window.innerHeight` at the large viewport while its
 * address bar is on screen, so the two heights disagree by ~56 px with no keyboard
 * anywhere in the picture; and `visualViewport.height` counts the CSS pixels of the
 * page the visible strip COVERS, so a zoomed page reports a shorter visual viewport
 * for a reason that has nothing to do with a keyboard either. So a gap counts only
 * once it has been put back into the layout viewport's own units, only past a
 * threshold no browser chrome reaches, and only while the focus is somewhere that
 * opens a keyboard — which is also the state the caller cares about, since a layout
 * that rearranges itself is only welcome while somebody is writing.
 *
 * ⚠ AND THE ZOOM IS CORRECTED FOR RATHER THAN REFUSED, WHICH IS THE HALF THAT MAKES
 * THIS WORK ON iOS AT ALL (review round 2). Declining every reading at scale ≠ 1
 * looks like the careful choice and is the opposite of one: Safari ZOOMS THE PAGE
 * whenever a field whose computed font-size is under 16 px takes focus, and every
 * field in this app is `text-sm` under a viewport meta carrying no `maximum-scale`.
 * The composer's own focus is therefore what puts the page at ~1.14 — so a scale-1
 * veto would have declined to see the keyboard on exactly the half of the
 * household's phones this arithmetic was written for, and the one browser that
 * cannot be rescued by `interactive-widget` instead. Multiplied back, a pinch still
 * reads as no keyboard, because pinching to 2× halves `visualViewport.height` and
 * the multiplication hands it straight back.
 */

/**
 * The gap below which an obscured strip is browser chrome rather than a keyboard.
 *
 * A phone keyboard is ~250–320 px upright and ~200 px on its side. Chrome's address
 * bar is ~56 px, Safari's toolbars ~90 px, and Samsung Internet's two bars together
 * are under 120. 150 sits in the gap between the two families with room either way.
 */
export const KEYBOARD_MIN_PX = 150

/** One reading of the two viewports, so the decision below can be tested without a browser. */
export interface ViewportFrame {
  /** `window.innerHeight` — the layout viewport, which a keyboard does not shrink. */
  layout: number
  /** `visualViewport.height` — how many of those pixels the strip above the keyboard
   *  covers, which is fewer than the strip is tall once the page is zoomed. */
  visual: number
  /** `visualViewport.scale` — 1 unless the page is zoomed, by a pinch or by Safari. */
  scale: number
  /** Whether the focus is in something that opens a keyboard. */
  typing: boolean
}

export interface SoftKeyboard {
  /** True while a keyboard is covering the bottom of the layout viewport. */
  open: boolean
  /** The height left on screen above it, in CSS pixels. 0 while closed. */
  viewport: number
}

/** ⚠ FROZEN, because it is the ONE object every closed reading hands back. */
const CLOSED: SoftKeyboard = Object.freeze({ open: false, viewport: 0 })

/**
 * The `<input>` types that open a picker or nothing at all, never a keyboard.
 *
 * ⚠ THE DATE AND TIME FAMILY BELONGS IN HERE (review round 2). Every one of them
 * opens a wheel or a calendar on a phone — tall enough to clear the threshold below
 * — and this hook answers whether somebody is WRITING. Left out, opening the date
 * field in an event, an electricity advance or a schedule took the thumb-tab bar
 * away for a keyboard that was never there.
 */
const NO_KEYBOARD = new Set([
  'button',
  'checkbox',
  'color',
  'date',
  'datetime-local',
  'file',
  'hidden',
  'image',
  'month',
  'radio',
  'range',
  'reset',
  'submit',
  'time',
  'week',
])

/** Whether this element is one a member types into. */
export function isTypingTarget(el: Element | null): boolean {
  if (!(el instanceof HTMLElement)) return false
  if (el.isContentEditable) return true
  if (el instanceof HTMLTextAreaElement) return true
  // `type` normalises an unknown or absent attribute to "text", so a bare <input>
  // counts and <input type="nonsense"> does too — which is what the browser does
  // with it as well.
  if (el instanceof HTMLInputElement) return !NO_KEYBOARD.has(el.type)
  return false
}

/** The decision, separated from the DOM it is read out of. */
export function readSoftKeyboard(frame: ViewportFrame): SoftKeyboard {
  if (!frame.typing) return CLOSED
  // ⚠ `visual * scale`, NOT `visual`. The two terms are in different units until the
  // multiplication: `layout` is the page's CSS pixels, and `visual` is how many of
  // them the glass currently COVERS — so zooming in halves it over the same strip of
  // screen. Corrected, a pinch to 2× cancels to a gap of zero and Safari's own
  // auto-zoom onto a focused field stops swallowing the keyboard behind it.
  const covered = frame.layout - frame.visual * frame.scale
  if (covered < KEYBOARD_MIN_PX) return CLOSED
  return { open: true, viewport: frame.visual }
}

/**
 * useSoftKeyboard watches the two viewports and answers whether a keyboard is up.
 *
 * ⚠ IT LISTENS FOR THE RESIZE AND FOR `focusin`, AND DELIBERATELY NOT FOR
 * `focusout`. A keyboard that goes away always resizes the visual viewport, so the
 * resize alone closes this — while `focusout` fires BEFORE the next element takes
 * focus, with `document.activeElement` transiently on `<body>`. Listening for it
 * would report the keyboard gone and back for every move between two fields, and
 * the layout would flinch each time on a keyboard that never moved.
 */
export function useSoftKeyboard(): SoftKeyboard {
  const [keyboard, setKeyboard] = useState<SoftKeyboard>(CLOSED)

  useEffect(() => {
    const vv = window.visualViewport
    // A browser without the API simply never reports a keyboard, which leaves the
    // layout exactly as it was before this hook existed.
    if (!vv) return

    const read = () => {
      const next = readSoftKeyboard({
        layout: window.innerHeight,
        visual: vv.height,
        scale: vv.scale,
        typing: isTypingTarget(document.activeElement),
      })
      // ⚠ COMPARED BEFORE IT IS STORED. `resize` fires on every frame of the
      // keyboard's slide-in, and a fresh object each time is a re-render of the
      // whole shell each time — for the frames where the answer did not change.
      setKeyboard((prev) =>
        prev.open === next.open && prev.viewport === next.viewport ? prev : next,
      )
    }

    read()
    vv.addEventListener('resize', read)
    document.addEventListener('focusin', read)
    return () => {
      vv.removeEventListener('resize', read)
      document.removeEventListener('focusin', read)
    }
  }, [])

  return keyboard
}
