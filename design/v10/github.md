repo: kareltilcer/ws-tilcer-home
branch: main
path: design, frontend/src, handoff

## Last sync
date: 2026-08-26T12:20:00Z
<!-- commit sha omitted: only a tree hash was resolved (2a0051f98741), not a commit -->

### Updated in this project
- Implemented the **v10 addendum** (`handoff/v10/HANDOFF-design.md` §v10 + `HANDOFF-12-chat.md` + `V10-chat-brief.md`, D216–D262): **Chat** — the eleventh module and the first one the household does not read in full. Designed at 1440 px and 375 px in both themes.
- **Membership is the third axis of access** after v1–v8's "everyone" and v9's "one person". The design consequence is a person looking at a room they are only partly inside, so the **history floor** is drawn in three places and nowhere else: one quiet, permanent line at the top of a thread, the empty-quote shape "Zpráva mimo vaši historii" (D226), and `effective_from` per member in the members panel — plus the removal dialog's sentence about the permanent gap.
- **Všichni never shows the floor line** (D258): the household room gives every member the whole history, because nobody decided to add them. One predicate, two values, no branch — and a mock that shows the line there has misread the decision.
- **The first demotion in the app's history** (D260): Chat takes a bottom tab and **Okno do budoucnosti moves into "Více"**, first in the sheet with its full name. Unread badge is accent (unread is not a warning), mono tabular so 3 and 40 don't shift the bar.
- **No new colour.** `--bub-mine` / `--bub-theirs` are aliases over accent-soft / s2, canvas-sRGB measured at **1,55:1 dark / 1,16:1 light** — deliberately below 3:1, so alignment and the author label carry the distinction. Found while measuring: the author label in a foreign bubble must be `--muted`, since `--subtle` on s2 falls to **4,04:1** in light. `--att-removed` is `--unmeasured` promoted to a second meaning of the same idea; both warning registers reuse v9's `--attention`.
- **Three attachment states, two of them text**: live (image with reserved height from the API's w/h, video inline with a designed unplayable-`.mov` fallback, PDF in the browser), `removed` as a settled absence keeping filename and size (D243), `moved` as a plain fact naming where the file lives now (D246).
- **Úklid úložiště chatu** is a working screen, sorted by size, single-page and honest about it. *Ponechat* is not a button (D242); leaving while still over raises a confirm, never a block (D244).
- **The move is a publish** (D245) — the fixed sentence stands before the confirm, and the picker offers shared folders only, with the private v9 roots absent-and-explained rather than greyed out.
- **7-day koš** (D253) with typed confirmation, and **Smazat natrvalo** because trashed bytes keep counting toward both thresholds (D254) — the copy carries the relationship: deleting frees the space in seven days, purging frees it now.
- **Reader is the first writer in Home** (D222) and still cannot clean up: `/chat/uklid` is 403 with the reason named, and the warning does not offer them the link (D241).
- **Chat is excluded from the PWA persister** — its own offline state, not a stale thread.
- **D263 (added after review) — the thresholds are now editable**: Správa úložiště gains a third sub-tab **Limity** with two MB fields (`chat.total`, `chat.conversation`), autosave on blur with the state always beside the field, an invalid value refused back to the last valid one, and lowering a limit below current usage saved with a sentence naming what it just switched on. Every change goes to the Log as `chat.threshold.update` (who, when, from what to what). v9's `HOME_STORAGE_WARN_TOTAL_MB` stays an env var and stays out of this page. The chat block only displays the two numbers and points at the tab.
- **Administrace → Úložiště** gains a chat block: total against `chat.total`, per-conversation table with member counts, over-limit flags and koš state, **Nezálohováno** as a normal row, and one line of copy explaining why nothing in the table is clickable (D240, D255). Chat also enters the per-module blob attribution under `chat/` with kind `shared` — the wrong word, kept on purpose (D235).
- Systém/Stavy docs: **v0.10** label, a "Nové v v10" block, a v10 token panel (four aliases, no new colour), eleven new hard problems (67–77), seven Czech-copy rules and nineteen v10 state-matrix rows.
- ⚠ Recorded while implementing: v9's alias tokens are declared with `var()` in `:root` and therefore **do not re-resolve under `.light`** — v10's aliases are written out in both blocks. Worth fixing for v9's aliases in the build.

## Screen map
| Design (Home.dc.html) | Repo source |
|---|---|
| Design tokens / `:root`, **`--bub-mine` · `--bub-theirs` · `--att-removed`** | frontend/src/theme/globals.css (needs the four v10 aliases in BOTH theme blocks) |
| APLIKACE · nav shell — **Chat tab, Okno demoted to "Více"** | frontend/src/app/AppShell.tsx, app/routes.ts (D260) |
| APLIKACE · Chat — seznam, vlákno, podlaha, členové, koš | to be built: `frontend/src/routes/chat/` (ChatPage, ThreadView, MembersPanel, ConversationList) |
| APLIKACE · Chat — skladatel a přílohy | to be built: ChatComposer + UploadQueue pattern lifted from routes/dokumenty/UploadQueue.tsx |
| APLIKACE · Chat — /chat/uklid | to be built: routes/chat/UklidPage.tsx; gate member ∧ (editor|admin) |
| APLIKACE · Administrace → Úložiště, blok Chat | frontend/src/modules/admin/AdministracePage.tsx (storage tab) + `GET /api/admin/storage` gains GroupSource rows (openapi 0.12.0) |
| APLIKACE · Administrace → Limity (D263) | to be built: admin storage-limits sub-tab + `GET`/`PUT /api/admin/storage/thresholds` over `storage_thresholds` (D236); Log label `chat.threshold.update` |
| APLIKACE · Nástěnka | routes/nastenka/NastenkaPage.tsx, platform/widgets/registry.tsx — **must not be touched** (D252) |
| APLIKACE · Poznámky · Dokumenty · Úkoly · Okno · Finance · Zahrada · Elektřina | unchanged in v10 |
| Data model / copy | api/types.ts, api/keys.ts (chat keys carry conversation id), i18n/cs.ts (v10 block), i18n/plural.ts (zpráva · konverzace · člen · soubor · den) |
| Log — struktura bez zpráv | routes/log/LogPage.tsx — eleven new chat.* labels; **no message events** (D231) |

## Sync history
- 2026-08-23 — v9 (Soukromé položky a Úložiště): first version with no new module; two roots per module, publish as the only irreversible action, foreign private item = ordinary 404, Úložiště with replica + backup bucket outside the breakdown (D214), Soukromé položky as a maintenance tool, Administrace grown to six tabs.
- 2026-08-20 — v8 (Elektřina): tenth module, four sub-routes, `compute.go` mirrored as a pure `elCompute()`, headroom as the day-one headline, blocked ≠ error, VT/NT beyond colour, exact vs. approximate as a token rule; **no Nástěnka widget anywhere** (D147).
- 2026-08-19 — v7 (Zahrada): ninth module, eight sub-routes behind one „Více" entry, plan checks C1–C11, crop library with the {kotva, od, do} window, print artefacts, `garden.prace` widget.
- 2026-08-17 — v6 (Finance): fin.tilcer.cz cloned as the eighth module; four-bucket palette Path A/B.
- 2026-08-16 — v5 (Administrace, Web Push, PWA).
- 2026-08-12 — v4 (Dokumenty). 2026-07-30 — v3 (Poznámky). 2026-07-29 — v2 a11y token fixes.

## Notes
- `Home.dc.html` is the single evolving design doc (v1 → v10); `NoteView.dc.html`, `DocumentView.dc.html`, `CardTile.dc.html` are the reusable pieces. Versioned snapshots live in the repo under `design/v1 … design/v5`; a `design/v10` snapshot should be cut from this project when v10 is approved.
- **Design-side flag for the build:** the warning threshold is compared against the **modules' R2 total only** — the Litestream replica and the backup bucket are derived copies and stay out of it, consistently with D214 keeping them out of the per-module sums. If the API compares the whole bucket instead, the default 1024 MB fires permanently and the register stops meaning anything.
- Out of design scope for v9 (build concerns, stated as such in the addendum): the 23-row leak table, the redaction seam and its read paths, the four slug indexes + `Store.SiblingSlugTaken`, the `platform/storage` catalog, the `sqlite_master` completeness test and its FTS5 allow-list, the `dbstat` boot probe, and the HTTP cache-header change for private streams.
- Deliberately NOT designed in v9 (per the Do-NOT list): per-person sharing or share links, an unpublish control, a permission-denied screen for a foreign private item, any encryption affordance, a storage widget/metric/list/push/scheduled summary, storage history or forecasts, quota UI, a cleanup wizard or bucket browser, titles/filenames/thumbnails anywhere in Administrace, privacy in any other module, a new nav destination, and offline writes.
- **Open decision for Karel:** still Path A vs Path B for the four-bucket palette (v6); v10 changes nothing there.
- Out of design scope for v10 (build concerns, stated as such in the addendum): the scoped hub (`platform/ws` `PublishTo`), the five-step custody transfer and its crash ordering, the `chat_deleted_keys` drain, the FTS5 floor pushdown, the `notification_deliveries` table rebuild, and the two out-of-order goose migrations.
- Deliberately NOT designed in v10 (per the Do-NOT list): system messages announcing joins and leaves (D221), an edit history, a preview pipeline or transcoding, a Nástěnka widget / metric / list for chat (D252), a permission screen for a non-member (404 only), a staged-actions tray on the clean-up page, storage forecasts or quota UI, and any way into a conversation from Administrace.
- **Design-side flag for v10:** the per-conversation threshold is compared against the conversation's own `chat/` bytes **including a trashed conversation's** until it is really purged (D254). If the API excludes trashed bytes, the storage page stops summing and *Smazat natrvalo* loses its reason to exist.
- Deliberately not folded in: the Mode B **Login** screen and the **redirect / signed-out** shell (LoginScreen.tsx, RedirectingShell.tsx).
