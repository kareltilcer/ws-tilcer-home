import { useState } from 'react'
import { Route, Routes, useNavigate, useParams } from 'react-router-dom'
import { WifiOff } from 'lucide-react'
import { cs } from '@/i18n/cs'
import { routes } from '@/app/routes'
import { useOnline } from '@/platform/pwa/offline'
import { ConversationList } from './ConversationList'
import { ThreadView } from './ThreadView'
import { MembersPanel } from './MembersPanel'
import { StorageWarning } from './StorageWarning'
import { UklidPage } from './UklidPage'
import { useChatLiveSync, useLeaveWhenGone } from './api/hooks'

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

  if (!online) return <ChatOffline />

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
  // ⚠ The room this tab has open can be taken away by somebody else — trashed or
  // purged — and `gone` is the frame that says so. Leaving the route is the point of
  // that flag; dropping the thread cache and staying put left the member on a
  // header, a composer and a "Konverzace nebyla nalezena." with no way to read it as
  // anything but a failure. The list is where they belong once the room is not
  // theirs. It lives here rather than in applyChatFrame because only the route knows
  // which conversation is open.
  useLeaveWhenGone(id, () => navigate(routes.chat))

  return (
    /* --chat-chrome is declared in theme/globals.css, beside the other layout
       tokens — it is the header plus the thumb-tab bar below 1024. */
    <div className="h-[calc(100dvh-var(--chat-chrome))] lg:h-[calc(100dvh-4rem)]">
      <div className="grid h-full min-h-0 overflow-hidden rounded-lg border border-border bg-s1 lg:grid-cols-[320px_1fr]">
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
        <aside className={id ? 'hidden min-h-0 lg:flex lg:flex-col lg:border-r lg:border-border' : 'flex min-h-0 flex-col lg:border-r lg:border-border'}>
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
function ChatOffline() {
  return (
    <div className="grid min-h-[340px] place-items-center px-6 text-center">
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
