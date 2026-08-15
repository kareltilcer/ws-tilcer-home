import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { EmojiPickerPanel } from './EmojiPickerPanel'
import { cs } from '@/i18n/cs'

describe('EmojiPickerPanel', () => {
  afterEach(cleanup)

  const searchBox = () => screen.getByPlaceholderText(cs.common.emojiSearchPlaceholder)

  it('filters the grid by search and reports the picked emoji', () => {
    const onSelect = vi.fn()
    render(<EmojiPickerPanel onSelect={onSelect} />)

    // Narrow to the single black-heart glyph, then pick it.
    fireEvent.change(searchBox(), { target: { value: 'cerne srdce' } })
    fireEvent.click(screen.getByText('🖤'))

    expect(onSelect).toHaveBeenCalledWith('🖤')
  })

  it('shows an empty state when nothing matches', () => {
    render(<EmojiPickerPanel onSelect={vi.fn()} />)
    fireEvent.change(searchBox(), { target: { value: 'zzqxnotarealthing' } })
    expect(screen.getByText(cs.common.emojiNoResults)).toBeInTheDocument()
  })

  it('marks the current value as pressed', () => {
    render(<EmojiPickerPanel value="🚗" onSelect={vi.fn()} />)
    fireEvent.change(searchBox(), { target: { value: 'auto' } })
    const car = screen.getByText('🚗').closest('button')
    expect(car).toHaveAttribute('aria-pressed', 'true')
  })
})
