# PRD — Home

> Status: **Draft** — scope extended to four modules 2026-07-19; decisions D1–D22 resolved (see §10; D22 added 2026-07-21 from the design review); pending Karel's final approval. · Owner: Karel · Last updated: 2026-07-21
> Companion spec: `openapi.yaml` (OpenAPI 3.1, v0.2.0) · Notes: `notes.md` · Design brief: `HANDOFF-design.md`

> **v1 scope:** a single `home` fe/be pair that will grow into a household management system. **Four** modules ship in v1:
> 1. **Logging spine** (`logging`) — an in-process audit component every module writes through, plus a detailed log-browser frontend.
> 2. **To-do** (`todo`) — a Trello-style board of many sortable, collapsible columns feeding "Právě dělám" columns and an archive; cards carry notes, links, checklists, and labels.
> 3. **Okno do budoucnosti** (`events`) — future events (all-day, optionally recurring) listed by month, each optionally raising an in-app reminder a chosen lead time before the date.
> 4. **Nástěnka** (`dashboard`) — the landing page: active event reminders plus every to-do sitting in a "Právě dělám" column, each openable in detail and markable done.
>
> Everything is built around the logging spine so that every module — present and future — inherits auditability for free.

## 1. Overview

- **One-line summary:** A Czech-language, mobile- and desktop-friendly household management system whose modules all write through a central audit-logging spine; v1 delivers that spine (with a rich log browser), a Trello-style to-do board, a forward-looking events module, and a dashboard that aggregates what's actionable right now.
- **Type:** fe/be pair. React + TS + TanStack Query SPA over a Go + embedded-SQLite backend.
- **Subdomain:** `home.tilcer.cz`.
- **Exposure:** **public** (routed via Coolify), gated by auth. The system is used from phones and laptops on and off the home network, so it must be reachable publicly and protected by the shared auth service — never open.
- **Consumers:**
  - **Household members** (browser, mobile + desktop) — the primary users; they run the dashboard, to-do board, and events.
  - **Admins** (household member with the `admin` role) — additionally browse the audit log.
  - No other service consumes `home` in v1. (If the logging spine is later extracted into a standalone service, *other* backends become consumers — see §Architecture and §10 D-notes.)
- **Depends on:**
  - **auth** (`auth.tilcer.cz`), site key **`home`**, **Mode A (auth-hosted login)**. Home holds only site-scoped JWTs; the backend verifies them via auth's `POST /introspect` as an auth **service client** bound to site `home`. No other internal dependency.
- **Naming:** module identifiers in code, API paths, and the audit log's `module` field are **English** (`logging`, `todo`, `events`, `dashboard`); the **UI is Czech** ("Nástěnka", "Okno do budoucnosti") — §10 D17, D20.

### Architecture — the logging spine (§10 D-arch)

The logging module is implemented as a **first-class, in-process Go package** with its **own dedicated SQLite tables in the home database** — the internal "spine" that every other module writes through. It is **not** a separate service in v1.

- **Same-transaction writes.** A module records an audit event **inside the same SQLite transaction** as the change it describes. The event and the change commit or roll back together, which *guarantees* no successful action goes unlogged and no logged action was rolled back. This atomicity is the reason the spine lives in-process and in the same DB.
- **Extraction seam.** Modules call the spine through a narrow `AuditSink` interface (record an event + its field changes). v1 ships one implementation: an in-process writer to the home DB. If a *separate* service later needs to log here, we add a second implementation — a BE→BE HTTP client — and stand up a `POST /internal/logs` ingest endpoint on a (possibly extracted) logging service, provisioned with its own auth service client. Nothing in module code changes. We do **not** build the ingest endpoint or extract the service until a second service actually needs it (YAGNI).
- **Two log planes, kept separate.** *Operational* request logs (method, path, status, latency, request id) go to **stdout** as structured JSON for Coolify to capture (the project baseline). *Domain* audit events (who did what to which entity, with field diffs) go to the **logging spine's DB tables** and are queryable through the log-browser frontend. These are different concerns and different stores.
- **Cross-module actions log under their owning module.** Marking a to-do done *from Nástěnka* logs as `todo.card.move` (not `dashboard.*`), with `meta.via = "dashboard"` recording where it was triggered. This keeps an entity's timeline complete regardless of which screen touched it.

### Architecture — recurrence & reminders (§10 D11–D14, D19)

Grounded in the standard guidance for calendar data (store the **rule**, not the expanded occurrences; expand on read within a bounded window; never materialize an open-ended series):

- **Events store a recurrence rule, not occurrences.** A one-off event has `rrule = NULL`; a recurring one stores an **RFC 5545 RRULE subset** string anchored at `starts_on`. Occurrences are **expanded on read** for the requested window only, with a hard cap.
- **Nothing is materialized per occurrence except completion.** The only per-occurrence row that ever exists is an `event_reminder_completions` record, written when someone ticks a reminder off. This sidesteps the infinite-series problem entirely — there is no table that grows with time.
- **Reminders are computed, not scheduled.** A reminder is "active" when today has reached `occurrence − lead`. Nástěnka evaluates this live on load. **No cron, no scheduler, no delivery pipeline** — which is why decision D9 (*no automated jobs in v1*) still holds. Nothing reaches you while the app is closed; that is the accepted trade-off.
- **One live reminder per event.** For a recurring event, only the **earliest uncompleted occurrence within the lookback window** is considered — mirroring the standard "keep the reminder on the next due occurrence and advance it when it fires" pattern. Past occurrences cannot pile up.

## 2. Goals & Non-Goals

**Goals**

- Ship a logging spine that captures **every mutating action across every module** (itself included) as a queryable audit event, written atomically with the change.
- Use the **hybrid** log model (see §5): every action produces an event; **key entities also record field-level before/after diffs**.
- Give admins a **detailed log browser**: filter by time, module, actor, action, entity, and level; free-text search; per-entity timeline; and simple analytics (counts by dimension over time).
- Ship a **Trello-style to-do system** pleasant on both mobile and desktop: many columns that can be **collapsed** and **sorted by priority**, "Právě dělám" columns, and an archive; cards with **notes, links, checklists, and labels**.
- Ship **Okno do budoucnosti**: a forward-looking, month-grouped list of all-day events with optional weekly/monthly/yearly recurrence, links, and an optional reminder at a chosen lead time.
- Ship **Nástěnka** as the landing page: one screen answering *"what needs my attention right now?"* — active event reminders plus everything in a "Právě dělám" column, each openable and completable in place.
- Make **most common actions reachable in one or two taps/clicks** from either form factor.
- Support a **household**: multiple people share the same boards and events; every change is attributed to its actor via the spine.
- Be a clean second consumer of **auth** (Mode A) and a faithful implementation of the project conventions (observability, Goose, Litestream→R2, OpenAPI 3.1).

**Non-Goals (v1)**

- **No push or email notifications.** Reminders are **in-app only**, computed when you open Nástěnka (§10 D11). Nothing arrives while the app is closed.
- **No per-occurrence exceptions or overrides.** Editing or deleting a recurring event affects the **whole series**; you cannot skip or move a single occurrence (§10 D14).
- **No time of day on events** — events are all-day (§10 D18).
- **No external calendar sync** (Google/Apple/CalDAV) and no iCal import/export in v1. Storing a real RRULE keeps that door open.
- **No multiple reminders per event** — one checkbox, one lead time.
- **No due dates on to-do cards**, and **no card assignee** — the household shares boards, and accountability comes from the audit log's actor. (Both are likely v2 additions.)
- **No collaborative editing** (live cursors, simultaneous co-editing of the same text via OT/CRDT). v1 *does* push changes over a websocket (§10 D10) so devices stay in sync — but concurrent edits to the same field are last-write-wins, not merged.
- **No separate logging microservice / BE→BE ingest endpoint** in v1 (the spine is in-process; only the extraction seam is built).
- **No retention/rotation policy tuning** beyond an optional prune switch (default: keep forever).
- **No offline / PWA install**, no native apps. Responsive web only.
- **No cross-board card moves**, no card templates, no automations/rules (Butler-style) in v1.

## 3. Users & Roles

Home is a consumer **site** (`home`) of the shared auth service. Household members are **`single_site`** accounts bound to the `home` site (their email is unique within `home`; the same address could exist independently on another site). Access uses **Mode A**: the frontend redirects unauthenticated users to `auth.tilcer.cz` to log in; auth owns the login UI, session cookie, and token refresh; home only ever holds a site-scoped JWT.

**Roles on the `home` site** — reuse auth's **default template `admin` / `editor` / `reader`** (§10 D5), mapped as:

- **`admin`** — full access, including the **log browser** and structural management (create/delete boards, columns, labels).
- **`editor`** — full use of **to-do**, **events**, and **Nástěnka** (create/move/edit/complete). No log browser.
- **`reader`** — **view-only**: read boards, cards, events, and Nástěnka; no mutations (cannot mark anything done), no log browser.

Authorization is carried in the site-scoped JWT `roles` claim (superuser tokens present `roles:["*"]` and are treated as full access). Home authorizes each request from the verified claims:

- **Reads** (`GET` on `/api/boards/**`, `/api/columns/**`, `/api/cards/**`, `/api/labels/**`, `/api/events/**`, `/api/dashboard`): any authenticated `home` user.
- **Writes** (`POST`/`PATCH`/`DELETE`/move/complete on the same): **`editor` or `admin`**.
- **Log browser** (`/api/logs/**`): **`admin` only**.
- Health probes: public.

## 4. Functional Requirements

Requirements are grouped by module. **Every mutating requirement in every module records an audit event through the logging spine, in the same transaction as the change** — stated once here, not repeated per FR. Reads are **not** logged (they would swamp the log); only mutations and explicit administrative actions are.

### Logging module (`logging`)

#### FR-L1: Record an audit event (spine, internal)
- **Description:** The internal API every module calls to record one audit event.
- **Trigger:** Any module mutation (user- or system-initiated). Not exposed over HTTP in v1.
- **Inputs:** `actor` (user id + type `user|system|service`), `module` (`logging|todo|events|dashboard`), `action` (dotted verb, e.g. `card.create`, `card.move`, `event.update`, `reminder.complete`), `entity_type` + `entity_id` (nullable), `summary` (human-readable), `level` (`info|warn|error`, default `info`), and request context (`request_id`, `ip`, `user_agent`, `site`) supplied by middleware. Optional `meta` (JSON — including `via` for cross-module triggers) and optional `changes` (see FR-L2).
- **Behaviour:** Insert one `audit_events` row **within the caller's open transaction**. Timestamp is server UTC. Actor/context are read from the request-scoped context populated by auth + logging middleware. If the surrounding transaction rolls back, the event is discarded with it.
- **Outputs:** the event id (for correlating child change rows).
- **Errors:** a failure to insert the event **fails the whole transaction** (the action does not silently succeed unlogged).

#### FR-L2: Field-level change capture (hybrid)
- **Description:** For **key entities**, record before/after values per changed field alongside the event.
- **Trigger:** A mutation on a key entity. **Key entities in v1:** `card`, `column`, `board`, `label`, `checklist_item`, `event` (§10 D6).
- **Inputs:** a list of `{ field, old, new }` for fields that actually changed (unchanged fields omitted); values serialized as text/JSON.
- **Behaviour:** Insert one `audit_changes` row per changed field, linked to the FR-L1 event, in the same transaction. Creates record `{field, null → new}`; deletes record the final state or a single `deleted` marker. **Full** old/new values are stored, including large text like card notes — no truncation (§10 D6). Reminder completions log an event but carry no field diffs (the row is create-only).
- **Errors:** as FR-L1 (atomic with the event and the change).

#### FR-L3: Browse / query events
- **Description:** List audit events with rich filtering for the log browser.
- **Inputs (query):** `from`, `to`, `module`, `actor`, `action`, `entity_type`, `entity_id`, `level`, `q` (FTS5 free-text over `summary` + `meta`), plus `limit` and `cursor` (UUIDv7 keyset, newest-first).
- **Behaviour:** Return matching events ordered by `ts` desc (id tiebreak), each with a change **count**. Filters compose (AND).
- **Outputs:** `200` paged list `{ items, next_cursor }`.
- **Errors:** `403` non-admin; `422` bad filter/range.

#### FR-L4: Event detail
- **Inputs:** event `id`. **Outputs:** `200` event + `changes[]`. **Errors:** `403`; `404`.

#### FR-L5: Entity timeline (audit trail of one thing)
- **Description:** The complete, ordered history of a single entity — the payoff of the hybrid model. Everything that ever happened to card X or event Y, with each field diff. Includes actions triggered from other screens (e.g. a card completed via Nástěnka).
- **Inputs:** `entity_type`, `entity_id`; optional `from`/`to`, `limit`, `cursor`.
- **Behaviour:** All events matching `(entity_type, entity_id)`, oldest-first (chronological), each with its changes.
- **Errors:** `403`; unknown entity ⇒ empty timeline, not an error.

#### FR-L6: Analytics / aggregations
- **Inputs:** `dimension` (`module|actor|action|level`), `bucket` (`day|week`), `from`, `to`, optional FR-L3 filters.
- **Behaviour:** Grouped counts for charting, plus top-N actors/actions for the range.
- **Errors:** `403`; `422`.

#### FR-L7: Retention (append-only + optional prune)
- **Description:** Audit data is **append-only**: no update or delete via any API. Optional prune deletes events older than `HOME_LOG_RETENTION_DAYS` (default `0` = keep forever). A prune run that deletes anything **records its own audit event** (`logging.prune`, `system` actor) — the module logging itself.

### To-do module (`todo`)

> Model recap: a **board** contains **many columns**; columns are **sortable** (priority and/or manual order) and **collapsible**; columns marked `kind=now` are the "Právě dělám" columns and `kind=done` the archive. A **card** lives in exactly one column and carries a title, markdown **notes**, structured **links**, a **checklist**, and **labels**. Workflow: cards wait in their columns, get **moved into "Právě dělám"** for the period, then **moved to Done** when finished.

#### FR-T1: Boards
- **Description:** CRUD of boards (top-level container). **Multiple boards** supported, surfaced via a board switcher (§10 D1).
- **Inputs:** `name`, optional `description`.
- **Behaviour:** On first run, seed a default board **"Domácnost"** with three columns — **"Zásobník"**, **"Právě dělám"** (`kind=now`), **"Hotovo"** (`kind=done`) — if no board exists. List returns boards in manual order.
- **Errors:** `403`; `404`; `409` duplicate name (if enforced).

#### FR-T2: Columns — CRUD, priority, sort, collapse
- **Inputs:** `board_id`, `name`, optional `priority`, optional `kind` (`normal|now|done`), `position`.
- **Behaviour:**
  - **Create/rename/delete**. Deleting a non-empty column requires `?cascade=true` (cards deleted → each logged); default blocks with `409` + card count.
  - **Reorder** via explicit move (lexorank string ordering, §10 D4). Columns can also be **sorted by `priority`** in the UI without changing manual order.
  - **Collapse/expand** is client-side (localStorage) per device (§10 D3).
  - `kind` is a **free-form UI hint**: any number of `now`/`done` columns per board. It drives mobile pinning, the `done_at` stamp, **and which cards Nástěnka picks up** (§10 D7).
- **Errors:** `403`; `404`; `409` (non-empty delete).

#### FR-T3: Cards — CRUD, move, reorder
- **Behaviour:**
  - **Create** appends to a column. **Edit** title/notes. **Delete** is **soft** by default (`archived=true`), hard delete behind `?hard=true` (§10 D8).
  - **Move** to another column and/or position in one operation. Moving into a `kind=done` column stamps `done_at`; moving out clears it.
  - Reordering within a column uses the same ordering scheme as columns.
- **Errors:** `403`; `404`; `422` (unknown target column).

#### FR-T4: Card notes & links
- **Behaviour:** `notes` is markdown. **Links** are structured rows `{ url, title? }`, ordered, managed independently so they list and tap cleanly on mobile. URLs validated (scheme allowlist `http`/`https`).
- **Errors:** `422` invalid URL.

#### FR-T5: Checklists (sub-todos)
- **Behaviour:** One ordered checklist per card; add/rename/check/uncheck/delete/reorder. Card surfaces progress (`done/total`).
- **Errors:** `403`; `404`.

#### FR-T6: Labels / tags
- **Behaviour:** CRUD board-scoped labels; attach/detach to cards; filter the board by label. Deleting a label detaches it everywhere (logged).
- **Errors:** `403`; `404`; `409` duplicate name per board.

#### FR-T7: Board fetch & filtering (read model)
- **Inputs:** `board_id`; optional `label`, `q`, `include_archived`.
- **Behaviour:** Return the board **tree** — columns in sort order, each with cards in order, each card with label ids and checklist progress. Filtering narrows cards; empty columns still render. Collapse applied client-side.
- **Errors:** `403`; `404`.

### Okno do budoucnosti (`events`)

> Model recap: an **event** is an **all-day** dated thing in the future, with a title, description, and links. It may **recur** weekly, monthly, or yearly. It may raise **one reminder**, a chosen lead time before the date. The page lists events **grouped by month**.

#### FR-E1: Create / edit / delete an event
- **Description:** CRUD of events.
- **Inputs:** `title` (required), `description` (markdown, optional), `starts_on` (**date**, required — no time, §10 D18), optional `rrule` (see FR-E2), `reminder_enabled` (bool), `reminder_lead` (`1d|2d|1w|2w|1m`, required when `reminder_enabled`).
- **Behaviour:** Create/update/delete. **Edits and deletes apply to the whole series** — there are no per-occurrence exceptions (§10 D14); the UI must say so before saving a change to a recurring event. Delete is **soft** by default (`archived=true`), hard behind `?hard=true`, consistent with D8. Deleting an event cascades its links and its reminder completions.
- **Errors:** `403`; `404`; `422` (missing title/date, `reminder_lead` absent while enabled, malformed `rrule`).

#### FR-E2: Recurrence
- **Description:** Weekly / monthly / yearly repetition, stored as an **RFC 5545 RRULE subset** (§10 D13).
- **Inputs:** UI exposes exactly: **nikdy** (none), **týdně**, **měsíčně**, **ročně**, plus an optional **end date**.
- **Behaviour:** Persist as an RRULE string anchored at `starts_on` — `FREQ=WEEKLY|MONTHLY|YEARLY`, always `INTERVAL=1` in v1, optional `UNTIL=`. `rrule = NULL` means a one-off. Occurrences are **expanded on read** for the requested window only, never stored, capped at `HOME_RRULE_MAX_OCCURRENCES` per event per request (default 500) to bound open-ended series. Expansion resolves against `HOME_TIMEZONE` (`Europe/Prague`).
  - **Short-month clamping (§10 D19 — a deliberate deviation from RRULE defaults):** RFC 5545 `FREQ=MONTHLY` anchored on the 31st **skips** months that have no 31st, and `FREQ=YEARLY` on 29 February skips non-leap years. For household use ("zaplatit 31."), a silently skipped month is a bug. So when the anchor day exceeds the target month's length, the occurrence **clamps to the last day of that month** (31 Jan → 28/29 Feb; 29 Feb → 28 Feb). This must be implemented as an explicit post-expansion adjustment and covered by tests.
- **Errors:** `422` unsupported/malformed rule.

#### FR-E3: Event links
- **Behaviour:** Structured `{ url, title? }` rows on an event, ordered, added/removed independently — same shape and validation as card links (FR-T4).
- **Errors:** `422` invalid URL.

#### FR-E4: Reminder configuration
- **Description:** The per-event checkbox that turns an event into something that shows up on Nástěnka.
- **Inputs:** `reminder_enabled` (bool) + `reminder_lead` ∈ **1 day, 2 days, 1 week, 2 weeks, 1 month**.
- **Behaviour:** Purely declarative — enabling a reminder **schedules nothing**. Whether a reminder is *active* is computed at read time (FR-N1). Changing the lead time immediately changes which reminders appear, with no backfill or catch-up.
- **Errors:** `422` (`reminder_lead` missing while enabled, or not one of the five values).

#### FR-E5: List events by month
- **Description:** The Okno do budoucnosti page.
- **Inputs:** a window — default **current month forward**; the UI can page to other months, including past ones (events are retained, never auto-deleted). Optional `include_archived`.
- **Behaviour:** Expand all non-archived events' occurrences within the window and return them **grouped by month**, ascending. Each returned occurrence carries its parent event's fields, its `occurrence_on` date, whether the parent recurs, and its reminder config. Expansion is capped as in FR-E2.
- **Outputs:** `200` months → occurrences.
- **Errors:** `403`; `422` (window too large — cap the span, e.g. 24 months).

#### FR-E6: Complete a reminder occurrence
- **Description:** Tick off the reminder for one occurrence (usually from Nástěnka, but available from the event too).
- **Inputs:** `event_id`, `occurrence_on` (date).
- **Behaviour:** Insert an `event_reminder_completions` row for `(event_id, occurrence_on)`; idempotent (a second call is a no-op, not an error). This is the **only** per-occurrence row that ever exists. For a recurring event, completing the current occurrence means the **next** occurrence becomes the live one (FR-N1) — no new row is created for it until it too is completed. Undo is supported by deleting the completion row (logged).
- **Errors:** `403`; `404` unknown event; `422` `occurrence_on` is not a real occurrence of that event.

### Nástěnka (`dashboard`)

> Model recap: the **landing page**. Two lists — active event reminders, and every to-do currently sitting in a "Právě dělám" (`kind=now`) column. Tap either for a detail dialog; mark either done in place. **Active items only** (§10 D16).

#### FR-N1: Dashboard aggregation (read model)
- **Description:** One request returning everything that needs attention now.
- **Behaviour:** Two sections computed live:
  - **Události (event reminders).** For each non-archived event with `reminder_enabled`: expand occurrences and take the **earliest uncompleted occurrence** whose date is `>= today − HOME_DASHBOARD_LOOKBACK_DAYS` (default 30). That occurrence is **active** when `today >= occurrence_on − reminder_lead`. At most **one active reminder per event** — a recurring event never stacks up. Each item is flagged **`overdue`** when `occurrence_on < today`. Sorted overdue-first, then by `occurrence_on` ascending.
  - **Úkoly (to-dos).** Every non-archived card in any column with `kind=now`, across all non-archived boards (§10 D7 allows several such columns, and D1 several boards). Each item carries its board and column for grouping. Sorted by board order, then column priority, then card position.
- **Outputs:** `200` `{ reminders[], tasks[] }` with enough per-item detail to render a row without a second request.
- **Errors:** `403`.

#### FR-N2: Detail dialog
- **Description:** Opening any Nástěnka row shows full detail without leaving the page.
- **Behaviour:** A to-do row opens the **card detail** (title, notes, links, checklist, labels — same component as the board). A reminder row opens the **event detail** (title, description, links, date, recurrence, lead time). Read-only for `reader`.
- **Errors:** `403`; `404`.

#### FR-N3: Mark done from Nástěnka
- **Description:** Complete an item in place, via a **press-and-hold confirm** on the row's done control (§10 D22). The gesture is deliberate: this is the most frequent action on the page *and* the page is the landing route, so an accidental tap would silently complete something the household still needs.
- **Gesture spec:** hold the row's done control for **2000 ms** to commit; a fill animation runs over ~1.9 s so the progress is visible; releasing early cancels with no side effect. A short tap on the control does **nothing** (it must not fall through to opening the row). Tapping the row *body* still opens the detail dialog (FR-N2).
- **Accessible equivalent (required):** a sustained press is not operable for every user, so the hold must not be the only path. Keyboard/assistive activation of the done control (`Enter`/`Space`) commits **immediately, without a hold**, and the detail dialog's explicit "✓ Hotovo" button is likewise a **single** activation — opening the dialog is already the deliberate step. Screen-reader users get the control labelled as a normal action, not as a hold.
- **Behaviour:**
  - **To-do:** move the card to its board's **first `kind=done` column** (by column order), stamping `done_at` — identical to dragging it there on the board. If the board has **no** `kind=done` column, fall back to `archived=true` (§10 D15). Logged as `todo.card.move` with `meta.via="dashboard"`.
  - **Reminder:** record a completion for that occurrence (FR-E6). Logged as `events.reminder.complete` with `meta.via="dashboard"`.
  - Both are optimistic in the UI with rollback, and both broadcast over the websocket so other devices' dashboards update.
- **Errors:** `403` (`reader`); `404`; `422`.

### Health

#### FR-H1: Health probes
- `GET /healthz` (liveness) and `GET /readyz` (readiness incl. SQLite ping), per the project baseline. Public.

## 5. Data Model

SQLite (embedded), Goose migrations. Timestamps UTC ISO-8601; **event dates are plain `DATE` (all-day, no time)**. IDs **UUIDv7** (sortable, keyset-pagination friendly). Ordering fields (`position`) use **lexorank-style string keys** — always insertable between two neighbors, so reorder/move never rewrites siblings (§10 D4).

### Logging tables (created first — "build everything around it")

**audit_events**
- `id` PK (UUIDv7) · `ts` TIMESTAMP NOT NULL · `actor_user_id` TEXT NULL · `actor_type` TEXT CHECK in (`user`,`system`,`service`) · `actor_label` TEXT NULL · `module` TEXT NOT NULL · `action` TEXT NOT NULL · `entity_type` TEXT NULL · `entity_id` TEXT NULL · `summary` TEXT NOT NULL · `level` TEXT CHECK in (`info`,`warn`,`error`) DEFAULT `info` · `request_id` TEXT NULL · `ip` TEXT NULL · `user_agent` TEXT NULL · `site` TEXT NOT NULL DEFAULT `home` · `meta` TEXT NULL (JSON; carries `via` for cross-module triggers).
- Indexes: `idx_events_ts` (`ts` desc), `idx_events_module_ts`, `idx_events_actor_ts`, `idx_events_entity` (`entity_type`,`entity_id`,`ts`), `idx_events_action`.
- **Free-text search:** a **SQLite FTS5** virtual table (`audit_events_fts`) indexes `summary` + flattened `meta`, synced via triggers (§10 D6).
- **Append-only:** no `UPDATE`/`DELETE` path except FR-L7 prune.

**audit_changes**
- `id` PK · `event_id` FK→audit_events ON DELETE CASCADE · `field` TEXT NOT NULL · `old_value` TEXT NULL · `new_value` TEXT NULL. **Full** values, no truncation (§10 D6).
- Index: `idx_changes_event`, optional `idx_changes_field`.

### To-do tables

**boards** — `id` PK · `name` TEXT NOT NULL · `description` TEXT NULL · `position` · `created_by` · `created_at` · `archived` BOOL DEFAULT false.

**columns** — `id` PK · `board_id` FK→boards ON DELETE CASCADE · `name` TEXT NOT NULL · `priority` INTEGER NOT NULL DEFAULT 0 · `position` · `kind` TEXT CHECK in (`normal`,`now`,`done`) DEFAULT `normal` · `created_at`. Indexes: `(board_id, position)`, `(board_id, priority)`, **`(kind)` — Nástěnka queries `kind='now'` across boards**. `kind` is a non-unique hint (§10 D7).

**cards** — `id` PK · `column_id` FK→columns ON DELETE CASCADE · `title` TEXT NOT NULL · `notes` TEXT NULL · `position` · `created_by` · `created_at` · `updated_at` · `done_at` TIMESTAMP NULL · `archived` BOOL DEFAULT false. Indexes: `(column_id, position)`, `idx_cards_updated`.

**card_links** — `id` PK · `card_id` FK ON DELETE CASCADE · `url` TEXT NOT NULL · `title` TEXT NULL · `position`. Index `(card_id, position)`.

**checklist_items** — `id` PK · `card_id` FK ON DELETE CASCADE · `text` TEXT NOT NULL · `done` BOOL DEFAULT false · `position`. Index `(card_id, position)`.

**labels** — `id` PK · `board_id` FK ON DELETE CASCADE · `name` TEXT NOT NULL · `color` TEXT NOT NULL · `created_at`. Unique `(board_id, name)`.

**card_labels** — `card_id` FK ON DELETE CASCADE · `label_id` FK ON DELETE CASCADE · PK `(card_id, label_id)`. Index `(label_id)`.

*(No `user_column_state` table — column collapse is client-side/localStorage per §10 D3.)*

### Events tables (Okno do budoucnosti)

**events**
- `id` PK (UUIDv7) · `title` TEXT NOT NULL · `description` TEXT NULL (markdown) · `starts_on` **DATE** NOT NULL (all-day anchor; §10 D18) · `rrule` TEXT NULL (RFC 5545 subset; NULL = one-off; §10 D13) · `timezone` TEXT NOT NULL DEFAULT `Europe/Prague` · `reminder_enabled` BOOL NOT NULL DEFAULT false · `reminder_lead` TEXT NULL CHECK in (`1d`,`2d`,`1w`,`2w`,`1m`) · `created_by` · `created_at` · `updated_at` · `archived` BOOL DEFAULT false.
- CHECK: `reminder_enabled = 0 OR reminder_lead IS NOT NULL`.
- Indexes: `idx_events_starts_on` (`starts_on`), `idx_events_reminder` (`reminder_enabled`) partial `WHERE archived = 0`.
- **No occurrences table by design** (§10 D11) — the rule is the storage.

**event_links** — `id` PK · `event_id` FK→events ON DELETE CASCADE · `url` TEXT NOT NULL · `title` TEXT NULL · `position`. Index `(event_id, position)`.

**event_reminder_completions**
- `id` PK · `event_id` FK→events ON DELETE CASCADE · `occurrence_on` **DATE** NOT NULL · `completed_by` TEXT · `completed_at` TIMESTAMP NOT NULL.
- **Unique `(event_id, occurrence_on)`** — one completion per occurrence; makes FR-E6 naturally idempotent.
- Index `(event_id, occurrence_on)`.
- The **only** per-occurrence row in the system. Sparse: written on completion, never pre-created.

**Goose notes:** `0001_init` creates the **logging tables first** (incl. `audit_events_fts` + triggers), then to-do tables, then events tables, all indexes and CHECKs, and seeds the default **"Domácnost"** board + `Zásobník`/`Právě dělám`/`Hotovo` columns **only when `boards` is empty** (so a fresh build restored from R2 does not double-seed). Future migrations: card assignee, card due dates, per-occurrence event overrides, multiple checklists, richer RRULE (`INTERVAL`, `BYDAY`).

## 6. API Surface

Full detail in `openapi.yaml`. All application endpoints are under `/api` and require a valid site-scoped **JWT** (`bearerAuth`); writes require `editor`/`admin`; log endpoints require **`admin`**; health is public. There are **no BE→BE endpoints in v1**.

- **To-do:**
  - `GET/POST /api/boards`, `GET/PATCH/DELETE /api/boards/{id}`, `GET /api/boards/{id}/tree` (FR-T7).
  - `GET/POST /api/boards/{id}/columns`, `PATCH/DELETE /api/columns/{id}`, `POST /api/columns/{id}/move`. *(Collapse is client-side — no endpoint.)*
  - `POST /api/columns/{id}/cards`, `GET/PATCH/DELETE /api/cards/{id}`, `POST /api/cards/{id}/move`.
  - `GET/POST /api/cards/{id}/links`, `DELETE /api/links/{id}`.
  - `GET/POST /api/cards/{id}/checklist`, `PATCH/DELETE /api/checklist/{id}`.
  - `GET/POST /api/boards/{id}/labels`, `PATCH/DELETE /api/labels/{id}`, `POST/DELETE /api/cards/{id}/labels/{labelId}`.
- **Events (Okno do budoucnosti):**
  - `GET/POST /api/events`, `GET/PATCH/DELETE /api/events/{id}` — series-level CRUD (FR-E1).
  - `GET /api/events/occurrences?from=&to=` — expanded, month-grouped occurrences (FR-E5).
  - `GET/POST /api/events/{id}/links`, `DELETE /api/event-links/{id}` (FR-E3).
  - `POST /api/events/{id}/complete` `{ occurrence_on }` and `DELETE /api/events/{id}/complete?occurrence_on=` (FR-E6, idempotent + undo).
- **Nástěnka:** `GET /api/dashboard` (FR-N1) — the aggregation. Mark-done reuses the owning module's endpoints (`POST /api/cards/{id}/move`, `POST /api/events/{id}/complete`) so there is exactly one code path per action.
- **Logs (admin):** `GET /api/logs`, `GET /api/logs/{id}`, `GET /api/logs/entity/{type}/{id}`, `GET /api/logs/stats`.
- **Real-time:** `GET /ws` — authenticated **websocket**; pushes board/column/card **and** event/completion changes so open boards *and* dashboards update live (§10 D10). Not modeled in `openapi.yaml` (HTTP only).
- **Health (public):** `GET /healthz`, `GET /readyz`.

**Auth per endpoint:** `security: []` for health; `bearerAuth` for everything under `/api` and the `/ws` upgrade. Writes require `editor`/`admin`; `/api/logs/**` require `admin` (checked from JWT `roles`). JWTs verified via auth `POST /introspect` as the `home` service client with **short-TTL caching** (§10 D2).

**Conventions:** list/log endpoints use `?limit=&cursor=` (UUIDv7 keyset). Occurrence and dashboard endpoints are **window-bounded and capped**, never unbounded. Errors use the shared `Error { error, detail }` schema. Reorder/move use lexorank positions to avoid rewriting siblings.

**Routing note:** `/api/events/occurrences` shares a prefix with `/api/events/{id}` — register the **static** route before the parameterised one so `occurrences` is never parsed as an event id. (Chi and most Go routers do this correctly by default; it still needs a test.)

## 7. Frontend

React + TS + TanStack Query SPA, **mobile-first and desktop-friendly**, **Czech-language UI** (§10 D20) on a **dark-default theme** (§10 D21). App shell navigation — **Nástěnka · Úkoly · Okno do budoucnosti · Log** (last one admin-only) — as a **bottom tab bar on mobile / side nav on desktop**. **Nástěnka is the landing route** (§10 D16). Auth is Mode A: an unauthenticated load redirects to `auth.tilcer.cz` (site `home`); a shared fetch wrapper attaches the JWT and, on `401`, calls auth `POST /token/refresh` once and retries, else redirects to login.

**Nástěnka (landing)**

- Two clearly separated lists: **Události** (active event reminders) and **Úkoly** (cards in "Právě dělám" columns), grouped by board when more than one board has such columns.
- **Overdue reminders are visually distinct** and sort to the top.
- Each row is tappable → detail dialog (card detail or event detail, §FR-N2) and carries a **press-and-hold done control** (2000 ms, with a visible fill; §FR-N3, §10 D22) applied optimistically with rollback. Keyboard activation commits immediately — see FR-N3's accessible equivalent.
- Empty state matters: "nothing needs you right now" is a *good* outcome and should look deliberate, not broken.

**Úkoly (to-do board)**

- A lightweight **board switcher** selects the active board (§10 D1).
- **Desktop:** horizontal **kanban**. Columns **collapsible** (thin labeled spine; state per device via localStorage, §10 D3) and **sortable** (drag, or sort by priority). Drag-and-drop for cards and columns (dnd-kit). "Právě dělám" columns emphasized.
- **Mobile:** a **vertical accordion** of collapsible columns, `now` columns **pinned to the top**. An explicit **"Přesunout do…" control** alongside touch drag, so the core action is one tap → pick target column. Quick-add card per column.
- **Card detail:** full-screen sheet on mobile, dialog on desktop — title, markdown notes, links, checklist, labels.
- **Filtering:** label chips + text search; toggle archived/done.

**Okno do budoucnosti**

- **Month-grouped list**, current month forward by default, pageable to other months (including past).
- Each row: date, title, recurrence indicator, reminder indicator (with lead time).
- **Event form:** title, description (markdown), links, date picker, recurrence selector (*nikdy / týdně / měsíčně / ročně* + optional end date), reminder checkbox + lead-time selector (*1 den / 2 dny / 1 týden / 2 týdny / 1 měsíc*).
- **Series-edit warning:** because there are no per-occurrence exceptions (§10 D14), editing or deleting a recurring event must state plainly that it affects **all** occurrences, before saving.

**Log (admin)**

- **Filter bar:** date range, module, actor, action, entity type/id, level, free-text `q`.
- **Result stream:** newest-first, rows expandable to reveal `old → new` field diffs.
- **Entity timeline:** full chronological history of one entity, reachable from an event row or from a card/event detail.
- **Analytics panel:** counts by module/actor/action over the range, plus top-N.

**Data fetching (TanStack Query):**

- Keys: `['dashboard']`, `['boards']`, `['board', id, 'tree', {filters}]`, `['card', id]`, `['board', id, 'labels']`, `['events', {window}]`, `['event', id]`, `['logs', {filters}]`, `['logs','entity',type,id]`, `['logs','stats',{params}]`.
- **Optimistic updates** for move/reorder/check/complete with rollback; on settle, invalidate the affected board tree **and** `['dashboard']` (a card moved on the board can change what Nástěnka shows).
- **Real-time (§10 D10):** an authenticated **websocket** (`/ws`) pushes card/column/board and event/completion changes; applied via `setQueryData`/targeted invalidation so boards and dashboards stay live. Refetch-on-focus covers reconnects.
- Explicit **empty / loading / error** states everywhere.

## 8. Non-Functional Requirements

- **Observability (baseline):** `/healthz`, `/readyz` (SQLite ping), **structured JSON logs to stdout**, per-request log (method, path, status, latency, request id). The request id is stamped onto audit events (FR-L1), tying the two planes together.
- **Audit completeness & integrity:** audit writes are **in the same transaction** as the mutation — an action cannot succeed unlogged, and a rolled-back action leaves no event. Audit tables are **append-only** (prune is the sole exception and self-logs).
- **Bounded computation:** occurrence expansion (FR-E2/E5) and dashboard evaluation (FR-N1) are always **window-bounded and capped** (`HOME_RRULE_MAX_OCCURRENCES`, a max window span, and the dashboard lookback). An open-ended recurring event must never be able to produce unbounded work or an unbounded response.
- **Date correctness:** events are all-day, so there is **no clock-time DST hazard**; but "today", month boundaries, and lead-time arithmetic are all evaluated in `HOME_TIMEZONE` (`Europe/Prague`), never in UTC, or reminders would flip a day early/late around midnight. Short-month clamping (§10 D19) must be unit-tested against 31 Jan monthly and 29 Feb yearly.
- **Performance:** household scale (a handful of users, thousands of cards, a growing audit log). Targets: p95 < 50 ms for board-tree, dashboard, and indexed log queries. Dashboard is the most-loaded route (it's the landing page) — keep it a single query round and avoid N+1 expansion across events.
- **Security:** JWT validated via auth `/introspect` (home service client) with short-TTL caching; **role gating** enforced server-side from claims (`editor`/`admin` for writes, `admin` for logs) and never trusted from the client; the **websocket** authenticates on connect and is role-authorized; input validation on every write (URL scheme allowlist, length caps, `reminder_lead` and `rrule` whitelisted rather than free-form); secrets via **Coolify env** only; HTTPS/WSS via Coolify.
- **Backup:** **Litestream → Cloudflare R2**, prefix **`home/`**. Fresh build **restores from R2 before serving**; seed runs **only if empty after restore**.

## 9. Configuration

Env vars (values live in Coolify; nothing secret in the repo):

- `HOME_DB_PATH` — SQLite file path (persisted volume).
- `HOME_SITE_KEY` — auth site key (default `home`).
- `AUTH_BASE_URL` — `https://auth.tilcer.cz` (login redirect, `/introspect`, `/token/refresh`).
- `HOME_AUTH_SERVICE_SECRET` — auth **service-client** secret bound to site `home`, for `POST /introspect` (cached for the token TTL). *(Local HS256 verify considered and rejected — §10 D2.)*
- `HOME_ALLOWED_ORIGINS` — CORS allowlist (`https://*.tilcer.cz`).
- `HOME_TIMEZONE` — IANA zone for "today", month boundaries, and recurrence expansion. Default `Europe/Prague`.
- `HOME_DASHBOARD_LOOKBACK_DAYS` — how long an uncompleted reminder stays on Nástěnka after its date. Default `30`.
- `HOME_RRULE_MAX_OCCURRENCES` — expansion safety cap per event per request. Default `500`.
- `HOME_LOG_RETENTION_DAYS` — audit prune threshold; default `0` = keep forever.
- `LITESTREAM_*` / R2 credentials — backup to prefix `home/`.

## 10. Resolved Decisions

Resolved with Karel on 2026-07-19. **D1–D10** cover the original two modules; **D11–D19** the two added modules; **D20–D21** are product-level design inputs (detail in `HANDOFF-design.md`).

- **D1 — Boards:** **multiple boards**, with a board switcher in the UI.
- **D2 — JWT verification:** auth **`POST /introspect`** as the `home` service client with **short-TTL caching**. Signing secret not distributed.
- **D3 — Collapse state:** column collapse is **client-side (localStorage)**, per device.
- **D4 — Ordering scheme:** **lexorank-style string keys** for `position`.
- **D5 — Home roles:** auth's **default `admin`/`editor`/`reader`**. Log browser **`admin`-only**.
- **D6 — Diffs & search:** field diffs for `card`, `column`, `board`, `label`, `checklist_item`, **`event`**, storing **full** old/new values. Log free-text via **FTS5**.
- **D7 — `now`/`done` columns:** `kind` is a **free-form, non-unique hint**. It drives mobile pinning, the `done_at` stamp, and **which cards Nástěnka shows**.
- **D8 — Deletion:** **soft** by default (`archived=true`), hard behind `?hard=true` — for cards **and** events.
- **D9 — Automated actions:** **none in v1** — and D11 keeps it that way, since reminders are computed rather than scheduled.
- **D10 — Real-time:** **websockets**; extended to push **event and completion** changes too, so Nástěnka stays live.
- **D11 — Reminder mechanism:** **in-app, computed on read.** A reminder is active when `today >= occurrence − lead`; Nástěnka evaluates it live. No scheduler, no email/push, no delivery pipeline. Only **completions** are persisted.
- **D12 — Reminder entity:** event reminders are a **separate entity**, shown in their own Nástěnka list. They never become to-do cards, so recurrence never pollutes the kanban.
- **D13 — Recurrence storage:** an **RFC 5545 RRULE subset** string (`FREQ=WEEKLY|MONTHLY|YEARLY`, `INTERVAL=1`, optional `UNTIL`), anchored at `starts_on`, expanded on read with a library (`teambition/rrule-go`). UI exposes only the three frequencies in v1. *Risk noted: rrule-go has seen little recent release activity; the supported subset is small enough to hand-roll if it becomes a problem.*
- **D14 — Exceptions:** **series-only editing.** No per-occurrence skips or overrides (no `EXDATE`) in v1; edits and deletes affect the whole series, and the UI says so before saving.
- **D15 — Nástěnka mark-done (to-do):** move the card to its board's **first `kind=done` column**, stamping `done_at`; fall back to `archived=true` if the board has none.
- **D16 — Nástěnka scope:** it is the **landing page** and shows **active items only** — reminders past their lead time plus cards in `kind=now` columns. No look-ahead section.
- **D17 — Identifiers:** **English in code**, Czech in the UI. Modules are `logging`, `todo`, `events`, `dashboard`.
- **D18 — Event timing:** events are **date-only (all-day)**. No time of day, which removes clock-time DST hazards entirely.
- **D19 — Short-month clamping:** occurrences **clamp to the last day of the month** when the anchor day doesn't exist (31 Jan monthly → 28/29 Feb; 29 Feb yearly → 28 Feb). A **deliberate deviation from RFC 5545 default semantics**, which would skip those months — silently skipping a household recurrence is a bug, not correctness.
- **D20 — UI language:** **Czech-only.** No language switcher, no i18n framework; strings centralized. Czech plural rules (1 / 2–4 / 5+) apply to every count label.
- **D21 — Theme:** **dark is the default**, light is secondary. Tokens for both; dark values in `:root`, light under `.light`.
- **D22 — Nástěnka done gesture: press-and-hold, 2000 ms.** *Supersedes the earlier "one-tap done" requirement* (resolved 2026-07-21 after reviewing the Claude Design prototype, which had implemented the hold). Rationale: Nástěnka is the landing route and its rows are dense, so a stray tap would silently complete something still outstanding; a deliberate 2 s hold with a visible fill makes completion intentional. Accepted cost: the app's most frequent action is slower than a tap. **Mandatory counterpart** — the hold must never be the only path: keyboard/assistive activation commits immediately, and the detail dialog's "✓ Hotovo" stays a single activation (FR-N3).

## 11. Acceptance Criteria

- [ ] PRD + `openapi.yaml` reviewed and approved; decisions (§10) locked.
- [ ] `home` site registered in **auth** with roles **`admin`/`editor`/`reader`**; a `home` **service client** provisioned for `/introspect` (cached); Mode A login redirect + refresh loop works end-to-end.
- [ ] Goose `0001_init` creates **logging tables first** (incl. **FTS5** + triggers), then to-do, then events tables, with all indexes/CHECKs; seeds **Domácnost** + `Zásobník`/`Právě dělám`/`Hotovo` **only when empty**.
- [ ] **Logging spine:** every mutation in **all four modules** writes an `audit_events` row **in the same transaction**; a forced rollback leaves **no** event; a forced event-insert failure rolls back the mutation.
- [ ] **Hybrid diffs:** key-entity mutations (incl. `event`) write `audit_changes` with **full** old/new values; event detail and entity timeline show `old → new`.
- [ ] **Cross-module attribution:** completing a to-do from Nástěnka logs as `todo.card.move` with `meta.via="dashboard"` and appears in that card's entity timeline.
- [ ] **Log browser:** filter by time/module/actor/action/entity/level + **FTS5** free-text; per-entity timeline; analytics; **non-admin gets `403`**.
- [ ] **Role gating:** `reader` gets `403` on every mutation across all modules (including Nástěnka mark-done); `editor`/`admin` succeed; only `admin` reaches `/api/logs/**`.
- [ ] **To-do:** multiple boards + switcher; boards/columns/cards CRUD; columns reorder + priority-sort (lexorank) + collapse (client-side); cards move between columns (→ a `done` column stamps `done_at`); notes, links (validated), checklist, labels.
- [ ] **Events CRUD:** create/edit/delete all-day events with description and links; **soft delete** default.
- [ ] **Recurrence:** weekly/monthly/yearly expand correctly on read; **no occurrences are ever persisted**; expansion respects `HOME_RRULE_MAX_OCCURRENCES` and the window cap; an open-ended series cannot produce unbounded work.
- [ ] **Short-month clamping (D19):** a monthly event anchored 31 Jan yields 28 Feb (29 in a leap year) — **not** a skipped month; a yearly event on 29 Feb yields 28 Feb in non-leap years. Unit-tested.
- [ ] **Reminders:** an event with a lead time appears on Nástěnka exactly when `today >= occurrence − lead`, evaluated in `Europe/Prague`; at most **one active reminder per event**; completing one advances a recurring series to the next occurrence; completion is **idempotent** and undoable.
- [ ] **Series-only editing (D14):** editing a recurring event changes all occurrences, and the UI warns before saving.
- [ ] **Nástěnka:** is the **landing route**; shows active reminders + all cards in `kind=now` columns across boards; overdue reminders flagged and sorted first; detail dialog for both types; **mark-done moves the card to the first `kind=done` column** (or archives if none).
- [ ] **Real-time:** `/ws` pushes card/column/board **and** event/completion changes; two devices see each other's edits live on both the board and the dashboard; reconnect falls back to refetch-on-focus.
- [ ] **Nástěnka done gesture (D22):** the row control commits only after a **2000 ms hold** with a visible fill; early release cancels cleanly; a short tap does nothing and does not fall through to opening the row.
- [ ] **Accessible equivalent (D22):** keyboard/assistive activation of the done control commits **immediately without a hold**, and the detail dialog's "✓ Hotovo" is a single activation. Verified with keyboard only.
- [ ] **Mobile UX:** board accordion with `now` columns pinned and one-tap "Přesunout do…"; ≤2-tap common actions; desktop kanban with drag reorder. Verified at real viewport sizes.
- [ ] **Czech UI (D20)** throughout, incl. correct plural forms; **dark theme default (D21)**.
- [ ] Baseline observability present; stdout request ids appear on audit events.
- [ ] **Litestream→R2 (`home/`)** configured; fresh-build **restore** verified; seed does not double-run after restore.
- [ ] Service registered in `REGISTRY.md`.
