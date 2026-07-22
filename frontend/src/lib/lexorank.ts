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
export const tail = (prev: string) => between(prev, '')
