import { describe, expect, it } from 'vitest'
import { render, screen, act } from '@testing-library/react'
import { OnlineProvider, offlineWriteProps, useOnline } from './offline'

function Probe() {
  const online = useOnline()
  const props = offlineWriteProps(online)
  return (
    <button type="button" disabled={props.disabled} title={props.title}>
      Uložit
    </button>
  )
}

/** setConnection flips navigator.onLine and fires the matching event, the way a
 *  real disconnect does. */
function setConnection(online: boolean) {
  Object.defineProperty(navigator, 'onLine', { value: online, configurable: true })
  act(() => {
    window.dispatchEvent(new Event(online ? 'online' : 'offline'))
  })
}

describe('offline', () => {
  it('disables write controls with ONE standard message when offline', () => {
    setConnection(true)
    render(
      <OnlineProvider>
        <Probe />
      </OnlineProvider>,
    )

    const button = screen.getByRole('button', { name: 'Uložit' })
    expect(button).toBeEnabled()

    setConnection(false)

    // Disabled, NOT hidden (D71): the user should still see what they could do.
    expect(button).toBeDisabled()
    expect(button).toHaveAttribute('title', 'Změny nelze uložit offline')

    setConnection(true)
    expect(button).toBeEnabled()
  })

  it('never invents a second offline wording', () => {
    expect(offlineWriteProps(false).title).toBe('Změny nelze uložit offline')
    expect(offlineWriteProps(true).title).toBeUndefined()
  })
})
