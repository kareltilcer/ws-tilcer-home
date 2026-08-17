import { useNavigate } from 'react-router-dom'
import { Wallet } from 'lucide-react'
import type { RozpocetWidget as RozpocetData } from '@/api/types'
import type { WidgetComponentProps } from '@/platform/widgets/shared'
import { routes } from '@/app/routes'
import { cs } from '@/i18n/cs'
import { monthLabel } from '@/i18n/format'
import { Button } from '@/components/ui/ui'
import { displayNeeds, fmtMoney } from '@/routes/finance/buckets'

// The finance.rozpocet widget (FR-F7, D88) — two states, and the SECOND one
// matters more.
//
//   recorded → the month's headline numbers, tapping opens Finance;
//   missing  → "Zadat ⟨srpen 2026⟩", linking to the add form.
//
// The missing state is the reason this widget exists. The app's actual failure
// mode is a month nobody entered, and this is the only thing on the household's
// landing page that will notice — so it is designed as a small, friendly
// obligation, not as a widget with nothing to say.
export function RozpocetWidget({ data, canWrite }: WidgetComponentProps) {
  const navigate = useNavigate()
  const w = data as RozpocetData | undefined
  if (!w) return null

  const label = monthLabel(w.month)

  if (w.state === 'missing') {
    return (
      <div className="space-y-2.5">
        <p className="flex items-start gap-2 text-[13px] text-pretty text-muted">
          <Wallet size={15} className="mt-0.5 flex-none text-subtle" aria-hidden />
          <span>{cs.finance.missingTitle(label)}</span>
        </p>
        <Button
          variant="primary"
          className="w-full"
          // A reader sees the prompt and cannot act on it — the control stays
          // visible and disabled, as everywhere else in the app.
          disabled={!canWrite}
          title={canWrite ? undefined : cs.common.readOnly}
          // Straight to the ADD FORM, which is what the label promises —
          // FinancePage consumes this state and opens it. Landing on the list
          // and asking for a second click would lengthen the one path this
          // widget exists to shorten.
          onClick={() => navigate(routes.finance, { state: { add: true } })}
        >
          {cs.finance.missingAction(label)}
        </Button>
      </div>
    )
  }

  const rows = [
    { label: cs.finance.widgetKajaKeeps, value: w.personal_kaja ?? 0 },
    { label: cs.finance.widgetAndyKeeps, value: w.personal_andy ?? 0 },
    // `needs`, not what Kandy received — the savings leave that account, so the
    // qualified label is the only honest one for this number.
    { label: cs.finance.operationalNeeds, value: displayNeeds(w.needs ?? 0) },
    { label: cs.finance.toSavings, value: w.savings ?? 0 },
  ]

  // The headline is the control; the breakdown is a LIST beside it, not inside
  // it. A <button> takes phrasing content only, so wrapping the <ul> in it was
  // invalid markup — and it collapsed the whole payload into one control whose
  // accessible name was every number in a row, losing both the list semantics
  // and the label/value pairing. Same nesting as the other widgets' lists.
  return (
    <div className="space-y-2">
      <button
        type="button"
        onClick={() => navigate(routes.finance)}
        className="flex w-full items-baseline gap-2 rounded-lg text-left focus-visible:outline-2 focus-visible:outline-focus"
      >
        <span className="text-[12px] font-semibold text-muted">{label}</span>
        <span className="ml-auto font-mono text-lg font-semibold tabular-nums">{fmtMoney(w.total_income ?? 0)}</span>
      </button>
      <ul className="space-y-1">
        {rows.map((r) => (
          <li key={r.label} className="flex items-baseline gap-2 text-[12.5px]">
            <span className="min-w-0 flex-1 truncate text-muted">{r.label}</span>
            <span className="font-mono tabular-nums">{fmtMoney(r.value)}</span>
          </li>
        ))}
      </ul>
    </div>
  )
}
