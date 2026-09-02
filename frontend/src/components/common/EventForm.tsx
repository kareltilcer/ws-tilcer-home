import { useEffect, useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { qk } from '@/api/keys'
import * as api from '@/modules/events/api/endpoints'
import type { EventInput } from '@/modules/events/api/endpoints'
import type { ReminderLead } from '@/api/types'
import { ResponsiveModal } from '@/components/ui/modal'
import { Button, Input, Textarea } from '@/components/ui/ui'
import { cn } from '@/lib/utils'
import { LinksEditor, type EditableLink } from './LinksEditor'
import { LEAD_OPTIONS } from './reminderLead'

type Recurrence = 'none' | 'weekly' | 'monthly' | 'yearly'

const RECURRENCE_OPTIONS: { value: Recurrence; label: string }[] = [
  { value: 'none', label: 'nikdy' },
  { value: 'weekly', label: 'týdně' },
  { value: 'monthly', label: 'měsíčně' },
  { value: 'yearly', label: 'ročně' },
]

const SERIES_WARNING =
  'Změny se uloží pro celou sérii — všechny výskyty. Jednotlivý výskyt nelze upravit ani přeskočit.'

function rruleToRecurrence(rrule: string | null): Recurrence {
  if (!rrule) return 'none'
  if (rrule.includes('WEEKLY')) return 'weekly'
  if (rrule.includes('MONTHLY')) return 'monthly'
  if (rrule.includes('YEARLY')) return 'yearly'
  return 'none'
}

function rruleUntil(rrule: string | null): string {
  const m = rrule?.match(/UNTIL=(\d{4})(\d{2})(\d{2})/)
  return m ? `${m[1]}-${m[2]}-${m[3]}` : ''
}

function buildRRule(rec: Recurrence, until: string): string {
  if (rec === 'none') return ''
  const freq = rec === 'weekly' ? 'WEEKLY' : rec === 'monthly' ? 'MONTHLY' : 'YEARLY'
  let s = `FREQ=${freq};INTERVAL=1`
  if (until) s += `;UNTIL=${until.replaceAll('-', '')}`
  return s
}

// EventForm creates or edits an event (whole series). All-day date (no time
// field), recurrence with an optional end date, and a reminder lead. Both of the
// conditional blocks are ALWAYS LAID OUT and merely hidden, so neither reveal
// jumps the layout and neither has a reserved height that could be guessed wrong.
// Editing a recurring event requires confirming the series-edit warning before
// saving.
export function EventForm({
  eventId,
  open,
  onOpenChange,
}: {
  eventId?: string
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const qc = useQueryClient()
  const editing = !!eventId
  const existing = useQuery({ queryKey: qk.event(eventId ?? ''), queryFn: () => api.getEvent(eventId!), enabled: open && editing })

  const [title, setTitle] = useState('')
  const [description, setDescription] = useState('')
  const [startsOn, setStartsOn] = useState('')
  const [recurrence, setRecurrence] = useState<Recurrence>('none')
  const [until, setUntil] = useState('')
  const [reminderEnabled, setReminderEnabled] = useState(false)
  const [reminderLead, setReminderLead] = useState<ReminderLead>('1w')
  const [links, setLinks] = useState<EditableLink[]>([])
  const [error, setError] = useState('')
  const [confirming, setConfirming] = useState(false)

  // Prefill from the existing event (or reset for create) when opened.
  useEffect(() => {
    if (!open) return
    const e = editing ? existing.data : undefined
    setTitle(e?.title ?? '')
    setDescription(e?.description ?? '')
    setStartsOn(e?.starts_on ?? '')
    setRecurrence(rruleToRecurrence(e?.rrule ?? null))
    setUntil(rruleUntil(e?.rrule ?? null))
    setReminderEnabled(e?.reminder_enabled ?? false)
    setReminderLead((e?.reminder_lead as ReminderLead) ?? '1w')
    setLinks(e?.links.map((l) => ({ id: l.id, url: l.url, title: l.title })) ?? [])
    setError('')
    setConfirming(false)
  }, [open, editing, existing.data])

  const wasRecurring = useMemo(() => !!existing.data?.rrule, [existing.data])

  const save = useMutation({
    mutationFn: async () => {
      const body: EventInput = {
        title: title.trim(),
        description: description.trim() || undefined,
        starts_on: startsOn,
        rrule: buildRRule(recurrence, until) || undefined,
        reminder_enabled: reminderEnabled,
        reminder_lead: reminderEnabled ? reminderLead : undefined,
      }
      const saved = editing ? await api.updateEvent(eventId!, body) : await api.createEvent(body)
      await reconcileLinks(saved.id, existing.data?.links ?? [], links)
      return saved
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['events'] })
      if (eventId) void qc.invalidateQueries({ queryKey: qk.event(eventId) })
      void qc.invalidateQueries({ queryKey: qk.dashboard })
      onOpenChange(false)
    },
    onError: (e: unknown) => setError(e instanceof Error ? e.message : 'Uložení se nezdařilo'),
  })

  function attemptSave() {
    setError('')
    if (!title.trim()) return setError('Zadejte název.')
    if (!startsOn) return setError('Zadejte datum.')
    if (reminderEnabled && !reminderLead) return setError('Vyberte, kdy připomenout.')
    // Editing a recurring event affects the whole series — confirm first.
    if (editing && wasRecurring && !confirming) {
      setConfirming(true)
      return
    }
    save.mutate()
  }

  return (
    <ResponsiveModal
      open={open}
      onOpenChange={onOpenChange}
      title={editing ? 'Upravit událost' : 'Nová událost'}
      footer={
        <>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            Zrušit
          </Button>
          <Button variant="primary" loading={save.isPending} onClick={attemptSave}>
            {editing ? 'Uložit' : 'Vytvořit'}
          </Button>
        </>
      }
    >
      <div className="space-y-5">
        <Field label="Název">
          <Input value={title} onChange={(e) => setTitle(e.target.value)} placeholder="Např. Zaplatit plyn" autoFocus />
        </Field>

        <Field label="Popis">
          <Textarea value={description} onChange={(e) => setDescription(e.target.value)} placeholder="Markdown…" className="min-h-[90px]" />
        </Field>

        <Field label="Datum (celodenní)">
          <Input type="date" value={startsOn} onChange={(e) => setStartsOn(e.target.value)} className="max-w-[220px]" />
        </Field>

        <Field label="Opakování">
          <div className="flex flex-wrap gap-1.5">
            {RECURRENCE_OPTIONS.map((o) => (
              <Chip key={o.value} active={recurrence === o.value} onClick={() => setRecurrence(o.value)}>
                {o.label}
              </Chip>
            ))}
          </div>
          {/* Laid out always, hidden while the event does not repeat — the same
              trade as the lead selector below, for the same reason: a min-height
              reserve is a guess at the height of what it hides, and the one it
              replaces (64px) was already about 2px away from the Field it stood in
              for. visibility:hidden keeps the date input out of the tab order and
              the accessibility tree while it means nothing. */}
          <div className={cn('mt-2', recurrence === 'none' && 'invisible')} aria-hidden={recurrence === 'none' || undefined}>
            <Field label="Konec opakování (nepovinné)">
              <Input type="date" value={until} onChange={(e) => setUntil(e.target.value)} className="max-w-[220px]" />
            </Field>
          </div>
        </Field>

        <Field label="Připomínka">
          <label className="flex items-center gap-2 text-sm">
            <input type="checkbox" checked={reminderEnabled} onChange={(e) => setReminderEnabled(e.target.checked)} className="h-4 w-4" />
            Připomenout
          </label>
          {/* The lead selector is always laid out and merely hidden while the box is
              unticked, so the space it reserves IS its own — it cannot be reserved
              wrongly, and it costs nothing when the labels get longer or a lead is
              added. A min-height was the reserve before, and its 44px held ONE row of
              chips while the phone had always drawn two, so ticking the box pushed
              everything below it down — the jump the reserve exists to prevent.
              visibility:hidden also keeps the hidden chips out of the tab order and
              the accessibility tree, which display:none-by-unmounting did for free
              and opacity would not. */}
          <div className={cn('mt-2 flex flex-wrap gap-1.5', !reminderEnabled && 'invisible')} aria-hidden={!reminderEnabled || undefined}>
            {LEAD_OPTIONS.map((o) => (
              <Chip key={o.value} active={reminderLead === o.value} onClick={() => setReminderLead(o.value)}>
                {o.label}
              </Chip>
            ))}
          </div>
        </Field>

        <Field label="Odkazy">
          <LinksEditor
            links={links}
            onAdd={(url, t) => setLinks((ls) => [...ls, { id: `new-${crypto.randomUUID()}`, url, title: t || null }])}
            onRemove={(id) => setLinks((ls) => ls.filter((l) => l.id !== id))}
          />
        </Field>

        {editing && wasRecurring && (
          <p className="rounded-md border border-warn/40 bg-warn/10 px-3 py-2 text-[13px] text-fg">{SERIES_WARNING}</p>
        )}
        {error && <p className="text-[13px] text-danger">{error}</p>}
      </div>

      {/* Pre-save confirmation for a recurring series edit. */}
      <ResponsiveModal open={confirming} onOpenChange={setConfirming} title="Upravit celou sérii?"
        footer={
          <>
            <Button variant="ghost" onClick={() => setConfirming(false)}>Zpět</Button>
            <Button variant="primary" onClick={() => { setConfirming(false); save.mutate() }}>Uložit sérii</Button>
          </>
        }
      >
        <p className="text-sm text-fg">{SERIES_WARNING}</p>
      </ResponsiveModal>
    </ResponsiveModal>
  )
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <div className="mb-1.5 text-[11px] font-semibold uppercase tracking-wide text-subtle">{label}</div>
      {children}
    </div>
  )
}

// Chip is one option in a single-select row. aria-pressed is what carries the
// selection: the active state is otherwise a colour, which a screen reader has no
// way to read, and the lead a member has chosen would be knowable only by saving
// and reopening the event.
function Chip({ active, onClick, children }: { active: boolean; onClick: () => void; children: React.ReactNode }) {
  return (
    <button
      type="button"
      aria-pressed={active}
      onClick={onClick}
      className={
        'rounded-md border px-3 py-1.5 text-sm font-semibold ' +
        (active ? 'border-accent bg-accent-soft text-fg' : 'border-border bg-s2 text-muted hover:text-fg')
      }
    >
      {children}
    </button>
  )
}

// reconcileLinks adds newly-added links and deletes removed ones.
async function reconcileLinks(
  eventId: string,
  before: { id: string; url: string; title: string | null }[],
  after: EditableLink[],
) {
  const afterIds = new Set(after.filter((l) => !l.id.startsWith('new-')).map((l) => l.id))
  for (const b of before) {
    if (!afterIds.has(b.id)) await api.deleteEventLink(b.id)
  }
  for (const a of after) {
    if (a.id.startsWith('new-')) await api.addEventLink(eventId, { url: a.url, title: a.title ?? undefined })
  }
}
