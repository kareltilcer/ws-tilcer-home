import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { Check, Printer, SkipForward, Undo2 } from 'lucide-react'
import { qk } from '@/api/keys'
import { useAuth } from '@/app/auth'
import { useOnline } from '@/platform/pwa/offline'
import { cs } from '@/i18n/cs'
import { count, PLURAL } from '@/i18n/plural'
import { todayISO } from '@/i18n/format'
import { ApiError } from '@/api/client'
import { Button, Spinner } from '@/components/ui/ui'
import { cn } from '@/lib/utils'
import { completeTask, listBeds, listTasks, reopenTask, updateTask } from '../api/endpoints'
import type { GardenSeason, GardenTask } from '../api/types'
import { fmtWindow } from '../components/labels'

// KALENDÁŘ — WORK AS WINDOWS, NOT DATES.
//
// Most calendar UIs draw points. Garden work does not have points: "vysaď
// rajčata mezi 15. a 25. květnem" is the truth, a job legitimately spans ten
// days, and it can be OVERDUE — window passed, still open — which a due-date
// calendar cannot express at all.
//
// So the layout is a list grouped by ISO WEEK, with overdue lifted out to the
// top as its own group. Weeks are how gardening is planned and how the printed
// sheet is laid out, so the screen and the paper have the same shape.
//
// This is also the PRINT TARGET (D125). Writes are disabled offline and the
// garden has no signal; paper is the answer, not an apology for it.

const MONTH_NAMES = [
  'leden', 'únor', 'březen', 'duben', 'květen', 'červen',
  'červenec', 'srpen', 'září', 'říjen', 'listopad', 'prosinec',
]

export function KalendarTab({ season }: { season: GardenSeason | undefined }) {
  const qc = useQueryClient()
  const { canWrite } = useAuth()
  const online = useOnline()
  const writable = canWrite && online

  const [month, setMonth] = useState(() => new Date().getMonth())
  const [year, setYear] = useState(() => season?.year ?? new Date().getFullYear())
  const [bedFilter, setBedFilter] = useState('')
  const [showDone, setShowDone] = useState(false)

  const from = `${year}-${String(month + 1).padStart(2, '0')}-01`
  const to = `${year}-${String(month + 1).padStart(2, '0')}-${String(daysInMonth(year, month)).padStart(2, '0')}`
  // LOCAL, not UTC — the server's overdue bound is derived in its own timezone,
  // and a day-behind `to_end` hides exactly the work that just became missed.
  const yesterday = todayISO(-1)

  const beds = useQuery({ queryKey: qk.gardenBeds(false), queryFn: () => listBeds(false) })
  const tasks = useQuery({
    queryKey: qk.gardenTasks({ from, to, bedFilter, showDone }),
    // The window filter is an OVERLAP server-side, so a job that started in
    // March and runs into April appears in both months — which is what somebody
    // looking at April needs to see.
    queryFn: () =>
      listTasks({
        from,
        to,
        bed_id: bedFilter || undefined,
        status: showDone ? undefined : 'open',
        limit: 200,
      }),
  })
  // Missed work is NOT a subset of the month query: `from` means window_to >=
  // from, which excludes exactly the windows that ended before the displayed
  // month. The dashboard widget's missed rows navigate here, so the calendar
  // asks the same `to_end` question the widget does (as Přehled already does)
  // and lifts the answer into Zmeškané, whatever month is on screen.
  const missed = useQuery({
    queryKey: qk.gardenTasks({ toEnd: yesterday, bedFilter }),
    queryFn: () =>
      listTasks({ to_end: yesterday, bed_id: bedFilter || undefined, status: 'open', limit: 200 }),
  })

  const invalidate = () => {
    void qc.invalidateQueries({ queryKey: qk.gardenAll })
    void qc.invalidateQueries({ queryKey: qk.dashboard })
  }
  const onErr = (e: unknown) =>
    toast.error(e instanceof ApiError ? (e.detail ?? cs.common.errorTitle) : cs.common.errorTitle)

  const complete = useMutation({ mutationFn: (id: string) => completeTask(id), onSuccess: invalidate, onError: onErr })
  const reopen = useMutation({ mutationFn: (id: string) => reopenTask(id), onSuccess: invalidate, onError: onErr })
  const skip = useMutation({
    mutationFn: (id: string) => updateTask(id, { status: 'skipped' }),
    onSuccess: invalidate,
    onError: onErr,
  })

  const groups = useMemo(
    () => groupByWeek(tasks.data?.items ?? [], missed.data?.items ?? []),
    [tasks.data, missed.data],
  )

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2 print:hidden">
        <select
          value={month}
          onChange={(e) => setMonth(Number(e.target.value))}
          className="h-9 rounded-md border border-border bg-s2 px-2 text-sm font-semibold capitalize"
        >
          {MONTH_NAMES.map((m, i) => (
            <option key={m} value={i}>
              {m}
            </option>
          ))}
        </select>
        <select
          value={year}
          onChange={(e) => setYear(Number(e.target.value))}
          className="h-9 rounded-md border border-border bg-s2 px-2 text-sm font-semibold"
        >
          {yearRange(season?.year ?? new Date().getFullYear()).map((y) => (
            <option key={y} value={y}>
              {y}
            </option>
          ))}
        </select>

        <select
          value={bedFilter}
          onChange={(e) => setBedFilter(e.target.value)}
          className="h-9 rounded-md border border-border bg-s2 px-2 text-sm"
        >
          <option value="">{cs.garden.filterBed}: vše</option>
          {(beds.data?.items ?? []).map((b) => (
            <option key={b.id} value={b.id}>
              {b.code} — {b.name}
            </option>
          ))}
        </select>

        <label className="inline-flex items-center gap-1.5 text-[12.5px] text-muted">
          <input type="checkbox" checked={showDone} onChange={(e) => setShowDone(e.target.checked)} />
          hotové
        </label>

        <div className="flex-1" />
        {/* The print action stays available OFFLINE — it is the offline answer. */}
        <Button size="sm" variant="secondary" onClick={() => window.print()}>
          <Printer size={14} aria-hidden />
          {cs.garden.print}
        </Button>
      </div>

      {tasks.isPending ? (
        <div className="grid min-h-[200px] place-items-center print:hidden">
          <Spinner />
        </div>
      ) : (
        <div className="space-y-4 print:hidden">
          {groups.overdue.length > 0 && (
            <WeekGroup
              title={`${cs.garden.overdue} · ${count(groups.overdue.length, PLURAL.works)}`}
              tone="danger"
              tasks={groups.overdue}
              writable={writable}
              onComplete={(id) => complete.mutate(id)}
              onReopen={(id) => reopen.mutate(id)}
              onSkip={(id) => skip.mutate(id)}
            />
          )}
          {groups.weeks.map((w) => (
            <WeekGroup
              key={w.key}
              title={cs.garden.week(w.label)}
              tasks={w.items}
              writable={writable}
              onComplete={(id) => complete.mutate(id)}
              onReopen={(id) => reopen.mutate(id)}
              onSkip={(id) => skip.mutate(id)}
            />
          ))}
          {groups.overdue.length === 0 && groups.weeks.length === 0 && (
            <p className="rounded-lg border border-dashed border-border p-8 text-center text-sm text-muted">
              {cs.garden.widgetQuiet}
            </p>
          )}
        </div>
      )}

      <MonthPrintSheet
        month={month}
        year={year}
        overdue={groups.overdue}
        weeks={groups.weeks}
      />
    </div>
  )
}

function WeekGroup({
  title,
  tone,
  tasks,
  writable,
  onComplete,
  onReopen,
  onSkip,
}: {
  title: string
  tone?: 'danger'
  tasks: GardenTask[]
  writable: boolean
  onComplete: (id: string) => void
  onReopen: (id: string) => void
  onSkip: (id: string) => void
}) {
  return (
    <section className="rounded-lg border border-border bg-s1">
      <header
        className={cn(
          'border-b border-border px-4 py-2.5 text-[12px] font-bold tracking-wide uppercase',
          tone === 'danger' ? 'text-danger' : 'text-subtle',
        )}
      >
        {title}
      </header>
      <ul className="divide-y divide-border">
        {tasks.map((t) => (
          <li key={t.id} className="flex flex-wrap items-center gap-2 px-4 py-2.5">
            <span className="min-w-0 flex-1">
              <span
                className={cn(
                  'block truncate text-[13.5px] font-semibold',
                  t.status !== 'open' && 'text-muted line-through',
                )}
              >
                {t.title_cs}
              </span>
              <span className="flex flex-wrap items-center gap-x-2 font-mono text-[11px] text-muted">
                {/* A RANGE, always. */}
                <span>{fmtWindow(t.window_from, t.window_to)}</span>
                {t.bed_code && <span className="text-subtle">· {t.bed_code}</span>}
                {t.is_edited && (
                  <span className="text-accent" title={cs.garden.taskEditedHint}>
                    · {cs.garden.taskEdited}
                  </span>
                )}
              </span>
            </span>

            {t.status === 'open' ? (
              <span className="flex flex-none gap-1">
                <Button size="sm" variant="ghost" disabled={!writable} onClick={() => onSkip(t.id)}>
                  <SkipForward size={14} aria-hidden />
                  <span className="sr-only">{cs.garden.markSkipped}</span>
                </Button>
                <Button size="sm" variant="secondary" disabled={!writable} onClick={() => onComplete(t.id)}>
                  <Check size={14} aria-hidden />
                  {cs.garden.markDone}
                </Button>
              </span>
            ) : (
              <Button size="sm" variant="ghost" disabled={!writable} onClick={() => onReopen(t.id)}>
                <Undo2 size={14} aria-hidden />
                {cs.garden.reopen}
              </Button>
            )}
          </li>
        ))}
      </ul>
    </section>
  )
}

/** The month of work as a real artefact (D125): checkboxes you can tick with a
 *  pencil, bed codes readable at arm's length, grouped by week. */
function MonthPrintSheet({
  month,
  year,
  overdue,
  weeks,
}: {
  month: number
  year: number
  overdue: GardenTask[]
  weeks: { key: string; label: string; items: GardenTask[] }[]
}) {
  const blocks = [
    ...(overdue.length ? [{ key: 'overdue', label: cs.garden.overdue, items: overdue }] : []),
    ...weeks.map((w) => ({ key: w.key, label: `${cs.garden.week(w.label)}`, items: w.items })),
  ]
  return (
    <div className="garden-print">
      <h1>
        {cs.garden.printMonth} — {MONTH_NAMES[month]} {year}
      </h1>
      <p className="garden-print-sub">{cs.garden.title}</p>
      {blocks.map((b) => (
        <section key={b.key} className="garden-print-week">
          <h2>{b.label}</h2>
          {b.items.map((t) => (
            <div key={t.id} className="garden-print-row">
              <span className="garden-print-box" aria-hidden />
              <span className="garden-print-bed">{t.bed_code ?? '—'}</span>
              <span className="garden-print-window">{fmtWindow(t.window_from, t.window_to)}</span>
              <span className="garden-print-title">{t.title_cs}</span>
            </div>
          ))}
        </section>
      ))}
      {blocks.length === 0 && <p>{cs.garden.widgetQuiet}</p>}
    </div>
  )
}

function groupByWeek(tasks: GardenTask[], missed: GardenTask[]) {
  // The month query's own overdue rows are a subset of the to_end query, so
  // dedupe by id — but keep both sources so neither a pending missed query nor
  // its 200-row cap can hide work the month view already has in hand.
  const seen = new Set(missed.map((t) => t.id))
  const overdue = [...missed, ...tasks.filter((t) => t.overdue && !seen.has(t.id))]
  const rest = tasks.filter((t) => !t.overdue)
  const map = new Map<string, GardenTask[]>()
  for (const t of rest) {
    const key = isoWeekKey(t.window_from)
    map.set(key, [...(map.get(key) ?? []), t])
  }
  const weeks = [...map.entries()]
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([key, items]) => ({ key, label: weekLabel(key), items }))
  return { overdue, weeks }
}

function isoWeekKey(iso: string): string {
  const d = new Date(iso + 'T00:00:00Z')
  if (Number.isNaN(d.getTime())) return '0000-W00'
  // ISO week: Thursday of the current week decides the year.
  const target = new Date(d.getTime())
  target.setUTCDate(target.getUTCDate() + 3 - ((target.getUTCDay() + 6) % 7))
  const firstThursday = new Date(Date.UTC(target.getUTCFullYear(), 0, 4))
  const week =
    1 + Math.round(((target.getTime() - firstThursday.getTime()) / 86400000 - 3 + ((firstThursday.getUTCDay() + 6) % 7)) / 7)
  return `${target.getUTCFullYear()}-W${String(week).padStart(2, '0')}`
}

function weekLabel(key: string): string {
  const [year, week] = key.split('-W')
  return `${Number(week)}/${year}`
}

function daysInMonth(year: number, monthIndex: number): number {
  return new Date(Date.UTC(year, monthIndex + 1, 0)).getUTCDate()
}

function yearRange(centre: number): number[] {
  return [centre - 1, centre, centre + 1]
}
