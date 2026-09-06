// Log / logging — the module's wire types, mirroring the Go backend JSON
// (openapi.yaml). The read side of the audit spine.

export interface AuditChange {
  field: string
  old_value: string | null
  new_value: string | null
}

export interface AuditEvent {
  id: string
  ts: string
  actor_user_id: string | null
  actor_type: string
  actor_label: string | null
  module: string
  action: string
  entity_type: string | null
  entity_id: string | null
  summary: string
  level: 'info' | 'warn' | 'error'
  request_id: string | null
  site: string
  meta: Record<string, unknown> | null
  /**
   * True when this event concerns a PRIVATE note or document and the caller is
   * not its owner (v9, D187). The row is still returned — the spine records
   * everything — but `summary` carries the fixed phrase *"Soukromá položka —
   * podrobnosti skryty"*, `entity_id` is dropped and `changes` comes back empty.
   *
   * ⚠ Render it as a STATE, not by string-matching the summary. Without it the
   * browser shows the phrase as though somebody had written it, and there is no
   * way to tell a redacted row from a row about something dull.
   */
  redacted: boolean
  change_count: number
  /**
   * `mcp` when the change was made through an MCP token; null for the browser
   * and for system/service actors (v11, D290).
   *
   * ⚠ `actor_type` IS DELIBERATELY UNCHANGED and still `user | system |
   * service`. The actor IS the member — same id, same roles, so every ownership
   * and membership check in eleven modules keeps working untouched. What names
   * the token is `actor_label` ("Karel · Claude (notebook)") and this field.
   *
   * ⚠ AND IT CANNOT BE ANSWERED RETROACTIVELY. Every row written before v11
   * has it null, which is correct: those changes really were made in the browser.
   */
  via: 'mcp' | null
  /** `mcp_tokens.id`, non-null iff `via = "mcp"`. A token row is never deleted,
   *  only revoked, so it resolves for the life of the event. */
  via_token_id: string | null
}

export interface AuditEventDetail extends AuditEvent {
  changes: AuditChange[]
}

export interface AuditEventPage {
  items: AuditEvent[]
  next_cursor: string | null
}

export interface AuditEventDetailPage {
  items: AuditEventDetail[]
  next_cursor: string | null
}

export interface StatsResponse {
  dimension: string
  bucket: string
  buckets: { ts: string; counts: Record<string, number> }[]
  totals: { key: string; count: number }[]
}
