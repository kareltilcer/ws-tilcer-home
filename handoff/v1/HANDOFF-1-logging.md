# Home — Module 1: `logging` (audit spine + log browser)

> **Read first:** `HANDOFF.md` (foundation + shared conventions), then PRD §4 FR-L1–L7, §5 logging tables, §6 log endpoints, §7 Log screen.
> **Depends on:** foundation (F1–F6). **Blocks:** everything — build this before any feature module.
> **Scope:** the in-process audit spine every other module writes through, plus the admin-only log browser.

## Why this is first

Every mutation in every module writes an audit event **inside the same transaction as the change**. That guarantee is only cheap if the spine exists before the features do; retrofitting it means touching every handler again. Build it, then make module 2 the first consumer.

## 1. The spine (`internal/audit`)

### The interface

Modules never touch audit tables directly. They call a narrow sink:

```go
type Sink interface {
    // Record writes one event (and any field changes) using the caller's tx.
    Record(ctx context.Context, tx *sql.Tx, e Event) (eventID string, err error)
}

type Event struct {
    Module     string   // "logging" | "todo" | "events" | "dashboard"
    Action     string   // dotted verb: "card.move", "event.update", "reminder.complete"
    EntityType string   // "card" | "column" | "board" | "label" | "checklist_item" | "event" | ""
    EntityID   string
    Summary    string   // human-readable, Czech (this is what the log browser shows)
    Level      string   // "info" (default) | "warn" | "error"
    Meta       map[string]any // optional; carries `via` for cross-module triggers
    Changes    []Change       // field diffs, key entities only
}

type Change struct{ Field string; Old, New *string }
```

**Take the `*sql.Tx`, not a DB handle.** That's what makes the atomicity guarantee structural rather than a convention someone can forget.

Actor and request context (`actor_user_id`, `actor_type`, `request_id`, `ip`, `user_agent`, `site`) come from the request context populated by the auth + logging middleware — **not** from the caller's arguments. A handler shouldn't be able to log the wrong actor.

### The contract (enforce it)

- An event insert failure **fails the whole transaction**. Never swallow it, never log-and-continue: an action that succeeds unlogged is the one bug this module exists to prevent.
- If the surrounding transaction rolls back, the event goes with it. No compensating writes.
- **Reads are never logged.** Only mutations and explicit admin actions. (Logging reads would swamp the table and put a write on every page load.)
- `timestamp` is server UTC; display converts to `HOME_TIMEZONE`.

### Extraction seam

Keep `Sink` narrow enough that a second implementation — an HTTP client posting to a future standalone logging service — could be dropped in without touching module code (D-arch). **Do not build that HTTP client or a `/internal/logs` ingest endpoint now.** The seam is the deliverable; the second implementation is YAGNI until a *different* service needs to log here.

## 2. Data model

Per PRD §5. In `0001_init`, create these **first**, before the to-do and events tables.

**audit_events** — `id` (UUIDv7) · `ts` · `actor_user_id` NULL · `actor_type` CHECK(`user`,`system`,`service`) · `actor_label` NULL · `module` · `action` · `entity_type` NULL · `entity_id` NULL · `summary` · `level` CHECK(`info`,`warn`,`error`) DEFAULT `info` · `request_id` NULL · `ip` NULL · `user_agent` NULL · `site` DEFAULT `home` · `meta` (JSON text).

Indexes: `(ts DESC)`, `(module, ts)`, `(actor_user_id, ts)`, `(entity_type, entity_id, ts)`, `(action)`.

**audit_changes** — `id` · `event_id` FK→`audit_events` ON DELETE CASCADE · `field` · `old_value` NULL · `new_value` NULL. Index `(event_id)`.

**Full values, no truncation** (D6) — including a paragraph-long card note. The UI truncates with an expand; the storage keeps everything.

**audit_events_fts** — a **FTS5** virtual table over `summary` + flattened `meta`, kept in sync with triggers on insert/delete. The `q` filter queries this, not `LIKE`.

**Append-only.** No UPDATE or DELETE path anywhere except the prune in §4. Don't add an "edit event" endpoint, ever.

### Key entities for field diffs (D6)

`card`, `column`, `board`, `label`, `checklist_item`, `event`. Diffs record only fields that actually changed. Creates record `{field, null → new}`. Reminder completions log an event with **no** diffs (the row is create-only).

## 3. Log browser endpoints (admin only)

All require `admin` (D5) — a `reader` or `editor` gets `403`.

- `GET /api/logs` — filters compose (AND): `from`, `to`, `module`, `actor`, `action`, `entity_type`, `entity_id`, `level`, `q` (FTS5), plus `limit` + `cursor`. Newest-first, UUIDv7 keyset. Each item carries a **change count**, not the change list.
- `GET /api/logs/{id}` — one event with its full `changes[]`.
- `GET /api/logs/entity/{type}/{entityId}` — the entity timeline: every event for that entity, **oldest-first** (it's a history, not a feed), each with its changes. Unknown entity ⇒ empty list, not `404`.
- `GET /api/logs/stats` — grouped counts: `dimension` (`module|actor|action|level`) × `bucket` (`day|week`) over `from`–`to`, plus top-N.

Every filter must hit an index or the FTS table. A full scan here is a bug even though the dataset is small — it won't be small in two years.

## 4. Retention (FR-L7)

Append-only, with one exception: an optional prune deleting events older than `HOME_LOG_RETENTION_DAYS`. **Default `0` = keep forever**, which is the expected production setting. If a prune run deletes anything it records its own event (`logging.prune`, `system` actor) — the module logs itself, which is the point of "every module, itself included".

## 5. Frontend — the Log screen (admin)

Visual reference: the Log screen in `../design/Home.dc.html`.

- **Filter bar** — seven dimensions (date range, module, actor, action, entity, level, free text). This is the hardest responsive problem in the module; the prototype solves it — follow it rather than reinventing.
- **Result stream** — newest-first rows, each expandable to reveal `old → new` field diffs. Diffs use a non-hue cue (− / + and strikethrough) as well as colour, so they read for colour-blind users and on dark.
- **Entity timeline** — full chronological history of one entity, reachable from a log row and from a card/event detail. This is the module's payoff feature; make it a first-class screen, not a filtered list.
- **Analytics panel** — counts by module/actor/action over the range, day/week toggle, top-N.
- Query keys: `['logs', {filters}]`, `['logs','entity',type,id]`, `['logs','stats',{params}]`.
- Non-admins never see the nav entry, and the route itself also refuses (don't rely on hiding the tab).

## 6. Seed data for development

Seed a realistic Czech audit history so the browser, filters, timeline, and analytics have something to show — spanning **all four** modules and including at least one `todo.card.move` with `meta.via="dashboard"` (proving cross-module attribution renders correctly) and one `events.reminder.complete`. The design prototype's demo log is a good model.

## 7. Tests

- **Atomicity, both directions:** force the surrounding transaction to roll back → assert **no** event row. Force the event insert to fail → assert the *mutation* rolled back too. These two are the module's reason to exist; write them first.
- Actor/request-id come from context, not from caller arguments (a handler cannot forge an actor).
- Field diffs record only changed fields; full values survive a paragraph-length note unchanged.
- FTS5 search finds a Czech term **with diacritics** (`"kotlík"`) and the trigger keeps the index in sync after insert.
- Keyset pagination returns each event exactly once across pages with no gaps.
- Entity timeline is oldest-first and includes cross-module events (a card completed via Nástěnka appears in that card's timeline).
- Role gating: `reader` and `editor` get `403` on all four `/api/logs/**` endpoints.
- Prune deletes only beyond the threshold and logs itself; with the default `0`, prune is a no-op.

## 8. Definition of done

- [ ] `Sink` takes a `*sql.Tx`; no module can mutate without logging in the same transaction.
- [ ] Both atomicity tests pass.
- [ ] `audit_events`, `audit_changes`, and `audit_events_fts` + triggers created in `0001_init`, before the feature tables.
- [ ] Full untruncated diff values stored; the six key entities produce diffs.
- [ ] All four log endpoints match `openapi.yaml`; every filter is index- or FTS-backed.
- [ ] Log screen: 7-dimension filters, expandable diffs, entity timeline, analytics — usable at 375 px.
- [ ] `admin`-only enforced at the route, not just hidden in nav.
- [ ] Append-only holds: no UPDATE/DELETE path except prune, which self-logs.
