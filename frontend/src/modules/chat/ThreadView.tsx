import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { ArrowLeft, BellOff, CornerUpLeft, MoreHorizontal, Users } from 'lucide-react'
import { cn } from '@/lib/utils'
import { cs } from '@/i18n/cs'
import { count, PLURAL } from '@/i18n/plural'
import { Button, Input, Spinner, Textarea } from '@/components/ui/ui'
import { ResponsiveModal } from '@/components/ui/modal'
import { useAuth } from '@/app/auth'
import {
  useAdvanceRead,
  useConversation,
  useDeleteConversation,
  useDeleteMessage,
  useEditMessage,
  useLoadOlderMessages,
  useMessages,
  useRenameConversation,
  useSendMessage,
} from './api/hooks'
import type { ChatMessage, Conversation, MessageQuote } from './api/types'

/**
 * One conversation's thread.
 *
 * ⚠ THE FLOOR IS MADE LEGIBLE IN EXACTLY THREE PLACES AND NOWHERE ELSE (D218):
 * a quiet permanent line at the top of a thread for a member who joined later, the
 * empty-quote shape below, and `effective_from` per member in the members panel
 * (plus the removal dialog's gap sentence). A thread that simply STARTS somewhere
 * with no explanation reads as data loss; one that apologises on every screen reads
 * as broken.
 *
 * ⚠ AND NEVER IN VŠICHNI. The household room gives every member its whole history
 * because nobody decided to add them (D258) — showing the line there has misread the
 * decision, not merely over-explained.
 */
export function ThreadView({ conversationID, onOpenMembers }: {
  conversationID: string
  onOpenMembers: () => void
}) {
  const conversation = useConversation(conversationID)
  const thread = useMessages(conversationID)
  const older = useLoadOlderMessages(conversationID)
  const advanceRead = useAdvanceRead(conversationID)
  const { identity } = useAuth()
  const me = identity?.userId ?? ''

  // The thread is stored newest-first and rendered oldest-first, so the reversal
  // happens once, here, rather than in every consumer.
  const messages = useMemo(() => [...(thread.data?.items ?? [])].reverse(), [thread.data])
  const newest = thread.data?.items[0]

  /**
   * ⚠ A THREAD OPENS AT ITS NEWEST MESSAGE, AND NOTHING MADE IT (v10 review). The
   * list renders oldest-first into a scroll box that starts at the top, so a room
   * with more than a screenful opened on the OLDEST message of the loaded page — a
   * 62-message room opened at message 13 — and every message that then arrived
   * landed below the fold while the read marker cleared its badge. A member watched
   * a conversation they could not see being marked read.
   *
   * ⚠ AND IT ONLY FOLLOWS SOMEBODY ALREADY AT THE BOTTOM. Yanking the view down
   * while they are reading history is the other half of the same bug; `atBottom` is
   * what separates "keep up with the conversation" from "interrupt me".
   */
  const scrollRef = useRef<HTMLDivElement>(null)
  const atBottom = useRef(true)
  const onScroll = () => {
    const el = scrollRef.current
    if (!el) return
    // A slack of one line, so a fractional scrollTop or a rounding difference does
    // not read as "they have scrolled away".
    atBottom.current = el.scrollHeight - el.scrollTop - el.clientHeight < 24
  }
  useEffect(() => {
    const el = scrollRef.current
    if (!el || !newest || !atBottom.current) return
    el.scrollTop = el.scrollHeight
    // The trigger is the IDENTITY of the newest message, not the object: an edit or
    // a tombstone rewrites `newest` without extending the thread, and following that
    // down would move the view for a change that happened where they already are.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [newest?.id, conversationID])

  /**
   * Loading older messages PREPENDS above the viewport, so without an anchor the
   * content under the member's eyes jumps down by the height of the page they just
   * asked for — they press *Načíst starší* and lose their place, which is the one
   * thing the button exists to help with.
   *
   * ⚠ THE ANCHOR IS DISTANCE FROM THE BOTTOM, RESTORED IN A LAYOUT EFFECT. Measuring
   * the height delta in the mutation's callback does not work: React has not
   * committed the taller list yet, so `scrollHeight` still reads the old value and
   * the correction is zero. Distance-from-bottom is invariant under a prepend, and
   * useLayoutEffect runs after the DOM grows and before the browser paints, so the
   * restore is never visible as a jump.
   */
  const anchor = useRef<number | null>(null)
  const oldest = messages[0]?.id
  const loadOlder = () => {
    const el = scrollRef.current
    if (el) anchor.current = el.scrollHeight - el.scrollTop
    older.mutate()
  }
  useLayoutEffect(() => {
    const el = scrollRef.current
    if (!el || anchor.current === null) return
    el.scrollTop = el.scrollHeight - anchor.current
    anchor.current = null
  }, [oldest])

  // Advancing the read marker is idempotent and never moves backwards (D250), so
  // firing it whenever the newest message changes is safe — a replayed older marker
  // could not un-read the room even if this raced.
  const marked = useRef<string>('')
  useEffect(() => {
    if (!newest || marked.current === newest.id) return
    marked.current = newest.id
    advanceRead.mutate(newest.id)
    // advanceRead is a stable mutation object; including it would re-fire on every
    // render of a busy thread.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [newest?.id])

  const [replyTo, setReplyTo] = useState<ChatMessage | null>(null)
  const [editing, setEditing] = useState<ChatMessage | null>(null)

  if (conversation.isPending || thread.isPending) {
    return (
      <div className="grid h-full place-items-center text-muted">
        <Spinner />
      </div>
    )
  }
  if (conversation.isError) {
    return (
      <div className="grid h-full place-items-center p-6 text-center">
        <p className="max-w-sm text-sm text-muted text-pretty">{cs.chat.notFound}</p>
      </div>
    )
  }

  const room = conversation.data as Conversation

  return (
    <div className="flex h-full min-h-0 flex-col">
      <ThreadHeader conversation={room} onOpenMembers={onOpenMembers} />

      <div
        ref={scrollRef}
        onScroll={onScroll}
        className="min-h-0 flex-1 overflow-y-auto om-scroll px-4 py-4"
      >
        {/* ⚠ ABOVE the floor line, and only when there IS more. The two say
            different things and the order matters: "there is older history you can
            load" sits above "and beyond that, history that is not yours". A thread
            that showed only the floor line while silently holding back page two
            explained the truncation as the membership floor. */}
        {thread.data?.has_more && (
          <div className="mb-4 flex justify-center">
            <Button size="sm" variant="secondary" loading={older.isPending} onClick={loadOlder}>
              {cs.chat.loadOlder}
            </Button>
          </div>
        )}

        <FloorLine conversation={room} />

        {messages.length === 0 && (
          <div className="grid min-h-[200px] place-items-center text-center">
            <div>
              <div className="mb-1 text-sm font-bold">{cs.chat.threadEmpty}</div>
              <p className="text-sm text-muted">{cs.chat.threadEmptyHint}</p>
            </div>
          </div>
        )}

        <ul className="flex flex-col gap-2">
          {messages.map((m) => (
            <li key={m.id}>
              <Bubble
                message={m}
                mine={m.author_id === me}
                conversationID={conversationID}
                onReply={() => setReplyTo(m)}
                onEdit={() => setEditing(m)}
              />
            </li>
          ))}
        </ul>
      </div>

      <Composer
        conversationID={conversationID}
        replyTo={replyTo}
        editing={editing}
        onClearReply={() => setReplyTo(null)}
        onClearEdit={() => setEditing(null)}
      />
    </div>
  )
}

function ThreadHeader({
  conversation,
  onOpenMembers,
}: {
  conversation: Conversation
  onOpenMembers: () => void
}) {
  const [menuOpen, setMenuOpen] = useState(false)
  const [renaming, setRenaming] = useState(false)
  const [deleting, setDeleting] = useState(false)
  // ⚠ Všichni is renameable and NOT deletable (D219) — the same asymmetry the
  // service enforces with a 422. Hiding the entry is not the guard; it is what
  // stops a member meeting the guard as an error message.
  const isDefault = conversation.kind === 'default'

  return (
    <header className="flex items-center gap-2 border-b border-border px-3 py-2.5 lg:px-4">
      {/* Below 1024 the thread is a route push, so back returns to the list. The
          link is hidden on desktop, where both panes are on screen at once. */}
      <Link
        to="/chat"
        className="grid h-9 w-9 flex-none place-items-center rounded-md text-muted hover:bg-s2 hover:text-fg lg:hidden"
        aria-label={cs.chat.listTitle}
      >
        <ArrowLeft size={18} aria-hidden />
      </Link>
      <div className="min-w-0 flex-1">
        <h2 className="truncate text-base font-bold tracking-tight">{conversation.name}</h2>
        {/* The declined noun, not the section label: `count` is what the
            conversation list uses for this same number one file away. */}
        <p className="truncate text-xs text-muted">
          {count(conversation.member_count, PLURAL.members)}
        </p>
      </div>
      {conversation.muted && (
        <span className="flex-none text-muted" title={cs.chat.word.mute}>
          <BellOff size={16} aria-hidden />
          <span className="sr-only">{cs.chat.word.mute}</span>
        </span>
      )}
      <Button size="sm" variant="ghost" onClick={onOpenMembers}>
        <Users size={14} aria-hidden />
        <span className="hidden sm:inline">{cs.chat.word.members}</span>
      </Button>

      {/* ⚠ THE ROOM'S OWN VERBS. Without them a conversation created by mistake was
          permanent from the UI: nothing anywhere could rename or trash one, so the
          koš section below could only ever be empty and its Obnovit button could
          only ever be dead. */}
      <div className="relative flex-none">
        <button
          type="button"
          onClick={() => setMenuOpen((v) => !v)}
          aria-label={cs.chat.word.conversation}
          aria-expanded={menuOpen}
          className="grid h-8 w-8 place-items-center rounded-md text-muted hover:bg-s2 hover:text-fg"
        >
          <MoreHorizontal size={16} aria-hidden />
        </button>
        {menuOpen && (
          <div className="absolute right-0 z-10 mt-1 w-52 rounded-md border border-border bg-s1 p-1 shadow-[var(--shadow)]">
            <button
              type="button"
              className="block w-full rounded px-2 py-1.5 text-left text-sm hover:bg-s2"
              onClick={() => {
                setMenuOpen(false)
                setRenaming(true)
              }}
            >
              {cs.chat.word.rename}
            </button>
            {!isDefault && (
              <button
                type="button"
                className="block w-full rounded px-2 py-1.5 text-left text-sm text-danger hover:bg-danger/10"
                onClick={() => {
                  setMenuOpen(false)
                  setDeleting(true)
                }}
              >
                {cs.chat.word.deleteConversation}
              </button>
            )}
          </div>
        )}
      </div>

      <RenameDialog conversation={conversation} open={renaming} onClose={() => setRenaming(false)} />
      <DeleteDialog conversation={conversation} open={deleting} onClose={() => setDeleting(false)} />
    </header>
  )
}

function RenameDialog({
  conversation,
  open,
  onClose,
}: {
  conversation: Conversation
  open: boolean
  onClose: () => void
}) {
  const [name, setName] = useState(conversation.name)
  const rename = useRenameConversation(conversation.id)

  // Re-seed each time it opens, so a cancelled edit does not persist into the next.
  useEffect(() => {
    if (open) setName(conversation.name)
  }, [open, conversation.name])

  const submit = () => {
    const trimmed = name.trim()
    if (!trimmed) return
    rename.mutate(trimmed, { onSuccess: onClose })
  }

  return (
    <ResponsiveModal
      open={open}
      onOpenChange={(o) => !o && onClose()}
      title={cs.chat.word.rename}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {cs.chat.cancel}
          </Button>
          <Button variant="primary" loading={rename.isPending} onClick={submit} disabled={!name.trim()}>
            {cs.chat.save}
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
    </ResponsiveModal>
  )
}

/**
 * The delete confirmation.
 *
 * ⚠ THE NAME HAS TO BE TYPED, because any member may delete a room holding
 * everybody else's messages and files (D253) — the koš is what makes that
 * survivable, and the typing is what makes it deliberate.
 *
 * ⚠ AND BOTH DOORS ARE HERE, side by side and labelled with what each costs.
 * *Smazat natrvalo* exists so somebody deleting a heavy conversation TO FREE SPACE
 * is never told to come back in seven days.
 */
function DeleteDialog({
  conversation,
  open,
  onClose,
}: {
  conversation: Conversation
  open: boolean
  onClose: () => void
}) {
  const [typed, setTyped] = useState('')
  const remove = useDeleteConversation()
  const navigate = useNavigate()

  useEffect(() => {
    if (open) setTyped('')
  }, [open])

  const confirmed = typed.trim() === conversation.name
  const go = (hard: boolean) =>
    remove.mutate(
      { id: conversation.id, hard },
      {
        onSuccess: () => {
          onClose()
          // The room is gone from every read the moment this commits, so staying on
          // /chat/{id} would render its own 404.
          navigate('/chat')
        },
      },
    )

  return (
    <ResponsiveModal
      open={open}
      onOpenChange={(o) => !o && onClose()}
      title={cs.chat.deleteTitle}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {cs.chat.cancel}
          </Button>
          <Button variant="secondary" loading={remove.isPending} disabled={!confirmed} onClick={() => go(true)}>
            {cs.chat.word.purge}
          </Button>
          <Button variant="danger" loading={remove.isPending} disabled={!confirmed} onClick={() => go(false)}>
            {cs.chat.confirmDelete}
          </Button>
        </>
      }
    >
      <p className="text-sm text-pretty">{cs.chat.deleteBody}</p>
      <p className="mt-2 text-sm text-muted text-pretty">{cs.chat.purgeBody}</p>
      <label className="mt-4 block">
        <span className="mb-1.5 block text-sm font-semibold">{cs.chat.deleteConfirmPrompt}</span>
        <Input value={typed} autoFocus onChange={(e) => setTyped(e.target.value)} />
      </label>
    </ResponsiveModal>
  )
}

/**
 * The floor line — the hardest sentence in v10.
 *
 * ⚠ PERMANENT, NOT DISMISSIBLE, and never in Všichni. It is the only place the
 * feature gets to explain itself, and it has to be TRUE rather than reassuring: the
 * history genuinely is not coming back.
 *
 * The `kind` test here is a RENDERING decision about one line of copy, not a
 * history branch — the server has already bounded the thread by the floor, and this
 * component could not widen it if it tried.
 */
function FloorLine({ conversation }: { conversation: Conversation }) {
  if (conversation.kind === 'default') return null
  // A member whose floor is the conversation's own start has no missing history to
  // explain: they were there from the beginning.
  if (conversation.effective_from <= conversation.created_at) return null
  return (
    <p className="mb-4 border-b border-border pb-3 text-center text-xs text-muted text-pretty">
      {cs.chat.floorLine}{' '}
      <span className="whitespace-nowrap">
        {cs.chat.floorLineFrom} {formatDate(conversation.effective_from)}.
      </span>
    </p>
  )
}

function Bubble({
  message,
  mine,
  conversationID,
  onReply,
  onEdit,
}: {
  message: ChatMessage
  mine: boolean
  conversationID: string
  onReply: () => void
  onEdit: () => void
}) {
  const remove = useDeleteMessage(conversationID)
  const [menuOpen, setMenuOpen] = useState(false)

  if (message.deleted) {
    return (
      <div className={cn('flex', mine ? 'justify-end' : 'justify-start')}>
        <div className="max-w-[min(80%,44ch)] rounded-lg border border-dashed border-border px-3 py-2 text-sm italic text-muted">
          {cs.chat.word.deleted}
        </div>
      </div>
    )
  }

  return (
    <div className={cn('group flex items-end gap-2', mine ? 'justify-end' : 'justify-start')}>
      <div
        className={cn(
          'max-w-[min(80%,52ch)] rounded-lg px-3 py-2',
          // ⚠ COLOUR ONLY REINFORCES. The two bubble tints are measured at 1.55:1
          // dark / 1.16:1 light against each other — deliberately below 3:1 — so
          // ALIGNMENT and the author label are what actually carry own-versus-others.
          mine ? 'bg-bub-mine text-fg' : 'bg-bub-theirs text-fg',
        )}
      >
        {!mine && (
          <div className="mb-0.5 text-xs font-bold text-bub-label">{message.author_label}</div>
        )}
        {message.reply_to && <Quote quote={message.reply_to} />}
        <div className="whitespace-pre-wrap break-words text-sm">{message.body}</div>
        <div className="mt-1 flex items-center gap-1.5 text-[11px] text-muted">
          <time dateTime={message.created_at}>{formatTime(message.created_at)}</time>
          {message.edited_at && <span>· {cs.chat.word.edited}</span>}
        </div>
      </div>

      <div className="flex flex-none items-center gap-0.5 opacity-0 transition-opacity focus-within:opacity-100 group-hover:opacity-100">
        <button
          type="button"
          onClick={onReply}
          aria-label={cs.chat.word.reply}
          className="grid h-7 w-7 place-items-center rounded-md text-muted hover:bg-s2 hover:text-fg"
        >
          <CornerUpLeft size={14} aria-hidden />
        </button>
        {mine && (
          <div className="relative">
            <button
              type="button"
              onClick={() => setMenuOpen((v) => !v)}
              aria-label={cs.chat.word.edit}
              aria-expanded={menuOpen}
              className="grid h-7 w-7 place-items-center rounded-md text-muted hover:bg-s2 hover:text-fg"
            >
              <MoreHorizontal size={14} aria-hidden />
            </button>
            {menuOpen && (
              <div className="absolute right-0 z-10 mt-1 w-44 rounded-md border border-border bg-s1 p-1 shadow-[var(--shadow)]">
                <button
                  type="button"
                  className="block w-full rounded px-2 py-1.5 text-left text-sm hover:bg-s2"
                  onClick={() => {
                    setMenuOpen(false)
                    onEdit()
                  }}
                >
                  {cs.chat.word.edit}
                </button>
                <button
                  type="button"
                  className="block w-full rounded px-2 py-1.5 text-left text-sm text-danger hover:bg-danger/10"
                  onClick={() => {
                    setMenuOpen(false)
                    remove.mutate(message.id)
                  }}
                >
                  {cs.chat.word.deleteMessage}
                </button>
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  )
}

/**
 * A reply's quoted parent.
 *
 * ⚠ WHEN IT IS ABOVE THE CALLER'S FLOOR THE SHAPE IS EMPTY — no author, no date, no
 * excerpt (D226). It is drawn as something that is CLEARLY a quote and CLEARLY empty
 * so it does not read as a failed load, and the sentence says what it is rather than
 * leaving a blank.
 */
function Quote({ quote }: { quote: MessageQuote }) {
  if (!quote.available) {
    return (
      <div className="mb-1.5 border-l-2 border-border pl-2 text-xs italic text-muted">
        {cs.chat.word.outsideHistory}
      </div>
    )
  }
  return (
    <div className="mb-1.5 border-l-2 border-accent/50 pl-2 text-xs text-muted">
      <span className="font-semibold">{quote.author_label}</span>
      {': '}
      <span className={cn(quote.deleted && 'italic')}>
        {quote.deleted ? cs.chat.word.deleted : quote.excerpt}
      </span>
    </div>
  )
}

/**
 * The composer.
 *
 * ⚠ TEXT ONLY IN PR 2. Drag-and-drop, paste, the file picker, the per-file progress
 * rows and the over-cap refusal all arrive with the bytes in PR 3 — an attachment
 * button that opens a picker whose upload 404s is worse than no button.
 */
function Composer({
  conversationID,
  replyTo,
  editing,
  onClearReply,
  onClearEdit,
}: {
  conversationID: string
  replyTo: ChatMessage | null
  editing: ChatMessage | null
  onClearReply: () => void
  onClearEdit: () => void
}) {
  const [body, setBody] = useState('')
  const send = useSendMessage(conversationID)
  const edit = useEditMessage(conversationID)
  const ref = useRef<HTMLTextAreaElement>(null)

  useEffect(() => {
    if (editing) {
      setBody(editing.body)
      ref.current?.focus()
    }
  }, [editing])

  const submit = () => {
    const text = body.trim()
    if (!text) return
    if (editing) {
      edit.mutate(
        { id: editing.id, body: text },
        {
          onSuccess: () => {
            setBody('')
            onClearEdit()
          },
        },
      )
      return
    }
    send.mutate(
      { body: text, replyToID: replyTo?.id },
      {
        onSuccess: () => {
          setBody('')
          onClearReply()
        },
      },
    )
  }

  const busy = send.isPending || edit.isPending

  return (
    <div className="border-t border-border px-3 py-2.5 lg:px-4">
      {replyTo && !editing && (
        <div className="mb-2 flex items-center gap-2 rounded-md bg-s2 px-2.5 py-1.5 text-xs">
          <span className="min-w-0 flex-1 truncate text-muted">
            {cs.chat.replyingTo} <span className="font-semibold">{replyTo.author_label}</span>
          </span>
          <button type="button" onClick={onClearReply} className="flex-none text-muted hover:text-fg">
            {cs.chat.cancel}
          </button>
        </div>
      )}
      {editing && (
        <div className="mb-2 flex items-center gap-2 rounded-md bg-s2 px-2.5 py-1.5 text-xs">
          <span className="min-w-0 flex-1 truncate text-muted">{cs.chat.word.edit}</span>
          <button
            type="button"
            onClick={() => {
              setBody('')
              onClearEdit()
            }}
            className="flex-none text-muted hover:text-fg"
          >
            {cs.chat.cancelEdit}
          </button>
        </div>
      )}
      <div className="flex items-end gap-2">
        <Textarea
          ref={ref}
          rows={1}
          value={body}
          maxLength={8000}
          placeholder={cs.chat.composerPlaceholder}
          aria-label={cs.chat.composerPlaceholder}
          className="max-h-40 min-h-10 resize-y"
          onChange={(e) => setBody(e.target.value)}
          onKeyDown={(e) => {
            // Enter sends, Shift+Enter breaks the line — the shape every chat uses,
            // and the one a household will already have in their fingers.
            if (e.key === 'Enter' && !e.shiftKey) {
              e.preventDefault()
              submit()
            }
          }}
        />
        <Button variant="primary" loading={busy} onClick={submit} disabled={!body.trim()}>
          {editing ? cs.chat.save : cs.chat.send}
        </Button>
      </div>
    </div>
  )
}

// ---- formatting ----

const timeFmt = new Intl.DateTimeFormat('cs-CZ', { hour: '2-digit', minute: '2-digit' })
const dateFmt = new Intl.DateTimeFormat('cs-CZ', { day: 'numeric', month: 'long', year: 'numeric' })

function formatTime(iso: string): string {
  return timeFmt.format(new Date(iso))
}

function formatDate(iso: string): string {
  return dateFmt.format(new Date(iso))
}
