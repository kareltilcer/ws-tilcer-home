import { useEffect, useMemo, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { CalendarPlus, Copy, Lock, Plus, Printer, Snowflake, Unlock } from 'lucide-react'
import { qk } from '@/api/keys'
import { routes } from '@/app/routes'
import { useAuth } from '@/app/auth'
import { useOnline } from '@/platform/pwa/offline'
import { cs } from '@/i18n/cs'
import { count, PLURAL } from '@/i18n/plural'
import { ApiError } from '@/api/client'
import { Button, Input, Spinner } from '@/components/ui/ui'
import { ResponsiveModal } from '@/components/ui/modal'

import {
  checkSeason,
  createPlanting,
  createSeason,
  getEnums,
  getSeason,
  listBeds,
  listPlantings,
  listSeasons,
  reopenSeason,
  updateSeason,
} from '../api/endpoints'
import type {
  GardenBed,
  GardenPlanting,
  GardenSeason,
  GardenSeasonPreview,
  GardenWarning,
} from '../api/types'
import { BedWarningBadge, CheckPanel } from '../components/CheckPanel'
import { CropPicker } from '../components/CropPicker'
import { OccupancyStrip } from '../components/OccupancyStrip'
import { PlantingDialog } from '../components/PlantingDialog'
import { SeasonCloseDialog } from '../components/SeasonCloseDialog'
import { toastGardenError } from '../api/hooks'
import { currentYear } from '../GardenPage'

// PLÁN — the planner, and the module's centre of gravity.
//
// At the scale this garden actually is (~15 beds), the right layout is ONE GRID
// OF BED CARDS: no two-pane workspace, no virtualisation, no pagination
// controls. Every bed is on screen, which is what makes "where does this go"
// answerable at a glance.
//
// Each card carries four things and no more: the bed, what is in it, HOW THE
// YEAR IS OCCUPIED, and whether anything about it is wrong. The occupancy strip
// is not decoration — it is the concept every warning turns on (D107), and a
// card that listed crops without it would hide why one pairing warns and another
// does not.

export function PlanTab() {
  const params = useParams<{ year: string }>()
  const navigate = useNavigate()
  const qc = useQueryClient()
  const { canWrite } = useAuth()
  const online = useOnline()

  const year = Number(params.year) || currentYear()
  const writable = canWrite && online

  const [picking, setPicking] = useState<GardenBed | null>(null)
  const [openPlanting, setOpenPlanting] = useState<string | null>(null)
  const [copyOpen, setCopyOpen] = useState(false)
  const [closeOpen, setCloseOpen] = useState(false)
  const [seasonOpen, setSeasonOpen] = useState(false)

  const season = useQuery({
    queryKey: qk.gardenSeason(year),
    queryFn: () => getSeason(year),
    retry: false,
  })
  const beds = useQuery({ queryKey: qk.gardenBeds(false), queryFn: () => listBeds(false) })
  const plantings = useQuery({
    queryKey: qk.gardenPlantings({ year }),
    queryFn: () => listPlantings({ year, limit: 200 }),
    enabled: season.isSuccess,
  })
  const check = useQuery({
    queryKey: qk.gardenCheck(year),
    queryFn: () => checkSeason(year),
    enabled: season.isSuccess,
  })
  const enums = useQuery({ queryKey: qk.gardenEnums, queryFn: getEnums, staleTime: Infinity })

  // A planting mutation invalidates the plan, its CHECK and the tasks TOGETHER.
  // A stale check is worse than no check, because it is a green tick that has
  // stopped being true.
  const invalidatePlan = () => {
    void qc.invalidateQueries({ queryKey: qk.gardenAll })
    void qc.invalidateQueries({ queryKey: qk.dashboard })
  }

  const addPlanting = useMutation({
    mutationFn: createPlanting,
    onSuccess: invalidatePlan,
    onError: toastGardenError,
  })

  const byBed = useMemo(() => {
    const map = new Map<string, GardenPlanting[]>()
    for (const p of plantings.data?.items ?? []) {
      if (!p.bed_id) continue
      const list = map.get(p.bed_id) ?? []
      list.push(p)
      map.set(p.bed_id, list)
    }
    return map
  }, [plantings.data])

  const warningsByBed = useMemo(() => {
    const map = new Map<string, GardenWarning[]>()
    for (const w of check.data?.warnings ?? []) {
      if (!w.bed_id) continue
      const list = map.get(w.bed_id) ?? []
      list.push(w)
      map.set(w.bed_id, list)
    }
    return map
  }, [check.data])

  // NO SEASON FOR THIS YEAR. Not an error — the season holds the frost dates
  // every window resolves against, so it is the first thing to make.
  if (season.isError) {
    const notFound = season.error instanceof ApiError && season.error.status === 404
    return (
      <div className="grid min-h-[280px] place-items-center text-center">
        <div className="max-w-md rounded-lg border border-dashed border-border p-8">
          <CalendarPlus size={24} className="mx-auto mb-3 text-subtle" aria-hidden />
          <h2 className="mb-1.5 text-lg font-bold">
            {notFound ? cs.garden.emptySeasonTitle : cs.garden.loadFailed}
          </h2>
          <p className="mb-4 text-sm text-muted text-pretty">
            {notFound ? cs.garden.emptySeasonBody : cs.garden.loadFailedBody}
          </p>
          {notFound && (
            <NewSeasonButton year={year} disabled={!writable} onDone={() => void season.refetch()} />
          )}
        </div>
      </div>
    )
  }

  if (season.isPending || beds.isPending) {
    return (
      <div className="grid min-h-[240px] place-items-center">
        <Spinner />
      </div>
    )
  }

  const closed = season.data?.status === 'closed'
  const canEdit = writable && !closed
  const bedList = beds.data?.items ?? []

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <YearSwitcher year={year} onPick={(y) => navigate(`${routes.zahrada}/plan/${y}`)} />
        <div className="flex-1" />
        {closed && (
          <span className="inline-flex items-center gap-1.5 rounded-md border border-border bg-s3 px-2.5 py-1.5 text-[12px] font-semibold text-muted">
            <Lock size={13} aria-hidden />
            {cs.garden.seasonClosed}
          </span>
        )}
        {/* THE SEASON'S OWN FROST DATES. Every frost-anchored window resolves
            against them, and a season created before the garden's defaults were
            filled in has none — so without this button that year's plan can
            never be anchored at all. It is also the only door to reopening a
            closed season. */}
        <Button size="sm" variant="secondary" onClick={() => setSeasonOpen(true)}>
          <Snowflake size={14} aria-hidden />
          {cs.garden.sezona}
        </Button>
        <Button size="sm" variant="secondary" onClick={() => window.print()}>
          <Printer size={14} aria-hidden />
          {cs.garden.print}
        </Button>
        <Button size="sm" variant="secondary" onClick={() => setCopyOpen(true)} disabled={!writable}>
          <Copy size={14} aria-hidden />
          {cs.garden.copySeason}
        </Button>
        {!closed && (
          <Button size="sm" variant="secondary" onClick={() => setCloseOpen(true)} disabled={!canEdit}>
            <Lock size={14} aria-hidden />
            {cs.garden.uzavritSezonu}
          </Button>
        )}
      </div>

      <div className="grid gap-4 xl:grid-cols-[1fr_380px]">
        {/* ONE GRID OF BED CARDS. Fifteen beds fit; nothing is virtualised. */}
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-2 2xl:grid-cols-3">
          {bedList.map((bed) => (
            <BedCard
              key={bed.id}
              bed={bed}
              year={year}
              plantings={byBed.get(bed.id) ?? []}
              warnings={warningsByBed.get(bed.id) ?? []}
              canEdit={canEdit}
              onAdd={() => setPicking(bed)}
              onOpenPlanting={setOpenPlanting}
            />
          ))}
          {bedList.length === 0 && (
            <p className="col-span-full rounded-lg border border-dashed border-border p-8 text-center text-sm text-muted">
              {cs.garden.emptyBedsBody}
            </p>
          )}
        </div>

        <div className="xl:sticky xl:top-4 xl:self-start">
          <CheckPanel year={year} check={check.data} canWrite={canEdit} />
        </div>
      </div>

      {/* PICK-THEN-PLACE: the card asks, the sheet answers. No drag. */}
      <CropPicker
        open={Boolean(picking)}
        onOpenChange={(open) => !open && setPicking(null)}
        enums={enums.data}
        onPick={(plant, variety) => {
          if (!picking) return
          addPlanting.mutate({
            season_year: year,
            bed_id: picking.id,
            plant_id: plant.id,
            variety_id: variety?.id,
            // A sensible default the planner can correct: one square metre is
            // the smallest useful patch, and asking for a number before the crop
            // is even placed would put a form between two taps.
            area_m2: 1,
          })
          setPicking(null)
        }}
      />

      <PlantingDialog
        plantingId={openPlanting}
        onClose={() => setOpenPlanting(null)}
        canEdit={canEdit}
      />

      <CopySeasonDialog
        open={copyOpen}
        onOpenChange={setCopyOpen}
        targetYear={year}
        disabled={!writable}
      />

      {season.data && (
        <SeasonCloseDialog
          open={closeOpen}
          onOpenChange={setCloseOpen}
          season={season.data}
          plantings={plantings.data?.items ?? []}
        />
      )}

      {season.data && (
        <SeasonDialog open={seasonOpen} onOpenChange={setSeasonOpen} season={season.data} />
      )}

      {/* The print target: the season plan on ONE page (D125). */}
      <PlanPrintSheet year={year} beds={bedList} byBed={byBed} />
    </div>
  )
}

function BedCard({
  bed,
  year,
  plantings,
  warnings,
  canEdit,
  onAdd,
  onOpenPlanting,
}: {
  bed: GardenBed
  year: number
  plantings: GardenPlanting[]
  warnings: GardenWarning[]
  canEdit: boolean
  onAdd: () => void
  onOpenPlanting: (id: string) => void
}) {
  const used = peakUsedArea(plantings)

  return (
    <article className="flex flex-col rounded-lg border border-border bg-s1 p-3">
      <header className="mb-2 flex items-start gap-2">
        <span className="flex-none rounded-md bg-s3 px-1.5 py-0.5 font-mono text-[13px] font-bold">
          {bed.code}
        </span>
        <span className="min-w-0 flex-1">
          <span className="block truncate text-[13.5px] font-semibold">{bed.name}</span>
          <span className="font-mono text-[11px] text-subtle">
            {used == null
              ? cs.garden.bedUsageUnknown(round1(bed.area_m2))
              : cs.garden.bedUsage(round1(used), round1(bed.area_m2))}
          </span>
        </span>
        <BedWarningBadge warnings={warnings} />
      </header>

      {plantings.length === 0 ? (
        <p className="py-3 text-center text-[12.5px] text-subtle">{cs.garden.bedFree}</p>
      ) : (
        <>
          <ul className="mb-1 space-y-1">
            {plantings.map((p) => (
              <li key={p.id}>
                <button
                  type="button"
                  onClick={() => onOpenPlanting(p.id)}
                  className="flex min-h-[36px] w-full items-center gap-2 rounded-md px-1.5 py-1 text-left hover:bg-s2"
                >
                  <span className="min-w-0 flex-1 truncate text-[13px]">
                    {p.plant_name}
                    {p.variety_name && <span className="text-muted"> · {p.variety_name}</span>}
                  </span>
                  {p.drift && (
                    <span className="flex-none font-mono text-[10.5px] text-warn" title={p.drift.message_cs}>
                      {p.drift.days > 0 ? `+${p.drift.days}` : p.drift.days} d
                    </span>
                  )}
                </button>
              </li>
            ))}
          </ul>
          {/* TIME PASSES THROUGH THE BED. Without this the card would be a list
              of names and the overlap rule would be invisible. */}
          <OccupancyStrip plantings={plantings} year={year} compact />
        </>
      )}

      <Button
        size="sm"
        variant="ghost"
        className="mt-2 w-full"
        onClick={onAdd}
        disabled={!canEdit}
        title={canEdit ? undefined : cs.common.readOnly}
      >
        <Plus size={14} aria-hidden />
        {cs.garden.addPlanting}
      </Button>
    </article>
  )
}

function YearSwitcher({ year, onPick }: { year: number; onPick: (year: number) => void }) {
  const seasons = useQuery({ queryKey: qk.gardenSeasons, queryFn: listSeasons })
  const years = (seasons.data?.items ?? []).map((s) => s.year)
  if (!years.includes(year)) years.unshift(year)
  return (
    <label className="inline-flex items-center gap-2">
      <span className="text-[12.5px] font-semibold text-muted">{cs.garden.sezona}</span>
      <select
        value={year}
        onChange={(e) => onPick(Number(e.target.value))}
        className="h-9 rounded-md border border-border bg-s2 px-2 text-sm font-semibold"
      >
        {[...new Set(years)]
          .sort((a, b) => b - a)
          .map((y) => (
            <option key={y} value={y}>
              {y}
            </option>
          ))}
      </select>
    </label>
  )
}

function NewSeasonButton({
  year,
  disabled,
  onDone,
}: {
  year: number
  disabled: boolean
  onDone: () => void
}) {
  const qc = useQueryClient()
  const create = useMutation({
    mutationFn: () => createSeason({ year }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.gardenAll })
      onDone()
    },
    onError: () => toast.error(cs.common.errorTitle),
  })
  return (
    <Button variant="primary" disabled={disabled || create.isPending} onClick={() => create.mutate()}>
      {cs.garden.emptySeasonCta}
    </Button>
  )
}

/** SeasonDialog edits the ONE thing a season owns: the frost dates every
 *  frost-anchored window resolves against.
 *
 *  It exists because "Založit sezónu" fills those dates from the garden's
 *  MM-DD defaults, and those defaults are empty until somebody opens Nastavení
 *  zahrady. A season created first therefore has none — and every `last_frost` /
 *  `first_frost` window stays unresolvable, no sowing or transplant work is
 *  generated, and the crop form says "Sezóna nemá zadané datum mrazu" with
 *  nothing to press. Editing the defaults afterwards only helps the NEXT season;
 *  this is the control that fixes the year you are looking at.
 *
 *  It also carries the reopen action, which is the module's only admin-gated
 *  one: a closed season is frozen by the server, and the close dialog promises
 *  "znovu ji může otevřít jen správce" — so that promise needs a button. */
function SeasonDialog({
  open,
  onOpenChange,
  season,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  season: GardenSeason
}) {
  const qc = useQueryClient()
  const { canWrite, isAdmin } = useAuth()
  const online = useOnline()
  const writable = canWrite && online
  const closed = season.status === 'closed'

  const [lastFrost, setLastFrost] = useState('')
  const [firstFrost, setFirstFrost] = useState('')

  // Re-seed whenever the dialog opens, and whenever it opens on a different
  // season — PlanTab is not remounted when only the :year param changes.
  useEffect(() => {
    if (!open) return
    setLastFrost(season.last_frost_on ?? '')
    setFirstFrost(season.first_frost_on ?? '')
  }, [open, season.year, season.last_frost_on, season.first_frost_on])

  const invalidate = () => {
    void qc.invalidateQueries({ queryKey: qk.gardenAll })
    void qc.invalidateQueries({ queryKey: qk.dashboard })
  }

  const save = useMutation({
    // An EMPTIED box is omitted rather than sent. PATCH /seasons/{year} reads a
    // null and an absent field the same way — as "no change" — so there is no
    // way to clear a frost date here, and pretending otherwise would leave the
    // old value on screen after a save that looked like it worked. Setting one
    // is what this dialog is for; unsetting one is not a thing anybody needs.
    mutationFn: () =>
      updateSeason(season.year, {
        last_frost_on: lastFrost || undefined,
        first_frost_on: firstFrost || undefined,
      }),
    onSuccess: () => {
      invalidate()
      onOpenChange(false)
    },
    onError: toastGardenError,
  })

  const reopen = useMutation({
    mutationFn: () => reopenSeason(season.year),
    onSuccess: () => {
      invalidate()
      onOpenChange(false)
    },
    onError: toastGardenError,
  })

  const missing = !season.last_frost_on || !season.first_frost_on

  return (
    <ResponsiveModal open={open} onOpenChange={onOpenChange} title={`${cs.garden.sezona} ${season.year}`}>
      <div className="space-y-3">
        <p className="text-[13px] text-muted text-pretty">{cs.garden.emptySeasonBody}</p>

        {/* Said plainly, because this is the state that makes half the module
            look broken for no visible reason. */}
        {missing && (
          <p className="rounded-md border border-warn/45 bg-warn/10 px-3 py-2 text-[12.5px] text-pretty">
            {cs.garden.windowNoAnchor}
          </p>
        )}

        <div className="flex flex-wrap gap-3">
          <label className="min-w-[150px] flex-1">
            <span className="mb-1 block text-[12.5px] font-semibold">{cs.garden.posledniMraz}</span>
            <Input
              type="date"
              value={lastFrost}
              disabled={!writable || closed}
              onChange={(e) => setLastFrost(e.target.value)}
            />
          </label>
          <label className="min-w-[150px] flex-1">
            <span className="mb-1 block text-[12.5px] font-semibold">{cs.garden.prvniMraz}</span>
            <Input
              type="date"
              value={firstFrost}
              disabled={!writable || closed}
              onChange={(e) => setFirstFrost(e.target.value)}
            />
          </label>
        </div>

        {closed ? (
          <div className="space-y-2 rounded-md border border-border bg-s2 px-3 py-2.5">
            <p className="text-[12.5px] text-muted text-pretty">{cs.garden.reopenWarning}</p>
            <Button
              size="sm"
              variant="secondary"
              disabled={!isAdmin || !online || reopen.isPending}
              title={isAdmin ? undefined : cs.common.readOnly}
              onClick={() => reopen.mutate()}
            >
              <Unlock size={14} aria-hidden />
              {cs.garden.reopenSeason}
            </Button>
          </div>
        ) : (
          <p className="text-[11.5px] text-subtle text-pretty">
            Změna data mrazu přepočítá termíny, které jste nezadali ručně, a s nimi i navazující práce.
          </p>
        )}

        <div className="flex justify-end gap-2 pt-1">
          <Button variant="secondary" onClick={() => onOpenChange(false)}>
            {cs.finance.cancel}
          </Button>
          <Button
            variant="primary"
            disabled={!writable || closed || save.isPending}
            onClick={() => save.mutate()}
          >
            {cs.finance.save}
          </Button>
        </div>
      </div>
    </ResponsiveModal>
  )
}

/** CopySeasonDialog is the dry-run preview (D129): it shows the prospective plan
 *  AND ITS CHECK, beside the source season's, before the season exists.
 *
 *  That side-by-side is the whole point. A copy that silently reproduces last
 *  year's rotation error is worse than typing the year in by hand. */
function CopySeasonDialog({
  open,
  onOpenChange,
  targetYear,
  disabled,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  targetYear: number
  disabled: boolean
}) {
  const qc = useQueryClient()
  const navigate = useNavigate()
  const [from, setFrom] = useState(String(targetYear - 1))
  const [offset, setOffset] = useState('1')
  const [preview, setPreview] = useState<GardenSeasonPreview | null>(null)

  // PlanTab is NOT remounted when only the :year param changes, so a source year
  // seeded once at mount goes stale the moment somebody uses the year switcher —
  // and the copy would then silently pull the wrong season's plan. Re-seed it
  // whenever the dialog opens against a different target, and drop any preview
  // that was computed for the old one.
  useEffect(() => {
    if (!open) return
    setFrom(String(targetYear - 1))
    setPreview(null)
  }, [open, targetYear])

  const body = () => ({
    year: targetYear,
    copy_from: Number(from),
    shift: Number(offset) ? { offset: Number(offset), by_family: true } : undefined,
  })

  const dryRun = useMutation({
    mutationFn: () => createSeason(body(), true) as Promise<GardenSeasonPreview>,
    onSuccess: setPreview,
    onError: toastGardenError,
  })

  const apply = useMutation({
    mutationFn: () => createSeason(body(), false),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.gardenAll })
      onOpenChange(false)
      setPreview(null)
      navigate(`${routes.zahrada}/plan/${targetYear}`)
    },
    onError: toastGardenError,
  })

  const before = preview?.check_before
  const after = preview?.check

  return (
    <ResponsiveModal open={open} onOpenChange={onOpenChange} title={cs.garden.copySeason}>
      <div className="space-y-3">
        <div className="flex flex-wrap gap-3">
          <label className="w-32">
            <span className="mb-1 block text-[12.5px] font-semibold">{cs.garden.sezona}</span>
            <Input type="number" value={from} onChange={(e) => setFrom(e.target.value)} />
          </label>
          <label className="w-40">
            <span className="mb-1 block text-[12.5px] font-semibold">{cs.garden.copyShift}</span>
            <Input type="number" value={offset} onChange={(e) => setOffset(e.target.value)} />
          </label>
        </div>

        <Button variant="secondary" onClick={() => dryRun.mutate()} disabled={disabled || dryRun.isPending}>
          {cs.garden.copyPreview}
        </Button>

        {preview && (
          <>
            <p className="text-[12.5px] text-muted">
              {count(preview.plantings.length, PLURAL.plantings)} · nic zatím není uloženo
            </p>
            <div className="grid grid-cols-2 gap-2">
              <CheckSummary title={cs.garden.copyBefore} counts={before?.counts} />
              <CheckSummary title={cs.garden.copyAfter} counts={after?.counts} />
            </div>
            <div className="flex justify-end gap-2 pt-1">
              <Button variant="secondary" onClick={() => setPreview(null)}>
                {cs.finance.cancel}
              </Button>
              <Button variant="primary" onClick={() => apply.mutate()} disabled={disabled || apply.isPending}>
                {cs.finance.save}
              </Button>
            </div>
          </>
        )}
      </div>
    </ResponsiveModal>
  )
}

function CheckSummary({
  title,
  counts,
}: {
  title: string
  counts: { error: number; warn: number; info: number; tip: number } | undefined
}) {
  const total = (counts?.error ?? 0) + (counts?.warn ?? 0)
  return (
    <div className="rounded-md border border-border bg-s2 px-3 py-2">
      <div className="text-[11.5px] font-semibold text-muted">{title}</div>
      <div className="mt-0.5 font-mono text-lg font-semibold">{total}</div>
      <div className="text-[11px] text-subtle">{count(total, PLURAL.warnings)}</div>
    </div>
  )
}

/** The season plan, printed on ONE page (D125): black on white, one row per bed,
 *  no app iconography. */
function PlanPrintSheet({
  year,
  beds,
  byBed,
}: {
  year: number
  beds: GardenBed[]
  byBed: Map<string, GardenPlanting[]>
}) {
  return (
    <div className="garden-print">
      <h1>
        {cs.garden.printPlan} {year}
      </h1>
      <p className="garden-print-sub">{cs.garden.title}</p>
      <table className="garden-print-plan">
        <thead>
          <tr>
            <th style={{ width: '18mm' }}>{cs.garden.zahon}</th>
            <th>{cs.garden.vysadba}</th>
            <th style={{ width: '34mm' }}>{cs.garden.terminVysevu}</th>
          </tr>
        </thead>
        <tbody>
          {beds.map((bed) => {
            const list = byBed.get(bed.id) ?? []
            return (
              <tr key={bed.id}>
                <td style={{ fontWeight: 700 }}>{bed.code}</td>
                <td>{list.length ? list.map((p) => p.plant_name).join(', ') : '—'}</td>
                <td>
                  {list
                    .map((p) => p.sow_direct_on ?? p.transplant_on ?? p.sow_indoor_on)
                    .filter(Boolean)
                    .join(', ') || '—'}
                </td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}

/** peakUsedArea is the most area the bed holds AT ONE TIME, which is the same
 *  question check C4 answers — so the card and the warning panel cannot show a
 *  bed as full while C4 calls it fine, or the reverse.
 *
 *  Two rules, both mirroring the server: the year is sampled at the instants
 *  plantings take the bed rather than summed over the whole season (three
 *  successive 2 m² crops use 2 m², not 6), and a bed is measured only when every
 *  planting in it states an area. A planting sized by plant_count needs the
 *  crop's density to convert, which this screen does not have — so it returns
 *  null and the caller shows the bed's size alone rather than a total that
 *  silently counts those crops as zero. */
function peakUsedArea(plantings: GardenPlanting[]): number | null {
  if (plantings.length === 0) return 0
  if (plantings.some((p) => p.area_m2 == null)) return null

  const at = (p: GardenPlanting, day: number | null): boolean => {
    const from = p.occupancy.from ? Date.parse(`${p.occupancy.from}T00:00:00Z`) : null
    const to = p.occupancy.to ? Date.parse(`${p.occupancy.to}T00:00:00Z`) : null
    // An undated planting is open-ended at both ends, so it counts everywhere —
    // the same conservative direction the server's overlap rule takes.
    if (day === null) return from === null
    if (from !== null && day < from) return false
    // Half-open at the end: cleared on the day the next is sown is a succession.
    if (to !== null && day >= to) return false
    return true
  }

  const instants: (number | null)[] = []
  let undated = false
  for (const p of plantings) {
    if (!p.occupancy.from) {
      undated = true
      continue
    }
    const day = Date.parse(`${p.occupancy.from}T00:00:00Z`)
    if (!Number.isNaN(day) && !instants.includes(day)) instants.push(day)
  }
  if (undated) instants.push(null)

  let peak = 0
  for (const day of instants) {
    let sum = 0
    for (const p of plantings) if (at(p, day)) sum += p.area_m2 ?? 0
    if (sum > peak) peak = sum
  }
  return peak
}

function round1(v: number): number {
  return Math.round(v * 10) / 10
}
