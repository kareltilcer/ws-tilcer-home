import { useEffect, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { qk } from '@/api/keys'
import { cs } from '@/i18n/cs'
import { todayISO } from '@/i18n/format'
import { ApiError } from '@/api/client'
import { Button, Field, FormError, Input } from '@/components/ui/ui'
import { ResponsiveModal } from '@/components/ui/modal'
import { createTariff, updateTariff } from '../api/endpoints'
import type { ElTariff } from '../api/types'

// THE CENÍK FORM — three numbers, entered exactly as the supplier printed them.
//
// The inputs take KORUNY WITH HALÉŘE (4858,65) because that is what is on the
// contract; the wire carries haléře, so the form converts. Unlike the odečet
// form, a decimal separator is not just allowed here — it is the point.
//
// All three are entered S DPH A VČETNĚ DISTRIBUCE and used exactly as typed
// (D135). The itemized alternative — silová, distribuce, jistič, systémové
// služby, POZE, OTE, daň, computed DPH — was specified in full and rejected:
// ten fields to re-type every January would be ten chances to mistype, in
// exchange for an itemization the household never reads. The hint says so, so
// nobody wonders where the VAT field went.

export function TariffDialog({
  open,
  onOpenChange,
  tariff,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  tariff: ElTariff | null
}) {
  const qc = useQueryClient()
  const [from, setFrom] = useState('')
  const [vt, setVt] = useState('')
  const [nt, setNt] = useState('')
  const [fee, setFee] = useState('')
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!open) return
    setError(null)
    if (tariff) {
      setFrom(tariff.effective_from)
      setVt(halerToInput(tariff.price_vt_haler))
      setNt(halerToInput(tariff.price_nt_haler))
      setFee(halerToInput(tariff.monthly_fee_haler))
      return
    }
    setFrom(todayISO())
    setVt('')
    setNt('')
    setFee('')
  }, [open, tariff])

  const save = useMutation({
    mutationFn: async () => {
      const body = {
        effective_from: from,
        price_vt_haler: inputToHaler(vt),
        price_nt_haler: inputToHaler(nt),
        monthly_fee_haler: inputToHaler(fee),
      }
      return tariff ? updateTariff(tariff.id, body) : createTariff(body)
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.electricityAll })
      onOpenChange(false)
    },
    onError: (e: unknown) => {
      if (e instanceof ApiError && (e.status === 422 || e.status === 409)) {
        setError(e.detail ?? e.message)
        return
      }
      toast.error(cs.electricity.form.saveFailed)
    },
  })

  // A lone separator must not reach the wire. `moneyChars` keeps it, `Number(",")`
  // is NaN, and JSON.stringify turns NaN into null — which the PATCH path reads as
  // "field omitted" and leaves the old price standing while the dialog reports
  // success. Same guard as the odečet form's, for the same reason.
  const filled = (v: string) => v.trim() !== '' && Number.isFinite(moneyNumber(v))
  const valid = from !== '' && filled(vt) && filled(nt) && filled(fee)

  return (
    <ResponsiveModal
      open={open}
      onOpenChange={onOpenChange}
      title={tariff ? cs.electricity.tariff.editTitle : cs.electricity.tariff.formTitle}
      footer={
        <div className="flex gap-2">
          <Button variant="secondary" onClick={() => onOpenChange(false)} className="flex-1">
            {cs.electricity.form.cancel}
          </Button>
          <Button onClick={() => save.mutate()} disabled={!valid || save.isPending} className="flex-1">
            {save.isPending ? cs.electricity.form.saving : cs.electricity.tariff.save}
          </Button>
        </div>
      }
    >
      <div className="space-y-4">
        <Field label={cs.electricity.tariff.effectiveFrom}>
          <Input
            type="date"
            value={from}
            onChange={(e) => setFrom(e.target.value)}
            className="h-12 text-[15px]"
          />
          <p className="mt-1 text-[11.5px] text-subtle text-pretty">
            {cs.electricity.tariff.futureNote}
          </p>
        </Field>

        <div className="grid gap-4 sm:grid-cols-2">
          <Field label={cs.electricity.tariff.priceVT}>
            <MoneyInput value={vt} onChange={setVt} />
          </Field>
          <Field label={cs.electricity.tariff.priceNT}>
            <MoneyInput value={nt} onChange={setNt} />
          </Field>
        </div>
        <Field label={cs.electricity.tariff.fee}>
          <MoneyInput value={fee} onChange={setFee} />
        </Field>

        <p className="rounded-lg bg-s2 px-3 py-2 text-[12px] text-muted text-pretty">
          {cs.electricity.tariff.withVAT}
        </p>

        {error && (
          <FormError>{error}</FormError>
        )}
      </div>
    </ResponsiveModal>
  )
}

function MoneyInput({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  return (
    <Input
      inputMode="decimal"
      value={value}
      onChange={(e) => onChange(moneyChars(e.target.value))}
      className="h-12 font-mono text-[17px] font-semibold tabular-nums"
      autoComplete="off"
    />
  )
}

/** moneyChars accepts a separator in EITHER convention — a Czech keyboard offers
 *  a comma, a numeric keypad offers a dot, and the user should not have to care
 *  which one they got — and normalises the field to the CZECH comma, so what is
 *  on screen matches how the number is written everywhere else in the app and
 *  does not flip separator on the first keystroke after an edit opens. */
function moneyChars(v: string): string {
  const cleaned = v.replace(/[^\d.,]/g, '').replace(/\./g, ',')
  const [whole, ...rest] = cleaned.split(',')
  return rest.length ? `${whole},${rest.join('').slice(0, 2)}` : whole
}

/** moneyNumber parses the field, or NaN when it holds no number (a lone comma). */
function moneyNumber(v: string): number {
  return Number(v.replace(',', '.'))
}

/** inputToHaler converts "4858.65" into 485865 haléře. Rounding here is the
 *  ordinary decimal-string case, not the money path — nothing computed passes
 *  through it. */
function inputToHaler(v: string): number {
  const n = Number(v.replace(',', '.') || 0)
  return Math.round(n * 100)
}

function halerToInput(haler: number): string {
  return (haler / 100).toFixed(2).replace('.', ',')
}
