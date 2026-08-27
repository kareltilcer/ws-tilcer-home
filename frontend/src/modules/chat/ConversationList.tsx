import { useState } from 'react'
import { Link } from 'react-router-dom'
import { Plus, Trash2 } from 'lucide-react'
import { cn } from '@/lib/utils'
import { cs } from '@/i18n/cs'
import { count, PLURAL } from '@/i18n/plural'
import { Button, Spinner } from '@/components/ui/ui'
import { ResponsiveModal } from '@/components/ui/modal'
import { Input } from '@/components/ui/ui'
import { useConversations, useCreateConversation, useRestoreConversation } from './api/hooks'
import type { Conversation } from './api/types'

/**
 * The conversation list — left pane at ≥1024, the whole screen below it.
 *
 * ⚠ WHICH CONVERSATION IS ON SCREEN MUST BE UNMISTAKABLE at 375 px in both themes.
 * The cost of getting it wrong is posting into the wrong room, and there is no
 * unsend — edit and delete leave a tombstone everybody has already seen. So the
 * selected row carries three cues, not one: an accent left rule, a raised surface,
 * and `aria-current`.
 */
export function ConversationList({ activeID }: { activeID?: string }) {
  const active = useConversations('active')
  const trashed = useConversations('trash')
  const [creating, setCreating] = useState(false)

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex items-center justify-between gap-2 border-b border-border px-4 py-3">
        <h2 className="text-sm font-bold tracking-tight">{cs.chat.listTitle}</h2>
        <Button size="sm" variant="secondary" onClick={() => setCreating(true)}>
          <Plus size={14} aria-hidden />
          {cs.chat.word.newConversation}
        </Button>
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto om-scroll">
        {active.isPending && (
          <div className="grid place-items-center py-10 text-muted">
            <Spinner />
          </div>
        )}

        {active.data?.items.length === 0 && !active.isPending && (
          <div className="px-4 py-8 text-center">
            <div className="mb-1.5 text-sm font-bold">{cs.chat.emptyTitle}</div>
            <p className="text-sm text-muted text-pretty">{cs.chat.emptyBody}</p>
          </div>
        )}

        <ul className="p-2">
          {active.data?.items.map((c) => (
            <li key={c.id}>
              <ConversationRow conversation={c} selected={c.id === activeID} />
            </li>
          ))}
        </ul>

        {/* The koš, as its own section (D253). A trashed conversation has left every
            other surface entirely, so this is the only place it appears at all. */}
        {(trashed.data?.items.length ?? 0) > 0 && (
          <div className="border-t border-border p-2">
            <div className="flex items-center gap-1.5 px-2 py-1.5 text-xs font-bold uppercase tracking-wide text-muted">
              <Trash2 size={12} aria-hidden />
              {cs.chat.trashSectionTitle}
            </div>
            <ul>
              {trashed.data?.items.map((c) => (
                <li key={c.id}>
                  <TrashedRow conversation={c} />
                </li>
              ))}
            </ul>
          </div>
        )}
      </div>

      <NewConversationDialog open={creating} onOpenChange={setCreating} />
    </div>
  )
}

function ConversationRow({ conversation, selected }: { conversation: Conversation; selected: boolean }) {
  const unread = conversation.unread_count
  return (
    <Link
      to={`/chat/${conversation.id}`}
      aria-current={selected ? 'page' : undefined}
      className={cn(
        'flex items-center gap-3 rounded-md border-l-2 px-3 py-2.5 transition-colors',
        selected
          ? 'border-l-accent bg-s2 text-fg'
          : 'border-l-transparent text-muted hover:bg-s2 hover:text-fg',
      )}
    >
      <span className="min-w-0 flex-1">
        <span className={cn('block truncate text-sm', selected || unread > 0 ? 'font-bold text-fg' : 'font-medium')}>
          {conversation.name}
        </span>
        <span className="mt-0.5 block truncate text-xs text-muted">
          {count(conversation.member_count, PLURAL.members)}
        </span>
      </span>
      {unread > 0 && (
        <span
          // ⚠ ACCENT, NOT A WARNING COLOUR, and mono tabular so 3 and 40 do not
          // shift the row. An unread count is a reason to open something, not an
          // alarm about it.
          className="flex-none rounded-full bg-accent px-2 py-0.5 font-mono text-[11px] font-bold tabular-nums text-accent-fg"
          aria-label={`${unread} ${cs.chat.unreadLabel}`}
        >
          {unread}
        </span>
      )}
    </Link>
  )
}

/**
 * A row in the koš.
 *
 * ⚠ ITS BYTES STILL COUNT toward both storage thresholds until it is really purged
 * (D254), which is honest and looks wrong — so the countdown is stated rather than
 * left to be inferred, and *Smazat natrvalo* (PR 3's storage half) is what stops it
 * trapping anyone.
 */
function TrashedRow({ conversation }: { conversation: Conversation }) {
  const restore = useRestoreConversation()
  return (
    <div className="flex items-center gap-3 rounded-md px-3 py-2.5">
      <span className="min-w-0 flex-1">
        <span className="block truncate text-sm font-medium text-muted line-through">
          {conversation.name}
        </span>
        {conversation.purge_after && (
          <span className="mt-0.5 block truncate text-xs text-muted">
            {cs.chat.trashDaysLeft} {daysUntil(conversation.purge_after)}
          </span>
        )}
      </span>
      <Button
        size="sm"
        variant="ghost"
        loading={restore.isPending}
        onClick={() => restore.mutate(conversation.id)}
      >
        {cs.chat.word.restore}
      </Button>
    </div>
  )
}

/** daysUntil renders the koš countdown with the Czech plural. */
function daysUntil(iso: string): string {
  const ms = new Date(iso).getTime() - Date.now()
  const days = Math.max(0, Math.ceil(ms / 86_400_000))
  return count(days, PLURAL.days)
}

function NewConversationDialog({
  open,
  onOpenChange,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const [name, setName] = useState('')
  const create = useCreateConversation()

  const submit = () => {
    const trimmed = name.trim()
    if (!trimmed) return
    create.mutate(
      { name: trimmed },
      {
        onSuccess: () => {
          setName('')
          onOpenChange(false)
        },
      },
    )
  }

  return (
    <ResponsiveModal
      open={open}
      onOpenChange={onOpenChange}
      title={cs.chat.word.newConversation}
      footer={
        <>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            {cs.chat.cancel}
          </Button>
          <Button variant="primary" loading={create.isPending} onClick={submit} disabled={!name.trim()}>
            {cs.chat.create}
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
      <p className="mt-3 text-sm text-muted text-pretty">
        {/* Members are added afterwards, from the panel, so the create dialog stays
            one field — and so the floor sentence has one place to live rather than
            two. */}
        {cs.chat.directoryHint}
      </p>
    </ResponsiveModal>
  )
}
