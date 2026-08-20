import { useMemo, useState } from 'react'
import { NavLink, Navigate, Route, Routes, useLocation, useNavigate } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { AlertTriangle, CircleAlert, Settings2, Sprout } from 'lucide-react'
import { qk } from '@/api/keys'
import { routes } from '@/app/routes'
import { useAuth } from '@/app/auth'
import { cs } from '@/i18n/cs'
import { useDocumentTitle } from '@/hooks/useDocumentTitle'
import { Button, Spinner } from '@/components/ui/ui'
import { cn } from '@/lib/utils'
import { checkSeason, listBeds, listSeasons } from './api/endpoints'
import { PrehledTab } from './pages/PrehledTab'
import { PlodinyTab } from './pages/PlodinyTab'
import { ZahonyTab } from './pages/ZahonyTab'
import { PlanTab } from './pages/PlanTab'
import { KalendarTab } from './pages/KalendarTab'
import { SklizenTab } from './pages/SklizenTab'
import { SkladTab } from './pages/SkladTab'
import { TrvalkyTab } from './pages/TrvalkyTab'
import { SettingsDialog } from './components/SettingsDialog'
import './print.css'

// ZAHRADA — the module shell (PRD §V7-7).
//
// This is the first Home module that is a DOORWAY rather than a screen: eight
// sub-pages behind one "Více" entry. The pattern chosen — a horizontal,
// scroll-snapped TAB STRIP under a persistent module header — is the one the
// design settled on, and it becomes the precedent for the next multi-screen
// module, so it is worth saying why:
//
//   - the four thumb tabs stay untouched, which was the constraint;
//   - a strip degrades to a swipeable row at 375 px without a second layout;
//   - the season selector and the warning badge belong to the MODULE, not to any
//     one page, so they live in the header where they stay visible.
//
// The header's warning badge is deliberately always present. Kontrola plánu is
// the feature, and a check you have to navigate to is a check nobody reads.

const TABS = [
  { path: '', label: cs.garden.tabs.prehled, end: true },
  { path: 'plan', label: cs.garden.tabs.plan },
  { path: 'kalendar', label: cs.garden.tabs.kalendar },
  { path: 'plodiny', label: cs.garden.tabs.plodiny },
  { path: 'zahony', label: cs.garden.tabs.zahony },
  { path: 'sklizen', label: cs.garden.tabs.sklizen },
  { path: 'sklad', label: cs.garden.tabs.sklad },
  { path: 'trvalky', label: cs.garden.tabs.trvalky },
]

/** currentYear is the local year, which is what "this season" means everywhere
 *  in the module. It must agree with the backend's HOME_TIMEZONE view — both are
 *  Europe/Prague in practice. */
export function currentYear(now = new Date()): number {
  return now.getFullYear()
}

export function GardenPage() {
  useDocumentTitle(cs.garden.title)
  const location = useLocation()
  const navigate = useNavigate()
  const { canWrite } = useAuth()
  const [settingsOpen, setSettingsOpen] = useState(false)

  // The season the header talks about: the one whose year is now, else the most
  // recent. A garden that has not been set up yet has none, and the pages say so
  // rather than erroring.
  const seasons = useQuery({ queryKey: qk.gardenSeasons, queryFn: listSeasons })
  const beds = useQuery({ queryKey: qk.gardenBeds(false), queryFn: () => listBeds(false) })

  const season = useMemo(() => {
    const items = seasons.data?.items ?? []
    return items.find((s) => s.year === currentYear()) ?? items[0]
  }, [seasons.data])

  const check = useQuery({
    queryKey: qk.gardenCheck(season?.year ?? 0),
    queryFn: () => checkSeason(season!.year),
    enabled: Boolean(season),
  })

  const loading = seasons.isPending || beds.isPending
  const failed = seasons.isError || beds.isError

  if (failed) {
    return (
      <div className="grid min-h-[340px] place-items-center text-center">
        <div className="max-w-sm">
          <div className="mx-auto mb-4 grid h-14 w-14 place-items-center rounded-lg bg-danger/15 text-danger">
            <CircleAlert size={26} aria-hidden />
          </div>
          <div className="mb-1.5 text-lg font-bold">{cs.garden.loadFailed}</div>
          <p className="mb-4 text-sm text-muted text-pretty">{cs.garden.loadFailedBody}</p>
          <Button onClick={() => { void seasons.refetch(); void beds.refetch() }}>{cs.common.retry}</Button>
        </div>
      </div>
    )
  }

  const bedCount = beds.data?.items.length ?? 0
  const counts = check.data?.counts
  const openWarnings = (counts?.error ?? 0) + (counts?.warn ?? 0)

  return (
    <div className="print:block">
      <header className="mb-4 print:hidden">
        <div className="flex flex-wrap items-start gap-3">
          <div className="min-w-[200px] flex-1">
            <h1 className="flex items-center gap-2 text-2xl font-extrabold tracking-tight">
              <Sprout size={22} className="text-accent" aria-hidden />
              {cs.garden.title}
            </h1>
            <p className="mt-1 text-sm text-muted text-pretty">{cs.garden.lede}</p>
          </div>

          {season && (
            <span className="inline-flex items-center gap-2 rounded-full border border-border bg-s2 px-3 py-1.5 text-[12.5px] font-semibold">
              <span>{season.year}</span>
              <span className="text-subtle">· {seasonStatusLabel(season.status)}</span>
            </span>
          )}

          {/* The warning badge belongs to the module, not to the Plán page: a
              check you have to navigate to is a check nobody reads. */}
          {season && (
            <button
              type="button"
              onClick={() => navigate(`${routes.zahrada}/plan/${season.year}#kontrola`)}
              className={cn(
                'inline-flex items-center gap-2 rounded-full border px-3 py-1.5 text-[12.5px] font-semibold',
                openWarnings > 0
                  ? 'border-warn/45 bg-warn/10 text-warn'
                  : 'border-border bg-s2 text-muted',
              )}
            >
              <AlertTriangle size={14} aria-hidden />
              {cs.garden.kontrolaPlanu}
              <span className="font-mono">{openWarnings}</span>
            </button>
          )}

          {/* Garden settings are a MODULE-level thing — the location and the
              frosts every date hangs off — so they open from the header rather
              than claiming a ninth tab. */}
          <Button size="sm" variant="secondary" onClick={() => setSettingsOpen(true)}>
            <Settings2 size={14} aria-hidden />
            <span className="sr-only sm:not-sr-only">{cs.garden.settingsTitle}</span>
          </Button>

          {!canWrite && (
            <span className="inline-flex items-center rounded-full border border-border bg-s3 px-3 py-1.5 text-[12px] font-semibold text-muted">
              {cs.common.readOnly}
            </span>
          )}
        </div>

        {/* The tab strip. Scrollable rather than wrapped, so the row height is
            stable and the active tab can be scrolled into view on mobile. */}
        <nav
          aria-label={cs.garden.title}
          className="-mx-4 mt-4 flex gap-1 overflow-x-auto px-4 md:mx-0 md:px-0"
        >
          {TABS.map((tab) => (
            <NavLink
              key={tab.path}
              to={tab.path ? `${routes.zahrada}/${tab.path}` : routes.zahrada}
              end={tab.end}
              className={({ isActive }) =>
                cn(
                  'flex-none rounded-t-md border-b-2 px-3 py-2 text-[13.5px] font-semibold whitespace-nowrap',
                  isActive
                    ? 'border-accent text-fg'
                    : 'border-transparent text-muted hover:text-fg',
                )
              }
            >
              {tab.label}
            </NavLink>
          ))}
        </nav>
      </header>

      {loading ? (
        <div className="grid min-h-[240px] place-items-center">
          <Spinner />
        </div>
      ) : (
        <Routes>
          <Route index element={<PrehledTab season={season} bedCount={bedCount} check={check.data} />} />
          <Route path="plodiny/*" element={<PlodinyTab />} />
          <Route path="zahony" element={<ZahonyTab />} />
          {/* The planner is addressed by YEAR, matching the API (D128). Landing
              on /plan without one redirects to the current season. */}
          <Route path="plan" element={<Navigate to={`${routes.zahrada}/plan/${season?.year ?? currentYear()}`} replace />} />
          <Route path="plan/:year" element={<PlanTab />} />
          <Route path="kalendar" element={<KalendarTab season={season} />} />
          <Route path="sklizen" element={<SklizenTab season={season} />} />
          <Route path="sklad" element={<SkladTab />} />
          <Route path="trvalky" element={<TrvalkyTab />} />
          <Route path="*" element={<Navigate to={routes.zahrada} replace />} />
        </Routes>
      )}

      <SettingsDialog open={settingsOpen} onOpenChange={setSettingsOpen} canEdit={canWrite} />

      {/* The location key is unused but keeps the route re-render honest when a
          tab links to itself with a different hash (the Kontrola plánu badge). */}
      <span className="hidden">{location.pathname}</span>
    </div>
  )
}

export function seasonStatusLabel(status: string): string {
  switch (status) {
    case 'planning':
      return 'plánuje se'
    case 'active':
      return 'probíhá'
    case 'closed':
      return 'uzavřená'
    default:
      return status
  }
}
