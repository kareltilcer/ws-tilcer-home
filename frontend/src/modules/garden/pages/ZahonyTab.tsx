import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { ChevronDown, ChevronUp, History, Info, Plus, Trash2 } from 'lucide-react'
import { qk } from '@/api/keys'
import { useAuth } from '@/app/auth'
import { useOnline } from '@/platform/pwa/offline'
import { cs } from '@/i18n/cs'
import { ApiError } from '@/api/client'
import { Button, Input, Spinner } from '@/components/ui/ui'
import { ResponsiveModal } from '@/components/ui/modal'
import { bedHistory, createBed, deleteBed, getEnums, listBeds, moveBed, updateBed } from '../api/endpoints'
import type { GardenBed, GardenBedInput } from '../api/types'

// ZÁHONY — WHERE THE ORDER MEANS SOMETHING.
//
// Everywhere else in Home a reorder control is cosmetic. Here it is DATA:
// adjacency is inferred from the order within a zone (D117), and check C11 reads
// exactly that. Somebody who reorders for tidiness would silently change their
// warnings — so the interaction has to teach that at the moment of the move, not
// in a help page nobody opens.
//
// Three things do that work: the explanatory panel above the list (once, at the
// top, where the decision is made), the NEIGHBOUR NAMES on every card, and the
// fact that the move controls are labelled "posunout" rather than being a
// grab-handle with no words. Explicit up/down buttons rather than pointer drag
// is also the accessible and thumb-friendly choice — this list is short, and the
// keyboard path is the same path.

export function ZahonyTab() {
  const qc = useQueryClient()
  const { canWrite } = useAuth()
  const online = useOnline()
  const writable = canWrite && online

  const [editing, setEditing] = useState<GardenBed | null>(null)
  const [creating, setCreating] = useState(false)
  const [historyFor, setHistoryFor] = useState<GardenBed | null>(null)

  const beds = useQuery({ queryKey: qk.gardenBeds(true), queryFn: () => listBeds(true) })
  const enums = useQuery({ queryKey: qk.gardenEnums, queryFn: getEnums, staleTime: Infinity })

  const invalidate = () => void qc.invalidateQueries({ queryKey: qk.gardenAll })
  const onErr = (e: unknown) =>
    toast.error(e instanceof ApiError ? (e.detail ?? cs.common.errorTitle) : cs.common.errorTitle)

  const move = useMutation({
    mutationFn: ({ id, afterId, beforeId }: { id: string; afterId?: string; beforeId?: string }) =>
      moveBed(id, { after_id: afterId, before_id: beforeId }),
    onSuccess: invalidate,
    onError: onErr,
  })
  const remove = useMutation({ mutationFn: deleteBed, onSuccess: invalidate, onError: onErr })

  const items = beds.data?.items ?? []
  const byId = new Map(items.map((b) => [b.id, b]))

  return (
    <div className="space-y-4">
      {/* The adjacency explanation, at the top, where the decision is made. */}
      <div className="flex items-start gap-2.5 rounded-md border border-accent/45 bg-accent/5 px-3 py-2.5">
        <Info size={16} className="mt-0.5 flex-none text-accent" aria-hidden />
        <p className="text-[12.5px] text-pretty">{cs.garden.adjacencyHint}</p>
      </div>

      <div className="flex items-center gap-2">
        <h2 className="flex-1 text-base font-bold">{cs.garden.bedsTitle}</h2>
        <Button size="sm" variant="primary" disabled={!writable} onClick={() => setCreating(true)}>
          <Plus size={14} aria-hidden />
          {cs.garden.addBed}
        </Button>
      </div>

      {beds.isPending ? (
        <div className="grid min-h-[200px] place-items-center">
          <Spinner />
        </div>
      ) : items.length === 0 ? (
        <p className="rounded-lg border border-dashed border-border p-8 text-center text-sm text-muted text-pretty">
          {cs.garden.emptyBedsBody}
        </p>
      ) : (
        <ul className="space-y-2">
          {items.map((bed, i) => {
            // Neighbours are only neighbours WITHIN a zone, so the up/down
            // controls stop at a zone boundary rather than silently moving a bed
            // into another part of the garden.
            const inZone = (b: GardenBed | undefined) =>
              b && (b.zone ?? '') === (bed.zone ?? '') ? b : undefined
            const sameZonePrev = inZone(items[i - 1])
            const sameZoneNext = inZone(items[i + 1])
            // BOTH SIDES OF THE GAP, always. The server places the bed with
            // lexorank.Between(after, before), and an empty side means ±∞ — so
            // sending only the bed being stepped over lands this one at the edge
            // of the zone instead of one place along. Here that is not cosmetic:
            // consecutive beds are neighbours (D117) and check C11 reads exactly
            // this order, so a sloppy move rewrites warnings nobody asked to
            // change. The far anchor is only sent when it is in the same zone;
            // at the zone edge an open side is the correct answer.
            const upAfter = sameZonePrev ? inZone(items[i - 2]) : undefined
            const downBefore = sameZoneNext ? inZone(items[i + 2]) : undefined
            return (
              <li key={bed.id} className="rounded-lg border border-border bg-s1 p-3">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="flex-none rounded-md bg-s3 px-1.5 py-0.5 font-mono text-[13px] font-bold">
                    {bed.code}
                  </span>
                  <button
                    type="button"
                    onClick={() => setEditing(bed)}
                    className="min-w-0 flex-1 text-left text-[13.5px] font-semibold hover:underline"
                  >
                    {bed.name}
                  </button>
                  <span className="flex-none font-mono text-[11.5px] text-muted">{bed.area_m2} m²</span>
                  {!bed.is_active && (
                    <span className="flex-none rounded-md border border-border bg-s3 px-1.5 py-0.5 text-[10.5px] font-semibold text-subtle">
                      nepoužívaný
                    </span>
                  )}
                </div>

                {/* The neighbour line: what the order MEANS, on every card. */}
                <p className="mt-1 text-[11.5px] text-subtle">
                  {bed.zone ? `${bed.zone} · ` : ''}
                  {cs.garden.bedNeighbours}:{' '}
                  {(bed.neighbours ?? []).length > 0
                    ? (bed.neighbours ?? []).map((id) => byId.get(id)?.code ?? '?').join(', ')
                    : '—'}
                </p>

                <div className="mt-2 flex flex-wrap gap-1">
                  <Button
                    size="sm"
                    variant="ghost"
                    disabled={!writable || !sameZonePrev}
                    onClick={() =>
                      sameZonePrev &&
                      move.mutate({ id: bed.id, afterId: upAfter?.id, beforeId: sameZonePrev.id })
                    }
                  >
                    <ChevronUp size={14} aria-hidden />
                    {cs.garden.moveUp}
                  </Button>
                  <Button
                    size="sm"
                    variant="ghost"
                    disabled={!writable || !sameZoneNext}
                    onClick={() =>
                      sameZoneNext &&
                      move.mutate({ id: bed.id, afterId: sameZoneNext.id, beforeId: downBefore?.id })
                    }
                  >
                    <ChevronDown size={14} aria-hidden />
                    {cs.garden.moveDown}
                  </Button>
                  <Button size="sm" variant="ghost" onClick={() => setHistoryFor(bed)}>
                    <History size={14} aria-hidden />
                    {cs.garden.bedHistory}
                  </Button>
                  <div className="flex-1" />
                  <Button
                    size="sm"
                    variant="danger"
                    disabled={!writable}
                    onClick={() => remove.mutate(bed.id)}
                  >
                    <Trash2 size={14} aria-hidden />
                  </Button>
                </div>
              </li>
            )
          })}
        </ul>
      )}

      <BedForm
        open={creating || Boolean(editing)}
        bed={editing}
        types={enums.data?.bed_type ?? []}
        onClose={() => {
          setCreating(false)
          setEditing(null)
        }}
      />

      <BedHistoryDialog bed={historyFor} onClose={() => setHistoryFor(null)} />
    </div>
  )
}

function BedForm({
  open,
  bed,
  types,
  onClose,
}: {
  open: boolean
  bed: GardenBed | null
  types: { value: string; label_cs: string }[]
  onClose: () => void
}) {
  const qc = useQueryClient()
  const [draft, setDraft] = useState<GardenBedInput>({})

  // The draft is EDITS ONLY, and it is dropped whenever the form is opened on a
  // different bed. Without this, cancelling out of one bed (the Zrušit button
  // calls onClose, which never touched the draft) left the typed values behind,
  // and the next bed opened would be shown — and saved — carrying them.
  useEffect(() => {
    if (open) setDraft({})
  }, [open, bed?.id])

  const current: GardenBedInput = {
    name: bed?.name ?? '',
    code: bed?.code ?? '',
    type: bed?.type ?? 'ground',
    length_cm: bed?.length_cm ?? null,
    width_cm: bed?.width_cm ?? null,
    area_m2: bed?.area_m2 ?? null,
    zone: bed?.zone ?? '',
    is_active: bed?.is_active ?? true,
    ...draft,
  }

  const save = useMutation({
    // An EDIT patches only what was touched; a create sends the whole form.
    // Echoing the stored area_m2 back on every edit is what made "Plocha se
    // dopočítá z rozměrů" a lie: the server derives the area from the dimensions
    // only when the body carries no area of its own, so a bed whose length
    // changed kept the old area — and a bed whose area was typed in directly
    // keeps it, because an untouched area is now simply absent from the patch.
    mutationFn: () => (bed ? updateBed(bed.id, draft) : createBed(current)),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.gardenAll })
      setDraft({})
      onClose()
    },
    onError: (e) =>
      toast.error(e instanceof ApiError ? (e.detail ?? cs.common.errorTitle) : cs.common.errorTitle),
  })

  const set = (patch: GardenBedInput) => setDraft((d) => ({ ...d, ...patch }))

  return (
    <ResponsiveModal
      open={open}
      onOpenChange={(o) => {
        if (!o) {
          setDraft({})
          onClose()
        }
      }}
      title={bed ? cs.garden.zahon : cs.garden.addBed}
    >
      <div className="space-y-3">
        <div className="flex gap-3">
          <label className="w-28">
            <span className="mb-1 block text-[12.5px] font-semibold">{cs.garden.bedCode}</span>
            <Input value={current.code ?? ''} maxLength={8} onChange={(e) => set({ code: e.target.value })} />
          </label>
          <label className="flex-1">
            <span className="mb-1 block text-[12.5px] font-semibold">{cs.garden.bedName}</span>
            <Input value={current.name ?? ''} onChange={(e) => set({ name: e.target.value })} />
          </label>
        </div>

        <label className="block">
          <span className="mb-1 block text-[12.5px] font-semibold">{cs.garden.bedType}</span>
          <select
            value={current.type ?? 'ground'}
            onChange={(e) => set({ type: e.target.value })}
            className="h-10 w-full rounded-md border border-border bg-s2 px-2 text-sm"
          >
            {types.map((t) => (
              <option key={t.value} value={t.value}>
                {t.label_cs}
              </option>
            ))}
          </select>
        </label>

        <div className="flex flex-wrap gap-3">
          <label className="w-28">
            <span className="mb-1 block text-[12.5px] font-semibold">Délka (cm)</span>
            <Input
              type="number"
              value={current.length_cm ?? ''}
              onChange={(e) => set({ length_cm: e.target.value ? Number(e.target.value) : null })}
            />
          </label>
          <label className="w-28">
            <span className="mb-1 block text-[12.5px] font-semibold">Šířka (cm)</span>
            <Input
              type="number"
              value={current.width_cm ?? ''}
              onChange={(e) => set({ width_cm: e.target.value ? Number(e.target.value) : null })}
            />
          </label>
          <label className="w-28">
            <span className="mb-1 block text-[12.5px] font-semibold">{cs.garden.bedArea}</span>
            <Input
              type="number"
              value={current.area_m2 ?? ''}
              onChange={(e) => set({ area_m2: e.target.value ? Number(e.target.value) : null })}
            />
          </label>
        </div>
        <p className="text-[11.5px] text-subtle">
          Plocha se dopočítá z rozměrů. U nepravidelného záhonu ji zadejte přímo.
        </p>

        <label className="block">
          <span className="mb-1 block text-[12.5px] font-semibold">{cs.garden.bedZone}</span>
          <Input
            value={current.zone ?? ''}
            onChange={(e) => set({ zone: e.target.value })}
            placeholder="Např. „horní zahrada“"
          />
          <span className="mt-1 block text-[11.5px] text-subtle">
            Sousedství se počítá jen v rámci jedné části.
          </span>
        </label>

        <div className="flex justify-end gap-2 pt-1">
          <Button variant="secondary" onClick={onClose}>
            {cs.finance.cancel}
          </Button>
          <Button variant="primary" disabled={save.isPending} onClick={() => save.mutate()}>
            {cs.finance.save}
          </Button>
        </div>
      </div>
    </ResponsiveModal>
  )
}

function BedHistoryDialog({ bed, onClose }: { bed: GardenBed | null; onClose: () => void }) {
  const history = useQuery({
    queryKey: qk.gardenBedHistory(bed?.id ?? ''),
    queryFn: () => bedHistory(bed!.id),
    enabled: Boolean(bed),
  })

  return (
    <ResponsiveModal
      open={Boolean(bed)}
      onOpenChange={(o) => !o && onClose()}
      title={`${cs.garden.bedHistory} · ${bed?.code ?? ''}`}
    >
      {history.isPending ? (
        <div className="grid min-h-[120px] place-items-center">
          <Spinner />
        </div>
      ) : (history.data?.seasons ?? []).length === 0 ? (
        // CLOSED SEASONS ONLY (D120), which is why a young garden sees nothing
        // here — and why C3 reports no_history rather than a pass.
        <p className="py-6 text-center text-[13px] text-muted text-pretty">
          {cs.garden.bedHistoryEmpty} {cs.garden.noHistoryBody}
        </p>
      ) : (
        <ul className="space-y-2">
          {(history.data?.seasons ?? []).map((s) => (
            <li key={s.year} className="rounded-md border border-border bg-s2 px-3 py-2.5">
              <div className="font-mono text-[13px] font-bold">{s.year}</div>
              <div className="mt-0.5 text-[12.5px] text-muted">
                {(s.plantings ?? []).map((p) => p.plant_name).join(', ') || s.families.join(', ')}
              </div>
            </li>
          ))}
        </ul>
      )}
    </ResponsiveModal>
  )
}
