import { useMemo } from 'react'
import { cs } from '@/i18n/cs'
import { cn } from '@/lib/utils'
import type { GardenPlanting } from '../api/types'
import { fmtWindow } from './labels'

// TIME PASSES THROUGH A BED (D107).
//
// Every warning in this module turns on OVERLAPPING OCCUPANCY, so a bed card
// that shows its crops as a flat list is hiding the concept the module runs on.
// Spring špenát and autumn pórek share a bed on the calendar and never meet —
// and a design that cannot show that is a design where the household cannot see
// why one pairing warns and another does not.
//
// The honest representation is a year strip with occupancy bars. It is
// desktop-shaped by nature, so the reflow is the real design question: at 375 px
// the strip stays a strip and simply gets shorter — the crop name rides on its
// own bar and truncates rather than moving away from it. A stacked list of crop
// names would have thrown away the whole idea, so that is the one thing the
// reflow does not do.
//
// The bars follow the chart rules: fixed order, 2 px gaps rather than hairline
// borders, a label on every bar, and the dates in the title — colour is never
// the only carrier.

const MONTH_TICKS = ['L', 'Ú', 'B', 'D', 'K', 'Č', 'Č', 'S', 'Z', 'Ř', 'L', 'P']

/** The five categorical slots, used in FIXED ORDER. There are more families than
 *  slots by design — see labels.tsx — so a colour here means "the third crop in
 *  this bed", never "brukvovité". The label on the bar carries the meaning. */
const BAR_TONES = [
  'bg-[var(--c1)]',
  'bg-[var(--c2)]',
  'bg-[var(--c4)]',
  'bg-[var(--c3)]',
  'bg-[var(--c5)]',
]

interface Band {
  planting: GardenPlanting
  leftPct: number
  widthPct: number
  from: string
  to: string
  openEnded: boolean
}

export function OccupancyStrip({
  plantings,
  year,
  compact,
}: {
  plantings: GardenPlanting[]
  year: number
  compact?: boolean
}) {
  const bands = useMemo(() => buildBands(plantings, year), [plantings, year])

  if (bands.length === 0) return null

  return (
    <div className={cn('select-none', compact ? 'py-1' : 'py-2')}>
      <div className="mb-1 flex justify-between font-mono text-[9px] text-subtle" aria-hidden>
        {MONTH_TICKS.map((m, i) => (
          <span key={i} className="w-[8.33%] text-center">
            {m}
          </span>
        ))}
      </div>

      {/* One row per crop rather than one stacked bar: overlap is the thing being
          shown, and stacking would hide exactly the case that matters.

          The strip is PRESENTATIONAL. The crop list above it opens the same
          plantings at a full-size target, and duplicating that here at 16 px
          would be a tap target nobody outdoors can hit — so this is a picture of
          the year, with the whole of it also said in words for assistive tech. */}
      <ul
        className="space-y-[2px]"
        aria-label={bands
          .map((b) => `${b.planting.plant_name}: ${fmtWindow(b.from, b.to)}`)
          .join('; ')}
      >
        {bands.map((band, i) => (
          <li
            key={band.planting.id}
            className="relative h-4 w-full rounded-[3px] bg-s3"
            title={`${band.planting.plant_name} · ${fmtWindow(band.from, band.to)}`}
          >
            <span
              className={cn(
                'absolute inset-y-0 flex items-center overflow-hidden rounded-[3px] px-1.5',
                BAR_TONES[i % BAR_TONES.length],
                // An open-ended occupancy is drawn square at the open end, so
                // "still going" and "ends here" are visibly different.
                band.openEnded && 'rounded-r-none',
              )}
              style={{ left: `${band.leftPct}%`, width: `${band.widthPct}%` }}
            >
              {/* The label rides ON the bar: colour never carries the meaning by
                  itself, here or anywhere else in the module. */}
              <span className="truncate text-[10px] font-bold text-[var(--bg)]">
                {band.planting.plant_name}
              </span>
            </span>
          </li>
        ))}
      </ul>

      {compact && (
        <p className="mt-1 text-[10.5px] text-subtle">{cs.garden.occupancyAxis}</p>
      )}
    </div>
  )
}

/** buildBands turns each planting's derived occupancy into a position on the
 *  year. A planting with no start is skipped rather than drawn at the origin: a
 *  bar that means "we have not decided yet" would read as "January". */
function buildBands(plantings: GardenPlanting[], year: number): Band[] {
  const yearStart = Date.UTC(year, 0, 1)
  const yearEnd = Date.UTC(year, 11, 31)
  const span = yearEnd - yearStart

  const out: Band[] = []
  for (const p of plantings) {
    const fromISO = p.occupancy?.from
    if (!fromISO) continue
    const toISO = p.occupancy?.to
    const from = Date.parse(fromISO + 'T00:00:00Z')
    const to = toISO ? Date.parse(toISO + 'T00:00:00Z') : yearEnd
    if (Number.isNaN(from) || Number.isNaN(to)) continue

    const clampedFrom = Math.max(from, yearStart)
    const clampedTo = Math.min(Math.max(to, clampedFrom), yearEnd)
    const leftPct = ((clampedFrom - yearStart) / span) * 100
    const widthPct = Math.max(((clampedTo - clampedFrom) / span) * 100, 2)

    out.push({
      planting: p,
      leftPct,
      widthPct: Math.min(widthPct, 100 - leftPct),
      from: fromISO,
      to: toISO ?? `${year}-12-31`,
      openEnded: !toISO,
    })
  }
  // Chronological, so the strip reads left to right like the year it draws.
  return out.sort((a, b) => a.leftPct - b.leftPct)
}
