import { Link } from 'react-router-dom'
import { AlertTriangle } from 'lucide-react'
import { cs } from '@/i18n/cs'
import { fmtStorageBytes } from '@/i18n/format'
import { routes } from '@/app/routes'
import { useChatStorage } from './api/hooks'

/**
 * The in-module storage warning (FR-V10-12, D237/D241).
 *
 * ⚠ INFORMATIONAL, NEVER DESTRUCTIVE. Nothing is blocked, no upload fails, there is
 * no quota and nobody has done anything wrong — so it uses v9's promoted
 * `--attention` register and never the delete red. Two thresholds can fire it: the
 * module total, and any conversation the member belongs to being over its own limit.
 *
 * ⚠ IT IS A CARD INSIDE THE PANE, NOT A BAR ACROSS THE TOP OF IT. A full-bleed
 * strip is the shape this app uses for something that has gone wrong; the outlined
 * card is the register v9 gave `--attention`, and the whole point of the copy below
 * is that nothing has.
 *
 * ⚠ AND THE TAIL SENTENCE IS PART OF THE WARNING, not decoration. `--attention` says
 * "look at this"; *"Nic se tím neblokuje"* is what keeps a member from reading it as
 * a quota they have hit.
 *
 * ⚠ AND THE LINK IS NOT RENDERED FOR A MEMBER WHO CANNOT USE IT (D241). The gate is
 * member ∧ (editor|admin), the client holds only the role half, and the server
 * answers the intersection in `can_clean_up` — so a reader sees the warning and the
 * sentence explaining who can act on it, rather than a button that would 403 them.
 * That asymmetry is recorded, not hidden: a reader can fill storage they can never
 * clean.
 */
export function StorageWarning({ conversationID }: { conversationID?: string }) {
  const storage = useChatStorage()
  const data = storage.data
  if (!data) return null

  const room = conversationID
    ? data.conversations.find((c) => c.id === conversationID)
    : undefined
  const overRoom = room?.over_limit ?? false

  // ⚠ THE THREAD PANE SHOWS THE ROOM'S LIMIT AND NOTHING ELSE (FR-V10-12). The
  // module total's homes are Administrace and the conversation list; falling
  // through to it here put the SAME banner in both panes at ≥1024, side by side,
  // about the same bytes — and the comment that used to sit here claimed a member is
  // never shown both at once while the layout showed exactly that.
  //
  // Below 1024 the list is the route a member lands on, so the total still reaches
  // them; a thread they opened is about that room.
  if (conversationID) {
    if (!overRoom) return null
  } else if (!data.total_exceeded) {
    return null
  }

  const message = overRoom
    ? cs.chat.storageOverConversation(
        room?.name ?? '',
        fmtStorageBytes(room?.bytes ?? null),
        `${data.threshold_conversation_mb} MB`,
      )
    : cs.chat.storageOverTotal(
        fmtStorageBytes(data.total_bytes),
        `${data.threshold_total_mb} MB`,
      )

  return (
    <div className="flex-none px-3 pt-2.5 lg:px-4 lg:pt-3">
      <div
        role="status"
        className="flex items-start gap-2.5 rounded-[12px] border border-attention bg-attention-soft px-3 py-2.5"
      >
        <AlertTriangle size={14} className="mt-0.5 flex-none text-attention" aria-hidden />
        {/* ⚠ ONE LINK, MOVED BY THE FLEX DIRECTION — not two with a breakpoint
            hiding one. A `lg:hidden` twin is still in the accessibility tree, so a
            screen reader meets *Uklidit úložiště* twice on a warning whose whole job
            is to be read once and understood. */}
        <div className="flex min-w-0 flex-1 flex-col gap-2.5 lg:flex-row lg:items-start lg:gap-3">
          <div className="min-w-0 flex-1">
            <div className="mb-0.5 text-[13px] font-bold text-attention">
              {cs.chat.storageWarnWord}
            </div>
            <p className="text-[12.5px] leading-normal text-pretty">{message}</p>
            <p className="mt-1 text-xs text-muted text-pretty">{cs.chat.storageWarnTail}</p>
            {!data.can_clean_up && (
              <p className="mt-1.5 text-xs text-muted text-pretty">
                {cs.chat.cleanupNotForReaders}
              </p>
            )}
          </div>
          {data.can_clean_up && (
            <Link
              to={routes.chatUklid}
              className="inline-flex min-h-10 flex-none items-center self-start rounded-lg border border-attention px-3 text-[12.5px] font-bold text-attention hover:bg-attention/10 lg:min-h-8"
            >
              {cs.chat.cleanupLink}
            </Link>
          )}
        </div>
      </div>
    </div>
  )
}
