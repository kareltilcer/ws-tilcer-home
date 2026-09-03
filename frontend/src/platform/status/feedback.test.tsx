import { renderHook, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

// The widget is injected ONCE per page load and the promise is cached at module
// scope, so — as with the crash reporter — each test takes a fresh copy of the
// module after stubbing the build args.
async function load(env: Record<string, string> = {}) {
  vi.resetModules()
  for (const [key, value] of Object.entries(env)) vi.stubEnv(key, value)
  return import('@/platform/status/feedback')
}

const CONFIGURED = {
  VITE_STATUS_WIDGET_KEY: 'wk_widget_key',
  VITE_STATUS_SITE: 'home',
  VITE_STATUS_RELEASE: 'home@2026.36.1',
}

function injected(): HTMLScriptElement | null {
  return document.querySelector('script[data-site]')
}

beforeEach(() => {
  document.head.innerHTML = ''
})

afterEach(() => {
  vi.unstubAllEnvs()
  delete window.StatusFeedback
})

describe('useFeedbackWidget', () => {
  it('renders no trigger and loads nothing without a widget key', async () => {
    const { useFeedbackWidget } = await load()
    const { result } = renderHook(() => useFeedbackWidget('Kája'))

    await waitFor(() => expect(result.current).toBe(false))
    expect(injected()).toBeNull()
  })

  it('embeds the documented attributes and enables the trigger once loaded', async () => {
    const { useFeedbackWidget } = await load(CONFIGURED)
    const { result } = renderHook(() => useFeedbackWidget('Kája'))

    const script = await waitFor(() => {
      const s = injected()
      expect(s).not.toBeNull()
      return s!
    })
    expect(script.src).toBe('https://status.tilcer.cz/widget/v1.js')
    expect(script.defer).toBe(true)
    expect(script.dataset.site).toBe('home')
    expect(script.dataset.key).toBe('wk_widget_key')
    expect(script.dataset.lang).toBe('cs')
    // ⚠ The widget's own floating launcher is suppressed: it does not account for
    // home's 56 px thumb-tab bar, and the shell supplies its own trigger.
    expect(script.dataset.launcher).toBe('none')
    // A display LABEL, never an identity — status does not verify it.
    expect(script.dataset.reporter).toBe('Kája')
    expect(script.dataset.release).toBe('home@2026.36.1')

    expect(result.current).toBe(false) // nothing rendered until the script loads
    script.dispatchEvent(new Event('load'))
    await waitFor(() => expect(result.current).toBe(true))
  })

  // A blocked script, an ad blocker, an offline load: the shell must show no
  // trigger rather than one that does nothing when pressed.
  it('leaves the trigger hidden when the script fails to load', async () => {
    const { useFeedbackWidget } = await load(CONFIGURED)
    const { result } = renderHook(() => useFeedbackWidget('Kája'))

    const script = await waitFor(() => {
      const s = injected()
      expect(s).not.toBeNull()
      return s!
    })
    script.dispatchEvent(new Event('error'))
    await waitFor(() => expect(result.current).toBe(false))
  })

  // StrictMode mounts every effect twice, and the shell itself can remount. One
  // script element, or the page ends up with several widgets.
  it('injects one script however many times it is mounted', async () => {
    const { useFeedbackWidget } = await load(CONFIGURED)
    renderHook(() => useFeedbackWidget('Kája'))
    renderHook(() => useFeedbackWidget('Kája'))
    renderHook(() => useFeedbackWidget('Někdo jiný'))

    await waitFor(() => expect(injected()).not.toBeNull())
    expect(document.querySelectorAll('script[data-site]')).toHaveLength(1)
  })

  it('honours an overridden bundle URL so a staging copy talks to staging', async () => {
    const { useFeedbackWidget } = await load({
      ...CONFIGURED,
      VITE_STATUS_WIDGET_URL: 'https://status.staging.example/widget/v1.js',
    })
    renderHook(() => useFeedbackWidget(''))

    const script = await waitFor(() => {
      const s = injected()
      expect(s).not.toBeNull()
      return s!
    })
    expect(script.src).toBe('https://status.staging.example/widget/v1.js')
    // An empty reporter label is left off rather than sent as "".
    expect(script.dataset.reporter).toBeUndefined()
  })
})

describe('openFeedback', () => {
  it('opens the widget dialog', async () => {
    const { openFeedback } = await load(CONFIGURED)
    const open = vi.fn()
    window.StatusFeedback = { open }
    openFeedback()
    expect(open).toHaveBeenCalledTimes(1)
  })

  // The widget never throws into the host app, and neither does the door into
  // it: the global is simply absent when the script was blocked.
  it('is a silent no-op when the widget is not there', async () => {
    const { openFeedback } = await load(CONFIGURED)
    expect(() => openFeedback()).not.toThrow()
  })

  it('swallows a throw from the widget itself', async () => {
    const { openFeedback } = await load(CONFIGURED)
    window.StatusFeedback = {
      open: () => {
        throw new Error('trusted types blocked the dialog')
      },
    }
    expect(() => openFeedback()).not.toThrow()
  })
})
