import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { ArrowRight, Trash2 } from 'lucide-react'
import { qk } from '@/api/keys'
import { cs } from '@/i18n/cs'

import { ApiError } from '@/api/client'
import { Button, Input, Spinner } from '@/components/ui/ui'
import { ResponsiveModal } from '@/components/ui/modal'
import { deletePlanting, getPlanting, shiftTasks, updatePlanting } from '../api/endpoints'
import type { GardenPlanting, GardenPlantingInput } from '../api/types'
import { fmtDayYear } from './labels'

// VÝSADBA — plan versus reality, and the one action offered against the gap.
//
// The rule this screen exists to express is D119: ACTUAL DATES NEVER RE-DRIVE
// PLANNED ONES. Sow two weeks late and the harvest window stays where February
// put it, because a self-reshuffling calendar is untrustworthy.
//
// The compensating control is that the drift is NEVER SILENT. The sentence comes
// from the server, so this page, the widget and any future notification say it
// identically — and beside it sits exactly one button, "posunout navazující
// práce", which moves the remaining open work and marks it edited. After that,
// regeneration leaves those tasks alone permanently (D110).

const PLANNED_FIELDS = [
  { key: 'sow_indoor_on', label: 'Výsev do sadbovačů' },
  { key: 'sow_direct_on', label: 'Přímý výsev' },
  { key: 'transplant_on', label: 'Výsadba' },
  { key: 'harvest_from', label: 'Sklizeň od' },
  { key: 'harvest_to', label: 'Sklizeň do' },
] as const

const ACTUAL_FIELDS = [
  { key: 'sowed_on', label: 'Vyseto' },
  { key: 'transplanted_on', label: 'Vysazeno' },
  { key: 'first_harvest_on', label: 'První sklizeň' },
  { key: 'cleared_on', label: 'Uklizeno' },
] as const

export function PlantingDialog({
  plantingId,
  onClose,
  canEdit,
}: {
  plantingId: string | null
  onClose: () => void
  canEdit: boolean
}) {
  const qc = useQueryClient()
  const [draft, setDraft] = useState<GardenPlantingInput>({})

  const planting = useQuery({
    queryKey: qk.gardenPlanting(plantingId ?? ''),
    queryFn: () => getPlanting(plantingId!),
    enabled: Boolean(plantingId),
  })

  useEffect(() => {
    if (plantingId) setDraft({})
  }, [plantingId])

  const invalidate = () => {
    void qc.invalidateQueries({ queryKey: qk.gardenAll })
    void qc.invalidateQueries({ queryKey: qk.dashboard })
  }

  const save = useMutation({
    mutationFn: (body: GardenPlantingInput) => updatePlanting(plantingId!, body),
    onSuccess: () => {
      invalidate()
      setDraft({})
    },
    onError: (e) => toast.error(e instanceof ApiError ? (e.detail ?? cs.common.errorTitle) : cs.common.errorTitle),
  })

  const shift = useMutation({
    mutationFn: (days: number) => shiftTasks(plantingId!, days),
    onSuccess: (res) => {
      invalidate()
      toast.success(cs.garden.shiftTasksDone(res.items.length))
    },
    onError: (e) => toast.error(e instanceof ApiError ? (e.detail ?? cs.common.errorTitle) : cs.common.errorTitle),
  })

  const remove = useMutation({
    mutationFn: () => deletePlanting(plantingId!),
    onSuccess: () => {
      invalidate()
      onClose()
    },
    onError: (e) => toast.error(e instanceof ApiError ? (e.detail ?? cs.common.errorTitle) : cs.common.errorTitle),
  })

  const p = planting.data
  const dirty = Object.keys(draft).length > 0

  return (
    <ResponsiveModal
      open={Boolean(plantingId)}
      onOpenChange={(open) => !open && onClose()}
      title={p ? `${p.plant_name}${p.variety_name ? ` · ${p.variety_name}` : ''}` : cs.garden.vysadba}
    >
      {planting.isPending || !p ? (
        <div className="grid min-h-[160px] place-items-center">
          <Spinner />
        </div>
      ) : (
        <div className="space-y-4">
          <div className="flex flex-wrap items-center gap-2 text-[12.5px] text-muted">
            {p.bed_code && <span className="font-mono font-bold text-fg">{p.bed_code}</span>}
            {p.area_m2 != null && <span>{p.area_m2} m²</span>}
            {p.plant_count != null && <span>{p.plant_count} ks</span>}
            {p.season_year != null ? <span>{p.season_year}</span> : <span>{cs.garden.trvalka}</span>}
          </div>

          {/* THE DRIFT LINE. Server-authored, so every surface says it the same
              way — and the single action beside it is the whole of D119's
              compensating control. */}
          {p.drift && (
            <div className="rounded-md border border-warn/45 bg-warn/10 px-3 py-2.5">
              <p className="text-[13px] font-semibold text-pretty">{p.drift.message_cs}</p>
              <Button
                size="sm"
                variant="secondary"
                className="mt-2"
                disabled={!canEdit || shift.isPending}
                onClick={() => shift.mutate(p.drift!.days)}
              >
                <ArrowRight size={14} aria-hidden />
                {cs.garden.shiftTasks}
              </Button>
            </div>
          )}

          {p.status === 'failed' && (
            <div className="rounded-md border border-border bg-s2 px-3 py-2.5">
              <div className="text-[12.5px] font-semibold">{cs.garden.failedTitle}</div>
              {p.fail_reason && <p className="mt-0.5 text-[12.5px] text-muted">{p.fail_reason}</p>}
            </div>
          )}

          {/* CLEARING A DATE DRAFTS "", NOT null. The API cannot tell an
              explicitly-null date from an omitted one — both decode to the same
              nil pointer server-side — so a null is read as "no edit" and the
              date silently survives the save. An empty string is the clear the
              server understands, and it is also what hands a manually-pinned
              date back to the planner. */}
          <DateGroup
            title={cs.garden.planned}
            fields={PLANNED_FIELDS}
            planting={p}
            draft={draft}
            canEdit={canEdit}
            onChange={(key, value) => setDraft((d) => ({ ...d, [key]: value }))}
            hint="Změna plánovaného data přepočítá navazující práce."
          />

          <DateGroup
            title={cs.garden.actual}
            fields={ACTUAL_FIELDS}
            planting={p}
            draft={draft}
            canEdit={canEdit}
            onChange={(key, value) => setDraft((d) => ({ ...d, [key]: value }))}
            hint="Skutečná data plánem nehýbou — jen ukážou, o kolik se to rozešlo."
          />

          {(p.yield_expected != null || p.yield_actual != null) && (
            <div className="grid grid-cols-2 gap-2">
              <Stat label={cs.garden.yieldExpected} value={p.yield_expected} />
              <Stat label={cs.garden.yieldActual} value={p.yield_actual} />
            </div>
          )}

          {p.planted_on && (
            <div className="rounded-md border border-border bg-s2 px-3 py-2 text-[12.5px]">
              <span className="text-muted">Vysazeno </span>
              <span className="font-semibold">{fmtDayYear(p.planted_on)}</span>
              {p.rootstock && <span className="text-muted"> · podnož {p.rootstock}</span>}
            </div>
          )}

          <div className="flex items-center gap-2 pt-1">
            <Button
              variant="danger"
              size="sm"
              disabled={!canEdit || remove.isPending}
              onClick={() => remove.mutate()}
            >
              <Trash2 size={14} aria-hidden />
              {cs.finance.remove}
            </Button>
            <div className="flex-1" />
            <Button variant="secondary" onClick={onClose}>
              {cs.finance.cancel}
            </Button>
            <Button
              variant="primary"
              disabled={!canEdit || !dirty || save.isPending}
              onClick={() => save.mutate(draft)}
            >
              {cs.finance.save}
            </Button>
          </div>
        </div>
      )}
    </ResponsiveModal>
  )
}

function DateGroup({
  title,
  fields,
  planting,
  draft,
  canEdit,
  onChange,
  hint,
}: {
  title: string
  fields: readonly { key: string; label: string }[]
  planting: GardenPlanting
  draft: GardenPlantingInput
  canEdit: boolean
  onChange: (key: string, value: string) => void
  hint: string
}) {
  const manual = new Set(planting.manual_dates ?? [])
  return (
    <section>
      <h3 className="mb-1.5 text-[12px] font-bold tracking-wide text-subtle uppercase">{title}</h3>
      <div className="space-y-1.5">
        {fields.map((f) => {
          // KEY PRESENCE, not `??`. Clearing a date drafts an explicit null,
          // and `??` would read that as "no edit" and fall through to the
          // stored date — putting the old value back on screen while the draft
          // still holds the clear that Save is about to send.
          const stored = (planting as unknown as Record<string, string | null>)[f.key]
          const edited = f.key in draft
          const value = (edited ? ((draft as Record<string, unknown>)[f.key] as string | null) : stored) ?? ''
          return (
            <label key={f.key} className="flex items-center gap-2">
              <span className="w-[124px] flex-none text-[12.5px] text-muted">{f.label}</span>
              <Input
                type="date"
                value={value}
                disabled={!canEdit}
                onChange={(e) => onChange(f.key, e.target.value)}
                className="flex-1"
              />
              {manual.has(f.key) && (
                <span
                  className="flex-none text-[10.5px] font-semibold text-accent"
                  title="Datum jste zadali ručně, přepočet ho nepřepíše."
                >
                  ručně
                </span>
              )}
            </label>
          )
        })}
      </div>
      <p className="mt-1 text-[11.5px] text-subtle text-pretty">{hint}</p>
    </section>
  )
}

function Stat({ label, value }: { label: string; value: number | null }) {
  return (
    <div className="rounded-md border border-border bg-s2 px-3 py-2">
      <div className="text-[11.5px] text-muted">{label}</div>
      <div className="font-mono text-base font-semibold">{value != null ? value : '—'}</div>
    </div>
  )
}
