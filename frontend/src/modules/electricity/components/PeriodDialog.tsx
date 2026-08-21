import { useEffect, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { qk } from '@/api/keys'
import { cs } from '@/i18n/cs'
import { todayISO } from '@/i18n/format'
import { ApiError } from '@/api/client'
import { Button, Input } from '@/components/ui/ui'
import { ResponsiveModal } from '@/components/ui/modal'
import { createPeriod, updatePeriod } from '../api/endpoints'
import type { ElPeriod } from '../api/types'

// THE ZÚČTOVACÍ OBDOBÍ FORM.
//
// `ends_on` defaults to starts_on + 1 rok − 1 den and stays UNCONFIRMED until the
// supplier states the real date (D153). That is not a placeholder — it is the
// honest state of the world for most of a period's life, and the UI badges it
// "předpokládaný konec" everywhere the date appears. Correcting it later is this
// one field, after which every forecast figure follows and no actual figure moves.
//
// NOTHING HERE LOCKS ANYTHING (D139). The vyúčtování fields are five optional
// numbers that add a comparison line; recording them does not close, freeze or
// archive the period, and there is deliberately no control that would.
//
// The five are ALWAYS SENT, as a value or as an explicit null — never omitted.
// The server tells "field absent" from "field set to null" by key presence, and
// only the null can clear a vyúčtování that was typed in wrong.

export function PeriodDialog({
  open,
  onOpenChange,
  period,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  period: ElPeriod | null
}) {
  const qc = useQueryClient()
  const [startsOn, setStartsOn] = useState('')
  const [endsOn, setEndsOn] = useState('')
  const [confirmed, setConfirmed] = useState(false)
  const [invTotal, setInvTotal] = useState('')
  const [invBalance, setInvBalance] = useState('')
  const [invVT, setInvVT] = useState('')
  const [invNT, setInvNT] = useState('')
  const [invAt, setInvAt] = useState('')
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!open) return
    setError(null)
    if (period) {
      setStartsOn(period.starts_on)
      setEndsOn(period.ends_on)
      setConfirmed(period.ends_on_confirmed)
      setInvTotal(kcInput(period.invoiced_total_haler))
      setInvBalance(kcInput(period.invoiced_balance_haler))
      setInvVT(kwhInput(period.invoiced_vt_dkwh))
      setInvNT(kwhInput(period.invoiced_nt_dkwh))
      setInvAt(period.invoiced_at ?? '')
      return
    }
    setStartsOn(todayISO())
    setEndsOn('')
    setConfirmed(false)
    setInvTotal('')
    setInvBalance('')
    setInvVT('')
    setInvNT('')
    setInvAt('')
  }, [open, period])

  const save = useMutation({
    mutationFn: async () => {
      const body = {
        starts_on: startsOn,
        // An explicit null makes the SERVER apply the +1 year − 1 day default —
        // on edit as well as create — so there is one implementation of that
        // rule rather than two that could drift.
        ends_on: endsOn ? endsOn : null,
        ends_on_confirmed: confirmed,
        invoiced_total_haler: toHaler(invTotal),
        invoiced_balance_haler: toHaler(invBalance),
        invoiced_vt_dkwh: toDkwh(invVT),
        invoiced_nt_dkwh: toDkwh(invNT),
        invoiced_at: invAt ? invAt : null,
      }
      return period ? updatePeriod(period.id, body) : createPeriod(body)
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.electricityAll })
      onOpenChange(false)
    },
    onError: (e: unknown) => {
      // The overlap refusal (422) NAMES the period it collides with, which is the
      // only way to fix it — so it belongs in the form, not in a toast.
      if (e instanceof ApiError && (e.status === 422 || e.status === 409)) {
        setError(e.detail ?? e.message)
        return
      }
      toast.error(cs.electricity.form.saveFailed)
    },
  })

  return (
    <ResponsiveModal
      open={open}
      onOpenChange={onOpenChange}
      title={period ? cs.electricity.period.editTitle : cs.electricity.period.formTitle}
      footer={
        <div className="flex gap-2">
          <Button variant="secondary" onClick={() => onOpenChange(false)} className="flex-1">
            {cs.electricity.form.cancel}
          </Button>
          <Button
            onClick={() => save.mutate()}
            disabled={startsOn === '' || save.isPending}
            className="flex-1"
          >
            {save.isPending ? cs.electricity.form.saving : cs.electricity.period.save}
          </Button>
        </div>
      }
    >
      <div className="space-y-4">
        <Field label={cs.electricity.period.startsOn}>
          <Input
            type="date"
            value={startsOn}
            onChange={(e) => setStartsOn(e.target.value)}
            className="h-12 text-[15px]"
          />
        </Field>

        <Field label={`${cs.electricity.period.endsOn} (${cs.electricity.form.optional})`}>
          <Input
            type="date"
            value={endsOn}
            onChange={(e) => setEndsOn(e.target.value)}
            className="h-12 text-[15px]"
          />
          <p className="mt-1 text-[11.5px] text-subtle text-pretty">
            {cs.electricity.period.endsOnHint}
          </p>
        </Field>

        <label className="flex items-center gap-2.5 rounded-lg border border-border bg-s2 px-3 py-2.5">
          <input
            type="checkbox"
            checked={confirmed}
            onChange={(e) => setConfirmed(e.target.checked)}
            className="h-4 w-4"
          />
          <span className="text-[13px] font-semibold">{cs.electricity.period.confirmed}</span>
        </label>

        {/* The vyúčtování. Optional, and the section says so — but without these
            fields there is no way to record what the supplier billed, and the
            computed-vs-invoiced comparison on Přehled can never appear at all.
            Storing THEIR final meter values, not just their money, is what lets
            a difference be attributed to kWh rather than only to Kč (D154). */}
        <div className="rounded-xl border border-border bg-s2 p-3">
          <h3 className="text-[13px] font-bold">{cs.electricity.period.invoiceTitle}</h3>
          <p className="mt-1 mb-3 text-[11.5px] text-subtle text-pretty">
            {cs.electricity.period.invoiceHint}
          </p>
          <div className="space-y-3">
            <div className="grid gap-3 sm:grid-cols-2">
              <Field label={cs.electricity.period.invoiceTotal}>
                <NumInput value={invTotal} onChange={setInvTotal} />
              </Field>
              <Field label={cs.electricity.period.invoiceBalance}>
                <NumInput value={invBalance} onChange={setInvBalance} signed />
              </Field>
            </div>
            <div className="grid gap-3 sm:grid-cols-2">
              <Field label={cs.electricity.period.invoiceVT}>
                <NumInput value={invVT} onChange={setInvVT} />
              </Field>
              <Field label={cs.electricity.period.invoiceNT}>
                <NumInput value={invNT} onChange={setInvNT} />
              </Field>
            </div>
            <Field label={cs.electricity.period.invoiceAt}>
              <Input
                type="date"
                value={invAt}
                onChange={(e) => setInvAt(e.target.value)}
                className="h-11 text-[14px]"
              />
            </Field>
          </div>
        </div>

        {error && (
          <p role="alert" className="rounded-lg border border-danger/40 bg-danger/10 px-3 py-2 text-[13px] text-pretty">
            {error}
          </p>
        )}
      </div>
    </ResponsiveModal>
  )
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <label className="block">
      <span className="mb-1.5 block text-[12.5px] font-semibold text-muted">{label}</span>
      {children}
    </label>
  )
}

/** NumInput takes whole units — koruny or kWh. `signed` allows the one leading
 *  minus a nedoplatek needs; every other figure here is non-negative. */
function NumInput({
  value,
  onChange,
  signed,
}: {
  value: string
  onChange: (v: string) => void
  signed?: boolean
}) {
  return (
    <Input
      inputMode={signed ? 'text' : 'numeric'}
      value={value}
      onChange={(e) => {
        const raw = e.target.value.replace(/[^\d-]/g, '')
        const neg = signed && raw.startsWith('-')
        onChange((neg ? '-' : '') + raw.replace(/-/g, ''))
      }}
      className="h-11 font-mono text-[15px] font-semibold tabular-nums"
      autoComplete="off"
    />
  )
}

/** An EMPTY field is a null, not a zero — that is what clears a vyúčtování
 *  entry back out, and 0 Kč vyúčtováno is a different claim from "not billed
 *  yet". A lone "-" parses to NaN and is treated as empty. */
function toHaler(v: string): number | null {
  const n = Number(v)
  return v.trim() === '' || !Number.isFinite(n) ? null : Math.round(n * 100)
}

function toDkwh(v: string): number | null {
  const n = Number(v)
  return v.trim() === '' || !Number.isFinite(n) ? null : Math.round(n * 10)
}

function kcInput(haler: number | null): string {
  return haler === null ? '' : String(Math.round(haler / 100))
}

function kwhInput(dkwh: number | null): string {
  return dkwh === null ? '' : String(dkwh / 10)
}
