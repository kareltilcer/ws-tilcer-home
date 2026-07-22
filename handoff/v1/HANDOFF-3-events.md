# Home — Module 3: `events` (Okno do budoucnosti)

> **Read first:** `HANDOFF.md` (foundation + shared conventions), then PRD §4 FR-E1–E6, §5 events tables, §6 events endpoints, §7 Okno screen, and the architecture note "recurrence & reminders".
> **Depends on:** foundation (F1–F6) and the audit spine (`HANDOFF-1-logging.md`). **Blocks:** `HANDOFF-4-dashboard.md`.
> **Scope:** all-day future events with optional recurrence and one optional in-app reminder.

## The three ideas that make this module simple

Recurring events are where calendar code usually goes wrong. This design avoids the usual traps by construction — **don't optimise these away**:

1. **Store the rule, not the occurrences.** There is deliberately **no occurrences table**. A series is one row with an RRULE; occurrences are expanded on read, for the requested window only. Pre-computing occurrences is the classic mistake: an open-ended series is infinite.
2. **Nothing is materialised per occurrence except completion.** The only per-occurrence row that ever exists is `event_reminder_completions`, written when someone ticks a reminder off. No table in this module grows with the passage of time.
3. **Reminders are computed, not scheduled.** There is **no cron, no queue, no delivery pipeline** (D9/D11). Whether a reminder is active is a function of today's date, evaluated when Nástěnka loads. If you find yourself adding a scheduler, stop — the design decision was explicit.

## 1. Data model (PRD §5)

**events** — `id` (UUIDv7) · `title` · `description` NULL (markdown) · `starts_on` **DATE** · `rrule` TEXT NULL · `timezone` DEFAULT `Europe/Prague` · `reminder_enabled` BOOL DEFAULT false · `reminder_lead` TEXT NULL CHECK(`1d`,`2d`,`1w`,`2w`,`1m`) · `created_by` · `created_at` · `updated_at` · `archived`.

- CHECK `reminder_enabled = 0 OR reminder_lead IS NOT NULL` — a reminder without a lead is not representable.
- Indexes: `(starts_on)`, and a partial `(reminder_enabled) WHERE archived = 0` (module 4's hot query).
- **`starts_on` is a DATE, not a timestamp** (D18). There is no time of day anywhere in this module.

**event_links** — mirrors `card_links`: `id` · `event_id` FK CASCADE · `url` · `title` NULL · `position`.

**event_reminder_completions** — `id` · `event_id` FK CASCADE · `occurrence_on` **DATE** · `completed_by` · `completed_at`. **Unique `(event_id, occurrence_on)`** — this makes completion naturally idempotent, which FR-E6 requires. Sparse: written on completion, never pre-created.

## 2. Recurrence

### Storage (D13)

An RFC 5545 **RRULE subset** string anchored at `starts_on`. v1 persists and accepts only:

```
FREQ=WEEKLY|MONTHLY|YEARLY  ·  INTERVAL=1  ·  optional UNTIL=
```

`rrule = NULL` means a one-off event. The UI exposes exactly four choices — *nikdy · týdně · měsíčně · ročně* — plus an optional end date. **Validate against a whitelist**, don't accept arbitrary RRULE text: anything outside the subset is `422`. (Storing a real RRULE keeps iCal export and richer rules cheap later; accepting arbitrary rules now would mean supporting expansion we haven't specified.)

Expansion library: `teambition/rrule-go`. Its release activity is low (noted in D13) — the supported subset is small enough to hand-roll if it becomes a problem, so keep expansion behind a small internal interface rather than scattering library calls through handlers.

### Expansion (FR-E2/E5)

Always **window-bounded and capped**: expand only within the requested `from`–`to`, cap at `HOME_RRULE_MAX_OCCURRENCES` (default 500) per event per request, and reject a window wider than the permitted span (`422`). An open-ended weekly event must never be able to produce unbounded work or an unbounded response.

Expansion resolves against `HOME_TIMEZONE`. Because events are all-day there's no clock-time DST hazard — but "today" and month boundaries still must be computed in `Europe/Prague`, not UTC, or occurrences flip a day around midnight.

### Short-month clamping — D19, the deliberate deviation

**RFC 5545 semantics would skip.** `FREQ=MONTHLY` anchored on the 31st produces nothing in February; `FREQ=YEARLY` on 29 February produces nothing in non-leap years. For a household ("zaplatit 31."), a silently skipped month is a bug.

**So: when the anchor day exceeds the target month's length, clamp to the last day of that month.** Implement it as an **explicit post-expansion adjustment** — a documented, tested step, not an accident of the library's behaviour. Leave a comment saying it intentionally departs from RFC 5545, or someone will "fix" it back later.

Effectively `day = min(anchorDay, daysInMonth(y, m))`. The design prototype implements exactly this and is a good reference.

## 3. Endpoints (see `openapi.yaml`)

- `GET/POST /api/events`, `GET/PATCH/DELETE /api/events/{id}` — series-level CRUD
- `GET /api/events/occurrences?from=&to=` — expanded, month-grouped
- `GET/POST /api/events/{id}/links`, `DELETE /api/event-links/{id}`
- `POST /api/events/{id}/complete` `{occurrence_on}` · `DELETE /api/events/{id}/complete?occurrence_on=` (undo)

**Routing:** register the static `/api/events/occurrences` route **before** the parameterised `/api/events/{id}`, or `occurrences` gets parsed as an event id. Chi does this correctly by default; add a test anyway.

Reads: any authenticated user. Writes: `editor`/`admin`.

### Behaviours

- **Edits and deletes hit the whole series** (D14). There are no per-occurrence exceptions and no `EXDATE` in v1. Delete is soft by default (`archived=true`), hard behind `?hard=true`, and cascades links + completions.
- **Complete is idempotent** (FR-E6): a second call for the same `(event_id, occurrence_on)` is a no-op returning `200`, not a `409`. Rely on the unique constraint. Reject an `occurrence_on` that isn't a real occurrence of that series with `422` — otherwise you can complete dates that don't exist.
- **Completing one occurrence does not create a row for the next one.** The next occurrence simply becomes the earliest uncompleted one; module 4 picks it up. No advancing logic to write.
- **Occurrence listing** (FR-E5) groups by month ascending, defaults to current month forward, and pages backward too — events are retained, never auto-deleted. Each occurrence carries its parent's fields plus `occurrence_on`, `recurring`, reminder config, and whether *that* occurrence is completed.
- **Reminder config is declarative** (FR-E4): enabling a reminder schedules nothing. Changing the lead time changes which reminders appear immediately, with no backfill or catch-up.

## 4. Websocket

Publish event and completion changes to the hub (foundation F5) — module 4's dashboard depends on them to stay live.

## 5. Frontend — Okno do budoucnosti

Visual reference: the Okno screen and event form in `../design/Home.dc.html`.

- **Month-grouped list**, current month forward, pageable including past months. Each row: date (`d. M. yyyy`), title, recurrence indicator, reminder indicator with lead time. Months with nothing in them still read cleanly.
- **Event form** — the most intricate form in the app, and it must work one-handed. Full-screen sheet on mobile, dialog on desktop:
  - title, markdown description, links (reuse the card-link editor from module 2)
  - **date picker — all-day, no time field anywhere**
  - **recurrence selector**: *nikdy · týdně · měsíčně · ročně*, plus an optional end date that appears only when recurring
  - **reminder checkbox → conditional lead-time selector**: *1 den · 2 dny · 1 týden · 2 týdny · 1 měsíc*. **Reserve the space** so revealing the selector doesn't jump the layout.
- **Series-edit warning (D14)** — when editing a recurring event, state plainly *before saving* that the change affects every occurrence and that a single occurrence can't be edited or skipped. Pre-save, not a toast afterwards. The prototype's wording is approved: *"Změny se uloží pro celou sérii — všechny výskyty. Jednotlivý výskyt nelze upravit ani přeskočit."*
- Query keys: `['events', {window}]`, `['event', id]`. Completing invalidates `['dashboard']` too.
- Loading / empty / error / `reader` (no create/edit affordances) states.

## 6. Tests

**The clamping matrix is the priority — it's the one place we knowingly leave the RFC:**

- Monthly anchored **31 Jan** → 28 Feb in a common year, **29 Feb in a leap year**, 31 Mar, 30 Apr. Not a skipped February.
- Yearly anchored **29 Feb** → 28 Feb in non-leap years, 29 Feb in leap years.
- Monthly anchored on the 30th → 28/29 Feb, 30 Apr.

Also:

- Weekly/monthly/yearly expansion produces the right dates within a window, and **no occurrence rows are ever persisted** (assert the schema has no occurrences table and that expansion writes nothing).
- An open-ended weekly series over a wide window stops at `HOME_RRULE_MAX_OCCURRENCES`; an over-wide window returns `422`.
- `UNTIL` terminates the series; a one-off (`rrule = NULL`) yields exactly one occurrence, and only if it falls in the window.
- Date boundaries computed in `Europe/Prague`: an occurrence on "today" is not off by one when the server clock is UTC.
- Complete is idempotent (second call → `200`, one row); undo removes it; a bogus `occurrence_on` → `422`.
- Series edit changes all occurrences; delete cascades links + completions.
- Static `/api/events/occurrences` resolves before `/api/events/{id}`.
- `reminder_enabled` without `reminder_lead` is rejected by the CHECK.
- Audit: event CRUD produces field diffs (`event` is a key entity); `reminder.complete` logs an event with no diffs.
- Role gating: `reader` gets `403` on create/edit/delete/complete.

## 7. Definition of done

- [ ] No occurrences table exists; expansion is read-only and window-bounded with a cap.
- [ ] RRULE subset whitelisted on input; anything outside it is `422`.
- [ ] **Short-month clamping implemented as an explicit, commented, tested step** — full matrix above passes.
- [ ] No scheduler, cron, or queue anywhere in the module.
- [ ] Completion idempotent via the unique constraint; undo works; bogus occurrence rejected.
- [ ] Series-only editing with the pre-save warning in the UI.
- [ ] Event form: all-day date, four recurrence options + optional end date, conditional reminder lead with no layout jump; works one-handed at 375 px.
- [ ] Month-grouped list, current month forward, pageable to past months.
- [ ] All date math in `HOME_TIMEZONE`.
- [ ] Every mutation audited in-transaction; `reader` blocked.
