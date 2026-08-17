# Home — Changelog

Version history for the `home` service. Full detail lives in `PRD.md` (§10 Decisions) and the `HANDOFF*.md` set. OpenAPI versions track the API contract in `openapi.yaml`.

---

## v6 — 2026-08-17 (spec) · **DRAFT, not built** · Finance (finance) + the `fin` migration & retirement

> OpenAPI **0.7.0 → 0.8.0**. Decisions **D81–D98**. Triggered by one product decision from Karel: the standalone `fin` service becomes a module of home, and then stops existing.

### Headline

An **eighth module, Finance (`finance`)** — a functional clone of `fin.tilcer.cz`, the two-person household budget-split app that has been running as its own fe/be pair since June. v6 is the first version of home that **removes a live service**: it absorbs `fin`'s behaviour, migrates its months, verifies them row-for-row, and only then retires the original.

Unlike v3/v4/v5, the interesting part is not the feature. Finance is the simplest module home has — one table, one form, no scheduler, no blob store, no new platform strand, **no new environment variable**. The work is in the two things that must not go wrong: **the calculation must be identical**, and **the data must be provably intact before anything is switched off**.

### What crosses over, and what doesn't (D81–D83)

`fin` is a full service: its own Mode B session store, its own auth client, its own JWT plumbing, its own English React SPA. Almost none of that is worth carrying — home has a better version of each. **Two** things cross over:

- **The locked split formula (D82)**, verbatim, including the rounding order and the four reconciliation invariants, with `fin`'s worked-example test. It is the one thing in `fin` that took real work, and only because the *old* app it replaced had two contradictory implementations. The split stays **derived on read, never stored**.
- **The column vocabulary (D83)** — `income_kaja`, `income_andy`, four `rate_*` — literally. Only the table is namespaced (`finance_months`), because home is one database holding eight modules' tables. No generalisation to person A/B or N people: it would put a translation layer between the formula, the seed and the tests for no behavioural gain in a two-person household.

Everything else is home's: Czech UI (D85), home's session and roles (D84), **the audit spine** (D86 — `fin` had no audit trail at all, and it is what makes keeping `fin`'s hard delete safe, D87), `snake_case` + `PATCH` + `/api/finance/months` (D92), live sync (D94), PWA read caching (D95). `fin`'s import endpoint is **dropped** rather than ported — the seed replaces it, so no import API outlives the one import it existed for.

### Citizenship: what the module contributes (D88–D90)

- **One widget**, `finance.rozpocet` (narrow, all roles), with **two states**: the current month's headline split, or **"Zadat ⟨měsíc⟩"** when the current month has no row. The second state is the point — the app's real failure mode is a month nobody entered, and `fin` had no way to say so.
- **Four household metrics** (9 → 13) and **one list** (8 → 9), both including `finance.missing_months`, which mirror each other by construction (D77's rule).
- Composed with v5's conditions (D75), those turn the failure mode into a notification: a summary on day 1, conditioned on `finance.missing_months gt 0`, listing exactly what is missing — and silent in every month where nothing is.

### Access (D84)

Finance is an **ordinary module, not an admin one**: reads for every member including `reader`, writes `editor`/`admin` — **including delete, which has no separate admin tier** because there is no soft/hard distinction to gate. It sits in the **"více" overflow for everyone**, beside Dokumenty — a once-a-month destination does not earn one of the four thumb tabs. Stated plainly in the PRD: a `reader` therefore sees both household incomes, which is accepted for this household and is the first thing to reconsider if a `reader` account ever goes to somebody outside it.

### Data model (§V6-5)

One table, `finance_months`, migration block **09** — inputs only, no stored split, plain `UNIQUE(month)`. Delete is **hard**, as in `fin` (D87): no `deleted_at`, no `?hard=true`, no admin tier. A month is seven numbers that take twenty seconds to re-enter, and carrying a nullable column plus a filter on every read to protect it is not a trade worth making twelve times a year — **the audit spine is the compensating control**, with `month.delete` writing a full-row diff so the deleted numbers stay readable in the Log. `fin`'s table-level rate-sum CHECK is kept deliberately: it makes a bad *seed* row fail loudly at migration time instead of quietly at read time.

### The migration (D91)

The historic months arrive as a **one-off Goose seed** with `fin`'s ids and timestamps preserved — shipped in its **own migration source** (`finance/seed`, block `09900`) that the server entrypoint includes and **`testsupport` excludes**. Without that split every module test would run against a database pre-loaded with thirty months of real household finances. `bootstrap.MigrationFS()` stays schema-only; `MigrationFSWithSeed()` is the opt-in — the default is the safe one on purpose.

### The retirement (D96–D98)

Gated, ordered, and **not** collapsed into the deploy:

1. Home v6 goes live **with `fin` still running**.
2. **Verification** — row-for-row against `fin`'s live output, **including all nine recomputed split values**. Comparing inputs alone would not catch a mis-ported formula, which is the one mistake this migration can actually make. Any mismatch stops everything.
3. Retire in order, with **no redirect** (D96): tell both users the app moved → stop the backend and frontend after a final snapshot → **retain** the `fin/` R2 prefix as provenance → archive the repo → deprovision the auth site last. `fin.tilcer.cz` simply goes away; with two users who both know where it went, a redirect app running indefinitely was not worth the standing infrastructure. The trade-off to respect: that redirect would also have been the post-cutover fallback, so the verification in step 2 is now the only gate.

**D98** catches something easy to miss: `services/fin/` in the project folder is **empty**. `fin`'s PRD, OpenAPI spec and handoff exist only inside the repo about to become read-only, so they are recovered into the project record *before* archiving.

### Open questions — resolved 2026-08-17

OQ-V6-1 → **hard delete, as in `fin`** (D87 rewritten; the audit full-row diff is the compensating control) · OQ-V6-2 → **"více" overflow**, the four thumb tabs untouched · OQ-V6-3 → **no redirect**, `fin.tilcer.cz` switched off outright (D96 rewritten) · OQ-V6-4 → **accepted**, a `reader` sees both household incomes. **None open.**

### Design

The **v6 design addendum** was drafted the same day into `HANDOFF-design.md` §v6 (pending Karel's approval). Unusually for this project it briefs against a **working reference UI** — `fin`'s own React frontend — so the round is about what to carry across intact versus what must change to become a Home screen. It also carries one **computed** finding: Home's existing `--c1`…`--c5` categorical palette **fails** the data-viz CVD checks for this module's four buckets (`c2`↔`c3` green/amber at ΔE 4.4 protan and 12.3 normal-vision; `c1`↔`c4` blue/violet at ΔE 0.8 protan in every possible ordering). Two paths are specified — reorder to `c1,c2,c4,c3` plus mandatory secondary encoding, or re-step the tokens (a validated candidate is given, which also repaints the Log's stats bars). Karel's call.

---

## v5 — 2026-08-16 (spec) · **deployed 2026-08-17** · Administrace (admin) + Web Push + PWA

> OpenAPI **0.5.0 → 0.6.0** (spec) **→ 0.7.0** (as built). Decisions **D51–D74** in the spec round, **D75–D80** during the build (`PRD.md` §V5-12). Triggered by one product addition from Karel: notifications.

### Headline

A **seventh module, Administrace (`admin`)** — admin-only, gated exactly like the Log browser and reached through the **"více"** overflow — that turns Home into a **Web Push sender**, plus the app-wide PWA groundwork the channel rides on. Unlike v3/v4 this is not only a feature module: it adds **five platform strands** (`push`, `scheduler`, `metrics`, `lists`, `pwa`) and an **outbox tailer** inside `platform/audit`. Auth, the dashboard-host contract, and the six existing modules are unchanged — `events` explicitly so (D68).

### What Administrace is (§V5-4, FR-P1–P5 / FR-S1 / FR-ADM1–6)

- **One shared push channel** (D52) — one service worker ⇒ one subscription per device, VAPID, every module sending through `platform/push.Send(envelope{module,type,title,body,url,tag,category,data})`. No per-module channel, no notification bus.
- **Subscription and consent are per-user, platform-owned** (D53) — **Nastavení → Oznámení** for every role including `reader`: permission + subscribe this device, a master switch and `broadcast`/`triggers`/`summaries` mutes (D53a), and a **self-test** (D78). An admin configures what is sent; an admin **cannot force-subscribe** anybody.
- **Three ways to send.** **(1) Broadcast** — ad-hoc to an audience, audited, recorded, not a persisted rule (D54). **(2) Trigger rules** — bind an **audit action key** to a push, default body = the event's Czech `summary`, overridable with a fixed safe token palette (D55/D61), with per-rule **coalescing** (default 60 s, `0` = every event, D57) and `exclude_actor` (default false, D66). **(3) Scheduled summaries** — wall-clock pushes composed from the **metrics catalog**, resolved **per recipient** (D59/D60).
- **`audit_events` is the transactional outbox** (D56) — a platform tailer reads it by UUIDv7 keyset with a persisted cursor (`audit_notify_cursor`), at-least-once and idempotent, fanning out to registered listeners; the `admin` listener reacts to todo/events/notes/documents alike **without importing any of them**, so the import-lint acceptance criterion (D28) stays true.
- **A scheduler exists** (D58) — a deliberate, scoped reversal of v1's "no scheduler": an in-process minute ticker, `Europe/Prague` and DST-correct, `last_fired` idempotency, catch-up only within 120 minutes (D58a), and a **day-of-month 1–31 with a clamp to the month's last day** in short months (D74, matching events' D19).

### PWA (§V5-1a, D67/D71/D72/D73)

Home becomes **installable** (manifest: `standalone`, dark, maskable icons) and **reads-only offline**: app-shell precache plus a **persisted TanStack Query cache** in IndexedDB, namespaced per user and cleared on logout. Deliberately **no service-worker `/api` cache** (it would leak across users), **no offline mutation queue, background sync, or conflict handling** — write controls simply disable offline. Login and document bytes stay online-only (D73). New builds activate **silently** (D72) — safe precisely because there is no write queue to skew.

### Data model (§V5-5)

- **Platform:** `push_subscriptions`, `notification_preferences`, `audit_notify_cursor`.
- **Admin:** `notification_rules`, `notification_schedules`, `notification_deliveries` — deliveries are **operational, not audit** (D64): 30-day default retention, dead endpoints (404/410) self-delete, continuously-failing subscriptions pruned after `HOME_NOTIF_MAX_FAILDAYS`.
- Migrations: platform gains `02002_push` + `02003_audit_cursor`; `admin` is a new `08001` block, applied last.
- **No new blob store**; the tables ride the existing Litestream `home/` replication (D65).

### API (openapi 0.5.0 → 0.6.0 → **0.7.0**, §V5-6)

- **Spec (0.6.0):** `/api/push/*` (vapid key, subscribe/unsubscribe, preferences) and `/api/admin/notifications/*` (broadcast, rules CRUD + test, schedules CRUD + test, deliveries, catalog); tags `push`, `admin-notifications`.
- **As built (0.7.0), the six post-spec additions (D75–D80):** **conditions** on rules *and* schedules (count-vs-number clauses, all/any, evaluated at send/fire time, **failing open**); **active hours** on trigger rules (wrapping `HH:MM` window, drops rather than queues); a **lists catalog** — the fourth registered catalog — with `{{list.…}}` body tokens naming the items behind a metric's number; **`POST /api/push/test`**; the **household member directory** on the catalog (with per-member device counts) and `user_label` on deliveries; and a **server-rendered Czech schedule `description`**, plus `url` path validation and `filter_module` qualification.

### Audit / security

Every admin configuration mutation is audited through the spine as usual; `admin` declares its `AuditActions()`. VAPID keys are **Coolify secrets** and only the public half is ever served (D65) — they are generated once with `cmd/vapidgen` and **never rotated**, since rotation invalidates every existing subscription. Subscription **endpoints are allowlisted** against known push services rather than trusted (D78). Audience resolution needs no new table: `all` = every user with a subscription, `roles` from home's **session role cache** (never client input), `users` by id; labels come from `sessions.display_name`.

### Frontend & nav (§V5-7)

New **Administrace** page (4 tabs: broadcast, triggers, schedules, deliveries) with the composer, audience picker and schedule builder; **Nastavení → Oznámení** for every role. Nav is unchanged in shape — Administrace joins Log in the admin-only part of **"více"**.

### Non-goals (v5)

No cross-service notification bus; no per-module push channels or subscriptions; no offline writes, sync queue or conflict resolution; no email/SMS fallback; no per-user reminder completion in `events` (D68 reverts OQ-7); no new "superadmin" role tier (D62); no lock-screen content restriction (D70).

### Relation to v4

v4 (Dokumenty) was the last spec-only version. **v5 closed the gap: v3, v4 and v5 were all built and deployed together**, so the live app went from v2's four modules to seven. The written masters lagged the build by four merged branches — `PRD.md` §V5-12 and `openapi.yaml` 0.7.0 are the reconciliation.

---

## v4 — 2026-08-11 (spec) · Dokumenty (documents) module

> OpenAPI **0.4.0 → 0.5.0**. Decisions **D39–D50** added; **D6** extended again, **D33** reaffirmed. Triggered by one product addition from Karel: a documents/file-storage module.

### Headline

A **sixth module, Dokumenty (`documents`)**, added on top of v3 as a self-contained package registered through the existing module registry — **no change** to Mode B auth, the dashboard-host contract, or the todo/events/logging/notes modules. It is the **first module with blob storage**: it owns a dedicated **Cloudflare R2 bucket** for file bytes alongside the Litestream-backed SQLite (which now holds only document *metadata*).

### What Dokumenty is (§1, §4 FR-DOC1–DOC11)

- **Upload / preview / download / delete files** — PDFs, images, and Office/other docs — in a **single-parent folder tree** (its own tree, isolated from Poznámky — D40), reusing Poznámky's slug/resolver/cycle-guard pattern (D31/D32) over separate data.
- **Blobs in a dedicated R2 bucket; metadata in SQLite** (D41). A document's bytes are **immutable / upload-once** — never replaced or overwritten; a changed file is a **new** document, and the old one can be deleted after confirmation (D50). No version history.
- **Permanent, id-based, household-only URL** (D42) — the permanent link is derived from the document's **immutable UUID** (`/d/{id}` → `/api/documents/{id}/raw`), **not** the slug path (which changes on rename/move, D32). Served by the backend, session-gated — **no public access, no share tokens** (D33 upheld).
- **Uploads through the backend** (D43) — multipart `POST /api/documents` streams to R2, sniffs MIME server-side, checksums (SHA-256), records metadata + audit in one transaction. Cap **50 MB**.
- **Preview** (D44) — PDFs/images/text preview natively; **Office → PDF** (spec said headless LibreOffice in-image; **shipped as a `home-gotenberg` sidecar** — see v5 §Relation), generated **once** async (immutable ⇒ derive-once, cache-forever) with `preview_status` transitions and a `/ws` push; failures degrade to download-only without losing the upload.
- **Search is filename + metadata** (FTS5 over title + filename + description, D46) — **not** file contents.
- **Two pin scopes** (D47, mirrors D35) — "pro všechny" (household, audited, editor+) and "jen pro mě" (personal, per-user, not audited).

### Dashboard (§4 FR-DOC11)

- New widget **`documents.pripnute`** (Připnuté dokumenty): household pins **∪** the caller's personal pins, de-duplicated (household precedence). A row **opens the document in a preview overlay on Nástěnka — the user never navigates away**; overlay actions reuse the documents endpoints with `meta.via="dashboard"`. No done gesture. No change to the dashboard-host contract — it's just another provider.

### Data model (§5)

- **New:** `document_folders` (self-ref parent, slug, lexorank — its own tree), `documents` (folder_id null=root, title/slug, original_filename, sniffed `content_type`, `byte_size`, `checksum`, `storage_key`, `preview_kind`/`preview_status`/`preview_key`/`thumbnail_key` — **no version columns**), `documents_fts` (FTS5 over title+filename+description), `document_pins` (partial unique indexes: one household per document, one personal per document per user).
- **Slug invariant:** unique across sibling document-folders+documents under one parent (`COALESCE` partial unique indexes + an app-level cross-check), exactly like Poznámky.
- **R2 object layout:** `documents/{id}/original` (write-once), `documents/{id}/preview.pdf`, `documents/{id}/thumb.webp` — **id-based keys** (renames/moves never touch R2).
- Per-module migrations extend the one Goose sequence: logging → platform → todo → events → notes → **documents**.

### Backup — the open architecture question, resolved (§8, D45)

- **Litestream cannot back up the R2 blob bucket** (it only replicates the SQLite WAL). So: document **metadata** rides Litestream (`home/`) as usual, and the **bucket** gets its own strategy — **object versioning** on the primary bucket (recovers deletes; bytes are immutable so there are no overwrites) **plus a scheduled server-side mirror to a second R2 bucket** (`HOME_DOCS_R2_BACKUP_BUCKET`). Periodic reconciliation flags orphaned objects / dangling rows. Fresh build: metadata from Litestream, bytes from R2. *(Answer: not Litestream — mirror + versioning.)*

### API (openapi 0.4.0 → 0.5.0, §6)

- **Added** under `/api/documents`: `tree`, `resolve?path=`, search `?q=`, **multipart upload**, document CRUD (PATCH = metadata only) + `move` + `pin`/unpin, the four content endpoints **`raw` / `download` / `preview` / `thumbnail`** (permanent, household-only, Range + checksum ETag + immutable cache), and `folders` CRUD + `move`. New `documents` tag and schemas (DocFolder(s), Document(s), DocumentDetail, DocumentsTree, DocResolveResult, DocumentUpload/Update/Move, DocumentPin, DocumentUrls, PripnuteDokumentyWidget/PinnedDocument). `documents.pripnute` added to `WidgetInstance.data` and the catalog `module` enum.
- **Unchanged:** auth, dashboard host, todo, events, logging, notes paths.

### Audit / real-time / security

- `document` and `document_folder` **join D6's key diff entities** — **metadata diffs only** (bytes are immutable, never diffed). Household pin/unpin audited; personal pins not. `/ws` now also pushes document/document-folder/household-pin changes and `document.preview_ready`/`_failed`.
- **Untrusted-content isolation** (D48): MIME sniffed server-side; originals default to attachment + `nosniff` + sandboxed CSP; inline previews only through safe viewers (PDF.js sandboxed iframe, `<img>`, escaped text); active types download-only.

### Frontend & nav (§7, D49)

- New **Dokumenty** destination (desktop tree+grid / mobile drill-down; upload + drag-drop; a standalone `DocumentView` reused by the dashboard overlay; pin UI; "Kopírovat odkaz" copies the permanent `/d/{id}` household link). **Regular members now have five destinations and also exceed the four-tab mobile ceiling** that only admins hit in v3, so the overflow/"více" pattern **generalizes to everyone**. Top item for the **v4 design addendum** (`HANDOFF-design.md`).

### Non-goals (v4)

No public/unauthenticated access or share tokens; no content replace/overwrite/version history (immutable, D41); no in-file full-text search or OCR (filename+metadata only, D46); no in-browser editing/annotation/e-sign; no third-party storage integrations; no per-document ACLs; Office preview limited to LibreOffice-convertible formats; >50 MB and streaming media out of scope — candidate future work.

### Relation to v3

v3 (Poznámky) is a spec addition on the live v2; v4 layers `documents` on top. Because the module is self-contained and additive, nothing in v1/v2/v3 changes to build it — the one new operational dependency is the R2 documents bucket(s) + headless LibreOffice in the runtime image.

---

## v3 — 2026-07-29 (spec) · Poznámky (notes) module

> OpenAPI **0.3.0 → 0.4.0**. Decisions **D30–D38** added; **D6** extended. Triggered by one product addition from Karel: a notes module.

### Headline

A **fifth module, Poznámky (`notes`)**, added on top of the live v2 as a self-contained package registered through the existing module registry — **no change** to Mode B auth, the dashboard-host contract, or the todo/events/logging modules. It's the first real exercise of "adding a module is adding a package that registers itself" (D25).

### What Poznámky is (§1, §4 FR-P1–P8)

- **Create / edit / view / delete Markdown notes.** The body is stored **once, as Markdown** (D30); the editor offers **WYSIWYG (default)** and a **raw-Markdown** toggle as two round-tripping views over that one source — no HTML or second copy is persisted.
- **Folders + subfolders**, a **single-parent tree** of arbitrary depth; a note lives at the **root or in exactly one folder** (D31). Lexorank sibling order.
- **Human-readable slug-path URLs** — every note and folder is addressable at `/poznamky/<folder>/…/<slug>`, slugs unique across sibling folders+notes so a path resolves to one item; canonical ops by stable id + a path→id resolver; **rename/move changes the URL with no redirects** (D32).
- **Sharing is in-app / household-only** (D33) — a link opens for logged-in members only; **no public access, share tokens, or public routes.**
- **Text + external links only** (D34) — no uploads/attachments, **no blob storage added.**
- **Two pin scopes** (D35) — **"pro všechny"** (household: shared, audited, editor+) and **"jen pro mě"** (personal: per-user view preference, any member incl. `reader`, not audited).
- **FTS5 search** over note title + body (D6-style, FR-P6).

### Dashboard (§4 FR-P8)

- New widget **`notes.pripnute`** (Připnuté poznámky): household pins **∪** the caller's personal pins, de-duplicated (household precedence). A row **opens the note in an overlay dialog on Nástěnka — the user never navigates away**; edits/unpin reuse the notes endpoints with `meta.via="dashboard"` (FR-D5). No change to the dashboard-host contract (FR-M2/D28) — it's just another provider.

### Data model (§5)

- **New:** `folders` (self-ref parent, slug, lexorank), `notes` (folder_id null=root, canonical `body_md`, slug), `notes_fts` (FTS5 over title+body), `note_pins` (scope household|personal, partial unique indexes: one household pin per note, one personal per note per user).
- **Slug invariant:** unique across sibling folders+notes under one parent (partial unique indexes + an app-level cross-check).
- Per-module migrations extend the one Goose sequence: logging → platform → todo → events → **notes**.

### API (openapi 0.3.0 → 0.4.0, §6)

- **Added** under `/api/notes`: `tree`, `resolve?path=`, search `?q=`, note CRUD + `move` + `pin`/unpin, and `folders` CRUD + `move`. New `notes` tag and schemas (Folder(s), Note(s), NotesTree, ResolveResult, PinState/PinRequest/NotePin, PripnutePoznamkyWidget/PinnedNote). `notes.pripnute` added to `WidgetInstance.data` and the catalog `module` enum.
- **Unchanged:** auth, dashboard host, todo, events, logging paths.

### Audit / real-time

- `note` and `folder` **join D6's key diff entities** (full-value diffs, truncate-with-expand). Household pin/unpin is audited; **personal pins are not** (a personal view preference). The audit log **is** the note's history — no separate versioning (D36). `/ws` now also pushes note/folder/household-pin changes.

### Frontend & nav (§7, D37)

- New **Poznámky** destination (desktop tree+pane / mobile drill-down; WYSIWYG↔Markdown editor; pin UI; "Kopírovat odkaz" household link). Regular members keep four thumb-reachable tabs; **admins now have five destinations and exceed the mobile ceiling**, so the app shell needs an **overflow / "více"** pattern for the admin-only Log. Top item for the **v3 design addendum** (`HANDOFF-design.md`).

### Non-goals (v3)

No public/unauthenticated sharing; no uploads/embedded media/blob storage; no slug redirects; no collaborative/simultaneous editing (last-write-wins, D38); no note tags/labels, no export (md/zip), no note-level ACLs — candidate future work.

### Relation to v2

v2 is **live** (deployed 2026-07-29). v3 is a **spec addition** layered on it; because the module is self-contained and additive, v2 continues to run unchanged until v3 is built.

---

## v2 — 2026-07-21 (spec) · self-hosted login, widget dashboard, modular architecture

> OpenAPI **0.2.0 → 0.3.0**. Decisions **D23–D29** added; **D2** and **D16** adjusted. Triggered by two product changes from Karel.

### Headline

Two structural shifts on top of the approved four-module v1:

1. **Auth: Mode A → Mode B.** Home now hosts its **own login UI** and owns its **own session**, verifying credentials against auth BE→BE instead of redirecting to `auth.tilcer.cz`.
2. **Nástěnka becomes a widget host.** Instead of two hardcoded lists, the dashboard is a per-user arrangement of **widgets** that modules provide; each user shows/hides, reorders, and resizes them. This forces the module boundaries to become real, so the whole codebase is restructured into a **compile-time modular monolith** with each module self-contained in `backend/` and `frontend/`.

### Auth (was Mode A, §3)

- **Home hosts login + logout only** (D23). The login form posts to home; home calls auth **`POST /internal/login`** as a service client, then creates its **own session** (cookie on `home.tilcer.cz`) and caches the user's identity + roles.
- **No JWT in the browser** — the earlier per-request `/introspect` verification (v1 D2) is replaced: home authorizes each request from its **own session**, and refreshes identity/roles periodically via auth **`POST /internal/token/mint`**. `/introspect` is no longer on home's hot path. *(D2 adjusted.)*
- **Password-only** in v1 (D23): no TOTP, no Google on home. If auth returns an MFA challenge, home degrades gracefully and points the user at auth-hosted login rather than building an MFA UI.
- **Users are admin-provisioned in auth** — no self-signup on home. **Password reset stays auth-hosted** (home links out). Google OAuth, if ever used, stays auth-hosted (redirect).
- **Home is now a Mode B consumer** of auth, so it needs its own **service client bound to site `home`** (as `fin` does) — used for `/internal/login` and `/internal/token/mint`, not just introspection.
- New session concerns: home owns a **session store** + cookie, **CSRF** (double-submit) on cookie-authenticated state-changing routes, and its **own revocation** (a disabled user keeps working until the next role refresh, ≤ the refresh interval).

### Dashboard → widget host (was FR-N1–N3, §4/§7)

- **Nástěnka is a widget host** (D24). It owns no feature data; it renders **widgets** contributed by modules and stores each user's **layout**.
- **Per-user layout is server-side** (D24): which widgets are visible, their order, and their size — persisted in home's DB and synced across devices. A user can **show/hide, reorder (drag), and resize** widgets (narrow/wide).
- **v1 widget catalog** (D27): **Právě dělám** (todo — cards in `kind=now` columns across boards), **Připomínky** (events — active reminders), **Tento měsíc** (events — look-ahead upcoming events). *(No admin log widget in v1.)*
- The old FR-N1 aggregation logic **moves into module-provided widget providers**; the host fans out to visible widgets in one request (D28). Mark-done still reuses the owning module's endpoints with `meta.via="dashboard"`, and the **2000 ms press-and-hold** (D22) is preserved inside the task/reminder widgets.
- **"Active items only" (v1 D16) relaxed:** widgets define their own scope, so *Tento měsíc* legitimately looks ahead. *(D16 adjusted.)*

### Architecture: compile-time modular monolith (D25, D28)

- **Strict module boundaries.** Each module is a **self-contained backend package** (its own routes, migrations, audit actions, and widget providers) and a **frontend folder** (its page + its widgets), wired through a **central registry**. One binary, one deploy — but modules don't import each other's internals.
- **Module code identifiers stay English** (D26, per the earlier D17): `logging`, `todo`, `events`, `dashboard`, `platform`/core. UI shows Czech (Úkoly, Okno, Nástěnka).
- **Per-module migrations** (D25): each module owns its migration files, run in one Goose sequence — replaces v1's single `0001_init`.
- **Cross-module data flows through the widget-provider contract only** (D28): the dashboard host calls a registered provider interface, never a module's tables. Modules communicate through defined interfaces, not shared DB access.

### Data model (§5)

- **New:** `sessions` (home's own session store — token hash, user, UA/IP, sliding expiry, revocation) and `user_dashboard_layout` (`user_id`, `widget_key`, `visible`, `position`, `size`).
- Events/todo/logging tables unchanged in shape; now created by **per-module migrations** rather than one init.

### API (openapi 0.2.0 → 0.3.0, §6)

- **Added:** `POST /api/auth/login`, `POST /api/auth/logout`, `GET /api/auth/session`.
- **Changed:** `GET /api/dashboard` now returns `{ layout, widgets[] }` (host fan-out), not two fixed lists. Added `GET /api/dashboard/catalog`, `PUT /api/dashboard/layout`, `GET /api/dashboard/widgets/{key}` (single-widget refresh).
- **Security schemes:** home **session cookie** + **CSRF header** on cookie-authenticated mutations; the browser no longer carries a bearer JWT.
- Todo/events/logging paths unchanged.

### Config (§9)

- **New/changed:** `HOME_AUTH_SERVICE_SECRET` now authenticates `/internal/login` + `/internal/token/mint` (not just introspect); `HOME_SESSION_TTL_DAYS` (own session sliding window, default 90); `HOME_ROLE_REFRESH_MINUTES` (how often home re-mints to refresh roles, default 15); `HOME_CSRF_*`. `AUTH_BASE_URL` now also the target of the "reset password" / MFA-fallback links.

### Design

- **New screens require a design addendum** (flagged in `HANDOFF-design.md` §v2): home-hosted **login/logout** screens, the **widget host** (grid, drag-reorder, resize, add/remove from a catalog), and the **empty/first-run** dashboard. The v1 prototype covered neither. Not blocking the backend build, but blocks the dashboard/login frontend.

### Migration from v1

v1 was a spec, never built — so there is **no data migration**. v2 supersedes v1 as the build target. Where v1 handoffs conflict with v2, **v2 wins**.

---

## v1 — 2026-07-21 (spec, superseded) · four modules, Mode A

Initial approved spec + design. Four modules (`logging`, `todo`, `events`, `dashboard`), Mode A auth (redirect to auth-hosted login), Nástěnka as a hardcoded two-list aggregator, single `0001_init`. Decisions D1–D22. Design prototype delivered and reviewed (five deviations; D22 press-and-hold adopted). Never implemented.
