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
 * anywhere in the picture; a pinch-zoom shrinks the visual viewport by whatever the
 * member pinched to. So a reading counts only at scale 1, only past a threshold no
 * browser chrome reaches, and only while the focus is somewhere that opens a
 * keyboard — which is also the state the caller cares about, since a layout that
 * rearranges itself is only welcome while somebody is writing.
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
  /** `visualViewport.height` — the part of it left on screen above the keyboard. */
  visual: number
  /** `visualViewport.scale` — 1 unless the member has pinched. */
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

const CLOSED: SoftKeyboard = { open: false, viewport: 0 }

/** The `<input>` types that open a picker or nothing at all, never a keyboard. */
const NO_KEYBOARD = new Set([
  'button',
  'checkbox',
  'color',
  'file',
  'hidden',
  'image',
  'radio',
  'range',
  'reset',
  'submit',
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
  // A pinched page reports a visual viewport smaller than the layout one for a
  // reason that has nothing to do with a keyboard, and the difference can be
  // hundreds of pixels. Only an unzoomed reading means what this reads it as.
  if (Math.abs(frame.scale - 1) > 0.01) return CLOSED
  const covered = frame.layout - frame.visual
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
