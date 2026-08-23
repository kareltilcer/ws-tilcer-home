# Home — Module 7: `admin` (Administrace) + push, scheduler & PWA

> Build brief for Claude Code. Source of truth for behaviour: `PRD-v5-admin.md` (decisions **D51–D74**, **FR-P1–P5 / FR-S1 / FR-ADM1–6**). Data shapes: `openapi-v5-admin.yaml` (**0.6.0** — merge into `openapi.yaml`). Screens: `HANDOFF-design-v5-admin.md`. Read `HANDOFF.md` §3 (module registry) and `HANDOFF-1-logging.md` (the audit spine) first — v5 leans on both.

## The model in one paragraph

v5 makes Home a **Web-Push sender** and an **installable, reads-only-offline PWA**. It is **one admin-only feature module (`admin`)** plus **three platform strands**: `platform/push` (the shared VAPID channel + subscription store + `Send`), a **notifier/outbox** inside `platform/audit` (a keyset tailer over `audit_events` that drives trigger notifications), and `platform/scheduler` (an in-process wall-clock ticker for summaries). Summaries read cross-module counts through a new `platform/metrics` registry (same shape as widgets). The frontend adds `platform/pwa` (one service worker: push + `notificationclick` + app-shell offline). **No existing module or the auth service changes**; `events` in particular is untouched (OQ-7 reverted, D68). The whole thing registers itself — the only new wiring the host learns is "start the tailer and the ticker on boot."

**Every mutation writes an audit event in the same transaction** (`HANDOFF-1`). The `admin` config actions (rule/schedule/broadcast create/update/delete, test) are audited; **per-device push subscribe/unsubscribe and mute-preference changes are audited too** (they're user actions, but low-volume). Push **deliveries** are NOT audit — they go to an operational table (§9, D64).

## Build order

Build the platform strands before the module that consumes them:

1. **§1 migrations** + **§2 `platform/push`** (channel + `Send`) → **§3 subscription/consent endpoints**. Ship and test push end-to-end (a hand-fired `Send` reaches a subscribed browser) before anything else.
2. **§4 `platform/audit` notifier** (the outbox tailer + `Listener` registration).
3. **§7 `platform/metrics`** registry + the 9 providers (small edits to `todo`/`events`/`notes`/`documents`).
4. **§6 `platform/scheduler`**.
5. **§5, §8, §9, §10, §11 — the `admin` module** (trigger rules, template engine, schedules, broadcast, catalog, deliveries) wiring §2/§4/§6/§7 together.
6. **§12 `platform/pwa`** + **§13 frontend** (Administrace, Nastavení → Oznámení, the app-wide offline treatment).

## 1. Data model (PRD §V5-5) — tables, migrations & registration

Two owners: **`platform` migrations** own the per-user + tailer tables (any module may send, so they can't live in `admin`); **`admin` migrations** own the rule/schedule/delivery tables and run **last** in the sequence.

**`push_subscriptions`** *(platform)* — `id` (UUIDv7) · `user_id` · `endpoint` **UNIQUE** · `p256dh` · `auth` · `user_agent` NULL · `created_at` · `last_seen_at` · `failing_since` NULL. Index `(user_id)`, `UNIQUE(endpoint)`. One row per browser endpoint; a user may have several.

**`notification_preferences`** *(platform)* — `user_id` **PK** · `enabled` DEFAULT 1 · `cat_broadcast` DEFAULT 1 · `cat_triggers` DEFAULT 1 · `cat_summaries` DEFAULT 1 · `updated_at`. Absent row ⇒ treat as all-on (lazy-create on first PATCH).

**`audit_notify_cursor`** *(platform)* — single row: `id` (const `1`, CHECK) · `last_event_id` (UUIDv7, the last processed `audit_events.id`) · `updated_at`. The tailer's persisted keyset position (§4). Seed `last_event_id` to the **current max** `audit_events.id` at first boot after this migration, so v5 does **not** replay the entire pre-existing history as notifications.

**`notification_rules`** *(admin — triggers)* — `id` · `name` · `enabled` · `action_key` NULL · `action_prefix` NULL · `filter_module` NULL · `filter_entity_type` NULL · `filter_level` NULL · `audience` (JSON, §5) · `title_template` NULL · `body_template` NULL · `coalesce_window_seconds` DEFAULT 60 · `exclude_actor` DEFAULT 0 · `created_by` · `created_at` · `updated_at`. CHECK: exactly one of `action_key`/`action_prefix` non-null. Index `(enabled)`.

**`notification_schedules`** *(admin)* — `id` · `name` · `enabled` · `time_local` (`HH:MM`) · `days_spec` (JSON: `{preset}` | `{weekdays:[…]}` | `{day_of_month:N}`) · `audience` (JSON) · `title_template` · `body_template` · `last_fired_at` NULL · `last_fired_local_date` NULL (`YYYY-MM-DD` in Prague) · `created_by` · `created_at` · `updated_at`. Index `(enabled)`.

**`notification_deliveries`** *(admin — operational, prune-able)* — `id` (UUIDv7) · `ts` · `kind` CHECK(`broadcast`,`trigger`,`schedule`,`test`) · `category` CHECK(`broadcast`,`triggers`,`summaries`) · `rule_id` NULL · `user_id` · `subscription_id` NULL · `status` CHECK(`sent`,`failed`,`expired`) · `error` NULL. Index `(ts DESC)`, `(kind, ts)`, `(rule_id, ts)`, `(status, ts)`.

**Migrations & registration.** `platform` tables go in the `platform` migration slot; `admin` ships its own Goose files appended **last**: `logging → platform → todo → events → notes → documents → dashboard → admin`. Must apply cleanly on an empty DB and after a Litestream restore. **Nothing is seeded.** Register `admin` via `registry.Module` (routes, migrations, `AuditActions()` §11; **no `Widgets()`** — admin contributes none). The tailer + ticker are started by the host after registration (§4/§6) — the one non-registry wiring v5 adds.

## 2. `platform/push` — the shared Web Push channel (VAPID)

A new infra package in `platform/`, importable by anyone (like `db`/`ws`/`audit`); the envelope carries the routing tag so **any** module can send (D52). Use **`github.com/SherClockHolmes/webpush-go`** (pure Go, VAPID, aes128gcm — CGO stays off).

```go
type Envelope struct {
    Module   string // "admin" | "events" | "todo" | …  (routes the SW click)
    Type     string // "broadcast" | "trigger" | "summary" | …
    Title    string
    Body     string
    URL      string // in-app path a click opens (default "/")
    Tag      string // optional collapse key
    Category string // "broadcast" | "triggers" | "summaries" (mute bucket)
    Data     map[string]any
}
type Push interface {
    // Send fans out to each recipient's subscriptions, honouring mute prefs.
    Send(ctx context.Context, recipients []string, e Envelope) []DeliveryResult
    VAPIDPublicKey() string
}
```

- **Send.** Resolve each recipient's rows in `push_subscriptions`, **filtered by `notification_preferences`** (master off ⇒ skip user; the envelope's `Category` off ⇒ skip). Encrypt+POST via webpush-go with the VAPID `Authorization`, `TTL` ~ a day, `Urgency: normal`. Bounded concurrency (a worker pool, e.g. 8). Write one `notification_deliveries` row per endpoint attempt (`kind`/`category` from the caller). **Never** block a request thread on Send — callers (broadcast, tailer, ticker) run it off the request path.
- **Dead-endpoint pruning.** `404`/`410` ⇒ **delete** the subscription (status `expired`). `429`/`5xx`/network ⇒ status `failed`, set/keep `failing_since`; a subscription failing continuously past `HOME_NOTIF_MAX_FAILDAYS` is pruned on the next attempt.
- **Payload size:** Web Push caps ~4 KB encrypted — keep bodies short; truncate defensively.
- The VAPID keypair comes from env (§14); `VAPIDPublicKey()` serves the public key to FR-P3. **Never** expose the private key.

## 3. Consent & subscriptions — per-user endpoints (FR-P1–P3, P5)

All `/api/push/**`, **any authenticated member** (incl. `reader`); mutations need CSRF; a member only ever sees/mutates their own rows.

- **`GET /api/push/vapid-key`** → `{ key }` (base64url public key).
- **`POST /api/push/subscriptions`** — upsert on `endpoint` (re-subscribe updates keys + `last_seen_at`, clears `failing_since`; no duplicate), bind to caller. `201`/`200`. `422` on malformed keys/endpoint. Audit `platform.push.subscribe`.
- **`DELETE /api/push/subscriptions`** — body `{endpoint}`; delete the caller's row; idempotent `204`. Audit `platform.push.unsubscribe`. (Also the target of a SW `pushsubscriptionchange`.)
- **`GET/PATCH /api/push/preferences`** — master + 3 category booleans (D53a); lazy-create; honoured by `Send` (§2). Audit `platform.push.prefs` on change.

## 4. `platform/audit` notifier — the transactional outbox tailer (D56)

This is the spine of trigger notifications and the one piece to get exactly right. The audit `Sink.Record` writes inside the **caller's** tx (`HANDOFF-1`), so `platform/audit` can't know when that tx commits — **so don't try**. Instead treat `audit_events` as a **transactional outbox** and tail it: a separate read only ever sees **committed** rows, which is precisely the "fire after commit" guarantee.

```go
// in platform/audit
type Listener interface { OnEvent(ctx context.Context, ev Event, changes []Change) error }
func RegisterListener(l Listener)   // called from a module's New(); no Module-interface change (D56)
func StartNotifier(ctx, db, cursorStore)  // host starts this once on boot
```

- **The tailer** (one goroutine): loop `SELECT … FROM audit_events WHERE id > :cursor ORDER BY id LIMIT :n`, oldest-first (UUIDv7 keyset — same pattern as the log browser). For each row, load its `audit_changes` **only if** a registered listener needs them (match on cheap fields first — §5), call each `Listener`, then advance `last_event_id` in `audit_notify_cursor`. Wake on a ticker (~1 s) **and** on a best-effort signal channel nudged by `Sink.Record` (low latency without losing the poll fallback).
- **Delivery semantics: at-least-once.** The cursor advances after a batch, so a crash mid-batch re-processes — **listeners must be idempotent** (§5 dedups by event id within the coalesce window; the delivery layer tolerates a rare duplicate push). Do **not** hold the cursor back waiting on Send — Send is fire-and-forget from the tailer's view.
- **Import-lint:** the tailer + `Event`/`Change` types live in `platform/audit`; the `admin` module `RegisterListener`s from its `New()`. `admin` imports `platform/audit`, **never** `modules/logging`. Cover with the arch test (§16).
- **Seed the cursor** to the max event id at first boot (§1) so existing history isn't replayed as a notification storm.

## 5. Trigger rules — matching, templating, coalescing (FR-ADM2; D55/D57/D66)

The `admin` module registers one `Listener`. Per event:

- **Match.** Find enabled `notification_rules` where `action_key == ev.Action` **or** `action_prefix` is a dotted prefix of `ev.Action` (`"event."` matches `event.update`; match on segment boundaries, not raw string prefix), AND every set filter (`filter_module`, `filter_entity_type`, `filter_level`) equals the event's. Cache the enabled-rule set in memory, invalidated on rule CRUD (avoid a DB read per event).
- **Audience.** Resolve to user ids (§10 Audience). If `exclude_actor` (**default false**, D66) and the event has an actor user id, drop the actor.
- **Render** title/body via the template engine (§8) with the **event token palette**; an **empty `body_template` ⇒ use `ev.Summary`** (the audit event's Czech human summary, D55). Only load `audit_changes` if the rule's template references a `{{change.*}}` token.
- **Coalesce** (D57): per-`rule_id` in-memory debounce. window `= coalesce_window_seconds` (rule) or `HOME_NOTIF_COALESCE_DEFAULT` (60). `0` ⇒ send immediately. Otherwise buffer: first match starts a timer and remembers the render + a count; further matches within the window bump the count (and refresh the render to the latest event); on expiry, **one** `Send` — if count > 1, append a Czech "(a {{n}} dalších)"-style suffix. In-memory only (single instance; a restart drops pending buffers — acceptable at-least-once). Envelope: `Module=ev.Module` (so a click lands in the originating module), `Type="trigger"`, `Category="triggers"`, `URL` = the entity's in-app route where derivable else `/`.

## 6. `platform/scheduler` + summaries — the wall-clock ticker (FR-S1; D58/D58a/D74)

New infra package; a deliberate, scoped reversal of "no scheduler" (D9/D11 → D58). Single binary ⇒ **one instance, no locking**.

- **Ticker** every `HOME_SCHED_TICK_SECONDS` (60). Load `time.LoadLocation("Europe/Prague")` **once**; compute `now := time.Now().In(prague)` each tick (DST-correct automatically). Never use `Date.now()`-style UTC day math for the day check.
- **Due?** For each enabled schedule: (a) the day matches `days_spec` — `preset` daily/weekdays(Mon–Fri)/weekends; or `weekdays` set contains `now.Weekday()`; or **`day_of_month` with clamp (D74): effective day = `min(N, daysInMonth(now))`**, so 31 fires on 28/29 Feb, 30 Apr, etc. (b) `now`'s `HH:MM == time_local` within this tick's minute. (c) Not already fired: `last_fired_local_date != now.Format("2006-01-02")`.
- **Fire.** **Set `last_fired_at`/`last_fired_local_date` first** (persist), *then* resolve audience and send — so a mid-send crash never re-fires the slot (deliveries record failure; the summary is best-effort, not double-sent). Resolve **metric tokens per recipient** (§7) — one render per user, since personal metrics differ (D60). Envelope `Module="admin"`, `Type="summary"`, `Category="summaries"`, `URL="/"` (or a dashboard route).
- **Catch-up (D58a):** on a tick, if a slot's `HH:MM` is **earlier than now but within `HOME_SCHED_CATCHUP_GRACE` minutes** (default 120) and it hasn't fired for today, fire it once (covers a deploy/restart across the slot). Older misses are skipped — no backfill storm.
- A metric resolver error degrades that token to a safe placeholder and logs `warn`; it never aborts the whole send.

## 7. The metrics catalog — provider contract + 9 launch metrics (D59/D60/D69)

A new `platform/metrics` registry, shaped like the widget catalog (a third "modules declare what they provide" surface). Modules `Register` in their `New()` — **no cross-module import**; `admin`/scheduler resolve values only through this registry (D28 upheld).

```go
// platform/metrics
type Descriptor struct { Key, Label, Unit string; Scope string } // Scope: "household" | "personal"
type Provider interface {
    Descriptors() []Descriptor
    Value(ctx context.Context, userID, key string, asOf time.Time) (int, error)
}
func Register(p Provider)
func Catalog() []Descriptor
func Resolve(ctx, userID, key string, asOf time.Time) (int, error)
```

`asOf` is `time.Now().In(prague)` at fire time; "today" = that local day. Reuse the existing widget providers' bounded reads — no new N+1. **Launch set (D69):**

- **`todo` (household):** `todo.pravedelam_count` = cards in `kind=now` columns across non-archived boards (the `todo.pravedelam` population). `todo.done_today` = cards whose current column is `kind=done` and whose move-to-done happened on `asOf`'s local day (add a `done_at` timestamp on the move-to-done transition, or derive from the `card.move`→done audit rows for today; module's call). `todo.open_total` = cards with `kind in (normal, now)` across non-archived boards.
- **`events` (household — shared completion, D68):** `events.pripominky_today` = reminder occurrences due `asOf`'s day (the same bounded RRULE expansion the `events.pripominky` widget does). `events.pripominky_today_open` = those without a completion row for that occurrence. `events.overdue_open` = past occurrences within the lookback still uncompleted. `events.due_within_7d` = occurrences due in `[today, today+7d]`.
- **`notes` / `documents` (personal, per recipient):** `notes.pinned_count` / `documents.pinned_count` = the caller's visible pins (household ∪ their personal, de-duped) — differs per user, which is why summaries resolve per recipient.

## 8. The template engine — fixed safe token palette (D61)

Not a general template language — a **whitelisted token substitutor**, so an admin can never inject logic.

- **Palettes by context:** broadcast → `{{now}}`, `{{date}}`. trigger → `{{event.summary|action|module|entity_type|entity_id|actor_label}}`, `{{change.<field>.old|new}}`, plus time tokens. summary → `{{metric.<key>}}` (any key in the catalog) + time tokens.
- **Validate at write time** (rule/schedule save): parse `{{…}}`, reject any token not in the context's palette or any `metric.<key>` not in the catalog → `422` (surfaces on the field, per design). This is why the composer offers dropdowns, not free-typed keys.
- **Render:** substitute tokens with resolved values; a missing/errored value → a safe placeholder (`—`), never a raw `{{…}}` and never an error to the user. Output is plain text (notification body) — no HTML, no escaping games.
- The **live preview** the design shows is this same engine run over sample values (event: a representative recent event; metric: the descriptor's sample) — expose a tiny `render(template, sampleContext)` the FE composer can call, or resolve preview server-side in the catalog response.

## 9. Broadcast, catalog & deliveries (FR-ADM1/4/5; D54/D64)

- **Broadcast (`POST /api/admin/notifications/broadcast`)** — `admin`+CSRF. Render (time tokens only), resolve audience, `Send` off-thread, record a delivery batch, **audit `admin.broadcast.send`** with recipient count in `meta`. `202` `{recipients, subscriptions}`. `422` on empty title/body or empty resolved audience.
- **Catalog (`GET …/catalog`)** — assemble from the **live registry**: the **action-key catalog** (the existing `AuditActions()` aggregate, grouped by module, each with a sample summary), the **metrics catalog** (`metrics.Catalog()`), and the **token palette** per context. This is what makes the composer's freedom usable *and* bounded.
- **Deliveries (`GET …/deliveries`)** — paged (UUIDv7 keyset, `Limit`/`Cursor`), filter `kind`/`status`/`rule_id`/`user`/`from`/`to`. **Operational, not audit.** A prune (on a slow ticker or at write) removes rows older than `HOME_NOTIF_DELIVERY_RETENTION_DAYS` (default 30; `0` = keep forever, D64).
- **Test send (`…/rules/{id}/test`, `…/schedules/{id}/test`)** — render with the current draft/rule and `Send` **only to the calling admin's** subscriptions, bypassing audience + mute. `kind="test"`. Audit `admin.notification.test`.

## 10. Endpoints (see `openapi.yaml` 0.6.0) + role gating

- **Per-user (`push`, any member):** `GET /api/push/vapid-key` · `POST|DELETE /api/push/subscriptions` · `GET|PATCH /api/push/preferences`.
- **Admin (`admin-notifications`, `admin` only — same gate as `/api/logs/**`):** `POST …/broadcast` · `GET|POST …/rules` · `GET|PATCH|DELETE …/rules/{id}` · `POST …/rules/{id}/test` · `GET|POST …/schedules` · `GET|PATCH|DELETE …/schedules/{id}` · `POST …/schedules/{id}/test` · `GET …/catalog` · `GET …/deliveries`.
- **Audience** (JSON on broadcast/rules/schedules): `{ scope:"all"|"roles"|"users", roles?:[…], users?:[…] }`. Resolve `all` = every user with ≥1 subscription; `roles` = members holding any listed role (roles come from home's session/role cache, not client input); `users` = the listed ids. Default scope **all** (D66). `exclude_actor` (trigger rules only, default false).
- **Gating:** admin routes reject non-admin with `403` (`*` superuser passes; **no separate superadmin tier**, D62). Writes require CSRF. There is **no reader "view-only" Administrace** — the module is absent for non-admins (the FE routes them to Nastavení → Oznámení).

## 11. Audit (spine, `HANDOFF-1`)

`admin.AuditActions()` declares: `admin.broadcast.send`, `admin.rule.create|update|delete`, `admin.schedule.create|update|delete`, `admin.notification.test`; and `platform`-emitted `platform.push.subscribe|unsubscribe|prefs`. Config mutations write their audit event in the same tx as the row change (rule/schedule/pref writes). The notifier **logs itself** — recursive but bounded, like `logging.prune` — so browsing Log shows who changed a rule and when. **Deliveries are NOT audit** (§9).

## 12. `platform/pwa` — service worker, manifest, offline reads-only (D67/D71/D72/D73)

App-wide frontend infra; shares the **one** service worker with push. Use **`vite-plugin-pwa` in `injectManifest` mode** (custom SW + Workbox precache injection) with `registerType: 'autoUpdate'`.

- **Manifest** (`manifest.webmanifest`): `name`/`short_name` "Home", **maskable + standard icons** (192/512), `display: standalone`, `theme_color`/`background_color` = the dark token values (splash/standalone chrome is dark, not white), `start_url: "/"`, `scope: "/"`.
- **Service worker:**
  - **Precache the app shell** (Workbox `precacheAndRoute` over the injected build manifest, hashed by build id) + **navigation fallback** to `index.html` for SPA routes, so a cold offline load renders the shell.
  - **`push`** → `showNotification(title, {body, icon, badge, tag, data:{module,type,url}})` from the envelope. **`notificationclick`** → focus an existing client on `url` or `clients.openWindow(url)` (route on `module`/`type`).
  - **Silent auto-update (D72):** `autoUpdate` = `skipWaiting` + `clients.claim`; **no update toast** (Do NOT build one). No offline write queue exists, so there's no version-skew to gate.
  - **Do NOT** runtime-cache `/api` responses in the SW (avoids any cross-user cache leak). Offline **reads** come from persisted TanStack Query (below), not the SW.
- **Offline data = persisted TanStack Query (D71):** `persistQueryClient` with an **IndexedDB** persister, **namespaced by user id**, **cleared on logout / user change**. On boot the app hydrates from it, so last-synced boards/cards/events/notes/document-**metadata** render offline. **Document bytes are never cached (D73)** — preview/download show an online-required state. **Login/CSRF are online-only.**
- **Offline UX (app-wide):** an `online`/`offline` hook (`navigator.onLine` + events) drives (a) the global **calm neutral** status banner ("Jste offline — zobrazená data mohou být starší · Změny nelze uložit offline…", `role="status"`, **not** error-red), and (b) **disabling every write control** (create/move/edit/complete/pin/upload) with "Změny nelze uložit offline" — **disabled, not hidden**. Mutations are additionally guarded client-side so an in-flight offline write fails closed with that message. **No queue, no background sync, no conflict UI** (Do NOT build them).
- **Security:** because the persisted cache holds another session's data risk, key it by user id and purge on logout; the SW caches only public shell assets.

## 13. Frontend — Administrace + Nastavení → Oznámení (design: `HANDOFF-design-v5-admin.md`)

Build against the v5 design addendum (Claude Design's `Home.dc.html` renders it). Two surfaces + the nav/offline cross-cutting from §12.

- **`src/modules/admin/`** — the **Administrace** page, `admin`-gated, in the **"Více"** overflow (mobile) and the side-nav (desktop). Four tabs: **Rozeslat** (composer + audience + test/send + result), **Pravidla** (rule list + editor: catalog action-picker showing the human Czech phrase over the quiet raw key, filters, composer with event tokens + default-body-as-summary placeholder, "Sloučit opakování", "Upozornit i původce akce" **default on**, audience, save/test/delete), **Souhrny** (schedule list + editor: time + **day-pattern incl. 1–31 with the last-day clamp hint, not a 28 cap**, composer with metric tokens grouped by module, per-recipient note, audience, save/test), **Doručení** (Log-style filter bar + dense table, kind/status chips). The **composer + live preview** is the shared, hardest piece — one component, three token palettes by context; resolve preview via `render()` (§8).
- **Nastavení → Oznámení** (platform/settings; **every role incl. `reader`**): per-device subscribe with a **priming step** (browser prompt only on intent) and **granted / dismissed / blocked / unsupported** states; **master + 3 category** mutes; self-test. "toto zařízení" must be unmistakable. PWA install affordance + the offline explanation live in the same Nastavení screen.
- TanStack Query keys: `['push','prefs'|'vapid']`, `['admin','rules'|'schedules'|'catalog']`, `['admin','deliveries',{filters}]`; invalidate the list on each mutation. Query persistence scoped per user id (§12).

## 14. Config (PRD §V5-9) + prerequisites

- `HOME_VAPID_PUBLIC_KEY`, `HOME_VAPID_PRIVATE_KEY`, `HOME_VAPID_SUBJECT` (`mailto:` or origin) — **secrets**.
- `HOME_NOTIF_COALESCE_DEFAULT` (60 s), `HOME_NOTIF_DELIVERY_RETENTION_DAYS` (30; 0=keep), `HOME_NOTIF_MAX_FAILDAYS` (14), `HOME_SCHED_TICK_SECONDS` (60), `HOME_SCHED_CATCHUP_GRACE` (120 min). Existing `HOME_TIMEZONE` (`Europe/Prague`) governs all date math.

**Prerequisites (Karel / ops, before build):** **generate the VAPID keypair once** (`webpush.GenerateVAPIDKeys()` or `web-push generate-vapid-keys`) and put it in the Coolify secrets above — do **not** commit or regenerate per deploy (rotating the key invalidates every existing subscription). Provide the **Home notification icon assets** the manifest + `showNotification` need: a **maskable** icon (192/512) and an Android **mono badge** (still owed from design).

## 15. Security

- VAPID **private key never leaves the server**; only the public key is served (FR-P3).
- Admin routes gated server-side to `admin` (+ `*`); CSRF on every cookie-authenticated mutation; **no separate superadmin** (D62).
- Subscription rows are per-user — a member can never read/delete another's; the endpoints filter by session user id, not a client-supplied id.
- **PWA cache isolation:** the persisted query cache is namespaced by user id and **purged on logout**; the SW caches only public shell assets; `/api` is never SW-cached. A logout→re-login as a different user must not surface the prior user's data.
- Push bodies can appear on a **lock screen**; per D70 there is **no composer restriction** — accepted for a household app (don't build a redaction toggle).

## 16. Tests

- **Outbox tailer:** an event committed by any module is delivered to a registered listener **after commit**; the cursor advances and persists; a simulated crash mid-batch re-processes (**at-least-once**) and the listener dedups so at most one push results; the cursor is seeded to max-id on first boot (no history replay).
- **Import/arch:** `modules/admin` imports **no** feature module (may import `platform/{push,scheduler,metrics,audit,db,ws}`); it `RegisterListener`s without importing `logging`; metric providers are resolved only via `platform/metrics`. The arch test fails on any cross-module import.
- **Trigger matching + coalescing:** `action_key` and `action_prefix` (segment-boundary) match; filters AND correctly; empty `body_template` → the event's `summary`; a burst of N matching events within the window yields **one** push (count suffix); `coalesce=0` sends each; `exclude_actor` drops the actor (and default **false** keeps them).
- **Scheduler:** the 08:00 and 20:00 examples fire at the right Prague minute; **correct across a spring-forward / fall-back DST boundary**; **day-of-month clamps** (31 fires on the last day of a short month); a slot fires **once** per local day (no double-fire on the next tick); a slot missed by < grace fires once on recovery, > grace is skipped; `last_fired` is set before Send.
- **Metrics:** each of the 9 resolves through `platform/metrics` (no module import); `todo.*`/`events.*` are identical for two users (household), `notes.pinned_count`/`documents.pinned_count` differ per user (personal); "today" respects Prague.
- **Push/prefs:** `Send` skips a user with master-off and a category-off for that envelope's category; `404`/`410` prunes the subscription (status `expired`); a continuously-failing sub is pruned past `MAX_FAILDAYS`; re-subscribe upserts (no duplicate endpoint).
- **Endpoints/roles:** admin routes `403` for `editor`/`reader`, `200`/`*` for admin; `/api/push/**` work for `reader`; unknown `action_key`/`metric` key at save → `422`; CSRF enforced on mutations.
- **PWA/offline:** cold offline load renders the shell + last-synced reads from the persisted cache; every write control is disabled with the standard message and **no mutation queues**; document preview/download and login show online-required; a new build activates **silently** (no toast); logout purges the persisted cache (no cross-user leak).
- **Delivery log:** rows recorded per attempt with correct `kind`/`status`; retention prune drops rows past the window; it is **not** in the audit log.

## 17. Definition of done

- [ ] `push_subscriptions`, `notification_preferences`, `audit_notify_cursor` (platform) + `notification_rules`, `notification_schedules`, `notification_deliveries` (admin) created by migrations, `admin` slotted **last**; clean on empty DB + after Litestream restore; nothing seeded; cursor seeded to max event id.
- [ ] `platform/push`: VAPID via webpush-go; `Send` honours master+category mutes, bounded fan-out, prunes 404/410, records deliveries; only the public key ever leaves the server.
- [ ] Subscription/consent endpoints (subscribe/unsubscribe/vapid-key/preferences) for **all roles**, per-user isolation, CSRF, audited.
- [ ] **Outbox tailer** in `platform/audit`: keyset scan, persisted cursor, at-least-once, idempotent; `admin` registers its listener **without importing `logging`** (arch test green).
- [ ] **Trigger rules** CRUD; key/prefix + filter matching; default body = `summary`; token render; **60 s coalescing** collapses bursts; `exclude_actor` default **false**; audience default **all incl. actor**.
- [ ] **`platform/scheduler`**: Prague/DST-correct minute ticker; **day-of-month 1–31 with last-day clamp (D74)**; fire-once-per-slot; catch-up within grace; `last_fired` set before Send; the two worked examples build trivially.
- [ ] **`platform/metrics`** + the **9 providers** resolved through the registry (no cross-module import); household vs per-recipient scope correct; Prague "today".
- [ ] **Template engine**: whitelisted palettes per context; unknown token/metric → `422` at save; safe placeholder at render; live preview uses the same engine.
- [ ] **Broadcast / catalog / deliveries / test-send** per FR-ADM1/4/5/6; catalog returns actions+metrics+tokens; deliveries paged/filterable + 30-day prune; test reaches only the caller.
- [ ] `admin` module: admin-only gate (== Log; no reader state); nav in **"Více"** (mobile) + side-nav (desktop) for admins; declares `AuditActions()`; all config mutations audited in-tx.
- [ ] **`platform/pwa`**: one SW (push + notificationclick + shell precache + nav fallback); **silent auto-update, no toast**; installable manifest (standalone, dark, maskable icons).
- [ ] **App-wide offline reads-only**: persisted TanStack Query per user (cleared on logout); calm neutral banner; write controls **disabled** with "Změny nelze uložit offline"; document bytes + login online-only; **no queue/sync/conflict**.
- [ ] Frontend Administrace (4 tabs + composer/live-preview) and Nastavení → Oznámení (per-device, permission gauntlet, master+3 mutes, self-test) built against `HANDOFF-design-v5-admin.md`; loading/empty/error/permission/offline states; 375 px + 1440 px, both themes.
- [ ] Config wired (§14); VAPID keypair generated + in Coolify secrets; icon assets present; import/arch test covers `admin`; `openapi.yaml` bumped to 0.6.0; `REGISTRY.md` reflects `admin` once deployed.
- [ ] **Untouched:** `events` (shared completion, D68), auth, and the dashboard-host contract are unchanged; the arch test confirms no other module imports `admin`.

## 18. Module packaging & build order

`admin` is **module 7**, built after the foundation, the spine, and the four v5 platform strands it consumes. Package it like the others (`HANDOFF.md` §3): own `module.go` implementing `registry.Module` (routes, migrations, `AuditActions()`; **no `Widgets()`**), own `migrations/*.sql`, own frontend `src/modules/admin/`; it registers its **audit `Listener`** (§4) and its use of `platform/scheduler`/`platform/metrics` from `New()`. It may import only `platform/*` (incl. the new `platform/{push,scheduler,metrics}` and the notifier in `platform/audit`), **never** another feature module. The metric providers are **small edits inside `todo`/`events`/`notes`/`documents`** (each adds a `platform/metrics.Register` in its `New()` — no cross-module import). The only non-registry host wiring v5 adds is **starting the tailer and the ticker on boot**. Migrations slot in **after `dashboard`, last** in the sequence.
