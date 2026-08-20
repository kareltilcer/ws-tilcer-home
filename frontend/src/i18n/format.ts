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

/** fmtDateTime renders an RFC3339 timestamp as `d. M. yyyy HH:mm` (24-hour). */
export function fmtDateTime(iso: string): string {
  const d = new Date(iso)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getDate()}. ${d.getMonth() + 1}. ${d.getFullYear()} ${p(d.getHours())}:${p(d.getMinutes())}`
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
