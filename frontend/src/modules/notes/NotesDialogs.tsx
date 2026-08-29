import { useState } from 'react'
import { ResponsiveModal } from '@/components/ui/modal'
import { Button, Input } from '@/components/ui/ui'
import { DEFAULT_FOLDER_ICON, FolderIconPicker, iconToStore } from '@/components/common/FolderIconPicker'
import { cs } from '@/i18n/cs'

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
  onSubmit: (name: string, icon: string) => void
  onClose: () => void
}) {
  const [name, setName] = useState('')
  const [icon, setIcon] = useState(DEFAULT_FOLDER_ICON)
  // Guard on `pending`: the footer Button disables itself while loading, but the
  // Input's Enter handler bypasses that, so a held/double-tapped Enter would fire
  // onSubmit twice and create duplicate notes/folders.
  const submit = () => {
    if (pending) return
    if (name.trim()) onSubmit(name.trim(), iconToStore(icon))
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
      {kind === 'folder' && (
        <div className="mt-3">
          <FolderIconPicker value={icon} onChange={setIcon} />
        </div>
      )}
    </ResponsiveModal>
  )
}

// RenameDialog — rename a folder, prefilled with its current name. Mirrors the
// create/move/delete dialog + useMutation pattern (pending state, in-app modal)
// instead of the native window.prompt the rename path used to reach for.
export function RenameDialog({
  currentName,
  currentIcon,
  pending,
  onSubmit,
  onClose,
}: {
  currentName: string
  currentIcon: string
  pending: boolean
  onSubmit: (name: string, icon: string) => void
  onClose: () => void
}) {
  const [name, setName] = useState(currentName)
  const [icon, setIcon] = useState(currentIcon || DEFAULT_FOLDER_ICON)
  const submit = () => {
    if (pending) return
    const trimmed = name.trim()
    if (!trimmed) return
    // Unchanged (both name AND icon) → nothing to save.
    if (trimmed === currentName && icon === (currentIcon || DEFAULT_FOLDER_ICON)) return onClose()
    onSubmit(trimmed, iconToStore(icon))
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
      <div className="mt-3">
        <FolderIconPicker value={icon} onChange={setIcon} />
      </div>
    </ResponsiveModal>
  )
}
