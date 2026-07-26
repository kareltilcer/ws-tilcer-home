import { Repeat } from 'lucide-react'
import type { DashboardReminder, PripominkyWidget as PripominkyData } from '@/api/types'
import { daysUntilLabel } from '@/i18n/format'
import { cn } from '@/lib/utils'
import { WidgetEmpty, WidgetRow, type WidgetComponentProps } from '@/platform/widgets/shared'

// PripominkyWidget renders the events.pripominky payload: active reminders,
// overdue-first with a restrained danger tint (FR-E7). Mark-done via the hold.
export function PripominkyWidget({ data, canWrite, onOpenReminder, onCompleteReminder }: WidgetComponentProps) {
  const reminders = (data as PripominkyData | undefined)?.reminders ?? []
  if (reminders.length === 0) return <WidgetEmpty text="Žádné aktivní připomínky." />
  return (
    <ul className="space-y-2">
      {reminders.map((r) => (
        <ReminderRow key={r.event_id} r={r} canWrite={canWrite} onOpenReminder={onOpenReminder} onCompleteReminder={onCompleteReminder} />
      ))}
    </ul>
  )
}

function ReminderRow({
  r,
  canWrite,
  onOpenReminder,
  onCompleteReminder,
}: {
  r: DashboardReminder
  canWrite: boolean
  onOpenReminder: (eventId: string, occurrenceOn: string) => void
  onCompleteReminder: (r: DashboardReminder) => void
}) {
  return (
    <WidgetRow
      onOpen={() => onOpenReminder(r.event_id, r.occurrence_on)}
      canWrite={canWrite}
      onComplete={() => onCompleteReminder(r)}
      completeLabel={`Odškrtnout „${r.title}“`}
      tint={r.overdue}
    >
      <div className="flex items-center gap-2">
        <span className={cn('text-sm font-semibold', r.overdue ? 'text-danger' : 'text-fg')}>{r.title}</span>
        {r.recurring && <Repeat size={13} className="text-subtle" aria-label="opakuje se" />}
      </div>
      <div className={cn('mt-0.5 text-[12.5px]', r.overdue ? 'text-danger/90' : 'text-muted')}>
        {daysUntilLabel(r.days_until)} · {r.occurrence_on}
      </div>
    </WidgetRow>
  )
}
