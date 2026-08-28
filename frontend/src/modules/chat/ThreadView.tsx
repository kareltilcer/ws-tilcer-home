import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { ArrowLeft, BellOff, CornerUpLeft, MoreHorizontal, Paperclip, Users, X } from 'lucide-react'
import { cn } from '@/lib/utils'
import { cs } from '@/i18n/cs'
import { count, PLURAL } from '@/i18n/plural'
import { fmtBytes, fmtDate, fmtTime } from '@/i18n/format'
import { Button, Input, Spinner, Textarea } from '@/components/ui/ui'
import { ResponsiveModal } from '@/components/ui/modal'
import { useAuth } from '@/app/auth'
import {
  useAdvanceRead,
  useChatStorage,
  useConversation,
  useDeleteConversation,
  useDeleteMessage,
  useEditMessage,
  useLoadOlderMessages,
  useMessages,
  useRenameConversation,
  useSendMessage,
  useUploadMessage,
} from './api/hooks'
import type { ChatMessage, Conversation, MessageQuote } from './api/types'
import { AttachmentView } from './AttachmentView'

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
  const remove = useDeleteMessage(conversationID)
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
  /**
   * showingNewest MEASURES THE BOX. One spelling, two consumers: the `atBottom` ref
   * that decides whether to follow a live message down, and the read marker below.
   *
   * ⚠ AN UNMOUNTED BOX IS `false`, AND THAT IS THE WHOLE POINT (v10 review). The
   * marker used to read `atBottom.current`, whose initial value is an optimistic
   * `true` — so on a cold load, where the thread query resolves while the
   * conversation query is still pending and this component is still returning the
   * spinner, the marker fired against a scroll box that did not exist yet. Nothing
   * had been on screen to be read, and `MAX(last_read_id, ?)` made it permanent.
   */
  const showingNewest = () => {
    const el = scrollRef.current
    if (!el) return false
    // A slack of one line, so a fractional scrollTop or a rounding difference does
    // not read as "they have scrolled away".
    return el.scrollHeight - el.scrollTop - el.clientHeight < 24
  }
  useEffect(() => {
    const el = scrollRef.current
    if (!el || !newest || !atBottom.current) return
    el.scrollTop = el.scrollHeight
    // The trigger is the IDENTITY of the newest message, not the object: an edit or
    // a tombstone rewrites `newest` without extending the thread, and following that
    // down would move the view for a change that happened where they already are.
    //
    // ⚠ AND `conversation.data` IS A DEP FOR THE REASON THE CATCH-UP BELOW HAS ONE
    // (v10 review). Both queries start together and either may land first. When the
    // THREAD lands first this effect runs while the component is still returning the
    // spinner — `conversation.isPending` — so the box does not exist and it bails on
    // `!el`; the newest id never changes again, so it never re-ran, and a 62-message
    // room opened on message 13 with everything after it below the fold. The box
    // appears on the commit where the LAST of the two queries resolves, so that is
    // what has to be depended on. A re-run with the member scrolled up is harmless:
    // `atBottom` is false and nothing moves.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [newest?.id, conversationID, conversation.data])

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
    // ⚠ THE ANCHOR IS CLEARED ON SETTLE AS WELL AS ON RESTORE (v10 review). Two
    // paths reach here without `oldest` changing — the mutationFn returns null once
    // `has_more` is false, and onSuccess filters the page by id, so a page whose
    // rows are all already held moves nothing. The layout effect below then never
    // runs, the anchor stays set, and it fires on the NEXT unrelated change to the
    // oldest message: a refetch after a message delete or a gap repair, which comes
    // back at `limit = held` with a possibly different tail. The view then jumps by
    // the difference between two unrelated layouts, with no button press to explain
    // it. Whoever set it clears it.
    const held = new Set(messages.map((m) => m.id))
    older.mutate(undefined, {
      onSettled: (page) => {
        // Nothing new means `oldest` cannot change, which means the layout effect
        // below will not run and will not clear this itself.
        if (!page?.items.some((m) => !held.has(m.id))) anchor.current = null
      },
    })
  }
  useLayoutEffect(() => {
    const el = scrollRef.current
    if (!el || anchor.current === null) return
    el.scrollTop = el.scrollHeight - anchor.current
    anchor.current = null
  }, [oldest])

  /**
   * Advancing the read marker is idempotent and never moves backwards (D250), so a
   * replayed older marker could not un-read the room even if this raced.
   *
   * ⚠ BUT IT ONLY FIRES FOR A MESSAGE THAT WAS ACTUALLY ON SCREEN (v10 review).
   * Monotonic is exactly what makes the mistake permanent: mark three messages read
   * while the member is scrolled back through history — where `atBottom` is false
   * precisely so the view does NOT follow them down — and `MAX(last_read_id, ?)`
   * means no later read can ever count them again. A hidden tab is the same case
   * with the whole window as the reason. It is the failure this PR already fixed
   * once, in the shape where the thread could not scroll at all: a member watching
   * a conversation they cannot see being marked read.
   */
  const marked = useRef<string>('')
  /** The request in flight, so a burst of scroll events sends one POST, not thirty. */
  const marking = useRef<string>('')
  /**
   * ⚠ THE NEWEST ID LIVES IN A REF BECAUSE THE HANDLER IS REGISTERED ONCE (v10
   * review). It used to be captured, which is why the listener had to be torn down
   * and re-attached on every cache mutation to see a new message.
   */
  const newestID = newest?.id ?? ''
  const newestRef = useRef(newestID)
  newestRef.current = newestID

  /**
   * catchUp is the module's ONE scroll handler.
   *
   * ⚠ ONE, REGISTERED ONCE (v10 review). There were two — the `onScroll` prop and a
   * native listener — measuring the same box for two halves of the same question,
   * and the native one was re-registered on every arriving message because its
   * effect depended on `thread.data`. On a busy thread that is a removeEventListener
   * and an addEventListener per message, plus two document listeners, for a function
   * whose only changing input is one id. The id moved into a ref and the listener
   * stopped moving at all.
   *
   * ⚠ AND THE MARKER IS RECORDED ONLY ONCE THE REQUEST HAS LANDED (v10 review).
   * `marked.current = id` before the mutation meant a POST lost to a dropped
   * connection still permanently marked that id handled — both entry points
   * short-circuit on it, so neither scrolling nor returning to the tab would ever
   * retry, and the badge sat over messages plainly read until a NEWER message
   * arrived to move the id along. `marking` holds the in-flight id so the retry
   * costs nothing while it is still trying.
   */
  const catchUp = useCallback(() => {
    const onScreen = showingNewest()
    // ⚠ ONLY A MOUNTED BOX MAY ANSWER `atBottom`. An unmounted one is not "scrolled
    // away", it is not there yet — recording that as false would leave the optimistic
    // `true` overwritten before the commit where the box appears, and the auto-scroll
    // above reads exactly that flag to decide whether to follow the thread down.
    if (scrollRef.current) atBottom.current = onScreen
    const latest = newestRef.current
    if (!latest || marked.current === latest || marking.current === latest) return
    // ⚠ MEASURED, NOT ASSUMED. showingNewest() answers false while the scroll box is
    // unmounted and false while the member is scrolled back through history, which
    // are the two ways a marker can run ahead of what anybody has actually seen.
    if (!onScreen || document.visibilityState !== 'visible') return
    marking.current = latest
    advanceRead.mutate(latest, {
      onSuccess: () => {
        marked.current = latest
      },
      onError: () => {
        // Leave `marked` alone, so the next scroll or tab return tries again.
        if (marking.current === latest) marking.current = ''
      },
    })
    // advanceRead is a stable mutation object and everything else here is a ref;
    // including them would re-register the listeners this indirection removed.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  /** Returning to the tab catches the marker up. Registered once, never re-bound. */
  useEffect(() => {
    document.addEventListener('visibilitychange', catchUp)
    return () => document.removeEventListener('visibilitychange', catchUp)
  }, [catchUp])

  /**
   * ⚠ THE DATA CHANGE CALLS IT; IT NO LONGER RE-REGISTERS ANYTHING (v10 review). A
   * thread short enough to be wholly visible has no scroll to catch it up, so the
   * arrival of the data is the only moment it can be marked read.
   *
   * ⚠ WHICH IS WHY `conversation.data` IS A DEP. The box appears on the commit where
   * the LAST of the two queries resolves, and when that is the conversation rather
   * than the thread, nothing about the thread changed to re-run this. A taller
   * thread still correctly waits for the scroll that reaches its end, because
   * showingNewest() measures.
   */
  useEffect(() => {
    catchUp()
  }, [catchUp, thread.data, conversation.data])

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
  // ⚠ A THREAD THAT FAILED TO LOAD IS NOT AN EMPTY ONE (v10 review). Only the
  // CONVERSATION query was checked, so a 500 on the messages left `thread.data`
  // undefined, `messages` empty, and the render fell through to the empty state:
  // "Zatím tu nikdo nic nenapsal." over a room with two years of history, under a
  // composer that still worked, with nothing on screen to press. The retry is part
  // of the fix — the two states differ in what the member can DO about them.
  if (thread.isError) {
    return (
      <div className="grid h-full place-items-center p-6 text-center">
        <div className="max-w-sm">
          <p className="text-sm font-bold">{cs.chat.threadLoadFailed}</p>
          <p className="mt-1 text-sm text-muted text-pretty">{cs.chat.threadLoadFailedHint}</p>
          <Button
            size="sm"
            variant="secondary"
            className="mt-4"
            loading={thread.isFetching}
            onClick={() => void thread.refetch()}
          >
            {cs.chat.retry}
          </Button>
        </div>
      </div>
    )
  }

  const room = conversation.data as Conversation

  return (
    <div className="flex h-full min-h-0 flex-col">
      <ThreadHeader conversation={room} onOpenMembers={onOpenMembers} />

      <div
        ref={scrollRef}
        onScroll={catchUp}
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
                // ⚠ ONE MUTATION FOR THE THREAD, NOT ONE PER BUBBLE (v10 review).
                // Bubble called useDeleteMessage itself, so a thread at the
                // 200-message cap mounted two hundred mutation observers — for a
                // verb at most one bubble at a time can use, and on messages whose
                // menu is never even rendered. The composer's send and edit already
                // live at this level for the same reason.
                onDelete={() => remove.mutate(m.id)}
                onReply={() => setReplyTo(m)}
                // ⚠ STARTING AN EDIT DROPS A PENDING REPLY (v10 review). The composer
                // hides the reply chip while an edit is open, so a reply begun before
                // it was invisible but still armed — and once the edit finished, the
                // next ordinary message went out as a reply to a message nobody had
                // looked at in a while, with no unsend behind it.
                onEdit={() => {
                  setReplyTo(null)
                  setEditing(m)
                }}
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
  //
  // ⚠ THE SERVER ANSWERS THIS; IT IS NOT DERIVED FROM THE TWO TIMESTAMPS (v10
  // review). The floor is an id bound, and `effective_from <= created_at` was a
  // second spelling of it that disagreed — adding somebody to a room with no
  // messages yet writes `effective_from = now` over an EMPTY bound, so the clock
  // said "history withheld" and this permanent, non-dismissible line appeared over
  // history that never existed.
  if (conversation.reads_from_beginning) return null
  return (
    <p className="mb-4 border-b border-border pb-3 text-center text-xs text-muted text-pretty">
      {cs.chat.floorLine}{' '}
      <span className="whitespace-nowrap">
        {cs.chat.floorLineFrom} {fmtDate(new Date(conversation.effective_from))}.
      </span>
    </p>
  )
}

function Bubble({
  message,
  mine,
  onReply,
  onEdit,
  onDelete,
}: {
  message: ChatMessage
  mine: boolean
  onReply: () => void
  onEdit: () => void
  onDelete: () => void
}) {
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
        {/* ⚠ THE ATTACHMENTS COME BEFORE THE BODY, which is the order every chat
            uses and the order a caption reads in. An image with a caption under it
            is a photo; a caption with an image under it is a document. */}
        {message.attachments.length > 0 && (
          <div className="mb-1.5 flex flex-col gap-1.5">
            {message.attachments.map((a) => (
              <AttachmentView key={a.id} attachment={a} />
            ))}
          </div>
        )}
        {message.body && (
          <div className="whitespace-pre-wrap break-words text-sm">{message.body}</div>
        )}
        <div className="mt-1 flex items-center gap-1.5 text-[11px] text-muted">
          <time dateTime={message.created_at}>{fmtTime(message.created_at)}</time>
          {message.edited_at && <span>· {cs.chat.word.edited}</span>}
        </div>
      </div>

      {/* ⚠ THE `hover:none` ESCAPE IS LOAD-BEARING (v10 review). Tailwind v4 wraps
          every hover variant in `@media (hover: hover)`, so on a phone —
          the 375 px case this module is designed around — `group-hover:opacity-100`
          is dead CSS and these controls stayed at opacity 0 permanently. They are
          still in the layout and still clickable, so reply, edit and delete were
          invisible targets a member had to find by tapping. Where there is no hover
          to reveal them, they are simply always shown. */}
      <div className="flex flex-none items-center gap-0.5 opacity-0 transition-opacity focus-within:opacity-100 group-hover:opacity-100 [@media(hover:none)]:opacity-100">
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
                    onDelete()
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
 * The composer: drag-and-drop, paste and a picker, up to ten files (D224).
 *
 * ⚠ AN OVER-CAP FILE IS REFUSED BEFORE IT IS UPLOADED, naming the limit in MB. The
 * server enforces the same cap and answers 413 — that is the authority — but making
 * a member watch a 60 MB video upload to be told no at the end is the failure this
 * check exists to avoid, on a household connection where that is minutes.
 *
 * ⚠ AND EDITING NEVER CARRIES FILES. An edit changes a body and nothing else (D225);
 * attaching to an edit would need an attachment-add path that does not exist, so the
 * picker is hidden while editing rather than offered and refused.
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
  const [files, setFiles] = useState<File[]>([])
  const [rejected, setRejected] = useState<string[]>([])
  const [progress, setProgress] = useState<number | null>(null)
  const [dragging, setDragging] = useState(false)
  const send = useSendMessage(conversationID)
  const upload = useUploadMessage(conversationID)
  const edit = useEditMessage(conversationID)
  const ref = useRef<HTMLTextAreaElement>(null)
  const picker = useRef<HTMLInputElement>(null)
  const maxBytes = useMaxUploadBytes()

  useEffect(() => {
    if (editing) {
      setBody(editing.body)
      ref.current?.focus()
    }
  }, [editing])

  /**
   * accept applies both caps and reports what it dropped.
   *
   * ⚠ THE PARTIAL FAILURE IS THE DESIGNED CASE, not the exception: a phone photo
   * roll is what produces "nine files up, one rejected", so the nine are kept and
   * the one is named. Silently dropping it would be the same bug as silently
   * truncating a paste.
   */
  const accept = (incoming: File[]) => {
    if (incoming.length === 0) return
    const tooBig: string[] = []
    const kept: File[] = []
    for (const f of incoming) {
      if (f.size > maxBytes) tooBig.push(f.name)
      else kept.push(f)
    }
    setRejected(tooBig)
    setFiles((current) => {
      const room = MAX_FILES - current.length
      if (kept.length > room) {
        setRejected((r) => [...r, ...kept.slice(room).map((f) => f.name)])
      }
      return [...current, ...kept.slice(0, Math.max(0, room))]
    })
  }

  const submit = () => {
    const text = body.trim()
    if (editing) {
      if (!text) return
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
    // ⚠ A MESSAGE NEEDS A BODY **OR** AN ATTACHMENT (D224), which is why this is not
    // `if (!text) return`: sending a photo with no caption is the ordinary case.
    if (!text && files.length === 0) return
    const done = () => {
      setBody('')
      setFiles([])
      setRejected([])
      setProgress(null)
      onClearReply()
    }
    if (files.length > 0) {
      setProgress(0)
      upload.mutate(
        { body: text, replyToID: replyTo?.id, files, onProgress: setProgress },
        { onSuccess: done, onError: () => setProgress(null) },
      )
      return
    }
    send.mutate({ body: text, replyToID: replyTo?.id }, { onSuccess: done })
  }

  const busy = send.isPending || edit.isPending || upload.isPending

  return (
    <div
      className={cn(
        'border-t border-border px-3 py-2.5 lg:px-4',
        dragging && 'bg-accent-soft outline-2 -outline-offset-2 outline-dashed outline-accent',
      )}
      onDragOver={(e) => {
        if (editing) return
        e.preventDefault()
        setDragging(true)
      }}
      onDragLeave={() => setDragging(false)}
      onDrop={(e) => {
        if (editing) return
        e.preventDefault()
        setDragging(false)
        accept(Array.from(e.dataTransfer.files))
      }}
    >
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
      {/* The staged files, with their sizes — so "why is this taking so long" has an
          answer on screen before the upload starts. */}
      {files.length > 0 && (
        <ul className="mb-2 flex flex-col gap-1">
          {files.map((f, i) => (
            <li
              key={`${f.name}-${i}`}
              className="flex items-center gap-2 rounded-md bg-s2 px-2.5 py-1.5 text-xs"
            >
              <Paperclip size={13} className="flex-none text-muted" aria-hidden />
              <span className="min-w-0 flex-1 truncate">{f.name}</span>
              <span className="flex-none tabular-nums text-muted">{fmtBytes(f.size)}</span>
              {progress === null && (
                <button
                  type="button"
                  aria-label={`${cs.chat.removeFile} ${f.name}`}
                  onClick={() => setFiles((cur) => cur.filter((_, j) => j !== i))}
                  className="flex-none text-muted hover:text-fg"
                >
                  <X size={14} aria-hidden />
                </button>
              )}
            </li>
          ))}
          {progress !== null && (
            <li className="px-0.5">
              {/* ⚠ ONE BAR FOR THE REQUEST, NOT ONE PER FILE. The upload IS one
                  request (D224), so per-file bars would be an invented breakdown of
                  a number the browser reports once. */}
              <div
                role="progressbar"
                aria-valuemin={0}
                aria-valuemax={100}
                aria-valuenow={Math.round(progress * 100)}
                aria-label={cs.chat.uploading}
                className="h-1.5 overflow-hidden rounded-full bg-s3"
              >
                <div
                  className="h-full rounded-full bg-accent transition-[width]"
                  style={{ width: `${Math.round(progress * 100)}%` }}
                />
              </div>
            </li>
          )}
        </ul>
      )}
      {rejected.length > 0 && (
        <p className="mb-2 rounded-md bg-attention-soft px-2.5 py-1.5 text-xs text-attention text-pretty">
          {cs.chat.filesRejected(rejected.join(', '), Math.round(maxBytes / (1024 * 1024)))}
        </p>
      )}

      <div className="flex items-end gap-2">
        {!editing && (
          <>
            <input
              ref={picker}
              type="file"
              multiple
              className="hidden"
              onChange={(e) => {
                accept(Array.from(e.target.files ?? []))
                // Reset, or picking the same file twice in a row does nothing.
                e.target.value = ''
              }}
            />
            <Button
              variant="ghost"
              aria-label={cs.chat.attachFiles}
              title={cs.chat.attachFiles}
              disabled={files.length >= MAX_FILES || progress !== null}
              onClick={() => picker.current?.click()}
            >
              <Paperclip size={16} aria-hidden />
            </Button>
          </>
        )}
        <Textarea
          ref={ref}
          rows={1}
          value={body}
          maxLength={8000}
          placeholder={cs.chat.composerPlaceholder}
          aria-label={cs.chat.composerPlaceholder}
          className="max-h-40 min-h-10 resize-y"
          onChange={(e) => setBody(e.target.value)}
          onPaste={(e) => {
            if (editing) return
            // ⚠ A PASTED SCREENSHOT ARRIVES AS A FILE ITEM WITH NO NAME. Taking it
            // only when there are files keeps an ordinary text paste untouched.
            const pasted = Array.from(e.clipboardData.files)
            if (pasted.length > 0) {
              e.preventDefault()
              accept(pasted)
            }
          }}
          onKeyDown={(e) => {
            // Enter sends, Shift+Enter breaks the line — the shape every chat uses,
            // and the one a household will already have in their fingers.
            if (e.key === 'Enter' && !e.shiftKey) {
              e.preventDefault()
              submit()
            }
          }}
        />
        <Button
          variant="primary"
          loading={busy}
          onClick={submit}
          disabled={!body.trim() && files.length === 0}
        >
          {editing ? cs.chat.save : cs.chat.send}
        </Button>
      </div>
    </div>
  )
}

/** D224's ten. */
const MAX_FILES = 10

/**
 * useMaxUploadBytes is the per-file cap the composer refuses against.
 *
 * ⚠ IT COMES FROM THE SERVER (`/api/chat/storage`), NOT FROM A CONSTANT. It is
 * Dokumenty's cap, shared on purpose (D228), so an operator who raises
 * `HOME_DOCS_MAX_UPLOAD_MB` raises it here too — and a hard-coded 50 would refuse
 * files the server would happily take while naming a limit that is not the limit.
 *
 * The server is still the authority: this check exists so a member does not spend
 * minutes of a household uplink on a file that will be refused, and the 413 is what
 * actually decides. The fallback while the query is in flight is the documented
 * default, which errs toward letting the request through to a 413 that explains
 * itself rather than refusing something valid with no server involved.
 */
function useMaxUploadBytes(): number {
  const storage = useChatStorage()
  return (storage.data?.max_upload_mb ?? 50) * 1024 * 1024
}

// ---- formatting ----
//
// ⚠ THERE IS NONE HERE ANY MORE (v10 review). This file declared its own
// Intl.DateTimeFormat pair and rendered `27. srpna 2026`, where PRD D20 fixes the
// house shape at `d. M. yyyy` and i18n/format.ts is where every other module gets
// it. The floor line is one of the three places v10 explains itself, so it is the
// last place the date should disagree with the rest of the app.
