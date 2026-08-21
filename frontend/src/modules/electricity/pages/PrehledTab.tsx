import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ChevronDown, CircleSlash, Plus } from 'lucide-react'
import { qk } from '@/api/keys'
import { cs } from '@/i18n/cs'
import { PLURAL } from '@/i18n/plural'
import { fmtDateISO, monthLabel, todayISO } from '@/i18n/format'
import { useAuth } from '@/app/auth'
import { useOnline } from '@/platform/pwa/offline'
import { Button } from '@/components/ui/ui'
import { cn } from '@/lib/utils'
import { getSummary } from '../api/endpoints'
import type { ElSummary } from '../api/types'
import { kc, kc2, kcSigned, kwh, kwhWhole, plural } from '../format'
import { TariffShareBar, VTSwatch, NTSwatch } from '../components/TariffMarks'

// PŘEHLED — the module's entire reason to exist (PRD §V8-7).
//
// The screen answers ONE question — vyjdou zálohy, nebo doplatím? — and it has to
// answer it in the first screenful at 375 px, because the user came here on
// purpose and nothing anywhere else in the app will ever mention this module.
// The fold is a hard budget: headline, its hedge, and the odečet action.
// Everything else lives below it.
//
// FOUR FIRST-CLASS STATES, none of which is a fallback:
//
//   ok                — actual + forecast. The ordinary case, eventually.
//   insufficient_data — the HEADROOM screen. This is Karel's day one and the
//                       primary screen for the module's first weeks, so it gets
//                       the headline slot at full weight. No spinner, no blank
//                       panel, and above all NO ZERO.
//   blocked           — valid figures above a date-anchored divider, the blocked
//                       region below it in the informational language, never the
//                       destructive one.
//   complete          — predikce becomes skutečnost, the hedges disappear, and
//                       the number stays exactly where it was so the eye does not
//                       have to re-find it.

export function PrehledTab({
  periodId,
  onAddReading,
}: {
  periodId: string
  onAddReading: (date?: string) => void
}) {
  const summary = useQuery({
    queryKey: qk.electricitySummary(periodId),
    queryFn: () => getSummary(periodId),
  })

  if (summary.isPending) {
    // A SKELETON, never a spinner in the headline slot: a spinner where the
    // answer belongs reads as "broken" on a screen visited a few times a year.
    return (
      <div className="space-y-3">
        <div className="h-40 animate-pulse rounded-2xl border border-border bg-s1" />
        <div className="h-28 animate-pulse rounded-2xl border border-border bg-s1" />
        <div className="h-28 animate-pulse rounded-2xl border border-border bg-s1" />
      </div>
    )
  }

  if (summary.isError) {
    return (
      <div className="grid min-h-[30vh] place-items-center text-center">
        <div>
          <p className="font-bold">{cs.electricity.error.loadFailed}</p>
          <p className="mt-1 text-[13px] text-muted">{cs.electricity.error.loadFailedHint}</p>
          <Button className="mt-3" onClick={() => summary.refetch()}>
            {cs.electricity.error.retry}
          </Button>
        </div>
      </div>
    )
  }

  const s = summary.data

  return (
    <div className="space-y-3 pb-4">
      {/* ⚠ The null test is `=== null`, NOT falsiness. A genuine zero balance —
          the zálohy landing exactly on the cost — is a real and meaningful
          answer, and `!s.balance_haler` would hide it behind the "cannot
          predict" state. */}
      {s.cost_total_haler === null ? <NoPrediction s={s} onAddReading={onAddReading} /> : <Headline s={s} />}

      <NudgeLine s={s} onAddReading={onAddReading} />

      {s.blocking.length > 0 && <BlockedDivider s={s} onAddReading={onAddReading} />}

      {s.actual && s.actual.days > 0 && <TariffBreakdown s={s} />}

      <AdvancesPanel s={s} />

      {s.invoice_comparison && <InvoicePanel s={s} />}
    </div>
  )
}

// ---------------------------------------------------------------------------
// The headline — a big confident number that must never read as a fact
// ---------------------------------------------------------------------------

function Headline({ s }: { s: ElSummary }) {
  const balance = s.balance_haler ?? 0
  const over = balance < 0
  const complete = s.status === 'complete'
  const hedged = !s.period.ends_on_confirmed && !complete

  return (
    <section className="rounded-2xl border border-border bg-s1 p-4 sm:p-5">
      {/* KICKER above: what the number is, and the date it projects to. The
          prediction always names the date it projected to. */}
      <div className="mb-2 flex flex-wrap items-center gap-2">
        <span className="text-[12px] font-semibold text-muted">
          {complete ? cs.electricity.head.kickerActual : cs.electricity.head.kickerEstimate}{' '}
          {fmtDateISO(s.period.ends_on)}
        </span>
        {hedged && (
          <span className="inline-flex items-center rounded-md border border-border bg-s3 px-2 py-0.5 text-[11.5px] font-semibold text-muted">
            {cs.electricity.word.expectedEnd}
          </span>
        )}
      </div>

      {/* THE FIGURE. Full size, tabular, and hedged by structure rather than by
          timidity: shrinking or greying it would trade away the module's only
          payoff, and it has to be legible at arm's length in a dim cupboard.
          The SIGN and the WORD carry the meaning; colour only reinforces it. */}
      <div
        className={cn(
          'font-mono text-[clamp(2.5rem,9vw,3.75rem)] leading-none font-semibold tracking-tight tabular-nums',
          over ? 'text-el-over' : 'text-el-under',
        )}
      >
        {kcSigned(balance)}
      </div>
      <div className="mt-2 flex flex-wrap items-center gap-2">
        <span
          className={cn(
            'inline-flex items-center rounded-lg px-2.5 py-1 text-[13px] font-bold',
            over ? 'bg-el-over-soft text-el-over' : 'bg-el-under/15 text-el-under',
          )}
        >
          {over ? cs.electricity.word.underpay : cs.electricity.word.overpay}
        </span>
      </div>

      {/* BASIS below: so the number is never mistaken for a fact. On a completed
          period this line states the opposite — that it IS one. */}
      <p className="mt-3 text-[12.5px] text-muted text-pretty">
        {complete
          ? cs.electricity.head.basisActual(
              plural(s.actual?.days ?? 0, PLURAL.days),
              fmtDateISO(addDays(s.period.ends_on, 1)),
            )
          : cs.electricity.head.basisPrediction(plural(daysBasis(s), PLURAL.days))}
      </p>
      <p className="mt-1 font-mono text-[12.5px] text-subtle tabular-nums">
        {cs.electricity.head.costLine(
          kc(s.cost_total_haler ?? 0),
          kc(s.advances.expected_total_haler),
        )}
      </p>

      {s.actual && s.forecast && s.forecast.days > 0 && (
        <div className="mt-4">
          <div className="h-1.5 overflow-hidden rounded-full bg-s3">
            <div
              className="h-full rounded-full bg-accent"
              style={{ width: `${progressPct(s)}%` }}
            />
          </div>
          <p className="mt-1.5 text-[12px] text-subtle">
            {cs.electricity.head.progress(
              plural(s.actual.days, PLURAL.days),
              plural(s.forecast.days, PLURAL.days),
            )}
          </p>
        </div>
      )}
    </section>
  )
}

function daysBasis(s: ElSummary): number {
  return s.actual?.days ?? 0
}

function progressPct(s: ElSummary): number {
  const a = s.actual?.days ?? 0
  const f = s.forecast?.days ?? 0
  if (a + f === 0) return 0
  return Math.round((a / (a + f)) * 100)
}

function addDays(iso: string, n: number): string {
  const [y, m, d] = iso.split('-').map(Number)
  const dt = new Date(y, m - 1, d + n)
  const p = (x: number) => String(x).padStart(2, '0')
  return `${dt.getFullYear()}-${p(dt.getMonth() + 1)}-${p(dt.getDate())}`
}

// ---------------------------------------------------------------------------
// "Zatím nelze předpovědět" — the primary screen for the module's first weeks
// ---------------------------------------------------------------------------

function NoPrediction({ s, onAddReading }: { s: ElSummary; onAddReading: (d?: string) => void }) {
  const h = s.headroom
  const reasonText = s.reason ? cs.electricity.reason[s.reason] : undefined

  return (
    <section className="rounded-2xl border border-border bg-s1 p-4 sm:p-5">
      <div className="mb-2 text-[12px] font-semibold text-muted">
        {cs.electricity.cannotPredict}
        {reasonText ? ` — ${reasonText}` : ''}
      </div>

      {h ? (
        <>
          {/* The headroom takes the HEADLINE SLOT at full weight, so the later
              arrival of a real prediction is a change of answer rather than a
              repair of a broken screen. This figure is computable with zero
              consumption data, and it is the honest answer to "will the zálohy
              cover it" before anything has been measured. */}
          <div className="font-mono text-[clamp(2.25rem,8vw,3.25rem)] leading-none font-semibold tracking-tight tabular-nums">
            {kc2(h.energy_budget_haler)}
          </div>
          <p className="mt-2 text-[13px] text-muted text-pretty">
            {cs.electricity.headroom.line(
              kc2(h.energy_budget_haler),
              kc(h.advance_haler),
              kc2(h.monthly_fee_haler),
            )}
          </p>

          <div className="mt-3 flex flex-wrap gap-2">
            <KwhChip label={cs.electricity.headroom.mix} value={kwhWhole(h.kwh_mix_dkwh)} lead />
            <KwhChip label={cs.electricity.headroom.allVT} value={kwhWhole(h.kwh_all_vt_dkwh)} vt />
            <KwhChip label={cs.electricity.headroom.allNT} value={kwhWhole(h.kwh_all_nt_dkwh)} nt />
          </div>
          {/* Named as a guess, because it is one. */}
          <p className="mt-2 text-[12px] text-subtle text-pretty">
            {cs.electricity.headroom.mixNote}
          </p>
        </>
      ) : (
        <p className="text-[13.5px] text-muted text-pretty">{cs.electricity.headroom.missing}</p>
      )}

      <Button className="mt-4" variant="secondary" onClick={() => onAddReading(undefined)}>
        <Plus size={16} aria-hidden />
        {cs.electricity.word.addReading}
      </Button>
    </section>
  )
}

function KwhChip({
  label,
  value,
  vt,
  nt,
  lead,
}: {
  label: string
  value: string
  vt?: boolean
  nt?: boolean
  lead?: boolean
}) {
  return (
    <span
      className={cn(
        'inline-flex items-center gap-2 rounded-lg border px-2.5 py-1.5 text-[12.5px]',
        lead ? 'border-border-strong bg-s3' : 'border-border bg-s2',
      )}
    >
      {vt && <VTSwatch />}
      {nt && <NTSwatch />}
      <span className="text-muted">{label}</span>
      <strong className="font-mono font-semibold tabular-nums">{value}</strong>
    </span>
  )
}

// ---------------------------------------------------------------------------
// The hard block — valid data and a blocked region on the same screen
// ---------------------------------------------------------------------------

function BlockedDivider({ s, onAddReading }: { s: ElSummary; onAddReading: (d?: string) => void }) {
  const online = useOnline()
  const canWrite = useAuth().canWrite && online
  const b = s.blocking[0]
  return (
    // A DATE-ANCHORED DIVIDER, in the informational palette. Everything above it
    // renders normally and stays true; this line marks where the module stops
    // being able to compute. It is deliberately NOT the destructive red: nobody
    // has done anything wrong and the numbers around it are still correct.
    <section className="rounded-2xl border border-el-blocked/40 bg-el-blocked-soft p-4">
      <div className="flex gap-3">
        <span className="mt-0.5 grid h-8 w-8 flex-none place-items-center rounded-lg border border-el-blocked/50 text-el-blocked">
          <CircleSlash size={16} aria-hidden />
        </span>
        <div className="min-w-0 flex-1">
          <p className="text-[13.5px] font-bold">{cs.electricity.blocked.heading}</p>
          <p className="mt-1 text-[13px] text-pretty">{b.message_cs}</p>
          <p className="mt-1.5 text-[12px] text-muted text-pretty">
            {cs.electricity.blocked.belowNote}
          </p>
          <Button
            className="mt-3"
            variant="secondary"
            disabled={!canWrite}
            onClick={() => onAddReading(b.required_reading_on)}
          >
            <Plus size={16} aria-hidden />
            {cs.electricity.word.addReading} — {fmtDateISO(b.required_reading_on)}
          </Button>
          <p className="mt-2 text-[11.5px] text-subtle text-pretty">
            {cs.electricity.blocked.prefill}
          </p>
        </div>
      </div>
    </section>
  )
}

// ---------------------------------------------------------------------------
// The nudge — a tone problem before it is a layout problem
// ---------------------------------------------------------------------------

function NudgeLine({ s, onAddReading }: { s: ElSummary; onAddReading: (d?: string) => void }) {
  const online = useOnline()
  const canWrite = useAuth().canWrite && online
  const days = s.days_since_last_reading

  // ESCALATION IN WORDS ONLY. The sub-line changes at 15 and 90 days; the colour
  // and the size never do. If 47 days and 200 days looked identical the line
  // would be useless — but if 200 days turned red the module would have started
  // chasing, which is the one thing that was refused. The register is
  // explanation, not reproach: this is the honest reason the prediction above is
  // stale, with the action attached because the user is already here.
  //
  // ⚠ `days` can be ZERO or NEGATIVE. Zero is today's reading; negative means one
  // is dated in the FUTURE — a mistyped date, or a closing odečet entered ahead
  // of time. Both are legal, and "před −42 dny" is nonsense, so neither goes
  // through the counting branch.
  const future = days !== null && days < 0
  const sub =
    days === null
      ? cs.electricity.nudge.never
      : future
        ? cs.electricity.nudge.futureHint
        : days >= 90
          ? cs.electricity.nudge.stale
          : days >= 15
            ? cs.electricity.nudge.ageing
            : cs.electricity.nudge.fresh

  return (
    <section className="flex flex-wrap items-center gap-3 rounded-xl border border-border bg-s2 px-3.5 py-3">
      <div className="min-w-[180px] flex-1">
        <p className="text-[13px] font-semibold">
          {days === null
            ? cs.electricity.nudge.never
            : future
              ? cs.electricity.nudge.future(fmtDateISO(s.last_reading_on!))
              : days === 0
                ? cs.electricity.nudge.today
                : cs.electricity.nudge.line(plural(days, PLURAL.days))}
        </p>
        {days !== null && <p className="mt-0.5 text-[12px] text-muted text-pretty">{sub}</p>}
      </div>
      <Button size="sm" variant="secondary" disabled={!canWrite} onClick={() => onAddReading(undefined)}>
        {cs.electricity.word.addReading}
      </Button>
    </section>
  )
}

// ---------------------------------------------------------------------------
// Spotřeba a náklady, VT vs NT
// ---------------------------------------------------------------------------

function TariffBreakdown({ s }: { s: ElSummary }) {
  const a = s.actual!
  // At 4 858,65 vs 4 026,69 Kč/MWh roughly 830 Kč rides on every MWh moved from
  // VT to NT, and this split is the only place that becomes visible.
  return (
    <section className="rounded-2xl border border-border bg-s1 p-4">
      <h2 className="mb-3 text-[13.5px] font-bold">
        {cs.electricity.word.consumption} a {cs.electricity.word.costs.toLowerCase()}
      </h2>

      <TariffShareBar vtHaler={a.energy.vt_haler} ntHaler={a.energy.nt_haler} />

      <dl className="mt-3 grid gap-2 sm:grid-cols-2">
        <TariffRow
          swatch={<VTSwatch />}
          label={cs.electricity.word.vtLong}
          kwhText={kwh(a.vt_dkwh)}
          kcText={kc2(a.energy.vt_haler)}
          share={share(a.energy.vt_haler, a.energy.total_haler)}
        />
        <TariffRow
          swatch={<NTSwatch />}
          label={cs.electricity.word.ntLong}
          kwhText={kwh(a.nt_dkwh)}
          kcText={kc2(a.energy.nt_haler)}
          share={share(a.energy.nt_haler, a.energy.total_haler)}
        />
      </dl>

      {/* The period is stated as ENERGIE + POPLATKY, which is also how the
          supplier states it. Fees belong to no interval (D143), so they appear
          exactly once — here. */}
      <div className="mt-3 border-t border-border pt-3 text-[12.5px]">
        <div className="flex justify-between gap-3">
          <span className="text-muted">{cs.electricity.word.energy}</span>
          <span className="font-mono tabular-nums">{kc2(a.energy.total_haler)}</span>
        </div>
        <div className="mt-1 flex justify-between gap-3">
          <span className="text-muted">{cs.electricity.word.monthlyFee}</span>
          <span className="font-mono tabular-nums">{kc2(a.fees_haler)}</span>
        </div>
      </div>
    </section>
  )
}

function share(part: number, total: number): string {
  if (total === 0) return '0 %'
  return `${Math.round((part / total) * 100)} %`
}

function TariffRow({
  swatch,
  label,
  kwhText,
  kcText,
  share: shareText,
}: {
  swatch: React.ReactNode
  label: string
  kwhText: string
  kcText: string
  share: string
}) {
  return (
    <div className="rounded-xl border border-border bg-s2 p-3">
      <dt className="flex items-center gap-2 text-[12.5px] font-bold">
        {swatch}
        {label}
        <span className="ml-auto font-mono text-[11.5px] font-normal text-muted tabular-nums">
          {shareText}
        </span>
      </dt>
      <dd className="mt-1.5 flex items-baseline justify-between gap-2">
        <span className="font-mono text-[15px] tabular-nums">{kwhText}</span>
        <span className="font-mono text-[13px] text-muted tabular-nums">{kcText}</span>
      </dd>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Zálohy, the counted months, and the doporučená záloha
// ---------------------------------------------------------------------------

function AdvancesPanel({ s }: { s: ElSummary }) {
  const [open, setOpen] = useState(false)
  const a = s.advances
  const current = currentScheduled(s)

  return (
    <section className="rounded-2xl border border-border bg-s1 p-4">
      <h2 className="mb-3 text-[13.5px] font-bold">{cs.electricity.advances.title}</h2>

      <div className="grid gap-2 sm:grid-cols-2">
        {/* ⚠ NOT "zaplaceno". due_so_far_haler sums the counted months whose due
            day has passed, and an unpaid month still contributes its předpis
            amount — so this figure says what was OWED by now, never what left the
            account. The note under it points at where a real platba is recorded. */}
        <Figure
          label={cs.electricity.advances.dueSoFar}
          value={kc(a.due_so_far_haler)}
          note={cs.electricity.advances.dueSoFarNote}
        />
        <Figure label={cs.electricity.advances.expectedTotal} value={kc(a.expected_total_haler)} />
      </div>

      {s.recommended_advance_kc !== null && (
        <div className="mt-3 rounded-xl border border-border bg-s2 p-3">
          <div className="text-[12px] text-muted">{cs.electricity.word.recommended}</div>
          <div className="mt-0.5 font-mono text-[15px] font-semibold tabular-nums">
            {/* A COMPARISON, not a lone number: "doporučeno X · nyní Y" is the
                only form in which the recommendation means anything. */}
            {/* Both figures go through kc(), so the recommendation is grouped the
                same way as the current amount beside it — "1 460 Kč · 1 500 Kč",
                never "1460 Kč · 1 500 Kč". The server sends the koruny value and
                its haléře twin precisely so this comparison can be formatted with
                one function. */}
            {cs.electricity.advances.recommendedVs(
              kc(s.recommended_advance_haler ?? (s.recommended_advance_kc ?? 0) * 100),
              current !== null ? kc(current) : '—',
            )}
          </div>
          <p className="mt-1 text-[11.5px] text-subtle">{cs.electricity.advances.recommendedNote}</p>
        </div>
      )}

      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        className="mt-3 flex min-h-11 w-full items-center justify-between gap-2 rounded-lg border border-border bg-s2 px-3 text-[12.5px] font-semibold"
      >
        <span>
          {open ? cs.electricity.advances.hideMonths : cs.electricity.advances.showMonths} (
          {plural(a.months_counted, PLURAL.months)})
        </span>
        <ChevronDown size={16} aria-hidden className={cn('transition-transform', open && 'rotate-180')} />
      </button>

      {open && (
        <div className="mt-2 space-y-1.5">
          {/* D145 is the module's most surprising arithmetic, so the disclosure
              SPELLS OUT the rule and then lists the months it produced. Folklore
              becomes something checkable. */}
          <p className="rounded-lg bg-s2 p-2.5 text-[12px] text-muted text-pretty">
            {cs.electricity.advances.countRule}
          </p>
          {a.counted_months.map((m) => (
            <div
              key={m.month}
              className={cn(
                'flex flex-wrap items-center gap-x-3 gap-y-1 rounded-lg border border-border px-3 py-2 text-[12.5px]',
                m.is_due ? 'bg-s2' : 'bg-transparent',
              )}
            >
              <span className="min-w-[110px] font-semibold">{monthLabel(m.month)}</span>
              <span className="font-mono tabular-nums">{kc(m.amount_haler)}</span>
              <span className="rounded border border-border px-1.5 py-0.5 font-mono text-[10px] font-bold tracking-wide uppercase text-muted">
                {m.source === 'payment'
                  ? cs.electricity.advances.sourcePayment
                  : m.source === 'schedule'
                    ? cs.electricity.advances.sourceSchedule
                    : cs.electricity.advances.sourceNone}
              </span>
              <span className="ml-auto text-[11.5px] text-subtle">
                {cs.electricity.advances.due} {fmtDateISO(m.due_on)}
                {m.due_clamped ? ` (${cs.electricity.advance.clamped})` : ''}
                {m.is_due ? ` — ${cs.electricity.advances.duePassed}` : ''}
              </span>
            </div>
          ))}
        </div>
      )}
    </section>
  )
}

/** currentScheduled is the amount the záloha is running at NOW — which is what
 *  "nyní" means to a reader standing in front of the recommendation.
 *
 *  ⚠ It is deliberately NOT the last counted month. Zálohy are date-versioned
 *  exactly like the ceník, and entering a version ahead of its start is normal:
 *  with a 1 800 Kč předpis effective next January, every counted month from
 *  January on carries 1 800, so taking the last month would print "nyní 1 800 Kč"
 *  while 1 500 Kč is what actually leaves the account today. So the search is
 *  bounded to the months that have already STARTED, and falls back to the
 *  earliest month only for a period that lies entirely in the future — where
 *  there is no "now" inside it at all. */
function currentScheduled(s: ElSummary): number | null {
  const months = s.advances.counted_months
  if (months.length === 0) return null
  const nowMonth = todayISO().slice(0, 7)
  const started = months.filter((m) => m.month <= nowMonth)
  if (started.length > 0) {
    const withSource = [...started].reverse().find((m) => m.source !== 'none')
    return withSource ? withSource.amount_haler : null
  }
  const withSource = months.find((m) => m.source !== 'none')
  return withSource ? withSource.amount_haler : null
}

function Figure({ label, value, note }: { label: string; value: string; note?: string }) {
  return (
    <div className="rounded-xl border border-border bg-s2 p-3">
      <div className="text-[12px] text-muted">{label}</div>
      <div className="mt-0.5 font-mono text-[17px] font-semibold tabular-nums">{value}</div>
      {note && <p className="mt-1 text-[11px] text-subtle text-pretty">{note}</p>}
    </div>
  )
}

// ---------------------------------------------------------------------------
// The vyúčtování comparison — two lines, because kWh and Kč mean different things
// ---------------------------------------------------------------------------

function InvoicePanel({ s }: { s: ElSummary }) {
  const c = s.invoice_comparison!
  return (
    <section className="rounded-2xl border border-border bg-s1 p-4">
      <h2 className="mb-2 text-[13.5px] font-bold">{cs.electricity.word.invoice}</h2>
      {/* THE HEDGE COMES FIRST, above the figures it qualifies. The vyúčtování
          normally arrives before anyone walks to the meter cupboard, so until the
          uzavírací odečet is in, "spočteno" is still part predikce and the
          difference is mostly our own projection — not a disagreement with the
          supplier. Stated as a chip rather than folded into the hint, because the
          hint below changes meaning with it. */}
      {c.computed_is_forecast && (
        <p className="mb-2 inline-flex items-center rounded-md border border-border bg-s3 px-2 py-0.5 text-[11.5px] font-semibold text-muted">
          {cs.electricity.period.comparisonForecast}
        </p>
      )}
      <p className="font-mono text-[12.5px] tabular-nums text-pretty">
        {cs.electricity.period.comparisonKc(
          kc(c.computed_total_haler),
          kc(c.invoiced_total_haler),
          kcSigned(c.diff_haler),
        )}
      </p>
      {/* ⚠ BOTH registers on BOTH sides, never one. The line adds VT and NT into
          a single kWh total, so a vyúčtování with only VT recorded would print
          an invoiced total silently missing NT — beside a difference computed
          from diff_vt alone, which reads 0. Two totals thousands of kWh apart
          with "rozdíl 0 kWh" between them is worse than no line at all, and
          diff_nt_dkwh is null exactly when invoiced_nt_dkwh is. */}
      {c.computed_vt_dkwh !== null &&
        c.computed_nt_dkwh !== null &&
        c.invoiced_vt_dkwh !== null &&
        c.invoiced_nt_dkwh !== null && (
          <p className="mt-1 font-mono text-[12.5px] tabular-nums text-pretty">
            {cs.electricity.period.comparisonKwh(
              kwh(c.computed_vt_dkwh + c.computed_nt_dkwh),
              kwh(c.invoiced_vt_dkwh + c.invoiced_nt_dkwh),
              kwh((c.diff_vt_dkwh ?? 0) + (c.diff_nt_dkwh ?? 0)),
            )}
          </p>
        )}
      {/* "Rozdíl v kWh znamená jiný odečet" is only TRUE once both sides measure
          the same thing. Before the closing odečet it would blame the supplier for
          our own forecast, so the sentence is swapped rather than merely softened. */}
      <p className="mt-2 text-[11.5px] text-subtle text-pretty">
        {c.computed_is_forecast
          ? cs.electricity.period.comparisonForecastHint
          : cs.electricity.period.comparisonHint}
      </p>
    </section>
  )
}
