import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { HoldToComplete } from './HoldToComplete'

describe('HoldToComplete (D22 gesture)', () => {
  beforeEach(() => vi.useFakeTimers())
  afterEach(() => {
    vi.useRealTimers()
    cleanup()
  })

  it('does NOT complete on a short (500 ms) press', () => {
    const onComplete = vi.fn()
    render(<HoldToComplete label="Dokončit" onComplete={onComplete} />)
    const btn = screen.getByRole('button', { name: 'Dokončit' })

    fireEvent.pointerDown(btn)
    vi.advanceTimersByTime(500)
    fireEvent.pointerUp(btn)
    vi.advanceTimersByTime(2000)

    expect(onComplete).not.toHaveBeenCalled()
  })

  it('completes after a full 2000 ms hold', () => {
    const onComplete = vi.fn()
    render(<HoldToComplete label="Dokončit" onComplete={onComplete} />)
    const btn = screen.getByRole('button', { name: 'Dokončit' })

    fireEvent.pointerDown(btn)
    expect(onComplete).not.toHaveBeenCalled()
    vi.advanceTimersByTime(1999)
    expect(onComplete).not.toHaveBeenCalled()
    vi.advanceTimersByTime(1)
    expect(onComplete).toHaveBeenCalledTimes(1)
  })

  it('cancels when released early (no side effect)', () => {
    const onComplete = vi.fn()
    render(<HoldToComplete label="Dokončit" onComplete={onComplete} />)
    const btn = screen.getByRole('button', { name: 'Dokončit' })

    fireEvent.pointerDown(btn)
    vi.advanceTimersByTime(1000)
    fireEvent.pointerLeave(btn)
    vi.advanceTimersByTime(5000)

    expect(onComplete).not.toHaveBeenCalled()
  })

  it('keyboard (Enter/Space) commits IMMEDIATELY, without a hold', () => {
    const onComplete = vi.fn()
    render(<HoldToComplete label="Dokončit" onComplete={onComplete} />)
    const btn = screen.getByRole('button', { name: 'Dokončit' })

    fireEvent.keyDown(btn, { key: 'Enter' })
    expect(onComplete).toHaveBeenCalledTimes(1)

    fireEvent.keyDown(btn, { key: ' ' })
    expect(onComplete).toHaveBeenCalledTimes(2)
  })

  it('a short tap (click) neither completes nor bubbles to the row', () => {
    const onComplete = vi.fn()
    const rowClick = vi.fn()
    render(
      <div onClick={rowClick}>
        <HoldToComplete label="Dokončit" onComplete={onComplete} />
      </div>,
    )
    const btn = screen.getByRole('button', { name: 'Dokončit' })
    fireEvent.click(btn)
    expect(onComplete).not.toHaveBeenCalled()
    expect(rowClick).not.toHaveBeenCalled() // stopPropagation prevents fall-through
  })

  it('exposes a plain action button to assistive tech (not "hold to complete")', () => {
    render(<HoldToComplete label="Dokončit úkol" onComplete={() => {}} />)
    expect(screen.getByRole('button', { name: 'Dokončit úkol' })).toBeInTheDocument()
  })
})
