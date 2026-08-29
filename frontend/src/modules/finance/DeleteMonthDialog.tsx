import { AlertTriangle } from 'lucide-react'
import type { FinanceMonth } from '@/api/types'
import { cs } from '@/i18n/cs'
import { monthLabel } from '@/i18n/format'
import { Button } from '@/components/ui/ui'
import { ResponsiveModal } from '@/components/ui/modal'
import { fmtMoney } from './buckets'

// Finance is the ONE module that deletes for real (D87). Poznámky and Dokumenty
// archive; this removes the row. That is a deliberate exception to a convention
// the rest of the app has taught the user, so the copy carries the whole weight:
// it names the month, shows what is about to go, and says plainly that it cannot
// be undone.
//
// (An admin can still read the numbers back out of the Log — the month.delete
// event carries a full-row diff — but that is a forensic record, not an undo, and
// it is deliberately not offered here as an affordance.)
export function DeleteMonthDialog({
  month,
  onOpenChange,
  deleting,
  onConfirm,
}: {
  month: FinanceMonth | null
  onOpenChange: (open: boolean) => void
  deleting: boolean
  onConfirm: () => void
}) {
  return (
    <ResponsiveModal
      open={month !== null}
      onOpenChange={onOpenChange}
      title={month ? cs.finance.deleteTitle(monthLabel(month.month)) : ''}
      footer={
        <>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            {cs.finance.cancel}
          </Button>
          <Button variant="danger" loading={deleting} onClick={onConfirm}>
            {cs.finance.deleteConfirm}
          </Button>
        </>
      }
    >
      {month && (
        <div className="flex items-start gap-3">
          <span className="grid h-10 w-10 flex-none place-items-center rounded-lg bg-danger/15 text-danger">
            <AlertTriangle size={20} aria-hidden />
          </span>
          <div className="min-w-0">
            <p className="text-sm text-pretty">{cs.finance.deleteBody}</p>
            <p className="mt-2 font-mono text-[13px] tabular-nums text-muted">
              {cs.finance.totalIncome}: {fmtMoney(month.split.total_income)}
            </p>
          </div>
        </div>
      )}
    </ResponsiveModal>
  )
}
