// Asistenti (MCP) tokens — v11, openapi 0.15.0 (PRD §V11-6).
//
// ⚠ THE FOUR ROUTES HERE ARE THE ORDINARY /api SURFACE. The MCP server itself
// lives at POST /mcp, speaks JSON-RPC, and this application never calls it: the
// browser has a session cookie and `/mcp` refuses cookies outright (D280). What
// these routes do is bring a credential for that OTHER door into existence.
//
// ⚠ IT STAYS IN src/api/ BECAUSE NO MODULE OWNS IT, the same reasoning as push.ts:
// the member's own list is rendered by platform/settings and the household-wide one
// by modules/admin, and putting the endpoints in either would make the other import
// across a boundary that does not exist on the backend either — v11 adds no module.
//
// ⚠ AND THE SECRET APPEARS IN EXACTLY ONE RETURN TYPE. `mintMcpToken` is the only
// function in this file whose result carries `secret`, because POST is the only
// response in the whole contract that has ever contained one (D284). Everything
// else returns `McpToken`, which has no such field to log by accident.

import { apiFetch } from './client'
import type { McpToken, McpTokenAdmin, McpTokenCreate, McpTokenCreated, McpTokenUpdate } from './types'

/** The caller's own tokens, including expired and revoked ones. */
export const listMcpTokens = () => apiFetch<McpToken[]>('/api/mcp/tokens')

/**
 * mintMcpToken returns the secret, once.
 *
 * ⚠ THE RESULT MUST NOT BE PUT IN A QUERY CACHE, written to storage, or logged.
 * It is handed straight to the reveal dialog and falls out of memory with it —
 * `qk.mcpTokens` is invalidated instead, and that refetch comes back WITHOUT the
 * secret, which is the shape the cache is allowed to hold.
 */
export const mintMcpToken = (body: McpTokenCreate) =>
  apiFetch<McpTokenCreated>('/api/mcp/tokens', { method: 'POST', body })

/**
 * updateMcpToken renames a token or changes its module scope.
 *
 * ⚠ NAME AND MODULES ONLY. Sending an expiry is a 422 rather than a silent no-op
 * (D285) — extending a token's life is minting a new one, and the type is what
 * keeps this client from asking.
 *
 * ⚠ NO SCREEN CALLS THIS YET, AND THAT IS THE DESIGN RATHER THAN AN OVERSIGHT.
 * HANDOFF-design §v11 draws six things per row and an Odvolat; it draws no rename
 * and no scope editor, because the remedy for a token that is wrong is a new one —
 * which is a minute — and a rename affordance would sit next to the one control
 * that is genuinely destructive. The route exists, the contract describes it, and
 * this is the wrapper the screen that wants it will use.
 */
export const updateMcpToken = (id: string, body: McpTokenUpdate) =>
  apiFetch<McpToken>(`/api/mcp/tokens/${id}`, { method: 'PATCH', body })

/** revokeMcpToken stamps `revoked_at`. The row stays; a Log entry naming this
 *  token must still resolve years from now. */
export const revokeMcpToken = (id: string) =>
  apiFetch<void>(`/api/mcp/tokens/${id}`, { method: 'DELETE' })

// ---- admin (Administrace → Asistenti → Tokeny) ----

/** Every member's tokens. */
export const listAllMcpTokens = () => apiFetch<McpTokenAdmin[]>('/api/admin/mcp/tokens')

/**
 * revokeAnyMcpToken revokes somebody else's.
 *
 * ⚠ THERE IS NO ADMIN MINT AND THERE IS DELIBERATELY NO FUNCTION FOR ONE (D316):
 * an admin may see that a key exists and take it away, and may not make one in
 * another member's name. The contract has no route to call.
 */
export const revokeAnyMcpToken = (id: string) =>
  apiFetch<void>(`/api/admin/mcp/tokens/${id}`, { method: 'DELETE' })

/**
 * mcpServerUrl is what goes in a client's config — this deployment's own origin
 * plus `/mcp`.
 *
 * ⚠ READ FROM THE BROWSER RATHER THAN HARD-CODED. home.tilcer.cz is one
 * deployment of this app; a snippet that names it regardless of where the page
 * was served would hand a member a config pointing at somebody else's household.
 */
export function mcpServerUrl(): string {
  return `${window.location.origin}/mcp`
}

/**
 * mcpConnectSnippet is the ready-to-paste client configuration, with the secret
 * already in it.
 *
 * ⚠ THIS IS WHERE THE SECRET IS RENDERED, AND IT IS THE ONLY PLACE. The reveal
 * dialog does not also show the bare token above it: two copies of one secret on
 * one screen double the chance a copy ends up somewhere it should not, and at
 * 375 px they double the height of the thing a member is trying to read.
 */
export function mcpConnectSnippet(secret: string): string {
  return JSON.stringify(
    { mcpServers: { home: { url: mcpServerUrl(), headers: { Authorization: `Bearer ${secret}` } } } },
    null,
    2,
  )
}
