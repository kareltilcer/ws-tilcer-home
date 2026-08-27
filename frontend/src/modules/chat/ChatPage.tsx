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
    <div className="h-[calc(100dvh-var(--chat-chrome,8rem))] lg:h-[calc(100dvh-4rem)]">
      <div className="grid h-full min-h-0 overflow-hidden rounded-lg border border-border bg-s1 lg:grid-cols-[320px_1fr]">
        {/* Below 1024 exactly one pane is on screen: the list, or the thread. The
            hidden pane is not rendered at all rather than hidden with CSS, so a
            phone never fetches a thread nobody is looking at. */}
        <aside className={id ? 'hidden lg:block lg:border-r lg:border-border' : 'block lg:border-r lg:border-border'}>
          <ConversationList activeID={id} />
        </aside>

        <main className={id ? 'block min-w-0' : 'hidden min-w-0 lg:block'}>
          {id ? (
            <ThreadView conversationID={id} onOpenMembers={() => setMembersOpen(true)} />
          ) : (
            <div className="grid h-full place-items-center p-6 text-center">
              <p className="max-w-xs text-sm text-muted text-pretty">{cs.chat.pickPrompt}</p>
            </div>
          )}
        </main>
      </div>

      {id && (
        <MembersPanel conversationID={id} open={membersOpen} onOpenChange={setMembersOpen} />
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
