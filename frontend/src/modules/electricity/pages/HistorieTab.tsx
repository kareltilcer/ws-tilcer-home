import { useQuery } from '@tanstack/react-query'
import { qk } from '@/api/keys'
import { cs } from '@/i18n/cs'
import { fmtDateISO } from '@/i18n/format'
import { Button } from '@/components/ui/ui'
import { getHistory } from '../api/endpoints'
import type { ElHistoryMonth, ElPeriod } from '../api/types'
import { kc, kwh, monthLabelShort } from '../format'
import { ApproxMark, NTSwatch, VTSwatch } from '../components/TariffMarks'

// HISTORIE A GRAFY — and the module's one approximation.
//
// This is the first screen in home where an APPROXIMATE number and an EXACT one
// sit side by side, and the whole design problem is that the user must never
// have to wonder which kind they are reading:
//
//   kWh COLUMNS ARE APPROXIMATE (D138). An interval's consumption is spread
//   evenly across its days so months can be drawn at all. They carry the ≈ mark
//   and the --el-approx vocabulary.
//
//   Kč COLUMNS ARE EXACT (D159). A month's cost is an ALLOCATION of already-exact
//   interval costs by day count — never a repricing of the invented kWh, which is
//   precisely what D137 forbids. Kč NEVER receives the approximate treatment.
//
// The caveat is carried by the marks plus ONE footnote, not by a warning banner
// over the chart: the numbers are usable, they are simply cut along an
// approximate line, and a banner would suggest otherwise.

export function HistorieTab({
  periods,
  currentId,
}: {
  periods: ElPeriod[]
  currentId: string
}) {
  const history = useQuery({ queryKey: qk.electricityHistory(), queryFn: () => getHistory() })

  if (history.isPending) {
    return <div className="h-64 animate-pulse rounded-2xl border border-border bg-s1" />
  }

  // ⚠ A FAILED REQUEST IS NOT AN EMPTY HISTORY. Without this branch `?? []`
  // below turns any error into the "not enough history yet" panel — which says
  // the opposite of what happened, offers no retry, and would hide a full year
  // of readings behind a matter-of-fact sentence.
  if (history.isError) {
    return (
      <div className="grid min-h-[30vh] place-items-center text-center">
        <div>
          <p className="font-bold">{cs.electricity.error.loadFailed}</p>
          <p className="mt-1 text-[13px] text-muted">{cs.electricity.error.loadFailedHint}</p>
          <Button className="mt-3" onClick={() => history.refetch()}>
            {cs.electricity.error.retry}
          </Button>
        </div>
      </div>
    )
  }

  const months = history.data?.months ?? []
  const withData = months.filter((m) => m.vt_dkwh + m.nt_dkwh > 0 || m.total_haler > 0)

  if (withData.length === 0) {
    // The honest first-year state — matter-of-fact, NOT an error. The counterpart
    // of v7's "chybí historie": a module that has not run long enough to have a
    // history is working correctly.
    return (
      <div className="grid min-h-[30vh] place-items-center px-2">
        <div className="max-w-sm rounded-2xl border border-dashed border-border p-7 text-center">
          <h2 className="mb-1.5 text-[16px] font-bold">{cs.electricity.history.empty}</h2>
          <p className="text-[13px] text-muted text-pretty">{cs.electricity.history.emptyHint}</p>
        </div>
      </div>
    )
  }

  const maxKwh = Math.max(...withData.map((m) => m.vt_dkwh + m.nt_dkwh), 1)
  const maxKc = Math.max(...withData.map((m) => m.total_haler), 1)

  return (
    <div className="space-y-5 pb-4">
      <section className="rounded-2xl border border-border bg-s1 p-4">
        <div className="mb-3 flex flex-wrap items-center gap-3">
          <h2 className="text-[14px] font-bold">{cs.electricity.history.consumption}</h2>
          {/* The legend is ALWAYS present — the v6 chart rule — and names both
              series in text, so the swatches are reinforcement rather than the
              only encoding. */}
          <div className="ml-auto flex items-center gap-3 text-[12px]">
            <span className="flex items-center gap-1.5">
              <VTSwatch />
              {cs.electricity.word.vt}
            </span>
            <span className="flex items-center gap-1.5">
              <NTSwatch />
              {cs.electricity.word.nt}
            </span>
          </div>
        </div>

        <div className="overflow-x-auto">
          <div className="flex min-w-max items-end gap-2 pb-1">
            {withData.map((m) => (
              <KwhColumn key={m.month} month={m} max={maxKwh} />
            ))}
          </div>
        </div>
        <p className="mt-3 border-t border-border pt-2 text-[11.5px] text-el-approx text-pretty">
          {cs.electricity.history.approxNote}
        </p>
      </section>

      <section className="rounded-2xl border border-border bg-s1 p-4">
        <h2 className="mb-3 text-[14px] font-bold">{cs.electricity.history.cost}</h2>
        <div className="space-y-1.5">
          {withData.map((m) => (
            <div key={m.month} className="flex items-center gap-3">
              <span className="w-16 flex-none font-mono text-[11.5px] text-muted tabular-nums">
                {monthLabelShort(m.month)}
              </span>
              <div className="h-5 flex-1 overflow-hidden rounded bg-s3">
                <div
                  className="h-full rounded bg-accent"
                  style={{ width: `${(m.total_haler / maxKc) * 100}%` }}
                />
              </div>
              {/* EXACT, and in the mono treatment that means "exact" throughout
                  the module. No ≈ mark here, ever. */}
              <span className="w-24 flex-none text-right font-mono text-[12px] font-semibold tabular-nums">
                {kc(m.total_haler)}
              </span>
            </div>
          ))}
        </div>
      </section>

      {/* Every period EXCEPT the one being shown above — matched by id, not by
          dropping the head of the list. The head is merely the newest by
          starts_on, so a period created ahead of its start would push the one
          actually running into "Minulá období". */}
      {periods.some((p) => p.id !== currentId) && (
        <section className="rounded-2xl border border-border bg-s1 p-4">
          <h2 className="mb-3 text-[14px] font-bold">{cs.electricity.history.pastPeriods}</h2>
          <div className="space-y-2">
            {periods.filter((p) => p.id !== currentId).map((p) => (
              <div key={p.id} className="rounded-xl border border-border bg-s2 p-3">
                <div className="text-[13px] font-semibold">
                  {fmtDateISO(p.starts_on)} – {fmtDateISO(p.ends_on)}
                </div>
                {p.invoiced_total_haler !== null ? (
                  <>
                    <p className="mt-1 font-mono text-[12.5px] tabular-nums">
                      {cs.electricity.word.invoice}: {kc(p.invoiced_total_haler)}
                    </p>
                    {p.invoiced_vt_dkwh !== null && p.invoiced_nt_dkwh !== null && (
                      <p className="mt-0.5 font-mono text-[12px] text-muted tabular-nums">
                        {kwh(p.invoiced_vt_dkwh)} VT · {kwh(p.invoiced_nt_dkwh)} NT
                      </p>
                    )}
                  </>
                ) : (
                  <p className="mt-1 text-[12px] text-subtle">{cs.electricity.period.invoiceHint}</p>
                )}
              </div>
            ))}
          </div>
        </section>
      )}
    </div>
  )
}

function KwhColumn({ month, max }: { month: ElHistoryMonth; max: number }) {
  const total = month.vt_dkwh + month.nt_dkwh
  const h = Math.max(4, (total / max) * 140)
  const vtH = total > 0 ? (month.vt_dkwh / total) * h : 0
  const ntH = h - vtH
  const label = `${monthLabelShort(month.month)}: ${cs.electricity.word.vtLong} ${kwh(month.vt_dkwh)}, ${cs.electricity.word.ntLong} ${kwh(month.nt_dkwh)}`

  return (
    <div className="flex w-14 flex-none flex-col items-center gap-1">
      <span className="font-mono text-[10.5px] text-muted tabular-nums">
        {Math.round(total / 10)}
        {month.is_approximate && <ApproxMark />}
      </span>
      {/* Stacked, with a 2 px surface gap between the two segments so the
          boundary is a shape rather than a colour transition — the same rule the
          share bar follows. */}
      <div
        role="img"
        aria-label={label}
        className="flex w-9 flex-col justify-end overflow-hidden rounded-t"
        style={{ height: `${h}px` }}
      >
        <div className="w-full flex-none bg-el-nt" style={{ height: `${ntH}px`, backgroundImage: NT_HATCH }} />
        <div className="h-[2px] w-full flex-none bg-s1" />
        <div className="w-full flex-none bg-el-vt" style={{ height: `${Math.max(0, vtH - 2)}px` }} />
      </div>
      <span className="font-mono text-[10.5px] text-subtle tabular-nums">
        {monthLabelShort(month.month)}
      </span>
      {/* The Kč beside the approximate column, in the EXACT treatment (no ≈, no
          --el-approx). The two kinds of number are adjacent by design here, which
          is exactly why they must not share a visual language.
          kc(), not kc2(): a month total is a TOTAL, and the same figure appears
          again in "Náklady po měsících" below — two precisions for one number on
          one screen would undo the whole per-figure-type rule. */}
      <span className="font-mono text-[10px] text-muted tabular-nums">{kc(month.total_haler)}</span>
    </div>
  )
}

const NT_HATCH =
  'repeating-linear-gradient(135deg, oklch(1 0 0 / 0.30) 0 3px, transparent 3px 7px)'
