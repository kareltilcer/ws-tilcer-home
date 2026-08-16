// The schedule builder (HANDOFF-design §v5 §5).
//
// A time picker plus a day-pattern control that stays one-handed at 375 px. The
// two worked examples from the PRD (08:00 daily, 20:00 daily) are two taps.
//
// Day-of-month accepts 1–31, NOT the 28 the v5 design file drew (PRD D74): a day
// the month lacks clamps to that month's last day, matching the events module's
// short-month rule, and the hint says so where an admin can act on it.

import { cs } from '@/i18n/cs'
import { cn } from '@/lib/utils'
import { Input } from '@/components/ui/ui'
import type { DaysSpec, WeekdayToken } from '@/api/types'

const WEEKDAYS: { key: WeekdayToken; label: string }[] = [
  { key: 'mon', label: 'Po' },
  { key: 'tue', label: 'Út' },
  { key: 'wed', label: 'St' },
  { key: 'thu', label: 'Čt' },
  { key: 'fri', label: 'Pá' },
  { key: 'sat', label: 'So' },
  { key: 'sun', label: 'Ne' },
]

type Mode = 'daily' | 'weekdays' | 'weekends' | 'picked' | 'dom'

function modeOf(days: DaysSpec): Mode {
  if (days.day_of_month) return 'dom'
  if (days.weekdays?.length) return 'picked'
  if (days.preset === 'weekdays') return 'weekdays'
  if (days.preset === 'weekends') return 'weekends'
  return 'daily'
}

export function ScheduleBuilder({
  timeLocal,
  days,
  onTimeChange,
  onDaysChange,
  disabled,
}: {
  timeLocal: string
  days: DaysSpec
  onTimeChange: (v: string) => void
  onDaysChange: (d: DaysSpec) => void
  disabled?: boolean
}) {
  const mode = modeOf(days)

  const setMode = (m: Mode) => {
    switch (m) {
      case 'daily':
        return onDaysChange({ preset: 'daily' })
      case 'weekdays':
        return onDaysChange({ preset: 'weekdays' })
      case 'weekends':
        return onDaysChange({ preset: 'weekends' })
      case 'picked':
        return onDaysChange({ weekdays: days.weekdays?.length ? days.weekdays : ['mon'] })
      case 'dom':
        return onDaysChange({ day_of_month: days.day_of_month || 1 })
    }
  }

  const toggleWeekday = (key: WeekdayToken) => {
    const set = new Set(days.weekdays ?? [])
    if (set.has(key)) set.delete(key)
    else set.add(key)
    // Never leave an empty selection: a schedule that matches no day would be
    // saved as "enabled" and silently never fire.
    onDaysChange({ weekdays: set.size ? [...set] : ['mon'] })
  }

  const dom = days.day_of_month ?? 1

  return (
    <div className="space-y-3">
      <label className="block max-w-[10rem]">
        <span className="mb-1 block text-[12.5px] font-semibold text-muted">{cs.admin.scheduleTime}</span>
        <Input
          type="time"
          value={timeLocal}
          disabled={disabled}
          onChange={(e) => onTimeChange(e.target.value)}
          className="font-mono"
        />
      </label>

      <div>
        <div className="mb-1.5 text-[12.5px] font-semibold text-muted">{cs.admin.scheduleDays}</div>
        <div className="flex flex-wrap gap-1.5">
          {(
            [
              ['daily', cs.admin.dayDaily],
              ['weekdays', cs.admin.dayWeekdays],
              ['weekends', cs.admin.dayWeekends],
              ['picked', cs.admin.dayPicked],
              ['dom', cs.admin.dayOfMonth],
            ] as const
          ).map(([key, label]) => (
            <button
              key={key}
              type="button"
              disabled={disabled}
              onClick={() => setMode(key)}
              className={cn(
                'inline-flex min-h-[36px] items-center rounded-lg border px-3 text-[13px] font-semibold',
                mode === key ? 'border-accent bg-accent-soft text-accent' : 'border-border bg-s2 text-muted',
                disabled && 'cursor-not-allowed opacity-50',
              )}
            >
              {label}
            </button>
          ))}
        </div>
      </div>

      {mode === 'picked' && (
        <div className="flex flex-wrap gap-1.5">
          {WEEKDAYS.map((d) => {
            const on = (days.weekdays ?? []).includes(d.key)
            return (
              <button
                key={d.key}
                type="button"
                disabled={disabled}
                aria-pressed={on}
                onClick={() => toggleWeekday(d.key)}
                className={cn(
                  'inline-flex h-9 w-11 items-center justify-center rounded-lg border text-[12.5px] font-semibold',
                  on ? 'border-accent text-accent' : 'border-border text-muted',
                )}
              >
                {d.label}
              </button>
            )
          })}
        </div>
      )}

      {mode === 'dom' && (
        <div className="space-y-1.5">
          <label className="flex items-center gap-2.5">
            <span className="text-[12.5px] text-muted">{cs.admin.dayOfMonthLabel}</span>
            <Input
              type="number"
              min={1}
              max={31}
              value={dom}
              disabled={disabled}
              onChange={(e) => {
                const n = Number(e.target.value)
                if (Number.isFinite(n)) onDaysChange({ day_of_month: Math.min(31, Math.max(1, Math.trunc(n))) })
              }}
              className="w-20 font-mono"
            />
          </label>
          {dom >= 29 && (
            <p className="text-[12px] text-muted text-pretty">{cs.admin.dayOfMonthClampHint}</p>
          )}
        </div>
      )}
    </div>
  )
}
