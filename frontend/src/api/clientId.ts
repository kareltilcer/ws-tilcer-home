// A per-tab opaque identifier, sent on every mutating request as X-Client-Id and
// echoed back on websocket pushes as `origin`. It lets a tab tell its OWN change
// (already applied optimistically) apart from one made on another device or tab,
// so the "changed elsewhere" toast only fires for the latter (see api/ws.ts).
//
// In-memory by design: each tab / page load is its own origin, which is exactly
// the granularity we want — two tabs in one browser should still notify each
// other. randomUUID needs a secure context (https or localhost); the fallback
// covers anything else.
export const clientId: string =
  typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function'
    ? crypto.randomUUID()
    : `${Math.random().toString(36).slice(2)}${Date.now().toString(36)}`
