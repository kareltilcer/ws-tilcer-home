import { useState } from 'react'
import { Route, Routes, useParams } from 'react-router-dom'
import { WifiOff } from 'lucide-react'
import { cs } from '@/i18n/cs'
import { useOnline } from '@/platform/pwa/offline'
import { ConversationList } from './ConversationList'
import { ThreadView } from './ThreadView'
import { MembersPanel } from './MembersPanel'
import { useChatLiveSync } from './api/hooks'

/**
 * Chat — the eleventh module, and the first one the household does not read in full.
 *
 * ⚠ TWO PANES AT ≥1024, STACKED BELOW (D262). `/chat/{id}` renders BOTH on desktop,
 * so a member never loses the unread counts to read a message; below 1024 the thread
 * is a route push and browser-back returns to the list.
 *
 * ⚠ AND THIS MODULE IS NOT IN THE NAV YET. The route is registered so `/chat` works,
 * but AppShell keeps its four thumb tabs until PR 3 lands attachments — a chat the
 * household meets before it can send a photo is a chat they will try to send a photo
 * with. The demotion of Okno do budoucnosti (D260) rides with that same PR.
 */
export function ChatPage() {
  useChatLiveSync()
  const online = useOnline()

  if (!online) return <ChatOffline />

  return (
    <Routes>
      <Route index element={<ChatLayout />} />
      <Route path=":id" element={<ChatLayout />} />
    </Routes>
  )
}

function ChatLayout() {
  const { id } = useParams<{ id: string }>()
  const [membersOpen, setMembersOpen] = useState(false)

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
        <aside className={id ? 'hidden min-h-0 lg:block lg:border-r lg:border-border' : 'block min-h-0 lg:border-r lg:border-border'}>
          <ConversationList activeID={id} />
        </aside>

        <main className={id ? 'block min-h-0 min-w-0' : 'hidden min-h-0 min-w-0 lg:block'}>
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
            <ThreadView key={id} conversationID={id} onOpenMembers={() => setMembersOpen(true)} />
          ) : (
            <div className="grid h-full place-items-center p-6 text-center">
              <p className="max-w-xs text-sm text-muted text-pretty">{cs.chat.pickPrompt}</p>
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
    <div className="grid min-h-[340px] place-items-center text-center">
      <div className="max-w-sm">
        <div className="mx-auto mb-4 grid h-14 w-14 place-items-center rounded-lg bg-info-soft text-info">
          <WifiOff size={26} aria-hidden />
        </div>
        <div className="mb-1.5 text-lg font-bold">{cs.chat.offlineTitle}</div>
        <p className="text-sm text-muted text-pretty">{cs.chat.offlineBody}</p>
      </div>
    </div>
  )
}
