import { useEffect, useState } from 'react'
import { ResponsiveModal } from '@/components/ui/modal'
import { Button, Input } from '@/components/ui/ui'

// BoardDialog — create or rename a board (v1). A single name field.
export function BoardDialog({
  open,
  onOpenChange,
  mode,
  initialName = '',
  onSubmit,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  mode: 'create' | 'rename'
  initialName?: string
  onSubmit: (name: string) => void
}) {
  const [name, setName] = useState(initialName)
  useEffect(() => setName(initialName), [initialName, open])

  const submit = () => {
    const t = name.trim()
    if (!t) return
    onSubmit(t)
    onOpenChange(false)
  }

  return (
    <ResponsiveModal
      open={open}
      onOpenChange={onOpenChange}
      title={mode === 'create' ? 'Nová tabule' : 'Přejmenovat tabuli'}
      footer={
        <>
          <Button variant="ghost" size="sm" onClick={() => onOpenChange(false)}>
            Zrušit
          </Button>
          <Button variant="primary" size="sm" onClick={submit} disabled={!name.trim()}>
            {mode === 'create' ? 'Vytvořit' : 'Uložit'}
          </Button>
        </>
      }
    >
      <form
        onSubmit={(e) => {
          e.preventDefault()
          submit()
        }}
      >
        <div className="mb-2 font-mono text-[10px] uppercase tracking-wide text-subtle">Název</div>
        <Input autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder="Např. „Zahrada“ nebo „Rekonstrukce“" aria-label="Název tabule" />
      </form>
    </ResponsiveModal>
  )
}
