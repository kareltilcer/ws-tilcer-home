import { useEffect, useState } from 'react'

/** useMediaQuery tracks a CSS media query (SSR-safe default false). */
export function useMediaQuery(query: string): boolean {
  const [matches, setMatches] = useState(() =>
    typeof window !== 'undefined' ? window.matchMedia(query).matches : false,
  )
  useEffect(() => {
    const mql = window.matchMedia(query)
    const onChange = () => setMatches(mql.matches)
    onChange()
    mql.addEventListener('change', onChange)
    return () => mql.removeEventListener('change', onChange)
  }, [query])
  return matches
}

/** useIsDesktop is true at the md breakpoint (768px) and up. */
export function useIsDesktop(): boolean {
  return useMediaQuery('(min-width: 768px)')
}
