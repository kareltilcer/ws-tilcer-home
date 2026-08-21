import { useState } from 'react'
import { NavLink, Navigate, Route, Routes } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { Plus, Zap } from 'lucide-react'
import { qk } from '@/api/keys'
import { routes } from '@/app/routes'
import { useAuth } from '@/app/auth'
import { useOnline } from '@/platform/pwa/offline'
import { cs } from '@/i18n/cs'
import { useDocumentTitle } from '@/hooks/useDocumentTitle'
import { fmtDateISO, todayISO } from '@/i18n/format'
import { Button, Spinner } from '@/components/ui/ui'
import { cn } from '@/lib/utils'
import { listPeriods } from './api/endpoints'
import { PrehledTab } from './pages/PrehledTab'
import { OdectyTab } from './pages/OdectyTab'
import { CenikTab } from './pages/CenikTab'
import { HistorieTab } from './pages/HistorieTab'
import { ReadingDialog } from './components/ReadingDialog'
import { PeriodDialog } from './components/PeriodDialog'

// ELEKTŘINA — the module shell (PRD §V8-7).
//
// Four sub-pages behind one "Více" entry, reusing v7's module-header + tab-strip
// pattern rather than inventing a second sub-navigation. Zahrada established it
// for eight pages; four fit the same shape without argument.
//
// WHAT IS DIFFERENT HERE, AND IT SHAPES EVERY DECISION BELOW: this is the only
// module in home with NO Nástěnka surface and no notification of any kind (D147,
// D156). Nothing will ever tell the household it exists. So the module has to be
// worth opening on purpose, which means two things structurally:
//
//   - "Zadat odečet" lives in the MODULE HEADER, not on one tab. It is the one
//     action anybody arrives to perform, and it must be reachable in one thumb
//     press from whichever page they landed on.
//   - Přehled is not "the main screen"; it is the module's entire reason to
//     exist, and it must answer "vyjdou zálohy, nebo doplatím?" within the first
//     screenful at 375 px.

const TABS = [
  { path: '', label: cs.electricity.tabs.prehled, end: true },
  { path: 'odecty', label: cs.electricity.tabs.odecty },
  { path: 'cenik', label: cs.electricity.tabs.cenik },
  { path: 'historie', label: cs.electricity.tabs.historie },
]

export function ElectricityPage() {
  useDocumentTitle(cs.electricity.title)
  const online = useOnline()
  const canWrite = useAuth().canWrite && online
  const [readingOpen, setReadingOpen] = useState(false)
  const [periodOpen, setPeriodOpen] = useState(false)
  // prefillDate carries the hard block's missing date into the reading form —
  // THE DATE ONLY, never a value (D137). Estimating a meter reading is exactly
  // what this module refuses to do.
  const [prefillDate, setPrefillDate] = useState<string | undefined>()

  const periods = useQuery({ queryKey: qk.electricityPeriods, queryFn: listPeriods })
  const items = periods.data?.items ?? []
  // THE PERIOD CONTAINING TODAY, matching store.PeriodContaining — which is what
  // the server falls back to for /summary and what /history always resolves.
  // The list is newest-first by starts_on, so taking its head instead would jump
  // to a period created ahead of its start: Přehled would describe a period with
  // no readings while Historie described the one actually running.
  const today = todayISO()
  const current =
    items.find((p) => p.starts_on <= today && p.ends_on >= today) ?? items[0]

  const openReading = (date?: string) => {
    setPrefillDate(date)
    setReadingOpen(true)
  }

  if (periods.isPending) {
    return (
      <div className="grid min-h-[40vh] place-items-center">
        <Spinner />
      </div>
    )
  }

  // A failed load is NOT "no period": the first-run card below invites a writer
  // to create one, which on a household with a year of data is a duplicate.
  if (periods.isError) {
    return (
      <div className="grid min-h-[40vh] place-items-center text-center">
        <div>
          <p className="font-bold">{cs.electricity.error.loadFailed}</p>
          <p className="mt-1 text-[13px] text-muted">{cs.electricity.error.loadFailedHint}</p>
          <Button className="mt-3" onClick={() => periods.refetch()}>
            {cs.electricity.error.retry}
          </Button>
        </div>
      </div>
    )
  }

  // TRUE first run: no period at all. Routing to "create one" is the honest
  // answer — every other screen in the module is a projection of a period, so
  // there is genuinely nothing to show yet.
  if (!current) {
    return (
      <div>
        <ModuleHeader canWrite={false} onAddReading={() => {}} periodLine={null} />
        <div className="grid min-h-[40vh] place-items-center px-2">
          <div className="max-w-md rounded-2xl border border-dashed border-border p-8 text-center">
            <div className="mx-auto mb-4 grid h-12 w-12 place-items-center rounded-xl border border-dashed border-border-strong bg-s2 text-subtle">
              <Zap size={22} aria-hidden />
            </div>
            <h2 className="mb-1.5 text-lg font-bold">{cs.electricity.period.none}</h2>
            <p className="mb-4 text-[13.5px] text-muted text-pretty">
              {cs.electricity.period.noneHint}
            </p>
            <Button onClick={() => setPeriodOpen(true)} disabled={!canWrite}>
              <Plus size={16} aria-hidden />
              {cs.electricity.period.create}
            </Button>
          </div>
        </div>
        <PeriodDialog open={periodOpen} onOpenChange={setPeriodOpen} period={null} />
      </div>
    )
  }

  const periodLine = `${fmtDateISO(current.starts_on)} – ${fmtDateISO(current.ends_on)}`

  return (
    <div>
      <ModuleHeader
        canWrite={canWrite}
        onAddReading={() => openReading(undefined)}
        periodLine={periodLine}
        expectedEnd={!current.ends_on_confirmed}
      />

      <Routes>
        <Route index element={<PrehledTab periodId={current.id} onAddReading={openReading} />} />
        <Route path="odecty" element={<OdectyTab periodId={current.id} onAddReading={openReading} />} />
        <Route path="cenik" element={<CenikTab periodId={current.id} />} />
        <Route path="historie" element={<HistorieTab periods={items} currentId={current.id} />} />
        <Route path="*" element={<Navigate to={routes.elektrina} replace />} />
      </Routes>

      <ReadingDialog
        open={readingOpen}
        onOpenChange={setReadingOpen}
        reading={null}
        prefillDate={prefillDate}
      />
    </div>
  )
}

function ModuleHeader({
  canWrite,
  onAddReading,
  periodLine,
  expectedEnd,
}: {
  canWrite: boolean
  onAddReading: () => void
  periodLine: string | null
  expectedEnd?: boolean
}) {
  return (
    <header className="mb-4">
      <div className="flex flex-wrap items-start gap-3">
        <div className="min-w-[200px] flex-1">
          <h1 className="flex items-center gap-2 text-2xl font-extrabold tracking-tight">
            <Zap size={22} className="text-accent" aria-hidden />
            {cs.electricity.title}
          </h1>
          {periodLine ? (
            <p className="mt-1 flex flex-wrap items-center gap-2 text-sm text-muted">
              <span>{periodLine}</span>
              {expectedEnd && (
                // A NEUTRAL chip, deliberately not a warning colour: it sits next
                // to the largest figure on the screen and must not compete with it.
                <span className="inline-flex items-center rounded-md border border-border bg-s3 px-2 py-0.5 text-[11.5px] font-semibold text-muted">
                  {cs.electricity.word.expectedEnd}
                </span>
              )}
            </p>
          ) : (
            <p className="mt-1 text-sm text-muted text-pretty">{cs.electricity.lede}</p>
          )}
        </div>

        {/* The one action anyone arrives to perform. Disabled and VISIBLE for a
            reader — the established discipline, and this is the button a reader
            will most want to press. */}
        <Button onClick={onAddReading} disabled={!canWrite} className="min-h-11">
          <Plus size={16} aria-hidden />
          {cs.electricity.word.addReading}
        </Button>

        {!canWrite && (
          <span className="inline-flex items-center rounded-full border border-border bg-s3 px-3 py-1.5 text-[12px] font-semibold text-muted">
            {cs.common.readOnly}
          </span>
        )}
      </div>

      <nav
        aria-label={cs.electricity.title}
        className="-mx-4 mt-4 flex gap-1 overflow-x-auto px-4 md:mx-0 md:px-0"
      >
        {TABS.map((tab) => (
          <NavLink
            key={tab.path}
            to={tab.path ? `${routes.elektrina}/${tab.path}` : routes.elektrina}
            end={tab.end}
            className={({ isActive }) =>
              cn(
                'flex-none rounded-t-md border-b-2 px-3 py-2 text-[13.5px] font-semibold whitespace-nowrap',
                isActive ? 'border-accent text-fg' : 'border-transparent text-muted hover:text-fg',
              )
            }
          >
            {tab.label}
          </NavLink>
        ))}
      </nav>
    </header>
  )
}
