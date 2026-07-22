import { useEffect, useRef, useState } from 'react'
import { Check } from 'lucide-react'
import { cn } from '@/lib/utils'
import { useMediaQuery } from '@/hooks/useMediaQuery'

// HoldToComplete is the Nástěnka done control (PRD D22 / FR-N3).
//
// - Pointer: hold for holdMs (2000) to commit, with a fill over ~fillMs (1900)
//   so progress is visible; releasing early cancels with no effect.
// - A short tap does NOTHING and must NOT fall through to opening the row —
//   pointerdown AND click stop propagation.
// - MANDATORY accessible path: keyboard/AT activation (Enter/Space) commits
//   IMMEDIATELY, with no hold. Screen readers hear a plain action button, not a
//   "hold to complete" gesture.
// - prefers-reduced-motion: the fill does not animate; the hold still applies.
export function HoldToComplete({
  onComplete,
  label,
  holdMs = 2000,
  fillMs = 1900,
  className,
}: {
  onComplete: () => void
  label: string
  holdMs?: number
  fillMs?: number
  className?: string
}) {
  const [holding, setHolding] = useState(false)
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  const reducedMotion = useMediaQuery('(prefers-reduced-motion: reduce)')

  useEffect(() => () => clearTimer(), [])

  function clearTimer() {
    if (timer.current) {
      clearTimeout(timer.current)
      timer.current = undefined
    }
  }

  function startHold(e: React.PointerEvent) {
    // Do not let the press reach the row (which opens the detail dialog).
    e.stopPropagation()
    if (e.button !== undefined && e.button !== 0) return
    setHolding(true)
    clearTimer()
    timer.current = setTimeout(() => {
      setHolding(false)
      onComplete()
    }, holdMs)
  }

  function cancelHold() {
    setHolding(false)
    clearTimer()
  }

  function onKeyDown(e: React.KeyboardEvent) {
    if (e.key === 'Enter' || e.key === ' ') {
      // Immediate commit — no hold required (the accessible path).
      e.preventDefault()
      e.stopPropagation()
      onComplete()
    }
  }

  const fillScale = holding ? 1 : 0
  const fillTransition = holding && !reducedMotion ? `${fillMs}ms` : '120ms'

  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      onPointerDown={startHold}
      onPointerUp={cancelHold}
      onPointerLeave={cancelHold}
      onPointerCancel={cancelHold}
      onKeyDown={onKeyDown}
      onClick={(e) => {
        // A short tap/click is a no-op and must not bubble to the row.
        e.preventDefault()
        e.stopPropagation()
      }}
      onContextMenu={(e) => e.preventDefault()}
      className={cn(
        'relative grid h-11 min-w-11 select-none place-items-center overflow-hidden rounded-md border border-border bg-s2 px-3 text-good',
        'touch-none focus-visible:outline-2 focus-visible:outline-focus',
        className,
      )}
    >
      {/* Fill overlay grows left→right while holding. */}
      <span
        aria-hidden
        className="absolute inset-y-0 left-0 w-full origin-left bg-good/25"
        style={{ transform: `scaleX(${fillScale})`, transition: `transform ${fillTransition} linear` }}
      />
      <Check size={18} aria-hidden className="relative" />
    </button>
  )
}
