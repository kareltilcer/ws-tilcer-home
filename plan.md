# Implementation Plan — `home` (household management service)

> ## v2 status (2026-07-26) — modular monolith + Mode B + widget host
>
> **v2 is implemented on the backend (fully green) and the frontend (builds +
> unit tests green).** v2 reshaped the built v1 into: (1) a **compile-time modular
> monolith** — `backend/internal/platform/*` (core) + `backend/internal/modules/*`
> (logging, todo, events, dashboard), each self-contained with its own routes,
> **per-module Goose migrations** assembled in one sequence by a central
> **registry**, audit actions, and widget providers; a CI **import-lint**
> (`internal/arch`) fails on any cross-module import. (2) **Mode B auth** —
> `platform/auth` hosts `/api/auth/login|logout|session`, owns a hashed **session
> store**, session middleware with **role re-mint + fail-closed revocation**,
> **CSRF** double-submit + Origin allowlist, and login rate-limiting; no JWT in the
> browser; `/ws` is session-authenticated. Mode A introspection is removed. (3)
> **Nástěnka is a widget host** — `user_dashboard_layout`, `GET /api/dashboard`
> fan-out `{layout, widgets[]}`, `/catalog`, `PUT /layout` (reader-allowed),
> `/widgets/{key}`; the three widgets (`todo.pravedelam`, `events.pripominky`,
> `events.tento-mesic`) are **module-owned providers**; the host imports no feature
> module. **`go build/vet/test ./...` all green** (incl. auth, dashboard host,
> providers, arch-boundary tests); a real boot serves the new endpoints.
> **Frontend**: Mode B (login screen, session bootstrap, CSRF fetch wrapper,
> cookie-auth WS, logout) + widget-host Nástěnka (registry + responsive grid +
> keyboard-operable arrange mode + catalog picker + empty/first-run) — `tsc` +
> `vite build` + Vitest (6) green. **Remaining (polish/config):** live Coolify
> deploy (register `home` in auth incl. the Mode B `/internal/login` +
> `/internal/token/mint` endpoints, R2 creds); optional dnd-kit pointer-drag for
> widget reorder (keyboard ↑/↓ + size toggle already ship); optional full
> relocation of the *legacy* v1 screens into `src/modules/*` (their v2 widgets
> already live under `src/platform/widgets` + `src/modules/*/widgets`); refresh the
> Playwright/e2e specs for the Mode B login flow.
>
> _The v1 detail below is retained for history; where it conflicts with the v2
> status above, v2 wins._
>
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
- [x] **Phase 6** *(v3)* — `notes` (Poznámky) — backend ✅ + Poznámky frontend ✅ (tree, slug paths, Markdown editor, two-scope pins, `notes.pripnute` widget)
- [x] **Phase 7** *(v4)* — `documents` (Dokumenty) — backend ✅ + Dokumenty frontend ✅ (see the phase section below)
- [~] **Phase 8** *(v5)* — `admin` (Administrace) + `platform/push` + `platform/scheduler` + `platform/pwa` — **backend ✅ + frontend ✅**; remaining: Playwright/axe pass and the live deploy (VAPID secrets, real icons)

---

## Phase 8 — v5: Administrace (`admin`) + Web Push + PWA — **backend ✅ + frontend ✅**

> **Build status (2026-08-16).** All eleven steps below are implemented. `go
> build/vet/test ./...` is green (incl. `internal/arch` — `admin` imports only
> `platform/*`), `tsc -b` + `vite build` + Vitest (24) are green, and a real boot
> proved the whole trigger chain end to end: a card created over HTTP → audit
> event committed → outbox tailer → rule matched → audience resolved → send
> attempted → delivery row recorded.
>
> **Implementation notes worth carrying forward (deviations, all small):**
> 1. **`push.Recorder` instead of platform writing an admin table.** HANDOFF-7 §2
>    has `Send` insert `notification_deliveries` directly, but that table is the
>    admin module's (§1) — platform writing it would invert the ownership rule the
>    whole architecture rests on. `platform/push` takes a narrow `Recorder`
>    interface which the admin store implements; the row-per-attempt behaviour is
>    identical, and a household running without the module just keeps no log.
> 2. **`metrics.Collect` over an optional interface, not package-level `Register`.**
>    §7 sketches globals; a `*Registry` built at composition from modules
>    implementing `metrics.Source` keeps the same "modules declare what they
>    provide" shape without global mutable state that leaks between tests.
> 3. **`audit.NewSink()` now returns `*audit.Writer`** (was the `Sink` interface)
>    so the composition root can attach the tailer's nudge. Every existing call
>    site still compiles — the concrete type satisfies `audit.Sink`.
> 4. **`push` tests are an external `push_test` package.** `testsupport` builds the
>    full migration set through `bootstrap`, which now imports `admin`, which
>    imports `push` — an in-package test would be an import cycle. `export_test.go`
>    carries the two seams the external tests need.
> 5. **Catalog carries `members`.** The audience picker needs display names and
>    openapi 0.6.0's `NotificationCatalog` has no field for them; serving them on
>    the catalog keeps the composer to one round trip. Additive to the spec.
> 6. **Icons are generated placeholders** (`frontend/scripts/gen-icons.mjs`) until
>    design delivers the real set (§14) — an installed PWA with a missing icon
>    looks broken in a way a plain mark does not.
>
> **Remaining:** the Playwright/axe pass at 375/1440 in both themes, and the live
> deploy (generate + set the VAPID secrets, swap in the real icons).

**Sources.** `handoff/v5/HANDOFF-7-admin.md` — **the build brief; it wins on every "how"** —
over `handoff/v5/PRD.md` §V5-1…V5-11 (D51–D74; FR-P1–P5, FR-S1, FR-ADM1–6), `handoff/v5/openapi.yaml`
(**already merged to 0.6.0**), `handoff/v5/HANDOFF-design.md` §v5, and `design/v5/Home.dc.html` (the
delivered visual source of truth — Administrace, Nastavení → Oznámení, offline, install). The three
`*-v5-admin.*` addenda were folded into those files on 2026-08-16 and deleted; their content merged
verbatim. *(Doc gaps, harmless: `HANDOFF.md`'s build-order index and `CHANGELOG.md` have no v5 row yet.)*

**Shape.** v5 is additive, exactly like v3/v4: one new **admin-only feature module** (`admin`) plus
three **platform** strands (`push`, `scheduler`, `pwa`) and small **metric providers** inside the four
existing feature modules. No change to auth, the dashboard-host contract, `events` (D68), or any
existing table.

```
backend/internal/
  platform/push/        envelope + webpush-go send, subscriptions, prefs, Send()  ← new infra (like ws/blobstore)
  platform/scheduler/   minute ticker, Prague/DST slot evaluation, catch-up      ← new infra
  platform/metrics/     Register/Catalog/Resolve — 3rd registered catalog (D59)  ← new infra
  platform/audit/       + notifier.go: audit_events keyset tailer → Listeners (the outbox, D56)
  modules/admin/        rules, schedules, deliveries, templates, audience, catalog  ← the 7th module
  modules/{todo,events,notes,documents}/  + metrics.go (9 descriptors, D69)
frontend/src/
  platform/push/        subscribe/permission hooks, prefs
  platform/pwa/         SW registration, offline context, install prompt, persisted Query cache
  sw.ts                 ONE service worker: push + notificationclick + precache (D63)
  modules/admin/        4 tabs + composer + audience picker + schedule builder + deliveries (§13)
  platform/settings/    Nastavení → Oznámení panel (every role), theme, install (§13)
```

### Open questions the build brief closed

`HANDOFF-7-admin.md` answers everything the earlier draft of this plan had to decide for itself, and
overrules one call: **use `github.com/SherClockHolmes/webpush-go`** (§2) rather than hand-rolling RFC
8291 on the standard library. Also settled by the brief, no longer this plan's judgement: the SW must
**not** runtime-cache `/api` and offline reads come from a **user-id-namespaced persisted TanStack
Query cache** (§12); `vite-plugin-pwa` in **`injectManifest` + `registerType:'autoUpdate'`** (§12);
coalescing is an **in-memory per-rule debounce** with a Czech "(a N dalších)" count suffix (§5);
day-of-month is **1–31 with the last-day clamp**, not the design file's 28 cap (§6/§13, D74).

**Audience resolution needs no new table** (§10): `all` = every user with ≥1 row in
`push_subscriptions`; `roles` = members holding a listed role **from home's session role cache**, never
client input; `users` = the given ids. The one wrinkle the brief leaves open is *labels* — the
"Vybraným lidem" picker and Doručení's recipient column need display names, so both read
`sessions.display_name` (latest row per user) through one small helper in `platform/push`. **No
`known_users` table** — the earlier draft of this plan proposed one; the brief makes it unnecessary.

### Decisions still left to this plan

- **P-1 — migration numbering.** Existing blocks: `01` logging · `02` platform · `03` todo · `04`
  events · `05` dashboard · `06` notes · `07` documents. v5 adds `02002_push.sql` +
  `02003_audit_cursor.sql` (platform block, per §1) and a new `08001_admin_notifications.sql` block
  for `admin`. Goose applies by numeric prefix, so `admin` is last (as §1/§18 require) while the
  platform tables land at 02 — harmless (no cross-table deps) and it keeps the per-module block
  convention the repo relies on.
- **P-2 — `todo.done_today` needs no schema change.** §7 leaves the source to the module ("add a
  `done_at` timestamp… or derive from the audit rows"). The `todo` module has **already stamped
  `done_at` on the move-to-done transition since Phase 3** — so the metric is a single indexed count
  over `done_at` within `asOf`'s Prague day. No migration, no audit-table scan.
- **P-3 — the tailer's `Sink.Record` nudge must be un-droppable-safe.** §4 wants `Record` to nudge a
  signal channel for low latency. `Record` runs **inside every module's write transaction**, so the
  nudge is a **non-blocking send on a buffered channel with a `default:` drop** — a full channel or an
  absent notifier can never slow, fail, or block a caller's tx. The ~1 s poll is the correctness path;
  the nudge is only latency.
- **P-4 — frontend folder convention.** §13 puts the page in `src/modules/admin/` and the settings
  panel in `platform/settings`, but the built app keeps pages in `src/routes/*` and uses
  `src/modules/*` for **widgets only** (relocating the legacy screens is still an open Phase 1 item).
  Follow the brief for new code — `src/modules/admin/` + `src/platform/settings/` — and leave the
  existing five pages where they are rather than mixing a half-migration into this phase.

### 8.1 Spec merge + config + scaffolding
- [ ] `handoff/v5/openapi.yaml` is **already merged to 0.6.0** — validate it and copy to
      `backend/openapi.yaml` (the repo's single spec source). No merge work left.
- [ ] `platform/config`: `HOME_VAPID_PUBLIC_KEY` / `_PRIVATE_KEY` / `_SUBJECT` (secrets, redacted in
      `Redacted()`), `HOME_NOTIF_COALESCE_DEFAULT` (60 s), `HOME_NOTIF_DELIVERY_RETENTION_DAYS` (30,
      0 = forever), `HOME_NOTIF_MAX_FAILDAYS` (14), `HOME_SCHED_TICK_SECONDS` (60),
      `HOME_SCHED_CATCHUP_GRACE` (120 min). **Fail-fast rule:** a malformed VAPID key aborts boot; a
      *missing* pair disables push with one loud `warn` (the rest of the app must still run in dev).
- [ ] `cmd/vapidgen` (tiny, wrapping `webpush.GenerateVAPIDKeys()`): emit the keypair for Coolify —
      **generated once, never per deploy** (rotating invalidates every existing subscription, §14).
      Documented in `README.md`. (`npx web-push generate-vapid-keys` is the equivalent one-off.)
- [ ] `audit.ModuleAdmin = "admin"`; `platform` audit actions extended (`platform.push.subscribe|unsubscribe|prefs`).

### 8.2 `platform/push` — the one shared channel (§2–§3; FR-P1–P5, D52/D53)

> **§11 build order: ship this end-to-end first.** A hand-fired `Send` must reach a real subscribed
> browser before any rule, schedule, or composer work starts — everything downstream is worthless
> until the channel provably delivers.

- [ ] `02002_push.sql`: `push_subscriptions` (UNIQUE endpoint, idx user_id, `failing_since`),
      `notification_preferences` (user PK, master + 3 categories default 1; **absent row ⇒ all-on**,
      lazy-created on first PATCH).
- [ ] Add `github.com/SherClockHolmes/webpush-go` (pure Go, CGO stays off — §2). `Envelope`
      {Module, Type, Title, Body, URL, Tag, Category, Data} + `Push` interface {`Send`, `VAPIDPublicKey`}.
- [ ] `Send` — resolve each recipient's subscriptions **filtered by `notification_preferences`**
      (master off ⇒ skip user; envelope `Category` off ⇒ skip), encrypt+POST with the VAPID header,
      `TTL` ~1 day, `Urgency: normal`, **bounded worker pool (~8)**, one `notification_deliveries` row
      per endpoint attempt. **Never called on a request thread** — broadcast, tailer and ticker all
      run it off the request path. Truncate bodies defensively (~4 KB encrypted cap).
- [ ] Dead-endpoint pruning: `404`/`410` ⇒ **delete** the subscription (`expired`); `429`/`5xx`/network
      ⇒ `failed` + set/keep `failing_since`, pruned on the next attempt past `HOME_NOTIF_MAX_FAILDAYS`.
- [ ] Audience helper (§10): `all` = users with ≥1 subscription; `roles` = from the session role
      cache; `users` = given ids; display names from the latest `sessions` row per user.
- [ ] Routes (any member incl. `reader`, CSRF on writes): `GET /api/push/vapid-key`, `POST|DELETE
      /api/push/subscriptions` (upsert on endpoint, clears `failing_since`; delete idempotent 204),
      `GET|PATCH /api/push/preferences`. Per-user isolation — filtered by **session** user id, never a
      client-supplied one. All three audited (`platform.push.subscribe|unsubscribe|prefs`, §11).
- [ ] Tests: upsert-not-duplicate; mute matrix (master off, each category off); 410 prunes; failing-since
      prune; cross-user access refused; `reader` may subscribe; envelope shape.

### 8.3 The audit outbox tailer (§4, D56) — `platform/audit`
- [ ] `02003_audit_cursor.sql`: `audit_notify_cursor(id CHECK(id=1), last_event_id, updated_at)`.
      **Seed `last_event_id` to the current `MAX(audit_events.id)`** on the first boot after this
      migration — otherwise v5 replays the entire pre-existing history as a notification storm on a
      DB that already holds months of events. *(The single highest-consequence detail in §1/§4.)*
- [ ] `audit.Listener` + `RegisterListener(l)` + `StartNotifier(ctx, db, cursorStore)`: keyset scan
      `WHERE id > cursor ORDER BY id LIMIT n` (UUIDv7 — the log browser already relies on this),
      oldest-first, **at-least-once**, cursor advanced after each batch. `audit_changes` loaded
      **only** when a listener's template references `{{change.*}}` (match on cheap fields first).
      Listeners must dedupe on event id. Started/stopped with the background context in `main.go`.
- [ ] Wake on a ~1 s ticker **and** on a best-effort signal channel nudged by `Sink.Record` — per
      **P-3** the nudge is a non-blocking buffered send with a `default:` drop, because `Record` runs
      inside every module's write transaction. The poll is correctness; the nudge is only latency.
- [ ] `admin` registers its listener from its `New()` via the platform hook — so it **never imports
      `logging`** and `internal/arch` stays green (an explicit test, §16).
- [ ] Tests: an event committed by any module reaches the listener **after commit**; cursor survives a
      restart (no re-delivery, no gap); a simulated crash mid-batch re-processes and the listener's
      dedupe yields at most one push; **cursor seeded to max-id ⇒ no history replay**; a listener
      panic doesn't kill the tailer.

### 8.4 `platform/metrics` — provider registry + the 9 launch metrics (§7; D59/D60/D69)
- [ ] New `platform/metrics` package shaped like the widget catalog: `Descriptor{Key,Label,Unit,Scope}`,
      `Provider{Descriptors(), Value(ctx, userID, key, asOf)}`, `Register` / `Catalog` / `Resolve`.
      Each module `Register`s from its own `New()` — **no cross-module import**; `admin` and the
      scheduler resolve values only through the registry (D28 upheld). Duplicate keys rejected.
- [ ] `todo` (household): `pravedelam_count`, **`done_today` via the existing `done_at` column**
      (see **P-2** — no migration), `open_total`. `events` (household, **shared completion, D68 — no
      events change**): `pripominky_today`, `pripominky_today_open`, `overdue_open`, `due_within_7d`,
      reusing the `events.pripominky` widget's bounded RRULE expansion. `notes`/`documents`
      (**per recipient**): `pinned_count` = household ∪ the caller's personal pins, de-duped.
- [ ] Also add `registry.CollectActions(modules)` — `AuditActions()` currently has **no consumer**;
      §9's catalog is its first one, and it must be merged with the `platform.*` actions (which come
      from no module) and carry a **sample summary** per key for the composer.
- [ ] Tests: all 9 resolve through the registry; `todo.*`/`events.*` identical for two users while
      both `pinned_count`s differ; "today" respects Prague; a resolver error degrades, never panics;
      arch test proves no cross-module import was needed.

### 8.5 `platform/scheduler` (§6; FR-S1, D58/D58a/D74)
- [ ] Minute ticker (single instance ⇒ no locking); `time.LoadLocation("Europe/Prague")` **once**,
      `now` recomputed per tick so DST is automatic — never UTC day math. Due = day matches
      `days_spec` **and** `HH:MM` falls in this tick's minute **and** `last_fired_local_date != today`.
- [ ] **Persist `last_fired_at`/`last_fired_local_date` BEFORE resolving audience and sending** (§6)
      — a mid-send crash then never re-fires the slot; a failed send is recorded in deliveries, and a
      summary is explicitly best-effort rather than at-risk of double-sending.
- [ ] Metric tokens resolve **per recipient** (one render per user, D60); a resolver error degrades
      that token to a placeholder + `warn` and never aborts the send. Missed slot fires once within
      `HOME_SCHED_CATCHUP_GRACE`, else skipped with an `info` log (no backfill storm).
- [ ] **Day-of-month 1–31 with short-month clamping** — effective day = `min(N, daysInMonth(now))`,
      reusing the same integer clamp step as `platform/recur` (D19), so 31 fires on 28/29 Feb, 30 Apr… (D74).
- [ ] Tests (the clamping/DST matrix is the priority, exactly as Phase 4's was): 08:00 + 20:00 daily;
      weekdays/weekends/selected days; DOM 29/30/31 across Feb (leap + non-leap), Apr, Dec; spring-forward
      and autumn-back boundaries; never-double-fire across restarts; catch-up at 119 vs 121 minutes.

### 8.6 `admin` module (§5, §8–§11; FR-ADM1–6, D62)
- [ ] `08001_admin_notifications.sql`: `notification_rules` (**CHECK exactly one of
      `action_key`/`action_prefix`**), `notification_schedules`, `notification_deliveries` (+ its four
      indexes), per §1. Nothing seeded. `registry.Module` with routes + migrations + `AuditActions()`
      and **no `Widgets()`** — admin contributes none.
- [ ] **Template engine** (§8) — a whitelisted token *substitutor*, not a language: `{{event.*}}`,
      `{{change.<field>.old|new}}`, `{{metric.<key>}}`, `{{now}}`, `{{date}}`, **palette per context**
      (broadcast = time only). **Validated at write time** — an out-of-palette token or an unknown
      `metric.<key>` ⇒ `422` on the field; at render time a missing value becomes a safe `—`, never a
      raw `{{…}}` and never an error. Plain text out (no HTML). Expose `render(template, sample)` so
      the composer's live preview runs **the same engine**.
- [ ] **Trigger listener** (§5) — matches `action_key` **or** `action_prefix` **on dotted segment
      boundaries** (`event.` matches `event.update`, not `eventual.x`) AND every set filter; the
      enabled-rule set is **cached in memory and invalidated on rule CRUD** (no DB read per event).
      Empty `body_template` ⇒ the audit event's Czech `summary` (D55). Envelope `Module = ev.Module`
      so a click lands in the originating module, `Type="trigger"`, `Category="triggers"`.
- [ ] **Coalescing** (§5, D57) — per-`rule_id` in-memory debounce over `coalesce_window_seconds`
      (rule) or `HOME_NOTIF_COALESCE_DEFAULT`; `0` ⇒ send immediately. First match starts the timer and
      remembers the render + count; later matches bump the count and refresh the render to the latest
      event; on expiry **one** `Send`, with a Czech "(a N dalších)" suffix when count > 1. In-memory
      only — a restart drops pending buffers (accepted; at-least-once, best-effort).
- [ ] **Audience resolution** via the §8.2 helper: `all` (default, actor included — D66) / `roles` /
      `users`; `exclude_actor` per rule (default false).
- [ ] **Routes**, all behind `httpx.RequireAdmin` + CSRF, mounted like `/api/logs/**`:
      `POST …/broadcast` (render time-tokens → resolve audience → `Send` **off-thread** → 202
      `{recipients, subscriptions}`; `422` on empty title/body or empty resolved audience), rules CRUD
      + `/test`, schedules CRUD + `/test` (**render the current draft, send only to the calling
      admin's** subscriptions, bypassing audience *and* mutes, `kind="test"`), `GET …/catalog`
      (action keys grouped by module **with sample summaries** + metrics + token palette per context),
      `GET …/deliveries` (UUIDv7 keyset, filters kind/status/rule/user/from/to) + a retention prune
      past `HOME_NOTIF_DELIVERY_RETENTION_DAYS`.
- [ ] `AuditActions()` = `admin.broadcast.send`, `admin.rule.create|update|delete`,
      `admin.schedule.create|update|delete`, `admin.notification.test`; every config mutation audits
      **in-tx**; deliveries are **operational, not audit** (D64) and prune on boot + daily.
- [ ] Tests: rule match/no-match matrix; coalescing collapses a burst into one; `exclude_actor`;
      unknown action/metric ⇒ 422; audience matrix incl. empty ⇒ 422; test-send reaches only the
      caller and bypasses mutes; non-admin ⇒ 403 on all nine routes; delivery keyset paging + prune.

### 8.7 Composition (`bootstrap` + `cmd/home/main.go`)
- [ ] `bootstrap.MigrationSources()` += `admin`; `main.go` builds push sender → tailer → scheduler →
      `admin.NewModule(...)` (which `RegisterListener`s and registers its metric use from `New()`),
      appends `adminMod` to `modules`, and **starts the tailer + ticker on `bgCtx`** — per §18 the
      *only* non-registry host wiring v5 adds — stopping them before `srv.Shutdown` (existing pattern).
- [ ] Boot log line per strand (push enabled/disabled, tailer cursor, scheduler tick, N schedules).

### 8.8 Frontend — platform (push + Nastavení)
- [ ] `api/endpoints.ts` + `api/types.ts` + `qk` keys: `['push','vapid'|'prefs']`,
      `['admin','rules'|'schedules'|'catalog'|'deliveries',…]`.
- [ ] `platform/push`: permission state machine (`unsupported | default | granted | denied`),
      `PushManager.subscribe(applicationServerKey)`, POST/DELETE subscription, prefs mutations.
- [ ] **New route `/nastaveni` in `src/platform/settings/` (every role incl. `reader`)** — Oznámení
      panel per the design: **priming step** (the OS prompt fires only on intent), granted / dismissed
      / **blocked recovery callout** / **unsupported** states, master + 3 category toggles, "Poslat
      zkušební oznámení", the unmistakable **"toto zařízení"** framing; plus the theme toggle, the
      **install** affordance and the offline explanation (§13 puts all three on this one screen).
      *(Note for iOS: Web Push requires the app to be installed to the home screen — surfaced in the
      unsupported copy.)*
- [ ] Nav: `Nastavení` joins the "Více" sheet for everyone, `Administrace` for admins only; desktop
      side-nav lists both; route-level `RequireAdmin` on `/administrace` (deep-link ⇒ Přístup odepřen,
      as the design shows).

### 8.9 Frontend — Administrace (4 tabs)
- [ ] `src/modules/admin/` (per §13 / **P-4**): shell + tabs **Rozeslat · Pravidla · Souhrny ·
      Doručení** (mobile segmented / desktop tabs).
- [ ] **The composer, built once and specialised three ways** (the payoff screen): Nadpis + Text,
      **"Vložit údaj"** catalog-driven token palette (time / event / metric by context — tokens are
      *picked*, never typed), and **Živý náhled** rendering tokens resolved to sample values.
- [ ] **Rozeslat**: audience + Poslat test + Odeslat; plural-correct result ("dorazí 4 lidem na 7
      zařízení"); empty-audience guard; nobody-subscribed warning.
- [ ] **Pravidla**: list + editor; **human Czech action picker** (`Když někdo dokončí připomínku`,
      raw key as quiet secondary text) — this Czech phrase map is real work and lives in `i18n/cs.ts`;
      filters; default-body-as-summary placeholder; Sloučit opakování; Upozornit i původce akce
      (default on); enable switch; empty state.
- [ ] **Souhrny**: list + editor; schedule builder (time + day pattern chips, **DOM 1–31 with the
      clamp hint**, not the design file's 28 cap); metric tokens grouped by module with a
      per-recipient note; the 08:00/20:00 examples trivial to build.
- [ ] **Doručení**: reuse the Log browser's filter bar + dense table; kind + status chips; keyset
      paging; copy that says plainly this is a best-effort delivery record, **not** the audit log.

### 8.10 PWA + app-wide offline (D67/D71/D72/D73)
- [ ] `vite-plugin-pwa` (`injectManifest`, `registerType:'autoUpdate'`) + hand-written `src/sw.ts`:
      Workbox `precacheAndRoute` over the injected build manifest **+ navigation fallback to
      `index.html`** so a cold offline load renders the shell; `push` → `showNotification` (envelope →
      title/body/icon/badge/tag/data), `notificationclick` → focus an existing client on `url` else
      `clients.openWindow(url)`, `pushsubscriptionchange` → re-register against `/api/push/subscriptions`;
      **silent activation** (`skipWaiting`+`clients.claim`, **no "nová verze" toast** — D72).
- [ ] `manifest.webmanifest`: name/short_name "Home", `display: standalone`, `start_url`/`scope` `/`,
      `theme_color`/`background_color` = the **dark** token values. **Icons are owed by design**
      (§14: maskable + standard 192/512, Android **mono badge**) — until they land, ship generated
      placeholders from `public/favicon.svg` and keep the ask open.
- [ ] Persisted Query cache (`@tanstack/react-query-persist-client` + IndexedDB persister),
      **namespaced by user id, purged on logout / user change** (§12); the SW caches only public shell
      assets and **never** runtime-caches `/api`. Document bytes are never cached (D73); login/CSRF
      online-only. Mutations are additionally guarded client-side so an offline write fails closed.
- [ ] Offline treatment, app-wide and consistent: one **calm neutral** indicator bar (never
      error-red), every write control **disabled, not hidden**, with "Změny nelze uložit offline";
      online-required inline states for login, document preview/download, upload, and push subscribe.
      **No queue, no sync status, no conflict UI.**
- [ ] **Serving fix (easy to miss):** `frontend/nginx.conf` currently caches every `*.js`
      `immutable` for a year — that would freeze the service worker. Add explicit
      `location = /sw.js` and `= /manifest.webmanifest` (and `registerSW.js`) with
      `Cache-Control: no-cache`, in **both** `nginx.conf` and `nginx.harness.conf`.

### 8.11 Verification, a11y, ops
- [ ] `go build/vet/test ./...` green incl. `internal/arch` (admin imports no feature module).
- [ ] Go, from §16 (beyond each step's own): `action_prefix` matches on **segment boundaries**; a burst
      of N events in one window yields **one** push with the count suffix while `coalesce=0` sends each;
      `exclude_actor` on/off; unknown action/metric key at save ⇒ `422`; admin routes `403` for
      editor/reader; `/api/push/**` works for `reader`; deliveries are recorded per attempt, prune on
      retention, and are **absent from the audit log**.
- [ ] Vitest: composer token insert + live preview resolution (same `render()` as the server);
      permission state machine; offline disabled-writes helper; Czech plurals for
      příjemce/zařízení/oznámení/pravidlo.
- [ ] Playwright + axe on `/administrace` (4 tabs) and `/nastaveni`, **375 px + 1440 px × both
      themes**; offline run via `context.setOffline(true)` proving reads render and writes are
      disabled; keyboard path through composer / audience / schedule builder; targets ≥44 px.
- [ ] Real boot check: subscribe a browser → broadcast arrives → click opens the right route; a rule
      fires off a real `card.move`; a schedule fires at a near-future minute; deliveries show all
      three statuses.
- [ ] **Karel / ops (not code, §14):** generate the VAPID keypair **once** and set the three Coolify
      secrets (never rotate casually — it invalidates every subscription); deliver the **notification
      icon assets still owed from design** (maskable 192/512 + Android mono badge); add the eight v5
      vars to `docker-compose.yml` + the `README.md` env matrix; confirm the new tables restore on a
      fresh Litestream build; update `REGISTRY.md`. *(Also worth a line in `HANDOFF.md`'s index and
      `CHANGELOG.md`, neither of which has a v5 row.)*

**Risks / watch-list.** ① **The cursor seed** (§8.3) — deploying against a DB with months of audit
history and an unseeded cursor means every past event fires as a notification. It is one `MAX(id)`
statement and the loudest possible failure; test it before anything touches production. ② The
permission prompt is one-shot per origin: get the priming step right or a device is unrecoverable from
inside the app. ③ The SW is shared by push and PWA — a broken SW breaks *both*, so keep its two halves
separately testable and ship the shell precache last. ④ `Sink.Record`'s nudge channel (**P-3**) sits
inside every module's write transaction — non-blocking or nothing.

---

## Phase 7 — `documents` (Dokumenty, v4) — **backend ✅ + frontend ✅**

Built from `handoff/v4/HANDOFF-6-documents.md` against the delivered design bundle
(`design/v4/Home.dc.html`, `design/v4/DocumentView.dc.html`). The first module with
bytes outside SQLite.

- [x] **`platform/blobstore`** (new infra, like `db`/`ws`) — `BlobStore` interface (Put/Get-with-range/Stat/Delete/List/Copy, streaming only); **S3/R2** implementation (aws-sdk-go-v2, path-style, region `auto`) and a **filesystem** implementation used by dev and every test, so `go test ./...` needs no network or Docker. One shared contract test table runs against both (S3 only when `HOME_TEST_S3_ENDPOINT` is set).
- [x] **Config + platform additions** — the `HOME_DOCS_*` block with fail-fast validation (a half-configured bucket, or *any* filesystem store in production, aborts boot); `httpx` gains 413/415/416/502 constructors; `audit.ModuleDocuments`.
- [x] **Migrations `07001_documents.sql`** — `document_folders`, `documents` (VACUUM-safe `seq` rowid + `id` TEXT UNIQUE), `document_pins`, `documents_fts` + triggers; `COALESCE` sibling-slug indexes, pin partial-unique indexes, a partial index for the pending-preview scan. Nothing seeded. (Runs after `notes`; the handoff's "before `dashboard`" is moot — `dashboard` is prefix 05 and owns an unrelated table.)
- [x] **Store + service** — folders/documents CRUD, cross-table slug uniqueness in-tx, cycle guard **before** any write, move rewriting **one** row, soft/hard delete (hard purges R2 objects after commit), tree (3 bounded queries), keyset-paged list, FTS search, two-scope pinning, resolve. Every mutation audits in-tx **except personal pins** (D47).
- [x] **Upload pipeline (FR-DOC1 ordering)** — stream through the cap into a temp file while hashing → **sniff** the MIME (extension refinement for OOXML/ODF, which sniff as ZIP) → allowlist → **`Put` to storage** → *then* row + `document.create` in one tx → enqueue preview after commit. Failure modes: 413 over-cap, 415 disallowed, 502 storage outage — **each with nothing written**.
- [x] **Content endpoints** — `raw` (inline only for PDF/image/plain-text/Markdown, attachment for everything else incl. **HTML and SVG**, `nosniff`, `CSP: sandbox`, `ETag`=checksum, `immutable` cache, `Range`→206, `If-None-Match`→304, out-of-range→416), `download` (always attachment, RFC 5987 Czech filename), `preview` (native/derived-PDF, 409 pending-or-failed, 204 download-only), `thumbnail`.
- [x] **Preview worker** — in-process pool, boot re-enqueue of `pending` (crash safety), idempotent, per-job temp dir; **Office→PDF via a Gotenberg sidecar** (not an in-image LibreOffice — see the deviations below), `pdftoppm`+`cwebp` thumbnails, `/ws` `document.preview_ready|preview_failed`. Every failure path degrades to download-only and never loses an upload.
- [x] **`documents.pripnute` widget provider** — household ∪ personal, de-duplicated with household precedence, one bounded query. The live dashboard host picked it up with **no host edit**.
- [x] **Blob backup (D45)** — daily in-process mirror (copy-if-absent into the backup bucket) + reconciliation that deletes aged **orphaned objects** and only ever *logs* **dangling rows**; counts on one structured log line.
- [x] **Frontend** — `/dokumenty/*` slug-path navigation + the permanent `/d/{id}` route; desktop tree + pane, mobile drill-down; **list default with a grid toggle**; multi-file upload queue with per-file progress, client-side size pre-check, and drag-and-drop; standalone `DocumentView` (sandboxed-iframe PDF, `<img>`, Markdown/text) reused **verbatim** by the Nástěnka overlay; two-scope pin menu; move/rename/delete dialogs with the soft-vs-hard distinction; `reader` view-only; **D49 nav** — the mobile "Více" sheet is now shown to *everyone* (Dokumenty for all, Log for admins), desktop lists all six.
- [x] **Tests** — 41 Go tests in `internal/modules/documents` (upload ordering incl. an injected storage outage, immutability, permanent-URL stability across rename+move, isolation headers, preview worker against a fake Gotenberg, slug/resolver/cycle guard, folder cascade + hard purge, pinning matrix, widget de-dup, FTS metadata-only, audit attribution, mirror/reconciliation) plus a **statement-counting driver** proving the tree (3 statements) and the widget (2) do not scale with the data.

**Deviations from `HANDOFF-6` §13/§16, agreed with Karel and documented in `README.md`:**
1. Office→PDF runs in a **Gotenberg sidecar**, so `HOME_DOCS_GOTENBERG_URL` replaces `HOME_DOCS_SOFFICE_PATH` and the backend image stays ~100 MB (poppler-utils + libwebp-tools only).
2. The blob mirror runs **daily**, and `HOME_DOCS_MIRROR_CRON` is a **Go duration** (`24h`), not a cron expression — a cron-looking value is rejected at boot.
3. `/preview` serves PDFs with `CSP: sandbox allow-scripts` (no `allow-same-origin`) because a bare `sandbox` blocks the browser's PDF viewer; the frame stays origin-opaque, and the viewer offers an "open in a new tab" fallback.

**Verified by a real boot** (dev bypass, filesystem store): folder + PDF/Markdown/HTML uploads, inline-vs-attachment headers, ETag→304, `Range`→206, preview CSP, slug resolve, FTS matching metadata but **not** file contents, household pin, `documents.pripnute` through `/api/dashboard/widgets/…`, documents events in `/api/logs` + the entity timeline, and a hard delete removing the stored bytes.

**Remaining (config, not code):** create the two documents R2 buckets + enable versioning on the primary, set `HOME_DOCS_R2_*`, deploy the Gotenberg service, and browser-verify the sandboxed PDF frame at 375/1440 in both themes.

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
