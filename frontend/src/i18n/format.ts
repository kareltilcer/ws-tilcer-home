// Czech formatting (PRD D20): dates as `d. M. yyyy`, 24h time, space thousands
// separator, comma decimal, Czech collation. All-day event dates are handled as
// 'yyyy-MM-dd' strings and never parsed with `new Date('yyyy-MM-dd')` (which is
// interpreted as UTC midnight and can shift a day in Europe/Prague).

import { czPlural, PLURAL } from './plural'

const czNumber = new Intl.NumberFormat('cs-CZ')
const czCollator = new Intl.Collator('cs')

/** fmtDate renders a Date as `d. M. yyyy` (e.g. 19. 7. 2026). */
export function fmtDate(d: Date): string {
  return `${d.getDate()}. ${d.getMonth() + 1}. ${d.getFullYear()}`
}

/** fmtDateISO parses a plain 'yyyy-MM-dd' date (local, no UTC shift) and formats it. */
export function fmtDateISO(iso: string): string {
  const [y, m, d] = iso.split('-').map(Number)
  return `${d}. ${m}. ${y}`
}

/** todayISO returns a LOCAL calendar date as 'yyyy-MM-dd', optionally shifted by
 *  a whole number of days.
 *
 *  Use this and never `new Date().toISOString().slice(0, 10)`, which yields the
 *  UTC date: for the first one or two hours after local midnight in
 *  Europe/Prague that is the PREVIOUS day. Every backend module derives "today"
 *  in the configured timezone, so a UTC-derived bound sent to it disagrees with
 *  the server's own answer for those hours — and a UTC-derived default stamps
 *  user-entered records with yesterday's date.
 *
 *  The shift goes through setDate, so month ends and DST transitions land on the
 *  calendar day a person would name. */
export function todayISO(offsetDays = 0): string {
  const d = new Date()
  if (offsetDays !== 0) d.setDate(d.getDate() + offsetDays)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`
}

/** fmtTime renders an RFC3339 timestamp as `HH:mm` (24-hour) — fmtDateTime's own
 *  time half, so a screen that wants only the clock (a chat bubble) does not grow
 *  a second spelling of it. */
export function fmtTime(iso: string): string {
  const d = new Date(iso)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${p(d.getHours())}:${p(d.getMinutes())}`
}

/** fmtDateTime renders an RFC3339 timestamp as `d. M. yyyy HH:mm` (24-hour). */
export function fmtDateTime(iso: string): string {
  return `${fmtDate(new Date(iso))} ${fmtTime(iso)}`
}

/** monthLabel renders a 'yyyy-MM' key as a Czech month + year, e.g. "červenec 2026". */
export function monthLabel(ym: string): string {
  const [y, m] = ym.split('-').map(Number)
  const d = new Date(y, m - 1, 1)
  return new Intl.DateTimeFormat('cs-CZ', { month: 'long', year: 'numeric' }).format(d)
}

/** daysUntilLabel renders a relative-day label (dnes / zítra / za N dní / před N dny). */
export function daysUntilLabel(n: number): string {
  if (n === 0) return 'dnes'
  if (n === 1) return 'zítra'
  if (n === -1) return 'včera'
  if (n > 0) return `za ${n} ${czPlural(n, PLURAL.days)}`
  const a = -n
  return `před ${a} ${czPlural(a, ['dnem', 'dny', 'dny'])}`
}

/** fmtNumber renders a number with Czech grouping/decimals. */
export function fmtNumber(n: number): string {
  return czNumber.format(n)
}

/** fmtBytes renders a file size in Czech: `B` / `kB` / `MB` with a decimal comma
 *  (ported from the design prototype's fmtBytes, so the UI matches the bundle). */
export function fmtBytes(n: number): string {
  if (!Number.isFinite(n) || n < 0) return ''
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${Math.round(n / 1024)} kB`
  const mb = n / (1024 * 1024)
  return `${(Math.round(mb * 10) / 10).toString().replace('.', ',')} MB`
}

/** compareCz compares strings using Czech collation (č ř š ž sort after c r s z). */
export function compareCz(a: string, b: string): number {
  return czCollator.compare(a, b)
}

/**
 * fmtStorageBytes renders a storage figure in Czech, up to GB (v9, §V9-7).
 *
 * Distinct from fmtBytes above, deliberately. fmtBytes describes ONE FILE and
 * stops at MB, which is right for a document row — a 2 GB upload cannot exist,
 * the cap is 50 MB. Úložiště describes a WHOLE BUCKET, where GB is the ordinary
 * case and rounding a gigabyte down to "1024,0 MB" reads as a bug.
 *
 * Czech formatting throughout: space thousands separator, decimal comma, unit
 * after a space. ⚠ B / kB / MB / GB DO NOT INFLECT — only the counted nouns
 * beside them do (objekt/objekty/objektů).
 *
 * ⚠ AN UNUSABLE INPUT RENDERS AS *nezměřeno*, NOT AS AN EMPTY STRING. A blank cell
 * is the one state a reader mistakes for a rendering glitch, on a page that
 * otherwise goes to lengths to make a missing figure recognisable before it is
 * read. A NaN or a negative means the same thing a null means — we do not have
 * this number — so it says so.
 */
export function fmtStorageBytes(n: number | null | undefined): string {
  if (!isMeasuredBytes(n)) return UNMEASURED
  if (n < 1024) return `${fmtNumber(n)} B`
  // ⚠ ROUND FIRST, THEN PICK THE BAND. Testing the raw value and printing the
  // rounded one disagree exactly at the boundary: 1 048 065 B is 1023.5 kB, which
  // is < 1024 and so took the kB branch, and then printed `1 024 kB`. Same one
  // step up — 1 023,96 MB printed `1 024,0 MB`, the reading this function's doc
  // above says is the whole reason it stops at GB rather than MB. Rounding before
  // the comparison means a value that rounds up to a full unit is reported in that
  // unit.
  const round = (v: number, decimals: number): number => {
    const f = 10 ** decimals
    return Math.round(v * f) / f
  }
  const kb = round(n / 1024, 0)
  if (kb < 1024) return `${fmtNumber(kb)} kB`
  const mb = round(n / 1024 ** 2, 1)
  if (mb < 1024) return `${fmtNumber(mb)} MB`
  return `${fmtNumber(round(n / 1024 ** 3, 1))} GB`
}

/**
 * UNMEASURED is what a null byte figure renders as — NEVER `0 B` (v9, D193).
 *
 * ⚠ This is the rule the Úložiště page exists to honour. A page whose whole job
 * is reporting byte figures must not print a zero it did not measure: `0 B` on a
 * table reads as good news, and the truth is that nobody looked. The absence gets
 * its own word AND its own type family (proportional italic where a mono figure
 * would otherwise sit), so it is recognisable before it is read rather than after.
 */
export const UNMEASURED = 'nezměřeno'

/**
 * isMeasuredBytes reports whether a byte figure is one we actually have.
 *
 * ⚠ It is the SINGLE predicate behind both the text and the styling, exported so
 * the storage page's `Bytes` cell does not re-spell it and drift: a figure that
 * prints as *nezměřeno* must also get the *nezměřeno* type family, or the absence
 * is legible in one of the two channels the design uses to carry it.
 */
export function isMeasuredBytes(n: number | null | undefined): n is number {
  return n !== null && n !== undefined && Number.isFinite(n) && n >= 0
}

/**
 * fmtMeasuredBytes is the one place a nullable byte figure becomes text.
 *
 * Returns the formatted size, or UNMEASURED. Callers style the two differently —
 * see the `unmeasured` treatment in the storage page — which is why this returns
 * the string rather than a node: the decision about WHICH is here, the decision
 * about how it looks belongs to the screen.
 */
export function fmtMeasuredBytes(n: number | null | undefined): string {
  return fmtStorageBytes(n)
}
