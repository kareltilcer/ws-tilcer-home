import { useMemo } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { Sprout } from 'lucide-react'
import { qk } from '@/api/keys'
import { routes } from '@/app/routes'
import { useAuth } from '@/app/auth'
import { cs } from '@/i18n/cs'
import { count, PLURAL } from '@/i18n/plural'
import { todayISO } from '@/i18n/format'
import { Button, Spinner } from '@/components/ui/ui'
import { listBeds, listHarvests, listPlantings, listTasks } from '../api/endpoints'
import type { GardenCheckResult, GardenSeason } from '../api/types'
import { currentYear } from '../GardenPage'
import { fmtWindow } from '../components/labels'

// PŘEHLED — the season at a glance, designed for TWO GARDENS.
//
// In February this app is a planner: beds empty, the plan being drafted,
// warnings loud, almost no work. In July it is a tracker: everything occupied,
// harvest tasks daily, the plan frozen, the warnings long dismissed. A dashboard
// tuned to July is dead for four months of the year, and a planner tuned to
// February is in the way for the other eight.
//
// The answer here is that the SAME four tiles carry both — they are questions
// that have an answer in either month ("what needs doing", "how much is
// unplanned", "what has come out") — and the two panels below reorder rather
// than appear and disappear. Nothing on this screen is empty in one season and
// full in the other; it just says different numbers.

export function PrehledTab({
  season,
  bedCount,
  check,
}: {
  season: GardenSeason | undefined
  bedCount: number
  check: GardenCheckResult | undefined
}) {
  const navigate = useNavigate()
  const { canWrite } = useAuth()
  const year = season?.year ?? currentYear()
  // LOCAL dates, not UTC: the backend derives the same bounds in the configured
  // timezone, so a UTC-derived `yesterday` would ask a different question than
  // the widget for the first hours after local midnight.
  const today = todayISO()
  const yesterday = todayISO(-1)
  const in30 = todayISO(30)

  const tasks = useQuery({
    queryKey: qk.gardenTasks({ from: today, to: in30, status: 'open' }),
    queryFn: () => listTasks({ from: today, to: in30, status: 'open', limit: 100 }),
  })
  // Overdue work needs its OWN query, exactly as the widget does. `from: today`
  // above means window_to >= today, which is the precise negation of overdue
  // (window_to < today) — so filtering the list above for it can only ever
  // return nothing, however much work is actually missed.
  const overdueTasks = useQuery({
    queryKey: qk.gardenTasks({ toEnd: yesterday, status: 'open' }),
    queryFn: () => listTasks({ to_end: yesterday, status: 'open', limit: 200 }),
  })
  const beds = useQuery({ queryKey: qk.gardenBeds(false), queryFn: () => listBeds(false) })
  const plantings = useQuery({
    queryKey: qk.gardenPlantings({ year }),
    queryFn: () => listPlantings({ year, limit: 200 }),
  })
  // Permanents have no season, so the year query above cannot see them — and a
  // bed holding only a fruit tree would be counted "nezaplánováno" in every
  // season for the rest of its life, disagreeing with check C7, which does count
  // them.
  const permanents = useQuery({
    queryKey: qk.gardenPlantings({ permanent: true }),
    queryFn: () => listPlantings({ permanent: true, limit: 200 }),
  })
  const harvests = useQuery({
    queryKey: qk.gardenHarvests({ year }),
    queryFn: () => listHarvests({ year }),
  })

  const stats = useMemo(() => {
    const items = tasks.data?.items ?? []
    const overdue = (overdueTasks.data?.items ?? []).length
    const used = new Set(
      [...(plantings.data?.items ?? []), ...(permanents.data?.items ?? [])]
        .map((p) => p.bed_id)
        .filter(Boolean),
    )
    const unplanned = (beds.data?.items ?? []).filter((b) => !used.has(b.id)).length
    // kg ONLY — rows in ks/l/svazek are excluded rather than added to kilograms,
    // and the tile says so rather than quietly producing a wrong total.
    const kg = (harvests.data?.items ?? [])
      .filter((h) => h.unit === 'kg')
      .reduce((sum, h) => sum + h.quantity, 0)
    return { due: items.length, overdue, unplanned, kg }
  }, [tasks.data, overdueTasks.data, beds.data, plantings.data, permanents.data, harvests.data])

  // FIRST RUN: the plan is made of beds, so an empty garden routes to Záhony
  // rather than showing an empty planner. And the reason the order matters is
  // said here, once, where it is actionable.
  if (bedCount === 0) {
    return (
      <div className="grid min-h-[300px] place-items-center text-center">
        <div className="max-w-md rounded-lg border border-dashed border-border p-8">
          <Sprout size={26} className="mx-auto mb-3 text-subtle" aria-hidden />
          <h2 className="mb-1.5 text-lg font-bold">{cs.garden.emptyBedsTitle}</h2>
          <p className="mb-4 text-sm text-muted text-pretty">{cs.garden.emptyBedsBody}</p>
          <Button
            variant="primary"
            disabled={!canWrite}
            title={canWrite ? undefined : cs.common.readOnly}
            onClick={() => navigate(`${routes.zahrada}/zahony`)}
          >
            {cs.garden.emptyBedsCta}
          </Button>
        </div>
      </div>
    )
  }

  const openWarnings = (check?.counts.error ?? 0) + (check?.counts.warn ?? 0)
  const loading = tasks.isPending || beds.isPending || plantings.isPending

  return (
    <div className="space-y-4">
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <Tile
          label={cs.garden.prace}
          value={String(stats.due)}
          meta={stats.overdue > 0 ? `${stats.overdue} zmeškaných` : 'na nejbližších 30 dní'}
          tone={stats.overdue > 0 ? 'warn' : undefined}
          to={`${routes.zahrada}/kalendar`}
        />
        <Tile
          label={cs.garden.varovani}
          value={String(openWarnings)}
          meta={check?.history.status === 'no_history' ? 'bez historie rotace' : cs.garden.kontrolaPlanu}
          tone={openWarnings > 0 ? 'warn' : undefined}
          to={`${routes.zahrada}/plan/${year}#kontrola`}
        />
        <Tile
          label={cs.garden.zahon}
          value={String(stats.unplanned)}
          meta={`nezaplánováno z ${count(bedCount, PLURAL.beds)}`}
          to={`${routes.zahrada}/plan/${year}`}
        />
        <Tile
          label={cs.garden.sklizen}
          value={formatKg(stats.kg)}
          meta="letos, jen v kilogramech"
          to={`${routes.zahrada}/sklizen`}
        />
      </div>

      {loading ? (
        <div className="grid min-h-[200px] place-items-center">
          <Spinner />
        </div>
      ) : (
        <div className="grid gap-4 lg:grid-cols-[1.3fr_1fr]">
          <section className="rounded-lg border border-border bg-s1">
            <header className="flex items-center gap-2 border-b border-border px-4 py-3">
              <h2 className="flex-1 text-base font-bold">{cs.garden.calendarTitle}</h2>
              <Link
                to={`${routes.zahrada}/kalendar`}
                className="text-[12.5px] font-semibold text-accent underline-offset-2 hover:underline"
              >
                {cs.garden.tabs.kalendar}
              </Link>
            </header>
            <ul className="divide-y divide-border">
              {(tasks.data?.items ?? []).slice(0, 8).map((t) => (
                <li key={t.id} className="flex items-center gap-3 px-4 py-2.5">
                  <span className="min-w-0 flex-1">
                    <span className="block truncate text-[13.5px] font-semibold">{t.title_cs}</span>
                    <span className="font-mono text-[11px] text-muted">
                      {fmtWindow(t.window_from, t.window_to)}
                      {t.bed_code ? ` · ${t.bed_code}` : ''}
                    </span>
                  </span>
                  {t.overdue && (
                    <span className="flex-none rounded-md border border-danger/45 bg-danger/10 px-1.5 py-0.5 text-[10.5px] font-bold text-danger">
                      {cs.garden.overdue}
                    </span>
                  )}
                </li>
              ))}
              {(tasks.data?.items ?? []).length === 0 && (
                <li className="px-4 py-6 text-center text-[13px] text-muted">{cs.garden.widgetQuiet}</li>
              )}
            </ul>
          </section>

          <section className="rounded-lg border border-border bg-s1">
            <header className="flex items-center gap-2 border-b border-border px-4 py-3">
              <h2 className="flex-1 text-base font-bold">{cs.garden.zahon}</h2>
              <Link
                to={`${routes.zahrada}/plan/${year}`}
                className="text-[12.5px] font-semibold text-accent underline-offset-2 hover:underline"
              >
                {cs.garden.tabs.plan}
              </Link>
            </header>
            <ul className="divide-y divide-border">
              {(beds.data?.items ?? []).slice(0, 10).map((bed) => {
                const inBed = (plantings.data?.items ?? []).filter((p) => p.bed_id === bed.id)
                return (
                  <li key={bed.id} className="flex items-center gap-3 px-4 py-2.5">
                    <span className="flex-none font-mono text-[13px] font-bold">{bed.code}</span>
                    <span className="min-w-0 flex-1 truncate text-[13px] text-muted">
                      {inBed.length > 0
                        ? inBed.map((p) => p.plant_name).join(', ')
                        : cs.garden.bedFree}
                    </span>
                  </li>
                )
              })}
            </ul>
          </section>
        </div>
      )}
    </div>
  )
}

function Tile({
  label,
  value,
  meta,
  tone,
  to,
}: {
  label: string
  value: string
  meta: string
  tone?: 'warn'
  to: string
}) {
  return (
    <Link
      to={to}
      className="relative block overflow-hidden rounded-lg border border-border bg-s1 px-4 py-3.5 hover:bg-s2"
    >
      <span
        className={`absolute inset-y-0 left-0 w-[3px] ${tone === 'warn' ? 'bg-warn' : 'bg-accent'}`}
        aria-hidden
      />
      <span className="block text-[12px] font-semibold text-muted">{label}</span>
      <span className="mt-1 block font-mono text-2xl font-semibold tabular-nums">{value}</span>
      <span className="mt-0.5 block text-[11.5px] text-subtle text-pretty">{meta}</span>
    </Link>
  )
}

function formatKg(kg: number): string {
  if (kg === 0) return '0'
  return kg >= 100 ? String(Math.round(kg)) : kg.toFixed(1).replace('.', ',')
}
