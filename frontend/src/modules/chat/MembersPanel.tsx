import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import * as Dialog from '@radix-ui/react-dialog'
import { Plus, X } from 'lucide-react'
import { cn, initial } from '@/lib/utils'
import { cs } from '@/i18n/cs'
import { count, PLURAL } from '@/i18n/plural'
import { fmtDate } from '@/i18n/format'
import { Button, Spinner } from '@/components/ui/ui'
import { ResponsiveModal } from '@/components/ui/modal'
import { useIsDesktop } from '@/hooks/useMediaQuery'
import { useAuth } from '@/app/auth'
import { DirectoryPicker } from './DirectoryPicker'
import {
  useAddMember,
  useConversation,
  useDirectory,
  useMembers,
  useRemoveMember,
} from './api/hooks'
import type { ConversationMember } from './api/types'

/**
 * The members panel — a right-hand drawer at the desk, a sheet under a thumb.
 *
 * ⚠ NOT A THIRD COLUMN (D262). The list is consulted when somebody is added or when
 * the floor needs explaining, not read continuously, and a permanent column would
 * cost the thread the width it actually needs at 1024.
 *
 * ⚠ AND NOT A CENTRED DIALOG EITHER. It is a list ABOUT the thread behind it — who
 * is in this room, and since when — so it is drawn beside the thread rather than
 * over the middle of it, and the thread stays legible while it is open. That is what
 * lets somebody check a date against the messages they are looking at.
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
  const [adding, setAdding] = useState(false)
  const directory = useDirectory(open && adding)
  const add = useAddMember(conversationID)
  const { identity } = useAuth()
  const me = identity?.userId ?? ''
  const desktop = useIsDesktop()

  const [removing, setRemoving] = useState<ConversationMember | null>(null)

  // The picker is closed again whenever the panel is: leaving it open would reopen
  // the panel already unfolded onto a list nobody asked for.
  useEffect(() => {
    if (!open) setAdding(false)
  }, [open])

  const isDefault = conversation.data?.kind === 'default'
  const present = new Set((members.data?.items ?? []).map((m) => m.user_id))
  const addable = (directory.data?.items ?? []).filter((d) => !present.has(d.user_id))
  // ⚠ THE LAST MEMBER CANNOT LEAVE, AND THE SERVER IS THE GUARD (v10 review) — this
  // is only what stops somebody meeting it as an error message, exactly as hiding
  // Všichni's delete entry does one file over. A group emptied of members is a live
  // row that has left every read there is: not trashed, so absent from the koš, and
  // every listing is a membership join, so nobody — not even an admin — can reach
  // it again. Deleting the conversation is the verb that was wanted.
  const soleMember = (members.data?.items.length ?? 0) <= 1
  const total = members.data?.items.length ?? conversation.data?.member_count ?? 0

  return (
    <>
      <Dialog.Root open={open} onOpenChange={onOpenChange}>
        <Dialog.Portal>
          <Dialog.Overlay className="fixed inset-0 z-40 bg-black/40" />
          <Dialog.Content
            className={cn(
              'fixed z-50 flex flex-col bg-s1 text-fg shadow-[var(--shadow)] focus:outline-none',
              desktop
                ? 'bottom-0 right-0 top-0 w-[min(340px,92vw)] border-l border-border-strong'
                : 'inset-x-0 bottom-0 top-14 rounded-t-[20px] border-t border-border-strong',
            )}
          >
            <div className="flex flex-none items-center gap-2.5 border-b border-border px-4 py-3">
              <div className="min-w-0 flex-1">
                <Dialog.Title className="truncate text-base font-extrabold">
                  {cs.chat.word.members}
                </Dialog.Title>
                <p className="truncate text-[11.5px] text-muted">
                  {count(total, PLURAL.members)}
                </p>
              </div>
              <Dialog.Close
                aria-label={cs.chat.close}
                className="grid h-11 w-11 flex-none place-items-center rounded-[11px] border border-border bg-s2 text-muted hover:text-fg lg:h-8 lg:w-8 lg:rounded-[9px]"
              >
                <X size={16} aria-hidden />
              </Dialog.Close>
            </div>

            {/* ⚠ THE LEAD SAYS A DIFFERENT THING IN VŠICHNI. The household room has
                everybody from its beginning (D258), so a date beside each name would
                imply a floor that is not there — and the sentence explains the
                absence of one rather than leaving it to be noticed. */}
            <p className="flex-none border-b border-border px-4 py-3 text-[12.5px] leading-relaxed text-muted text-pretty">
              {isDefault ? cs.chat.membersLeadEveryone : cs.chat.membersLeadGroup}
            </p>

            <div className="min-h-0 flex-1 overflow-y-auto om-scroll px-2.5 py-2">
              {members.isPending && (
                <div className="grid place-items-center py-8 text-muted">
                  <Spinner />
                </div>
              )}
              <ul className="flex flex-col gap-0.5">
                {members.data?.items.map((m) => (
                  <li
                    key={m.user_id}
                    className="flex min-h-14 items-center gap-3 rounded-[10px] px-2 py-2"
                  >
                    <span className="grid h-[34px] w-[34px] flex-none place-items-center rounded-full bg-s3 text-[13px] font-bold text-muted lg:h-[30px] lg:w-[30px] lg:text-xs">
                      {initial(m.display_name)}
                    </span>
                    <span className="min-w-0 flex-1">
                      <span className="flex items-center gap-1.5">
                        <span className="min-w-0 truncate text-[13.5px] font-bold">
                          {m.display_name}
                        </span>
                        {m.user_id === me && (
                          <span className="flex-none font-mono text-[9.5px] uppercase text-subtle">
                            {cs.chat.membersYou}
                          </span>
                        )}
                      </span>
                      <span className="mt-0.5 block truncate font-mono text-[10.5px] text-muted">
                        <FloorLabel member={m} />
                      </span>
                    </span>
                    {/* ⚠ Nobody is removed from Všichni and nobody leaves it (D219):
                        it is the one conversation whose membership IS the household. */}
                    {!isDefault && !soleMember && (
                      <Button
                        size="sm"
                        variant="ghost"
                        className="min-h-11 flex-none border-border lg:min-h-7"
                        onClick={() => setRemoving(m)}
                      >
                        {m.user_id === me ? cs.chat.leave : cs.chat.word.removeMember}
                      </Button>
                    )}
                  </li>
                ))}
              </ul>
            </div>

            {!isDefault && (
              <div className="flex flex-none flex-col gap-2.5 border-t border-border px-4 pb-5 pt-3">
                {!adding ? (
                  <Button
                    variant="primary"
                    className="min-h-12 w-full lg:min-h-10"
                    onClick={() => setAdding(true)}
                  >
                    <Plus size={16} aria-hidden />
                    {cs.chat.word.addMember}
                  </Button>
                ) : (
                  <DirectoryPicker
                    directory={directory}
                    addable={addable}
                    label={cs.chat.word.addMember}
                    renderChip={(d) => (
                      <Button
                        key={d.user_id}
                        size="sm"
                        variant="secondary"
                        className="min-h-11 lg:min-h-8"
                        // ⚠ THE ONE THAT WAS PRESSED, not all of them (v10 review).
                        // Every button read the shared mutation's isPending, so
                        // adding one person put the whole picker into the loading
                        // state and the member could not tell which add was in
                        // flight. `variables` is the id this mutation was called
                        // with.
                        loading={add.isPending && add.variables === d.user_id}
                        onClick={() => add.mutate(d.user_id)}
                      >
                        {d.display_name}
                      </Button>
                    )}
                  />
                )}
                {/* ⚠ The directory is a LOGIN HISTORY projected from sessions — Home
                    has no user table — so somebody who has never logged in is simply
                    not here. The note says so rather than letting the gap look like a
                    bug, and it is visible BEFORE the picker opens, where it answers
                    "who can I add" instead of explaining a short list after the fact. */}
                <p className="text-[11.5px] leading-relaxed text-muted text-pretty">
                  {cs.chat.directoryHint}
                </p>
              </div>
            )}

            {isDefault && (
              <p className="flex-none border-t border-border px-4 pb-5 pt-3 text-[11.5px] text-muted text-pretty">
                {cs.chat.everyoneCannotLeave}
              </p>
            )}
          </Dialog.Content>
        </Dialog.Portal>
      </Dialog.Root>

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
function FloorLabel({ member }: { member: ConversationMember }) {
  // ⚠ THE SERVER ANSWERS THIS; IT IS NOT DERIVED FROM THE TIMESTAMPS (v10 review).
  // The floor is an id bound, and comparing `effective_from` to the room's
  // `created_at` was a second spelling of it that disagreed — somebody added to a
  // room with no messages yet gets `effective_from = now` over an EMPTY bound, so
  // the clock claimed a gap where the server says they read all of it.
  if (member.reads_from_beginning) {
    return <>{cs.chat.memberSinceBeginning}</>
  }
  return (
    <>
      {cs.chat.memberSince} {fmtDate(new Date(member.effective_from))}
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
  const navigate = useNavigate()
  // ⚠ LEAVING NAVIGATES AWAY, REMOVING SOMEBODY ELSE DOES NOT. The room is gone
  // from every read the moment the removal commits, so staying on /chat/{id} leaves
  // the member looking at a header, a thread and a usable composer belonging to a
  // conversation that now 404s — with this panel open on top of a members list that
  // is already stale. DeleteDialog takes the same exit for the same reason.
  const done = () => {
    onClose()
    if (isSelf) navigate('/chat')
  }
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
            onClick={() => member && remove.mutate(member.user_id, { onSuccess: done })}
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
