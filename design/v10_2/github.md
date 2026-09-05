repo: kareltilcer/ws-tilcer-home
branch: main
path: design, frontend/src, handoff

## Last sync
date: 2026-09-05T05:12:00Z
<!-- commit sha omitted: only a tree hash was resolved (b2ec0f75a881), not a commit -->

### Updated in this project
- **Chat is built and the design was read back against it.** The repo is now at **v10.1** (`handoff/v10/CHANGELOG.md` §v10.1, `HANDOFF-design.md` §v10.1 — as built, decisions **D265–D269**, OpenAPI 0.13.0), and the module lives at `frontend/src/modules/chat/` — not the `routes/chat/` the old screen map guessed.
- **Reactions are D265, not D264.** The design bundle numbered them D264 and that number was taken three days earlier. Renumbered here with the collision recorded rather than quietly resolved, per the CHANGELOG.
- **The reaction palette is now an overlay hung under the message row**, not the desktop mock's popover above the ☺ and not the phone's in-flow strip. Two reasons, both from the build: a strip resized the message card the moment it opened (a bar is ~380 px, a bubble is as wide as what somebody typed), and an overlay anchored to the row travels with its message inside the scroll box, so nothing is measured per frame. Aligned to the message's side, `max-width` = the row's (351 px at 375, not the bubble's 298), 44 px thumb / 34 px desk targets scrolling horizontally inside the bar, and a ✕ **Zavřít reakce** — the phone needs a visible dismissal.
- **Chip labels corrected to the shipped contract:** `❤️ vy, Petr` (emoji first, *vy* not *ty*), and it is the chip's **accessible name**, not only a `title` — an emoji plus a numeral announces nothing otherwise.
- **Three touch gestures folded in (D268):** double tap hearts (and un-hearts — the request is a desired state, not a toggle, because a gesture fires twice easily), swipe right replies, long press opens the palette. Every gesture has a visible control doing the same thing, and none of them may fire during a scroll.
- **The conversation row previews its last message (D266), bounded by the floor** — the room's newest message is not the newest one that member may read. It says who, a files-only message previews as a count with no filename, a tombstone previews as the tombstone, and *Zatím žádné zprávy* in italics covers both the empty room and the just-added member. It took the member count's line; the count stays in the thread header and the members panel.
- **`/chat` opens the last-opened conversation at ≥1024 (D269)** — the empty second pane was a permanent half-page of instructions. An id is remembered, never content, per user; below 1024 there is no redirect, or the list would be unreachable.
- **Two new state-matrix rows and four new hard problems (78–81)**, including the design-side lesson of the pass: a `truncate` line plus a grid item's default `min-width: auto` made the list pane's minimum width the preview sentence's width — 415 px inside a 375 px grid, ＋ clipped and every timestamp reading *21* instead of *21:45*. Any mock that puts variable-length text on a fixed-width pane carries that hazard; the fix is `min-w-0` on the box where the width has to stop.
- Version label moved to **v0.10.1**; the fixed chat vocabulary gained *Přidat reakci · Zavřít reakce*.
- **Not changed, and checked:** the koš section already only drew when it had something in it (D267); the list pane's subtitle and the composer's hint are already erased in this bundle; both caps are still stated where they bite.

## Screen map
| Design (Home.dc.html) | Repo source |
|---|---|
| Design tokens / `:root`, `--bub-mine` · `--bub-theirs` · `--att-removed` | frontend/src/theme/globals.css (v10 aliases written out in both theme blocks; v9's still use `var()` in `:root` and do not re-resolve under `.light`) |
| APLIKACE · nav shell — Chat tab, Okno in "Více" | frontend/src/app/AppShell.tsx, app/routes.ts (`DESKTOP_NAV_ORDER`, `isFullBleedRoute`, `chatUklid`) |
| APLIKACE · Chat — seznam, náhled řádku, koš | frontend/src/modules/chat/ConversationList.tsx, when.ts, lastOpened.ts |
| APLIKACE · Chat — vlákno, podlaha, citace, reakce, gesta | frontend/src/modules/chat/ThreadView.tsx (+ `ReactionPicker`), reactions.ts, gestures.ts |
| APLIKACE · Chat — skladatel a přílohy | frontend/src/modules/chat/ChatPage.tsx, AttachmentView.tsx; upload queue pattern from modules/documents/UploadQueue.tsx |
| APLIKACE · Chat — členové, přidat člena | frontend/src/modules/chat/MembersPanel.tsx, DirectoryPicker.tsx, useLeaveConfirm.ts |
| APLIKACE · Chat — varování o úložišti · /chat/uklid | frontend/src/modules/chat/StorageWarning.tsx, UklidPage.tsx |
| APLIKACE · Administrace → Úložiště, blok Chat | frontend/src/modules/admin/StorageTab.tsx, ChatStorageBlock.tsx |
| APLIKACE · Administrace → Limity (D263) | frontend/src/modules/admin/LimitsTab.tsx |
| APLIKACE · Nástěnka | frontend/src/modules/dashboard/NastenkaPage.tsx, platform/widgets/registry.tsx — **untouched, three releases running** (D252) |
| APLIKACE · Poznámky · Dokumenty · Úkoly · Okno · Finance · Zahrada · Elektřina | frontend/src/modules/{notes,documents,todo,events,finance,garden,electricity}/ |
| Data model / copy | frontend/src/api/types.ts, api/keys.ts, i18n/cs.ts (`cs.chat`), i18n/plural.ts |
| Log — struktura bez zpráv | frontend/src/modules/logging/LogPage.tsx — chat.* labels, **no message and no reaction events** (D231) |

## Sync history
- 2026-08-26 — v10 (Chat): the eleventh module and the first the household does not read in full. Membership as the third axis of access; the history floor drawn in three places and nowhere else; Všichni never shows the floor line (D258); the first demotion in the app's history (D260); no new colour; three attachment states; Úklid úložiště chatu; 7-day koš; reader as the first writer; chat excluded from the PWA persister; editable thresholds (D263).
- 2026-08-23 — v9 (Soukromé položky a Úložiště): first version with no new module; two roots per module, publish as the only irreversible action, foreign private item = ordinary 404, Úložiště with replica + backup bucket outside the breakdown (D214), Administrace grown to six tabs.
- 2026-08-20 — v8 (Elektřina): tenth module, four sub-routes, `compute.go` mirrored as a pure `elCompute()`, headroom as the day-one headline, blocked ≠ error.
- 2026-08-19 — v7 (Zahrada): ninth module, eight sub-routes behind one „Více" entry, plan checks C1–C11, crop library, print artefacts.
- 2026-08-17 — v6 (Finance): fin.tilcer.cz cloned as the eighth module; four-bucket palette Path A/B.
- 2026-08-16 — v5 (Administrace, Web Push, PWA).
- 2026-08-12 — v4 (Dokumenty). 2026-07-30 — v3 (Poznámky). 2026-07-29 — v2 a11y token fixes.

## Notes
- `Home.dc.html` is the single evolving design doc (v1 → v10.1); `NoteView.dc.html`, `DocumentView.dc.html`, `CardTile.dc.html` are the reusable pieces. Versioned snapshots live in the repo under `design/v1 … design/v10_1`; the project file now matches `design/v10_1` **plus** the as-built corrections listed above, so a `design/v10_2` snapshot should be cut from here.
- `handoff/v10/HANDOFF-design.md` and `handoff/v10/CHANGELOG.md` in this project are copies of the repo at this sync — read §v10.1 in both before trusting an older number. `handoff/v10/HANDOFF-12-chat.md` is the v10 build guide and names the superseded 0.12.0 contract; the served contract is **0.13.0**.
- **Open with the build, not fixed:** the published `reply_to` is a per-recipient field on a frame marshalled once — v10's defect on the send path, recorded and deliberately not fixed in v10.1.
- **Design-side flag:** the warning threshold is compared against the modules' R2 total only — the Litestream replica and the backup bucket stay out of it (D214). The per-conversation threshold *includes* a trashed conversation's bytes until it is really purged (D254); if the API excludes them, *Smazat natrvalo* loses its reason to exist.
- **Open decision for Karel:** still Path A vs Path B for the four-bucket palette (v6).
- Deliberately NOT designed in v10 / v10.1 (per the Do-NOT lists): system messages for joins and leaves (D221), an edit history, a preview pipeline or transcoding, a Nástěnka widget / metric / list for chat (D252), a permission screen for a non-member (404 only), a staged-actions tray on the clean-up page, storage forecasts or quota UI, any way into a conversation from Administrace, and per-person sharing or share links.
- Deliberately not folded in: the Mode B **Login** screen and the **redirect / signed-out** shell (LoginScreen.tsx, RedirectingShell.tsx).
