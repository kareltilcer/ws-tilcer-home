import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { Check, Pencil, Trash2 } from 'lucide-react'
import { qk } from '@/api/keys'
import * as api from '@/api/endpoints'
import type { Label } from '@/api/types'
import { ResponsiveModal } from '@/components/ui/modal'
import { Button, Input } from '@/components/ui/ui'
import { cn } from '@/lib/utils'
import { count, PLURAL } from '@/i18n/plural'

// Theme label palette (theme-aware, flips in light/dark). Stored as `var(--l-*)`
// so the chip colour follows the active theme. Matches design/v1 LB_PALETTE.
const PALETTE = ['--l-byt', '--l-fin', '--l-auto', '--l-zdravi', '--l-zahrada', '--l-urgent'].map((t) => `var(${t})`)

// LabelManager — board label CRUD (v1, FR-T6): create (name + swatch), recolor,
// rename, delete, with per-label usage counts. Owns its own mutations.
export function LabelManager({
  open,
  onOpenChange,
  boardId,
  labels,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  boardId: string
  labels: Label[]
}) {
  const qc = useQueryClient()
  const invalidate = () => {
    void qc.invalidateQueries({ queryKey: qk.boardLabels(boardId) })
    void qc.invalidateQueries({ queryKey: ['board', boardId] })
    void qc.invalidateQueries({ queryKey: qk.dashboard })
  }
  const onErr = (e: unknown) =>
    toast.error(e instanceof Error && e.message ? e.message : 'Štítek se nepodařilo uložit')

  const createMut = useMutation({
    mutationFn: (v: { name: string; color: string }) => api.createLabel(boardId, v),
    onSuccess: invalidate,
    onError: onErr,
  })
  const updateMut = useMutation({
    mutationFn: (v: { id: string; name: string; color: string }) => api.updateLabel(v.id, { name: v.name, color: v.color }),
    onSuccess: invalidate,
    onError: onErr,
  })
  const deleteMut = useMutation({
    mutationFn: (id: string) => api.deleteLabel(id),
    onSuccess: invalidate,
    onError: onErr,
  })

  const [draftName, setDraftName] = useState('')
  const [draftColor, setDraftColor] = useState(PALETTE[0])
  const [editId, setEditId] = useState<string | null>(null)
  const [editName, setEditName] = useState('')

  const addLabel = () => {
    const t = draftName.trim()
    if (!t) return
    // Reset only on success — on failure the typed name/colour stay so nothing is lost.
    createMut.mutate(
      { name: t, color: draftColor },
      {
        onSuccess: () => {
          setDraftName('')
          setDraftColor(PALETTE[0])
        },
      },
    )
  }

  return (
    <ResponsiveModal open={open} onOpenChange={onOpenChange} title="Štítky">
      <div className="space-y-4">
        <div className="rounded-lg border border-border bg-s2 p-3">
          <div className="mb-2 font-mono text-[10px] uppercase tracking-wide text-subtle">Nový štítek</div>
          <form
            className="flex items-center gap-2"
            onSubmit={(e) => {
              e.preventDefault()
              addLabel()
            }}
          >
            <span className="h-3.5 w-3.5 flex-none rounded-full" style={{ backgroundColor: draftColor }} />
            <Input value={draftName} onChange={(e) => setDraftName(e.target.value)} placeholder="Název štítku" aria-label="Název nového štítku" className="h-9" />
            <Button type="submit" size="sm" variant="primary" className="flex-none" disabled={!draftName.trim()}>
              Přidat
            </Button>
          </form>
          <div className="mt-2.5 flex gap-2">
            {PALETTE.map((c) => (
              <Swatch key={c} color={c} active={c === draftColor} onClick={() => setDraftColor(c)} />
            ))}
          </div>
        </div>

        <div className="divide-y divide-border">
          {labels.length === 0 && <p className="py-4 text-center text-sm text-subtle">Zatím žádné štítky.</p>}
          {labels.map((l) => {
            const editing = editId === l.id
            return (
              <div key={l.id} className="flex items-center gap-2.5 py-2.5">
                <span className="h-3 w-3 flex-none rounded-full" style={{ backgroundColor: l.color }} />
                {editing ? (
                  <form
                    className="flex flex-1 gap-1.5"
                    onSubmit={(e) => {
                      e.preventDefault()
                      const t = editName.trim()
                      if (t && t !== l.name) updateMut.mutate({ id: l.id, name: t, color: l.color })
                      setEditId(null)
                    }}
                  >
                    <Input autoFocus value={editName} onChange={(e) => setEditName(e.target.value)} aria-label="Přejmenovat štítek" className="h-8" />
                    <Button type="submit" size="sm" variant="primary" className="flex-none">
                      Uložit
                    </Button>
                  </form>
                ) : (
                  <>
                    <div className="min-w-0 flex-1">
                      <div className="truncate text-sm font-semibold text-fg">{l.name}</div>
                      <div className="font-mono text-[10.5px] text-subtle">{count(l.card_count ?? 0, PLURAL.cards)}</div>
                    </div>
                    <div className="flex gap-1.5">
                      {PALETTE.map((c) => (
                        <Swatch key={c} color={c} active={c === l.color} onClick={() => updateMut.mutate({ id: l.id, name: l.name, color: c })} small />
                      ))}
                    </div>
                    <button
                      type="button"
                      onClick={() => {
                        setEditId(l.id)
                        setEditName(l.name)
                      }}
                      aria-label={`Přejmenovat štítek ${l.name}`}
                      className="grid h-8 w-8 flex-none place-items-center rounded-md border border-border bg-s2 text-muted hover:text-fg"
                    >
                      <Pencil size={13} aria-hidden />
                    </button>
                    <button
                      type="button"
                      onClick={() => {
                        const used = l.card_count ?? 0
                        const msg =
                          used > 0
                            ? `Smazat štítek „${l.name}“? Odebere se z ${count(used, PLURAL.cards)}.`
                            : `Smazat štítek „${l.name}“?`
                        if (window.confirm(msg)) deleteMut.mutate(l.id)
                      }}
                      aria-label={`Smazat štítek ${l.name}`}
                      className="grid h-8 w-8 flex-none place-items-center rounded-md border border-border bg-s2 text-danger hover:bg-danger/10"
                    >
                      <Trash2 size={13} aria-hidden />
                    </button>
                  </>
                )}
              </div>
            )
          })}
        </div>
      </div>
    </ResponsiveModal>
  )
}

function Swatch({ color, active, onClick, small }: { color: string; active: boolean; onClick: () => void; small?: boolean }) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-label={active ? 'Vybraná barva' : 'Vybrat barvu'}
      className={cn('flex-none rounded-md', small ? 'h-6 w-6' : 'h-6 w-6 sm:h-7 sm:w-7')}
      style={{ backgroundColor: color, boxShadow: active ? '0 0 0 2px var(--text), 0 0 0 3px var(--border)' : '0 0 0 1px var(--border)' }}
    >
      {active && <Check size={12} className="mx-auto text-white mix-blend-difference" aria-hidden />}
    </button>
  )
}
