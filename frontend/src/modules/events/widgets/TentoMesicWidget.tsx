import { Repeat } from 'lucide-react'
import type { Occurrence, TentoMesicWidget as TentoMesicData } from '@/api/types'
import { WidgetEmpty, WidgetRow, type WidgetComponentProps } from '@/platform/widgets/shared'

// TentoMesicWidget renders the events.tento-mesic payload: a read-only look-ahead
// of upcoming occurrences through the end of the month (FR-E8). No done control —
// tapping a row opens the event detail.
export function TentoMesicWidget({ data, onOpenReminder }: WidgetComponentProps) {
  const occurrences = (data as TentoMesicData | undefined)?.occurrences ?? []
  if (occurrences.length === 0) return <WidgetEmpty text="Do konce měsíce už nic." />
  return (
    <ul className="space-y-2">
      {occurrences.map((o) => (
        <OccurrenceRow key={`${o.event_id}-${o.occurrence_on}`} o={o} onOpen={() => onOpenReminder(o.event_id, o.occurrence_on)} />
      ))}
    </ul>
  )
}

function OccurrenceRow({ o, onOpen }: { o: Occurrence; onOpen: () => void }) {
  return (
    <WidgetRow onOpen={onOpen} canWrite={false}>
      <div className="flex items-center gap-2">
        <span className="text-sm font-medium text-fg">{o.title}</span>
        {o.recurring && <Repeat size={13} className="text-subtle" aria-label="opakuje se" />}
      </div>
      <div className="mt-0.5 text-[12.5px] text-muted">{o.occurrence_on}</div>
    </WidgetRow>
  )
}
