# Home — Implementation Handoff (Claude Code) — v2 *(foundation; current app = v5)*

> **Read first:** root `CLAUDE.md` (project conventions), then `PRD.md` (the source of truth), `openapi.yaml`, and `CHANGELOG.md` (what changed). `notes.md` holds the decisions in short form; `HANDOFF-design.md` is the design brief.
>
> Status: v2 issued 2026-07-21 · Owner: Karel. **v2 supersedes v1.** Where any v1 note conflicts with v2, v2 wins.
>
> **v3 (2026-07-29):** adds a fifth module, **Poznámky (`notes`)** — build guide `HANDOFF-5-notes.md`, design brief `HANDOFF-design.md` §v3, `PRD.md` v3 + `openapi.yaml` 0.4.0, decisions D30–D38. It's additive and self-contained: the foundation, auth, and modules 1–4 below are unchanged. If building v3, read this foundation, then `HANDOFF-5-notes.md`.
>
> **v4 (2026-08-11):** adds a sixth module, **Dokumenty (`documents`)** — build guide `HANDOFF-6-documents.md`, design brief `HANDOFF-design.md` §v4 (**design delivered**: `design/Home.dc.html`, `design/DocumentView.dc.html`), `PRD.md` v4 + `openapi.yaml` 0.5.0, decisions D39–D50. Additive and self-contained (foundation, auth, and modules 1–5 unchanged), and the **first module with blob storage** — it owns a dedicated **R2 bucket** for file bytes (metadata stays in SQLite/Litestream). New ops prerequisites: primary + backup R2 buckets (versioning on the primary) and **headless LibreOffice + poppler-utils** in the runtime image. If building v4, read this foundation, then `HANDOFF-6-documents.md`.
>
> **v5 (spec 2026-08-16 · built + deployed 2026-08-17):** adds a seventh module, **Administrace (`admin`)** — build guide `HANDOFF-7-admin.md`, design brief `HANDOFF-design.md` §v5, `PRD.md` §V5-1…§V5-12 + `openapi.yaml` **0.7.0**, decisions D51–D74 (spec) and **D75–D80 (taken during the build — `PRD.md` §V5-12; the brief is stale where it disagrees)**. Admin-only, gated like the Log browser: Web Push over one shared VAPID channel, broadcasts, audit-key trigger rules, and scheduled summaries. Unlike v3/v4 it is not only a feature module — it adds **five platform strands** (`push`, `scheduler`, `metrics`, `lists`, `pwa`) plus an **outbox tailer** in `platform/audit`, and promotes Home to an installable, **reads-only-offline** PWA. New ops prerequisites: a **VAPID keypair** in Coolify secrets (`cmd/vapidgen`, generated once — **never rotate**, it invalidates every subscription) and, optionally, `HOME_PUSH_ENDPOINT_HOSTS`. If building on v5, read this foundation, then `HANDOFF-7-admin.md`.
>
> **Index status (2026-08-21): v6, v7 and v8 are all BUILT AND LIVE.** Home runs ten modules on **OpenAPI 0.10.1**. Build guides: v7 = `HANDOFF-9-garden.md` (Zahrada, block 10), v8 = `HANDOFF-10-electricity.md` (Elektřina, block 11). As-built reconciliations live in `PRD.md` **§V7-12** and **§V8-12** — read them before trusting any version-scoped section below. ⚠ **One process lesson from both builds: `backend/openapi.yaml` was never updated by either**, so the served contract sat three versions behind until a separate pass fixed it. Add it to the module checklist beside the four non-registry host maps. The paragraph below is kept as written at v6 spec time.
>
> **v6 (spec 2026-08-17 — as drafted, since built and deployed 2026-08-18):** adds an eighth module, **Finance (`finance`)** — build guide `HANDOFF-8-finance.md`, `PRD.md` §V6-1…§V6-12 + `openapi.yaml` **0.8.0**, decisions **D81–D98**. It is a functional **clone of the standalone `fin` service** (`fin.tilcer.cz`), which v6 then **retires** — the first version of Home that removes a live service rather than adding a capability. The module itself is the simplest one here: one table, one form, **no scheduler, no blob store, no new platform strand and no new env var**. The risk sits elsewhere — the **locked split formula must be ported verbatim** (D82) and the historic months must be **proven intact before `fin` is switched off** (D97). Ordinary all-roles module in the "více" overflow (D84); joins the audit spine, which `fin` never had (D86); contributes one widget, four metrics and one list (D88–D90). Design brief: `HANDOFF-design.md` **§v6** (drafted 2026-08-17, pending approval) — it also raises a **palette decision** that must be settled before the frontend is built. New ops prerequisites: **none for the app** — but a **one-off data migration and a decommissioning runbook** (`HANDOFF-8-finance.md` §13). If building v6, read this foundation, then `HANDOFF-8-finance.md`.
>
> **Correction to the v4 note above:** Office→PDF preview shipped as a **`home-gotenberg` sidecar** (`HOME_DOCS_GOTENBERG_URL`, internal port 3000), **not** headless LibreOffice inside the backend image — it keeps the image at ~100 MB instead of ~1 GB. Thumbnails use `pdftoppm` + `cwebp`.

## 0. What's different from v1 (read this before scaffolding)

Two structural changes reshape the whole build, so don't lift the v1 structure wholesale:

1. **Auth is Mode B, not Mode A.** Home hosts its **own login** and owns a **session**. The browser carries **no JWT** — requests are authorized by home's session cookie. Home verifies credentials against auth BE→BE (`/internal/login`) and refreshes roles via `/internal/token/mint`. See §4.
2. **The codebase is a compile-time modular monolith.** Each module is self-contained (its own routes, migrations, audit actions, widget providers) in both `backend/` and `frontend/`, wired through a central registry. Nástěnka is a **widget host** that renders module-provided widgets and owns no feature data. See §3 and `HANDOFF-4-dashboard.md`.

## 1. This is an index — build order

| Doc | Scope | Depends on |
|---|---|---|
| **this file** | Foundation — repo, module registry, config, DB, **Mode B auth + session**, CSRF, shell, deploy | — |
| `HANDOFF-1-logging.md` | `logging` — audit spine + log browser | foundation |
| `HANDOFF-2-todo.md` | `todo` — Úkoly board **+ its Právě dělám widget** | foundation, spine |
| `HANDOFF-3-events.md` | `events` — Okno **+ its Připomínky and Tento měsíc widgets** | foundation, spine |
| `HANDOFF-4-dashboard.md` | `dashboard` — the widget host (catalog, layout, grid) | foundation, spine, todo, events |
| `HANDOFF-5-notes.md` *(v3)* | `notes` — Poznámky (Markdown notes, folder tree, `notes.pripnute` widget) | foundation, spine |
| `HANDOFF-6-documents.md` *(v4)* | `documents` — Dokumenty (files in a folder tree, R2 blobs, preview/download, `documents.pripnute` widget) | foundation, spine, **`platform/blobstore` (R2)** |
| `HANDOFF-7-admin.md` *(v5)* | `admin` — Administrace (Web Push: broadcast, audit-key triggers, scheduled summaries) + the PWA | foundation, spine, **`platform/{push,scheduler,metrics,lists,pwa}`** + the `platform/audit` tailer |
| `HANDOFF-8-finance.md` *(v6)* | `finance` — Finance (monthly income split; the retiring `fin` service absorbed, its data migrated, the service decommissioned; `finance.rozpocet` widget) | foundation, spine |

Build the foundation, then 1 → 2 → 3 → 4. The spine is first (every mutation writes through it in the same transaction); the dashboard host is last (it renders the others' widgets). **v3:** `notes` (module 5) slots in as another feature module — migrations after `events`/before `dashboard`; the live host discovers its widget through the registry with **no host edit**. **v4:** `documents` (module 6) slots in the same way — migrations **after `notes`/before `dashboard`** — but additionally introduces a **`platform/blobstore`** R2 client (infra, like `db`/`ws`) and an in-process **preview worker** + **backup-mirror** job; see `HANDOFF-6-documents.md` §2/§4/§13. **v5:** `admin` (module 7) is the only module that is **admin-gated and last in the migration order**; it reaches the other modules only through the audit outbox and the metrics/lists catalogs, never by import — see `HANDOFF-7-admin.md` and `PRD.md` §V5-12 for the shapes the build changed. **v6:** `finance` (module 8) is an ordinary feature module appended at migration block **09** — but it ships a **second, production-only migration source** for the seeded `fin` history (`finance/seed`, block `09900`), which `testsupport` must NOT include; and its build guide carries a **migration + retirement runbook** that gates every decommissioning step on a row-for-row verification. See `HANDOFF-8-finance.md` §7 and §13.

> **Module packaging note for docs 1–3:** each of those handoffs now includes, at the end, the module's **widget provider(s)** and a **"module packaging"** reminder (own routes/migrations/audit, registered via the core; no reaching into other modules). The behaviour of todo/events/logging is otherwise unchanged from v1.

## 2. Repo

**Monorepo `ws-tilcer-home`.** Carry a copy of the root `CLAUDE.md` at the repo root so every session inherits conventions.

```
ws-tilcer-home/
  CLAUDE.md
  backend/
    cmd/home/main.go            # wire config, logger, DB, module registry, router, server, static SPA
    internal/
      platform/                 # the core: NOT a feature module
        config/                 # env (PRD §9)
        registry/               # Module interface + registration; builds router + migration set + widget catalog
        auth/                   # Mode B: /internal/login + /internal/token/mint clients, session store, middleware
        httpx/                  # request-id, request log, CSRF, role gate, error helpers
        db/                     # sqlite open, goose runner (per-module migrations), litestream hooks
        ws/                     # websocket hub (module-agnostic)
        audit/                  # the AuditSink interface + in-process writer (impl lives with logging module tables)
      modules/
        logging/                # audit tables + FTS5 + log-browser routes  (HANDOFF-1)
        todo/                   # boards/columns/cards + Právě dělám widget provider  (HANDOFF-2)
        events/                 # events/recurrence + Připomínky & Tento měsíc providers  (HANDOFF-3)
        notes/                  # folders/notes/pins + FTS5 + Připnuté poznámky widget  (HANDOFF-5, v3)
        dashboard/              # widget host: catalog, per-user layout, fan-out  (HANDOFF-4)
      each module/
        module.go               # implements registry.Module (routes, migrations, audit actions, widgets)
        migrations/*.sql        # THIS module's Goose migrations
        ...
  frontend/
    src/
      platform/                 # api client (cookie + CSRF), auth screens, app shell, theme tokens, widget registry
      modules/
        todo/  events/  logging/  dashboard/  notes/   # each: pages + widgets + query hooks
  design/                       # approved Claude Design bundle (+ v2 addendum when ready)
  docker-compose.yml
  README.md
```

**One Coolify app, same origin** (unchanged from v1): API + built SPA on `home.tilcer.cz`; `/api/**`, `/ws`, `/healthz`, `/readyz`, and SPA fallback. Same-origin, so no CORS for home's own calls; `HOME_ALLOWED_ORIGINS` is only for the CSRF Origin allowlist.

## 3. The module contract (build this in the core first)

Everything hangs off a small registry. Get it right before writing any module.

```go
type Module interface {
    Name() string                         // "logging" | "todo" | "events" | "dashboard"
    RegisterRoutes(r chi.Router)          // mounts the module's /api routes
    Migrations() fs.FS                    // this module's goose *.sql
    AuditActions() []string               // the action verbs it emits (for the log filter catalog)
    Widgets() []WidgetProvider            // dashboard widgets it contributes (may be empty)
}

type WidgetProvider interface {
    Key() string                          // stable, e.g. "todo.pravedelam"
    Title() string                        // Czech
    Module() string
    DefaultSize() string                  // "narrow" | "wide"
    AdminOnly() bool
    Data(ctx context.Context, u User) (any, error)  // user-scoped payload
}
```

**Rules the core enforces (§10 D25/D28):**

- The core builds the router by asking each module to `RegisterRoutes`; runs migrations by concatenating each module's `Migrations()` into one Goose sequence (deterministic order: `logging`, `platform`, `todo`, `events`, `dashboard`); builds the widget catalog from `Widgets()`.
- **No module imports another module's package.** The dashboard host reaches feature data only through `WidgetProvider.Data(...)`. Add an **import-lint / architecture test** that fails CI if `modules/X` imports `modules/Y` — this is an acceptance criterion, not a nicety.
- Shared, non-feature concerns (auth, db, audit sink, ws, config) live in `platform/` and may be imported by anyone.

## 4. Foundation build order

### F1. Config
`internal/platform/config` loads PRD §9 env. New/changed vs v1: `HOME_AUTH_SERVICE_SECRET` (now authenticates `/internal/login` + `/internal/token/mint`), `HOME_SESSION_TTL_DAYS` (default 90), `HOME_ROLE_REFRESH_MINUTES` (default 15), `HOME_ALLOWED_ORIGINS` (CSRF Origin allowlist), plus the v1 timezone/lookback/rrule-cap/retention vars. Fail fast on missing required vars.

### F2. Module registry + DB + backup
Build the `registry` (§3). SQLite (WAL); Goose runner that assembles **per-module** migrations in the fixed order and runs them once on boot. Litestream restore-if-empty **before** serving; seed runs only when empty (the `todo` module seeds its one board). Litestream → R2 prefix `home/`.

### F3. Observability
`/healthz`, `/readyz` (SQLite ping); structured JSON logs to stdout; per-request log with a generated **request id** in context, stamped onto audit events by the sink.

### F4. Mode B auth + session — the biggest v2 change

**Login (FR-A1).** `POST /api/auth/login {email,password}` → call auth `POST /internal/login` with `X-Service-Secret: HOME_AUTH_SERVICE_SECRET` (body `{email,password}`; site is the client's bound site). On success, auth returns the user's site-scoped assertion (identity + roles). Create a **home session**:

- 256-bit random token; store only its **SHA-256 hash** in `sessions` with `user_id`, `email`, `display_name`, `roles` (JSON), `roles_refreshed_at=now`, UA/IP, sliding `expires_at`.
- Set `session` cookie: `HttpOnly; Secure; SameSite=Lax; Path=/`, **host-only** (no Domain), `Max-Age = HOME_SESSION_TTL_DAYS`.
- Set `csrf` cookie: `Secure; SameSite=Lax`, **not** HttpOnly (JS reads it), host-only.
- **Never log or persist the password.** Forward to auth, discard. Log `platform.login` (actor = user).
- Handle auth's responses: `401`→`401` generic; `403`→`403`; an MFA-required response → `409 {error:"mfa_required"}` (home does **not** build MFA UI, §10 D23); `5xx`/timeout → `502`.

**Session authz + role refresh (FR-A2).** Middleware on every `/api/**` except `/api/auth/*`: look up the session by token hash, reject if missing/expired/revoked (`401`), load identity+roles into the request context, enforce the role gate. When `now - roles_refreshed_at > HOME_ROLE_REFRESH_MINUTES`, call auth `POST /internal/token/mint {user_id, site:"home"}`, refresh cached roles, bump the timestamp. **If mint fails closed** (auth says disabled/deleted/unverified), revoke the session and return `401`. Slide `expires_at` on activity.

**Logout (FR-A3):** revoke the session, clear cookies, log `platform.logout`. **Session bootstrap (FR-A4):** `GET /api/auth/session` → `{user}` or `401`.

**CSRF (FR-A5):** middleware requiring `X-CSRF-Token` == `csrf` cookie **and** an `Origin`/`Referer` in `HOME_ALLOWED_ORIGINS` on every cookie-authenticated **mutation** (and logout). Login is exempt (no session yet) but **rate-limited** per email and per IP.

> **Security invariants (PRD §8 / §10 D23, D29):** TLS only; password never logged/persisted; no bearer token in the browser; session token stored hashed; a revoked auth user keeps access ≤ `HOME_ROLE_REFRESH_MINUTES`. State these in code comments where they're enforced.

**Prerequisite (Karel, before F4):** register site `home` in auth (roles `admin`/`editor`/`reader`), provision a **service client bound to site `home`** → `HOME_AUTH_SERVICE_SECRET`, and create the household member accounts in auth (no self-signup on home).

### F5. Websocket hub
`GET /ws`, **session-authenticated** on connect (not a JWT), role-aware. Module-agnostic broadcast hub in `platform/ws`. Modules publish; the frontend applies pushes via `setQueryData`/invalidation with refetch-on-focus fallback.

### F6. Frontend shell + auth screens
- **Login screen** (new v2): email+password → `POST /api/auth/login`; error states incl. the MFA-required → "dokončete přihlášení na auth.tilcer.cz" message; a "Zapomněli jste heslo?" link out to auth; "Nemáte účet? Požádejte správce". **No token handling in JS.**
- SPA calls `GET /api/auth/session` on load → app or login. Fetch wrapper: `credentials:'include'`, attaches `X-CSRF-Token` (read from the `csrf` cookie) on mutations; on `401` route to login (no client token refresh).
- **Signed-out / redirecting shell state** (the v1 gap) — build it here.
- Four-tab nav **Nástěnka · Úkoly · Okno · Log** (Log admin-only); **Nástěnka is the landing route**. Theme tokens from the design bundle, dark in `:root`/light under `.light`. `czPlural` helper. Global loading/empty/error/`reader` patterns.
- **Frontend widget registry** in `platform/`: modules register their widget components by `key`; the dashboard host renders from it (mirror of the backend contract).
- *Login and widget-host visuals need the design addendum (§8) — until then, build from tokens + PRD.*

### F7. Deploy
Single Coolify app; env per PRD §9; `/healthz`+`/readyz` green; Litestream→R2 (`home/`) and a **tested** fresh-build restore. Update `REGISTRY.md` to live when deployed.

## 5. Conventions (all modules)

UUIDv7 ids; lexorank string `position`; soft delete default (`?hard=true`); shared `Error{error,detail}`; English code ids / Czech UI; three Czech plural forms via one helper; dates `d. M. yyyy` in `HOME_TIMEZONE`; dark default. **Every mutation writes an audit event in the same transaction** (the spine, `HANDOFF-1`). **Tests alongside each step.**

## 6. Design reference

`design/Home.dc.html` is the visual source of truth for Úkoly, Okno, and Log. **Do not copy `IMPLEMENTATION-PLAN.md` or `screenshots/`** from the bundle — both are stale (the plan describes a two-module prototype; screenshots disagree with the HTML). The v1 prototype does **not** cover the Mode B login screens or the widget host — those need the §8 addendum.

## 7. Service-wide definition of done

- [ ] Module registry composes routes, per-module migrations (one sequence), and the widget catalog; **import-lint fails on cross-module imports**.
- [ ] **Mode B login** works end-to-end: form → `/internal/login` → home session cookie; **no JWT in the browser**; bad creds `401`, disabled `403`, MFA `409`+link; passwords never logged.
- [ ] Session authz + **role refresh** via `/internal/token/mint`; mint-fails-closed drops the session within one interval; CSRF + Origin enforced on mutations; login rate-limited; logout revokes.
- [ ] All of PRD §11 pass; every endpoint conforms to `openapi.yaml` 0.3.0.
- [ ] Per-module migrations apply cleanly on empty DB and after Litestream restore; seed one board only when empty.
- [ ] Widget host renders module-provided widgets; per-user layout (show/hide, reorder, narrow/wide) persists and syncs; host imports no module tables.
- [ ] Logging spine, todo, events behave per v1; every mutation audited in-transaction; `reader` blocked from mutations (but may arrange own layout); `/api/logs/**` admin-only.
- [ ] `/ws` refreshes widgets + boards live; Czech UI + plurals; dark default; verified 375 px + 1440 px both themes.
- [ ] Coolify deploy green; R2 backup + fresh restore verified; `REGISTRY.md` updated.

## 8. Blocked on: v2 design addendum

The Mode B **login/logout screens**, the **widget host** (grid, drag-reorder, resize, catalog picker, empty state), and the **signed-out** state are not in the approved prototype. The **backend** for all of this can be built now from the PRD. The **frontend** for login + dashboard host should wait for, or be reconciled against, a Claude Design v2 addendum (`HANDOFF-design.md` §v2). Not a blocker for modules 1–3 or any backend work.
