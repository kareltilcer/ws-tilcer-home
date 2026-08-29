import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Pencil, Plus, Trash2 } from 'lucide-react'
import { toast } from 'sonner'
import { qk } from '@/api/keys'
import { cs } from '@/i18n/cs'
import { PLURAL } from '@/i18n/plural'
import { fmtDateISO, monthLabel, todayISO } from '@/i18n/format'
import { useAuth } from '@/app/auth'
import { useOnline } from '@/platform/pwa/offline'
import { ApiError } from '@/api/client'
import { Button } from '@/components/ui/ui'
import { cn } from '@/lib/utils'
import {
  deleteAdvance,
  deletePayment,
  deletePeriod,
  deleteTariff,
  getSummary,
  listAdvances,
  listPayments,
  listPeriods,
  listTariffs,
} from '../api/endpoints'
import type { ElAdvance, ElPayment, ElPeriod, ElTariff } from '../api/types'
import { kc, kc2, plural } from '../format'
import { TariffDialog } from '../components/TariffDialog'
import { AdvanceDialog } from '../components/AdvanceDialog'
import { PaymentDialog } from '../components/PaymentDialog'
import { PeriodDialog } from '../components/PeriodDialog'

// CENÍKY A POPLATKY — the versions, and the záloha schedule that lives with them.
//
// A version's END IS DERIVED and never stored (D136), so this list is the ONLY
// place a person can see where one version stops and the next begins. That makes
// the validity range a first-class property of the row rather than something to
// infer from two dates in different cards.
//
// A FUTURE-DATED VERSION IS THE NORMAL CASE, not an edge case: the supplier
// announces January's prices in October, you type them in, and the forecast
// honours them immediately (D142). The row says so rather than treating it as
// exotic.
//
// The záloha schedule is here too, because this is the module's entire
// configuration surface — there is no settings screen and nothing else to
// configure (§3.5).

export function CenikTab({ periodId }: { periodId: string }) {
  const online = useOnline()
  const canWrite = useAuth().canWrite && online
  const qc = useQueryClient()
  const [tariffEdit, setTariffEdit] = useState<ElTariff | null>(null)
  const [tariffNew, setTariffNew] = useState(false)
  const [advanceEdit, setAdvanceEdit] = useState<ElAdvance | null>(null)
  const [advanceNew, setAdvanceNew] = useState(false)
  const [paymentEdit, setPaymentEdit] = useState<ElPayment | null>(null)
  const [paymentNew, setPaymentNew] = useState(false)
  const [periodEdit, setPeriodEdit] = useState<ElPeriod | null>(null)
  const [periodNew, setPeriodNew] = useState(false)

  const tariffs = useQuery({ queryKey: qk.electricityTariffs, queryFn: listTariffs })
  const advances = useQuery({ queryKey: qk.electricityAdvances, queryFn: listAdvances })
  const payments = useQuery({ queryKey: qk.electricityPayments, queryFn: listPayments })
  const periods = useQuery({ queryKey: qk.electricityPeriods, queryFn: listPeriods })
  const summary = useQuery({
    queryKey: qk.electricitySummary(periodId),
    queryFn: () => getSummary(periodId),
  })

  const removeTariff = useMutation({
    mutationFn: (id: string) => deleteTariff(id),
    onSuccess: () => void qc.invalidateQueries({ queryKey: qk.electricityAll }),
    onError: (e: unknown) => {
      // The 409 explains WHICH period would be stranded, so it is worth showing
      // in full rather than replacing with a generic failure.
      //
      // ⚠ NOT `apiErrorMessage(e, …)`, which every other site in the app now
      // uses. These two fall back to `e.message` — the bare error CODE — when an
      // ApiError carries no detail, where the helper falls back to the Czech
      // sentence. Swapping it in would stop showing `conflict` to a user, which
      // is a visible change and so not this refactor's to make.
      toast.error(e instanceof ApiError ? (e.detail ?? e.message) : cs.electricity.form.saveFailed)
    },
  })
  const removeAdvance = useMutation({
    mutationFn: (id: string) => deleteAdvance(id),
    onSuccess: () => void qc.invalidateQueries({ queryKey: qk.electricityAll }),
    onError: () => toast.error(cs.electricity.form.saveFailed),
  })
  const removePayment = useMutation({
    mutationFn: (id: string) => deletePayment(id),
    onSuccess: () => void qc.invalidateQueries({ queryKey: qk.electricityAll }),
    onError: () => toast.error(cs.electricity.form.saveFailed),
  })
  const removePeriod = useMutation({
    mutationFn: (id: string) => deletePeriod(id),
    onSuccess: () => void qc.invalidateQueries({ queryKey: qk.electricityAll }),
    onError: (e: unknown) => {
      // The same `e.message` fallback as removeTariff above, and the same reason
      // it is not `apiErrorMessage`.
      toast.error(e instanceof ApiError ? (e.detail ?? e.message) : cs.electricity.form.saveFailed)
    },
  })

  const tariffItems = tariffs.data?.items ?? []
  const advanceItems = advances.data?.items ?? []
  const paymentItems = payments.data?.items ?? []
  const periodItems = periods.data?.items ?? []
  const period = summary.data?.period
  const today = todayISO()

  return (
    <div className="space-y-5 pb-4">
      {/* ---- ceníky ---- */}
      <section>
        <div className="mb-2 flex flex-wrap items-center gap-2">
          <h2 className="text-[15px] font-bold">{cs.electricity.tariff.title}</h2>
          <Button size="sm" className="ml-auto" disabled={!canWrite} onClick={() => setTariffNew(true)}>
            <Plus size={15} aria-hidden />
            {cs.electricity.form.add}
          </Button>
        </div>
        <p className="mb-3 text-[12.5px] text-muted text-pretty">{cs.electricity.tariff.lede}</p>

        {tariffItems.length === 0 ? (
          <p className="rounded-xl border border-dashed border-border p-5 text-center text-[13px] text-muted">
            {cs.electricity.tariff.empty}
          </p>
        ) : (
          <div className="space-y-2">
            {tariffItems.map((t) => {
              // The server derives effective_to from the NEXT version, across
              // page boundaries too — the array alone cannot see past its end.
              const nextFrom = t.effective_to ? addDay(t.effective_to) : undefined
              return (
                <TariffCard
                  key={t.id}
                  tariff={t}
                  nextFrom={nextFrom}
                  isFuture={t.effective_from > today}
                  coversDays={coveredDays(t.effective_from, nextFrom, period)}
                  canWrite={canWrite}
                  onEdit={() => setTariffEdit(t)}
                  onDelete={() => {
                    if (window.confirm(cs.electricity.tariff.deleteConfirm)) removeTariff.mutate(t.id)
                  }}
                />
              )
            })}
          </div>
        )}
      </section>

      {/* ---- předpis záloh ---- */}
      <section>
        <div className="mb-2 flex flex-wrap items-center gap-2">
          <h2 className="text-[15px] font-bold">{cs.electricity.advance.title}</h2>
          <Button size="sm" className="ml-auto" disabled={!canWrite} onClick={() => setAdvanceNew(true)}>
            <Plus size={15} aria-hidden />
            {cs.electricity.form.add}
          </Button>
        </div>
        <p className="mb-3 text-[12.5px] text-muted text-pretty">{cs.electricity.advance.lede}</p>

        {advanceItems.length === 0 ? (
          <p className="rounded-xl border border-dashed border-border p-5 text-center text-[13px] text-muted">
            {cs.electricity.advance.empty}
          </p>
        ) : (
          <div className="space-y-2">
            {advanceItems.map((a) => (
              <article key={a.id} className="rounded-xl border border-border bg-s1 p-3">
                <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
                  <span className="text-[13.5px] font-bold">{fmtDateISO(a.effective_from)}</span>
                  <span className="font-mono text-[15px] font-semibold tabular-nums">
                    {kc(a.amount_haler)}
                  </span>
                  <span className="text-[12px] text-muted">
                    {cs.electricity.word.dueDay.toLowerCase()} {a.due_day}.
                  </span>
                  {canWrite && (
                    <div className="ml-auto flex gap-1">
                      <IconBtn label={cs.electricity.form.edit} onClick={() => setAdvanceEdit(a)}>
                        <Pencil size={14} aria-hidden />
                      </IconBtn>
                      <IconBtn
                        label={cs.electricity.form.delete}
                        onClick={() => {
                          if (window.confirm(cs.electricity.advance.deleteConfirm))
                            removeAdvance.mutate(a.id)
                        }}
                      >
                        <Trash2 size={14} aria-hidden />
                      </IconBtn>
                    </div>
                  )}
                </div>
                {/* The únor clamp, made visible rather than left to surprise
                    someone in February (D155). */}
                {a.due_day > 28 && (
                  <p className="mt-1 text-[11.5px] text-subtle text-pretty">
                    {cs.electricity.advance.dueDayHint}
                  </p>
                )}
              </article>
            ))}
          </div>
        )}
      </section>

      {/* ---- zaplacené zálohy ---- */}
      {/* D144's escape hatch, and it needs its own controls: without them the
          předpis is the only thing that can ever set a counted month's amount,
          and "Žádné platby nejsou zapsané" is permanent no matter what actually
          left the account. */}
      <section>
        <div className="mb-2 flex flex-wrap items-center gap-2">
          <h2 className="text-[15px] font-bold">{cs.electricity.payment.title}</h2>
          <Button size="sm" className="ml-auto" disabled={!canWrite} onClick={() => setPaymentNew(true)}>
            <Plus size={15} aria-hidden />
            {cs.electricity.form.add}
          </Button>
        </div>
        <p className="mb-3 text-[12.5px] text-muted text-pretty">{cs.electricity.payment.lede}</p>
        {paymentItems.length === 0 ? (
          <p className="rounded-xl border border-dashed border-border p-5 text-center text-[13px] text-muted">
            {cs.electricity.payment.empty}
          </p>
        ) : (
          <div className="space-y-2">
            {paymentItems.map((p) => (
              <div
                key={p.id}
                className="flex flex-wrap items-baseline gap-x-3 rounded-xl border border-border bg-s1 p-3 text-[13px]"
              >
                <span className="font-semibold">{monthLabel(p.month)}</span>
                <span className="font-mono font-semibold tabular-nums">{kc(p.amount_haler)}</span>
                {p.paid_on && (
                  <span className="text-[12px] text-subtle">{fmtDateISO(p.paid_on)}</span>
                )}
                {canWrite && (
                  <div className="ml-auto flex gap-1">
                    <IconBtn label={cs.electricity.form.edit} onClick={() => setPaymentEdit(p)}>
                      <Pencil size={14} aria-hidden />
                    </IconBtn>
                    <IconBtn
                      label={cs.electricity.form.delete}
                      onClick={() => {
                        if (window.confirm(cs.electricity.payment.deleteConfirm))
                          removePayment.mutate(p.id)
                      }}
                    >
                      <Trash2 size={14} aria-hidden />
                    </IconBtn>
                  </div>
                )}
              </div>
            ))}
          </div>
        )}
      </section>

      {/* ---- zúčtovací období ---- */}
      {/* The periods live here because this tab IS the module's configuration
          surface — there is no settings screen. Without this section a period
          can be created once and never touched again: `ends_on` could not be
          confirmed when the supplier states it (D153), the vyúčtování could not
          be recorded at all (D154, so Přehled's comparison never appears), and
          the next period could not be opened. */}
      <section>
        <div className="mb-2 flex flex-wrap items-center gap-2">
          <h2 className="text-[15px] font-bold">{cs.electricity.period.title}</h2>
          <Button size="sm" className="ml-auto" disabled={!canWrite} onClick={() => setPeriodNew(true)}>
            <Plus size={15} aria-hidden />
            {cs.electricity.form.add}
          </Button>
        </div>
        <p className="mb-3 text-[12.5px] text-muted text-pretty">
          {cs.electricity.period.endsOnHint}
        </p>
        <div className="space-y-2">
          {periodItems.map((p) => (
            <article key={p.id} className="rounded-xl border border-border bg-s1 p-3">
              <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
                <span className="text-[13.5px] font-bold">
                  {fmtDateISO(p.starts_on)} – {fmtDateISO(p.ends_on)}
                </span>
                {!p.ends_on_confirmed && (
                  <span className="rounded border border-border bg-s3 px-1.5 py-0.5 text-[10.5px] font-semibold text-muted">
                    {cs.electricity.word.expectedEnd}
                  </span>
                )}
                {canWrite && (
                  <div className="ml-auto flex gap-1">
                    <IconBtn label={cs.electricity.form.edit} onClick={() => setPeriodEdit(p)}>
                      <Pencil size={14} aria-hidden />
                    </IconBtn>
                    <IconBtn
                      label={cs.electricity.form.delete}
                      onClick={() => {
                        if (window.confirm(cs.electricity.period.deleteConfirm))
                          removePeriod.mutate(p.id)
                      }}
                    >
                      <Trash2 size={14} aria-hidden />
                    </IconBtn>
                  </div>
                )}
              </div>
              {p.invoiced_total_haler !== null ? (
                <p className="mt-1 font-mono text-[12.5px] tabular-nums">
                  {cs.electricity.word.invoice}: {kc(p.invoiced_total_haler)}
                </p>
              ) : (
                <p className="mt-1 text-[11.5px] text-subtle text-pretty">
                  {cs.electricity.period.invoiceHint}
                </p>
              )}
            </article>
          ))}
        </div>
      </section>

      <TariffDialog
        open={tariffNew || tariffEdit !== null}
        onOpenChange={(o) => {
          if (!o) {
            setTariffNew(false)
            setTariffEdit(null)
          }
        }}
        tariff={tariffEdit}
      />
      <AdvanceDialog
        open={advanceNew || advanceEdit !== null}
        onOpenChange={(o) => {
          if (!o) {
            setAdvanceNew(false)
            setAdvanceEdit(null)
          }
        }}
        advance={advanceEdit}
      />
      <PaymentDialog
        open={paymentNew || paymentEdit !== null}
        onOpenChange={(o) => {
          if (!o) {
            setPaymentNew(false)
            setPaymentEdit(null)
          }
        }}
        payment={paymentEdit}
      />
      <PeriodDialog
        open={periodNew || periodEdit !== null}
        onOpenChange={(o) => {
          if (!o) {
            setPeriodNew(false)
            setPeriodEdit(null)
          }
        }}
        period={periodEdit}
      />
    </div>
  )
}

function TariffCard({
  tariff,
  nextFrom,
  isFuture,
  coversDays,
  canWrite,
  onEdit,
  onDelete,
}: {
  tariff: ElTariff
  nextFrom?: string
  isFuture: boolean
  coversDays: number
  canWrite: boolean
  onEdit: () => void
  onDelete: () => void
}) {
  return (
    <article className={cn('rounded-xl border bg-s1 p-3', isFuture ? 'border-accent/40' : 'border-border')}>
      <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
        {/* The DERIVED validity range: the one thing the schema does not store,
            and therefore the one thing this list exists to show. */}
        <span className="text-[13.5px] font-bold">
          {nextFrom
            ? cs.electricity.tariff.validity(
                fmtDateISO(tariff.effective_from),
                fmtDateISO(prevDay(nextFrom)),
              )
            : cs.electricity.tariff.validityOpen(fmtDateISO(tariff.effective_from))}
        </span>
        {isFuture && (
          <span className="rounded border border-accent/40 bg-accent/10 px-1.5 py-0.5 text-[10.5px] font-bold tracking-wide uppercase text-accent">
            {cs.electricity.tariff.future}
          </span>
        )}
        {canWrite && (
          <div className="ml-auto flex gap-1">
            <IconBtn label={cs.electricity.form.edit} onClick={onEdit}>
              <Pencil size={14} aria-hidden />
            </IconBtn>
            <IconBtn label={cs.electricity.form.delete} onClick={onDelete}>
              <Trash2 size={14} aria-hidden />
            </IconBtn>
          </div>
        )}
      </div>

      {/* Three numbers, in the two-decimal treatment: these are the figures the
          supplier printed on a contract, and their haléře ARE the number. */}
      <dl className="mt-2 grid gap-x-4 gap-y-1 sm:grid-cols-3">
        <Num label={cs.electricity.word.priceVT} value={`${kc2(tariff.price_vt_haler)}/MWh`} />
        <Num label={cs.electricity.word.priceNT} value={`${kc2(tariff.price_nt_haler)}/MWh`} />
        <Num label={cs.electricity.word.monthlyFee} value={`${kc2(tariff.monthly_fee_haler)}/měs`} />
      </dl>

      {coversDays > 0 && (
        <p className="mt-2 text-[11.5px] text-subtle">
          {cs.electricity.tariff.coversDays(plural(coversDays, PLURAL.days))}
        </p>
      )}
      {isFuture && (
        <p className="mt-1 text-[11.5px] text-subtle text-pretty">
          {cs.electricity.tariff.futureNote}
        </p>
      )}
      {tariff.note && <p className="mt-1 text-[12px] text-subtle text-pretty">{tariff.note}</p>}
    </article>
  )
}

function Num({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <dt className="text-[11.5px] text-muted">{label}</dt>
      <dd className="font-mono text-[13.5px] font-semibold tabular-nums">{value}</dd>
    </div>
  )
}

function IconBtn({
  label,
  onClick,
  children,
}: {
  label: string
  onClick: () => void
  children: React.ReactNode
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-label={label}
      title={label}
      className="grid h-9 w-9 place-items-center rounded-md border border-border bg-s2 text-muted hover:text-fg"
    >
      {children}
    </button>
  )
}

function prevDay(iso: string): string {
  const [y, m, d] = iso.split('-').map(Number)
  const dt = new Date(y, m - 1, d - 1)
  const p = (x: number) => String(x).padStart(2, '0')
  return `${dt.getFullYear()}-${p(dt.getMonth() + 1)}-${p(dt.getDate())}`
}

/** coveredDays counts how many days of the CURRENT PERIOD this version governs —
 *  the "platí pro 153 dní tohoto období" line. It is the intersection of the
 *  version's derived validity range with the period, clamped at zero. */
function coveredDays(
  from: string,
  nextFrom: string | undefined,
  period: { starts_on: string; ends_on: string } | undefined,
): number {
  if (!period) return 0
  const start = maxDate(from, period.starts_on)
  const endExcl = minDate(nextFrom ?? addDay(period.ends_on), addDay(period.ends_on))
  const days = dayDiff(start, endExcl)
  return days > 0 ? days : 0
}

function toDate(iso: string): Date {
  const [y, m, d] = iso.split('-').map(Number)
  return new Date(y, m - 1, d)
}
function maxDate(a: string, b: string): string {
  return a > b ? a : b
}
function minDate(a: string, b: string): string {
  return a < b ? a : b
}
function addDay(iso: string): string {
  const dt = toDate(iso)
  dt.setDate(dt.getDate() + 1)
  const p = (x: number) => String(x).padStart(2, '0')
  return `${dt.getFullYear()}-${p(dt.getMonth() + 1)}-${p(dt.getDate())}`
}
function dayDiff(a: string, b: string): number {
  return Math.round((toDate(b).getTime() - toDate(a).getTime()) / 86_400_000)
}
