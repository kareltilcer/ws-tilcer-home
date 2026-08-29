import { useEffect, useState } from 'react'
import { Route, Routes, useNavigate, useOutletContext, useParams } from 'react-router-dom'
import { WifiOff } from 'lucide-react'
import { cn } from '@/lib/utils'
import { cs } from '@/i18n/cs'
import { routes, type ShellLayout } from '@/app/routes'
import { useAuth } from '@/app/auth'
import { useOnline } from '@/platform/pwa/offline'
import { ConversationList } from './ConversationList'
import { ThreadView } from './ThreadView'
import { MembersPanel } from './MembersPanel'
import { StorageWarning } from './StorageWarning'
import { UklidPage } from './UklidPage'
import { pickConversationToOpen, readLastOpened, rememberLastOpened } from './lastOpened'
import { useChatLiveSync, useConversations, useLeaveWhenGone } from './api/hooks'

/**
 * Chat — the eleventh module, and the first one the household does not read in full.
 *
 * ⚠ TWO PANES AT ≥1024, STACKED BELOW (D262). `/chat/{id}` renders BOTH on desktop,
 * so a member never loses the unread counts to read a message; below 1024 the thread
 * is a route push and browser-back returns to the list.
 *
 * ⚠ AND IT IS IN THE NAV NOW. PR 2 deliberately left it off — a chat the household
 * meets before it can send a photo is a chat they will try to send a photo with —
 * so the tab and the demotion of Okno do budoucnosti (D260) land here, with the
 * attachments.
 *
 * ⚠ `/chat/uklid` IS MATCHED BEFORE `/chat/:id`. React Router picks the more
 * specific route regardless of order, but the order is written that way anyway:
 * `uklid` is a reserved word in this route tree, and a conversation is addressed by
 * UUIDv7 (D220 — no slugs anywhere in the module), so the two can never actually
 * collide.
 */
export function ChatPage() {
  useChatLiveSync()
  const online = useOnline()
  // ⚠ THE OFFLINE SCREEN REPLACES THE WHOLE MODULE, `/chat/uklid` INCLUDED, so it
  // has to fit whichever box the shell handed it — the unpadded content box for the
  // list and the thread, an ordinary padded page for the clean-up screen. The shell
  // says which, through its Outlet; asking isFullBleedRoute again here would be the
  // same answer arrived at twice, with nothing keeping the two honest.
  const { fullBleed } = useOutletContext<ShellLayout>()

  if (!online) return <ChatOffline fill={fullBleed} />

  return (
    <Routes>
      <Route index element={<ChatLayout />} />
      <Route path="uklid" element={<UklidPage />} />
      <Route path=":id" element={<ChatLayout />} />
    </Routes>
  )
}

function ChatLayout() {
  const { id } = useParams<{ id: string }>()
  const [membersOpen, setMembersOpen] = useState(false)
  const navigate = useNavigate()
  useOpenSomething(id)
  // ⚠ The room this tab has open can be taken away by somebody else — trashed or
  // purged — and `gone` is the frame that says so. Leaving the route is the point of
  // that flag; dropping the thread cache and staying put left the member on a
  // header, a composer and a "Konverzace nebyla nalezena." with no way to read it as
  // anything but a failure. The list is where they belong once the room is not
  // theirs. It lives here rather than in applyChatFrame because only the route knows
  // which conversation is open.
  useLeaveWhenGone(id, () => navigate(routes.chat))

  return (
    /* ⚠ TWO HEIGHTS, AND NEITHER IS A CARD'S. The shell hands chat its content box
       whole (isFullBleedRoute) rather than padding a page around it, which the
       artboards draw on both frames: the panes reach the shell's edges, the list
       pane's own border-right is the only line between them, and there is no rounded
       frame to inset. From 768 up that box IS the viewport minus nothing, so
       `h-full` is the whole answer; below it the shell still has a header above and
       thumb tabs below, which is what --chat-chrome measures (theme/globals.css).
       The old `lg:h-[calc(100dvh-4rem)]` was the md:py-8 that is now gone. */
    <div className="h-[calc(100dvh-var(--chat-chrome))] md:h-full">
      {/* 312px is the artboard's list pane, not a rounded 320: with the card frame
          and the shell's padding gone, that pane's border-right is the only line
          between the two and it now lands where the design puts it. */}
      <div className="grid h-full min-h-0 overflow-hidden bg-s1 lg:grid-cols-[312px_1fr]">
        {/* Below 1024 exactly one pane is on screen: the list, or the thread. The
            hidden pane is not rendered at all rather than hidden with CSS, so a
            phone never fetches a thread nobody is looking at.

            ⚠ `min-h-0` ON BOTH PANES IS LOAD-BEARING (v10 review). A grid item
            defaults to `min-height: auto`, so each pane grew to its CONTENT — 3426
            px of thread inside a 656 px grid — and `overflow-hidden` clipped the
            difference. The inner `overflow-y-auto` box never scrolled because it
            was never smaller than what it held: the newest messages and the
            composer were simply cut off the bottom of the page, with no scrollbar
            to say so. The two panes are where the height has to stop. */}
        {/* ⚠ `min-w-0` IS LOAD-BEARING HERE TOO, and it is the width twin of the
            `min-h-0` note above — found by opening the page at 375 px, not by any
            test (v10.1). `<main>` has carried it since v10; the aside did not, and
            nothing noticed while its second line said "5 členů". A grid item
            defaults to `min-width: auto`, which resolves to its MIN-CONTENT width,
            and a `truncate` line is `white-space: nowrap` — so the moment the row's
            second line became a preview of a real sentence, the pane's minimum
            width became that sentence's. It measured 415 px inside a 375 px grid
            and `overflow-hidden` clipped the difference: the ＋ button lost its
            right half, every row's timestamp read "21" instead of "21:45", and
            nothing scrolled to say so. The truncation could not help — it shortens
            text to the width it is GIVEN, and the pane was being given the width of
            the untruncated text. */}
        <aside className={id ? 'hidden min-h-0 min-w-0 lg:flex lg:flex-col lg:border-r lg:border-border' : 'flex min-h-0 min-w-0 flex-col lg:border-r lg:border-border'}>
          {/* ⚠ THE MODULE-TOTAL WARNING SITS ON THE LIST, NOT IN A THREAD. It is
              about the household's bucket, so a member meets it where they choose a
              room rather than inside one — and it never appears beside the
              per-conversation warning, because two banners about the same bytes is
              one banner too many (StorageWarning decides which). */}
          <StorageWarning />
          <div className="min-h-0 flex-1">
            <ConversationList activeID={id} />
          </div>
        </aside>

        <main className={id ? 'flex min-h-0 min-w-0 flex-col' : 'hidden min-h-0 min-w-0 lg:flex lg:flex-col'}>
          {id && <StorageWarning conversationID={id} />}
          {id ? (
            /* ⚠ `key` IS LOAD-BEARING, NOT A LIST HINT (v10 review). At ≥1024 both
               panes are on screen, so /chat/a → /chat/b matches the SAME `:id`
               route: React reconciles ThreadView instead of remounting it and every
               piece of per-conversation state survives the switch. The composer's
               draft is the one that costs — a half-typed message follows you into
               the next room and Enter posts it there, with no unsend, because a
               delete leaves a tombstone everybody has already seen. `replyTo` and
               `editing` still point at the old room's messages, and `atBottom`
               carries a scrolled-up thread's answer into a fresh one, so B opens at
               its oldest loaded message — the bug the pane heights were fixed for,
               by another route. Keying on the id makes a different conversation a
               different component, which is what it is. */
            /* ⚠ `min-h-0 flex-1` RATHER THAN `h-full`. The banner above is a
               sibling now, so a child sized to the FULL pane would overflow it by
               exactly the banner's height — and the thread's own scroll box is
               inside, so the overflow would land on the composer. */
            <div className="min-h-0 flex-1">
              <ThreadView key={id} conversationID={id} onOpenMembers={() => setMembersOpen(true)} />
            </div>
          ) : (
            <div className="grid h-full flex-1 place-items-center p-6 text-center">
              <div className="max-w-[440px]">
                <div className="mb-2 text-lg font-extrabold">{cs.chat.emptyAllTitle}</div>
                <p className="text-[13.5px] leading-relaxed text-muted text-pretty">
                  {cs.chat.pickPrompt}
                </p>
              </div>
            </div>
          )}
        </main>
      </div>

      {id && (
        /* ⚠ KEYED FOR THE REASON ThreadView IS (v10 review). This panel holds
           `removing` — the member row a confirmation dialog is armed with — and it
           was the one component here React was free to reconcile across a room
           switch. Arm a removal in room A, change route (browser back, a push deep
           link, the `gone` frame's navigate), and the dialog is still open over a
           panel that now says room B: confirming called removeMember(B, petra),
           taking Petra out of a conversation nobody asked about. `membersOpen`
           lives out here and deliberately survives — a panel left open should
           follow you into the next room; it is the ARMED ROW that must not. */
        <MembersPanel key={id} conversationID={id} open={membersOpen} onOpenChange={setMembersOpen} />
      )}
    </div>
  )
}

/**
 * The desktop never lands on an empty pane again (v10.1, D269).
 *
 * ⚠ IT REMEMBERS, AND IT ONLY ACTS AT ≥1024. At that width the list and the thread
 * are both on screen (D262), so `/chat` was a permanent half-page telling the member
 * to click something — every time they opened the module, including the ones where
 * they were coming back to the room they had left five minutes ago. Below 1024 the
 * list IS the screen and there is nothing empty to fill; redirecting there would
 * make the conversation list unreachable, because the thread's back arrow goes to
 * `/chat` and would bounce straight back into the thread.
 *
 * ⚠ THE MEDIA QUERY IS READ, NOT SUBSCRIBED TO. A member who drags a window from
 * 900 px to 1300 px has a conversation list on screen and is looking at it; yanking
 * them into a room because the viewport crossed a threshold is the app taking a
 * decision they did not make. The check happens when the list arrives and when the
 * route changes, which are the two moments a member has actually asked for
 * something.
 *
 * ⚠ AND THE NAVIGATION IS A REPLACE. A push would put the empty `/chat` in the
 * history, so browser-back from the thread would land on it and be redirected
 * forward again — the trap, rebuilt one width up.
 */
function useOpenSomething(openID: string | undefined): void {
  const navigate = useNavigate()
  const { identity } = useAuth()
  const me = identity?.userId ?? ''
  const active = useConversations('active')
  const rooms = active.data?.items

  // Remembering is the half that runs on every width: a phone member's last room is
  // what the desktop will open tomorrow, and the two share the record.
  useEffect(() => {
    if (openID) rememberLastOpened(me, openID)
  }, [me, openID])

  useEffect(() => {
    if (openID || !rooms) return
    if (!window.matchMedia?.('(min-width: 1024px)').matches) return
    const target = pickConversationToOpen(rooms, readLastOpened(me))
    // ⚠ A MEMBER IN NO CONVERSATION GETS THE EMPTY STATE, which says what the module
    // is. `pickConversationToOpen` answers '' for that rather than guessing, and
    // this is where that answer is respected.
    if (target) navigate(`${routes.chat}/${target}`, { replace: true })
  }, [openID, rooms, me, navigate])
}

/**
 * The offline state — a deliberate departure from every other module.
 *
 * ⚠ EVERY OTHER SCREEN IN HOME RENDERS READ-ONLY FROM CACHE WHEN THE NETWORK IS
 * GONE. Chat renders this instead, because chat is excluded from the PWA persister
 * entirely: message bodies and other members' display names on a shared laptop's
 * disk are worth less than the offline convenience, and v9 already established the
 * threat model — a laptop in the kitchen gets used by more than one person.
 *
 * So the copy has to read as a CHOICE rather than as a failure to load. "Zprávy se
 * do zařízení neukládají" says what was decided; "nepodařilo se načíst" would say
 * something untrue.
 */
function ChatOffline({ fill }: { fill: boolean }) {
  return (
    <div
      className={cn(
        'grid min-h-[340px] place-items-center px-6 py-10 text-center',
        // ⚠ NO 100dvh ARITHMETIC HERE, UNLIKE ChatLayout, and the reason is that
        // this is the one chat screen that renders WHILE THE OFFLINE BANNER IS UP —
        // they have the same trigger. --chat-chrome counts the header and the thumb
        // bar; the banner is a third strip above both, and its height depends on how
        // its two sentences wrap (73 px at 375 px), so a constant cannot know it.
        // Subtracting only --chat-chrome left this box 16 px taller than the
        // viewport: a strip that scrolled with nothing in it, which is the exact
        // defect the panes were just fixed for. A short centred block does not need
        // a viewport below 768. From 768 up `<main>` is a flex child with a real
        // height and the banner is already accounted for inside it, so `h-full`
        // fills what is left and centres in that.
        fill && 'md:h-full',
      )}
    >
      <div className="max-w-[460px]">
        {/* ⚠ A NEUTRAL SURFACE, NOT THE INFORMATIONAL BLUE. `--info` is v9's register
            for a fact the app is telling you about your data; this is the module
            declining to hold any, which is not a state of anything the member did.
            The plain s2 tile is the design's, and it keeps the screen from reading
            as a condition to be cleared. */}
        <div className="mx-auto mb-3.5 grid h-12 w-12 place-items-center rounded-[13px] border border-border bg-s2 text-muted">
          <WifiOff size={22} aria-hidden />
        </div>
        <div className="mb-2 text-[19px] font-extrabold tracking-tight">
          {cs.chat.offlineTitle}
        </div>
        <p className="text-[13.5px] leading-relaxed text-muted text-pretty">
          {cs.chat.offlineBody}
        </p>
      </div>
    </div>
  )
}
