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
