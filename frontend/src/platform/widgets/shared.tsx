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
  // Opens a document in a preview overlay ON Nástěnka — never a navigation away
  // (FR-DOC11).
  onOpenDocument: (documentId: string) => void
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

// PinScope is the three-way scope the two pin providers return: a row pinned to
// the household, to the caller personally, or to both.
export type PinScope = 'household' | 'personal' | 'both'

// PinScopeBadge is the chip on the right of a pinned row.
//
// ⚠ THREE DISTINCT SCOPES, NOT TWO. Collapsing `household` into the "both" badge
// discards the distinction the backend went to the trouble of computing — a row
// the household pinned is not a row you also pinned. Only `personal` is tinted
// differently, because it is the one whose pin nobody else can see.
//
// It takes its labels rather than reading `cs` itself: the badge is the same
// chip in Pripnuté poznámky and Pripnuté dokumenty, where the words come from
// `cs.notes.*` and `cs.documents.*` respectively. That difference is the only
// thing the two copies of this had.
export function PinScopeBadge({
  scope,
  labels,
}: {
  scope: PinScope
  labels: { personal: string; household: string; both: string }
}) {
  const personal = scope === 'personal'
  return (
    <span
      className={cn(
        'inline-flex h-5 flex-none items-center rounded-full px-2 text-[10.5px] font-bold',
        personal ? 'bg-s3 text-muted' : 'bg-accent-soft text-accent',
      )}
    >
      {personal ? labels.personal : scope === 'household' ? labels.household : labels.both}
    </span>
  )
}

// WidgetEmpty is a widget's calm "nothing here" body (per-widget, distinct from
// the whole-dashboard empty state).
export function WidgetEmpty({ text }: { text: string }) {
  return <p className="px-1 py-4 text-center text-[13px] text-muted">{text}</p>
}
