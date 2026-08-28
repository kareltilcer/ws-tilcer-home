import { useMemo, useState } from 'react'
import { unstable_usePrompt as usePrompt } from 'react-router-dom'
import { AlertTriangle, FileText, Trash2, MoveRight } from 'lucide-react'
import { useQuery } from '@tanstack/react-query'
import { cn } from '@/lib/utils'
import { cs } from '@/i18n/cs'
import { fmtDate, fmtStorageBytes } from '@/i18n/format'
import { qk } from '@/api/keys'
import { getDocumentsTree } from '@/api/endpoints'
import { ApiError } from '@/api/client'
import { Button, Spinner } from '@/components/ui/ui'
import { ResponsiveModal } from '@/components/ui/modal'
import { ScreenHeader } from '@/components/common/states'
import { thumbnailURL } from './api/endpoints'
import { useChatStorage, useCleanup, useMoveAttachment, useRemoveAttachment } from './api/hooks'
import type { CleanupItem } from './api/types'
import type { DocFolderNode } from '@/api/types'

/**
 * Úklid úložiště chatu — a working screen, not a dashboard (FR-V10-13, D241–D246).
 *
 * ⚠ *PONECHAT* IS NOT A BUTTON (D242). Nothing is staged, nothing is queued, and
 * closing the page is a valid outcome — *"not every document has to be dealt with
 * at that moment"* is a statement about STATE, not about a control. So there is no
 * "review changes" step and no staged-actions tray here, and the page says what
 * doing nothing means instead of offering a no-op.
 *
 * ⚠ SORTED BY SIZE BY DEFAULT, because that is the order in which cleaning pays —
 * and that ordering is SINGLE-PAGE, which the screen states honestly rather than
 * offering a Load-more that would not work.
 *
 * ⚠ A MOVED OR REMOVED ROW IS GONE ON THE NEXT LOAD (D246), not struck through and
 * not greyed. The listing is *what still counts*.
 */
export function UklidPage() {
  const [sort, setSort] = useState<'size' | 'recent'>('size')
  const storage = useChatStorage()
  const listing = useCleanup(sort)
  const [removing, setRemoving] = useState<CleanupItem | null>(null)
  const [moving, setMoving] = useState<CleanupItem | null>(null)

  /**
   * ⚠ LEAVING WHILE STILL OVER RAISES A CONFIRMATION (D244), AND IT HOOKS THE
   * ROUTER'S NAVIGATION BLOCKER. `beforeunload` alone misses client-side route
   * changes, which is most exits — tapping another tab in the bottom bar never
   * reaches it. It is a CONFIRM, never a block: the member can always leave.
   */
  const stillOver = storage.data?.total_exceeded ?? false
  usePrompt({
    when: stillOver,
    message: `${cs.chat.cleanupLeaveOver(
      fmtStorageBytes(storage.data?.total_bytes ?? null),
      `${storage.data?.threshold_total_mb ?? 0} MB`,
    )} ${cs.chat.cleanupLeaveBody}`,
  })

  // ⚠ THE GROUPING RUNS BEFORE THE 403 BRANCH, and the order is the rules-of-hooks
  // rule rather than a preference: a useMemo after an early return is called
  // conditionally, so a reader's 403 and an editor's listing would run different
  // numbers of hooks and React would desync every one after it.
  //
  // ⚠ AND IT MEMOISES ON `listing.data`, NOT ON `items`. `data?.items ?? []` is a new
  // array reference on every render when the query has not resolved, so keying on it
  // would recompute the grouping on every keystroke elsewhere in the tree — a memo
  // that never hits is a memo that costs and buys nothing.
  const items = listing.data?.items ?? []
  const grouped = useMemo(() => groupByConversation(listing.data?.items ?? []), [listing.data])

  // ⚠ THE READER'S 403 IS A SCREEN, NOT A TOAST (D241). It is the module's one
  // recorded asymmetry — a reader can fill storage they can never clean — and the
  // copy explains rather than scolds.
  if (listing.error instanceof ApiError && listing.error.status === 403) {
    return (
      <div className="mx-auto max-w-3xl">
        <ScreenHeader title={cs.chat.cleanupTitle} />
        <div className="rounded-lg border border-border bg-s1 p-6 text-center">
          <div className="mx-auto mb-3 grid h-12 w-12 place-items-center rounded-lg bg-attention-soft text-attention">
            <AlertTriangle size={22} aria-hidden />
          </div>
          <p className="mb-1 font-bold">{cs.chat.cleanupForbidden}</p>
          <p className="mx-auto max-w-md text-sm text-muted text-pretty">
            {cs.chat.cleanupForbiddenHint}
          </p>
        </div>
      </div>
    )
  }


  return (
    <div className="mx-auto max-w-3xl">
      <ScreenHeader title={cs.chat.cleanupTitle} subtitle={cs.chat.cleanupSubtitle} />

      {storage.data && (
        <div
          className={cn(
            'mb-4 flex flex-wrap items-baseline gap-x-3 gap-y-1 rounded-lg border px-4 py-3',
            storage.data.total_exceeded
              ? 'border-attention/40 bg-attention-soft'
              : 'border-border bg-s1',
          )}
        >
          <span className="text-sm font-semibold">{cs.chat.cleanupTotal}</span>
          <span className="font-mono text-base tabular-nums">
            {fmtStorageBytes(listing.data?.total_bytes ?? null)}
          </span>
          <span className="text-sm text-muted">
            {cs.chat.storageOverTotal(
              fmtStorageBytes(storage.data.total_bytes),
              `${storage.data.threshold_total_mb} MB`,
            )}
          </span>
        </div>
      )}

      <div className="mb-3 flex items-center gap-1">
        <SortTab active={sort === 'size'} onClick={() => setSort('size')}>
          {cs.chat.cleanupSortSize}
        </SortTab>
        <SortTab active={sort === 'recent'} onClick={() => setSort('recent')}>
          {cs.chat.cleanupSortRecent}
        </SortTab>
      </div>

      {listing.isPending ? (
        <div className="grid place-items-center py-12">
          <Spinner />
        </div>
      ) : items.length === 0 ? (
        <div className="rounded-lg border border-border bg-s1 p-8 text-center">
          <p className="text-sm text-muted text-pretty">
            {/* ⚠ A MEMBER OF NO CONVERSATION GETS AN EXPLANATION, NOT A REFUSAL
                (D241): the gate passed, there is simply nothing to clean. The two
                empty states say different things because they ARE different. */}
            {(storage.data?.conversations.length ?? 0) === 0
              ? cs.chat.cleanupEmptyNoRooms
              : cs.chat.cleanupEmpty}
          </p>
        </div>
      ) : (
        <>
          <div className="flex flex-col gap-5">
            {grouped.map((group) => (
              <section key={group.id}>
                <h2 className="mb-2 flex flex-wrap items-baseline gap-2">
                  <span className="text-sm font-bold">{group.name}</span>
                  {group.overLimit && (
                    <span className="rounded-full bg-attention-soft px-2 py-0.5 text-[11px] font-semibold text-attention">
                      {cs.chat.cleanupOverLimit}
                    </span>
                  )}
                  <span className="font-mono text-xs tabular-nums text-muted">
                    {fmtStorageBytes(group.bytes)}
                  </span>
                </h2>
                <ul className="flex flex-col gap-1.5">
                  {group.items.map((item) => (
                    <CleanupRow
                      key={item.attachment.id}
                      item={item}
                      onRemove={() => setRemoving(item)}
                      onMove={() => setMoving(item)}
                    />
                  ))}
                </ul>
              </section>
            ))}
          </div>
          {/* ⚠ *Ponechat* explained rather than offered. */}
          <p className="mt-6 border-t border-border pt-4 text-xs text-muted text-pretty">
            {cs.chat.cleanupKeepExplainer}
          </p>
          {sort === 'size' && (
            <p className="mt-1.5 text-xs text-muted">{cs.chat.cleanupSortSizeSinglePage}</p>
          )}
        </>
      )}

      <RemoveDialog item={removing} onClose={() => setRemoving(null)} />
      <MoveDialog item={moving} onClose={() => setMoving(null)} />
    </div>
  )
}

function SortTab({
  active,
  onClick,
  children,
}: {
  active: boolean
  onClick: () => void
  children: React.ReactNode
}) {
  return (
    <button
      type="button"
      role="tab"
      aria-selected={active}
      onClick={onClick}
      className={cn(
        'min-h-[36px] rounded-md px-3 text-[13px]',
        active ? 'bg-s3 font-bold text-fg' : 'font-semibold text-muted hover:bg-s2',
      )}
    >
      {children}
    </button>
  )
}

function CleanupRow({
  item,
  onRemove,
  onMove,
}: {
  item: CleanupItem
  onRemove: () => void
  onMove: () => void
}) {
  const a = item.attachment
  const storage = useChatStorage()
  // ⚠ NO BUTTON AT ALL WHEN THERE IS NO SINK (D239). The move is 501 in that
  // deployment and it does NOT fall back to delete, so offering it would be
  // offering a control that can only fail.
  const canMove = storage.data?.can_clean_up ?? false
  return (
    <li className="flex items-center gap-3 rounded-md border border-border bg-s1 px-3 py-2">
      {a.has_thumbnail ? (
        <img
          src={thumbnailURL(a.id)}
          alt=""
          width={40}
          height={40}
          className="h-10 w-10 flex-none rounded object-cover"
        />
      ) : (
        <span className="grid h-10 w-10 flex-none place-items-center rounded bg-s2 text-muted">
          <FileText size={17} aria-hidden />
        </span>
      )}
      <span className="min-w-0 flex-1">
        <span className="block truncate text-sm font-semibold">{a.original_filename}</span>
        <span className="block text-[11px] text-muted">
          {cs.chat.cleanupUploadedBy} {item.uploaded_by_label} · {fmtDate(new Date(a.created_at))}
        </span>
      </span>
      <span className="flex-none font-mono text-xs tabular-nums text-muted">
        {fmtStorageBytes(a.byte_size)}
      </span>
      <span className="flex flex-none items-center gap-1">
        {canMove && (
          <Button variant="ghost" size="sm" aria-label={cs.chat.moveConfirm} onClick={onMove}>
            <MoveRight size={15} aria-hidden />
          </Button>
        )}
        <Button
          variant="ghost"
          size="sm"
          aria-label={cs.chat.wordRemoveFile}
          onClick={onRemove}
        >
          <Trash2 size={15} className="text-danger" aria-hidden />
        </Button>
      </span>
    </li>
  )
}

function RemoveDialog({ item, onClose }: { item: CleanupItem | null; onClose: () => void }) {
  const remove = useRemoveAttachment(item?.conversation_id)
  return (
    <ResponsiveModal
      open={item !== null}
      onOpenChange={(open) => !open && onClose()}
      title={cs.chat.removeAttachmentTitle}
      footer={
        <>
          <Button onClick={onClose}>{cs.chat.cancel}</Button>
          <Button
            variant="danger"
            loading={remove.isPending}
            onClick={() =>
              item && remove.mutate(item.attachment.id, { onSuccess: onClose })
            }
          >
            {cs.chat.confirmDelete}
          </Button>
        </>
      }
    >
      <p className="text-sm text-pretty">{cs.chat.removeAttachmentBody}</p>
      {item && (
        <p className="mt-2 text-sm font-semibold">
          {item.attachment.original_filename}{' '}
          <span className="font-mono font-normal tabular-nums text-muted">
            {fmtStorageBytes(item.attachment.byte_size)}
          </span>
        </p>
      )}
    </ResponsiveModal>
  )
}

/**
 * ⚠ THE PUBLISH SENTENCE STANDS BEFORE THE CONFIRM (D245), fixed and verbatim. A
 * move is the one action in v10 that WIDENS access: the file becomes readable by
 * every household member, including people who are not in this conversation.
 *
 * ⚠ AND THE PICKER OFFERS SHARED FOLDERS ONLY, with the private roots ABSENT AND
 * EXPLAINED rather than greyed out — a private target would make the file
 * unreadable to the conversation's other members, which is the opposite of what the
 * move is for. The server refuses one with 422 regardless; this is what stops a
 * member choosing it in the first place.
 */
function MoveDialog({ item, onClose }: { item: CleanupItem | null; onClose: () => void }) {
  const move = useMoveAttachment(item?.conversation_id)
  const [folderID, setFolderID] = useState('')
  const tree = useQuery({
    queryKey: qk.documentsTree('shared'),
    queryFn: () => getDocumentsTree('shared'),
    enabled: item !== null,
  })
  const folders = useMemo(() => flattenFolders(tree.data?.roots ?? []), [tree.data])

  return (
    <ResponsiveModal
      open={item !== null}
      onOpenChange={(open) => {
        if (!open) {
          setFolderID('')
          onClose()
        }
      }}
      title={cs.chat.moveTitle}
      footer={
        <>
          <Button onClick={onClose}>{cs.chat.cancel}</Button>
          <Button
            variant="primary"
            loading={move.isPending}
            disabled={!folderID}
            onClick={() =>
              item &&
              move.mutate(
                { id: item.attachment.id, folderID },
                {
                  onSuccess: () => {
                    setFolderID('')
                    onClose()
                  },
                },
              )
            }
          >
            {cs.chat.moveConfirm}
          </Button>
        </>
      }
    >
      {/* The fixed sentence, first and unmissable. */}
      <p className="mb-3 rounded-md bg-attention-soft px-3 py-2 text-sm text-attention text-pretty">
        {cs.chat.movePublishSentence}
      </p>
      <p className="mb-3 text-sm text-muted text-pretty">{cs.chat.moveBody}</p>

      <label className="block text-sm font-semibold" htmlFor="chat-move-folder">
        {cs.chat.moveFolder}
      </label>
      <select
        id="chat-move-folder"
        value={folderID}
        onChange={(e) => setFolderID(e.target.value)}
        className="mt-1 h-10 w-full rounded-md border border-border bg-s2 px-2.5 text-sm"
      >
        <option value="">{cs.chat.moveFolderPlaceholder}</option>
        {folders.map((f) => (
          <option key={f.id} value={f.id}>
            {f.label}
          </option>
        ))}
      </select>
      <p className="mt-1.5 text-xs text-muted text-pretty">
        {folders.length === 0 && !tree.isPending ? cs.chat.moveNoFolders : cs.chat.moveSharedOnly}
      </p>
    </ResponsiveModal>
  )
}

// ---- grouping ----

interface Group {
  id: string
  name: string
  overLimit: boolean
  bytes: number
  items: CleanupItem[]
}

/**
 * groupByConversation keeps the SERVER'S order inside each group and orders the
 * groups by their own weight.
 *
 * ⚠ It never re-sorts the rows. Under `sort=size` the server has already put the
 * biggest first, and re-sorting here would be a second ordering that disagrees with
 * the one the cursor (under `sort=recent`) can resume.
 */
function groupByConversation(items: CleanupItem[]): Group[] {
  const byID = new Map<string, Group>()
  for (const item of items) {
    let group = byID.get(item.conversation_id)
    if (!group) {
      group = {
        id: item.conversation_id,
        name: item.conversation_name,
        overLimit: item.conversation_over_limit,
        bytes: 0,
        items: [],
      }
      byID.set(item.conversation_id, group)
    }
    group.items.push(item)
    group.bytes += item.attachment.byte_size
  }
  // Over-limit rooms first — that is where cleaning pays — then by weight.
  return [...byID.values()].sort((a, b) => {
    if (a.overLimit !== b.overLimit) return a.overLimit ? -1 : 1
    return b.bytes - a.bytes
  })
}

/** flattenFolders renders the shared tree as an indented flat list for a <select>. */
function flattenFolders(
  roots: DocFolderNode[],
  depth = 0,
): { id: string; label: string }[] {
  const out: { id: string; label: string }[] = []
  for (const node of roots) {
    if (node.folder.archived) continue
    out.push({ id: node.folder.id, label: `${'  '.repeat(depth)}${node.folder.name}` })
    out.push(...flattenFolders(node.subfolders, depth + 1))
  }
  return out
}
