import { useQuery } from '@tanstack/react-query'
import { CalendarDays, Check, Repeat } from 'lucide-react'
import { qk } from '@/api/keys'
import * as api from '@/api/endpoints'
import type { ReminderLead } from '@/api/types'
import { ResponsiveModal } from '@/components/ui/modal'
import { Button, Spinner } from '@/components/ui/ui'
import { fmtDateISO } from '@/i18n/format'
import { MarkdownView } from './MarkdownView'
import { LinksEditor } from './LinksEditor'

const LEAD_LABEL: Record<ReminderLead, string> = {
  '1d': '1 den předem',
  '2d': '2 dny předem',
  '1w': 'týden předem',
  '2w': '2 týdny předem',
  '1m': 'měsíc předem',
}

function recurrenceLabel(rrule: string | null): string {
  if (!rrule) return 'Neopakuje se'
  if (rrule.includes('WEEKLY')) return 'Opakuje se týdně'
  if (rrule.includes('MONTHLY')) return 'Opakuje se měsíčně'
  if (rrule.includes('YEARLY')) return 'Opakuje se ročně'
  return 'Opakuje se'
}

// EventDetail is the shared read view for an event (Okno + Nástěnka). When
// onComplete is provided (a reminder opened from the dashboard) it shows a
// single-activation "✓ Hotovo" — the deliberate step is opening the dialog.
export function EventDetail({
  eventId,
  occurrenceOn,
  open,
  onOpenChange,
  onComplete,
  readOnly = false,
}: {
  eventId: string
  occurrenceOn?: string
  open: boolean
  onOpenChange: (open: boolean) => void
  onComplete?: () => void
  readOnly?: boolean
}) {
  const ev = useQuery({ queryKey: qk.event(eventId), queryFn: () => api.getEvent(eventId), enabled: open })
  const e = ev.data

  return (
    <ResponsiveModal
      open={open}
      onOpenChange={onOpenChange}
      title={e?.title ?? '…'}
      footer={
        !readOnly && onComplete ? (
          <Button
            variant="primary"
            onClick={() => {
              onComplete()
              onOpenChange(false)
            }}
          >
            <Check size={16} aria-hidden /> Hotovo
          </Button>
        ) : undefined
      }
    >
      {ev.isLoading || !e ? (
        <div className="grid place-items-center py-10">
          <Spinner />
        </div>
      ) : (
        <div className="space-y-5">
          <div className="flex flex-wrap items-center gap-x-4 gap-y-1.5 text-sm">
            <span className="inline-flex items-center gap-1.5 text-fg">
              <CalendarDays size={15} className="text-muted" aria-hidden />
              {fmtDateISO(occurrenceOn ?? e.starts_on)}
            </span>
            <span className="inline-flex items-center gap-1.5 text-muted">
              <Repeat size={14} aria-hidden />
              {recurrenceLabel(e.rrule)}
            </span>
            {e.reminder_enabled && e.reminder_lead && (
              <span className="text-muted">🔔 {LEAD_LABEL[e.reminder_lead]}</span>
            )}
          </div>

          <section>
            <h3 className="mb-2 text-[11px] font-semibold uppercase tracking-wide text-subtle">Popis</h3>
            <MarkdownView>{e.description ?? ''}</MarkdownView>
          </section>

          <section>
            <h3 className="mb-2 text-[11px] font-semibold uppercase tracking-wide text-subtle">Odkazy</h3>
            <LinksEditor links={e.links} readOnly onAdd={() => {}} onRemove={() => {}} />
          </section>
        </div>
      )}
    </ResponsiveModal>
  )
}
