# PRD — Home

> Status: **Draft v4** — adds the **Dokumenty** (`documents`) module on top of v3. Self-hosted login (Mode B), widget dashboard, modular architecture unchanged. Supersedes v3. Decisions D1–D50 (§10); v3→v4 delta in `CHANGELOG.md`. Pending Karel's final approval. · Owner: Karel · Last updated: 2026-08-11
> Companion spec: `openapi.yaml` (OpenAPI 3.1, **v0.5.0**) · Notes: `notes.md` · Design brief: `HANDOFF-design.md` (v2 addendum done; **v3 + v4 addenda pending** — Poznámky screens, Dokumenty screens, and the mobile 6th-destination nav) · Build: `HANDOFF*.md`

> **v4 scope:** a single `home` fe/be pair — a Czech-language, mobile- and desktop-friendly household management system — built as a **compile-time modular monolith**. **Six** modules:
> 1. **Logging spine** (`logging`) — an in-process audit component every module writes through, plus an admin log browser.
> 2. **To-do** (`todo`) — a Trello-style board (Úkoly).
> 3. **Okno do budoucnosti** (`events`) — all-day, optionally recurring future events with in-app reminders.
> 4. **Nástěnka** (`dashboard`) — the landing page, a **per-user widget host**: modules contribute widgets, each user shows/hides/reorders/resizes them.
> 5. **Poznámky** (`notes`) — new in v3. Markdown notes (WYSIWYG-default with a raw-Markdown toggle) organised into folders/subfolders, each note and folder reachable by a stable in-app URL and contributing a **pinned-notes widget** to Nástěnka.
> 6. **Dokumenty** (`documents`) — **new in v4.** File storage: PDFs, images, and Office/other docs uploaded into folders/subfolders, each with an in-browser **preview** (PDF/images/text native; Office server-converted to PDF) and **download**, a **permanent household-only URL**, blobs held in a dedicated **Cloudflare R2** bucket, contributing a **pinned-documents widget** to Nástěnka.
>
> Home hosts its **own login** and owns its **own session** (Mode B), verifying credentials against the shared auth service BE→BE. Everything is built around the logging spine so every module inherits auditability, and around a **module registry** so modules stay isolated. **v4 adds one self-contained module (`documents`) through that registry — no change to auth, the dashboard host contract, or the other five modules.** `documents` is the first module to add **blob storage**: it owns a dedicated R2 bucket alongside the Litestream-backed SQLite (see §1 Architecture — Dokumenty, §8 Backup).

## 1. Overview

- **One-line summary:** A Czech-language household management system whose modules all write through a central audit spine and surface widgets on a per-user dashboard; home hosts its own login and owns its session, authenticating against auth BE→BE.
- **Type:** fe/be pair. React + TS + TanStack Query SPA over a Go + embedded-SQLite backend, both organized as isolated modules.
- **Subdomain:** `home.tilcer.cz`.
- **Exposure:** **public** (routed via Coolify), gated by home's own session. Used from phones and laptops on and off the home network.
- **Consumers:** household members (browser); admins (a household member with the `admin` role) additionally browse the audit log. No other service consumes `home`.
- **Depends on:** **auth** (`auth.tilcer.cz`), site key **`home`**, **Mode B (consumer-delegated login)**. Home authenticates users against auth BE→BE and is itself a registered auth **service client** bound to site `home`.

### Architecture — auth is Mode B (§10 D23)

Home hosts its own login and owns its own session. It is a **Mode B consumer** of auth (the pattern auth documents for `fin`).

- **Login.** The browser posts credentials to **home**; home calls auth **`POST /internal/login`** (as its service client, `X-Service-Secret`) to verify them. On success auth returns a site-scoped assertion of the user (identity + roles); home creates its **own session** and caches that identity.
- **No JWT in the browser.** Home authorizes each request from **its own session cookie** — there is no bearer token in JS and no per-request `/introspect` (this replaces v1's Mode-A verification, §10 D2). Home keeps roles fresh by re-minting via auth **`POST /internal/token/mint`** on a threshold (`HOME_ROLE_REFRESH_MINUTES`, default 15 min); a mint that fails closed (user disabled/deleted in auth) drops home's session. This is the accepted Mode B revocation trade-off: a revoked user keeps working for at most one refresh interval.
- **Password-only (v1 of Mode B).** No TOTP, no Google on home (§10 D23). If auth ever returns an MFA challenge, home does **not** build an MFA UI — it shows a message directing the user to auth-hosted login. Google sign-in, if ever added, stays auth-hosted (redirect).
- **Provisioning & recovery stay on auth.** Users are **admin-provisioned in auth** (no self-signup on home). **Password reset is auth-hosted** — home links out to it. Home never sees a signup or reset flow.
- **Trade-offs accepted (Mode B, per auth §3.5):** home receives users' **plaintext passwords in transit** on `/api/auth/login` — it must use TLS, never log or persist them, and forward-then-discard. Home owns its **own session lifecycle and revocation**.

### Architecture — the logging spine (§10 D-arch, unchanged from v1)

The `logging` module is an **in-process Go package** with **dedicated SQLite tables**, the spine every module writes through **inside the same transaction** as the change it records — guaranteeing no successful action goes unlogged and no rolled-back action leaves an event. Kept behind an `AuditSink` interface (extraction seam) but not extracted in v1. Operational request logs (stdout, for Coolify) and domain audit events (the spine's DB, browsable) stay separate, linked by request id. Cross-module actions log under their **owning** module with `meta.via` naming the trigger.

### Architecture — compile-time modular monolith (§10 D25, D28)

The whole codebase is organized as **isolated modules** behind a central registry — one binary, one deploy, strict seams.

- **A module owns**, in the backend: its routes, its Goose migrations, its audit actions, and any **widget providers** it exposes. In the frontend: its page(s), its widget component(s), and its API bindings. Nothing else reaches into it.
- **Modules do not import each other's internals.** Cross-module data flows only through **registered contracts** — chiefly the widget-provider interface (§4 FR-M2). The dashboard host renders widgets without ever touching `todo`/`events` tables.
- **A central registry** (`platform`/core) wires modules in: each module registers its routes, migrations, audit actions, and widgets at startup. Adding a future module is adding a package that registers itself — not editing the host.
- **Module code identifiers are English** (§10 D26, per D17): `logging`, `todo`, `events`, `dashboard`. The UI shows Czech.

### Architecture — recurrence & reminders (§10 D11–D14, D19, unchanged from v1)

Events store an **RFC 5545 RRULE subset**, never expanded occurrences; occurrences are computed on read within a bounded, capped window. The only per-occurrence row is a reminder **completion**. Reminders are **computed on read** (no scheduler — §10 D9/D11). Short-month anchors **clamp to the month's last day** (§10 D19), a deliberate deviation from RFC defaults. All date math runs in `HOME_TIMEZONE` (`Europe/Prague`).

### Architecture — Poznámky (`notes`, §10 D30–D37, new in v3)

A self-contained fifth module registered like the others (routes, own migrations, audit actions, one widget provider). Key shape:

- **Markdown is the single canonical form** (§10 D30). A note's body is stored once, as Markdown. The editor offers **WYSIWYG (default) and raw-Markdown** as two views over that one source — both read and write the same Markdown, round-tripping; there is no second stored representation and no HTML persisted.
- **A single-parent folder tree** (§10 D31). Folders nest arbitrarily; a note lives at the **root or in exactly one folder** (no multi-filing). Siblings order by lexorank, like everything else.
- **Human-readable slug-path URLs** (§10 D32). Each folder and note is reachable at `/poznamky/<folder>/…/<slug>`; slugs are **unique across the sibling folders *and* notes under one parent**, so a path resolves to exactly one item. Canonical operations are by **stable id**; a resolver turns a path into an id. **Renaming or moving changes the URL** (the slug/path is derived from the title and location) — v1 keeps **no redirects**, so an old path 404s. Accepted tradeoff of readable links.
- **Sharing is in-app / household-only** (§10 D33). A URL is "shared" in the sense that any **logged-in** home member can open it; there is **no public, unauthenticated access** — no share tokens, no public routes. Poznámky stays entirely inside Mode B's gate.
- **Text + external links only** (§10 D34). No uploads and no embedded attachments; **no blob storage is added.** Markdown may reference an external image URL, but nothing is stored server-side.
- **Two pin scopes** (§10 D35). A note can be pinned **"pro všechny"** (a household pin — shared, audited, editor+) or **"jen pro mě"** (a personal pin — a per-user view preference any member incl. `reader` may set, not audited). The `notes.pripnute` widget shows the household pins **∪** the caller's personal pins.

### Architecture — Dokumenty (`documents`, §10 D39–D50, new in v4)

A self-contained sixth module registered like the others (routes, own migrations, audit actions, one widget provider). It **reuses Poznámky's folder-tree + slug-path pattern** but stores **files in a dedicated R2 bucket** instead of Markdown in SQLite. SQLite holds only **metadata**; the bytes live in R2. Key shape:

- **Its own folder tree, isolated from Poznámky** (§10 D40). `documents` ships `document_folders` + `documents` tables and its own resolver — a Dokumenty folder is **not** a Poznámky folder (module isolation, D25/D28). Same single-parent, arbitrary-depth, lexorank, slug-path model as `notes` (D31/D32), separate data.
- **Blobs in a dedicated R2 bucket; content is immutable** (§10 D41). Each uploaded document is written **once** to R2 and its bytes are **never replaced or overwritten** — to change content you upload a **new** document; the old one can be deleted (after confirmation). Immutability is what makes the permanent URL trivially cacheable.
- **Permanent URL = stable, id-based, household-only** (§10 D42, D33). The permanent link is derived from the document's **immutable UUID**, not its slug path (a slug path changes on rename/move — D32). The backend serves it, gated by home's session — **no public access, no share tokens** (Mode B, D33 upheld). The human-readable slug path (`/dokumenty/<folder>/…/<slug>`) stays for navigation but is explicitly **non-permanent**.
- **Uploads go through the backend** (§10 D43). A multipart `POST /api/documents` streams to R2, the backend validates size/MIME, computes a SHA-256 checksum, and records metadata + audit **in one transaction**. R2 stays fully private. Max **50 MB** per file (`HOME_DOCS_MAX_UPLOAD_MB`).
- **Previews: native for PDF/images/text, server-converted for Office** (§10 D44). PDFs, images, and text/Markdown preview directly; docx/xlsx/pptx/odt are converted to a **derived preview PDF** by headless LibreOffice **once** at upload (bytes immutable ⇒ derive once, cache forever), stored as a second R2 object. Conversion is **async** — a document is created immediately with `preview_status="pending"`, then flips to `ready`/`failed`/`none` and pushes `/ws`. Everything else is download-only.
- **Search is filename + metadata, not contents** (§10 D46). `documents_fts` (FTS5) over title + original filename + description; no in-file text extraction / OCR in v4.
- **Two pin scopes + widget, exactly like Poznámky** (§10 D47). `document_pins` (household audited / personal per-user unaudited) and a `documents.pripnute` ("Připnuté dokumenty") widget whose rows open a **preview overlay on Nástěnka** without navigating away.
- **Backup: mirror + versioning, because Litestream can't back up blobs** (§10 D45, §8). Litestream backs up the metadata DB as usual (`home/`); the R2 bucket is protected separately by **object versioning** (recovers deletes/mistakes) **and a scheduled mirror to a second R2 bucket**. A fresh build restores metadata from Litestream and reads bytes from R2.
- **Untrusted-content isolation** (§10 D48, §8). Because arbitrary user files are served from home's own origin, originals default to `Content-Disposition: attachment` + `X-Content-Type-Options: nosniff` + a sandboxed CSP; inline previews are limited to safe viewers (PDF.js in a sandboxed iframe, `<img>`, escaped text). MIME is **sniffed server-side**, never trusted from the client.

## 2. Goals & Non-Goals

**Goals**

- Host home's **own login** and session (Mode B), authenticating against auth BE→BE, with **no token in the browser**.
- Ship the **logging spine** capturing every mutation across every module as a queryable audit event, atomic with the change; plus an admin log browser.
- Ship **to-do** (Úkoly) and **events** (Okno do budoucnosti) as before.
- Ship **Nástěnka as a per-user widget host**: modules contribute widgets; each user shows/hides, reorders, and resizes them; layout persists server-side and syncs across devices.
- Ship **Poznámky** (`notes`, v3): create/edit/view/delete **Markdown** notes (WYSIWYG-default, raw-Markdown toggle) in a **folder/subfolder** tree; every note and folder has a stable in-app **slug-path URL** any household member can open; notes can be **pinned** ("pro všechny" / "jen pro mě") and surface through a **Nástěnka widget** whose rows open in an overlay dialog **without leaving Nástěnka**.
- Ship **Dokumenty** (`documents`, v4): upload/preview/download/delete files (PDFs, images, Office & other docs) in a **folder/subfolder** tree; blobs stored in a dedicated **R2** bucket with **metadata in SQLite**; every document has a **permanent, id-based, household-only URL** and an in-browser **preview** (native for PDF/image/text, Office server-converted to PDF); content is **immutable** (upload-once, no overwrite); documents can be **pinned** ("pro všechny" / "jen pro mě") and surface through a **Nástěnka widget** whose rows open a preview overlay **without leaving Nástěnka**; the bucket is **backed up** (versioning + mirror bucket) since Litestream covers only the DB.
- Enforce **real module boundaries** — each module self-contained in `backend/` and `frontend/`, wired through a registry, communicating only through defined contracts.
- Support a **household** (multiple members), Czech UI, dark-default theme, mobile- and desktop-friendly, common actions in one or two taps.
- Faithfully follow project conventions (observability, Goose, Litestream→R2, OpenAPI 3.1).

**Non-Goals (v2)**

- **No self-signup and no password reset on home** — users are admin-provisioned in auth; reset is auth-hosted (home links out).
- **No TOTP or Google sign-in on home** — password-only; MFA/Google stay auth-hosted (§10 D23).
- **No JWT in the browser** — authorization is from home's own session.
- **No runtime-pluggable modules** — modularity is compile-time (§10 D25); no dynamic load/enable, no plugin lifecycle.
- **No user-authored or third-party widgets** — the widget catalog is the set the modules ship (§10 D27).
- **Poznámky (v3):** **no public / unauthenticated sharing** — in-app household access only, no share tokens or public routes (§10 D33). **No file or image uploads / embedded attachments** — Markdown + external links only, no blob storage (§10 D34). **No slug redirects** — a renamed/moved item's old URL 404s (§10 D32). **No collaborative / simultaneous editing** — note bodies are **last-write-wins** (§10 D38); the audit spine's full-value diffs are the note's history — **no separate version-history UI** (§10 D36). **No note tags/labels, no full note export (md/zip), no note-level permissions/ACLs** in v3 — notes inherit the household role model; these are candidate future work.
- **Dokumenty (v4):** **no public / unauthenticated access** — permanent URLs are household-only, no share tokens or public routes (§10 D33/D42). **No content replace / overwrite / version history** — a document's bytes are immutable; a changed file is a **new** document (§10 D41). **No in-file full-text search or OCR** — search is filename + metadata only (§10 D46). **No in-browser document editing, annotation, or e-signing.** **No third-party storage integrations** (Google Drive/Dropbox/etc.). **No per-document ACLs** — documents inherit the household role model. Office preview is limited to **LibreOffice-convertible** formats; files above **50 MB** and streaming media are out of scope for v4 — candidate future work.
- **No push or email notifications** — reminders are in-app, computed on read.
- **No per-occurrence event exceptions, no time-of-day on events, no external calendar sync/iCal, no multiple reminders per event.**
- **No card assignee, no card due dates, no collaborative editing, no separate logging microservice, no offline/PWA, no cross-board card moves.**

## 3. Users, Roles & Auth

Home is a consumer **site** (`home`) of auth. Household members are **`single_site`** accounts bound to site `home`, **provisioned by an admin in auth** (no self-signup here). Access is **Mode B**: home hosts login and owns the session (see §1 Architecture).

**Roles** — auth's default template **`admin` / `editor` / `reader`** (§10 D5):

- **`admin`** — full access, including the **log browser** and structural management (create/delete boards, columns, labels; delete folders; **hard-delete documents/folders, purging R2 objects**).
- **`editor`** — full use of to-do, events, Nástěnka, **Poznámky**, and **Dokumenty** (create/move/edit/complete; create/edit/move notes and folders; **upload/rename/move/soft-delete documents and document folders**; **household-pin** a note or document "pro všechny"; arrange own widgets). No log browser.
- **`reader`** — **view-only**: read boards, cards, events, **notes and folders**, **documents and document folders** (incl. **preview and download**), and their dashboard; no mutations, no log browser. (A reader still arranges their **own** dashboard layout **and may pin a note or document "jen pro mě"** — both are personal view preferences, not data mutations.)

Roles come from auth (cached in home's session, refreshed via `/internal/token/mint` per `HOME_ROLE_REFRESH_MINUTES`). `roles:["*"]` (superuser) ⇒ full access. Home authorizes every request **server-side from its session** — never from client input.

- **Reads** (`GET` on module data, incl. notes/folders/tree/resolve/search; **documents/folders/tree/resolve/search and `raw`/`download`/`preview`/`thumbnail`**): any authenticated member.
- **Writes** (`POST`/`PATCH`/`DELETE`/move/complete/layout; notes+folders CRUD/move; **documents+folders upload/CRUD/move**; **household pin**): `editor` or `admin`. **Exempt (personal view preference, any member incl. `reader`):** dashboard **layout**, a **personal note pin**, and a **personal document pin** ("jen pro mě"). Note/folder/**document/document-folder** **hard-delete** (`?hard=true`, cascading; documents also purge R2 objects) follows the app-wide destructive-op convention (`admin`).
- **Log browser** (`/api/logs/**`): `admin` only.
- **Auth + health** (`/api/auth/*`, `/healthz`, `/readyz`): unauthenticated-reachable (login must work before there's a session).

## 4. Functional Requirements

Grouped by module. **Every mutating requirement records an audit event through the spine in the same transaction** — stated once, not repeated. Reads are not logged.

### Platform / auth (`platform`)

#### FR-A1: Login (home-hosted, Mode B)
- **Trigger:** member submits home's login form.
- **Inputs:** `email`, `password`.
- **Behaviour:** call auth `POST /internal/login` as the `home` service client. On success, create a home **session** (256-bit token, stored hashed; sliding `HOME_SESSION_TTL_DAYS`) set as an `HttpOnly; Secure; SameSite=Lax` cookie host-only on `home.tilcer.cz`; cache `user_id`, `email`, `display_name`, `roles`, and `roles_refreshed_at` in the session. Issue a CSRF cookie (readable by JS) for later state-changing requests. **Never log or persist the password**; forward and discard. Log `platform.login` (actor = the user).
- **Outputs:** `200 { user }` + `Set-Cookie: session`, `Set-Cookie: csrf`.
- **Errors:** `401` bad credentials (generic); `403` disabled/unverified/no access to site `home`; `409 { error:"mfa_required" }` if auth demands MFA (home does not handle it — the UI points the user to auth-hosted login, §10 D23); `429` throttled; `502` auth unreachable.

#### FR-A2: Session authorization & role refresh
- **Behaviour:** every `/api/**` request (except `/api/auth/*`) is authorized from the **session cookie**: validate the session, load cached identity + roles, enforce the role gate (§3). When `now - roles_refreshed_at > HOME_ROLE_REFRESH_MINUTES`, re-mint via auth `POST /internal/token/mint` `{ user_id, site:"home" }`, refresh cached roles, and update the timestamp. If mint **fails closed** (user disabled/deleted/unverified in auth), revoke the session and return `401`. Slide the session expiry on activity.
- **Errors:** `401` no/invalid/expired session; `403` insufficient role.

#### FR-A3: Logout
- **Behaviour:** revoke the current session server-side and clear the cookies. Log `platform.logout`.
- **Outputs:** `204`.

#### FR-A4: Current session (bootstrap)
- **Behaviour:** `GET /api/auth/session` returns `{ user }` for a valid session, else `401`. The SPA calls this on load to decide login vs app.

#### FR-A5: CSRF protection
- **Behaviour:** cookie-authenticated **state-changing** routes (everything under `/api` that mutates, plus `/api/auth/logout`) require a double-submit CSRF token (`X-CSRF-Token` header matching the `csrf` cookie) **and** an `Origin`/`Referer` allowlist check against `*.tilcer.cz`. Login itself is exempt (no session yet) but rate-limited.

### Module registry (`platform`)

#### FR-M1: Module registration
- **Description:** each module registers itself with the core at startup — its **routes**, **migrations**, **audit actions**, and **widget providers**. The core owns no feature logic; it composes modules.
- **Behaviour:** a module implements a `Module` interface (name, `RegisterRoutes(router)`, `Migrations() fs.FS`, `Widgets() []WidgetProvider`). The core iterates registered modules to build the router, run migrations in one Goose sequence, and populate the widget catalog. Modules are compiled in; there is no dynamic loading (§10 D25).

#### FR-M2: Widget provider contract
- **Description:** the single interface through which a module exposes a dashboard widget, and the **only** way feature data reaches the dashboard host (§10 D28).
- **A widget provider declares:** a stable `key` (e.g. `todo.pravedelam`), a Czech `title`, its owning `module`, a `default_size`, an `admin_only` flag, and a server-side `Data(ctx, user) → payload` function scoped to the requesting user's permissions.
- **Behaviour:** the host never imports a module's tables; it calls `Data(...)`. A widget may also register a **single-widget refresh** endpoint (`GET /api/dashboard/widgets/{key}`) that returns the same payload, used for websocket-driven refresh. Mutations invoked from a widget (mark-done) call the **owning module's** existing endpoint, not a dashboard-specific one.

### Logging module (`logging`) — unchanged from v1

FR-L1 record event (spine, `*sql.Tx`, same-transaction, fails the tx on error); FR-L2 field diffs for key entities `card, column, board, label, checklist_item, event` (full values); FR-L3 browse with FTS5 free-text + composed filters; FR-L4 event detail; FR-L5 entity timeline (oldest-first, includes cross-module actions); FR-L6 analytics; FR-L7 append-only + optional self-logging prune. Log browser is **admin-only**. See `HANDOFF-1-logging.md`.

### To-do module (`todo`) — unchanged from v1, plus a widget

FR-T1 boards (seed one **"Domácnost"** / Zásobník·Právě dělám·Hotovo when empty); FR-T2 columns (CRUD, lexorank order, priority, client-side collapse, free-form `now`/`done` `kind`); FR-T3 cards (CRUD, move with `done_at` stamping, soft delete); FR-T4 notes+links; FR-T5 checklist; FR-T6 labels; FR-T7 board tree read model. See `HANDOFF-2-todo.md`.

#### FR-T8: "Právě dělám" widget provider (§10 D27)
- **Provides** widget `todo.pravedelam`: every non-archived card in any `kind=now` column **across all non-archived boards**, each carrying board + column (for grouping), labels, and checklist progress. Sorted board order → column priority → card position. Mark-done in the widget calls `POST /api/cards/{id}/move` (→ the board's first `kind=done` column, else archive) with `meta.via="dashboard"`, via the **2000 ms hold** gesture (§10 D22).

### Events module (`events`) — unchanged from v1, plus two widgets

FR-E1 CRUD (all-day, series-only edits, soft delete); FR-E2 recurrence (RRULE subset, expand-on-read, capped, D19 clamping); FR-E3 links; FR-E4 reminder config (declarative); FR-E5 month-grouped occurrence list; FR-E6 idempotent per-occurrence completion + undo. See `HANDOFF-3-events.md`.

#### FR-E7: "Připomínky" widget provider (§10 D27)
- **Provides** widget `events.pripominky`: active event reminders — the earliest uncompleted occurrence per reminder-enabled event within `HOME_DASHBOARD_LOOKBACK_DAYS`, shown when `today >= occurrence − lead`, overdue flagged and sorted first, at most one per event. Mark-done calls `POST /api/events/{id}/complete` with `meta.via="dashboard"`, via the **2000 ms hold** (§10 D22).

#### FR-E8: "Tento měsíc" widget provider (§10 D27)
- **Provides** widget `events.tento-mesic`: a **read-only look-ahead** — upcoming occurrences from today through the end of the current month (or the next `N` days), grouped/sorted by date. No completion control. This is why "active items only" (v1 D16) is relaxed: widgets set their own scope (§10 D16-adjusted).

### Notes module (`notes`) — new in v3, plus a widget

> Poznámky: Markdown notes in a folder tree, each note and folder addressable by an in-app slug path, with a pinned-notes widget. A self-contained module (own routes, migrations, audit actions, widget provider) registered like the rest (FR-M1). Markdown is the single stored form (§10 D30). All mutations audit through the spine in-transaction; **personal pins are the one exception — a per-user view preference, not audited** (§10 D35).

#### FR-P1: Notes CRUD
- **Trigger:** a member creates, edits, views, or deletes a note.
- **Inputs:** `title`, optional `folder_id` (null ⇒ root), optional `body_md` (Markdown). Edits may change `title`, `body_md`, `archived`.
- **Behaviour:** store the body once as **Markdown** (§10 D30). On create/rename, derive a URL `slug` from the title, unique among the parent's sibling folders+notes (§10 D32) — collisions get a numeric suffix. Soft-delete by default, `?hard=true` to purge (§10 D8). Body edits are **last-write-wins** (§10 D38). A note carries `created_by`/`created_at`/`updated_at`.
- **Outputs:** the note (detail view returns `body_md`, folder path/breadcrumb, and the caller's pin state); `204` on delete.
- **Errors:** `401`; `403` non-editor write; `404`; `409` slug conflict that can't be auto-resolved; `422` empty title / bad `folder_id`.

#### FR-P2: Folders CRUD (single-parent tree)
- **Behaviour:** create a folder under a parent (null ⇒ root); nest **arbitrarily deep** (§10 D31). `name` → `slug`, unique among siblings (folders+notes) under the parent. Rename updates the slug (URL changes — §10 D32). Delete is soft by default; a **non-empty** folder requires `?cascade=true` (else `409` with the child count), mirroring column delete; `?hard=true` purges (`admin`, §3).
- **Errors:** `401`; `403`; `404`; `409` (non-empty without cascade / slug conflict); `422`.

#### FR-P3: Move note / folder
- **Behaviour:** `POST /api/notes/{id}/move` (`{ folder_id?, position }`) reparents and/or reorders a note; `POST /api/notes/folders/{id}/move` (`{ parent_id?, position }`) does the same for a folder. Reparenting **re-derives the slug** if needed to stay unique in the new parent and **changes the item's URL** (and every descendant's — §10 D32). **A folder may not move into itself or a descendant** → `422` (cycle guard). Order via lexorank (§10 D4).

#### FR-P4: Slug-path URLs & resolver
- **Description:** the shareable, bookmarkable address of a note or folder.
- **Behaviour:** the SPA's pretty route is the slug path (`/poznamky/<folder>/…/<slug>`). `GET /api/notes/resolve?path=<slug-path>` maps a path to `{ type: "folder"|"note", id }` (or `404` if nothing/if the item was renamed/moved — **no redirects**, §10 D32). All mutating/detail endpoints address items by **stable id**; the frontend resolves path→id on navigation, then works by id. Sharing is **in-app/household-only** — resolve, like every `/api/**` route, requires a valid session (§10 D33).

#### FR-P5: Notes tree (read model)
- **Behaviour:** `GET /api/notes/tree` returns the whole folder tree with each folder's child folders and notes as lightweight nodes (`id`, `title`/`name`, `slug`, `position`, `archived`, and for notes a `pinned` summary for the caller) — the navigation model for the sidebar/browser. Household-scale, bounded (one query set, no N+1); archived items excluded unless `?include_archived=true`.

#### FR-P6: Note search
- **Behaviour:** `GET /api/notes?q=<text>` runs an **FTS5** free-text search over note **title + body**, returning matches with their folder path, newest-updated first, capped/paged (mirrors the log browser's FTS5 approach, §10 D6). Reads only.

#### FR-P7: Pin a note — two scopes (§10 D35)
- **Trigger:** a member pins/unpins a note for the Nástěnka widget, choosing **"pro všechny"** (household) or **"jen pro mě"** (personal).
- **Behaviour:** `POST /api/notes/{id}/pin { scope: "household"|"personal" }`; `DELETE /api/notes/{id}/pin?scope=…`. A **household** pin is a shared, **audited** mutation (`editor`/`admin`) — one per note. A **personal** pin is a per-user **view preference** (any member incl. `reader`, **not audited**) — one per (note, user). Pin order within each scope is a lexorank `position`. Household and personal pins are independent; a note can be both (the widget de-dupes — see FR-P8).
- **Errors:** `401`; `403` non-editor setting a **household** pin; `404`. Re-pinning a scope that's already pinned is **idempotent** (`200`, not an error) — the partial unique indexes prevent duplicates.

#### FR-P8: "Připnuté poznámky" widget provider (§10 D27-extended, D35)
- **Provides** widget `notes.pripnute`: the notes visible to the caller in the Nástěnka pin widget = **household pins ∪ the caller's personal pins**, **de-duplicated** (a note pinned both ways shows once; household precedence), each row carrying `id`, `title`, slug path, `scope` (household/personal/both), `updated_at`, and a short plain-text **excerpt** of the body. Ordered household block then personal block, each by pin `position`. **Opening a row shows the note in an overlay dialog on Nástěnka — the user never leaves Nástěnka** (§7); the dialog reuses the module's note view (read, with the WYSIWYG/Markdown edit toggle for `editor`+). Edits/unpin from the overlay call the notes module's own endpoints with `meta.via="dashboard"` (FR-D5). Admin-only: **no**.

### Documents module (`documents`) — new in v4, plus a widget

> Dokumenty: files (PDF, image, Office & other) uploaded into a folder tree, each with a **permanent id-based household-only URL**, in-browser **preview** + **download**, blobs in a dedicated **R2** bucket and metadata in SQLite. A self-contained module (own routes, migrations, audit actions, widget provider) registered like the rest (FR-M1). It reuses Poznámky's folder-tree/slug/resolver pattern (§10 D40) but a document's **bytes are immutable** (§10 D41). All mutations audit through the spine in-transaction; **personal pins are the one exception** (§10 D47). Reads (list/tree/resolve/search and the four content endpoints) are open to any member; writes are `editor`/`admin`.

#### FR-DOC1: Upload a document
- **Trigger:** a member uploads a file.
- **Inputs:** `multipart/form-data` — the `file`, optional `folder_id` (null ⇒ root), optional `title` (defaults to the filename sans extension), optional `description`.
- **Behaviour:** enforce the **size cap** (`HOME_DOCS_MAX_UPLOAD_MB`, 50) and **sniff the MIME** from the leading bytes (never trust the client's `Content-Type`, §10 D48). Stream the bytes to the R2 object `documents/{id}/original`, compute a **SHA-256** checksum, and insert the `documents` row (`content_type`, `byte_size`, `checksum`, `storage_key`, derived `slug`, `preview_status="pending"`) **in one transaction** with the `document.create` audit event. Enqueue preview generation (FR-DOC9). Bytes are **write-once** — there is no replace/overwrite path (§10 D41).
- **Outputs:** `201` the created `Document` (with `preview_status:"pending"`, permanent URLs).
- **Errors:** `401`; `403` non-editor; `404` bad `folder_id`; `409` slug conflict that can't be auto-resolved; `413` over the size cap; `415` a disallowed/blocked type (if an allowlist is configured); `422` malformed upload / empty file; `502` R2 unreachable (nothing is committed — the row is only written after the object lands).

#### FR-DOC2: Document metadata (get / rename / describe / soft-delete) — bytes immutable
- **Behaviour:** `GET /api/documents/{id}` returns `DocumentDetail` (metadata, breadcrumb `path[]`, `slug_path`, `preview_kind`/`preview_status`, permanent URLs, the caller's pin state). `PATCH` changes **only metadata** — `title` (re-derives the slug ⇒ URL changes, D42), `description`, `archived`; **never the file bytes** (D41). Soft-delete by default; `?hard=true` purges the row **and its R2 objects** (original + preview + thumbnail) and is `admin`-gated (§3). Delete requires an explicit **confirmation** in the UI (§7, §10 D50). A document carries `created_by`/`created_at`/`updated_at`.
- **Errors:** `401`; `403`; `404`; `409` unresolvable slug conflict; `422` empty title / bad field.

#### FR-DOC3: Document folders CRUD (single-parent tree)
- **Behaviour:** identical to Poznámky folders (FR-P2) but over the **`document_folders`** table (§10 D40): create under a parent (null ⇒ root), nest arbitrarily deep, `name`→`slug` unique among siblings (folders+documents under the parent), rename updates the slug (URL changes), soft-delete by default, a **non-empty** folder needs `?cascade=true` (else `409` + child count), `?hard=true` purges (`admin`, and purges every descendant document's R2 objects).
- **Errors:** `401`; `403`; `404`; `409`; `422`.

#### FR-DOC4: Move document / folder
- **Behaviour:** `POST /api/documents/{id}/move` (`{ folder_id?, position }`) reparents and/or reorders a document; `POST /api/documents/folders/{id}/move` (`{ parent_id?, position }`) does the same for a folder. Reparenting **re-derives the slug** only if needed to stay unique in the new parent and **changes the item's URL** (the **permanent id-based content URL is unaffected** — D42). **A folder may not move into itself or a descendant** → `422` (cycle guard). Order via lexorank (§10 D4). Moving never touches R2 (the object key is id-based, independent of folder/slug).

#### FR-DOC5: Slug-path URLs, resolver, and the permanent content URL (§10 D42)
- **Behaviour:** the SPA's pretty route is the slug path (`/dokumenty/<folder>/…/<slug>`). `GET /api/documents/resolve?path=<slug-path>` maps a path to `{ type:"folder"|"document", id }` (or `404` if renamed/moved — **no redirects**, D32). All mutating/detail endpoints address items by **stable id**. The **permanent URL** of a document's content is **id-based** — `GET /api/documents/{id}/raw` (and `/download`, `/preview`, `/thumbnail`), fronted by the SPA short route `/d/{id}` — and is **stable for the life of the document** because the id never changes and the bytes never change; the slug path is a convenience that **is not permanent** (rename/move changes it). Every route (incl. `resolve` and the content endpoints) requires a valid session — **household-only, no public path** (§10 D33/D42).

#### FR-DOC6: Documents tree (read model)
- **Behaviour:** `GET /api/documents/tree` returns the whole folder tree with each folder's child folders and documents as lightweight nodes (`id`, `title`, `slug`, `position`, `archived`, `content_type`, `byte_size`, `preview_kind`/`preview_status`, a `thumbnail` URL, and the caller's `pinned` summary) — the navigation model for the sidebar/grid. Household-scale, bounded (one query set, no N+1); archived items excluded unless `?include_archived=true`.

#### FR-DOC7: Document search (filename + metadata)
- **Behaviour:** `GET /api/documents?q=<text>` runs an **FTS5** free-text search over **title + original filename + description** (diacritic-insensitive, `remove_diacritics=2`), returning matches with their folder path, newest-updated first, capped/paged (mirrors the notes/log FTS5 approach). **Does not** look inside file contents (§10 D46). Reads only.

#### FR-DOC8: Serve content — raw / download / preview / thumbnail (§10 D42, D48)
- **Behaviour:** four **read** endpoints stream from R2 through the backend (R2 stays private), all session-gated:
  - `GET /api/documents/{id}/raw` — the original bytes, `Content-Disposition: inline` for safe types else `attachment`, correct sniffed `Content-Type`, **`ETag`= checksum** and `Cache-Control: private, immutable, max-age=31536000` (bytes never change, D41), **HTTP Range** supported (PDF/media seeking).
  - `GET /api/documents/{id}/download` — same bytes, always `Content-Disposition: attachment`.
  - `GET /api/documents/{id}/preview` — the **best previewable representation**: the original for natively-previewable types (`preview_kind="native"`), else the derived preview PDF (`preview_kind="pdf"`). `409` if `preview_status` is `pending`/`failed`, `404`/`204` if `none` (download-only type).
  - `GET /api/documents/{id}/thumbnail` — a small image thumbnail (or `404` if not generated).
- **Isolation (D48):** originals carry `X-Content-Type-Options: nosniff` and a sandboxed CSP; the frontend renders inline previews only through safe viewers (PDF.js in a sandboxed iframe, `<img>`, escaped text). Untrusted active types (HTML/SVG/…) are **download-only**, never rendered in home's origin.

#### FR-DOC9: Preview & thumbnail generation (async, §10 D44)
- **Behaviour:** after the upload transaction commits, a background worker derives previews **once** (bytes immutable ⇒ cache forever): **images** → a scaled thumbnail; **PDF** → a first-page thumbnail; **Office (docx/xlsx/pptx/odt/…)** → a **preview PDF** via headless LibreOffice (`soffice --headless --convert-to pdf`, bounded by `HOME_DOCS_PREVIEW_TIMEOUT_SEC`) **plus** a first-page thumbnail; **text/Markdown** → native (rendered client-side), no derived object. Derived objects are written to `documents/{id}/preview.pdf` and `documents/{id}/thumb.webp`; the row's `preview_kind`/`preview_status`/`preview_key`/`thumbnail_key` are updated and a `/ws` `document.preview_ready` (or `…_failed`) is published. Failure sets `preview_status="failed"` and leaves the document **download-only** — the upload is never lost. Conversion is gated by `HOME_DOCS_PREVIEW_ENABLED`.

#### FR-DOC10: Pin a document — two scopes (§10 D47, mirrors FR-P7)
- **Behaviour:** `POST /api/documents/{id}/pin { scope:"household"|"personal" }`; `DELETE …/pin?scope=`. **household** ("pro všechny") is a shared, **audited** mutation (`editor`/`admin`), one per document, `/ws`-broadcast. **personal** ("jen pro mě") is a per-user **view preference** (any member incl. `reader`, **not audited**), one per (document, user), not broadcast. Pin order per scope is a lexorank `position`; partial unique indexes prevent duplicates; re-pinning is idempotent `200`.
- **Errors:** `401`; `403` non-editor setting a **household** pin; `404`.

#### FR-DOC11: "Připnuté dokumenty" widget provider (§10 D27-extended, D47)
- **Provides** widget `documents.pripnute`: **household pins ∪ the caller's personal pins**, **de-duplicated** (household precedence), each row carrying `document_id`, `title`, slug path, `scope` (household/personal/both), `content_type`, `byte_size`, `preview_kind`/`preview_status`, a `thumbnail` URL, and `updated_at`. Ordered household block then personal block, each by pin `position`. **Opening a row shows the document in a preview overlay on Nástěnka — the user never leaves Nástěnka** (§7); the overlay reuses the module's document view (preview + download; rename/unpin for `editor`+). Overlay actions call the documents endpoints with `meta.via="dashboard"` (FR-D5). **No press-and-hold done gesture** — documents aren't completed. Admin-only: **no**.

### Dashboard host (`dashboard`)

> The host owns **no feature data** — it renders widgets contributed by modules (FR-M2) and stores each user's layout. Nástěnka is the landing route.

#### FR-D1: Widget catalog
- **Behaviour:** `GET /api/dashboard/catalog` lists the widgets available **to this user** — every registered provider whose `admin_only` is false, plus admin-only ones if the user is `admin`. Each entry: `key`, `title`, `module`, `description`, `default_size`. This is the set a user can add to their dashboard.

#### FR-D2: Per-user layout (server-side)
- **Description:** each user's arrangement — which widgets are visible, their order, and their size — persisted server-side and synced across devices (§10 D24).
- **Inputs (`PUT /api/dashboard/layout`):** an ordered list of `{ widget_key, visible, size }`; `size ∈ {narrow, wide}` (§10 D24). 
- **Behaviour:** upsert the caller's `user_dashboard_layout` rows. A first-time user with no rows gets a **default layout** (all non-admin widgets visible, `default_size`, a sensible order). Unknown or now-unavailable widget keys are ignored (a module could be removed). Layout is a **personal preference**, so `reader` may set it (the one write a reader is allowed).
- **Errors:** `401`; `422` bad size/key shape.

#### FR-D3: Dashboard fetch (host fan-out)
- **Description:** one request renders the landing page.
- **Behaviour:** `GET /api/dashboard` reads the caller's layout, then for each **visible** widget calls its provider's `Data(ctx, user)` and returns `{ layout, widgets:[{ key, size, data }] }`. Fan-out is bounded and concurrent where safe; the host never queries module tables directly (§10 D28). A single widget can be refreshed alone via `GET /api/dashboard/widgets/{key}` (used on websocket pushes).
- **Errors:** `401`.

#### FR-D4: Arrange widgets (frontend)
- **Behaviour:** the host UI lets a user **add** a widget from the catalog, **hide/remove** one, **drag to reorder**, and **resize** narrow↔wide; each change persists via FR-D2 (optimistic, with rollback). Empty dashboard (everything hidden) is a valid, deliberate state.

#### FR-D5: Actions inside widgets
- **Behaviour:** mark-done and open-detail inside the Právě dělám / Připomínky widgets reuse the owning module's endpoints and components (card detail, event detail), exactly as v1 — including the **2000 ms press-and-hold** with its mandatory immediate keyboard path (§10 D22). The host adds no parallel completion logic.

### Health

#### FR-H1: `GET /healthz` (liveness), `GET /readyz` (readiness + SQLite ping). Public.

## 5. Data Model

SQLite (embedded), **per-module Goose migrations** run in one sequence (§10 D25). UUIDv7 ids; lexorank string `position` keys (D4); event dates are all-day `DATE`; timestamps UTC, displayed in `HOME_TIMEZONE`.

### platform tables

**sessions** — `id` PK · `user_id` TEXT (auth user id) · `token_hash` TEXT UNIQUE (SHA-256 of the cookie) · `email` · `display_name` · `roles` TEXT (JSON, cached from auth) · `roles_refreshed_at` TIMESTAMP · `user_agent` · `ip` · `created_at` · `last_seen_at` · `expires_at` · `revoked_at` NULL. Index `(token_hash)`, `(user_id)`.

**user_dashboard_layout** — `user_id` TEXT · `widget_key` TEXT · `visible` BOOL DEFAULT true · `position` TEXT (lexorank) · `size` TEXT CHECK in (`narrow`,`wide`) DEFAULT `narrow` · PK `(user_id, widget_key)`. Index `(user_id)`.

### logging tables — `audit_events` (+ `audit_events_fts` FTS5 + triggers), `audit_changes`. Unchanged from v1 (append-only, full diff values). Created first.

### to-do tables — `boards`, `columns` (incl. `(kind)` index), `cards`, `card_links`, `checklist_items`, `labels`, `card_labels`. Unchanged from v1. No `user_column_state` (collapse is client-side, D3).

### events tables — `events`, `event_links`, `event_reminder_completions` (`(event_id, occurrence_on)` unique; the only per-occurrence row). Unchanged from v1.

### notes tables (new in v3)

**folders** — `id` PK (UUIDv7) · `parent_id` TEXT NULL (self-ref; NULL = root) · `name` TEXT · `slug` TEXT · `position` TEXT (lexorank) · `archived` BOOL DEFAULT false · `created_by` · `created_at` · `updated_at`. Cycle-free single-parent tree (FR-P3 guards it). Index `(parent_id)`.

**notes** — `id` PK (UUIDv7) · `folder_id` TEXT NULL (NULL = root; FK→`folders.id`) · `title` TEXT · `slug` TEXT · `body_md` TEXT (**the single canonical Markdown body**, §10 D30) · `position` TEXT (lexorank) · `archived` BOOL DEFAULT false · `created_by` · `created_at` · `updated_at`. Index `(folder_id)`.

- **Slug uniqueness (the addressing invariant, §10 D32):** a slug is unique across the **folders and notes that share one parent**, so a path segment resolves to exactly one item. Enforce with matching partial unique indexes keyed on the parent scope over non-archived rows (root handled with a sentinel/`IS NULL`-aware index) — one for `folders(parent_id, slug)`, one for `notes(folder_id, slug)`, plus an application-level cross-check between the two on write.

**notes_fts** — FTS5 virtual table over note `title` + `body_md` (+ sync triggers), mirroring `audit_events_fts`. Backs FR-P6 search.

**note_pins** — `note_id` TEXT (FK→`notes.id`) · `scope` TEXT CHECK in (`household`,`personal`) · `user_id` TEXT NULL (NULL for household; the auth user id for personal) · `pinned_by` TEXT (actor, for audit) · `position` TEXT (lexorank, per scope/user) · `created_at`. **Partial unique indexes:** `UNIQUE(note_id) WHERE scope='household'` (one household pin per note) and `UNIQUE(note_id, user_id) WHERE scope='personal'` (one personal pin per note per user). Household-pin rows are audited; personal-pin rows are not (§10 D35).

### documents tables (new in v4) — metadata only; bytes live in R2 (§10 D40–D42)

**document_folders** — `id` PK (UUIDv7) · `parent_id` TEXT NULL (self-ref; NULL = root) · `name` TEXT · `slug` TEXT · `position` TEXT (lexorank) · `archived` BOOL DEFAULT false · `created_by` · `created_at` · `updated_at`. Its **own** tree, isolated from `folders` (§10 D40). Index `(parent_id, position)`. Same `COALESCE`-keyed partial unique index over non-archived rows as Poznámky (see below).

**documents** — `id` PK (UUIDv7) · `folder_id` TEXT NULL (NULL = root; FK→`document_folders.id`) · `title` TEXT (display, editable) · `slug` TEXT · `description` TEXT NULL (for search) · `original_filename` TEXT · `content_type` TEXT (**server-sniffed** MIME, §10 D48) · `byte_size` INTEGER · `checksum` TEXT (SHA-256 hex; also the `raw` `ETag`) · `storage_key` TEXT (R2 key of the original, e.g. `documents/{id}/original`) · `preview_kind` TEXT CHECK in (`native`,`pdf`,`none`) · `preview_status` TEXT CHECK in (`pending`,`ready`,`failed`,`none`) DEFAULT `pending` · `preview_key` TEXT NULL (R2 key of the derived preview PDF) · `thumbnail_key` TEXT NULL (R2 key of the thumbnail) · `position` TEXT (lexorank) · `archived` BOOL DEFAULT false · `created_by` · `created_at` · `updated_at`. **Immutable bytes** — no version columns; a re-upload is a new row (§10 D41). Indexes `(folder_id, position)`, `(updated_at)`, `(checksum)`.

- **Slug uniqueness (the addressing invariant, §10 D32/D40):** a slug is unique across the **document_folders and documents that share one parent**. Enforce with matching partial unique indexes over non-archived rows — `ux_docfolders_sibling_slug ON document_folders (COALESCE(parent_id,''), slug) WHERE archived=0` and `ux_documents_sibling_slug ON documents (COALESCE(folder_id,''), slug) WHERE archived=0` — **plus** an application-level cross-check between the two tables on write. (Root handled with the `COALESCE` sentinel exactly as in Poznámky.)

**documents_fts** — FTS5 virtual table over `title` + `original_filename` + `description` (+ sync triggers), `unicode61` tokenizer with `remove_diacritics=2` (mirrors `notes_fts`). Backs FR-DOC7 search. **File contents are not indexed** (§10 D46).

**document_pins** — `document_id` TEXT (FK→`documents.id` CASCADE) · `scope` TEXT CHECK in (`household`,`personal`) · `user_id` TEXT NULL (NULL for household; the auth user id for personal) · `pinned_by` TEXT (actor) · `position` TEXT (lexorank, per scope/user) · `created_at`. **Partial unique indexes:** `UNIQUE(document_id) WHERE scope='household'` and `UNIQUE(document_id, user_id) WHERE scope='personal'`. Household-pin rows are audited; personal-pin rows are not (§10 D47).

**R2 object layout (the documents bucket, `HOME_DOCS_R2_BUCKET`):** per document, `documents/{id}/original` (write-once), `documents/{id}/preview.pdf` (derived, when `preview_kind="pdf"`), `documents/{id}/thumb.webp` (derived). Keys are **id-based** — independent of folder/slug — so renames/moves never touch R2. The bucket is **not** covered by Litestream; it is backed up by **object versioning + a scheduled mirror to `HOME_DOCS_R2_BACKUP_BUCKET`** (§8, §10 D45).

**Migrations:** each module ships its own migration files under its package; the core runs them in one Goose sequence, **logging first**, then platform (sessions, layout), todo, events, **notes**, **documents**. Seed the default board only when `boards` is empty (no notes/folders/documents are seeded). A restored build (Litestream restores the DB; R2 restores the blobs) must not re-seed or double-migrate.

## 6. API Surface

Full detail in `openapi.yaml` (0.5.0). Grouped by module. **The browser carries no bearer token** — `/api/**` is authorized by the **session cookie**; state-changing routes also require the **CSRF header** (FR-A5). Writes require `editor`/`admin`; `/api/logs/**` require `admin`; `/api/auth/*` and health are reachable pre-session.

- **Auth (platform):** `POST /api/auth/login`, `POST /api/auth/logout`, `GET /api/auth/session`.
- **Dashboard host:** `GET /api/dashboard` (layout + widget data, fan-out), `GET /api/dashboard/catalog`, `PUT /api/dashboard/layout`, `GET /api/dashboard/widgets/{key}` (single-widget refresh).
- **To-do:** boards/columns/cards/links/checklist/labels + `/tree` (as v1).
- **Events:** `/api/events` CRUD, `/api/events/occurrences`, links, `/api/events/{id}/complete` (+ undo) (as v1).
- **Notes (Poznámky, new in v3):** `GET /api/notes/tree` (nav read model); `GET /api/notes?q=` (FTS5 search); `GET /api/notes/resolve?path=` (slug-path → id); `POST /api/notes`, `GET|PATCH|DELETE /api/notes/{id}`, `POST /api/notes/{id}/move`, `POST /api/notes/{id}/pin` + `DELETE /api/notes/{id}/pin?scope=`; folders: `POST /api/notes/folders`, `GET|PATCH|DELETE /api/notes/folders/{id}`, `POST /api/notes/folders/{id}/move`.
- **Documents (Dokumenty, new in v4):** `GET /api/documents/tree` (nav read model); `GET /api/documents?q=` (FTS5 filename+metadata search, `?folder_id=`, `?include_archived=`); `GET /api/documents/resolve?path=` (slug-path → id); `POST /api/documents` (**multipart upload**), `GET|PATCH|DELETE /api/documents/{id}` (PATCH = metadata only; DELETE soft, `?hard=true` admin purges R2), `POST /api/documents/{id}/move`, `POST /api/documents/{id}/pin` + `DELETE /api/documents/{id}/pin?scope=`; **content (permanent, household-only, read):** `GET /api/documents/{id}/raw`, `/download`, `/preview`, `/thumbnail`; folders: `POST /api/documents/folders`, `GET|PATCH|DELETE /api/documents/folders/{id}`, `POST /api/documents/folders/{id}/move`.
- **Logs (admin):** `/api/logs`, `/api/logs/{id}`, `/api/logs/entity/{type}/{id}`, `/api/logs/stats` (as v1).
- **Real-time:** `GET /ws` — session-authenticated websocket; pushes board/column/card, event/completion, note/folder/household-pin, **and document/document-folder/household-pin + `document.preview_ready`/`_failed`** changes so open boards, note/document views, and **dashboard widgets (incl. `notes.pripnute` and `documents.pripnute`)** update live. Not modeled in `openapi.yaml`.
- **Health (public):** `/healthz`, `/readyz`.

**Routing:** register static `/api/events/occurrences` before `/api/events/{id}`; `/api/dashboard/catalog` + `/api/dashboard/widgets/{key}` before any parameterised dashboard route; the static `/api/notes/tree`, `/api/notes/resolve`, `/api/notes/folders*`, plus the `q=` search on `GET /api/notes`, before the parameterised `/api/notes/{id}`; and likewise the static `/api/documents/tree`, `/api/documents/resolve`, `/api/documents/folders*` before the parameterised `/api/documents/{id}` (the `{id}/raw|download|preview|thumbnail|move|pin` sub-routes register under it).

## 7. Frontend

React + TS + TanStack Query SPA, **Czech UI** (D20), **dark-default** (D21), organized into **module folders** (`src/modules/{todo,events,logging,dashboard,notes,documents}` + `src/platform`). App shell nav — **Nástěnka · Úkoly · Okno · Poznámky · Dokumenty · Log** (Log admin-only), bottom tabs on mobile / side nav on desktop. **Nástěnka is the landing route.**

- **v3 nav — the fifth destination (§10 D37).** Poznámky is the fifth module. Regular members still see **four** tabs (Nástěnka · Úkoly · Okno · Poznámky — Log is admin-only), so the original four-tab mobile ceiling holds for them. **Admins now have five destinations and exceed that ceiling**, so the mobile app shell needs an **overflow / "více" pattern** (e.g. Log — the least-frequent, admin-only destination — moves behind an overflow affordance) that keeps the daily four thumb-reachable. This is the top item for the **v3 design addendum**.
- **v4 nav — the sixth destination (§10 D49).** Dokumenty is the sixth module. **Now regular members have five destinations** (Nástěnka · Úkoly · Okno · Poznámky · Dokumenty) and **also exceed the four-tab ceiling** that only admins hit in v3 — so the overflow/"více" pattern **generalizes to everyone**, not just the admin Log. The **v4 design addendum** must define the mobile information architecture for **five member destinations (+ Log for admins)**: e.g. a bottom bar of the four most-used plus a "Více" sheet holding the rest, or a five-slot bar with Log/least-used behind overflow. Desktop side-nav simply lists all six.

**Auth (Mode B, home-hosted)**

- **Login screen** on home: email + password → `POST /api/auth/login`. Error states (bad credentials, disabled, MFA-required→link to auth, server error). *No signup, no reset form* — a "Zapomněli jste heslo?" link points to auth-hosted reset; "Nemáte účet? Požádejte správce" for provisioning.
- The SPA calls `GET /api/auth/session` on load: session ⇒ app, else ⇒ login screen. **No token handling in JS.**
- Shared fetch wrapper: sends the session cookie (`credentials:'include'`), attaches `X-CSRF-Token` on mutations; on `401`, routes to the login screen (there is no client-side token refresh — home refreshes roles server-side).
- **Signed-out / redirecting state** and the login screen are new v2 screens — *design addendum pending* (`HANDOFF-design.md` §v2).

**Nástěnka (widget host)**

- Renders the user's visible widgets in the **responsive grid** (§10 D24): one reorderable column on mobile; a 2-column grid on desktop where each widget is **narrow (1 col) or wide (2 col)**.
- **Arrange mode:** add a widget from the catalog, hide/remove, **drag to reorder** (dnd-kit), **resize** narrow↔wide. Each change persists via `PUT /api/dashboard/layout`, optimistic with rollback. Empty dashboard is a deliberate state with an "add a widget" affordance.
- Widget components live in their **owning modules** (`todo` provides Právě dělám; `events` provides Připomínky + Tento měsíc) and are pulled into the host through the frontend widget registry — the host doesn't hardcode them.
- Inside widgets: the **2000 ms press-and-hold** done gesture with its immediate keyboard path (D22); rows open the module's detail dialog.
- *New v2 screens (host, arrange mode, catalog picker, empty state) — design addendum pending.*

**Poznámky (notes, new in v3)**

- **Layout:** desktop is a **folder-tree sidebar + note pane**; mobile is **drill-down** (folder → its contents → note), tree collapsed behind a toggle. Both driven by `GET /api/notes/tree`. Breadcrumb of the current path everywhere.
- **Editor:** **WYSIWYG by default** with a **toggle to raw Markdown** (D30) — both edit the one canonical Markdown body and round-trip; the raw view is the escape hatch. Reuse the app's existing Markdown rendering (already used for card notes / event descriptions) for the read view; the WYSIWYG rich editor is a **new dependency to pick in the v3 design addendum** (must be Markdown-backed, mobile-usable, and not clip Czech diacritics). Save is **last-write-wins** (D38); a `/ws` "změněno jinde" notice warns if the note changed under an open editor.
- **Pinning UI:** a pin control on each note offering the two scopes — **"Připnout pro všechny"** (household) and **"Připnout jen pro mě"** (personal) — with clear current-state indication; `reader` sees only the personal option (D35).
- **Sharing UI:** a **"Kopírovat odkaz"** action yields the item's slug-path URL. Copy must make clear it's a **household link** (opens only for logged-in members — D33), not a public share. A gentle note that renaming/moving changes the link (D32).
- **States:** loading, empty **folder**, empty **root** (no notes yet), no search results, error, and `reader` view-only (no create/edit/move/household-pin affordances; personal pin + read remain).

**Dokumenty (documents, new in v4)**

- **Route** `/dokumenty/*` = slug paths; resolve path→id on navigation, then work by id. The **permanent share link** copied for a document is the **id-based** `/d/{id}` (not the slug path — D42).
- **Layout:** desktop **folder-tree sidebar + a documents pane** (grid of thumbnail cards or a detail list, toggleable); mobile **drill-down** (folder → contents → document). Breadcrumb everywhere. Driven by `['documents','tree']`. Each card shows a **thumbnail** (or type icon), title, type, size, and a `preview_status` indicator (a "Náhled se připravuje…" state while `pending`).
- **Upload:** a prominent **"Nahrát dokument"** action + **drag-and-drop** onto a folder; a client-side size/type pre-check before `POST /api/documents`; an upload progress state; on success the document appears with `preview_status:"pending"` and updates to a thumbnail via `/ws` when ready.
- **Viewer / preview:** a **DocumentView** that renders the preview inline through safe viewers only (§10 D48) — **PDF.js in a sandboxed iframe** for PDFs and Office→PDF, `<img>` for images, escaped text for text/Markdown — with a **Stáhnout** (download) button always present; download-only types show a type card + Stáhnout. Build **DocumentView as a standalone component** — the dashboard overlay (FR-DOC11) reuses it verbatim.
- **Organise:** create/rename folders; rename/describe documents; move via a **"Přesunout do…"** picker (reuse Úkoly/Poznámky's pattern) + desktop drag (dnd-kit, keyboard alternative); non-empty folder delete shows the **cascade warning**; **every delete requires an explicit confirmation** (§10 D50) — the copy notes that a hard-delete also removes the file from R2.
- **Pin control:** two scopes ("Připnout pro všechny" / "Připnout jen pro mě") with state; `reader` sees only personal.
- **"Kopírovat odkaz":** copies the **permanent** `/d/{id}` link; UI communicates **household-only, not public** (D33/D42) — this link works for logged-in members only.
- **Query keys:** `['documents','tree']`, `['documents','detail',id]`, `['documents','resolve',path]`, `['documents','search',q]`, `['dashboard','widget','documents.pripnute']`. A document/folder/move mutation invalidates `['documents','tree']`; a **household** pin also invalidates `['dashboard']` + the `documents.pripnute` widget. Content endpoints (`raw`/`preview`/`thumbnail`) are addressed as URLs (`<img src>`, iframe, download), not query-cached.
- **States:** loading, empty root, empty folder, no-results, **preview pending / preview failed (download-only)**, upload in-progress / upload error (too large / blocked type / R2 error), error, and `reader` view-only (no upload/edit/move/household-pin; personal pin + preview + download remain).
- **Accessibility:** keyboard-operable tree, move, and pin; touch targets ≥44 px; `prefers-reduced-motion` on tree/overlay transitions; the preview iframe and thumbnails carry meaningful labels.

**Úkoly / Okno / Log** — as v1 (board kanban+accordion; month list + event form with series-edit warning; log browser with filters/diffs/timeline/analytics), now living in their module folders.

**Data fetching (TanStack Query):** keys `['auth','session']`, `['dashboard']`, `['dashboard','catalog']`, `['dashboard','widget',key]`, plus the v1 module keys, the v3 notes keys `['notes','tree']`, `['notes','detail',id]`, `['notes','resolve',path]`, `['notes','search',q]`, `['dashboard','widget','notes.pripnute']`, and the v4 documents keys `['documents','tree']`, `['documents','detail',id]`, `['documents','resolve',path]`, `['documents','search',q]`, `['dashboard','widget','documents.pripnute']`. Note/document, folder, move, and pin mutations invalidate the owning module's `tree` and, for a household pin, `['dashboard']` (+ that module's pin widget). Websocket pushes — incl. `document.preview_ready`/`_failed` — refresh affected widgets, boards, and open note/document views. Explicit empty/loading/error/`reader` states everywhere.

## 8. Non-Functional Requirements

- **Observability (baseline):** `/healthz`, `/readyz` (SQLite ping), structured JSON logs to stdout, per-request log with request id stamped onto audit events.
- **Audit completeness:** in-transaction with the mutation; append-only; every module (incl. login/logout) writes through the spine.
- **Mode B security (§10 D23):** home receives plaintext passwords on `/api/auth/login` — **TLS only, never logged/persisted, discarded immediately**. Home owns session + revocation; a revoked auth user keeps working ≤ `HOME_ROLE_REFRESH_MINUTES`. Session cookie `HttpOnly; Secure; SameSite=Lax`, host-only, hashed at rest, sliding TTL. **CSRF** double-submit + Origin allowlist on cookie-authenticated mutations. Login rate-limited (per email + per IP). Service-client secret (`X-Service-Secret`) high-entropy, in Coolify env only. **No bearer token in the browser.**
- **Module isolation (§10 D25/D28):** no module imports another module's package internals; cross-module data flows only through the widget-provider contract (and the audit sink). Enforce with an architecture test / import-lint so a boundary violation fails CI.
- **Bounded computation:** occurrence expansion and every widget provider are window-bounded and capped; the dashboard fan-out (FR-D3) issues a bounded number of queries with no N+1 across events — it's the landing route. The **notes tree** (FR-P5) loads in one bounded query set (no N+1 over folders), and **note search** (FR-P6) is FTS5-backed, capped, and paged. The **documents tree** (FR-DOC6) and **document search** (FR-DOC7) are held to the same bar (one bounded query set, FTS5-backed, capped/paged); content endpoints stream from R2 with **Range** support and never buffer a whole 50 MB file in memory.
- **Date correctness:** all-day events avoid clock DST; "today"/month/lead math in `Europe/Prague`; short-month clamping unit-tested (D19).
- **Performance:** household scale. p95 < 50 ms for board-tree, dashboard fan-out, and indexed log queries.
- **Backup — DB (unchanged):** Litestream → R2 prefix `home/`; fresh build restores the SQLite DB before serving; seed only if empty after restore.
- **Backup — documents bucket (new in v4, §10 D45):** **Litestream cannot back up the R2 blob bucket** — it only replicates the SQLite WAL. So the `documents` **metadata** is covered by Litestream like everything else, but the **files** get their own strategy: (a) **object versioning** enabled on the primary documents bucket (recovers accidental deletes/hard-deletes — bytes themselves are immutable, D41, so there are no overwrites to recover); and (b) a **scheduled server-side mirror** of the primary bucket into a **separate backup R2 bucket** (`HOME_DOCS_R2_BACKUP_BUCKET`) via an internal job (S3-compatible copy / `rclone`-style sync). Metadata↔blob consistency: a document row is the source of truth for "which objects should exist"; a periodic reconciliation flags orphaned objects (row deleted) and dangling rows (object missing). A **fresh build** restores metadata from Litestream and reads bytes from R2 (primary, or the mirror if the primary is lost).
- **Document previews (new in v4, §10 D44):** Office→PDF conversion (headless LibreOffice) and thumbnailing run **out of the request path**, bounded by `HOME_DOCS_PREVIEW_TIMEOUT_SEC`, generated **once** per document (immutable bytes) and cached in R2 forever; a failed conversion degrades to download-only and never blocks or loses the upload.
- **Untrusted-content isolation (new in v4, §10 D48):** user files are served with a **server-sniffed** `Content-Type`, `X-Content-Type-Options: nosniff`, a sandboxed CSP, and `Content-Disposition: attachment` for anything not rendered through a safe viewer; content endpoints require the session like every route. The immutable `raw` responses carry a checksum `ETag` and long-lived `immutable` cache headers. (Hardening option deferred: serve blobs from a separate cookieless subdomain via short-lived signed links.)
- **Upload limits (new in v4):** enforce `HOME_DOCS_MAX_UPLOAD_MB` (50) server-side; reject over-cap with `413` and blocked types with `415`; the write to the DB happens only **after** the object is durably in R2 (no half-committed documents).

## 9. Configuration

Env (Coolify only; nothing secret in the repo):

- `HOME_DB_PATH` · `HOME_SITE_KEY` (default `home`) · `AUTH_BASE_URL` (`https://auth.tilcer.cz` — `/internal/login`, `/internal/token/mint`, and the target of "reset password" / MFA-fallback links).
- `HOME_AUTH_SERVICE_SECRET` — auth **service-client** secret bound to site `home`; authenticates `/internal/login` **and** `/internal/token/mint`. *(New role vs v1, where it only gated introspect.)*
- `HOME_SESSION_TTL_DAYS` — home session sliding window (default 90).
- `HOME_ROLE_REFRESH_MINUTES` — how often home re-mints to refresh cached roles (default 15).
- `HOME_ALLOWED_ORIGINS` — CORS/CSRF Origin allowlist (`https://*.tilcer.cz`).
- `HOME_TIMEZONE` (`Europe/Prague`) · `HOME_DASHBOARD_LOOKBACK_DAYS` (30) · `HOME_RRULE_MAX_OCCURRENCES` (500) · `HOME_LOG_RETENTION_DAYS` (0 = keep forever).
- `LITESTREAM_*` / R2 credentials — prefix `home/` (the **DB** replica).
- **Documents / R2 (new in v4):** `HOME_DOCS_R2_BUCKET`, `HOME_DOCS_R2_ENDPOINT`, `HOME_DOCS_R2_ACCESS_KEY_ID`, `HOME_DOCS_R2_SECRET_ACCESS_KEY` — the **primary documents bucket** (separate from the Litestream DB replica). `HOME_DOCS_R2_BACKUP_BUCKET` (+ its endpoint/keys if a distinct account) — the **mirror** target (§8, D45). `HOME_DOCS_MIRROR_CRON` — mirror schedule (default hourly). `HOME_DOCS_MAX_UPLOAD_MB` (default 50). `HOME_DOCS_ALLOWED_MIME` — optional allowlist (empty = allow all, still sniffed). `HOME_DOCS_PREVIEW_ENABLED` (default true), `HOME_DOCS_SOFFICE_PATH` (headless LibreOffice binary), `HOME_DOCS_PREVIEW_TIMEOUT_SEC` (default 60). `HOME_DOCS_PUBLIC_BASE_URL` — base for the permanent `/d/{id}` links (defaults to the app origin).

**Prerequisite (Karel, before build):** register site `home` in auth with roles `admin`/`editor`/`reader`; **provision a `home` service client bound to site `home`** and put its secret in `HOME_AUTH_SERVICE_SECRET`; create the household member accounts in auth (no self-signup). **v4:** create the **primary + backup R2 buckets** for documents, **enable object versioning** on the primary, and put their credentials in the `HOME_DOCS_R2_*` env vars; ensure the runtime image includes **headless LibreOffice** (for Office→PDF previews).

## 10. Resolved Decisions

D1–D22 are from v1 (`CHANGELOG.md`). D23–D29 are v2; D2 and D16 are adjusted. D30–D38 are v3 (Poznámky); D6 is extended. **D39–D50 are v4 (Dokumenty); D33 is reaffirmed and D6 extended again.**

- **D1** multiple boards + switcher · **D3** collapse client-side · **D4** lexorank ordering · **D5** roles `admin`/`editor`/`reader` · **D6 (extended v3)** full diffs + FTS5, key diff entities incl. `event` — and now `note` and `folder` (§10 D36) · **D7** `now`/`done` free-form hint · **D8** soft delete + `?hard=true` · **D9** no scheduler · **D10** websockets (board + event/completion) · **D11** reminders computed on read · **D12** reminders a separate entity · **D13** RRULE subset via `teambition/rrule-go` · **D14** series-only editing · **D15** dashboard mark-done → first `kind=done` column, else archive · **D17** English code ids, Czech UI · **D18** date-only events · **D19** short-month clamping (deliberate RFC deviation) · **D20** Czech-only UI + plural forms · **D21** dark default · **D22** Nástěnka done = 2000 ms press-and-hold with an immediate keyboard path.
- **D2 (adjusted) — token verification:** in Mode B the browser holds **no** JWT; home authorizes from **its own session** and refreshes roles via `/internal/token/mint`. Per-request `/introspect` is dropped from the hot path. The signing secret is still never distributed.
- **D16 (adjusted) — dashboard scope:** widgets define their own scope; *Tento měsíc* legitimately looks ahead. Nástěnka is still the landing route, but "active items only" no longer applies globally.
- **D23 — Auth is Mode B, login + logout only, password-only.** Home hosts login, owns its session, calls auth `/internal/login` + `/internal/token/mint` as a service client. No self-signup (admin-provisioned in auth), no reset UI (auth-hosted), no TOTP/Google on home (auth-hosted; MFA challenge → graceful redirect message). Accepts the Mode B trade-offs (plaintext passwords in transit, own revocation).
- **D24 — Nástěnka is a per-user widget host.** Server-side layout (show/hide, drag-reorder, **narrow/wide** resize), synced across devices. Responsive grid: one column mobile, two-column desktop.
- **D25 — Compile-time modular monolith.** Each module self-contained (routes, **own migrations**, audit actions, widget providers) in `backend/` and `frontend/`, wired via a central registry. One binary; no runtime plugins.
- **D26 — Module code identifiers stay English** (`logging`, `todo`, `events`, `dashboard`, `platform`); UI is Czech. (Extends D17.)
- **D27 — v1 widget catalog:** `todo.pravedelam` (Právě dělám), `events.pripominky` (Připomínky), `events.tento-mesic` (Tento měsíc). No admin log widget in v1. No user-authored widgets.
- **D28 — Dashboard host owns no feature data.** Cross-module data reaches it only through the **widget-provider contract** (FR-M2); the host never queries `todo`/`events` tables. Boundary enforced by an import/arch test.
- **D29 — Home owns a session + CSRF.** Own session store (hashed token, sliding `HOME_SESSION_TTL_DAYS`), `HttpOnly; Secure; SameSite=Lax` host-only cookie, CSRF double-submit + Origin allowlist on cookie-authenticated mutations, login rate-limited. No token in the browser.

**v3 — Poznámky (`notes`) module (D30–D38).** A self-contained fifth module registered like the rest; no change to auth, the dashboard-host contract, or todo/events/logging.

- **D30 — Markdown is the single canonical body.** A note's body is stored once, as Markdown. The editor offers **WYSIWYG (default)** and **raw Markdown** as two views over that one source (round-trip); no HTML and no second representation is persisted.
- **D31 — Single-parent folder tree, arbitrary depth.** Folders nest arbitrarily; a note lives at the **root or in exactly one folder** (no multi-filing). Siblings order by lexorank.
- **D32 — Human-readable slug-path URLs; no redirects.** Every folder and note is addressable at `/poznamky/<folder>/…/<slug>`; a slug is **unique across the sibling folders + notes under one parent**, so a path resolves to exactly one item. Canonical operations are by **stable id**; a resolver maps path→id. **Rename or move changes the URL**, and v1 keeps **no redirects** — an old path 404s. Accepted tradeoff for readable links (over a stable-opaque-id or slug+id scheme).
- **D33 — Sharing is in-app / household-only.** A URL "shares" a note/folder only to **logged-in home members**; there is **no public / unauthenticated access, no share tokens, no public routes.** Poznámky stays inside Mode B's gate.
- **D34 — Text + external links only; no uploads.** No file/image uploads and no embedded attachments; **no blob storage is added.** Markdown may reference an external image URL, but nothing is stored server-side.
- **D35 — Two pin scopes ("pro všechny" / "jen pro mě").** A note can be pinned **household** (shared, **audited**, `editor`/`admin`, one per note) or **personal** (a per-user **view preference**, any member incl. `reader`, **not audited**, one per note per user). The `notes.pripnute` widget shows household pins **∪** the caller's personal pins, de-duplicated (household precedence).
- **D36 — Notes join the audit spine's key diff entities.** `note` and `folder` are added to D6's full-value diff set; long note-body diffs use the existing truncate-with-expand pattern. **The audit log is the note's history — no separate version-history feature in v3.**
- **D37 — Nav grows to a fifth destination.** Regular members still see four tabs (Log is admin-only), so the four-tab mobile ceiling holds for them; **admins exceed it** and the app shell needs an **overflow / "více"** pattern for the least-frequent (admin-only Log) destination. A v3 design-addendum item.
- **D38 — Note bodies are last-write-wins.** No collaborative/simultaneous editing; a `/ws` "changed elsewhere" notice softens a concurrent overwrite, but there is no OT/CRDT and no per-field merge.

**v4 — Dokumenty (`documents`) module (D39–D50).** A self-contained sixth module registered like the rest; no change to auth, the dashboard-host contract, or the other five modules. First module with **blob storage**.

- **D39 — Dokumenty is a self-contained sixth module.** Own routes, migrations, audit actions, and one widget provider, wired through the core registry (FR-M1); no cross-module imports. Migration order extends the one Goose sequence: logging → platform → todo → events → notes → **documents**. Reads open to all members; writes `editor`/`admin`, with the personal-pin exception (D47).
- **D40 — Its own folder tree, isolated from Poznámky.** `documents` ships `document_folders` + `documents` tables and its own resolver — a Dokumenty folder is **not** a Poznámky folder (module isolation, D25/D28). It **reuses the pattern** (single-parent, arbitrary depth, lexorank, slug paths, cross-table sibling-slug invariant with `COALESCE` partial indexes, cycle-guarded moves, no redirects — D31/D32) over separate data.
- **D41 — Blobs in a dedicated R2 bucket; content is immutable (upload-once).** Each document's bytes are written **once** to R2 and **never replaced or overwritten** (Karel's explicit choice) — to change content you upload a **new** document; the old one can be deleted after confirmation (D50). There is no version history and no replace endpoint. SQLite stores only metadata; R2 stores the bytes. Immutability makes the permanent content URL trivially cacheable (`immutable`, checksum `ETag`).
- **D42 — Permanent URL = stable, id-based, household-only.** A document's permanent link is derived from its **immutable UUID** (`/d/{id}` → `/api/documents/{id}/raw|download|preview|thumbnail`), **not** its slug path (which changes on rename/move, D32). The backend serves it, gated by home's session — **no public access, no share tokens** (D33 upheld). The slug path (`/dokumenty/…`) remains for navigation but is explicitly **non-permanent**.
- **D43 — Uploads go through the backend.** Multipart `POST /api/documents`; the Go backend validates size/MIME, streams to R2, checksums (SHA-256), and records metadata + `document.create` audit in one transaction. R2 stays private; the browser never gets a presigned URL on the permanent path. Cap 50 MB (`HOME_DOCS_MAX_UPLOAD_MB`). (Presigned direct-to-R2 was considered and declined for v4 — through-backend keeps validation, preview generation, audit, and privacy in one place at household scale.)
- **D44 — Previews: native for PDF/image/text, Office server-converted to PDF; generated once, async.** PDFs, images, and text/Markdown preview directly; docx/xlsx/pptx/odt/… convert to a **derived preview PDF** via headless LibreOffice, produced **once** after upload (immutable ⇒ derive-once, cache-forever) and stored as a second R2 object; thumbnails likewise. Conversion is **out-of-band** — the document is created with `preview_status="pending"`, then flips to `ready`/`failed`/`none` and pushes `/ws`. A failed conversion degrades to download-only; the upload is never lost. Gated by `HOME_DOCS_PREVIEW_ENABLED`.
- **D45 — Backup: object versioning + a mirror bucket; Litestream is DB-only.** Litestream cannot back up the R2 blob bucket (it replicates the SQLite WAL). The `documents` **metadata** rides Litestream (`home/`) like everything else; the **bucket** is protected by **object versioning** (recovers deletes) **and a scheduled server-side mirror to `HOME_DOCS_R2_BACKUP_BUCKET`**. A periodic reconciliation flags orphaned objects / dangling rows. A fresh build restores metadata from Litestream and reads bytes from R2 (primary, or mirror on loss). *(This is the direct answer to the open architecture question: not Litestream — mirror + versioning.)*
- **D46 — Search is filename + metadata, not file contents.** `documents_fts` (FTS5, diacritic-insensitive) over title + original filename + description. No in-file text extraction or OCR in v4 — a candidate future addition (the derived preview PDFs would be the extraction source).
- **D47 — Two pin scopes + `documents.pripnute` widget (mirrors D35/D27).** `document_pins`: **household** ("pro všechny" — shared, audited, `editor`/`admin`, one per document, `/ws`-broadcast) and **personal** ("jen pro mě" — per-user view preference, any member incl. `reader`, not audited, one per document per user, not broadcast). The widget shows household ∪ the caller's personal pins, de-duplicated (household precedence); a row opens a **preview overlay on Nástěnka** without navigating away; no done gesture.
- **D48 — Untrusted-content isolation.** Because arbitrary user files are served from home's own origin, MIME is **sniffed server-side** (never trusted from the client); originals default to `Content-Disposition: attachment` + `X-Content-Type-Options: nosniff` + a sandboxed CSP; inline previews are limited to safe viewers (PDF.js in a sandboxed iframe, `<img>`, escaped text); active types (HTML/SVG/…) are download-only. Hardening option deferred: a separate cookieless content subdomain with short-lived signed links.
- **D49 — Nav grows to a sixth destination; the overflow pattern generalizes.** With Dokumenty, **regular members now have five destinations** and also exceed the four-tab mobile ceiling that only admins hit in v3 (D37). The mobile app shell's overflow/"více" pattern applies to everyone; the **v4 design addendum** defines the mobile IA for five member destinations (+ Log for admins). Desktop side-nav lists all six.
- **D50 — Delete requires explicit confirmation; soft by default, hard is admin and purges R2.** Soft-delete (archive) is the default and needs a confirmation step in the UI; a non-empty folder needs `?cascade=true`; `?hard=true` purges the metadata row **and the R2 objects** (original + derived) and is `admin`-gated per the app-wide destructive-op rule. `document` and `document_folder` join D6's key-diff set (metadata diffs only — bytes are immutable, never diffed).

## 11. Acceptance Criteria

- [ ] PRD v4 + `openapi.yaml` 0.5.0 reviewed and approved; decisions D1–D50 locked.
- [ ] Site `home` registered in auth (roles `admin`/`editor`/`reader`); a **service client bound to site `home`** provisioned; members created in auth.
- [ ] **Login (Mode B):** home form → `/internal/login`; a home session cookie is set; the browser holds **no** JWT; bad credentials → `401`, disabled/no-access → `403`, MFA demand → graceful `409` + link to auth.
- [ ] **Session authz + role refresh:** requests authorize from the session; roles re-mint via `/internal/token/mint` past `HOME_ROLE_REFRESH_MINUTES`; a mint failing closed drops the session within one interval. Passwords never logged/persisted.
- [ ] **CSRF:** cookie-authenticated mutations require a matching `X-CSRF-Token` + allowed Origin; login is exempt but rate-limited. Logout revokes the session.
- [ ] **Module boundaries:** each module self-contained (routes, own migrations, audit, widgets) in `backend/` and `frontend/`; an **import/arch test fails** if one module imports another's internals; the dashboard host never touches `todo`/`events` tables.
- [ ] **Per-module migrations** run in one Goose sequence (logging → platform → todo → events); seed one board only when empty; restore doesn't double-run.
- [ ] **Widget host:** `GET /api/dashboard` returns `{layout, widgets[]}` by fanning out to visible providers; `GET /api/dashboard/catalog` lists user-available widgets; `PUT /api/dashboard/layout` persists show/hide + order + narrow/wide, synced across devices; unknown widget keys ignored; first-run default layout applied.
- [ ] **Widgets:** Právě dělám (cards in `kind=now` across all boards), Připomínky (active reminders, overdue first, one per event), Tento měsíc (look-ahead, read-only) — each provided by its module through the widget contract; the host imports none of their tables.
- [ ] **Widget actions:** mark-done reuses `POST /api/cards/{id}/move` and `POST /api/events/{id}/complete` with `meta.via="dashboard"`; the **2000 ms hold** commits with an **immediate keyboard path**; `reader` sees no done control but **can** arrange their own layout.
- [ ] **Arrange UX:** add from catalog, hide/remove, drag-reorder, resize narrow↔wide; optimistic with rollback; empty dashboard is a deliberate state; verified at 375 px and 1440 px, both themes.
- [ ] **Poznámky — notes & folders:** create/edit/view/delete notes and folders; a note lives at root or one folder; arbitrary folder depth; move reparents+reorders; **moving a folder into its own descendant is rejected** (cycle guard); soft delete by default, non-empty folder needs cascade, hard-delete is `admin`.
- [ ] **Poznámky — Markdown & editor:** body stored once as Markdown; **WYSIWYG default** with a raw-Markdown toggle that round-trips; no HTML/second copy persisted; save is last-write-wins with a `/ws` "changed elsewhere" notice; Czech diacritics not clipped in the editor.
- [ ] **Poznámky — URLs:** every note/folder resolves at its slug path via `GET /api/notes/resolve`; slugs unique across sibling folders+notes; **rename/move changes the URL and the old path 404s** (no redirects); the path opens only for a logged-in member (**no public access**).
- [ ] **Poznámky — search & tree:** `GET /api/notes/tree` returns the nav model in one bounded query set; `GET /api/notes?q=` is FTS5-backed, capped, paged.
- [ ] **Poznámky — pinning:** household pin ("pro všechny") is `editor`+ and **audited**; personal pin ("jen pro mě") is settable by any member incl. `reader` and **not audited**; one household pin per note, one personal pin per note per user.
- [ ] **Poznámky — widget:** `notes.pripnute` shows household ∪ the caller's personal pins, de-duplicated (household precedence); a row **opens the note in an overlay dialog on Nástěnka without navigating away**; edits/unpin from the overlay hit the notes endpoints with `meta.via="dashboard"`.
- [ ] **Poznámky — audit & isolation:** `note`/`folder` mutations audit in-transaction with full diffs (D6-extended); the module is self-contained (routes, own migrations, audit, the one widget) and reaches the dashboard only through the widget-provider contract; the **import/arch test** covers it.
- [ ] **Poznámky — nav:** the fifth destination is added; regular members keep four thumb-reachable tabs; the **admin's 5th (Log) uses an overflow pattern** on mobile; verified at 375 px and 1440 px, both themes.
- [ ] **Dokumenty — upload & storage:** multipart upload streams to R2 and writes the metadata row + `document.create` audit **in one transaction, only after the object is durable**; size cap enforced (`413`) and MIME **sniffed server-side**; a document's bytes are **immutable — there is no replace/overwrite path** (re-upload is a new document).
- [ ] **Dokumenty — folders & move:** create/rename/move/delete folders and documents; a document lives at root or one folder; arbitrary depth; **moving a folder into its own descendant is rejected** (cycle guard); moving/renaming never touches R2 (id-based keys); soft-delete by default, non-empty folder needs cascade, **hard-delete is `admin` and purges the R2 objects**; delete requires explicit confirmation.
- [ ] **Dokumenty — permanent URL:** `/d/{id}` → `/api/documents/{id}/raw` is **stable for the document's life** (id + bytes immutable) and **household-only** (session-gated, no public route); slug path resolves via `GET /api/documents/resolve` and **404s after rename/move** (no redirects).
- [ ] **Dokumenty — preview & download:** PDFs/images/text preview natively; **Office converts to a preview PDF via headless LibreOffice**, generated **once** async with `preview_status` transitions and a `/ws` push; a failed conversion degrades to **download-only** without losing the upload; `raw` supports **Range** + checksum `ETag` + `immutable` caching; downloads always attach; untrusted types are download-only with `nosniff` + sandboxed CSP.
- [ ] **Dokumenty — search & tree:** `GET /api/documents/tree` returns the nav model in one bounded query set (no N+1); `GET /api/documents?q=` is FTS5-backed over **title + filename + description** (diacritic-insensitive), capped/paged; **file contents are not indexed**.
- [ ] **Dokumenty — pinning & widget:** household pin is `editor`+ and audited, personal pin is any member (incl. `reader`) and not audited (partial unique indexes enforce one-per-scope); `documents.pripnute` shows household ∪ personal, de-duplicated (household precedence); a row **opens a preview overlay on Nástěnka without navigating away**; overlay actions carry `meta.via="dashboard"`; **no done gesture**.
- [ ] **Dokumenty — backup:** documents **metadata** rides Litestream (`home/`); the **bucket** has **object versioning on** and a **scheduled mirror to the backup bucket**; a fresh build restores metadata from Litestream and reads bytes from R2; reconciliation flags orphaned objects / dangling rows.
- [ ] **Dokumenty — audit & isolation:** `document`/`document_folder` mutations (except personal pins) audit in-transaction with metadata diffs (D6-extended; bytes never diffed); the module is self-contained (routes, own migrations, audit, the one widget) and reaches the dashboard only through the widget-provider contract; the **import/arch test** covers it.
- [ ] **Dokumenty — nav:** the sixth destination is added; the **mobile overflow/"více" pattern now covers regular members' five destinations (+ Log for admins)** per the v4 design addendum; desktop side-nav lists all six; verified at 375 px and 1440 px, both themes.
- [ ] Logging spine, to-do, and events behave per v1 (their acceptance items carry over) — every mutation audited in-transaction; recurrence/clamping/reminder tests pass.
- [ ] `/ws` refreshes affected widgets and boards live across devices.
- [ ] Czech UI + plural forms; dark default. Baseline observability; Litestream→R2 (`home/`) DB restore verified, **and the documents bucket versioning + mirror-to-backup-bucket verified**.
- [ ] `REGISTRY.md` updated (repo, status).

## 12. Changelog

Full detail in `CHANGELOG.md`.

- **v4 (2026-08-11)** — adds the **Dokumenty** (`documents`) module: file storage (PDF/image/Office/other) in a single-parent folder tree, blobs in a dedicated **Cloudflare R2** bucket with metadata in SQLite, **immutable upload-once** content, a **permanent id-based household-only URL** (`/d/{id}`), in-browser **preview** (native for PDF/image/text, Office→PDF via headless LibreOffice, async) + **download**, FTS5 filename/metadata search, two-scope pinning, and the `documents.pripnute` Nástěnka widget (preview overlay, no navigation away). New `document_folders`, `documents`, `documents_fts`, `document_pins` tables + `/api/documents` endpoints incl. `raw`/`download`/`preview`/`thumbnail`; `document`/`document_folder` join the audit diff set; nav grows to a 6th destination (mobile overflow now covers everyone). **Bucket backup = object versioning + mirror to a second R2 bucket** (Litestream stays DB-only). OpenAPI → 0.5.0. Decisions D39–D50; D6 extended, D33 reaffirmed. Design v4 addendum pending.
- **v3 (2026-07-29)** — adds the **Poznámky** (`notes`) module: Markdown notes (single canonical body; WYSIWYG-default + raw toggle) in a single-parent folder tree, human-readable slug-path URLs (in-app/household-only, no redirects), FTS5 search, two-scope pinning ("pro všechny"/"jen pro mě"), and the `notes.pripnute` Nástěnka widget (overlay dialog, no navigation away). New `folders`, `notes`, `notes_fts`, `note_pins` tables + notes endpoints; `note`/`folder` join the audit diff set; nav grows to a 5th destination (admin overflow). Text + external links only — no uploads/blob storage. OpenAPI → 0.4.0. Decisions D30–D38; D6 extended. Design v3 addendum pending.
- **v2 (2026-07-21)** — Mode B self-hosted login + own session (no browser token); Nástěnka as a per-user widget host (server-side layout, three module-provided widgets); compile-time modular monolith with per-module packaging and migrations; new `sessions` + `user_dashboard_layout` tables; auth/session + dashboard-host endpoints; OpenAPI → 0.3.0. Decisions D23–D29; D2/D16 adjusted.
- **v1 (2026-07-21)** — Initial four-module spec (logging, todo, events, dashboard), Mode A auth, hardcoded two-list Nástěnka, single `0001_init`. Decisions D1–D22. Design prototype delivered and reviewed. Never implemented.
