import { ArrowRight } from 'lucide-react'
import type { FinanceMonth } from './api/types'
import { cs } from '@/i18n/cs'
import { cn } from '@/lib/utils'
import { BUCKETS, displayNeeds, fmtMoney } from './buckets'
import { BucketSwatch } from './AllocationBar'

// The three-stage flow visualisation, ported in STRUCTURE from `fin`'s FlowViz
// and re-dressed in home's tokens. It is the reason this app is nicer than a
// spreadsheet: the split reads as money MOVING through stages, not as a table of
// nine numbers.
//
//   Příjem → Osobní (what each keeps, and what each sends on) → Společné účty
//
// Desktop keeps fin's three columns with connectors between them. At 375 px the
// columns become three stacked bands with the SAME connectors rotated to point
// down — the sense of flow is what has to survive the reflow, and a vertical list
// of nine labelled numbers would have thrown the whole point away.

function Person({ name, initial, children }: { name: string; initial: string; children: React.ReactNode }) {
  return (
    <span className="flex items-center gap-2.5">
      {/* Kája and Andy get identity from the LETTER, on a neutral surface — no
          person colour, because the four bucket hues already occupy blue, green,
          amber and violet and a fifth would read as another bucket. */}
      <span
        className="grid h-[26px] w-[26px] flex-none place-items-center rounded-full border border-border bg-s3 text-[11.5px] font-bold text-muted"
        aria-hidden
      >
        {initial}
      </span>
      <span className="text-[13.5px] font-bold">{name}</span>
      {children}
    </span>
  )
}

function FlowCard({ children, className }: { children: React.ReactNode; className?: string }) {
  return (
    <div className={cn('flex flex-col gap-1.5 rounded-xl border border-border bg-s1 p-3', className)}>{children}</div>
  )
}

function Amount({ children }: { children: React.ReactNode }) {
  return <div className="font-mono text-[19px] font-semibold tabular-nums">{children}</div>
}

function StageLabel({ children }: { children: React.ReactNode }) {
  return (
    <div className="px-0.5 pb-0.5 pt-1.5 font-mono text-[10px] uppercase tracking-[0.06em] text-subtle">{children}</div>
  )
}

/** Connector points right between columns on desktop and down between bands on
 *  mobile — one element, rotated, so the two layouts cannot drift apart. */
function Connector() {
  return (
    <div className="grid place-items-center py-1 text-subtle md:py-0 md:pt-[52px]" aria-hidden>
      <ArrowRight size={16} className="rotate-90 md:rotate-0" />
    </div>
  )
}

export function MonthFlow({ month }: { month: FinanceMonth }) {
  const s = month.split
  const r = month.rates
  const negativeNeeds = s.needs < 0

  const people = [
    { name: 'Kája', initial: 'K', income: month.income_kaja, personal: s.personal_kaja, rest: s.to_operational_kaja },
    { name: 'Andy', initial: 'A', income: month.income_andy, personal: s.personal_andy, rest: s.to_operational_andy },
  ]

  const joint = [
    {
      bucket: BUCKETS[1],
      label: cs.finance.operationalNeeds,
      tag: cs.finance.ofIncome(r.operational),
      value: displayNeeds(s.needs),
      note: cs.finance.operationalNote(fmtMoney(s.operational_received)),
      // The value is correct and is NOT clamped in the data — only the display
      // shows 0 Kč, with the rounding stated underneath so it reads as precision
      // rather than as an error.
      foot: negativeNeeds ? cs.finance.roundingFootnote(fmtMoney(s.needs)) : null,
    },
    {
      bucket: BUCKETS[2],
      label: cs.finance.funSavings,
      tag: cs.finance.ofTotal(r.fun),
      value: s.fun_savings,
      note: null,
      foot: null,
    },
    {
      bucket: BUCKETS[3],
      label: cs.finance.noFunSavings,
      tag: cs.finance.ofTotal(r.no_fun),
      value: s.no_fun_savings,
      note: null,
      foot: null,
    },
  ]

  return (
    <div>
      <div className="grid items-start gap-x-0 gap-y-1 md:grid-cols-[minmax(0,1fr)_30px_minmax(0,1fr)_30px_minmax(0,1.15fr)]">
        {/* Stage 1 — Příjem */}
        <div className="flex flex-col gap-2.5">
          <StageLabel>{cs.finance.stageIncome}</StageLabel>
          {people.map((p) => (
            <FlowCard key={p.name}>
              <Person name={p.name} initial={p.initial}>
                <span className="ml-auto font-mono text-[9.5px] uppercase tracking-[0.05em] text-subtle">
                  {cs.finance.income.toLowerCase()}
                </span>
              </Person>
              <Amount>{fmtMoney(p.income)}</Amount>
              {p.income === 0 && <p className="text-[11.5px] text-subtle">{cs.finance.noIncomeThisMonth}</p>}
            </FlowCard>
          ))}
        </div>

        <Connector />

        {/* Stage 2 — Osobní */}
        <div className="flex flex-col gap-2.5">
          <StageLabel>{cs.finance.stagePersonal(r.personal)}</StageLabel>
          {people.map((p) => (
            <FlowCard key={p.name}>
              <span className="flex items-center gap-2.5">
                <BucketSwatch bucket={BUCKETS[0]} />
                <span className="font-mono text-[9.5px] font-bold text-subtle">{BUCKETS[0].mark}</span>
                <span className="text-[13.5px] font-bold">
                  {p.name} · {cs.finance.personal.toLowerCase()}
                </span>
              </span>
              <Amount>{fmtMoney(p.personal)}</Amount>
              <span className="flex items-center gap-2 border-t border-dashed border-border pt-1.5 text-[12px] text-muted">
                <span className="flex-1">{cs.finance.restToKandy}</span>
                <span className="font-mono tabular-nums text-fg">{fmtMoney(p.rest)}</span>
              </span>
            </FlowCard>
          ))}
        </div>

        <Connector />

        {/* Stage 3 — Společné účty */}
        <div className="flex flex-col gap-2.5">
          <StageLabel>{cs.finance.stageJoint}</StageLabel>
          {joint.map((j) => (
            <FlowCard key={j.bucket.key}>
              <span className="flex items-center gap-2.5">
                <BucketSwatch bucket={j.bucket} />
                <span className="font-mono text-[9.5px] font-bold text-subtle">{j.bucket.mark}</span>
                <span className="min-w-0 text-[13.5px] font-bold">{j.label}</span>
                <span className="ml-auto whitespace-nowrap font-mono text-[9.5px] text-subtle">{j.tag}</span>
              </span>
              <Amount>{fmtMoney(j.value)}</Amount>
              {j.foot && <div className="font-mono text-[11px] text-subtle">{j.foot}</div>}
              {j.note && <p className="text-[11.5px] text-pretty text-muted">{j.note}</p>}
            </FlowCard>
          ))}
        </div>
      </div>

      <p className="mt-3.5 flex items-start gap-2.5 rounded-xl border border-border bg-s1 px-3 py-2.5 text-[12.5px] text-pretty text-muted">
        <span aria-hidden className="text-subtle">
          ⤳
        </span>
        <span>{cs.finance.reconciliation(fmtMoney(s.total_income))}</span>
      </p>
    </div>
  )
}
