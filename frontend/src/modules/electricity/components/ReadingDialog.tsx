import { useEffect, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { qk } from '@/api/keys'
import { cs } from '@/i18n/cs'
import { todayISO } from '@/i18n/format'
import { ApiError } from '@/api/client'
import { Button, Input, Textarea } from '@/components/ui/ui'
import { ResponsiveModal } from '@/components/ui/modal'
import { createReading, updateReading } from '../api/endpoints'
import type { ElReading } from '../api/types'

// THE ODEČET FORM — four fields, and the whole product.
//
// This is the only thing standing between the module and neglect: nothing will
// ever remind the household to open it, so entering a reading has to take about
// fifteen seconds, ONE-HANDED, at a meter cupboard, in bad light, possibly with
// no signal. Every decision below serves that:
//
//   - WHOLE kWh in practice. Karel's meter has no decimal (OQ-V8-1), so the
//     input is step=1 inputMode=numeric and nobody will ever type a separator.
//     The wire carries tenths (*_dkwh), so the form multiplies by 10 on submit
//     and divides for display — and it ACCEPTS one tenth rather than erasing it,
//     because a field that strips the separator turns a loaded 1280,5 into 12805
//     on the first keystroke and submits ten times the reading.
//   - The two register fields are LARGE and WELL SEPARATED, because hitting the
//     wrong one while holding a phone at arm's length is the likely error and it
//     produces a wrong bill rather than an obvious mistake.
//   - The date DEFAULTS TO TODAY, since that is what it is nine times in ten.
//   - No scroll to save: the footer is pinned by the modal.
//
// The PRE-FILLED variant arrives from the hard block on Přehled: the date is
// filled in and the VALUES ARE LEFT EMPTY, always (D137). Estimating a meter
// reading is the one thing this module refuses to do, and pre-filling a plausible
// value would be exactly that, wearing a helpful face.

export function ReadingDialog({
  open,
  onOpenChange,
  reading,
  prefillDate,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  reading: ElReading | null
  prefillDate?: string
}) {
  const qc = useQueryClient()
  const [readOn, setReadOn] = useState('')
  const [vt, setVt] = useState('')
  const [nt, setNt] = useState('')
  const [note, setNote] = useState('')
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!open) return
    setError(null)
    if (reading) {
      setReadOn(reading.read_on)
      setVt(dkwhToInput(reading.vt_dkwh))
      setNt(dkwhToInput(reading.nt_dkwh))
      setNote(reading.note ?? '')
      return
    }
    // A fresh form: the block's date if we came from there, else today. The
    // values stay EMPTY either way.
    setReadOn(prefillDate ?? todayISO())
    setVt('')
    setNt('')
    setNote('')
  }, [open, reading, prefillDate])

  const save = useMutation({
    mutationFn: async () => {
      const body = {
        read_on: readOn,
        vt_dkwh: inputToDkwh(vt),
        nt_dkwh: inputToDkwh(nt),
        note: note.trim() ? note.trim() : null,
      }
      return reading ? updateReading(reading.id, body) : createReading(body)
    },
    onSuccess: () => {
      // ONE invalidation for the whole module: the summary, the intervals and the
      // history are three views of the same computation, and a fresh reading list
      // beside a stale headline is a number that has quietly stopped being true.
      void qc.invalidateQueries({ queryKey: qk.electricityAll })
      onOpenChange(false)
    },
    onError: (e: unknown) => {
      // The monotonicity refusal (422) NAMES the neighbouring reading and its
      // value, so it belongs in the form beside the field the user must fix —
      // not in a toast that disappears while they are re-reading the meter.
      if (e instanceof ApiError && (e.status === 422 || e.status === 409)) {
        setError(e.detail ?? e.message)
        return
      }
      toast.error(cs.electricity.form.saveFailed)
    },
  })

  // A lone separator parses to NaN, which must not reach the wire as 0.
  const filled = (v: string) => v.trim() !== '' && Number.isFinite(kwhNumber(v))
  const valid = readOn !== '' && filled(vt) && filled(nt)

  return (
    <ResponsiveModal
      open={open}
      onOpenChange={onOpenChange}
      title={reading ? cs.electricity.reading.editTitle : cs.electricity.reading.formTitle}
      footer={
        <div className="flex gap-2">
          <Button variant="secondary" onClick={() => onOpenChange(false)} className="flex-1">
            {cs.electricity.form.cancel}
          </Button>
          <Button
            onClick={() => save.mutate()}
            disabled={!valid || save.isPending}
            className="flex-1"
          >
            {save.isPending ? cs.electricity.form.saving : cs.electricity.reading.save}
          </Button>
        </div>
      }
    >
      <div className="space-y-4">
        {prefillDate && !reading && (
          <p className="rounded-lg border border-el-blocked/40 bg-el-blocked-soft px-3 py-2 text-[12.5px] text-pretty">
            {cs.electricity.blocked.prefill}
          </p>
        )}

        <Field label={cs.electricity.reading.date}>
          <Input
            type="date"
            value={readOn}
            onChange={(e) => setReadOn(e.target.value)}
            className="h-12 text-[15px]"
          />
        </Field>

        {/* The two registers, deliberately far apart and oversized. The mono,
            tabular figures match how the digits look on the meter itself. */}
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label={cs.electricity.reading.vt}>
            <Input
              inputMode="numeric"
              pattern="[0-9]*[.,]?[0-9]*"
              step={1}
              min={0}
              value={vt}
              onChange={(e) => setVt(kwhChars(e.target.value))}
              className="h-14 font-mono text-[22px] font-semibold tabular-nums"
              autoComplete="off"
            />
          </Field>
          <Field label={cs.electricity.reading.nt}>
            <Input
              inputMode="numeric"
              pattern="[0-9]*[.,]?[0-9]*"
              step={1}
              min={0}
              value={nt}
              onChange={(e) => setNt(kwhChars(e.target.value))}
              className="h-14 font-mono text-[22px] font-semibold tabular-nums"
              autoComplete="off"
            />
          </Field>
        </div>
        <p className="text-[12px] text-subtle text-pretty">{cs.electricity.reading.hint}</p>

        <Field label={cs.electricity.reading.note}>
          <Textarea
            rows={2}
            value={note}
            onChange={(e) => setNote(e.target.value)}
            placeholder={cs.electricity.reading.notePlaceholder}
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

/** kwhChars keeps the field to whole kWh plus AT MOST ONE TENTH, accepting a
 *  separator in either convention and normalising it to the Czech comma.
 *
 *  ⚠ It deliberately does not strip the separator outright. A field that erases
 *  it turns a loaded 1280,5 into 12805 on the first keystroke, and the form then
 *  submits 128050 dkWh — a tenfold reading that is still increasing, so the
 *  monotonicity guard passes it and every figure in the module inflates
 *  silently. Karel's meter has no decimal, so nobody will type the comma; the
 *  point is that a value which HAS one survives being edited. */
function kwhChars(v: string): string {
  const cleaned = v.replace(/[^\d.,]/g, '').replace(/\./g, ',')
  const [whole, ...rest] = cleaned.split(',')
  return rest.length ? `${whole},${rest.join('').slice(0, 1)}` : whole
}

/** kwhNumber parses the field, or NaN when it holds no number (a lone comma). */
function kwhNumber(v: string): number {
  return Number(v.replace(',', '.'))
}

/** inputToDkwh converts the form's kWh into the wire's tenths. */
function inputToDkwh(v: string): number {
  return Math.round(kwhNumber(v) * 10)
}

/** dkwhToInput shows whole kWh, and keeps a tenth if one is present — so a value
 *  from a future decimal meter is visible rather than truncated on the way into
 *  an edit. The comma is what `kwhChars` above preserves. */
function dkwhToInput(dkwh: number): string {
  return dkwh % 10 === 0 ? String(dkwh / 10) : String(dkwh / 10).replace('.', ',')
}
