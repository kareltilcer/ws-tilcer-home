import {
  Fragment,
  useCallback,
  useEffect,
  useId,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from 'react'
import { Link, useNavigate } from 'react-router-dom'
import {
  ArrowLeft,
  ArrowUp,
  Bell,
  BellOff,
  MoreHorizontal,
  Plus,
  Smile,
  Users,
  X,
} from 'lucide-react'
import { cn } from '@/lib/utils'
import { cs } from '@/i18n/cs'
import { count, PLURAL } from '@/i18n/plural'
import { fmtBytes, fmtDate, fmtTime } from '@/i18n/format'
import { Button, Input, Textarea } from '@/components/ui/ui'
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
  useSetMuted,
  useSetReaction,
  useUploadMessage,
} from './api/hooks'
import type { ChatMessage, Conversation, MessageQuote } from './api/types'
import { AttachmentView } from './AttachmentView'
import { useMessageGestures } from './gestures'
import { hasReacted, HEART, isMine, reactionLabel, REACTION_PALETTE } from './reactions'
import { newMessagesAnchor } from './when'

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
  // ⚠ ONE MUTATION FOR THE THREAD, NOT ONE PER BUBBLE — the correction the delete
  // took in the v10 review, applied to the verb that arrives with a gesture. A
  // thread at the 200-message cap would otherwise mount two hundred mutation
  // observers for something at most one bubble at a time is doing.
  const react = useSetReaction(conversationID)
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
  const [deletingMessage, setDeletingMessage] = useState<ChatMessage | null>(null)

  /**
   * The *Nové zprávy* divider's count, TAKEN ONCE (§V10-7).
   *
   * ⚠ IT CANNOT READ THE LIVE COUNT. `catchUp` above advances the read marker the
   * moment the newest message is on screen, which invalidates the conversation and
   * brings `unread_count` back as 0 — so a divider driven by the live value is drawn
   * and removed inside the same second, telling a member "you left off here" and
   * then taking it away before they can look. The snapshot is the count as the room
   * was opened, which is the thing the line is actually about.
   *
   * ⚠ AND IT IS TAKEN ON THE FIRST FRAME THAT HAS ONE, not on mount: the two queries
   * start together, and on mount `conversation.data` is usually still undefined.
   */
  const enteredUnread = useRef<number | null>(null)
  if (enteredUnread.current === null && conversation.data) {
    enteredUnread.current = conversation.data.unread_count
  }

  /**
   * The message the divider sits above, RESOLVED ONCE (see `newMessagesAnchor`).
   *
   * ⚠ AN ID, NOT A POSITION (v10 review). The position was recomputed every render
   * as `loaded - unread`, which is stable under a prepend and moves under an append
   * — so each arriving /ws message, and each message the member sent themselves,
   * slid the line one row further down the thread. It ended up below messages that
   * had already been read and eventually labelled their own newest message *Nové
   * zprávy*, in the one place the room says where they left off.
   *
   * Wrapped in an object so a legitimately null anchor still counts as resolved.
   */
  const dividerAnchor = useRef<{ id: string | null } | null>(null)

  if (conversation.isPending || thread.isPending) {
    return <ThreadSkeleton />
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
  // Resolved on the first frame that has a thread to resolve it against — both
  // queries have landed by here — and never again, so neither an older page above
  // it nor an arriving message below it can move the line.
  if (dividerAnchor.current === null && messages.length > 0) {
    dividerAnchor.current = {
      id: newMessagesAnchor(
        messages,
        enteredUnread.current ?? 0,
        thread.data?.has_more ?? false,
        me,
      ),
    }
  }
  const dividerID = dividerAnchor.current?.id ?? null

  return (
    <div className="flex h-full min-h-0 flex-col">
      <ThreadHeader conversation={room} onOpenMembers={onOpenMembers} />

      <div
        ref={scrollRef}
        onScroll={catchUp}
        className="min-h-0 flex-1 overflow-y-auto overflow-x-hidden om-scroll px-3 py-4 lg:px-5"
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

        {/* `atTop` is false once *Načíst starší* is above it: the panel hangs off
            the top of the scroll box, and hanging it off a button instead would
            overlap the control it is meant to sit under. */}
        <FloorLine conversation={room} atTop={!thread.data?.has_more} />

        {messages.length === 0 && (
          <div className="grid min-h-[200px] place-items-center px-4 text-center">
            <div className="max-w-[440px]">
              <div className="mb-2 text-lg font-extrabold">{cs.chat.threadEmpty}</div>
              <p className="text-[13.5px] leading-relaxed text-muted text-pretty">
                {cs.chat.threadEmptyHint}
              </p>
            </div>
          </div>
        )}

        <ul className="flex flex-col gap-2.5">
          {messages.map((m) => (
            <Fragment key={m.id}>
              {/* The *Nové zprávy* line — accent, because unread is a reason to look
                  rather than a warning, and drawn as a rule THROUGH the thread so it
                  reads as a position in it and not as a message.
                  ⚠ ITS OWN `<li>`, not a block inside the next message's (v10
                  review). Nested, a screen reader read the line out as part of that
                  one message — the opposite of "a position in the thread" — and the
                  list carried one item fewer than it had messages. */}
              {m.id === dividerID && (
                <li className="my-2 flex items-center gap-2.5">
                  <span className="h-px flex-1 bg-accent" />
                  <span className="font-mono text-[10px] font-bold uppercase tracking-[0.06em] text-accent">
                    {cs.chat.newMessagesDivider}
                  </span>
                  <span className="h-px flex-1 bg-accent" />
                </li>
              )}
              <li>
                <Bubble
                  message={m}
                  mine={m.author_id === me}
                  me={me}
                  onReact={(emoji, reacted) =>
                    react.mutate({ messageID: m.id, emoji, reacted })
                  }
                  // ⚠ ONE MUTATION FOR THE THREAD, NOT ONE PER BUBBLE (v10 review).
                  // Bubble called useDeleteMessage itself, so a thread at the
                  // 200-message cap mounted two hundred mutation observers — for a
                  // verb at most one bubble at a time can use, and on messages whose
                  // menu is never even rendered. The composer's send and edit already
                  // live at this level for the same reason.
                  // ⚠ IT ASKS NOW, because the verb moved out of a menu and into the
                  // footer. A delete blanks the body in place and leaves a tombstone
                  // everybody has already seen (D223) — there is no undo — and a
                  // one-tap irreversible control sitting permanently beside *Upravit*
                  // at 375 px is a mis-tap waiting to happen. The menu used to be the
                  // deliberation; the confirm is.
                  onDelete={() => setDeletingMessage(m)}
                  onReply={() => setReplyTo(m)}
                  // ⚠ STARTING AN EDIT DROPS A PENDING REPLY (v10 review). The
                  // composer hides the reply chip while an edit is open, so a reply
                  // begun before it was invisible but still armed — and once the edit
                  // finished, the next ordinary message went out as a reply to a
                  // message nobody had looked at in a while, with no unsend behind it.
                  onEdit={() => {
                    setReplyTo(null)
                    setEditing(m)
                  }}
                />
              </li>
            </Fragment>
          ))}
        </ul>
      </div>

      <Composer
        conversationID={conversationID}
        roomName={room.name}
        replyTo={replyTo}
        editing={editing}
        onClearReply={() => setReplyTo(null)}
        onClearEdit={() => setEditing(null)}
      />

      <ResponsiveModal
        open={deletingMessage !== null}
        onOpenChange={(o) => !o && setDeletingMessage(null)}
        title={cs.chat.word.deleteMessage}
        footer={
          <>
            <Button variant="ghost" onClick={() => setDeletingMessage(null)}>
              {cs.chat.cancel}
            </Button>
            {/* ⚠ IT CLOSES ON SUCCESS, NOT ON THE CLICK (v10 review). Dismissing
                synchronously meant `loading` could never render — the dialog was
                gone before the mutation was pending — and the member released an
                irreversible verb into a dialog that vanished with no acknowledgement
                that anything had been sent. RenameDialog and DeleteDialog below both
                wait for the same reason; a failure still surfaces as the mutation's
                own toast, with the dialog still open behind it. */}
            <Button
              variant="danger"
              loading={remove.isPending}
              onClick={() => {
                const m = deletingMessage
                if (!m) return
                remove.mutate(m.id, { onSuccess: () => setDeletingMessage(null) })
              }}
            >
              {cs.chat.confirmDelete}
            </Button>
          </>
        }
      >
        <p className="text-sm text-pretty">{cs.chat.deleteMessageBody}</p>
      </ResponsiveModal>
    </div>
  )
}

/**
 * The loading state — shimmering bubble shapes, not a centred spinner.
 *
 * ⚠ IT HAS TO LOOK LIKE THE THING THAT IS COMING. A spinner in the middle of the
 * pane says "something is happening somewhere"; staggered bars along the left edge
 * say "messages, shortly" and hold roughly the height the first screenful will take,
 * so the thread does not jump the moment it lands.
 */
function ThreadSkeleton() {
  const widths = ['55%', '38%', '62%', '30%', '48%']
  return (
    // ⚠ THE BARS ARE `aria-hidden`, THE REGION IS NOT. A skeleton is a picture of
    // absent content, so announcing five empty boxes is noise — but announcing
    // nothing at all leaves a screen-reader user on a silent pane, which is what the
    // spinner this replaced at least avoided.
    <div role="status" className="flex h-full min-h-0 flex-col px-3 py-4 lg:px-5">
      <span className="sr-only">{cs.chat.loadingThread}</span>
      <div className="flex flex-col gap-2.5" aria-hidden>
        {widths.map((w, i) => (
          <div
            key={i}
            className="h-11 animate-pulse rounded-[14px] bg-s2"
            style={{ width: w, animationDelay: `${i * 90}ms` }}
          />
        ))}
      </div>
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
  const setMuted = useSetMuted(conversation.id)
  // ⚠ Všichni is renameable and NOT deletable (D219) — the same asymmetry the
  // service enforces with a 422. Hiding the entry is not the guard; it is what
  // stops a member meeting the guard as an error message.
  const isDefault = conversation.kind === 'default'
  const muted = conversation.muted
  const muteLabel = muted ? cs.chat.word.unmute : cs.chat.word.mute
  const toggleMute = () => setMuted.mutate(!muted)

  return (
    <header className="flex flex-none items-center gap-2 border-b border-border px-3 py-2.5 lg:px-5 lg:py-3">
      {/* Below 1024 the thread is a route push, so back returns to the list. The
          link is hidden on desktop, where both panes are on screen at once. */}
      <Link
        to="/chat"
        className="grid h-11 w-11 flex-none place-items-center rounded-[11px] border border-border bg-s2 text-fg hover:bg-s3 lg:hidden"
        aria-label={cs.chat.listTitle}
      >
        <ArrowLeft size={17} aria-hidden />
      </Link>
      <div className="min-w-0 flex-1">
        <h2 className="truncate text-[15.5px] font-extrabold tracking-tight lg:text-[17px]">
          {conversation.name}
        </h2>
        {/* The declined noun, not the section label: `count` is what the
            conversation list uses for this same number one file away. */}
        <p className="truncate text-[11px] text-muted lg:text-[11.5px]">
          {count(conversation.member_count, PLURAL.members)}
        </p>
      </div>

      {/* ⚠ THE VERBS ARE SPELLED OUT AT THE DESK AND FOLDED UNDER A THUMB. The
          design draws Členové · Ztlumit · Smazat as three labelled buttons at 1440
          and a single 44 px members control at 375, where a fourth control would
          crowd the room's own name — which is the one thing on this header that
          must never be in doubt (there is no unsend). So the menu carries whatever
          the width could not, rather than duplicating what it already shows. */}
      <button
        type="button"
        onClick={onOpenMembers}
        aria-label={cs.chat.word.members}
        className="grid h-11 w-11 flex-none place-items-center rounded-[11px] border border-border bg-s2 text-muted hover:text-fg lg:hidden"
      >
        <Users size={16} aria-hidden />
      </button>
      <div className="hidden flex-none items-center gap-2 lg:flex">
        <Button size="sm" variant="secondary" onClick={onOpenMembers}>
          <Users size={14} aria-hidden />
          {cs.chat.word.members}
        </Button>
        <Button
          size="sm"
          variant="secondary"
          aria-pressed={muted}
          loading={setMuted.isPending}
          onClick={toggleMute}
        >
          {muted ? <BellOff size={14} aria-hidden /> : <Bell size={14} aria-hidden />}
          {muteLabel}
        </Button>
        {isDefault ? (
          // ⚠ NOT A DISABLED BUTTON. Všichni cannot be deleted (D219) and the
          // service answers 422; a greyed control invites the press that meets it,
          // so the header states the fact instead of offering the action.
          <span
            title={cs.chat.everyoneCannotLeave}
            className="inline-flex h-8 flex-none items-center rounded-md border border-border px-2.5 text-[13px] font-semibold text-subtle"
          >
            {cs.chat.deleteUnavailable}
          </span>
        ) : (
          <Button size="sm" variant="danger" onClick={() => setDeleting(true)}>
            {cs.chat.word.deleteConversation}
          </Button>
        )}
      </div>

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
          className="grid h-11 w-11 place-items-center rounded-[11px] text-muted hover:bg-s2 hover:text-fg lg:h-8 lg:w-8 lg:rounded-md"
        >
          <MoreHorizontal size={16} aria-hidden />
        </button>
        {menuOpen && (
          <>
            {/* ⚠ A MENU NEEDS A WAY OUT THAT IS NOT THE MENU. Without this the only
                way to dismiss it was to press the trigger again — on a phone, where
                the obvious gesture is to tap the thread and have it go away.

                ⚠ AND IT IS A POINTER TARGET ONLY (v10 review). Announced and in the
                tab order, it was an invisible full-viewport control called *Zrušit*
                sitting between the trigger and the first menu entry — met by exactly
                the two people who cannot see what it is for. Keyboard loses nothing:
                Shift+Tab returns to the trigger, which closes the menu. */}
            <button
              type="button"
              tabIndex={-1}
              aria-hidden
              className="fixed inset-0 z-10 cursor-default"
              onClick={() => setMenuOpen(false)}
            />
            <div className="absolute right-0 z-20 mt-1 w-56 rounded-md border border-border bg-s1 p-1 shadow-[var(--shadow)]">
              {/* ⚠ THE FOLD IS `lg:hidden`, NOT A JS MEDIA QUERY (v10 review). These
                  entries carry what the header row above could not fit, and the row
                  is shown by Tailwind's `lg:` — 1024 px. Hiding them with
                  `useIsDesktop()` instead put the fold at the MD breakpoint, 768 px,
                  and between the two neither existed: at any width from 768 to 1023
                  — an iPad in portrait is exactly 768 — Ztlumit konverzaci and
                  Smazat konverzaci were unreachable, and a room created by mistake
                  had no delete anywhere in the UI. One mechanism, one breakpoint, no
                  gap. `display:none` also takes them out of the accessibility tree,
                  so the desk never meets either verb twice. */}
              <button
                type="button"
                className="block w-full rounded px-2 py-2 text-left text-sm hover:bg-s2 lg:hidden"
                onClick={() => {
                  setMenuOpen(false)
                  toggleMute()
                }}
              >
                {muteLabel}
              </button>
              <button
                type="button"
                className="block w-full rounded px-2 py-2 text-left text-sm hover:bg-s2"
                onClick={() => {
                  setMenuOpen(false)
                  setRenaming(true)
                }}
              >
                {cs.chat.word.rename}
              </button>
              {isDefault ? (
                // ⚠ THE REASON TRAVELS WITH THE FACT, exactly as it does on the
                // desk. Stated-rather-than-disabled (D219) only works while the
                // sentence explaining it is reachable; without the title this read
                // as a refusal with nothing behind it.
                <p
                  title={cs.chat.everyoneCannotLeave}
                  className="px-2 py-2 text-left text-sm text-subtle lg:hidden"
                >
                  {cs.chat.deleteUnavailable}
                </p>
              ) : (
                <button
                  type="button"
                  className="block w-full rounded px-2 py-2 text-left text-sm text-danger hover:bg-danger/10 lg:hidden"
                  onClick={() => {
                    setMenuOpen(false)
                    setDeleting(true)
                  }}
                >
                  {cs.chat.word.deleteConversation}
                </button>
              )}
            </div>
          </>
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
function FloorLine({
  conversation,
  atTop,
}: {
  conversation: Conversation
  atTop: boolean
}) {
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
    /* ⚠ IT IS THE CEILING OF THE THREAD, drawn as a panel hanging off the top of the
       scroll box rather than as a paragraph among the messages. The negative margins
       pull it full-bleed and square its top edge against the header, so it reads as
       where the thread STOPS — a message-shaped block in the flow would read as the
       first message, which is the one thing it is not. */
    <div
      className={cn(
        '-mx-3 mb-2.5 border-b border-l border-r border-border bg-s2 px-4 py-3 lg:-mx-5',
        atTop ? '-mt-4' : 'rounded-t-xl border-t',
      )}
    >
      <p className="mx-auto max-w-[74ch] text-center text-[12.5px] leading-relaxed text-muted text-pretty">
        {cs.chat.floorLine}{' '}
        <span className="whitespace-nowrap">
          {cs.chat.floorLineFrom} {fmtDate(new Date(conversation.effective_from))}.
        </span>
      </p>
    </div>
  )
}

function Bubble({
  message,
  mine,
  me,
  onReply,
  onEdit,
  onDelete,
  onReact,
}: {
  message: ChatMessage
  mine: boolean
  /** The caller's user id — how a chip decides whether it is theirs (D265). */
  me: string
  onReply: () => void
  onEdit: () => void
  onDelete: () => void
  onReact: (emoji: string, reacted: boolean) => void
}) {
  if (message.deleted) {
    return (
      <div className={cn('flex', mine ? 'justify-end' : 'justify-start')}>
        {/* The tombstone (D223). ⚠ `--att-removed`, the same register as a removed
            attachment: both are things that are deliberately not there, and the
            module should not invent a second vocabulary for one idea. */}
        <div className="max-w-[min(560px,86%)] rounded-[14px] border border-dashed border-border-strong px-3 py-2 text-[12.5px] italic text-att-removed">
          {cs.chat.word.deleted}
        </div>
      </div>
    )
  }

  // ⚠ THE TOMBSTONE RETURNS ABOVE, BEFORE ANY HOOK. A tombstone has nothing to
  // react to, nothing to reply to and nothing to swipe — and keeping the gesture
  // state out of this branch entirely is what lets the early return stay an early
  // return rather than becoming a rules-of-hooks problem the next person has to
  // work around.
  return (
    <LiveBubble
      message={message}
      mine={mine}
      me={me}
      onReply={onReply}
      onEdit={onEdit}
      onDelete={onDelete}
      onReact={onReact}
    />
  )
}

/**
 * A message that still says something — the bubble with its gestures.
 *
 * ⚠ THE THREE GESTURES ARE ACCELERATORS AND NEVER THE ONLY WAY IN (D268). Double
 * tap hearts, swipe-right replies, long press opens the reaction bar — and each of
 * them has a control on screen doing the same thing, because this is a household app
 * whose members include somebody on a desktop with no touchscreen and somebody who
 * will never find a gesture nobody told them about. The footer keeps *Odpovědět*,
 * the chips are buttons, and the ☺ opens the same bar the long press does.
 */
function LiveBubble({
  message,
  mine,
  me,
  onReply,
  onEdit,
  onDelete,
  onReact,
}: {
  message: ChatMessage
  mine: boolean
  me: string
  onReply: () => void
  onEdit: () => void
  onDelete: () => void
  onReact: (emoji: string, reacted: boolean) => void
}) {
  const [picking, setPicking] = useState(false)
  /** The ☺ and the bar it opens are no longer siblings, so the link is written out. */
  const pickerID = useId()
  /**
   * ⚠ AND THE TRIGGER IS HANDED DOWN THE SAME WAY, for the same reason (v10.1
   * review). Moving the bar out of the bubble put the whole footer between the ☺ and
   * the palette it opens — press the ☺ on a keyboard and the next Tab stops are the
   * message's own verbs, which on your own message means *Odpovědět*, *Upravit* and
   * *Smazat zprávu* before a single emoji. The bar takes focus when it opens and
   * gives it back to this button when it closes, which is what the DOM adjacency
   * used to do for nothing.
   */
  const smile = useRef<HTMLButtonElement>(null)
  const gestures = useMessageGestures({
    // ⚠ THE DOUBLE TAP TOGGLES rather than always adding. It is the one gesture with
    // no visible twin of its own — the chip IS the twin — so a second double tap
    // has to undo the first, or a mis-tapped heart is only removable by finding the
    // chip and pressing it, which is the discoverability problem the gesture was
    // meant to be an accelerator over.
    onDoubleTap: () => onReact(HEART, !hasReacted(message.reactions, HEART, me)),
    onSwipeReply: onReply,
    onLongPress: () => setPicking(true),
  })

  return (
    // ⚠ THE ROW IS THE PICKER'S ANCHOR, AND IT CANNOT BE THE BUBBLE (v10.1 fix). The
    // palette used to be a strip in the flow INSIDE the bubble, so opening it resized
    // the message: seven 44 px targets and a ✕ are ~380 px wide, and a bubble holding
    // *ok* jumped to the thread's full width, grew a row taller than its own body, and
    // pushed every message below it down. A control that reshapes what it is attached
    // to is a layout change, not a picker.
    //
    // The bubble was the obvious anchor and is the wrong one twice over: it carries
    // `translateX` for the swipe, and a transform makes an element the containing block
    // for its absolute AND fixed descendants — so the bar would have been capped at the
    // bubble's width, and the dismiss scrim would have covered the bubble instead of
    // the viewport. The row spans the thread, is never transformed, and sits inside the
    // scroll box, which is what lets the bar move with the message for free.
    <div
      className={cn(
        'relative flex',
        mine ? 'justify-end' : 'justify-start',
        // ⚠ AND IT HAS TO BE LIFTED. Every bubble is transformed, so every bubble is a
        // stacking context painted in DOM order — without a positive z-index here the
        // bar is drawn UNDER each message that follows it.
        picking && 'z-20',
      )}
    >
      <div
        // ⚠ `pan-y` RATHER THAN `none`. The browser keeps vertical panning on its
        // own thread, so the thread scrolls exactly as it did while a horizontal
        // drag is still available to the swipe. `touch-action: none` here would
        // hand every scroll that starts on a bubble — which is most of them — to
        // JavaScript, and a thread that stutters is a worse trade than no gesture.
        style={{ transform: `translateX(${gestures.swipeX}px)`, touchAction: 'pan-y' }}
        {...gestures.handlers}
        className={cn(
          'max-w-[min(560px,86%)] border px-3 py-2.5 text-fg lg:max-w-[min(560px,78%)]',
          // No transition while the finger is down — the bubble tracks it — and one
          // on the way back, so a released swipe settles instead of snapping.
          gestures.swipeX === 0 && 'transition-transform',
          // ⚠ COLOUR ONLY REINFORCES. The two bubble tints are measured at 1.55:1
          // dark / 1.16:1 light against each other — deliberately below 3:1 — so
          // ALIGNMENT, the tail corner and the author label are what actually carry
          // own-versus-others. The squared corner is the load-bearing half: it
          // survives greyscale, low brightness and both themes, which the fill does
          // not pretend to.
          mine
            ? 'rounded-[14px] rounded-br-[5px] border-bub-mine-edge bg-bub-mine'
            : 'rounded-[14px] rounded-bl-[5px] border-bub-theirs-edge bg-bub-theirs',
          // ⚠ THE ARMED SWIPE SAYS SO, AND LAST SO IT WINS THE MERGE (v10.1 review).
          // SWIPE_COMMIT decides when releasing will reply and the rubber band says
          // further travel changes nothing — neither was drawn, so the member had 24 px
          // of meaningless extra drag and no way to tell a committed swipe from an
          // aborted one before lifting a finger that cannot be un-lifted. The accent
          // edge is the cue a pressed chip already uses, on the border the bubble
          // already has.
          gestures.swipeArmed && 'border-accent',
        )}
      >
        {/* ⚠ `--bub-label` IS `--muted`, NOT `--subtle`: measured on `--s2` in the
            light theme, --subtle falls to 4.04:1, under the AA bar. */}
        {!mine && (
          <div className="mb-1 text-[11px] font-bold text-bub-label">{message.author_label}</div>
        )}
        {message.reply_to && <Quote quote={message.reply_to} />}
        {/* ⚠ THE ATTACHMENTS COME BEFORE THE BODY, which is the order every chat
            uses and the order a caption reads in. An image with a caption under it
            is a photo; a caption with an image under it is a document. */}
        {message.attachments.length > 0 && (
          <div className="mb-1.5 flex flex-col gap-2">
            {message.attachments.map((a) => (
              <AttachmentView key={a.id} attachment={a} />
            ))}
          </div>
        )}
        {message.body && (
          <div className="whitespace-pre-wrap break-words text-[13.5px] leading-normal text-pretty">
            {message.body}
          </div>
        )}

        {/* ⚠ THE CHIPS SIT UNDER THE CONTENT AND ABOVE THE SIGNATURE, which is the
            design's placement and an argument rather than a preference: a reaction
            belongs to what was SAID, not to when it was said. Between the body and
            the timestamp is the only place that reads that way. */}
        <ReactionRow
          message={message}
          me={me}
          picking={picking}
          pickerID={pickerID}
          triggerRef={smile}
          onPicking={setPicking}
          onReact={onReact}
        />

        {/* ⚠ THE VERBS LIVE IN THE FOOTER, ALWAYS DRAWN. They used to be icons in a
            hover-revealed rail outside the bubble, which Tailwind v4 wraps in
            `@media (hover: hover)` — so at 375 px, the width this module is designed
            around, they were invisible-but-clickable targets a member found by
            accident. Words in the footer are legible at every width, need no hover
            to exist, and are the shape the design draws. */}
        <div className="mt-1.5 flex flex-wrap items-center gap-x-2.5 gap-y-1 font-mono text-[10px] text-muted">
          <time dateTime={message.created_at}>{fmtTime(message.created_at)}</time>
          {message.edited_at && (
            <span className="font-sans text-[10.5px] italic">{cs.chat.word.edited}</span>
          )}
          <span className="flex-1" />
          <button type="button" onClick={onReply} className="hover:text-fg hover:underline">
            {cs.chat.word.reply}
          </button>
          {mine && (
            <>
              <button type="button" onClick={onEdit} className="hover:text-fg hover:underline">
                {cs.chat.word.edit}
              </button>
              <button
                type="button"
                onClick={onDelete}
                className="hover:text-danger hover:underline"
              >
                {cs.chat.word.deleteMessage}
              </button>
            </>
          )}
        </div>
      </div>

      {/* Outside the bubble on purpose — see the row's note above. */}
      {picking && (
        <ReactionPicker
          id={pickerID}
          message={message}
          me={me}
          mine={mine}
          trigger={smile}
          onPicking={setPicking}
          onReact={onReact}
        />
      )}
    </div>
  )
}

/**
 * The chips and the ☺ (v10.1, D265). The bar the ☺ opens is `ReactionPicker` below.
 *
 * ⚠ THE ROW IS ALWAYS RENDERED, EVEN WITH NO CHIPS, and that is what makes the
 * feature discoverable at all. The ☺ is the visible twin of the long press — a
 * gesture with no control beside it is a feature only the person who built it knows
 * about, and this app is used by a household rather than by its author.
 */
function ReactionRow({
  message,
  me,
  picking,
  pickerID,
  triggerRef,
  onPicking,
  onReact,
}: {
  message: ChatMessage
  me: string
  picking: boolean
  /** The bar's `id`, for the ☺'s `aria-controls` — they are in different subtrees. */
  pickerID: string
  /** The ☺ itself, so the bar can hand focus back to it. Same subtree problem. */
  triggerRef: React.RefObject<HTMLButtonElement | null>
  onPicking: (open: boolean) => void
  onReact: (emoji: string, reacted: boolean) => void
}) {
  return (
    <div className="mt-1.5 flex flex-wrap items-center gap-1.5">
      {message.reactions.map((r) => {
        const ours = isMine(r, me)
        return (
          <button
            key={r.emoji}
            type="button"
            onClick={() => onReact(r.emoji, !ours)}
            // ⚠ THE STATE IS NOT CARRIED BY THE ACCENT RING ALONE. `aria-pressed`
            // says it to a screen reader and to anybody who cannot separate the
            // two borders — the same rule the create dialog's member chips take.
            aria-pressed={ours}
            title={reactionLabel(r, me)}
            // ⚠ AND THE ACCESSIBLE NAME IS WHO REACTED, not the numeral. Without
            // it the button announces "❤️ 3" and the three people — the whole
            // point of the chip — are unreachable.
            aria-label={reactionLabel(r, me)}
            className={cn(
              'flex min-h-8 items-center gap-1.5 rounded-full border px-2 text-[13px] leading-none lg:min-h-[26px] lg:text-[12.5px]',
              ours
                ? 'border-accent bg-accent-soft text-fg'
                : 'border-border bg-s2 text-fg hover:bg-s3',
            )}
          >
            <span aria-hidden>{r.emoji}</span>
            {/* Mono tabular, so 3 and 40 do not shift the chips beside them — the
                same reasoning the unread badge takes one pane over. */}
            <span
              aria-hidden
              className={cn(
                'font-mono text-[10.5px] font-bold tabular-nums',
                ours ? 'text-fg' : 'text-muted',
              )}
            >
              {r.by.length}
            </span>
          </button>
        )
      })}
      <button
        ref={triggerRef}
        type="button"
        onClick={() => onPicking(!picking)}
        aria-expanded={picking}
        // Only while it exists: a reference to an absent id is a broken one.
        aria-controls={picking ? pickerID : undefined}
        aria-label={cs.chat.reactionAdd}
        title={cs.chat.reactionAdd}
        className={cn(
          'grid h-8 w-8 place-items-center rounded-full text-[13px] lg:h-[26px] lg:w-[26px] lg:text-[12px]',
          picking
            ? 'border border-accent bg-accent-soft text-fg'
            : 'border border-dashed border-border-strong text-muted hover:text-fg',
        )}
      >
        <Smile size={14} aria-hidden />
      </button>
    </div>
  )
}

/**
 * The palette bar — an OVERLAY hung off the row, not a strip inside the bubble.
 *
 * ⚠ THE STRIP IS WHAT THIS FIXES. In the flow it resized the message card the moment
 * it opened, which the row's own note above spells out: the bar is ~380 px wide and a
 * bubble is as wide as what somebody typed.
 *
 * ⚠ AND IT STILL DOES NOT REPOSITION ON SCROLL, which was the strip's whole argument.
 * It is absolute against the row, and the row is inside the scroll box — so it travels
 * with its message for nothing, with no listener and no measurement per frame. The one
 * measurement is `scrollIntoView`, taken once when it opens.
 *
 * ⚠ EXPORTED FOR ITS TEST, AND IT IS THE ONE PIECE OF THIS WORTH TESTING (v10.1 review
 * round 2). The rest of the fix is stacking contexts, containing blocks and
 * `scrollIntoView`, none of which jsdom has a layout engine to see — but the focus
 * contract is plain DOM, jsdom implements `activeElement` and `focus()`, and it is
 * where the bug this round found actually was. Nothing else below `ThreadView` needs
 * to be reachable: this component takes seven ordinary props and calls no query hook.
 */
export function ReactionPicker({
  id,
  message,
  me,
  mine,
  trigger,
  onPicking,
  onReact,
}: {
  id: string
  message: ChatMessage
  me: string
  /** Which edge it hangs from — the same edge its bubble is already against. */
  mine: boolean
  /** The ☺ that opened it, so focus has somewhere to go back to. */
  trigger: React.RefObject<HTMLButtonElement | null>
  onPicking: (open: boolean) => void
  onReact: (emoji: string, reacted: boolean) => void
}) {
  const bar = useRef<HTMLDivElement>(null)

  /**
   * ⚠ AN OVERLAY BELOW THE FOLD IS A FEATURE THAT LOOKS BROKEN, and the fold is
   * exactly where the common case puts it: the thread is pinned to its newest message
   * and the newest message is the one most likely to be reacted to, so the bar opens
   * under the last bubble, past the end of the thread's own content. It DOES extend the
   * scroll range when it hangs off the end — an absolute box still counts as scrollable
   * overflow, measured at 599 → 643 px — but nothing scrolls to it: the stick-to-bottom
   * effect above fires on a new message id and on nothing else, so the member presses
   * and the screen does not move. `block: 'nearest'` moves the box by the minimum and
   * does nothing at all when the bar already fits — 0 px mid-thread against 44 on the
   * last message — and in a layout effect it is never painted in the wrong place first.
   * The optional call is for jsdom, which has no such method.
   *
   * ⚠ IT IS NOT THAT THE SCRIM PINS THE THREAD, which an earlier draft of this comment
   * claimed. The scrim is `fixed`, but it is a DOM descendant of the scroll box, so a
   * wheel or a drag over it still pans the thread — measured, 39 → 35 px with the bar
   * open — and the bar travels with its row because it is anchored inside the same box.
   * The member can move the thread by hand; they simply have no reason to know they
   * need to.
   *
   * ⚠ AND IT TAKES FOCUS, BECAUSE IT IS NO LONGER NEXT TO ITS TRIGGER (v10.1 review).
   * In the flow the bar sat immediately after the ☺, so Tab walked straight into the
   * emoji; hung off the row it comes after the whole bubble, and on your own message
   * the stops in between are *Odpovědět*, *Upravit* and *Smazat zprávu* — the member
   * presses the ☺, tabs, and is offered the delete. `aria-controls` says the two are
   * related but moves nobody. Taking focus on open and handing it back on close is
   * what the dialogs in this app already do, and it repairs the older half of the
   * same problem: picking an emoji used to drop focus on `<body>`.
   *
   * `preventScroll` because the scroll was already decided one line up, and the
   * restore is skipped when the member has deliberately tabbed out of the bar — then
   * the focus is theirs and not ours to move.
   *
   * ⚠ WHICH IS WHY THE SCRIM REFUSES THE FOCUS A PRESS WOULD GIVE IT (round 2). The
   * scrim is this bar's SIBLING, so a scrim holding the focus is indistinguishable
   * from here from a member who moved it — and dismissing by tapping outside, the
   * commonest way out there is, took the exception and dropped focus on `<body>`. The
   * refusal is one `preventDefault` on the element itself; see its note below.
   *
   * Both nodes are read on the way in and held: a cleanup that reaches through a ref
   * is reading it after the commit that tore the tree down, and React never remounts
   * the ☺ while the bar it opened is up.
   */
  useLayoutEffect(() => {
    const el = bar.current
    const back = trigger.current
    el?.scrollIntoView?.({ block: 'nearest' })
    el?.querySelector('button')?.focus({ preventScroll: true })
    return () => {
      const active = document.activeElement
      if (!active || active === document.body || el?.contains(active)) {
        back?.focus({ preventScroll: true })
      }
    }
  }, [trigger])

  /**
   * ⚠ ESCAPE CLOSES IT, BECAUSE THE SCRIM MADE IT MODAL (v10.1 review round 2). The
   * strip this replaced was an ordinary run of buttons in the flow: the page went on
   * working around it, so leaving it open cost nothing and there was nothing to
   * escape from. The overlay is the opposite — it takes the focus, and its scrim
   * swallows every pointer event in the viewport — and a surface that stops the app
   * has to answer the one key everybody presses at it. Without this the way out was
   * Tab to the ✕ or Shift+Tab to the ☺, both of which have to be discovered.
   *
   * On `document` rather than on the bar, so it still answers after the member has
   * tabbed out of the bar and left it open behind them — the one state where they
   * are furthest from the ✕ and most likely to reach for the key.
   */
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onPicking(false)
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [onPicking])

  return (
    <>
      {/* ⚠ A WAY OUT THAT IS NOT THE BAR — the conversation menu's scrim, on the
          surface that needs it more: this one covers the messages under it, so without
          a scrim a tap on the neighbour it hides does nothing at all. A pointer target
          only, for the reason written out up there: announced, it is an invisible
          full-viewport control called *Zrušit* between the ☺ and the first emoji, met
          by exactly the people who cannot see what it is for. The keyboard keeps the
          ✕, which is a labelled control and is inside the bar.

          ⚠ AND IT MUST NOT TAKE THE FOCUS ON ITS WAY DOWN (v10.1 review round 2).
          This is where the header's copy stops being a copy. A press focuses a
          button, `tabIndex={-1}` or not, and the scrim is the bar's SIBLING — so
          dismissing by tapping outside, which is the way out this scrim exists to
          provide, made `document.activeElement` the scrim: not null, not `<body>`,
          not inside the bar, so every arm of the restore above failed, the ☺ never
          got its focus back, and unmounting the scrim dropped focus on `<body>`.
          Measured: `activeElement` was BODY after a scrim click. It also meant a
          press left real focus sitting on an `aria-hidden` node, which is the one
          place a screen reader has been told there is nothing to read.

          Refusing the default on mousedown answers both, and it is the whole of the
          difference: focus stays on the emoji the bar took it to, the restore above
          sees it inside the bar and hands it back, and the click still fires. The
          header's scrim needs none of this — its menu never takes focus, so it has
          none to lose. */}
      <button
        type="button"
        tabIndex={-1}
        aria-hidden
        className="fixed inset-0 z-10 cursor-default"
        onMouseDown={(e) => e.preventDefault()}
        onClick={() => onPicking(false)}
      />
      <div
        ref={bar}
        id={id}
        role="group"
        aria-label={cs.chat.reactionPickerLabel}
        className={cn(
          'om-scroll absolute top-full z-20 mt-1.5 flex w-max max-w-full gap-1 overflow-x-auto',
          'rounded-[12px] border border-border-strong bg-s1 p-1 shadow-[var(--shadow)]',
          // ⚠ `max-w-full` IS THE ROW'S WIDTH, WHICH IS THE THREAD'S. That is the
          // second reason the anchor is the row: seven 44 px targets and a ✕ do not
          // fit a phone either way, but they get the 351 px the thread has at 375
          // rather than the 298 the bubble has, and the remainder scrolls inside the
          // bar the way the design's does.
          mine ? 'right-0' : 'left-0',
        )}
      >
        {REACTION_PALETTE.map((emoji) => (
          <button
            key={emoji}
            type="button"
            onClick={() => {
              onReact(emoji, !hasReacted(message.reactions, emoji, me))
              onPicking(false)
            }}
            aria-label={emoji}
            // ⚠ 44 px UNDER A THUMB, 34 AT THE DESK — the design's two sizes. This
            // bar is the gesture's landing place, so it is reached with the same
            // finger that held the message down.
            className="grid h-11 w-11 flex-none place-items-center rounded-[10px] text-[20px] leading-none hover:bg-s3 lg:h-[34px] lg:w-[34px] lg:rounded-[9px] lg:text-[17px]"
          >
            <span aria-hidden>{emoji}</span>
          </button>
        ))}
        <button
          type="button"
          onClick={() => onPicking(false)}
          aria-label={cs.chat.reactionPickerClose}
          title={cs.chat.reactionPickerClose}
          className="grid h-11 w-9 flex-none place-items-center rounded-[10px] text-muted hover:bg-s3 hover:text-fg lg:h-[34px] lg:w-8"
        >
          <X size={15} aria-hidden />
        </button>
      </div>
    </>
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
  /* ⚠ ONE RULE, ONE COLOUR, BOTH CASES. The empty shape and the filled one are the
     same quote shape — a 3 px rule against `--border-strong` — because a member has
     to read the empty one AS a quote before they read it as missing. Only the text
     inside differs: `--att-removed` and italic, the module's settled-absence
     register, never an error colour. */
  if (!quote.available) {
    return (
      <div className="mb-1.5 border-l-[3px] border-border-strong py-1 pl-2.5 text-[11.5px] italic text-att-removed">
        {cs.chat.word.outsideHistory}
      </div>
    )
  }
  return (
    <div className="mb-1.5 border-l-[3px] border-border-strong py-1 pl-2.5">
      <div className="text-[10.5px] font-bold text-muted">{quote.author_label}</div>
      <div className={cn('truncate text-[11.5px] text-muted', quote.deleted && 'italic')}>
        {quote.deleted ? cs.chat.word.deleted : quote.excerpt}
      </div>
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
  roomName,
  replyTo,
  editing,
  onClearReply,
  onClearEdit,
}: {
  conversationID: string
  /** ⚠ The composer NAMES THE ROOM it will post into. There is no unsend, and at
   *  375 px the thread header scrolls out of a member's attention long before their
   *  thumb leaves the keyboard. */
  roomName: string
  replyTo: ChatMessage | null
  editing: ChatMessage | null
  onClearReply: () => void
  onClearEdit: () => void
}) {
  const [body, setBody] = useState('')
  const [files, setFiles] = useState<File[]>([])
  /** ⚠ NAME AND SIZE, not just the name. A refused file is listed beside the ones
   *  that were kept, in the same row shape, so "why is this one not going" is
   *  answered by the figure next to it rather than by a sentence somewhere else.
   *
   *  ⚠ AND WHICH CAP REFUSED IT (v10 review). Two rules drop a file — the per-file
   *  MB limit and D224's ten per message — and both used to land in one list that
   *  was then explained by the MB sentence alone: twelve 400 kB photos produced
   *  "Jeden soubor může mít nejvýše 50 MB" over two files nowhere near it. */
  const [rejected, setRejected] = useState<RejectedFile[]>([])
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
    const overCap: RejectedFile[] = []
    const kept: File[] = []
    for (const f of incoming) {
      if (f.size > maxBytes) overCap.push({ name: f.name, size: f.size, why: 'size' })
      else kept.push(f)
    }
    // ⚠ BOTH DECISIONS ARE MADE BEFORE EITHER setState, AGAINST `files` RATHER THAN
    // INSIDE AN UPDATER. The first version called setRejected from inside setFiles's
    // updater — an updater must be pure, and React runs it twice under StrictMode,
    // so the names of the files that overflowed the ten-file cap were appended to
    // the rejection list twice. It also made what the member is told depend on how
    // many times React chose to re-run the updater, which is not a guarantee React
    // offers. Both handlers here are user events, so `files` is current.
    const room = Math.max(0, MAX_FILES - files.length)
    const overflow: RejectedFile[] = kept
      .slice(room)
      .map((f) => ({ name: f.name, size: f.size, why: 'count' }))
    setFiles([...files, ...kept.slice(0, room)])
    setRejected([...overCap, ...overflow])
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
        'flex flex-none flex-col gap-2 border-t border-border bg-s1 px-3 pb-3 pt-2.5 lg:gap-2.5 lg:px-5 lg:pb-3.5',
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
        <div className="flex items-center gap-2 rounded-[10px] border border-border bg-s2 px-2.5 py-2 text-xs">
          <span className="min-w-0 flex-1 truncate text-muted">
            {cs.chat.replyingTo} <span className="font-semibold">{replyTo.author_label}</span>
          </span>
          <button type="button" onClick={onClearReply} className="flex-none text-muted hover:text-fg">
            {cs.chat.cancel}
          </button>
        </div>
      )}
      {editing && (
        <div className="flex items-center gap-2 rounded-[10px] border border-border bg-s2 px-2.5 py-2 text-xs">
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
          answer on screen before the upload starts. Refused files stand in the SAME
          list, marked, rather than in a sentence underneath it: a phone photo roll
          produces "nine up, one too big", and that is one list with one odd row in
          it, not two unrelated blocks. */}
      {(files.length > 0 || rejected.length > 0) && (
        <div className="flex flex-col gap-1.5">
          {/* ⚠ IT COUNTS THE ROWS THAT ARE ON SCREEN, refused ones included (v10
              review). Counting only the kept files put "0 souborů · nejvýš 10 na
              zprávu" directly above a list showing one — a heading disagreeing with
              the list it heads, in the one place a member checks what is about to
              be sent. */}
          <div className="font-mono text-[10.5px] text-subtle">
            {cs.chat.composerFileCount(
              count(files.length + rejected.length, PLURAL.files),
              MAX_FILES,
            )}
          </div>
          <ul className="flex flex-col gap-1.5">
            {files.map((f, i) => (
              <li
                key={`${f.name}-${i}`}
                className="flex items-center gap-2.5 rounded-[10px] border border-border bg-s2 px-2.5 py-2"
              >
                <span className="min-w-0 flex-1 truncate text-xs">{f.name}</span>
                <span className="flex-none font-mono text-[11px] tabular-nums text-muted">
                  {fmtBytes(f.size)}
                </span>
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
            {rejected.map((r, i) => (
              <li
                key={`rejected-${r.name}-${i}`}
                className="flex items-center gap-2.5 rounded-[10px] border border-danger bg-danger/10 px-2.5 py-2"
              >
                <span className="min-w-0 flex-1 truncate text-xs">{r.name}</span>
                <span className="flex-none font-mono text-[11px] tabular-nums text-muted">
                  {fmtBytes(r.size)}
                </span>
                <span className="flex-none text-[11px] font-bold text-danger">
                  {cs.chat.fileRefused}
                </span>
              </li>
            ))}
          </ul>
          {progress !== null && (
            /* ⚠ ONE BAR FOR THE REQUEST, NOT ONE PER FILE. The upload IS one
               request (D224), so per-file bars would be an invented breakdown of a
               number the browser reports once. */
            <div
              role="progressbar"
              aria-valuemin={0}
              aria-valuemax={100}
              aria-valuenow={Math.round(progress * 100)}
              aria-label={cs.chat.uploading}
              className="h-1 overflow-hidden rounded-full bg-s3"
            >
              <div
                className="h-full rounded-full bg-accent transition-[width]"
                style={{ width: `${Math.round(progress * 100)}%` }}
              />
            </div>
          )}
          {/* ⚠ ONE SENTENCE PER RULE. The MB cap and D224's ten refuse different
              files for different reasons, and a single sentence naming the megabytes
              was being applied to both. */}
          {rejected.some((r) => r.why === 'size') && (
            <p className="text-xs leading-normal text-muted text-pretty">
              {cs.chat.filesRejected(
                rejected
                  .filter((r) => r.why === 'size')
                  .map((r) => r.name)
                  .join(', '),
                Math.round(maxBytes / (1024 * 1024)),
              )}
            </p>
          )}
          {rejected.some((r) => r.why === 'count') && (
            <p className="text-xs leading-normal text-muted text-pretty">
              {cs.chat.filesOverCount(
                rejected
                  .filter((r) => r.why === 'count')
                  .map((r) => r.name)
                  .join(', '),
                count(MAX_FILES, PLURAL.files),
              )}
            </p>
          )}
        </div>
      )}

      <div className="flex items-end gap-2 lg:gap-2.5">
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
            <button
              type="button"
              aria-label={cs.chat.attachFiles}
              title={cs.chat.attachFiles}
              disabled={files.length >= MAX_FILES || progress !== null}
              onClick={() => picker.current?.click()}
              className="grid h-[46px] w-[46px] flex-none place-items-center rounded-[12px] border border-border bg-s2 text-muted hover:text-fg disabled:pointer-events-none disabled:opacity-50 lg:h-11 lg:w-11"
            >
              <Plus size={18} aria-hidden />
            </button>
          </>
        )}
        <Textarea
          ref={ref}
          rows={1}
          value={body}
          maxLength={8000}
          placeholder={cs.chat.composerPlaceholderIn(roomName)}
          // ⚠ THE SAME SENTENCE THE PLACEHOLDER SHOWS (v10 review). The label kept
          // the generic *Napište zprávu…* while the visible text names the room, so
          // the one reader who cannot see which thread is behind the composer was
          // the only one not told which room this posts into — on a control with no
          // unsend behind it.
          aria-label={cs.chat.composerPlaceholderIn(roomName)}
          className="max-h-40 min-h-[46px] resize-y rounded-[12px] py-3 text-sm lg:min-h-11"
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
        {/* ⚠ AN ARROW WHILE SENDING, A WORD WHILE EDITING. The two are different
            acts and the control says which one is armed: *Uložit* rewrites a message
            that is already in the room and visible to everyone, and an arrow that
            means "post" one moment and "rewrite" the next is the wrong affordance
            for the second. Both carry an accessible name either way. */}
        <Button
          variant="primary"
          className={cn(
            'min-h-[46px] flex-none rounded-[12px] lg:min-h-11',
            !editing && 'w-[46px] px-0 lg:w-11',
          )}
          loading={busy}
          onClick={submit}
          // ⚠ AN EDIT IS A BODY AND NOTHING ELSE (D225), so staged files must not
          // arm *Uložit* (v10 review). They did: the test read `files.length` in
          // both modes, while `submit`'s edit branch returns on an empty body — so
          // clearing the text with a file staged left a live button that did
          // nothing at all when pressed.
          disabled={editing ? !body.trim() : !body.trim() && files.length === 0}
          aria-label={editing ? cs.chat.save : cs.chat.send}
          title={editing ? cs.chat.save : cs.chat.send}
        >
          {editing ? cs.chat.save : <ArrowUp size={18} aria-hidden />}
        </Button>
      </div>

      {/* ⚠ THE STANDING HINT IS GONE (design v10.1). It named both caps and the
          reason they are Dokumenty's, permanently, under every composer in the app
          — a paragraph of documentation parked where the member types, read once
          and then in the way for good.

          ⚠ NEITHER CAP STOPPED BEING STATED, and this is the check that matters:
          `maxBytes` still refuses an over-cap file BEFORE it is uploaded, and
          `filesRejected` above names the MB beside the file it refused. A limit is
          worth reading at the moment it bites, which is where it now is. */}
    </div>
  )
}

/** D224's ten. */
const MAX_FILES = 10

/**
 * A file the composer would not take, and WHICH rule refused it.
 *
 * ⚠ `why` IS NOT COSMETIC. `size` is the server's per-file cap (`max_upload_mb`,
 * D228) and `count` is MAX_FILES above; the two produce different sentences, and
 * merging them was how a 400 kB photo came to be told it was over 50 MB.
 */
type RejectedFile = { name: string; size: number; why: 'size' | 'count' }

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
