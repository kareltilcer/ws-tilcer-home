import { useState } from 'react'
import { UserMinus } from 'lucide-react'
import { cs } from '@/i18n/cs'
import { Button, Spinner } from '@/components/ui/ui'
import { ResponsiveModal } from '@/components/ui/modal'
import { useAuth } from '@/app/auth'
import {
  useAddMember,
  useConversation,
  useDirectory,
  useMembers,
  useRemoveMember,
  useSetMuted,
} from './api/hooks'
import type { ConversationMember } from './api/types'

/**
 * The members panel — a sheet on mobile, a dialog on desktop.
 *
 * ⚠ NOT A THIRD COLUMN (D262). The list is consulted when somebody is added or when
 * the floor needs explaining, not read continuously, and a permanent column would
 * cost the thread the width it actually needs at 1024.
 *
 * ⚠ EVERY ROW SHOWS `effective_from`, which is the second of the floor's three
 * surfaces. It is what lets the app say plainly that somebody added yesterday cannot
 * read last week — a fact the floor makes true and that nothing else in the UI would
 * explain.
 */
export function MembersPanel({
  conversationID,
  open,
  onOpenChange,
}: {
  conversationID: string
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const conversation = useConversation(conversationID)
  const members = useMembers(open ? conversationID : undefined)
  const directory = useDirectory(open)
  const add = useAddMember(conversationID)
  const setMuted = useSetMuted(conversationID)
  const { identity } = useAuth()
  const me = identity?.userId ?? ''

  const [removing, setRemoving] = useState<ConversationMember | null>(null)

  const isDefault = conversation.data?.kind === 'default'
  const present = new Set((members.data?.items ?? []).map((m) => m.user_id))
  const addable = (directory.data?.items ?? []).filter((d) => !present.has(d.user_id))

  return (
    <>
      <ResponsiveModal open={open} onOpenChange={onOpenChange} title={cs.chat.membersTitle}>
        {members.isPending && (
          <div className="grid place-items-center py-8 text-muted">
            <Spinner />
          </div>
        )}

        <ul className="flex flex-col gap-1">
          {members.data?.items.map((m) => (
            <li key={m.user_id} className="flex items-center gap-3 rounded-md px-2 py-2 hover:bg-s2">
              <span className="min-w-0 flex-1">
                <span className="block truncate text-sm font-semibold">{m.display_name}</span>
                <span className="mt-0.5 block truncate text-xs text-muted">
                  <FloorLabel
                    effectiveFrom={m.effective_from}
                    conversationCreatedAt={conversation.data?.created_at}
                  />
                </span>
              </span>
              {/* ⚠ Nobody is removed from Všichni and nobody leaves it (D219): it is
                  the one conversation whose membership IS the household. */}
              {!isDefault && (
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={() => setRemoving(m)}
                  aria-label={m.user_id === me ? cs.chat.leave : cs.chat.word.removeMember}
                >
                  <UserMinus size={14} aria-hidden />
                </Button>
              )}
            </li>
          ))}
        </ul>

        {!isDefault && (
          <div className="mt-5 border-t border-border pt-4">
            <div className="mb-2 text-sm font-semibold">{cs.chat.word.addMember}</div>
            {directory.isPending && <Spinner />}
            {!directory.isPending && addable.length === 0 && (
              <p className="text-sm text-muted text-pretty">{cs.chat.directoryEmpty}</p>
            )}
            <div className="flex flex-wrap gap-2">
              {addable.map((d) => (
                <Button
                  key={d.user_id}
                  size="sm"
                  variant="secondary"
                  loading={add.isPending}
                  onClick={() => add.mutate(d.user_id)}
                >
                  {d.display_name}
                </Button>
              ))}
            </div>
            {/* ⚠ The directory is a LOGIN HISTORY projected from sessions — Home has
                no user table — so somebody who has never logged in is simply not
                here. The hint says so rather than letting the gap look like a bug. */}
            <p className="mt-2 text-xs text-muted text-pretty">{cs.chat.directoryHint}</p>
          </div>
        )}

        <div className="mt-5 border-t border-border pt-4">
          <label className="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={conversation.data?.muted ?? false}
              onChange={(e) => setMuted.mutate(e.target.checked)}
              className="h-4 w-4 accent-[var(--accent)]"
            />
            {cs.chat.word.mute}
          </label>
        </div>

        {isDefault && (
          <p className="mt-4 text-xs text-muted text-pretty">{cs.chat.everyoneCannotLeave}</p>
        )}
      </ResponsiveModal>

      <RemoveDialog
        conversationID={conversationID}
        member={removing}
        isSelf={removing?.user_id === me}
        onClose={() => setRemoving(null)}
      />
    </>
  )
}

/**
 * FloorLabel is the per-member floor, in words.
 *
 * A member whose floor is the conversation's own beginning reads all of it — that is
 * everyone in Všichni, and everyone who was present when a group was created — so
 * they get a different sentence rather than a date that means nothing to them.
 */
function FloorLabel({
  effectiveFrom,
  conversationCreatedAt,
}: {
  effectiveFrom: string
  conversationCreatedAt?: string
}) {
  if (conversationCreatedAt && effectiveFrom <= conversationCreatedAt) {
    return <>{cs.chat.memberSinceBeginning}</>
  }
  return (
    <>
      {cs.chat.memberSince} {dateFmt.format(new Date(effectiveFrom))}
    </>
  )
}

/**
 * The removal confirmation.
 *
 * ⚠ IT HAS TO MENTION THE GAP, because nothing afterwards will explain it: removing
 * deletes the membership row, re-adding writes a NEW floor, and the member is then
 * left with a permanent hole in the middle of a conversation they otherwise read in
 * full (D218). Their own messages stay — authorship does not depend on membership —
 * and the copy says that too, because the opposite is what people assume.
 */
function RemoveDialog({
  conversationID,
  member,
  isSelf,
  onClose,
}: {
  conversationID: string
  member: ConversationMember | null
  isSelf: boolean
  onClose: () => void
}) {
  const remove = useRemoveMember(conversationID)
  return (
    <ResponsiveModal
      open={!!member}
      onOpenChange={(o) => !o && onClose()}
      title={isSelf ? cs.chat.leaveTitle : cs.chat.removeTitle}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {cs.chat.cancel}
          </Button>
          <Button
            variant="danger"
            loading={remove.isPending}
            onClick={() => member && remove.mutate(member.user_id, { onSuccess: onClose })}
          >
            {isSelf ? cs.chat.leave : cs.chat.word.removeMember}
          </Button>
        </>
      }
    >
      <p className="text-sm text-pretty">{isSelf ? cs.chat.leaveBody : cs.chat.removeBody}</p>
      {member && !isSelf && (
        <p className="mt-3 text-sm font-semibold">{member.display_name}</p>
      )}
    </ResponsiveModal>
  )
}

const dateFmt = new Intl.DateTimeFormat('cs-CZ', { day: 'numeric', month: 'long', year: 'numeric' })
