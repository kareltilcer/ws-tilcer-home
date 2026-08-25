import { Lock, Users } from 'lucide-react'
import { ResponsiveModal } from '@/components/ui/modal'
import { Button } from '@/components/ui/ui'
import { cs } from '@/i18n/cs'
import { count, PLURAL } from '@/i18n/plural'

/**
 * "Publikovat do sdílených" — the one irreversible action in either tree
 * (v9, D182, HANDOFF-design §v9 §3).
 *
 * Everything else in Home is a soft delete with an audit trail: forgiving, and
 * reversible by somebody. This is not. A published item is visible to the
 * household from that moment, and THERE IS NO UNPUBLISH ROUTE — not "not yet",
 * but by decision. Somebody who wants it back must re-create it privately and
 * delete the shared copy, leaving both facts in the log.
 *
 * ⚠ THE WEIGHT IS CARRIED BY THE SENTENCE, NOT BY COLOUR. The confirm button is
 * `accent`, not `danger`: nothing is being destroyed, and borrowing the delete red
 * would both misdescribe the action and dilute the one colour that means delete.
 * Home has an established discipline here (v8's "blocked ≠ error"), and this is
 * the same discipline applied to "irreversible ≠ destructive".
 *
 * ⚠ AND THERE IS NO UNDO TOAST. Not an oversight — there would be nothing for it
 * to call. A toast offering "vrátit" that then cannot is worse than no toast.
 *
 * The folder variant states HOW MANY items become visible, because "publikovat
 * složku" reads much smaller than what it does.
 */
export function PublishDialog({
  kind,
  title,
  itemCount,
  folderCount,
  pending,
  onConfirm,
  onClose,
}: {
  kind: 'note' | 'document' | 'folder'
  title: string
  /** Folder variant: notes/documents that become visible. */
  itemCount?: number
  /** Folder variant: subfolders that become visible. */
  folderCount?: number
  pending: boolean
  onConfirm: () => void
  onClose: () => void
}) {
  const heading =
    kind === 'folder'
      ? cs.privacy.publishFolderTitle
      : kind === 'document'
        ? cs.privacy.publishDocumentTitle
        : cs.privacy.publishNoteTitle

  return (
    <ResponsiveModal
      open
      onOpenChange={(o) => !o && onClose()}
      title={heading}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {cs.notes.cancel}
          </Button>
          {/* accent, not danger — see the note above. */}
          <Button loading={pending} onClick={onConfirm}>
            {cs.privacy.publishConfirm}
          </Button>
        </>
      }
    >
      <div className="space-y-3">
        {/* The before/after is drawn rather than described: two chips and an
            arrow say "this audience becomes that audience" faster than a
            sentence, and the sentence below then carries only the consequence. */}
        <div className="flex items-center gap-2 text-[12.5px]">
          <span className="inline-flex items-center gap-1.5 rounded-full border border-vis-private bg-vis-private-soft px-2 py-1 font-semibold">
            <Lock className="size-3.5" aria-hidden />
            {cs.privacy.private}
          </span>
          <span aria-hidden className="text-muted">
            →
          </span>
          <span className="inline-flex items-center gap-1.5 rounded-full border border-accent bg-accent-soft px-2 py-1 font-semibold text-accent">
            <Users className="size-3.5" aria-hidden />
            {cs.privacy.shared}
          </span>
        </div>

        <p className="truncate text-sm font-semibold text-fg" title={title}>
          {title}
        </p>

        {kind === 'folder' && (
          <>
            <p className="text-sm font-semibold text-fg text-pretty">
              {cs.privacy.publishFolderCount(
                count(itemCount ?? 0, PLURAL.items),
                folderCount ? count(folderCount, PLURAL.subfolders) : '',
              )}
            </p>
            {/* The count comes from the live tree (archived excluded), but the
                backend publishes archived descendants too — without this line
                the dialog understates what becomes household-visible. */}
            <p className="text-sm text-muted text-pretty">{cs.privacy.publishFolderArchivedNote}</p>
          </>
        )}

        {/* The irreversibility, in one sentence, without theatrics. The
            consequence in a household app is usually mild and occasionally
            serious; the copy has to fit both without crying wolf. */}
        <p className="text-sm text-muted text-pretty">{cs.privacy.publishBody}</p>
      </div>
    </ResponsiveModal>
  )
}
