import { useNavigate } from 'react-router-dom'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { Sprout } from 'lucide-react'
import type { WidgetComponentProps } from '@/platform/widgets/shared'
import { WidgetRow } from '@/platform/widgets/shared'
import { qk } from '@/api/keys'
import { routes } from '@/app/routes'
import { cs } from '@/i18n/cs'
import { count, PLURAL } from '@/i18n/plural'
import { completeTask } from '../api/endpoints'
import type { GardenPraceItem, GardenPraceWidget } from '../api/types'

// THE garden.prace WIDGET (FR-G16, D123) — wide, all roles, and the module's
// ONLY widget. Harvest surfaces here as a `harvest` task rather than as a
// separate card that would be dead weight from November to May.
//
// Two things about its shape are deliberate:
//
//   - OVERDUE COMES FIRST AND SEPARATELY. Work whose window has passed is a
//     different question from work coming up, and merging them into one sorted
//     list buries it.
//   - THE QUIET STATE IS A CORRECT ANSWER, not an empty one. From November to
//     February "na zahradě je teď klid" is this widget's normal state, and it
//     must not look broken.
export function PraceWidget({ data, canWrite }: WidgetComponentProps) {
  const navigate = useNavigate()
  const qc = useQueryClient()
  const w = data as GardenPraceWidget | undefined

  const complete = useMutation({
    // meta.via="dashboard" keeps the entity's timeline complete regardless of
    // which screen touched it — the cross-module attribution rule.
    mutationFn: (id: string) => completeTask(id, { via: 'dashboard' }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.dashboard })
      void qc.invalidateQueries({ queryKey: qk.gardenAll })
    },
    onError: () => toast.error(cs.common.errorTitle),
  })

  if (!w) return null

  if (w.state === 'quiet') {
    return (
      <div className="grid place-items-center px-1 py-6 text-center">
        <Sprout size={22} className="mb-2 text-subtle" aria-hidden />
        <p className="text-[13px] font-semibold">{cs.garden.widgetQuiet}</p>
        <p className="mt-0.5 text-[12px] text-muted text-pretty">{cs.garden.widgetQuietBody}</p>
      </div>
    )
  }

  const openCalendar = () => navigate(`${routes.zahrada}/kalendar`)

  return (
    <div className="space-y-3">
      {w.overdue && w.overdue.length > 0 && (
        <section>
          <h4 className="mb-1.5 px-1 text-[11px] font-bold tracking-wide text-danger uppercase">
            {cs.garden.overdue} · {count(w.overdue.length, PLURAL.works)}
          </h4>
          <ul className="space-y-2">
            {w.overdue.map((item) => (
              <TaskLine
                key={item.id}
                item={item}
                canWrite={canWrite}
                onOpen={openCalendar}
                onComplete={() => complete.mutate(item.id)}
              />
            ))}
          </ul>
        </section>
      )}

      {(w.weeks ?? []).map((week) => (
        <section key={week.week}>
          <h4 className="mb-1.5 px-1 text-[11px] font-bold tracking-wide text-subtle uppercase">
            {cs.garden.week(week.week)}
          </h4>
          <ul className="space-y-2">
            {week.items.map((item) => (
              <TaskLine
                key={item.id}
                item={item}
                canWrite={canWrite}
                onOpen={openCalendar}
                onComplete={() => complete.mutate(item.id)}
              />
            ))}
          </ul>
        </section>
      ))}
    </div>
  )
}

function TaskLine({
  item,
  canWrite,
  onOpen,
  onComplete,
}: {
  item: GardenPraceItem
  canWrite: boolean
  onOpen: () => void
  onComplete: () => void
}) {
  return (
    <WidgetRow
      onOpen={onOpen}
      canWrite={canWrite}
      onComplete={onComplete}
      completeLabel={cs.garden.markDone}
      tint={item.overdue}
    >
      <div className="min-w-0">
        <div className="truncate text-sm font-semibold">{item.title_cs}</div>
        <div className="mt-0.5 flex flex-wrap items-center gap-x-2 gap-y-0.5 font-mono text-[11px] text-muted">
          {/* A window, written as a range — never a single date. */}
          <span>{item.window_cs}</span>
          {item.bed_code && <span className="text-subtle">· {item.bed_code}</span>}
        </div>
      </div>
    </WidgetRow>
  )
}
