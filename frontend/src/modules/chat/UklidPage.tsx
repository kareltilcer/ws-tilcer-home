import { useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { useLeaveConfirm } from './useLeaveConfirm'
import { AlertTriangle, ArrowLeft, FileText } from 'lucide-react'
import { useQuery } from '@tanstack/react-query'
import { cn } from '@/lib/utils'
import { cs } from '@/i18n/cs'
import { fmtDate, fmtStorageBytes } from '@/i18n/format'
import { qk } from '@/api/keys'
import { routes } from '@/app/routes'
import { getDocumentsTree } from '@/modules/documents/api/endpoints'
import { ApiError } from '@/api/client'
import { Button, Spinner } from '@/components/ui/ui'
import { ResponsiveModal } from '@/components/ui/modal'
import { thumbnailURL } from './api/endpoints'
import { useChatStorage, useCleanup, useMoveAttachment, useRemoveAttachment } from './api/hooks'
import type { CleanupItem } from './api/types'
import type { DocFolderNode } from '@/modules/documents/api/types'

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
   * ⚠ LEAVING WHILE STILL OVER RAISES A CONFIRMATION (D244), and it catches
   * CLIENT-SIDE route changes — tapping another tab in the bottom bar, the side nav,
   * the header's back link — because that is most exits and `beforeunload` never
   * sees any of them. It is a CONFIRM, never a block. See useLeaveConfirm for why it
   * is not react-router's `useBlocker` (data-router only; this app mounts
   * `<BrowserRouter>`) and for the one exit it does not cover.
   */
  /**
   * ⚠ AND IT NAMES *WHICH* THRESHOLD (D244), which is why the per-conversation one
   * is here too. It used to watch only the module total, so somebody who came in
   * from the warning about one heavy room — the case the link on that warning
   * exists for — left without being asked, because the household as a whole was
   * fine. A sentence about the total, on a screen a member reached because of a
   * room, breaks the one thing this page stands on: that its figures mean what
   * they say.
   */
  const overRoom = storage.data?.conversations.find((c) => c.over_limit)
  const overTotalNow = storage.data?.total_exceeded ?? false
  const stillOver = overTotalNow || !!overRoom
  const leaveLine = overTotalNow
    ? cs.chat.cleanupLeaveOver(
        fmtStorageBytes(storage.data?.total_bytes ?? null),
        `${storage.data?.threshold_total_mb ?? 0} MB`,
      )
    : cs.chat.cleanupLeaveOverConversation(
        overRoom?.name ?? '',
        fmtStorageBytes(overRoom?.bytes ?? null),
        `${storage.data?.threshold_conversation_mb ?? 0} MB`,
      )
  useLeaveConfirm(stillOver, `${leaveLine} ${cs.chat.cleanupLeaveBody}`)

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
  // The room's OWN weight, beside what this single page of it lists. Keyed rather
  // than searched per group: `conversations` is the caller's whole membership.
  const roomBytes = useMemo(
    () => new Map((storage.data?.conversations ?? []).map((c) => [c.id, c.bytes])),
    [storage.data],
  )

  // ⚠ THE READER'S 403 IS A SCREEN, NOT A TOAST (D241). It is the module's one
  // recorded asymmetry — a reader can fill storage they can never clean — and the
  // copy explains rather than scolds.
  if (listing.error instanceof ApiError && listing.error.status === 403) {
    return (
      <div className="mx-auto max-w-[1000px]">
        <CleanupHeader />
        <div className="max-w-[620px] rounded-[14px] border border-border bg-s1 p-6">
          <div className="mb-2 flex items-center gap-2 font-mono text-[10.5px] uppercase tracking-[0.06em] text-subtle">
            <AlertTriangle size={13} aria-hidden />
            {cs.chat.cleanupForbiddenLabel}
          </div>
          <p className="mb-2 text-[17px] font-extrabold">{cs.chat.cleanupForbidden}</p>
          <p className="text-[13.5px] leading-relaxed text-muted text-pretty">
            {cs.chat.cleanupForbiddenHint}
          </p>
        </div>
      </div>
    )
  }

  return (
    <div className="mx-auto max-w-[1000px]">
      <CleanupHeader />

      {/* The figure this screen exists to move, the flag when it is over, and the
          ordering — one line, because they are read together. */}
      <div className="mb-4 flex flex-wrap items-center gap-x-3 gap-y-2">
        {/* `overTotalNow`, not a second reading of the same field: the leave
            confirmation above already decided what the total means, and two names
            for one verdict is how the badge and the sentence come to disagree. */}
        {overTotalNow && (
          <span className="inline-flex flex-none items-center gap-1.5 rounded-full border border-attention bg-attention-soft px-3 py-1 text-xs font-bold text-attention">
            <AlertTriangle size={12} aria-hidden />
            {cs.chat.storageWarnWord}
          </span>
        )}
        {storage.data && (
          <span className="font-mono text-[12.5px] tabular-nums">
            {cs.chat.cleanupTotalLine(
              fmtStorageBytes(storage.data.total_bytes),
              `${storage.data.threshold_total_mb} MB`,
            )}
          </span>
        )}
        <span className="hidden flex-1 lg:block" />
        <div className="flex items-center gap-1.5">
          <SortTab active={sort === 'size'} onClick={() => setSort('size')}>
            {cs.chat.cleanupSortSize}
          </SortTab>
          <SortTab active={sort === 'recent'} onClick={() => setSort('recent')}>
            {cs.chat.cleanupSortRecent}
          </SortTab>
        </div>
      </div>

      {listing.isPending ? (
        <div className="grid place-items-center py-12">
          <Spinner />
        </div>
      ) : items.length === 0 ? (
        <div className="max-w-[560px] rounded-[14px] border border-dashed border-border px-6 py-7 text-center">
          <p className="mb-2 text-[17px] font-extrabold">{cs.chat.cleanupEmptyTitle}</p>
          <p className="text-[13.5px] leading-relaxed text-muted text-pretty">
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
          <div className="flex flex-col gap-3.5">
            {grouped.map((group) => (
              <section
                key={group.id}
                className="overflow-hidden rounded-[14px] border border-border bg-s1"
              >
                <div className="flex flex-wrap items-center gap-x-2.5 gap-y-1.5 border-b border-border px-4 py-3">
                  <h2 className="text-[14.5px] font-extrabold">{group.name}</h2>
                  {group.overLimit && (
                    <span className="flex-none whitespace-nowrap rounded-full border border-attention bg-attention-soft px-2 py-0.5 text-[10.5px] font-bold text-attention">
                      {cs.chat.cleanupOverLimit}
                    </span>
                  )}
                  <span className="hidden flex-1 sm:block" />
                  {/* ⚠ TWO FIGURES, NOT ONE. What this VIEW lists is not what the
                      ROOM holds — sorting by size is single-page, and a heading
                      showing only the page's sum would quietly claim the room is
                      lighter than it is, on the screen whose whole value is that its
                      numbers add up. */}
                  <span className="font-mono text-[11.5px] tabular-nums text-muted">
                    {cs.chat.cleanupGroupSum(
                      fmtStorageBytes(group.bytes),
                      fmtStorageBytes(roomBytes.get(group.id) ?? null),
                    )}
                  </span>
                </div>
                <ul>
                  {group.items.map((item) => (
                    <CleanupRow
                      key={item.attachment.id}
                      item={item}
                      // ⚠ PASSED DOWN, NOT RE-QUERIED PER ROW. Each row calling
                      // useChatStorage() subscribed 200 components to one key — the
                      // fetch is deduped, the re-renders are not, and they all fire
                      // on every Odstranit and every move, which is exactly when the
                      // list is busiest.
                      canMove={storage.data?.move_available ?? false}
                      onRemove={() => setRemoving(item)}
                      onMove={() => setMoving(item)}
                    />
                  ))}
                </ul>
              </section>
            ))}
          </div>

          {sort === 'size' && (
            <p className="mt-3.5 max-w-[80ch] rounded-xl border border-dashed border-border px-4 py-3 text-[12.5px] leading-relaxed text-muted text-pretty">
              {cs.chat.cleanupSortSizeSinglePage}
            </p>
          )}

          {/* ⚠ *Ponechat* explained rather than offered (D242), and beside it the
              only exit control — because leaving is confirmed, never blocked (D244),
              and a screen that confirms an exit should say where the exit is. */}
          <div className="mt-3.5 flex flex-col gap-3 sm:flex-row sm:items-center">
            <p className="max-w-[62ch] flex-1 text-[12.5px] text-subtle text-pretty">
              {cs.chat.cleanupKeepExplainer}
            </p>
            {stillOver ? (
              <Link
                to={routes.chat}
                className="inline-flex min-h-11 flex-none items-center justify-center rounded-[10px] border border-border bg-s2 px-3.5 text-[12.5px] font-semibold hover:bg-s3 lg:min-h-9"
              >
                {cs.chat.cleanupLeavePage}
              </Link>
            ) : (
              <span className="flex-none text-[12.5px] text-muted">{cs.chat.cleanupLeaveFree}</span>
            )}
          </div>
        </>
      )}

      <RemoveDialog item={removing} onClose={() => setRemoving(null)} />
      <MoveDialog item={moving} onClose={() => setMoving(null)} />
    </div>
  )
}

/**
 * The screen's own header — a breadcrumb and a way back, not `ScreenHeader`.
 *
 * ⚠ `/chat/uklid` IS A SUB-PAGE OF A MODULE THAT HAS NO OTHER SUB-PAGES, and it is
 * reached from a warning rather than from the nav — so nothing in the chrome says
 * where a member is or how to get back to the conversation they were reading. The
 * standard page title says neither.
 */
function CleanupHeader() {
  return (
    <header className="mb-5 flex flex-wrap items-start gap-3">
      <div className="min-w-0 flex-1">
        <div className="mb-1 font-mono text-[11px] text-muted">
          {cs.nav.chat} <span className="text-subtle">›</span>{' '}
          <span className="text-fg">{cs.chat.cleanupTitle}</span>
        </div>
        <h1 className="text-[22px] font-extrabold tracking-tight">{cs.chat.cleanupTitle}</h1>
        <p className="mt-1 max-w-[78ch] text-[12.5px] text-muted text-pretty">
          {cs.chat.cleanupSubtitle}
        </p>
      </div>
      <Link
        to={routes.chat}
        className="inline-flex min-h-11 flex-none items-center gap-1.5 rounded-[10px] border border-border bg-s2 px-3.5 text-[12.5px] font-semibold hover:bg-s3 lg:min-h-9"
      >
        <ArrowLeft size={14} aria-hidden />
        {cs.chat.cleanupBack}
      </Link>
    </header>
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
        'inline-flex min-h-9 items-center whitespace-nowrap rounded-full border px-3 font-mono text-[11px] font-semibold',
        active
          ? 'border-accent bg-accent-soft text-fg'
          : 'border-border bg-s2 text-muted hover:text-fg',
      )}
    >
      {children}
    </button>
  )
}

function CleanupRow({
  item,
  canMove,
  onRemove,
  onMove,
}: {
  item: CleanupItem
  /**
   * ⚠ NO BUTTON AT ALL WHEN THERE IS NO SINK (D239) — and this is `move_available`,
   * NOT `can_clean_up`. The two answer different questions: one is the role gate,
   * the other is whether `documents` was wired to accept custody at all. Gating on
   * the role meant a deployment with no sink still offered the control, opened the
   * folder picker, and answered 501 after the confirm — which is the opposite of a
   * capability being plainly absent.
   */
  canMove: boolean
  onRemove: () => void
  onMove: () => void
}) {
  const a = item.attachment
  return (
    <li className="border-b border-border px-4 py-3 last:border-b-0">
      <div className="flex items-center gap-3">
        {a.has_thumbnail ? (
          <img
            src={thumbnailURL(a.id)}
            alt=""
            width={40}
            height={40}
            className="h-10 w-10 flex-none rounded-[9px] border border-border object-cover"
          />
        ) : (
          <span className="grid h-10 w-10 flex-none place-items-center rounded-[9px] border border-border bg-s2 text-muted">
            <FileText size={17} aria-hidden />
          </span>
        )}
        <span className="min-w-0 flex-1">
          <span className="block truncate text-[13px] font-semibold">{a.original_filename}</span>
          <span className="mt-0.5 block truncate text-[11.5px] text-muted">
            {cs.chat.cleanupUploadedBy} {item.uploaded_by_label} · {fmtDate(new Date(a.created_at))}
          </span>
        </span>
        <span className="flex-none font-mono text-[13px] tabular-nums">
          {fmtStorageBytes(a.byte_size)}
        </span>
      </div>
      {/* ⚠ THE VERBS CARRY THEIR WORDS. An icon rail was two unlabelled glyphs for
          the two actions this screen exists for, one of which PUBLISHES the file to
          the whole household (D245) — and *Přesunout do Dokumentů* is fixed
          vocabulary, so the row is exactly where it has to be readable. They wrap
          under the row on a phone and sit beside it at the desk. */}
      <div className="mt-2.5 flex flex-wrap gap-2 sm:mt-0 sm:justify-end">
        {canMove && (
          <Button
            variant="secondary"
            size="sm"
            className="min-h-11 flex-1 border-border-strong sm:min-h-8 sm:flex-none"
            onClick={onMove}
          >
            {cs.chat.word.moveToDocuments}
          </Button>
        )}
        <Button
          variant="ghost"
          size="sm"
          className="min-h-11 flex-none border-border px-3.5 sm:min-h-8"
          onClick={onRemove}
        >
          {cs.chat.word.remove}
        </Button>
      </div>
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
