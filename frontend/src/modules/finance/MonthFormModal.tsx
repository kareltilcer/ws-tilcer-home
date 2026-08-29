import { useMemo, useState } from 'react'
import { AlertCircle } from 'lucide-react'
import type { FinanceMonth, FinanceRates } from '@/api/types'
import { cs } from '@/i18n/cs'
import { monthLabel } from '@/i18n/format'
import { Button, Input } from '@/components/ui/ui'
import { ResponsiveModal } from '@/components/ui/modal'
import { cn } from '@/lib/utils'
import { BUCKETS, displayNeeds, fmtMoney } from './buckets'
import { AllocationBar, BucketSwatch } from './AllocationBar'
import { computeSplit, ratesSum } from './split'

// The default rates for the very first month ever recorded (FR-F6). After that
// the form pre-fills from the most recent saved month — a pure frontend
// behaviour, carried over from `fin`; there is no endpoint for it.
const FALLBACK_RATES: FinanceRates = { personal: 20, operational: 60, fun: 10, no_fun: 10 }

/** monthOptions lists every month from two years back through next month, so a
 *  forgotten month from last year is still reachable. Months already recorded are
 *  DISABLED rather than hidden: seeing "už zadaný" beside one answers "did I
 *  enter it?" without leaving the form.
 *
 *  `include` is the month being EDITED. The list only ever shrinks from the far
 *  end while the recorded history does not, so an old row would eventually fall
 *  outside the window and leave the select matching no option at all — showing an
 *  empty field for a month that is in fact set. The row's own month is always in
 *  the list, however old it is. */
function monthOptions(now: Date, include?: string): string[] {
  const out: string[] = []
  // Newest first, so the month someone is most likely to be entering is at the top.
  for (let i = 1; i >= -35; i--) {
    const d = new Date(now.getFullYear(), now.getMonth() + i, 1)
    out.push(`${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`)
  }
  if (include && !out.includes(include)) {
    out.push(include)
    out.sort((a, b) => b.localeCompare(a))
  }
  return out
}

// Money and rates are WHOLE integers end to end — the Go types are `int`, so a
// float on the wire is a decode error, not a validation message. Parse the Czech
// way — spaces group, a comma is the decimal mark — and round to a whole number
// here, so the preview shows exactly what will be saved.
//
// Anything else is null, NOT 0. Falling back to 0 was silent corruption: 0 is a
// legitimate income (one person can earn nothing in a month), so nothing
// downstream — not the submit gate, not the server's validateIncome — would
// have caught it. A dot is the sharp edge: "60.000" is sixty thousand written
// with a thousands separator, and Number() reads it as sixty.
const num = (v: string): number | null => {
  const cleaned = v.replace(/\s/g, '')
  if (cleaned === '') return 0
  if (!/^\d+(,\d+)?$/.test(cleaned)) return null
  return Math.round(Number(cleaned.replace(',', '.')))
}

export function MonthFormModal({
  open,
  onOpenChange,
  editing,
  months,
  defaultMonth,
  saving,
  error,
  onSubmit,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** null = add, a month = edit. */
  editing: FinanceMonth | null
  /** Every recorded month, newest first — for the disabled options and the rate pre-fill. */
  months: FinanceMonth[]
  defaultMonth: string
  saving: boolean
  error: string | null
  onSubmit: (values: { month: string; income_kaja: number; income_andy: number; rates: FinanceRates }) => void
}) {
  const latest = months[0]
  const initial = editing ?? {
    month: defaultMonth,
    income_kaja: '',
    income_andy: '',
    rates: latest?.rates ?? FALLBACK_RATES,
  }

  const [month, setMonth] = useState(initial.month)
  const [kaja, setKaja] = useState(String(editing?.income_kaja ?? ''))
  const [andy, setAndy] = useState(String(editing?.income_andy ?? ''))
  const [rates, setRates] = useState<FinanceRates>(initial.rates)

  const sum = ratesSum(rates)
  const remainder = 100 - sum
  const balanced = remainder === 0

  // An unreadable income blocks submit rather than saving a number nobody typed.
  // The rate fields need no equivalent: they are already gated on summing to
  // exactly 100, which an unreadable one cannot do.
  const kajaNum = num(kaja)
  const andyNum = num(andy)
  const incomesOk = kajaNum !== null && andyNum !== null

  const preview = useMemo(() => computeSplit(num(kaja) ?? 0, num(andy) ?? 0, rates), [kaja, andy, rates])

  const taken = useMemo(() => {
    const s = new Set(months.map((m) => m.month))
    if (editing) s.delete(editing.month)
    return s
  }, [months, editing])

  const options = useMemo(() => monthOptions(new Date(), editing?.month), [editing])

  const setRate = (key: keyof FinanceRates, v: string) => setRates((r) => ({ ...r, [key]: num(v) ?? 0 }))

  // Auto-balance is a convenience, not a rule: it pours whatever is left into
  // no-fun savings, which is the bucket people adjust last anyway.
  const balance = () =>
    setRates((r) => ({ ...r, no_fun: Math.max(0, 100 - (r.personal + r.operational + r.fun)) }))

  const rateFields: { key: keyof FinanceRates; label: string; bucket: (typeof BUCKETS)[number] }[] = [
    { key: 'personal', label: cs.finance.ratePersonal, bucket: BUCKETS[0] },
    { key: 'operational', label: cs.finance.rateOperational, bucket: BUCKETS[1] },
    { key: 'fun', label: cs.finance.rateFun, bucket: BUCKETS[2] },
    { key: 'no_fun', label: cs.finance.rateNoFun, bucket: BUCKETS[3] },
  ]

  const previewRows = [
    { mark: BUCKETS[0].mark, label: `${cs.finance.personal} — Kája / Andy`, value: `${fmtMoney(preview.personal_kaja)} / ${fmtMoney(preview.personal_andy)}` },
    // The qualified label, matching MonthFlow: this is what STAYS in Kandy after
    // the savings leave, not what the account received.
    { mark: BUCKETS[1].mark, label: cs.finance.operationalNeeds, value: fmtMoney(displayNeeds(preview.needs)) },
    { mark: BUCKETS[2].mark, label: cs.finance.funSavings, value: fmtMoney(preview.fun_savings) },
    { mark: BUCKETS[3].mark, label: cs.finance.noFunSavings, value: fmtMoney(preview.no_fun_savings) },
  ]

  return (
    <ResponsiveModal
      open={open}
      onOpenChange={onOpenChange}
      title={
        <span>
          <span className="block">{editing ? cs.finance.edit : cs.finance.add}</span>
          <span className="block text-[12px] font-normal text-muted">{cs.finance.formLede}</span>
        </span>
      }
      footer={
        <div className="flex w-full items-center gap-2">
          {!balanced ? (
            <span className="mr-auto max-w-[220px] text-[12px] text-pretty text-muted">{cs.finance.ratesMustSum}</span>
          ) : !incomesOk ? (
            <span className="mr-auto max-w-[220px] text-[12px] text-pretty text-muted">{cs.finance.incomeInvalid}</span>
          ) : null}
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            {cs.finance.cancel}
          </Button>
          <Button
            variant="primary"
            loading={saving}
            // Submit stays blocked until the rates total exactly 100 and both
            // incomes read as numbers. The chip above has been saying how far off
            // the rates are the whole time and the income hint names the other
            // case, so this is never a silent refusal.
            disabled={!balanced || !incomesOk || saving}
            onClick={() =>
              onSubmit({ month, income_kaja: kajaNum ?? 0, income_andy: andyNum ?? 0, rates })
            }
          >
            {editing ? cs.finance.save : cs.finance.add}
          </Button>
        </div>
      }
    >
      <div className="space-y-4">
        <label className="block">
          <span className="mb-1.5 block text-[12.5px] font-semibold">{cs.finance.month}</span>
          <select
            value={month}
            onChange={(e) => setMonth(e.target.value)}
            className="h-11 w-full rounded-md border border-border bg-s3 px-2.5 text-sm text-fg focus-visible:outline-2 focus-visible:outline-focus"
          >
            {options.map((ym) => (
              <option key={ym} value={ym} disabled={taken.has(ym)}>
                {monthLabel(ym)}
                {taken.has(ym) ? ` ${cs.finance.monthTaken}` : ''}
              </option>
            ))}
          </select>
          {!editing && <span className="mt-1.5 block text-[11.5px] text-subtle">{cs.finance.monthTakenHint}</span>}
        </label>

        <div className="grid grid-cols-2 gap-3">
          <label className="block">
            <span className="mb-1.5 block text-[12.5px] font-semibold">{cs.finance.incomeKaja}</span>
            <Input
              value={kaja}
              onChange={(e) => setKaja(e.target.value)}
              inputMode="numeric"
              placeholder="0"
              className="h-11 bg-s3 text-right font-mono text-[15px] tabular-nums"
            />
          </label>
          <label className="block">
            <span className="mb-1.5 block text-[12.5px] font-semibold">{cs.finance.incomeAndy}</span>
            <Input
              value={andy}
              onChange={(e) => setAndy(e.target.value)}
              inputMode="numeric"
              placeholder="0"
              className="h-11 bg-s3 text-right font-mono text-[15px] tabular-nums"
            />
          </label>
          {!incomesOk && (
            <p className="col-span-2 text-[11.5px] text-pretty text-warn">{cs.finance.incomeInvalid}</p>
          )}
        </div>

        <div>
          <div className="mb-2 flex flex-wrap items-center gap-2.5">
            <span className="text-[12.5px] font-semibold">{cs.finance.rates}</span>
            {/* The running total, always visible: assistance, not a slap. */}
            <span
              className={cn(
                'inline-flex h-[30px] items-center gap-2 rounded-full border px-3 font-mono text-[11.5px] font-semibold',
                balanced
                  ? 'border-good/40 bg-good/15 text-good'
                  : 'border-warn/45 bg-warn/15 text-warn',
              )}
            >
              {sum} %{' · '}
              {balanced
                ? cs.finance.ratesOk
                : remainder > 0
                  ? cs.finance.ratesRemaining(remainder)
                  : cs.finance.ratesOver(-remainder)}
            </span>
            {!balanced && (
              <Button size="sm" onClick={balance}>
                {cs.finance.ratesBalance}
              </Button>
            )}
          </div>
          <div className="grid grid-cols-2 gap-3">
            {rateFields.map((f) => (
              <label key={f.key} className="block">
                <span className="mb-1.5 flex items-center gap-2">
                  <BucketSwatch bucket={f.bucket} size={10} />
                  <span className="text-[12.5px] font-semibold">{f.label}</span>
                </span>
                <Input
                  value={String(rates[f.key])}
                  onChange={(e) => setRate(f.key, e.target.value)}
                  inputMode="numeric"
                  className="h-11 bg-s3 text-right font-mono text-[15px] tabular-nums"
                />
              </label>
            ))}
          </div>
        </div>

        {/* The live preview is what makes the form worth using: change a rate and
            the money moves. It renders from the frontend MIRROR of the formula —
            everything else on the screen comes from the server's split. */}
        <div className="rounded-xl border border-border bg-s2 p-3">
          <div className="mb-2 flex items-center gap-2.5">
            <span className="font-mono text-[10px] uppercase tracking-[0.06em] text-subtle">
              {cs.finance.previewTitle}
            </span>
            <span className="ml-auto font-mono text-[13.5px] tabular-nums">{fmtMoney(preview.total_income)}</span>
          </div>
          <AllocationBar split={preview} markMinPercent={13} className="mb-2.5" />
          <ul className="space-y-1">
            {previewRows.map((row) => (
              <li key={row.label} className="flex items-baseline gap-2 text-[12.5px]">
                <span className="font-mono text-[10px] font-bold text-subtle">{row.mark}</span>
                <span className="min-w-0 flex-1 truncate text-muted">{row.label}</span>
                <span className="font-mono tabular-nums">{row.value}</span>
              </li>
            ))}
          </ul>
          {preview.needs < 0 && (
            <p className="mt-2 text-[11.5px] text-pretty text-muted">
              {cs.finance.previewNegative(fmtMoney(preview.needs))}
            </p>
          )}
        </div>

        {error && (
          <p className="flex items-center gap-2.5 rounded-xl border border-danger/45 bg-danger/10 px-3 py-2.5 text-[13px]">
            <AlertCircle size={16} className="flex-none text-danger" aria-hidden />
            {error}
          </p>
        )}
      </div>
    </ResponsiveModal>
  )
}
