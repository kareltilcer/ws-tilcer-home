import { useEffect, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { cs } from '@/i18n/cs'
import { fmtStorageBytes } from '@/i18n/format'
import { qk } from '@/api/keys'
import { getStorageSnapshot, setStorageThresholds } from '@/api/endpoints'
import { Input, Spinner } from '@/components/ui/ui'
import type { StorageSnapshot } from '@/api/types'

/**
 * Správa úložiště → Limity (D263, D236).
 *
 * ⚠ TWO FIELDS, AUTOSAVED ON BLUR, WITH THE STATE ALWAYS BESIDE THE FIELD. There is
 * no Save button, so an invalid value cannot be left sitting in a form somebody
 * thinks they have submitted — it is refused back to the last valid one, in place.
 *
 * ⚠ LOWERING A LIMIT BELOW CURRENT USAGE IS SAVED, NOT REFUSED (D237/D244). Nothing
 * in v10 is ever blocked by a threshold — the whole register is warn-only, no upload
 * fails, there is no quota — so the screen SAYS WHAT IT JUST SWITCHED ON rather than
 * arguing about it.
 *
 * ⚠ v9's `HOME_STORAGE_WARN_TOTAL_MB` IS NOT HERE and stays an environment variable
 * (D236). Home now has two threshold mechanisms; the inconsistency is recorded
 * rather than hidden, and reconciling them is v11's.
 */
export function LimitsTab() {
  const qc = useQueryClient()
  const q = useQuery({ queryKey: qk.adminStorage, queryFn: () => getStorageSnapshot() })
  const chat = q.data?.chat

  const save = useMutation({
    mutationFn: setStorageThresholds,
    onError: () => toast.error(cs.storage.limitsSaveFailed),
    onSuccess: (saved) => {
      // ⚠ THE SNAPSHOT CARRIES THE THRESHOLDS, so it has to be re-read or the page
      // shows the old number back — which, on a field that autosaves and has no Save
      // button to press again, reads as "it did not take". The server invalidates its
      // own cache on write; this is the client half of the same fix.
      qc.setQueryData<StorageSnapshot>(qk.adminStorage, (old) =>
        old?.chat
          ? {
              ...old,
              chat: {
                ...old.chat,
                threshold_total_mb: saved.chat_total_mb,
                threshold_conversation_mb: saved.chat_conversation_mb,
                thresholds_updated_at: saved.updated_at,
                thresholds_updated_by: saved.updated_by,
              },
            }
          : old,
      )
      void qc.invalidateQueries({ queryKey: qk.adminStorage })
    },
  })

  if (q.isLoading) {
    return (
      <div className="grid min-h-[220px] place-items-center">
        <Spinner />
      </div>
    )
  }
  // A build with no chat module reports no groups and therefore no block, so the tab
  // says so rather than rendering two fields that write into nothing.
  if (!chat) {
    return (
      <div className="rounded-2xl border border-border bg-s1 p-6 text-center text-sm text-muted">
        {cs.storage.limitsUnavailable}
      </div>
    )
  }

  return (
    <section className="space-y-4 rounded-2xl border border-border bg-s1 p-4">
      <div>
        <h3 className="text-base font-extrabold tracking-tight">{cs.storage.limitsTitle}</h3>
        <p className="mt-1 text-[12px] text-muted text-pretty">{cs.storage.limitsSubtitle}</p>
      </div>

      <LimitField
        id="chat-total"
        label={cs.storage.limitTotal}
        hint={cs.storage.limitTotalHint}
        value={chat.threshold_total_mb}
        usage={chat.total_bytes}
        saving={save.isPending}
        onSave={(mb) => save.mutate({ chat_total_mb: mb })}
      />
      <LimitField
        id="chat-conversation"
        label={cs.storage.limitConversation}
        hint={cs.storage.limitConversationHint}
        value={chat.threshold_conversation_mb}
        usage={largestConversationBytes(chat.conversations)}
        saving={save.isPending}
        onSave={(mb) => save.mutate({ chat_conversation_mb: mb })}
      />

      <p className="border-t border-border pt-3 font-mono text-[11px] text-subtle">
        {chat.thresholds_updated_by
          ? cs.storage.limitsEditedBy(chat.thresholds_updated_by)
          : cs.storage.limitsNeverEdited}
      </p>
    </section>
  )
}

function LimitField({
  id,
  label,
  hint,
  value,
  usage,
  saving,
  onSave,
}: {
  id: string
  label: string
  hint: string
  value: number
  usage: number | null
  saving: boolean
  onSave: (mb: number) => void
}) {
  const [draft, setDraft] = useState(String(value))
  const input = useRef<HTMLInputElement>(null)
  /**
   * A value that changed on the server — another admin, or our own save landing —
   * replaces the draft, because the field is a VIEW of the setting rather than a
   * form.
   *
   * ⚠ EXCEPT WHILE IT IS FOCUSED, which is the whole correction. The snapshot query
   * refetches on window focus and on the invalidate the OTHER field's save triggers,
   * and re-syncing then overwrote whatever was being typed: an admin part-way through
   * `1024` watched the field jump back to `512` mid-keystroke, on a screen with no
   * Save button to press again. A focused field belongs to whoever is typing in it;
   * the blur that follows is what reconciles the two.
   */
  useEffect(() => {
    if (document.activeElement === input.current) return
    setDraft(String(value))
  }, [value])

  const parsed = Number.parseInt(draft, 10)
  const valid = Number.isFinite(parsed) && parsed >= 1
  // ⚠ THE STATE LIVES BESIDE THE FIELD, always. `belowUsage` is not an error — it is
  // a legitimate save that switches a warning ON, and the sentence says which.
  const belowUsage = valid && usage !== null && usage > parsed * 1024 * 1024

  return (
    <div>
      <label htmlFor={id} className="block text-sm font-semibold">
        {label}
      </label>
      <div className="mt-1 flex items-center gap-2">
        <Input
          ref={input}
          id={id}
          type="number"
          min={1}
          inputMode="numeric"
          value={draft}
          disabled={saving}
          className="w-28 text-right font-mono tabular-nums"
          onChange={(e) => setDraft(e.target.value)}
          onBlur={() => {
            // ⚠ AN INVALID VALUE IS REFUSED BACK TO THE LAST VALID ONE, in place.
            // There is no Save button to leave it sitting in front of.
            if (!valid) {
              setDraft(String(value))
              return
            }
            if (parsed !== value) onSave(parsed)
          }}
          onKeyDown={(e) => {
            if (e.key === 'Enter') e.currentTarget.blur()
          }}
        />
        <span className="text-sm text-muted">MB</span>
      </div>
      <p className="mt-1 text-[12px] text-muted text-pretty">{hint}</p>
      {belowUsage && (
        <p className="mt-1 text-[12px] text-attention text-pretty">
          {cs.storage.limitBelowUsage(fmtStorageBytes(usage))}
        </p>
      )}
    </div>
  )
}

/** The heaviest room, which is what the per-conversation limit is measured against. */
function largestConversationBytes(rooms: { bytes: number | null }[]): number | null {
  let max: number | null = null
  for (const r of rooms) {
    if (r.bytes === null) continue
    if (max === null || r.bytes > max) max = r.bytes
  }
  return max
}
