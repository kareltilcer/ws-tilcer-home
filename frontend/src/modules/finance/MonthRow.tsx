import { ChevronDown, ChevronRight, Pencil, Trash2 } from 'lucide-react'
import type { FinanceMonth } from './api/types'
import { cs } from '@/i18n/cs'
import { monthLabel } from '@/i18n/format'
import { czPlural, PLURAL } from '@/i18n/plural'
import { Button } from '@/components/ui/ui'
import { cn } from '@/lib/utils'
import { fmtMoney } from './buckets'
import { AllocationBar } from './AllocationBar'
import { MonthFlow } from './MonthFlow'
import { savingsOf } from './split'

/** monthsAgo is the quiet second line under a month label — "tento měsíc",
 *  "před měsícem", "před 5 měsíci". A once-a-month screen has no muscle memory,
 *  so saying where you are in time is worth the pixels.
 *
 *  Future months get their own wording rather than falling into "tento měsíc":
 *  the form deliberately offers next month, so a row entered early would
 *  otherwise claim to be now, directly above the row that actually is. */
function monthsAgo(ym: string, now: Date): string {
  const [y, m] = ym.split('-').map(Number)
  const n = now.getFullYear() * 12 + now.getMonth() + 1 - (y * 12 + m)
  if (n === 0) return 'tento měsíc'
  if (n === -1) return 'příští měsíc'
  // "za 2 měsíce" / "za 5 měsíců" needs the plural triple; "před … měsíci" does not.
  if (n < 0) return `za ${-n} ${czPlural(-n, PLURAL.months)}`
  if (n === 1) return 'před měsícem'
  // "před 2 měsíci" and "před 5 měsíci" take the same form in Czech, so unlike
  // the count labels this one needs no plural triple.
  return `před ${n} měsíci`
}

export function MonthRow({
  month,
  open,
  onToggle,
  canWrite,
  writeTitle,
  onEdit,
  onDelete,
}: {
  month: FinanceMonth
  open: boolean
  onToggle: () => void
  canWrite: boolean
  writeTitle?: string
  onEdit: () => void
  onDelete: () => void
}) {
  const label = monthLabel(month.month)
  return (
    <li className="border-b border-border last:border-b-0">
      <button
        type="button"
        onClick={onToggle}
        aria-expanded={open}
        className={cn(
          'flex w-full items-center gap-3 px-3.5 py-3 text-left transition-colors hover:bg-s2 md:gap-4',
          open && 'bg-s2',
        )}
      >
        <span className="flex-none text-subtle" aria-hidden>
          {open ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
        </span>

        {/* Two layouts, because the row has four things to show and 375 px has
            room for two of them side by side. Desktop keeps the columns aligned
            down the list; mobile stacks them so the bar survives at full width —
            it is the at-a-glance answer, and dropping it would leave the narrow
            screen with only numbers. */}
        <span className="min-w-0 flex-1 md:hidden">
          <span className="flex items-baseline gap-2">
            {/* Czech month names stay lower-case even at the start of a label, and
                the stat tiles above say "srpen 2026" too — no capitalize here. */}
            <span className="truncate text-[13.5px] font-bold">{label}</span>
            <span className="ml-auto font-mono text-[13px] tabular-nums">{fmtMoney(month.split.total_income)}</span>
          </span>
          {/* decorative: the bar sits inside the row's toggle button, whose
              accessible name would otherwise absorb all four bucket labels and
              amounts. MonthFlow spells them out when the row expands. */}
          <AllocationBar split={month.split} height={12} markMinPercent={16} className="mt-1.5" decorative />
          <span className="mt-1 flex items-center gap-2 font-mono text-[10.5px] text-subtle">
            <span>{monthsAgo(month.month, new Date())}</span>
            <span className="ml-auto">
              {cs.finance.toSavings.toLowerCase()} {fmtMoney(savingsOf(month.split))}
            </span>
          </span>
        </span>

        <span className="hidden w-[150px] flex-none md:block">
          <span className="block truncate text-sm font-bold">{label}</span>
          <span className="block font-mono text-[10.5px] text-subtle">{monthsAgo(month.month, new Date())}</span>
        </span>
        <AllocationBar split={month.split} className="hidden min-w-0 flex-1 md:flex" decorative />
        <span className="hidden w-[130px] flex-none text-right font-mono text-sm tabular-nums md:block">
          {fmtMoney(month.split.total_income)}
        </span>
        <span className="hidden w-[120px] flex-none text-right font-mono text-[13px] tabular-nums text-muted md:block">
          {fmtMoney(savingsOf(month.split))}
        </span>
      </button>

      {open && (
        <div className="bg-s2 px-3.5 pb-4 pt-0.5">
          <MonthFlow month={month} />
          <div className="mt-3 flex gap-2.5">
            {/* Reader discipline: write controls stay VISIBLE and disabled, so the
                screen reads the same for everyone (offline does the same). */}
            <Button disabled={!canWrite} title={writeTitle} onClick={onEdit}>
              <Pencil size={15} aria-hidden /> {cs.finance.edit}
            </Button>
            <Button variant="danger" disabled={!canWrite} title={writeTitle} onClick={onDelete}>
              <Trash2 size={15} aria-hidden /> {cs.finance.remove}
            </Button>
          </div>
        </div>
      )}
    </li>
  )
}
