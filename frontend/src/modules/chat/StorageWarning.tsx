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
  // Inside a thread, the room's own limit is the relevant one; on the list, the
  // module total is. A member is never shown both at once — two warnings about the
  // same bytes is one warning too many.
  const overRoom = room?.over_limit ?? false
  if (!overRoom && !data.total_exceeded) return null

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
    <div
      role="status"
      className="flex flex-wrap items-center gap-x-3 gap-y-1 border-b border-attention/30 bg-attention-soft px-3 py-2 text-xs text-attention lg:px-4"
    >
      <AlertTriangle size={14} className="flex-none" aria-hidden />
      <span className="min-w-0 flex-1 text-pretty">{message}</span>
      {data.can_clean_up ? (
        <Link
          to={routes.chatUklid}
          className="flex-none font-bold underline underline-offset-2 hover:no-underline"
        >
          {cs.chat.cleanupLink}
        </Link>
      ) : (
        <span className="flex-none opacity-90">{cs.chat.cleanupNotForReaders}</span>
      )}
    </div>
  )
}
