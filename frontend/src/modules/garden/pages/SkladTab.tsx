import { useEffect, useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { Plus, Trash2 } from 'lucide-react'
import { qk } from '@/api/keys'
import { useAuth } from '@/app/auth'
import { useOnline } from '@/platform/pwa/offline'
import { cs } from '@/i18n/cs'
import { todayISO } from '@/i18n/format'
import { Button, Input, Spinner } from '@/components/ui/ui'
import { ResponsiveModal } from '@/components/ui/modal'
import { cn } from '@/lib/utils'
import { createStorage, deleteStorage, getEnums, listStorage, updateStorage } from '../api/endpoints'
import { toastGardenError, useInvalidateGarden } from '../api/hooks'
import type { GardenStorageInput, GardenStorageItem } from '../api/types'
import { fmtDayYear } from '../components/labels'

// SKLAD — GARDEN PRODUCE ONLY (D121), not a general pantry.
//
// Consumption is recorded by editing the remaining quantity IN PLACE. There is
// deliberately no movements table behind it: the audit spine's field diffs
// already answer "when did we eat the last jar", and a second table would buy
// only a chart.
//
// The two automatic behaviours are the ones that keep the list honest without
// asking anything: reaching zero marks an item consumed by itself, and
// best-before is shown as a state rather than as a date somebody has to compare
// against today in their head.

export function SkladTab() {
  const { canWrite } = useAuth()
  const online = useOnline()
  const writable = canWrite && online

  const [status, setStatus] = useState('stored')
  const [adding, setAdding] = useState(false)
  const invalidate = useInvalidateGarden()

  const items = useQuery({
    queryKey: qk.gardenStorage(status),
    queryFn: () => listStorage({ status: status || undefined, limit: 200 }),
  })
  const enums = useQuery({ queryKey: qk.gardenEnums, queryFn: getEnums, staleTime: Infinity })

  const consume = useMutation({
    mutationFn: ({ id, remaining }: { id: string; remaining: number }) =>
      updateStorage(id, { quantity_remaining: remaining }),
    onSuccess: invalidate,
    onError: toastGardenError,
  })
  const spoil = useMutation({
    mutationFn: (id: string) => updateStorage(id, { status: 'spoiled' }),
    onSuccess: invalidate,
    onError: toastGardenError,
  })
  const remove = useMutation({
    mutationFn: (id: string) => deleteStorage(id),
    onSuccess: invalidate,
    onError: toastGardenError,
  })

  // LOCAL: a UTC "today" would fail to badge an item whose best_before was
  // yesterday for the first hours after local midnight.
  const today = todayISO()

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <h2 className="flex-1 text-base font-bold">{cs.garden.storageTitle}</h2>
        <select
          value={status}
          onChange={(e) => setStatus(e.target.value)}
          className="h-9 rounded-md border border-border bg-s2 px-2 text-sm"
        >
          <option value="stored">ve skladu</option>
          <option value="consumed">spotřebováno</option>
          <option value="spoiled">zkažené</option>
          <option value="">vše</option>
        </select>
        <Button size="sm" variant="primary" disabled={!writable} onClick={() => setAdding(true)}>
          <Plus size={14} aria-hidden />
          {cs.garden.addStorage}
        </Button>
      </div>

      <p className="text-[11.5px] text-subtle text-pretty">{cs.garden.consumeHint}</p>

      {items.isPending ? (
        <div className="grid min-h-[200px] place-items-center">
          <Spinner />
        </div>
      ) : (items.data?.items ?? []).length === 0 ? (
        <p className="rounded-lg border border-dashed border-border p-8 text-center text-sm text-muted">
          Ve skladu zatím nic není.
        </p>
      ) : (
        <ul className="grid gap-2 sm:grid-cols-2 xl:grid-cols-3">
          {(items.data?.items ?? []).map((item) => (
            <li key={item.id} className="rounded-lg border border-border bg-s1 p-3">
              <div className="flex items-start gap-2">
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-[13.5px] font-semibold">{item.product_name}</span>
                  <span className="font-mono text-[11px] text-muted">
                    {methodLabel(enums.data?.storage_method, item.method)}
                    {item.location ? ` · ${item.location}` : ''}
                  </span>
                </span>
                <Button size="sm" variant="ghost" disabled={!writable} onClick={() => remove.mutate(item.id)}>
                  <Trash2 size={13} aria-hidden />
                </Button>
              </div>

              <div className="mt-2 flex items-center gap-2">
                <span className="flex-none text-[11.5px] text-muted">{cs.garden.remaining}</span>
                <RemainingInput
                  item={item}
                  disabled={!writable || item.status !== 'stored'}
                  onCommit={(remaining) => consume.mutate({ id: item.id, remaining })}
                />
                <span className="flex-none font-mono text-[12px] text-subtle">
                  / {item.quantity_initial} {item.unit}
                </span>
              </div>

              <div className="mt-2 flex flex-wrap items-center gap-2">
                <span className="text-[11px] text-subtle">
                  {cs.garden.bestBefore}: {item.best_before ? fmtDayYear(item.best_before) : '—'}
                </span>
                {item.best_before && item.best_before < today && item.status === 'stored' && (
                  <span className="rounded-md border border-warn/45 bg-warn/10 px-1.5 py-0.5 text-[10.5px] font-bold text-warn">
                    prošlo
                  </span>
                )}
                <span
                  className={cn(
                    'rounded-md border px-1.5 py-0.5 text-[10.5px] font-semibold',
                    item.status === 'stored'
                      ? 'border-border bg-s3 text-muted'
                      : item.status === 'consumed'
                        ? 'border-good/45 bg-good/10 text-good'
                        : 'border-danger/45 bg-danger/10 text-danger',
                  )}
                >
                  {statusLabel(item.status)}
                </span>
                <div className="flex-1" />
                {item.status === 'stored' && (
                  <Button size="sm" variant="ghost" disabled={!writable} onClick={() => spoil.mutate(item.id)}>
                    {cs.garden.markSpoiled}
                  </Button>
                )}
              </div>
            </li>
          ))}
        </ul>
      )}

      <StorageForm
        open={adding}
        onClose={() => setAdding(false)}
        methods={enums.data?.storage_method ?? []}
        units={enums.data?.harvest_unit ?? []}
      />
    </div>
  )
}

/** RemainingInput edits the remaining quantity locally and COMMITS ON BLUR (or
 *  Enter), rather than PATCHing on every keystroke.
 *
 *  Per-keystroke saving looks harmless until somebody clears the box to retype
 *  a number: the empty string becomes 0, the server reads reaching zero as "it
 *  is eaten", flips the item to `consumed`, and the row leaves the default
 *  filter with its input now disabled — so the number they meant to type can no
 *  longer be typed. An empty or unchanged box therefore commits NOTHING, and
 *  "3" on the way to "2.5" never reaches the server at all. */
function RemainingInput({
  item,
  disabled,
  onCommit,
}: {
  item: GardenStorageItem
  disabled: boolean
  onCommit: (remaining: number) => void
}) {
  const [draft, setDraft] = useState<string | null>(null)
  const value = draft ?? String(item.quantity_remaining)

  const commit = () => {
    setDraft(null)
    if (draft === null || draft.trim() === '') return // nothing typed, or cleared
    const next = Number(draft)
    if (!Number.isFinite(next) || next === item.quantity_remaining) return
    onCommit(next)
  }

  return (
    <Input
      type="number"
      inputMode="decimal"
      step="0.5"
      disabled={disabled}
      value={value}
      onChange={(e) => setDraft(e.target.value)}
      onBlur={commit}
      onKeyDown={(e) => {
        if (e.key === 'Enter') e.currentTarget.blur()
        if (e.key === 'Escape') setDraft(null)
      }}
      className="h-9 w-20"
    />
  )
}

/** emptyStorageDraft is a FUNCTION, not a constant: stored_on has to be read
 *  from the clock at the moment the form opens. */
function emptyStorageDraft(): GardenStorageInput {
  return { method: 'cellar', unit: 'ks', stored_on: todayISO() }
}

function StorageForm({
  open,
  onClose,
  methods,
  units,
}: {
  open: boolean
  onClose: () => void
  methods: { value: string; label_cs: string }[]
  units: { value: string; label_cs: string }[]
}) {
  const invalidate = useInvalidateGarden()
  // stored_on defaults to the LOCAL date: a UTC default would stamp a jar put
  // away just after midnight with yesterday.
  const [draft, setDraft] = useState<GardenStorageInput>(emptyStorageDraft)

  // The form is mounted for the life of the tab, so the draft has to be
  // re-derived every time it OPENS, not only after a save. Without this,
  // cancelling out of one jar (Zrušit calls onClose, which never touched the
  // draft) left the typed values behind, and the next "Přidat" showed — and
  // saved — the previous item's quantity, location and best-before. It is also
  // what keeps stored_on on today: seeded once, a tab left open across midnight
  // stamps new jars with yesterday.
  useEffect(() => {
    if (open) setDraft(emptyStorageDraft())
  }, [open])

  const save = useMutation({
    mutationFn: () => createStorage(draft),
    onSuccess: () => {
      invalidate()
      setDraft(emptyStorageDraft())
      onClose()
    },
    onError: toastGardenError,
  })

  const set = (patch: GardenStorageInput) => setDraft((d) => ({ ...d, ...patch }))

  return (
    <ResponsiveModal open={open} onOpenChange={(o) => !o && onClose()} title={cs.garden.addStorage}>
      <div className="space-y-3">
        <label className="block">
          <span className="mb-1 block text-[12.5px] font-semibold">{cs.garden.productName}</span>
          <Input
            value={draft.product_name ?? ''}
            onChange={(e) => set({ product_name: e.target.value })}
            placeholder="Např. „okurky sterilované“"
            autoFocus
          />
        </label>

        <div className="flex flex-wrap gap-3">
          <label className="min-w-[140px] flex-1">
            <span className="mb-1 block text-[12.5px] font-semibold">{cs.garden.method}</span>
            <select
              value={draft.method}
              onChange={(e) => set({ method: e.target.value })}
              className="h-10 w-full rounded-md border border-border bg-s2 px-2 text-sm"
            >
              {methods.map((m) => (
                <option key={m.value} value={m.value}>
                  {m.label_cs}
                </option>
              ))}
            </select>
          </label>
          <label className="w-28">
            <span className="mb-1 block text-[12.5px] font-semibold">{cs.garden.unit}</span>
            <select
              value={draft.unit}
              onChange={(e) => set({ unit: e.target.value })}
              className="h-10 w-full rounded-md border border-border bg-s2 px-2 text-sm"
            >
              {units.map((u) => (
                <option key={u.value} value={u.value}>
                  {u.label_cs}
                </option>
              ))}
            </select>
          </label>
          <label className="w-28">
            <span className="mb-1 block text-[12.5px] font-semibold">{cs.garden.quantity}</span>
            <Input
              type="number"
              inputMode="decimal"
              value={draft.quantity_initial ?? ''}
              onChange={(e) => set({ quantity_initial: Number(e.target.value) })}
            />
          </label>
        </div>

        <label className="block">
          <span className="mb-1 block text-[12.5px] font-semibold">{cs.garden.location}</span>
          <Input
            value={draft.location ?? ''}
            onChange={(e) => set({ location: e.target.value })}
            placeholder="sklep · mrazák · spajz"
          />
        </label>

        <div className="flex flex-wrap gap-3">
          <label className="min-w-[150px] flex-1">
            <span className="mb-1 block text-[12.5px] font-semibold">Uskladněno</span>
            <Input type="date" value={draft.stored_on ?? ''} onChange={(e) => set({ stored_on: e.target.value })} />
          </label>
          <label className="min-w-[150px] flex-1">
            <span className="mb-1 block text-[12.5px] font-semibold">{cs.garden.bestBefore}</span>
            <Input
              type="date"
              value={draft.best_before ?? ''}
              onChange={(e) => set({ best_before: e.target.value || null })}
            />
          </label>
        </div>

        <div className="flex justify-end gap-2">
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

function methodLabel(methods: { value: string; label_cs: string }[] | undefined, value: string): string {
  return methods?.find((m) => m.value === value)?.label_cs ?? value
}

function statusLabel(status: GardenStorageItem['status']): string {
  return status === 'stored' ? 've skladu' : status === 'consumed' ? 'spotřebováno' : 'zkažené'
}
