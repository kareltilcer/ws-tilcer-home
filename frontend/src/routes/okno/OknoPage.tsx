import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { Bell, ChevronLeft, ChevronRight, Plus, Repeat, Trash2 } from 'lucide-react'
import { qk } from '@/api/keys'
import * as api from '@/api/endpoints'
import type { Occurrence } from '@/api/types'
import { useAuth } from '@/app/auth'
import { fmtDateISO, monthLabel } from '@/i18n/format'
import { Button, Spinner } from '@/components/ui/ui'
import { ScreenHeader } from '@/components/common/states'
import { EventForm } from '@/components/common/EventForm'
import { EventDetail } from '@/components/common/EventDetail'

const WINDOW_MONTHS = 6
const pad = (n: number) => String(n).padStart(2, '0')

export function OknoPage() {
  const { canWrite } = useAuth()
  const qc = useQueryClient()
  const now = new Date()
  const [anchor, setAnchor] = useState({ y: now.getFullYear(), m: now.getMonth() })

  const from = `${anchor.y}-${pad(anchor.m + 1)}-01`
  const end = new Date(anchor.y, anchor.m + WINDOW_MONTHS, 0)
  const to = `${end.getFullYear()}-${pad(end.getMonth() + 1)}-${pad(end.getDate())}`

  const occ = useQuery({ queryKey: qk.events({ from, to }), queryFn: () => api.getOccurrences(from, to) })

  const [createOpen, setCreateOpen] = useState(false)
  const [editId, setEditId] = useState<string | null>(null)
  const [viewEvent, setViewEvent] = useState<{ eventId: string; occurrenceOn: string } | null>(null)

  const del = useMutation({
    mutationFn: (id: string) => api.deleteEvent(id),
    onSuccess: () => {
      toast.success('Událost smazána')
      void qc.invalidateQueries({ queryKey: ['events'] })
      void qc.invalidateQueries({ queryKey: qk.dashboard })
    },
    onError: () => toast.error('Smazání se nezdařilo'),
  })

  const shift = (delta: number) => {
    const d = new Date(anchor.y, anchor.m + delta, 1)
    setAnchor({ y: d.getFullYear(), m: d.getMonth() })
  }

  const months = occ.data?.months ?? []
  const hasAny = months.some((m) => m.occurrences.length > 0)

  return (
    <div className="mx-auto max-w-3xl">
      <ScreenHeader title="Okno do budoucnosti" subtitle="Nadcházející události podle měsíců" />

      <div className="mb-5 flex items-center gap-2">
        <Button size="sm" variant="secondary" onClick={() => shift(-WINDOW_MONTHS)} aria-label="Předchozí měsíce">
          <ChevronLeft size={16} aria-hidden />
        </Button>
        <Button size="sm" variant="secondary" onClick={() => shift(WINDOW_MONTHS)} aria-label="Další měsíce">
          <ChevronRight size={16} aria-hidden />
        </Button>
        <span className="text-sm text-muted">{monthLabel(from.slice(0, 7))} –</span>
        <div className="flex-1" />
        {canWrite && (
          <Button variant="primary" size="sm" onClick={() => setCreateOpen(true)}>
            <Plus size={15} aria-hidden /> Nová událost
          </Button>
        )}
      </div>

      {occ.isLoading ? (
        <div className="grid place-items-center py-16">
          <Spinner />
        </div>
      ) : !hasAny ? (
        <div className="rounded-lg border border-dashed border-border bg-s1 p-8 text-center text-sm text-muted">
          V tomto období nejsou žádné události.
        </div>
      ) : (
        <div className="space-y-6">
          {months
            .filter((m) => m.occurrences.length > 0)
            .map((m) => (
              <section key={m.month}>
                <h2 className="mb-2 text-sm font-bold capitalize text-fg">{monthLabel(m.month)}</h2>
                <ul className="space-y-1.5">
                  {m.occurrences.map((o) => (
                    <OccurrenceRow
                      key={`${o.event_id}-${o.occurrence_on}`}
                      o={o}
                      canWrite={canWrite}
                      onOpen={() =>
                        canWrite ? setEditId(o.event_id) : setViewEvent({ eventId: o.event_id, occurrenceOn: o.occurrence_on })
                      }
                      onDelete={() => del.mutate(o.event_id)}
                    />
                  ))}
                </ul>
              </section>
            ))}
        </div>
      )}

      {createOpen && <EventForm open onOpenChange={(o) => !o && setCreateOpen(false)} />}
      {editId && <EventForm eventId={editId} open onOpenChange={(o) => !o && setEditId(null)} />}
      {viewEvent && (
        <EventDetail
          eventId={viewEvent.eventId}
          occurrenceOn={viewEvent.occurrenceOn}
          open
          onOpenChange={(o) => !o && setViewEvent(null)}
          readOnly
        />
      )}
    </div>
  )
}

function OccurrenceRow({
  o,
  canWrite,
  onOpen,
  onDelete,
}: {
  o: Occurrence
  canWrite: boolean
  onOpen: () => void
  onDelete: () => void
}) {
  return (
    <li className="flex items-center gap-3 rounded-lg border border-border bg-s1 p-3">
      <div className="w-24 flex-none text-[13px] font-semibold text-muted">{fmtDateISO(o.occurrence_on)}</div>
      <button type="button" onClick={onOpen} className="min-w-0 flex-1 text-left">
        <span className="text-sm font-medium text-fg">{o.title}</span>
        <span className="ml-2 inline-flex items-center gap-2 align-middle text-subtle">
          {o.recurring && <Repeat size={13} aria-label="opakuje se" />}
          {o.reminder_enabled && <Bell size={13} aria-label="připomínka" />}
        </span>
      </button>
      {canWrite && (
        <button
          type="button"
          onClick={onDelete}
          className="flex-none text-subtle hover:text-danger"
          aria-label={`Smazat událost ${o.title}`}
        >
          <Trash2 size={15} aria-hidden />
        </button>
      )}
    </li>
  )
}
