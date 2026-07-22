# Home — Module 4: `dashboard` (Nástěnka)

> **Read first:** `HANDOFF.md` (foundation + shared conventions), then PRD §4 FR-N1–N3, §6 `/api/dashboard`, §7 Nástěnka screen, and decisions D15, D16, D22.
> **Depends on:** foundation, the audit spine, **and both feature modules** (`HANDOFF-2-todo.md`, `HANDOFF-3-events.md`) — this module aggregates them. Build it last.
> **Scope:** the landing page. One screen answering *"what needs me right now?"*

## What this module is (and isn't)

Nástěnka owns **no data**. It is a read model over the other two modules plus one gesture. It has exactly one endpoint and no tables of its own.

Its mark-done actions **reuse the owning module's endpoints** — `POST /api/cards/{id}/move` and `POST /api/events/{id}/complete`. Do not write a second completion path here: one implementation per action, called from two screens. Cross-module calls log under the **owning** module with `meta.via="dashboard"` so an entity's timeline stays complete regardless of which screen touched it.

## 1. The read model — `GET /api/dashboard` (FR-N1)

Two computed lists, both **active items only** (D16 — no look-ahead section, no "coming up").

### Události (event reminders)

For each non-archived event with `reminder_enabled`:

1. Expand its occurrences (module 3's expander) and take the **earliest uncompleted occurrence** whose date is `>= today − HOME_DASHBOARD_LOOKBACK_DAYS` (default 30).
2. That occurrence is **active** when `today >= occurrence_on − reminder_lead`.
3. Flag it **`overdue`** when `occurrence_on < today`.

**At most one reminder per event.** This is what stops a recurring event stacking up a year of missed occurrences, and it's why no "advance the reminder" logic is needed — completing the current one simply makes the next the earliest uncompleted. The lookback bound is what stops an ignored event accumulating forever.

Sort: overdue first, then `occurrence_on` ascending.

### Úkoly (to-dos)

Every non-archived card in **any column with `kind='now'`, across all non-archived boards** — not just the active board. Each item carries its board and column so the UI can group.

> **This is one of the three design gaps (see `HANDOFF.md` §6).** The prototype aggregates the active board only and hardcodes the board name. The correct behaviour is all boards. Build from this spec, not from the prototype.

Sort: board order, then column priority, then card position.

### Performance

This is the **landing route** — the most-loaded endpoint in the service. Requirements:

- **One round of queries, no N+1.** Don't expand occurrences event-by-event in a loop of queries; load candidate events in one query (the partial index `(reminder_enabled) WHERE archived=0`), then expand in memory.
- The task list uses the `columns(kind)` index from module 2 — one join across boards, not a query per board.
- Every item must carry enough to render its row without a second request (title, dates, board/column, label ids, checklist progress).

## 2. Mark done (FR-N3)

- **To-do:** `POST /api/cards/{id}/move` to that card's board's **first `kind=done` column** by column order, which stamps `done_at`. If the board has **no** `done` column, fall back to `archived=true` (D15). Logged as `todo.card.move`, `meta.via="dashboard"`.
- **Reminder:** `POST /api/events/{id}/complete` with the occurrence date. Logged as `events.reminder.complete`, `meta.via="dashboard"`.
- Both optimistic with rollback, and both broadcast over the websocket so other devices' dashboards update.
- `reader` gets `403` on both — and shows no done control at all.

## 3. The done gesture — D22, press-and-hold

**This deliberately supersedes the earlier one-tap requirement.** Nástěnka is the landing route with dense rows, so a stray tap would silently complete something still outstanding.

- **Hold the row's done control for 2000 ms** to commit, with a fill animation over ~1.9 s so progress is visible.
- **Releasing early cancels** with no side effect.
- **A short tap does nothing** — and critically, must **not fall through** to opening the row. Stop the event; don't let it bubble to the row's tap handler.
- Tapping the row **body** still opens the detail dialog.

### The accessible path is mandatory, not optional

A sustained 2-second press isn't reliably operable with tremor, limited grip strength, or switch access — and this is the most frequent action in the app. So:

- **Keyboard/assistive activation (`Enter`/`Space`) on the done control commits immediately, with no hold.**
- The detail dialog's **"✓ Hotovo" button is a single activation** — opening the dialog was already the deliberate step.
- Expose the control to screen readers as a normal action (a button that completes the item), not as "hold to complete". Don't announce a gesture the assistive path doesn't require.
- Respect `prefers-reduced-motion` for the fill animation — the hold still works, the animation just doesn't animate.

## 4. Frontend — Nástěnka

Visual reference: the Nástěnka screen in `../design/Home.dc.html`.

- **The landing route** (D16) — this is what opens on launch.
- Two clearly separated, labelled lists: **Události** and **Úkoly**, with Czech plural count labels (*5 připomínek*, *2 úkoly* — all three forms).
- **Group the task list by board when more than one board contributes**; show no grouping heading when only one does. *(Design gap — no prototype reference; follow the established design language.)*
- **Overdue reminders visually distinct and sorted first**, using a restrained accent rather than a wall of red — a household that's a week behind shouldn't open to an alarm. The prototype's approach (a low-percentage danger tint on background and border) is approved.
- Rows are tappable → detail dialog: **reuse module 2's card detail** for tasks and module 3's event detail for reminders. Don't fork new components.
- **Empty state is a success state** — "nothing needs you right now" should look deliberate and calm, not like a failed load.
- Loading / error / `reader` (no done controls) states.
- Query key `['dashboard']`. Any card move or reminder completion — from anywhere in the app — invalidates it.

## 5. Tests

- **One reminder per event:** an event with three missed occurrences in the lookback window yields exactly one dashboard row (the earliest uncompleted).
- **Activation boundary:** with a 1-week lead, the reminder is absent on day −8 and present on day −7, evaluated in `Europe/Prague` (not off by one against a UTC server clock).
- **Advance on completion:** completing the current occurrence makes the *next* occurrence the live one, with no new completion row created for it.
- **Lookback bound:** an uncompleted occurrence older than `HOME_DASHBOARD_LOOKBACK_DAYS` drops off rather than accumulating.
- **Overdue flag and ordering:** past-dated uncompleted items are flagged and sort above future ones.
- **Cross-board aggregation:** cards in `kind='now'` columns on *two different boards* both appear, each with its own board name; grouping appears only when >1 board contributes. (Explicitly covers the design gap.)
- **Multiple `now` columns on one board** all contribute (D7).
- **Mark done → first `done` column** by order; with **no** `done` column on that board, the card is archived instead (D15).
- **Cross-module attribution:** completing from Nástěnka logs under `todo` / `events` with `meta.via="dashboard"`, and the event appears in that entity's timeline in the log browser.
- **Idempotency:** double-completing a reminder from the dashboard produces one completion row.
- **Gesture:** a 500 ms press does not complete; a 2000 ms press does; early release leaves no change; a short tap neither completes nor opens the row.
- **Accessible path:** keyboard activation completes immediately without a hold; the dialog's "✓ Hotovo" is a single activation.
- **No N+1:** a dashboard load with 50 reminder-enabled events issues a bounded number of queries (assert query count, not just latency).
- Role gating: `reader` gets `403` on both completion paths and sees no done control.

## 6. Definition of done

- [ ] `GET /api/dashboard` returns both lists per FR-N1, with no tables of its own.
- [ ] **Tasks aggregate across all boards**, with board grouping when >1 contributes.
- [ ] At most one reminder per event; lookback bound enforced; overdue flagged and sorted first.
- [ ] Mark-done reuses the owning modules' endpoints — **no duplicate completion logic** — with `meta.via="dashboard"`.
- [ ] Falls back to archive when a board has no `done` column.
- [ ] **2000 ms hold** commits; early release cancels; short tap does nothing and doesn't open the row.
- [ ] **Keyboard/assistive activation commits immediately**; dialog "✓ Hotovo" is single activation; `prefers-reduced-motion` respected.
- [ ] Nástěnka is the landing route; empty state reads as success.
- [ ] Detail dialogs reuse modules 2 and 3's components.
- [ ] Dashboard load is a bounded query count with no N+1 over events.
- [ ] Verified at 375 px and 1440 px, both themes; `reader` sees a coherent read-only page.
