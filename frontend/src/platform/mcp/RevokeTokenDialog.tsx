// Odvolat — the one destructive action on either Asistenti screen (v11, item 7).
//
// ⚠ NO TYPE-THE-NAME CONFIRMATION, and that is a decision rather than an omission.
// v10's conversation delete makes somebody type the name because the thing being
// destroyed is years of other people's messages. Here the cost of revoking by
// accident is minting another token, which is a minute — and the cost of friction
// is a member who does NOT revoke a token they are unsure about. The confirmation
// names the token and stops there.
//
// ⚠ THE ADMIN'S CONFIRMATION ADDITIONALLY NAMES THE OWNER, because revoking
// somebody else's credential is a different act from revoking your own, and the
// sentence is the only place that difference appears.

import { ResponsiveModal } from '@/components/ui/modal'
import { Button } from '@/components/ui/ui'
import { cs } from '@/i18n/cs'
import { lastUsedLabel } from './tokens'
import type { McpToken } from '@/api/types'

export function RevokeTokenDialog({
  token,
  owner,
  pending,
  onConfirm,
  onClose,
}: {
  token: McpToken
  /** The owner's display name, on the admin screen only. */
  owner?: string | null
  pending: boolean
  onConfirm: () => void
  onClose: () => void
}) {
  const last = lastUsedLabel(token)
  const heading = owner ? cs.mcp.revokeTitleAdmin(token.name, owner) : cs.mcp.revokeTitle(token.name)
  return (
    <ResponsiveModal
      open
      onOpenChange={(o) => !o && onClose()}
      title={heading}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {cs.mcp.cancel}
          </Button>
          {/* Danger, and this is the one control in v11 that earns it: revoking is
              immediate, irreversible, and takes effect on the assistant's very next
              call. */}
          <Button variant="danger" loading={pending} onClick={onConfirm}>
            {cs.mcp.revoke}
          </Button>
        </>
      }
    >
      <div className="space-y-3">
        <p className="font-mono text-[11.5px] text-muted">
          {token.prefix}… · {cs.mcp.colLastUsed} {last.label}
        </p>
        <p className="text-[13.5px] text-muted text-pretty">{cs.mcp.revokeBody}</p>
      </div>
    </ResponsiveModal>
  )
}
