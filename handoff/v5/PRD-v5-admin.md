# PRD — Home · v5 addendum (Admin module + push notifications + PWA)

> Status: **Draft v5** — adds the **Admin** (`admin`) module, two platform capabilities (`platform/push`, `platform/scheduler`), and a **PWA** strand (`platform/pwa`, installable + reads-only offline) on top of v4. Self-hosted login (Mode B), widget dashboard, modular architecture unchanged. **All open questions resolved 2026-08-16** (§V5-10) — no open items remain. Decisions **D51–D73** (D68 reverted). · Owner: Karel · Last updated: 2026-08-16
> Companion spec: `openapi.yaml` (OpenAPI 3.1, **v0.5.0 → v0.6.0**; companion `openapi-v5-admin.yaml`) · Build: `HANDOFF-7-admin.md` + design addendum follow approval.
> This file is an **addendum** — it states only the v5 delta. Everything not mentioned here is unchanged from v4. Section numbers are prefixed `V5-` so they fold into `PRD.md` without renumbering v4.

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
- **Behaviour:** evaluated by FR-S1; metric tokens resolve **per recipient**. CRUD audited. `422` on an unknown metric key.
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
- `events.pripominky_today` — reminder occurrences due today.
- `events.pripominky_today_open` — of those, not completed (household completion).
- `events.overdue_open` — past occurrences still uncompleted.
- `events.due_within_7d` — occurrences due in the next 7 days.

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
- Existing `HOME_TIMEZONE` (`Europe/Prague`) governs all schedule/metric date math.

---

## V5-10. Decisions (D51–D73) & Resolutions

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
- [ ] OpenAPI 0.6.0 validates; new paths/schemas reuse shared `Cursor`/`Limit`/`responses`/security components.
