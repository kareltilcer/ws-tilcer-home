import { useState } from 'react'
import { AlertTriangle } from 'lucide-react'
import { ResponsiveModal } from '@/components/ui/modal'
import { Button } from '@/components/ui/ui'
import { count, PLURAL, type PluralForms } from '@/i18n/plural'
import { cn } from '@/lib/utils'
import type { MoveTarget } from '@/lib/foldertree'

/**
 * The move and delete confirmations Poznámky and Dokumenty both show.
 *
 * ⚠ THIS IS NOT THE notes⇄documents MERGE. The two modules stay two modules
 * (v4 D40); these two dialogs are shared because they were ~90% identical and
 * because the 10% had ALREADY DRIFTED — see MoveDialog below. Everything module-
 * specific stays module-specific: the Czech, the create and rename dialogs, and
 * what each page counts before it opens the delete confirmation.
 *
 * Each module passes a label bundle rather than a `cs` namespace, so the shared
 * component never reaches into i18n and cannot pick the wrong noun.
 */

export interface MoveDialogLabels {
  /** Modal heading when a folder is being moved. */
  folderTitle: string
  /** Modal heading when an item is being moved. */
  itemTitle: string
}

export function MoveDialog({
  isFolder,
  title,
  targets,
  labels,
  pending,
  onPick,
  onClose,
}: {
  isFolder: boolean
  title: string
  targets: MoveTarget[]
  labels: MoveDialogLabels
  pending: boolean
  onPick: (targetId: string | null) => void
  onClose: () => void
}) {
  return (
    <ResponsiveModal open onOpenChange={(o) => !o && onClose()} title={isFolder ? labels.folderTitle : labels.itemTitle}>
      <p className="mb-2 truncate text-sm font-semibold text-fg">{title}</p>
      <div className="space-y-0.5">
        {targets.map((t) => (
          <button
            key={t.id ?? '__root__'}
            type="button"
            disabled={pending}
            onClick={() => onPick(t.id)}
            // min-h-11 is the 44px touch target. It reached the Dokumenty copy and
            // never the Poznámky one, which is what a hand-maintained twin costs:
            // the rows are the same rows, and only one set was comfortable to tap.
            className="flex min-h-11 w-full items-center gap-2 rounded-md px-2.5 py-2.5 text-left text-sm text-fg hover:bg-accent-soft disabled:opacity-50"
            style={{ paddingLeft: 10 + t.depth * 14 }}
          >
            {/* aria-hidden: the icon repeats the name beside it, and the ▸ stands in
                for an icon that is not there. Neither is worth announcing. */}
            <span className="flex-none leading-none" aria-hidden>
              {t.icon ? t.icon : <span className="text-subtle">▸</span>}
            </span>
            <span className="min-w-0 flex-1 truncate font-semibold">{t.name}</span>
          </button>
        ))}
      </div>
    </ResponsiveModal>
  )
}

export interface DeleteDialogLabels {
  cancel: string
  confirm: string
  folderTitle: (name: string) => string
  itemTitle: (name: string) => string
  /** Body shown when the thing being deleted is an item, not a folder. */
  itemBody: string
  /** Body shown above the counts when a folder still holds something. */
  folderCascade: string
  /** Body shown when the folder is empty. */
  folderEmpty: string
  /** Plural forms for the module's item noun, for the cascade count line. */
  itemPlural: PluralForms
}

/** The admin-only hard-delete escalation. Absent means the module has none. */
export interface HardDeleteOption {
  label: string
  hint: string
}

/**
 * DeleteDialog — every delete confirms first (D50).
 *
 * ⚠ `subfolders` and `items` are what the CALLER counted, and the two pages do not
 * count the same thing today: Poznámky reports the whole subtree, Dokumenty its
 * direct children. Both numbers are rendered verbatim here, because changing
 * either is a behaviour change and not this component's call to make.
 */
export function DeleteDialog({
  isFolder,
  title,
  subfolders,
  items,
  labels,
  hardDelete,
  pending,
  onConfirm,
  onClose,
}: {
  isFolder: boolean
  title: string
  subfolders: number
  items: number
  labels: DeleteDialogLabels
  hardDelete?: HardDeleteOption
  pending: boolean
  onConfirm: (opts: { cascade: boolean; hard: boolean }) => void
  onClose: () => void
}) {
  const [hard, setHard] = useState(false)
  const nonEmpty = isFolder && (subfolders > 0 || items > 0)
  const cascadeParts: string[] = []
  if (subfolders > 0) cascadeParts.push(count(subfolders, PLURAL.folders))
  if (items > 0) cascadeParts.push(count(items, labels.itemPlural))

  return (
    <ResponsiveModal
      open
      onOpenChange={(o) => !o && onClose()}
      title={isFolder ? labels.folderTitle(title) : labels.itemTitle(title)}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {labels.cancel}
          </Button>
          <Button variant="danger" loading={pending} onClick={() => onConfirm({ cascade: nonEmpty, hard })}>
            {labels.confirm}
          </Button>
        </>
      }
    >
      <div className={cn('flex gap-3', nonEmpty && 'rounded-xl border border-warn/40 bg-warn/10 p-3')}>
        {nonEmpty && <AlertTriangle size={18} className="mt-0.5 flex-none text-warn" aria-hidden />}
        <p className="text-sm text-muted text-pretty">
          {!isFolder && labels.itemBody}
          {isFolder && nonEmpty && (
            <>
              {labels.folderCascade}
              {cascadeParts.length > 0 && <span className="mt-1 block font-semibold text-fg">{cascadeParts.join(' · ')}</span>}
            </>
          )}
          {isFolder && !nonEmpty && labels.folderEmpty}
        </p>
      </div>

      {hardDelete && (
        <label className="mt-3 flex cursor-pointer items-start gap-2.5 rounded-xl border border-border bg-s2 p-3">
          <input
            type="checkbox"
            checked={hard}
            onChange={(e) => setHard(e.target.checked)}
            className="mt-0.5 h-4 w-4 accent-[var(--danger)]"
          />
          <span>
            <span className="block text-[13px] font-semibold text-fg">{hardDelete.label}</span>
            <span className="block text-[11.5px] text-subtle text-pretty">{hardDelete.hint}</span>
          </span>
        </label>
      )}
    </ResponsiveModal>
  )
}
