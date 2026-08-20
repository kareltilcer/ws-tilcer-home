import { useEffect, useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { BadgeCheck, ChevronDown, Plus, Trash2 } from 'lucide-react'
import { qk } from '@/api/keys'
import { cs } from '@/i18n/cs'
import { Button, Input, Spinner, Textarea } from '@/components/ui/ui'
import { ResponsiveModal } from '@/components/ui/modal'
import { cn } from '@/lib/utils'
import { createPlant, createVariety, deletePlant, deleteVariety, listVarieties, updatePlant } from '../api/endpoints'
import { toastGardenError, useInvalidateGarden } from '../api/hooks'
import type { GardenEnums, GardenPlant, GardenPlantInput, GardenSeason, GardenWindow } from '../api/types'
import { TimingWindowInput } from './TimingWindowInput'
import { UnverifiedBadge } from './labels'

// THE FORTY-FIELD FORM, made bearable.
//
// Progressive disclosure is the whole design here. Only the FIRST section is
// open by default, and it holds exactly the minimum viable crop: name, family,
// type, hardiness, sow method, harvest unit. Family and hardiness are required
// because the rotation engine and the frost logic respectively cannot function
// without them (D104, D111) — a crop missing either is one the module cannot
// reason about, so the form says so rather than accepting it and failing later.
//
// Everything else collapses. A crop the household barely knows is six fields; a
// fully-filled one is forty; and the form does not make the first case feel like
// an unfinished version of the second.

interface SectionProps {
  title: string
  defaultOpen?: boolean
  children: React.ReactNode
}

function Section({ title, defaultOpen, children }: SectionProps) {
  const [open, setOpen] = useState(Boolean(defaultOpen))
  return (
    <section className="rounded-md border border-border">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        className="flex min-h-[44px] w-full items-center gap-2 px-3 py-2 text-left"
      >
        <span className="flex-1 text-[13px] font-bold">{title}</span>
        <ChevronDown size={16} className={cn('text-subtle transition-transform', open && 'rotate-180')} aria-hidden />
      </button>
      {open && <div className="space-y-3 border-t border-border px-3 py-3">{children}</div>}
    </section>
  )
}

export function PlantForm({
  open,
  plant,
  enums,
  season,
  canEdit,
  onClose,
}: {
  open: boolean
  plant: GardenPlant | null
  enums: GardenEnums | undefined
  season: GardenSeason | undefined
  canEdit: boolean
  onClose: () => void
}) {
  const invalidate = useInvalidateGarden()
  const [draft, setDraft] = useState<GardenPlantInput>({})

  useEffect(() => {
    if (open) setDraft({})
  }, [open, plant?.id])

  const value = <K extends keyof GardenPlantInput>(key: K): GardenPlantInput[K] =>
    (draft[key] !== undefined ? draft[key] : (plant?.[key as keyof GardenPlant] as GardenPlantInput[K]))

  const set = (patch: GardenPlantInput) => setDraft((d) => ({ ...d, ...patch }))

  const save = useMutation({
    mutationFn: () => {
      const body: GardenPlantInput = plant
        ? draft
        : {
            // A create sends the whole draft plus the required identity, which
            // the server validates and rejects with a Czech sentence naming what
            // is missing.
            ...draft,
            name_cs: draft.name_cs ?? '',
            family: draft.family ?? '',
            plant_type: draft.plant_type ?? 'vegetable',
            hardiness: draft.hardiness ?? '',
            harvest_unit: draft.harvest_unit ?? 'kg',
          }
      return plant ? updatePlant(plant.id, body) : createPlant(body)
    },
    onSuccess: () => {
      invalidate()
      onClose()
    },
    onError: toastGardenError,
  })

  const verify = useMutation({
    mutationFn: () => updatePlant(plant!.id, { verified: true }),
    onSuccess: invalidate,
    onError: toastGardenError,
  })

  const remove = useMutation({
    mutationFn: () => deletePlant(plant!.id),
    onSuccess: () => {
      invalidate()
      onClose()
    },
    onError: toastGardenError,
  })

  const unverified = plant && plant.provenance.source === 'llm' && !plant.provenance.verified_at

  const enumSelect = (
    label: string,
    key: keyof GardenPlantInput,
    enumName: string,
    required?: boolean,
    hint?: string,
  ) => (
    <label className="block">
      <span className="mb-1 block text-[12.5px] font-semibold">
        {label}
        {required && <span className="ml-1 text-danger">*</span>}
      </span>
      <select
        value={(value(key) as string | null | undefined) ?? ''}
        disabled={!canEdit}
        onChange={(e) => set({ [key]: e.target.value || null } as GardenPlantInput)}
        className="h-10 w-full rounded-md border border-border bg-s2 px-2 text-sm"
      >
        <option value="">—</option>
        {(enums?.[enumName] ?? []).map((o) => (
          <option key={o.value} value={o.value}>
            {o.label_cs}
          </option>
        ))}
      </select>
      {hint && <span className="mt-1 block text-[11.5px] text-subtle text-pretty">{hint}</span>}
    </label>
  )

  const num = (label: string, key: keyof GardenPlantInput, unit?: string) => (
    <label className="block">
      <span className="mb-1 block text-[12.5px] font-semibold">
        {label}
        {unit && <span className="ml-1 font-normal text-subtle">({unit})</span>}
      </span>
      <Input
        type="number"
        inputMode="decimal"
        disabled={!canEdit}
        value={(value(key) as number | null | undefined) ?? ''}
        onChange={(e) => set({ [key]: e.target.value === '' ? null : Number(e.target.value) } as GardenPlantInput)}
      />
    </label>
  )

  const flag = (label: string, key: keyof GardenPlantInput, hint: string) => (
    <label className="flex items-start gap-2">
      <input
        type="checkbox"
        className="mt-1"
        disabled={!canEdit}
        checked={Boolean(value(key))}
        onChange={(e) => set({ [key]: e.target.checked } as GardenPlantInput)}
      />
      <span>
        <span className="block text-[12.5px] font-semibold">{label}</span>
        <span className="block text-[11.5px] text-subtle text-pretty">{hint}</span>
      </span>
    </label>
  )

  const window = (label: string, key: keyof GardenPlantInput) => (
    <TimingWindowInput
      label={label}
      value={value(key) as GardenWindow | null | undefined}
      season={season}
      disabled={!canEdit}
      onChange={(next) => set({ [key]: next } as GardenPlantInput)}
    />
  )

  return (
    <ResponsiveModal
      open={open}
      onOpenChange={(o) => !o && onClose()}
      title={plant ? plant.name_cs : cs.garden.addPlant}
    >
      <div className="space-y-3">
        {unverified && (
          <div className="flex flex-wrap items-center gap-2 rounded-md border border-border bg-s2 px-3 py-2">
            <UnverifiedBadge />
            <span className="min-w-0 flex-1 text-[12.5px] text-muted text-pretty">
              {cs.garden.unverifiedHint}
            </span>
            <Button size="sm" variant="secondary" disabled={!canEdit} onClick={() => verify.mutate()}>
              <BadgeCheck size={14} aria-hidden />
              {cs.garden.verify}
            </Button>
          </div>
        )}

        {/* The minimum viable crop, open by default. */}
        <Section title={cs.garden.sectionIdentity} defaultOpen>
          <label className="block">
            <span className="mb-1 block text-[12.5px] font-semibold">
              {cs.garden.plodina}
              <span className="ml-1 text-danger">*</span>
            </span>
            <Input
              value={(value('name_cs') as string | undefined) ?? ''}
              disabled={!canEdit}
              onChange={(e) => set({ name_cs: e.target.value })}
              autoFocus={!plant}
            />
          </label>
          <label className="block">
            <span className="mb-1 block text-[12.5px] font-semibold">Latinský název</span>
            <Input
              value={(value('name_latin') as string | null | undefined) ?? ''}
              disabled={!canEdit}
              onChange={(e) => set({ name_latin: e.target.value || null })}
            />
          </label>
          {enumSelect(cs.garden.celed, 'family', 'family', true, 'Podle čeledi se hlídá střídání plodin.')}
          {enumSelect('Typ', 'plant_type', 'plant_type', true)}
          {enumSelect(
            'Odolnost proti mrazu',
            'hardiness',
            'hardiness',
            true,
            'Podle ní se pozná, co je v noci ohrožené mrazem.',
          )}
          {enumSelect('Způsob výsevu', 'sow_method', 'sow_method')}
          {enumSelect('Jednotka sklizně', 'harvest_unit', 'harvest_unit', true)}
        </Section>

        <Section title={cs.garden.sectionAgronomy}>
          {enumSelect(cs.garden.narokNaZiviny, 'feeder_class', 'feeder_class')}
          {enumSelect('Hloubka kořenů', 'root_depth', 'root_depth')}
          {enumSelect('Osvit', 'sun', 'sun')}
          {enumSelect('Nárok na vodu', 'water_need', 'water_need')}
          <div className="grid grid-cols-2 gap-3">
            {num('pH od', 'soil_ph_min')}
            {num('pH do', 'soil_ph_max')}
          </div>
          {num('Pauza pro střídání', 'rotation_break_years', 'let')}
        </Section>

        <Section title={cs.garden.sectionPropagation}>
          <div className="grid grid-cols-2 gap-3">
            {num('Hloubka výsevu', 'sow_depth_cm', 'cm')}
            {num('Otužování', 'hardening_days', 'dní')}
            {num('Rozestup řádků', 'spacing_row_cm', 'cm')}
            {num('Rozestup rostlin', 'spacing_plant_cm', 'cm')}
            {num('Rostlin na m²', 'plants_per_m2')}
            {num('Teplota klíčení', 'germination_temp_c', '°C')}
            {num('Klíčení od', 'days_to_germinate_min', 'dní')}
            {num('Klíčení do', 'days_to_germinate_max', 'dní')}
            {num('Do sklizně od', 'days_to_maturity_min', 'dní')}
            {num('Do sklizně do', 'days_to_maturity_max', 'dní')}
          </div>
          <div className="space-y-2 pt-1">
            {flag('Pikýrovat', 'needs_pricking_out', 'Vytvoří práci „pikýrování“ po vyklíčení.')}
            {flag('Potřebuje oporu', 'needs_support', 'Vytvoří práci „opora a vyvazování“.')}
            {flag('Mulčovat', 'wants_mulch', 'Vytvoří práci „mulčování“ po výsadbě.')}
            {flag('Hlídat škůdce', 'wants_pest_check', 'Vytvoří pravidelnou kontrolu po celou dobu růstu.')}
          </div>
        </Section>

        {/* The four windows, each with its live resolved-date echo. */}
        <Section title={cs.garden.sectionTiming}>
          {window('Výsev do sadbovačů', 'win_sow_indoor')}
          {window('Přímý výsev', 'win_sow_direct')}
          {window('Výsadba ven', 'win_transplant')}
          {window('Sklizeň', 'win_harvest')}
        </Section>

        <Section title={cs.garden.sectionHarvest}>
          <div className="grid grid-cols-2 gap-3">
            {num('Výnos na m²', 'yield_per_m2')}
            {num('Výnos na rostlinu', 'yield_per_plant')}
            {num('Skladovací teplota', 'storage_temp_c', '°C')}
            {num('Skladovací vlhkost', 'storage_humidity', '%')}
            {num('Vydrží', 'shelf_life_days', 'dní')}
          </div>
          <fieldset disabled={!canEdit}>
            <legend className="mb-1 text-[12.5px] font-semibold">Způsoby uskladnění</legend>
            <div className="flex flex-wrap gap-1.5">
              {(enums?.storage_method ?? []).map((m) => {
                const list = (value('storage_methods') as string[] | null | undefined) ?? []
                const on = list.includes(m.value)
                return (
                  <button
                    key={m.value}
                    type="button"
                    onClick={() =>
                      set({
                        storage_methods: on ? list.filter((v) => v !== m.value) : [...list, m.value],
                      })
                    }
                    className={cn(
                      'min-h-[32px] rounded-md border px-2.5 text-[12.5px] font-semibold',
                      on ? 'border-accent bg-accent/10 text-fg' : 'border-border bg-s2 text-muted',
                    )}
                  >
                    {m.label_cs}
                  </button>
                )
              })}
            </div>
          </fieldset>
        </Section>

        <Section title={cs.garden.sectionNotes}>
          <Textarea
            rows={5}
            disabled={!canEdit}
            value={(value('notes_md') as string | null | undefined) ?? ''}
            onChange={(e) => set({ notes_md: e.target.value || null })}
            placeholder="Zkušenosti, odrůdy, na co si dát pozor…"
          />
        </Section>

        {plant && <VarietyList plantId={plant.id} canEdit={canEdit} />}

        <div className="flex items-center gap-2 pt-1">
          {plant && (
            <Button size="sm" variant="danger" disabled={!canEdit} onClick={() => remove.mutate()}>
              <Trash2 size={14} aria-hidden />
              {cs.finance.remove}
            </Button>
          )}
          <div className="flex-1" />
          <Button variant="secondary" onClick={onClose}>
            {cs.finance.cancel}
          </Button>
          <Button variant="primary" disabled={!canEdit || save.isPending} onClick={() => save.mutate()}>
            {cs.finance.save}
          </Button>
        </div>
      </div>
    </ResponsiveModal>
  )
}

/** Varieties are nested under the species, because a variety with no species is
 *  meaningless and a separate screen for them would be a navigation with no
 *  destination. */
function VarietyList({ plantId, canEdit }: { plantId: string; canEdit: boolean }) {
  const invalidate = useInvalidateGarden()
  const [name, setName] = useState('')

  const varieties = useQuery({
    queryKey: qk.gardenVarieties(plantId),
    queryFn: () => listVarieties(plantId),
  })

  const add = useMutation({
    mutationFn: () => createVariety(plantId, { name }),
    onSuccess: () => {
      setName('')
      invalidate()
      void varieties.refetch()
    },
    onError: toastGardenError,
  })

  const remove = useMutation({
    mutationFn: (id: string) => deleteVariety(id),
    onSuccess: () => {
      invalidate()
      void varieties.refetch()
    },
    onError: toastGardenError,
  })

  return (
    <Section title={cs.garden.varieties}>
      {varieties.isPending ? (
        <div className="grid place-items-center py-4">
          <Spinner />
        </div>
      ) : (
        <ul className="space-y-1.5">
          {(varieties.data?.items ?? []).map((v) => (
            <li key={v.id} className="flex items-center gap-2 rounded-md border border-border bg-s2 px-3 py-2">
              <span className="min-w-0 flex-1 truncate text-[13px] font-semibold">{v.name}</span>
              {/* A variety that overrides nothing INHERITS everything (D103), and
                  saying so is what stops somebody re-typing the species' data. */}
              <span className="flex-none text-[11px] text-subtle">{cs.garden.inherits}</span>
              <Button size="sm" variant="ghost" disabled={!canEdit} onClick={() => remove.mutate(v.id)}>
                <Trash2 size={13} aria-hidden />
              </Button>
            </li>
          ))}
          {(varieties.data?.items ?? []).length === 0 && (
            <li className="py-2 text-center text-[12.5px] text-muted">Zatím žádná odrůda.</li>
          )}
        </ul>
      )}

      <div className="flex gap-2">
        <Input
          value={name}
          disabled={!canEdit}
          onChange={(e) => setName(e.target.value)}
          placeholder={cs.garden.addVariety}
        />
        <Button
          variant="secondary"
          disabled={!canEdit || !name.trim() || add.isPending}
          onClick={() => add.mutate()}
        >
          <Plus size={14} aria-hidden />
        </Button>
      </div>
    </Section>
  )
}
