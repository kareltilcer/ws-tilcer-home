import { useState, type ReactNode } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { ChevronDown, ChevronUp, EyeOff, Plus, SlidersHorizontal, X } from 'lucide-react'
import { qk } from '@/api/keys'
import * as api from '@/api/endpoints'
import type { Dashboard, DashboardReminder, DashboardTask, LayoutItem, WidgetCatalogEntry, WidgetSize } from '@/api/types'
import { cs } from '@/i18n/cs'
import { cn } from '@/lib/utils'
import { useAuth } from '@/app/auth'
import { useDocumentTitle } from '@/hooks/useDocumentTitle'
import { Spinner } from '@/components/ui/ui'
import { CardDetail } from '@/components/common/CardDetail'
import { EventDetail } from '@/components/common/EventDetail'
import { NoteOverlay } from '@/routes/poznamky/NoteOverlay'
import { DocumentOverlay } from '@/routes/dokumenty/DocumentOverlay'
import { widgetRegistry } from '@/platform/widgets/registry'

const catalogKey = ['dashboard', 'catalog'] as const

export function NastenkaPage() {
  useDocumentTitle(cs.dashboard.title)
  const { canWrite } = useAuth()
  const qc = useQueryClient()

  const dash = useQuery({ queryKey: qk.dashboard, queryFn: api.getDashboard })
  const catalog = useQuery({ queryKey: catalogKey, queryFn: api.getDashboardCatalog })

  const [arrange, setArrange] = useState(false)
  const [picker, setPicker] = useState(false)
  const [card, setCard] = useState<{ cardId: string; boardId: string } | null>(null)
  const [reminder, setReminder] = useState<{ eventId: string; occurrenceOn: string } | null>(null)
  const [note, setNote] = useState<string | null>(null)
  const [documentId, setDocumentId] = useState<string | null>(null)

  const saveLayout = useMutation({
    mutationFn: api.saveDashboardLayout,
    onError: () => {
      toast.error('Nepodařilo se uložit rozvržení')
      void qc.invalidateQueries({ queryKey: qk.dashboard })
    },
    onSuccess: () => void qc.invalidateQueries({ queryKey: qk.dashboard }),
  })

  const completeTask = useMutation({
    mutationFn: (t: DashboardTask) =>
      t.done_column_id
        ? api.moveCard(t.card_id, { column_id: t.done_column_id }, 'dashboard')
        : api.updateCard(t.card_id, { archived: true }),
    onError: () => toast.error('Nepodařilo se dokončit úkol'),
    onSettled: () => void qc.invalidateQueries({ queryKey: qk.dashboard }),
  })

  const completeReminder = useMutation({
    mutationFn: (r: DashboardReminder) => api.completeReminder(r.event_id, r.occurrence_on, 'dashboard'),
    onError: () => toast.error('Nepodařilo se odškrtnout připomínku'),
    onSettled: () => void qc.invalidateQueries({ queryKey: qk.dashboard }),
  })

  if (dash.isLoading) {
    return (
      <div className="mx-auto max-w-5xl">
        <Header arrange={arrange} onToggleArrange={() => setArrange((a) => !a)} onAdd={() => setPicker(true)} />
        <div className="grid place-items-center py-16">
          <Spinner />
        </div>
      </div>
    )
  }

  const layout = dash.data?.layout ?? []
  const widgetsByKey = new Map((dash.data?.widgets ?? []).map((w) => [w.key, w]))
  const entries = catalog.data ?? []
  const titleOf = (key: string) => entries.find((e) => e.key === key)?.title ?? key
  const visible = layout.filter((li) => li.visible)

  // Layout edits (optimistic; positions are re-derived server-side from order).
  const persist = (next: LayoutItem[]) => {
    qc.setQueryData<Dashboard>(qk.dashboard, (d) => (d ? { ...d, layout: next } : d))
    saveLayout.mutate(next.map((li) => ({ widget_key: li.widget_key, visible: li.visible, size: li.size })))
  }
  const move = (key: string, dir: -1 | 1) => {
    // The arrows (and canUp/canDown) act on the visible ordering, so swap with the
    // adjacent *visible* neighbor. Hidden widgets keep their place in the layout
    // instead of silently absorbing the move.
    const vi = visible.findIndex((li) => li.widget_key === key)
    const vj = vi + dir
    if (vi < 0 || vj < 0 || vj >= visible.length) return
    const i = layout.findIndex((li) => li.widget_key === key)
    const j = layout.findIndex((li) => li.widget_key === visible[vj].widget_key)
    const next = [...layout]
    ;[next[i], next[j]] = [next[j], next[i]]
    persist(next)
  }
  const resize = (key: string, size: WidgetSize) =>
    persist(layout.map((li) => (li.widget_key === key ? { ...li, size } : li)))
  const hide = (key: string) => persist(layout.map((li) => (li.widget_key === key ? { ...li, visible: false } : li)))
  const toggle = (key: string, on: boolean) => {
    if (layout.some((li) => li.widget_key === key)) {
      persist(layout.map((li) => (li.widget_key === key ? { ...li, visible: on } : li)))
    } else {
      const size = entries.find((e) => e.key === key)?.default_size ?? 'narrow'
      persist([...layout, { widget_key: key, visible: on, position: '', size }])
    }
  }

  return (
    <div className="mx-auto max-w-5xl">
      <Header arrange={arrange} onToggleArrange={() => setArrange((a) => !a)} onAdd={() => setPicker(true)} />

      {visible.length === 0 ? (
        <EmptyDashboard onAdd={() => setPicker(true)} />
      ) : (
        <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
          {visible.map((li, idx) => {
            const Comp = widgetRegistry[li.widget_key]
            if (!Comp) return null
            const inst = widgetsByKey.get(li.widget_key)
            return (
              <WidgetFrame
                key={li.widget_key}
                title={titleOf(li.widget_key)}
                size={li.size}
                arrange={arrange}
                canUp={idx > 0}
                canDown={idx < visible.length - 1}
                onResize={(s) => resize(li.widget_key, s)}
                onHide={() => hide(li.widget_key)}
                onUp={() => move(li.widget_key, -1)}
                onDown={() => move(li.widget_key, 1)}
              >
                {inst ? (
                  <Comp
                    data={inst.data}
                    canWrite={canWrite}
                    onOpenCard={(cardId, boardId) => setCard({ cardId, boardId })}
                    onOpenReminder={(eventId, occurrenceOn) => setReminder({ eventId, occurrenceOn })}
                    onOpenNote={(noteId) => setNote(noteId)}
                    onOpenDocument={(id) => setDocumentId(id)}
                    onCompleteTask={(t) => completeTask.mutate(t)}
                    onCompleteReminder={(r) => completeReminder.mutate(r)}
                  />
                ) : (
                  <div className="grid place-items-center py-6">
                    <Spinner />
                  </div>
                )}
              </WidgetFrame>
            )
          })}
        </div>
      )}

      {picker && <CatalogPicker entries={entries} layout={layout} onToggle={toggle} onClose={() => setPicker(false)} />}

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
        />
      )}
      {note && <NoteOverlay noteId={note} readOnly={!canWrite} onClose={() => setNote(null)} />}
      {documentId && (
        <DocumentOverlay documentId={documentId} readOnly={!canWrite} onClose={() => setDocumentId(null)} />
      )}
    </div>
  )
}

function Header({ arrange, onToggleArrange, onAdd }: { arrange: boolean; onToggleArrange: () => void; onAdd: () => void }) {
  return (
    <div className="mb-5 flex items-end justify-between gap-3">
      <div>
        <h1 className="text-2xl font-extrabold tracking-tight">{cs.dashboard.title}</h1>
        <p className="mt-0.5 text-sm text-muted">{cs.dashboard.subtitle}</p>
      </div>
      <div className="flex shrink-0 items-center gap-2">
        {arrange && (
          <button
            type="button"
            onClick={onAdd}
            className="flex h-9 items-center gap-1.5 rounded-md border border-border bg-s2 px-3 text-sm font-semibold text-fg hover:bg-s3"
          >
            <Plus size={16} aria-hidden /> {cs.dashboard.addWidget}
          </button>
        )}
        <button
          type="button"
          onClick={onToggleArrange}
          aria-pressed={arrange}
          className={cn(
            'flex h-9 items-center gap-1.5 rounded-md px-3 text-sm font-semibold',
            arrange ? 'bg-accent text-accent-fg' : 'border border-border bg-s2 text-fg hover:bg-s3',
          )}
        >
          <SlidersHorizontal size={16} aria-hidden /> {arrange ? cs.dashboard.arrangeDone : cs.dashboard.arrange}
        </button>
      </div>
    </div>
  )
}

function WidgetFrame({
  title,
  size,
  arrange,
  canUp,
  canDown,
  onResize,
  onHide,
  onUp,
  onDown,
  children,
}: {
  title: string
  size: WidgetSize
  arrange: boolean
  canUp: boolean
  canDown: boolean
  onResize: (s: WidgetSize) => void
  onHide: () => void
  onUp: () => void
  onDown: () => void
  children: ReactNode
}) {
  return (
    <section
      className={cn(
        'rounded-xl border bg-s1 p-4',
        arrange ? 'border-dashed border-accent/50 ring-1 ring-accent/30' : 'border-border',
        size === 'wide' && 'md:col-span-2',
      )}
    >
      <div className="mb-3 flex items-center justify-between gap-2">
        <h2 className="text-base font-bold">{title}</h2>
        {arrange && (
          <div className="flex items-center gap-1">
            <IconBtn label="Nahoru" disabled={!canUp} onClick={onUp}>
              <ChevronUp size={15} />
            </IconBtn>
            <IconBtn label="Dolů" disabled={!canDown} onClick={onDown}>
              <ChevronDown size={15} />
            </IconBtn>
            <div className="mx-1 flex overflow-hidden rounded-md border border-border" role="group" aria-label="Šířka">
              <SizeBtn active={size === 'narrow'} onClick={() => onResize('narrow')}>
                {cs.dashboard.narrow}
              </SizeBtn>
              <SizeBtn active={size === 'wide'} onClick={() => onResize('wide')}>
                {cs.dashboard.wide}
              </SizeBtn>
            </div>
            <IconBtn label="Skrýt" onClick={onHide}>
              <EyeOff size={15} />
            </IconBtn>
          </div>
        )}
      </div>
      {children}
    </section>
  )
}

function IconBtn({ label, onClick, disabled, children }: { label: string; onClick: () => void; disabled?: boolean; children: ReactNode }) {
  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      disabled={disabled}
      onClick={onClick}
      className="grid h-8 w-8 place-items-center rounded-md border border-border bg-s2 text-fg hover:bg-s3 disabled:opacity-40"
    >
      {children}
    </button>
  )
}

function SizeBtn({ active, onClick, children }: { active: boolean; onClick: () => void; children: ReactNode }) {
  return (
    <button
      type="button"
      aria-pressed={active}
      onClick={onClick}
      className={cn('h-8 px-2.5 text-[12px] font-semibold', active ? 'bg-accent text-accent-fg' : 'bg-s2 text-muted hover:bg-s3')}
    >
      {children}
    </button>
  )
}

function EmptyDashboard({ onAdd }: { onAdd: () => void }) {
  return (
    <div className="grid place-items-center rounded-xl border border-dashed border-border bg-s1 py-16 text-center">
      <p className="mb-1 text-lg font-bold">{cs.dashboard.emptyTitle}</p>
      <p className="mb-4 max-w-sm text-sm text-muted">{cs.dashboard.emptyBody}</p>
      <button
        type="button"
        onClick={onAdd}
        className="flex h-9 items-center gap-1.5 rounded-md bg-accent px-3.5 text-sm font-bold text-accent-fg"
      >
        <Plus size={16} aria-hidden /> {cs.dashboard.addWidget}
      </button>
    </div>
  )
}

function CatalogPicker({
  entries,
  layout,
  onToggle,
  onClose,
}: {
  entries: WidgetCatalogEntry[]
  layout: LayoutItem[]
  onToggle: (key: string, on: boolean) => void
  onClose: () => void
}) {
  return (
    <div
      className="fixed inset-0 z-40 grid place-items-end bg-black/40 sm:place-items-center sm:p-6"
      onClick={onClose}
      role="presentation"
    >
      <div
        role="dialog"
        aria-label={cs.dashboard.catalogTitle}
        className="w-full rounded-t-2xl border border-border bg-s1 p-4 sm:max-w-md sm:rounded-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="mb-3 flex items-center justify-between">
          <h2 className="text-base font-bold">{cs.dashboard.catalogTitle}</h2>
          <button type="button" aria-label="Zavřít" onClick={onClose} className="grid h-8 w-8 place-items-center rounded-md hover:bg-s2">
            <X size={16} />
          </button>
        </div>
        <ul className="divide-y divide-border">
          {entries.map((e) => {
            const on = layout.find((li) => li.widget_key === e.key)?.visible ?? false
            return (
              <li key={e.key} className="flex items-center justify-between gap-3 py-3">
                <div className="min-w-0">
                  <div className="text-sm font-semibold text-fg">{e.title}</div>
                  {e.description && <div className="text-[12.5px] text-subtle">{e.description}</div>}
                </div>
                <button
                  type="button"
                  role="switch"
                  aria-checked={on}
                  onClick={() => onToggle(e.key, !on)}
                  className={cn(
                    'h-8 shrink-0 rounded-md px-3 text-[12.5px] font-semibold',
                    on ? 'bg-accent text-accent-fg' : 'border border-border bg-s2 text-fg hover:bg-s3',
                  )}
                >
                  {on ? cs.dashboard.shown : cs.dashboard.add}
                </button>
              </li>
            )
          })}
        </ul>
      </div>
    </div>
  )
}
