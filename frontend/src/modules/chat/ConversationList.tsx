import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { ChevronDown, ChevronRight, MessageSquare, Plus, Search } from 'lucide-react'
import { cn } from '@/lib/utils'
import { cs } from '@/i18n/cs'
import { count, PLURAL } from '@/i18n/plural'
import { fmtStorageBytes } from '@/i18n/format'
import { Button, Spinner } from '@/components/ui/ui'
import { ResponsiveModal } from '@/components/ui/modal'
import { Input } from '@/components/ui/ui'
import { useAuth } from '@/app/auth'
import { DirectoryPicker } from './DirectoryPicker'
import { fmtWhen } from './when'
import {
  useChatSearch,
  useConversations,
  useCreateConversation,
  useDeleteConversation,
  useDirectory,
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
 * selected row carries three cues, not one: an accent outline, an accent-soft
 * surface, and `aria-current`.
 *
 * ⚠ AND EVERY ROW CARRIES ITS WEIGHT. The design puts the room's size on the row
 * beside the *Nad limitem* flag, because this list is the only place a member finds
 * out WHICH of their rooms is heavy — the module warning says the household is over,
 * never where.
 */
export function ConversationList({ activeID }: { activeID?: string }) {
  const active = useConversations('active')
  const [creating, setCreating] = useState(false)
  const [query, setQuery] = useState('')
  // ⚠ THE KOŠ IS FETCHED ONLY WHEN IT IS OPENED. It used to be a second request on
  // every mount and on every chatAll invalidation — create, rename, delete,
  // restore, remove-member and every membership frame — for a section that renders
  // only when it is non-empty, in the module whose whole live-sync design is
  // justified by counting requests. The design draws the section open; the header
  // is the disclosure, so the section is still always there to be found.
  const [trashOpen, setTrashOpen] = useState(false)
  const trashed = useConversations('trash', trashOpen)
  const moreActive = useLoadMoreConversations('active')
  const moreTrashed = useLoadMoreConversations('trash')

  const searching = query.trim().length > 0

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex-none px-3 pb-2.5 pt-3 lg:px-3.5">
        <div className="mb-2.5 flex items-center gap-2.5">
          <div className="min-w-0 flex-1">
            <h2 className="truncate text-[17px] font-extrabold tracking-tight">
              {cs.chat.listHeading}
            </h2>
            <p className="truncate text-[11.5px] text-muted">{cs.chat.listSubtitle}</p>
          </div>
          {/* The design's one accent affordance in this pane: 34 px at the desk,
              44 px under a thumb. */}
          <button
            type="button"
            onClick={() => setCreating(true)}
            title={cs.chat.word.newConversation}
            aria-label={cs.chat.word.newConversation}
            className="grid h-11 w-11 flex-none place-items-center rounded-[12px] bg-accent text-accent-fg hover:opacity-90 lg:h-[34px] lg:w-[34px] lg:rounded-[9px]"
          >
            <Plus size={18} aria-hidden />
          </button>
        </div>

        {/* ⚠ SEARCH LIVES HERE, over the whole list, because a hit spans conversations
            — the server's one MATCH carries the membership join and the per-row floor
            precisely so it can (D251). Scoping the box to the open thread would throw
            that away. */}
        <div className="relative">
          <Search
            size={14}
            aria-hidden
            className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-subtle"
          />
          <Input
            value={query}
            type="search"
            className="h-10 rounded-[10px] border-border bg-s2 pl-9 text-[13px] lg:h-9 lg:text-[12.5px]"
            placeholder={cs.chat.searchPlaceholder}
            aria-label={cs.chat.searchPlaceholder}
            onChange={(e) => setQuery(e.target.value)}
          />
        </div>
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto om-scroll px-2 pb-4">
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
              <div className="px-3 py-8 text-center">
                <div className="mb-1.5 text-sm font-bold">{cs.chat.emptyTitle}</div>
                <p className="text-sm text-muted text-pretty">{cs.chat.emptyBody}</p>
              </div>
            )}

            <ul className="flex flex-col gap-1 pt-1">
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
              <div className="px-1 pt-2">
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
            <div className="mt-3 px-1">
              <button
                type="button"
                onClick={() => setTrashOpen((v) => !v)}
                aria-expanded={trashOpen}
                className="flex w-full items-center gap-2 py-1.5 text-left"
              >
                {trashOpen ? (
                  <ChevronDown size={12} aria-hidden className="flex-none text-subtle" />
                ) : (
                  <ChevronRight size={12} aria-hidden className="flex-none text-subtle" />
                )}
                <span className="font-mono text-[10px] uppercase tracking-[0.06em] text-subtle">
                  {cs.chat.trashSectionTitle}
                </span>
                <span className="h-px flex-1 bg-border" />
              </button>
              {trashOpen && (
                <div className="flex flex-col gap-2 pb-1 pt-1.5">
                  {trashed.isPending && (
                    <div className="grid place-items-center py-4 text-muted">
                      <Spinner />
                    </div>
                  )}
                  {trashed.data?.items.length === 0 && (
                    <p className="px-1 py-1 text-xs text-muted">{cs.chat.trashEmpty}</p>
                  )}
                  {trashed.data?.items.map((c) => (
                    <TrashedRow key={c.id} conversation={c} />
                  ))}
                  {trashed.data?.next_cursor && (
                    <Button
                      size="sm"
                      variant="ghost"
                      className="w-full"
                      loading={moreTrashed.isPending}
                      onClick={() => moreTrashed.mutate()}
                    >
                      {cs.chat.loadMore}
                    </Button>
                  )}
                  {/* ⚠ THE RELATIONSHIP, NOT JUST THE COUNTDOWN (D254). Its bytes go
                      on counting toward both thresholds until something really
                      purges them, which is honest and looks wrong — so the section
                      says why, and names the verb that ends it. */}
                  {!!trashed.data?.items.length && (
                    <p className="px-1 text-[10.5px] leading-relaxed text-subtle text-pretty">
                      {cs.chat.trashNote}
                    </p>
                  )}
                </div>
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
    return <p className="px-3 py-8 text-center text-sm text-muted">{cs.chat.searchEmpty}</p>
  }
  return (
    <>
      <ul className="flex flex-col gap-1 pt-1">
        {hits.data.items.map((h) => (
          <li key={h.message_id}>
            <Link
              to={`/chat/${h.conversation_id}`}
              className="block rounded-[12px] border border-transparent px-3 py-2.5 text-muted hover:border-border hover:bg-s2 hover:text-fg"
            >
              <span className="flex items-baseline gap-2">
                <span className="min-w-0 flex-1 truncate text-xs font-bold text-fg">
                  {h.conversation_name}
                </span>
                <span className="flex-none font-mono text-[10.5px] text-subtle">
                  {h.author_label}
                </span>
              </span>
              <span className="mt-0.5 block break-words text-sm">{h.snippet}</span>
            </Link>
          </li>
        ))}
      </ul>
      <p className="px-3 pb-4 pt-3 text-xs text-muted text-pretty">{cs.chat.searchSinglePage}</p>
    </>
  )
}

function ConversationRow({
  conversation,
  selected,
}: {
  conversation: Conversation
  selected: boolean
}) {
  const unread = conversation.unread_count
  return (
    <Link
      to={`/chat/${conversation.id}`}
      aria-current={selected ? 'page' : undefined}
      className={cn(
        'flex min-h-16 items-start gap-2.5 rounded-[13px] border px-3 py-2.5 transition-colors lg:min-h-0 lg:rounded-[12px]',
        selected
          ? 'border-accent bg-accent-soft text-fg'
          : 'border-border bg-s1 text-fg hover:bg-s2 lg:border-transparent lg:bg-transparent',
      )}
    >
      {/* The room's tile. Not an avatar of a person — a conversation is a room, and
          every row gets the same mark so the column reads as one list. */}
      <span className="grid h-[38px] w-[38px] flex-none place-items-center rounded-[11px] bg-s3 text-muted lg:h-[34px] lg:w-[34px] lg:rounded-[10px]">
        <MessageSquare size={15} aria-hidden />
      </span>

      <span className="min-w-0 flex-1">
        <span className="flex items-center gap-2">
          <span
            className={cn(
              'min-w-0 flex-1 truncate text-[13.5px]',
              unread > 0 ? 'font-extrabold' : 'font-bold',
            )}
          >
            {conversation.name}
          </span>
          <span className="flex-none font-mono text-[10.5px] text-subtle">
            {fmtWhen(conversation.updated_at)}
          </span>
        </span>

        <span className="mt-0.5 block truncate text-[11.5px] text-muted">
          {count(conversation.member_count, PLURAL.members)}
        </span>

        <span className="mt-1.5 flex flex-wrap items-center gap-x-2 gap-y-1">
          {/* ⚠ *nezměřeno* rather than a zero it did not measure (D193/D161): the
              row prints what the server answered, and `null` is not `0 B`. */}
          <span className="font-mono text-[10.5px] tabular-nums text-subtle">
            {fmtStorageBytes(conversation.bytes)}
          </span>
          {/* ⚠ THE FLAG IS THE POINT, NOT THE SIZE. HANDOFF-design §v10 lists "one
              room over its limit" as a state of this list, and it is the only place
              a member finds out WHICH of their rooms is heavy. The mark reuses
              `--attention` (informational: nothing is blocked and nobody did
              anything wrong) and carries a word, never colour alone.

              ⚠ Rendered only when the verdict is a real `true`. It is a pointer for
              the D161 reason: a verdict about an unmeasured figure cannot be more
              certain than the figure, so `null` renders nothing rather than "under
              the limit". */}
          {conversation.over_conversation_limit === true && (
            <span className="flex-none whitespace-nowrap rounded-full border border-attention bg-attention-soft px-1.5 py-px text-[9.5px] font-bold leading-[1.15] text-attention">
              {cs.chat.cleanupOverLimit}
            </span>
          )}
          {/* ⚠ Všichni says what it is on the row. Its membership IS the household,
              which is also why no floor line ever appears inside it (D258) — the
              absence reads as a decision once the row has said so. */}
          {conversation.kind === 'default' && (
            <span className="font-mono text-[9.5px] uppercase tracking-[0.05em] text-subtle">
              {cs.chat.everyoneMark}
            </span>
          )}
        </span>
      </span>

      {unread > 0 && (
        <span
          // ⚠ ACCENT, NOT A WARNING COLOUR, and mono tabular so 3 and 40 do not
          // shift the row. An unread count is a reason to open something, not an
          // alarm about it.
          className="flex-none rounded-full bg-accent px-1.5 py-0.5 text-center font-mono text-[11px] font-bold tabular-nums text-accent-fg"
          style={{ minWidth: 20 }}
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
 * (D254), which is honest and looks wrong — so the size is on the card, the
 * countdown is stated rather than left to be inferred, and *Smazat natrvalo* is what
 * stops it trapping anyone.
 *
 * ⚠ AND IT IS DRAWN AS AN ABSENCE. A dashed outline with no surface under it, the
 * name in `--muted`: this room is not one of the rooms above, and the card says so
 * before the buttons do.
 */
function TrashedRow({ conversation }: { conversation: Conversation }) {
  const restore = useRestoreConversation()
  const [purging, setPurging] = useState(false)
  return (
    <div className="rounded-[13px] border border-dashed border-border-strong px-3 py-2.5">
      <div className="flex items-baseline gap-2">
        <span className="min-w-0 flex-1 truncate text-[13px] font-bold text-muted">
          {conversation.name}
        </span>
        <span className="flex-none font-mono text-[11.5px] tabular-nums text-muted">
          {fmtStorageBytes(conversation.bytes)}
        </span>
      </div>
      {/* ⚠ THE COUNTDOWN ALONE, WITHOUT THE NAME the design's mock shows beside it.
          `deleted_by` is a column on the row and is not on the wire — the
          `Conversation` shape carries `deleted_at` and `purge_after` and nothing
          about who — so the line says the part that is true rather than a "Smazal"
          with nobody after it. */}
      {conversation.purge_after && (
        <div className="mt-1 text-[11px] text-subtle">
          {daysUntil(conversation.purge_after)} {cs.chat.trashUntilPurge}
        </div>
      )}
      <div className="mt-2.5 flex gap-2">
        <Button
          size="sm"
          variant="secondary"
          className="min-h-11 flex-1 lg:min-h-0"
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
        <Button
          size="sm"
          variant="ghost"
          className="min-h-11 flex-1 border-border lg:min-h-0"
          onClick={() => setPurging(true)}
        >
          {cs.chat.word.purge}
        </Button>
      </div>
      <PurgeDialog conversation={conversation} open={purging} onClose={() => setPurging(false)} />
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

/** Ties the picker's visible heading to the group of chips it names. */
const CREATE_MEMBERS_LABEL = 'chat-create-members-label'

/**
 * Nová konverzace — a name and the people it is for.
 *
 * ⚠ THE MEMBERS ARE PICKED HERE AND NOT ONLY AFTERWARDS. The dialog used to be one
 * field followed by `directoryHint`, a sentence about who is in a list that was not
 * on the screen — so it read as a picker that had failed to load, and creating a
 * group meant creating an empty room and then going to find Členové. The founding
 * members go in the create call, where the server gives every one of them the
 * conversation's own beginning as their floor: nobody founds a room with history
 * behind them, and there is no history yet anyway.
 *
 * ⚠ IT STAYS OPTIONAL. A room created for nobody is a legitimate thing to make —
 * a note to self, or a group whose people are added as they log in for the first
 * time — so an empty selection creates the conversation rather than blocking it.
 */
function NewConversationDialog({
  open,
  onOpenChange,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const [name, setName] = useState('')
  const [picked, setPicked] = useState<string[]>([])
  const directory = useDirectory(open)
  const { identity } = useAuth()
  const me = identity?.userId ?? ''
  const create = useCreateConversation()

  // ⚠ THE DIALOG IS EMPTIED WHEN IT CLOSES, NOT WHEN IT SUCCEEDS. This component is
  // mounted for the life of the pane — only the modal's CHILDREN unmount — so state
  // cleared in `onSuccess` alone survived every other way out: cancel, Esc, a click
  // on the overlay. A name left behind is visible on the next open and merely untidy;
  // a SELECTION left behind is not, because pressing Vytvořit founds the new room
  // around somebody the member picked, thought better of, and never saw again.
  useEffect(() => {
    if (!open) {
      setName('')
      setPicked([])
    }
  }, [open])

  // ⚠ THE CREATOR IS NOT OFFERED. CreateConversation joins them itself, so a toggle
  // for the one member who cannot be left out is a control that does nothing.
  const addable = (directory.data?.items ?? []).filter((d) => d.user_id !== me)

  const toggle = (id: string) =>
    setPicked((prev) => (prev.includes(id) ? prev.filter((p) => p !== id) : [...prev, id]))

  const submit = () => {
    const trimmed = name.trim()
    if (!trimmed) return
    create.mutate({ name: trimmed, member_ids: picked }, { onSuccess: () => onOpenChange(false) })
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
      <div className="mt-4">
        <span id={CREATE_MEMBERS_LABEL} className="mb-1.5 block text-sm font-semibold">
          {cs.chat.createMembers}
        </span>
        <DirectoryPicker
          directory={directory}
          addable={addable}
          label={cs.chat.createMembers}
          labelledBy={CREATE_MEMBERS_LABEL}
          renderChip={(d) => {
            const on = picked.includes(d.user_id)
            return (
              <Button
                key={d.user_id}
                size="sm"
                // ⚠ SELECTION IS NOT CARRIED BY COLOUR ALONE — `aria-pressed` states
                // it, which is what a screen reader and a CVD reader both get.
                variant={on ? 'primary' : 'secondary'}
                aria-pressed={on}
                className="min-h-11 lg:min-h-8"
                onClick={() => toggle(d.user_id)}
              >
                {d.display_name}
              </Button>
            )
          }}
        />
        {/* The directory is a LOGIN HISTORY projected from sessions — Home has no
            user table — so somebody who has never logged in is simply not in this
            list. The note says so rather than letting the gap look like a bug. */}
        <p className="mt-2.5 text-[11.5px] leading-relaxed text-muted text-pretty">
          {cs.chat.directoryHint}
        </p>
      </div>
    </ResponsiveModal>
  )
}
