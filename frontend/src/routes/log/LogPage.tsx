import { useState } from 'react'
import { useInfiniteQuery, useQuery } from '@tanstack/react-query'
import * as Tabs from '@radix-ui/react-tabs'
import { ChevronDown, ChevronRight, History, SlidersHorizontal } from 'lucide-react'
import { qk } from '@/api/keys'
import * as api from '@/api/endpoints'
import type { LogFilters } from '@/api/endpoints'
import type { AuditChange, AuditEvent } from '@/api/types'
import { fmtDateTime } from '@/i18n/format'
import { cn } from '@/lib/utils'
import { useIsDesktop } from '@/hooks/useMediaQuery'
import { Button, Input, Spinner } from '@/components/ui/ui'
import { ScreenHeader } from '@/components/common/states'
import { ResponsiveModal } from '@/components/ui/modal'

const MODULES = ['', 'logging', 'todo', 'events', 'dashboard']
const LEVELS = ['', 'info', 'warn', 'error']

const selectCls =
  'h-10 rounded-md border border-border bg-s1 px-2 text-sm text-fg focus-visible:outline-2 focus-visible:outline-focus'

export function LogPage() {
  return (
    <div className="mx-auto max-w-5xl">
      <ScreenHeader title="Log" subtitle="Auditní záznam všech změn" />
      <Tabs.Root defaultValue="stream">
        <Tabs.List className="mb-4 flex gap-1 border-b border-border">
          <TabTrigger value="stream">Záznamy</TabTrigger>
          <TabTrigger value="stats">Analytika</TabTrigger>
        </Tabs.List>
        <Tabs.Content value="stream">
          <StreamView />
        </Tabs.Content>
        <Tabs.Content value="stats">
          <StatsView />
        </Tabs.Content>
      </Tabs.Root>
    </div>
  )
}

function TabTrigger({ value, children }: { value: string; children: React.ReactNode }) {
  return (
    <Tabs.Trigger
      value={value}
      className="border-b-2 border-transparent px-3 py-2 text-sm font-semibold text-muted data-[state=active]:border-accent data-[state=active]:text-fg"
    >
      {children}
    </Tabs.Trigger>
  )
}

function StreamView() {
  const desktop = useIsDesktop()
  const [filtersOpen, setFiltersOpen] = useState(false)
  const [draft, setDraft] = useState<LogFilters>({})
  const [applied, setApplied] = useState<LogFilters>({})
  const [timeline, setTimeline] = useState<{ type: string; id: string } | null>(null)

  const set = (k: keyof LogFilters, v: string) => setDraft((d) => ({ ...d, [k]: v || undefined }))
  const activeFilterCount = Object.values(applied).filter(Boolean).length

  const query = useInfiniteQuery({
    queryKey: qk.logs(applied),
    queryFn: ({ pageParam }) => api.listLogs({ ...applied, cursor: pageParam as string | undefined, limit: 50 }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.next_cursor ?? undefined,
  })

  const events = query.data?.pages.flatMap((p) => p.items) ?? []

  return (
    <>
      {!desktop && (
        <button
          type="button"
          onClick={() => setFiltersOpen((v) => !v)}
          className="mb-3 inline-flex items-center gap-2 rounded-md border border-border bg-s2 px-3 py-2 text-sm font-semibold text-fg"
        >
          <SlidersHorizontal size={15} aria-hidden /> Filtry{activeFilterCount > 0 ? ` (${activeFilterCount})` : ''}
        </button>
      )}
      <div
        className={cn(
          'mb-4 grid-cols-1 gap-2 rounded-lg border border-border bg-s1 p-3 sm:grid-cols-2 lg:grid-cols-3',
          desktop || filtersOpen ? 'grid' : 'hidden',
        )}
      >
        <select className={selectCls} value={draft.module ?? ''} onChange={(e) => set('module', e.target.value)} aria-label="Modul">
          {MODULES.map((m) => (
            <option key={m} value={m}>
              {m || 'Všechny moduly'}
            </option>
          ))}
        </select>
        <select className={selectCls} value={draft.level ?? ''} onChange={(e) => set('level', e.target.value)} aria-label="Úroveň">
          {LEVELS.map((l) => (
            <option key={l} value={l}>
              {l || 'Všechny úrovně'}
            </option>
          ))}
        </select>
        <Input placeholder="Akce (např. card.move)" value={draft.action ?? ''} onChange={(e) => set('action', e.target.value)} />
        <Input placeholder="Aktér (id)" value={draft.actor ?? ''} onChange={(e) => set('actor', e.target.value)} />
        <Input placeholder="Typ entity" value={draft.entity_type ?? ''} onChange={(e) => set('entity_type', e.target.value)} />
        <Input placeholder="ID entity" value={draft.entity_id ?? ''} onChange={(e) => set('entity_id', e.target.value)} />
        <Input type="date" aria-label="Od" value={draft.from?.slice(0, 10) ?? ''} onChange={(e) => set('from', e.target.value ? `${e.target.value}T00:00:00Z` : '')} />
        <Input type="date" aria-label="Do" value={draft.to?.slice(0, 10) ?? ''} onChange={(e) => set('to', e.target.value ? `${e.target.value}T23:59:59Z` : '')} />
        <Input placeholder="Hledat v textu…" value={draft.q ?? ''} onChange={(e) => set('q', e.target.value)} />
        <div className="flex gap-2 sm:col-span-2 lg:col-span-3">
          <Button variant="primary" size="sm" onClick={() => setApplied(draft)}>
            Filtrovat
          </Button>
          <Button variant="ghost" size="sm" onClick={() => { setDraft({}); setApplied({}) }}>
            Vymazat filtry
          </Button>
        </div>
      </div>

      {query.isLoading ? (
        <div className="grid place-items-center py-16">
          <Spinner />
        </div>
      ) : events.length === 0 ? (
        <p className="rounded-lg border border-dashed border-border bg-s1 p-8 text-center text-sm text-muted">
          Žádné záznamy pro tento filtr.
        </p>
      ) : (
        <ul className="space-y-1.5">
          {events.map((e) => (
            <LogRow key={e.id} event={e} onOpenTimeline={(type, id) => setTimeline({ type, id })} />
          ))}
        </ul>
      )}

      {query.hasNextPage && (
        <div className="mt-4 grid place-items-center">
          <Button variant="secondary" size="sm" loading={query.isFetchingNextPage} onClick={() => query.fetchNextPage()}>
            Načíst další
          </Button>
        </div>
      )}

      {timeline && (
        <EntityTimeline type={timeline.type} id={timeline.id} open onOpenChange={(o) => !o && setTimeline(null)} />
      )}
    </>
  )
}

const LEVEL_CLS: Record<string, string> = {
  info: 'bg-s3 text-muted',
  warn: 'bg-warn/20 text-warn',
  error: 'bg-danger/20 text-danger',
}

function LogRow({ event, onOpenTimeline }: { event: AuditEvent; onOpenTimeline: (type: string, id: string) => void }) {
  const [expanded, setExpanded] = useState(false)
  const detail = useQuery({ queryKey: [...qk.logs(), 'detail', event.id], queryFn: () => api.getLog(event.id), enabled: expanded })

  return (
    <li className="rounded-lg border border-border bg-s1">
      <button type="button" onClick={() => setExpanded((v) => !v)} className="flex w-full items-start gap-2 p-3 text-left">
        <span className="mt-0.5 flex-none text-subtle">{expanded ? <ChevronDown size={15} /> : <ChevronRight size={15} />}</span>
        <span className="min-w-0 flex-1">
          <span className="flex flex-wrap items-center gap-2">
            <span className={cn('rounded-full px-1.5 py-0.5 text-[10px] font-bold uppercase', LEVEL_CLS[event.level])}>{event.level}</span>
            <span className="font-mono text-[12px] text-accent">{event.module}.{event.action}</span>
            <span className="text-[12px] text-subtle">{event.actor_label ?? event.actor_user_id ?? event.actor_type}</span>
            {event.change_count > 0 && <span className="text-[11px] text-subtle">· {event.change_count}×Δ</span>}
          </span>
          <span className="mt-0.5 block text-sm text-fg">{event.summary}</span>
        </span>
        <span className="flex-none text-[11.5px] text-subtle">{fmtDateTime(event.ts)}</span>
      </button>
      {expanded && (
        <div className="border-t border-border px-3 py-3 pl-9">
          {detail.isLoading ? (
            <Spinner className="h-4 w-4" />
          ) : (
            <>
              {(detail.data?.changes.length ?? 0) > 0 ? (
                <div className="space-y-1.5">
                  {detail.data!.changes.map((c, i) => (
                    <DiffLine key={i} change={c} />
                  ))}
                </div>
              ) : (
                <p className="text-[13px] text-subtle">Bez změn polí.</p>
              )}
              {event.entity_type && event.entity_id && (
                <button
                  type="button"
                  onClick={() => onOpenTimeline(event.entity_type!, event.entity_id!)}
                  className="mt-2 inline-flex items-center gap-1 text-[12px] font-semibold text-accent hover:underline"
                >
                  <History size={13} aria-hidden /> Historie této entity
                </button>
              )}
            </>
          )}
        </div>
      )}
    </li>
  )
}

function DiffLine({ change }: { change: AuditChange }) {
  return (
    <div className="text-[13px]">
      <span className="font-mono text-[11.5px] text-subtle">{change.field}</span>
      <div className="mt-0.5 space-y-0.5">
        {change.old_value != null && (
          <div className="rounded bg-diff-del-bg px-2 py-1 text-diff-del-fg">
            <span className="opacity-70">− </span>
            <Truncated text={change.old_value} strike />
          </div>
        )}
        {change.new_value != null && (
          <div className="rounded bg-diff-add-bg px-2 py-1 text-diff-add-fg">
            <span className="opacity-70">+ </span>
            <Truncated text={change.new_value} />
          </div>
        )}
      </div>
    </div>
  )
}

function Truncated({ text, strike }: { text: string; strike?: boolean }) {
  const [open, setOpen] = useState(false)
  const long = text.length > 200
  const shown = open || !long ? text : text.slice(0, 200) + '…'
  return (
    <span>
      <span className={cn('whitespace-pre-wrap break-words', strike && 'line-through')}>{shown}</span>
      {long && (
        <button type="button" onClick={() => setOpen((v) => !v)} className="ml-1 text-[11px] font-semibold underline opacity-80">
          {open ? 'méně' : 'více'}
        </button>
      )}
    </span>
  )
}

function EntityTimeline({ type, id, open, onOpenChange }: { type: string; id: string; open: boolean; onOpenChange: (o: boolean) => void }) {
  const q = useQuery({ queryKey: qk.logsEntity(type, id), queryFn: () => api.getEntityTimeline(type, id), enabled: open })
  return (
    <ResponsiveModal open={open} onOpenChange={onOpenChange} title={`Historie · ${type}`}>
      {q.isLoading ? (
        <div className="grid place-items-center py-8">
          <Spinner />
        </div>
      ) : (q.data?.items.length ?? 0) === 0 ? (
        <p className="text-sm text-subtle">Žádná historie.</p>
      ) : (
        <ol className="space-y-3 border-l border-border pl-4">
          {q.data!.items.map((e) => (
            <li key={e.id} className="relative">
              <span className="absolute -left-[21px] top-1.5 h-2 w-2 rounded-full bg-accent" aria-hidden />
              <div className="flex flex-wrap items-center gap-2 text-[11.5px] text-subtle">
                <span className="font-mono text-accent">{e.module}.{e.action}</span>
                <span>{fmtDateTime(e.ts)}</span>
              </div>
              <div className="text-sm text-fg">{e.summary}</div>
              {e.changes.length > 0 && (
                <div className="mt-1 space-y-1">
                  {e.changes.map((c, i) => (
                    <DiffLine key={i} change={c} />
                  ))}
                </div>
              )}
            </li>
          ))}
        </ol>
      )}
    </ResponsiveModal>
  )
}

function StatsView() {
  const [dimension, setDimension] = useState('module')
  const stats = useQuery({ queryKey: qk.logsStats({ dimension }), queryFn: () => api.getLogStats(dimension, 'day') })
  const totals = stats.data?.totals ?? []
  const max = Math.max(1, ...totals.map((t) => t.count))

  return (
    <div>
      <div className="mb-4 flex items-center gap-2">
        <label className="text-sm text-muted" htmlFor="dim">
          Rozměr
        </label>
        <select id="dim" className={selectCls} value={dimension} onChange={(e) => setDimension(e.target.value)}>
          <option value="module">Modul</option>
          <option value="actor">Aktér</option>
          <option value="action">Akce</option>
          <option value="level">Úroveň</option>
        </select>
      </div>
      {stats.isLoading ? (
        <div className="grid place-items-center py-16">
          <Spinner />
        </div>
      ) : totals.length === 0 ? (
        <p className="text-sm text-muted">Žádná data.</p>
      ) : (
        <div className="space-y-2">
          {totals.map((t, i) => (
            <div key={t.key} className="flex items-center gap-3">
              <span className="w-40 flex-none truncate text-[13px] text-muted">{t.key}</span>
              <div className="h-5 flex-1 rounded bg-s2">
                <div
                  className="h-5 rounded"
                  style={{ width: `${(t.count / max) * 100}%`, backgroundColor: `var(--c${(i % 5) + 1})` }}
                />
              </div>
              <span className="w-10 flex-none text-right text-[13px] font-semibold text-fg">{t.count}</span>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
