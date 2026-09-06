# Home — Module 5: `notes` (Poznámky)

> **Read first:** `HANDOFF.md` (foundation, module registry, Mode B auth, conventions), then PRD §1 (Architecture — Poznámky), §4 **FR-P1–P8**, §5 notes tables, §6 notes endpoints, §7 Poznámky, and decisions **D30–D38** (D6 extended).
> **Depends on:** foundation (F1–F6) and the audit spine (`HANDOFF-1-logging.md`). **Blocks:** nothing — the dashboard host picks up the `notes.pripnute` widget automatically through the registry (`HANDOFF-4`), no host change needed.
> **Scope:** Markdown notes in a folder tree, slug-path URLs (household-only), two-scope pinning, and the `notes.pripnute` Nástěnka widget (§10 below).
> **v3:** a self-contained module per `HANDOFF.md` §3 (own routes/migrations/audit actions/widget, registered via the core; **no cross-module imports**). Auth is Mode B — authorization is from the home **session**; writes = `editor`/`admin`, with two documented exceptions (§5). This is the first module added after v2 shipped — adding it must be *adding a package that registers itself*, not editing the host or the other modules.

## The model in one paragraph

A **folder** holds subfolders and notes; folders nest arbitrarily and each has exactly one parent (or none = root). A **note** lives at the root or in exactly one folder and carries a title and a **single canonical Markdown body** (`body_md`) — the WYSIWYG and raw-Markdown editors are two views over that one string (D30). Every folder and note is addressable by a **human-readable slug path** (`/poznamky/<folder>/…/<slug>`); slugs are unique across the sibling folders **and** notes under one parent, so a path resolves to exactly one item (D32). Canonical operations are by **stable id**; a resolver turns a path into an id. A note can be **pinned** "pro všechny" (household — shared, audited) or "jen pro mě" (personal — a per-user preference, not audited), and pinned notes surface in the `notes.pripnute` widget, whose rows open in an overlay on Nástěnka.

**Every mutation below writes an audit event in the same transaction** (see `HANDOFF-1`) — **except personal pins** (§5/§6). Not repeated per requirement.

## 1. Data model (PRD §5)

All ids UUIDv7; `position` lexorank strings; soft delete default (`?hard=true`); timestamps UTC.

**folders** — `id` · `parent_id` NULL (self-ref FK→`folders.id`; NULL = root) · `name` · `slug` · `position` · `archived` BOOL DEFAULT false · `created_by` · `created_at` · `updated_at`. Index `(parent_id, position)`.

**notes** — `id` · `folder_id` NULL (FK→`folders.id`; NULL = root/unfiled) · `title` · `slug` · `body_md` NULL (**the one canonical Markdown body**) · `position` · `archived` BOOL DEFAULT false · `created_by` · `created_at` · `updated_at`. Indexes `(folder_id, position)`, `(updated_at)`.

**note_pins** — `note_id` FK→`notes.id` CASCADE · `scope` CHECK(`household`,`personal`) · `user_id` NULL (NULL for household; the auth user id for personal) · `pinned_by` (actor) · `position` (lexorank, per scope/user) · `created_at`.

**notes_fts** — FTS5 virtual table over note `title` + `body_md`, kept in sync by triggers (mirror `audit_events_fts` in `HANDOFF-1`). Use the `unicode61` tokenizer with `remove_diacritics=2` so a search for `gulas` finds *Guláš* (see the diacritics test).

### The uniqueness indexes — get these exactly right

**Sibling slug uniqueness (per table).** SQLite treats `NULL` as distinct, so a plain `UNIQUE(parent_id, slug)` will **not** dedupe root-level siblings. Key on a coalesced parent, over non-archived rows only:

```sql
CREATE UNIQUE INDEX ux_folders_sibling_slug ON folders (COALESCE(parent_id,''), slug) WHERE archived = 0;
CREATE UNIQUE INDEX ux_notes_sibling_slug   ON notes   (COALESCE(folder_id,''), slug) WHERE archived = 0;
```

**Cross-table sibling uniqueness (the addressing invariant, D32).** A folder and a note under the **same parent** must not share a slug, or a path segment is ambiguous. No single index spans two tables, so enforce it **in the write transaction**: before inserting/renaming/moving, check *both* tables for a live row with the same parent scope (`parent_id`/`folder_id` = P) and slug. This check + the two indexes together are the invariant — cover it with a test.

**Pin scopes (partial unique).**

```sql
CREATE UNIQUE INDEX ux_note_pins_household ON note_pins (note_id)           WHERE scope = 'household';
CREATE UNIQUE INDEX ux_note_pins_personal ON note_pins (note_id, user_id)  WHERE scope = 'personal';
```

One household pin per note; one personal pin per note per user; the two are independent (a note can be both).

### Migrations & registration

This module ships its own Goose files. Insert `notes` into the core's one sequence **after `events`, before `dashboard`**: `logging → platform → todo → events → notes → dashboard`. **Nothing is seeded** (no default folder/note). Must apply cleanly on an empty DB and after a Litestream restore. Register via `registry.Module`: routes, migrations, `AuditActions()` (§6), and one `Widgets()` entry (§10).

## 2. Slugs & URLs — get this right once

- **Derivation.** Slug = fold Czech diacritics to ASCII (ě→e, š→s, č→c, ř→r, ž→z, ý→y, á→a, í→i, é→e, ú/ů→u, ď→d, ť→t, ň→n, ó→o), lowercase, spaces→`-`, drop other punctuation, collapse repeated `-`. Empty result (e.g. title was only punctuation) → fall back to the short id.
- **Collision.** If the derived slug already exists in the parent scope (either table), append `-2`, `-3`, … until free. This runs inside the same transaction as the cross-table check.
- **The full path is computed, not stored.** A note/folder stores only its **own** slug; the path is built by walking `parent_id` to the root. **Therefore moving a folder does NOT rewrite its descendants** — only the moved item may need a fresh slug to stay unique in its new parent. Do not walk the subtree rewriting rows; that's the mistake to avoid here.
- **Resolver (`GET /api/notes/resolve?path=`).** Split the path on `/`; walk from root, matching each segment's slug among that parent's child **folders** for intermediate segments, and among child folders **or** notes for the final segment. Return `{type, id, slug_path}`; any unmatched segment → `404`. **No redirects** — a renamed/moved item's old path just 404s (D32); do not build a slug-history/redirect table.
- **Addressing.** The SPA's pretty route is the slug path; it calls `resolve` once on navigation, then works by **stable id** for detail/mutations. Every notes route (incl. `resolve`) requires a valid session — sharing is household-only, there is no public path (D33).

## 3. Ordering

`position` is lexorank (D4), exactly as in `todo` — inserting between neighbours writes **one row**; a move rewrites one row, never renumbers siblings; handle the degenerate "200 inserts at the same spot" case. Notes and folders order **independently** within a parent (they're separate tables); the browser interleaves them for display (folders-then-notes, or by position — a UI choice, see the design addendum). Pin `position` is per scope/user.

## 4. Endpoints (see `openapi.yaml` 0.4.0)

- **Notes:** `GET /api/notes` (list; `?q=` → FTS5 search, `?folder_id=`, `?include_archived=`), `POST /api/notes`, `GET/PATCH/DELETE /api/notes/{id}`, `POST /api/notes/{id}/move`, `POST /api/notes/{id}/pin` + `DELETE /api/notes/{id}/pin?scope=`.
- **Folders:** `POST /api/notes/folders`, `GET/PATCH/DELETE /api/notes/folders/{id}`, `POST /api/notes/folders/{id}/move`.
- **Tree & resolve:** `GET /api/notes/tree`, `GET /api/notes/resolve?path=`.

**Routing order:** register the static `/api/notes/tree`, `/api/notes/resolve`, `/api/notes/folders*` (and the `?q=` branch of `GET /api/notes`) **before** the parameterised `/api/notes/{id}` so `tree`/`resolve` aren't captured as an `{id}`.

Reads: any authenticated member. Writes: `editor`/`admin` (F4 middleware) — **except** a **personal** pin/unpin, allowed for any authenticated member incl. `reader` (§5). Every mutation needs the CSRF header (F4).

### Behaviours worth calling out

- **`GET /api/notes/tree`** is the navigation read model: the folder tree with each folder's child folders + notes as lightweight `NoteSummary` nodes (id, title, slug, position, archived, `updated_at`, and the caller's `pinned` `{household, personal}` state) — **no bodies**. One bounded query set, **no N+1** over folders (load all folders + all note summaries, assemble the tree in memory). `?include_archived` off by default. Shape = `NotesTree`.
- **`GET /api/notes/{id}`** returns `NoteDetail`: the note incl. `body_md`, the breadcrumb `path[]`, `slug_path`, and the caller's `pinned` state.
- **`PATCH /api/notes/{id}`** — title/body/archived. Renaming re-derives the slug (URL changes). Body writes are **last-write-wins** (D38); no optimistic-concurrency token — the `/ws` "changed elsewhere" signal (§7) is the only guard, and it's advisory.
- **`POST /api/notes/{id}/move`** and **`POST /api/notes/folders/{id}/move`** — reparent (`folder_id`/`parent_id`, null = root) and/or reorder (`position`) in one call; re-derive the slug only if needed to stay unique in the new parent.
- **Folder move cycle guard.** Reject moving a folder into **itself or any descendant** → `422`. Implement by walking up from the target parent to the root; if you pass through the folder being moved, reject. Do this **before** any write.
- **Folder delete** — soft by default; a **non-empty** folder returns `409` with the child count unless `?cascade=true` (which soft-deletes the subtree, **each child logged**); `?hard=true` purges and is `admin`-gated (the app-wide destructive-op rule). Note delete is soft by default, hard behind `?hard=true`.

## 5. Pinning — two scopes (FR-P7, D35)

- **`POST /api/notes/{id}/pin { scope }`**, **`DELETE …/pin?scope=`**.
- **household ("pro všechny")** — a shared mutation: requires `editor`/`admin`, **audited** (`note.pin` / `note.unpin`, entity `note`, `meta.scope="household"`), one per note. Publish to `/ws` so everyone's widget refreshes.
- **personal ("jen pro mě")** — a per-user **view preference**: allowed for **any** authenticated member incl. `reader` (the notes analogue of "a reader may set their own dashboard layout"), **not audited**, one per note per user. No `/ws` broadcast — it only affects the caller; invalidate that user's widget query client-side.
- Idempotent: pinning a scope that's already pinned is a no-op **`200`** (not `409`), matching `openapi.yaml`; the partial indexes guarantee no duplicate row.
- Enforce the reader exception **narrowly**: `personal` pin/unpin is the *only* notes write a `reader` may make; every other notes mutation returns `403` for a reader.

## 6. Audit (spine, `HANDOFF-1`)

- **Actions** (dotted verbs, `AuditActions()` returns them for the log filter): `note.create`, `note.update`, `note.move`, `note.delete`, `note.pin`, `note.unpin`, `folder.create`, `folder.update`, `folder.move`, `folder.delete`. **Personal pins emit nothing.**
- **Entity types** `note` and `folder` **join D6's key-diff set** (PRD §10 D36). `note.update`/`folder.update` record field diffs — for notes: `title`, `body_md`, `slug`, `folder_id`, `position`, `archived`; for folders: `name`, `slug`, `parent_id`, `position`, `archived`. **`body_md` diffs are full, untruncated** (a note body can be pages long — the log browser already truncates-with-expand; storage keeps everything). Creates record `{field, null → new}`.
- The spine needs no code change — the notes module computes the `Changes` and passes them through `Sink.Record(ctx, tx, …)`. Actor/request-id come from context, never from arguments.
- Cross-module: edits/unpin invoked from the dashboard overlay log under **`notes`** with `meta.via="dashboard"` (so the note's entity timeline stays complete).

## 7. Websocket (F5)

Publish `note`/`folder` changes and **household** pin/unpin to the hub so open note views, the tree, and the `notes.pripnute` widget update live. A note-body change pushes so any other member with that note open gets the **"změněno jinde"** advisory (design addendum). **Personal pins are not broadcast** (caller-only). Frontend applies via `setQueryData`/invalidation with refetch-on-focus fallback.

## 8. Search (FR-P6)

`GET /api/notes?q=` runs `notes_fts MATCH` over title + body, returns `NoteSummary` items (with their folder path for display), capped + keyset-paged, ordered by relevance or `updated_at` desc. Reads only. Every query path (search and tree) must hit an index or the FTS table — no full scans (small today, not in two years).

## 9. Frontend — Poznámky

**Visual reference: pending the v3 design addendum** (`HANDOFF-design.md` §v3) — Poznámky is not in the v1/v2 prototype (same situation the widget host was in for v2). **Build the backend now**; reconcile the UI against the addendum. Meanwhile, from the PRD + tokens:

- **Route** `/poznamky/*` = slug paths; resolve path→id on navigation, then work by id.
- **Layout:** desktop **folder-tree sidebar + note pane**; mobile **drill-down** (folder → contents → note); breadcrumb everywhere. Driven by `['notes','tree']`.
- **Editor:** **WYSIWYG default** + a **raw-Markdown toggle**, both over the one `body_md` (round-trip). The rich editor library is chosen in the design addendum (must be Markdown-backed, mobile-usable, diacritic-safe). Build a **standalone `NoteView`/`NoteEditor` component** — the dashboard overlay (§10) reuses it verbatim.
- **Organise:** create/rename; move via the **"Přesunout do…"** picker (reuse Úkoly's pattern) + desktop drag (dnd-kit, with a keyboard alternative); non-empty folder delete shows the **cascade warning** before deleting.
- **Pin control:** two scopes ("Připnout pro všechny" / "Připnout jen pro mě") with state; `reader` sees only personal.
- **"Kopírovat odkaz":** copies the slug-path URL; UI communicates **household-only, not public** (D33) and that rename/move changes the link (D32).
- **Query keys:** `['notes','tree']`, `['notes','detail',id]`, `['notes','resolve',path]`, `['notes','search',q]`. A note/folder/move mutation invalidates `['notes','tree']`; a **household** pin also invalidates `['dashboard']` + `['dashboard','widget','notes.pripnute']`.
- **States:** loading, empty root, empty folder, no-results, error, and `reader` view-only (no edit affordances; personal-pin + read remain).
- **Accessibility:** keyboard-operable tree, move, and pin; touch targets ≥44 px; `prefers-reduced-motion` on tree/overlay transitions.

## 10. The `notes.pripnute` widget provider (FR-P8)

This module contributes one dashboard widget through the `WidgetProvider` contract (`HANDOFF.md` §3). The host calls it; it never reads this module's tables from outside.

- **Key** `notes.pripnute`, title **"Připnuté poznámky"**, default size **wide** (rows show title + excerpt — the design addendum may adjust), not admin-only.
- **`Data(ctx, user)`** returns **household pins ∪ the caller's personal pins**, **de-duplicated** (a note pinned both ways appears once, `scope="both"`, household precedence), household block first then personal, each ordered by pin `position`. Each row = `note_id`, `title`, `slug_path` (for the overlay/link), `scope` (`household|personal|both`), a short **plain-text excerpt** (strip Markdown, ~140 chars), `updated_at`, `position`. Shape = `PinnedNote` in `openapi.yaml`. One bounded query (join `note_pins`→`notes`, filter to household + this user's personal), no N+1.
- **Frontend widget component** (registered in the frontend widget registry by key): renders the rows; **a row tap opens the note in an overlay dialog on Nástěnka — it does NOT navigate to Poznámky** (the explicit requirement). The overlay reuses `NoteView` (read + WYSIWYG/Markdown edit toggle for editor+). Edit/unpin from the overlay call the notes endpoints with `meta.via="dashboard"`. **No press-and-hold done gesture** — notes aren't completed.
- Publishes/consumes `/ws`: a household pin or a note change refreshes the widget via `['dashboard','widget','notes.pripnute']`.

## 11. Tests

- **Slug invariant:** a folder and a note cannot take the same slug under one parent (cross-table check); root-level siblings are deduped (the `COALESCE` index works where a naïve `UNIQUE` wouldn't); collision appends `-2`, `-3`.
- **Resolver:** a valid path resolves to the right `{type,id}`; a path to a renamed/moved item returns `404` (no redirect); an intermediate segment that isn't a folder fails.
- **Move doesn't rewrite the subtree:** moving a folder with descendants rewrites **one** row (the folder's `position`/possibly `slug`), not the descendants — assert row-write count; descendants' paths still resolve via the new parent chain.
- **Cycle guard:** moving a folder into itself or a descendant → `422`, and no write occurred.
- **Ordering:** lexorank move = one write; 200 inserts at the same spot stay strictly ordered/distinct.
- **Folder delete:** non-empty blocks `409` + child count; `?cascade=true` soft-deletes the subtree and logs each child; `?hard=true` is admin-only.
- **Pinning:** household pin requires `editor`+ and is audited; a `reader` can set/remove a **personal** pin (`200`) but gets `403` on a household pin and on every other notes mutation; partial indexes prevent duplicates in each scope.
- **Widget:** de-dup with household precedence; household block then personal; excerpt is plain text; one bounded query; a household pin refreshes the widget for another user, a personal pin does not.
- **FTS5 diacritics:** searching `gulas` finds *Guláš*; the trigger keeps `notes_fts` in sync after insert/update/delete.
- **Audit:** every mutation except personal pins produces an event; `note.update`/`folder.update` produce field diffs; a full-page `body_md` survives the diff untruncated; a note edited from the overlay logs under `notes` with `meta.via="dashboard"` and shows in that note's entity timeline.
- **Isolation:** the import/arch test asserts `modules/notes` imports no other feature module, and no other module imports `modules/notes`.
- **Role gating:** `reader` gets `200` on all reads (tree, detail, resolve, search) and `403` on all writes except personal pin.

## 12. Definition of done

- [ ] `folders`, `notes`, `note_pins`, `notes_fts` (+ triggers) created by this module's migrations, inserted after `events`/before `dashboard`; clean on empty DB and after Litestream restore; nothing seeded.
- [ ] Slug invariant enforced (two `COALESCE` partial indexes + the in-transaction cross-table check); collision suffixing works; root-level dedupe verified.
- [ ] Slug path is **computed** from the parent chain; moving a folder does **not** rewrite descendants; resolver maps path→id with **no redirects**.
- [ ] Folder-move cycle guard rejects self/descendant targets before writing.
- [ ] All endpoints conform to `openapi.yaml` 0.4.0; static routes registered before `/api/notes/{id}`; reads open to all members, writes `editor`/`admin`, **personal pin the one reader-allowed write**; CSRF on mutations.
- [ ] Two-scope pinning: household audited + `/ws`-broadcast + one-per-note; personal not audited, not broadcast, one-per-note-per-user, reader-allowed.
- [ ] `note`/`folder` join the audit diff set; full untruncated `body_md` diffs; dashboard-originated edits carry `meta.via="dashboard"`.
- [ ] FTS5 search is diacritic-insensitive and index-backed; tree loads with no N+1.
- [ ] `notes.pripnute` widget provider registered; de-dup + ordering correct; **row opens an overlay on Nástěnka without navigating away**; reuses `NoteView`; no done gesture.
- [ ] Poznámky frontend (tree/pane + drill-down, WYSIWYG↔Markdown editor, pin + "Kopírovat odkaz") built against the v3 design addendum; loading/empty/error/`reader` states; verified 375 px + 1440 px, both themes.
- [ ] Import/arch test covers `notes`; every mutation (except personal pins) audited in-transaction.
- [ ] `REGISTRY.md` reflects `notes` live once deployed.
