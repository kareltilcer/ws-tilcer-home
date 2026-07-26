import type { DashboardTask, PravedelamWidget as PravedelamData } from '@/api/types'
import { WidgetEmpty, WidgetRow, type WidgetComponentProps } from '@/platform/widgets/shared'

// PravedelamWidget renders the todo.pravedelam payload: cards in "Právě dělám"
// columns across all boards, grouped by board when more than one contributes
// (FR-T8). Mark-done uses the 2000 ms hold via WidgetRow.
export function PravedelamWidget({ data, canWrite, onOpenCard, onCompleteTask }: WidgetComponentProps) {
  const tasks = (data as PravedelamData | undefined)?.tasks ?? []
  if (tasks.length === 0) return <WidgetEmpty text="Nic ve „Právě dělám“." />

  const boards = Array.from(new Set(tasks.map((t) => t.board_id)))
  const grouped = boards.length > 1

  if (!grouped) {
    return (
      <ul className="space-y-2">
        {tasks.map((t) => (
          <TaskRow key={t.card_id} t={t} canWrite={canWrite} onOpenCard={onOpenCard} onCompleteTask={onCompleteTask} />
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
                <TaskRow key={t.card_id} t={t} canWrite={canWrite} onOpenCard={onOpenCard} onCompleteTask={onCompleteTask} />
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
  onOpenCard,
  onCompleteTask,
}: {
  t: DashboardTask
  canWrite: boolean
  onOpenCard: (cardId: string, boardId: string) => void
  onCompleteTask: (t: DashboardTask) => void
}) {
  const prog = t.checklist_progress
  return (
    <WidgetRow
      onOpen={() => onOpenCard(t.card_id, t.board_id)}
      canWrite={canWrite}
      onComplete={() => onCompleteTask(t)}
      completeLabel={`Dokončit „${t.title}“`}
    >
      <div className="text-sm font-medium text-fg">{t.title}</div>
      <div className="mt-0.5 flex flex-wrap items-center gap-2 text-[12px] text-subtle">
        <span>{t.column_name}</span>
        {prog.total > 0 && (
          <span>
            ☑ {prog.done}/{prog.total}
          </span>
        )}
        {t.label_ids.length > 0 && <span>🏷 {t.label_ids.length}</span>}
      </div>
    </WidgetRow>
  )
}
