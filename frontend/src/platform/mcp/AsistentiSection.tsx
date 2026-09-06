// Nastavení → Asistenti (MCP) — the fourth section (v11, HANDOFF-design §v11).
//
// ⚠ IT SITS LAST OF THE FOUR, on purpose: it is the least-visited panel on the
// screen and the only one a member may never open at all. Oznámení, Vzhled a účet
// and Aplikace all describe things every member already has; this one describes a
// thing most of them will never create.
//
// ⚠ AND THE EMPTY STATE IS THE ONLY PLACE THIS FEATURE EVER EXPLAINS ITSELF.
// v11 adds no nav entry, no widget, no onboarding and no tour, so one short
// paragraph — what a token is, that it acts as you, that it can be revoked — is
// the whole of the documentation a member will ever be shown.

import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { ChevronDown, ChevronRight, Plus } from 'lucide-react'
import { cn } from '@/lib/utils'
import { cs } from '@/i18n/cs'
import { qk } from '@/api/keys'
import { apiErrorMessage } from '@/api/client'
import { listMcpTokens, mintMcpToken, revokeMcpToken } from '@/api/mcp'
import { Button, Input } from '@/components/ui/ui'
import { ResponsiveModal } from '@/components/ui/modal'
import { useOnline } from '@/platform/pwa/offline'
import type { McpModule, McpToken, McpTokenCreated } from '@/api/types'
import { RevealTokenDialog } from './RevealTokenDialog'
import { RevokeTokenDialog } from './RevokeTokenDialog'
import {
  RevokeButton,
  TokenHeaderCell,
  TokenLastUsedCell,
  TokenNameCell,
  TokenScopeCell,
} from './TokenCells'
import { expiryLabel, moduleLabel, sortTokens, tokenCountLabel, tokenState } from './tokens'

/** The nine a scope may name, in the contract's order. */
const SCOPE_MODULES: readonly McpModule[] = [
  'todo',
  'events',
  'notes',
  'documents',
  'finance',
  'garden',
  'electricity',
  'chat',
  'admin',
]

/** The four expiry choices (D285). ⚠ Four choices, never a date picker: a picker
 *  invites a date nobody can justify. */
const TTL_CHOICES: readonly { days: 30 | 90 | 365 | null; label: string }[] = [
  { days: 30, label: cs.mcp.ttl30 },
  { days: 90, label: cs.mcp.ttl90 },
  { days: 365, label: cs.mcp.ttl365 },
  { days: null, label: cs.mcp.ttlNone },
]

// The header strip and the rows share one column template, written out as a
// literal.
//
// ⚠ IT CANNOT BE COMPOSED AT RUNTIME. Tailwind scans source TEXT for class
// names, so a template assembled with string concatenation produces classes that
// exist in the DOM and in no stylesheet — a layout that is correct in the editor
// and collapsed in the browser.
//
// ⚠ AND THE ROW STACKS BELOW `md`. Five columns do not fit 375 px, and Nastavení
// keeps its full row in the Více sheet (D273) — a member reaches this screen on a
// phone exactly as they always have, so it has to work there.
const GRID_COLS =
  'md:grid md:grid-cols-[minmax(0,1.5fr)_minmax(0,.85fr)_minmax(0,.9fr)_minmax(0,1fr)_auto] md:items-center md:gap-3.5'

export function AsistentiSection() {
  const online = useOnline()
  const client = useQueryClient()
  const [minting, setMinting] = useState(false)
  const [revoking, setRevoking] = useState<McpToken | null>(null)
  // ⚠ THE MINTED TOKEN LIVES IN THIS STATE AND NOWHERE ELSE. It is never put in a
  // query cache, never written to storage and never logged: it is rendered from
  // the mutation's own response object and falls out of memory when the dialog
  // closes. The refetch that follows comes back WITHOUT a secret, which is the
  // shape the cache is allowed to hold.
  const [revealed, setRevealed] = useState<McpTokenCreated | null>(null)

  const tokensQuery = useQuery({ queryKey: qk.mcpTokens, queryFn: listMcpTokens })
  const tokens = sortTokens(tokensQuery.data ?? [])

  const revoke = useMutation({
    mutationFn: (id: string) => revokeMcpToken(id),
    onSuccess: (_res, id) => {
      const name = tokens.find((t) => t.id === id)?.name ?? ''
      setRevoking(null)
      toast.success(cs.mcp.revokeDone(name))
      void client.invalidateQueries({ queryKey: qk.mcpTokens })
    },
    onError: (e) => toast.error(apiErrorMessage(e, cs.mcp.revokeFailed)),
  })

  return (
    <section className="rounded-xl border border-border bg-s1 p-4 md:p-5">
      <div className="mb-3 flex flex-wrap items-center gap-2.5">
        <h2 className="text-base font-extrabold">{cs.mcp.title}</h2>
        {tokens.length > 0 && (
          <span className="font-mono text-[11px] text-muted">{tokenCountLabel(tokens.length)}</span>
        )}
        <div className="flex-1" />
        {tokens.length > 0 && (
          <Button variant="primary" disabled={!online} onClick={() => setMinting(true)}>
            <Plus size={16} aria-hidden />
            <span className="ml-1.5">{cs.mcp.mint}</span>
          </Button>
        )}
      </div>

      {tokensQuery.isError ? (
        <p className="text-[13.5px] text-muted">{cs.mcp.loadFailed}</p>
      ) : tokens.length === 0 ? (
        <div className="flex flex-col items-start gap-3.5">
          <p className="max-w-[62ch] text-[13.5px] leading-relaxed text-muted text-pretty">{cs.mcp.empty}</p>
          <Button variant="primary" disabled={!online} onClick={() => setMinting(true)}>
            <Plus size={16} aria-hidden />
            <span className="ml-1.5">{cs.mcp.mint}</span>
          </Button>
        </div>
      ) : (
        <>
          {/* ⚠ The table is a grid on desktop and stacks below `md`. The phone gets
              the same five facts in a column rather than a horizontally scrolling
              table — Nastavení keeps its full row in the Více sheet (D273), so a
              member reaches this screen on a phone exactly as they always have. */}
          <div className="overflow-hidden rounded-xl border border-border">
            <div className={cn(GRID_COLS, 'hidden border-b border-border bg-s2 px-3.5 py-2 md:grid')}>
              <TokenHeaderCell>{cs.mcp.colName}</TokenHeaderCell>
              <TokenHeaderCell>{cs.mcp.colScope}</TokenHeaderCell>
              <TokenHeaderCell>{cs.mcp.colExpiry}</TokenHeaderCell>
              <TokenHeaderCell>{cs.mcp.colLastUsed}</TokenHeaderCell>
              <span />
            </div>
            {tokens.map((token) => {
              const state = tokenState(token)
              return (
                <div
                  key={token.id}
                  className={cn(
                    'flex flex-col gap-2 border-b border-border px-3.5 py-3 last:border-b-0',
                    GRID_COLS,
                  )}
                >
                  <TokenNameCell token={token} state={state} />
                  <TokenScopeCell token={token} />
                  <div className="min-w-0 truncate font-mono text-[12.5px] text-muted">
                    {/* The label rides along only where there is no header to carry it —
                        below `md` the grid has stacked and the strip is gone. */}
                    <span className="md:hidden">{cs.mcp.colExpiry} </span>
                    {expiryLabel(token)}
                  </div>
                  <TokenLastUsedCell token={token} />
                  <div className="md:justify-self-end">
                    <RevokeButton state={state} onClick={() => setRevoking(token)} />
                  </div>
                </div>
              )
            })}
          </div>
          {/* ⚠ Said out loud, because a list that keeps dead rows looks like a list
              nobody cleans up until somebody explains why it must. */}
          <p className="mt-2.5 text-[12px] text-muted text-pretty">{cs.mcp.keepNote}</p>
        </>
      )}

      {minting && (
        <MintDialog
          onClose={() => setMinting(false)}
          onMinted={(token) => {
            setMinting(false)
            setRevealed(token)
            void client.invalidateQueries({ queryKey: qk.mcpTokens })
          }}
        />
      )}
      {revealed && <RevealTokenDialog token={revealed} onDone={() => setRevealed(null)} />}
      {revoking && (
        <RevokeTokenDialog
          token={revoking}
          pending={revoke.isPending}
          onConfirm={() => revoke.mutate(revoking.id)}
          onClose={() => setRevoking(null)}
        />
      )}
    </section>
  )
}

/**
 * The mint dialog — three fields, and one of them is a scope.
 *
 * ⚠ THE CEILING IS REPORTED AS A SENTENCE, NOT AS A GREYED-OUT BUTTON. The server
 * refuses an eleventh token with a 422 naming the remedy ("Odvolejte některý
 * starý"), and that message is shown here in the form. A disabled *Vytvořit token*
 * would be the same refusal with the reason removed — and the count is the
 * server's to know: it is `HOME_MCP_MAX_TOKENS_PER_USER`, not a constant this
 * client can hold without the two drifting.
 */
function MintDialog({
  onClose,
  onMinted,
}: {
  onClose: () => void
  onMinted: (token: McpTokenCreated) => void
}) {
  const [name, setName] = useState('')
  const [days, setDays] = useState<30 | 90 | 365 | null>(90)
  const [modules, setModules] = useState<McpModule[]>([])
  const [scopeOpen, setScopeOpen] = useState(false)

  const mint = useMutation({
    mutationFn: () => mintMcpToken({ name: name.trim(), expires_in_days: days, modules }),
    onSuccess: onMinted,
  })

  const toggle = (m: McpModule) =>
    setModules((current) => (current.includes(m) ? current.filter((x) => x !== m) : [...current, m]))

  const scopeSummaryLabel =
    modules.length === 0 ? cs.mcp.scopeAll : modules.map(moduleLabel).join(' · ')

  return (
    <ResponsiveModal
      open
      onOpenChange={(o) => !o && onClose()}
      title={cs.mcp.mintTitle}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {cs.mcp.cancel}
          </Button>
          <Button
            variant="primary"
            loading={mint.isPending}
            disabled={name.trim() === ''}
            onClick={() => mint.mutate()}
          >
            {cs.mcp.mint}
          </Button>
        </>
      }
    >
      <div className="space-y-4">
        <p className="text-[12.5px] text-muted text-pretty">{cs.mcp.mintLead}</p>

        <div>
          <div className="mb-1.5 flex items-baseline gap-2">
            <label htmlFor="mcp-token-name" className="text-[12.5px] font-semibold">
              {cs.mcp.nameLabel}
            </label>
            <div className="flex-1" />
            <span className="font-mono text-[10.5px] text-muted">{name.length} / 60</span>
          </div>
          <Input
            id="mcp-token-name"
            value={name}
            maxLength={60}
            placeholder={cs.mcp.namePlaceholder}
            onChange={(e) => setName(e.target.value)}
          />
          {/* The hint teaches by example rather than by rule: the name is what
              appears in the Log next to the member's own, and it will be read by
              somebody trying to remember what this was. */}
          <p className="mt-1.5 text-[12px] text-muted text-pretty">{cs.mcp.nameHint}</p>
        </div>

        <div>
          <div className="mb-2 text-[12.5px] font-semibold">{cs.mcp.expiryLabel}</div>
          <div role="radiogroup" aria-label={cs.mcp.expiryLabel} className="flex flex-wrap gap-2">
            {TTL_CHOICES.map((choice) => (
              <button
                key={String(choice.days)}
                type="button"
                role="radio"
                aria-checked={days === choice.days}
                onClick={() => setDays(choice.days)}
                className={cn(
                  'min-h-[38px] rounded-lg border px-3 text-[13px] font-semibold',
                  days === choice.days
                    ? 'border-accent bg-accent-soft text-fg'
                    : 'border-border bg-s2 text-muted',
                )}
              >
                {choice.label}
              </button>
            ))}
          </div>
          <p className="mt-1.5 text-[12px] text-muted text-pretty">{cs.mcp.expiryHint}</p>
        </div>

        {/* ⚠ ROZSAH MUST NOT LOOK LIKE A SECURITY CONTROL, BECAUSE IT IS NOT ONE
            (D288). No lock, no shield, no privacy signal of any kind — it narrows
            what the assistant is OFFERED and changes nothing about what the member
            may see, and the helper text says exactly that. */}
        <div className="overflow-hidden rounded-xl border border-border bg-s2">
          <button
            type="button"
            aria-expanded={scopeOpen}
            onClick={() => setScopeOpen((v) => !v)}
            className="flex min-h-[46px] w-full items-center gap-2.5 px-3.5 text-left"
          >
            <span className="flex-none text-[12.5px] font-semibold">{cs.mcp.scopeLabel}</span>
            <span className="min-w-0 flex-1 truncate text-[13px] text-muted">{scopeSummaryLabel}</span>
            {scopeOpen ? (
              <ChevronDown size={14} className="flex-none text-muted" aria-hidden />
            ) : (
              <ChevronRight size={14} className="flex-none text-muted" aria-hidden />
            )}
          </button>
          {scopeOpen && (
            <div className="grid grid-cols-[repeat(auto-fit,minmax(148px,1fr))] gap-1.5 px-3.5 pb-3.5">
              {SCOPE_MODULES.map((m) => {
                const on = modules.includes(m)
                return (
                  <button
                    key={m}
                    type="button"
                    role="checkbox"
                    aria-checked={on}
                    onClick={() => toggle(m)}
                    className={cn(
                      'flex min-h-[38px] items-center gap-2 rounded-lg border px-2.5 text-left text-[13px]',
                      on ? 'border-accent bg-accent-soft text-fg' : 'border-border bg-s1 text-muted',
                    )}
                  >
                    <span
                      aria-hidden
                      className={cn(
                        'grid h-4 w-4 flex-none place-items-center rounded border text-[10px]',
                        on ? 'border-accent bg-accent text-accent-fg' : 'border-border-strong',
                      )}
                    >
                      {on ? '✓' : ''}
                    </span>
                    {moduleLabel(m)}
                  </button>
                )
              })}
            </div>
          )}
          <p className="px-3.5 pb-3.5 text-[12px] text-muted text-pretty">{cs.mcp.scopeHelp}</p>
        </div>

        {mint.isError && (
          <p role="alert" className="rounded-lg border border-danger/40 bg-danger/10 px-3 py-2 text-[13px] text-pretty">
            {apiErrorMessage(mint.error, cs.mcp.mintFailed)}
          </p>
        )}
      </div>
    </ResponsiveModal>
  )
}
