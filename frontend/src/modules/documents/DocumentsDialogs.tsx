import { useState } from 'react'
import { ResponsiveModal } from '@/components/ui/modal'
import { Button, Input, Textarea } from '@/components/ui/ui'
import { DEFAULT_FOLDER_ICON, FolderIconPicker, iconToStore } from '@/components/common/FolderIconPicker'
import { cs } from '@/i18n/cs'

// CreateFolderDialog — a new folder in the current location.
export function CreateFolderDialog({
  location,
  pending,
  onSubmit,
  onClose,
}: {
  location: string
  pending: boolean
  onSubmit: (name: string, icon: string) => void
  onClose: () => void
}) {
  const [name, setName] = useState('')
  const [icon, setIcon] = useState(DEFAULT_FOLDER_ICON)
  // Guard on `pending`: the footer Button disables itself while loading, but the
  // Input's Enter handler bypasses that, so a held Enter would create duplicates.
  const submit = () => {
    if (pending) return
    if (name.trim()) onSubmit(name.trim(), iconToStore(icon))
  }
  return (
    <ResponsiveModal
      open
      onOpenChange={(o) => !o && onClose()}
      title={cs.documents.createFolderHeading}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {cs.documents.cancel}
          </Button>
          <Button variant="primary" loading={pending} onClick={submit}>
            {cs.documents.submitCreate}
          </Button>
        </>
      }
    >
      <p className="mb-2 font-mono text-[11px] text-subtle">
        {cs.documents.locationLabel} {location}
      </p>
      <Input
        autoFocus
        value={name}
        onChange={(e) => setName(e.target.value)}
        onKeyDown={(e) => e.key === 'Enter' && submit()}
        placeholder={cs.documents.folderNamePlaceholder}
      />
      <div className="mt-3">
        <FolderIconPicker value={icon} onChange={setIcon} />
      </div>
    </ResponsiveModal>
  )
}

// RenameFolderDialog — rename a folder, prefilled.
export function RenameFolderDialog({
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
      title={cs.documents.renameFolderHeading}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {cs.documents.cancel}
          </Button>
          <Button variant="primary" loading={pending} onClick={submit}>
            {cs.documents.submitRename}
          </Button>
        </>
      }
    >
      <Input
        autoFocus
        value={name}
        onChange={(e) => setName(e.target.value)}
        onKeyDown={(e) => e.key === 'Enter' && submit()}
        placeholder={cs.documents.folderNamePlaceholder}
      />
      <div className="mt-3">
        <FolderIconPicker value={icon} onChange={setIcon} />
      </div>
    </ResponsiveModal>
  )
}

// EditDocumentDialog — title + description. There is deliberately no field for the
// file itself: the bytes are immutable, so a changed file is a new upload (D41).
export function EditDocumentDialog({
  currentTitle,
  currentDescription,
  pending,
  onSubmit,
  onClose,
}: {
  currentTitle: string
  currentDescription: string
  pending: boolean
  onSubmit: (values: { title: string; description: string }) => void
  onClose: () => void
}) {
  const [title, setTitle] = useState(currentTitle)
  const [description, setDescription] = useState(currentDescription)
  const submit = () => {
    if (pending) return
    const t = title.trim()
    if (!t) return
    if (t === currentTitle && description.trim() === currentDescription) return onClose()
    onSubmit({ title: t, description: description.trim() })
  }
  return (
    <ResponsiveModal
      open
      onOpenChange={(o) => !o && onClose()}
      title={cs.documents.renameHeading}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {cs.documents.cancel}
          </Button>
          <Button variant="primary" loading={pending} onClick={submit}>
            {cs.documents.submitRename}
          </Button>
        </>
      }
    >
      <Input
        autoFocus
        value={title}
        onChange={(e) => setTitle(e.target.value)}
        onKeyDown={(e) => e.key === 'Enter' && submit()}
        placeholder={cs.documents.documentNamePlaceholder}
      />
      <label className="mt-3 block">
        <span className="mb-1 block text-[12px] font-semibold text-muted">{cs.documents.descriptionLabel}</span>
        <Textarea
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          placeholder={cs.documents.descriptionPlaceholder}
          className="min-h-[90px]"
        />
      </label>
      {/* Renaming changes the slug path but never the permanent /d/{id} link. */}
      <p className="mt-2 text-[11.5px] text-subtle text-pretty">{cs.documents.immutable}</p>
    </ResponsiveModal>
  )
}
