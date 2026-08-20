import { useEffect, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Search } from 'lucide-react'
import { qk } from '@/api/keys'
import { cs } from '@/i18n/cs'
import { Input, Spinner } from '@/components/ui/ui'
import { ResponsiveModal } from '@/components/ui/modal'
import { compareCz } from '@/i18n/format'
import { listPlants, listVarieties } from '../api/endpoints'
import type { GardenEnums, GardenPlant, GardenVariety } from '../api/types'
import { FamilyChip } from './labels'

// PUTTING A CROP IN A BED — the interaction that matters, and it must work with
// a thumb.
//
// Drag-and-drop is desktop thinking and fails outdoors: one hand, gloves off,
// sunlight, a phone. So the primary path is PICK-THEN-PLACE — the bed card asks,
// this sheet answers. Drag is not offered at all rather than offered badly.
//
// The picker is used forty times in a planning session, so it carries search,
// recently-used, and — the part that is easy to leave out — THE CROP'S FAMILY AT
// PICK TIME. Family is what the warnings are about, and seeing it here is what
// lets somebody avoid a warning instead of dismissing one.

const RECENT_KEY = 'garden.recentCrops'
const RECENT_MAX = 6

function readRecent(): string[] {
  try {
    const raw = localStorage.getItem(RECENT_KEY)
    return raw ? (JSON.parse(raw) as string[]) : []
  } catch {
    return []
  }
}

/** rememberCrop records a pick for the recently-used row. It is a per-device
 *  convenience, not shared state — which is why it lives in localStorage and not
 *  on the server. */
export function rememberCrop(plantId: string): void {
  try {
    const next = [plantId, ...readRecent().filter((id) => id !== plantId)].slice(0, RECENT_MAX)
    localStorage.setItem(RECENT_KEY, JSON.stringify(next))
  } catch {
    // A device that refuses localStorage simply has no recents.
  }
}

export function CropPicker({
  open,
  onOpenChange,
  enums,
  onPick,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  enums: GardenEnums | undefined
  onPick: (plant: GardenPlant, variety: GardenVariety | null) => void
}) {
  const [q, setQ] = useState('')
  const [chosen, setChosen] = useState<GardenPlant | null>(null)

  useEffect(() => {
    if (!open) {
      setQ('')
      setChosen(null)
    }
  }, [open])

  const plants = useQuery({
    queryKey: qk.gardenPlants({ q }),
    queryFn: () => listPlants({ q: q || undefined, limit: 200 }),
    enabled: open,
  })

  // The variety step appears only when the chosen crop HAS varieties — a second
  // screen for a crop with none would be a tap that asks nothing.
  const varieties = useQuery({
    queryKey: qk.gardenVarieties(chosen?.id ?? ''),
    queryFn: () => listVarieties(chosen!.id),
    enabled: Boolean(chosen && chosen.variety_count > 0),
  })

  const recent = useMemo(() => {
    if (q) return []
    const ids = readRecent()
    const byId = new Map((plants.data?.items ?? []).map((p) => [p.id, p]))
    return ids.map((id) => byId.get(id)).filter((p): p is GardenPlant => Boolean(p))
  }, [q, plants.data])

  const sorted = useMemo(
    () => [...(plants.data?.items ?? [])].sort((a, b) => compareCz(a.name_cs, b.name_cs)),
    [plants.data],
  )

  const familyLabel = (value: string) =>
    enums?.family?.find((e) => e.value === value)?.label_cs ?? value

  const pick = (plant: GardenPlant, variety: GardenVariety | null) => {
    rememberCrop(plant.id)
    onPick(plant, variety)
    onOpenChange(false)
  }

  return (
    <ResponsiveModal
      open={open}
      onOpenChange={onOpenChange}
      title={chosen ? `${chosen.name_cs} — ${cs.garden.odruda}` : cs.garden.pickCrop}
    >
      {chosen && chosen.variety_count > 0 ? (
        <div className="space-y-2">
          <button
            type="button"
            onClick={() => pick(chosen, null)}
            className="w-full rounded-md border border-border bg-s2 px-3 py-3 text-left hover:bg-s3"
          >
            <div className="text-sm font-semibold">{chosen.name_cs}</div>
            <div className="text-[12px] text-muted">bez určené odrůdy</div>
          </button>
          {varieties.isPending ? (
            <div className="grid place-items-center py-6">
              <Spinner />
            </div>
          ) : (
            (varieties.data?.items ?? [])
              .filter((v) => !v.retired)
              .map((v) => (
                <button
                  key={v.id}
                  type="button"
                  onClick={() => pick(chosen, v)}
                  className="w-full rounded-md border border-border bg-s2 px-3 py-3 text-left hover:bg-s3"
                >
                  <div className="text-sm font-semibold">{v.name}</div>
                  {v.supplier && <div className="text-[12px] text-muted">{v.supplier}</div>}
                </button>
              ))
          )}
        </div>
      ) : (
        <div className="space-y-3">
          <label className="relative block">
            <Search size={16} className="absolute top-1/2 left-3 -translate-y-1/2 text-subtle" aria-hidden />
            <Input
              value={q}
              onChange={(e) => setQ(e.target.value)}
              placeholder={cs.garden.pickCropSearch}
              className="pl-9"
              autoFocus
            />
          </label>

          {recent.length > 0 && (
            <section>
              <h4 className="mb-1.5 text-[11px] font-bold tracking-wide text-subtle uppercase">
                {cs.garden.recentlyUsed}
              </h4>
              <div className="flex flex-wrap gap-1.5">
                {recent.map((p) => (
                  <button
                    key={p.id}
                    type="button"
                    onClick={() => (p.variety_count > 0 ? setChosen(p) : pick(p, null))}
                    className="rounded-md border border-border bg-s2 px-2.5 py-1.5 text-[13px] font-semibold hover:bg-s3"
                  >
                    {p.name_cs}
                  </button>
                ))}
              </div>
            </section>
          )}

          {plants.isPending ? (
            <div className="grid place-items-center py-8">
              <Spinner />
            </div>
          ) : sorted.length === 0 ? (
            <p className="py-6 text-center text-[13px] text-muted">{cs.garden.emptyPlantsTitle}</p>
          ) : (
            <ul className="space-y-1.5">
              {sorted.map((p) => (
                <li key={p.id}>
                  <button
                    type="button"
                    onClick={() => (p.variety_count > 0 ? setChosen(p) : pick(p, null))}
                    // ≥44 px target: this is tapped forty times in a session,
                    // outdoors, one-handed.
                    className="flex min-h-[44px] w-full items-center gap-2 rounded-md border border-border bg-s2 px-3 py-2 text-left hover:bg-s3"
                  >
                    <span className="min-w-0 flex-1 truncate text-sm font-semibold">{p.name_cs}</span>
                    {/* Family AT PICK TIME — the warnings are about this. */}
                    <FamilyChip label={familyLabel(p.family)} />
                    {p.variety_count > 0 && (
                      <span className="flex-none font-mono text-[11px] text-subtle">
                        {p.variety_count}×
                      </span>
                    )}
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
    </ResponsiveModal>
  )
}
