// Elektřina — number formatting.
//
// THIS MODULE IS THE FIRST IN HOME WITH TWO MONEY PRECISIONS, and the rule is
// stated HERE, once, per figure type — not left to each screen to decide:
//
//   TOTALS are whole koruny.        21 560 Kč      (kc)
//   UNIT PRICES and FEES are not.   4 858,65 Kč/MWh, 642,35 Kč/měs   (kc2)
//
// The reason is not aesthetic. A total is a sum of many roundings and its last
// haléř means nothing; a unit price is a number the supplier printed on a
// contract and its haléře are the number. Showing 4 859 Kč/MWh would make the
// ceník screen disagree with the paper it was typed from.
//
// Czech formatting throughout: a NON-BREAKING space groups thousands and a comma
// is the decimal mark. The non-breaking space matters at 375 px, where an
// ordinary space lets "21 560 Kč" wrap into "21" / "560 Kč".

import { czPlural, type PluralForms } from '@/i18n/plural'

const NBSP = ' '

/** group renders an integer with non-breaking thousands separators. */
function group(n: number): string {
  const neg = n < 0
  const digits = Math.abs(n).toString()
  let out = ''
  for (let i = 0; i < digits.length; i++) {
    if (i > 0 && (digits.length - i) % 3 === 0) out += NBSP
    out += digits[i]
  }
  return (neg ? '−' : '') + out
}

/** kc renders haléře as WHOLE koruny: 2 156 049 → "21 560 Kč". For totals. */
export function kc(haler: number): string {
  return `${group(Math.round(haler / 100))}${NBSP}Kč`
}

/** kcSigned renders a balance with an explicit sign, because the sign is half the
 *  meaning and colour must never carry it alone: "+40 Kč" / "−1 240 Kč". */
export function kcSigned(haler: number): string {
  const whole = Math.round(Math.abs(haler) / 100)
  return `${haler < 0 ? '−' : '+'}${group(whole)}${NBSP}Kč`
}

/** kc2 renders haléře with two decimals: 485 865 → "4 858,65 Kč". For unit prices
 *  and monthly fees, where the haléře are the number. */
export function kc2(haler: number): string {
  const neg = haler < 0
  const abs = Math.abs(haler)
  const whole = Math.floor(abs / 100)
  const cents = abs % 100
  return `${neg ? '−' : ''}${group(whole)},${String(cents).padStart(2, '0')}${NBSP}Kč`
}

/** kwh renders tenths of kWh, dropping the decimal when it is a whole number —
 *  which it always is for a meter like Karel's. A future decimal meter shows its
 *  tenth rather than being silently truncated. */
export function kwh(dkwh: number): string {
  const neg = dkwh < 0
  const abs = Math.abs(dkwh)
  const whole = Math.floor(abs / 10)
  const tenth = abs % 10
  const body = tenth === 0 ? group(whole) : `${group(whole)},${tenth}`
  return `${neg ? '−' : ''}${body}${NBSP}kWh`
}

/** kwhNum is `kwh` without the unit, for places that print the unit separately. */
export function kwhNum(dkwh: number): string {
  return kwh(dkwh).replace(`${NBSP}kWh`, '')
}

/** kwhWhole renders tenths of kWh as WHOLE kWh, rounded DOWN.
 *
 *  For the headroom chips only, and the flooring is deliberate on both counts.
 *  A budget figure should understate rather than overstate what the money buys —
 *  "asi 200 kWh" that turns out to be 200,6 is a pleasant surprise, the reverse
 *  is not — and it is what the spec's pinned values assume: 1765 dkWh reads as
 *  176, not the 177 that rounding would give. Everywhere else in the module,
 *  kwh() keeps the tenth, because a METER READING must never be rounded at all. */
export function kwhWhole(dkwh: number): string {
  return `${group(Math.floor(dkwh / 10))}${NBSP}kWh`
}

/** pct renders a share as a whole percentage. */
export function pct(part: number, total: number): string {
  if (total === 0) return '0 %'
  return `${Math.round((part / total) * 100)}${NBSP}%`
}

/** plural renders "N form" with a non-breaking space, so a count never wraps
 *  away from its noun. */
export function plural(n: number, forms: PluralForms): string {
  return `${n}${NBSP}${czPlural(n, forms)}`
}

/** monthLabelShort renders a 'YYYY-MM' key as "čvc 26" for a chart axis, where
 *  the full month name never fits at 375 px. */
const SHORT_MONTHS = ['led', 'úno', 'bře', 'dub', 'kvě', 'čvn', 'čvc', 'srp', 'zář', 'říj', 'lis', 'pro']
export function monthLabelShort(ym: string): string {
  const [y, m] = ym.split('-').map(Number)
  return `${SHORT_MONTHS[m - 1]}${NBSP}${String(y).slice(2)}`
}
