import { useMemo } from 'react'
import { cs } from '@/i18n/cs'
import { Input } from '@/components/ui/ui'
import { cn } from '@/lib/utils'
import type { GardenAnchor, GardenSeason, GardenWindow } from '../api/types'
import { fmtWindow } from './labels'

// THE TIMING-WINDOW CONTROL — the weirdest input in Home, and the one that
// decides whether the knowledge base is fillable at all.
//
// A window is {anchor, from, to} with three anchors, and it has to be enterable
// by someone reading a Czech seed packet ("výsev březen–duben") AND by someone
// thinking in frost offsets ("6–8 týdnů před posledním mrazem"), without a
// manual. It is used four times per crop, forty crops deep — a control that
// takes six taps costs an evening.
//
// So: one control with a MODE, and — the part that makes the abstraction land —
// THE RESOLVED REAL DATES ECHOED LIVE UNDERNEATH as you type. Without that echo
// "last_frost, -56, -42" is three numbers; with it, it is a fortnight in March.

const ANCHORS: { value: GardenAnchor; label: string }[] = [
  { value: 'week', label: cs.garden.anchorWeek },
  { value: 'last_frost', label: cs.garden.anchorLastFrost },
  { value: 'first_frost', label: cs.garden.anchorFirstFrost },
]

export function TimingWindowInput({
  label,
  value,
  season,
  disabled,
  onChange,
}: {
  label: string
  value: GardenWindow | null | undefined
  season: GardenSeason | undefined
  disabled?: boolean
  onChange: (next: GardenWindow | null) => void
}) {
  const win = value ?? null
  const anchor = win?.anchor ?? 'week'

  const resolved = useMemo(() => resolveWindow(win, season), [win, season])
  const inverted = win != null && win.to < win.from

  const set = (patch: Partial<GardenWindow>) => {
    const base: GardenWindow = win ?? { anchor: 'week', from: 1, to: 1 }
    onChange({ ...base, ...patch })
  }

  return (
    <fieldset
      className={cn('rounded-md border border-border bg-s2 p-3', disabled && 'opacity-60')}
      disabled={disabled}
    >
      <legend className="px-1 text-[12.5px] font-semibold">{label}</legend>

      <div className="mt-1 flex flex-wrap items-end gap-2">
        <label className="min-w-[160px] flex-1">
          <span className="sr-only">{cs.garden.terminVysevu}</span>
          <select
            value={win ? anchor : ''}
            onChange={(e) => {
              const next = e.target.value as GardenAnchor | ''
              if (!next) {
                onChange(null)
                return
              }
              // Switching mode resets the numbers, because a week number and a
              // day offset are not the same quantity and carrying "12" across
              // would silently mean "twelve days after the frost".
              onChange(next === 'week' ? { anchor: next, from: 10, to: 13 } : { anchor: next, from: -14, to: 0 })
            }}
            className="h-10 w-full rounded-md border border-border bg-s3 px-2 text-sm text-fg"
          >
            <option value="">—</option>
            {ANCHORS.map((a) => (
              <option key={a.value} value={a.value}>
                {a.label}
              </option>
            ))}
          </select>
        </label>

        {win && (
          <>
            <label className="w-24">
              <span className="mb-1 block text-[11.5px] text-muted">{cs.garden.windowFrom}</span>
              <Input
                type="number"
                inputMode="numeric"
                value={win.from}
                onChange={(e) => set({ from: Number(e.target.value) })}
              />
            </label>
            <label className="w-24">
              <span className="mb-1 block text-[11.5px] text-muted">{cs.garden.windowTo}</span>
              <Input
                type="number"
                inputMode="numeric"
                value={win.to}
                onChange={(e) => set({ to: Number(e.target.value) })}
              />
            </label>
          </>
        )}
      </div>

      {win && (
        <p className="mt-1.5 text-[11.5px] text-subtle text-pretty">
          {anchor === 'week' ? cs.garden.windowWeekHint : cs.garden.windowFrostHint}
        </p>
      )}

      {/* The live echo. Three states, and the middle one is the important one:
          a window whose anchor the season has not set resolves to NOTHING, and
          says which anchor is missing rather than showing a plausible date. */}
      {win && inverted && (
        <p className="mt-1.5 text-[12px] font-semibold text-danger">{cs.garden.windowInverted}</p>
      )}
      {win && !inverted && resolved && (
        <p className="mt-1.5 text-[12px] font-semibold text-accent">{cs.garden.windowResolved(resolved)}</p>
      )}
      {win && !inverted && !resolved && (
        <p className="mt-1.5 text-[12px] text-muted text-pretty">{cs.garden.windowNoAnchor}</p>
      )}
    </fieldset>
  )
}

/** resolveWindow mirrors the server's timing.go for the live echo only.
 *
 *  It is a PREVIEW, never a source of truth: every date the app stores comes
 *  from the backend, which resolves against the season exactly once. This exists
 *  so the number you are typing has a visible meaning while you type it. */
export function resolveWindow(win: GardenWindow | null, season: GardenSeason | undefined): string | null {
  if (!win || !season || win.to < win.from) return null

  if (win.anchor === 'week') {
    const from = isoWeekMonday(season.year, win.from)
    const to = addDays(isoWeekMonday(season.year, win.to), 6)
    return fmtWindow(toISO(from), toISO(to))
  }
  const anchorISO = win.anchor === 'last_frost' ? season.last_frost_on : season.first_frost_on
  if (!anchorISO) return null
  const base = new Date(anchorISO + 'T00:00:00Z')
  if (Number.isNaN(base.getTime())) return null
  return fmtWindow(toISO(addDays(base, win.from)), toISO(addDays(base, win.to)))
}

/** isoWeekMonday returns the Monday of an ISO week, clamping a missing week 53
 *  to week 52 — the same deviation the backend makes, for the same reason: the
 *  alternative silently lands in January of the following year. */
function isoWeekMonday(year: number, week: number): Date {
  const weeks = weeksInISOYear(year)
  const w = Math.min(Math.max(week, 1), weeks)
  // 4 January is in ISO week 1 of every year, by definition.
  const jan4 = new Date(Date.UTC(year, 0, 4))
  const offset = (jan4.getUTCDay() + 6) % 7 // Monday = 0
  return addDays(jan4, -offset + (w - 1) * 7)
}

function weeksInISOYear(year: number): number {
  const jan4 = new Date(Date.UTC(year, 0, 4))
  const offset = (jan4.getUTCDay() + 6) % 7
  const week53 = addDays(jan4, -offset + 52 * 7)
  // Week 53 exists iff its Thursday is still in this year.
  return addDays(week53, 3).getUTCFullYear() === year ? 53 : 52
}

function addDays(d: Date, n: number): Date {
  return new Date(d.getTime() + n * 86400000)
}

function toISO(d: Date): string {
  return d.toISOString().slice(0, 10)
}
