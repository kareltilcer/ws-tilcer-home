import { useEffect, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { qk } from '@/api/keys'
import { cs } from '@/i18n/cs'
import { todayISO } from '@/i18n/format'
import { ApiError } from '@/api/client'
import { Button, Input } from '@/components/ui/ui'
import { ResponsiveModal } from '@/components/ui/modal'
import { createPayment, updatePayment } from '../api/endpoints'
import type { ElPayment } from '../api/types'

// THE ZAPLACENÁ ZÁLOHA FORM — one optional row per month.
//
// This is the D144 escape hatch, and without it the whole electricity_payments
// table is unreachable: the předpis would be the only thing that could ever set
// a counted month's amount, and a month where a different sum actually left the
// account could never be corrected. It is also the fix HANDOFF-10 §9 prescribes
// for a záloha paid in a month the period does not count — record it as a row
// against a month that IS counted, rather than bending D145's counting rule.
//
// ⚠ ATTRIBUTION IS BY `month`, NEVER BY `paid_on`. A March záloha paid on
// 2. dubna still belongs to March; `paid_on` records when the money left and
// decides nothing. That is why the month field is the required one and the date
// is optional.

export function PaymentDialog({
  open,
  onOpenChange,
  payment,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  payment: ElPayment | null
}) {
  const qc = useQueryClient()
  const [month, setMonth] = useState('')
  const [amount, setAmount] = useState('')
  const [paidOn, setPaidOn] = useState('')
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!open) return
    setError(null)
    if (payment) {
      setMonth(payment.month)
      setAmount(String(Math.round(payment.amount_haler / 100)))
      setPaidOn(payment.paid_on ?? '')
      return
    }
    setMonth(todayISO().slice(0, 7))
    setAmount('')
    setPaidOn('')
  }, [open, payment])

  const save = useMutation({
    mutationFn: async () => {
      const body = {
        month,
        // Zálohy are whole koruny, like the předpis they stand in for.
        amount_haler: Math.round(Number(amount || 0) * 100),
        // Always sent, so a mistyped date can be cleared back to null: the
        // server tells "omitted" from "set to null" by key presence.
        paid_on: paidOn ? paidOn : null,
      }
      return payment ? updatePayment(payment.id, body) : createPayment(body)
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.electricityAll })
      onOpenChange(false)
    },
    onError: (e: unknown) => {
      // 409 names the month that already has a row, which is the only way to
      // fix it — so it belongs in the form, not in a toast.
      if (e instanceof ApiError && (e.status === 422 || e.status === 409)) {
        setError(e.detail ?? e.message)
        return
      }
      toast.error(cs.electricity.form.saveFailed)
    },
  })

  const valid = /^\d{4}-\d{2}$/.test(month) && amount.trim() !== ''

  return (
    <ResponsiveModal
      open={open}
      onOpenChange={onOpenChange}
      title={payment ? cs.electricity.form.edit : cs.electricity.payment.title}
      footer={
        <div className="flex gap-2">
          <Button variant="secondary" onClick={() => onOpenChange(false)} className="flex-1">
            {cs.electricity.form.cancel}
          </Button>
          <Button onClick={() => save.mutate()} disabled={!valid || save.isPending} className="flex-1">
            {save.isPending ? cs.electricity.form.saving : cs.electricity.payment.save}
          </Button>
        </div>
      }
    >
      <div className="space-y-4">
        <Field label={cs.electricity.payment.month}>
          <Input
            type="month"
            value={month}
            onChange={(e) => setMonth(e.target.value)}
            className="h-12 text-[15px]"
          />
        </Field>

        <Field label={cs.electricity.payment.amount}>
          <Input
            inputMode="numeric"
            pattern="[0-9]*"
            value={amount}
            onChange={(e) => setAmount(e.target.value.replace(/[^\d]/g, ''))}
            className="h-12 font-mono text-[17px] font-semibold tabular-nums"
            autoComplete="off"
          />
        </Field>

        <Field label={`${cs.electricity.payment.paidOn} (${cs.electricity.form.optional})`}>
          <Input
            type="date"
            value={paidOn}
            onChange={(e) => setPaidOn(e.target.value)}
            className="h-12 text-[15px]"
          />
        </Field>

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
