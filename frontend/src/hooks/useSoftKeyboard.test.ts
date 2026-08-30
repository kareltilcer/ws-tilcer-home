import { describe, expect, it, beforeEach, afterEach } from 'vitest'
import { act, renderHook } from '@testing-library/react'
import {
  isTypingTarget,
  KEYBOARD_MIN_PX,
  readSoftKeyboard,
  useSoftKeyboard,
  type ViewportFrame,
} from './useSoftKeyboard'

// A phone at 375x812 with a ~300 px keyboard is the frame every number below is
// taken from; 812 is also what jsdom is told window.innerHeight is.
const LAYOUT = 812

function frame(over: Partial<ViewportFrame> = {}): ViewportFrame {
  return { layout: LAYOUT, visual: LAYOUT, scale: 1, typing: true, ...over }
}

describe('readSoftKeyboard — a gap is only a keyboard under three conditions', () => {
  it('reports a keyboard, and the height it left on screen', () => {
    expect(readSoftKeyboard(frame({ visual: 512 }))).toEqual({ open: true, viewport: 512 })
  })

  it('ignores a gap while nobody is typing — the layout only moves for a writer', () => {
    expect(readSoftKeyboard(frame({ visual: 512, typing: false }))).toEqual({
      open: false,
      viewport: 0,
    })
  })

  it("ignores Chrome's address bar, which is the same shape of gap at ~56 px", () => {
    expect(readSoftKeyboard(frame({ visual: LAYOUT - 56 }))).toEqual({ open: false, viewport: 0 })
  })

  it('holds the threshold at KEYBOARD_MIN_PX, inclusive', () => {
    expect(readSoftKeyboard(frame({ visual: LAYOUT - KEYBOARD_MIN_PX + 1 })).open).toBe(false)
    expect(readSoftKeyboard(frame({ visual: LAYOUT - KEYBOARD_MIN_PX })).open).toBe(true)
  })

  it('ignores a pinched page, where the whole difference is the zoom', () => {
    expect(readSoftKeyboard(frame({ visual: 400, scale: 2 }))).toEqual({ open: false, viewport: 0 })
  })

  it('reads a keyboard THROUGH a zoom rather than declining to look', () => {
    // Safari zooms a page to ~16/14 the moment a `text-sm` field takes focus, which
    // is every field in this app: the strip above a 300 px keyboard is 512 px of
    // glass, and at that scale it covers 448 of the page's own CSS pixels.
    expect(readSoftKeyboard(frame({ visual: 448, scale: 16 / 14 }))).toEqual({
      open: true,
      viewport: 448,
    })
  })

  it('finds a keyboard under a pinch too, because the zoom is arithmetic and not a veto', () => {
    // The same 300 px keyboard, pinched to 2×: 512 px of glass over 256 page pixels.
    expect(readSoftKeyboard(frame({ visual: 256, scale: 2 }))).toEqual({
      open: true,
      viewport: 256,
    })
  })

  it('ignores a visual viewport larger than the layout one rather than reporting a negative', () => {
    expect(readSoftKeyboard(frame({ visual: LAYOUT + 40 })).open).toBe(false)
  })
})

describe('isTypingTarget', () => {
  it('accepts what opens a keyboard', () => {
    const textarea = document.createElement('textarea')
    const text = document.createElement('input')
    const bare = document.createElement('input')
    const search = document.createElement('input')
    search.type = 'search'
    text.type = 'text'
    const rich = document.createElement('div')
    rich.contentEditable = 'true'
    // jsdom does not compute isContentEditable from the attribute, so assert the
    // branch through the property the browser exposes.
    Object.defineProperty(rich, 'isContentEditable', { value: true })

    expect(isTypingTarget(textarea)).toBe(true)
    expect(isTypingTarget(text)).toBe(true)
    expect(isTypingTarget(bare)).toBe(true)
    expect(isTypingTarget(search)).toBe(true)
    expect(isTypingTarget(rich)).toBe(true)
  })

  it('refuses what does not', () => {
    const checkbox = document.createElement('input')
    checkbox.type = 'checkbox'
    const file = document.createElement('input')
    file.type = 'file'
    // A wheel and a calendar cover the bottom of a phone exactly as a keyboard does,
    // and neither is somebody writing — which is the question this answers.
    // All five of the family, because all five are in the app: `date` in an event
    // and an electricity advance, `time` in a schedule, `month` in a payment.
    const pickers = ['date', 'datetime-local', 'month', 'time', 'week'].map((type) => {
      const el = document.createElement('input')
      el.type = type
      return el
    })

    expect(isTypingTarget(checkbox)).toBe(false)
    expect(isTypingTarget(file)).toBe(false)
    for (const picker of pickers) {
      expect(isTypingTarget(picker), `${picker.type} opens a wheel, not a keyboard`).toBe(false)
    }
    expect(isTypingTarget(document.createElement('button'))).toBe(false)
    expect(isTypingTarget(document.createElement('div'))).toBe(false)
    expect(isTypingTarget(null)).toBe(false)
  })
})

/** Stands in for the VisualViewport jsdom does not implement. */
class FakeViewport {
  height = LAYOUT
  scale = 1
  private readonly listeners = new Set<() => void>()
  addEventListener(_type: string, fn: () => void) {
    this.listeners.add(fn)
  }
  removeEventListener(_type: string, fn: () => void) {
    this.listeners.delete(fn)
  }
  /** What the browser does as the keyboard slides in. */
  resize(height: number) {
    this.height = height
    act(() => {
      for (const fn of [...this.listeners]) fn()
    })
  }
  get subscribers(): number {
    return this.listeners.size
  }
}

/**
 * Puts a window property back the way jsdom had it — including "jsdom did not have
 * one", which is the case for `visualViewport` and the reason this is not a plain
 * reassignment.
 */
function restoreWindow(key: 'visualViewport' | 'innerHeight', was: PropertyDescriptor | undefined) {
  if (was) Object.defineProperty(window, key, was)
  else Reflect.deleteProperty(window, key)
}

describe('useSoftKeyboard', () => {
  let viewport: FakeViewport
  let textarea: HTMLTextAreaElement
  // ⚠ BOTH GLOBALS ARE PUT BACK AFTERWARDS. The last test below deliberately leaves
  // `window.visualViewport` undefined, and `innerHeight` is pinned to a phone's 812
  // rather than jsdom's own 768 — a file that walks away from either hands whatever
  // runs next a window it did not ask for and cannot see it was given.
  const wasViewport = Object.getOwnPropertyDescriptor(window, 'visualViewport')
  const wasInnerHeight = Object.getOwnPropertyDescriptor(window, 'innerHeight')

  beforeEach(() => {
    viewport = new FakeViewport()
    Object.defineProperty(window, 'visualViewport', { value: viewport, configurable: true })
    Object.defineProperty(window, 'innerHeight', { value: LAYOUT, configurable: true })
    textarea = document.createElement('textarea')
    document.body.append(textarea)
  })

  afterEach(() => {
    textarea.remove()
    restoreWindow('visualViewport', wasViewport)
    restoreWindow('innerHeight', wasInnerHeight)
  })

  it('starts closed and opens when the viewport shrinks under a focused field', () => {
    const { result } = renderHook(() => useSoftKeyboard())
    expect(result.current).toEqual({ open: false, viewport: 0 })

    textarea.focus()
    viewport.resize(512)
    expect(result.current).toEqual({ open: true, viewport: 512 })
  })

  it('closes again when the keyboard goes, even with the field still focused', () => {
    textarea.focus()
    const { result } = renderHook(() => useSoftKeyboard())
    viewport.resize(512)
    expect(result.current.open).toBe(true)

    // Dismissing the keyboard with the phone's own control leaves the focus where
    // it was; the resize is the whole signal.
    viewport.resize(LAYOUT)
    expect(result.current).toEqual({ open: false, viewport: 0 })
  })

  it('follows the keyboard as it grows, so the layout tracks it rather than jumping once', () => {
    textarea.focus()
    const { result } = renderHook(() => useSoftKeyboard())
    viewport.resize(600)
    expect(result.current.viewport).toBe(600)
    viewport.resize(512)
    expect(result.current.viewport).toBe(512)
  })

  it('reads a keyboard that was already up when it mounted', () => {
    textarea.focus()
    viewport.height = 512
    const { result } = renderHook(() => useSoftKeyboard())
    expect(result.current).toEqual({ open: true, viewport: 512 })
  })

  it('re-reads on focusin, so the answer is never stale by one field', () => {
    const { result } = renderHook(() => useSoftKeyboard())
    viewport.height = 512
    act(() => {
      textarea.focus()
      textarea.dispatchEvent(new FocusEvent('focusin', { bubbles: true }))
    })
    expect(result.current).toEqual({ open: true, viewport: 512 })
  })

  // ⚠ THE COMPOSER'S OWN SEND AND ATTACH BUTTONS TAKE THE FOCUS — neither refuses the
  // default on mousedown — so a focusin that answered "closed" would bring the tab bar
  // back and grow the chat box by the keyboard's height while the keyboard is still
  // on screen. The viewport is what reports a keyboard going, and it always does.
  it('lets focus open it and never close it — only the resize closes it', () => {
    textarea.focus()
    const { result } = renderHook(() => useSoftKeyboard())
    viewport.resize(512)
    expect(result.current.open).toBe(true)

    const button = document.createElement('button')
    document.body.append(button)
    act(() => {
      button.focus()
      button.dispatchEvent(new FocusEvent('focusin', { bubbles: true }))
    })
    expect(result.current, 'a button taking focus is not the keyboard leaving').toEqual({
      open: true,
      viewport: 512,
    })

    // The keyboard actually going is a resize, and that does close it.
    viewport.resize(LAYOUT)
    expect(result.current).toEqual({ open: false, viewport: 0 })
    button.remove()
  })

  it('unsubscribes on unmount', () => {
    const { unmount } = renderHook(() => useSoftKeyboard())
    expect(viewport.subscribers).toBe(1)
    unmount()
    expect(viewport.subscribers).toBe(0)
  })

  it('answers closed, and subscribes to nothing, where the API is missing', () => {
    Object.defineProperty(window, 'visualViewport', { value: undefined, configurable: true })
    textarea.focus()
    const { result } = renderHook(() => useSoftKeyboard())
    expect(result.current).toEqual({ open: false, viewport: 0 })
  })
})
