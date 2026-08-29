// Client-side lexorank, mirroring backend/internal/lexorank. Used to compute a
// drag-drop target position key strictly between two neighbours so a move
// rewrites exactly one row (the server accepts the client-computed position).

const ALPHABET = '0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz'
const BASE = ALPHABET.length

const idx = (c: string) => ALPHABET.indexOf(c)
const chr = (i: number) => ALPHABET[i]

/** between returns a key k with a < k < b. "" means −∞ (a) / +∞ (b). */
export function between(a: string, b: string): string {
  if (b !== '' && a !== '' && a >= b) return tail(a)
  let out = ''
  let i = 0
  // Local copy so we can drop the upper bound once we've descended below it.
  let upper = b
  for (;;) {
    const da = i < a.length ? idx(a[i]) : 0
    const db = upper !== '' && i < upper.length ? idx(upper[i]) : BASE
    if (da + 1 < db) {
      out += chr(Math.floor((da + db) / 2))
      return out
    }
    out += chr(da)
    if (db === da + 1) upper = ''
    i++
  }
}

export const first = () => between('', '')
export const head = (next: string) => between('', next)
// Not exported: tailOf below absorbed the last two callers outside this file, and
// "the key after this one" is a question callers should be asking about a list of
// siblings rather than about a single neighbour they picked themselves.
const tail = (prev: string) => between(prev, '')

/** comparePositions is a sort comparator over lexorank keys (ascending). */
export const comparePositions = (a: string, b: string): number => (a < b ? -1 : a > b ? 1 : 0)

/** canInsertBetween reports whether a key k with a < k < b can exist ('' bounds
 *  mean ∓∞). It is false only when the neighbours are equal or inverted — no single
 *  key separates them, so a caller must renumber rather than write one ambiguous
 *  row (between() would otherwise fall back to tail(a), placing the item after BOTH). */
export const canInsertBetween = (a: string, b: string): boolean => a === '' || b === '' || a < b

/** tailOf is the key that sorts after every one of `positions` — where a move or a
 *  create lands when the caller means "at the end of these siblings". An empty list
 *  is the empty parent, so it takes the first key.
 *
 *  It reduces rather than sorts: the list arrives in tree order, not rank order,
 *  and only the maximum matters. */
export function tailOf(positions: string[]): string {
  if (positions.length === 0) return tail('')
  const max = positions.reduce((a, b) => (a > b ? a : b))
  return tail(max)
}
