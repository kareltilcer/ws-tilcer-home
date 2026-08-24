repo: kareltilcer/ws-tilcer-home
branch: main
path: design, frontend/src, handoff

## Last sync
date: 2026-08-23T20:58:00Z
<!-- commit sha omitted: only a tree hash was resolved (9b74526c6073), not a commit -->

### Updated in this project
- Implemented the **v9 addendum** (`handoff/v9/HANDOFF-design.md` §v9 + `HANDOFF-11-privacy-storage.md` + `V9-privacy-storage-brief.md`, D176–D215): **Soukromé položky a Úložiště** — the first version that adds no module and instead changes `notes`, `documents` and `admin`. Designed at 1440 px and 375 px in both themes.
- **The root switcher is the whole privacy UX.** Two roots per module (Poznámky / Soukromé poznámky · Dokumenty / Soukromé dokumenty) held by the *shape* of the page, not a warning: nadpis + podnadpis, pill switcher above the tree, tinted tree with a 3 px left rail, root segment in the breadcrumb, scope named in the search placeholder, destination named in the upload panel. Five glanceable carriers, none of them a dismissible message.
- **Lock language defined once** — `--vis-private` as an *alias* over a new platform `--info` pair, always accompanied by the word („Soukromé", „Soukromé — jen ty"), never borrowed for a disabled control, an admin gate or a closed period.
- **Publikovat do sdílených** lives in the item's own menu (the pin popover's new *Viditelnost* group), owner-only, with the irreversibility in a sentence rather than in red; the folder variant states how many items become visible, and the notes slug-collision outcome (`recepty-2`) is explained without alarm. No undo toast — there is nothing to undo (D182).
- **A foreign private item is the ordinary nenalezeno screen** (D180), drawn exactly once with the path in mono; no padlock screen exists anywhere in v9. Reachable in the mock from the header's „Odkaz (v9)" switch.
- **Scoped search** (D184): scope in the placeholder, a mono scope line above results, and an empty state that names the tree it searched.
- **Pins**: „pro všechny" on a private item is *unavailable and explained*, not hidden; widget rows carry the lock mark plus the word, and a just-published row shows a „✓ teď sdílené" moment before the lock disappears (D183).
- **Úložiště** — totals (database exact from page_count × page_size + WAL; R2 for modules only), per-module tables with row counts, objects split shared / per member / **Nezařazené** with its explainer, and the **Litestream replika + zálohovací bucket in a separate block outside the breakdown** (D214) so the page's arithmetic reads as correct. Four switchable states: měřeno · bez `dbstat` · R2 nedostupné · nad prahem.
- **Nezměřeno is never 0 B**: `--el-approx` promoted to a platform `--unmeasured`, rendered in proportional italic where a mono figure would sit. The threshold register uses a promoted `--attention` (v8's nedoplatek token), carries the word „Nad prahem", and states that nothing is blocked.
- **Soukromé položky** — the purge screen as a maintenance tool: id, owner, module + kind, size, dates; no titles, filenames, content types, previews, downloads or search; sorting only by size and recency; purge confirmed by **typing the full identifier**; a visible note that opening the list writes `admin.private_items.view`. The **empty state is a designed screen** (D215).
- **Administrace grows to six tabs** through v7's tab-strip pattern (scroll-snap pills matching the shipped `AdministracePage.tsx` styling), no horizontal overflow at 375 px. No new nav entry, no widget, no metric, no push (D202).
- Systém/Stavy docs: **v0.9** label, a „Nové v v9" block, a v9 token panel (three aliases, no new colour) with **canvas-sRGB-measured** contrasts — `--vis-private` 8,48:1 dark / 6,83:1 light on `--s1`; text on `--vis-private-soft` 11,56 / 14,50; `--unmeasured` 5,30 / 4,97 on `--s1`; `--attention` on `--attention-soft` 6,47 / 5,35 — the byte-alignment rule (third magnitude family after Kč and kWh), ten new hard problems (57–66), five Czech-copy rules and fifteen v9 state-matrix rows.
- **Drift folded in from the shipped app:** Administrace's tab row is bordered pills with `accent-soft` active (not the segmented control the doc had); `--el-approx` carries the shipped AA-corrected values (0.635 dark / 0.545 light, was 0.560/0.640) and `--el-over` the shipped 0.505 light (was 0.545) — both now aliases of the new platform tokens.

## Screen map
| Design (Home.dc.html) | Repo source |
|---|---|
| Design tokens / `:root`, `--fin-*`, `--el-*`, **`--info` · `--unmeasured` · `--attention` · `--vis-private`** | frontend/src/theme/globals.css (needs the four v9 tokens + three alias repoints) |
| APLIKACE · nav shell („Více", Nastavení, Administrace) | frontend/src/app/AppShell.tsx, app/routes.ts — **unchanged in v9** (D202) |
| APLIKACE · Nástěnka (widget host) | frontend/src/routes/nastenka/NastenkaPage.tsx, platform/widgets/registry.tsx — **must not be touched** (D202) |
| APLIKACE · Poznámky — dva kořeny, přepínač, publikování | frontend/src/routes/poznamky/PoznamkyPage.tsx (+ `?scope=` in every query key), NoteView.tsx, NotesDialogs.tsx |
| APLIKACE · Dokumenty — dva kořeny, přepínač, publikování | frontend/src/routes/dokumenty/DokumentyPage.tsx, DocumentView.tsx, DocumentsDialogs.tsx, UploadQueue.tsx |
| APLIKACE · nenalezeno pro cizí soukromou položku | routes/dokumenty/DocumentPermalinkPage.tsx + both browsers' 404 path (never 403) |
| APLIKACE · Administrace — šest záložek | frontend/src/modules/admin/AdministracePage.tsx (`type Tab` gains `storage`, `private`) |
| APLIKACE · Administrace → Úložiště | to be built: `platform/storage` catalog + `GET /api/admin/storage` (openapi 0.11.0) |
| APLIKACE · Administrace → Soukromé položky | to be built: `GET /api/admin/storage/private-items`; delete calls the owning module's `?hard=true` route |
| APLIKACE · Log — redakce soukromých záznamů | frontend/src/routes/log/LogPage.tsx — **build concern, not design** (leak table rows 12–14) |
| APLIKACE · Úkoly · Okno · Finance · Zahrada · Elektřina | unchanged — no privacy anywhere else (brief §9) |
| Data model / copy | frontend/src/api/types.ts, api/keys.ts (scope in notes/documents keys), i18n/cs.ts (v9 block), i18n/plural.ts (položka · objekt · tabulka · soubor) |

## Sync history
- 2026-08-20 — v8 (Elektřina): tenth module, four sub-routes, `compute.go` mirrored as a pure `elCompute()`, headroom as the day-one headline, blocked ≠ error, VT/NT beyond colour, exact vs. approximate as a token rule; **no Nástěnka widget anywhere** (D147).
- 2026-08-19 — v7 (Zahrada): ninth module, eight sub-routes behind one „Více" entry, plan checks C1–C11, crop library with the {kotva, od, do} window, print artefacts, `garden.prace` widget.
- 2026-08-17 — v6 (Finance): fin.tilcer.cz cloned as the eighth module; four-bucket palette Path A/B.
- 2026-08-16 — v5 (Administrace, Web Push, PWA).
- 2026-08-12 — v4 (Dokumenty). 2026-07-30 — v3 (Poznámky). 2026-07-29 — v2 a11y token fixes.

## Notes
- `Home.dc.html` is the single evolving design doc (v1 → v9); `NoteView.dc.html`, `DocumentView.dc.html`, `CardTile.dc.html` are the reusable pieces. Versioned snapshots live in the repo under `design/v1 … design/v5`; a `design/v9` snapshot should be cut from this project when v9 is approved.
- **Design-side flag for the build:** the warning threshold is compared against the **modules' R2 total only** — the Litestream replica and the backup bucket are derived copies and stay out of it, consistently with D214 keeping them out of the per-module sums. If the API compares the whole bucket instead, the default 1024 MB fires permanently and the register stops meaning anything.
- Out of design scope for v9 (build concerns, stated as such in the addendum): the 23-row leak table, the redaction seam and its read paths, the four slug indexes + `Store.SiblingSlugTaken`, the `platform/storage` catalog, the `sqlite_master` completeness test and its FTS5 allow-list, the `dbstat` boot probe, and the HTTP cache-header change for private streams.
- Deliberately NOT designed in v9 (per the Do-NOT list): per-person sharing or share links, an unpublish control, a permission-denied screen for a foreign private item, any encryption affordance, a storage widget/metric/list/push/scheduled summary, storage history or forecasts, quota UI, a cleanup wizard or bucket browser, titles/filenames/thumbnails anywhere in Administrace, privacy in any other module, a new nav destination, and offline writes.
- **Open decision for Karel:** still Path A vs Path B for the four-bucket palette (v6); v9 changes nothing there.
- Deliberately not folded in: the Mode B **Login** screen and the **redirect / signed-out** shell (LoginScreen.tsx, RedirectingShell.tsx).
