# Home — Module 2: `todo` (Úkoly board)

> **Read first:** `HANDOFF.md` (foundation + shared conventions), then PRD §4 FR-T1–T7, §5 to-do tables, §6 to-do endpoints, §7 Úkoly screen.
> **Depends on:** foundation (F1–F6) and the audit spine (`HANDOFF-1-logging.md`). **Blocks:** `HANDOFF-4-dashboard.md`.
> **Scope:** the Trello-style board — the daily driver.

## The model in one paragraph

A **board** holds **many columns**. Columns are sortable (priority + manual order) and collapsible. A column's `kind` (`normal|now|done`) is a **non-unique hint** — a board may have several `now` ("Právě dělám") and several `done` ("Hotovo") columns. A **card** lives in exactly one column and carries a title, markdown notes, structured links, one checklist, and labels. The workflow: cards wait in their columns, get moved into a `now` column for the period, then moved to a `done` column.

**Every mutation below writes an audit event in the same transaction** (see `HANDOFF-1`). Not repeated per requirement.

## 1. Data model (PRD §5)

**boards** — `id` · `name` · `description` NULL · `position` · `created_by` · `created_at` · `archived`.

**columns** — `id` · `board_id` FK CASCADE · `name` · `priority` INT DEFAULT 0 · `position` · `kind` CHECK(`normal`,`now`,`done`) DEFAULT `normal` · `created_at`. Indexes `(board_id, position)`, `(board_id, priority)`, and **`(kind)`** — module 4 queries `kind='now'` across boards, so don't omit it.

**cards** — `id` · `column_id` FK CASCADE · `title` · `notes` NULL (markdown) · `position` · `created_by` · `created_at` · `updated_at` · `done_at` NULL · `archived`. Indexes `(column_id, position)`, `(updated_at)`.

**card_links** — `id` · `card_id` FK CASCADE · `url` · `title` NULL · `position`.
**checklist_items** — `id` · `card_id` FK CASCADE · `text` · `done` · `position`.
**labels** — `id` · `board_id` FK CASCADE · `name` · `color` · unique `(board_id, name)`.
**card_labels** — `(card_id, label_id)` PK, both FK CASCADE, index `(label_id)`.

**No `user_column_state` table** — collapse is client-side localStorage (D3). If you're tempted to add one, re-read D3.

**Seed (FR-T1):** exactly one board **"Domácnost"** with **Zásobník** (`normal`), **Právě dělám** (`now`), **Hotovo** (`done`) — **only when `boards` is empty**. The design prototype ships three demo boards; that's demo data, *not* the seed. Ship one.

## 2. Ordering — get this right once

`position` is a **lexorank-style string** (D4). Inserting between two neighbours computes a key strictly between them and writes **one row**. Moving a card to the head or tail computes against a single neighbour.

Two failure modes to avoid:
- **Renumbering siblings on every move.** If a move writes more than one row, it's wrong — that's the integer-position anti-pattern lexorank exists to avoid.
- **Key exhaustion.** Repeatedly inserting between the same two items grows the key. Handle the degenerate case (either lengthen the key, or renormalise that column's keys as a rare explicit operation) and cover it with a test that inserts 200 times at the same spot.

## 3. Endpoints (see `openapi.yaml`)

- Boards: `GET/POST /api/boards`, `GET/PATCH/DELETE /api/boards/{id}`, `GET /api/boards/{id}/tree`
- Columns: `GET/POST /api/boards/{id}/columns`, `PATCH/DELETE /api/columns/{id}`, `POST /api/columns/{id}/move`
- Cards: `POST /api/columns/{id}/cards`, `GET/PATCH/DELETE /api/cards/{id}`, `POST /api/cards/{id}/move`
- Links: `GET/POST /api/cards/{id}/links`, `DELETE /api/links/{id}`
- Checklist: `GET/POST /api/cards/{id}/checklist`, `PATCH/DELETE /api/checklist/{id}`
- Labels: `GET/POST /api/boards/{id}/labels`, `PATCH/DELETE /api/labels/{id}`, `POST/DELETE /api/cards/{id}/labels/{labelId}`

**There is no collapse endpoint** — collapse never reaches the server.

Reads: any authenticated user. Writes: `editor`/`admin`. (Foundation F4 middleware.)

### Behaviours worth calling out

- **`POST /api/cards/{id}/move`** is the core workflow action: target column and/or position in **one** operation. Moving into a `kind=done` column stamps `done_at`; moving **out** clears it. Module 4 calls this same endpoint — don't duplicate the logic there.
- **Column delete** with cards blocks with `409` + the card count unless `?cascade=true`, which deletes the cards (each one logged).
- **Card delete** is soft (`archived=true`) by default, hard behind `?hard=true` (D8).
- **Label delete** detaches from all cards (logged).
- **Board tree** (`/tree`) is the read model: columns in sort order, each with cards in order; each card carries label ids and checklist **progress** (`done/total`) but **not** the full checklist or links — those load with the card detail. Filters: `label` (multi), `q` (title + notes), `include_archived`. Empty columns still render so cards can be dropped into them.

## 4. Websocket

Publish card/column/board changes to the hub (foundation F5) so other devices update live. Also invalidate `['dashboard']` on the client when a card moves into or out of a `now` or `done` column — module 4's list depends on it.

## 5. Frontend — Úkoly

Visual reference: the Úkoly screen in `../design/Home.dc.html` (+ `CardTile.dc.html`).

- **Board switcher** — multiple boards from v1 (D1).
- **Desktop:** horizontal kanban. Columns collapsible to a thin labelled spine (many columns is the expected case), sortable by drag or by priority. dnd-kit for cards and columns. `now` columns visually emphasised.
- **Mobile:** a **vertical accordion** of collapsible columns, `now` columns **pinned to the top** — not a horizontal kanban. Touch drag is the secondary path; the primary is an explicit **"Přesunout do…"** control: one tap to open, one tap to pick the destination.
- **Collapse state in localStorage**, per device (D3).
- **Card detail** — full-screen sheet on mobile, dialog on desktop: title, markdown notes (view/edit toggle), links (add/open/remove), checklist with progress, labels. This exact component is reused by module 4, so build it standalone and importable.
- **Filtering** — label chips + text search + show/hide archived.
- **Optimistic** move/reorder/check with rollback + toast on failure.
- Query keys: `['boards']`, `['board', id, 'tree', {filters}]`, `['card', id]`, `['board', id, 'labels']`.

**Accessibility:** dnd-kit drag needs a keyboard alternative — a keyboard user must be able to move a card (the "Přesunout do…" control satisfies this if it's reachable and operable). Touch targets ≥44 px.

## 6. Tests

- **Move is one write.** Moving a card between positions rewrites exactly one row's `position`; siblings are untouched.
- Lexorank degenerate case: 200 inserts at the same position still yield strictly ordered, distinct keys.
- `done_at` stamps on entering a `kind=done` column and clears on leaving.
- **Multiple `now` columns and multiple `done` columns on one board both work** (D7) — no code path assumes exactly one. This is the assumption most likely to be coded wrong.
- Column delete: blocked `409` with card count; `?cascade=true` deletes and logs each card.
- Card delete soft by default; `?hard=true` really removes.
- Board tree returns ordered columns/cards with label ids + checklist progress, and *not* full checklists/links; label and text filters narrow correctly; empty columns still present.
- Audit: every mutation above produces an event; card edits produce field diffs.
- Role gating: `reader` gets `403` on every mutation, `200` on every read.

## 7. Definition of done

- [ ] One board seeded ("Domácnost" / Zásobník · Právě dělám · Hotovo), only when empty.
- [ ] Lexorank ordering: moves rewrite one row; degenerate-insert test passes.
- [ ] Multiple `now`/`done` columns per board fully supported.
- [ ] `POST /api/cards/{id}/move` handles column + position in one call, with `done_at` stamping — and is the single implementation module 4 reuses.
- [ ] Board tree matches the spec payload shape; filters work.
- [ ] Desktop kanban with drag reorder; mobile accordion with `now` pinned and one-tap "Přesunout do…"; collapse persisted in localStorage.
- [ ] Card detail is a standalone reusable component.
- [ ] Every mutation audited in-transaction; `reader` blocked from all of them.
- [ ] Verified at 375 px and 1440 px, both themes.
