import { useCallback, useEffect, useRef, useState } from 'react'

/**
 * The three touch gestures on a bubble (v10.1, D268).
 *
 *   double tap    ❤️ on, or off if it is already yours
 *   swipe right   reply to this message
 *   long press    open the reaction bar
 *
 * ⚠ EVERY ONE OF THEM HAS A VISIBLE CONTROL DOING THE SAME THING, and that is not
 * politeness — it is the difference between an accelerator and a feature. The
 * bubble's footer already carries *Odpovědět*, the chips are buttons, and the ☺
 * opens the same bar the long press does. A gesture nobody can discover, on a
 * household app used by a grandmother on a phone and by somebody on a desktop with
 * no touchscreen at all, cannot be the only way to reach anything.
 *
 * ⚠ AND THEY ARE TOUCH-ONLY, DELIBERATELY. `pointerType !== 'touch'` returns
 * immediately, so a mouse keeps its double-click-to-select-a-word and a drag keeps
 * selecting text. A "long press" bound to a held mouse button is a context menu
 * fighting the browser's own; there is a button for that, one row down.
 *
 * ⚠ THE HARD PART IS NOT FIRING WHEN THE MEMBER IS SCROLLING. A thread is a
 * vertical scroller and a bubble fills most of its width, so every scroll starts as
 * a press on one of these. The rules, in order:
 *
 *   - the long-press timer is cancelled by ANY movement past SLOP, not only by
 *     vertical movement — a slow drag that never leaves the bubble is still not a
 *     press-and-hold;
 *   - a swipe is only a swipe while |dx| > |dy|, checked continuously rather than
 *     once at the end, so a diagonal that turns into a scroll stops being a swipe
 *     the moment it does;
 *   - and a tap is only a tap if the finger never travelled past SLOP at all.
 *
 * The scroll itself is never prevented: `touch-action: pan-y` on the element lets
 * the browser keep vertical panning on its own thread, and nothing here calls
 * preventDefault. A gesture that can wedge scrolling is worse than no gesture.
 */

/** How far a finger may drift and still be holding still, in CSS pixels. */
const SLOP = 10
/** How long a press must last to be a long press. */
const LONG_PRESS_MS = 500
/** How close two taps must be, in milliseconds, to be one double tap. */
const DOUBLE_TAP_MS = 300
/** How far right the finger must travel before the swipe commits to a reply. */
const SWIPE_COMMIT = 64
/**
 * How far the bubble is allowed to follow the finger. Past this it stops moving
 * while the finger goes on — the rubber band that says the gesture is already
 * armed and further travel changes nothing.
 */
const SWIPE_MAX = 88
/**
 * The controls a bubble carries, which a gesture must never fire from.
 *
 * ⚠ THE HANDLERS SIT ON THE BUBBLE AND THE BUBBLE CONTAINS BUTTONS (v10.1 review).
 * Pointer events bubble, so every tap on a chip, on the ☺, on an emoji in the picker
 * and on *Odpovědět* was also fed to this state machine — and any two of them inside
 * 300 ms paired into a double tap. Opening the reaction bar and closing it again put
 * a ❤️ on the message; two taps on a 👍 chip did the same. A press that starts on a
 * control belongs to that control, so the test is made once, at pointer down: a
 * finger that starts on a button and slides onto the bubble is still that button's
 * press and not a swipe.
 */
const CONTROLS = 'button, a, input, textarea, select, [role="button"]'

/** Whether this pointer sequence began on something that already does its own job. */
function onAControl(e: React.PointerEvent): boolean {
  return e.target instanceof Element && e.target.closest(CONTROLS) !== null
}

export interface MessageGestureHandlers {
  onPointerDown: (e: React.PointerEvent) => void
  onPointerMove: (e: React.PointerEvent) => void
  onPointerUp: (e: React.PointerEvent) => void
  onPointerCancel: (e: React.PointerEvent) => void
}

export interface MessageGestures {
  handlers: MessageGestureHandlers
  /**
   * How far the bubble should currently be translated, in pixels. 0 unless a swipe
   * is in progress — the caller styles the transform, because only it knows which
   * element is the bubble.
   */
  swipeX: number
  /** True once the swipe has travelled far enough to fire on release. */
  swipeArmed: boolean
}

export interface MessageGestureActions {
  /** Double tap. Called with nothing — the caller knows which emoji is the heart. */
  onDoubleTap: () => void
  /** Swipe right, past the commit distance. */
  onSwipeReply: () => void
  /** Long press. */
  onLongPress: () => void
}

/**
 * useMessageGestures wires one bubble's touch gestures.
 *
 * The mutable half is a ref rather than state: a pointer sequence produces dozens of
 * move events, and re-rendering the thread on each one to store a coordinate would
 * make the gesture the most expensive thing in the module. Only `swipeX` — which the
 * bubble actually draws — is state, and it is set only while a horizontal swipe is
 * genuinely in progress.
 */
export function useMessageGestures(actions: MessageGestureActions): MessageGestures {
  // ⚠ THE ACTIONS GO THROUGH A REF. They are closures over the message and over
  // component state, so they are new on every render; binding them into the
  // callbacks directly would rebuild all four handlers every render, on every
  // bubble in a two-hundred-message thread.
  const latest = useRef(actions)
  latest.current = actions

  const [swipeX, setSwipeX] = useState(0)
  const swipe = useRef({ x: 0, y: 0, active: false, panning: false })
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const lastTap = useRef(0)
  const longFired = useRef(false)

  const cancelTimer = useCallback(() => {
    if (timer.current !== null) {
      clearTimeout(timer.current)
      timer.current = null
    }
  }, [])

  const reset = useCallback(() => {
    cancelTimer()
    swipe.current = { x: 0, y: 0, active: false, panning: false }
    setSwipeX(0)
  }, [cancelTimer])

  // ⚠ THE TIMER OUTLIVES THE BUBBLE OTHERWISE. A press that is still being held when
  // the thread goes — the room trashed by somebody else, a gap check re-rendering the
  // page, the member tapping back — left a 500 ms timeout pointing at a component
  // that no longer exists, which then opened a reaction bar on it.
  useEffect(() => cancelTimer, [cancelTimer])

  const onPointerDown = useCallback(
    (e: React.PointerEvent) => {
      if (e.pointerType !== 'touch' || onAControl(e)) return
      longFired.current = false
      swipe.current = { x: e.clientX, y: e.clientY, active: true, panning: false }
      setSwipeX(0)
      cancelTimer()
      timer.current = setTimeout(() => {
        timer.current = null
        // ⚠ THE FLAG IS WHAT STOPS THE RELEASE FIRING A SECOND GESTURE. Without it
        // a press-and-hold followed by lifting the finger opened the bar and then
        // counted as a tap — so two holds in a row read as a double tap and put a
        // heart on a message nobody had tapped twice.
        longFired.current = true
        setSwipeX(0)
        latest.current.onLongPress()
      }, LONG_PRESS_MS)
    },
    [cancelTimer],
  )

  const onPointerMove = useCallback(
    (e: React.PointerEvent) => {
      if (e.pointerType !== 'touch' || !swipe.current.active) return
      const dx = e.clientX - swipe.current.x
      const dy = e.clientY - swipe.current.y
      if (Math.abs(dx) > SLOP || Math.abs(dy) > SLOP) {
        // Any real movement ends the press-and-hold, whichever way it went.
        cancelTimer()
      }
      if (longFired.current) return

      // ⚠ RIGHT ONLY, AND ONLY WHILE IT IS MORE HORIZONTAL THAN VERTICAL. Both
      // halves are re-checked on every move: a finger that starts sideways and
      // curves into a scroll must give the scroll back, and a leftward drag is not
      // this gesture at all (there is no left-swipe verb, and treating one as a
      // reply would fire on a mis-swipe with no way to see it coming).
      const horizontal = dx > SLOP && Math.abs(dx) > Math.abs(dy)
      if (!horizontal) {
        if (swipe.current.panning) {
          swipe.current.panning = false
          setSwipeX(0)
        }
        return
      }
      swipe.current.panning = true
      // Past SWIPE_MAX the bubble stops following: the gesture is armed and the
      // extra travel is the member's finger, not the app's opinion.
      setSwipeX(Math.min(dx, SWIPE_MAX))
    },
    [cancelTimer],
  )

  const onPointerUp = useCallback(
    (e: React.PointerEvent) => {
      if (e.pointerType !== 'touch') return
      cancelTimer()
      // The release of a press that began on a control is that control's, and it must
      // not leave a tap marker behind for the next real tap to pair with.
      if (!swipe.current.active) return
      const dx = e.clientX - swipe.current.x
      const dy = e.clientY - swipe.current.y
      const wasPanning = swipe.current.panning
      const fired = longFired.current
      reset()
      if (fired) return

      if (wasPanning && dx >= SWIPE_COMMIT) {
        latest.current.onSwipeReply()
        // ⚠ A COMMITTED SWIPE IS NOT ALSO A TAP. The marker is cleared so the next
        // real tap cannot pair with this release into a double tap and add a heart
        // to the message the member just replied to.
        lastTap.current = 0
        return
      }
      // Anything that moved is not a tap, and that includes a swipe that did not
      // travel far enough — those release back to nothing on purpose.
      if (Math.abs(dx) > SLOP || Math.abs(dy) > SLOP) {
        lastTap.current = 0
        return
      }

      const now = Date.now()
      if (now - lastTap.current < DOUBLE_TAP_MS) {
        lastTap.current = 0
        latest.current.onDoubleTap()
        return
      }
      lastTap.current = now
    },
    [cancelTimer, reset],
  )

  const onPointerCancel = useCallback(
    (e: React.PointerEvent) => {
      if (e.pointerType !== 'touch') return
      // ⚠ A CANCEL IS THE BROWSER TAKING THE GESTURE, which is exactly what happens
      // when the scroller claims the pointer — the single most common end to a
      // press on a bubble. Nothing fires; the marker is cleared so the interrupted
      // press cannot pair with the next tap.
      reset()
      lastTap.current = 0
      longFired.current = false
    },
    [reset],
  )

  return {
    handlers: { onPointerDown, onPointerMove, onPointerUp, onPointerCancel },
    swipeX,
    swipeArmed: swipeX >= SWIPE_COMMIT,
  }
}

/** Exported for the tests, which assert the thresholds rather than restating them. */
export const GESTURE = {
  SLOP,
  LONG_PRESS_MS,
  DOUBLE_TAP_MS,
  SWIPE_COMMIT,
  SWIPE_MAX,
} as const
