# Home — Implementation Handoff (Claude Code)

> **Read first:** root `CLAUDE.md` (project conventions), then `PRD.md` (behaviour — the source of truth) and `openapi.yaml` (v0.2.0) in this folder. `notes.md` holds decisions D1–D22 in short form; `HANDOFF-design.md` is the design brief the prototype was built from.
>
> Status: issued 2026-07-21 · Owner: Karel · The design pass is complete and approved (with three known gaps, §6).

## 0. This is an index — the build is split per module

Four modules is far more than the auth v1 build, so the work is split into **self-contained per-module handoffs**, each intended for its own session:

| Doc | Module | Depends on |
|---|---|---|
| **this file** | Foundation — repo, config, DB, auth, shell, deploy | — |
| `HANDOFF-1-logging.md` | `logging` — the audit spine + log browser | foundation |
| `HANDOFF-2-todo.md` | `todo` — Úkoly board | foundation, spine |
| `HANDOFF-3-events.md` | `events` — Okno do budoucnosti | foundation, spine |
| `HANDOFF-4-dashboard.md` | `dashboard` — Nástěnka | foundation, spine, todo, events |

**Build the foundation in this file first, then 1 → 2 → 3 → 4.** The spine comes before any feature module because every mutation writes through it in the same transaction — retrofitting that is painful. Nástěnka is last because it aggregates the other two.

Each module doc is self-contained enough to work from alone, but assumes the foundation exists and does **not** repeat the conventions below.

## 1. Repo

**Monorepo `ws-tilcer-home`.** Carry a copy of the root `CLAUDE.md` at the repo root (or symlink it) so Claude Code inherits the project conventions in every session.

```
ws-tilcer-home/
  CLAUDE.md                 # copy of root conventions
  backend/                  # Go + embedded SQLite
    cmd/home/main.go        # entrypoint: config, logger, router, server, static SPA
    internal/
      config/               # env loading (PRD §9)
      http/                 # router, middleware (request log, auth, role gate), handlers
      audit/                # the logging spine (HANDOFF-1) — AuditSink + writer
      todo/                 # boards, columns, cards, links, checklist, labels
      events/               # events, recurrence expansion, reminder completions
      dashboard/            # the aggregation read model
      ws/                   # websocket hub
      db/                   # sqlite open, migrate runner, litestream hooks
    migrations/             # Goose (0001_init.sql, ...)
    openapi.yaml            # copied/symlinked from services/home/openapi.yaml (single source)
    Dockerfile
  frontend/                 # React + TS + Vite + TanStack Query + Tailwind + shadcn/ui
    src/
      api/                  # fetch wrapper (JWT attach, 401 -> auth refresh, retry)
      routes/               # nastenka/ ukoly/ okno/ log/
      components/           # shadcn primitives + app components
      theme/                # tokens from the design bundle
  design/                   # the approved Claude Design bundle (see §5)
  docker-compose.yml        # local dev
  README.md
```

**Deployment: a single Coolify app, same origin.** One container serves the Go API and the built SPA on `home.tilcer.cz`:

```
home.tilcer.cz
  /api/**  -> Go handlers
  /ws      -> websocket upgrade
  /healthz /readyz
  /*       -> SPA (index.html fallback for client-side routes)
```

Because the SPA is same-origin with the API, **there is no CORS to configure for home's own calls**. `HOME_ALLOWED_ORIGINS` stays in config only for the cross-subdomain auth refresh flow; don't invent CORS you don't need.

## 2. Conventions that apply to every module

These are stated once here and assumed by all four module docs.

- **Go + embedded SQLite**, Goose migrations, one DB file at `HOME_DB_PATH`.
- **IDs are UUIDv7** (sortable — keyset pagination depends on it).
- **Ordering is lexorank-style strings** (D4), never floats or integer positions. A move computes a key between its two neighbours and rewrites **one** row. If you find yourself renumbering siblings, the implementation is wrong.
- **The audit rule (D-arch, the most important rule in this codebase):** every mutating handler opens a transaction, performs the change, records the audit event *inside that same transaction*, and commits. There is no code path that mutates without logging. See `HANDOFF-1-logging.md` — build it first and make every later module use it.
- **Soft delete by default** (D8): `archived = true`, with a real delete only behind `?hard=true`.
- **Errors** use the shared `Error { error, detail }` schema from `openapi.yaml`. Every endpoint must match the spec.
- **Identifiers are English, the UI is Czech** (D17/D20). Module names in code and in the audit `module` column: `logging`, `todo`, `events`, `dashboard`. No Czech in code identifiers, no English in user-facing strings.
- **Czech has three plural forms** (1 / 2–4 / 5+). Build one helper (`czPlural(n, ['úkol','úkoly','úkolů'])`) and use it for *every* count label. The design prototype has a working implementation to copy.
- **Dates** render `d. M. yyyy`; all date arithmetic runs in `HOME_TIMEZONE` (`Europe/Prague`), never UTC.
- **Dark theme is the default** (D21): dark values in `:root`, light under `.light` — the inverse of shadcn/ui's usual convention. Don't let a scaffold flip it.
- **Tests alongside each step**, not at the end. Every module doc ends with the tests that must exist before that module is considered done.

## 3. Foundation build order

### F1. Scaffold + config
Go module, `cmd/home/main.go`, `internal/config` loading PRD §9 env vars (`HOME_DB_PATH`, `HOME_SITE_KEY`, `AUTH_BASE_URL`, `HOME_AUTH_SERVICE_SECRET`, `HOME_ALLOWED_ORIGINS`, `HOME_TIMEZONE`, `HOME_DASHBOARD_LOOKBACK_DAYS`, `HOME_RRULE_MAX_OCCURRENCES`, `HOME_LOG_RETENTION_DAYS`, `LITESTREAM_*`). Fail fast and loudly on missing required vars — a silently-defaulted secret is worse than a crash.

### F2. DB, migrations, backup
SQLite open with WAL; Goose runner on boot. **`0001_init` is written once and creates every table for all four modules** (logging first, then to-do, then events) — see the data model in PRD §5 and the per-module docs for the exact DDL. Litestream restore-if-empty hook *before* serving; seed the default board only when `boards` is empty (so a restored build doesn't double-seed). Litestream → R2 prefix `home/`.

> One migration for all four modules is deliberate: this is a greenfield service that hasn't shipped, so there's no reason to carry four migrations for tables that all arrive before first deploy.

### F3. Observability baseline
`GET /healthz` (liveness) and `GET /readyz` (SQLite ping). Structured JSON logs to stdout. Per-request log: method, path, status, latency, **request id**. Generate a request id per request, put it in the context, and make sure the audit spine stamps it onto every event — that's what ties the operational and domain log planes together.

### F4. Auth integration (Mode A)
- Verify the site-scoped JWT via auth `POST /introspect`, authenticating as the `home` **service client** with `HOME_AUTH_SERVICE_SECRET`. **Cache introspection results for the token's remaining TTL** (D2) — tokens live 15 minutes and the dashboard is the landing route, so uncached introspection would put an auth round-trip on every page load. Never distribute or accept the JWT signing secret.
- **Role gating middleware** from the `roles` claim (D5): reads = any authenticated user; writes = `editor` or `admin`; `/api/logs/**` = `admin`. Treat `roles:["*"]` (superuser) as full access. Enforce server-side from claims only — never trust a client-supplied role.
- Frontend: unauthenticated load redirects to `auth.tilcer.cz` for site `home`. Shared fetch wrapper attaches the JWT and, on `401`, calls auth `POST /token/refresh` **once** (same-site cookie on `tilcer.cz`, `credentials:'include'`), retries, and on failure redirects to login. Do not build a login form — login is auth-hosted (Mode A).

**Prerequisite (Karel, before this step):** register the `home` site in auth with roles `admin`/`editor`/`reader`, and provision a `home` service client bound to site `home`; put its secret in Coolify as `HOME_AUTH_SERVICE_SECRET`.

### F5. Websocket infrastructure
`GET /ws`, JWT-authenticated on connect and role-authorised, with a small hub that broadcasts change events to connected clients. Modules publish to it; the hub itself is module-agnostic. Frontend applies pushed changes via `queryClient.setQueryData` / targeted invalidation, with refetch-on-focus as the reconnect fallback. Drop the connection when the token can't be refreshed.

Build the hub here; each module doc says what it publishes.

### F6. Frontend shell
- **Four-destination nav** — **Nástěnka · Úkoly · Okno · Log** (Log only for `admin`): bottom tab bar on mobile, side nav on desktop.
- **Nástěnka is the landing route** (D16).
- **Theme tokens** ported from the design bundle (§5) into Tailwind config + shadcn CSS variables, dark in `:root` and light under `.light`.
- TanStack Query client, the fetch wrapper from F4, toast infrastructure (needed for optimistic rollback in later modules), and the `czPlural` helper.
- **Signed-out / redirecting state** — a minimal "Přesměrování na auth.tilcer.cz…" shell. *This is one of the three gaps with no prototype reference (§6) — build it from the design tokens.*
- Global loading / empty / error / `reader` view-only patterns, so module screens inherit them rather than each inventing their own.

### F7. Deploy
Single Coolify app, env per PRD §9, `/healthz` + `/readyz` green, Litestream → R2 (`home/`) confirmed, and a **fresh-build restore actually tested** (not assumed). Update `REGISTRY.md` status to live when deployed.

## 4. What the frontend should look like

The approved design is the Claude Design bundle in `../../design`. **`Home.dc.html` is the visual source of truth** — read it directly for colours, spacing, type, and component anatomy; it also contains a full design-system screen (tokens, type scale, spacing, radii, motion, component→shadcn mapping, Czech typography rules).

Reproduce the visual output, not the prototype's internal structure — it's a single-file HTML mock with in-memory state, and the real app is React + TanStack Query + shadcn/ui. The prototype's component table names the shadcn primitive each element maps to; follow it. Nothing outside shadcn/ui + Radix + Tailwind except **dnd-kit** (drag) and a light chart lib for the log analytics.

## 5. The design bundle

Commit the approved bundle into `../../design` so the visual reference travels with the code.

**Do not copy `IMPLEMENTATION-PLAN.md` or the `screenshots/` folder from the bundle.** Both are stale: the plan describes an earlier two-module prototype and would send you building work that already exists, and the screenshots disagree with each other and with the current HTML (some still show an old nav label). Keep `Home.dc.html`, `CardTile.dc.html`, `support.js`, and `DESIGN-DEVIATIONS.md` — and read that last one knowing its "Úkolníček" finding is already fixed in the HTML.

## 6. Three things the design doesn't cover

Build these from the PRD plus the established design language; there is no prototype reference:

1. **Cross-board dashboard aggregation** — the prototype pulls `kind=now` cards from the active board only and hardcodes the board name. The real behaviour is *all* non-archived boards (PRD FR-N1). See `HANDOFF-4-dashboard.md`.
2. **Board-group heading on Nástěnka** — group the task list by board when more than one board contributes; don't show the heading when only one does.
3. **Signed-out / redirecting shell state** — F6 above.

## 7. Definition of done (whole service)

The per-module docs each carry their own DoD. Service-wide:

- [ ] All of PRD §11 acceptance criteria pass; every endpoint conforms to `openapi.yaml` v0.2.0.
- [ ] `0001_init` applies cleanly on an empty DB **and** after a Litestream restore; the seed runs only when empty.
- [ ] **No mutation path exists that doesn't write an audit event in the same transaction** (the rule from §2, verified per module).
- [ ] Role gating holds across all four modules: `reader` gets `403` on every mutation; only `admin` reaches `/api/logs/**`.
- [ ] Czech UI throughout with correct plural forms; dark default; verified at 375 px and 1440 px in both themes.
- [ ] `/ws` pushes to-do **and** event/completion changes; two devices stay in sync on both board and dashboard.
- [ ] Coolify deploy green; `/healthz` + `/readyz` live; R2 backup and fresh-build restore verified.
- [ ] `REGISTRY.md` updated (repo URL, status → live).
