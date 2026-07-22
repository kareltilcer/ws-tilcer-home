# Implementation Plan — `home` (household management service)

> **How to use this file:** this is the living build tracker. Each phase and step has a
> checkbox — **mark it `[x]` as soon as it's done and verified** (tests green / behaviour
> confirmed), and keep the "Status" line current. Update this file in the same commit as
> the work it tracks, so the repo always shows exactly how far the build has progressed.
>
> **Status:** **FEATURE-COMPLETE + POLISHED + BROWSER-VERIFIED + REAL AUTH WIRED.** Backend 100% (`go test ./...` green + e2e HTTP). All four frontend screens built; `npm run build` + Vitest (6 gesture) + **Playwright+axe (4, both themes × 375/1440)** green — the a11y pass caught & fixed 3 real WCAG-AA contrast bugs. **Real Mode A auth now wired against the confirmed ws-tilcer-auth contract** (see memory): backend introspect uses `X-Service-Secret`; frontend does session-cookie `/token/refresh` (bearer in-body) on boot, 401→refresh→retry→redirect, login redirect `?site=home`, WS bearer via `?access_token=`, signed-out **RedirectingShell**. Dev keeps the offline stub (REAL_AUTH off in dev; on in prod builds). **F7 deploy is BUILT + FULLY VERIFIED IN DOCKER** (Docker 29.6.2, 2026-07-22): the image builds (72.7 MB), the offline harness (`docker compose up`) boots and serves the API + SPA, and the **fresh-volume restore is actually tested** against a local MinIO R2 stand-in — first run seeds+replicates to the `home/` prefix, volume is wiped, second run restores the data with no double-seed (**PASS**). That test also caught + fixed a first-deploy crash bug (missing Litestream `-if-replica-exists`). Go serves the built SPA via `HOME_STATIC_DIR`. **Only remaining is the live Coolify deploy itself** (config, not code): register `home` in auth (→ `HOME_AUTH_SERVICE_SECRET`), set `VITE_AUTH_BASE_URL`, supply real R2 creds, deploy with `HOME_ENV=production`. **Open (minor):** within-column drag-reorder. Overall ~5.9 / 6. **Run:** backend `go run ./cmd/home` w/ `HOME_DEV_AUTH_BYPASS=true`; frontend `cd frontend && npm run dev`; e2e `npm run test:e2e`.
>
> **Environment note (updated 2026-07-22):** Go 1.26.5 (windows/arm64, `CGO_ENABLED=0`) portable
> SDK at `C:\Users\karel\sdk\go`. Node 24 present. **Docker Desktop 29.6.2 now installed and
> working** (WSL2 backend — required on Win11 Home, no Hyper-V). Gotcha: neither `go` nor `docker`
> is on the *session* shell PATH — Go at `C:\Users\karel\sdk\go\bin`, Docker at
> `C:\PROGRA~1\Docker\Docker\resources\bin` (use the 8.3 short path; the space in "Program Files"
> plus `rm` tokens trips the sandbox's protected-path guard). F7 image build + fresh-restore test
> done in Docker; dev still runs without it via `go run` + `vite dev`.

## Progress at a glance

- [x] **Phase 0** — Repo scaffold & design bundle
- [~] **Phase 1** — Foundation (F1–F7): config, DB, auth, shell, deploy — **F1–F6 ✅; F7 scaffold built + fully verified in Docker** (image serves; fresh-restore via MinIO **PASS**); only the live Coolify deploy (real auth/R2 creds) remains
- [x] **Phase 2** — `logging` (audit + log browser) — backend ✅ + Log frontend ✅ (filters, diff stream, entity timeline, analytics)
- [x] **Phase 3** — `todo` (Úkoly board) — backend ✅ + Úkoly frontend ✅ (open: within-column drag-reorder)
- [x] **Phase 4** — `events` (Okno do budoucnosti) — backend ✅ + Okno frontend ✅ (form + month list + series-edit warning)
- [x] **Phase 5** — `dashboard` (Nástěnka) — backend ✅ + Nástěnka frontend ✅ + hold gesture ✅ (Vitest-verified)

---

## Context

`home` (`home.tilcer.cz`) is a greenfield, Czech-language household-management SPA over a Go + embedded-SQLite backend. It is the second consumer of the shared `auth` service and a member of the `*.tilcer.cz` family. It ships **four modules in v1**, all built around a central **audit-logging spine**:

1. **`logging`** — an in-process audit spine every module writes through (same-transaction), plus an admin-only log browser ("Log").
2. **`todo`** — a Trello-style board ("Úkoly"): sortable/collapsible columns, `now`/`done` column kinds, cards with notes/links/checklist/labels.
3. **`events`** — "Okno do budoucnosti": all-day, optionally recurring future events with one optional in-app reminder.
4. **`dashboard`** — "Nástěnka", the landing page aggregating active reminders + every card in a `kind=now` column across all boards.

**Why now:** the PRD (v0.2.0), OpenAPI spec, per-module engineering handoffs, and an approved Claude Design bundle are all complete and approved. The repo currently contains only those docs + the design zip — no source. This plan turns the approved specs into a concrete, phased build.

**Source-of-truth documents** (`handoff/v1/`): `PRD.md` (behaviour), `openapi.yaml` (API v0.2.0), `HANDOFF.md` (foundation + conventions), `HANDOFF-{1,2,3,4}-*.md` (per module), `HANDOFF-design.md` (design brief). Org conventions: `handoff/CLAUDE.md`. Visual source of truth: `design/Home-handoff-v1.zip → home/project/Home.dc.html`.

## Decisions locked

- **Local-dev auth:** an **env-gated dev bypass** (fake introspection) so the app runs/tests offline. Refuses to start in production; self-flags on `/readyz`.
- **Root conventions:** copy `handoff/CLAUDE.md` → repo-root `./CLAUDE.md` (Phase 0) so every session inherits it.
- **Auth service contract:** the shared `auth` service exists and is documented; **Karel provides its docs**. Real `/introspect`, `/token/refresh`, and WebSocket-auth transport are wired against those docs; until then everything sits behind a narrow introspection interface + the dev bypass.

## Tech stack

### Backend (`backend/`)
- **Go 1.24+** (`toolchain` pinned) · router **`go-chi/chi/v5`** (correct static-before-param matching).
- **SQLite driver `modernc.org/sqlite`** (pure-Go, `CGO_ENABLED=0`, **FTS5 built in**, clean single-container Coolify build) + **boot-time FTS5 probe**. WAL, `busy_timeout=5000`, `foreign_keys=ON`, `synchronous=NORMAL`, single-writer pool.
- UUIDv7 `google/uuid` · migrations `pressly/goose/v3` (library + `//go:embed`) · recurrence **hybrid** (`teambition/rrule-go` parses; hand-rolled stepping + D19 clamp) · WS `coder/websocket` · logging `log/slog` JSON · config hand-written fail-fast loader.
- Tests: `testing` + `testify` + `httptest` + `go-cmp`; **temp-file SQLite** (not `:memory:`).

### Frontend (`frontend/`)
- **Vite + React 19 + TS (strict)** · **Tailwind v4** (native `oklch()` → verbatim token port; v3.4 fallback) · shadcn/ui + Radix + lucide.
- TanStack Query v5 · react-router v7 · react-hook-form + zod · `@dnd-kit/*` (KeyboardSensor = keyboard drag path) · Recharts v2 (verify React 19) · date-fns v4 + tz + `cs` (all-day dates stay `yyyy-MM-dd` strings) · react-markdown + remark-gfm + **rehype-sanitize** · sonner · `Intl` for cs formatting/collation · self-hosted Hanken Grotesk + IBM Plex Mono (latin-ext).
- **No client-state lib** — React context + `localStorage` hooks. Tests: Vitest + Testing Library + Playwright + `@axe-core/playwright`.

## Repo layout (target)

```
ws-tilcer-home/
  CLAUDE.md  README.md  docker-compose.yml  plan.md
  design/                          # committed keep-list from the bundle
  backend/
    cmd/home/main.go               # bootstrap only
    migrations/                    # embed.go + 0001_init.sql (ALL tables, logging first)
    internal/{config,reqctx,db,audit,auth,httpx,lexorank,recur,dates,todo,events,dashboard,ws,testsupport}/
    openapi.yaml                   # copy of handoff/v1/openapi.yaml (single source)
    Dockerfile
  frontend/
    src/{theme,i18n,api,app,components/{ui,common},routes/{nastenka,ukoly,okno,log},hooks}/
```

Deploy: **one Coolify container, same origin** — Go serves `/api/**`, `/ws`, `/healthz`, `/readyz`, and the built SPA (`index.html` fallback for client routes; unmatched `/api` → JSON 404; `/ws` excluded). No CORS for home's own calls.

## Cross-cutting conventions (every module)

- **The audit rule:** every mutating handler opens a tx → mutates → `audit.Sink.Record(ctx, tx, event)` **in the same tx** → commits. No mutation without an audit write. WS publish happens **after** commit.
- IDs UUIDv7; ordering is **lexorank string keys** (a move rewrites exactly one row). Soft delete default (`?hard=true` to hard-delete).
- **English code identifiers, Czech UI.** Modules: `logging|todo|events|dashboard`.
- **Czech plurals (1 / 2–4 / 5+)** via one `czPlural` (port from `Home.dc.html`) for every count. Dates `d. M. yyyy`; all date math in `Europe/Prague`.
- **Dark theme default** — dark tokens in `:root`, light under `.light` (inverse of shadcn; no `.dark` class, no Tailwind `dark:` variant).
- Errors use `Error {error, detail}`; every endpoint conforms to `openapi.yaml` v0.2.0. Tests alongside each step.

---

## Phase 0 — Repo scaffold & design bundle  ✅

- [x] `go mod init github.com/kareltilcer/ws-tilcer-home/backend` + `backend/` tree; `frontend/` Vite (React-TS) tree; `go build ./...` ✓ and `vite build` ✓ both succeed on skeletons.
- [x] Copy `handoff/CLAUDE.md` → repo-root `./CLAUDE.md`.
- [x] Extract design keep-list into `design/`: **`Home.dc.html`, `CardTile.dc.html`, `support.js`, `README.md`** (flattened). Excluded `screenshots/`, `uploads/`. *Note:* the zip has no `DESIGN-DEVIATIONS.md` — `Home.dc.html` is authoritative. The original `Home-handoff-v1.zip` is left in `design/` as the pristine source.
- [x] Copy `handoff/v1/openapi.yaml` → `backend/openapi.yaml` (single spec source).

**Proved:** both toolchains build; conventions inherited at repo root; visual source-of-truth travels with the code.

## Phase 1 — Foundation (HANDOFF F1–F7)

- [x] **F1 Config + entrypoint** — `internal/config` (fail-fast, aggregates all missing vars, dev-bypass gating + prod hard-refusal, `Redacted()` masks secret) + `cmd/home/main.go`. Tested; server exits non-zero listing missing vars. *(also built `internal/idgen` UUIDv7)*
- [x] **F2 DB + migrations + seed** — `internal/db` (`Open` w/ WAL+pragmas+single-writer, `Migrate` via embedded goose, `ProbeFTS5`, `SeedIfEmpty`, `WithTx` atomicity backbone); `migrations/0001_init.sql` creates all tables **logging-first** incl. `audit_events_fts` + sync triggers, all indexes/CHECKs. Tested: full table set incl. FTS5, seed-once guard, tx rollback/commit. *(also built `internal/lexorank` + tests — Phase 3 dep)*
- [x] **F3 Observability** — `internal/httpx` (`RequestID`, `Logger` slog-JSON, `Recover`, `Healthz`, `Readyz` w/ SQLite ping + `insecure_auth` flag, chi router, JSON/Error/DecodeJSON helpers) + `internal/reqctx` (actor/request context carriers + `HasRole`). Tested (httptest) **and verified against a running server**: healthz 200, readyz 200/503, request-id generated+echoed+logged.
- [x] **Audit spine** (`internal/audit`) — `Sink.Record(ctx, *sql.Tx, Event)` reading actor/request from ctx; sqlite writer; **both atomicity tests**, full-value diffs, meta JSON, FTS5 diacritic search + delete-trigger sync. (Read-side log browser endpoints are Phase 2.)
- [x] **F4 Auth (Mode A)** — `internal/auth` `Introspector` + **sha256-keyed TTL cache** (verified cache hit avoids 2nd call) + HTTP introspection client *(ASSUMED contract, flagged — awaiting auth docs)*; `httpx` `Authenticate`/`RequireWrite`/`RequireAdmin` + **dev bypass**. Tested: 401 no/inactive token; reader/editor/admin/`*` role matrix on read/write/logs; bypass works tokenless.
- [x] **F5 WebSocket hub** — `internal/ws` module-agnostic `Hub.Publish`, self-authenticating `/ws` handler (token via `access_token` query / `bearer` subprotocol / Authorization — *ASSUMED transport, flagged*), backpressure drop, ctx-based teardown. Tested: dial rejected without valid token; broadcast reaches two clients. Wired into router + `main.go`.
- [x] **F6 Frontend shell** — *complete; builds.* Tailwind v4 + full **design-token port**, self-hosted fonts (latin+latin-ext); Vite `@`-alias + dev proxy; theme provider (dark default); responsive nav + **Nástěnka landing** + route-level **`RequireAdmin`**; TanStack Query; centralized i18n; shared states + toasts. **Real Mode A auth layer:** fetch wrapper (JWT attach, single 401→refresh→retry→redirect), boot session-cookie refresh, **RedirectingShell**, websocket client (`?access_token=`) — all against the confirmed auth contract; **dev-admin stub** used only when REAL_AUTH is off (dev/e2e). Verified via Playwright/axe (both themes @375/1440).
- [x] **F7 Deploy scaffold — BUILT + FULLY VERIFIED IN DOCKER** (only the live Coolify deploy with real auth/R2 creds remains, a Karel config step). Written: `backend/Dockerfile` (4-stage — node SPA build → `CGO_ENABLED=0` Go build with embedded `time/tzdata` → `litestream:0.3.13` binary → `alpine`+`ca-certificates` runtime; **final image 72.7 MB**), `litestream.yml` (R2 replica, prefix `home`, env-expanded creds), `docker-entrypoint.sh` (`restore -if-db-not-exists -if-replica-exists` → `litestream replicate -exec home`; `LITESTREAM_ENABLED=false` bypass for the offline harness), `docker-compose.yml` (offline smoke test), `.dockerignore` + `.gitattributes`, and a root `README.md` (Coolify build config + full env matrix + fresh-restore procedure). **Go now serves the SPA** (`internal/httpx/spa.go` + new `HOME_STATIC_DIR`, wired via `httpx.Deps.StaticDir`): index.html fallback for client routes, `immutable` cache on `/assets/*`, and unmatched `/api/**` + `/ws` **excluded → JSON 404** (never the shell). Unit-tested (4 in `spa_test.go`) + `authTransport.ts` lets `VITE_REAL_AUTH=false` force a prod build off real auth; generated `frontend/package-lock.json` for reproducible `npm ci`.
  - **Verified in Docker (2026-07-22, Docker 29.6.2):** ① image builds clean; ② **offline harness** (`docker compose up`) boots (entrypoint → dev mode, DB seeded, listening) and serves correctly — `/healthz`+`/readyz` 200, SPA shell on `/`+`/ukoly` (`no-cache`), hashed asset `immutable`, `/api/x`→JSON 404, `/ws`→426, real `/api/dashboard`→200; ③ **fresh-volume restore actually tested** against a local **MinIO** R2 stand-in: first run on empty R2 starts+seeds+replicates (`home/` prefix objects: snapshot + WAL), graceful SIGTERM does a final sync, **wipe the volume**, second run **restores from R2** (`restoring snapshot`→`applied wal`→rename) and comes back with the pre-wipe marker board **AND no double-seed** (exactly one `Domácnost`). ⇒ **RESULT: PASS.**
  - **Bug the restore test caught + fixed:** the entrypoint originally ran `litestream restore -if-db-not-exists` only; on a *first-ever* deploy (empty R2) that exits non-zero (`no matching backups found`) and, under `set -e`, **crash-loops the container**. Added **`-if-replica-exists`** so the no-backup case is a clean no-op and the app starts fresh. (This is precisely the failure "fresh-build restore *actually* tested, not assumed" is meant to surface.)
  - *Remaining (Karel, config not code):* register `home` in auth + `HOME_AUTH_SERVICE_SECRET`; set `VITE_AUTH_BASE_URL` at build; supply real R2 endpoint/bucket/keys; deploy on Coolify with `HOME_ENV=production` and confirm `/healthz`+`/readyz` green there.

**Boot order (`main.go`):** config.Load → slog + timezone → db.Open → goose.Up → FTS5 probe → SeedIfEmpty → build introspector+cache+hub+router → start hub → serve.

## Phase 2 — `logging` (audit browser) — **backend ✅, frontend pending**

- [x] **`internal/audit`** (spine) — `Sink.Record(ctx, *sql.Tx, Event)`, ctx-sourced actor, one `audit_events` + one `audit_changes` per field, append-only, `TSLayout` fixed-width ts for correct keyset order. (Built in foundation; both atomicity tests + FTS + diffs pass.)
- [x] **Read side** — `internal/audit/query.go` + `http.go`: `GET /api/logs`, `/logs/{id}`, `/logs/entity/{type}/{entityId}`, `/logs/stats` — **admin-only** (mounted behind `RequireAdmin`); composed AND filters, FTS5 `q` (quoted-token), composite `(ts,id)` keyset cursor, timeline oldest-first, stats (day/week buckets + top-N). Tested: filters, FTS diacritics, pagination-each-once, timeline cross-module, stats, list/stats HTTP + role gating (reader/editor→403).
- [x] **Retention (FR-L7)** — `audit.Prune` deletes beyond `HOME_LOG_RETENTION_DAYS` (default 0 = no-op), self-logs `logging.prune`; runs once on boot when configured (no scheduler). Tested.
- [x] **Log screen (FE)** — *built & building.* Admin-only route; tabs (Záznamy / Analytika); filter bar (module, level, action, actor, entity type/id, from/to, free-text `q`) with Filtrovat/Vymazat; **keyset stream via `useInfiniteQuery`** + "Načíst další"; rows expand → `old→new` **DiffView** with `+`/`−` non-hue cue + truncate-with-expand; first-class **entity timeline** modal (from any row); analytics = top-N bars per dimension (lightweight CSS bars on the chart tokens — Recharts avoided to dodge the React-19 risk). *Pending polish:* mobile filter drawer.
- [ ] **Dev seed** — realistic Czech audit history across all four modules incl. one `todo.card.move` `meta.via="dashboard"` + one `events.reminder.complete`. *(nice-to-have for demo; the log already fills from real usage.)*

**Tests (atomicity pair first):** rollback ⇒ no event; forced event-insert failure ⇒ mutation rolls back; actor/request-id from ctx not args; diffs only-changed-fields + full values survive a paragraph; FTS5 finds `kotlík` + triggers stay synced; keyset returns each event once; timeline oldest-first incl. cross-module; reader/editor→403 on all four log endpoints; prune deletes only beyond threshold + self-logs (no-op at 0).

## Phase 3 — `todo` (Úkoly)

- [x] **`internal/lexorank`** — base-62 fractional indexing `Between/First/Head/Tail/Rebalance` + `NKeys`; tested incl. 200-insert degenerate. (built in foundation)
- [x] **`internal/todo`** — boards/columns/cards/links/checklist/labels per FR-T1–T7 + `openapi.yaml`, via `Store` (SQL, batched Tree, no N+1) + `Service` (WithTx + audit-in-tx + hub notify) + `Handler` (reads open, writes `RequireWrite`). `kind` **non-unique**; **`POST /api/cards/{id}/move`** single core action w/ `done_at` stamp/clear + optional `?via=` (dashboard reuse). Column delete 409+count unless `?cascade=true` (each cascaded card logged); card soft-delete default; `tree` = ordered columns+cards w/ `label_ids`+progress, filters. **Fixed a `MaxOpenConns=1` deadlock** (reads inside a tx must use the tx — see memory). Tested: one-row move, `done_at`, multiple now/done, cascade 409+log, soft/hard, tree shape+filters, card-update diff, role gating. Mounted in `main.go`.
- [x] **Frontend (Úkoly)** — *built & wired; builds.* Board switcher, columns (desktop kanban / mobile stack, `now` emphasised), quick-add, primary **"Přesunout do…"** control (keyboard-operable), card tiles, reusable **`CardDetail`**, shared `LinksEditor`/`MarkdownView`, optimistic move + rollback, **search + label chips + archived toggle**, **column collapse** (localStorage, desktop spine), **dnd-kit cross-column drag** (grip handle + DragOverlay, client lexorank position), websocket live-sync. *Open:* within-column drag-reorder (cross-column done; move-to covers precise ordering).

**Tests (backend ✅):** move rewrites exactly one row; lexorank degenerate 200-insert; `done_at` stamp/clear; **multiple `now`/`done` columns**; column delete 409/cascade; soft vs `?hard=true`; tree payload + filters; every mutation audited + card edit diffs; reader→403 on mutations / 200 reads.

## Phase 4 — `events` (Okno do budoucnosti)

- [x] **`internal/dates` + `internal/recur`** — civil `Date` type; `Parse`/`String`/`Expand`/`IsOccurrence` for the RRULE subset. **Short-month clamping (D19)** as an explicit commented step on integer `(y,m,anchorDay)`. Window computed near `from` (not scanned from anchor). Tested: **full clamping matrix** (31-Jan→28/29 Feb, 29-Feb yearly, 30th), weekly/UNTIL/one-off/cap/window-start, parse-rejects-unsupported, round-trip, IsOccurrence.
- [x] **`internal/events`** — series CRUD (whole-series edits, soft default, hard cascades links+completions), `GET /api/events/occurrences` (month-grouped, **static route before `/{id}`**, window-span capped → 422), links, `POST/DELETE /api/events/{id}/complete` — **idempotent** (only first insert self-logs), bogus occurrence → 422, undo, `?via=` for dashboard reuse. **No occurrences table** (asserted). Reminder CHECK validated → 422. Audit diffs on create/update; `reminder.complete` no-diff. Hub notify. Tested + role-gated. Mounted in `main.go`.
- [x] **Frontend (Okno)** — *built & wired; builds.* Month-grouped list with a ±6-month pager (current month forward, pageable to past), recurrence/reminder row indicators, empty-period message. **`EventForm`** (create+edit): all-day native date (no time), recurrence chips (*nikdy·týdně·měsíčně·ročně*) + optional end date with **reserved height**, reminder checkbox → conditional lead chips with **reserved space (no jump)**, links via shared `LinksEditor` (reconciled on save). **Series-edit warning** shown inline AND as a **pre-save confirm** for recurring events (approved copy). Standalone **`EventDetail`** viewer (reused by Nástěnka). Editors edit/delete; readers view.

**Tests (clamping matrix priority):** 31-Jan monthly → 28/29 Feb / 31 Mar / 30 Apr; 29-Feb yearly → 28/29 Feb; 30th monthly → 28/29 Feb / 30 Apr; expansion persists nothing; open-ended weekly stops at cap; over-wide window 422; UNTIL terminates; one-off → one occurrence; boundaries correct in Europe/Prague vs UTC clock; complete idempotent + undo + bogus 422; series edit changes all + cascade; static route before `{id}`; CHECK rejects reminder-without-lead; event diffs + `reminder.complete` no-diff; reader→403.

## Phase 5 — `dashboard` (Nástěnka) — **backend ✅**, frontend pending

- [x] **`internal/dashboard`** — no tables; `GET /api/dashboard` (read, any authed) returns two computed lists. **Události:** earliest uncompleted occurrence within `HOME_DASHBOARD_LOOKBACK_DAYS`, active when `today >= occurrence − lead`, **at most one per event**, `overdue` + `days_until`, sorted overdue-first then ascending; window bounded `[today−lookback, today+40]`, "today" injectable for tests. **Úkoly:** all non-archived `kind='now'` cards across non-archived boards via one join (+ batched label/progress, no N+1), each with board/column and an additive **`done_column_id`** (board's first done column, or null → archive path). Tested: cross-board + multiple-now-columns + done-column resolution, one-reminder-per-event + advance-on-completion, activation boundary (−7 vs −8), lookback bound. **Verified e2e over HTTP.**
- [x] **Mark done reuses owning endpoints** — no duplicate logic; the frontend calls `POST /api/cards/{id}/move?via=dashboard` (to `done_column_id`, else `PATCH archived=true`) and `POST /api/events/{id}/complete?via=dashboard`, both already built + `via`-tagged for cross-module attribution.
- [x] **Frontend (Nástěnka)** — *built & wired.* Landing route; two labelled lists with Czech plural counts; **tasks grouped by board only when >1 contributes**; overdue = restrained danger tint (backend sorts overdue-first); rows tap → reuse `CardDetail` / `EventDetail`; **empty = success** state; optimistic complete + rollback toast; invalidates `['dashboard']`; reader hides done controls.
- [x] **`HoldToComplete` gesture (D22)** — real `<button aria-label>` + fill span (`scaleX 0→1` ~1.9 s); pointerdown `stopPropagation` + 2000 ms timer; up/leave/cancel cancels; contextmenu prevented; `touch-none`. **Anti-tap-fallthrough** (stopPropagation on pointerdown *and* click). **Immediate keyboard/AT path** (Enter/Space commits at once; plain action-button label). `prefers-reduced-motion` skips the fill. Detail dialog "✓ Hotovo" = single activation. **Vitest-verified** (6 tests: 500 ms no-op, exact 2000 ms commit, early-release cancel, keyboard-immediate, tap-no-fallthrough, AT label).

**Tests:** one reminder per event (3 missed → 1 earliest); activation boundary −7 vs −8 days in Europe/Prague; advance-on-completion (no new row); lookback drops stale; overdue flag + order; **cross-board aggregation** (two boards; grouping only >1); multiple `now` columns; mark-done → first `done` else archive; cross-module attribution (`meta.via="dashboard"` in entity timeline); idempotent double-complete; **gesture** (500 ms no-op, 2000 ms completes, early release no change, short tap neither completes nor opens); **keyboard commits immediately**; dialog single activation; **no N+1** (assert query count with 50 events); reader→403 both paths + no done control.

---

## Verification (how each phase is proven)

- **Backend:** `go test ./...` with temp-file SQLite; table-driven role-gating via a fake `Introspector`; the atomicity pair and clamping matrix are non-negotiable.
- **Local end-to-end** with `HOME_DEV_AUTH_BYPASS=true` (`docker-compose up` or `go run ./cmd/home` + `vite dev` proxying `/api`+`/ws`): create board/column/card, move a card into Právě dělám (appears on Nástěnka), complete via 2 s hold, create a recurring reminder (appears in lead window), browse the log + open an entity timeline.
- **Frontend a11y/gesture (Playwright + axe):** 2000 ms hold, early-release cancel, short-tap-doesn't-open, Enter-commits-immediately, keyboard card drag, AA in **both themes @375 & 1440 px**, czPlural (1/2/5).
- **Deploy:** Coolify green; healthz/readyz live; Litestream→R2 (`home/`) confirmed; **fresh-build restore actually tested**; seed doesn't double-run.
- **Service-wide DoD (HANDOFF §7):** all PRD §11 pass; every endpoint conforms to `openapi.yaml` v0.2.0; no mutation without a same-tx audit event; role gating holds across modules; `/ws` keeps two devices in sync on board **and** dashboard.

## Open items needing Karel (block real deploy, not the build)

1. ✅ **Auth service contract** — received (ws-tilcer-auth repo) & **wired**: introspect via `X-Service-Secret`; `/token/refresh` bearer in-body via session cookie; login `?site=home`; WS via `?access_token=`. (See the auth-service-contract memory.)
2. **Auth prerequisite (still needed for deploy)** — register `home` site (roles admin/editor/reader) + provision a `home` service client → `HOME_AUTH_SERVICE_SECRET` in Coolify; set `VITE_AUTH_BASE_URL` for the frontend prod build.
3. ✅ **"Production" signal** — resolved in code: the dev-bypass hard-refusal keys on `HOME_ENV=production` (config.go). **Karel action:** set `HOME_ENV=production` in Coolify (documented in `README.md`); never set `HOME_DEV_AUTH_BYPASS` there.
4. ✅ **Litestream execution model** — scaffolded as `-exec` (entrypoint supervises the app; app exit stops litestream). Env names defined: `LITESTREAM_ENABLED`, `LITESTREAM_R2_ENDPOINT`, `LITESTREAM_R2_BUCKET`, `LITESTREAM_ACCESS_KEY_ID`, `LITESTREAM_SECRET_ACCESS_KEY`. **Karel action:** supply the R2 endpoint/bucket/keys + confirm/pin the `litestream/litestream` image tag (currently `0.3.13`) when Docker is available.
5. **`REGISTRY.md`** lives in Nextcloud (`../../../Nextcloud/Claude/Web Server/REGISTRY.md`) — register `home` (repo URL, status → live) when deployed.
6. Confirm low-risk defaults: modernc FTS5 pin, single-writer SQLite, composite keyset cursor, rrule hybrid, Tailwind v4, Recharts.

## Critical files to create (representative)

- `backend/internal/db/tx.go` — `WithTx(ctx, db, fn)` atomicity backbone.
- `backend/internal/audit/sqlite.go` — `Sink` reading actor/request from ctx.
- `backend/migrations/0001_init.sql` — all tables **logging-first**, FTS5 + triggers, indexes/CHECKs, seed.
- `backend/internal/httpx/middleware.go` — RequestID, Auth, role gates (security perimeter).
- `backend/internal/recur/expand.go` — window-bounded expansion + explicit **D19 clamp**.
- `backend/internal/lexorank/lexorank.go` — fractional-index ordering.
- `frontend/src/theme/globals.css` — token port + shadcn aliasing (`:root` dark / `.light`).
- `frontend/src/api/client.ts` + `api/ws.ts` — fetch wrapper (single 401→refresh→retry) + live-sync.
- `frontend/src/components/common/HoldToComplete.tsx` — press-and-hold + mandatory keyboard path.
- `frontend/src/components/common/{CardDetail,EventDetail,LinksEditor}.tsx` — shared across board/okno/dashboard.
- `frontend/src/i18n/{plural,cs,format}.ts` — centralized Czech + `czPlural` (ported from `Home.dc.html`).
- Root `./CLAUDE.md` (from `handoff/CLAUDE.md`); `design/` keep-list.
