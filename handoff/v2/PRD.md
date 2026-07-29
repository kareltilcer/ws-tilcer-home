# PRD — Home

> Status: **Draft v2** — self-hosted login (Mode B), widget dashboard, modular architecture. Supersedes v1. Decisions D1–D29 (§10); v1→v2 delta in `CHANGELOG.md`. Pending Karel's final approval. · Owner: Karel · Last updated: 2026-07-21
> Companion spec: `openapi.yaml` (OpenAPI 3.1, **v0.3.0**) · Notes: `notes.md` · Design brief: `HANDOFF-design.md` (v2 addendum pending) · Build: `HANDOFF*.md`

> **v2 scope:** a single `home` fe/be pair — a Czech-language, mobile- and desktop-friendly household management system — built as a **compile-time modular monolith**. Four modules:
> 1. **Logging spine** (`logging`) — an in-process audit component every module writes through, plus an admin log browser.
> 2. **To-do** (`todo`) — a Trello-style board (Úkoly).
> 3. **Okno do budoucnosti** (`events`) — all-day, optionally recurring future events with in-app reminders.
> 4. **Nástěnka** (`dashboard`) — the landing page, now a **per-user widget host**: modules contribute widgets, each user shows/hides/reorders/resizes them.
>
> Home hosts its **own login** and owns its **own session** (Mode B), verifying credentials against the shared auth service BE→BE. Everything is built around the logging spine so every module inherits auditability, and around a **module registry** so modules stay isolated.

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

## 2. Goals & Non-Goals

**Goals**

- Host home's **own login** and session (Mode B), authenticating against auth BE→BE, with **no token in the browser**.
- Ship the **logging spine** capturing every mutation across every module as a queryable audit event, atomic with the change; plus an admin log browser.
- Ship **to-do** (Úkoly) and **events** (Okno do budoucnosti) as before.
- Ship **Nástěnka as a per-user widget host**: modules contribute widgets; each user shows/hides, reorders, and resizes them; layout persists server-side and syncs across devices.
- Enforce **real module boundaries** — each module self-contained in `backend/` and `frontend/`, wired through a registry, communicating only through defined contracts.
- Support a **household** (multiple members), Czech UI, dark-default theme, mobile- and desktop-friendly, common actions in one or two taps.
- Faithfully follow project conventions (observability, Goose, Litestream→R2, OpenAPI 3.1).

**Non-Goals (v2)**

- **No self-signup and no password reset on home** — users are admin-provisioned in auth; reset is auth-hosted (home links out).
- **No TOTP or Google sign-in on home** — password-only; MFA/Google stay auth-hosted (§10 D23).
- **No JWT in the browser** — authorization is from home's own session.
- **No runtime-pluggable modules** — modularity is compile-time (§10 D25); no dynamic load/enable, no plugin lifecycle.
- **No user-authored or third-party widgets** — the widget catalog is the set the modules ship (§10 D27).
- **No push or email notifications** — reminders are in-app, computed on read.
- **No per-occurrence event exceptions, no time-of-day on events, no external calendar sync/iCal, no multiple reminders per event.**
- **No card assignee, no card due dates, no collaborative editing, no separate logging microservice, no offline/PWA, no cross-board card moves.**

## 3. Users, Roles & Auth

Home is a consumer **site** (`home`) of auth. Household members are **`single_site`** accounts bound to site `home`, **provisioned by an admin in auth** (no self-signup here). Access is **Mode B**: home hosts login and owns the session (see §1 Architecture).

**Roles** — auth's default template **`admin` / `editor` / `reader`** (§10 D5):

- **`admin`** — full access, including the **log browser** and structural management (create/delete boards, columns, labels).
- **`editor`** — full use of to-do, events, and Nástěnka (create/move/edit/complete; arrange own widgets). No log browser.
- **`reader`** — **view-only**: read boards, cards, events, and their dashboard; no mutations, no log browser. (A reader still arranges their **own** dashboard layout — that's a personal view preference, not a data mutation.)

Roles come from auth (cached in home's session, refreshed via `/internal/token/mint` per `HOME_ROLE_REFRESH_MINUTES`). `roles:["*"]` (superuser) ⇒ full access. Home authorizes every request **server-side from its session** — never from client input.

- **Reads** (`GET` on module data): any authenticated member.
- **Writes** (`POST`/`PATCH`/`DELETE`/move/complete/layout): `editor` or `admin` (layout is exempt — see reader note above).
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

**Migrations:** each module ships its own migration files under its package; the core runs them in one Goose sequence, **logging first**, then platform (sessions, layout), todo, events. Seed the default board only when `boards` is empty. A restored build (Litestream) must not re-seed or double-migrate.

## 6. API Surface

Full detail in `openapi.yaml` (0.3.0). Grouped by module. **The browser carries no bearer token** — `/api/**` is authorized by the **session cookie**; state-changing routes also require the **CSRF header** (FR-A5). Writes require `editor`/`admin`; `/api/logs/**` require `admin`; `/api/auth/*` and health are reachable pre-session.

- **Auth (platform):** `POST /api/auth/login`, `POST /api/auth/logout`, `GET /api/auth/session`.
- **Dashboard host:** `GET /api/dashboard` (layout + widget data, fan-out), `GET /api/dashboard/catalog`, `PUT /api/dashboard/layout`, `GET /api/dashboard/widgets/{key}` (single-widget refresh).
- **To-do:** boards/columns/cards/links/checklist/labels + `/tree` (as v1).
- **Events:** `/api/events` CRUD, `/api/events/occurrences`, links, `/api/events/{id}/complete` (+ undo) (as v1).
- **Logs (admin):** `/api/logs`, `/api/logs/{id}`, `/api/logs/entity/{type}/{id}`, `/api/logs/stats` (as v1).
- **Real-time:** `GET /ws` — session-authenticated websocket; pushes board/column/card **and** event/completion changes so open boards and **dashboard widgets** update live. Each push carries an **`origin`** (the originating tab's `X-Client-Id`, stamped via `reqctx`) so a client can distinguish its own echo from a change made elsewhere. Not modeled in `openapi.yaml`.
- **Health (public):** `/healthz`, `/readyz`.

**Routing:** register static `/api/events/occurrences` before `/api/events/{id}`, and `/api/dashboard/catalog` + `/api/dashboard/widgets/{key}` before any parameterised dashboard route.

## 7. Frontend

React + TS + TanStack Query SPA, **Czech UI** (D20), **dark-default** (D21), organized into **module folders** (`src/modules/{todo,events,logging,dashboard}` + `src/platform`). App shell nav — **Nástěnka · Úkoly · Okno · Log** (Log admin-only), bottom tabs on mobile / side nav on desktop. **Nástěnka is the landing route.**

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

**Úkoly / Okno / Log** — as v1 (board kanban+accordion; month list + event form with series-edit warning; log browser with filters/diffs/timeline/analytics), now living in their module folders.

**Data fetching (TanStack Query):** keys `['auth','session']`, `['dashboard']`, `['dashboard','catalog']`, `['dashboard','widget',key]`, plus the v1 module keys. Move/complete/layout mutations invalidate `['dashboard']`. Websocket pushes refresh affected widgets and boards. A push whose **`origin` differs from this tab** (i.e. a change made on another device/tab) that touches the **route currently on screen** also raises a brief **"changed elsewhere" toast** while the tab is visible, so the live update isn't a silent surprise; a tab never toasts its own change. Explicit empty/loading/error/`reader` states everywhere.

## 8. Non-Functional Requirements

- **Observability (baseline):** `/healthz`, `/readyz` (SQLite ping), structured JSON logs to stdout, per-request log with request id stamped onto audit events.
- **Audit completeness:** in-transaction with the mutation; append-only; every module (incl. login/logout) writes through the spine.
- **Mode B security (§10 D23):** home receives plaintext passwords on `/api/auth/login` — **TLS only, never logged/persisted, discarded immediately**. Home owns session + revocation; a revoked auth user keeps working ≤ `HOME_ROLE_REFRESH_MINUTES`. Session cookie `HttpOnly; Secure; SameSite=Lax`, host-only, hashed at rest, sliding TTL. **CSRF** double-submit + Origin allowlist on cookie-authenticated mutations. Login rate-limited (per email + per IP). Service-client secret (`X-Service-Secret`) high-entropy, in Coolify env only. **No bearer token in the browser.**
- **Module isolation (§10 D25/D28):** no module imports another module's package internals; cross-module data flows only through the widget-provider contract (and the audit sink). Enforce with an architecture test / import-lint so a boundary violation fails CI.
- **Bounded computation:** occurrence expansion and every widget provider are window-bounded and capped; the dashboard fan-out (FR-D3) issues a bounded number of queries with no N+1 across events — it's the landing route.
- **Date correctness:** all-day events avoid clock DST; "today"/month/lead math in `Europe/Prague`; short-month clamping unit-tested (D19).
- **Performance:** household scale. p95 < 50 ms for board-tree, dashboard fan-out, and indexed log queries.
- **Backup:** Litestream → R2 prefix `home/`; fresh build restores before serving; seed only if empty after restore.

## 9. Configuration

Env (Coolify only; nothing secret in the repo):

- `HOME_DB_PATH` · `HOME_SITE_KEY` (default `home`) · `AUTH_BASE_URL` (`https://auth.tilcer.cz` — `/internal/login`, `/internal/token/mint`, and the target of "reset password" / MFA-fallback links).
- `HOME_AUTH_SERVICE_SECRET` — auth **service-client** secret bound to site `home`; authenticates `/internal/login` **and** `/internal/token/mint`. *(New role vs v1, where it only gated introspect.)*
- `HOME_SESSION_TTL_DAYS` — home session sliding window (default 90).
- `HOME_ROLE_REFRESH_MINUTES` — how often home re-mints to refresh cached roles (default 15).
- `HOME_ALLOWED_ORIGINS` — CORS/CSRF Origin allowlist (`https://*.tilcer.cz`).
- `HOME_TIMEZONE` (`Europe/Prague`) · `HOME_DASHBOARD_LOOKBACK_DAYS` (30) · `HOME_RRULE_MAX_OCCURRENCES` (500) · `HOME_LOG_RETENTION_DAYS` (0 = keep forever).
- `LITESTREAM_*` / R2 credentials — prefix `home/`.

**Prerequisite (Karel, before build):** register site `home` in auth with roles `admin`/`editor`/`reader`; **provision a `home` service client bound to site `home`** and put its secret in `HOME_AUTH_SERVICE_SECRET`; create the household member accounts in auth (no self-signup).

## 10. Resolved Decisions

D1–D22 are from v1 (`CHANGELOG.md`). D23–D29 are v2; D2 and D16 are adjusted.

- **D1** multiple boards + switcher · **D3** collapse client-side · **D4** lexorank ordering · **D5** roles `admin`/`editor`/`reader` · **D6** full diffs + FTS5, key entities incl. `event` · **D7** `now`/`done` free-form hint · **D8** soft delete + `?hard=true` · **D9** no scheduler · **D10** websockets (board + event/completion) · **D11** reminders computed on read · **D12** reminders a separate entity · **D13** RRULE subset via `teambition/rrule-go` · **D14** series-only editing · **D15** dashboard mark-done → first `kind=done` column, else archive · **D17** English code ids, Czech UI · **D18** date-only events · **D19** short-month clamping (deliberate RFC deviation) · **D20** Czech-only UI + plural forms · **D21** dark default · **D22** Nástěnka done = 2000 ms press-and-hold with an immediate keyboard path.
- **D2 (adjusted) — token verification:** in Mode B the browser holds **no** JWT; home authorizes from **its own session** and refreshes roles via `/internal/token/mint`. Per-request `/introspect` is dropped from the hot path. The signing secret is still never distributed.
- **D16 (adjusted) — dashboard scope:** widgets define their own scope; *Tento měsíc* legitimately looks ahead. Nástěnka is still the landing route, but "active items only" no longer applies globally.
- **D23 — Auth is Mode B, login + logout only, password-only.** Home hosts login, owns its session, calls auth `/internal/login` + `/internal/token/mint` as a service client. No self-signup (admin-provisioned in auth), no reset UI (auth-hosted), no TOTP/Google on home (auth-hosted; MFA challenge → graceful redirect message). Accepts the Mode B trade-offs (plaintext passwords in transit, own revocation).
- **D24 — Nástěnka is a per-user widget host.** Server-side layout (show/hide, drag-reorder, **narrow/wide** resize), synced across devices. Responsive grid: one column mobile, two-column desktop.
- **D25 — Compile-time modular monolith.** Each module self-contained (routes, **own migrations**, audit actions, widget providers) in `backend/` and `frontend/`, wired via a central registry. One binary; no runtime plugins.
- **D26 — Module code identifiers stay English** (`logging`, `todo`, `events`, `dashboard`, `platform`); UI is Czech. (Extends D17.)
- **D27 — v1 widget catalog:** `todo.pravedelam` (Právě dělám), `events.pripominky` (Připomínky), `events.tento-mesic` (Tento měsíc). No admin log widget in v1. No user-authored widgets.
- **D28 — Dashboard host owns no feature data.** Cross-module data reaches it only through the **widget-provider contract** (FR-M2); the host never queries `todo`/`events` tables. Boundary enforced by an import/arch test.
- **D29 — Home owns a session + CSRF.** Own session store (hashed token, sliding `HOME_SESSION_TTL_DAYS`), `HttpOnly; Secure; SameSite=Lax` host-only cookie, CSRF double-submit + Origin allowlist on cookie-authenticated mutations, login rate-limited. No token in the browser.

## 11. Acceptance Criteria

- [ ] PRD v2 + `openapi.yaml` 0.3.0 reviewed and approved; decisions locked.
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
- [ ] Logging spine, to-do, and events behave per v1 (their acceptance items carry over) — every mutation audited in-transaction; recurrence/clamping/reminder tests pass.
- [ ] `/ws` refreshes affected widgets and boards live across devices.
- [ ] Czech UI + plural forms; dark default. Baseline observability; Litestream→R2 (`home/`) restore verified.
- [ ] `REGISTRY.md` updated (repo, status).

## 12. Changelog

Full detail in `CHANGELOG.md`.

- **v2 (2026-07-21)** — Mode B self-hosted login + own session (no browser token); Nástěnka as a per-user widget host (server-side layout, three module-provided widgets); compile-time modular monolith with per-module packaging and migrations; new `sessions` + `user_dashboard_layout` tables; auth/session + dashboard-host endpoints; OpenAPI → 0.3.0. Decisions D23–D29; D2/D16 adjusted.
- **v1 (2026-07-21)** — Initial four-module spec (logging, todo, events, dashboard), Mode A auth, hardcoded two-list Nástěnka, single `0001_init`. Decisions D1–D22. Design prototype delivered and reviewed. Never implemented.
