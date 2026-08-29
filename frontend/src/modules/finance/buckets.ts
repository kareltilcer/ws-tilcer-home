import type { FinanceSplit } from './api/types'
import { cs } from '@/i18n/cs'
import { fmtNumber } from '@/i18n/format'

// The four buckets and the money formatting, kept apart from the components that
// draw them (AllocationBar.tsx) so this module's pure helpers can be imported
// anywhere — the widget included — without pulling in JSX.

/** fmtMoney renders whole koruny the Czech way: space-grouped, " Kč" suffix, no
 *  decimals. Everything in this module is whole koruny — there is no other unit. */
export function fmtMoney(v: number): string {
  return `${fmtNumber(v)} Kč`
}

/** displayNeeds is what the UI shows for `needs`. The value can be −1 or −2 Kč
 *  from rounding (PRD §V6-4); that is correct arithmetic and the DATA is never
 *  clamped, but a household account does not read as "minus two crowns", so the
 *  screen shows 0 Kč and states the rounding underneath. */
export function displayNeeds(needs: number): number {
  return needs < 0 ? 0 : needs
}

export interface Bucket {
  key: 'personal' | 'needs' | 'fun' | 'nofun'
  label: string
  /** The single-letter mark. This is SECONDARY ENCODING, not decoration — see
   *  the palette note in theme/globals.css: colour alone does not separate all
   *  four buckets for a protan reader, so every bucket carries a mark and a
   *  shape wherever the four appear together. */
  mark: string
  /** Tailwind background utility, mapped to a --fin-* token. No hex anywhere. */
  bg: string
  /** The swatch shape — the fourth encoding channel after colour, mark and label. */
  shape: string
}

// Order is FIXED: money flows personal → needs → savings, and the bar, the
// legend, the flow viz and the form preview all read it the same way.
export const BUCKETS: readonly Bucket[] = [
  { key: 'personal', label: cs.finance.personal, mark: 'O', bg: 'bg-fin-personal', shape: 'rounded-[3px]' },
  { key: 'needs', label: cs.finance.needs, mark: 'P', bg: 'bg-fin-needs', shape: 'rounded-full' },
  { key: 'fun', label: cs.finance.funSavings, mark: 'Z', bg: 'bg-fin-fun', shape: 'rounded-[2px] rotate-45' },
  { key: 'nofun', label: cs.finance.noFunSavings, mark: 'N', bg: 'bg-fin-nofun', shape: 'rounded-[2px]' },
]

export interface BucketValue extends Bucket {
  value: number
}

/** bucketValues pairs each bucket with its koruny for one month. `needs` uses
 *  the displayed value so a −2 Kč remainder cannot draw a negative-width
 *  segment. */
export function bucketValues(split: FinanceSplit): BucketValue[] {
  return [
    { ...BUCKETS[0], value: split.personal_kaja + split.personal_andy },
    { ...BUCKETS[1], value: displayNeeds(split.needs) },
    { ...BUCKETS[2], value: split.fun_savings },
    { ...BUCKETS[3], value: split.no_fun_savings },
  ]
}
