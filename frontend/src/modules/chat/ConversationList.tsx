import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { Plus, Search, Trash2 } from 'lucide-react'
import { cn } from '@/lib/utils'
import { cs } from '@/i18n/cs'
import { count, PLURAL } from '@/i18n/plural'
import { Button, Spinner } from '@/components/ui/ui'
import { ResponsiveModal } from '@/components/ui/modal'
import { Input } from '@/components/ui/ui'
import {
  useChatSearch,
  useConversations,
  useCreateConversation,
  useDeleteConversation,
  useLoadMoreConversations,
  useRestoreConversation,
} from './api/hooks'
import type { Conversation } from './api/types'

/**
 * The conversation list — left pane at ≥1024, the whole screen below it.
 *
 * ⚠ WHICH CONVERSATION IS ON SCREEN MUST BE UNMISTAKABLE at 375 px in both themes.
 * The cost of getting it wrong is posting into the wrong room, and there is no
 * unsend — edit and delete leave a tombstone everybody has already seen. So the
 * selected row carries three cues, not one: an accent left rule, a raised surface,
 * and `aria-current`.
 */
export function ConversationList({ activeID }: { activeID?: string }) {
  const active = useConversations('active')
  const [creating, setCreating] = useState(false)
  const [query, setQuery] = useState('')
  // ⚠ THE KOŠ IS FETCHED ONLY WHEN IT IS OPENED. It used to be a second request on
  // every mount and on every chatAll invalidation — create, rename, delete,
  // restore, remove-member and every membership frame — for a section that renders
  // only when it is non-empty, in the module whose whole live-sync design is
  // justified by counting requests.
  const [trashOpen, setTrashOpen] = useState(false)
  const trashed = useConversations('trash', trashOpen)
  const moreActive = useLoadMoreConversations('active')
  const moreTrashed = useLoadMoreConversations('trash')

  const searching = query.trim().length > 0

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex items-center justify-between gap-2 border-b border-border px-4 py-3">
        <h2 className="text-sm font-bold tracking-tight">{cs.chat.listTitle}</h2>
        <Button size="sm" variant="secondary" onClick={() => setCreating(true)}>
          <Plus size={14} aria-hidden />
          {cs.chat.word.newConversation}
        </Button>
      </div>

      {/* ⚠ SEARCH LIVES HERE, over the whole list, because a hit spans conversations
          — the server's one MATCH carries the membership join and the per-row floor
          precisely so it can (D251). Scoping the box to the open thread would throw
          that away. */}
      <div className="border-b border-border px-3 py-2">
        <div className="relative">
          <Search size={14} aria-hidden className="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-muted" />
          <Input
            value={query}
            type="search"
            className="pl-8"
            placeholder={cs.chat.searchPlaceholder}
            aria-label={cs.chat.searchPlaceholder}
            onChange={(e) => setQuery(e.target.value)}
          />
        </div>
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto om-scroll">
        {searching ? (
          <SearchResults query={query} />
        ) : (
          <>
            {active.isPending && (
              <div className="grid place-items-center py-10 text-muted">
                <Spinner />
              </div>
            )}

            {active.data?.items.length === 0 && !active.isPending && (
              <div className="px-4 py-8 text-center">
                <div className="mb-1.5 text-sm font-bold">{cs.chat.emptyTitle}</div>
                <p className="text-sm text-muted text-pretty">{cs.chat.emptyBody}</p>
              </div>
            )}

            <ul className="p-2">
              {active.data?.items.map((c) => (
                <li key={c.id}>
                  <ConversationRow conversation={c} selected={c.id === activeID} />
                </li>
              ))}
            </ul>

            {/* ⚠ THE LIST IS PAGED, AND SAYING SO IS THE POINT. The server clamps
                `limit` to 50 and hands back a `next_cursor`; consuming neither made
                the 51st room unreachable — no row, no unread badge, and nothing on
                screen to say a page had been withheld. */}
            {active.data?.next_cursor && (
              <div className="px-2 pb-2">
                <Button
                  size="sm"
                  variant="secondary"
                  className="w-full"
                  loading={moreActive.isPending}
                  onClick={() => moreActive.mutate()}
                >
                  {cs.chat.loadMore}
                </Button>
              </div>
            )}

            {/* The koš, as its own section (D253). A trashed conversation has left
                every other surface entirely, so this is the only place it appears at
                all — which is why the section header is always here to be opened,
                rather than appearing only once something is in it. */}
            <div className="border-t border-border p-2">
              <button
                type="button"
                onClick={() => setTrashOpen((v) => !v)}
                aria-expanded={trashOpen}
                className="flex w-full items-center gap-1.5 rounded px-2 py-1.5 text-xs font-bold uppercase tracking-wide text-muted hover:bg-s2 hover:text-fg"
              >
                <Trash2 size={12} aria-hidden />
                {cs.chat.trashSectionTitle}
              </button>
              {trashOpen && (
                <>
                  {trashed.isPending && (
                    <div className="grid place-items-center py-4 text-muted">
                      <Spinner />
                    </div>
                  )}
                  {trashed.data?.items.length === 0 && (
                    <p className="px-2 py-2 text-xs text-muted">{cs.chat.trashEmpty}</p>
                  )}
                  <ul>
                    {trashed.data?.items.map((c) => (
                      <li key={c.id}>
                        <TrashedRow conversation={c} />
                      </li>
                    ))}
                  </ul>
                  {trashed.data?.next_cursor && (
                    <Button
                      size="sm"
                      variant="ghost"
                      className="mt-1 w-full"
                      loading={moreTrashed.isPending}
                      onClick={() => moreTrashed.mutate()}
                    >
                      {cs.chat.loadMore}
                    </Button>
                  )}
                </>
              )}
            </div>
          </>
        )}
      </div>

      <NewConversationDialog open={creating} onOpenChange={setCreating} />
    </div>
  )
}

/**
 * Search hits, in place of the list.
 *
 * ⚠ SINGLE PAGE, AND THE SCREEN SAYS SO. The ordering is relevance and a keyset
 * cursor is an id, which does not locate a position in a rank ordering — the server
 * answers a cursor with 422 rather than serving page one forever, so there is
 * deliberately no Load-more here and a line of copy explains the absence.
 */
function SearchResults({ query }: { query: string }) {
  const hits = useChatSearch(query)

  if (hits.isPending) {
    return (
      <div className="grid place-items-center py-10 text-muted">
        <Spinner />
      </div>
    )
  }
  if (!hits.data?.items.length) {
    return <p className="px-4 py-8 text-center text-sm text-muted">{cs.chat.searchEmpty}</p>
  }
  return (
    <>
      <ul className="p-2">
        {hits.data.items.map((h) => (
          <li key={h.message_id}>
            <Link
              to={`/chat/${h.conversation_id}`}
              className="block rounded-md px-3 py-2.5 text-muted hover:bg-s2 hover:text-fg"
            >
              <span className="flex items-baseline gap-2">
                <span className="min-w-0 flex-1 truncate text-xs font-bold text-fg">
                  {h.conversation_name}
                </span>
                <span className="flex-none text-[11px] text-subtle">{h.author_label}</span>
              </span>
              <span className="mt-0.5 block break-words text-sm">{h.snippet}</span>
            </Link>
          </li>
        ))}
      </ul>
      <p className="px-4 pb-4 text-xs text-muted text-pretty">{cs.chat.searchSinglePage}</p>
    </>
  )
}

function ConversationRow({ conversation, selected }: { conversation: Conversation; selected: boolean }) {
  const unread = conversation.unread_count
  return (
    <Link
      to={`/chat/${conversation.id}`}
      aria-current={selected ? 'page' : undefined}
      className={cn(
        'flex items-center gap-3 rounded-md border-l-2 px-3 py-2.5 transition-colors',
        selected
          ? 'border-l-accent bg-s2 text-fg'
          : 'border-l-transparent text-muted hover:bg-s2 hover:text-fg',
      )}
    >
      <span className="min-w-0 flex-1">
        <span className={cn('block truncate text-sm', selected || unread > 0 ? 'font-bold text-fg' : 'font-medium')}>
          {conversation.name}
        </span>
        <span className="mt-0.5 flex items-baseline gap-1.5 truncate text-xs text-muted">
          <span className="truncate">{count(conversation.member_count, PLURAL.members)}</span>
          {/* ⚠ THE FLAG IS THE POINT, NOT THE SIZE. HANDOFF-design §v10 lists "one
              room over its limit" as a state of this list, and it is the only place
              a member finds out WHICH of their rooms is heavy — the module warning
              says the household is over, not where. The mark reuses `--attention`
              (informational: nothing is blocked and nobody did anything wrong) and
              carries a word, never colour alone.

              ⚠ Rendered only when the verdict is a real `true`. It is a pointer for
              the D161 reason: a verdict about an unmeasured figure cannot be more
              certain than the figure, so `null` renders nothing rather than "under
              the limit". */}
          {conversation.over_conversation_limit === true && (
            <span className="flex-none rounded-full bg-attention-soft px-1.5 py-px text-[10px] font-semibold text-attention">
              {cs.chat.cleanupOverLimit}
            </span>
          )}
        </span>
      </span>
      {unread > 0 && (
        <span
          // ⚠ ACCENT, NOT A WARNING COLOUR, and mono tabular so 3 and 40 do not
          // shift the row. An unread count is a reason to open something, not an
          // alarm about it.
          className="flex-none rounded-full bg-accent px-2 py-0.5 font-mono text-[11px] font-bold tabular-nums text-accent-fg"
          // ⚠ Through `count`, like every other number in this module and in Home
          // (D20). The badge renders the numeral alone, so this label is all a
          // screen reader gets — and a fixed noun made it "1 nepřečtené zprávy".
          aria-label={count(unread, PLURAL.unreadMessages)}
        >
          {unread}
        </span>
      )}
    </Link>
  )
}

/**
 * A row in the koš.
 *
 * ⚠ ITS BYTES STILL COUNT toward both storage thresholds until it is really purged
 * (D254), which is honest and looks wrong — so the countdown is stated rather than
 * left to be inferred, and *Smazat natrvalo* (PR 3's storage half) is what stops it
 * trapping anyone.
 */
function TrashedRow({ conversation }: { conversation: Conversation }) {
  const restore = useRestoreConversation()
  const [purging, setPurging] = useState(false)
  return (
    <div className="flex items-center gap-3 rounded-md px-3 py-2.5">
      <span className="min-w-0 flex-1">
        <span className="block truncate text-sm font-medium text-muted line-through">
          {conversation.name}
        </span>
        {conversation.purge_after && (
          <span className="mt-0.5 block truncate text-xs text-muted">
            {cs.chat.trashDaysLeft} {daysUntil(conversation.purge_after)}
          </span>
        )}
      </span>
      <Button
        size="sm"
        variant="ghost"
        loading={restore.isPending}
        onClick={() => restore.mutate(conversation.id)}
      >
        {cs.chat.word.restore}
      </Button>
      {/* ⚠ THE OTHER DOOR, AND THE KOŠ IS WHERE IT BELONGS (v10 review). The
          service accepts `?hard=true` on an already-trashed room precisely so
          Smazat natrvalo can be reached from here — and nothing could reach it:
          the only other button lives in the thread header, which a trashed room
          can never render, because it has left every read and /chat/{id} answers
          404. So the row offered Obnovit or seven days, and D254's "its bytes go
          on counting until it is really purged" had no exit. */}
      <Button size="sm" variant="ghost" className="text-danger" onClick={() => setPurging(true)}>
        {cs.chat.word.purge}
      </Button>
      <PurgeDialog
        conversation={conversation}
        open={purging}
        onClose={() => setPurging(false)}
      />
    </div>
  )
}

/**
 * Purging one room out of the koš.
 *
 * ⚠ THE NAME IS TYPED HERE TOO, and the first typing does not carry over. That one
 * confirmed a REVERSIBLE move — the room is sitting in the koš because it worked —
 * and this one destroys every message and file in it with no restore behind it
 * (D253). The same prompt for the two is what makes the second one land.
 */
function PurgeDialog({
  conversation,
  open,
  onClose,
}: {
  conversation: Conversation
  open: boolean
  onClose: () => void
}) {
  const [typed, setTyped] = useState('')
  const purge = useDeleteConversation()

  useEffect(() => {
    if (open) setTyped('')
  }, [open])

  return (
    <ResponsiveModal
      open={open}
      onOpenChange={(o) => !o && onClose()}
      title={cs.chat.purgeTitle}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {cs.chat.cancel}
          </Button>
          <Button
            variant="danger"
            loading={purge.isPending}
            disabled={typed.trim() !== conversation.name}
            onClick={() =>
              purge.mutate({ id: conversation.id, hard: true }, { onSuccess: onClose })
            }
          >
            {cs.chat.word.purge}
          </Button>
        </>
      }
    >
      <p className="text-sm text-pretty">{cs.chat.purgeBody}</p>
      <label className="mt-4 block">
        <span className="mb-1.5 block text-sm font-semibold">{cs.chat.deleteConfirmPrompt}</span>
        <Input value={typed} autoFocus onChange={(e) => setTyped(e.target.value)} />
      </label>
    </ResponsiveModal>
  )
}

/** daysUntil renders the koš countdown with the Czech plural. */
function daysUntil(iso: string): string {
  const ms = new Date(iso).getTime() - Date.now()
  const days = Math.max(0, Math.ceil(ms / 86_400_000))
  return count(days, PLURAL.days)
}

function NewConversationDialog({
  open,
  onOpenChange,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const [name, setName] = useState('')
  const create = useCreateConversation()

  const submit = () => {
    const trimmed = name.trim()
    if (!trimmed) return
    create.mutate(
      { name: trimmed },
      {
        onSuccess: () => {
          setName('')
          onOpenChange(false)
        },
      },
    )
  }

  return (
    <ResponsiveModal
      open={open}
      onOpenChange={onOpenChange}
      title={cs.chat.word.newConversation}
      footer={
        <>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            {cs.chat.cancel}
          </Button>
          <Button variant="primary" loading={create.isPending} onClick={submit} disabled={!name.trim()}>
            {cs.chat.create}
          </Button>
        </>
      }
    >
      <label className="block">
        <span className="mb-1.5 block text-sm font-semibold">{cs.chat.word.conversation}</span>
        <Input
          value={name}
          maxLength={80}
          autoFocus
          onChange={(e) => setName(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') submit()
          }}
        />
      </label>
      <p className="mt-3 text-sm text-muted text-pretty">
        {/* Members are added afterwards, from the panel, so the create dialog stays
            one field — and so the floor sentence has one place to live rather than
            two. */}
        {cs.chat.directoryHint}
      </p>
    </ResponsiveModal>
  )
}
