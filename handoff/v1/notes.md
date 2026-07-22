# home — notes

Decisions, interview answers, and research. Detailed spec in `PRD.md` + `openapi.yaml`; design brief in `HANDOFF-design.md`.

## Modules (v1)

| Code id | UI name | What it is |
|---|---|---|
| `logging` | Log | Audit spine + log browser (admin-only) |
| `todo` | Úkoly | Trello-style board |
| `events` | Okno do budoucnosti | All-day, optionally recurring future events with in-app reminders |
| `dashboard` | Nástěnka | Landing page: active reminders + cards in "Právě dělám" columns |

## Interview answers

**2026-07-19 (round 1 — original two modules)**

- **Logging architecture:** in-app **spine**, extractable. A Go package + its own SQLite tables inside the `home` DB; every module writes through it, in the **same transaction** as the change. Behind an `AuditSink` interface so it can become a standalone service later — only when a *second* service needs it.
- **Users:** **household** (multiple people). Shared boards; accountability via the audit log's actor rather than a per-card assignee.
- **Auth:** **Mode A** (auth-hosted login). Site key `home`, single_site accounts, JWTs verified via `/introspect`.
- **Log detail:** **hybrid** — every action → an event; key entities also record field-level diffs.
- **To-do model:** "folder" = **column**. Many columns, sortable + collapsible, feeding "Právě dělám", plus Hotovo/archive. Cards carry notes, links, checklists, labels.

**2026-07-19 (round 2 — added modules)**

- **Okno do budoucnosti:** all-day events, title/description/links, weekly/monthly/yearly recurrence, one optional reminder at 1d/2d/1w/2w/1m lead, listed by month.
- **Nástěnka:** landing page aggregating active event reminders + all cards in "Právě dělám" columns; detail dialog for both; mark done in place.

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

## Pre-implementation setup (after approval)

- **Design pass first:** `HANDOFF-design.md` briefs Claude Design (design system + hi-fi prototype, four modules, both breakpoints, Tailwind + shadcn/ui). The Claude Code handoff (`HANDOFF.md`) is written against the PRD *and* the approved design.
- Register `home` site in auth with roles **`admin`/`editor`/`reader`** and provision a `home` **service client** (bound to site `home`) → `HOME_AUTH_SERVICE_SECRET`.
- Coolify env per PRD §9 (note the new `HOME_TIMEZONE`, `HOME_DASHBOARD_LOOKBACK_DAYS`, `HOME_RRULE_MAX_OCCURRENCES`); Litestream → R2 prefix `home/`.
- New repo (TBD).
