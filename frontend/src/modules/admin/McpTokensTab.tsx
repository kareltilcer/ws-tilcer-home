// Administrace → Asistenti → Tokeny — a third level-1 group with one tab (v11).
//
// ⚠ THERE IS NO ADMIN MINT, AND THERE IS NOTHING HERE THAT LOOKS LIKE ONE (D316).
// This is D240's shape — *names and sizes, and no way in* — applied to credentials
// instead of to conversations: an admin may see that a key exists and take it
// away, and may not make one in somebody else's name. The contract has no route to
// call, `src/api/mcp.ts` has no function to call it with, and the panel says so in
// a sentence rather than leaving the absence to be noticed. An absence nobody
// names is an absence somebody adds back.
//
// ⚠ AND THE OWNER COLUMN IS WHY THIS SCREEN EXISTS. The member's own list in
// Nastavení answers "what have I connected"; this one answers "who in this
// household has connected an assistant, and is any of it still live".

import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { cn } from '@/lib/utils'
import { cs } from '@/i18n/cs'
import { qk } from '@/api/keys'
import { apiErrorMessage } from '@/api/client'
import { listAllMcpTokens, revokeAnyMcpToken } from '@/api/mcp'
import { compareCz, fmtDate } from '@/i18n/format'
import { RevokeTokenDialog } from '@/platform/mcp/RevokeTokenDialog'
import {
  RevokeButton,
  TokenHeaderCell,
  TokenLastUsedCell,
  TokenNameCell,
  TokenScopeCell,
} from '@/platform/mcp/TokenCells'
import { expiryLabel, sortTokens, tokenCountLabel, tokenState } from '@/platform/mcp/tokens'
import type { McpTokenAdmin } from '@/api/types'

// Seven columns and an action. ⚠ A literal, for the reason AsistentiSection's own
// template is one: Tailwind reads source text, not runtime strings.
const GRID_COLS =
  'md:grid md:grid-cols-[minmax(0,.7fr)_minmax(0,1.3fr)_minmax(0,.8fr)_minmax(0,.7fr)_minmax(0,.9fr)_minmax(0,.9fr)_auto] md:items-center md:gap-3'

export function McpTokensTab() {
  const client = useQueryClient()
  const [owner, setOwner] = useState('')
  const [revoking, setRevoking] = useState<McpTokenAdmin | null>(null)

  const tokensQuery = useQuery({ queryKey: qk.adminMcpTokens, queryFn: listAllMcpTokens })
  const all = useMemo(() => sortTokens(tokensQuery.data ?? []), [tokensQuery.data])

  // ⚠ THE FILTER IS KEYED BY user_id AND LABELLED BY display_name. A household with
  // two members called Jana would otherwise have one filter chip that matches both,
  // and the directory projection can be null for somebody who has never logged in.
  const owners = useMemo(() => {
    const seen = new Map<string, string>()
    for (const t of all) {
      if (!seen.has(t.user_id)) seen.set(t.user_id, t.display_name ?? cs.mcp.adminUnknownOwner)
    }
    return [...seen.entries()].sort((a, b) => compareCz(a[1], b[1]))
  }, [all])

  const rows = owner ? all.filter((t) => t.user_id === owner) : all

  const revoke = useMutation({
    mutationFn: (id: string) => revokeAnyMcpToken(id),
    onSuccess: (_res, id) => {
      const name = all.find((t) => t.id === id)?.name ?? ''
      setRevoking(null)
      toast.success(cs.mcp.revokeDone(name))
      void client.invalidateQueries({ queryKey: qk.adminMcpTokens })
    },
    onError: (e) => toast.error(apiErrorMessage(e, cs.mcp.revokeFailed)),
  })

  return (
    <div className="space-y-3.5">
      <section className="rounded-xl border border-border bg-s1 p-4 md:p-5">
        <h2 className="mb-1.5 text-[15px] font-extrabold">{cs.mcp.adminHeading}</h2>
        <p className="mb-2.5 text-[12.5px] leading-relaxed text-muted text-pretty">{cs.mcp.adminLead}</p>
        <div className="flex gap-2.5 rounded-lg border border-border bg-s2 px-3 py-2.5">
          <span className="flex-none text-muted" aria-hidden>
            ◷
          </span>
          <p className="text-[12px] leading-normal text-pretty">{cs.mcp.adminNoMint}</p>
        </div>
      </section>

      {owners.length > 1 && (
        <div className="flex flex-wrap items-center gap-2">
          <span className="font-mono text-[10px] uppercase tracking-wider text-muted">
            {cs.mcp.adminOwnerFilter}
          </span>
          {owners.map(([id, label]) => (
            <button
              key={id}
              type="button"
              aria-pressed={owner === id}
              onClick={() => setOwner(owner === id ? '' : id)}
              className={cn(
                'min-h-[32px] rounded-lg border px-2.5 text-[12px] font-semibold',
                owner === id ? 'border-accent bg-accent-soft text-fg' : 'border-border bg-s2 text-muted',
              )}
            >
              {label}
            </button>
          ))}
          {owner && (
            <button
              type="button"
              onClick={() => setOwner('')}
              className="min-h-[32px] rounded-lg border border-border px-2.5 text-[12px] text-muted"
            >
              {cs.mcp.adminClearFilter}
            </button>
          )}
          <div className="flex-1" />
          <span className="font-mono text-[11.5px] text-muted">{tokenCountLabel(rows.length)}</span>
        </div>
      )}

      {tokensQuery.isError ? (
        <p className="text-[13.5px] text-muted">{cs.mcp.loadFailed}</p>
      ) : rows.length === 0 ? (
        <p className="rounded-xl border border-border bg-s1 p-4 text-[13.5px] text-muted">{cs.mcp.adminEmpty}</p>
      ) : (
        <>
          <section className="overflow-hidden rounded-xl border border-border bg-s1">
            <div className={cn(GRID_COLS, 'hidden border-b border-border bg-s2 px-4 py-2 md:grid')}>
              <TokenHeaderCell>{cs.mcp.colOwner}</TokenHeaderCell>
              <TokenHeaderCell>{cs.mcp.colName}</TokenHeaderCell>
              <TokenHeaderCell>{cs.mcp.colScope}</TokenHeaderCell>
              <TokenHeaderCell>{cs.mcp.colCreated}</TokenHeaderCell>
              <TokenHeaderCell>{cs.mcp.colLastUsedIP}</TokenHeaderCell>
              <TokenHeaderCell>{cs.mcp.colExpiry}</TokenHeaderCell>
              <span />
            </div>
            {rows.map((token) => {
              const state = tokenState(token)
              return (
                <div
                  key={token.id}
                  className={cn('flex flex-col gap-2 border-b border-border px-4 py-3 last:border-b-0', GRID_COLS)}
                >
                  <div className="min-w-0 truncate text-[13.5px] font-bold">
                    {token.display_name ?? cs.mcp.adminUnknownOwner}
                  </div>
                  <TokenNameCell token={token} state={state} />
                  <TokenScopeCell token={token} />
                  <div className="min-w-0 truncate font-mono text-[12px] text-muted">
                    <span className="md:hidden">{cs.mcp.colCreated} </span>
                    {fmtDate(new Date(token.created_at))}
                  </div>
                  <div className="min-w-0">
                    <TokenLastUsedCell token={token} />
                    {/* ⚠ The IP is the admin screen's own column and appears nowhere
                        else. It is what turns "this token is still being used" into
                        "and it is being used from somewhere I recognise". */}
                    <div className="truncate font-mono text-[11px] text-muted">{token.last_used_ip ?? '—'}</div>
                  </div>
                  <div className="min-w-0 truncate font-mono text-[12px] text-muted">
                    <span className="md:hidden">{cs.mcp.colExpiry} </span>
                    {expiryLabel(token)}
                  </div>
                  <div className="md:justify-self-end">
                    <RevokeButton state={state} onClick={() => setRevoking(token)} />
                  </div>
                </div>
              )
            })}
          </section>
          <p className="text-[12px] text-muted text-pretty">{cs.mcp.adminSortNote}</p>
        </>
      )}

      {revoking && (
        <RevokeTokenDialog
          token={revoking}
          // ⚠ THE OWNER IS PASSED, AND IT IS WHAT MAKES THIS CONFIRMATION DIFFERENT.
          // Revoking somebody else's credential is a different act from revoking your
          // own, and the sentence is the only place that difference appears.
          owner={revoking.display_name ?? cs.mcp.adminUnknownOwner}
          pending={revoke.isPending}
          onConfirm={() => revoke.mutate(revoking.id)}
          onClose={() => setRevoking(null)}
        />
      )}
    </div>
  )
}
