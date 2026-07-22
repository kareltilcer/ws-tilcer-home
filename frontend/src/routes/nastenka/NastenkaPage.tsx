import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { Repeat } from 'lucide-react'
import { qk } from '@/api/keys'
import * as api from '@/api/endpoints'
import type { Dashboard, DashboardReminder, DashboardTask } from '@/api/types'
import { cs } from '@/i18n/cs'
import { count, PLURAL } from '@/i18n/plural'
import { daysUntilLabel } from '@/i18n/format'
import { cn } from '@/lib/utils'
import { useAuth } from '@/app/auth'
import { Spinner } from '@/components/ui/ui'
import { EmptySuccess, ScreenHeader } from '@/components/common/states'
import { HoldToComplete } from '@/components/common/HoldToComplete'
import { CardDetail } from '@/components/common/CardDetail'
import { EventDetail } from '@/components/common/EventDetail'

export function NastenkaPage() {
  const { canWrite } = useAuth()
  const qc = useQueryClient()
  const dash = useQuery({ queryKey: qk.dashboard, queryFn: api.getDashboard })

  const [card, setCard] = useState<{ cardId: string; boardId: string } | null>(null)
  const [reminder, setReminder] = useState<{ eventId: string; occurrenceOn: string } | null>(null)

  const completeReminder = useMutation({
    mutationFn: (r: DashboardReminder) => api.completeReminder(r.event_id, r.occurrence_on, 'dashboard'),
    onMutate: async (r) => {
      await qc.cancelQueries({ queryKey: qk.dashboard })
      const prev = qc.getQueryData<Dashboard>(qk.dashboard)
      qc.setQueryData<Dashboard>(qk.dashboard, (d) =>
        d ? { ...d, reminders: d.reminders.filter((x) => x.event_id !== r.event_id) } : d,
      )
      return { prev }
    },
    onError: (_e, _r, ctx) => {
      if (ctx?.prev) qc.setQueryData(qk.dashboard, ctx.prev)
      toast.error('Nepodařilo se odškrtnout připomínku')
    },
    onSettled: () => void qc.invalidateQueries({ queryKey: qk.dashboard }),
  })

  const completeTask = useMutation({
    mutationFn: (t: DashboardTask) =>
      t.done_column_id
        ? api.moveCard(t.card_id, { column_id: t.done_column_id }, 'dashboard')
        : api.updateCard(t.card_id, { archived: true }),
    onMutate: async (t) => {
      await qc.cancelQueries({ queryKey: qk.dashboard })
      const prev = qc.getQueryData<Dashboard>(qk.dashboard)
      qc.setQueryData<Dashboard>(qk.dashboard, (d) =>
        d ? { ...d, tasks: d.tasks.filter((x) => x.card_id !== t.card_id) } : d,
      )
      return { prev }
    },
    onError: (_e, _t, ctx) => {
      if (ctx?.prev) qc.setQueryData(qk.dashboard, ctx.prev)
      toast.error('Nepodařilo se dokončit úkol')
    },
    onSettled: () => void qc.invalidateQueries({ queryKey: qk.dashboard }),
  })

  if (dash.isLoading) {
    return (
      <div className="mx-auto max-w-3xl">
        <ScreenHeader title={cs.dashboard.title} subtitle={cs.dashboard.subtitle} />
        <div className="grid place-items-center py-16">
          <Spinner />
        </div>
      </div>
    )
  }

  const data = dash.data ?? { reminders: [], tasks: [] }
  const empty = data.reminders.length === 0 && data.tasks.length === 0

  return (
    <div className="mx-auto max-w-3xl">
      <ScreenHeader title={cs.dashboard.title} subtitle={cs.dashboard.subtitle} />

      {empty ? (
        <EmptySuccess title={cs.dashboard.emptyTitle} body={cs.dashboard.emptyBody} />
      ) : (
        <div className="space-y-8">
          {data.reminders.length > 0 && (
            <section>
              <SectionHeader label={cs.dashboard.remindersHeading} n={data.reminders.length} forms={PLURAL.reminders} />
              <ul className="space-y-2">
                {data.reminders.map((r) => (
                  <ReminderRow
                    key={r.event_id}
                    r={r}
                    canWrite={canWrite}
                    onOpen={() => setReminder({ eventId: r.event_id, occurrenceOn: r.occurrence_on })}
                    onComplete={() => completeReminder.mutate(r)}
                  />
                ))}
              </ul>
            </section>
          )}

          {data.tasks.length > 0 && (
            <section>
              <SectionHeader label={cs.dashboard.tasksHeading} n={data.tasks.length} forms={PLURAL.tasks} />
              <TaskList
                tasks={data.tasks}
                canWrite={canWrite}
                onOpen={(t) => setCard({ cardId: t.card_id, boardId: t.board_id })}
                onComplete={(t) => completeTask.mutate(t)}
              />
            </section>
          )}
        </div>
      )}

      {card && (
        <CardDetail
          cardId={card.cardId}
          boardId={card.boardId}
          open
          onOpenChange={(o) => !o && setCard(null)}
          readOnly={!canWrite}
        />
      )}
      {reminder && (
        <EventDetail
          eventId={reminder.eventId}
          occurrenceOn={reminder.occurrenceOn}
          open
          onOpenChange={(o) => !o && setReminder(null)}
          readOnly={!canWrite}
          onComplete={
            canWrite
              ? () => {
                  const r = data.reminders.find((x) => x.event_id === reminder.eventId)
                  if (r) completeReminder.mutate(r)
                }
              : undefined
          }
        />
      )}
    </div>
  )
}

function SectionHeader({ label, n, forms }: { label: string; n: number; forms: readonly [string, string, string] }) {
  return (
    <div className="mb-3 flex items-baseline justify-between">
      <h2 className="text-lg font-bold">{label}</h2>
      <span className="text-[13px] text-subtle">{count(n, forms)}</span>
    </div>
  )
}

function Row({
  onOpen,
  canWrite,
  onComplete,
  completeLabel,
  tint,
  children,
}: {
  onOpen: () => void
  canWrite: boolean
  onComplete: () => void
  completeLabel: string
  tint?: boolean
  children: React.ReactNode
}) {
  return (
    <li
      className={cn(
        'flex items-center gap-3 rounded-lg border bg-s1 p-3',
        tint ? 'border-danger/40 bg-danger/5' : 'border-border',
      )}
    >
      <button type="button" onClick={onOpen} className="min-w-0 flex-1 text-left">
        {children}
      </button>
      {canWrite && <HoldToComplete label={completeLabel} onComplete={onComplete} />}
    </li>
  )
}

function ReminderRow({
  r,
  canWrite,
  onOpen,
  onComplete,
}: {
  r: DashboardReminder
  canWrite: boolean
  onOpen: () => void
  onComplete: () => void
}) {
  return (
    <Row onOpen={onOpen} canWrite={canWrite} onComplete={onComplete} completeLabel={`Odškrtnout „${r.title}“`} tint={r.overdue}>
      <div className="flex items-center gap-2">
        <span className={cn('text-sm font-semibold', r.overdue ? 'text-danger' : 'text-fg')}>{r.title}</span>
        {r.recurring && <Repeat size={13} className="text-subtle" aria-label="opakuje se" />}
      </div>
      <div className={cn('mt-0.5 text-[12.5px]', r.overdue ? 'text-danger/90' : 'text-muted')}>
        {daysUntilLabel(r.days_until)} · {r.occurrence_on}
      </div>
    </Row>
  )
}

function TaskList({
  tasks,
  canWrite,
  onOpen,
  onComplete,
}: {
  tasks: DashboardTask[]
  canWrite: boolean
  onOpen: (t: DashboardTask) => void
  onComplete: (t: DashboardTask) => void
}) {
  // Group by board only when more than one board contributes (design gap #2).
  const boards = Array.from(new Set(tasks.map((t) => t.board_id)))
  const grouped = boards.length > 1

  if (!grouped) {
    return (
      <ul className="space-y-2">
        {tasks.map((t) => (
          <TaskRow key={t.card_id} t={t} canWrite={canWrite} onOpen={() => onOpen(t)} onComplete={() => onComplete(t)} showBoard={false} />
        ))}
      </ul>
    )
  }
  return (
    <div className="space-y-4">
      {boards.map((bid) => {
        const inBoard = tasks.filter((t) => t.board_id === bid)
        return (
          <div key={bid}>
            <h3 className="mb-1.5 text-[11px] font-semibold uppercase tracking-wide text-subtle">{inBoard[0].board_name}</h3>
            <ul className="space-y-2">
              {inBoard.map((t) => (
                <TaskRow key={t.card_id} t={t} canWrite={canWrite} onOpen={() => onOpen(t)} onComplete={() => onComplete(t)} showBoard={false} />
              ))}
            </ul>
          </div>
        )
      })}
    </div>
  )
}

function TaskRow({
  t,
  canWrite,
  onOpen,
  onComplete,
  showBoard,
}: {
  t: DashboardTask
  canWrite: boolean
  onOpen: () => void
  onComplete: () => void
  showBoard: boolean
}) {
  const prog = t.checklist_progress
  return (
    <Row onOpen={onOpen} canWrite={canWrite} onComplete={onComplete} completeLabel={`Dokončit „${t.title}“`}>
      <div className="text-sm font-medium text-fg">{t.title}</div>
      <div className="mt-0.5 flex flex-wrap items-center gap-2 text-[12px] text-subtle">
        <span>{showBoard ? `${t.board_name} · ${t.column_name}` : t.column_name}</span>
        {prog.total > 0 && (
          <span>
            ☑ {prog.done}/{prog.total}
          </span>
        )}
        {t.label_ids.length > 0 && <span>🏷 {t.label_ids.length}</span>}
      </div>
    </Row>
  )
}
