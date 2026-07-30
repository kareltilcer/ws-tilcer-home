# Home — Changelog

Version history for the `home` service. Full detail lives in `PRD.md` (§10 Decisions) and the `HANDOFF*.md` set. OpenAPI versions track the API contract in `openapi.yaml`.

---

## v3 — 2026-07-29 (spec) · Poznámky (notes) module

> OpenAPI **0.3.0 → 0.4.0**. Decisions **D30–D38** added; **D6** extended. Triggered by one product addition from Karel: a notes module.

### Headline

A **fifth module, Poznámky (`notes`)**, added on top of the live v2 as a self-contained package registered through the existing module registry — **no change** to Mode B auth, the dashboard-host contract, or the todo/events/logging modules. It's the first real exercise of "adding a module is adding a package that registers itself" (D25).

### What Poznámky is (§1, §4 FR-P1–P8)

- **Create / edit / view / delete Markdown notes.** The body is stored **once, as Markdown** (D30); the editor offers **WYSIWYG (default)** and a **raw-Markdown** toggle as two round-tripping views over that one source — no HTML or second copy is persisted.
- **Folders + subfolders**, a **single-parent tree** of arbitrary depth; a note lives at the **root or in exactly one folder** (D31). Lexorank sibling order.
- **Human-readable slug-path URLs** — every note and folder is addressable at `/poznamky/<folder>/…/<slug>`, slugs unique across sibling folders+notes so a path resolves to one item; canonical ops by stable id + a path→id resolver; **rename/move changes the URL with no redirects** (D32).
- **Sharing is in-app / household-only** (D33) — a link opens for logged-in members only; **no public access, share tokens, or public routes.**
- **Text + external links only** (D34) — no uploads/attachments, **no blob storage added.**
- **Two pin scopes** (D35) — **"pro všechny"** (household: shared, audited, editor+) and **"jen pro mě"** (personal: per-user view preference, any member incl. `reader`, not audited).
- **FTS5 search** over note title + body (D6-style, FR-P6).

### Dashboard (§4 FR-P8)

- New widget **`notes.pripnute`** (Připnuté poznámky): household pins **∪** the caller's personal pins, de-duplicated (household precedence). A row **opens the note in an overlay dialog on Nástěnka — the user never navigates away**; edits/unpin reuse the notes endpoints with `meta.via="dashboard"` (FR-D5). No change to the dashboard-host contract (FR-M2/D28) — it's just another provider.

### Data model (§5)

- **New:** `folders` (self-ref parent, slug, lexorank), `notes` (folder_id null=root, canonical `body_md`, slug), `notes_fts` (FTS5 over title+body), `note_pins` (scope household|personal, partial unique indexes: one household pin per note, one personal per note per user).
- **Slug invariant:** unique across sibling folders+notes under one parent (partial unique indexes + an app-level cross-check).
- Per-module migrations extend the one Goose sequence: logging → platform → todo → events → **notes**.

### API (openapi 0.3.0 → 0.4.0, §6)

- **Added** under `/api/notes`: `tree`, `resolve?path=`, search `?q=`, note CRUD + `move` + `pin`/unpin, and `folders` CRUD + `move`. New `notes` tag and schemas (Folder(s), Note(s), NotesTree, ResolveResult, PinState/PinRequest/NotePin, PripnutePoznamkyWidget/PinnedNote). `notes.pripnute` added to `WidgetInstance.data` and the catalog `module` enum.
- **Unchanged:** auth, dashboard host, todo, events, logging paths.

### Audit / real-time

- `note` and `folder` **join D6's key diff entities** (full-value diffs, truncate-with-expand). Household pin/unpin is audited; **personal pins are not** (a personal view preference). The audit log **is** the note's history — no separate versioning (D36). `/ws` now also pushes note/folder/household-pin changes.

### Frontend & nav (§7, D37)

- New **Poznámky** destination (desktop tree+pane / mobile drill-down; WYSIWYG↔Markdown editor; pin UI; "Kopírovat odkaz" household link). Regular members keep four thumb-reachable tabs; **admins now have five destinations and exceed the mobile ceiling**, so the app shell needs an **overflow / "více"** pattern for the admin-only Log. Top item for the **v3 design addendum** (`HANDOFF-design.md`).

### Non-goals (v3)

No public/unauthenticated sharing; no uploads/embedded media/blob storage; no slug redirects; no collaborative/simultaneous editing (last-write-wins, D38); no note tags/labels, no export (md/zip), no note-level ACLs — candidate future work.

### Relation to v2

v2 is **live** (deployed 2026-07-29). v3 is a **spec addition** layered on it; because the module is self-contained and additive, v2 continues to run unchanged until v3 is built.

---

## v2 — 2026-07-21 (spec) · self-hosted login, widget dashboard, modular architecture

> OpenAPI **0.2.0 → 0.3.0**. Decisions **D23–D29** added; **D2** and **D16** adjusted. Triggered by two product changes from Karel.

### Headline

Two structural shifts on top of the approved four-module v1:

1. **Auth: Mode A → Mode B.** Home now hosts its **own login UI** and owns its **own session**, verifying credentials against auth BE→BE instead of redirecting to `auth.tilcer.cz`.
2. **Nástěnka becomes a widget host.** Instead of two hardcoded lists, the dashboard is a per-user arrangement of **widgets** that modules provide; each user shows/hides, reorders, and resizes them. This forces the module boundaries to become real, so the whole codebase is restructured into a **compile-time modular monolith** with each module self-contained in `backend/` and `frontend/`.

### Auth (was Mode A, §3)

- **Home hosts login + logout only** (D23). The login form posts to home; home calls auth **`POST /internal/login`** as a service client, then creates its **own session** (cookie on `home.tilcer.cz`) and caches the user's identity + roles.
- **No JWT in the browser** — the earlier per-request `/introspect` verification (v1 D2) is replaced: home authorizes each request from its **own session**, and refreshes identity/roles periodically via auth **`POST /internal/token/mint`**. `/introspect` is no longer on home's hot path. *(D2 adjusted.)*
- **Password-only** in v1 (D23): no TOTP, no Google on home. If auth returns an MFA challenge, home degrades gracefully and points the user at auth-hosted login rather than building an MFA UI.
- **Users are admin-provisioned in auth** — no self-signup on home. **Password reset stays auth-hosted** (home links out). Google OAuth, if ever used, stays auth-hosted (redirect).
- **Home is now a Mode B consumer** of auth, so it needs its own **service client bound to site `home`** (as `fin` does) — used for `/internal/login` and `/internal/token/mint`, not just introspection.
- New session concerns: home owns a **session store** + cookie, **CSRF** (double-submit) on cookie-authenticated state-changing routes, and its **own revocation** (a disabled user keeps working until the next role refresh, ≤ the refresh interval).

### Dashboard → widget host (was FR-N1–N3, §4/§7)

- **Nástěnka is a widget host** (D24). It owns no feature data; it renders **widgets** contributed by modules and stores each user's **layout**.
- **Per-user layout is server-side** (D24): which widgets are visible, their order, and their size — persisted in home's DB and synced across devices. A user can **show/hide, reorder (drag), and resize** widgets (narrow/wide).
- **v1 widget catalog** (D27): **Právě dělám** (todo — cards in `kind=now` columns across boards), **Připomínky** (events — active reminders), **Tento měsíc** (events — look-ahead upcoming events). *(No admin log widget in v1.)*
- The old FR-N1 aggregation logic **moves into module-provided widget providers**; the host fans out to visible widgets in one request (D28). Mark-done still reuses the owning module's endpoints with `meta.via="dashboard"`, and the **2000 ms press-and-hold** (D22) is preserved inside the task/reminder widgets.
- **"Active items only" (v1 D16) relaxed:** widgets define their own scope, so *Tento měsíc* legitimately looks ahead. *(D16 adjusted.)*

### Architecture: compile-time modular monolith (D25, D28)

- **Strict module boundaries.** Each module is a **self-contained backend package** (its own routes, migrations, audit actions, and widget providers) and a **frontend folder** (its page + its widgets), wired through a **central registry**. One binary, one deploy — but modules don't import each other's internals.
- **Module code identifiers stay English** (D26, per the earlier D17): `logging`, `todo`, `events`, `dashboard`, `platform`/core. UI shows Czech (Úkoly, Okno, Nástěnka).
- **Per-module migrations** (D25): each module owns its migration files, run in one Goose sequence — replaces v1's single `0001_init`.
- **Cross-module data flows through the widget-provider contract only** (D28): the dashboard host calls a registered provider interface, never a module's tables. Modules communicate through defined interfaces, not shared DB access.

### Data model (§5)

- **New:** `sessions` (home's own session store — token hash, user, UA/IP, sliding expiry, revocation) and `user_dashboard_layout` (`user_id`, `widget_key`, `visible`, `position`, `size`).
- Events/todo/logging tables unchanged in shape; now created by **per-module migrations** rather than one init.

### API (openapi 0.2.0 → 0.3.0, §6)

- **Added:** `POST /api/auth/login`, `POST /api/auth/logout`, `GET /api/auth/session`.
- **Changed:** `GET /api/dashboard` now returns `{ layout, widgets[] }` (host fan-out), not two fixed lists. Added `GET /api/dashboard/catalog`, `PUT /api/dashboard/layout`, `GET /api/dashboard/widgets/{key}` (single-widget refresh).
- **Security schemes:** home **session cookie** + **CSRF header** on cookie-authenticated mutations; the browser no longer carries a bearer JWT.
- Todo/events/logging paths unchanged.

### Config (§9)

- **New/changed:** `HOME_AUTH_SERVICE_SECRET` now authenticates `/internal/login` + `/internal/token/mint` (not just introspect); `HOME_SESSION_TTL_DAYS` (own session sliding window, default 90); `HOME_ROLE_REFRESH_MINUTES` (how often home re-mints to refresh roles, default 15); `HOME_CSRF_*`. `AUTH_BASE_URL` now also the target of the "reset password" / MFA-fallback links.

### Design

- **New screens require a design addendum** (flagged in `HANDOFF-design.md` §v2): home-hosted **login/logout** screens, the **widget host** (grid, drag-reorder, resize, add/remove from a catalog), and the **empty/first-run** dashboard. The v1 prototype covered neither. Not blocking the backend build, but blocks the dashboard/login frontend.

### Migration from v1

v1 was a spec, never built — so there is **no data migration**. v2 supersedes v1 as the build target. Where v1 handoffs conflict with v2, **v2 wins**.

---

## v1 — 2026-07-21 (spec, superseded) · four modules, Mode A

Initial approved spec + design. Four modules (`logging`, `todo`, `events`, `dashboard`), Mode A auth (redirect to auth-hosted login), Nástěnka as a hardcoded two-list aggregator, single `0001_init`. Decisions D1–D22. Design prototype delivered and reviewed (five deviations; D22 press-and-hold adopted). Never implemented.
