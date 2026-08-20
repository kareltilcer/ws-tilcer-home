# home — notes

Decisions, interview answers, and research. Detailed spec in `PRD.md` + `openapi.yaml`; design brief in `HANDOFF-design.md`.

## Modules

| Code id | UI name | What it is |
|---|---|---|
| `logging` | Log | Audit spine + log browser (admin-only) |
| `todo` | Úkoly | Trello-style board |
| `events` | Okno do budoucnosti | All-day, optionally recurring future events with in-app reminders |
| `notes` | Poznámky | **v3.** Markdown notes (WYSIWYG-default + raw toggle) in a folder tree; slug-path URLs (household-only); two-scope pinning; a pinned-notes widget |
| `documents` | Dokumenty | **v4.** Files in a folder tree; bytes in a dedicated R2 bucket, metadata in SQLite; immutable upload-once; preview/download; permanent `/d/{id}` URL; a pinned-documents widget |
| `admin` | Administrace | **v5.** Admin-only. Web Push over one shared VAPID channel: broadcasts, audit-key trigger rules, scheduled summaries; + the installable reads-only-offline PWA |
| `finance` | Finance | **v6 (spec).** Monthly household income split into personal / operational (Kandy) / two savings accounts by a LOCKED formula, derived on read. A clone of the retiring `fin` service; a `finance.rozpocet` widget |
| `dashboard` | Nástěnka | Landing page — **per-user widget host** (v2); renders module-provided widgets, owns no feature data |
| `platform` | — | Core (v2): module registry, Mode B auth + session, db/goose, ws hub, config |

## Interview answers

**2026-07-19 (round 1 — original two modules)**

- **Logging architecture:** in-app **spine**, extractable. A Go package + its own SQLite tables inside the `home` DB; every module writes through it, in the **same transaction** as the change. Behind an `AuditSink` interface so it can become a standalone service later — only when a *second* service needs it.
- **Users:** **household** (multiple people). Shared boards; accountability via the audit log's actor rather than a per-card assignee.
- **Auth:** **Mode A** (auth-hosted login). Site key `home`, single_site accounts, JWTs verified via `/introspect`. *(v1 answer — superseded in v2 by D23: home hosts its own login, Mode B.)*
- **Log detail:** **hybrid** — every action → an event; key entities also record field-level diffs.
- **To-do model:** "folder" = **column**. Many columns, sortable + collapsible, feeding "Právě dělám", plus Hotovo/archive. Cards carry notes, links, checklists, labels.

**2026-07-19 (round 2 — added modules)**

- **Okno do budoucnosti:** all-day events, title/description/links, weekly/monthly/yearly recurrence, one optional reminder at 1d/2d/1w/2w/1m lead, listed by month.
- **Nástěnka:** landing page aggregating active event reminders + all cards in "Právě dělám" columns; detail dialog for both; mark done in place.

**2026-07-29 (round 3 — Poznámky / `notes`, v3)**

- **What it is:** create/edit/view/delete **Markdown** notes; edit in **Markdown and WYSIWYG (toggle), WYSIWYG default**; organise into **folders and subfolders**; each note/folder/subfolder has a unique URL to be shared; one **Nástěnka widget** listing pinned notes, a click opening the note in an **overlay dialog without leaving Nástěnka**.
- **"Unique URL to be shared" → in-app / household only** (Q1). The link opens for logged-in home members; **no public/unauthenticated access**. → D33.
- **Attachments → text + external links only** (Q2). No uploads, no embedded files, **no blob storage added**. → D34. ⚠ **SUPERSEDED by the built app (2026-08-17):** notes **do** upload images — bytes go through `platform/blobstore` at `note-images/{id}`, `body_md` keeps only `![](/api/notes/images/{id})`, with a mirror + reconciliation job in `notes/mirror.go`. External links still work.
- **Widget pinning → both scopes** (Q3, Karel's words): *"I would like both options. When pinning make option 'pro všechny' and 'jen pro mě'."* → D35 (household pin = shared+audited, editor+; personal pin = per-user view preference, any member incl. reader, not audited).
- **URL form → human-readable path/slug** (Q4), e.g. `/poznamky/recepty/gulas`. Accepted that rename/move changes the link; **no redirects** in v1. → D32.
- **Derived defaults (stated in PRD, open to change):** Markdown is the single stored form, WYSIWYG/raw are two views over it (D30); single-parent folder tree, arbitrary depth, a note at root or one folder (D31); `note`/`folder` join the audit diff set and the log **is** the note history — no separate versioning (D36); last-write-wins bodies (D38); the 5th nav destination pushes admins past the 4-tab mobile ceiling → overflow pattern (D37).

**2026-08-17 (round 4 — Finance / `finance`, v6; the `fin` service absorbed)**

- **What it is (Karel's words):** *"New module and v6 of home app will be Finance, it will be literally clone of Fin app (fin.tilcer.cz) that is part of this project, we want this app to become the part of home app, migrate all data and then retire Fin app… Finance module will be functionally the same as Fin app, it will just adopt Home app design and common architectural structure."*
- **Who sees it → all roles** (Q1). Finance is an ordinary module, not an admin one: reads for every member incl. `reader`, writes `editor`/`admin`. → D84. **Accepted consequence:** a `reader` sees both household incomes (OQ-V6-4).
- **People model → keep it literally identical** (Q2). `income_kaja` / `income_andy` carry over verbatim; no person A/B rename, no generalisation to N people. Only the table is namespaced (`finance_months`). → D83.
- **Data migration → one-off Goose seed** (Q3), not an import endpoint and not manual re-entry. `fin`'s ids and timestamps are preserved. → D91. *(Derived constraint found while specifying it: the seed must be its OWN migration source, excluded from `testsupport`, or every module test runs against thirty months of real household finances.)*
- **Deliverable → the full v6 spec set** (Q4): PRD §V6, `openapi.yaml` 0.8.0, `HANDOFF-8-finance.md`, changelog + registry.
- **Derived defaults (stated in the PRD, open to change):** the split formula and its rounding order are **locked and ported verbatim**, split derived on read (D82); Finance joins the audit spine, which `fin` never had (D86); delete is hard as in fin, with a full-row audit diff as the compensating control (D87); "více" overflow rather than a fifth thumb tab (D84); one widget with a **"Zadat ⟨měsíc⟩"** state because the real failure mode is a month nobody entered (D88); four household metrics + one list, `finance.missing_months` composing with v5 conditions into a self-silencing monthly reminder (D89/D90); wire format is home's — snake_case, `PATCH`, `/api/finance/months` (D92); retirement gated on a row-for-row verification **including the recomputed split**, with no redirect behind it (D96/D97).
- **Design addendum (2026-08-17, same session):** drafted into `HANDOFF-design.md` §v6 — briefs against `fin`'s working UI (carry the three-stage flow viz, re-dress it in home's tokens) and raises one **computed** blocker: home's `--c1`…`--c5` palette **fails** the CVD checks for four adjacent buckets (`c2`↔`c3` ΔE 4.4 protan / 12.3 normal; `c1`↔`c4` ΔE 0.8 protan in every ordering and every 4-of-5 subset). **Path A** = order `c1,c2,c4,c3` + mandatory secondary encoding (passes adjacent-pairs both themes); **Path B** = re-step the tokens (validated candidate in the addendum; repaints the Log stats bars too). **Karel's call — not yet made.**
- **Round 4b (2026-08-17, same session):** all four open questions answered — **hard delete · overflow · no redirect · reader access accepted**. D87 and D96 rewritten accordingly; the v6 spec now has **no open questions left**.
- **Found while specifying, worth its own decision:** `services/fin/` in the project folder is **empty** — `fin`'s PRD, OpenAPI and handoff live only in the repo about to be archived. Recover them before archiving. → D98.

## Research that shaped the design (2026-07-19)

Recurring events are a well-known source of data-model mistakes; the design follows the established guidance rather than inventing:

- **Store the rule, not the occurrences.** A master record holds the RRULE; occurrences are expanded on read. Pre-computing and storing occurrences is the classic wrong turn. → D13, and *no occurrences table at all*.
- **Expansion must be bounded.** Open-ended series are infinite; real systems cap expansion (Nextcloud, for instance, caps at 3500 occurrences). → `HOME_RRULE_MAX_OCCURRENCES` + a max window span.
- **Reminders should track the next due occurrence**, advancing when one fires — not be attached to every occurrence. → D11 + the "earliest uncompleted occurrence" rule in FR-N1, which makes pile-up structurally impossible.
- **Use a real IANA timezone**, not a fixed offset, or series drift an hour at DST. All-day events (D18) dodge the clock-time hazard entirely, but "today" and lead-time arithmetic still must be evaluated in `Europe/Prague`.
- **The short-month trap.** RFC 5545 `FREQ=MONTHLY` anchored on the 31st **skips** months with no 31st. For "zaplatit 31." that's a bug, so we deliberately deviate and clamp to the month's last day → **D19**, explicitly flagged as non-standard and test-covered.
- **Library:** `teambition/rrule-go` (complete RFC 5545 implementation, port of python-dateutil, supports EXDATE/RDATE sets). Caveat: little recent release activity — noted as a risk in D13, mitigated by the fact that our supported subset is small enough to hand-roll.

## Key design points

- **Two log planes:** operational request logs → stdout (Coolify); domain audit events → the spine's DB tables → log browser. Request id links them.
- **Atomicity guarantees completeness:** if the audit write fails the mutation rolls back, and vice versa. This is why the spine is in-process + same-DB.
- **Cross-module attribution:** completing a to-do from Nástěnka logs under `todo` (not `dashboard`) with `meta.via="dashboard"`, so an entity's timeline stays complete regardless of which screen touched it.
- **No scheduler anywhere.** Reminders are computed on read, which is what lets D9 (no automated jobs) survive the addition of a reminders feature.
- **The only per-occurrence row in the system** is `event_reminder_completions` — written when someone ticks a reminder off, never pre-created.
- **Append-only audit;** full untruncated diff values; FTS5 for free-text search.
- **Mobile UX:** Nástěnka completion is a deliberate 2000 ms press-and-hold (D22, with an immediate keyboard path); board is a vertical accordion with `now` columns pinned; collapse state is client-side.
- **Ordering:** lexorank-style string position keys.
- **Real-time:** authenticated `/ws` pushes board, event, and completion changes; refetch-on-focus is the reconnect fallback.

## Resolved decisions (see PRD §10 for full text)

**Original two modules (D1–D10)**

- **D1** multiple boards + switcher · **D2** JWT via `/introspect` + cache · **D3** collapse client-side · **D4** lexorank ordering · **D5** roles `admin`/`editor`/`reader`, logs admin-only · **D6** full diff values + FTS5 (key entities now include `event`) · **D7** `now`/`done` is a free-form non-unique hint (and drives what Nástěnka shows) · **D8** soft delete + `?hard=true` · **D9** no automated jobs · **D10** websockets (extended to event/completion changes)

**Added modules (D11–D19)**

- **D11** reminders **computed on read**, in-app only — no scheduler, no email/push
- **D12** event reminders are a **separate entity**, never to-do cards
- **D13** recurrence stored as an **RFC 5545 RRULE subset**, expanded on read (`teambition/rrule-go`)
- **D14** **series-only editing** — no per-occurrence exceptions; UI warns before saving
- **D15** Nástěnka mark-done → move card to the board's **first `kind=done` column**, else archive
- **D16** Nástěnka is the **landing page**, **active items only** (no look-ahead)
- **D17** **English code identifiers, Czech UI**
- **D18** events are **date-only (all-day)**
- **D19** **short-month clamping** (31 Jan monthly → 28/29 Feb) — deliberate deviation from RFC default

**Design inputs (D20–D22)**

- **D20** UI is **Czech-only** (plural forms 1 / 2–4 / 5+ everywhere) · **D21** **dark theme default**, light secondary
- **D22** Nástěnka done = **2000 ms press-and-hold** (supersedes one-tap; decided 2026-07-21 from the design review). Guards against stray taps on the landing route; **must** ship with an immediate keyboard/assistive path and a single-activation "✓ Hotovo" in the detail dialog.

**v2 — self-hosted login, widget dashboard, modular architecture (D23–D29; D2/D16 adjusted). See CHANGELOG.md.**

- **D23** Auth is **Mode B** — home hosts login + logout only, **password-only**; calls auth `/internal/login` + `/internal/token/mint` as a service client; no self-signup (admin-provisioned in auth), reset stays auth-hosted, MFA/Google stay auth-hosted (graceful redirect). Accepts plaintext-password-in-transit + own revocation.
- **D24** Nástěnka is a **per-user widget host** — server-side layout (show/hide, drag-reorder, **narrow/wide** resize), synced across devices; responsive grid (1 col mobile / 2 col desktop).
- **D25** **Compile-time modular monolith** — each module self-contained (routes, **own migrations**, audit, widget providers) in backend/ + frontend/, wired via a central registry; one binary, no runtime plugins.
- **D26** Module code ids stay **English** (`logging`/`todo`/`events`/`dashboard`/`platform`); UI Czech. (Extends D17.)
- **D27** v1 widget catalog: **Právě dělám** (todo), **Připomínky** + **Tento měsíc** (events). No admin log widget; no user-authored widgets.
- **D28** Dashboard host owns **no feature data** — cross-module data only via the **widget-provider contract**; enforced by an import/arch test.
- **D29** Home owns a **session + CSRF** — hashed session token, sliding TTL, `HttpOnly; Secure; SameSite=Lax` host-only cookie, CSRF double-submit + Origin allowlist, login rate-limited. **No token in the browser.**
- **D2 (adjusted)** no browser JWT / no per-request introspect; authorize from home session, refresh roles via `/internal/token/mint`. · **D16 (adjusted)** widgets set their own scope; *Tento měsíc* looks ahead.

**v3 — Poznámky (`notes`) module (D30–D38; D6 extended). See CHANGELOG.md.**

- **D30** notes stored as **canonical Markdown**; WYSIWYG (default) + raw Markdown are two views over the one source (round-trip); no HTML/second copy persisted
- **D31** **single-parent folder tree**, arbitrary depth; a note lives at root or in exactly one folder (no multi-filing); lexorank sibling order
- **D32** **human-readable slug-path URLs** (`/poznamky/<folder>/…/<slug>`); slug unique across sibling folders+notes; canonical ops by stable id + a path→id resolver; **rename/move changes the URL, no redirects** (old path 404s) — accepted tradeoff
- **D33** sharing is **in-app / household-only** — logged-in members only, **no public access / share tokens / public routes** (stays inside Mode B)
- **D34** ~~**text + external links only** — no uploads/embedded attachments, **no blob storage**~~ — **SUPERSEDED in the built app (recorded 2026-08-16, confirmed from code 2026-08-17):** inline **image upload** ships (v4.1 branch `poznamky-image-upload`), stored via `platform/blobstore` under `note-images/{id}`; the note body still stores only a Markdown reference, and external image URLs still work. See the *Built-app reconciliation* block in `PRD.md`.
- **D35** **two pin scopes** — **household** ("pro všechny": shared, audited, editor+, one per note) and **personal** ("jen pro mě": per-user view preference, any member incl. reader, not audited); `notes.pripnute` widget = household ∪ caller's personal, de-duplicated (household precedence)
- **D36** `note` + `folder` join D6's **key diff entities**; the audit log **is** the note history — no separate versioning in v3
- **D37** nav grows to a **5th destination** (Poznámky); regular members keep 4 tabs, **admins exceed the mobile ceiling** → overflow/"více" for admin-only Log
- **D38** note bodies are **last-write-wins**; a `/ws` "changed elsewhere" notice softens concurrent overwrite; no OT/CRDT
- **D6 (extended)** full diffs + FTS5 now also cover `note`/`folder`

### v6 — Finance (D81–D98, spec 2026-08-17)

*(D39–D80 are not restated here — see `PRD.md` §10, the v5 section and §V5-12.)*

- **D81** v6 is a **clone, not a port** — `fin`'s session store, auth client, JWT plumbing, English UI and import endpoint are dropped, not migrated
- **D82** the **split formula is LOCKED and carried over verbatim**, split **derived on read, never stored**
- **D83** `fin`'s **column vocabulary is literal** (`income_kaja`/`income_andy`/four `rate_*`); only the table is namespaced (`finance_months`)
- **D84** ordinary **all-roles** module (reads incl. `reader`, writes editor/admin, hard delete admin), in the **"více" overflow** — not admin-gated
- **D85** Czech UI vocabulary fixed in the spec; the joint account keeps the household's nickname **Kandy**
- **D86** joins the **audit spine** — `month.create|update|delete`, entity `finance_month` in the field-diff set (a capability `fin` never had)
- **D87** *(resolved 2026-08-17)* **delete is HARD, as in `fin`** — no `deleted_at`, no `?hard=true`, no admin tier, plain `UNIQUE(month)`. Compensating control = the audit spine: `month.delete` writes a **full-row diff** so the deleted numbers stay readable in the Log; the UI says the delete is permanent
- **D88** one widget **`finance.rozpocet`** (narrow, all roles) with two states — the month's split, or **"Zadat ⟨měsíc⟩"** when it is missing
- **D89** **four household metrics**: `total_income_current`, `savings_current`, `missing_months`, `months_recorded`
- **D90** one list **`finance.missing_months`**, mirroring the metric of the same key exactly
- **D91** historic data = a **one-off Goose seed in its OWN migration source** (`finance/seed`, block 09900), production-only and **excluded from `testsupport`**; `INSERT OR IGNORE`, ids/timestamps preserved
- **D92** wire format is **home's, not `fin`'s** — snake_case, `PATCH` not `PUT`, `/api/finance/months`, shared error/paging components
- **D93** rate defaults stay a **frontend** behaviour (latest month, else 20/60/10/10)
- **D94** live sync like every module — `finance.changed`, toast "Finance byly mezitím upraveny"
- **D95** no special PWA handling — reads cached, writes disabled offline
- **D96** *(resolved 2026-08-17)* `fin` retired in a **fixed order, with NO redirect**: tell both users → verify → stop backend+frontend and release the subdomain → archive repo → deprovision auth site. The redirect would also have been the post-cutover fallback, so the verification is now the only gate
- **D97** retirement **gated on a row-for-row verification including the recomputed split** — inputs alone would not catch a mis-ported formula
- **D98** `fin`'s PRD/OpenAPI/handoff **recovered into `services/fin/`** (currently empty) before the repo is archived

**Resolved 2026-08-17 (Karel):** OQ-V6-1 → **hard delete, as in fin** (D87) · OQ-V6-2 → **"více" overflow**, four thumb tabs untouched (D84) · OQ-V6-3 → **no redirect**, fin.tilcer.cz switched off outright (D96) · OQ-V6-4 → **accepted**, a `reader` sees both incomes. **None open.**

## Pre-implementation setup (after approval)

- **Design pass first:** `HANDOFF-design.md` briefs Claude Design (design system + hi-fi prototype, four modules, both breakpoints, Tailwind + shadcn/ui). The Claude Code handoff (`HANDOFF.md`) is written against the PRD *and* the approved design.
- Register `home` site in auth with roles **`admin`/`editor`/`reader`**; provision a `home` **service client** (bound to site `home`) → `HOME_AUTH_SERVICE_SECRET`. **v2: this client now authenticates `/internal/login` + `/internal/token/mint` (Mode B), not just introspect.**
- **v2: create the household member accounts in auth** (no self-signup on home).
- Coolify env per PRD §9 (v2 adds `HOME_SESSION_TTL_DAYS`, `HOME_ROLE_REFRESH_MINUTES`; plus `HOME_TIMEZONE`, `HOME_DASHBOARD_LOOKBACK_DAYS`, `HOME_RRULE_MAX_OCCURRENCES`); Litestream → R2 prefix `home/`.
- Repo `ws-tilcer-home` (not yet created).
- **v2 design addendum** needed for login screens + widget host before the dashboard/login frontend (backend can proceed).
- **v3 design addendum** needed for Poznámky: the notes browser (desktop tree+pane / mobile drill-down), the WYSIWYG↔Markdown editor (pick a Markdown-backed rich editor), pin UI (two scopes), "Kopírovat odkaz" (household link), the `notes.pripnute` widget + overlay dialog, and the **5th-destination nav / admin overflow** (D37). Backend can proceed; the notes frontend waits on it.
