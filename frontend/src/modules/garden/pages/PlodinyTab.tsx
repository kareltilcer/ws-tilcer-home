import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { toast } from 'sonner'
import { Download, Plus, Search, Sparkles } from 'lucide-react'
import { qk } from '@/api/keys'
import { useAuth } from '@/app/auth'
import { useOnline } from '@/platform/pwa/offline'
import { cs } from '@/i18n/cs'
import { count, PLURAL } from '@/i18n/plural'
import { compareCz, todayISO } from '@/i18n/format'
import { ApiError } from '@/api/client'
import { Button, Input, Spinner } from '@/components/ui/ui'
import { exportKnowledgeBase, getEnums, listPlants, listSeasons } from '../api/endpoints'
import type { GardenEnums, GardenPlant } from '../api/types'
import { FamilyChip, UnverifiedBadge, describeWindow } from '../components/labels'
import { PlantForm } from '../components/PlantForm'
import { ImportDialog } from '../components/ImportDialog'
import { currentYear } from '../GardenPage'

// PLODINY — the knowledge base, and the escape hatch that makes it fillable.
//
// The crop editor is the largest form in Home by a wide margin: roughly forty
// fields across identity, agronomy, propagation, four timing windows, harvest,
// storage, pests and notes. Progressive disclosure makes it survivable
// (PlantForm), but the actual feature is the other path: generate a prompt,
// paste it into any model, paste the answer back, PREVIEW IT, apply.
//
// Everything a model writes is badged "Neověřeno" until a person confirms it
// (D114). The tone of that badge is the design problem, not its pixels: it is
// bookkeeping, not an accusation, because most of it will be fine and a scary
// badge on forty crops teaches people to ignore it.

export function PlodinyTab() {
  const { canWrite } = useAuth()
  const online = useOnline()
  const writable = canWrite && online

  const [q, setQ] = useState('')
  const [onlyUnverified, setOnlyUnverified] = useState(false)
  const [editing, setEditing] = useState<GardenPlant | null>(null)
  const [creating, setCreating] = useState(false)
  const [importOpen, setImportOpen] = useState(false)

  const plants = useQuery({
    queryKey: qk.gardenPlants({ q, onlyUnverified }),
    queryFn: () => listPlants({ q: q || undefined, unverified: onlyUnverified || undefined, limit: 200 }),
  })
  const enums = useQuery({ queryKey: qk.gardenEnums, queryFn: getEnums, staleTime: Infinity })
  const seasons = useQuery({ queryKey: qk.gardenSeasons, queryFn: listSeasons })

  // The crop form's live date echo resolves against a real season, so the
  // numbers you type mean a fortnight in March rather than "−56".
  const season = useMemo(() => {
    const items = seasons.data?.items ?? []
    return items.find((s) => s.year === currentYear()) ?? items[0]
  }, [seasons.data])

  const sorted = useMemo(
    () => [...(plants.data?.items ?? [])].sort((a, b) => compareCz(a.name_cs, b.name_cs)),
    [plants.data],
  )

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <label className="relative min-w-[200px] flex-1">
          <Search size={16} className="absolute top-1/2 left-3 -translate-y-1/2 text-subtle" aria-hidden />
          <Input
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder={cs.garden.searchPlants}
            className="pl-9"
          />
        </label>
        <label className="inline-flex items-center gap-1.5 text-[12.5px] text-muted">
          <input
            type="checkbox"
            checked={onlyUnverified}
            onChange={(e) => setOnlyUnverified(e.target.checked)}
          />
          {cs.garden.onlyUnverified}
        </label>
        <Button size="sm" variant="secondary" onClick={() => void downloadExport()}>
          <Download size={14} aria-hidden />
          {cs.garden.exportKb}
        </Button>
        <Button size="sm" variant="secondary" disabled={!writable} onClick={() => setImportOpen(true)}>
          <Sparkles size={14} aria-hidden />
          {cs.garden.promptTitle}
        </Button>
        <Button size="sm" variant="primary" disabled={!writable} onClick={() => setCreating(true)}>
          <Plus size={14} aria-hidden />
          {cs.garden.addPlant}
        </Button>
      </div>

      {plants.isPending ? (
        <div className="grid min-h-[200px] place-items-center">
          <Spinner />
        </div>
      ) : sorted.length === 0 ? (
        <div className="rounded-lg border border-dashed border-border p-8 text-center">
          <h2 className="mb-1.5 text-lg font-bold">{cs.garden.emptyPlantsTitle}</h2>
          <p className="mx-auto max-w-md text-sm text-muted text-pretty">{cs.garden.emptyPlantsBody}</p>
        </div>
      ) : (
        <>
          <p className="text-[12.5px] text-muted">{count(sorted.length, PLURAL.crops)}</p>
          <ul className="grid gap-2 sm:grid-cols-2 xl:grid-cols-3">
            {sorted.map((p) => (
              <li key={p.id}>
                <button
                  type="button"
                  onClick={() => setEditing(p)}
                  className="flex min-h-[44px] w-full flex-col rounded-lg border border-border bg-s1 p-3 text-left hover:bg-s2"
                >
                  <span className="flex w-full items-center gap-2">
                    <span className="min-w-0 flex-1 truncate text-[13.5px] font-semibold">{p.name_cs}</span>
                    {!p.provenance.verified_at && p.provenance.source === 'llm' && <UnverifiedBadge />}
                    <FamilyChip label={familyLabel(enums.data, p.family)} />
                  </span>
                  <span className="mt-1 truncate font-mono text-[11px] text-subtle">
                    {describeWindow(p.win_sow_direct ?? p.win_sow_indoor ?? p.win_transplant)}
                  </span>
                  {p.variety_count > 0 && (
                    <span className="mt-0.5 text-[11px] text-muted">
                      {count(p.variety_count, PLURAL.varieties)}
                    </span>
                  )}
                </button>
              </li>
            ))}
          </ul>
        </>
      )}

      <PlantForm
        open={creating || Boolean(editing)}
        plant={editing}
        enums={enums.data}
        season={season}
        canEdit={writable}
        onClose={() => {
          setCreating(false)
          setEditing(null)
        }}
      />

      <ImportDialog open={importOpen} onOpenChange={setImportOpen} />
    </div>
  )
}

function familyLabel(enums: GardenEnums | undefined, value: string): string {
  return enums?.family?.find((e) => e.value === value)?.label_cs ?? value
}

/** downloadExport writes the knowledge base to a file the household can keep.
 *
 *  It is the IMPORTER'S OWN SHAPE (D126), so the file re-imports — a readable
 *  backup and a way to move the knowledge base between installs, rather than an
 *  opaque dump. */
async function downloadExport(): Promise<void> {
  try {
    const data = await exportKnowledgeBase()
    const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `zahrada-plodiny-${todayISO()}.json`
    a.click()
    URL.revokeObjectURL(url)
  } catch (e) {
    toast.error(e instanceof ApiError ? (e.detail ?? cs.common.errorTitle) : cs.common.errorTitle)
  }
}

