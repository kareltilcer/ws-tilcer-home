import { useEffect, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { qk } from '@/api/keys'
import { cs } from '@/i18n/cs'
import { todayISO } from '@/i18n/format'
import { ApiError } from '@/api/client'
import { Button, Input } from '@/components/ui/ui'
import { ResponsiveModal } from '@/components/ui/modal'
import { createAdvance, updateAdvance } from '../api/endpoints'
import type { ElAdvance } from '../api/types'

// THE ZÁLOHA SCHEDULE FORM.
//
// `due_day` is a plain 1–31 and is stored RAW: the clamp to a short month's last
// day happens at READ time (D155). The hint explains what a 31 does in February,
// because that is the one thing about this field a person would otherwise
// discover by being surprised — and it is read in exactly one place, so it moves
// the doporučená záloha and nothing else.

export function AdvanceDialog({
  open,
  onOpenChange,
  advance,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  advance: ElAdvance | null
}) {
  const qc = useQueryClient()
  const [from, setFrom] = useState('')
  const [amount, setAmount] = useState('')
  const [dueDay, setDueDay] = useState('15')
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!open) return
    setError(null)
    if (advance) {
      setFrom(advance.effective_from)
      setAmount(String(Math.round(advance.amount_haler / 100)))
      setDueDay(String(advance.due_day))
      return
    }
    setFrom(todayISO())
    setAmount('')
    setDueDay('15')
  }, [open, advance])

  const save = useMutation({
    mutationFn: async () => {
      const body = {
        effective_from: from,
        // Zálohy are always whole koruny — nobody is prescribed 1 500,37 Kč a
        // month — so this field, unlike the ceník's, takes no decimal.
        amount_haler: Math.round(Number(amount || 0) * 100),
        due_day: Number(dueDay || 0),
      }
      return advance ? updateAdvance(advance.id, body) : createAdvance(body)
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

  const dayNum = Number(dueDay)
  const valid = from !== '' && amount.trim() !== '' && dayNum >= 1 && dayNum <= 31

  return (
    <ResponsiveModal
      open={open}
      onOpenChange={onOpenChange}
      title={advance ? cs.electricity.advance.editTitle : cs.electricity.advance.formTitle}
      footer={
        <div className="flex gap-2">
          <Button variant="secondary" onClick={() => onOpenChange(false)} className="flex-1">
            {cs.electricity.form.cancel}
          </Button>
          <Button onClick={() => save.mutate()} disabled={!valid || save.isPending} className="flex-1">
            {save.isPending ? cs.electricity.form.saving : cs.electricity.advance.save}
          </Button>
        </div>
      }
    >
      <div className="space-y-4">
        <Field label={cs.electricity.advance.effectiveFrom}>
          <Input
            type="date"
            value={from}
            onChange={(e) => setFrom(e.target.value)}
            className="h-12 text-[15px]"
          />
        </Field>

        <div className="grid gap-4 sm:grid-cols-2">
          <Field label={cs.electricity.advance.amount}>
            <Input
              inputMode="numeric"
              pattern="[0-9]*"
              value={amount}
              onChange={(e) => setAmount(e.target.value.replace(/[^\d]/g, ''))}
              className="h-12 font-mono text-[17px] font-semibold tabular-nums"
              autoComplete="off"
            />
          </Field>
          <Field label={cs.electricity.advance.dueDay}>
            <Input
              inputMode="numeric"
              pattern="[0-9]*"
              min={1}
              max={31}
              value={dueDay}
              onChange={(e) => setDueDay(e.target.value.replace(/[^\d]/g, '').slice(0, 2))}
              className="h-12 font-mono text-[17px] font-semibold tabular-nums"
              autoComplete="off"
            />
          </Field>
        </div>
        <p className="text-[12px] text-subtle text-pretty">{cs.electricity.advance.dueDayHint}</p>

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
