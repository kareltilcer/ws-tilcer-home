import { act, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { GESTURE, useMessageGestures } from './gestures'

// The three bubble gestures (v10.1, D268), and the property that matters more than
// any of them: NONE OF THEM FIRES WHILE SOMEBODY IS SCROLLING.
//
// ⚠ A THREAD IS A VERTICAL SCROLLER AND A BUBBLE FILLS MOST OF ITS WIDTH, so every
// scroll in the module starts as a press on one of these. A gesture layer that is
// merely "usually right" here is a layer that hearts a message every few flicks,
// with no undo the member will find.

type Point = { x: number; y: number }

/** A pointer event as the handlers read it — nothing else on it is touched. */
function touch({ x, y }: Point) {
  return { pointerType: 'touch', clientX: x, clientY: y } as unknown as React.PointerEvent
}

function mouse({ x, y }: Point) {
  return { pointerType: 'mouse', clientX: x, clientY: y } as unknown as React.PointerEvent
}

function setup() {
  const actions = {
    onDoubleTap: vi.fn(),
    onSwipeReply: vi.fn(),
    onLongPress: vi.fn(),
  }
  const view = renderHook(() => useMessageGestures(actions))
  return { actions, view }
}

/** One complete press: down, an optional path of moves, then up. */
function gesture(
  view: ReturnType<typeof setup>['view'],
  from: Point,
  path: Point[],
  end?: Point,
) {
  act(() => view.result.current.handlers.onPointerDown(touch(from)))
  for (const p of path) {
    act(() => view.result.current.handlers.onPointerMove(touch(p)))
  }
  act(() => view.result.current.handlers.onPointerUp(touch(end ?? path.at(-1) ?? from)))
}

beforeEach(() => vi.useFakeTimers())
afterEach(() => vi.useRealTimers())

describe('double tap', () => {
  it('fires on two taps inside the window and not on one', () => {
    const { actions, view } = setup()

    gesture(view, { x: 100, y: 100 }, [])
    expect(actions.onDoubleTap).not.toHaveBeenCalled()

    act(() => vi.advanceTimersByTime(GESTURE.DOUBLE_TAP_MS - 50))
    gesture(view, { x: 100, y: 100 }, [])
    expect(actions.onDoubleTap).toHaveBeenCalledTimes(1)
  })

  it('does not fire when the two taps are too far apart in time', () => {
    const { actions, view } = setup()
    gesture(view, { x: 100, y: 100 }, [])
    act(() => vi.advanceTimersByTime(GESTURE.DOUBLE_TAP_MS + 50))
    gesture(view, { x: 100, y: 100 }, [])
    expect(actions.onDoubleTap).not.toHaveBeenCalled()
  })

  // ⚠ THE ONE THAT COSTS. A flick that scrolls the thread is a press, a travel and a
  // release — indistinguishable from a tap unless the travel is checked. Two flicks
  // in a row would otherwise put a heart on whichever message happened to be under
  // the thumb, in a module with no undo for it beyond finding the chip again.
  it('does not count a scroll flick as a tap', () => {
    const { actions, view } = setup()
    const drag: Point[] = [
      { x: 100, y: 90 },
      { x: 100, y: 40 },
    ]
    gesture(view, { x: 100, y: 100 }, drag)
    gesture(view, { x: 100, y: 100 }, drag)
    expect(actions.onDoubleTap).not.toHaveBeenCalled()
  })

  it('ignores a mouse entirely, so double-click still selects a word', () => {
    const { actions, view } = setup()
    act(() => view.result.current.handlers.onPointerDown(mouse({ x: 10, y: 10 })))
    act(() => view.result.current.handlers.onPointerUp(mouse({ x: 10, y: 10 })))
    act(() => view.result.current.handlers.onPointerDown(mouse({ x: 10, y: 10 })))
    act(() => view.result.current.handlers.onPointerUp(mouse({ x: 10, y: 10 })))
    expect(actions.onDoubleTap).not.toHaveBeenCalled()
  })
})

describe('long press', () => {
  it('fires after the hold and only once', () => {
    const { actions, view } = setup()
    act(() => view.result.current.handlers.onPointerDown(touch({ x: 50, y: 50 })))
    act(() => vi.advanceTimersByTime(GESTURE.LONG_PRESS_MS + 10))
    expect(actions.onLongPress).toHaveBeenCalledTimes(1)

    act(() => view.result.current.handlers.onPointerUp(touch({ x: 50, y: 50 })))
    expect(actions.onLongPress).toHaveBeenCalledTimes(1)
  })

  // ⚠ WITHOUT THIS, TWO HOLDS IN A ROW READ AS A DOUBLE TAP. The release after a
  // long press would have counted as a tap, so opening the reaction bar twice —
  // which is exactly what somebody does when they change their mind about which
  // emoji — put a heart on the message as well.
  it('does not let its own release count as a tap', () => {
    const { actions, view } = setup()
    for (let i = 0; i < 2; i++) {
      act(() => view.result.current.handlers.onPointerDown(touch({ x: 50, y: 50 })))
      act(() => vi.advanceTimersByTime(GESTURE.LONG_PRESS_MS + 10))
      act(() => view.result.current.handlers.onPointerUp(touch({ x: 50, y: 50 })))
    }
    expect(actions.onLongPress).toHaveBeenCalledTimes(2)
    expect(actions.onDoubleTap).not.toHaveBeenCalled()
  })

  it('is cancelled by movement in any direction, not only vertical', () => {
    for (const drift of [
      { x: 50 + GESTURE.SLOP + 5, y: 50 },
      { x: 50, y: 50 + GESTURE.SLOP + 5 },
      { x: 50 - GESTURE.SLOP - 5, y: 50 },
    ]) {
      const { actions, view } = setup()
      act(() => view.result.current.handlers.onPointerDown(touch({ x: 50, y: 50 })))
      act(() => view.result.current.handlers.onPointerMove(touch(drift)))
      act(() => vi.advanceTimersByTime(GESTURE.LONG_PRESS_MS + 10))
      expect(actions.onLongPress).not.toHaveBeenCalled()
    }
  })

  // A cancel is the scroller claiming the pointer, which is the single most common
  // way a press on a bubble ends.
  it('is cancelled when the browser takes the gesture', () => {
    const { actions, view } = setup()
    act(() => view.result.current.handlers.onPointerDown(touch({ x: 50, y: 50 })))
    act(() => view.result.current.handlers.onPointerCancel(touch({ x: 50, y: 50 })))
    act(() => vi.advanceTimersByTime(GESTURE.LONG_PRESS_MS + 10))
    expect(actions.onLongPress).not.toHaveBeenCalled()
  })
})

describe('swipe to reply', () => {
  it('fires once the finger has travelled far enough to the right', () => {
    const { actions, view } = setup()
    gesture(view, { x: 40, y: 100 }, [
      { x: 40 + GESTURE.SLOP + 5, y: 100 },
      { x: 40 + GESTURE.SWIPE_COMMIT + 5, y: 102 },
    ])
    expect(actions.onSwipeReply).toHaveBeenCalledTimes(1)
  })

  it('does not fire when it stops short, and lets the bubble settle back', () => {
    const { actions, view } = setup()
    gesture(view, { x: 40, y: 100 }, [{ x: 40 + GESTURE.SWIPE_COMMIT - 20, y: 100 }])
    expect(actions.onSwipeReply).not.toHaveBeenCalled()
    expect(view.result.current.swipeX).toBe(0)
  })

  // ⚠ LEFT IS NOT A VERB. There is no left-swipe action, and treating one as a reply
  // would fire on a mis-swipe the member could not have seen coming.
  it('ignores a leftward swipe', () => {
    const { actions, view } = setup()
    gesture(view, { x: 200, y: 100 }, [{ x: 200 - GESTURE.SWIPE_COMMIT - 20, y: 100 }])
    expect(actions.onSwipeReply).not.toHaveBeenCalled()
  })

  // ⚠ THE DIAGONAL IS RE-CHECKED ON EVERY MOVE, not judged once at the end. A finger
  // that starts sideways and curves into a scroll has to give the scroll back — and
  // the bubble has to stop following it, or the thread scrolls with one message
  // hanging out of line.
  it('gives up when a sideways start turns into a scroll', () => {
    const { actions, view } = setup()
    act(() => view.result.current.handlers.onPointerDown(touch({ x: 40, y: 300 })))
    act(() => view.result.current.handlers.onPointerMove(touch({ x: 40 + 30, y: 305 })))
    expect(view.result.current.swipeX).toBeGreaterThan(0)

    act(() => view.result.current.handlers.onPointerMove(touch({ x: 40 + 35, y: 180 })))
    expect(view.result.current.swipeX).toBe(0)

    act(() => view.result.current.handlers.onPointerUp(touch({ x: 40 + 35, y: 120 })))
    expect(actions.onSwipeReply).not.toHaveBeenCalled()
  })

  it('stops following the finger past the rubber band', () => {
    const { view } = setup()
    act(() => view.result.current.handlers.onPointerDown(touch({ x: 0, y: 100 })))
    act(() => view.result.current.handlers.onPointerMove(touch({ x: 400, y: 100 })))
    expect(view.result.current.swipeX).toBe(GESTURE.SWIPE_MAX)
    expect(view.result.current.swipeArmed).toBe(true)
  })

  // ⚠ A COMMITTED SWIPE IS NOT ALSO A TAP. Without clearing the marker, replying by
  // swipe and then tapping the message once put a heart on the message that was
  // just replied to.
  it('does not pair with a following tap into a double tap', () => {
    const { actions, view } = setup()
    gesture(view, { x: 40, y: 100 }, [{ x: 40 + GESTURE.SWIPE_COMMIT + 10, y: 100 }])
    gesture(view, { x: 100, y: 100 }, [])
    expect(actions.onSwipeReply).toHaveBeenCalledTimes(1)
    expect(actions.onDoubleTap).not.toHaveBeenCalled()
  })
})

// ⚠ THE BUBBLE CONTAINS BUTTONS AND POINTER EVENTS BUBBLE. The chips, the ☺, the
// seven emoji in the picker and *Odpovědět* all sit inside the element carrying these
// handlers, so without a guard every tap on one of them was also fed to this state
// machine — and any two inside the double-tap window put a ❤️ on the message.
describe('presses that begin on a control', () => {
  /** A touch whose target is a real button, the way the DOM delivers one. */
  function onButton({ x, y }: Point) {
    const button = document.createElement('button')
    return {
      pointerType: 'touch',
      clientX: x,
      clientY: y,
      target: button,
    } as unknown as React.PointerEvent
  }

  it('does not pair two taps on the ☺ into a heart', () => {
    const { actions, view } = setup()
    // Open the reaction bar, then close it again — two taps on the same button.
    act(() => view.result.current.handlers.onPointerDown(onButton({ x: 60, y: 100 })))
    act(() => view.result.current.handlers.onPointerUp(onButton({ x: 60, y: 100 })))
    act(() => view.result.current.handlers.onPointerDown(onButton({ x: 60, y: 100 })))
    act(() => view.result.current.handlers.onPointerUp(onButton({ x: 60, y: 100 })))
    expect(actions.onDoubleTap).not.toHaveBeenCalled()
  })

  it('does not open the reaction bar when a chip is held', () => {
    const { actions, view } = setup()
    act(() => view.result.current.handlers.onPointerDown(onButton({ x: 60, y: 100 })))
    act(() => vi.advanceTimersByTime(GESTURE.LONG_PRESS_MS + 50))
    expect(actions.onLongPress).not.toHaveBeenCalled()
  })

  it('does not reply when a drag starts on a button', () => {
    const { actions, view } = setup()
    act(() => view.result.current.handlers.onPointerDown(onButton({ x: 40, y: 100 })))
    act(() =>
      view.result.current.handlers.onPointerMove(touch({ x: 40 + GESTURE.SWIPE_COMMIT + 20, y: 100 })),
    )
    act(() =>
      view.result.current.handlers.onPointerUp(touch({ x: 40 + GESTURE.SWIPE_COMMIT + 20, y: 100 })),
    )
    expect(actions.onSwipeReply).not.toHaveBeenCalled()
    expect(view.result.current.swipeX).toBe(0)
  })

  // ⚠ AND A CONTROL'S RELEASE LEAVES NO MARKER BEHIND, or the next real tap on the
  // bubble pairs with it and hearts the message anyway.
  it('leaves no tap marker for a following real tap to pair with', () => {
    const { actions, view } = setup()
    act(() => view.result.current.handlers.onPointerDown(onButton({ x: 60, y: 100 })))
    act(() => view.result.current.handlers.onPointerUp(onButton({ x: 60, y: 100 })))
    gesture(view, { x: 100, y: 100 }, [])
    expect(actions.onDoubleTap).not.toHaveBeenCalled()
  })
})

// ⚠ A PRESS THAT IS STILL HELD WHEN THE THREAD GOES left a 500 ms timeout pointing at
// a component that no longer exists — the room trashed by somebody else, a gap check
// re-rendering the page, the member tapping back.
describe('unmounting mid-press', () => {
  it('does not fire the long press after the bubble is gone', () => {
    const { actions, view } = setup()
    act(() => view.result.current.handlers.onPointerDown(touch({ x: 60, y: 100 })))
    view.unmount()
    act(() => vi.advanceTimersByTime(GESTURE.LONG_PRESS_MS + 50))
    expect(actions.onLongPress).not.toHaveBeenCalled()
  })
})
