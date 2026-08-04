import type { ReactNode } from 'react'
import type { DashboardReminder, DashboardTask } from '@/api/types'
import { HoldToComplete } from '@/components/common/HoldToComplete'
import { cn } from '@/lib/utils'

// WidgetComponentProps is the contract every widget body implements. The host
// passes the provider payload (data), the caller's write permission, and the
// shared open/complete handlers — so a widget carries no data-fetching of its own
// (mirrors the backend WidgetProvider contract).
export interface WidgetComponentProps {
  data: unknown
  canWrite: boolean
  onOpenCard: (cardId: string, boardId: string) => void
  onOpenReminder: (eventId: string, occurrenceOn: string) => void
  onOpenNote: (noteId: string) => void
  onCompleteTask: (t: DashboardTask) => void
  onCompleteReminder: (r: DashboardReminder) => void
}

// WidgetRow is the shared row shell used inside widgets: a tappable body that
// opens the detail dialog, plus the optional 2000 ms press-and-hold done control
// (D22, with its immediate keyboard path — see HoldToComplete).
export function WidgetRow({
  onOpen,
  canWrite,
  onComplete,
  completeLabel,
  tint,
  children,
}: {
  onOpen: () => void
  canWrite: boolean
  onComplete?: () => void
  completeLabel?: string
  tint?: boolean
  children: ReactNode
}) {
  return (
    <li
      className={cn(
        'flex items-center gap-3 rounded-lg border p-3',
        tint ? 'border-danger/40 bg-danger/5' : 'border-border bg-s2',
      )}
    >
      <button type="button" onClick={onOpen} className="min-w-0 flex-1 text-left">
        {children}
      </button>
      {canWrite && onComplete && completeLabel && <HoldToComplete label={completeLabel} onComplete={onComplete} />}
    </li>
  )
}

// WidgetEmpty is a widget's calm "nothing here" body (per-widget, distinct from
// the whole-dashboard empty state).
export function WidgetEmpty({ text }: { text: string }) {
  return <p className="px-1 py-4 text-center text-[13px] text-muted">{text}</p>
}
