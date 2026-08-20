import { useEffect, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { Lock } from 'lucide-react'
import { qk } from '@/api/keys'
import { cs } from '@/i18n/cs'
import { ApiError } from '@/api/client'
import { Button, Input } from '@/components/ui/ui'
import { ResponsiveModal } from '@/components/ui/modal'
import { cn } from '@/lib/utils'
import { closeSeason } from '../api/endpoints'
import type { GardenPlanting, GardenSeason, GardenSeasonCloseInput } from '../api/types'

// UZAVŘÍT SEZÓNU — a once-a-year ritual worth doing (D120).
//
// This is the ONLY thing that creates rotation history, which is why it is a
// REVIEW rather than a form: the year is shown — what was planted, what came out
// — and the numbers are confirmed rather than typed. A screen that asked twelve
// questions in empty boxes would be skipped, and then C3 and C8 would have
// nothing to read for another year.
//
// Marking a planting FAILED is neutral record-keeping. A failure is data; the UI
// must not make it feel like a confession, so it sits beside "sklidili jsme" as
// an equal choice rather than behind a warning.

type Outcome = { status: 'done' | 'failed'; fail_reason: string; final_yield: string }

/** defaultOutcome pre-fills a row from what actually happened: a planting with a
 *  harvest recorded is done, and confirming that is one tap rather than one
 *  decision. Derived on every render, so a row is correct whenever its planting
 *  arrives rather than only if it arrived before the dialog mounted. */
function defaultOutcome(p: GardenPlanting): Outcome {
  return {
    status: p.status === 'failed' ? 'failed' : 'done',
    fail_reason: p.fail_reason ?? '',
    final_yield: p.yield_actual != null ? String(p.yield_actual) : '',
  }
}

export function SeasonCloseDialog({
  open,
  onOpenChange,
  season,
  plantings,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  season: GardenSeason
  plantings: GardenPlanting[]
}) {
  const qc = useQueryClient()
  const [lastFrost, setLastFrost] = useState(season.last_frost_actual_on ?? '')
  const [firstFrost, setFirstFrost] = useState(season.first_frost_actual_on ?? '')
  const [notes, setNotes] = useState('')
  // EDITS ONLY, and the default is derived per row rather than seeded here.
  //
  // PlanTab mounts this dialog the moment the season resolves, while the
  // plantings query is still pending — so a map built at mount would be built
  // from an empty list and never rebuilt, leaving the review blank and the
  // confirm button reading `.status` off undefined.
  const [edits, setEdits] = useState<Record<string, Partial<Outcome>>>({})
  const outcomeFor = (p: GardenPlanting): Outcome => ({ ...defaultOutcome(p), ...edits[p.id] })

  // Re-seed whenever the dialog opens, and whenever it opens on a different
  // season — PlanTab renders this dialog for its whole lifetime and only toggles
  // `open`, so nothing here is remounted between openings. Without this, a
  // planting flipped to "nepovedlo se" and then cancelled comes back marked
  // failed and gets written into a season only an admin can reopen; and a year
  // switched through the YearSwitcher keeps the previous season's actual frost
  // dates in the boxes, ready to be saved onto this one.
  useEffect(() => {
    if (!open) return
    setLastFrost(season.last_frost_actual_on ?? '')
    setFirstFrost(season.first_frost_actual_on ?? '')
    setNotes('')
    setEdits({})
  }, [open, season.year, season.last_frost_actual_on, season.first_frost_actual_on])

  const close = useMutation({
    mutationFn: () => {
      const body: GardenSeasonCloseInput = {
        last_frost_actual_on: lastFrost || undefined,
        first_frost_actual_on: firstFrost || undefined,
        notes_md: notes || undefined,
        outcomes: plantings.map((p) => {
          const o = outcomeFor(p)
          return {
            planting_id: p.id,
            status: o.status,
            fail_reason: o.status === 'failed' ? o.fail_reason || undefined : undefined,
            // Only send a yield the household did not already record as
            // harvests — otherwise closing would double-count the season.
            final_yield:
              p.yield_actual == null && o.final_yield ? Number(o.final_yield) : undefined,
            // AND THE UNIT IT WAS TYPED IN. The box is labelled with the crop's
            // own unit, so "120" against cibule means 120 kusů — sent without a
            // unit the server would have to guess, and a guess of kg lands a
            // count of onions in the season's kilogram total.
            final_yield_unit:
              p.yield_actual == null && o.final_yield ? (p.harvest_unit ?? undefined) : undefined,
          }
        }),
      }
      return closeSeason(season.year, body)
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.gardenAll })
      void qc.invalidateQueries({ queryKey: qk.dashboard })
      onOpenChange(false)
      toast.success(cs.garden.uzavritSezonu)
    },
    onError: (e) => toast.error(e instanceof ApiError ? (e.detail ?? cs.common.errorTitle) : cs.common.errorTitle),
  })

  const set = (id: string, patch: Partial<Outcome>) =>
    setEdits((prev) => ({ ...prev, [id]: { ...prev[id], ...patch } }))

  return (
    <ResponsiveModal
      open={open}
      onOpenChange={onOpenChange}
      title={`${cs.garden.closeTitle} ${season.year}`}
    >
      <div className="space-y-4">
        <p className="text-[13px] text-muted text-pretty">{cs.garden.closeLede}</p>

        <section>
          <h3 className="mb-1.5 text-[12px] font-bold tracking-wide text-subtle uppercase">
            {cs.garden.closeFrostActual}
          </h3>
          <div className="flex flex-wrap gap-3">
            <label className="min-w-[150px] flex-1">
              <span className="mb-1 block text-[12.5px] text-muted">{cs.garden.posledniMraz}</span>
              <Input type="date" value={lastFrost} onChange={(e) => setLastFrost(e.target.value)} />
            </label>
            <label className="min-w-[150px] flex-1">
              <span className="mb-1 block text-[12.5px] text-muted">{cs.garden.prvniMraz}</span>
              <Input type="date" value={firstFrost} onChange={(e) => setFirstFrost(e.target.value)} />
            </label>
          </div>
        </section>

        <section>
          <h3 className="mb-1.5 text-[12px] font-bold tracking-wide text-subtle uppercase">
            {cs.garden.closeOutcome}
          </h3>
          <ul className="space-y-2">
            {plantings.map((p) => {
              const o = outcomeFor(p)
              return (
                <li key={p.id} className="rounded-md border border-border bg-s2 px-3 py-2.5">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="min-w-0 flex-1 truncate text-[13.5px] font-semibold">
                      {p.plant_name}
                      {p.bed_code && <span className="ml-1.5 font-mono text-[11px] text-muted">{p.bed_code}</span>}
                    </span>
                    {/* Two equal choices, not a default and an exception. */}
                    <div className="flex gap-1">
                      {(['done', 'failed'] as const).map((status) => (
                        <button
                          key={status}
                          type="button"
                          onClick={() => set(p.id, { status })}
                          className={cn(
                            'min-h-[32px] rounded-md border px-2.5 text-[12.5px] font-semibold',
                            o.status === status
                              ? 'border-accent bg-accent/10 text-fg'
                              : 'border-border bg-s3 text-muted',
                          )}
                        >
                          {status === 'done' ? cs.garden.closeDone : cs.garden.closeFailed}
                        </button>
                      ))}
                    </div>
                  </div>

                  {o.status === 'failed' ? (
                    <Input
                      className="mt-2"
                      value={o.fail_reason}
                      onChange={(e) => set(p.id, { fail_reason: e.target.value })}
                      placeholder={cs.garden.failReason}
                    />
                  ) : p.yield_actual != null ? (
                    <p className="mt-1 font-mono text-[12px] text-muted">
                      {cs.garden.yieldActual}: {p.yield_actual} {p.harvest_unit ?? ''}
                    </p>
                  ) : (
                    <label className="mt-2 flex items-center gap-2">
                      <span className="flex-none text-[12.5px] text-muted">{cs.garden.closeFinalYield}</span>
                      <Input
                        type="number"
                        inputMode="decimal"
                        value={o.final_yield}
                        onChange={(e) => set(p.id, { final_yield: e.target.value })}
                        className="w-28"
                      />
                      {/* The unit, beside the box. A number typed into an
                          unlabelled field is an ambiguous answer, and this crop's
                          unit is not always kilograms. */}
                      <span className="flex-none font-mono text-[12px] text-subtle">
                        {p.harvest_unit ?? ''}
                      </span>
                    </label>
                  )}
                </li>
              )
            })}
            {plantings.length === 0 && (
              <li className="rounded-md border border-dashed border-border px-3 py-4 text-center text-[12.5px] text-muted">
                V téhle sezóně nebyla žádná výsadba.
              </li>
            )}
          </ul>
        </section>

        <label className="block">
          <span className="mb-1 block text-[12.5px] font-semibold">Poznámka k sezóně</span>
          <Input value={notes} onChange={(e) => setNotes(e.target.value)} />
        </label>

        <p className="rounded-md border border-warn/45 bg-warn/10 px-3 py-2 text-[12.5px] text-pretty">
          {cs.garden.closeWarning}
        </p>

        <div className="flex justify-end gap-2">
          <Button variant="secondary" onClick={() => onOpenChange(false)}>
            {cs.finance.cancel}
          </Button>
          <Button variant="primary" disabled={close.isPending} onClick={() => close.mutate()}>
            <Lock size={14} aria-hidden />
            {cs.garden.closeConfirm}
          </Button>
        </div>
      </div>
    </ResponsiveModal>
  )
}
