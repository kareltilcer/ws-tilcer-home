# Home — Module 4: `dashboard` (Nástěnka — widget host) — v2

> **Read first:** `HANDOFF.md` (foundation, module registry, Mode B auth), then PRD §4 FR-M2 + FR-D1–D5, §5 `user_dashboard_layout`, §6 dashboard paths, §7 Nástěnka, and decisions **D24, D27, D28, D22, D15, D16-adjusted**.
> **Depends on:** foundation + spine + **both feature modules** (they provide the widgets). Build last.
> **Scope:** the landing page as a **per-user widget host**. Owns no feature data.

## The shape of this module in one paragraph

Nástěnka renders **widgets** that other modules contribute, in an arrangement each user controls. This module owns exactly two things: the **widget catalog** (assembled by the core from every module's `Widgets()`) and each user's **layout** (`user_dashboard_layout`). It reaches feature data **only** through the `WidgetProvider.Data(...)` contract (§10 D28) — it must never import `todo` or `events` packages or query their tables. If you find a `SELECT ... FROM cards` in this module, it's wrong.

## 1. Data model

**user_dashboard_layout** — `user_id` TEXT · `widget_key` TEXT · `visible` BOOL DEFAULT true · `position` TEXT (lexorank) · `size` TEXT CHECK in (`narrow`,`wide`) DEFAULT `narrow` · PK `(user_id, widget_key)` · index `(user_id)`.

That's the whole module's schema. No feature tables.

## 2. The widget catalog (FR-D1)

The core builds the catalog from every registered `WidgetProvider` (see `HANDOFF.md` §3). In v1 the providers are (D27):

| key | title (cs) | module | default size | admin only |
|---|---|---|---|---|
| `todo.pravedelam` | Právě dělám | todo | wide | no |
| `events.pripominky` | Připomínky | events | narrow | no |
| `events.tento-mesic` | Tento měsíc | events | narrow | no |

`GET /api/dashboard/catalog` returns the entries **this user may add** — all non-admin widgets, plus admin-only ones (none in v1) when the user is `admin`. This is the "add a widget" menu.

## 3. Per-user layout (FR-D2)

- `PUT /api/dashboard/layout` takes an ordered array of `{widget_key, visible, size}`. Order in the array is the display order; derive `position` lexorank keys server-side. Upsert the caller's rows.
- **First-run default:** a user with no rows gets all non-admin widgets visible at their `default_size`, in catalog order. Return this default from `GET /api/dashboard` so the client always has something to render.
- **Ignore unknown/unavailable keys** — a widget's module could be absent, or an admin-only widget could appear for a non-admin; filter those out rather than erroring.
- **Layout is a personal view preference, so a `reader` may set it** (D24). This is the single write a `reader` is allowed — enforce that specifically: `PUT /api/dashboard/layout` is allowed for any authenticated user; every *other* mutation in the app still requires `editor`/`admin`.
- Requires the CSRF header like any cookie-authenticated mutation.

## 4. Dashboard fan-out (FR-D3)

`GET /api/dashboard`:

1. Load the caller's layout (or the default).
2. For each **visible** widget, call its provider's `Data(ctx, user)`.
3. Return `{ layout, widgets: [{ key, size, data }] }` in layout order.

Requirements:

- **Respect the boundary:** call `WidgetProvider.Data`, never module tables (D28).
- **Bounded, no N+1:** this is the landing route and the most-loaded endpoint. Each provider must itself be bounded (the events providers cap expansion; the todo provider is one indexed query across boards). Fan out concurrently where safe; don't loop per-event with per-event queries.
- **User-scoped:** `Data` receives the user; an admin-only widget's data is never computed for a non-admin (it won't be in their layout, but guard anyway).
- `GET /api/dashboard/widgets/{key}` returns a single `WidgetInstance` — same payload as one entry — for targeted refresh when the websocket signals that widget's data changed.

## 5. The widget providers live in their modules (do not build them here)

For reference — these are specified in the module handoffs, not this one:

- **`todo.pravedelam`** (`HANDOFF-2` FR-T8): cards in `kind=now` columns **across all non-archived boards**, with board/column for grouping, labels, checklist progress. This is the v1 cross-board aggregation, now a provider.
- **`events.pripominky`** (`HANDOFF-3` FR-E7): active reminders — earliest uncompleted occurrence per event within the lookback, `today >= occurrence − lead`, overdue first, one per event.
- **`events.tento-mesic`** (`HANDOFF-3` FR-E8): read-only look-ahead through end of month / next N days.

The host just renders whatever `data` they return, keyed by widget type.

## 6. Frontend — the widget host

Visual reference: **pending the v2 design addendum** (`HANDOFF-design.md` §v2) — the v1 prototype's Nástěnka was a fixed two-list page, not a widget host. Build backend now; reconcile the host UI with the addendum. Meanwhile, from the PRD + tokens:

- **Frontend widget registry** (in `platform/`): each module registers its widget **component** by key; the host looks up the component for each `{key, size, data}` the API returns. The host hardcodes no widget.
- **Responsive grid (D24):** one reorderable column on mobile; a 2-column grid on desktop where a widget is **narrow (1 col)** or **wide (2 col)**.
- **Arrange mode (FR-D4):** add from the catalog, hide/remove, **drag to reorder** (dnd-kit), **resize** narrow↔wide. Each change → `PUT /api/dashboard/layout`, optimistic with rollback. A keyboard path for reorder/resize (dnd-kit keyboard sensor), since drag isn't operable for everyone.
- **Empty dashboard is a deliberate state** — everything hidden shows a calm "přidat widget" affordance, not a broken page.
- **Inside widgets (FR-D5):** the Právě dělám and Připomínky widgets carry the **2000 ms press-and-hold done gesture** (D22) with its **mandatory immediate keyboard path**, and open the owning module's detail dialog (card detail / event detail) on row tap. The host adds no completion logic — it renders the module's widget component, which calls the module's endpoints (`POST /api/cards/{id}/move`, `POST /api/events/{id}/complete`) with `meta.via="dashboard"`.
- Query keys: `['dashboard']`, `['dashboard','catalog']`, `['dashboard','widget',key]`. A move/complete/layout change invalidates `['dashboard']`; a `/ws` push refreshes the affected widget via `['dashboard','widget',key]`.

## 7. Tests

- **Boundary:** an architecture/import test asserts the `dashboard` module imports neither `todo` nor `events`; grep/AST for `modules/todo`/`modules/events` imports fails CI.
- **Fan-out:** `GET /api/dashboard` returns one entry per visible widget in layout order; hidden widgets are absent; data comes via providers.
- **Default layout:** a fresh user (no rows) gets all non-admin widgets visible; the response renders without a prior `PUT`.
- **Layout persistence:** `PUT` then `GET` round-trips visibility, order, and size; order is preserved via lexorank; unknown keys are ignored.
- **Reader may arrange:** a `reader` can `PUT /api/dashboard/layout` (`200`) but still gets `403` on every other mutation (card move, event complete, etc.).
- **CSRF:** `PUT` without the CSRF header/Origin is rejected.
- **No N+1:** a dashboard load with many reminder-enabled events issues a bounded query count (assert count).
- **Widget refresh:** `GET /api/dashboard/widgets/{key}` returns the same payload as that widget's entry; unknown key → `404`; admin-only key for a non-admin → `403`.
- **Gesture (in-widget):** 2000 ms hold completes; short tap doesn't and doesn't open the row; keyboard activation completes immediately.
- **Cross-module attribution:** completing from a widget logs under `todo`/`events` with `meta.via="dashboard"` and appears in the entity timeline.

## 8. Definition of done

- [ ] `dashboard` module owns only `user_dashboard_layout`; imports no feature module (import test passes).
- [ ] Catalog assembled from providers; user-scoped (admin-only filtered).
- [ ] Per-user layout persists show/hide + order + narrow/wide, syncs across devices; first-run default; unknown keys ignored; `reader` may set layout but nothing else.
- [ ] `GET /api/dashboard` fans out to visible providers, bounded, no N+1; single-widget refresh endpoint works.
- [ ] Widget components rendered via the frontend registry; host hardcodes none.
- [ ] Arrange mode: add/hide/reorder/resize, optimistic + rollback, keyboard-operable; empty state is deliberate.
- [ ] In-widget 2000 ms hold + immediate keyboard path; mark-done reuses module endpoints with `meta.via="dashboard"`.
- [ ] Nástěnka is the landing route; verified 375 px + 1440 px, both themes (against the v2 design addendum once available).
