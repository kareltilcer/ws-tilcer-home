import { useEffect, useMemo, useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { Plus, Trash2 } from 'lucide-react'
import { qk } from '@/api/keys'
import { useAuth } from '@/app/auth'
import { useOnline } from '@/platform/pwa/offline'
import { cs } from '@/i18n/cs'
import { todayISO } from '@/i18n/format'
import { Button, Input, Spinner } from '@/components/ui/ui'
import { ResponsiveModal } from '@/components/ui/modal'
import { createHarvest, deleteHarvest, listHarvests, listPlantings } from '../api/endpoints'
import { toastGardenError, useInvalidateGarden } from '../api/hooks'
import type { GardenPlanting, GardenSeason } from '../api/types'
import { fmtDayYear } from '../components/labels'
import { currentYear } from '../GardenPage'

// SKLIZEŇ — an entry-heavy screen used with dirty hands.
//
// The quick-entry path is the design: FROM THE PLANTING, two taps and a number.
// The unit is pre-filled from the crop, so the form never asks the one question
// whose answer it already knows — and the harvest date defaults to today,
// because that is when you are standing there with a basket.
//
// The payoff of recording anything at all is the YIELD-VERSUS-EXPECTED
// comparison, which is why the planting list below shows both numbers rather
// than only the total.

export function SklizenTab({ season }: { season: GardenSeason | undefined }) {
  const { canWrite } = useAuth()
  const online = useOnline()
  const writable = canWrite && online
  const year = season?.year ?? currentYear()

  const [quickFor, setQuickFor] = useState<GardenPlanting | null>(null)
  const invalidate = useInvalidateGarden()

  const plantings = useQuery({
    queryKey: qk.gardenPlantings({ year }),
    queryFn: () => listPlantings({ year, limit: 200 }),
  })
  // A permanent has no season (D106), so the year query cannot see one — and a
  // fruit tree would then be named "ke sklizni" by garden.harvest_ready with no
  // row here to record the picking against. Its harvests count toward the
  // calendar year, exactly as the season total sums them.
  const permanents = useQuery({
    queryKey: qk.gardenPlantings({ permanent: true }),
    queryFn: () => listPlantings({ permanent: true, limit: 200 }),
  })
  const harvests = useQuery({
    queryKey: qk.gardenHarvests({ year }),
    queryFn: () => listHarvests({ year }),
  })

  const remove = useMutation({
    mutationFn: (id: string) => deleteHarvest(id),
    onSuccess: invalidate,
    onError: toastGardenError,
  })

  const totalKg = useMemo(
    () => (harvests.data?.items ?? []).filter((h) => h.unit === 'kg').reduce((s, h) => s + h.quantity, 0),
    [harvests.data],
  )

  // Seasonal crops first, then the permanents — one list, because "co jsme
  // letos sklidili" does not distinguish a bed from a tree.
  const rows = useMemo(
    () => [...(plantings.data?.items ?? []), ...(permanents.data?.items ?? [])],
    [plantings.data, permanents.data],
  )

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <h2 className="flex-1 text-base font-bold">
          {cs.garden.harvestTitle} {year}
        </h2>
        <span className="font-mono text-[13px] font-semibold">
          {totalKg.toFixed(1).replace('.', ',')} kg
        </span>
      </div>
      {/* Said out loud rather than quietly assumed: mixed units cannot be added,
          so the total is kilograms only. */}
      <p className="text-[11.5px] text-subtle">Součet je jen z kilogramů — kusy, litry a svazky se nesčítají.</p>

      <div className="grid gap-4 lg:grid-cols-2">
        <section className="rounded-lg border border-border bg-s1">
          <header className="border-b border-border px-4 py-2.5 text-[12px] font-bold tracking-wide text-subtle uppercase">
            {cs.garden.vysadba}
          </header>
          {plantings.isPending || permanents.isPending ? (
            <div className="grid min-h-[120px] place-items-center">
              <Spinner />
            </div>
          ) : (
            <ul className="divide-y divide-border">
              {rows.map((p) => (
                <li key={p.id} className="flex flex-wrap items-center gap-2 px-4 py-2.5">
                  <span className="min-w-0 flex-1">
                    <span className="block truncate text-[13.5px] font-semibold">
                      {p.plant_name}
                      {p.bed_code && <span className="ml-1.5 font-mono text-[11px] text-muted">{p.bed_code}</span>}
                    </span>
                    {/* THE CROP'S OWN UNIT, always. Both numbers are stated in
                        it and it is rarely kilograms — printed bare under a
                        header that reads "… kg", 120 kusů cibule reads as 120
                        kilograms. */}
                    <span className="font-mono text-[11px] text-muted">
                      {p.yield_actual != null ? p.yield_actual : 0} {p.harvest_unit ?? ''}
                      {p.yield_expected != null && (
                        <span className="text-subtle">
                          {' '}
                          / {p.yield_expected} {p.harvest_unit ?? ''} očekáváno
                        </span>
                      )}
                    </span>
                  </span>
                  <Button
                    size="sm"
                    variant="secondary"
                    disabled={!writable}
                    onClick={() => setQuickFor(p)}
                  >
                    <Plus size={14} aria-hidden />
                    {cs.garden.quickHarvest}
                  </Button>
                </li>
              ))}
              {rows.length === 0 && (
                <li className="px-4 py-6 text-center text-[13px] text-muted">
                  V téhle sezóně zatím není žádná výsadba.
                </li>
              )}
            </ul>
          )}
        </section>

        <section className="rounded-lg border border-border bg-s1">
          <header className="border-b border-border px-4 py-2.5 text-[12px] font-bold tracking-wide text-subtle uppercase">
            {cs.garden.harvestTitle}
          </header>
          {harvests.isPending ? (
            <div className="grid min-h-[120px] place-items-center">
              <Spinner />
            </div>
          ) : (
            <ul className="divide-y divide-border">
              {(harvests.data?.items ?? []).map((h) => (
                <li key={h.id} className="flex items-center gap-2 px-4 py-2.5">
                  <span className="min-w-0 flex-1">
                    <span className="block truncate text-[13.5px] font-semibold">{h.plant_name}</span>
                    <span className="font-mono text-[11px] text-muted">
                      {fmtDayYear(h.harvested_on)}
                      {h.bed_code ? ` · ${h.bed_code}` : ''}
                    </span>
                  </span>
                  <span className="flex-none font-mono text-[13px] font-semibold">
                    {h.quantity} {h.unit}
                  </span>
                  <Button size="sm" variant="ghost" disabled={!writable} onClick={() => remove.mutate(h.id)}>
                    <Trash2 size={13} aria-hidden />
                  </Button>
                </li>
              ))}
              {(harvests.data?.items ?? []).length === 0 && (
                <li className="px-4 py-6 text-center text-[13px] text-muted">Letos zatím nic sklizeného.</li>
              )}
            </ul>
          )}
        </section>
      </div>

      <QuickHarvestDialog planting={quickFor} onClose={() => setQuickFor(null)} />
    </div>
  )
}

/** Two taps and a number. The unit comes from the crop and the date from today,
 *  so the only field that needs a human is the one only a human knows. */
function QuickHarvestDialog({
  planting,
  onClose,
}: {
  planting: GardenPlanting | null
  onClose: () => void
}) {
  const invalidate = useInvalidateGarden()
  const [quantity, setQuantity] = useState('')
  const [when, setWhen] = useState(todayISO)

  // The dialog is mounted for the life of the tab, so BOTH fields have to be
  // re-derived when a planting is picked — not just on save. Without this, a
  // date changed to record yesterday's picking silently carries into the next
  // crop's entry, and a tab left open overnight keeps offering the previous day.
  useEffect(() => {
    if (planting) {
      setQuantity('')
      setWhen(todayISO())
    }
  }, [planting])

  const save = useMutation({
    mutationFn: () =>
      createHarvest({
        planting_id: planting!.id,
        quantity: Number(quantity.replace(',', '.')),
        harvested_on: when,
      }),
    onSuccess: () => {
      invalidate()
      setQuantity('')
      onClose()
    },
    onError: toastGardenError,
  })

  return (
    <ResponsiveModal
      open={Boolean(planting)}
      onOpenChange={(o) => !o && onClose()}
      title={`${cs.garden.addHarvest} · ${planting?.plant_name ?? ''}`}
    >
      <div className="space-y-3">
        <label className="block">
          <span className="mb-1 block text-[12.5px] font-semibold">{cs.garden.quantity}</span>
          {/* A big numeric field: this is typed outdoors, one-handed. */}
          <Input
            type="number"
            inputMode="decimal"
            step="0.1"
            value={quantity}
            onChange={(e) => setQuantity(e.target.value)}
            className="h-14 text-xl"
            autoFocus
          />
        </label>
        <label className="block">
          <span className="mb-1 block text-[12.5px] font-semibold">{cs.garden.harvestedOn}</span>
          <Input type="date" value={when} onChange={(e) => setWhen(e.target.value)} />
        </label>
        <div className="flex justify-end gap-2">
          <Button variant="secondary" onClick={onClose}>
            {cs.finance.cancel}
          </Button>
          <Button
            variant="primary"
            disabled={!quantity.trim() || save.isPending}
            onClick={() => save.mutate()}
          >
            {cs.finance.save}
          </Button>
        </div>
      </div>
    </ResponsiveModal>
  )
}
