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

/** compareCz compares strings using Czech collation (č ř š ž sort after c r s z). */
export function compareCz(a: string, b: string): number {
  return czCollator.compare(a, b)
}
