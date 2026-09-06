// The cells both token lists draw — v11.
//
// ⚠ THE MARK IS NEUTRAL AND SO IS *vyprší brzy*. There is no new colour token in
// v11: ordinary text and `--muted` for metadata, the danger family for *Odvolat*
// and nothing else, neutral for the marks and for the Log's chip. A "10 days left"
// badge in the attention register would tell a member something is wrong with a
// token that is doing exactly what they asked it to.
//
// ⚠ AND `--muted`, NEVER `--subtle`, ON EVERY LINE HERE. `--subtle` on `--s1`
// measures 4.29:1 in the light theme — under the 4.5:1 AA bar — and has been open
// since v10.2. These two screens are the most metadata-dense in the application
// (prefix, dates, scope, last used, IP are all secondary text), which makes them
// the worst possible place to inherit it. Nastavení's own notification panel
// carries the same swap and the same note.

import { cn } from '@/lib/utils'
import { cs } from '@/i18n/cs'
import type { McpToken } from '@/api/types'
import { isDead, lastUsedLabel, markLabel, scopeSummary, type TokenState } from './tokens'

/** The state marker beside a token's prefix — absent for a token with nothing to
 *  report. */
export function TokenMark({ token, state }: { token: McpToken; state: TokenState }) {
  const label = markLabel(token, state)
  if (!label) return null
  return (
    <span
      className={cn(
        'inline-flex h-[22px] flex-none items-center whitespace-nowrap rounded-md px-2 font-mono text-[10.5px] font-semibold text-muted',
        // A dead row's marker is dashed rather than red: the token is gone, and
        // nothing about that is an incident.
        isDead(state) ? 'border border-dashed border-border-strong bg-s2' : 'border border-border bg-s3',
      )}
    >
      {label}
    </span>
  )
}

/** Název + prefix + marker — the strongest thing in the row. */
export function TokenNameCell({ token, state }: { token: McpToken; state: TokenState }) {
  return (
    <div className="flex min-w-0 flex-col gap-1">
      <span
        className={cn(
          'truncate text-sm font-bold tracking-tight',
          isDead(state) ? 'text-muted' : 'text-fg',
        )}
      >
        {token.name}
      </span>
      <div className="flex min-w-0 flex-wrap items-center gap-2">
        {/* ⚠ The prefix exists so a member can match a row to a config file BY EYE,
            which is the whole of why it is monospace and why it keeps its ellipsis. */}
        <span className="font-mono text-[11.5px] text-muted">{token.prefix}…</span>
        <TokenMark token={token} state={state} />
      </div>
    </div>
  )
}

/** Rozsah — *Všechny moduly*, or a count whose title names them. */
export function TokenScopeCell({ token }: { token: McpToken }) {
  const scope = scopeSummary(token.modules)
  return (
    <div className="min-w-0 truncate text-[12.5px] text-muted" title={scope.title}>
      {scope.label}
    </div>
  )
}

/**
 * Naposledy použito.
 *
 * ⚠ THE LARGEST SECONDARY TEXT ON THE SCREEN, ON PURPOSE. It is the column that
 * decides everything — a token list is only useful if a person can look at it in
 * eighteen months and answer *"do I still need this?"* — and the design note says
 * outright that it must not be the smallest text on the row.
 */
export function TokenLastUsedCell({ token }: { token: McpToken }) {
  const last = lastUsedLabel(token)
  return (
    <div className="min-w-0" title={last.title}>
      <span className={cn('text-sm', last.never ? 'italic text-muted' : 'font-semibold text-fg')}>
        {last.label}
      </span>
    </div>
  )
}

/** The column header strip both lists share the styling of. */
export function TokenHeaderCell({ children }: { children: React.ReactNode }) {
  return (
    <span className="min-w-0 font-mono text-[10px] uppercase tracking-wider text-muted">{children}</span>
  )
}

/**
 * Odvolat — the one destructive control on either screen.
 *
 * ⚠ IT IS ABSENT ON A DEAD ROW RATHER THAN DISABLED. A revoked token cannot be
 * revoked again, and a greyed-out button invites the click that explains why not.
 */
export function RevokeButton({ state, onClick }: { state: TokenState; onClick: () => void }) {
  if (isDead(state)) return null
  return (
    <button
      type="button"
      onClick={onClick}
      // ≥ 44 px of touch target in a dense row: the visible control is 34 px and
      // the inset pseudo-element carries the rest, the same trick the settings
      // toggles use.
      className="relative min-h-[34px] flex-none whitespace-nowrap rounded-lg border border-danger/45 px-3 text-[12.5px] font-semibold text-danger after:absolute after:inset-x-0 after:-inset-y-[5px] after:content-[''] hover:bg-danger/10"
    >
      {cs.mcp.revoke}
    </button>
  )
}
