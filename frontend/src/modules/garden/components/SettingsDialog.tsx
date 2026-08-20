import { useEffect, useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { CloudOff, Info } from 'lucide-react'
import { qk } from '@/api/keys'
import { cs } from '@/i18n/cs'
import { Button, Input, Spinner } from '@/components/ui/ui'
import { ResponsiveModal } from '@/components/ui/modal'
import { getSettings, updateSettings } from '../api/endpoints'
import { toastGardenError, useInvalidateGarden } from '../api/hooks'
import type { GardenSettingsInput } from '../api/types'

// NASTAVENÍ ZAHRADY (FR-G14) — the location and the frosts every date hangs off.
//
// COORDINATES LIVE HERE, not in Coolify (D112). They are user data rather than
// secrets, and a typo in a latitude should not need a redeploy to fix. Setting
// them is also what turns the forecast on, so the panel says that rather than
// leaving somebody to wonder why the frost warning never fires.
//
// AND THERE ARE NO NOTIFICATION SETTINGS HERE (D113). Who gets told about a
// frost, when, and through what channel is all Administrace's — this module
// publishes a number, a list and one audit event, and stops. The panel says so
// explicitly, because "where do I turn this on" is otherwise a fair question
// with no visible answer.

export function SettingsDialog({
  open,
  onOpenChange,
  canEdit,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  canEdit: boolean
}) {
  const invalidate = useInvalidateGarden()
  const [draft, setDraft] = useState<GardenSettingsInput>({})

  const settings = useQuery({ queryKey: qk.gardenSettings, queryFn: getSettings, enabled: open })

  // The draft is EDITS ONLY, and it is dropped whenever the dialog opens. The
  // dialog is mounted for the life of GardenPage and Zrušit only flips `open`,
  // so without this an abandoned edit stayed in state — and because value()
  // prefers the draft over the server value, reopening showed it as though it
  // were stored, and the next unrelated save carried it along.
  useEffect(() => {
    if (open) setDraft({})
  }, [open])

  const save = useMutation({
    mutationFn: () => updateSettings(draft),
    onSuccess: () => {
      invalidate()
      setDraft({})
      onOpenChange(false)
    },
    onError: toastGardenError,
  })

  const s = settings.data
  const value = <K extends keyof GardenSettingsInput>(key: K): GardenSettingsInput[K] =>
    draft[key] !== undefined ? draft[key] : (s?.[key as keyof typeof s] as GardenSettingsInput[K])
  const set = (patch: GardenSettingsInput) => setDraft((d) => ({ ...d, ...patch }))

  const hasCoords = value('latitude') != null && value('longitude') != null

  return (
    <ResponsiveModal open={open} onOpenChange={onOpenChange} title={cs.garden.settingsTitle}>
      {settings.isPending || !s ? (
        <div className="grid min-h-[160px] place-items-center">
          <Spinner />
        </div>
      ) : (
        <div className="space-y-4">
          <p className="text-[13px] text-muted text-pretty">{cs.garden.settingsLede}</p>

          <section className="space-y-3">
            <h3 className="text-[12px] font-bold tracking-wide text-subtle uppercase">Mrazy</h3>
            <div className="flex flex-wrap gap-3">
              <label className="min-w-[150px] flex-1">
                <span className="mb-1 block text-[12.5px] font-semibold">{cs.garden.posledniMraz}</span>
                <Input
                  placeholder="MM-DD, např. 05-15"
                  disabled={!canEdit}
                  value={(value('default_last_frost') as string | null | undefined) ?? ''}
                  onChange={(e) => set({ default_last_frost: e.target.value || null })}
                />
              </label>
              <label className="min-w-[150px] flex-1">
                <span className="mb-1 block text-[12.5px] font-semibold">{cs.garden.prvniMraz}</span>
                <Input
                  placeholder="MM-DD, např. 10-05"
                  disabled={!canEdit}
                  value={(value('default_first_frost') as string | null | undefined) ?? ''}
                  onChange={(e) => set({ default_first_frost: e.target.value || null })}
                />
              </label>
            </div>
            <p className="text-[11.5px] text-subtle text-pretty">
              Předvyplní se jimi každá nová sezóna. Termíny vázané na mráz se podle nich dopočítají.
            </p>
          </section>

          <section className="space-y-3">
            <h3 className="text-[12px] font-bold tracking-wide text-subtle uppercase">Poloha</h3>
            <div className="flex flex-wrap gap-3">
              <label className="w-32">
                <span className="mb-1 block text-[12.5px] font-semibold">{cs.garden.latitude}</span>
                <Input
                  type="number"
                  step="0.0001"
                  disabled={!canEdit}
                  value={(value('latitude') as number | null | undefined) ?? ''}
                  onChange={(e) => set({ latitude: e.target.value === '' ? null : Number(e.target.value) })}
                />
              </label>
              <label className="w-32">
                <span className="mb-1 block text-[12.5px] font-semibold">{cs.garden.longitude}</span>
                <Input
                  type="number"
                  step="0.0001"
                  disabled={!canEdit}
                  value={(value('longitude') as number | null | undefined) ?? ''}
                  onChange={(e) => set({ longitude: e.target.value === '' ? null : Number(e.target.value) })}
                />
              </label>
              <label className="w-32">
                <span className="mb-1 block text-[12.5px] font-semibold">{cs.garden.altitude}</span>
                <Input
                  type="number"
                  disabled={!canEdit}
                  value={(value('altitude_m') as number | null | undefined) ?? ''}
                  onChange={(e) => set({ altitude_m: e.target.value === '' ? null : Number(e.target.value) })}
                />
              </label>
            </div>

            {/* Weather is SOFT: off or absent is a state, never an error, because
                a forecast that did not load is not something anyone can act on. */}
            {!s.weather_enabled ? (
              <p className="flex items-start gap-2 text-[12px] text-muted text-pretty">
                <CloudOff size={14} className="mt-0.5 flex-none text-subtle" aria-hidden />
                {cs.garden.weatherOff}
              </p>
            ) : !hasCoords ? (
              <p className="flex items-start gap-2 text-[12px] text-muted text-pretty">
                <Info size={14} className="mt-0.5 flex-none text-subtle" aria-hidden />
                {cs.garden.weatherNoCoords}
              </p>
            ) : null}
          </section>

          <section className="space-y-3">
            <h3 className="text-[12px] font-bold tracking-wide text-subtle uppercase">Kontrola plánu</h3>
            <div className="flex flex-wrap gap-3">
              <label className="w-40">
                <span className="mb-1 block text-[12.5px] font-semibold">{cs.garden.rotationDefault}</span>
                <Input
                  type="number"
                  disabled={!canEdit}
                  value={(value('rotation_break_default') as number | undefined) ?? 4}
                  onChange={(e) => set({ rotation_break_default: Number(e.target.value) })}
                />
              </label>
              <label className="w-40">
                <span className="mb-1 block text-[12.5px] font-semibold">{cs.garden.workloadThreshold}</span>
                <Input
                  type="number"
                  disabled={!canEdit}
                  value={(value('workload_week_threshold') as number | undefined) ?? 10}
                  onChange={(e) => set({ workload_week_threshold: Number(e.target.value) })}
                />
              </label>
              <label className="w-40">
                <span className="mb-1 block text-[12.5px] font-semibold">{cs.garden.frostThreshold}</span>
                <Input
                  type="number"
                  step="0.5"
                  disabled={!canEdit}
                  value={(value('frost_threshold_c') as number | undefined) ?? 2}
                  onChange={(e) => set({ frost_threshold_c: Number(e.target.value) })}
                />
              </label>
            </div>
          </section>

          {/* The one thing somebody will look for here and not find. */}
          <p className="flex items-start gap-2 rounded-md border border-border bg-s2 px-3 py-2 text-[12px] text-pretty">
            <Info size={14} className="mt-0.5 flex-none text-subtle" aria-hidden />
            {cs.garden.notificationsElsewhere}
          </p>

          <div className="flex justify-end gap-2">
            <Button variant="secondary" onClick={() => onOpenChange(false)}>
              {cs.finance.cancel}
            </Button>
            <Button
              variant="primary"
              disabled={!canEdit || save.isPending || Object.keys(draft).length === 0}
              onClick={() => save.mutate()}
            >
              {cs.finance.save}
            </Button>
          </div>
        </div>
      )}
    </ResponsiveModal>
  )
}
