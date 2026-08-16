# Home — Changelog

Version history for the `home` service. Full detail lives in `PRD.md` (§10 Decisions) and the `HANDOFF*.md` set. OpenAPI versions track the API contract in `openapi.yaml`.

---

## v4 — 2026-08-11 (spec) · Dokumenty (documents) module

> OpenAPI **0.4.0 → 0.5.0**. Decisions **D39–D50** added; **D6** extended again, **D33** reaffirmed. Triggered by one product addition from Karel: a documents/file-storage module.

### Headline

A **sixth module, Dokumenty (`documents`)**, added on top of v3 as a self-contained package registered through the existing module registry — **no change** to Mode B auth, the dashboard-host contract, or the todo/events/logging/notes modules. It is the **first module with blob storage**: it owns a dedicated **Cloudflare R2 bucket** for file bytes alongside the Litestream-backed SQLite (which now holds only document *metadata*).

### What Dokumenty is (§1, §4 FR-DOC1–DOC11)

- **Upload / preview / download / delete files** — PDFs, images, and Office/other docs — in a **single-parent folder tree** (its own tree, isolated from Poznámky — D40), reusing Poznámky's slug/resolver/cycle-guard pattern (D31/D32) over separate data.
- **Blobs in a dedicated R2 bucket; metadata in SQLite** (D41). A document's bytes are **immutable / upload-once** — never replaced or overwritten; a changed file is a **new** document, and the old one can be deleted after confirmation (D50). No version history.
- **Permanent, id-based, household-only URL** (D42) — the permanent link is derived from the document's **immutable UUID** (`/d/{id}` → `/api/documents/{id}/raw`), **not** the slug path (which changes on rename/move, D32). Served by the backend, session-gated — **no public access, no share tokens** (D33 upheld).
- **Uploads through the backend** (D43) — multipart `POST /api/documents` streams to R2, sniffs MIME server-side, checksums (SHA-256), records metadata + audit in one transaction. Cap **50 MB**.
- **Preview** (D44) — PDFs/images/text preview natively; **Office → PDF via headless LibreOffice**, generated **once** async (immutable ⇒ derive-once, cache-forever) with `preview_status` transitions and a `/ws` push; failures degrade to download-only without losing the upload.
- **Search is filename + metadata** (FTS5 over title + filename + description, D46) — **not** file contents.
- **Two pin scopes** (D47, mirrors D35) — "pro všechny" (household, audited, editor+) and "jen pro mě" (personal, per-user, not audited).

### Dashboard (§4 FR-DOC11)

- New widget **`documents.pripnute`** (Připnuté dokumenty): household pins **∪** the caller's personal pins, de-duplicated (household precedence). A row **opens the document in a preview overlay on Nástěnka — the user never navigates away**; overlay actions reuse the documents endpoints with `meta.via="dashboard"`. No done gesture. No change to the dashboard-host contract — it's just another provider.

### Data model (§5)

- **New:** `document_folders` (self-ref parent, slug, lexorank — its own tree), `documents` (folder_id null=root, title/slug, original_filename, sniffed `content_type`, `byte_size`, `checksum`, `storage_key`, `preview_kind`/`preview_status`/`preview_key`/`thumbnail_key` — **no version columns**), `documents_fts` (FTS5 over title+filename+description), `document_pins` (partial unique indexes: one household per document, one personal per document per user).
- **Slug invariant:** unique across sibling document-folders+documents under one parent (`COALESCE` partial unique indexes + an app-level cross-check), exactly like Poznámky.
- **R2 object layout:** `documents/{id}/original` (write-once), `documents/{id}/preview.pdf`, `documents/{id}/thumb.webp` — **id-based keys** (renames/moves never touch R2).
- Per-module migrations extend the one Goose sequence: logging → platform → todo → events → notes → **documents**.

### Backup — the open architecture question, resolved (§8, D45)

- **Litestream cannot back up the R2 blob bucket** (it only replicates the SQLite WAL). So: document **metadata** rides Litestream (`home/`) as usual, and the **bucket** gets its own strategy — **object versioning** on the primary bucket (recovers deletes; bytes are immutable so there are no overwrites) **plus a scheduled server-side mirror to a second R2 bucket** (`HOME_DOCS_R2_BACKUP_BUCKET`). Periodic reconciliation flags orphaned objects / dangling rows. Fresh build: metadata from Litestream, bytes from R2. *(Answer: not Litestream — mirror + versioning.)*

### API (openapi 0.4.0 → 0.5.0, §6)

- **Added** under `/api/documents`: `tree`, `resolve?path=`, search `?q=`, **multipart upload**, document CRUD (PATCH = metadata only) + `move` + `pin`/unpin, the four content endpoints **`raw` / `download` / `preview` / `thumbnail`** (permanent, household-only, Range + checksum ETag + immutable cache), and `folders` CRUD + `move`. New `documents` tag and schemas (DocFolder(s), Document(s), DocumentDetail, DocumentsTree, DocResolveResult, DocumentUpload/Update/Move, DocumentPin, DocumentUrls, PripnuteDokumentyWidget/PinnedDocument). `documents.pripnute` added to `WidgetInstance.data` and the catalog `module` enum.
- **Unchanged:** auth, dashboard host, todo, events, logging, notes paths.

### Audit / real-time / security

- `document` and `document_folder` **join D6's key diff entities** — **metadata diffs only** (bytes are immutable, never diffed). Household pin/unpin audited; personal pins not. `/ws` now also pushes document/document-folder/household-pin changes and `document.preview_ready`/`_failed`.
- **Untrusted-content isolation** (D48): MIME sniffed server-side; originals default to attachment + `nosniff` + sandboxed CSP; inline previews only through safe viewers (PDF.js sandboxed iframe, `<img>`, escaped text); active types download-only.

### Frontend & nav (§7, D49)

- New **Dokumenty** destination (desktop tree+grid / mobile drill-down; upload + drag-drop; a standalone `DocumentView` reused by the dashboard overlay; pin UI; "Kopírovat odkaz" copies the permanent `/d/{id}` household link). **Regular members now have five destinations and also exceed the four-tab mobile ceiling** that only admins hit in v3, so the overflow/"více" pattern **generalizes to everyone**. Top item for the **v4 design addendum** (`HANDOFF-design.md`).

### Non-goals (v4)

No public/unauthenticated access or share tokens; no content replace/overwrite/version history (immutable, D41); no in-file full-text search or OCR (filename+metadata only, D46); no in-browser editing/annotation/e-sign; no third-party storage integrations; no per-document ACLs; Office preview limited to LibreOffice-convertible formats; >50 MB and streaming media out of scope — candidate future work.

### Relation to v3

v3 (Poznámky) is a spec addition on the live v2; v4 layers `documents` on top. Because the module is self-contained and additive, nothing in v1/v2/v3 changes to build it — the one new operational dependency is the R2 documents bucket(s) + headless LibreOffice in the runtime image.

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
