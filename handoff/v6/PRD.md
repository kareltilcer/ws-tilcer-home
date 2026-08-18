# PRD — Home

> Status: **v5 — LIVE** (deployed 2026-08-17) · **v6 — SPEC DRAFT** (2026-08-17, awaiting Karel's review). v6 adds an eighth module, **Finance** (`finance`) — absorbing the standalone `fin` service (`fin.tilcer.cz`), migrating its data and **retiring it**. v5 added the **Administrace** (`admin`) module (Web Push notifications) and an installable, reads-only-offline **PWA** on top of v4 (Dokumenty). Self-hosted login (Mode B), widget dashboard and modular architecture unchanged throughout. Decisions **D1–D98** (§10 for D1–D50; the **v5 section** for D51–D74; **§V5-12 for D75–D80 — shipped after the spec froze**; the **v6 section** for D81–D98); v-deltas in `CHANGELOG.md`. **Seven modules live, an eighth specified.** · Owner: Karel · Last updated: 2026-08-17
> Companion spec: `openapi.yaml` (OpenAPI 3.1 — **0.7.0 = the deployed build**, **0.8.0 = the v6 spec**) · Notes: `notes.md` · Design brief: `HANDOFF-design.md` (v2–**v6** addenda; the v6 section is a **draft pending Karel's approval**) · Build: `HANDOFF-1…8-*.md` (v5 = `HANDOFF-7-admin.md`, v6 = `HANDOFF-8-finance.md`)

> **Built-app reconciliation (2026-08-16, surfaced by the v5 design review).** The shipped app has drifted from this written spec in a few v3/v4 areas; the drift is recorded here and the affected decisions are marked inline. (1) **Notes now support image upload to storage — this SUPERSEDES D34** (originally "text + external links only; no uploads; no blob storage"). External links still work, but images inserted into a note are stored server-side. *(Confirmed from code 2026-08-17: bytes go through `platform/blobstore` at `note-images/{id}`, `body_md` keeps only `![](/api/notes/images/{id})`, and `notes/mirror.go` runs its own mirror + reconciliation job — a deliberate near-twin of `documents`', not a shared abstraction.)* (2) **Folders in Poznámky and Dokumenty carry an emoji icon** (max 8 chars) chosen via a picker with search — a folder-model extension over D31/D40. (3) The **note editor has three modes — Číst · Vizuální · Markdown** (D30 described only WYSIWYG + raw-Markdown) — with **Ukládám… / Uloženo** states, **draft recovery**, and a **"změněno jinde"** concurrent-edit notice (the UI surfacing D38 last-write-wins). (4) **Dokumenty** added states: large text files are **download-only**, PDFs offer **"Otevřít v novém okně"**, and a non-empty / archived-item folder delete is refused with **409**. (5) Live changes raise **per-module toasts** ("Úkoly byly mezitím upraveny", …). (6) **Office→PDF preview runs in a `home-gotenberg` SIDECAR**, not headless LibreOffice inside the backend image (`HANDOFF-6-documents.md` §16) — agreed with Karel: it keeps the backend image at ~100 MB instead of ~1 GB and isolates the converter from the app process; thumbnails come from `pdftoppm` + `cwebp`. **Note:** `notes.md` carried the superseded D34 wording until 2026-08-17, now corrected; `HANDOFF-5-notes.md` never restated it.

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
- **Text + external links only** (§10 D34). No uploads and no embedded attachments; **no blob storage is added.** Markdown may reference an external image URL, but nothing is stored server-side. **[⚠ Superseded 2026-08-16 — notes now support image upload to storage; see the Built-app reconciliation note at the top.]**
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
- **D34 — Text + external links only; no uploads.** No file/image uploads and no embedded attachments; **no blob storage is added.** Markdown may reference an external image URL, but nothing is stored server-side. **[⚠ Superseded 2026-08-16 — the built app now supports image upload to storage in notes; see the Built-app reconciliation note at the top of this PRD. External links still supported; storage/cap TBD from code.]**
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

- **v6 (spec 2026-08-17 — DRAFT, not built; design addendum drafted the same day)** — adds the **Finance** (`finance`) module, a functional clone of the standalone **`fin`** service, which v6 then **retires**. `fin`'s locked split formula and column vocabulary cross over verbatim (D82/D83); its session store, JWT plumbing, English UI and import endpoint are dropped. Finance is an ordinary all-roles module in the "více" overflow (D84), joins the audit spine with `finance.month.*` and a field-level diff (D86), soft-deletes (D87), and contributes one widget (`finance.rozpocet`, D88), four household metrics and one list (D89/D90) — including `finance.missing_months`, which with v5 conditions turns "we forgot to enter last month" into a notification. One new table `finance_months` (block 09); the historic months arrive as a **one-off Goose seed in its own migration source**, excluded from `testsupport` (D91). OpenAPI → **0.8.0** (snake_case, `PATCH`, `/api/finance/months` — D92). `fin` retirement is **gated on a row-for-row verification including the recomputed split** (D97), then redirect → stop → archive → deprovision (D96); `fin`'s own spec documents are recovered into `services/fin/` first (D98). Decisions **D81–D98**. Design addendum drafted 2026-08-17 (`HANDOFF-design.md` §v6), pending approval.
- **v5 (spec 2026-08-16 · deployed 2026-08-17)** — adds the **Administrace** (`admin`) module: Web Push over **one shared VAPID channel** (`platform/push`), admin **broadcasts**, **audit-key trigger rules** fed by an `audit_events` outbox tailer (`platform/audit`), and **scheduled summaries** composed from a **metrics catalog** (`platform/metrics`) fired by an in-process minute ticker (`platform/scheduler`; Prague/DST, `last_fired` idempotency, 120-min catch-up, day-of-month clamp D74); plus an installable **reads-only-offline PWA** (`platform/pwa`). Per-user subscription + master/per-category mutes in **Nastavení → Oznámení** for every role incl. `reader`. New `push_subscriptions`, `notification_preferences`, `audit_notify_cursor` (platform) and `notification_rules`, `notification_schedules`, `notification_deliveries` (admin) tables. OpenAPI → 0.6.0. Decisions D51–D74. **Shipped with six post-spec additions — conditions, active hours, a lists catalog, a push self-test, the member directory, and a server-rendered schedule description — taking the deployed contract to OpenAPI 0.7.0 (D75–D80, §V5-12).**
- **v4 (2026-08-11)** — adds the **Dokumenty** (`documents`) module: file storage (PDF/image/Office/other) in a single-parent folder tree, blobs in a dedicated **Cloudflare R2** bucket with metadata in SQLite, **immutable upload-once** content, a **permanent id-based household-only URL** (`/d/{id}`), in-browser **preview** (native for PDF/image/text, Office→PDF via headless LibreOffice, async) + **download**, FTS5 filename/metadata search, two-scope pinning, and the `documents.pripnute` Nástěnka widget (preview overlay, no navigation away). New `document_folders`, `documents`, `documents_fts`, `document_pins` tables + `/api/documents` endpoints incl. `raw`/`download`/`preview`/`thumbnail`; `document`/`document_folder` join the audit diff set; nav grows to a 6th destination (mobile overflow now covers everyone). **Bucket backup = object versioning + mirror to a second R2 bucket** (Litestream stays DB-only). OpenAPI → 0.5.0. Decisions D39–D50; D6 extended, D33 reaffirmed. Design v4 addendum pending.
- **v3 (2026-07-29)** — adds the **Poznámky** (`notes`) module: Markdown notes (single canonical body; WYSIWYG-default + raw toggle) in a single-parent folder tree, human-readable slug-path URLs (in-app/household-only, no redirects), FTS5 search, two-scope pinning ("pro všechny"/"jen pro mě"), and the `notes.pripnute` Nástěnka widget (overlay dialog, no navigation away). New `folders`, `notes`, `notes_fts`, `note_pins` tables + notes endpoints; `note`/`folder` join the audit diff set; nav grows to a 5th destination (admin overflow). Text + external links only — no uploads/blob storage. OpenAPI → 0.4.0. Decisions D30–D38; D6 extended. Design v3 addendum pending.
- **v2 (2026-07-21)** — Mode B self-hosted login + own session (no browser token); Nástěnka as a per-user widget host (server-side layout, three module-provided widgets); compile-time modular monolith with per-module packaging and migrations; new `sessions` + `user_dashboard_layout` tables; auth/session + dashboard-host endpoints; OpenAPI → 0.3.0. Decisions D23–D29; D2/D16 adjusted.
- **v1 (2026-07-21)** — Initial four-module spec (logging, todo, events, dashboard), Mode A auth, hardcoded two-list Nástěnka, single `0001_init`. Decisions D1–D22. Design prototype delivered and reviewed. Never implemented.


---

> **v5 scope:** one new admin-only module, **Administrace** (`admin`), gated exactly like the log browser, that turns Home into a Web-Push sender; plus the app-wide PWA groundwork the push channel rides on. Three admin capabilities:
> 1. **Broadcast** — send a custom push to everyone (or a chosen audience) right now.
> 2. **Trigger notifications** — bind an **audit action key** (the same key the change already logs through the spine) to a push, with admin-authored, token-templated text.
> 3. **Scheduled / summary notifications** — a push at a wall-clock time (e.g. daily 08:00 / 20:00) whose text is composed from a **metrics catalog** the modules publish (e.g. cards in *Právě dělám*, reminders due today).
>
> Home is a single PWA on one origin (`home.tilcer.cz`) with **one service worker**, so there is **one push subscription per device/user and one shared push channel** every module reuses (§V5-1 D52). A user subscribes once; any module can send through it. Payloads carry a `module`/`type` tag so the service worker knows which module a notification came from and where a click should navigate. Standard **Web Push + VAPID** — no cross-service notification bus, just the one channel inside Home.
>
> **v5 also promotes Home to an installable, reads-only-offline PWA** (manifest + app-shell/data read caching), decided at OQ-3/PWA-OQ. That is app-wide work, kept in its own `platform/pwa` strand (§V5-1a) so it does not entangle the notification logic. **Offline is read-only in v5** (no offline write queue — PWA-OQ-A).

---

## V5-1. Overview (delta)

- **One-line summary (add):** an **admin-only** module that lets an admin push Web notifications to household members — ad-hoc broadcasts, audit-action-triggered alerts, and scheduled summaries — over a single shared Web Push channel; plus Home becoming an installable, reads-offline PWA.
- **Type / subdomain / exposure / consumers / depends-on:** **unchanged** from v4. No new BE→BE call. Web Push talks browser↔push-service directly; the browser's push endpoint is supplied by the browser vendor, not a Home dependency.
- **Modules after v5: seven.** `logging`, `todo`, `events`, `notes`, `documents`, `dashboard`, **`admin`** (new). **The other six modules are untouched** — including `events`, whose shared-completion model is unchanged (see §V5-4 FR-M).

### Architecture — one shared push channel (§V5-10 D52)

Home is one origin with one service worker, therefore **one `PushSubscription` per browser** (per user-device). That single subscription is a **shared channel**: any module sends through `platform/push.Send(...)`; there is no per-module subscription and no separate notification service. Every payload is an envelope `{ module, type, title, body, url, tag, data }`; the service worker shows the notification and, on click, navigates to `url` (routing on `module`/`type`). Web Push + VAPID end to end.

- **The channel is platform infrastructure, not the `admin` module** (§V5-10 D53). `platform/push` owns the VAPID keypair, the `push_subscriptions` store, and the `Send` helper — beside `platform/audit` and `platform/ws` so **every** module may send. The `admin` module is only the *configuration surface and rule engine* on top of that channel.
- **Consent and subscription are per-user / per-device, and live in `platform` — not in the admin UI** (§V5-10 D53). A member grants the browser permission and subscribes once from a **Nastavení → Oznámení** panel available to every role; they mute at the **master level and per category** (broadcast / triggers / summaries — §V5-10 D53a). **The admin configures *what* is sent; each user controls *whether their device receives*.** An admin cannot force-subscribe a device.

### Architecture — delivery is decoupled via the audit table as an outbox (§V5-10 D56)

Trigger notifications must fire **after** the mutation commits, and must not couple push latency/failure to the request. The audit spine already writes every mutation to `audit_events` **atomically with the change** — so `audit_events` **is** the transactional outbox.

- A **platform tailer** (`platform/audit` notifier) reads `audit_events` by **UUIDv7 keyset** from a persisted cursor — the pattern the log browser already uses — **at-least-once**, handing each new event to registered **`AuditListener`s**. Listeners must be **idempotent** (dedupe on event id).
- The `admin` module **registers an `AuditListener` in its constructor**; the listener matches events against enabled trigger rules and calls `platform/push.Send`. Because the tailer lives in `platform` and the listener is registered through a platform hook, **the `admin` module never imports `logging`** — the import-lint acceptance criterion (D28) is upheld. No change to the `Module` interface is required.

### Architecture — a scheduler, a deliberate scoped reversal of "no scheduler" (§V5-10 D58)

v4 has **no scheduler** (D9/D11 — reminders are computed on read). Scheduled/summary notifications cannot be computed on read: something server-side must wake at 08:00 Prague and push. v5 therefore adds `platform/scheduler`:

- An **in-process, minute-granularity ticker** in the single Go binary (single instance ⇒ no distributed locking). Each due schedule is evaluated in **`HOME_TIMEZONE` (`Europe/Prague`), DST-aware**.
- Each schedule persists `last_fired_at` + `last_fired_local_date` for **idempotency** (never double-fire a slot) and a **missed-fire catch-up**: a slot missed while the process was down fires once if the backend is back **within `HOME_SCHED_CATCHUP_GRACE` (default 120 min)**, otherwise it is skipped (§V5-10 D58a).

### Architecture — summaries compose over a metrics catalog (§V5-10 D59, D60)

Scheduled text needs cross-module counts *without* cross-module imports. This reuses the widget-provider idea as a third registered catalog (alongside the widget catalog and the audit-action catalog):

- A module optionally publishes **metric descriptors** (`key`, Czech `label`, `unit`, `scope`) and a resolver `Metric(ctx, user, key, asOf) → int`. The registry aggregates them into a **metrics catalog**. The admin composer references metric keys via tokens; the scheduler resolves them at fire time through the **provider contract**, never the module's tables (D28 upheld).
- **Metrics resolve per recipient** (the resolver takes `user`): a **household-shared** metric returns the same number for everyone, a **personal** metric personalizes. Most launch metrics are household-shared (todo board + shared event completion); the **pinned-count** metrics are personal (personal pins differ per user), which is what keeps the per-recipient mechanism live (§V5-4 FR-M).

### Architecture — a service worker (§V5-10 D63; PWA scope in §V5-1a)

Web Push requires a service worker on the origin. v5's service worker implements `push` and `notificationclick` (envelope → `showNotification`; click → focus/navigate to `url`) **and** the `platform/pwa` strand's shell/data read-caching (§V5-1a). It is registered app-wide.

### V5-1a. Architecture — PWA: installable + reads-only offline (`platform/pwa`, new strand, §V5-10 D67/D71/D72/D73)

OQ-3 promotes Home to an **installable PWA with read-only offline**. App-wide work, kept in its own strand so it does not entangle the notification module; the two only share the single service worker.

- **Installable** (D67): a **web app manifest** (`/manifest.webmanifest`: name, icons, `display: standalone`, dark theme/background colours, `start_url`, `scope`) so Home offers add-to-home-screen / standalone-window. Static frontend assets; **not** part of the API spec.
- **Offline is reads-only** (D71, PWA-OQ-A): the SW **precaches the app shell** (built SPA assets, hashed by build id) and **read-through-caches API GETs**; TanStack Query's cache is **persisted** so the last-synced boards, cards, events, notes, and document *metadata* render offline. **All write controls disable offline** with a clear state (e.g. "Změny nelze uložit offline"). **No offline mutation queue, no background sync, no conflict handling in v5** — a clean future increment. Login/CSRF are **online-only**.
- **Silent auto-update** (D72, PWA-OQ-B): a new build's SW **activates on the next load, no prompt**. With no offline write queue there is no queued-payload/version-skew risk to manage.
- **Documents are online-only** (D73, PWA-OQ-C): document **bytes** (originals + preview PDFs, up to 50 MB) are **never cached**; offline shows document metadata/list, but preview/download needs a connection.
- **Interaction with Mode B:** the shell is public static assets (cacheable); **every `/api` response stays session-gated and must never be served from a cache that crosses users** — a logout/again-login must not surface a prior user's cached data (the SW clears data caches on logout / on a user-id change). Auth/CSRF flows are online-only.
- **Boundary:** `platform/pwa` owns the manifest, the SW registration/lifecycle, and the read-cache layer. `platform/push` owns only the `push`/`notificationclick` handlers *inside* that SW. The `admin` module owns neither.

*(OQ-7 was reverted 2026-08-16: the `events` module keeps its existing shared-completion model — there is no per-user-completion change and no events addendum. See §V5-4 FR-M and §V5-10 D68.)*

---

## V5-2. Goals & Non-Goals (delta)

**Goals (add)**

- Ship a **single shared Web Push channel** (`platform/push`, VAPID) with one subscription per device, reusable by every module, and a per-user subscribe/mute panel (**master + per-category**) for all roles.
- Ship the **`admin`** module (admin-only, nav overflow): **broadcast** now; **trigger** notifications bound to audit action keys with token-templated text; **scheduled/summary** notifications composed from a metrics catalog at a wall-clock time.
- Deliver trigger notifications via the **audit-outbox tailer** (at-least-once, idempotent, import-lint-clean) and scheduled ones via a **Prague-time, DST-aware, catch-up-safe scheduler**.
- Promote Home to an **installable, reads-offline PWA** (`platform/pwa`: manifest + app-shell/data read caching), sharing the one service worker.
- Give the admin **real composition freedom within safe bounds**: free-text Czech around a fixed **token palette**, any published **action key** or **metric key**, any audience, any schedule — with the *building blocks* (which keys/metrics exist) defined in module code.

**Non-Goals (v5)**

- **No email / SMS / other channels** — Web Push only. (Email fallback is candidate future work.)
- **No arbitrary templating logic** — text is free-form around a **fixed token set** (no expressions, loops, or code; §V5-10 D61).
- **No user-authored rules** — only an admin creates broadcasts/trigger/schedule rules. Members can only **subscribe and mute** (§V5-10 D53).
- **No new metrics beyond the shipped catalog** — admins reference only the published set; new metrics are a module change (§V5-10 D59, D69).
- **No new role tier.** Roles stay `admin`/`editor`/`reader` (+ `*` superuser). "Superadmin" = the **`*` superuser** (§V5-10 D62).
- **No offline writes** — offline is **read-only** (D71): no mutation queue, no background sync, no conflict resolution in v5. **No offline auth** (login/CSRF online-only). **Document bytes are not cached offline** (D73).
- **No change to the `events` module** — shared completion stays; no per-user reminders in v5 (§V5-10 D68, reverting OQ-7).
- **No per-recipient scheduling by users, no snooze, no per-user quiet-hours** in v5 — candidate future work.
- **No delivery guarantees beyond best-effort Web Push** — expired endpoints are pruned; an unsubscribed / permission-revoked device receives nothing.

---

## V5-3. Users, Roles & Auth (delta)

Roles unchanged (`admin`/`editor`/`reader`, `*` = superuser). New surface splits by audience:

- **Admin configuration** (`/api/admin/notifications/**`): **`admin` only** — identical gate to the log browser (§V5-10 D62). The `*` superuser passes. There is no separate "superadmin".
- **Per-user push** (`/api/push/**`): **any authenticated member** (incl. `reader`) — fetch the VAPID public key, register/remove **their own** subscription, read/patch **their own** mute preferences (master + categories). A member only ever sees/mutates their own device's subscription.

Authorization stays **server-side from home's session**; admin writes additionally require the **CSRF header**.

---

## V5-4. Functional Requirements (v5)

**Every mutating requirement records an audit event through the spine in the same transaction.** The `admin` module declares its own audit actions (`admin.broadcast.send`, `admin.rule.create|update|delete`, `admin.schedule.create|update|delete`, `admin.notification.test`) via `AuditActions()` — the notifier logs itself, like `logging.prune`.

### Platform — push channel (`platform/push`)

#### FR-P1: Register a subscription
- **Trigger:** a member grants browser permission; the SPA calls `PushManager.subscribe(applicationServerKey)` and POSTs the result.
- **Inputs:** `endpoint` (unique), `keys.p256dh`, `keys.auth`, optional `user_agent`.
- **Behaviour:** upsert on `endpoint` (re-subscribe updates keys + `last_seen_at`, no duplicate); bind to caller's `user_id`; clear `failing_since`. One row per endpoint; a user may have several devices.
- **Outputs:** `201`/`200` with the stored subscription id. Audited (`platform.push.subscribe`).
- **Errors:** `422` malformed keys/endpoint.

#### FR-P2: Remove a subscription (unsubscribe this device)
- **Inputs:** `endpoint`. **Behaviour:** delete the caller's row; idempotent (`204` even if gone). Audited (`platform.push.unsubscribe`). Also invoked by the SW on `pushsubscriptionchange`.

#### FR-P3: Fetch the VAPID public key
- Return `HOME_VAPID_PUBLIC_KEY` (base64url); session-gated; never returns the private key. `{ "key": "…" }`.

#### FR-P4: Send (internal helper — not an HTTP route)
- **Signature:** `Send(ctx, recipients []UserID, payload Envelope) → per-endpoint results`.
- **Behaviour:** resolve each recipient's active subscriptions **honouring their mute preferences** (FR-P5) and the payload's `category`; encrypt (aes128gcm) and POST with a VAPID `Authorization`. **On `404`/`410` → delete the subscription.** On `429`/`5xx` → backoff + mark `failing_since`; prune after `HOME_NOTIF_MAX_FAILDAYS`. Every attempt writes a `notification_deliveries` row (operational, §V5-5). Bounded, concurrent fan-out; no N+1.

#### FR-P5: Per-user notification preferences
- **Inputs (PATCH):** `enabled` (master) + per-category `broadcast`, `triggers`, `summaries` (§V5-10 D53a).
- **Behaviour:** stored per user; **honoured by FR-P4** (master off ⇒ nothing; a category off ⇒ that class suppressed). Defaults on first read: all `true`. Audited on change (`platform.push.prefs`).

### Platform — scheduler (`platform/scheduler`)

#### FR-S1: Evaluate due schedules
- **Trigger:** minute ticker. **Behaviour:** for each **enabled** schedule, decide if its wall-clock slot is due now in `Europe/Prague` (DST-aware) and not already fired for that local date/slot. If due: resolve audience (FR-ADM3), resolve metric tokens **per recipient**, render, `Send`, persist the fired marker. **Missed-slot policy:** fire once if back within `HOME_SCHED_CATCHUP_GRACE` (default 120 min), else skip. A metric-resolver error degrades that token to a safe placeholder and logs `warn`; it never aborts the send.

### Admin module (`admin`)

#### FR-ADM1: Broadcast now
- **Inputs:** `title`, `body` (free text; only time tokens resolve in a broadcast), `audience` (default **all**, §V5-10 D66), optional `url` (default `/`), `category` = `broadcast`.
- **Behaviour:** resolve audience → `Send` immediately; record a delivery batch; **audit `admin.broadcast.send`** with recipient count. `202` + `{ recipients, subscriptions }`. `422` on empty title/body or empty resolved audience.

#### FR-ADM2: Trigger rules (CRUD)
- **Inputs:** `name`, `enabled`, `action_key` **or** `action_prefix` (must exist in the catalog, FR-ADM4), optional filters (`module`, `entity_type`, `level`, `actor_type`), `audience` (default **all**), `title_template`, `body_template` (**default = the audit event's Czech `summary`**), `coalesce_window_seconds` (default **60**, §V5-10 D57), `exclude_actor` (**default `false`**, §V5-10 D66), `category` = `triggers`.
- **Behaviour (via the outbox listener):** for each new matching audit event, resolve audience (minus the actor only if `exclude_actor`), render text (tokens `{{event.summary|action|module|entity_type|entity_id|actor_label}}`, `{{change.<field>.old|new}}`), **coalesce** repeats of the same rule within the window into one push, then `Send`. Rule CRUD audited. `422` unknown `action_key`, bad token, or empty audience.

#### FR-ADM3: Schedule rules (CRUD)
- **Inputs:** `name`, `enabled`, `schedule` = `{ time_local: "HH:MM", days: <daily | weekdays | weekends | [mon..sun] | day_of_month:N> }`, `audience` (default **all**), `title_template`, `body_template` (tokens `{{metric.<key>}}` from the catalog + `{{now}}`/`{{date}}`), `category` = `summaries`. Timezone fixed `Europe/Prague`.
- **Behaviour:** evaluated by FR-S1; metric tokens resolve **per recipient**. CRUD audited. `422` on an unknown metric key. **Day-of-month is 1–31** (the composer accepts the full range, **not** a 28 cap): a schedule on **29/30/31 clamps to the month's last day** in short months, matching the events short-month clamp (D19). So "31st of each month" fires on 28/29 Feb, 30 Apr, etc. (§V5-10 D74).
- **The two worked examples are expressible as-is:**
  - **08:00 daily:** `"Právě dělám: {{metric.todo.pravedelam_count}} úkolů · Připomínky na dnešek: {{metric.events.pripominky_today}}"`, audience all, days daily.
  - **20:00 daily:** `"Nesplněné připomínky na dnešek: {{metric.events.pripominky_today_open}} · Hotovo dnes: {{metric.todo.done_today}} · Zbývá v Právě dělám: {{metric.todo.pravedelam_count}}"`.

#### FR-ADM4: Catalog
- Return, from the live registry: (a) the **action-key catalog** (every `AuditActions()` key, grouped by module, with a sample summary); (b) the **metrics catalog** (every descriptor: `key`, Czech `label`, `unit`, `scope`); (c) the **token palette** per context. Admin-only. Drives the composer dropdowns so keys are picked, not typed.

#### FR-ADM5: Delivery log (browse)
- Paged (UUIDv7 keyset), filterable by `kind`, `status`, `rule_id`, `from`/`to`, `user`. Operational, not audit.

#### FR-ADM6: Test send (to self)
- Render with the current draft and send **only to the calling admin's** subscriptions, bypassing audience and mute. Audited `admin.notification.test`.

### Module metric providers — launch catalog (§V5-10 D69)

All launch metrics are **household-shared** except the two pinned-counts, which are **per recipient** (personal pins differ). The `events.*` metrics read the shared completion state (unchanged from v4 — §V5-10 D68).

**`todo` (household-shared):**
- `todo.pravedelam_count` — cards in `kind=now` columns across non-archived boards.
- `todo.done_today` — cards moved into a `kind=done` column since local midnight (`Europe/Prague`).
- `todo.open_total` — all non-done cards (`kind` in `normal|now`) across non-archived boards.

**`events` (household-shared; shared completion):**
- `events.pripominky_today` — current reminders: the `events.pripominky` widget's non-overdue rows (lead open, `occurrence − reminder_lead ≤ today`, day not yet passed), completed or not — see **D99**.
- `events.pripominky_today_open` — of those, not completed (household completion).
- `events.overdue_open` — past occurrences still uncompleted.
- `events.due_within_7d` — occurrences due in the next 7 days (event dates, not reminder dates).

**`notes` / `documents` (per recipient — household ∪ personal pins, de-duped):**
- `notes.pinned_count` — notes pinned visible to this user.
- `documents.pinned_count` — documents pinned visible to this user.

---

## V5-5. Data Model (v5)

New tables. SQLite → Litestream `home/` (no blob store; push has no blobs). UUIDv7 ids. **`platform` migrations** own the per-user tables; **`admin` migrations** own the rule/schedule/delivery tables and run **last**. **No existing table changes** (events unchanged; PWA read-caching is client-side and adds no server tables).

**`push_subscriptions`** *(platform)* — `id` · `user_id` · `endpoint` **UNIQUE** · `p256dh` · `auth` · `user_agent` NULL · `created_at` · `last_seen_at` · `failing_since` NULL. Indexes `(user_id)`, `UNIQUE(endpoint)`.

**`notification_preferences`** *(platform)* — `user_id` **PK** · `enabled` DEFAULT 1 · `cat_broadcast` DEFAULT 1 · `cat_triggers` DEFAULT 1 · `cat_summaries` DEFAULT 1 · `updated_at`.

**`notification_rules`** *(admin — triggers)* — `id` · `name` · `enabled` · `action_key` NULL · `action_prefix` NULL · `filter_module` NULL · `filter_entity_type` NULL · `filter_level` NULL · `audience` (JSON) · `title_template` NULL · `body_template` NULL · `coalesce_window_seconds` DEFAULT 60 · `exclude_actor` DEFAULT 0 · `created_by` · `created_at` · `updated_at`. Index `(enabled, action_key)`, `(enabled, action_prefix)`.

**`notification_schedules`** *(admin)* — `id` · `name` · `enabled` · `time_local` · `days_spec` (JSON) · `audience` (JSON) · `title_template` · `body_template` · `last_fired_at` NULL · `last_fired_local_date` NULL · `created_by` · `created_at` · `updated_at`. Index `(enabled)`.

**`notification_deliveries`** *(admin — operational, prune-able)* — `id` · `ts` · `kind` CHECK(`broadcast`,`trigger`,`schedule`,`test`) · `category` · `rule_id` NULL · `user_id` · `subscription_id` NULL · `status` CHECK(`sent`,`failed`,`expired`) · `error` NULL. Indexes `(ts DESC)`, `(kind, ts)`, `(rule_id, ts)`, `(status, ts)`. Retention: prune older than `HOME_NOTIF_DELIVERY_RETENTION_DAYS` (**default 30**; `0` = keep forever — §V5-10 D64).

**`audit_notify_cursor`** *(platform)* — single row `last_event_id` · `updated_at` — the outbox tailer's keyset position (§V5-10 D56).

Migration order: `logging → platform → todo → events → notes → documents → dashboard → admin`.

---

## V5-6. API Surface (v5)

Full detail in `openapi.yaml` v0.6.0 (companion `openapi-v5-admin.yaml`). All routes **session-authenticated**; state-changing routes require the **CSRF header**. New tags: `push`, `admin-notifications`. (`platform/pwa` adds no API — the manifest and cached assets are static frontend files.)

Per-user (`push`, any member): `GET /api/push/vapid-key` · `POST|DELETE /api/push/subscriptions` · `GET|PATCH /api/push/preferences`.

Admin (`admin-notifications`, `admin` only): `POST …/broadcast` · `GET|POST …/rules` · `GET|PATCH|DELETE …/rules/{id}` · `POST …/rules/{id}/test` · `GET|POST …/schedules` · `GET|PATCH|DELETE …/schedules/{id}` · `POST …/schedules/{id}/test` · `GET …/catalog` · `GET …/deliveries`.

**Audience** (shared JSON): `{ scope: "all"|"roles"|"users", roles?, users? }`. Default scope **all** everywhere (§V5-10 D66). `exclude_actor` lives on trigger rules (default false).

---

## V5-7. Frontend (v5)

**Admin module page (Administrace)** — admin-only, in the **"více" nav overflow** (same rule as Log). Tabs: **Rozeslat** (broadcast composer + audience + test + send), **Pravidla** (trigger-rule list/editor; `action_key` from the catalog dropdown, filters, audience, event-token template, coalesce window, `exclude_actor`), **Souhrny** (schedule list/editor; time + day mask, audience, metric-token template, test), **Doručení** (paged, filterable delivery log).

**Per-user — Nastavení → Oznámení** (every role): permission/subscribe for *this* device, **master + three category** toggles, self-test.

**Service worker (shared):** `push` → `showNotification(envelope)`; `notificationclick` → focus/open `url` (routes on `module`/`type`); **plus** the `platform/pwa` lifecycle — precache app shell by build id, **read-through cache** API GETs + persisted TanStack Query for **offline reads**, **silent activation** on a new build, and **data-cache clear on logout / user change**. **Offline UX:** last-synced content renders read-only; write controls disable with "Změny nelze uložit offline"; document previews/downloads and login show an online-required state. **Manifest:** installable, `display: standalone`, dark colours.

TanStack Query keys: `['push','prefs'|'vapid']`, `['admin','rules'|'schedules'|'catalog']`, `['admin','deliveries',{filters}]`. Invalidate the matching list on each mutation. Query persistence is scoped per user id (cleared on logout).

---

## V5-8. Non-Functional Requirements (v5)

- **Observability:** baseline unchanged. Add structured logs per send batch (rule id, recipients, sent/failed/expired) and per firing scheduler tick. Deliveries table is the queryable record; the audit spine records admin *configuration* actions.
- **Security:** VAPID keys are **secrets** (Coolify env), never in the repo, only the **public** key served (FR-P3). Admin routes `admin`-gated + CSRF. Subscription rows per-user; no cross-member read/delete. **PWA cache never crosses users** — `/api` responses stay session-gated and are not served from a cache that could surface another user's data after logout (SW clears data caches on logout / user-id change); auth/CSRF are online-only. Push bodies can appear on a **lock screen**; per OQ-10 the composer imposes **no restriction** — accepted for a household app.
- **Reliability:** trigger path **at-least-once** (idempotent listeners); schedule path **at-most-once-per-slot** (fired marker). Dead endpoints self-prune (404/410). A push-service outage degrades gracefully — deliveries marked `failed`, retried next occurrence, never blocking a mutation. Offline is read-only, so there is no write-replay reliability surface.
- **Performance:** fan-out bounded + concurrent; the outbox tailer is a single indexed keyset scan at a modest interval; no per-request push work on the hot path. Offline read-cache is served from the SW/Query cache without hitting the network.
- **Backup:** new tables ride Litestream `home/`; no new bucket. Fresh build restores them; stale `push_subscriptions` prune on first failed send.

---

## V5-9. Configuration (v5 env vars — Coolify only)

- `HOME_VAPID_PUBLIC_KEY`, `HOME_VAPID_PRIVATE_KEY`, `HOME_VAPID_SUBJECT` — VAPID keypair/identity. **Secrets.**
- `HOME_NOTIF_COALESCE_DEFAULT` — default per-rule coalesce window, **default 60 s**.
- `HOME_NOTIF_DELIVERY_RETENTION_DAYS` — **default 30**; `0` = keep forever.
- `HOME_NOTIF_MAX_FAILDAYS` — prune a continuously-failing subscription after this many days (default e.g. 14).
- `HOME_SCHED_TICK_SECONDS` — scheduler granularity (default 60).
- `HOME_SCHED_CATCHUP_GRACE` — fire a missed slot if back within this many minutes, **default 120**.
- `HOME_PUSH_ENDPOINT_HOSTS` *(added in the build, §V5-12 D78)* — **extra** push-service hostnames a subscription endpoint may name, on top of the built-in allowlist (Google, Mozilla, Apple, legacy WNS). Bare hostnames, comma-separated, **never URLs**; matched exactly or as a subdomain. Boot logs the effective list as `push_hosts=builtin` or `builtin+[…]`.
- Existing `HOME_TIMEZONE` (`Europe/Prague`) governs all schedule/metric date math.

---

## V5-10. Decisions (D51–D74) & Resolutions

**Decisions:**

- **D51** — v5 is **additive**: one admin-only module + `platform/push` + `platform/scheduler` + `platform/pwa` + small metric providers. No change to auth, the dashboard host contract, or the other six modules (including `events` — D68).
- **D52** — **One shared Web Push channel** (one SW, one subscription/device), VAPID; every module sends via `platform/push.Send`; envelope routed by the SW on `module`/`type`. No separate notification service.
- **D53** — **Subscription & consent are per-user/per-device, platform-owned** (settings panel for all roles); `admin` only configures what is sent. Admin cannot force-subscribe.
- **D53a** — **Per-category mutes:** master switch + `broadcast`/`triggers`/`summaries` toggles, honoured at send time.
- **D54** — **Broadcast** = ad-hoc admin send to an audience; audited, recorded in deliveries; not a persisted rule.
- **D55** — **Trigger notifications reuse the audit action-key vocabulary**; default body = the audit event's Czech `summary`, overridable via a token template.
- **D56** — **`audit_events` is the transactional outbox.** A platform tailer reads it by UUIDv7 keyset (persisted cursor, at-least-once) → idempotent `AuditListener`s; the `admin` listener never imports `logging`. No `Module`-interface change.
- **D57** — **Coalescing** collapses a burst of one rule's matches within a window; **default 60 s**, per-rule, `0` = fire every event.
- **D58** — **A scheduler is added** — a scoped reversal of D9/D11 — in-process minute ticker, `Europe/Prague` DST-aware, `last_fired` idempotency.
- **D58a** — **Missed-slot catch-up:** fire once if back within `HOME_SCHED_CATCHUP_GRACE` (**default 120 min**), else skip.
- **D59** — **Summaries compose over a metrics catalog** (third registered catalog); admin references only published keys; new metrics are a module change.
- **D60** — **Metrics resolve per recipient** — one mechanism serves household-shared and personal text; at launch only the pinned-counts are personal.
- **D61** — **Templating is a fixed, safe token palette**, not code.
- **D62** — **`admin` is admin-only** (log-browser gate; `*` superuser passes; **no new "superadmin" tier**), in the **"více"** overflow; per-user settings live in **Nastavení**. Migration order appends `admin` last; per-user tables are `platform` migrations.
- **D63** — v5's service worker does `push` + `notificationclick`, and also hosts the PWA read-cache (D67/D71).
- **D64** — **Deliveries are operational, not audit** — prune-able, **default 30-day** retention; dead endpoints (404/410) self-delete.
- **D65** — **VAPID keys are Coolify secrets**; only the **public** key is served. New tables ride Litestream `home/`; no new blob store.
- **D66** — **Default audience = all users, actor included**; `exclude_actor` is per-rule and **defaults false**.
- **D67** — **Installable PWA in v5** via a dedicated `platform/pwa` strand: web app manifest (`standalone`, dark colours), one shared SW. App-wide, kept separate from the notification logic.
- **D68** — *(reverts OQ-7)* **The `events` module keeps shared completion — no per-user reminders in v5.** `events.*` metrics are household-wide. No events schema change, no migration, no events addendum.
- **D69** — **Launch metric catalog** (§V5-4 FR-M): 3 `todo.*` + 4 `events.*` (all household-shared) + `notes.pinned_count` + `documents.pinned_count` (per recipient).
- **D70** — **No composer lock-screen restriction**; push bodies may appear on a lock screen (accepted for a household app).
- **D71** — **Offline is reads-only:** precache app shell + read-through cache API GETs + persisted TanStack Query; write controls disable offline; **no offline mutation queue / background sync / conflict handling** in v5. Login/CSRF online-only.
- **D72** — **Silent auto-update:** a new build's SW activates on the next load, no prompt (no write queue ⇒ no version-skew risk).
- **D73** — **Document bytes are online-only:** originals + preview PDFs are never cached offline; document metadata/list still renders offline.
- **D74** *(added 2026-08-16, from the v5 design review)* — **Schedule day-of-month is 1–31, clamping 29–31 to the month's last day** in short months (consistent with events' short-month clamp, D19). The composer accepts 1–31, **not** a 28 cap; a rule set to a day the month lacks fires on that month's last day. *(The v5 design file currently caps the day-of-month input at 28 — to be reconciled to 1–31 + a "posune se na poslední den měsíce" hint.)*

**Resolution map (2026-08-16):** OQ-1 → all/actor-included (D66) · OQ-2 → per-category mutes (D53a) · OQ-3 → installable PWA (D67) · OQ-4 → 60 s coalesce (D57) · OQ-5 → 120 min catch-up (D58a) · OQ-6 → 30-day retention (D64) · **OQ-7 → reverted; events keep shared completion (D68)** · OQ-8 → broad metric set (D69) · OQ-9 → gate on admin, no new tier (D62) · OQ-10 → no lock-screen restriction (D70) · PWA-OQ-A → reads-only offline (D71) · PWA-OQ-B → silent auto-update (D72) · PWA-OQ-C → documents online-only (D73) · EVENTS-OQ → void (OQ-7 reverted).

**Open questions: none.** All items resolved; the addendum is ready for the design + engineering handoffs.

---

## V5-11. Acceptance Criteria (v5)

- [ ] `platform/push`: subscribe/unsubscribe/vapid-key; `Send` encrypts (aes128gcm)+VAPID, prunes 404/410, honours **master + per-category** mutes; one subscription per endpoint; per-user isolation.
- [ ] Shared service worker: `push` shows the envelope; `notificationclick` focuses/navigates by `url`; **offline app-shell + read-through data cache** (persisted TanStack Query) render last-synced content read-only; **new build activates silently**; **manifest** makes Home installable (`standalone`, dark colours). No `/api` cache crosses users (cleared on logout/user change); auth + document bytes online-only.
- [ ] Offline write controls disabled with a clear state; **no mutation queue** exists (verified — writes fail closed offline, no replay).
- [ ] Per-user **Nastavení → Oznámení**: permission+subscribe for this device, master + 3 category toggles, self-test; available to `reader`.
- [ ] **Outbox tailer** in `platform`: keyset scan of `audit_events`, persisted cursor, at-least-once, idempotent; `admin` registers its listener **without importing `logging`** — import-lint green.
- [ ] **Trigger rules** CRUD (admin+CSRF); match by key/prefix + filters; default body = audit `summary`; token render; 60 s coalescing collapses bursts; `exclude_actor` (default false) honoured; **default audience = all incl. actor**.
- [ ] **Scheduler** fires the two examples at 08:00/20:00 `Europe/Prague`; correct across a DST boundary; never double-fires a slot; catches up only within 120 min.
- [ ] **Metrics catalog** (all 9) resolved through the provider contract (no cross-module import; import-lint green); `todo.*` + `events.*` household-wide, pinned-counts per recipient.
- [ ] **Broadcast** sends now; **catalog** returns actions+metrics+tokens; **deliveries** paged/filterable, 30-day prune; **test** reaches only the caller.
- [ ] `admin` module: admin-only gate identical to Log; page in **"více"**; declares `AuditActions()`; all config mutations audited.
- [ ] `events` module **unchanged** — shared completion intact; no migration ran.
- [ ] Migrations run `… dashboard → admin`; VAPID keys from Coolify env (secrets), only public served; new tables restore on a fresh Litestream build.
- [x] OpenAPI **0.7.0** validates; new paths/schemas reuse shared `Cursor`/`Limit`/`responses`/security components. *(0.6.0 was the spec; the build added the §V5-12 surface.)*

---

## V5-12. As built — what shipped beyond the v5 spec (2026-08-16/17, OpenAPI 0.6.0 → **0.7.0**)

> v5 was built and deployed straight after the spec froze, and four merged branches added product surface the spec never described. This section is the **reconciliation**: the deployed contract is `openapi.yaml` **0.7.0**, and the decisions below (**D75–D80**) were taken during the build, not in the design round. Everything in §V5-1…§V5-11 still holds — this only adds.

### Decisions taken during the build

- **D75 — Conditions gate a send.** Both trigger rules and scheduled summaries accept an optional condition block: `{mode: all|any, items: [{key, op: gt|gte|lt|lte|eq|neq, value}]}` (max 8 clauses), where `key` names a **metric** or a **list judged by its length**. Null ⇒ always send. A rule could previously say only *what* to react to, never *whether it is worth saying now*.
  - **Evaluated at the moment the push would go out** — after a trigger's coalescing window, at a summary's slot — so "jen když něco zbývá" is judged against the counts as they are *then*, not as they were when the window opened.
  - **Triggers may use household-scoped keys only.** A trigger renders once for its whole audience, so a personal value has no recipient to belong to; `validateRule` refuses them and the composer palette stops offering them. On a **summary**, a personal key is a per-recipient question and skips exactly the recipients it fails for; household clauses resolve once and seed every render.
  - **A clause that cannot be resolved FAILS OPEN**, with a warning. A notification suppressed by a transient read error is a notification lost, which is worse than one sent too eagerly.
  - Condition values resolve **through the render context**, so a key both gated on and printed costs one read and the text can never contradict the gate that let it through.
  - `ScheduleUpdate` gained the same absent / null / block **three-state decode** `RuleUpdate` has, so a read-modify-write of the GET response cannot mean "keep" when it says "no conditions".
- **D76 — Active hours on trigger rules.** `active_from_local` / `active_to_local`, `"HH:MM"` wall-clock in `HOME_TIMEZONE`, **both bounds or neither**, may wrap midnight (22:00–06:00). A send falling outside the window is **dropped, not queued** — stale news is worse than no news. Schedules already name their own time, so this is trigger-only.
- **D77 — Lists: a fourth registered catalog** (`platform/lists`, alongside widgets, audit actions and metrics). A metric answers *how many*; a list answers *which ones*. Providers return **items and never format them** — the sender decides what fits in a push, one bulleted line per item, capped. `{{list.<key>}}` tokens are allowed in **bodies and refused in titles** (bullets and newlines in a headline the sender clips at 80 runes). List keys **mirror the metric keys and mean the same selection** (one exception since **D100**: `events.pripominky_active` is list-only), so a summary can never say three and then list four — the scheduler derives the count as `len(items)` rather than paying for the read twice. Registration mechanics shared with metrics live in `platform/catalog` (generic over descriptor and provider, scope constants aliased by both) so the scheduler can filter tokens from both catalogs through one predicate. Discovery is by **type assertion** (`lists.Source`), so no `Module`-interface change and `admin` still imports no feature module. **Launch lists (9):** `todo.pravedelam_count` · `todo.done_today` · `todo.open_total` · `events.pripominky_today` · `events.pripominky_today_open` · `events.overdue_open` · `events.due_within_7d` · `events.pripominky_active` *(list-only, D100)* (household) · `notes.pinned` (personal). `documents` publishes none.
- **D78 — A push self-test, and an endpoint allowlist.** `POST /api/push/test` is the last step of the permission gauntlet — it answers "did it actually arrive on *this* device?". Open to **every role incl. `reader`** because the recipient is the session's user id, never anything from the request, and it **bypasses mutes on purpose** (FR-ADM6's rule applied to the member's own self-test): the question is whether the device works, not whether they want the category. Separately, because a subscription's endpoint URL is *where this server POSTs*, endpoints are **allowlisted** against the known push services (Google, Mozilla, Apple, legacy WNS) rather than trusted, extensible via `HOME_PUSH_ENDPOINT_HOSTS`; a refused device sees *"neznámá push služba: …"*.
- **D79 — The catalog carries the household directory; deliveries carry a label.** `GET` catalog now returns `members` (`user_id`, `email`, `display_name`, `roles`, **`subscriptions`** = registered device count) so the audience picker needs one round trip and can say out loud that selecting somebody with no device reaches nobody. Home has no user table — auth owns identity — so this is **projected from the session store**, which caches each identity and its roles at login (**no `known_users` table**, per the v5 brief). For the same reason the delivery log gained **`user_label`**: it exists to answer "did it reach Eva?", which a raw id cannot; it falls back to the id for a member home no longer has a session for.
- **D80 — Two contract hardenings.** (a) A schedule's recurrence is returned as a **server-rendered Czech `description`** ("Každý den v 8:00"), produced from the same rules the ticker applies, so the list and the ticker can never disagree about what a pattern means. (b) A push envelope's `url` must be a **same-origin path** — pattern `^/($|[^/])`, `maxLength 512`: the service worker resolves it against the app origin, a value carrying its own scheme would win over that base and send a click off-origin, and the cap keeps the encrypted payload inside the ~4 KB Web Push record. Relatedly, `filter_module` now **qualifies** `action_key` (a bare verb is not unique across modules; the enum gained `platform`), and a key/module pair no module emits is refused **422** rather than stored as a rule that could never fire.

### Implementation deviations worth carrying forward

These change no product behaviour but contradict the shapes sketched in `HANDOFF-7-admin.md`; the code is right and the brief is stale.

- **`platform/push` writes nothing of admin's.** `Send` takes a narrow **`Recorder`** interface that the admin store implements, instead of inserting `notification_deliveries` directly — platform writing a module's table would invert the ownership rule the architecture rests on. Row-per-attempt behaviour is identical, and a household running without the module simply keeps no log.
- **Catalogs are registries built at composition, not package-level globals.** `metrics` / `lists` expose an optional **`Source`** interface and a `*Registry` assembled in `bootstrap`, keeping "modules declare what they provide" without global mutable state leaking between tests.
- **`audit.NewSink()` returns `*audit.Writer`** (was the `Sink` interface) so the composition root can attach the tailer's nudge; every existing call site still compiles. The nudge is a **non-blocking buffered send with a `default:` drop** — `Record` runs inside every module's write transaction, so it can never slow, fail or block a caller's tx. The ~1 s poll remains the correctness path; the nudge is only latency.
- **Coalescing buffers the audit EVENT, not rendered text**, because D75 moved rendering to send time. Trigger templates may therefore print `{{metric.…}}` / `{{list.…}}` too, resolved when the push goes out.
- **Migration numbering:** v5 adds `02002_push.sql` + `02003_audit_cursor.sql` to the platform block and a new `08001_admin_notifications.sql` block, so Goose still applies `admin` last while the platform tables land at 02.
- **Frontend layout:** new v5 code follows the brief (`src/modules/admin/`, `src/platform/{push,pwa,settings}/`, `src/sw.ts`); the five legacy pages stay in `src/routes/*` — relocating them is still an open Phase 1 item, not v5's job.
- **Icons are real, not placeholders.** The launcher/notification mark is the favicon's house-sheltering-a-family in colour on the accent blue, drawn as one vector scene and rasterised with Playwright's Chromium (`npm run icons`); the badge stays a white house-with-door silhouette (Android tints and shrinks it to ~24 dp); the favicon stays black & white for 16 px.

### Still open

- The **Playwright/axe pass at 375 / 1440 in both themes** (plan.md §8.11) is the one v5 acceptance item not yet ticked.

---

> **v6 scope:** an **eighth module, Finance** (`finance`) — a functional clone of the standalone `fin` service (`fin.tilcer.cz`), absorbed into Home so the household has one app instead of two. The module records each month's two incomes and derives the household's account split from them; the **calculation is carried over verbatim and stays locked**. What changes is everything around it: Home's Czech UI, Home's session and roles, Home's audit spine, Home's dashboard, and Home's notification catalogs. `fin`'s own login, session table, JWT plumbing and English UI are **dropped, not ported** — Home already owns all of that.
>
> Three parts, in order:
> 1. **The module** — `finance`, registered like every other feature module: own routes, own migration block, own audit actions, one Nástěnka widget, metric + list providers.
> 2. **The data** — every month `fin` holds today, moved across as a **one-off Goose seed** and verified row-for-row before anything is torn down.
> 3. **The retirement** — `fin`'s backend stops, its repo is archived, its auth site is deprovisioned. **No redirect** (§V6-10 D96): the subdomain simply goes away, and both users are told the app moved. **Nothing is torn down until the verification in §V6-12 passes** — and with no redirect standing behind it, that verification is the only safety net there is.
>
> v6 is **additive**: no change to auth, the dashboard-host contract, `platform/*`, or the seven existing modules. It is the first module Home has gained that replaces a *live service* rather than adding a new capability, so the spec carries a migration and decommissioning plan the earlier versions never needed.

---

## V6-1. Overview (delta)

- **One-line summary (add):** a module that records each month's two household incomes and shows the derived flow into personal, operational and savings accounts — the retiring `fin` service, rebuilt inside Home.
- **Type / subdomain / exposure / consumers / depends-on:** **unchanged** from v5. No new BE→BE call, no new external dependency, no new bucket. `fin.tilcer.cz` is **switched off**, not redirected (§V6-10 D96) — it was never a dependency of Home and does not become one.
- **Modules after v6: eight.** `logging`, `todo`, `events`, `notes`, `documents`, `dashboard`, `admin`, **`finance`** (new). **The other seven modules are untouched.**
- **Catalogs after v6:** widgets 6 (+1), metrics 13 (+4), lists 9 (+1), audit actions +3. Every one of those is a registration, not a host edit.

### Architecture — a clone, not a port (§V6-10 D81)

`fin` is a two-app fe/be pair with its own Mode B session store, its own auth-service client, its own English React SPA and its own SQLite file. Almost none of that is worth carrying: Home has a better version of each. What is worth carrying is exactly two things — **the data model** and **the calculation** — and both are small enough to read in one sitting.

So `finance` is built as a **new Home module that happens to have the same behaviour**, not as a lift-and-shift:

| `fin` has | `finance` does |
|---|---|
| own `sessions` table + an auth client (`POST /login`, `/introspect`, `/internal/token/mint`) | **dropped** — Home's Mode B session (D23) |
| bearer JWT + `/introspect` verification | **dropped** — Home's session cookie + CSRF (D24) |
| English UI ("Months", "Fun savings") | **Czech UI** (§V6-10 D85), English code ids (D20) |
| no audit trail | **every mutation audited in the same tx** (the spine, D-arch) |
| `PUT`, camelCase JSON, `/months` | **`PATCH`, snake_case, `/api/finance/months`** (§V6-10 D92) |
| hard delete | **hard delete, kept** — the audit event carries the whole deleted row (§V6-10 D87) |
| no dashboard, no notifications | one **widget**, four **metrics**, one **list** (§V6-10 D88–D90) |
| `POST /months/import` (never used in anger) | **dropped** — replaced by the seed migration (§V6-10 D91) |
| **the `months` table's columns** | **carried over literally** (§V6-10 D83) |
| **the split formula** | **carried over verbatim and locked** (§V6-10 D82) |

### Architecture — the calculation is the asset (§V6-10 D82)

The one thing in `fin` that took real work is the formula, and only because the *old* app it replaced had two contradictory implementations. Karel confirmed and locked the intended arithmetic on 2026-06-10; `fin`'s `internal/months/split.go` is its authoritative expression, with a unit test pinning the worked example and the four reconciliation invariants.

**That file crosses over unchanged** — same rounding order, same remainder handling, same invariants, same worked-example test. The split is **derived on read and never stored**, exactly as in `fin`: storing only the inputs is what guarantees the locked formula stays the single source of truth and that history stays reproducible if a rate is ever corrected.

This is also why the schema is carried over literally rather than modernised (D83). Renaming `income_kaja` → `person_a` would mean the formula, the seed data and the tests all have to be re-checked against a translation layer, for no behavioural gain in a two-person household.

### Architecture — one live service replaces another (§V6-10 D91, D96, D97)

Every earlier version of Home added something new. v6 **removes** something: at the end of it, a service that people use today no longer exists. The spec therefore separates three moments that must not be collapsed into one:

1. **Home v6 goes live with the data seeded and `fin` still running.** Both apps work; `fin` is the reference.
2. **Verification** — row-for-row comparison of `fin`'s months against Home's, *including the recomputed split* (§V6-12). Until this passes, nothing is retired.
3. **Retirement** — stop, archive, deprovision, in that order (§V6-12). No redirect (D96).

The seed lands as a **Goose migration in its own migration source** rather than an import endpoint, so the historic rows are versioned with the schema and restored by Litestream like everything else — and so no import API outlives the one import it existed for.

---

## V6-2. Goals & Non-Goals (delta)

**Goals (add)**

- Reproduce `fin`'s behaviour **exactly** — same inputs, same locked split, same worked example — inside Home as the `finance` module.
- Move **every** month `fin` holds into Home, verified row-for-row against the live service before `fin` is touched.
- Give the module Home's citizenship: Czech UI, Home roles, audit trail, live sync, a Nástěnka widget, metric/list providers, PWA read caching.
- **Retire `fin` cleanly** — a stopped backend, an archived repo, a deprovisioned auth site, and a final DB snapshot kept as provenance. No redirect (D96): two users, both told directly.
- Make "we forgot to enter last month" a thing the app tells you, instead of something you notice in December (the `finance.missing_months` metric + list, §V6-10 D89/D90).

**Non-Goals (v6)** — the first five are `fin`'s own non-goals, carried over unchanged:

- **No categories, transactions, bank sync or forecasting.** Only the monthly income→accounts split.
- **No multi-currency.** Whole-koruna CZK integers.
- **No per-user private budgets.** All month data is household data.
- **No reporting or export** beyond the results view.
- **No change to the locked formula** (§V6-10 D82) — including no decimal rates. Rates stay whole integer percent summing to exactly 100.
- **No generalisation to N people.** Two income slots, named as `fin` names them (§V6-10 D83).
- **No import endpoint.** The seed migration is the only path in (§V6-10 D91).
- **No data left behind in `fin`.** If it is not in Home after §V6-12's verification, `fin` does not get retired.
- **No new platform strand.** `finance` uses `platform/*` exactly as it stands after v5.

---

## V6-3. Users, Roles & Auth (delta)

Roles unchanged (`admin`/`editor`/`reader`, `*` = superuser). Finance follows the **ordinary feature-module gate**, not the admin gate (§V6-10 D84):

- **Reads** (`GET /api/finance/**`): **any authenticated member, including `reader`.**
- **Writes** (`POST`/`PATCH`/`DELETE`): **`editor` or `admin`**, via `httpx.RequireWrite` at mount time — the same gate `todo`, `notes` and `documents` use.
- **Delete** is a plain write — `editor` or `admin`, like create and edit. There is no separate admin tier for it, because there is no soft/hard distinction to gate (§V6-10 D87).

**Accepted consequence, stated plainly:** a `reader` can see both household incomes. In a two-person household where both people are the earners this is not a privacy boundary worth building, and the alternative (a fourth role, or per-module ACLs) is machinery this household does not need. If a `reader` account is ever given to somebody outside the household, Finance is the module to reconsider first. *(Recorded as OQ-V6-4.)*

`fin`'s two auth accounts do **not** move: Kája and Andy already have Home accounts. The `fin` auth site is deprovisioned in §V6-12, not migrated.

---

## V6-4. Functional Requirements (v6)

### Finance module (`finance`) — new in v6

**FR-F1 — List months.** `GET /api/finance/months` returns saved months **newest first**, each with its inputs and its **computed split**, paged with the shared `limit`/`cursor` parameters (the dataset is a few dozen rows; paging exists for uniformity, not need). Reads open to every member.

**FR-F2 — Create a month.** `POST /api/finance/months` with `month` (`YYYY-MM`, unique), `income_kaja` (integer ≥ 0, CZK), `income_andy` (integer ≥ 0, CZK) and `rates {personal, operational, fun, no_fun}` (each integer ≥ 0, **summing to exactly 100**). Persists **inputs only**; computes and returns the split. `409` on a duplicate month, `422` on validation. Audited as `month.create`.

**FR-F3 — The split calculation (LOCKED — do not change).**

The single authoritative formula, confirmed with Karel 2026-06-10 and carried over from `fin` verbatim. Let `T = income_kaja + income_andy`. Rates `p` (personal/wants), `o` (operational/needs), `f` (fun savings), `n` (no-fun savings) are **percentages of `T`** with `p + o + f + n = 100`.

1. `personal_kaja = round(income_kaja × p/100)` — Kája keeps her own `p`% on her personal account.
2. `personal_andy = round(income_andy × p/100)` — Andy keeps his own `p`% on his personal account.
3. `to_operational_kaja = income_kaja − personal_kaja` and `to_operational_andy = income_andy − personal_andy` — each transfers the remaining `(100−p)`% of their **own** income to the joint **operational** account ("Kandy").
4. `operational_received = to_operational_kaja + to_operational_andy`.
5. Two transfers leave the operational account: `fun_savings = round(T × f/100)`, `no_fun_savings = round(T × n/100)`. Savings are **pooled/joint**, never attributed per person.
6. `needs = operational_received − fun_savings − no_fun_savings` — what stays in the operational account, **absorbing all rounding** so per-person and per-account totals reconcile exactly.

**Rounding rule:** whole CZK throughout. Personal is rounded **first** (so `personal + to_operational == income` per person); savings are rounded off `T`; `needs` is the **remainder** (so `fun + no_fun + needs == operational_received`).

**Invariants** — the four `split.go` documents, all carried over. *(`fin`'s `TestInvariants` actually asserts six conditions across 11 fixtures: these four plus `operational_received == to_operational_kaja + to_operational_andy` and `total_income == income_kaja + income_andy`. Port the test as it stands.)*

- `personal_kaja + to_operational_kaja == income_kaja`
- `personal_andy + to_operational_andy == income_andy`
- `fun_savings + no_fun_savings + needs == operational_received`
- `personal_kaja + personal_andy + operational_received == total_income`

**Worked example** — `income_kaja = 60 000`, `income_andy = 40 000` (`T = 100 000`), rates `20/60/10/10`:
`personal_kaja = 12 000`, `personal_andy = 8 000`; `to_operational_kaja = 48 000`, `to_operational_andy = 32 000`, `operational_received = 80 000`; `fun_savings = 10 000`, `no_fun_savings = 10 000`; `needs = 60 000`.

**Known edge case, inherited and deliberately preserved.** Because `needs` is the remainder, it can come out **negative — by at most 2 CZK**. The bound is provable, not empirical: personal rounding moves `operational_received` by at most 1, and each of the two savings roundings by at most ½, so

> `needs ≥ T × o/100 − 2`, and therefore `needs < 0` requires **`T × o < 200`**.

Two readings of that, and both matter:

- **`operational = 0`** — the condition holds at any income, so a household that has told the app nothing is meant to stay in the operational account can see `needs = −1` or `−2` on a perfectly real month. *(Worst case −2: `income_kaja = 6 887 385`, `income_andy = 2 030 905`, rates `90/0/5/5`.)*
- **`operational ≥ 1`** — it requires a **total household income below 200 Kč** (`T < 200/o`), e.g. `(0, 50)` at rates `1/1/1/97` → `needs = −1`. Only reachable with toy inputs, but reachable, so it belongs in the tests.

The four invariants hold in every one of these cases — the totals reconcile exactly, which is precisely what they assert.

This is `fin`'s behaviour and it stays. **Do not clamp `needs` at zero, and do not assert that a negative `needs` implies `operational == 0`**: clamping would silently break `fun_savings + no_fun_savings + needs == operational_received`, the invariant the whole rounding scheme exists to protect. The frontend renders a negative `needs` as **`0 Kč` with a footnote**, and both readings above belong in the test table (§V6-11).

**FR-F4 — Edit a month.** `PATCH /api/finance/months/{id}` — `month`, `income_kaja`, `income_andy`, `rates` all optional; `rates`, **if present, must be the complete four-value block summing to 100** (a rate is meaningless alone). Changing `month` must not collide with another row (`409`). Recomputes and returns the split. Audited as `month.update` **with a field-level diff**.

**FR-F5 — Delete a month.** `DELETE /api/finance/months/{id}` — the row is **removed permanently**, exactly as in `fin` (§V6-10 D87). `204`; `editor`/`admin` + CSRF; the UI confirms first and says plainly that it cannot be undone.

Because nothing is recoverable from the table afterwards, the audit event is the record: `month.delete` writes a **full-row diff** — `month`, both incomes and all four rates, each with its old value and a null new value. A deleted month can therefore still be read back out of the Log and re-entered by hand, and that is the compensating control which makes a hard delete acceptable in a module whose predecessor had no audit trail at all.

**FR-F6 — Rate defaults ("remembered").** The add-month form pre-fills the four rates from the **most recent saved month**; with no months at all it falls back to **20 / 60 / 10 / 10**. Pure frontend behaviour derived from FR-F1 data — **no endpoint** (unchanged from `fin` FR-7).

**FR-F7 — The `finance.rozpocet` Nástěnka widget** (`narrow`, every role, §V6-10 D88). Two states from one provider:

- **Current month recorded** → its headline numbers: total income, what each person keeps, what stays as needs, what went to savings; clicking opens the month in Finance.
- **Current month not recorded** → a prompt naming the month ("Zadat srpen 2026") linking to the add form. This is the state that matters: the app's actual failure mode is a month nobody entered.

**FR-F8 — Metric providers** (household-scoped, §V6-10 D89) — see the catalog table in §V6-4a.

**FR-F9 — List provider** (household-scoped, §V6-10 D90) — `finance.missing_months`, mirroring the metric of the same key.

**FR-F10 — Live sync.** Every mutation publishes a websocket change after commit (`finance.changed`), so a second tab or phone refreshes; the screen shows the standard Czech notice — **"Finance byly mezitím upraveny"** — rather than silently swapping numbers under the reader (the per-module toast pattern the built app already uses).

### V6-4a. Catalog contributions

**Metrics (+4, all household-scoped — 9 → 13):**

| Key | Czech label | Unit | Value |
|---|---|---|---|
| `finance.total_income_current` | Celkový příjem tento měsíc | Kč | `total_income` of the current month; **0** if unrecorded |
| `finance.savings_current` | Spoření tento měsíc | Kč | `fun_savings + no_fun_savings` of the current month; **0** if unrecorded |
| `finance.missing_months` | Nezadané měsíce | měsíců | count of months from the earliest recorded month through the current month with no live row |
| `finance.months_recorded` | Zadaných měsíců | měsíců | count of recorded months |

**Lists (+1 — 8 → 9):** `finance.missing_months` — "Nezadané měsíce", empty string **"nic nechybí"**, items = the Czech month labels of exactly the months the metric counts (D77's rule: same key, same selection, count derived as `len(items)`).

**Audit actions (+3):** `month.create`, `month.update`, `month.delete` — qualified `finance.month.*` in the log browser and the trigger composer. Entity type **`finance_month`** joins the field-diff set (the hybrid rule, D-arch): the diff records income and rate changes, so "who changed May's rates and to what" is answerable — something `fin` never could.

**Why these four metrics.** `finance.missing_months` is the point of the exercise. With v5's conditions (D75) it composes into the notification this household actually wants: a summary on the 1st of each month, *conditioned* on `finance.missing_months gt 0`, whose body lists them — silent in every month where there is nothing to chase. The other three exist so a monthly summary can state the household's own numbers without an admin having to type them.

---

## V6-5. Data Model (v6)

One new table, one new migration block, no change to any existing table. SQLite → Litestream `home/`; **no blob store** (Finance has no bytes).

**`finance_months`** *(finance — block 09)*

| Column | Type | Notes |
|---|---|---|
| `id` | TEXT PK | UUIDv7 — **preserved from `fin` for seeded rows** (D91) |
| `month` | TEXT NOT NULL | `YYYY-MM`; **UNIQUE** |
| `income_kaja` | INTEGER NOT NULL | CHECK ≥ 0, whole CZK |
| `income_andy` | INTEGER NOT NULL | CHECK ≥ 0, whole CZK |
| `rate_personal` | INTEGER NOT NULL | CHECK ≥ 0, whole percent |
| `rate_operational` | INTEGER NOT NULL | CHECK ≥ 0 |
| `rate_fun` | INTEGER NOT NULL | CHECK ≥ 0 |
| `rate_nofun` | INTEGER NOT NULL | CHECK ≥ 0 |
| `created_by` | TEXT NULL | Home user id; **NULL for seeded rows** (`fin` recorded no actor) |
| `created_at` | TEXT NOT NULL | preserved from `fin` for seeded rows |
| `updated_at` | TEXT NOT NULL | preserved from `fin` for seeded rows |

Table-level `CHECK (rate_personal + rate_operational + rate_fun + rate_nofun = 100)` — carried over from `fin`'s `0001_init`, which is worth keeping precisely because it makes a bad seed row fail loudly at migration time rather than quietly at read time.

Indexes: `CREATE UNIQUE INDEX ux_finance_months_month ON finance_months (month)` — a plain unique index, carried over from `fin`'s `0001_init`; with hard delete (D87) there is no soft-deleted row for a partial index to exclude. `CREATE INDEX idx_finance_months_month_desc ON finance_months (month DESC)` for the list and the metrics.

**Derived, never stored:** the whole split block (FR-F3), computed on read.

**Column names are `fin`'s, deliberately** (D83). Only the **table** is namespaced — `finance_months`, not `months` — because Home is one database holding eight modules' tables and a bare `months` would be the most collision-prone name in it.

**Migration order:** `logging(01) → platform(02) → todo(03) → events(04) → dashboard(05) → notes(06) → documents(07) → admin(08) → finance(09)`, then the **seed source** at `09900` (§V6-10 D91). `finance` owns `09001_finance.sql`; the seed is `09900_finance_seed.sql` in a **separate** embedded source.

---

## V6-6. API Surface (v6)

Full detail in `openapi.yaml` **v0.8.0**. All routes session-authenticated; state-changing routes require the CSRF header. New tag: `finance`.

| Route | Method | Gate | FR |
|---|---|---|---|
| `/api/finance/months` | `GET` | member | FR-F1 |
| `/api/finance/months` | `POST` | editor/admin + CSRF | FR-F2 |
| `/api/finance/months/{id}` | `GET` | member | FR-F1 |
| `/api/finance/months/{id}` | `PATCH` | editor/admin + CSRF | FR-F4 |
| `/api/finance/months/{id}` | `DELETE` | editor/admin + CSRF | FR-F5 |

**The wire format is Home's, not `fin`'s** (§V6-10 D92): `snake_case` JSON, `PATCH` for partial update (not `PUT`), the shared `Error {error, detail}`, the shared `Limit`/`Cursor` parameters, the shared `401/403/404/409/422` responses. The *values* are `fin`'s to the koruna; only the envelope conforms. A one-page field mapping for anyone reading `fin`'s code beside this spec:

`incomeKaja→income_kaja` · `incomeAndy→income_andy` · `rates.noFun→rates.no_fun` · `split.totalIncome→split.total_income` · `personalKaja→personal_kaja` · `toOperationalKaja→to_operational_kaja` · `operationalReceived→operational_received` · `funSavings→fun_savings` · `noFunSavings→no_fun_savings` · `needs→needs`.

**Dropped from `fin`'s surface:** `POST /months/import` (replaced by the seed, D91) and `PUT /months/{id}` (superseded by `PATCH`). `fin`'s `/healthz` and `/readyz` are Home's existing probes.

---

## V6-7. Frontend (v6)

**Route** `/finance`, page `src/routes/finance/FinancePage.tsx`; module code in `src/modules/finance/` (widget under `widgets/`), matching the v5 layout convention.

**Navigation** (§V6-10 D84): Finance joins the **"více" overflow for everyone**, beside Dokumenty — the four daily thumb-reachable tabs (Nástěnka, Úkoly, Okno, Poznámky) are unchanged. Finance is a once-a-month destination; it does not earn a tab. Icon: `Wallet` (lucide), consistent with the existing icon language.

**Screens** — the same three `fin` has, re-dressed:

- **Měsíce** — the list, newest first, each row expanding to the flow visualisation. Row summary: month label, the allocation bar (four segments), total income, to savings. Empty / loading / error states per Home's `States` conventions.
- **The flow visualisation** — `fin`'s three-stage layout, kept because it is the reason the app exists (it makes the split legible in one glance rather than as a table of nine numbers): **Příjem** (both incomes) → **Osobní** (what each keeps, and what each sends on) → **Společné účty** (needs remaining in the operational account, plus the two savings transfers). The reconciliation note stays, in Czech.
- **Přidat / upravit měsíc** — month picker (months already recorded are disabled when adding), two income fields, four rate fields with a **live computed preview**, submit blocked until the rates sum to 100. Pre-fills rates per FR-F6.
- **Smazat** — confirm dialog built on Home's **`ResponsiveModal`** (`components/ui/modal.tsx`) with `cs.finance.*` copy, matching how Poznámky and Dokumenty confirm a delete. The copy must say the deletion is **permanent** (D87) — unlike Poznámky and Dokumenty, this one does not archive. *(`fin`'s `DeleteConfirmModal` component does not come across — Home has no such component.)*

**Colour** — the four buckets map onto the theme's existing **categorical palette**: `--c1` osobní · `--c2` potřeby · `--c3` zábavné spoření · `--c4` nezábavné spoření. Both light and dark values already exist in `theme/globals.css` (the Log's stats bars are their current consumer); **no new hex values are introduced** and none are hardcoded in components.

**Formatting** — `monthLabel()` from `i18n/format.ts` already renders `YYYY-MM` as "srpen 2026"; money uses `fmtNumber()` + " Kč" (Czech grouping, non-breaking space). Nothing new needed.

**Data fetching** — query key `['finance','months']`; create/update/delete invalidate it; `['dashboard']` is invalidated alongside so the widget follows. Live sync via `finance.changed` per FR-F10.

**Offline** (§V6-10 D95) — nothing special: reads render from the persisted query cache like every other module, write controls disable with the standard "Změny nelze uložit offline".

### Czech UI vocabulary (§V6-10 D85)

Code ids stay English (D20); this is the display vocabulary, fixed here so the page, the widget, the metric labels and the notification tokens all say the same words.

| Concept (`fin`, English) | Czech UI |
|---|---|
| Finance / the module | **Finance** |
| Months | **Měsíce** |
| Income (in) | **Příjem** |
| Personal / wants | **Osobní** |
| Operational account ("Kandy") | **Provozní účet (Kandy)** — the household's own nickname is kept |
| Needs (stays in operational) | **Potřeby** |
| Fun savings | **Zábavné spoření** |
| No-fun savings | **Nezábavné spoření** |
| Rates | **Sazby** |
| Total income | **Celkový příjem** |
| To savings | **Do spoření** |
| Rest → Kandy | **Zbytek → Kandy** |
| Add / edit month | **Přidat měsíc** / **Upravit měsíc** |

---

## V6-8. Non-Functional Requirements (v6)

- **Observability:** baseline unchanged. The seed migration logs the number of rows it inserted and the number it skipped, so a re-run or a partial import is visible in the deploy log rather than inferred.
- **Correctness:** the split is the acceptance surface. The ported unit test (worked example + four invariants) plus a **property test** over randomised incomes and rate splits asserting the invariants hold — because the invariants, not the individual numbers, are what "the totals reconcile" means.
- **Security:** no new surface. Reads member-gated, writes `editor`/`admin` + CSRF — **delete included** *(corrected during the v6 build: this line and D84 below said "hard delete admin-only", which predates OQ-V6-1's resolution. With a hard delete there is no soft/hard distinction left to gate, so D87, FR-F5, §V6-3, §V6-6 and `openapi.yaml` all specify an ordinary `editor`/`admin` write. The build follows those; there is no admin-only route in the module.)*. Money values are integers — no float ever reaches the database, and the only float in the system is inside `math.Round` in the formula.
- **Performance:** a few dozen rows. The split is computed per row on read, which at this scale is free; the metrics are single indexed aggregates.
- **Backup:** the new table rides Litestream `home/`. No new bucket, no new prefix. **`fin`'s final `fin/` snapshot is retained**, not deleted, as the seed's provenance (§V6-12).
- **Reliability of the move:** the seed is `INSERT OR IGNORE` against the live-row unique index, so applying it to a database that already holds the months is a no-op rather than a duplicate-key failure.

---

## V6-9. Configuration (v6)

**No new environment variables.** Finance has no secrets, no external service, no tunables. `HOME_TIMEZONE` (`Europe/Prague`) already governs the "current month" boundary the widget, metrics and list depend on.

---

## V6-10. Decisions (D81–D98)

- **D81 — v6 is a clone, not a port.** `finance` is a new Home feature module with `fin`'s behaviour; `fin`'s session store, auth client, JWT plumbing, English UI and import endpoint are **dropped, not migrated**. Additive: auth, the dashboard-host contract, `platform/*` and the seven existing modules are untouched.
- **D82 — The split formula is LOCKED and carried over verbatim** (FR-F3), including the rounding order and the four invariants, with `fin`'s worked-example test. The split stays **derived on read, never stored**.
- **D83 — `fin`'s column vocabulary is carried over literally** — `income_kaja`, `income_andy`, `rate_personal`, `rate_operational`, `rate_fun`, `rate_nofun`. Only the **table** is namespaced (`finance_months`), because Home is one database holding eight modules' tables. No generalisation to person A/B or to N people: it would add a translation layer between the formula, the seed and the tests for no behavioural gain.
- **D84 — Finance is an ordinary module, open to all roles.** Reads for every member including `reader`; writes `editor`/`admin`, ~~hard delete `admin`~~ **delete included — no separate admin tier** *(corrected 2026-08-17 during the build; the `admin` clause predates OQ-V6-1 and contradicts D87/FR-F5/the OpenAPI)*. **Not** admin-gated, unlike `admin`/`logging`. It sits in the **"více" overflow for everyone**, beside Dokumenty — a once-a-month destination does not earn one of the four thumb tabs.
- **D85 — Czech UI vocabulary is fixed in the spec** (§V6-7 table) so the page, the widget, the metric labels and the notification tokens use one set of words. The joint account keeps the household's own nickname, **Kandy**.
- **D86 — Finance joins the audit spine.** Every mutation writes an audit event in the same transaction; actions `month.create` / `month.update` / `month.delete`; entity type `finance_month` joins the **field-diff** set. This is a capability `fin` never had, and it is what makes a rate correction traceable.
- **D87 — Delete is hard, exactly as in `fin`** *(Karel, 2026-08-17, resolving OQ-V6-1)* — no `deleted_at`, no `?hard=true`, a plain `UNIQUE(month)`, and an ordinary write gate. A deliberate departure from Home's soft-delete convention (`notes`, `documents`), taken because the convention buys little here: a month is a single row of seven numbers that takes twenty seconds to re-enter, and carrying a nullable column plus a filter on every read and every catalog query to protect it is not a trade worth making twelve times a year. **The compensating control is the audit spine** — `month.delete` writes a full-row diff (D86), so the deleted numbers stay readable in the Log. The UI must state that the delete is permanent.
- **D88 — One Nástěnka widget, `finance.rozpocet`** (narrow, all roles), with **two states**: the current month's headline split, or a "Zadat ⟨měsíc⟩" prompt when the current month has no row. The second state is the point — the app's real failure mode is a month nobody entered. *(Not `finance.tento-mesic`: `events` already publishes a "Tento měsíc" widget and two identically-titled catalog entries would be a usability bug.)*
- **D89 — Four household-scoped metrics** (§V6-4a). All household, none personal: a household budget has no per-recipient value.
- **D90 — One list, `finance.missing_months`**, mirroring the metric of the same key exactly (D77's rule), Czech empty string "nic nechybí".
- **D91 — Historic data arrives as a one-off Goose seed in its OWN migration source.** `finance/seed` (block `09900`) is a separate `registry.MigrationSource` that the production composition includes and **`platform/testsupport` excludes** — otherwise every module test would run against a database pre-loaded with thirty months of real household finances. Rows are `INSERT OR IGNORE`, **ids/timestamps preserved from `fin`**, `created_by` NULL. No import endpoint is built, so none has to be removed later.
- **D92 — The wire format is Home's, not `fin`'s**: `snake_case`, `PATCH` not `PUT`, `/api/finance/months`, the shared `Error`/paging/response components. Values identical to the koruna; only the envelope conforms. Field mapping in §V6-6.
- **D93 — Rate defaults stay a frontend behaviour** — pre-fill from the latest month, else 20/60/10/10, no endpoint (`fin` FR-7 unchanged).
- **D94 — Live sync like every module** — `finance.changed` published after commit, Czech toast "Finance byly mezitím upraveny".
- **D95 — No special PWA handling.** Reads come from the persisted query cache; write controls disable offline. Finance has no bytes, so D73's document rule does not apply.
- **D96 — `fin` is retired in a fixed order, gated on verification, with NO redirect** *(Karel, 2026-08-17, resolving OQ-V6-3)* (§V6-12): tell both users → verify → stop backend → archive repo → deprovision the auth site. `fin.tilcer.cz` is **not** redirected and the subdomain is released; anyone on an old bookmark or installed shortcut gets a dead host. With a user base of two people who both know the app moved, a redirect app running indefinitely to catch a mistake neither will often make is not worth the standing infrastructure. **Consequence to respect:** the redirect would also have been the fallback if something surfaced *after* the cutover, so §V6-12's verification is now the only gate — do not stop `fin` until it passes clean.
- **D97 — Retirement is gated on a row-for-row verification** of `fin`'s live output against Home's, **including the recomputed split**, not just the inputs. Comparing inputs alone would not catch a formula that was ported wrong — which is the one mistake this migration can actually make.
- **D98 — `fin`'s spec documents are recovered into `services/fin/` before the repo is archived.** That folder is currently **empty**; `fin`'s PRD, OpenAPI spec and handoff exist only inside the repo about to become read-only. Archiving the repo without this would leave the retired service's contract nowhere in the project's own record.
- **D99 — The `pripominky` summary tokens follow the REMINDER's window, not the event's date** *(Karel, 2026-08-18)*. `events.pripominky_today` / `_today_open` — metric **and** list, which must not part ways over a shared key — resolve through the Připomínky widget's **own** selection: a "připomínka na dnešek" is a **current** widget row — lead open (`occurrence − reminder_lead ≤ today`), day not yet passed. `_open` drops completed rows (the widget's non-overdue rows verbatim); the plain key also keeps a row ticked off while still current — completing it does not unsay it was today's reminder. The old event-date reading answered a question nobody asks a reminder app: the rent due next Wednesday, whose 1w lead opens this morning, was exactly what the 08:00 summary left out. *(Amended same day: an earlier cut selected `occurrence − reminder_lead == today` — the day the lead opens. That missed an event created after its lead had already opened, dropped every open reminder from the summary between its first morning and the day it turned overdue, and could name an occurrence the dashboard was not showing; anchoring to the widget's rows closes all three.)* Every line is **dated** (`Zaplatit nájem (22. 7.)`), because the occurrence is not today's by construction. `events.due_within_7d` is deliberately untouched: a look-ahead is a question about the calendar.
- **D100 — `events.pripominky_active`: the Připomínky widget, in words** *(Karel, 2026-08-18)*. A list-only key (no metric twin — "how many rows are on the dashboard" is a number nobody asks for) naming **every active reminder including overdue**, one line per event, overdue first then by date. It resolves through the widget's **own** selection — the extracted `activeReminders`, which the widget now calls too — rather than a second implementation shaped to agree with it: "aktivní" is a four-part rule (reminder-enabled, uncompleted, lead open, once per event) and each part is a place two copies could drift.

**Resolution map (Karel, 2026-08-17):** OQ-V6-1 → **hard delete, as in `fin`** (D87 rewritten; the audit full-row diff is the compensating control) · OQ-V6-2 → **"více" overflow for everyone**, the four thumb tabs untouched (D84 stands) · OQ-V6-3 → **no redirect** — `fin.tilcer.cz` is switched off outright (D96 rewritten) · OQ-V6-4 → **accepted**, a `reader` sees both household incomes (§V6-3 stands).

**Open questions: none.** The spec is ready for the design addendum and the build.

---

## V6-11. Acceptance Criteria (v6)

- [ ] **The split matches `fin` exactly.** The ported unit test passes on the worked example (`60 000` / `40 000`, `20/60/10/10` → `12 000` / `8 000` / `48 000` / `32 000` / `80 000` / `10 000` / `10 000` / `60 000`), and a property test over randomised inputs holds all four invariants.
- [ ] The **negative-`needs` edge case** is tested in **both** its forms — `operational = 0` at a realistic income, and `operational ≥ 1` with `T × o < 200` — asserting `needs ≥ −2`, that the invariants still hold, that nothing clamps it, and that the UI shows `0 Kč` with a footnote.
- [ ] `finance` registers through `registry.Module` — routes, migrations, `AuditActions()`, one widget provider — and `internal/arch`'s **`TestModulesDoNotImportEachOther`** stays green.
- [ ] The three **non-registry-driven maps** are updated: the frontend widget registry (`platform/widgets/registry.tsx`), the log browser's `MODULES` filter array, and `admin/listener.go`'s `inAppURL` — each silently no-ops if skipped.
- [ ] Migrations run `… admin(08) → finance(09)`, apply cleanly on an empty DB and after a Litestream restore.
- [ ] **The seed source is excluded from `testsupport`** — a module test's database contains no seeded months (verified by a test that asserts an empty `finance_months` on a fresh test DB).
- [ ] Months CRUD: duplicate month → `409`; rates ≠ 100 → `422`; negative income → `422`; bad `month` format → `422`; a partial `rates` block → `422`.
- [ ] Delete is **permanent**: the row is gone from the table, the list, the widget and both catalogs; the same month can then be re-created; the `month.delete` audit event carries a **full-row diff** so the numbers stay readable in the Log; the confirm dialog says it cannot be undone.
- [ ] Every mutation writes an audit event **in the same transaction**, `month.update` carrying a field-level diff; `finance.month.*` appears in the log browser filter and the trigger composer.
- [ ] `finance.rozpocet` appears in the widget catalog for **every** role, renders both states, and the "Zadat ⟨měsíc⟩" state links to the add form.
- [ ] All four metrics resolve through the provider contract (no cross-module import); `finance.missing_months` list and metric agree by construction; a v5 condition `finance.missing_months gt 0` gates a summary correctly.
- [ ] Frontend: Měsíce list, flow visualisation, add/edit with live preview and rate pre-fill, delete confirm; Czech vocabulary per §V6-7; the four buckets use `--c1…--c4` with **no new or hardcoded colour values**; nav shows Finance in "více" for all roles.
- [ ] Live sync: a change on one device shows "Finance byly mezitím upraveny" on another.
- [ ] Offline: months render read-only from cache; write controls disabled.
- [ ] OpenAPI **0.8.0** validates; new paths/schemas reuse the shared `Limit`/`Cursor`/`responses`/security components.
- [ ] **Verification (§V6-12) passes row-for-row against live `fin`, split included, before any retirement step is taken.**
- [ ] `fin` retired in order (D96); `REGISTRY.md` shows `fin` retired and `home` at v6 / 0.8.0.

---

## V6-12. Data migration & `fin` retirement

> **Steps 1–2 are DONE (2026-08-17).** The export was taken from the live service (**15 months, `2025-06`…`2026-08`, no gaps**), the seed was generated, **every row's split was re-derived with the locked formula and matched the live values exactly**, and the file applied cleanly and idempotently to a database carrying the `09001` schema. Artefacts live in **`services/home/v6-seed/`** (`fin-months-export.json` · `gen_seed.py` · `09900_finance_seed.sql` · `verify_migration.py` · `README.md`). Steps 3–6 remain.

### Step 1 — Export (before anything is built against it)

Export `fin`'s months from the **live service**, not from a backup, so what is seeded is what people actually see:

```
POST https://fin.tilcer.cz/login          # site `fin`, as Kája or Andy → { access_token }
GET  https://fin.tilcer.cz/months         # Authorization: Bearer <access_token>
```

**A cookie session is not enough** — `/months` sits behind `RequireBearer`, so a plain authenticated GET returns 401. The token is short-lived; refresh via `POST /token/refresh` if the export outlives it.

Save the JSON verbatim as the migration's provenance (**done — `services/home/v6-seed/fin-months-export.json`**). The `sqlite3 .dump` of `fin`'s volume is an acceptable second source and a useful cross-check, but the API response is the reference because it is the thing the users have been looking at.

### Step 2 — Generate the seed

A small generator (**`v6-seed/gen_seed.py`**) turns the export into `09900_finance_seed.sql`: one `INSERT OR IGNORE` per month, **preserving `id`, `created_at` and `updated_at`**, `created_by` NULL. The generated file is committed — it is data, not code, and it must be reviewable in a diff. Guard rails the generator enforces:

- every row's four rates sum to exactly 100 (else the table CHECK rejects the whole migration — loudly, which is the intent);
- incomes are non-negative integers;
- `month` matches `^\d{4}-(0[1-9]|1[0-2])$` and is unique across the file;
- the row count matches the export's length.

### Step 3 — Deploy Home v6 with `fin` still running

Both services live. `fin` remains the reference for as long as verification takes.

### Step 4 — Verify (the gate, D97)

Compare `fin`'s `GET /months` against Home's `GET /api/finance/months`, **row for row and field for field**, after mapping camelCase→snake_case:

- same set of `id`s, same count;
- `month`, `income_kaja`, `income_andy` and all four rates equal;
- **every one of the nine computed split values equal** — this is the part that catches a mis-ported formula, and it is why comparing inputs alone is not enough;
- `created_at` preserved.

Shipped as **`v6-seed/verify_migration.py`**, self-tested against a clean export (exit 0) and one with a single split value off by 1 (exit 1, naming the month and field). **Any mismatch stops the retirement.**

### Step 5 — Retire, in order (D96)

0. **Tell Kája and Andy the app has moved** — `home.tilcer.cz/finance`. With **no redirect** (D96) this is the entire migration comms plan, and it has to happen before the host goes dark, not after. If either has `fin.tilcer.cz` installed as a phone shortcut, have them remove it and install Home instead.
1. **Stop the `fin` backend and frontend** Coolify apps and release the `fin.tilcer.cz` route. Litestream replication to `fin/` stops with the backend; take a **final snapshot first**.
2. **Retain `fin/` in R2** — do not delete the prefix. It is the seed's provenance and costs nothing.
3. **Archive `ws-tilcer-fin`** (GitHub read-only) — *after* D98's document recovery.
4. **Deprovision the `fin` auth site** and its two `single_site` accounts, and drop **`FIN_AUTH_SERVICE_SECRET` from `fin`'s own Coolify env** — it is fin's variable, not auth's. ⚠ **Auth's `AUTH_SERVICE_SECRET` must NOT be touched**: it is the shared BE→BE secret `home`, `status` and `karel` also use. Last, because it is the one step that would break the old app if verification somehow had to be re-run.
5. **`REGISTRY.md`** — `fin` row → **retired**, with the date and a pointer to Home's `finance` module.

### Step 6 — Document recovery (D98)

`services/fin/` in the project folder is **empty**: `fin`'s PRD, OpenAPI spec and handoff live only in the repo about to be archived. Copy `handoff/PRD.md`, `handoff/openapi.yaml` and `handoff/HANDOFF.md` into `services/fin/` and mark them *retired — superseded by home v6* before step 5.4.
