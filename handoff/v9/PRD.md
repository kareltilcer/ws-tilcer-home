# PRD — Home

> Status: **v9 — SPEC** (Soukromé položky a Úložiště, drafted 2026-08-21 — **not built**) · **v8 — LIVE** (Elektřina / `electricity`, built and deployed 2026-08-21) · **v7 — LIVE** (Zahrada / `garden`, built and deployed 2026-08-21) · **`fin` is not yet retired** — the migration is **verified** (§V6-12 step 4 done) and only the ordered switch-off (step 5) is outstanding. **v9 is the first version of Home that adds no module**: Poznámky and Dokumenty each gain a **second root** — the uploader's own, invisible to everyone else including admins — and Administrace gains an **Úložiště** page plus a **Soukromé položky** purge screen. It changes `notes`, `documents` and `admin`, and adds one platform strand, `platform/storage`. v8 added a tenth module, **Elektřina** (`electricity`) — VT/NT readings, a date-versioned ceník and zálohy answering *vyjdou zálohy, nebo doplatím?*. v7 added a ninth, **Zahrada** (`garden`). v6 added an eighth, **Finance** (`finance`) — absorbing the standalone `fin` service (`fin.tilcer.cz`) and migrating its data. v5 added the **Administrace** (`admin`) module (Web Push notifications) and an installable, reads-only-offline **PWA** on top of v4 (Dokumenty). Self-hosted login (Mode B), widget dashboard and modular architecture unchanged throughout. Decisions **D1–D215** (§10 for D1–D50; the **v5 section** for D51–D74; **§V5-12 for D75–D80 — shipped after the v5 spec froze**; the **v6 section** for D81–D98, with **D99/D100 taken during the v6 build**; **§V6-13** = the v6 as-built reconciliation; the **v7 section** for D101–D132, with **§V7-12 = the v7 as-built reconciliation, D163–D168**; the **v8 section** for D133–D162, of which **D157–D160 were forced back into the spec by the `HANDOFF-10` pass** and **D161–D162 by the implementation-planning pass**, with **§V8-12 = the v8 as-built reconciliation, D169–D175**; the **v9 section** for **D176–D215**, of which **D206–D213 were forced back into the spec by a review pass against the shipped code** and **D196/D208/D214/D215 were then settled with Karel**); v-deltas in `CHANGELOG.md`. **Ten modules live; v9 adds none.** · Owner: Karel · Last updated: 2026-08-21
> Companion spec: `openapi.yaml` (OpenAPI 3.1 — **0.11.0 = the v9 SPEC**, not yet built; **0.10.1 = v8 AS BUILT, and is what is deployed**; 0.10.0 was the v8 spec, 0.9.0 the v7 spec, 0.8.0 the v6 build) · Notes: `notes.md` · Design brief: `HANDOFF-design.md` (v2–**v9** addenda; the v6 palette question is **RESOLVED — Path A**, the v8 nudge-escalation question is **RESOLVED by the build — D175**) · Build: `HANDOFF-1…11-*.md` (v5 = `HANDOFF-7-admin.md`, v6 = `HANDOFF-8-finance.md`, v7 = `HANDOFF-9-garden.md`, v8 = `HANDOFF-10-electricity.md` — all built; **v9 = `HANDOFF-11-privacy-storage.md`, not yet built**) · v9 scope brief: `V9-privacy-storage-brief.md` (frozen 2026-08-21)
>
> ⚠ **v9 is not like v3–v8, and reading it as though it were will produce a broken build.** Every release from v3 to v8 was additive — a module arrived through the registry and nothing already working had to be re-examined. v9 alters four tables that have carried real household data since v3 and v4, and invalidates an assumption roughly forty call sites were written against: *until v9, every row in `notes` and `documents` was visible to every member*. **§V9-4a is the leak table** — twenty-three surfaces where "private" can escape, twenty-two of which deny and one of which (an admin's hard delete) grants. It had eighteen rows until a review pass found five more, so treat its length as a floor. It is the requirement, not a summary of it, and it is the test plan.

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

- **v8 (spec 2026-08-20 · deployed 2026-08-21)** — adds the **Elektřina** (`electricity`) module, a tenth: irregular two-register meter readings (**VT**/**NT**) whose meaning rests on one rule — a reading is the meter state at 00:00 of its date, so day *d* = `reading(d+1) − reading(d)` (D134) — priced by a **ceník versioned by effective date** whose three numbers (cena VT, cena NT, měsíční poplatky, all s DPH a distribucí) govern only their own days (D135/D136), against **zálohy** held as a schedule with a due day plus optional real payments (D144/D155) over **user-set, never-locked settlement periods** whose end date may be merely expected (D139/D153). The output is a **nedoplatek/přeplatek prediction** from a plain average since the period start, measured to the last reading rather than to today (D141), pricing each future day with the ceník effective that day (D142) and pro-rating poplatky by days (D143). Its defining choice is strictness: a price change inside a reading interval **blocks** that interval rather than estimating it (D137) — **money is never interpolated**, though the history chart may interpolate kWh for its columns (D138) and prices them by allocating exact interval costs (D159). Five tables in block **11**, no seed, **no widget, no metric, no list, no push, no scheduler job, no blob storage** (D147/D152) — the first home module that contributes nothing to Nástěnka or the notification catalogs, so only **three** of the four non-registry host maps are touched and `platform/widgets/registry.tsx` deliberately is not. Everything computed on read by one pure `compute.go`, integer haléře and tenths of kWh (D148). OpenAPI → **0.10.0** (106 paths → 119, 203 schemas → 235; a new `DateCursor` generalising `finance`'s month-key precedent — D149), which also closes two stale enum gaps (`filter_module` and `WidgetCatalogEntry.module` were both missing `finance` and `garden`). Decisions **D133–D162**; **D157–D160 were forced back into the spec by the `HANDOFF-10` pass** and **D161–D162 by the implementation-planning pass** (nullable totals; the served 30/70 headroom mix), all marked as such. Resolved brief: `V8-electricity-brief.md`. **As built: OpenAPI → 0.10.1** (119 paths, **236** schemas) — the reconciliation also closed a defect predating v8, that `backend/openapi.yaml` had never left **0.8.0** and described neither `garden` nor `electricity`. Decisions **D169–D175** were taken during the build (§V8-12), two of them in the follow-up PRs #18 and #19 — the log's empty-collection contract and server-formatted log summaries.
- **v7 (spec 2026-08-18 · deployed 2026-08-21)** — adds the **Zahrada** (`garden`) module, a ninth: a two-level crop knowledge base (druh → odrůda, nullable overrides resolved by one function — D103) with **anchored timing windows** (`week` / `last_frost` / `first_frost`, D102), beds whose **lexorank order within a zone IS the adjacency model** (D117), a per-season plan whose checks **C1–C11** are computed on read and never block a save (D108/D109), and a **garden-owned task engine** with od–do windows and conservative regeneration (D101/D110). Plus a harvest log, a produce-only storage log (D121), perennials as plantings with `season_id IS NULL` (D106), an explicit **Uzavřít sezónu** ritual that is the *only* source of rotation history — so C3/C8 return `no_history` at first (D120) — copy-season with a family shift previewed by `dry_run` (D129), an **LLM prompt/import/export** whose schema is generated from the importer's own validator (D114/D126), and a **print view** (D125). One widget `garden.prace` (D123), **six metrics and six lists** (two of each key-only to their own side), **31 audit actions**. Eleven new tables in block **10** plus a rule seed at `10900` excluded from `testsupport` (D115); **no blob storage** (D122). The one external dependency is a keyless Open-Meteo forecast polled by the v5 scheduler (D112) — and **the module sends no push**: it publishes two catalog keys and one idempotent `garden.frost_warning` audit event per night, leaving audience and delivery to **Administrace** (D113). OpenAPI → **0.9.0** (34 paths → 106; seasons addressed by `{year}` — D128), which shipped inside **0.10.1** — the v7 build never updated `backend/openapi.yaml`. Decisions **D101–D132**, plus **D163–D168 during the build** (§V7-12), where the **export↔import round trip is recorded as NOT shipped**: the export is a superset, and import accepts crops only.
- **v6 (spec 2026-08-17 · deployed 2026-08-18)** — adds the **Finance** (`finance`) module, a functional clone of the standalone **`fin`** service, which v6 then **retires**. `fin`'s locked split formula and column vocabulary cross over verbatim (D82/D83); its session store, JWT plumbing, English UI and import endpoint are dropped. Finance is an ordinary all-roles module in the "více" overflow (D84), joins the audit spine with `finance.month.*` and a field-level diff (D86), **hard-deletes as `fin` did — with a full-row audit diff as the compensating control** (D87), and contributes one widget (`finance.rozpocet`, D88), four household metrics and one list (D89/D90) — including `finance.missing_months`, which with v5 conditions turns "we forgot to enter last month" into a notification. One new table `finance_months` (block 09); the historic months arrive as a **one-off Goose seed in its own migration source**, excluded from `testsupport` (D91). OpenAPI → **0.8.0** (snake_case, `PATCH`, `/api/finance/months` — D92), which is also what shipped. `fin` retirement is **gated on a row-for-row verification including the recomputed split** (D97), then — with **no redirect** (D96) — stop → archive → deprovision; `fin`'s own spec documents were recovered into `services/fin/` first (D98). Decisions **D81–D98**, plus **D99/D100 during the build**; as-built reconciliation in **§V6-13**. Design addendum `HANDOFF-design.md` §v6, **palette resolved as Path A**.
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

---

## V6-13. As built — the v6 build (2026-08-17/18, OpenAPI **0.8.0**, deployed 2026-08-18)

> v6 was built from this spec in one pass and deployed on 2026-08-18 (repo `main`: PR #14 `4f8a719` — the module; PR #15 `87cccdf` — D99/D100). **The contract did not move: 0.8.0 is both the spec and the deployed build.** The two decisions taken during the build change catalog *semantics*, and the catalogs are discovered at runtime and never enumerated in `openapi.yaml`. Everything in §V6-1…§V6-12 still holds — this section records the deltas.

### Corrections made to this document during the build

- **§V6-6 and D84's "hard delete `admin`"** was a leftover from before OQ-V6-1 was resolved. With a hard delete there is no soft/hard distinction left to gate, so **delete is an ordinary `editor`/`admin` write** — which is what D87, FR-F5, §V6-3 and `openapi.yaml` already said. There is no admin-only route in the module; both lines are corrected inline above.
- **The finance keyset cursor is a `YYYY-MM` month key, not a UUIDv7.** `GET /api/finance/months` orders by `month` — ordering by id would put a back-filled month in the wrong place — so it deliberately does **not** reuse the shared `Cursor` parameter; an id passed here would be compared lexically against `month` and silently return a wrong or empty page. `openapi.yaml` now documents the parameter (and `next_cursor`) inline, and a non-`YYYY-MM` cursor is refused with **422** instead of silently re-serving page one.

### Behaviour hardened during the build

- **`finance.missing_months` is floored at 36 months**, so one mistyped year cannot stretch the metric — and the list that shares its computation — over a century of months.
- **The in-row allocation bar is `aria-hidden`.** The month row is a button, and its accessible name was otherwise four bucket labels and four amounts long; the values stay available in the row's own label and in the detail view.
- **The widget's "Zadat ⟨měsíc⟩" opens the add form**, as its label promises, rather than dropping the user on the month list.
- **The four host-side maps that are not registry-driven** were all updated for `finance`: `admin/listener.go`'s `inAppURL` (→ `/finance`), the frontend widget registry, `AppShell`'s "více" overflow, and the Log's hardcoded module filter — which also gained the **`admin` and `platform`** entries it had been missing since v5. Each of these silently no-ops when a new module forgets it: treat the list as part of the module checklist.
- **Palette — Path A** (resolved by Karel 2026-08-17, recorded in `HANDOFF-design.md` §v6). `--c1`…`--c5` keep their values, so the Log's stats bars are untouched, and Finance's buckets are **aliases**: `--fin-personal: var(--c1)` · `--fin-needs: var(--c2)` · `--fin-fun: var(--c4)` · `--fin-nofun: var(--c3)` — no new colour value enters the codebase and the `.light` block re-steps them for free. Because Path A's all-pairs CVD weakness remains, **secondary encoding is mandatory and is implemented as shipped**: the O/P/Z/N marks, a distinct swatch shape per bucket, the 2 px surface gaps in the bar, an always-present legend, direct labels on every flow-viz card, and an `aria-label` naming each bucket and its value. **Colour never carries a bucket on its own** — anything added to Finance later must keep that rule.

### One product change outside Finance (D99, D100)

The `events` summary keys were re-defined in the same version: `events.pripominky_today` / `_today_open` now resolve through the Připomínky widget's own selection rather than the occurrence date, and the list-only `events.pripominky_active` was added — see **§V6-10 D99/D100**. Nothing in Finance depends on them; they are noted here because they shipped as part of v6. Catalog totals after v6: **13 metrics, 10 lists**.

### Verification carried out at build time

`TestComputeMatchesFinLiveExport` runs the ported formula over the committed 15-row `fin` export and matches **all nine split values for every month** — the §V6-12 step-4 comparison, executed before deployment rather than after it. The split additionally carries `fin`'s worked example, six invariants over 11 fixtures, a 20 000-case property test, odd-money cases and **both** forms of the negative-`needs` edge case (asserting nothing clamps it). A separate test asserts that a `testsupport.NewDB()` database holds **zero** months while a `MigrationFSWithSeed()` one holds **15** (D91).

### Retirement status (updated 2026-08-21)

§V6-12 **steps 1–4 are done** — export, seed, deploy, and the live re-export from both services through `v6-seed/verify_migration.py`, which is the **D97 gate and it passed**. **Step 6** (document recovery into `services/fin/`) was done in the spec round. **Only step 5 is outstanding**: the ordered switch-off with **no redirect** (D96). `fin.tilcer.cz` is still running.

---

> **v7 scope:** one new self-contained module, **Zahrada** (`garden`) — a kitchen-garden knowledge base, planner and work calendar. Four capabilities the household actually asked for:
> 1. **Plodiny** — a structured crop knowledge base at two levels (druh → odrůda): when to sow, when to plant out, how to grow, when to harvest, how to store — with an **LLM prompt template** that generates a fillable JSON record, an import that previews before it applies, and an export that round-trips.
> 2. **Záhony a plán** — beds, and a per-season assignment of crops to beds.
> 3. **Kontrola plánu** — companion, rotation, capacity, frost and workload checks that fire **while you plan**, per bed and across the whole garden.
> 4. **Kalendář a práce** — the work the plan implies (příprava záhonu → výsev → pikýrování → otužování → výsadba → sklizeň → zpracování → uskladnění → úklid), generated as dated tasks, ticked off from a Nástěnka widget.
>
> Plus: a **harvest log** that compares real yields against the crop's expected yield, a **storage log** for what went to the cellar and freezer, **perennials and fruit trees** in the same model as annuals, an **"Uzavřít sezónu"** ritual that turns a finished year into rotation history, and a **print view** for taking the month's work into a garden with no signal.
>
> v7 is **additive**: no change to auth, the dashboard-host contract or the eight existing modules, and **one additive exported hook in `platform/scheduler`** — `RegisterJob`, so the weather poll rides the ticker v5 already built instead of starting a second one inside a feature module (§V7-4 FR-G15). Two shapes are worth naming up front because they are what keep it additive. First, `garden` **owns its tasks** rather than writing into `todo` or `events` — §10 D25/D28 forbid the import, and neither a card nor an event can express a work *window* bound to a planting (§V7-10 D101). Second, `garden` **sends no push and owns no audience**: it publishes a metric, a list and one audit event per night, and **Administrace** decides who hears about a frost (§V7-10 D113). The v5 notification machinery is reused exactly as built, not re-implemented one module over.

---

## V7-1. Overview (delta)

- **One-line summary (add):** a module that holds what the household knows about its crops, plans them into beds season by season, warns while you plan when the plan is wrong, and turns the plan into dated garden work.
- **Type / subdomain / exposure / consumers:** **unchanged** from v6. No new BE→BE call, no new bucket, no new container.
- **Depends on (add):** one **outbound HTTP dependency** — a public weather forecast API (Open-Meteo: free, keyless, no account). It is *soft*: every failure is logged and swallowed, and the module degrades to manual frost dates (§V7-10 D112).
- **Modules after v7: nine.** `logging`, `todo`, `events`, `notes`, `documents`, `dashboard`, `admin`, `finance`, **`garden`** (new). **The other eight are untouched.**
- **Catalogs after v7:** widgets 7 (+1), metrics 19 (+6), lists 16 (+6), audit actions +12. Every one of those is a registration, not a host edit.
- **Scale target:** up to **~15 beds and ~40 crops** (§V7-10 D116 context). The API still pages by the shared `Limit`/`Cursor` convention; the *UI* is what gets to be simple — one grid of bed cards, no virtualisation, no pagination controls.

### Architecture — the module owns its work calendar (§V7-10 D101)

The obvious idea is to make garden work into `todo` cards or `events` occurrences, so everything lands in one list. It is the wrong idea here, for three separate reasons, and it is worth writing them down because the idea will come back:

1. **The module boundary forbids it.** §10 D25/D28: modules do not import each other's internals, and `internal/arch`'s `TestModulesDoNotImportEachOther` fails the build on the attempt. Reuse would mean inventing a *new* platform-level task contract, adding `source_module`/`source_key` columns to `cards`, and syncing completion in two directions — a change to three packages to avoid a change to one.
2. **The shape doesn't fit.** Garden work is a **window** ("výsadba rajčat: 15.–25. 5.") anchored to a planting, not a due date. A card has no window and no planting link; an event has a single date, an RRULE and no bed.
3. **The volume doesn't fit.** One season on a fifteen-bed garden generates on the order of 100–200 items. Poured into Úkoly or Připomínky they would drown the household's own lists.

So `garden` registers like every other module — routes, migrations (block **10**), audit actions, one widget provider, metric and list providers — and its tasks live in its own table. What *is* shared is the surfacing: the Nástěnka widget and the metrics/lists catalogs, which is how garden work reaches a notification without a line of push code in the module.

### Architecture — timing is anchored, not dated (§V7-10 D102)

A crop's sowing window is not a date; it is a *relation*. "Rajčata: výsev 6–8 týdnů před posledním mrazem" is true every year, in every garden; "výsev 15. března" is true in one garden in one year. But Czech garden literature — and Karel's own head — states most windows as calendar weeks, and forcing everything into frost-relative form would be pedantry.

So every window in the knowledge base is a triple `{anchor, from, to}` with **three anchors**, mixable per window and per crop:

| Anchor | `from`/`to` mean | Example |
|---|---|---|
| `week` | ISO week numbers, 1–53 | výsev do sadbovačů: 10 → 13 |
| `last_frost` | days relative to the season's last spring frost | −56 → −42 |
| `first_frost` | days relative to the season's first autumn frost | −14 → −3 |

When a season's frost dates change, **frost-anchored windows move and week-anchored ones do not** — which is exactly the intent in both cases. Resolution to real dates happens against the season, never against "today", so the same planting resolves identically no matter when the page is loaded.

### Architecture — the plan is a plan, not a tracker (§V7-10 D119)

Planned dates and actual dates are both recorded, and **actuals never re-drive the plan**. Sow two weeks late and the harvest window stays where February put it.

This was a deliberate choice over the seductive alternative (recompute everything downstream from `days_to_maturity`), because a calendar that silently reshuffles itself is a calendar you stop trusting. The compensating control is that the drift is never *silent*: the planting detail states it in words — *"vyseto o 14 dní později, sklizeň v plánu beze změny"* — and offers a single action, **"posunout navazující práce o 14 dní"**. Taking it marks the moved tasks `is_edited`, which by D110 means regeneration will never touch them again.

### Architecture — warnings are computed, dismissals are stored (§V7-10 D108)

The check is a **pure function of the plan**, run on read, exactly as RRULE expansion is in `events` and the split is in `finance`. Nothing about a warning is persisted except the fact that you told it to be quiet: `garden_warning_dismissals` holds a stable `warning_key` derived from (rule id, bed id, sorted planting ids, season). Change the plan enough that the key changes and the warning correctly returns.

Two consequences the UI must honour. A warning **never blocks a save** (D109) — the planner is a thinking tool, and a garden that violates a companion rule on purpose is a legitimate garden. And a check that *cannot run* must say so rather than pass: with no closed seasons behind it, the rotation checks C3 and C8 have no history, and the panel says **"rotaci zatím nelze zkontrolovat, chybí historie"** instead of showing an unearned green tick (§V7-10 D120).

### Architecture — Zahrada sends nothing; Administrace sends everything (§V7-10 D113)

The frost alert is the module's one time-critical output, and it would have been easy — and wrong — to give `garden` its own push call, its own audience picker and its own quiet hours. v5 already built all three, generically, in `admin`.

So `garden` imports **no `platform/push`** and stores **no audience**. It publishes three things and stops:

- metric **`garden.frost_risk_tonight`** — the forecast minimum °C for tonight;
- list **`garden.frost_sensitive_now`** — the tender/half-hardy plantings currently in the ground, each line naming crop and bed;
- one audit event **`garden.frost_warning`** per night, idempotent on the date, whose Czech `summary` already reads as a complete notification.

That gives Karel both delivery mechanisms, chosen in **Administrace → Oznámení** at runtime rather than in this document: a **scheduled summary** conditioned `garden.frost_risk_tonight lte 2` with body `{{list.garden.frost_sensitive_now}}` — silent on every night there is nothing to say, the same shape as the `finance.missing_months gt 0` nudge — or a **trigger rule** on `garden.frost_warning`, which fires the moment the poll flips and defaults its body to the event's summary.

---

## V7-2. Goals & Non-Goals (delta)

**Goals (add):**

- Hold the household's crop knowledge in a form that can be *queried and computed on*, not just read: fixed enums for family, feeder class and hardiness; numeric spacings, yields and maturities; anchored timing windows. Free prose exists (`notes_md`) but carries no logic.
- Make a wrong plan visible **during planning**, not in July. Companion conflicts, rotation breaches, over-booked beds, frost-risky transplant dates, out-of-window sowings and week-12-has-thirteen-sowings workload spikes.
- Turn the plan into work automatically, and keep that work honest under change: regeneration that cannot destroy what you have already done or deliberately edited.
- Make filling the knowledge base cheap enough to actually happen, via an LLM round trip that is **validated and previewed**, never trusted blind.
- Reuse the v5 notification machinery rather than growing a second one.

**Non-goals (v7):**

- **Seed inventory (osivo)** and any shopping list derived from it.
- **A drawn garden map**; adjacency is inferred from bed order (D117), not from coordinates.
- **Photos** of any kind, and therefore **no blob storage in `garden`** (D122).
- **A general household pantry**; the storage log covers garden produce only (D121).
- **Offline writes.** Home's PWA stays reads-only offline; the print view (D125) is the answer for a garden with no signal.
- **Bed sub-sections** (D116), **auto-generated watering/weeding** (D118), **green manure as a modelled crop** (D127), **a second widget** (D123), and **back-filling historical seasons** (D120).
- **An automatic planner.** v7 warns about an assignment you made; it does not propose one.

---

## V7-3. Users, Roles & Auth (delta)

Unchanged from v6. `garden` is an **ordinary all-roles module** in the "více" overflow, like Finance:

- **Reads** — any authenticated member, `reader` included: the knowledge base, beds, the plan, the check, the calendar, harvests, storage.
- **Writes** — `editor`/`admin` + CSRF. **Ticking a task off is an ordinary write** (§V7-10 D124): no role exception was created for it, so `reader` stays read-only everywhere in the module including the widget's completion control.
- **Admin-only** — exactly one route: re-opening a closed season (§V7-10 D120), because a closed season is rotation history and re-opening it rewrites the record the checks depend on.

No new session, scope or claim. No route is public.

---

## V7-4. Functional Requirements (v7)

Every mutating requirement records an audit event through the spine **in the same transaction**; stated once, not repeated. Reads are not logged.

### Garden module (`garden`) — new in v7

#### FR-G1: Crop knowledge base (druh)
- **Trigger:** member opens Plodiny; `editor` creates or edits a crop.
- **Inputs:** `name_cs` (required), `name_latin`, `family` (**required, enum**), `plant_type` (enum), `hardiness` (**required**, enum `tender`/`half_hardy`/`hardy`), `feeder_class`, `root_depth`, `sun`, `water_need`, `soil_ph_min/max`, `rotation_break_years`, `sow_method`, `needs_pricking_out`, `needs_support`, `wants_mulch`, `wants_pest_check`, `hardening_days`, `sow_depth_cm`, `spacing_row_cm`, `spacing_plant_cm`, `plants_per_m2`, `days_to_germinate_min/max`, `germination_temp_c`, `days_to_maturity_min/max`, four timing windows, `harvest_unit`, `yield_per_m2`, `yield_per_plant`, `storage_methods[]`, `storage_temp_c`, `storage_humidity`, `shelf_life_days`, `pests[]`, `diseases[]`, `notes_md`.
- **Behaviour:** `family` and `hardiness` are required because the rotation engine and the frost logic respectively cannot function without them (§V7-10 D104, D111) — a crop with neither is not a crop the module can reason about. `rotation_break_years` defaults from the family when omitted. `plants_per_m2` derives from the two spacings when omitted and is overridable. Names, `notes_md` and pest/disease names go into FTS5.
- **Outputs:** the crop, with every field resolved and its provenance block.
- **Errors:** `422` missing/invalid enum, negative measurement, `from`/`to` reversed in a window, or an ISO week outside 1–53; `409` duplicate `name_cs`.

#### FR-G2: Varieties (odrůda) and the resolution function
- **Description:** a variety belongs to exactly one species and **overrides only what differs**. Every timing, spacing, maturity, yield and storage field is nullable on the variety; `NULL` means inherit.
- **Behaviour:** the effective value of any field for a (plant, variety) pair is *variety value if non-null, else species value* — implemented **once**, in one exported function, with its own unit test covering a full-inheritance case and a full-override case (§V7-10 D103). The planner, the task generator, the check and the widget all read through it. Four independent re-implementations of "when do we sow Black Krim" is a bug nobody would ever find.
- **Errors:** `422` an override that is invalid in its own right; `404` unknown plant.

#### FR-G3: LLM prompt template, import, export
- **Trigger:** member clicks "Vygenerovat prompt" on a new or existing crop; pastes JSON back.
- **Behaviour:**
  1. `GET /api/garden/plants/prompt-template` returns a ready Czech prompt containing **the JSON schema generated from the importer's own validator** (§V7-10 D114) — one registry produces the validator, the schema in the prompt and `/api/garden/enums`, so the three cannot drift — plus the household's context (Czech climate, the garden's frost dates and altitude) and an instruction to answer with JSON only.
  2. `POST /api/garden/plants/import?dry_run=true` parses, validates, normalises and returns a **preview**: the resulting record, a field-by-field diff when it updates an existing crop, and an explicit list of input fields it could not map. Enum matching is lenient — Czech words map to enum members — but an unmappable enum is a `422` naming the offending field and value, **never** a silent default.
  3. The same call with `dry_run=false` applies it, recording `source=llm`, `source_model`, `source_at`. The crop is badged **"neověřeno"** until a member sets `verified_by`/`verified_at`.
  4. A JSON **array** is accepted, so twenty crops arrive in one paste; each element is validated independently and the response reports per-element status. The same path serves varieties and rules.
  5. `GET /api/garden/export` emits plants, varieties and rules in **the shape the importer accepts** (§V7-10 D126), so an export re-imports to an equivalent state — a readable backup and a way to move the knowledge base between installs.
- **Errors:** `422` unparseable JSON, schema violation, or unmappable enum; `413` payload over the configured cap.

#### FR-G4: Beds (záhony), their order, and their history
- **Inputs:** `name`, `code`, `type` (enum), `length_cm`, `width_cm`, `area_m2` (derived, overridable), `sun_exposure`, `zone`, `soil_notes_md`, `is_active`.
- **Behaviour:** beds are ordered by lexorank **within a zone**, and that order **is** the adjacency model (§V7-10 D117): two beds are neighbours iff they are consecutive in the same zone. Dragging beds into the order they physically stand in is the whole of the data entry, and check C11 becomes possible without a coordinate system, a drawing surface or a neighbour table. `GET /api/garden/beds/{id}/history` returns, per past closed season, which family occupied the bed — the read model behind the rotation column in the planner.
- **Errors:** `422` non-positive dimensions; `409` deleting a bed that has plantings in an open season (delete the plantings or close the season first).

#### FR-G5: Seasons, copying, and the rotation shift
- **Behaviour:** a season is a year with expected frost dates and a status (`planning` → `active` → `closed`). `POST /api/garden/seasons` creates one, optionally `copy_from` a previous year with an optional `shift`:
  1. copy every non-permanent planting;
  2. if `shift` is given, rotate bed assignments by that offset over the ordered active beds — either per planting or **per family block**, so a family moves together;
  3. re-anchor planned dates against the **new** season's frost dates: frost-anchored windows move, week-anchored windows stay (D102);
  4. run the check and return it.
- **`dry_run=true` returns the whole prospective season plus its check, without persisting** (§V7-10 D129) — the same preview idiom as the import. The UI shows what the shift fixed and what it broke, side by side, **before** the season exists. A copy that silently reproduces last year's rotation error is worse than typing the year in by hand.
- **Errors:** `409` the year already exists; `422` unknown `copy_from`, or a shift larger than the bed count.

#### FR-G6: Plantings (výsadba)
- **Inputs:** `season_id` (**NULL ⇒ permanent**, §V7-10 D106), `bed_id` (nullable) or `location_label`, `plant_id`, `variety_id`, exactly one of `area_m2` / `plant_count`, `rows`, the five planned dates, the four actual dates, `status`, `notes_md`; for permanents also `planted_on`, `rootstock`, `removed_on`.
- **Behaviour:** planned dates default from the resolved KB windows against the season's frost dates, and each carries a `*_is_manual` flag so a re-anchor never clobbers a date the user typed. A planting **belongs to the season of its harvest** (§V7-10 D105) — česnek sown in October 2026 is a 2027 planting, and its sow date legitimately falls in the previous calendar year.
- **The occupancy window** — first of (`sowed_on` ∥ `sow_direct_on` ∥ `transplant_on`) → (`cleared_on` ∥ `harvest_to`) — is what "shares a bed" means everywhere in the module (§V7-10 D107). Spring špenát and autumn pórek in one bed never meet and must not warn.
- **Drift and the manual shift:** recording an actual date that differs from the plan changes **no** planned window (D119). The detail view states the drift in Czech and offers `POST /api/garden/plantings/{id}/shift-tasks` with a day offset, which moves the planting's remaining open tasks and marks them `is_edited`.
- **Errors:** `422` both or neither of `area_m2`/`plant_count`, a planting in a closed season, or a permanent planting with a `season_id`; `404` unknown plant/variety/bed.

#### FR-G7: Task generation and regeneration
- **Trigger:** any change to a planting's dates, quantity, crop or variety; season creation; crop timing changes.
- **Behaviour:** the generator derives tasks from the planting and the resolved KB record — the kinds and their sources are tabulated in §V7-5. Each generated task carries `generation_key` = hash(planting id, kind, occurrence index) and `is_generated = true`.
  On regeneration the generator may **move the window of an open, unedited, generated task**. It **never** touches a task that is `done`, `skipped` or `is_edited`, and a generated task the user deleted leaves a **tombstone** so it does not resurrect on the next recompute (§V7-10 D110). Manual tasks are never touched at all.
- **Not generated (§V7-10 D118):** `water` (zálivka) and `weed` (plení) exist only as manual kinds. A cadence of chores nobody ticks off is how a task list loses its credibility, and both are decided by looking at the garden rather than at a list.
- **Outputs:** the affected tasks; the count of created/moved/left-alone is in the audit event's meta.

#### FR-G8: The work calendar and completion
- **Behaviour:** `GET /api/garden/tasks` filters by date range, status, kind, bed, planting and season, ordered by `window_from` then lexorank. Manual tasks are created and edited freely. `POST /api/garden/tasks/{id}/complete` is **idempotent** (§V7-10 D131) and mirrors `events`' completion semantics — completing an already-complete task is a `200`, not a `409` — because the widget's hold gesture can fire twice on a bad connection. `reopen` is its inverse; `skipped` is set through `PATCH`.
- **Errors:** `403` `reader`; `404` unknown task; `409` mutating a task in a closed season.

#### FR-G9: Kontrola plánu — the check
- **Trigger:** `GET /api/garden/seasons/{year}/check`, called by the planner on every plan change and by the metric/list providers.
- **Behaviour:** eleven checks, computed on read, each returning `key`, `severity`, Czech `title` and `detail`, and the entities it points at. C1 companions (over overlapping occupancy), C2 same family in a bed, C3 rotation, C4 bed over-booked, C5 workload spike, C6 family concentration, C7 empty active bed, C8 feeder succession (+ the legume tip), C9 frost-risky transplant, C10 planned date outside the crop's window, C11 adjacent-bed companions and seed-saving cross-pollination. Severities are per rule and configurable in settings; a check can be disabled entirely.
- **Dismissal:** `POST …/check/dismissals` with a key and an optional note silences one warning **for that season**; `DELETE` restores it. Without dismissal you stop reading the panel by April, and the panel is the feature.
- **History-dependent checks:** C3 and C8 read closed seasons only. With none, they return the explicit state **`no_history`** rather than a pass (§V7-10 D120), and the UI renders "rotaci zatím nelze zkontrolovat, chybí historie".
- **A warning never blocks a save** (D109). The check is advisory, always.

#### FR-G10: Uzavřít sezónu
- **Trigger:** `POST /api/garden/seasons/{year}/close`.
- **Behaviour:** one screen collects what the year actually did — final yields per planting, which plantings failed and why, the observed frost dates — then sets `status=closed`, stamps `closed_at`/`closed_by`, and **the season becomes rotation history**. Closed seasons are read-only to `editor`; `POST …/reopen` is **admin-only** and audited, because re-opening rewrites the record C3 and C8 depend on.
- **No historical back-fill** (§V7-10 D120): there is no importer for years before the module existed. Rotation checks are therefore structurally silent in the first season and only sharp from the third — stated in the UI rather than hidden.
- **Errors:** `409` already closed / not closed; `403` non-admin reopen.

#### FR-G11: Sklizeň — the harvest log
- **Inputs:** `planting_id`, `harvested_on`, `quantity`, `unit` (defaulted from the crop's `harvest_unit`, so the form never asks), `destination` (čerstvé / sklad / darováno / kompost), `quality`, `note`.
- **Behaviour:** harvests sum per planting into an actual yield, shown against the KB's expected `yield_per_m2 × area` on the planting detail and at season close. `garden.harvest_season` sums the season in kg (rows in other units are excluded from the metric and the UI says so).
- **Errors:** `422` non-positive quantity, harvest date before the planting's sow date, or a unit the crop does not use.

#### FR-G12: Sklad — storage and processing
- **Inputs:** `harvest_id` / `planting_id` (either, both, or neither for a batch you can't attribute), `product_name`, `method`, `location`, `quantity_initial`, `quantity_remaining`, `unit`, `stored_on`, `best_before`, `status`, `note`.
- **Behaviour:** consumption is recorded by **editing `quantity_remaining` in place** (§V7-10 D121); there is no movements table, because the audit spine's field diffs already answer "when did we eat the last jar" and a second table would buy only a chart. `status` moves to `consumed` automatically when remaining reaches zero, and to `spoiled` by hand.
- **Errors:** `422` remaining above initial, or negative.

#### FR-G13: Pravidla — compatibility and succession rules
- **Behaviour:** one table, three scopes — `plant_pair` (two crop ids), `family_pair` (two family enums), `succession` (predecessor → successor with `min_years_gap`). Pairs are stored in canonical order and matched both ways, so symmetry is structural rather than a discipline. **An explicit `plant_pair` beats a `family_pair`** — that precedence is the reason both scopes exist.
- **Built-ins (§V7-10 D115):** the botanical families with their default break years plus ~50–80 sourced Czech companion pairs ship as `10900_garden_seed.sql` — a **separate embedded migration source, excluded from `testsupport`**, `INSERT OR IGNORE`, exactly the v6 seed pattern. A built-in rule can be **disabled** but not deleted (`409`, §V7-10 D130); user rules can be deleted outright. Every rule carries its `source` string, so folklore and agronomy are told apart by looking.

#### FR-G14: Nastavení zahrady
- **Behaviour:** one row: default frost dates, `latitude`/`longitude`/`altitude`, default rotation break years, frost threshold °C and look-ahead days, workload-spike threshold, per-check enable/severity. Coordinates live here and **not** in Coolify — they are not a secret and they belong next to the frost dates they serve (§V7-10 D112). **No audience settings** (D113).

#### FR-G15: Weather poll and frost publication
- **Trigger:** `platform/scheduler`, twice daily, through a new generic **`RegisterJob(name, every, fn)`** hook — the package's only v7 change. Its existing summary firing is untouched; the alternative, a second ad-hoc ticker inside a feature module, is precisely what v5 created this package to avoid.
- **Behaviour:** fetch the forecast for the settings' coordinates in `HOME_TIMEZONE`, upsert `garden_weather_days` (retention ~90 days). Then evaluate: if tonight's forecast minimum ≤ the configured threshold **and** at least one planting whose crop is `tender`/`half_hardy` has an open occupancy window, write **one** `garden.frost_warning` audit event, idempotent on the date, whose Czech `summary` names temperature, crops and bed codes. The autumn mirror publishes `garden.first_frost_in_days`.
- **The module does not send a push** (§V7-10 D113); the event and the two catalog keys are its entire output. Delivery, audience, quiet hours and coalescing are Administrace's, unchanged from v5.
- **Errors:** none user-visible. A failed fetch is logged and swallowed; the page renders from cache or without weather, and never shows an error the user cannot act on.

#### FR-G16: "Práce na zahradě" widget provider
- **Provides** widget `garden.prace` (§V7-10 D123): tasks whose window overlaps the next 30 days, **overdue first**, then grouped by ISO week; each line carries crop, bed code and window. Mark-done calls `POST /api/garden/tasks/{id}/complete` with `meta.via="dashboard"` via the house **2000 ms hold** gesture (§10 D22). Empty state: *"na zahradě je teď klid"*. There is no second widget — harvest surfaces here as a `harvest` task, so nothing is lost.

#### FR-G17: Print
- **Behaviour:** one print stylesheet, two targets (§V7-10 D125): **this month's work** — tasks with real checkboxes, bed codes and windows, grouped by week — and **the season plan** on one page. It is the deliberate answer to reads-only-offline in a garden with no signal; nothing about the PWA rule changes.

### V7-4a. Catalog contributions

**Metrics (+6, all household-scoped — 13 → 19):**

| Key | Czech label | Unit | Value |
|---|---|---|---|
| `garden.tasks_due_7d` | Práce na zahradě (7 dní) | úkolů | open tasks whose window overlaps the next 7 days |
| `garden.tasks_overdue` | Zmeškané práce | úkolů | open tasks whose `window_to` is before today |
| `garden.plan_warnings` | Varování v plánu | varování | non-dismissed check results of severity `warn` or `error` in the current season |
| `garden.harvest_season` | Letošní sklizeň | kg | Σ harvest quantity in kg for the current season |
| `garden.beds_unplanned` | Nezaplánované záhony | záhonů | active beds with no planting in the current season |
| `garden.frost_risk_tonight` | Noční minimum | °C | forecast minimum for tonight; **null** when no forecast is cached |

**Lists (+6 — 10 → 16):** the four countable keys `tasks_due_7d`, `tasks_overdue`, `plan_warnings`, `beds_unplanned` mirror their metrics exactly (D77: same key, same selection, count = `len(items)`), plus two **list-only** keys on the D100 precedent — **`garden.harvest_ready`** (plantings inside their harvest window; empty string *"nic není ke sklizni"*) and **`garden.frost_sensitive_now`** (tender/half-hardy plantings in the ground, formatted *"rajčata (A1)"*; empty string *"nic citlivého venku"*).

**Audit actions (+12):** `plant.*`, `variety.*`, `bed.*`, `season.create/update/close/reopen`, `planting.*`, `task.*`, `harvest.*`, `storage.*`, `rule.*`, `settings.update`, and the system-written **`frost_warning`** — qualified `garden.*` in the log browser and the trigger composer. Entity types **`garden_plant`**, **`garden_planting`** and **`garden_task`** join the field-diff set: "who moved the tomato transplant date and to what" is the question the Log exists to answer here.

**Why these six metrics.** `garden.plan_warnings` and `garden.frost_risk_tonight` exist to be **conditions**, not decoration — the first gates a February planning nudge that stays silent once the plan is clean, the second gates the frost alert entirely (D113). `garden.tasks_due_7d` turns a Monday summary into an actual week plan; `garden.tasks_overdue` is the one that will nag in August. `garden.beds_unplanned` is the March version of `finance.missing_months`.

---

## V7-5. Data Model (v7)

Eleven new tables, one new migration block (**10**), **no change to any existing table**. SQLite → Litestream `home/`. **No blob store** (§V7-10 D122) — `garden` is the second module after `finance` to hold no bytes.

House conventions throughout: UUIDv7 `id`, lexorank `position` where users order things, soft delete (`deleted_at`), `created_by` / `created_at` / `updated_at`, English identifiers, Czech UI.

**`garden_plants`** *(the knowledge base, species level)*

| Column | Type | Notes |
|---|---|---|
| `id` | TEXT PK | UUIDv7 |
| `name_cs` | TEXT NOT NULL | UNIQUE among live rows |
| `name_latin` | TEXT NULL | |
| `family` | TEXT NOT NULL | **enum** — the rotation join key (D104) |
| `plant_type` | TEXT NOT NULL | `vegetable` · `herb` · `fruit_tree` · `fruit_bush` · `perennial` · `flower` |
| `hardiness` | TEXT NOT NULL | `tender` · `half_hardy` · `hardy` — **required**, the frost logic reads it (D111) |
| `feeder_class` | TEXT NULL | `heavy` · `medium` · `light` · `fixer` |
| `root_depth`, `sun`, `water_need` | TEXT NULL | enums |
| `soil_ph_min`, `soil_ph_max` | REAL NULL | |
| `rotation_break_years` | INTEGER NULL | defaults from `family` when NULL |
| `sow_method` | TEXT NOT NULL | `direct` · `seedling` · `both` · `vegetative` |
| `needs_pricking_out` | INTEGER NOT NULL | 0/1 |
| `needs_support`, `wants_mulch`, `wants_pest_check` | INTEGER NOT NULL | 0/1 — the three **care flags** the generator reads to decide whether the `support` / `mulch` / `pest_check` tasks exist for this crop. Overridable per variety like everything else. |
| `hardening_days` | INTEGER NULL | |
| `sow_depth_cm`, `spacing_row_cm`, `spacing_plant_cm` | REAL NULL | CHECK > 0 |
| `plants_per_m2` | REAL NULL | derived from the spacings when NULL |
| `days_to_germinate_min/max`, `days_to_maturity_min/max` | INTEGER NULL | CHECK min ≤ max |
| `germination_temp_c` | INTEGER NULL | |
| `win_sow_indoor_*`, `win_sow_direct_*`, `win_transplant_*`, `win_harvest_*` | TEXT/INTEGER NULL | four windows × (`anchor`, `from`, `to`) — anchors `week` · `last_frost` · `first_frost` (D102) |
| `harvest_unit` | TEXT NOT NULL | `kg` · `ks` · `l` · `svazek` |
| `yield_per_m2`, `yield_per_plant` | REAL NULL | |
| `storage_methods` | TEXT NULL | JSON array of enum members |
| `storage_temp_c`, `storage_humidity`, `shelf_life_days` | INTEGER NULL | |
| `pests`, `diseases` | TEXT NULL | JSON array of `{name, symptom, remedy_md}` |
| `notes_md` | TEXT NULL | Markdown, in FTS |
| `source`, `source_model`, `source_at`, `verified_by`, `verified_at` | TEXT NULL | provenance (D114) |
| `deleted_at`, `created_by`, `created_at`, `updated_at` | | house columns |

Indexes: unique partial on `name_cs` where `deleted_at IS NULL`; `(family)`; `(plant_type)`. **`garden_plants_fts`** (FTS5 over `name_cs`, `name_latin`, `notes_md`, pest/disease names) + triggers, mirroring the `notes`/`documents` pattern.

**`garden_varieties`** — `plant_id` FK, `name`, `supplier`, `description_md`, `is_favourite`, `retired`, plus a **nullable mirror of every timing / spacing / maturity / yield / storage column** above. NULL = inherit (D103). Unique `(plant_id, name)` among live rows.

**`garden_beds`** — `name`, `code`, `type`, `length_cm`, `width_cm`, `area_m2`, `sun_exposure`, `zone`, `soil_notes_md`, `is_active`, `position` (lexorank). Index `(zone, position)` — **that index is the adjacency model** (D117).

**`garden_seasons`** — `year` INTEGER **UNIQUE**, `status` (`planning`/`active`/`closed`), `last_frost_on`, `first_frost_on`, `last_frost_actual_on`, `first_frost_actual_on`, `closed_at`, `closed_by`, `notes_md`.

**`garden_plantings`**

| Column | Type | Notes |
|---|---|---|
| `id` | TEXT PK | UUIDv7 |
| `season_id` | TEXT NULL | **NULL ⇒ permanent** (D106) |
| `bed_id` | TEXT NULL | with `location_label` for things outside beds |
| `plant_id` | TEXT NOT NULL | |
| `variety_id` | TEXT NULL | |
| `area_m2` / `plant_count` | REAL / INTEGER NULL | CHECK exactly one is non-null |
| `rows` | INTEGER NULL | |
| `sow_indoor_on`, `sow_direct_on`, `transplant_on`, `harvest_from`, `harvest_to` | TEXT NULL | planned |
| `*_is_manual` | INTEGER NOT NULL | per planned date — a re-anchor skips manual ones |
| `sowed_on`, `transplanted_on`, `first_harvest_on`, `cleared_on` | TEXT NULL | actual; **never re-drive the planned dates** (D119) |
| `status` | TEXT NOT NULL | `planned` → `sown` → `planted` → `growing` → `harvesting` → `done` \| `failed` |
| `fail_reason` | TEXT NULL | |
| `planted_on`, `rootstock`, `removed_on` | | permanents only |
| `notes_md` | TEXT NULL | |

Indexes: `(season_id, bed_id)`, `(bed_id)`, `(plant_id)`, and a partial index on `season_id IS NULL` for the Trvalky view. The **occupancy window** (D107) is derived, never stored.

**`garden_tasks`**

| Column | Type | Notes |
|---|---|---|
| `id` | TEXT PK | UUIDv7 |
| `kind` | TEXT NOT NULL | enum, table below |
| `season_id`, `planting_id`, `bed_id` | TEXT NULL | |
| `title_cs` | TEXT NOT NULL | generated or user-authored |
| `window_from`, `window_to` | TEXT NOT NULL | the od–do window; `due_hint` optional inside it |
| `status` | TEXT NOT NULL | `open` · `done` · `skipped` |
| `completed_by`, `completed_at` | | |
| `is_generated`, `is_edited` | INTEGER NOT NULL | |
| `generation_key` | TEXT NULL | hash(planting, kind, occurrence) — the regeneration identity (D110) |
| `suppressed` | INTEGER NOT NULL | the tombstone: a generated task the user deleted |
| `notes_md`, `position` | | |

Indexes: `(window_from)`, `(status, window_to)`, `(planting_id)`, unique partial on `generation_key` where not suppressed.

**Task kinds and their generation source:**

| id | Czech | Generated from |
|---|---|---|
| `bed_prep` | Příprava záhonu | first planting date − lead |
| `sow_indoor` | Výsev do sadbovačů | `sow_indoor` window |
| `prick_out` | Pikýrování | sowing + `days_to_germinate`, when `needs_pricking_out` |
| `harden_off` | Otužování | transplant − `hardening_days` |
| `sow_direct` | Přímý výsev | `sow_direct` window |
| `transplant` | Výsadba | `transplant` window |
| `thin` | Protrhávání | direct sowings only |
| `support` | Opora a vyvazování | when `needs_support` |
| `feed` | Přihnojení | feeder class + days after planting |
| `mulch` | Mulčování | when `wants_mulch` |
| `pest_check` | Kontrola škůdců | monthly across occupancy, when `wants_pest_check` |
| `prune` | Řez | perennials, yearly window |
| `spray` | Postřik | perennials, yearly window |
| `harvest` | Sklizeň | `harvest` window |
| `process` | Zpracování | after first harvest, when storage methods exist |
| `store` | Uskladnění | after `process` |
| `clear` | Úklid záhonu | after `harvest_to` |
| `water` | Zálivka | **manual only** (D118) |
| `weed` | Plení a okopávka | **manual only** (D118) |
| `other` | Jiné | manual |

**`garden_harvests`** — `planting_id`, `harvested_on`, `quantity` REAL, `unit`, `destination`, `quality`, `note`. Index `(planting_id)`, `(harvested_on)`.

**`garden_storage_items`** — `harvest_id` NULL, `planting_id` NULL, `product_name`, `method`, `location`, `quantity_initial`, `quantity_remaining`, `unit`, `stored_on`, `best_before`, `status`, `note`. CHECK `quantity_remaining` between 0 and `quantity_initial`. Index `(status, best_before)`.

**`garden_rules`** — `scope` (`plant_pair`/`family_pair`/`succession`), `a_ref`, `b_ref`, `verdict`, `severity`, `min_years_gap`, `reason_cs`, `source`, `is_builtin`, `is_disabled`. Pairs stored in canonical (sorted) order; unique `(scope, a_ref, b_ref)`.

**`garden_warning_dismissals`** — `season_id`, `warning_key`, `dismissed_by`, `dismissed_at`, `note`. Unique `(season_id, warning_key)`. Warnings themselves are **never stored** (D108).

**`garden_settings`** — a single row (CHECK `id = 1`): default frost dates, `latitude`, `longitude`, `altitude`, `rotation_break_default`, `frost_threshold_c`, `frost_lookahead_days`, `workload_week_threshold`, `checks_config` (JSON: per-check enabled + severity).

**`garden_weather_days`** — `day` TEXT PK, `temp_min`, `temp_max`, `precip_mm`, `fetched_at`, `source`. Retention ~90 days, pruned by the same scheduler job that writes it.

**Derived, never stored:** the occupancy window · every check result · the effective (species+variety) field values · actual yield per planting · `plants_per_m2` when omitted.

**Migration order:** `logging(01) → platform(02) → todo(03) → events(04) → dashboard(05) → notes(06) → documents(07) → admin(08) → finance(09) → garden(10)`, then the **rule seed** at `10900` in a **separate embedded source**, excluded from `testsupport` (§V7-10 D115) — the v6 pattern, for the same reason: a module test's database must contain no seeded rules, or a check fixture would pass for the wrong reason.

---

## V7-6. API Surface (v7)

Full detail in `openapi.yaml` (**0.9.0**). All routes are session-authorized; writes additionally require CSRF and `editor`/`admin`. Reads are open to `reader`. **34 new paths**, taking the contract from 72 to **106 paths** and from 124 to **203 schemas**.

> **⚠ As built (2026-08-21):** the shipped contract is **0.10.1**, and several things below were reconciled to the code — seven endpoints documented filters that do not exist, `season` is really `year`, beds and seasons are unpaged, `/tasks` uses a composite cursor, dismissals return the recomputed check, and `GardenVariety.effective` has its own schema. **See §V7-12.**

| Group | Paths |
|---|---|
| Knowledge base | `GET|POST /api/garden/plants` · `GET|PATCH|DELETE /api/garden/plants/{id}` · `GET /api/garden/plants/prompt-template` · `POST /api/garden/plants/import` · `GET /api/garden/export` |
| Varieties | `GET|POST /api/garden/plants/{id}/varieties` · `GET|PATCH|DELETE /api/garden/varieties/{id}` |
| Beds | `GET|POST /api/garden/beds` · `GET|PATCH|DELETE /api/garden/beds/{id}` · `POST /api/garden/beds/{id}/move` · `GET /api/garden/beds/{id}/history` |
| Seasons | `GET|POST /api/garden/seasons` · `GET|PATCH /api/garden/seasons/{year}` · `POST …/close` · `POST …/reopen` (admin) |
| Check | `GET /api/garden/seasons/{year}/check` · `POST /api/garden/seasons/{year}/check/dismissals` · `DELETE …/dismissals/{key}` |
| Plantings | `GET|POST /api/garden/plantings` · `GET|PATCH|DELETE /api/garden/plantings/{id}` · `POST …/shift-tasks` |
| Tasks | `GET|POST /api/garden/tasks` · `GET|PATCH|DELETE /api/garden/tasks/{id}` · `POST …/complete` · `POST …/reopen` |
| Harvest & storage | `GET|POST /api/garden/harvests` · `GET|PATCH|DELETE /api/garden/harvests/{id}` · `GET|POST /api/garden/storage` · `GET|PATCH|DELETE /api/garden/storage/{id}` |
| Rules & settings | `GET|POST /api/garden/rules` · `GET|PATCH|DELETE /api/garden/rules/{id}` · `GET|PATCH /api/garden/settings` |
| Reference | `GET /api/garden/weather` · `GET /api/garden/enums` |

Conventions, and the three deliberate choices inside them:

- **Seasons are addressed by `{year}`, not by id** (§V7-10 D128). A season's identity *is* its year: unique, immutable, user-visible, and the planner URL `/zahrada/plan/2027` maps 1:1 onto the API. This is a knowing deviation from "canonical operations by stable id" (§10), taken for exactly one entity where the natural key is better than the surrogate.
- **`dry_run` is the shared preview idiom** (§V7-10 D129) — on the import and on season creation. It returns the same response shape the real call would, plus a diff, and persists nothing.
- **Pagination** uses the shared `Limit`/`Cursor` (UUIDv7 keyset) components on every collection. Unlike `finance` (§V6-6), nothing here keysets on a natural key.
- `POST /api/garden/tasks/{id}/complete` is **idempotent** (D131), like `events`' completion.
- Deleting a **built-in** rule is `409`; disabling it is a `PATCH` (D130).

---

## V7-7. Frontend (v7)

Eight routes, added to the "více" overflow as one nav destination (**Zahrada**) with in-page sub-navigation:

| Route | Page |
|---|---|
| `/zahrada` | **Přehled** — the season at a glance: bed cards with what's in them, warning badge, this month's work, harvest to date |
| `/zahrada/plodiny` | **Plodiny** — KB list with FTS search, detail, editor, prompt/import/export; varieties nested under the species |
| `/zahrada/zahony` | **Záhony** — beds, dimensions, drag-order (**which is the adjacency**), per-bed rotation history |
| `/zahrada/plan/{rok}` | **Plán** — the planner: bed-by-bed assignment, inline warnings per bed, the Kontrola plánu panel, copy-season with preview, Uzavřít sezónu |
| `/zahrada/kalendar` | **Kalendář** — work by month and week, filterable by bed/crop/kind; the print target |
| `/zahrada/sklizen` | **Sklizeň** — harvest entry, yields against expected |
| `/zahrada/sklad` | **Sklad** — stored produce, best-before, remaining |
| `/zahrada/trvalky` | **Trvalky a dřeviny** — permanent plantings and their yearly care |

At ~15 beds the planner is a single grid of bed cards with a crop picker — no two-pane layout, no virtualisation, no pagination controls. TanStack Query keys are `['garden', <resource>, …]`; a planting mutation invalidates the season's plan, its check and the affected tasks together, because a stale check is worse than no check.

Colours come from `--c1…--c5` under the **Path A** aliasing resolved in `HANDOFF-design.md`, with **mandatory secondary encoding** — "colour by botanical family" is exactly the chart that fails the CVD all-pairs test, so family is always also a label or a pattern, never colour alone.

Offline: every page renders read-only from the persisted TanStack Query cache; write controls disable. **Print** (D125) is the offline answer for the garden itself.

### Czech UI vocabulary (§V7-10 D132)

Code ids stay English (D20); this is the display vocabulary, fixed here so the pages, the widget, the metric labels and the notification tokens all say the same words.

| Concept (code, English) | Czech UI |
|---|---|
| Garden / the module | **Zahrada** |
| Plant (species) | **Plodina** |
| Variety | **Odrůda** |
| Botanical family | **Čeleď** |
| Bed | **Záhon** |
| Zone | **Část zahrady** |
| Season | **Sezóna** |
| Planting | **Výsadba** |
| Permanent planting | **Trvalka / dřevina** |
| Plan check | **Kontrola plánu** |
| Warning / dismiss | **Varování** / **Ignorovat** |
| Task | **Práce** |
| Sowing window | **Termín výsevu** |
| Last / first frost | **Poslední jarní mráz** / **první podzimní mráz** |
| Hardiness: tender / half-hardy / hardy | **citlivá** / **polootužilá** / **otužilá** |
| Feeder class | **Nárok na živiny** |
| Harvest | **Sklizeň** |
| Storage | **Sklad** |
| Close the season | **Uzavřít sezónu** |
| Unverified (LLM-sourced) | **Neověřeno** |

---

## V7-8. Non-Functional Requirements (v7)

- **Observability:** baseline unchanged. The weather job logs each poll's outcome (fetched / cached / failed) and the seed migration logs rows inserted vs skipped, so a partial import is visible in the deploy log rather than inferred.
- **Correctness:** three surfaces carry the module's weight and each is pinned by tests. (1) The **species→variety resolution function** — one inheritance fixture, one full-override fixture. (2) The **check** — every one of C1–C11 has a fixture that fires and one that must not, and C1's must-not case is specifically two crops in one bed whose occupancy windows do not overlap. (3) The **task generator** — regeneration must move an open unedited task and must leave `done`, `skipped`, `is_edited` and tombstoned ones alone; this is a property of the generator, not of any one crop, so it is tested as one.
- **Security:** no new surface. Reads member-gated, writes `editor`/`admin` + CSRF, one admin-only route (season reopen). The **outbound** weather call is the only new egress: a fixed host, no credentials, no user data in the query — coordinates and nothing else — and a hard timeout, so a hanging forecast cannot hold a scheduler tick.
- **Import safety:** LLM-supplied JSON is untrusted input. It is size-capped, schema-validated, enum-mapped explicitly, and applied only after a preview; Markdown fields are rendered through the same sanitiser as `notes`.
- **Performance:** a few thousand rows at most. The check is O(plantings² ) *within a bed* and O(plantings) across the garden — at ~15 beds this is free; it is recomputed on every plan read rather than cached, which is what keeps it honest.
- **Backup:** the eleven tables ride Litestream `home/`. **No bucket, no blobs** (D122).

---

## V7-9. Configuration (v7)

Three new environment variables, all with working defaults — the module runs with none of them set:

| Var | Default | Purpose |
|---|---|---|
| `HOME_GARDEN_WEATHER_ENABLED` | `true` | Master switch for the outbound forecast. `false` ⇒ manual frost dates only; the metric resolves null and any condition gating on it stays silent. |
| `HOME_GARDEN_WEATHER_URL` | Open-Meteo forecast endpoint | Base URL override, for testing or if the provider changes. |
| `HOME_GARDEN_WEATHER_POLL_HOURS` | `12` | Poll interval, 1–24. |

**Coordinates are not env vars.** `latitude`/`longitude`/`altitude` live in `garden_settings` next to the frost dates they serve (§V7-10 D112): they are not secrets, they are user data, and putting them in Coolify would mean a redeploy to fix a typo. `HOME_TIMEZONE` (`Europe/Prague`) already governs every date boundary the module depends on.

---

## V7-10. Decisions (D101–D132)

- **D101 — `garden` owns its work calendar.** Garden tasks are not mirrored into `todo` or `events`: §10 D25/D28 forbid the import, a card has no window and no planting link, and a season's 100–200 items would drown the household lists. The shared surfaces are the widget and the catalogs.
- **D102 — Timing is anchored.** Every knowledge-base window is `{anchor, from, to}` with anchors `week` / `last_frost` / `first_frost`, mixable per window. Frost-anchored windows move with the season's frost dates; week-anchored ones do not.
- **D103 — One resolution function.** Species → variety with nullable overrides, resolved by a single exported function with its own test. Four call sites, one implementation.
- **D104 — `family` is a fixed enum.** Rotation and family rules join on it; free text would make the engine a lottery.
- **D105 — A planting belongs to the season of its harvest.** Sow dates may fall in the previous calendar year (česnek), and rotation counts the planting in the year it occupied the bed through to harvest.
- **D106 — Permanents are plantings with `season_id IS NULL`**, not a second table. Occupancy, warnings, tasks and harvests keep exactly one code path; Trvalky is a filtered view.
- **D107 — Bed sharing is overlapping occupancy**, not calendar-year membership. Spring špenát and autumn pórek in one bed do not warn.
- **D108 — Warnings are computed on read; only dismissals persist**, keyed stably on (rule, bed, plantings, season) so a changed plan brings a silenced warning back.
- **D109 — A warning never blocks a save.** Severities `error` / `warn` / `info` / `tip`; dismissal is per season with an optional note.
- **D110 — Regeneration is conservative.** It may move an open, unedited, generated task; it never touches `done`, `skipped` or `is_edited`; a deleted generated task leaves a tombstone.
- **D111 — `hardiness` is required on every crop**, because the frost logic reads it and a crop the module cannot classify is a crop it cannot warn about.
- **D112 — Weather is Open-Meteo, polled by `platform/scheduler`, failing silently.** Coordinates live in settings, not env: user data, not secrets. The poll rides a new generic **`RegisterJob`** hook on the existing ticker — v7's only edit to `platform/*`, and additive.
- **D113 — The module sends no push and owns no audience.** It publishes `garden.frost_risk_tonight`, `garden.frost_sensitive_now` and one idempotent `garden.frost_warning` audit event per night; **Administrace** decides delivery, audience, conditions and active hours through the v5 machinery. Both a conditioned schedule and a trigger rule work on day one; the choice is made in the UI, not here.
- **D114 — The LLM prompt embeds a schema generated from the importer's own validator.** One registry produces the validator, the prompt's schema and `/api/garden/enums`, so a prompt cannot ask for a field the importer rejects. Imports preview with a diff, carry provenance, and are badged "neověřeno" until a human confirms them.
- **D115 — Built-in rules ship as a seed source excluded from `testsupport`:** families with break years plus ~50–80 sourced Czech companion pairs, `INSERT OR IGNORE`, each carrying its `source`.
- **D116 — No bed sub-sections.** A bed is one unit and everything in it counts as adjacent. Sections would mainly refine C1, which is already deliberately conservative.
- **D117 — Adjacency is inferred from lexorank order within a zone.** No neighbour table, no coordinates, no drawing surface: you drag beds into the order they stand in, and C11 becomes possible.
- **D118 — No auto-generated watering or weeding.** `water` and `weed` exist as manual kinds only; generated chores nobody ticks off devalue every other row in the list.
- **D119 — Actual dates never re-drive planned windows.** The planting detail states the drift in words and offers a one-click shift of downstream tasks, which marks them `is_edited`.
- **D120 — "Uzavřít sezónu" is explicit, and there is no historical back-fill.** Closing collects final yields, failures and observed frost dates, then locks the season into rotation history; reopening is admin-only. C3 and C8 return `no_history` — and the UI says so — until seasons have been closed.
- **D121 — Storage tracks `quantity_remaining` edited in place.** The audit spine's field diffs are the consumption history; a movements table would buy only a chart.
- **D122 — No photos, therefore no blob storage in `garden`.**
- **D123 — One widget, `garden.prace`.** Harvest surfaces as a `harvest` task rather than as a second card that is dead weight from November to May.
- **D124 — Ticking a task is an ordinary `editor` write.** No role exception; `reader` stays read-only across the module.
- **D125 — A print stylesheet with two targets:** this month's work with checkboxes, and the season plan on one page. This is the accepted answer to reads-only-offline in a garden with no signal.
- **D126 — `GET /api/garden/export` emits the importer's own JSON shape**, so an export re-imports to an equivalent state.
- **D127 — Green manure is not modelled in v7** — no plant type, no task kind. Noted consequence: mustard sown after cabbage will not register as brassica-on-brassica.
- **D128 — Seasons are addressed by `{year}`.** A knowing, single-entity deviation from addressing by surrogate id: the year is unique, immutable, user-visible, and makes `/zahrada/plan/2027` map 1:1 onto the API.
- **D129 — `dry_run` is the shared preview idiom** for the import and for season creation: same response shape, plus a diff, nothing persisted.
- **D130 — Built-in rules can be disabled but not deleted** (`409`); user rules can be deleted. You can always see what you did not type.
- **D131 — Task completion is idempotent**, mirroring `events`, because the 2000 ms hold gesture can fire twice on a bad connection.
- **D132 — Czech UI vocabulary is fixed in §V7-7**, so pages, widget, metric labels and notification tokens say the same words.

---

## V7-11. Acceptance Criteria (v7)

- [ ] `garden` registers through `registry.Module` — routes, migrations, `AuditActions()`, one widget provider, metric + list providers — and `internal/arch`'s **`TestModulesDoNotImportEachOther`** stays green. It imports **neither `platform/push` nor `platform/blobstore`**, and a test asserts it.
- [ ] The **species→variety resolution function** has a unit test with a full-inheritance fixture and a full-override fixture, and every consumer (planner, generator, check, widget) reads through it.
- [ ] Timing resolution: a frost-anchored window moves when a season's frost date moves; a week-anchored window does not. Both are covered.
- [ ] **Every check C1–C11 has a fixture that fires and one that does not.** C1's negative case is two crops in one bed with **non-overlapping occupancy** and must not warn. C3 and C8 return **`no_history`** on a fresh install, and the UI renders "rotaci zatím nelze zkontrolovat, chybí historie".
- [ ] Dismissing a warning silences it for that season only; changing the plan enough to change its key brings it back; `DELETE` restores it.
- [ ] **Regeneration:** move a planting's transplant date — an open generated task moves, a `done` one does not, an `is_edited` one does not, a deleted one stays deleted (tombstone). Manual tasks are untouched.
- [ ] Recording an actual sow date **moves no planned window**; the drift line appears; `POST …/shift-tasks` moves the remaining open tasks and marks them `is_edited`, after which regeneration leaves them alone.
- [ ] Copy-season with `shift`: reproduces the plan, re-anchors frost-anchored dates only, and `dry_run=true` returns the prospective plan **and its check** while persisting nothing.
- [ ] Season close locks the season, records yields/failures/actual frost dates, and a subsequent C3 in the same bed fires against it. Reopen is refused for `editor` (403) and audited for `admin`.
- [ ] Frost: `garden.frost_risk_tonight` and `garden.frost_sensitive_now` resolve (metric null when no forecast is cached); exactly **one** `garden.frost_warning` per date survives a catch-up tick; a v5 schedule conditioned `garden.frost_risk_tonight lte 2` sends once below threshold and stays silent above it. **No push call exists in the module.**
- [ ] A failed or disabled weather fetch surfaces nothing user-visible: pages render, the metric is null, no error toast.
- [ ] **Import:** valid JSON previews with a field-level diff; an unmappable enum returns `422` naming field and value; a 20-element array reports per-element status; provenance and the "neověřeno" badge are recorded. **Export re-imports to an equivalent state.**
- [ ] All six metrics resolve through the provider contract (no cross-module import); each of the four countable lists agrees with its metric by construction; the two list-only keys carry their Czech empty strings.
- [ ] `garden.prace` appears in the widget catalog for **every** role, renders both states, groups overdue first, and ticks via the 2000 ms hold with `meta.via="dashboard"`. `reader` sees it read-only.
- [ ] Every mutation writes an audit event **in the same transaction**; `planting.update` and `task.update` carry field-level diffs; `garden.*` appears in the log browser filter and the trigger composer.
- [ ] The **four non-registry host maps** are updated: `platform/widgets/registry.tsx`, `AppShell`'s `OVERFLOW`, the Log browser's `MODULES`, and `admin/listener.go`'s `inAppURL` → `/zahrada`.
- [ ] Migrations run `… finance(09) → garden(10)` cleanly on an empty DB and after a Litestream restore; **the rule seed is excluded from `testsupport`** (a test asserts an empty `garden_rules` on a fresh test DB).
- [ ] Built-in rule: `DELETE` → `409`; `PATCH {is_disabled:true}` succeeds and the check stops firing it.
- [ ] Deleting a bed with plantings in an open season → `409`; mutating anything in a closed season → `409`.
- [ ] OpenAPI **0.9.0** validates; new paths and schemas reuse the shared `Limit` / `Cursor` / `responses` / security components.
- [ ] Frontend: eight routes, Czech vocabulary per §V7-7, family colour **always paired with a label or pattern**, no new or hardcoded colour values; nav shows Zahrada in "více" for all roles.
- [ ] Print: the month view prints with checkboxes, bed codes and windows; the season plan prints on one page.
- [ ] Live sync: a plan change on one device shows "Zahrada byla mezitím upravena" on another.
- [ ] Offline: every garden page renders read-only from cache; write controls disabled.

---

## V7-12. As built — the v7 build (2026-08-19/21, OpenAPI 0.9.0 → shipped inside **0.10.1**, deployed 2026-08-21)

> v7 was built from this spec in one pass (repo `main`: PR #16 `fd79fed`) and deployed with v8 on 2026-08-21. Eleven tables at block **10**, **34 routes / 61 operations**, the four pure functions the rest of the module consumes (`timing.go` → `resolve.go` → `occupancy.go` → `check.go`), the `garden.prace` widget, six metrics, six lists, **31 audit actions** and the 82-rule seed at `10900`. Everything in §V7-1…§V7-11 still holds except where recorded below.
>
> **The one thing that did not happen: `backend/openapi.yaml` was never updated by this build.** The repo's served contract stayed at **0.8.0** — no `garden` paths at all — while 0.9.0 lived only in `handoff/v7/`. The gap was found and closed on 2026-08-21 by the as-built reconciliation that produced **0.10.1**; see §V8-12. Treat "update `backend/openapi.yaml` in the build PR" as part of the module checklist, alongside the four non-registry host maps.

### Decisions taken during the v7 build (D163–D168)

- **D163 — the copy-season shift slides every planting from its own bed; `by_family` is accepted but INERT.** The spec sold a switch that moves "a whole family as a block". The family-anchored implementation collapsed A1 and A3 onto A2 — a shift is only meaningful relative to where a planting actually was — so it was removed and each planting now slides its own `offset` from its own bed. The request field stays (it is decoded and ignored) so an existing client does not 422; `openapi.yaml` marks it inert rather than pretending it works. *(2026-08-19.)*
- **D164 — a planting's CROP cannot be changed by `PATCH`.** `plant_id` is absent from `GardenPlantingUpdate` and ignored if sent. Changing the crop changes the resolved timing, the generated task set and every check the planting participates in — that is a delete-and-recreate, not a field edit. The §V7-6 prose that listed "the crop" among the regeneration triggers was wrong and is corrected: regeneration is triggered by **planned dates, variety or quantity**. *(2026-08-19.)*
- **D165 — dismissing a warning returns `200` + the recomputed `GardenCheckResult`,** not `201` + the dismissal row. The caller's next act is always to re-render the panel, and a dismissal can change sibling warnings (the same rule re-fires against a different bed); returning the dismissal alone forced a second round trip that could disagree with the first. The `409` the spec documented is unreachable — the insert is an upsert. *(2026-08-19.)*
- **D166 — a harvest quantity is STRICTLY positive.** The spec said `minimum: 0`. A zero row poisons `yield_actual` — it reads as "harvested, nothing came" where the honest record is either no row at all or a `failure` outcome at season close — so `<= 0` is a 422. *(2026-08-19.)*
- **D167 — a season is closed only by its own action.** `PATCH /seasons/{year}` refuses `status: closed` with 422; `POST …/close` is the only door, because closing also records yields, failures and actual frost dates and is what creates rotation history. The `GardenSeasonStatus` enum on the update schema is narrowed accordingly. *(2026-08-19.)*
- **D168 — the garden collections do not all follow the house paging contract, and the spec now says so.** Beds and seasons are returned **unpaged** (beds in `zone, position, id` order — the adjacency model of D117 is meaningless split across pages); `/tasks` keysets on an **opaque composite** `(window_from, position, id)` rather than the house UUIDv7 `Cursor`; and plantings, tasks and harvests filter on **`year`**, not `season`. *(2026-08-19.)*

### Corrections the build forced back into this document

- **31 audit actions, not twelve.** §12's v7 bullet said twelve; the module registers 31 (`plant|variety|bed|season|planting|task|harvest|storage|rule` × create/update/delete, plus `bed.move`, `season.close`, `season.reopen`, `settings.update` and the system-written `frost_warning`). One nuance worth knowing: `POST /seasons` with `copy_from` onto an existing row records **`season.update`**, not `season.create`.
- **The catalog keys are not the ones the prose claimed.** Six metrics — `garden.tasks_due_7d`, `garden.tasks_overdue`, `garden.plan_warnings`, `garden.harvest_season`, `garden.beds_unplanned`, `garden.frost_risk_tonight`. Six lists — the same first three and fifth, **plus `garden.harvest_ready` and `garden.frost_sensitive_now`**. So `garden.harvest_season` and `garden.frost_risk_tonight` are **metric-only**, and `garden.harvest_ready` and `garden.frost_sensitive_now` are **list-only**. The four shared keys route through one selection function each (`tasksDueWithin`, `tasksOverdue`, `planWarnings`, `bedsUnplanned`) and the metric is `len(items)` of the identical call, so a count and its list cannot part ways. `garden.frost_sensitive_now` is evaluated at the **forecast night**, not at `asOf`'s calendar date — deliberately the same night the metric and the audit event use.
- **`GardenVariety.effective` is its own shape, not a `GardenPlant`.** It is `PlantCore` plus `plant_id`, `plant_name`, `family`, `plant_type`, `hardiness`, `variety_id`, `variety_name` — no `id`, `name_cs`, `variety_count`, `provenance` or timestamps. `openapi.yaml` now carries it as `GardenEffective`.
- **Every optional `GardenPlantCore` field serialises as explicit `null`, never absent** — 14 properties the spec had typed as non-nullable. This is deliberate: a resolved record has to be able to say "the variety says nothing here" distinctly from "this key is missing".
- **Occupancy is computed from more fields than §V7-5 listed.** `from` = the first of `sowed_on` (**suppressed when `sow_indoor_on` is set** — an indoor sowing does not occupy the bed), `sow_direct_on`, `transplanted_on`, `transplant_on`, falling back to `planted_on` for permanents; `to` = the first of `cleared_on`, `removed_on`, `harvest_to`.
- **A warning `key` hashes the check id and a per-check discriminator too** (C10 per date field, C11 per reason), not only rule/bed/plantings/season. The key stays opaque; the consequence is that two different checks firing on the same bed dismiss independently, which is what a user expects.
- **`{year}` accepts 1900–2200** (not 2999), and an out-of-range year is a **422**, not a 404.
- **Seven endpoints documented filters that were never implemented** — `/beds` (`zone`, `active`, paging), `/seasons` (paging), `/seasons/{year}/check` (`bed_id`, `include_dismissed`), `/harvests` (`from`, `to`), `/storage` (`location`, `expiring_before`), `/rules` (`ref`), `/plants/prompt-template` (`name`, `target`). All removed from the spec; `/tasks` gained the `to_end` bound it actually reads and `/plants/import` the `model` it records as provenance.
- **`/tasks` is ordered `window_from, position, id`** — there is no overdue-first term in the query. The widget groups overdue itself; a caller that needs that ordering must do the same.

### Known gap, recorded not fixed

**The D126 export↔import round trip did NOT ship.** `GET /api/garden/export` emits plants, varieties and rules; `POST /api/garden/plants/import` accepts **only** a crop object or an array of crop objects, each requiring `name_cs`. Feeding an export straight back is refused. Varieties and rules are export-only — the export is a **superset** of what import understands. The §V7-11 criterion *"Export re-imports to an equivalent state"* is therefore **not met**, and `openapi.yaml`'s prose no longer claims it. Closing it means teaching the importer the two extra collections and their reference resolution (rules reference crops by `name_cs`), which is a day's work, not a line.

### Verification carried out at build time

Verified end to end against a fresh database: migrations apply through block 10, the 82-rule seed lands, task generation produces twelve Czech-titled jobs from two plantings, the check fires C1 (from a *seeded* rule), C4, C6 and C7 while reporting `no_history` for C3/C8, and the widget renders both states on Nástěnka. `TestForbiddenPlatformImports` was proved by a deliberate violation: the module cannot import `platform/push` or `platform/blobstore` without failing the build. `TestSeedExcludedFromTestDB` asserts a fresh `testsupport.NewDB()` holds **zero** `garden_rules`, which is what stops a check fixture passing because a built-in rule matched. The **Playwright/axe pass at 375/1440 in both themes remains outstanding**, as it has since v5.

---

> **v8 scope:** one new self-contained module, **Elektřina** (`electricity`), the **tenth**. The household reads its own electricity meter whenever it happens to think of it — two registers, **VT** (vysoký tarif) and **NT** (nízký tarif) — and the module turns those irregular readings, a price list versioned by effective date and a záloha schedule into the one number the household actually wants: **will the zálohy cover the bill, or is a nedoplatek coming**.
>
> v8 is **additive**: no change to auth, the dashboard-host contract, `platform/*` or the nine existing modules. It is also, deliberately, the **smallest surface any home module has taken**: five tables, thirteen paths, one pure function, **no widget, no metric, no list, no push, no scheduler job, no blob storage and no seed migration** (§V8-10 D147, D152). Where v7's `garden` publishes facts for Administrace to deliver, `electricity` publishes nothing at all — Karel asked for "no widget" and "no chase", and the honest implementation of that is a module that contributes to no catalog rather than one that contributes and is then muted.

---

## V8-1. Overview (delta)

- **One-line summary (add):** irregular two-register meter readings + a date-versioned ceník + a záloha schedule ⇒ a settlement period's predicted **nedoplatek / přeplatek**, computed on read and never stored.
- **Modules (add):** `electricity` — Czech UI **Elektřina**, route `/elektrina`, in the "více" overflow beside Finance and Zahrada.
- **Depends on (add):** nothing. No outbound HTTP, no new platform package, no sidecar. v7's Open-Meteo call remains the only external dependency home has.
- **Scale target:** a few readings a year, one ceník version a year, one settlement period a year — **hundreds of rows over the app's lifetime**. Every computation is O(readings) over a period and runs on read; there is nothing here to cache and nothing to page in the UI. The API still uses the house `Limit`/`Cursor` convention.

### Architecture — a reading is an instant, not a day (§V8-10 D134)

The module rests on one sentence: **a reading is the state of the meter at 00:00 of `read_on`.** Everything follows from it and nothing has to be remembered separately.

- Consumption of day *d* = `reading(d+1) − reading(d)`.
- An interval between readings on `d1` and `d2` covers the days `d1 … d2−1`.
- A ceník version effective from `D` prices the days `D, D+1, …`.
- A settlement period `[starts_on, ends_on]` — inclusive, the way a Czech supplier writes it — needs an **opening reading dated `starts_on`** and is closed by the reading dated **`ends_on + 1`**, which is simultaneously the next period's opening reading. One meter reading, one instant, two periods; which is how the distributor treats it as well.

The alternative — treating a reading as covering its own day — leaves every boundary in the module arguable, and the arguments surface as one-day discrepancies against the supplier's invoice that nobody can then explain.

### Architecture — money is never interpolated; pictures may be (§V8-10 D137, D138, D159)

Karel chose the strict option at the interview: when a price change falls **strictly inside** a reading interval, that interval is **not priced at all**. The module names the missing odečet and computes nothing past it, while everything before the gap stays valid and on screen. The same rule makes an opening reading mandatory (D140): with no baseline the period shows no money whatsoever, only the missing-reading notice.

That would make the history chart impossible, so the counterpart is stated as explicitly as the rule: the **chart** does spread an interval's kWh evenly across its days (D138) — display only, labelled approximate — and a month's **Kč** column is an **allocation of already-exact interval costs by day count** (D159), never a repricing of the invented kWh. So the two kinds of number never touch: an approximate quantity may be drawn, an approximate quantity may never be priced.

### Architecture — the module that contributes nothing (§V8-10 D147, D152, D156)

`electricity` implements no `Source` interface, registers no widget and imports **none** of `platform/metrics`, `platform/lists`, `platform/push`, `platform/scheduler`, `platform/blobstore`. A test asserts the imports are absent, so a later refactor cannot quietly add one.

Two consequences worth stating. First, **the module has to be worth opening on purpose** — nothing will remind anyone that it exists — which is why the Přehled screen carries the module's entire value and why the one concession Karel allowed is a plain in-app line, *"poslední odečet před N dny"* with a **Zadat odečet** button (D156). Text on a page you already opened is not a notification. Second, the **four non-registry host maps** that v5, v6 and v7 all tripped over become **three**, and the fourth is now a trap in the opposite direction: `platform/widgets/registry.tsx` must **not** be touched, because there is no widget to register.

| Map | v8 |
|---|---|
| `AppShell`'s `OVERFLOW` nav list | add **Elektřina** → `/elektrina` |
| the Log browser's hardcoded `MODULES` | add `electricity` |
| `admin/listener.go`'s `inAppURL` | add `electricity` → `/elektrina` |
| `platform/widgets/registry.tsx` | **do not touch** |

---

## V8-2. Goals & Non-Goals (delta)

**Goals**

1. Record a meter reading on a phone, standing at the meter cupboard, in well under a minute — two whole numbers and a date.
2. Answer *"budou zálohy stačit?"* with a single figure, honestly hedged as a prediction, for a settlement period the household defines.
3. Let prices and fees change **from a date** without disturbing any figure that precedes the change — the requirement Karel stated first and the one the versioned ceník exists to satisfy.
4. Make the arithmetic checkable by hand: every interval, every fee chunk and every rounding point is visible on screen, because a household number nobody can reproduce is a number nobody trusts.
5. Say "I don't know" precisely. Missing odečet, missing baseline, not enough history — each has its own message naming exactly what is missing.

**Non-Goals (v8)**

- **No invoice itemization.** Three numbers per ceník version — cena VT, cena NT, měsíční poplatky — all **including DPH and distribuce**, used as typed (D135). No silová/regulovaná split, no jistič, no systémové služby, POZE, OTE or daň z elektřiny, no VAT arithmetic. *(Karel considered the itemized model and rejected it: "No, just VT, NT, poplatky, nothing more.")*
- **No výměna elektroměru** (D150). The schema assumes one monotonically non-decreasing pair of counters.
- **No second odběrné místo, no FVE/přetoky, no plyn, no voda.**
- **No widget, no metric, no list, no push** (D147).
- No invoice PDF attachment — therefore no blob storage. No print view. No offline writes.
- **No seasonal or weather-adjusted forecasting.** The prediction is a plain average since the period start, by decision (D141), not by omission.
- No back-fill importer. Karel's meter starts at 32/70 kWh on the period's first day; there is no history to import.

---

## V8-3. Users, Roles & Auth (delta)

No change to Mode B, the session, CSRF or the role model.

- **Reads** — any authenticated member including `reader`.
- **Writes** — `editor`/`admin` + CSRF, for all five entities.
- **Admin-only** — **none**. Unlike v7 (season reopen) and unlike the v6 spec's since-corrected draft, this module has no admin tier: nothing here is irreversible enough to warrant one, since periods never lock (D139) and deletes are soft.

---

## V8-4. Functional Requirements (v8)

Every mutating requirement records an audit event through the spine **in the same transaction**; stated once, not repeated. Reads are not logged.

### Elektřina module (`electricity`) — new in v8

#### FR-E1: Odečty — meter readings
- **Trigger:** member opens Odečty and adds a reading, typically after physically reading the meter.
- **Inputs:** `read_on` (date), `vt_dkwh`, `nt_dkwh` (tenths of kWh — the form accepts whole kWh, see D148), optional `note`.
- **Behaviour:** `read_on` is UNIQUE among live rows. Both registers must be **non-decreasing in date order**, validated against the neighbouring readings on **both** sides so a back-filled reading cannot break the chain either. Soft delete, the house default.
- **Outputs:** the reading, and — on the list — the interval that ends at it: days, VT/NT kWh, **energy Kč**, and which ceník priced it.
- **Errors:** `409` a live reading already exists on that date; `422` a register that would decrease, naming the offending neighbour. With výměna elektroměru out of scope (D150) a falling counter is always a typo, so refusing it is correct rather than restrictive.

#### FR-E2: Ceník — prices and fees, versioned by effective date
- **Trigger:** the supplier announces new prices; member adds a version.
- **Inputs:** `effective_from` (date, UNIQUE), `price_vt_haler`, `price_nt_haler` (Kč/MWh), `monthly_fee_haler` (Kč/měsíc) — all **s DPH a distribucí**.
- **Behaviour:** a version governs **every day ≥ `effective_from`** until the next version starts; the end is **derived, never stored** (D136), because a stored end is a second source of truth that eventually contradicts the next row's start. Editing a version's prices moves **only its own days** — this is the structural form of Karel's "effective to some date, which will not affect numbers before that date", rather than a rule someone has to remember. A **future** `effective_from` is the normal case: the forecast prices each future day with the version effective on that day (D142), so next January's prices entered in August immediately show their effect.
- **Errors:** `409` duplicate `effective_from`; `409` on delete **only** when the deletion would leave a day inside a settlement period with no effective version (D160) — deleting a middle version is a legitimate repricing, recorded by the audit event.

#### FR-E3: Zálohy — the schedule, and what was really paid
- **Inputs:** schedule version `{effective_from, amount_haler, due_day}`; payment `{month YYYY-MM, amount_haler, paid_on?}`.
- **Behaviour:** the schedule is versioned exactly like the ceník. A **recorded payment wins over the schedule for its month** (D144), so the common case is zero typing and the month that differed is one row. Attribution is by the `month` key, not by `paid_on` — a March záloha paid on 2. dubna still belongs to March. `due_day` is 1–31 and is **clamped to the month's last day at read time, never stored clamped** (D155, the §10 D74 precedent); it is read in exactly one place — whether a counted month is already paid — and therefore moves the *doporučená záloha* and nothing else. **At equality the month counts:** due on the 15th means paid on the 15th.
- **Errors:** `409` duplicate `effective_from` or duplicate `month`.

#### FR-E4: Zúčtovací období, and the vyúčtování when it arrives
- **Inputs:** `starts_on`, optional `ends_on`, `ends_on_confirmed`; later `invoiced_total_haler`, `invoiced_balance_haler`, `invoiced_vt_dkwh`, `invoiced_nt_dkwh`, `invoiced_at`.
- **Behaviour:** periods are **user-set, inclusive, non-overlapping and never locked** (D139). When `ends_on` is omitted it defaults to `starts_on + 1 year − 1 day` with `ends_on_confirmed` false, and the UI badges it **předpokládaný konec** (D153) — suppliers frequently do not state an end date in advance, and a prediction has to project somewhere. Correcting it later is one field, after which every forecast figure follows and no actual figure moves. Recording the invoice stores **four** figures including the supplier's final meter values (D154), so a discrepancy can be attributed to **kWh** rather than only to Kč — which is how an odhadnutý odečet on the supplier's side becomes visible instead of looking like a pricing surprise.
- **Errors:** `422` overlapping an existing period, or `ends_on` before `starts_on`.

#### FR-E5: Intervals and the hard block
- **Behaviour:** the readings inside a period are cut into consecutive intervals. An interval whose interior contains a ceník `effective_from` — strictly between `d1` and `d2` — is **not priced** (D137). The summary reports it under `blocking` as `chybí odečet k <date>`, computes nothing from that date onward, and leaves everything before the gap valid and visible. A period with no reading on `starts_on` reports `blocking` of kind `period_start` and shows **no money at all** (D140).
- **Outputs:** `blocking[]` with the exact date each missing reading must carry; the UI opens the reading form **pre-filled with that date and nothing else** — never with an estimated value.

#### FR-E6: Cost — energy and poplatky
- **Behaviour:** for an interval priced by one ceník version, in integer haléře and integer tenths of kWh (D148):

  `cost_haler = round((vt_dkwh · price_vt_haler + nt_dkwh · price_nt_haler) / 10000)`

  **One rounding per interval**, on the VT+NT sum — not per tariff and not per day. Poplatky are **pro-rata by days** (D143): a day costs `monthly_fee(day) / days_in_month`, summed per **(calendar month × ceník version)** chunk with **one rounding per chunk**, so a whole month inside one version costs exactly the monthly fee to the haléř and the pro-rata only shows at a period's ends and at a mid-month price change. A displayed VT/NT breakdown **rounds VT and gives NT the remainder** (D158) — the `needs` pattern from the fin split — so the parts sum to the whole by construction.
- **Note:** an interval carries **energy only**. Fee chunks belong to no interval; allocating them would invent a second rounding rule whose only purpose is keeping two views agreeing.

#### FR-E7: Prediction
- **Behaviour:** the boundary between fact and forecast is the **latest reading, not today** (D141) — days since the last reading are forecast like any other future day, which is what keeps "actual" meaning measured.

  ```
  elapsed_days   = last_reading.read_on − period.starts_on        (≥ 1 required)
  avg_vt_per_day = (vt_last − vt_start) / elapsed_days
  avg_nt_per_day = (nt_last − nt_start) / elapsed_days
  ```

  Each day from the last reading to `ends_on` inclusive is priced with the ceník effective **on that day** (D142) and carries its own fee share. Once the closing reading dated `ends_on + 1` exists the forecast span is **empty** and the period is entirely actual (D157) — Přehled swaps *predikce* for *skutečnost*, and the computed-vs-invoiced comparison becomes meaningful.
- **Refusals:** fewer than two readings in the period, `elapsed_days < 1`, no ceník effective on or before `starts_on`, or an unresolved block ⇒ *"zatím nelze předpovědět"*, naming what is missing. The module never shows a number it has not earned.

#### FR-E8: Balance, and the doporučená záloha
- **Behaviour:** a calendar month counts toward a period **iff the period contains that month's first day** (D145) — which makes a year-long period exactly 12 months whatever day it starts on. Then:

  ```
  advances    = Σ counted months (payment if recorded, else the schedule effective on the 1st)
  balance     = advances − cost_total          > 0 přeplatek · < 0 nedoplatek
  recommended = (cost_total − advances already due) / months not yet due
  ```

  `recommended` is **rounded up to whole koruny**, floored at 0, and omitted when no month remains (D146). Přehled lists the counted months with their amounts, so D145 is visible rather than folklore.

#### FR-E9: Headroom — the answer available before any consumption is known
- **Behaviour:** `energy_budget = advance − monthly_fee`, expressed in kWh at the VT price, at the NT price, and at a **30 % VT / 70 % NT mix** (D162) — the last being the figure Přehled leads with, since neither all-VT nor all-NT is a household. This is computable with **zero** consumption data, which is why it is what Přehled shows while the prediction is still impossible. For Karel's day-one state — 1 500 Kč záloha, 642,35 Kč poplatky — it reads: *857,65 Kč/měsíc na elektřinu, tj. asi 200 kWh — 176 kWh, kdyby všechno šlo ve VT, 213 kWh v NT.*
- **Note:** the mix is a **stated heuristic, never a measurement**, and the Czech copy names it as one. It is computed with one division and no intermediate rounding — `divRound(energy_budget · 100000, 3·price_vt + 7·price_nt)` — because rounding a blended price first moves the answer by a whole kWh.

#### FR-E10: Historie — per-month series
- **Behaviour:** kWh per month is approximate by construction (D138) and every month carries `is_approximate`, true whenever a contributing interval crosses the month boundary. Kč per month is an allocation of exact interval costs by day count (D159); fee chunks are already per month.
- **Outputs:** consumption and cost per month with the VT/NT split, plus past periods with computed vs. invoiced in **both Kč and kWh**.

#### FR-E11: The four screens
- Přehled · Odečty · Ceníky a poplatky · Historie — specified in §V8-7.

#### FR-E12: What the module deliberately does not register
- **Behaviour:** no widget provider, no metric provider, no list provider, no push, no scheduler job, no seed migration, no derived column and no cache (D147, D152). Everything is computed on read. A test asserts the absent imports; a second asserts the module contributes nothing to the widget, metric and list catalogs.

---

## V8-5. Data Model (v8)

**Five** new tables, one new migration block (**11**), **no change to any existing table**, and **no seed source** — `11001_electricity.sql` is the module's only migration. SQLite → Litestream `home/`. No blob store.

House conventions throughout: UUIDv7 `id`, soft delete (`deleted_at`), `created_by` / `created_at` / `updated_at`, English identifiers, Czech UI.

**Units, stated once and enforced by the column names.** Energy is **INTEGER tenths of kWh** (`*_dkwh`); money is **INTEGER haléře** (`*_haler`). Neither floats nor whole koruny would do: a ceník price is 4 858,65 Kč/MWh, so `finance`'s whole-CZK integers would lose money, and a float would lose determinism in a formula whose whole point is that two people can reproduce it (D148).

**`electricity_readings`**

| Column | Type | Notes |
|---|---|---|
| `id` | TEXT PK | UUIDv7 |
| `read_on` | TEXT NOT NULL | DATE. **UNIQUE among live rows.** The meter state at 00:00 of this day (D134) |
| `vt_dkwh` | INTEGER NOT NULL | CHECK ≥ 0. Tenths of kWh |
| `nt_dkwh` | INTEGER NOT NULL | CHECK ≥ 0 |
| `note` | TEXT NULL | |
| `created_by`, `created_at`, `updated_at`, `deleted_at` | | house |

Index on `(deleted_at, read_on)` — every read of this table is chronological. Monotonicity is a **service-level** check against both neighbours, not a table CHECK, because SQLite cannot express "greater than the previous live row".

**`electricity_tariffs`**

| Column | Type | Notes |
|---|---|---|
| `id` | TEXT PK | UUIDv7 |
| `effective_from` | TEXT NOT NULL | DATE, **UNIQUE among live rows**. Governs all days ≥ this (D136) |
| `price_vt_haler` | INTEGER NOT NULL | CHECK ≥ 0. Kč/MWh **s DPH a distribucí** |
| `price_nt_haler` | INTEGER NOT NULL | CHECK ≥ 0 |
| `monthly_fee_haler` | INTEGER NOT NULL | CHECK ≥ 0. Kč/měsíc s DPH |
| `note` | TEXT NULL | e.g. the supplier's ceník name |

**No `effective_to` column.** The API returns one, derived from the next row.

**`electricity_advances`**

| Column | Type | Notes |
|---|---|---|
| `id` | TEXT PK | UUIDv7 |
| `effective_from` | TEXT NOT NULL | DATE, **UNIQUE among live rows** |
| `amount_haler` | INTEGER NOT NULL | CHECK ≥ 0 |
| `due_day` | INTEGER NOT NULL | CHECK BETWEEN 1 AND 31. Clamped **at read time** (D155) |
| `note` | TEXT NULL | |

**`electricity_payments`**

| Column | Type | Notes |
|---|---|---|
| `id` | TEXT PK | UUIDv7 |
| `month` | TEXT NOT NULL | `YYYY-MM`, **UNIQUE among live rows**. The attribution key — not `paid_on` |
| `amount_haler` | INTEGER NOT NULL | CHECK ≥ 0 |
| `paid_on` | TEXT NULL | DATE, metadata only |
| `note` | TEXT NULL | |

**`electricity_periods`**

| Column | Type | Notes |
|---|---|---|
| `id` | TEXT PK | UUIDv7 |
| `starts_on` | TEXT NOT NULL | DATE, inclusive. Requires a reading dated exactly here (D140) |
| `ends_on` | TEXT NOT NULL | DATE, inclusive. Closing reading is dated `ends_on + 1` (D134) |
| `ends_on_confirmed` | INTEGER NOT NULL | 0/1, default 0 — drives the **předpokládaný konec** badge (D153) |
| `invoiced_total_haler` | INTEGER NULL | the supplier's total (D154) |
| `invoiced_balance_haler` | INTEGER NULL | their nedoplatek (negative) / přeplatek (positive) |
| `invoiced_vt_dkwh`, `invoiced_nt_dkwh` | INTEGER NULL | the meter values they billed to |
| `invoiced_at` | TEXT NULL | DATE |
| `note` | TEXT NULL | |

CHECK `ends_on > starts_on`. Non-overlap is a **service-level** check (SQLite has no exclusion constraint), returning 422.

**Audit actions (20):** `electricity.reading.create|update|delete`, `electricity.tariff.*`, `electricity.advance.*`, `electricity.payment.*`, `electricity.period.*` — every `update` carrying a field-level diff, because in this module a single corrected digit silently moves every downstream figure.

---

## V8-6. API Surface (v8)

Full detail in `openapi.yaml` (**0.10.0**). All routes session-authorized; writes additionally require CSRF and `editor`/`admin`; reads open to `reader`. **13 new paths**, taking the contract from 106 to **119 paths** and from 203 to **235 schemas**.

> **⚠ As built (2026-08-21):** the shipped contract is **0.10.1** — 119 paths, **236** schemas. The summary serves four figures this section did not declare (`reason`, `energy_total_haler`, `fee_total_haler`, `recommended_advance_kc`), counted months carry `paid_on` and `due_clamped`, and the collections use a module-local `limit` (100/500, non-clamping) that is a **known defect, not a design**. **See §V8-12.**

| Group | Paths |
|---|---|
| Odečty | `GET\|POST /api/electricity/readings` · `GET\|PATCH\|DELETE /api/electricity/readings/{id}` |
| Ceník | `GET\|POST /api/electricity/tariffs` · `GET\|PATCH\|DELETE /api/electricity/tariffs/{id}` |
| Zálohy | `GET\|POST /api/electricity/advances` · `GET\|PATCH\|DELETE /api/electricity/advances/{id}` · `GET\|POST /api/electricity/payments` · `GET\|PATCH\|DELETE /api/electricity/payments/{id}` |
| Období | `GET\|POST /api/electricity/periods` · `GET\|PATCH\|DELETE /api/electricity/periods/{id}` |
| Computed | `GET /api/electricity/summary` · `GET /api/electricity/intervals` · `GET /api/electricity/history` |

Conventions, and the choices inside them:

- **Cursors are dates, via a new shared `DateCursor` parameter** (§V8-10 D149). `readings` keysets on `read_on`, `tariffs`/`advances` on `effective_from`, `periods` on `starts_on`, and `payments` on its `YYYY-MM` key. These collections are ordered by a natural chronological key, and ordering by UUIDv7 would misplace a back-filled row — the same reasoning that produced `finance`'s month cursor (§V6-6, D92). A malformed cursor **422s** rather than silently re-serving page one. `DateCursor` is a *separate* component from `Cursor`; anything that "tidies" a `DateCursor` back into a `$ref: Cursor` has broken paging.
- **The three computed endpoints own no state.** `summary`, `intervals` and `history` are pure projections of the five tables through `compute.go` and are recomputed on every read (D152).
- `summary` and `intervals` default `period_id` to the period containing today, and `404` when there is none — the first-run state is "create a period", not an empty dashboard.
- **`summary.cost_total_haler` and `summary.balance_haler` are nullable and absent — never 0 — in `insufficient_data` and `blocked`** (§V8-10 **D161**). "The module never shows a number it hasn't earned" is a property of the type, not a rule each screen has to remember.
- **`summary.headroom` carries `kwh_mix_dkwh`**, the 30/70 figure Přehled leads with, served rather than derived on the client (§V8-10 **D162**).

**Two pre-existing enum gaps closed while writing 0.10.0.** Both had been silently stale since v6:

- `NotificationRule.filter_module` listed only `todo, events, notes, documents, dashboard, logging, platform, admin` — **`finance` and `garden` were missing**, so an admin composing a trigger rule could not qualify a finance or garden action key. Now includes `finance`, `garden` and `electricity`.
- `WidgetCatalogEntry.module` listed only `todo, events, notes, documents, logging` — **`finance` and `garden` were missing** despite both shipping widgets. Now includes them, and deliberately **not** `electricity`, which publishes no widget (D147).

---

## V8-7. Frontend (v8)

Four routes, added to the "více" overflow as one nav destination (**Elektřina**) with in-page sub-navigation. New code lives in `src/modules/electricity/*` — not the legacy `src/routes/` placement v6's finance code took (§V6-13), which remains an open Phase 1 tidy for that module rather than a pattern to copy.

| Route | Page |
|---|---|
| `/elektrina` | **Přehled** — the current period: the predicted nedoplatek/přeplatek, consumption and cost so far with the VT/NT split, zálohy paid vs. expected, the doporučená záloha, and the odečet age line |
| `/elektrina/odecty` | **Odečty** — readings with the interval that ends at each: days, VT/NT kWh, energie Kč, and which ceník priced it |
| `/elektrina/cenik` | **Ceníky a poplatky** — ceník versions with derived validity ranges, plus the záloha schedule and its due day |
| `/elektrina/historie` | **Historie** — consumption and cost per month, VT vs NT, and past periods with computed vs. invoiced |

**Přehled is the module.** With nothing on Nástěnka and no notification, nothing will remind anyone this module exists, so the landing screen has to answer the question within one screenful at 375 px. The headline is the balance with its period-end date and, while `ends_on_confirmed` is false, the words **předpokládaný konec**; directly under it the basis — *"predikce z průměru za posledních 122 dní"* — so a prediction is never read as a fact.

Three states the design must treat as first-class rather than as edge cases:

1. **`insufficient_data`** — with one reading and no second, there is no average and no forecast. This is what Karel sees on day one and for weeks after, so Přehled shows the **headroom** figure (FR-E9) at full weight instead of an empty panel or a zero.
2. **`blocked`** — valid figures above the gap, and one prominent Czech line naming the missing odečet, with a button that opens the reading form pre-filled with that date. Blocked must not read as an error, and the still-valid numbers above it must not read as untrustworthy.
3. **`complete`** — the closing reading exists, the wording changes from *predikce* to *skutečnost*, and the invoice comparison appears (D157).

Inputs take **whole kWh** — Karel's meter has no decimal (OQ-V8-1) — while the storage keeps the tenth so a future meter needs no migration. Numbers are Czech-formatted throughout: `21 560 Kč`, `4 858,65 Kč/MWh`, `1 234,5 kWh`, `24. 6. 2026`.

Charts use `--c1…--c5` under the **Path A** aliasing (`HANDOFF-design.md`), with **mandatory secondary encoding** — VT and NT sit edge-to-edge in the share bar, so they differ by pattern or direct label and never by colour alone. Month columns carry the "přibližné" caveat from D138, and must be visually distinguishable from the haléř-exact Kč figures beside them: this is home's first screen that shows an approximation and an exact number in the same view.

Offline: every page renders read-only from the persisted TanStack Query cache; write controls disable. No print view.

### Czech UI vocabulary (§V8-10 D133)

| Concept (code, English) | Czech UI |
|---|---|
| The module | **Elektřina** |
| Reading | **Odečet** |
| High / low tariff register | **VT** / **NT** |
| Price list version | **Ceník** |
| Monthly fees | **Měsíční poplatky** |
| Advance payment | **Záloha** · schedule = **Předpis záloh** · due day = **Splatnost** |
| Settlement period | **Zúčtovací období** |
| Supplier's invoice | **Vyúčtování** |
| Underpayment / overpayment | **Nedoplatek** / **Přeplatek** |
| Prediction / actual | **Predikce** / **Skutečnost** |
| Recommended advance | **Doporučená záloha** |
| Expected period end | **Předpokládaný konec** |

---

## V8-8. Non-Functional Requirements (v8)

- **Correctness.** One surface carries the entire module and is pinned accordingly: **`compute.go` is pure** — no `database/sql` import, asserted by a test — and takes a loaded snapshot in, returns the summary out. It is this module's `split.go`. Its tests include the two worked fixtures (the general two-ceník example landing on 21 560 Kč / +40 Kč / 1 795 Kč at splatnost 15., and Karel's real day-one state producing `insufficient_data`), a whole-month-costs-exactly-the-fee case, the 12-months-for-24.-6. case, a due-day-31 February case, and a property test asserting that the sum of interval energy costs plus the sum of fee chunks equals the period total for random reading/tariff sequences.
- **Reproducibility.** Every displayed figure must be reconstructible by hand from what is on screen. That is why intervals are listed with their own costs, why the counted months are listed with their amounts, and why the rounding points are specified rather than left to the implementation.
- **Observability:** baseline unchanged. No job, no outbound call, nothing to log beyond request logging.
- **Security:** no new surface at all — no new egress, no untrusted input, no file handling, no new role.
- **Performance:** hundreds of rows over the app's lifetime; every computation is a single pass over a period's readings. Nothing is cached because nothing needs to be.
- **Backup:** the five tables ride Litestream `home/`. No bucket, no blobs.

---

## V8-9. Configuration (v8)

**None.** v8 adds no environment variable, no secret and no feature flag. Like v6, the module runs on the existing configuration unchanged; `HOME_TIMEZONE` (`Europe/Prague`) already governs the date boundaries every calculation depends on.

There is deliberately **no settings table** either. The three things a settings screen would hold — prices, záloha, period — are all first-class, date-versioned entities, because in this module "the current value" is never the whole truth.

---

## V8-10. Decisions (D133–D162)

- **D133 — `electricity` is the tenth module.** Czech UI **Elektřina**, route `/elektrina`, nav in "více". Migration block **11**, OpenAPI **0.10.0**, `HANDOFF-10-electricity.md`. Czech vocabulary fixed in §V8-7.
- **D134 — A reading is the meter state at 00:00 of `read_on`.** Consumption of day *d* = `reading(d+1) − reading(d)`; a period `[starts_on, ends_on]` is closed by the reading dated `ends_on + 1`, which also opens the next period. Every boundary rule in the module is a corollary.
- **D135 — The ceník is three numbers.** Cena VT, cena NT (Kč/MWh) and měsíční poplatky (Kč/měs), all **including DPH and distribuce**, used as typed. No itemization, no VAT rate, no jistič. Chosen after the itemized alternative was specified and rejected.
- **D136 — A ceník version governs all days ≥ `effective_from`;** its end is derived from the next version and never stored. Editing a version moves only its own days.
- **D137 — Hard block.** A ceník change strictly inside a reading interval makes that interval unpriceable; the module names the missing odečet and computes nothing after it, while days before the gap stay valid. **Money is never interpolated.**
- **D138 — The history chart may interpolate kWh** across an interval's days so month columns can be drawn — display only, labelled approximate, never feeding a Kč figure. D137 and D138 must be read as a pair.
- **D139 — Settlement periods are user-set, inclusive, non-overlapping and never locked.** The vyúčtování is recorded as optional fields and produces a computed-vs-invoiced comparison rather than a state transition.
- **D140 — A period requires a reading on `starts_on`.** Without a baseline it shows no money at all — the direct consequence of D137, confirmed explicitly rather than inferred.
- **D141 — Prediction is a plain average since the period start,** VT and NT separately, measured from the opening reading to the **latest reading**. Days after the last reading are forecast, not actual. Chosen over a seasonal correction, which the module has no history to compute anyway.
- **D142 — Each forecast day is priced with the ceník effective on that day,** so an already-entered future price change is honoured automatically.
- **D143 — Poplatky are pro-rata by days:** a day costs `monthly_fee(day) / days_in_month`, summed per (calendar month × ceník version) chunk with one rounding, so a whole month inside one version costs exactly the fee.
- **D144 — Zálohy are a schedule plus optional real payments.** `{effective_from, amount, due_day}` versioned like the ceník; a `{month, amount}` payment row wins for its month; attribution is by the month key, not by the payment date.
- **D145 — A calendar month counts toward a period iff the period contains that month's first day.** A year-long period is always exactly 12 months — verified against 24. 6. 2026 – 23. 6. 2027, which counts červenec 2026 … červen 2027 and excludes červen 2026.
- **D146 — `balance = zálohy − cost_total`;** positive is a **přeplatek**, negative a **nedoplatek**. `recommended = (cost_total − zálohy already due) / months not yet due`, rounded up to whole koruny, floored at 0, omitted when no month remains.
- **D147 — No widget, no metric, no list, no push.** The first home module contributing nothing to Nástěnka or the notification catalogs. Exactly **three** of the four non-registry host maps are edited; `platform/widgets/registry.tsx` is **not** one of them.
- **D148 — Energy is INTEGER tenths of kWh, money is INTEGER haléře, no floats in the money path.** One rounding per interval, one per fee chunk. All of it in a pure `compute.go` with its own tests — this module's `split.go`. The form takes **whole kWh** (the meter has no decimal) while the storage keeps the tenth, so a future meter needs no migration.
- **D149 — Keyset cursors are dates,** via a shared `DateCursor` parameter distinct from the UUIDv7 `Cursor`; malformed values 422. The `finance` month-key precedent (D92) generalised.
- **D150 — Výměna elektroměru is out of scope.** The schema assumes one monotonically non-decreasing pair of counters and refuses a reading that would decrease either register. A known limitation taken knowingly, not an oversight: were it to happen, the swap date would need a manual data patch or a later version.
- **D151 — Ordinary all-roles module:** `reader` reads, `editor`/`admin` write, soft delete, **no admin-only route**. Four screens. Přehled breaks consumption and cost down VT vs NT with each tariff's share.
- **D152 — Everything is computed on read.** No derived column, no cache table, no scheduler job, no seed migration. `11001_electricity.sql` is the module's only migration.
- **D153 — `ends_on` is an expected date until confirmed.** It defaults to `starts_on + 1 year − 1 day`, carries `ends_on_confirmed`, and while false the UI says **předpokládaný konec** and the prediction names the date it projected to. Correcting it is one field.
- **D154 — The vyúčtování record stores four figures** — invoiced total, invoiced balance, and the supplier's final VT and NT readings — so a discrepancy is attributable to kWh rather than only to Kč. Still no locking.
- **D155 — The záloha schedule carries `due_day`** (1–31, clamped to the month's last day at read time, the D74 precedent). Read in exactly one place — whether a counted month is already paid — so it moves only the doporučená záloha, never the period total or the month count. **At equality the month counts.**
- **D156 — "No chase" means no push, no metric, no list and no widget — and one plain in-app line.** Přehled shows *"poslední odečet před N dny"* with a **Zadat odečet** button. Text on a page you already opened is not a notification, and it is also the honest explanation of why a prediction has gone stale.
- **D157 — A period with its closing reading is entirely actual.** Once a reading dated `ends_on + 1` exists the forecast span is empty, Přehled says *skutečnost* instead of *predikce*, and the computed-vs-invoiced comparison becomes meaningful. *(Surfaced by the `HANDOFF-10` pass — the spec had described only the mid-period case, so a finished period would have gone on forecasting an answer it already had.)*
- **D158 — A displayed VT/NT cost split rounds VT and gives NT the remainder,** so the parts always sum to the rounded total. D148 rounds once on the VT+NT sum, and two independent roundings would occasionally miss the headline by a haléř. The `needs` pattern from the fin split, reused deliberately. *(Surfaced by the `HANDOFF-10` pass.)*
- **D159 — "Cost per month" on Historie is an allocation of exact interval costs by day count,** never a repricing of the interpolated kWh. Without this, D137 and the month-cost chart contradict each other outright. *(Surfaced by the `HANDOFF-10` pass.)*
- **D160 — A ceník version may be deleted unless the deletion would leave a day inside a settlement period with no effective version** (409). The earlier "covering existing days" wording would have frozen every version ever used; deleting a middle version legitimately reprices its days and is audited like any other change. *(Surfaced by the `HANDOFF-10` pass.)*
- **D161 — `summary.cost_total_haler` and `summary.balance_haler` are NULLABLE, and null — never 0 — in `insufficient_data` and `blocked`.** 0.10.0 first declared both `required` integers, which contradicted this spec's own rule that the module *"never shows a number it hasn't earned, not a zero"*: the wire would have been forced to emit `0` in exactly the state where a `0 Kč nedoplatek` is a lie that looks like good news, leaving the honesty rule dependent on every screen remembering to gate on `status` first. Made nullable and dropped from `required`, matching `recommended_advance_haler`'s existing shape, so the absence is carried by the type. `actual` stays populated when it is valid — the figures **before** a gap do not become unknown because the ones after it are. *(Surfaced by the implementation-planning pass, 2026-08-20.)*
- **D162 — `summary.headroom` carries `kwh_mix_dkwh`,** the 30 % VT / 70 % NT figure Přehled leads with. 0.10.0 declared only the all-VT and all-NT figures, while the brief, the build guide and the design all required the mix and pinned **~200 kWh** as a test value — and a client cannot derive it from the two published kWh figures without reconstructing the prices, landing on a different number than the server's own tests. Computed with **one** division and no intermediate rounding: `divRound(energy_budget_haler · 100000, 3·price_vt_haler + 7·price_nt_haler)`; rounding a blended price first moves the answer by a whole kWh. The mix remains a **stated heuristic, never a measurement**, and the Czech copy says so. *(Surfaced by the implementation-planning pass, 2026-08-20.)*

Neither D161 nor D162 adds a schema or a path: 0.10.0 stays **13 paths / 235 schemas**, and both are amendments to `ElectricitySummary` and `ElectricityHeadroom` in place.

---

## V8-11. Acceptance Criteria (v8)

- [ ] `electricity` registers through `registry.Module` — routes, migrations, `AuditActions()` — and `internal/arch`'s **`TestModulesDoNotImportEachOther`** stays green. It imports **none** of `platform/metrics`, `platform/lists`, `platform/push`, `platform/scheduler`, `platform/blobstore`, and a test asserts it.
- [ ] `electricity` contributes **no** widget, metric or list — asserted against all three catalogs, so a future refactor cannot quietly add one.
- [ ] **`compute.go` has no `database/sql` import** (asserted) and every figure on every screen comes through it.
- [ ] The worked fixture (`V8-electricity-brief.md` §4.5) — period 1. 4. 2026 – 31. 3. 2027, two ceník versions, záloha 1 800 splatnost 15., readings 1. 4. and 1. 8. — returns cost **21 560 Kč**, balance **+40 Kč**, doporučená záloha **1 795 Kč**. The same fixture at splatnost 25. returns **1 796 Kč**, and both are tested.
- [ ] **Karel's real day-one state** — one reading (24. 6. 2026, 32/70), no second — returns `insufficient_data` and a headroom of **857,65 Kč/měsíc ≈ 176 kWh VT / 213 kWh NT / 200 kWh at the 30/70 mix** (D162), and the UI renders the designed empty state rather than a spinner, a blank panel or a zero.
- [ ] In `insufficient_data` **and** in `blocked`, `cost_total_haler` and `balance_haler` are serialized as **`null`, not `0`** (D161) — asserted on the wire, not only in the UI — while `actual` stays populated whenever it is valid.
- [ ] A ceník version added with a **future** date changes no past or present figure and immediately changes the forecast for days on and after its date.
- [ ] Editing a ceník version's prices changes only the days that version governs — a regression test asserts a preceding period's total is unchanged to the haléř.
- [ ] An interval straddling a ceník change is **refused, not estimated**: `blocking` reports `chybí odečet k <date>`, figures before the gap are unchanged, and adding that reading resolves it without moving any earlier number.
- [ ] A period with no reading on `starts_on` reports `blocking` of kind `period_start` and shows **no money at all**.
- [ ] The period **24. 6. 2026 – 23. 6. 2027** counts **12** months of zálohy, červenec 2026 … červen 2027, and červen 2026 is not among them.
- [ ] A whole calendar month inside one ceník version is charged **exactly** the monthly fee — no haléř lost to pro-rata rounding.
- [ ] The energy total equals the sum of the per-interval energy costs shown on Odečty, and the poplatky total equals the sum of the (month × ceník) fee chunks, for a fixture with two ceník versions and a partial month at each end. The displayed VT + NT parts sum exactly to the energy total (D158).
- [ ] A reading that would make either register decrease is refused `422` naming the neighbouring reading — checked against **both** neighbours, so a back-filled reading is validated too.
- [ ] A `due_day` of 31 marks února's záloha due on the 28th (29th in a leap year); changing `due_day` moves the doporučená záloha and leaves the period total and the month count identical.
- [ ] Entering the closing reading (`ends_on + 1`) flips `status` to `complete`, empties the forecast, changes the wording to *skutečnost*, and enables the computed-vs-invoiced comparison in both Kč and kWh (D157).
- [ ] Changing an unconfirmed `ends_on` re-projects the whole forecast and changes no actual figure; `ends_on_confirmed` removes the **předpokládaný konec** badge.
- [ ] Deleting a ceník version is `409` when it would leave a day inside a period unpriced, and succeeds otherwise (D160).
- [ ] Every mutation writes an audit event **in the same transaction**; every `update` carries a field-level diff; `electricity.*` appears in the log browser filter and in the trigger composer's module list.
- [ ] The **three** non-registry host maps are updated (`AppShell` OVERFLOW, the Log's `MODULES`, `admin/listener.go`'s `inAppURL` → `/elektrina`) and **`platform/widgets/registry.tsx` is untouched**.
- [ ] Migrations run `… garden(10) → electricity(11)` cleanly on an empty DB and after a Litestream restore; there is no seed source.
- [ ] OpenAPI **0.10.0** validates; the new paths reuse the shared `Limit` / `responses` / security components and the new `DateCursor` parameter, and **no electricity collection `$ref`s the UUIDv7 `Cursor`**.
- [ ] Frontend: four routes, Czech vocabulary per §V8-7, whole-kWh inputs, VT/NT distinguished by more than colour, month columns marked approximate and visually distinct from exact Kč figures; nav shows Elektřina in "více" for all roles.
- [ ] Offline: every electricity page renders read-only from cache; write controls disabled.

---

## V8-12. As built — the v8 build (2026-08-20/21, OpenAPI 0.10.0 → **0.10.1**, deployed 2026-08-21)

> v8 was built from this spec in one pass (repo `main`: PR #17 `4d217c1`), followed by two fixes — PR #18 `afce38e` (the log contract) and PR #19 `81102af` (log-summary units). Five tables at block **11**, **13 paths / 28 operations**, 15 audit actions, one pure `compute.go` (~1 150 lines), **no widget, no metric, no list, no push, no scheduler job, no blob storage, no seed and no env var** — every one of those five bans enforced by `internal/arch` as a test rather than a comment, and `platform/widgets/registry.tsx` verified **absent from the diff**. Everything in §V8-1…§V8-11 still holds except where recorded below.

### The contract moved: 0.10.0 → **0.10.1**

0.10.0 described the intention; the server serves slightly more than it. **0.10.1 is the reconciliation, and is what is deployed** — 119 paths, 236 schemas, 6 parameters. It also closes a defect that predates v8: **`backend/openapi.yaml` in the repo was still at 0.8.0**, because neither the v7 nor the v8 build updated it; 0.9.0 and 0.10.0 existed only in `handoff/v7/` and `handoff/v8/`. The served contract had described neither `garden` nor `electricity`. Where a description in `openapi.yaml` is marked ⚠ AS BUILT it records a deviation the code took and the spec followed — not a design.

### Decisions taken during the v8 build (D169–D175)

- **D169 — a counted month's `due_on` always comes from the SCHEDULE's `due_day`, never from a payment's `paid_on`.** The design prototype used `paid_on` when a payment existed, which makes a payment recorded **without** a date never count as due and quietly skews the doporučená záloha downwards — the one number the module exists to produce. D155 also says `due_day` is read in exactly one place, which only a uniform rule honours. A month with **no schedule at all** falls back to the month's **last day**, so a past unscheduled month counts as due and a future one does not. Neither pinned fixture moves. *(2026-08-20 — flagged in the build as "worth Karel's eye".)*
- **D170 — `no_tariff` is not a blocking kind; it is `status: insufficient_data` plus a `reason`, and the summary therefore carries a `reason` enum on the wire.** Internally there are three blocking kinds, on the wire two (`period_start`, `tariff_change`): "no ceník covers the period start" is not a *measurement* gap, it is a missing precondition, and rendering it as a blocked interval would invite the user to add a reading that fixes nothing. `reason` is one of `no_tariff`, `need_second_reading`, `second_reading_same_day`, `missing_opening_reading`, `tariff_change_inside_interval`, omitted when empty. *(2026-08-20.)*
- **D171 — `forecast` is an ALL-ZERO span when `status` is `complete`, and null when `blocked`.** 0.10.0 said null for `complete`. A closed period has a real, empty forecast window (`from_on == to_on`, `days: 0`), and emitting it as a span keeps the Přehled component that renders the forecast from needing a null branch on the one screen where the answer is finally certain. Read emptiness off `days`, not off nullness. `blocked` genuinely produces nothing. *(2026-08-20.)*
- **D172 — the summary serves four figures 0.10.0 did not declare, because a client that re-derived them would disagree with the server's own tests.** `energy_total_haler` and `fee_total_haler` (nullable in exactly the same states as `cost_total_haler` — they are filled together or not at all), `recommended_advance_kc` (the koruna original from which `recommended_advance_haler` is derived ×100 — the ceil-div of D146 happens once, in Kč, and the haléře form is a convenience, not a second calculation), and on each counted month `paid_on` and `due_clamped`. `POST /electricity/periods` also accepts the five `invoiced_*` fields, so a period whose vyúčtování is already in hand can be entered in one call. *(2026-08-20.)*
- **D173 — the log summary is the ONE place the server formats money and energy.** Everywhere else the API hands over haléře and tenths of kWh and the screen formats them; a summary is prose frozen at write time, so a wrong unit is not a rendering bug a later deploy repairs — it is a wrong sentence sitting in the log forever. `kc()` prints whole koruny with Czech non-breaking thousands separators and a new `kc2()` the two-decimal form for **unit prices and fees**, mirroring the split already stated in the frontend's `format.ts`: a total's last haléř is the residue of a dozen roundings and means nothing, while a unit price's haléře **are** the number, typed off the supplier's contract. `kwh()` gets the same grouping and non-breaking space, so a five-digit register reads `12 345,6 kWh` in the log exactly as on Odečty. *(PR #19, 2026-08-21.)*
- **D174 — empty collections serialise as `[]`, never `null`, and `AuditEventDetail.changes` is REQUIRED.** Go marshals a nil slice as `null`; an event with no field changes therefore crashed a client indexing into the log's detail view, and a filter matching nothing was indistinguishable from a failed request. Both halves are now pinned by tests — a Go test asserting `[]` on every log endpoint, and a Vitest file asserting each view's **error branch beats its empty state**, so reordering the ternary fails the build. This is a platform-wide contract hardening that happens to have shipped after v8. *(PR #18, 2026-08-21.)*
- **D175 — the "poslední odečet před N dny" nudge escalates in WORDS ONLY.** This closes the one design question `HANDOFF-design.md` §v8 left open. The sub-line changes at **15** and **90** days; the colour and the size never do. If 47 days and 200 days rendered identically the line would stop informing — but if 200 days turned red, the module would have started chasing, which Karel refused (D147/D156). The register is explanation, not reproach. `days_since_last_reading` can also be **zero or negative** (a reading dated today, or a closing odečet entered ahead of time); neither goes through the counting branch, because *"před −42 dny"* is nonsense. *(2026-08-21.)*

### Known defects, recorded not fixed

- **The electricity collections use `limit` default 100 / ceiling 500, and do NOT clamp.** Every other module uses the house 50/200 **and clamps** an out-of-range value; here `?limit=600` returns 100 items rather than 500 or 200. `openapi.yaml` documents the shipped behaviour through a module-local `ElectricityLimit` parameter marked ⚠ AS BUILT so nobody reads it as design. The fix is three lines in `http.go` plus reverting the parameter to `$ref: Limit`.
- **A negative `invoiced_vt_dkwh` / `invoiced_nt_dkwh` surfaces as a 500, not a 422.** There is no service-layer validation, so the value reaches the table's `CHECK` and the raw `sql` error propagates unwrapped. Every sibling field (`amount_haler`, the prices, the registers) is validated properly. The spec now carries `minimum: 0` on both, with the defect noted.

### Known limitations, deliberate

**Výměna elektroměru is out of scope (D150) and there is no escape hatch.** The monotonicity guard is **global across all readings**, not per period, so the first reading on a new meter — starting near zero — is refused with a 422 naming the old meter's last reading and cannot be entered at all. **Do not work around it with a manual DB edit**: the audit trail would then not explain the jump, which is exactly what a future reader needs explained. The smallest honest fix, recorded so nobody re-derives it: add `meter_id` (or an `offset_dkwh`) to `electricity_readings`, scope the monotonicity guard and the interval walk to one meter, and make the walk **break** at a meter boundary rather than span it — roughly a day including tests, plus a migration.

**The prediction has no seasonality.** D141 is a plain average since the opening reading, so a period starting in June and first predicted in August will **under-forecast the heating season**, with the mirror error in February. This is the deliberate trade: a plain average is explainable in one sentence and wrong in a knowable direction. Other accepted limits: one odběrné místo (a second would need `site_id` on four tables and a scope on every read), no FVE/přetoky/plyn/voda, no invoice PDF (hence no blob storage), no back-fill importer, and the headroom mix is a stated guess.

**Resist, in order:** adding a widget · adding a metric "since it's nearly free" · storing a computed total · interpolating kWh into a price · allocating a fee into an interval · adding a fourth rounding point.

### Verification carried out at build time

`compute.go` was written and pinned by both worked fixtures **before a line of SQL existed**, and its tests were mutation-checked twice: turning the forecast's single division into a rounded-average-times-*n* drifted the total by 19 haléře and failed four assertions, and a real `float64` declaration failed the purity test. Verified against a real boot, not only in tests: migrations applied through block 11 on an empty database; Karel's day-one state returned `insufficient_data` with all four totals as JSON **`null`**, headroom **857,65 Kč ≈ 176 / 213 / 200 kWh**, and **12 counted months** 2026-07…2027-06 with 2026-06 absent. A second reading flipped it to `ok`, fee chunking reconciled by hand (7/30 of June + 11 whole months + 23/30 of June 2027 = 7 708,20 Kč), a ceník change between two readings produced the hard block, and **adding the missing odečet left the pre-gap interval byte-identical** — the D137 acceptance criterion, checked on the wire. The catalogs read **15 actions, 0 metrics, 0 lists, 0 widgets**.

**Six frontend bugs were found by looking at the running app**, none of which any test would have caught: the negative day count in the nudge, an ungrouped `1460 Kč` beside a grouped `1 500 Kč`, *"dosud"* printed as the end of a **future** ceník's validity range, one month total rendered at two precisions on the same screen, and the headroom chips showing `200,6 / 176,5 kWh` where the spec pins whole `200 / 176`. All fixed; the last is now pinned by a test.

**Two AA failures were found and fixed in the delivered design tokens.** `--el-approx` measured **3.91** dark / **3.38** light against `--s1` while carrying the interpolation footnote — a full sentence of body text — and `--el-over` measured **4.33** on its own soft chip in light, where the chip sets bold 13 px text that WCAG counts as body, not large. Both retuned; every new token now clears 4.5:1 in both themes (approx 5.3/4.97, over-on-soft 6.47/5.35, blocked-on-soft 6.33/6.00, under 8.14/5.18). The check was done by canvas-sampling sRGB in the browser, because `getComputedStyle` returns `oklch()` here and naive parsing silently produces nonsense.

### Still owed by Karel

1. **The záloha's `due_day`** — a number 1–31 for the 1 500 Kč záloha. There is **no seed**: it is entered through the UI. (The **15** in the fixture is the brief's worked example, not Karel's answer.)
2. **The supplier's real period end date**, when it arrives — one `PATCH` of `ends_on` + `ends_on_confirmed`, and every number follows on the next read.
3. ⚠ **When the first záloha was paid.** Červen 2026 does **not** count toward the period (the period does not contain 1. 6.). If a záloha was actually paid in June for this supply it belongs to the period — record it as a payment for **`2026-07`**. Do **not** "fix" it by changing the counted-months rule.

---

## V9-1. Overview (delta)

- **One-line summary (add):** every note and document now lives in one of **two trees** — the household's or the uploader's own — and Administrace gains an **Úložiště** page that says how much space each module, each member and each bucket is actually using.
- **Modules (add): none.** v9 is the first version of Home that adds no module. It changes three that exist — `notes`, `documents`, `admin` — and adds one platform strand, `platform/storage`.
- **Depends on (add):** nothing. No new external call, no sidecar, no new bucket. v7's Open-Meteo poll remains home's only outbound dependency.
- **Scale target:** unchanged. Two members, a few thousand notes and documents over the app's lifetime, a bucket in the single-digit gigabytes. The storage snapshot is one `dbstat` scan plus two prefix listings, behind a 60-second cache.

### Architecture — v9 is a change, not an addition (§V9-10 D176)

Every release from v3 to v8 was additive: a new module arrived through the registry, and nothing that already worked had to be re-examined. That is why each of them could be written as a self-contained delta and built in one pass. **v9 cannot be.** It alters four tables that have carried real household data since v3 and v4, and it invalidates an assumption that roughly forty call sites were written against:

> Until v9, every row in `notes` and `documents` was visible to every member.

The trees, both searches, the resolver, the four content endpoints, the permalink, the two pinned widgets, the two metric providers, the one list provider (`notes` ships one; `documents` does not), the audit spine, the log browser, the trigger listener, the websocket fan-out and the mirror/GC jobs all rest on it. Each is a place where "private" can leak, and §V9-4a enumerates **twenty-three** of them as a table rather than as prose, because a list of leak sites is a checklist and a paragraph is not. It had eighteen rows until a review pass found five more — among them the log's entity timeline, the trigger template's `change.*` tokens and a year-long `immutable` HTTP cache header. **Treat twenty-three as a floor, not a total.**

The consequence for the build is stated once here: **the new code is the easy half.** `platform/storage` and the Úložiště page are ordinary work. The risk is entirely in the seams.

### Architecture — a second root, not a checkbox (§V9-10 D177, D178)

A private item is not a shared item with a flag. `folders`, `notes`, `document_folders` and `documents` each gain `visibility` (`shared` | `private`) and `owner_id` (`NULL` when shared, the auth user id when private), and a tree is addressed by its **root scope** — the pair `(visibility, owner_id)`, of which there are `1 + N` per module for `N` members. Each private root is a full tree: arbitrarily deep subfolders, the same folder CRUD, the same emoji icons, the same slug rules, the same move.

The per-item-flag alternative was specified and rejected. It puts folders of mixed visibility into a tree the household shares, so every member sees folders whose contents differ from what the folder claims to hold, and *"why is this folder empty for me?"* becomes a permanent question with no good answer.

**The model is really enforced by four indexes.** Both modules dedupe root-level siblings with a `COALESCE(parent, '')` sentinel, because SQLite treats `NULL`s as distinct and a plain `UNIQUE(parent_id, slug)` would not constrain the root at all. That sentinel now collides with itself: two members who each keep a private note called *Recepty* at their own root both key on `('', 'recepty')`, and the second one 409s against a note the second member cannot see. So the sentinel carries the root scope:

```
COALESCE(parent_id, 'root:' || visibility || ':' || COALESCE(owner_id, ''))
```

Four indexes, not one — `folders`, `notes`, `document_folders`, `documents`. This is the single line most likely to be copied unchanged, and the bug it produces surfaces only when two people happen to pick the same name, which in a household is often, and which no single-user test reaches.

### Architecture — private means private, with exactly one asymmetry (§V9-10 D180, D181)

Reads of a private item are refused to everyone but its owner, **admins included**, and they are refused with **404** rather than 403 — on `GET` and on `HEAD`. A 403 confirms that an id exists, which turns the permalink route into an existence oracle over the whole private tree.

Against that stands one power and one only: an `admin` may **hard-delete** a foreign private item. `GET` 404s, `DELETE …?hard=true` returns 204. The asymmetry is deliberate — somebody has to be able to reclaim space and remove a departed member's files — and it is the only place in v9 where anyone gains anything.

### Architecture — the audit spine is written in full and redacted on the way out (§V9-10 D187, D188, D189)

A private mutation writes the same event it always did — real summary, real field diffs — plus `meta.visibility="private"` and `meta.owner_id`. Redaction happens **at read time**, in one function, `audit.Redact(entry, viewer)`: for anyone but the owner the summary becomes a fixed Czech phrase, the `entity_id` is dropped and `changes` comes back empty.

Redacting at write time was the obvious alternative and is wrong: it would redact the record for the person it belongs to, permanently, and the spine would stop being the owner's own history. The price is that redaction has to be applied at every read path, which is why there is exactly one function — and why the log browser's full-text search gets a **second, different rule**. Redacting a search hit still tells the searcher that their term occurs in a private title, so for a non-owner private events are **excluded from `?q=` matching entirely**, while still appearing, redacted, in unfiltered browsing. Two rules, deliberately, because one rule leaks.

The same redacted entry is what the **push** renders from — once, for the whole resolved audience, the owner included (D189). A coalescing window builds one envelope per rule, not one per recipient; an owner-only second rendering would exist solely to carry the real title, and would be one audience-resolution bug away from delivering it to the household.

### Architecture — `platform/storage` is the fourth registered catalog (§V9-10 D191, D192)

`admin` may not ask `documents` how big it is: `internal/arch` fails the build on a cross-module import. Home has answered this shape of question three times already — the widget catalog (`registry.Catalog` over `registry.WidgetProvider`, in `platform/registry`; there is no `platform/widgets` Go package — `platform/widgets/registry.tsx` is a *frontend* file), plus `platform/metrics` and `platform/lists`, which are the two built on `platform/catalog`. v9 adds the fourth, built the way §V5-12 corrected those two: an **optional `Source` interface plus a `*Registry` assembled at composition**, never a package-level `Register` global.

A module declares the **tables it owns** (a plain `[]string`, so the platform can size them without knowing what they mean) and its **attributed blob usage** (rows of `{prefix, owner_id, visibility, objects, bytes}`, which only the module can compute — only `documents` knows that `documents/{id}/original` maps to `documents.created_by`, and only `notes` knows that `note-images/{id}` maps through `note_images.note_id` to its note's owner). **All ten feature modules declare tables** — `logging` 3, `todo` 7, `events` 3, `dashboard` 1, `notes` 5, `documents` 4, `admin` 3, `finance` 1, `garden` 13, `electricity` 5 — plus `platform`'s own four; **two of the ten additionally declare blobs**; `admin` reads the registry and imports nothing.

**v9 touches none of the four non-registry host maps** — no new module, no nav entry, no log-browser filter, and `platform/widgets/registry.tsx` stays untouched for the second version running. ⚠ **But it does touch a fifth, pre-existing map nobody had counted**: `admin/labels.go`'s `actionLabels`, a hand-maintained `module.action → Czech phrase` table that falls back to the raw key, so v9's five new actions would show as `notes.note.publish` in the rule composer (D213). And `inAppURL` — one of the four — needs a **change in the opposite direction**: not a new case, but a private-event fallback (D207). So the honest count is **six maps, two of which v9 edits.**

Against that, v9 opens a **new registration surface** — the storage catalog — and this one is closed by machine: a test enumerates `sqlite_master` and asserts that every user table is declared by exactly one module or named in the platform's own list. **A new table with no home fails the build.** It is the first of these surfaces that cannot be forgotten, which is the argument for closing the others the same way rather than adding a seventh to the list.

---

## V9-2. Goals & Non-Goals (delta)

**Goals**

1. Let a member keep something in Poznámky or Dokumenty that the rest of the household does not see, without leaving the module, learning a second concept, or losing folders, search, pinning or the permanent URL.
2. Make "private" mean private — including from an admin, including through the log, the push, the search index and the permalink — so the word does not have to be qualified in the UI.
3. Let a member publish something private into the shared tree in one action, and make that action conspicuous and audited, because it is irreversible.
4. Answer, from one admin screen, *what is using our space* — per module, per member, shared versus private, database versus objects — accurately enough to act on and honestly enough to trust.
5. Change nothing about how the household's existing notes and documents behave. Every row that exists on the day v9 deploys stays exactly as visible as it was.

**Non-Goals (v9)**

- **No per-person sharing.** Two visibilities, not an ACL — no grants, no share links, no groups. "Soukromé" is *mine*, "sdílené" is *the household*.
- **No unpublish** (D182). A document the household has relied on for six months cannot vanish into one member's private tree.
- **No encryption at rest** beyond what R2 and the droplet already provide. Private is access-controlled, not encrypted-to-the-user, and an admin holding the database file can read anything. The UI does not imply otherwise.
- **No privacy in any other module.** Úkoly, Okno, Finance, Zahrada and Elektřina are household-wide and stay so.
- **No storage history, growth curve, forecast, quota or automatic cleanup** (D195, D196).
- **No storage metric, list, widget or push** (D199) — considered against the v7 frost precedent, costed, and deferred with the reason recorded.
- **No per-user routing on `/ws`** (D190).
- **No private-item browser** beyond the purge screen's id / owner / kind / size listing (D198).
- Still not this version's job: relocating the legacy `routes/` screens into `src/modules/*` (open since v6), and the `fin` switch-off (§V6-12 step 5).

---

## V9-3. Users, Roles & Auth (delta)

No change to Mode B, the session, CSRF or the role model. What changes is that **an item now has an owner**, and ownership is a third axis beside role.

- **Reads of shared items** — any authenticated member including `reader`, unchanged.
- **Reads of private items** — the **owner only**. Every other caller, `admin` included, gets **404**.
- **Writes to private items** — the owner, and the owner must additionally hold `editor`/`admin` as before. A `reader` cannot create a private note, because a `reader` cannot create a note.
- **Publish** — the **owner only** (D182). An admin cannot publish someone else's private item: publishing what you cannot read is not a power that should exist.
- **Hard delete** — `admin`, as today, and now also over foreign private items (D181), which remain unreadable to them.
- **The storage page and the purge screen** — `admin` only, gated exactly like the Log browser and the rest of Administrace.

**Ownership is taken from the session, never from the request body.** `owner_id` is set from `reqctx` at creation and is not a writable field on any endpoint — the same discipline the v5 audience resolution follows for roles (§V5-8).

---

## V9-4. Functional Requirements (v9)

Every mutating requirement records an audit event through the spine **in the same transaction**; stated once, not repeated. Reads are not logged — with one exception, FR-V9-14.

### V9-4a. The twenty-three surfaces — the leak table

This is the requirement, not a summary of it. Each row is a place written before v9 that assumed universal visibility.

> **The table grew from eighteen rows to twenty-three during the review pass, and the five it gained are the interesting ones** — they are the surfaces nobody thinks of, which is exactly why they are the ones that leak. Rows **14**, **15b**, **19**, **21** and **23** (the log's entity timeline, the trigger template's `change.*` tokens and the push URL, HTTP response caching, image upload into a foreign note, and the slug-collision suffix) were all absent from the first draft of this section. Assume the table is still incomplete and add to it rather than trusting its length.

| # | Surface | v9 rule |
|---|---|---|
| 1 | `GET /api/{notes,documents}/tree` | Returns **one** root scope, chosen by `?scope=`, default `shared`. Never both trees in one response. |
| 2 | `GET /api/{notes,documents}` — list **and** `?q=` search | Same scoping. The FTS query joins the base table, so the visibility filter rides **in the same SQL**, never as a post-filter. |
| 3 | `GET /api/{notes,documents}/resolve` | Takes `?scope=`; a slug path is meaningless without one. |
| 4 | Detail by id — `/notes/{id}`, `/documents/{id}`, both folder gets | **404** for a non-owner. Never 403 (D180). |
| 5 | The four documents content endpoints — `/raw`, `/download`, `/preview`, `/thumbnail` | 404 for a non-owner, on **GET and HEAD** (both are registered; the HEAD branch is not dead code). |
| 6 | `/d/{id}` permalink | The same 404, rendered as the ordinary "nenalezeno" screen — not as a permission error. |
| 7 | `/api/notes/images/{id}` | An image inherits its owning note's visibility (D204). 404 for a non-owner. |
| 8 | Pins | A **household** pin on a private item ⇒ **422**. A **personal** pin ⇒ owner only. |
| 9 | `notes.pripnute` / `documents.pripnute` widgets | The **pins** are already per-caller, but both providers call `store.AllFolders(ctx, false)` to build breadcrumbs, which after v9 loads **every member's private folders**. Nothing is emitted from it today; scope the call anyway, because the next person to add a folder name to a widget row will not know. A private row carries a lock mark; a published item **keeps** its personal pin. |
| 10 | `notes.pinned_count` / `documents.pinned_count` metrics, and the **one** list provider (`notes.pinned_count` — `documents` ships no list) | Both metrics are already `ScopePersonal`, so they follow the pin rules unchanged — **verified by test, not assumed**. |
| 11 | Audit write | Full summary, full diffs, plus `meta.visibility="private"` and `meta.owner_id`. |
| 12 | Log browser — browsing | Redacted at read time for non-owners: fixed phrase, no `entity_id`, empty `changes` (D187). |
| 13 | Log browser — `?q=` | Private events **excluded from FTS matching** for non-owners (D188). |
| 14 | Log browser — `GET /api/logs/entity/{type}/{id}`, the `?entity_id=` filter, and `GET /api/logs/stats` | **The three the first draft missed** (D209). The entity timeline returns `AuditEventDetail` **with full `changes`** for any id — and the purge screen hands admins ids by design. The `entity_id` filter matches the raw column, so N redacted rows still confirm an id exists (the D188 argument, applied harder: an exact match is a stronger oracle than a lexical one). `stats` counts private events into admin-visible dimension and bucket totals. |
| 15a | Audit outbox → trigger rules → push **text** | Rendered **once**, from the redacted entry, for the whole audience including the owner (D189). |
| 15b | The trigger template's **`{{change.<field>.new/old}}`** tokens and the push's **`URL`** | Redaction of `summary` is not enough (D207). `RenderContext` is built from the **raw** entry, so an existing rule with `{{change.original_filename.new}}` delivers a private filename to every lock screen; and `inAppURL(e)` returns `/d/{entity_id}`, naming the private id even when the text is clean. For a private event: **`Changes` is empty and the URL falls back to the module route** (`/dokumenty`, `/poznamky`). |
| 16 | `/ws` fan-out | Payloads stay id-only; the hub keeps broadcasting to every client. The residual leak is **named, not fixed** (D190). |
| 17 | Documents mirror + orphan reconciliation, notes image GC | Visibility-blind by design — they work on keys and bytes. They must stay that way, and must not start reading titles for a log line. |
| 18 | Hard delete | The one asymmetry: `admin` may purge a foreign private item and may never read one (D181). |
| 19 | **HTTP response caching on the five content endpoints** | `ImmutableContentCache` (`private, immutable, max-age=31536000`) is applied today to all four documents streams and to `/api/notes/images/{id}`. `private` excludes shared *proxies*, **not the other person using the same laptop** — and `immutable` suppresses revalidation for a year, so the v9 404 never runs. The header now **depends on visibility**: shared keeps `immutable`, private gets **`private, no-cache, must-revalidate`** (D208). |
| 20 | Storage statistics + purge screen | Sizes, counts, owners, dates. **Never** a title, filename or content type (D197, D198). |
| 21 | `POST /api/notes/{id}/images` | Upload **into** a foreign private note — the write side of row 7, and absent from the first draft. 404, like every other read or write against a note that is not yours. |
| 22 | `idx_documents_checksum` | Dormant: nothing queries it today. The 07001 comment anticipates a *"this file is already here"* UI, which would be a **cross-scope duplicate oracle** the moment it ships. Named here so it is scoped when it is built, not after. |
| 23 | The **slug-collision suffix** | Not a rule but a leak: `freeSlug` silently appends `-2`, so an un-scoped `SiblingSlugTaken` gives the second member `recepty-2` — a slug that discloses an invisible sibling. Scoping the four indexes is **not enough**; the store's collision query takes the scope too (D210). |

**Twenty-two rows deny; row 18 is the only grant.**

### Notes module (`notes`) — changed in v9

#### FR-V9-1: The private root
- **Trigger:** a member switches to **Soukromé poznámky** and creates a note or a folder there.
- **Behaviour:** the item is written with `visibility='private'` and `owner_id` = the session's user id (D177). It is a full tree (D177): folders nest arbitrarily deep, carry emoji icons, take slugs, and move within the private tree exactly as in the shared one. Slug uniqueness is per **root scope** (D178). `owner_id` is never accepted from the request body (§V9-3).
- **Outputs:** the created item, carrying `visibility` and — for private items — `owner_id`.
- **Errors:** `401`; `403` non-editor write; `422` `scope` disagreeing with the parent folder's scope.

#### FR-V9-2: Scope on every read
- **Behaviour:** `GET /api/notes`, `/tree` and `/resolve` take `?scope=shared|private`, **default `shared`** (D184). `private` resolves to the caller's own private root — a member cannot request another member's scope, and there is no parameter that would express it. The `?folder_id=root` sentinel is now **scope-relative** (D203): it selects the root of the requested scope.
- **Errors:** `422` unknown `scope` value.

#### FR-V9-3: Search is scoped to the tree you are in
- **Behaviour:** FTS5 search over title + body runs inside one root scope, chosen by the same `?scope=` (D184). The visibility predicate is part of the search query, joined to the base table — **never applied to a result set afterwards**, which would leak through result counts and paging.

#### FR-V9-4: Publish — one-way, owner-only
- **Trigger:** the owner chooses **Publikovat do sdílených**.
- **Inputs:** `POST /api/notes/{id}/publish` (and `/api/notes/folders/{id}/publish`) with an optional destination `folder_id` in the shared tree and an optional `position`.
- **Behaviour:** in one transaction set `visibility='shared'`, clear `owner_id`, reparent to the destination (null ⇒ shared root), **re-derive the slug** if it now collides in its new scope, and — for a folder — apply the same to **every descendant**. Audited as `notes.note.publish` / `notes.folder.publish` with the `visibility` diff, because "this became visible to the household" is the most consequential thing that can happen to a private item. A personal pin **survives** the publish (D183).
- **There is no unpublish route** (D182).
- **Errors:** `401`; **`404` for a caller who is not the owner — an admin included — and identical to the response for an id that does not exist (D206). Never 403.** A 403 here would answer *"does this id exist, and is it private?"* for any id a caller cares to try, on a route open to every `editor`: it is the permalink oracle D180 closes, reopened with a different verb. `409` an unresolvable slug conflict; `422` a destination outside the shared tree.

#### FR-V9-5: Move refuses to cross scopes
- **Behaviour:** `POST /api/notes/{id}/move` and the folder equivalent **422** when the destination's root scope differs from the item's (D186). Publishing is the only crossing, and it is a different verb on purpose.

#### FR-V9-6: Pins on private notes
- **Behaviour:** `POST /api/notes/{id}/pin {scope:"household"}` on a private note ⇒ **422**, naming the reason in Czech. `{scope:"personal"}` is allowed for the owner only; for anyone else the note does not exist (404). The Nástěnka widget renders a private row with a **lock mark** (D183).

#### FR-V9-7: Note images inherit their note's visibility
- **Behaviour:** `note_images` gains no visibility column. `GET /api/notes/images/{id}` joins to the owning note and **404s** for a non-owner (D204). The image GC and the mirror/reconciliation pass are unchanged and stay visibility-blind (leak table row 16).

### Documents module (`documents`) — changed in v9

#### FR-V9-8: The private root, scope, search, publish, move, pins
- **Behaviour:** identical in every respect to FR-V9-1 … FR-V9-6 over `document_folders` / `documents` — the same two columns, the same root-scope slug index, the same `?scope=`, the same `POST …/publish`, the same cross-scope 422, the same pin rules. The v4 precedent (D40) that Dokumenty mirrors Poznámky's folder model holds here too: **one behaviour, two implementations, deliberately not a shared abstraction** — the same choice `notes/mirror.go` records.

#### FR-V9-9: The content endpoints and the permanent URL
- **Behaviour:** `/raw`, `/download`, `/preview`, `/thumbnail` and the SPA's `/d/{id}` return **404** for a non-owner of a private document, on **GET and HEAD** (D180). Everything else about them is unchanged: the id-based URL is still permanent, the bytes are still immutable, the ETag is still the checksum, Range still works. **Publishing does not change `/d/{id}`** — the key is id-based and independent of folder, slug and scope (D42), so a published document keeps the URL it was shared with.

#### FR-V9-10: Hard delete, and the admin asymmetry
- **Behaviour:** `DELETE /api/documents/{id}?hard=true` and `DELETE /api/notes/{id}?hard=true` succeed for an `admin` over a **foreign private item**, purging the row and its R2 objects, while every read path continues to 404 for that same caller (D181). The audit event records `meta.owner_id` and `meta.by_admin=true`. The response body carries **nothing about the item** — 204, as today.

### Admin module (`admin`) — changed in v9

#### FR-V9-11: Úložiště — the snapshot
- **Trigger:** an admin opens Administrace → **Úložiště**.
- **Behaviour:** `GET /api/admin/storage` returns, computed on read (D195):
  - **database** — the exact total (`page_count × page_size` plus the WAL file's size on disk), then **per module** from the storage catalog, and within each module **per table**. Table bytes come from `dbstat`; if the driver does not expose it the response carries row counts with `bytes: null` and `bytes_available: false` (D193). **It never estimates.**
  - **blobs** — per module, split **shared / per member**, with object counts and bytes, from `blobstore.List` joined back to SQLite for attribution (D194). Objects that resolve to no live row are reported as **`nezařazené`** rather than dropped — that number is the orphan backlog the mirror job already reconciles, surfaced for the first time.
  - **replica** — the **Litestream replica** under the `home/` prefix as one line: objects, bytes, generations, and the newest object's timestamp (D214). Not per module — Litestream replicates the whole file — so it is a total beside the breakdown, never inside it.
  - **backup** — the mirror bucket as one line, objects and bytes, when configured (D205). Between it and the replica, the page finally accounts for the whole R2 bill.
  - **warning** — the threshold, the measured total, and whether it is exceeded (D196).
  - **meta** — `generated_at`, `cached`, and the cache TTL in effect.
- **Outputs:** one `StorageSnapshot`. Admin-only.
- **Errors:** `401`; `403` non-admin. **An unreachable bucket is a `200`, not a 503** — `blobs.available:false` with the reason, database figures intact. A 5xx carrying partial results is a shape no client handles, and blanking the page over an object-store hiccup loses the half of the answer that was measurable.

#### FR-V9-12: The threshold
- **Behaviour:** one environment variable, `HOME_STORAGE_WARN_TOTAL_MB` (default **1024**, `0` disables). Above it the snapshot's `warning.exceeded` is true and the page enters a warning register, marking the largest contributors. **Nothing is ever blocked**; no upload fails because of it; there is no per-user quota and no new 413 (D196). The default is a *change detector, not a bill detector* (settled with Karel 2026-08-21). R2's free allowance is 10 GB and household usage is expected to sit well under a gigabyte, so a threshold placed at the billing cliff would stay silent for years and teach nobody anything. At 1 GB the line fires when something has **changed** — a runaway preview job, an unusually large upload, a member's private tree growing faster than anyone expected — while there is still nine-tenths of the free allowance left to think in. It is a smoke alarm, not an invoice.

#### FR-V9-13: Cache and refresh
- **Behaviour:** the snapshot sits behind a **60-second in-process cache** (`HOME_STORAGE_CACHE_SECONDS`, `0` disables); `?refresh=true` bypasses it. **No table, no scheduler job, no history** (D195). A one-minute TTL is not state: nothing survives a restart, nothing needs a migration, and nothing can be stale for longer than a minute.

#### FR-V9-14: Soukromé položky — the purge screen
- **Trigger:** an admin needs to reclaim space, or to remove a departed member's files.
- **Behaviour:** `GET /api/admin/storage/private-items` lists every member's private items — **`id`, `module`, `kind` (`document` | `note` | `note_folder` | `document_folder` | `note_image`), `owner_user_id`, `owner_label`, `byte_size`, `created_at`, `updated_at`** — and nothing else. **Folders are listed** (D212): `DELETE /api/{notes,documents}/folders/{id}?hard=true&cascade=true` is what actually reclaims a private subtree, and a screen that cannot name a folder cannot do the job it exists for. **`note_image` rows are informational** — there is no delete route for an image, and there should not be: an image belongs to its note and goes when the note does. The screen says so rather than offering a button that 405s. **No title, no filename, no description, no content type, no preview, no download, no search** (D198). Paged with the house UUIDv7 keyset, filterable by `owner_user_id` and `module`, sortable by size. The listing comes **through the storage catalog**, so `admin` still imports no module.
- **Deletion happens through the owning module's existing hard-delete route** — `DELETE /api/documents/{id}?hard=true`, `DELETE /api/notes/{id}?hard=true` — called from the SPA. `admin` gains **no delete path of its own** (D198), so the audit action stays the module's.
- **Opening the listing is itself audited** — `admin.private_items.view`, the only read in Home that writes an event (D198). It is the answer to "who looked".
- **Errors:** `401`; `403` non-admin.

#### FR-V9-15: What `admin` still does not do
- **Behaviour:** no storage metric, no list, no widget, no push, no scheduled summary and no threshold notification (D199). The v7 frost pattern (D113) would fit — publish a key plus one idempotent audit event and let Administrace deliver — and it is out of scope anyway, because it needs a daily job and a *did-I-already-fire-today* marker, which is exactly the stored state FR-V9-13 declines. **Considered, costed, deferred**, so the next person to think of it finds it already thought of.

### Platform — `platform/storage` (new strand)

#### FR-V9-16: The catalog
- **Behaviour:** an optional `Source` interface plus a `*Registry` built at composition (D191), never a package-level `Register` global — the §V5-12 correction, applied from the start this time. A module declares `StorageTables() []string` and, optionally, `StorageBlobs(ctx) ([]BlobUsage, error)` returning `{prefix, owner_id, visibility, objects, bytes}` rows it has already attributed. The platform sizes tables; the module attributes bytes; `admin` reads the registry.

#### FR-V9-17: The completeness test
- **Behaviour:** a test enumerates `sqlite_master` and asserts that **every user table is declared by exactly one module, or named in the platform's own list** (D192). A table with no home fails the build.
- **The allow-list is not optional** (D211): each external-content FTS5 virtual table materialises **five** `type='table'` rows — `X`, `X_config`, `X_data`, `X_docsize`, `X_idx` — so Home's three FTS tables account for **fifteen**, of which three are named in a migration. Add `goose_db_version` and the shadow tables, and attribute each shadow row to the module owning its parent, because `notes_fts_data` is typically among the largest b-trees in the file and a storage page whose totals do not add up has failed at its one job.

---

## V9-5. Data Model (v9)

**No new table.** Three migrations — `01002_private_meta.sql`, `06004_notes_private_scope.sql` and `07004_documents_private_scope.sql` — and nothing else. `admin` gains **no table at all**, because the storage page stores nothing (D195). SQLite → Litestream `home/`; no new bucket, no new prefix.

**Four tables gain the same two columns** (`folders`, `notes`, `document_folders`, `documents`):

| Column | Type | Notes |
|---|---|---|
| `visibility` | TEXT NOT NULL DEFAULT `'shared'` | CHECK in (`shared`, `private`). The column default is the entire migration of existing data (D200) |
| `owner_id` | TEXT NULL | `NULL` ⇔ shared; the auth user id ⇔ private. Never accepted from a request body |

**The pairing invariant** — `shared ⇒ owner_id IS NULL`, `private ⇒ owner_id IS NOT NULL` — is a **service-level check, not a table CHECK** (D179). SQLite cannot `ALTER TABLE … ADD CONSTRAINT`, and rebuilding these four tables is not a routine migration: `notes` and `documents` carry an explicit `seq INTEGER PRIMARY KEY` *precisely because* their FTS5 indexes are external-content and keyed on the rowid, and `06001` says so in a comment. A rebuild renumbers rowids and desynchronises the search index — a failure whose symptom is that search quietly returns the wrong rows. The v8 monotonicity precedent (D148) applies exactly: the check the table cannot express lives in the write transaction.

**The four slug indexes are replaced, not added to** (D178):

```sql
DROP INDEX ux_notes_sibling_slug;
CREATE UNIQUE INDEX ux_notes_sibling_slug ON notes (
    COALESCE(folder_id, 'root:' || visibility || ':' || COALESCE(owner_id, '')), slug
) WHERE archived = 0;
```

and the same for `ux_folders_sibling_slug`, `ux_docfolders_sibling_slug`, `ux_documents_sibling_slug`. The cross-table half of the invariant (a folder and a note under one parent may not share a slug) has no single-index form and stays where it already is — in the write transaction, now scope-aware.

**Two lookup indexes** carry the private reads:

```sql
CREATE INDEX idx_notes_owner_scope     ON notes     (owner_id, visibility) WHERE visibility = 'private';
CREATE INDEX idx_documents_owner_scope ON documents (owner_id, visibility) WHERE visibility = 'private';
```

**One expression index** on the audit table makes redaction cheap — added in the `logging` block as `01002_private_meta.sql`, the only third table v9 touches and the only one that gains no column:

```sql
CREATE INDEX idx_events_private_owner
  ON audit_events (json_extract(meta, '$.owner_id'))
  WHERE json_extract(meta, '$.visibility') = 'private';
```

An expression index needs no rebuild, which is why the redaction marker lives in the existing `meta` JSON rather than in two new columns.

**`note_images` is unchanged** (D204) — an image's visibility is its note's, resolved by a join on `note_images.note_id → notes.id`, both of which are already unique or indexed, so the join needs no new index. *(It is not `idx_note_images_note` that carries it — that index is note→images, for the GC — but the lookup is by primary key into a UNIQUE column and wants nothing added.)*

**Audit actions (add, 5):** `notes.note.publish`, `notes.folder.publish`, `documents.document.publish`, `documents.document_folder.publish`, `admin.private_items.view`. Every existing `notes.*` and `documents.*` action additionally carries `meta.visibility` and, when private, `meta.owner_id`.

**Migration order is unchanged:** `logging(01) → platform(02) → todo(03) → events(04) → dashboard(05) → notes(06) → documents(07) → admin(08) → finance(09) → garden(10) → electricity(11)`. v9 appends within blocks 01, 06 and 07; it opens no new block, which is the schema-level statement of "v9 adds no module".

**Down migrations drop the indexes before the columns** (D200) — SQLite refuses to drop a column an index references, so the reverse order fails halfway and leaves the table wedged.

---

## V9-6. API Surface (v9)

Full detail in `openapi.yaml` (**0.11.0**). All routes session-authorized; writes additionally require CSRF and `editor`/`admin`; reads open to `reader`, subject to ownership. **Six new paths**, taking the contract from 119 to **125**.

| Group | Paths |
|---|---|
| Publish (notes) | `POST /api/notes/{id}/publish` · `POST /api/notes/folders/{id}/publish` |
| Publish (documents) | `POST /api/documents/{id}/publish` · `POST /api/documents/folders/{id}/publish` |
| Storage (admin) | `GET /api/admin/storage` · `GET /api/admin/storage/private-items` |

**Changed in place, which is the larger half of the diff:**

- **A shared `Scope` parameter** — `shared` | `private`, **default `shared`** — added to `GET /api/notes`, `/api/notes/tree`, `/api/notes/resolve`, `/api/documents`, `/api/documents/tree`, `/api/documents/resolve`. One component, six references; a `scope` inlined per path is how six defaults drift apart.
- **`visibility` and `owner_id`** added to the **six** schemas that carry item fields directly — `Note`, `NoteSummary`, `Folder`, `Document`, `DocumentSummary`, `DocFolder`. `visibility` is **required**, `owner_id` is nullable and in no `required` list. Required, not optional: an item whose visibility a client has to infer is an item some client will get wrong. `NoteDetail`, `FolderDetail`, `DocumentDetail` and `DocFolderDetail` are `allOf` compositions over those six and inherit both fields; the tree nodes are `FolderNode` / `DocFolderNode`, which carry the folder and its children rather than fields of their own.
- **`POST /api/notes`, `POST /api/documents`** and **both** folder creates — `FolderCreate` **and `DocFolderCreate`** — accept `scope` (default `shared`), honoured **only when the parent id is null**; otherwise the parent's scope governs and a disagreement is a 422. Missing it on either folder create means that module's private root cannot hold a folder at all.
- **`404` documented where a private item is not the caller's** on every detail, content and folder-get path, with the reason stated in the description so nobody "fixes" it to a 403.
- **`422`** on a household pin over a private item, and on a cross-scope `move`.
- **`AuditEvent` / `AuditEventDetail`** gain **`redacted: boolean`** (required, default false). A client must be able to tell a redacted row from a row about something dull, or the log browser renders "podrobnosti skryty" as if it were a summary someone wrote.
- **`NotificationRule.filter_module`** and **`WidgetCatalogEntry.module`** need no change this time — both were brought current in 0.10.0, and v9 adds no module.

Conventions, and the choices inside them:

- **`GET /api/admin/storage/private-items` uses the house UUIDv7 `Cursor`**, not v8's `DateCursor`: it is ordered by id, not by a natural chronological key. The v8 collections' non-clamping `limit` is a **known defect** (§V8-12), not a pattern — this collection uses the house `Limit` (50/200, clamping).
- **`StorageSnapshot` is a projection, not a resource.** It owns nothing, is never written, and its `generated_at`/`cached` fields exist so a stale figure is visibly stale rather than silently so.
- **Every byte figure in the snapshot is nullable**, and null — never 0 — when it could not be measured (the D161 principle generalised): a `0 B` table on a page whose whole job is reporting bytes is a lie that looks like good news.

---

## V9-7. Frontend (v9)

No new route tree. Two existing modules gain a root switcher, and Administrace gains two tabs.

| Route | Change |
|---|---|
| `/poznamky` · `/dokumenty` | A **root switcher** above the tree: **Poznámky** / **Soukromé poznámky** (resp. Dokumenty). The switcher sets `?scope=` for every query the page makes |
| `/poznamky/soukrome/…` · `/dokumenty/soukrome/…` | The private tree's slug paths. `soukrome` is a **route literal**, and therefore a **reserved slug** at both shared roots (D185) — a shared folder named *Soukromé* gets `soukrome-2` |
| `/administrace` → **Úložiště** | The storage snapshot |
| `/administrace` → **Soukromé položky** | The purge screen |

**The switcher is the whole interaction design.** Everything else about Poznámky and Dokumenty is unchanged — the same tree, the same editor, the same upload queue, the same previews, the same pins — which is the point: a member who never opens the private root should not be able to tell that v9 shipped. What must be unmistakable at every moment is **which tree is on screen**, because the cost of getting it wrong is uploading something private into the household's tree, and §V9-2 provides no way back.

**Lock language, used consistently and only for this:** the private root, private rows in the tree, the private hit in a search result list, and the private row in a pinned widget. A lock mark means exactly *"only you can see this"* and is never borrowed for a disabled control or an admin gate.

**Publikovat do sdílených** is owner-only, sits in the item's own menu rather than on a toolbar, and confirms with a dialog that names what changes — the household will see it, and it cannot be undone. Not a toast-and-hope.

**Administrace** now carries six tabs — Rozeslat · Pravidla · Souhrny · Doručení · **Úložiště** · **Soukromé položky** — which is past the point where a tab row works at 375 px, so it adopts **v7's module tab-strip pattern** rather than inventing a second one.

**Úložiště** leads with the totals — database and objects — then the per-module breakdown, then the per-member split, then the mirror bucket. Byte figures are Czech-formatted (`1,2 GB`, `847 MB`, `12 345 objektů`). Where `dbstat` is unavailable the table shows row counts and says the bytes are unavailable; **it never shows a zero it did not measure.** The v8 exact-vs-approximate token distinction (`--el-approx`) is promoted to a platform-level treatment here — this is the second screen in Home where measured and unmeasured numbers sit side by side.

**Soukromé položky** is deliberately austere: id, owner, kind, size, dates, and a purge action confirmed by typing. No titles, no thumbnails, no search box, no bulk select. It should feel like a maintenance tool and not like a file browser, because the difference between those two things is the entire feature.

Offline: both trees render read-only from the persisted per-user TanStack Query cache, as today. **The private tree's cache is already namespaced per user and cleared on logout** (v5 D71/D73) — verify it, do not assume it, because that cache is now the one place a private title can outlive a session.

### Czech UI vocabulary (§V9-10 D201)

| Concept (code, English) | Czech UI |
|---|---|
| Shared root — notes / documents | **Poznámky** / **Dokumenty** |
| Private root — notes / documents | **Soukromé poznámky** / **Soukromé dokumenty** |
| Visibility | **Viditelnost** |
| Shared / private item | **Sdílené** / **Soukromé** |
| Publish to the shared tree | **Publikovat do sdílených** |
| Owner | **Vlastník** |
| Redacted log entry | **Soukromá položka — podrobnosti skryty** |
| Storage page | **Úložiště** |
| Database (per module) | **Databáze** (**podle modulu**) |
| Object storage | **Objektové úložiště (R2)** |
| Unattributed objects | **Nezařazené** |
| Backup bucket | **Zálohovací bucket** |
| Warning threshold | **Varovný práh** |
| Purge screen | **Soukromé položky** |
| Hard delete | **Trvale smazat** |

Plural forms: *1 položka · 2 položky · 5 položek*; *1 soubor · 2 soubory · 5 souborů*; *1 objekt · 2 objekty · 5 objektů*; *1 tabulka · 2 tabulky · 5 tabulek*. **MB, GB, kB and R2 do not inflect.**

---

## V9-8. Non-Functional Requirements (v9)

- **Security is the whole version.** The leak table (§V9-4a) is the test plan: each of its eighteen rows gets at least one test written from the *attacker's* side — a second member, and an admin who is not the owner — asserting 404 (not 403), an empty result, or a redacted field. A feature whose failure mode is silent needs tests that fail loudly.
- **One redaction function.** `audit.Redact` is the only place a summary is withheld, and a test asserts that the log query and the trigger listener both route through it. Two implementations of a privacy rule is one implementation and one bug.
- **404, not 403, everywhere** — asserted on GET **and** HEAD for all four content endpoints, because a HEAD-only oracle is still an oracle.
- **Correctness of the index.** The colliding-name case (§8.1 of the brief — two members, one name, two private roots) is a required test, and it is the case that fails if the existing index is copied unchanged.
- **Observability:** baseline unchanged. The storage snapshot logs one structured line per **uncached** computation with its duration and the object counts listed, so a slow page is attributable to the bucket rather than guessed at. `admin.private_items.view` is an audit event, not a log line.
- **Performance:** the snapshot is one `dbstat` scan plus one `List` per prefix, behind a 60-second cache. At household scale that is a few thousand keys. The visibility predicates are indexed (`idx_*_owner_scope`, `idx_events_private_owner`) and add one term to queries that already filter on `archived = 0`.
- **Reliability:** an object-storage outage degrades the storage page to database-only figures with `blobs.available:false` — it never blanks the page and never 500s. Nothing else in v9 has a failure mode the app did not already have.
- **Backup:** no new bucket, no new prefix, no new Litestream configuration. The two added columns ride `home/` like every other column.
- **Migration safety:** both migrations are additive (`ADD COLUMN` with a default, `DROP INDEX` + `CREATE INDEX`), touch no rowid, and rebuild no table (D179) — so the external-content FTS5 indexes stay in sync. Verify on a **restored** copy of production, not only on an empty database.

---

## V9-9. Configuration (v9)

**Two new environment variables, both defaulted, neither a secret:**

- `HOME_STORAGE_WARN_TOTAL_MB` — the warning threshold on total primary-bucket bytes. **Default 1024** (1 GB); `0` disables the warning. Nothing is ever blocked by it (D196).
- `HOME_STORAGE_CACHE_SECONDS` — the snapshot's in-process cache TTL. **Default 60**; `0` disables caching (D195).

No new secret, no new bucket credential, no feature flag, and **no configuration for privacy at all** — it is not switchable. A privacy feature with a kill switch is a privacy feature whose guarantee depends on an environment variable nobody re-reads.

---

## V9-10. Decisions (D176–D215)

- **D176 — v9 adds no module.** Three existing modules change (`notes`, `documents`, `admin`) plus one new platform strand (`platform/storage`). OpenAPI **0.11.0**, build guide `HANDOFF-11-privacy-storage.md`, migrations `01002` + `06004` + `07004` only, **no new migration block**. The risk profile inverts: the new code is the easy half, and the difficulty is in ~40 call sites written against an assumption that stops being true.
- **D177 — Privacy is a second root, not a per-item flag.** `visibility` + `owner_id` on `folders`, `notes`, `document_folders`, `documents`; a tree is addressed by its root scope `(visibility, owner_id)`; each private root is a **full tree** with the same folders, icons, slugs and moves. The flag alternative was specified and rejected: it produces shared folders whose contents differ per member, and a permanent *"why is this folder empty for me?"*.
- **D178 — The root scope enters the slug-uniqueness index.** `COALESCE(parent_id, 'root:' || visibility || ':' || COALESCE(owner_id, ''))`, in **four** indexes. Without it two members who each keep a private *Recepty* at their own root collide on `('', 'recepty')` and the second one 409s against a note they cannot see.
- **D179 — The visibility/owner pairing is a service-level invariant, not a table CHECK.** SQLite cannot add a constraint without rebuilding the table, and `notes`/`documents` carry an explicit `seq INTEGER PRIMARY KEY` because their FTS5 indexes are external-content and rowid-keyed — a rebuild desynchronises search. The v8 monotonicity precedent (D148) exactly.
- **D180 — Private means private from everyone, admins included, and the refusal is 404 rather than 403** — on GET **and** HEAD. A 403 confirms an id exists, which turns `/d/{id}` into an existence oracle over the whole private tree.
- **D181 — Exactly one asymmetry: an `admin` may hard-delete a foreign private item and may never read one.** `GET` 404s, `DELETE …?hard=true` returns 204, the audit event records `owner_id` and `by_admin`. Somebody has to be able to reclaim space and remove a departed member's files.
- **D182 — Publishing is one-way and owner-only.** `POST …/publish` sets `visibility='shared'`, clears `owner_id`, reparents, re-derives the slug, and cascades to a folder's descendants in one transaction. **There is no unpublish route** — a document the household has relied on for months must not be able to vanish into one member's tree; the member can re-upload privately and delete the shared copy, leaving both facts in the log. An admin cannot publish someone else's item: publishing what you cannot read is not a power that should exist.
- **D183 — Private items take personal pins only;** a household pin is **422**. The widget row carries a lock mark, and a personal pin **survives** a publish.
- **D184 — Search and every tree read are scoped to one root, via a shared `scope` parameter defaulting to `shared`.** The visibility predicate rides **inside** the FTS query, joined to the base table — a post-filter leaks through result counts and paging.
- **D185 — `soukrome` is a reserved slug at both shared roots.** The SPA route `/dokumenty/soukrome/…` uses it as a literal, so a shared folder named *Soukromé* takes `soukrome-2`.
- **D186 — A `move` whose destination sits in the other scope is 422.** Publishing is the only crossing, and it is a different verb on purpose.
- **D187 — The audit spine is written in full and redacted at read time,** in one function `audit.Redact(entry, viewer)`: fixed Czech phrase, no `entity_id`, empty `changes` for anyone but the owner. Redacting at write time would redact the record for the person it belongs to, permanently.
- **D188 — Redaction has two rules, not one.** Private events are **redacted** in unfiltered log browsing and **excluded from `?q=` FTS matching** for non-owners — a redacted hit still reveals that the search term occurs in a private title.
- **D189 — The push renders from the redacted entry once, for the whole audience, the owner included.** A coalescing window builds one envelope per rule, not one per recipient; an owner-only second rendering exists solely to carry the real title and is one audience-resolution bug away from delivering it to the household. The owner loses a lock-screen title and gains a guarantee.
- **D190 — `/ws` is left alone, and the residual leak is named.** Payloads are already id-only and the hub has no per-user routing; what crosses is a UUID and the timing of a change. Per-user routing is a platform change with its own failure modes, for one opaque identifier. **If `/ws` payloads ever grow a title, this decision expires with them.**
- **D191 — `platform/storage` is the fourth registered catalog,** built as `platform/metrics` and `platform/lists` were corrected to be (§V5-12): an optional `Source` interface plus a `*Registry` assembled at composition, never a package-level `Register` global. Modules declare their tables and their attributed blob usage; `admin` reads the registry and imports no module.
- **D192 — A test asserts every user table is declared by exactly one module or named in the platform's own list.** v9 touches none of the four non-registry host maps but opens a fifth registration surface, and this one is closed by machine: a new table with no home fails the build.
- **D193 — Database totals are exact; per-table bytes come from `dbstat`, and if the driver does not expose it the page shows row counts and says the bytes are unavailable.** `page_count × page_size` plus the WAL file is the only figure checkable against `ls`. **It never estimates** — a guessed byte figure on a page whose whole job is reporting bytes is worse than an honest gap. Whether `modernc.org/sqlite` exposes `dbstat` is **probed at boot, not assumed**, and the answer is recorded in §V9-12. *(Evidence gathered 2026-08-21 says it almost certainly does: `modernc.org/libsqlite3` publishes the constant `DBSTAT_PAGE_PADDING_BYTES`, which in the SQLite amalgamation lives inside the `#if defined(SQLITE_ENABLE_DBSTAT_VTAB)` guard and is therefore only transpiled when that flag is set — the `DBPAGE_COLUMN_*` constants beside it point the same way. Treat the fallback as a safety net rather than a likely path, and keep the probe: this is inference from a package index, not a read of the source.)*
- **D194 — R2 figures come from `blobstore.List`, joined back to SQLite for attribution, and unresolvable objects are reported as `nezařazené`.** Listing rather than summing `documents.byte_size` counts the derived objects (`preview.pdf`, `thumb.webp`) whose sizes are in no table, and makes the orphan backlog the mirror job reconciles visible for the first time.
- **D195 — The snapshot is computed on read behind a 60-second in-process cache; no table, no job, no history.** `?refresh=true` bypasses it. A one-minute TTL is not state: nothing survives a restart and nothing can be stale for longer than a minute.
- **D196 — One warning threshold on total primary-bucket bytes, and nothing is ever blocked.** `HOME_STORAGE_WARN_TOTAL_MB`, default **1024** (1 GB) — a *change detector, not a bill detector* (settled with Karel 2026-08-21). R2's free allowance is 10 GB and household usage is expected to sit well under a gigabyte, so a threshold placed at the billing cliff would stay silent for years and teach nobody anything. At 1 GB the line fires when something has **changed** — a runaway preview job, an unusually large upload, a member's private tree growing faster than anyone expected — while there is still nine-tenths of the free allowance left to think in. It is a smoke alarm, not an invoice. No per-user quota, no new 413. *(The default was 8192 in the first draft, placed at R2's billing cliff; revised down with Karel 2026-08-21 once the purpose was named.)*
- **D197 — Per-user object storage is reported as named totals split shared vs private — bytes and object counts only.** Never a title, a filename or a content type. An admin may see that space is used and by whom, never what it is.
- **D198 — The purge screen lists through the storage catalog and deletes through the owning module's existing hard-delete route.** `admin` gains no delete path of its own, so the audit action stays the module's. The listing carries id / module / kind / owner / size / dates and nothing else — no title, filename, content type, preview, download or search. **Opening it is audited** (`admin.private_items.view`), the only read in Home that writes an event.
- **D199 — v9 adds no widget, no metric, no list and no push,** including the storage-threshold metric the v7 frost pattern (D113) would make natural. It needs a daily job and a fired-today marker, which is exactly the stored state D195 declines. Considered, costed, deferred.
- **D200 — Existing rows become `shared` by column default; there is no backfill and no seed.** Down migrations drop the indexes **before** the columns — SQLite refuses to drop a column an index references, and the reverse order wedges the table halfway.
- **D201 — Czech vocabulary fixed in §V9-7,** including the redaction phrase *"Soukromá položka — podrobnosti skryty"*, which appears in the log, in the push and nowhere else.
- **D202 — v9 touches none of the four non-registry host maps.** No new module, no nav entry, no log-browser filter, no `inAppURL` case; `platform/widgets/registry.tsx` is untouched for the second version running. What it does add is the fifth surface (D191/D192) and the Administrace tab strip, which grows to six tabs and therefore adopts v7's module tab-strip pattern rather than a second one.
- **D203 — The `?folder_id=root` sentinel is scope-relative — and `notes` must first grow one.** In `documents` the sentinel exists and now selects the root of the requested `scope`, meaning two different folders depending on one other parameter (documented on the endpoint, not inferred). ⚠ **`notes` has no sentinel at all**: its handler passes `folder_id` straight through, and the store's predicate is `WHERE COALESCE(folder_id,'') = ?` with a nil pointer dereferencing to `''` — so **omitting `folder_id` on `/api/notes` already means "root notes only", not "all"**, and the 0.10.1 contract's "omit for all" was wrong before v9. That matters now because the same `COALESCE(…, '')` collapses **every scope's root into one bucket**: after v9, `GET /api/notes` would return other members' private root notes unless the scope term is added to that predicate too. It is the identical trap D178 spends a paragraph on, in a second place, and it is fixed in the same way. *(Surfaced by the review pass, 2026-08-21.)*
- **D204 — A note image inherits its note's visibility.** `note_images` gains no column; `GET /api/notes/images/{id}` joins to the owning note and 404s for a non-owner. The image GC and the mirror pass stay visibility-blind — they work on keys and bytes, and must not start reading titles for a log line.
- **D205 — The snapshot reports the backup mirror bucket as one line** — objects and bytes, when configured. It is half the R2 bill and no screen has ever shown it.

### Taken by the review pass (D206–D214, 2026-08-21)

These eight were surfaced by an adversarial read of this spec against the shipped code, before a line of v9 was written. **Five of them are privacy holes in the spec's own first draft**, which is the argument for the leak table existing at all — and for treating its length as a floor.

- **D206 — `publish` refuses a non-owner with 404, not 403.** The first draft specified 403 in five documents at once. On a route open to every `editor`, a 403-for-private / 404-for-unknown pair answers *"does this id exist, and is it private?"* for any id — the permalink oracle D180 closes, reopened with a different verb. The design addendum's "not yours" publish state is withdrawn with it: there is no permission screen for a foreign private item anywhere in v9.
- **D207 — Redaction covers the trigger template's `change.*` tokens and the push URL, not only the summary.** `RenderContext` is built from the raw entry and rendered **once, before the audience resolves**, so an existing rule whose body is `{{change.original_filename.new}}` delivers a private filename to every lock screen even with `summary` redacted — and `inAppURL(e)` returns `/d/{entity_id}`, naming the private id. For a private event `Changes` is empty and the URL falls back to the module route. The template whitelist is by *shape* (`change.<field>.old|new`), so this cannot be fixed by listing fields.
- **D208 — The content endpoints' `Cache-Control` depends on visibility: shared keeps `immutable`, private gets `private, no-cache, must-revalidate`.** All four documents streams and `/api/notes/images/{id}` currently send `private, immutable, max-age=31536000` unconditionally. `private` excludes shared *proxies*, **not the second person using the same laptop**, and `immutable` suppresses revalidation for a year — so without this the v9 404 never executes. The repo already names this threat model in the PWA persister ("a laptop in the kitchen gets used by more than one person"); it simply had not reached the HTTP layer.

    **`no-cache` rather than `no-store`, chosen deliberately** *(revised with Karel, 2026-08-21)*. `no-cache` does not mean "do not cache" — it means "cache, but revalidate before every reuse". The ownership check therefore runs on every view and a second member still gets 404, while a repeat view of a 30 MB private PDF is a **304 rather than a full re-download**. `no-store` would have been marginally stricter — nothing on disk at all — at the cost of re-fetching every preview and thumbnail in the private tree on every render, which is the sort of tax that gets a header quietly removed six months later. Shared items keep the immutable header: their bytes really are immutable and the caching is free.
- **D209 — The log's entity timeline, its `entity_id` filter and `/logs/stats` obey the redaction rules.** `GET /api/logs/entity/{type}/{id}` returns full `changes` for any id — and the purge screen hands admins ids by design. The `entity_id` filter matches the raw column, so N redacted rows still confirm an id exists: the D188 argument applied harder, since an exact match is a stronger oracle than a lexical one. `stats` counts private events into admin-visible dimension and bucket totals. Redaction lives in one function; **the query layer has more than two doors, and all of them need it.**
- **D210 — `SiblingSlugTaken` takes the scope, and the collision symptom is a silent suffix rather than a 409.** The first draft asserted, in five places, that an un-scoped index makes the second member's *Recepty* fail with a 409. It does not: `freeSlug` loops and appends `-2`. So the real symptom is quieter and worse — Andy's private note gets **`recepty-2`**, a slug that discloses an invisible sibling — and **fixing the four indexes alone does not fix it**, because the store's collision query has its own `COALESCE(parent,'')` predicate. Both halves move together.
- **D211 — The completeness test ships with an allow-list, because FTS5 shadow tables are not optional.** Each external-content FTS5 table materialises five `type='table'` rows (`X`, `X_config`, `X_data`, `X_docsize`, `X_idx`); Home's three account for fifteen, of which three appear in a migration. Add `goose_db_version`, and **attribute each shadow row to the module owning its parent** — `notes_fts_data` is typically among the largest b-trees in the file, and a storage page whose totals do not add up has failed at its one job.
- **D212 — The purge listing includes private FOLDERS, and says plainly that `note_image` rows are not deletable.** `DELETE /api/{notes,documents}/folders/{id}?hard=true&cascade=true` is what actually reclaims a private subtree, so a screen that cannot name a folder cannot do the job it exists for. There is no delete route for a note image and there should not be — an image belongs to its note — so the row is informational and the screen says so, rather than offering a button that 405s.
- **D213 — `admin/labels.go`'s `actionLabels` is a sixth host map, and v9 edits it.** A hand-maintained `module.action → Czech phrase` table that falls back to the raw key, so v9's five new actions would surface as `notes.note.publish` in the rule composer. It is not one of "the four" and never was, which is exactly why it has been silently degrading since v6. Counting it here is the point: **the four were never four.**
- **D215 — Soukromé položky is always visible, and its empty state does the explaining.** *(Settled with Karel 2026-08-21.)* The tab does not appear only once a foreign private item exists. Hiding it would hide the *screen*, not the *capability* — an admin can permanently delete another member's private item whether or not there is a tab for it today — and a power that exists but is invisible is worse than one that is stated. So the empty state is written copy, not an em-dash: it says what the screen is for, that it lists ids and sizes and never titles, and that opening it is recorded. It is the one place in the app where D181's asymmetry is explained to the household rather than merely implemented.
- **D214 — The snapshot reports the Litestream replica under the `home/` R2 prefix, as its own line.** *(Added with Karel, 2026-08-21.)* It is R2 space the household is billed for and **no screen has ever shown it** — the same argument that put the backup mirror bucket on the page (D205), and a larger number, because a replica holds many generations where the database figure above reports one. It is deliberately **not** part of the per-module breakdown and never will be: Litestream replicates the whole file, so attributing it per module would be an invention. A total beside the breakdown, with `generations` and `newest_at`, the latter being the practical answer to *"is replication actually running?"* — a question the app has never been able to answer from inside itself.

---

## V9-11. Acceptance Criteria (v9)

**The model**

- [ ] Two members each create a private note (and a private document) called **Recepty** at their own private root. Both succeed and **both slugs are `recepty`** — not `recepty` and `recepty-2` — and neither member can see the other's, asserted for all four tables. ⚠ The failure mode here is **silent, not a 409** (D210): `freeSlug` suffixes rather than erroring, so the assertion must be on the resulting **slug**, and it fails unless `SiblingSlugTaken` is scoped alongside the four indexes.
- [ ] `GET /api/notes?scope=private` returns **only the caller's** root notes — the `COALESCE(folder_id,'')` predicate carries the scope (D203) — and `/api/notes` without a `folder_id` behaves as documented rather than as it did in 0.10.1.
- [ ] The pairing invariant holds: no write can produce `visibility='shared'` with a non-null `owner_id`, or `visibility='private'` with a null one, and the service refuses both with 422 (D179).
- [ ] Both migrations apply cleanly **on a restored copy of production**, not only on an empty database; every pre-existing row reads `visibility='shared'`, `owner_id IS NULL`; **FTS5 search still returns the same rows it did before the migration** (D179, D200).
- [ ] The down migrations run: indexes dropped before columns, no wedged table (D200).

**The leak table — one test per row, written from the attacker's side**

- [ ] `tree`, list, search and `resolve` return exactly one root scope, defaulting to `shared`; no response ever contains both trees; `?scope=` cannot name another member's root (rows 1–3, D184).
- [ ] Detail, folder-get, all four content endpoints, `/d/{id}` and `/api/notes/images/{id}` return **404 — not 403** — to a second member **and to an admin**, on **GET and HEAD** (rows 4–7, D180, D204).
- [ ] A household pin on a private item is **422**; a personal pin by a non-owner is **404**; the widget shows the owner's private row with a lock mark; a published item **keeps** its personal pin (rows 8–9, D183).
- [ ] Both `pinned_count` metrics and the **one** list provider (`notes`; `documents` ships none) are asserted — not assumed — to follow the pin rules under two members with different private content, and both `pripnute` providers' `AllFolders` call is scoped (rows 9–10).
- [ ] A private mutation writes the full summary and full diffs **plus** `meta.visibility` and `meta.owner_id` (row 11).
- [ ] The Log browser shows a non-owner the fixed phrase with `redacted:true`, no `entity_id` and an **empty `changes` array** — `[]`, never null (D174 still holds) — while the owner sees the real summary and both values (row 12, D187).
- [ ] A `?q=` search by a non-owner for a term occurring **only** in a private title returns **no hits**; the owner's identical search returns one (row 13, D188).
- [ ] A trigger rule on `documents.document.create` with audience *all* delivers the **redacted** text to everyone **including the owner** (row 15a, D189).
- [ ] A trigger rule whose body is **`{{change.original_filename.new}}`** (and one on `notes.note.update` with `{{change.title.new}}`) delivers **nothing from the private item** — the tokens render empty — and the push's `URL` is the module route, **not `/d/{id}`** (row 15b, D207). Asserted on the rendered envelope, not on the summary alone.
- [ ] `GET /api/logs/entity/document/{private id}` returns redacted events with **empty `changes`** for a non-owner; `GET /api/logs?entity_id={private id}` returns **no rows** rather than N redacted ones; `/api/logs/stats` totals **exclude** private events for non-owners (row 14, D209).
- [ ] A private document's `/raw` response carries **`Cache-Control: private, no-cache, must-revalidate`** and a shared document's still carries `immutable, max-age=31536000` — asserted on the **header**, since the failure is invisible from the app (row 19, D208). The same split on `/download`, `/preview`, `/thumbnail` and `/api/notes/images/{id}`. A conditional re-request of a private document returns **304 to its owner and 404 to anyone else**.
- [ ] `POST /api/notes/{id}/images` against a foreign private note is **404** (row 21).
- [ ] `/ws` payloads remain id-only; a test asserts no title, name, slug or path is ever published to the hub from `notes` or `documents` — the property D190 depends on (row 15).
- [ ] The mirror, orphan reconciliation and image GC pass over private objects **unchanged**, and a test asserts they read no title (row 16).
- [ ] An admin `DELETE …?hard=true` over a foreign private note and document succeeds (204), purges the R2 objects, audits `owner_id` + `by_admin`, and the same admin's `GET` of the same id still 404s (row 17, D181).

**Publish**

- [ ] Publishing a private document into a shared folder that already holds `smlouva` lands it as `smlouva-2`, **leaves `/d/{id}` unchanged**, keeps the personal pin, and writes one `documents.document.publish` event carrying the `visibility` diff (D182).
- [ ] Publishing a **folder** moves every descendant in one transaction; a failure part-way leaves nothing published.
- [ ] Publish is refused **404** — not 403 — to a non-owner including an `admin`, and the response is **byte-identical** to the one for an id that does not exist (D206). There is **no unpublish route** in the contract (D182).
- [ ] A `move` whose destination is in the other scope is **422** (D186).
- [ ] A shared folder created as *Soukromé* at the root takes slug `soukrome-2`; the route `/dokumenty/soukrome/…` still resolves to the private tree (D185).

**Storage**

- [ ] `GET /api/admin/storage` returns a database total that **matches the file on disk** (`page_count × page_size` + WAL), per-module and per-table figures, R2 per module split shared / per member, `nezařazené` for objects resolving to no live row, the **Litestream replica** line with its generation count and newest-object timestamp, the mirror bucket line, and the warning block (D193, D194, D197, D205, D214).
- [ ] The replica line is **outside** the per-module breakdown and does not contribute to the per-module sums (D214) — a replica attributed to a module is an invented number.
- [ ] Whether `modernc.org/sqlite` exposes `dbstat` is **probed at boot**; where it is absent the response carries row counts with `bytes: null` and `bytes_available: false`, and the page says so. **No byte figure is ever estimated, and no unmeasured figure is ever rendered as 0** (D193).
- [ ] With object storage unreachable the endpoint still returns database figures, `blobs.available:false` and a reason — **no 500, no blank page**.
- [ ] The snapshot is cached for `HOME_STORAGE_CACHE_SECONDS` and `?refresh=true` bypasses it; **no table and no scheduler job exist** for it — asserted against the migration set and the job registry (D195).
- [ ] With `HOME_STORAGE_WARN_TOTAL_MB` set below the measured total, `warning.exceeded` is true and the largest contributors are marked; **no upload anywhere in the app fails because of it** (D196).
- [ ] `GET /api/admin/storage/private-items` lists id / module / kind / owner / size / dates for **both** members and **no titles, filenames or content types**; **private folders appear** and `note_image` rows are marked non-deletable (D212); it uses the house `Limit` (50/200, **clamping** — not v8's defect); opening it writes `admin.private_items.view` (D197, D198).
- [ ] The purge screen's delete calls the **module's** hard-delete route; `admin` exposes no delete path of its own (D198).
- [ ] **Soukromé položky is present in the tab strip even when the listing is empty**, and its empty state states what the screen is for, that it never shows titles, and that opening it is recorded (D215).

**Platform**

- [ ] `platform/storage` is registered as an optional `Source` + a `*Registry` built at composition — **no package-level `Register` global** (D191).
- [ ] `internal/arch`'s **`TestModulesDoNotImportEachOther`** stays green; `admin` imports neither `notes` nor `documents` (D191).
- [ ] A test enumerates `sqlite_master` and **fails on a table declared by no module** — verified by adding a throwaway table and watching the build go red (D192) — and it is **green on the shipped schema**, with the fifteen FTS5 shadow rows attributed to their parents' modules and `goose_db_version` allow-listed (D211). A test that red-lights on day one gets deleted, not fixed.
- [ ] The storage page's per-module byte totals **sum to the exact database total** (D211) — the arithmetic is the page's premise, and shadow tables are where it breaks.
- [ ] `admin/labels.go` carries Czech phrases for all five new audit actions; none appears as a raw key in the rule composer (D213).
- [ ] v9 contributes **no widget, no metric, no list and no push** — asserted against all four catalogs, exactly as v8 asserts it (D199).
- [ ] The **four non-registry host maps are untouched**, `platform/widgets/registry.tsx` included, and the diff proves it (D202).
- [ ] ⚠ **`backend/openapi.yaml` is updated to 0.11.0 in the same PR** — the v7/v8 process lesson, and the first release with a chance to not repeat it.

**Frontend**

- [ ] The root switcher is present on both modules; which tree is on screen is unmistakable at 375 px in **both** themes; every query the page issues carries the matching `scope`.
- [ ] Lock language is used for private items and **for nothing else** — never for a disabled control or an admin gate.
- [ ] **Publikovat do sdílených** is owner-only, lives in the item's own menu, and confirms with a dialog naming what changes and that it cannot be undone.
- [ ] Administrace renders six tabs through **v7's tab-strip pattern**, with no horizontal overflow at 375 px.
- [ ] Úložiště is Czech-formatted throughout (`1,2 GB`, `847 MB`, `12 345 objektů`), with unmeasured figures visually distinct from measured ones.
- [ ] The persisted TanStack Query cache is **verified** to be per-user namespaced and cleared on logout — it is now the one place a private title can outlive a session (v5 D71/D73).
- [ ] Offline: both trees render read-only from cache; write controls disabled with the standard **"Změny nelze uložit offline"**.
- [ ] Accessibility pass at 375/1440 in both themes: the lock mark is not the only carrier of "private" (a text label accompanies it), the warning register passes AA, and the purge confirmation is keyboard-operable.
