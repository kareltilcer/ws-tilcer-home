import { cn } from '@/lib/utils'
import { cs } from '@/i18n/cs'
import { count, PLURAL } from '@/i18n/plural'
import { fmtDate, fmtStorageBytes } from '@/i18n/format'
import type { StorageChat } from './api/types'

/**
 * Administrace → Úložiště, the chat block (FR-V10-16, D240/D254).
 *
 * ⚠ NOTHING HERE IS CLICKABLE, AND THAT IS THE SPECIFICATION. No thread, no message,
 * no attachment list, no clean-up link. An admin sees which room is heavy and asks
 * its members — clean-up is member-scoped (D241), and the only two chat verbs an
 * admin has over a room they are not in are restore and purge, neither of which
 * opens it (D255).
 *
 * ⚠ SO THE ABSENCE IS EXPLAINED. A table of names with nothing clickable reads as
 * broken unless the page says why, which is what `chatNoWayIn` is for. Adding a link
 * here reverses a decision; it does not fix an oversight.
 *
 * ⚠ AND A TRASHED ROOM IS STILL COUNTED (D254). Its bytes are still in R2 and this
 * page's premise is that its figures sum, so it is listed with its koš marker rather
 * than subtracted — *Smazat natrvalo* is what exists so that never traps anyone.
 */
export function ChatStorageBlock({ chat }: { chat: StorageChat }) {
  return (
    <section className="rounded-2xl border border-border bg-s1 p-4">
      <div className="mb-3 flex flex-wrap items-baseline gap-x-3 gap-y-1">
        <h3 className="text-base font-extrabold tracking-tight">{cs.storage.chatTitle}</h3>
        <span className="font-mono text-sm tabular-nums">
          {fmtStorageBytes(chat.total_bytes)}
        </span>
        <span className="text-[12px] text-muted">
          {cs.storage.chatOfLimit(`${chat.threshold_total_mb} MB`)}
        </span>
        {chat.exceeded && (
          <span className="rounded-full bg-attention-soft px-2 py-0.5 text-[11px] font-semibold text-attention">
            {cs.storage.chatOverLimit}
          </span>
        )}
        {/* ⚠ *Nezálohováno* is a NORMAL ROW here, not a warning (D229): chat blobs
            are deliberately excluded from the mirror, and rendering the gap as an
            empty cell would read as a figure nobody measured. */}
        {!chat.mirrored && (
          <span className="font-mono text-[11px] text-subtle">{cs.storage.chatNotBackedUp}</span>
        )}
      </div>

      <p className="mb-3 text-[12px] text-muted text-pretty">{cs.storage.chatNoWayIn}</p>

      {chat.conversations.length === 0 ? (
        <p className="text-sm text-muted">{cs.storage.chatEmpty}</p>
      ) : (
        <div className="om-scroll overflow-x-auto">
          <table className="w-full min-w-[34rem] text-sm">
            <thead>
              <tr className="border-b border-border text-left text-[12px] text-muted">
                <th className="py-1.5 pr-3 font-semibold">{cs.storage.chatConversation}</th>
                <th className="py-1.5 pr-3 text-right font-semibold">{cs.storage.chatBytes}</th>
                <th className="py-1.5 pr-3 text-right font-semibold">{cs.storage.chatObjects}</th>
                <th className="py-1.5 text-right font-semibold">{cs.storage.chatMembers}</th>
              </tr>
            </thead>
            <tbody>
              {chat.conversations.map((c) => (
                <tr key={c.id} className="border-b border-border/50 last:border-0">
                  <td className="py-1.5 pr-3">
                    <span className="font-semibold">{c.name}</span>
                    {c.over_limit && (
                      <span className="ml-2 rounded-full bg-attention-soft px-1.5 py-0.5 text-[10px] font-semibold text-attention">
                        {cs.storage.chatOverLimit}
                      </span>
                    )}
                    {c.trashed_at && (
                      <span className="ml-2 text-[11px] text-subtle">
                        {cs.storage.chatTrashed}
                        {c.purge_after && ` · ${cs.storage.chatPurgeAfter} ${fmtDate(new Date(c.purge_after))}`}
                      </span>
                    )}
                  </td>
                  <td
                    className={cn(
                      'py-1.5 pr-3 text-right font-mono tabular-nums',
                      c.over_limit && 'text-attention',
                    )}
                  >
                    {fmtStorageBytes(c.bytes)}
                  </td>
                  <td className="py-1.5 pr-3 text-right font-mono text-[12px] tabular-nums text-muted">
                    {c.objects === null ? '—' : c.objects}
                  </td>
                  <td className="py-1.5 text-right text-[12px] text-muted">
                    {count(c.members, PLURAL.members)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  )
}
