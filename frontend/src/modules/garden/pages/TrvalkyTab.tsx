import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { TreeDeciduous } from 'lucide-react'
import { qk } from '@/api/keys'
import { useAuth } from '@/app/auth'
import { useOnline } from '@/platform/pwa/offline'
import { cs } from '@/i18n/cs'
import { todayISO } from '@/i18n/format'
import { Spinner } from '@/components/ui/ui'
import { listPlantings, listTasks } from '../api/endpoints'
import { PlantingDialog } from '../components/PlantingDialog'
import { fmtDayYear, fmtWindow } from '../components/labels'

// TRVALKY A DŘEVINY — a FILTERED VIEW, not a second entity.
//
// A permanent is a planting with no season (D106). That one decision is why this
// page needs no model of its own: occupancy, warnings, tasks and harvests all
// take the same code path as an annual, and Trvalky is `?permanent=true`.
//
// What differs is only what is worth showing. A tree has no sowing date and no
// season to close; it has a planting date, a rootstock, and yearly care — so
// those are what the card carries, and the season columns simply are not here.

export function TrvalkyTab() {
  const { canWrite } = useAuth()
  const online = useOnline()
  const [open, setOpen] = useState<string | null>(null)

  const plantings = useQuery({
    queryKey: qk.gardenPlantings({ permanent: true }),
    queryFn: () => listPlantings({ permanent: true, limit: 200 }),
  })

  // LOCAL, like every other date bound the module sends the server.
  const today = todayISO()
  const horizon = todayISO(90)
  const tasks = useQuery({
    queryKey: qk.gardenTasks({ permanentCare: true, from: today, to: horizon }),
    queryFn: () => listTasks({ from: today, to: horizon, status: 'open', limit: 200 }),
  })

  const items = plantings.data?.items ?? []
  const careByPlanting = new Map<string, typeof tasks.data extends undefined ? never : NonNullable<typeof tasks.data>['items']>()
  for (const t of tasks.data?.items ?? []) {
    if (!t.planting_id) continue
    careByPlanting.set(t.planting_id, [...(careByPlanting.get(t.planting_id) ?? []), t])
  }

  return (
    <div className="space-y-4">
      <h2 className="text-base font-bold">{cs.garden.tabs.trvalky}</h2>

      {plantings.isPending ? (
        <div className="grid min-h-[200px] place-items-center">
          <Spinner />
        </div>
      ) : items.length === 0 ? (
        <div className="rounded-lg border border-dashed border-border p-8 text-center">
          <TreeDeciduous size={24} className="mx-auto mb-3 text-subtle" aria-hidden />
          <h3 className="mb-1.5 text-lg font-bold">Zatím žádné trvalky ani dřeviny</h3>
          <p className="mx-auto max-w-md text-sm text-muted text-pretty">
            Trvalka je výsadba bez sezóny — strom, rebarbora, chřest. Přidáte ji z plánu tak, že u výsadby
            necháte sezónu prázdnou.
          </p>
        </div>
      ) : (
        <ul className="grid gap-2 sm:grid-cols-2 xl:grid-cols-3">
          {items.map((p) => {
            const care = careByPlanting.get(p.id) ?? []
            return (
              <li key={p.id}>
                <button
                  type="button"
                  onClick={() => setOpen(p.id)}
                  className="flex w-full flex-col rounded-lg border border-border bg-s1 p-3 text-left hover:bg-s2"
                >
                  <span className="flex w-full items-center gap-2">
                    <span className="min-w-0 flex-1 truncate text-[13.5px] font-semibold">
                      {p.plant_name}
                      {p.variety_name && <span className="text-muted"> · {p.variety_name}</span>}
                    </span>
                    {p.bed_code && (
                      <span className="flex-none font-mono text-[11px] text-subtle">{p.bed_code}</span>
                    )}
                  </span>

                  <span className="mt-1 font-mono text-[11px] text-muted">
                    {p.planted_on ? `vysazeno ${fmtDayYear(p.planted_on)}` : 'datum výsadby neznámé'}
                    {p.rootstock ? ` · podnož ${p.rootstock}` : ''}
                  </span>
                  {p.location_label && (
                    <span className="mt-0.5 text-[11.5px] text-subtle">{p.location_label}</span>
                  )}

                  {/* Yearly care rather than a sowing cycle: prune, spray, and
                      whatever else the crop's own windows produce. */}
                  {care.length > 0 && (
                    <span className="mt-2 block w-full space-y-0.5">
                      {care.slice(0, 3).map((t) => (
                        <span key={t.id} className="flex items-center gap-2 text-[11.5px]">
                          <span className="min-w-0 flex-1 truncate">{t.title_cs}</span>
                          <span className="flex-none font-mono text-[10.5px] text-subtle">
                            {fmtWindow(t.window_from, t.window_to)}
                          </span>
                        </span>
                      ))}
                    </span>
                  )}
                </button>
              </li>
            )
          })}
        </ul>
      )}

      <PlantingDialog plantingId={open} onClose={() => setOpen(null)} canEdit={canWrite && online} />
    </div>
  )
}
