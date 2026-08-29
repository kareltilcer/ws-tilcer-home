import { useState } from 'react'
import { AlertTriangle } from 'lucide-react'
import { ResponsiveModal } from '@/components/ui/modal'
import { Button, Input, Textarea } from '@/components/ui/ui'
import { DEFAULT_FOLDER_ICON, FolderIconPicker, iconToStore } from '@/components/common/FolderIconPicker'
import { cs } from '@/i18n/cs'
import { count, PLURAL } from '@/i18n/plural'
import { cn } from '@/lib/utils'
import type { MoveTarget } from '@/lib/foldertree'

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

// MoveDialog — a folder-target picker (the same "Přesunout do…" pattern as Úkoly
// and Poznámky). Targets exclude the moved folder and its descendants, so the
// backend's cycle guard is never even reachable from the UI.
export function MoveDialog({
  kind,
  title,
  targets,
  pending,
  onPick,
  onClose,
}: {
  kind: 'document' | 'folder'
  title: string
  targets: MoveTarget[]
  pending: boolean
  onPick: (targetId: string | null) => void
  onClose: () => void
}) {
  return (
    <ResponsiveModal
      open
      onOpenChange={(o) => !o && onClose()}
      title={kind === 'folder' ? cs.documents.moveFolderInto : cs.documents.moveDocumentInto}
    >
      <p className="mb-2 truncate text-sm font-semibold text-fg">{title}</p>
      <div className="space-y-0.5">
        {targets.map((t) => (
          <button
            key={t.id ?? '__root__'}
            type="button"
            disabled={pending}
            onClick={() => onPick(t.id)}
            className="flex min-h-11 w-full items-center gap-2 rounded-md px-2.5 py-2.5 text-left text-sm text-fg hover:bg-accent-soft disabled:opacity-50"
            style={{ paddingLeft: 10 + t.depth * 14 }}
          >
            <span className="flex-none leading-none" aria-hidden>
              {t.icon ? t.icon : '▸'}
            </span>
            <span className="min-w-0 flex-1 truncate font-semibold">{t.name}</span>
          </button>
        ))}
      </div>
    </ResponsiveModal>
  )
}

// DeleteDialog — every delete confirms first (D50). Two things make this dialog
// different from the notes one: a non-empty folder shows the cascade warning, and
// an ADMIN can escalate to a hard delete that also removes the stored file. That
// escalation is opt-in, never the default.
export function DeleteDialog({
  kind,
  title,
  subfolders,
  documents,
  canHardDelete,
  pending,
  onConfirm,
  onClose,
}: {
  kind: 'document' | 'folder'
  title: string
  subfolders: number
  documents: number
  canHardDelete: boolean
  pending: boolean
  onConfirm: (opts: { cascade: boolean; hard: boolean }) => void
  onClose: () => void
}) {
  const [hard, setHard] = useState(false)
  const nonEmpty = kind === 'folder' && (subfolders > 0 || documents > 0)
  const cascadeParts: string[] = []
  if (subfolders > 0) cascadeParts.push(count(subfolders, PLURAL.folders))
  if (documents > 0) cascadeParts.push(count(documents, PLURAL.documents))

  return (
    <ResponsiveModal
      open
      onOpenChange={(o) => !o && onClose()}
      title={kind === 'folder' ? cs.documents.deleteFolderTitle(title) : cs.documents.deleteDocumentTitle(title)}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {cs.documents.cancel}
          </Button>
          <Button variant="danger" loading={pending} onClick={() => onConfirm({ cascade: nonEmpty, hard })}>
            {cs.documents.confirmDelete}
          </Button>
        </>
      }
    >
      <div className={cn('flex gap-3', nonEmpty && 'rounded-xl border border-warn/40 bg-warn/10 p-3')}>
        {nonEmpty && <AlertTriangle size={18} className="mt-0.5 flex-none text-warn" aria-hidden />}
        <p className="text-sm text-muted text-pretty">
          {kind === 'document' && cs.documents.deleteDocumentBody}
          {kind === 'folder' && nonEmpty && (
            <>
              {cs.documents.deleteFolderCascade}
              {cascadeParts.length > 0 && <span className="mt-1 block font-semibold text-fg">{cascadeParts.join(' · ')}</span>}
            </>
          )}
          {kind === 'folder' && !nonEmpty && cs.documents.deleteFolderEmpty}
        </p>
      </div>

      {canHardDelete && (
        <label className="mt-3 flex cursor-pointer items-start gap-2.5 rounded-xl border border-border bg-s2 p-3">
          <input
            type="checkbox"
            checked={hard}
            onChange={(e) => setHard(e.target.checked)}
            className="mt-0.5 h-4 w-4 accent-[var(--danger)]"
          />
          <span>
            <span className="block text-[13px] font-semibold text-fg">{cs.documents.hardDeleteLabel}</span>
            <span className="block text-[11.5px] text-subtle text-pretty">{cs.documents.hardDeleteHint}</span>
          </span>
        </label>
      )}
    </ResponsiveModal>
  )
}
