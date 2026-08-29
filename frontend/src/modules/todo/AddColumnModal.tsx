import { useState } from 'react'
import type { ColumnKind } from './api/types'
import { ResponsiveModal } from '@/components/ui/modal'
import { Button, Input } from '@/components/ui/ui'
import { KINDS, PRIORITIES } from './columnFields'
import { Segmented } from './Segmented'

// AddColumnModal — create a column on the active board (v1): name, kind, priority.
export function AddColumnModal({
  open,
  onOpenChange,
  onSubmit,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSubmit: (v: { name: string; kind: ColumnKind; priority: number }) => void
}) {
  const [name, setName] = useState('')
  const [kind, setKind] = useState<ColumnKind>('normal')
  const [priority, setPriority] = useState(0)

  const submit = () => {
    const t = name.trim()
    if (!t) return
    onSubmit({ name: t, kind, priority })
    setName('')
    setKind('normal')
    setPriority(0)
    onOpenChange(false)
  }

  return (
    <ResponsiveModal
      open={open}
      onOpenChange={onOpenChange}
      title="Přidat sloupec"
      footer={
        <>
          <Button variant="ghost" size="sm" onClick={() => onOpenChange(false)}>
            Zrušit
          </Button>
          <Button variant="primary" size="sm" onClick={submit} disabled={!name.trim()}>
            Přidat sloupec
          </Button>
        </>
      }
    >
      <div className="space-y-5">
        <div>
          <div className="mb-2 font-mono text-[10px] uppercase tracking-wide text-subtle">Název</div>
          <form
            onSubmit={(e) => {
              e.preventDefault()
              submit()
            }}
          >
            <Input autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder="Název sloupce — např. „Tento týden“" aria-label="Název sloupce" />
          </form>
        </div>
        <div>
          <div className="mb-2 font-mono text-[10px] uppercase tracking-wide text-subtle">Druh</div>
          <Segmented options={KINDS} value={kind} onPick={setKind} />
        </div>
        <div>
          <div className="mb-2 font-mono text-[10px] uppercase tracking-wide text-subtle">Priorita</div>
          <Segmented options={PRIORITIES} value={priority} onPick={setPriority} />
        </div>
      </div>
    </ResponsiveModal>
  )
}
