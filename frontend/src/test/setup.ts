import '@testing-library/jest-dom/vitest'

// jsdom has no matchMedia; stub it (used by useMediaQuery / reduced-motion).
if (!window.matchMedia) {
  window.matchMedia = (query: string): MediaQueryList =>
    ({
      matches: false,
      media: query,
      onchange: null,
      addEventListener: () => {},
      removeEventListener: () => {},
      addListener: () => {},
      removeListener: () => {},
      dispatchEvent: () => false,
    }) as unknown as MediaQueryList
}

// jsdom lacks PointerEvent; alias it to MouseEvent so fireEvent.pointer* works.
if (typeof window.PointerEvent === 'undefined') {
  // @ts-expect-error assigning MouseEvent as a PointerEvent stand-in for tests
  window.PointerEvent = window.MouseEvent
}

// jsdom has no ResizeObserver, and ThreadView builds one to re-pin its thread when
// the box shrinks under a keyboard. A no-op is the honest stub rather than a
// shortcut: jsdom has no layout engine, so nothing it observed could ever change
// size and a fuller fake would only promise a callback that cannot arrive.
if (typeof window.ResizeObserver === 'undefined') {
  window.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  } as unknown as typeof ResizeObserver
}
