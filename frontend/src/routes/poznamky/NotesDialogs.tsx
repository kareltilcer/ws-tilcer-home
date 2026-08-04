import { useState } from 'react'
import { AlertTriangle } from 'lucide-react'
import { ResponsiveModal } from '@/components/ui/modal'
import { Button, Input } from '@/components/ui/ui'
import { cs } from '@/i18n/cs'
import { count, PLURAL } from '@/i18n/plural'
import { cn } from '@/lib/utils'

export type MoveTarget = { id: string | null; name: string; depth: number }

// CreateDialog — new note or folder in the current location.
export function CreateDialog({
  kind,
  location,
  pending,
  onSubmit,
  onClose,
}: {
  kind: 'note' | 'folder'
  location: string
  pending: boolean
  onSubmit: (name: string) => void
  onClose: () => void
}) {
  const [name, setName] = useState('')
  // Guard on `pending`: the footer Button disables itself while loading, but the
  // Input's Enter handler bypasses that, so a held/double-tapped Enter would fire
  // onSubmit twice and create duplicate notes/folders.
  const submit = () => {
    if (pending) return
    if (name.trim()) onSubmit(name.trim())
  }
  return (
    <ResponsiveModal
      open
      onOpenChange={(o) => !o && onClose()}
      title={kind === 'folder' ? cs.notes.createFolderHeading : cs.notes.createNoteHeading}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {cs.notes.cancel}
          </Button>
          <Button variant="primary" loading={pending} onClick={submit}>
            {cs.notes.submitCreate}
          </Button>
        </>
      }
    >
      <p className="mb-2 font-mono text-[11px] text-subtle">
        {cs.notes.locationLabel} {location}
      </p>
      <Input
        autoFocus
        value={name}
        onChange={(e) => setName(e.target.value)}
        onKeyDown={(e) => e.key === 'Enter' && submit()}
        placeholder={kind === 'folder' ? cs.notes.folderNamePlaceholder : cs.notes.noteNamePlaceholder}
      />
    </ResponsiveModal>
  )
}

// RenameDialog — rename a folder, prefilled with its current name. Mirrors the
// create/move/delete dialog + useMutation pattern (pending state, in-app modal)
// instead of the native window.prompt the rename path used to reach for.
export function RenameDialog({
  currentName,
  pending,
  onSubmit,
  onClose,
}: {
  currentName: string
  pending: boolean
  onSubmit: (name: string) => void
  onClose: () => void
}) {
  const [name, setName] = useState(currentName)
  const submit = () => {
    if (pending) return
    const trimmed = name.trim()
    if (!trimmed) return
    if (trimmed === currentName) return onClose() // unchanged → nothing to save
    onSubmit(trimmed)
  }
  return (
    <ResponsiveModal
      open
      onOpenChange={(o) => !o && onClose()}
      title={cs.notes.renameFolderHeading}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {cs.notes.cancel}
          </Button>
          <Button variant="primary" loading={pending} onClick={submit}>
            {cs.notes.submitRename}
          </Button>
        </>
      }
    >
      <Input
        autoFocus
        value={name}
        onChange={(e) => setName(e.target.value)}
        onKeyDown={(e) => e.key === 'Enter' && submit()}
        placeholder={cs.notes.folderNamePlaceholder}
      />
    </ResponsiveModal>
  )
}

// MoveDialog — a folder-target picker (reuses the "Přesunout do…" pattern).
export function MoveDialog({
  kind,
  title,
  targets,
  pending,
  onPick,
  onClose,
}: {
  kind: 'note' | 'folder'
  title: string
  targets: MoveTarget[]
  pending: boolean
  onPick: (targetId: string | null) => void
  onClose: () => void
}) {
  return (
    <ResponsiveModal open onOpenChange={(o) => !o && onClose()} title={kind === 'folder' ? cs.notes.moveFolderInto : cs.notes.moveNoteInto}>
      <p className="mb-2 truncate text-sm font-semibold text-fg">{title}</p>
      <div className="space-y-0.5">
        {targets.map((t) => (
          <button
            key={t.id ?? '__root__'}
            type="button"
            disabled={pending}
            onClick={() => onPick(t.id)}
            className="flex w-full items-center gap-2 rounded-md px-2.5 py-2.5 text-left text-sm text-fg hover:bg-accent-soft disabled:opacity-50"
            style={{ paddingLeft: 10 + t.depth * 14 }}
          >
            <span className="text-subtle">▸</span>
            <span className="min-w-0 flex-1 truncate font-semibold">{t.name}</span>
          </button>
        ))}
      </div>
    </ResponsiveModal>
  )
}

// DeleteDialog — confirm; a non-empty folder shows the cascade warning before the
// delete (like the events series-edit warning), in plain Czech.
export function DeleteDialog({
  kind,
  title,
  subfolders,
  notes,
  pending,
  onConfirm,
  onClose,
}: {
  kind: 'note' | 'folder'
  title: string
  subfolders: number
  notes: number
  pending: boolean
  onConfirm: (cascade: boolean) => void
  onClose: () => void
}) {
  const nonEmpty = kind === 'folder' && (subfolders > 0 || notes > 0)
  const cascadeParts: string[] = []
  if (subfolders > 0) cascadeParts.push(count(subfolders, PLURAL.folders))
  if (notes > 0) cascadeParts.push(count(notes, PLURAL.notes))

  return (
    <ResponsiveModal
      open
      onOpenChange={(o) => !o && onClose()}
      title={kind === 'folder' ? cs.notes.deleteFolderTitle(title) : cs.notes.deleteNoteTitle(title)}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {cs.notes.cancel}
          </Button>
          <Button variant="danger" loading={pending} onClick={() => onConfirm(nonEmpty)}>
            {cs.notes.confirmDelete}
          </Button>
        </>
      }
    >
      <div className={cn('flex gap-3', nonEmpty && 'rounded-xl border border-warn/40 bg-warn/10 p-3')}>
        {nonEmpty && <AlertTriangle size={18} className="mt-0.5 flex-none text-warn" />}
        <p className="text-sm text-muted text-pretty">
          {kind === 'note' && cs.notes.deleteNoteBody}
          {kind === 'folder' && nonEmpty && (
            <>
              {cs.notes.deleteFolderCascade}
              {cascadeParts.length > 0 && <span className="mt-1 block font-semibold text-fg">{cascadeParts.join(' · ')}</span>}
            </>
          )}
          {kind === 'folder' && !nonEmpty && cs.notes.deleteFolderEmpty}
        </p>
      </div>
    </ResponsiveModal>
  )
}
