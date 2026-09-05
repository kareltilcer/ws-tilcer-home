import { clsx, type ClassValue } from 'clsx'
import { twMerge } from 'tailwind-merge'

/** cn merges conditional class names, de-duplicating Tailwind conflicts. */
export function cn(...inputs: ClassValue[]): string {
  return twMerge(clsx(inputs))
}

/**
 * A person's mark: the first character of their name, upper-cased.
 *
 * Not a photo — home has no user table and no avatars (D230) — and it is drawn in
 * two places now, the chat members panel and the side nav's user row, which is why
 * it lives here rather than in either of them.
 *
 * ⚠ IT SPREADS RATHER THAN INDEXES. `name[0]` cuts a surrogate pair in half and
 * renders U+FFFD, which is the one way a single character can be wrong.
 */
export function initial(name: string): string {
  return [...name.trim()].slice(0, 1).join('').toUpperCase()
}
