import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, Plus, Trash2, X } from 'lucide-react'
import { qk } from '@/api/keys'
import * as api from '@/api/endpoints'
import type { ChecklistItem } from '@/api/types'
import { ResponsiveModal } from '@/components/ui/modal'
import { Button, Input, Spinner, Textarea } from '@/components/ui/ui'
import { cn } from '@/lib/utils'
import { count, PLURAL } from '@/i18n/plural'
import { MarkdownView } from './MarkdownView'
import { LinksEditor } from './LinksEditor'

export function CardDetail({
  cardId,
  boardId,
  open,
  onOpenChange,
  readOnly = false,
}: {
  cardId: string
  boardId: string
  open: boolean
  onOpenChange: (open: boolean) => void
  readOnly?: boolean
}) {
  const qc = useQueryClient()
  const card = useQuery({ queryKey: qk.card(cardId), queryFn: () => api.getCard(cardId), enabled: open })
  const labels = useQuery({ queryKey: qk.boardLabels(boardId), queryFn: () => api.listLabels(boardId), enabled: open })

  const invalidate = () => {
    void qc.invalidateQueries({ queryKey: qk.card(cardId) })
    void qc.invalidateQueries({ queryKey: ['board'] })
    void qc.invalidateQueries({ queryKey: qk.dashboard })
  }
  const mutate = <A, R>(fn: (a: A) => Promise<R>) =>
    useMutation({ mutationFn: fn, onSuccess: invalidate })

  const mUpdate = mutate((body: Parameters<typeof api.updateCard>[1]) => api.updateCard(cardId, body))
  const mAddLink = mutate((b: { url: string; title?: string }) => api.addCardLink(cardId, b))
  const mDelLink = mutate((id: string) => api.deleteCardLink(id))
  const mAddChk = mutate((text: string) => api.addChecklistItem(cardId, { text }))
  const mSetChk = mutate((v: { id: string; done: boolean }) => api.updateChecklistItem(v.id, { done: v.done }))
  const mDelChk = mutate((id: string) => api.deleteChecklistItem(id))
  const mAttach = mutate((labelId: string) => api.attachLabel(cardId, labelId))
  const mDetach = mutate((labelId: string) => api.detachLabel(cardId, labelId))

  const c = card.data
  const title = c?.title ?? '…'

  return (
    <ResponsiveModal open={open} onOpenChange={onOpenChange} title={title}>
      {card.isLoading || !c ? (
        <div className="grid place-items-center py-10">
          <Spinner />
        </div>
      ) : (
        <div className="space-y-6">
          {!readOnly && <TitleEditor value={c.title} onSave={(v) => mUpdate.mutate({ title: v })} />}

          <Section label="Poznámky">
            <NotesEditor notes={c.notes ?? ''} readOnly={readOnly} onSave={(v) => mUpdate.mutate({ notes: v })} />
          </Section>

          <Section label={`Checklist${c.checklist.length ? ` · ${count(c.checklist.filter((i) => i.done).length, PLURAL.tasks)}` : ''}`}>
            <Checklist
              items={c.checklist}
              readOnly={readOnly}
              onToggle={(id, done) => mSetChk.mutate({ id, done })}
              onAdd={(text) => mAddChk.mutate(text)}
              onDelete={(id) => mDelChk.mutate(id)}
            />
          </Section>

          <Section label="Štítky">
            <LabelPicker
              attached={c.labels}
              all={labels.data ?? []}
              readOnly={readOnly}
              onAttach={(id) => mAttach.mutate(id)}
              onDetach={(id) => mDetach.mutate(id)}
            />
          </Section>

          <Section label="Odkazy">
            <LinksEditor
              links={c.links}
              readOnly={readOnly}
              onAdd={(url, t) => mAddLink.mutate({ url, title: t || undefined })}
              onRemove={(id) => mDelLink.mutate(id)}
            />
          </Section>
        </div>
      )}
    </ResponsiveModal>
  )
}

function Section({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <section>
      <h3 className="mb-2 text-[11px] font-semibold uppercase tracking-wide text-subtle">{label}</h3>
      {children}
    </section>
  )
}

function TitleEditor({ value, onSave }: { value: string; onSave: (v: string) => void }) {
  const [v, setV] = useState(value)
  useEffect(() => setV(value), [value])
  return (
    <Input
      value={v}
      onChange={(e) => setV(e.target.value)}
      onBlur={() => {
        const t = v.trim()
        if (t && t !== value) onSave(t)
      }}
      className="h-11 text-base font-semibold"
      aria-label="Název karty"
    />
  )
}

function NotesEditor({ notes, readOnly, onSave }: { notes: string; readOnly: boolean; onSave: (v: string) => void }) {
  const [editing, setEditing] = useState(false)
  const [v, setV] = useState(notes)
  useEffect(() => setV(notes), [notes])

  if (readOnly || !editing) {
    return (
      <div className="space-y-2">
        <MarkdownView>{notes}</MarkdownView>
        {!readOnly && (
          <Button size="sm" variant="ghost" onClick={() => setEditing(true)}>
            Upravit poznámky
          </Button>
        )}
      </div>
    )
  }
  return (
    <div className="space-y-2">
      <Textarea value={v} onChange={(e) => setV(e.target.value)} placeholder="Markdown…" autoFocus />
      <div className="flex gap-2">
        <Button size="sm" variant="primary" onClick={() => { onSave(v); setEditing(false) }}>
          Uložit
        </Button>
        <Button size="sm" variant="ghost" onClick={() => { setV(notes); setEditing(false) }}>
          Zrušit
        </Button>
      </div>
    </div>
  )
}

function Checklist({
  items,
  readOnly,
  onToggle,
  onAdd,
  onDelete,
}: {
  items: ChecklistItem[]
  readOnly: boolean
  onToggle: (id: string, done: boolean) => void
  onAdd: (text: string) => void
  onDelete: (id: string) => void
}) {
  const [text, setText] = useState('')
  return (
    <div className="space-y-1.5">
      {items.map((it) => (
        <div key={it.id} className="flex items-center gap-2">
          <button
            type="button"
            disabled={readOnly}
            onClick={() => onToggle(it.id, !it.done)}
            aria-label={it.done ? 'Odškrtnout' : 'Odškrtnout hotovo'}
            className={cn(
              'grid h-5 w-5 flex-none place-items-center rounded border',
              it.done ? 'border-good bg-good/20 text-good' : 'border-border-strong text-transparent',
            )}
          >
            <Check size={13} aria-hidden />
          </button>
          <span className={cn('flex-1 text-sm', it.done && 'text-subtle line-through')}>{it.text}</span>
          {!readOnly && (
            <button type="button" onClick={() => onDelete(it.id)} className="text-subtle hover:text-danger" aria-label="Smazat bod">
              <Trash2 size={14} aria-hidden />
            </button>
          )}
        </div>
      ))}
      {!readOnly && (
        <form
          className="flex gap-1.5 pt-1"
          onSubmit={(e) => {
            e.preventDefault()
            const t = text.trim()
            if (t) {
              onAdd(t)
              setText('')
            }
          }}
        >
          <Input value={text} onChange={(e) => setText(e.target.value)} placeholder="Přidat bod…" className="h-9" />
          <Button size="sm" type="submit" variant="secondary" className="flex-none">
            <Plus size={14} aria-hidden />
          </Button>
        </form>
      )}
    </div>
  )
}

function LabelPicker({
  attached,
  all,
  readOnly,
  onAttach,
  onDetach,
}: {
  attached: { id: string; name: string; color: string }[]
  all: { id: string; name: string; color: string }[]
  readOnly: boolean
  onAttach: (id: string) => void
  onDetach: (id: string) => void
}) {
  const attachedIds = new Set(attached.map((l) => l.id))
  const available = all.filter((l) => !attachedIds.has(l.id))
  return (
    <div className="space-y-2">
      <div className="flex flex-wrap gap-1.5">
        {attached.map((l) => (
          <span
            key={l.id}
            className="inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[12px] font-semibold text-fg"
            style={{ backgroundColor: `color-mix(in oklch, ${l.color} 22%, var(--s2))` }}
          >
            <span className="h-2 w-2 rounded-full" style={{ backgroundColor: l.color }} />
            {l.name}
            {!readOnly && (
              <button type="button" onClick={() => onDetach(l.id)} aria-label={`Odebrat štítek ${l.name}`}>
                <X size={12} aria-hidden />
              </button>
            )}
          </span>
        ))}
        {attached.length === 0 && <span className="text-sm text-subtle">Žádné štítky.</span>}
      </div>
      {!readOnly && available.length > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {available.map((l) => (
            <button
              key={l.id}
              type="button"
              onClick={() => onAttach(l.id)}
              className="inline-flex items-center gap-1 rounded-full border border-dashed border-border px-2 py-0.5 text-[12px] text-muted hover:text-fg"
            >
              <span className="h-2 w-2 rounded-full" style={{ backgroundColor: l.color }} />
              {l.name}
            </button>
          ))}
        </div>
      )}
    </div>
  )
}
