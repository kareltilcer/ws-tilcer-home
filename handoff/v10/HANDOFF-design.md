# Home — Design Handoff (Claude Design)

> **Read first:** root `CLAUDE.md` (project conventions), then `PRD.md` (source of truth for behaviour), `openapi.yaml` (data shapes you'll be rendering), and `notes.md` (resolved decisions). This brief covers *what to design and why*; the PRD governs *what it does*.
>
> Status: v1 brief issued 2026-07-19; **v2 addendum — 2026-07-21** (login + widget host); **v3 addendum — 2026-07-29** (Poznámky / `notes`); **v4 addendum — 2026-08-11** (Dokumenty / `documents`); **v5 addendum — 2026-08-16** (Administrace / `admin` + push notifications + PWA); **v6 addendum — 2026-08-17** (Finance / `finance` — the retiring `fin` service absorbed); **v7 addendum — 2026-08-18** (Zahrada / `garden` — crop knowledge base, bed plan, plan checks, work calendar; § v7); **v8 addendum — 2026-08-20** (Elektřina / `electricity` — VT/NT readings, ceník, zálohy, the nedoplatek/přeplatek screen; § v8), with a **§v8 — as built section at the end, 2026-08-21**, where the nudge-escalation question is closed and two AA token failures are recorded; **v9 addendum — 2026-08-21** (Soukromé položky a Úložiště / `notes` · `documents` · `admin` — a second, private root beside each of the two trees, plus a storage-statistics page and a purge screen; § v9), with a **§v9 — as built section at the end, 2026-08-25**, where the two-level Administrace strip, the `nav`-not-`tablist` switcher and the declined replica line are recorded. Owner: Karel · Precedes implementation (Claude Code) — design informs the build, not the other way round; **v7, v8 and v9 are all built and live**.
>
> **The body of this brief describes the v1 four-module design (delivered and approved). v2 changes two things — login is now home-hosted, and Nástěnka is a widget host. v3 adds a fifth module, Poznámky. v4 adds a sixth module, Dokumenty (file storage), and re-solves the mobile nav for six destinations. v5 adds a seventh, admin-only module (Administrace) plus app-wide push and PWA/offline treatment. v6 adds an eighth module, Finance — a clone of the standalone `fin` service, which v6 then retires, and the only addendum whose screens already exist somewhere to look at. v7 adds a ninth module, Zahrada — the biggest round since v1 (eight routes, eleven entities), the first whose primary screen is a *planning tool* rather than a list, and the first meant to be used **outdoors in daylight**, which makes the light theme a first-class case rather than a conversion. Read the `## v2` … `## v7 addendum` sections at the end before starting; where an addendum conflicts with the v1 body, the addendum wins for its own screens. The v4 nav section supersedes the v3 nav item; v5, v6 and v7 extend it (v5 adds Administrace for admins, v6 adds Finance for everyone, v7 adds Zahrada for everyone — and v7 is the first module with **eight sub-routes behind one nav entry**). **v9 is the first addendum that adds no module and no nav entry at all**: it changes two modules people already use daily — Poznámky and Dokumenty each gain a *second root*, the uploader's own — and grows Administrace to six tabs. Its brief is therefore a harder one, not an easier one: the design problem is not how to show a private folder, it is how a person always knows which of two trees they are standing in when there is no way to undo putting something in the wrong one.**

## Prime directive

**Function first, always. Then make it pretty.**

`home` is a tool Karel and his household will touch many times a day, mostly one-handed on a phone, sometimes on a laptop. A beautiful screen that costs an extra tap is a worse screen. Where visual polish and speed-of-use conflict, speed wins — every time. Within that constraint, make it genuinely nice to look at: this is greenfield, and the visual language you establish here is a candidate house style for the other `*.tilcer.cz` services.

The single hardest requirement, quoted from the original brief: *"big emphasis on mobile-friendly and desktop-friendly design, most actions need to be very accessible from both."* Neither form factor is the afterthought. A design that works beautifully on desktop and merely *functions* on mobile has failed this brief.

## What `home` is

A household management system that will grow module by module. **Four** modules ship in v1:

1. **Nástěnka** (dashboard) — **the landing page**. Two lists: active event reminders, and every to-do currently sitting in a "Právě dělám" column. Tap either for detail; complete either with a deliberate press-and-hold (§10 D22). This is the screen the household sees most often, and it answers one question: *what needs me right now?*
2. **Úkoly** (to-do) — a Trello-style board. The daily driver. Many columns that can be collapsed and sorted by priority, feeding **"Právě dělám"** columns, plus a **Hotovo/archive** column. Cards carry notes, links, a checklist, and labels.
3. **Okno do budoucnosti** (events) — future all-day events listed **grouped by month**. Title, description, links; optional weekly/monthly/yearly recurrence; an optional reminder a chosen lead time before the date (1 den / 2 dny / 1 týden / 2 týdny / 1 měsíc). Those reminders are what surface on Nástěnka.
4. **Log** (log browser) — a detailed audit-log explorer over the logging spine that records every action in every module. Admin-only. The project's long-term centerpiece: every future module feeds it, so its browsing/filtering/analysis UX needs to be genuinely good, not a debug table.

Design **all four in depth**. Nástěnka and Úkoly carry the most daily weight; Okno has the most intricate *form* (recurrence + reminder); Log has the most intricate *data*.

## Deliverables

1. **Design system document** — tokens (color for **both themes, dark being the default**; type scale, spacing, radii, elevation, motion), component inventory with variants/states, and usage rules. Expressed against **Tailwind**'s scale and mapped to **shadcn/ui** primitives so it drops straight into the build.
2. **Hi-fi interactive prototype** — a self-contained single file demonstrating the key screens at **both** breakpoints. Since shadcn/ui is React+Radix and a prototype shouldn't need a build step, either write it as single-file React or as HTML+Tailwind that *mirrors shadcn component anatomy* — and in either case **name the shadcn component each element maps to** (Button, Card, Dialog, Sheet, Popover, Command, Tabs, Badge, Checkbox, Select, ScrollArea, Separator, Tooltip, DropdownMenu…). In-memory state is fine; no backend.

The prototype must be **judgeable at 375 px and at 1440 px**. Make it easy to see both — a width toggle, side-by-side frames, or genuinely responsive markup the reviewer can resize.

### Stack constraints (fixed)

- **React + TypeScript + TanStack Query**, styled with **Tailwind + shadcn/ui**. Design within Tailwind's default scale unless you have a stated reason to extend it; document any extension as a token.
- Drag-and-drop will be **dnd-kit**. Design drag affordances that are achievable with it.
- No component library beyond shadcn/ui without flagging it.

## Language, theme, and locale (fixed)

### The UI is Czech-only

Not multilingual — no language switcher, no i18n framework needed. Keep user-facing strings centralized in one module anyway, so copy can be revised in one place. Write the actual UX copy **in Czech** (the `design:ux-copy` skill applies — in Czech).

Czech has consequences you must design around:

- **Strings run longer than English.** "Working on now" → *"Právě dělám"*, "Backlog" → *"Zásobník"*, "Done" → *"Hotovo"*. Some shrink, many compound labels grow. Column headers, buttons, and card tiles must survive it — no fixed-width labels that clip.
- **Diacritics need vertical room.** ě š č ř ž ý á í é ú ů ď ť ň, and uppercase forms (Č, Ř, Ů) sit taller. Tight line-heights and cramped button padding will clip them. Verify with real accented strings, never ASCII placeholders.
- **Three plural forms.** Czech pluralizes 1 / 2–4 / 5+ differently: *1 úkol*, *2 úkoly*, *5 úkolů*. Every count label — card counts in column headers, checklist progress, log result counts — needs all three. Design the copy accordingly and flag it for implementation.
- **Czech collation:** č, ř, š, ž sort *after* c, r, s, z — not as accented variants. Relevant wherever lists sort alphabetically.
- **Formats:** dates `d. M. yyyy` (*19. 7. 2026*), 24-hour time, space as thousands separator, comma as decimal mark. The log browser's timestamps and the analytics axes must follow this.

### Dark mode is the default theme

Design **dark-first**. Light is the secondary theme — not the baseline you dim afterwards.

- Ship **both** palettes as tokens, dark canonical. shadcn/ui drives themes off CSS variables, so put the **dark values in `:root`** and light overrides under a `.light` class — the inverse of shadcn's default `.dark` convention. State this explicitly in the system doc so the build doesn't invert it.
- **Dark-specific pitfalls to solve:** avoid pure `#000` + pure `#fff` (halation on OLED phones); convey elevation with **surface lightness steps**, not drop shadows, which mostly disappear on dark; saturated accents bloom — desaturate them. WCAG AA still applies and is *easier to fail* on dark.
- **Label colors** must stay mutually distinguishable on dark, and distinguishable to color-blind users — never encode meaning in hue alone.
- **Diff colors** (`old → new`) in the log browser: red/green on dark needs care. Pick dark-safe pairs that pass contrast and carry a non-hue cue too.
- **Charts** in the analytics panel need a dark-native palette, not a light palette dropped onto a dark card.

## Users & roles (shapes what each person sees)

Household members log in through the shared auth service (site `home`). Three roles:

| Role | Nástěnka / Úkoly / Okno | Log browser |
|---|---|---|
| `admin` | Full, incl. structural (create/delete boards, columns, labels) | **Yes** |
| `editor` | Full writes (columns, cards, checklists, links, labels, events, mark-done) | No |
| `reader` | **View-only** — no edit affordances at all, cannot mark anything done | No |

`reader` is a real design case: the board must look intentional in view-only mode, not like a broken editor with dead buttons. Decide whether write affordances are hidden or disabled-with-reason, and be consistent.

## Screen inventory

Design each with its **loading, empty, error, and permission-denied** states. "Empty" is not an afterthought here — a brand-new board and a freshly-filtered log are both first impressions.

### A. App shell

- **Mobile:** bottom tab bar — **Nástěnka · Úkoly · Okno · Log** (last admin-only). Thumb-reachable. Four tabs is the ceiling on a phone; if a fifth module ever lands, the pattern must survive it.
- **Desktop:** side nav, same four destinations.
- **Nástěnka is the landing route** — it's what opens on launch.
- **Board switcher** (within Úkoly) — multiple boards are supported from v1. Fast to reach and to scan on both form factors.
- Signed-out / redirecting state (login itself is **auth-hosted — do not design login, signup, or password screens**).

### B. Nástěnka (landing) — the most-seen screen

Two lists, clearly separated and clearly labelled:

- **Události** — active event reminders. At most one row per event. **Overdue rows are visually distinct and sort to the top.** Each row shows the date, how far away it is (or how overdue), the title, and whether it repeats.
- **Úkoly** — every card sitting in a "Právě dělám" column, across *all* boards. Group by board when more than one board contributes, otherwise don't add the noise.
- **Press-and-hold done** on every row — 2000 ms with a visible fill, early release cancels (§10 D22; this supersedes the original one-tap requirement). It must not require opening the detail first. **The hold cannot be the only path**: keyboard/assistive activation commits immediately, and the detail dialog's "✓ Hotovo" is a single activation.
- **Tap the row** (not the done control) → detail dialog: card detail for to-dos, event detail for reminders. Same components as their home modules.
- **Empty state is a success state.** "Nothing needs you right now" should feel earned and calm, not like a failed load. Design it deliberately — it's the state a well-run household hits often.

### C. Úkoly (to-do board) — the daily driver

- **Desktop:** horizontal kanban. Columns collapsible to a thin labeled spine so many fit. Drag to reorder columns and cards. `now` column visually emphasized.
- **Mobile:** a **vertical accordion of collapsible columns**, not a horizontal kanban. `now` column(s) pinned to the top. This is the key departure from Trello and the thing to get right.
- **Column header:** name, card count, priority, collapse toggle, overflow menu (rename, set priority, set kind, delete).
- **Card tile:** title, label chips, checklist progress, link/notes indicators. Dense but scannable.
- **Quick-add card** at the top of each column — must be near-instant, not a modal.
- **"Move to…" control** — the primary workflow action on mobile (touch drag is finicky, so drag is the *secondary* path there). Target: **one tap to open, one tap to pick the destination.** Common actions overall must be **≤2 taps** from the board.
- **Filter bar:** label chips, text search, show/hide archived-done.

### D. Card detail

Full-screen **sheet** on mobile, **dialog** on desktop. Contains: title, markdown notes (view/edit toggle), links list (add / open / remove), checklist with progress, labels. Editing notes on a phone must not feel cramped. Reused verbatim when opened from Nástěnka.

### E. Okno do budoucnosti

- **Month-grouped list**, current month forward by default, pageable to other months including past ones. Each row: date, title, a recurrence indicator, and a reminder indicator with its lead time. Months with nothing in them still need to read cleanly.
- **Event form** — the most intricate form in the app, and it has to work one-handed:
  - title, description (markdown), links
  - **date picker** (all-day; no time field anywhere)
  - **recurrence selector**: *nikdy · týdně · měsíčně · ročně*, plus an optional end date
  - **reminder**: a checkbox, and when ticked, a lead-time selector — *1 den · 2 dny · 1 týden · 2 týdny · 1 měsíc*
  - The reminder control is conditional on the checkbox. Design that reveal so it doesn't jump the layout.
- **Series-edit warning.** There are no per-occurrence exceptions: editing or deleting a recurring event changes **every** occurrence. The UI must say so plainly *before* saving — not a toast afterwards. This is the highest-consequence copy in the app; get it unambiguous.

### F. Log browser (admin)

- **Filter bar:** date range, module, actor, action, entity type/id, level, full-text search. Seven filter dimensions is a lot — solving this on a 375 px screen is a genuine design problem, not a "collapse it into a drawer" shrug.
- **Result stream:** newest-first rows, each expandable to reveal field diffs.
- **Diff rendering:** `old → new` per field. **Note:** the system stores *full, untruncated* values (decision D6), so a diff can be a whole paragraph of card notes. Design truncation-with-expand, and make a long-text diff readable on a phone.
- **Entity timeline:** the full chronological history of one card/column/board, with its diffs. This is the payoff feature — treat it as a first-class screen, not a filtered list.
- **Analytics panel:** counts by module/actor/action over a range, plus top-N. Small charts; keep them honest and legible, not decorative.

## Hard problems — solve these explicitly

These are the reasons this brief exists. Address each in the system doc.

1. **Many columns, small screen.** Karel expects *"so many"* columns waiting to feed "Právě dělám". Collapse, priority sort, and pinning are the given tools — show how someone with 20 columns finds the right one on a phone without endless scrolling.
2. **Move-to as a first-class action.** The core loop is *pull a card into "Právě dělám", do it, push it to "Hotovo".* That loop should be the fastest thing in the app on both form factors.
3. **Multiple `now`/`done` columns.** Per decision D7, `kind` is a free-form hint — a board may have several `now` or `done` columns. Your pinning and "move to" designs must not assume exactly one of each.
4. **Density vs. touch targets.** Dense enough to see a lot; targets still ≥44 px on touch. These fight — resolve it deliberately, likely with different densities per breakpoint.
5. **Live updates mid-interaction.** A websocket pushes other members' changes (D10). A card moving under someone's thumb while they're reading it is hostile. Design how remote changes arrive — animate in, badge, defer while a sheet is open?
6. **Optimistic writes and rollback.** Moves apply instantly and may fail. Design the pending and the rollback-with-reason states so a silent revert never confuses anyone.
7. **View-only that looks deliberate** (the `reader` case above).
8. **Seven-dimension filtering on a phone** (log browser above).
9. **Two item types, one dashboard.** Nástěnka mixes event reminders and to-do cards in one page with one shared "done" gesture. They must be tellable apart at a glance without the page fragmenting into two unrelated apps stacked vertically.
10. **Overdue without alarm fatigue.** Overdue reminders must stand out enough to act on, but a household that's a week behind shouldn't open to a wall of red. Find the register between "ignorable" and "shouting."
11. **The recurrence + reminder form, one-handed.** A date, a recurrence choice, an optional end date, a checkbox, and a conditional lead-time selector — on a 375 px screen, without the layout jumping when the checkbox reveals the selector.
12. **Warning before an irreversible series edit.** Changing a recurring event hits every occurrence and there's no undo per occurrence. Communicate that before the save, in plain Czech, without training people to click through it every time.

## Do NOT design

Out of scope per the PRD — designing these wastes your time and risks implying features:

- **Login / signup / password / 2FA screens** — auth-hosted (Mode A), not ours. **⚠ v2 override:** login *is* now home-hosted — design it per the v2 addendum. Signup / password-reset / 2FA / Google remain auth-hosted and out of scope.
- **Due dates on to-do cards** — events have dates; cards don't.
- **Push or email notifications** — reminders are **in-app only**, seen when Nástěnka is opened. Design no notification settings, no delivery preferences, no notification centre. **⚠ v5 override:** Web Push *is* now in scope — design it per the v5 addendum. Email/SMS remain out of scope.
- **Time of day on events** — events are all-day. No time pickers anywhere.
- **Per-occurrence editing of a recurring event** — there is no "this occurrence only" option. Series-only, with the warning described above.
- **Card assignee** — deliberately absent; accountability comes from the audit log, not per-card assignment.
- **Collaborative cursors / simultaneous co-editing** — changes sync, but editing is last-write-wins.
- **External calendar sync, iCal import/export.**
- **Offline / PWA install.** **⚠ v5 override:** Home *is* now an installable, reads-only-offline PWA — design it per the v5 addendum.
- **Column collapse as a synced setting** — it's per-device (localStorage), so don't design cross-device collapse affordances.

## Accessibility & quality bar

- **WCAG 2.1 AA** — contrast, focus visibility, semantic structure. Run the `design:accessibility-review` skill against your own output before handing back.
- **Check contrast in both themes.** Passing on light says nothing about dark, and dark is the default — so dark is the pass you cannot skip.
- **Touch targets ≥44 px**; full keyboard operability on desktop, including drag alternatives (a keyboard user must be able to move a card).
- **Respect `prefers-reduced-motion`** — especially for drag, sheet transitions, and live-update animations.
- Use **realistic Czech household content** in the prototype — real chores, bills, repairs, shopping (*"Vyměnit baterii v kotli"*, *"Zaplatit plyn"*, *"Objednat servis kotle"*) — never lorem ipsum and never English placeholders. Density, diacritic, and truncation problems only surface with real Czech strings. Also include deliberately awkward data: a very long card title, a 20-item checklist, a column with 40 cards, an empty column, a paragraph-length diff, a **badly overdue reminder**, a **month with no events**, and a **recurring event** (so the recurrence and reminder indicators are exercised).
- Useful companions: `design:design-system`, `design:ux-copy` (empty states, confirmations, the "Move to…" labels), `design:design-critique` for a self-review pass.

## Definition of done

- [ ] Design system doc: tokens, component inventory with all variants/states, mapped to Tailwind scale + named shadcn/ui primitives.
- [ ] Prototype covers: app shell (4-tab nav) + board switcher; **Nástěnka** (both lists, overdue state, press-and-hold done incl. the mid-hold fill state, empty state); **Úkoly** (desktop kanban *and* mobile accordion, column collapsed/expanded, card tile, quick-add, "Přesunout do…", filter bar); **card detail** (sheet + dialog); **Okno do budoucnosti** (month list + the full event form incl. recurrence, optional end date, and conditional reminder lead-time) + the series-edit warning; **Log** (filters, stream, expanded diff, entity timeline, analytics).
- [ ] Every screen shows loading, empty, error, and `reader` view-only states.
- [ ] All twelve **hard problems** explicitly addressed with a stated rationale.
- [ ] Verified at **375 px and 1440 px**, in **both themes**; **≤2 taps** for pull card → Právě dělám → Hotovo. Nástěnka completion is a **2000 ms press-and-hold** (D22) with a keyboard path that commits immediately.
- [ ] **All copy in Czech**, including the three plural forms for every count label; layout verified with real diacritics and long Czech strings.
- [ ] **Dark (default) and light** palettes both tokenized — dark in `:root`, light under `.light`; AA contrast passes in both; label, diff, and chart colors verified on dark.
- [ ] Accessibility pass done; keyboard path for drag documented.
- [ ] Nothing from the **Do NOT design** list appears in the output.

## Inputs resolved

- **2026-07-19 — UI language:** **Czech-only.** No language switcher, no i18n framework.
- **2026-07-19 — Dark mode:** **yes, and it is the default theme.** Light ships as the secondary theme.

---

## v2 addendum (2026-07-21) — new screens to design

v2 (see `CHANGELOG.md`, PRD §10 D23–D29) adds two things the approved v1 prototype doesn't cover. Extend the **existing** design system and tokens — same Czech, same dark-default, same function-first bar. Reuse v1 components wherever possible (card detail, event detail, the press-and-hold done control all carry over unchanged).

### 1. Home-hosted login (Mode B — D23)

Home now hosts its own login instead of redirecting to auth. Design:

- **Login screen** — email + password + "Přihlásit". This is the first thing a user sees signed-out; it sets the tone. States: idle, submitting, invalid credentials (generic — never reveal which field), account disabled, server/auth-unreachable, and an **MFA-required** case that shows a short message + link to finish on `auth.tilcer.cz` (home doesn't handle MFA).
- **No signup form, no reset form.** A "Zapomněli jste heslo?" link points out to auth-hosted reset; a quiet "Nemáte účet? Požádejte správce" line. Do **not** design signup, reset, TOTP, or Google screens — they're out of scope (auth-hosted).
- **Signed-out / redirecting shell state** (also the v1 gap) — a minimal branded frame while the app checks the session.

### 2. Nástěnka as a widget host (D24, D27, D28)

Nástěnka is no longer two fixed lists — it's a **per-user arrangement of widgets**. Design:

- **The grid:** one reorderable column on mobile; a **2-column grid on desktop** where a widget is **narrow (1 col)** or **wide (2 col)**.
- **v1 widgets** (their *contents* are already designed — reuse them, now framed as widgets): **Právě dělám** (tasks in "Právě dělám" columns, across boards, grouped by board when >1), **Připomínky** (active reminders, overdue-first), **Tento měsíc** (new: read-only look-ahead list of upcoming events). Each widget needs a consistent **frame**: title, optional count, and an arrange affordance.
- **Arrange mode:** add a widget from a **catalog/picker**, hide/remove one, **drag to reorder**, and **resize narrow↔wide**. Design the affordances for all four, on both form factors — reorder and resize must be operable by keyboard too, not drag-only.
- **Empty dashboard** (all widgets hidden) is a valid, deliberate state — a calm "přidat widget" prompt, not a broken page. Also design the **first-run default** (all widgets visible).
- The **2000 ms press-and-hold done** gesture (D22) and the detail dialogs carry over unchanged inside the task/reminder widgets.

### v2 Definition of done (addendum)

- [ ] Login + signed-out states, all error cases incl. MFA-redirect; no signup/reset/TOTP/Google screens.
- [ ] Widget host: grid at 375 px (1 col) and 1440 px (2 col), narrow vs wide widgets, arrange mode (add/hide/reorder/resize) with a keyboard path, empty + first-run states — both themes.
- [ ] v1 widget contents reused, now in consistent widget frames.
- [ ] Nothing from the v1 **Do NOT design** list, plus no signup/reset/MFA/Google.

---

*After the v2 addendum is approved, the frontend of the login + dashboard host in `HANDOFF-4-dashboard.md` / `HANDOFF.md` is reconciled against it. Backend and modules 1–3 do not wait on it.*

---

## v3 addendum (2026-07-29) — Poznámky (`notes`)

v3 (see `CHANGELOG.md`, PRD §4 FR-P1–P8, §10 **D30–D38**) adds a **fifth module, Poznámky**. Extend the **existing** design system and tokens — same Czech, same dark-default, same function-first bar. Reuse v1/v2 components wherever they fit (dialogs/sheets, the "Přesunout do…" mover from Úkoly, the widget frame from the dashboard host, the Markdown *rendering* already used for card notes / event descriptions). Where this addendum conflicts with the v1/v2 body, **v3 wins for Poznámky screens only.**

**One-liner:** Markdown notes (WYSIWYG-default, raw-Markdown toggle) in a folder/subfolder tree; every note and folder has a stable in-app slug-path URL any logged-in household member can open; notes can be pinned "pro všechny" or "jen pro mě" and surface in a Nástěnka widget whose rows open in an **overlay dialog without leaving Nástěnka**.

### What to design

**1. The notes browser (the module's home screen).**
- **Desktop:** a **folder-tree sidebar + note pane** — the tree (folders/subfolders, expand/collapse) on the left, the selected note on the right. A breadcrumb of the current path above the pane.
- **Mobile:** **drill-down**, not a squeezed sidebar — folder → its contents (subfolders + notes) → note, with a back affordance and the same breadcrumb. The tree is a screen, not a rail.
- Folder rows show a note/subfolder count; note rows show title + a one-line excerpt + a pin marker when pinned. Czech collation (č/ř/š/ž sort after c/r/s/z) for any alphabetical ordering; manual (lexorank) order is the default within a folder.

**2. Note view + editor — the centrepiece.**
- **WYSIWYG is the default** editing surface; a **toggle to raw Markdown** is the escape hatch. Both edit the one canonical Markdown body and round-trip — design the toggle so switching never implies two documents. A read/view state and an edit state.
- **Pick a Markdown-backed rich-text editor** and name it (as the v1 brief named shadcn primitives) — it must round-trip Markdown, be usable one-handed on a phone, and **not clip Czech diacritics** (ě š č ř ž ý á í é ú ů ď ť ň and capitals) in either mode. Keep the toolbar minimal (headings, bold/italic, list, link, code, quote) — this is a household notes app, not a CMS.
- Editing on a 375 px screen must not feel cramped (this echoes the v1 card-notes requirement, raised to a full editor). Show the **"změněno jinde"** (changed-elsewhere) notice when a `/ws` push reports the open note changed under the editor (last-write-wins, D38) — a non-blocking banner, not a hijacking modal.

**3. Organisation affordances.** Create / rename / move / delete for both notes and folders. Reuse the **"Přesunout do…"** pattern from Úkoly for move (drag on desktop, one-tap picker on mobile) — a folder picker with the tree. Deleting a **non-empty folder** needs a **cascade warning in plain Czech before the delete** (it removes subfolders + notes) — treat it like the events series-edit warning: unambiguous, pre-action, not a toast afterwards.

**4. Pinning — two scopes.** A pin control on a note offering **"Připnout pro všechny"** (household) and **"Připnout jen pro mě"** (personal), with clear current-state indication (a note can be both). Design so the two scopes are legible without clutter. A `reader` sees **only the personal** option (household pinning is an editor+ mutation, D35).

**5. Sharing — the copy-link affordance.** A **"Kopírovat odkaz"** action on a note/folder yields its slug-path URL. The affordance and any confirmation must make clear this is a **household link** — it opens only for logged-in members, it is **not public** (D33). Quietly communicate that renaming or moving an item **changes its link** (D32) — the copy is a "current address", not a permalink.

**6. The `notes.pripnute` widget + overlay dialog.** A dashboard widget (framed like the others — title **"Připnuté poznámky"**, optional count, the arrange affordance) listing the household pins **∪** the caller's personal pins, de-duplicated, each row = title + short excerpt + a scope marker. **Tapping a row opens the note in an overlay dialog on Nástěnka — the user never navigates away to Poznámky** (this is the explicit requirement). The overlay reuses the note view (read, with the WYSIWYG/Markdown edit toggle for editor+); edit/unpin from inside it act in place. **No press-and-hold "done" gesture here** — notes aren't completed; the row simply opens.

**7. Navigation — the fifth destination (the hard one, D37).** Poznámky is the fifth module. Regular members still see **four** tabs (Nástěnka · Úkoly · Okno · Poznámky — Log is admin-only), so the four-tab mobile ceiling holds for them. **Admins now have five destinations and exceed it.** Design a mobile app-shell **overflow / "více"** pattern that keeps the daily four thumb-reachable and moves the least-frequent, admin-only **Log** behind the overflow — without a second-class desktop experience. This is the top v3 nav problem; solve it explicitly at 375 px.

### States — design each (loading, empty, error, reader)

- **Empty root** (no notes/folders yet) — an inviting first-run, "vytvořte první poznámku", not a blank pane.
- **Empty folder** — reads as intentional, offers create-here.
- **No search results** for a query.
- **Reader view-only** — no create/edit/move/delete/household-pin affordances anywhere; **read + personal-pin remain**. The browser and note view must look deliberate in read-only, not like a disabled editor (same bar as the v1 `reader` board).
- **Loading / error** for tree, note fetch, search, and the widget.

### Czech UX copy (use `design:ux-copy`, in Czech)

Own the wording for: the two pin labels, the "Kopírovat odkaz" affordance + its "household, not public" cue, the cascade-delete warning, the "změněno jinde" notice, empty/first-run states, and the WYSIWYG/Markdown toggle labels. Count labels (e.g. folder contents) need the **three plural forms** (1 / 2–4 / 5+): *1 poznámka · 2 poznámky · 5 poznámek*, *1 složka · 2 složky · 5 složek*.

### Tokens & theme

Dark-first, tokenised, light under `.light` — as established. New surfaces to tokenise if the existing scale doesn't cover them: the **folder-tree** rows (hover/selected/expanded), the **editor** surface and its toolbar, and the **note-in-overlay** dialog. Reuse elevation-by-lightness (not shadows) on dark. Verify AA contrast in **both** themes with real Czech strings.

### Hard problems — address each with a rationale

1. **WYSIWYG ↔ Markdown parity, one-handed, without clipping diacritics.** The single hardest thing in the module.
2. **A deep folder tree on a 375 px screen** — the notes analogue of Úkoly's "many columns, small screen"; show how someone finds a note four levels down without endless scrolling.
3. **The 5th-destination nav / admin overflow** (D37).
4. **Two pin scopes, legible, not cluttered** — the widget and the note must both make "pro všechny" vs "jen pro mě" obvious at a glance.
5. **A share link that reads as household, not public** — the wrong mental model here is a real privacy trap.
6. **An overlay dialog on Nástěnka that never navigates away** and reuses the note view without a second implementation.
7. **URLs that change on rename/move** — communicate the tradeoff so no one thinks a copied link is permanent.

### v3 Definition of done (addendum)

- [ ] Notes browser: desktop tree+pane and mobile drill-down, breadcrumb, Czech collation; verified 375 px + 1440 px, both themes.
- [ ] Note view + editor: WYSIWYG default + raw-Markdown toggle (named editor library), read/edit states, "změněno jinde" notice, diacritics uncramped on mobile.
- [ ] Create/rename/move (drag + "Přesunout do…") and the **cascade-delete warning** for a non-empty folder.
- [ ] Two-scope pin control ("pro všechny" / "jen pro mě") with state; `reader` sees only personal.
- [ ] "Kopírovat odkaz" affordance that communicates **household-only, not public**, and the rename/move-changes-link tradeoff.
- [ ] `notes.pripnute` widget frame + **overlay dialog on Nástěnka (no navigation away)** reusing the note view; **no done gesture**.
- [ ] Mobile nav solved for the **fifth destination** with an admin overflow for Log; regular members keep four thumb-reachable tabs.
- [ ] Every screen: loading, empty (root + folder + no-results), error, and `reader` view-only states.
- [ ] All copy in Czech incl. the three plural forms for count labels; AA contrast in both themes with real diacritics.
- [ ] Accessibility pass (`design:accessibility-review`): keyboard-operable tree, move, and pin; touch targets ≥44 px; `prefers-reduced-motion` respected on the overlay + tree transitions.

### Do NOT design (v3 additions to the existing list)

- **Public / share-link management UI** — sharing is household-only; there is no "make public", no link-permissions panel, no share tokens (D33).
- **Upload / attachment / image-embed UI** — text + external links only; no file picker, no drag-to-upload, no media library (D34).
- **Version-history / revisions UI** — the audit log is the history; no per-note timeline UI in Poznámky (D36).
- **Note tags/labels, a note export (md/zip) flow, and note-level permission/ACL controls** — out of scope for v3.
- **A "done"/complete gesture in the notes widget** — notes aren't tasks.

*Backend for `notes` (`HANDOFF-5-notes.md`) can be built now from the PRD + `openapi.yaml`; the **Poznámky frontend and the nav overflow** are reconciled against this addendum once approved — the same rule v2 used for login + the widget host.*

---

## v4 addendum (2026-08-11) — Dokumenty (`documents`)

v4 (see `CHANGELOG.md`, PRD §4 **FR-DOC1–DOC11**, §10 **D39–D50**) adds a **sixth module, Dokumenty** — the first with **file storage**. Extend the **existing** design system and tokens — same Czech, same dark-default, same function-first bar. Reuse v1/v2/v3 components wherever they fit: dialogs/sheets, the **"Přesunout do…"** mover and cascade-delete warning from Úkoly/Poznámky, the **folder-tree + mobile drill-down** from Poznámky, the **widget frame + overlay** from the dashboard host, and the Markdown *rendering* for `.md` files. Where this addendum conflicts with the v1/v2/v3 body, **v4 wins for Dokumenty screens** — and **the v4 nav section (§7 below) supersedes the v3 addendum's nav item** (the overflow now affects everyone, not just admins).

**One-liner:** Files (PDF, image, Office & other) uploaded into a folder/subfolder tree; each has a **permanent, id-based, household-only URL**, an in-browser **preview** (PDF/image/text native, Office server-converted to PDF) and **download**; a document's bytes are **immutable — upload-once, never replaced**; documents can be pinned "pro všechny" / "jen pro mě" and surface in a Nástěnka widget whose rows open a **preview overlay without leaving Nástěnka**.

### What to design

**1. The documents browser (the module's home screen).**
- **Desktop:** a **folder-tree sidebar + a documents pane** (reuse Poznámky's tree). The pane's **default is a list** — filename-forward rows: file-type icon, title, size, modified date, a **preview-status** chip, and a pin marker — with a **grid/thumbnail toggle** (thumbnail cards once the preview is ready). Breadcrumb of the current path above the pane.
- **Mobile:** **drill-down** (folder → contents → document), **list by default**, same breadcrumb; the tree is a screen, not a rail. (Grid toggle available but list is the mobile default.)
- Folder rows show a document/subfolder count; document rows show title + type + size + a preview-status indicator (a quiet **"Náhled se připravuje…"** while `pending`). Czech collation (č/ř/š/ž after c/r/s/z) for any alphabetical sort; manual (lexorank) order is the default within a folder.

**2. Upload — the new interaction (Poznámky had none).**
- A prominent **"Nahrát dokument"** action + **drag-and-drop** onto a folder/pane on desktop; a **file picker** on mobile. Support **multiple files at once** — design the **upload queue**: per-file progress, success, and error rows.
- **Client-side pre-check** before the request: over-cap (**> 50 MB → "Soubor je větší než 50 MB"**) and blocked type, in plain Czech, before anything uploads. Server errors to design: `413` too large, `415` blocked type, and a network/R2 failure with **retry**.
- On success the item appears with `preview_status:"pending"` and **swaps to a thumbnail when `/ws` reports it ready** — design the pending→ready transition (skeleton/spinner → thumbnail) and the **preview-failed** outcome (stays usable, download-only) so neither is jarring.

**3. Document view + preview — the centrepiece.**
- A **DocumentView**: a header (title, type, size, breadcrumb), a **preview region**, and actions (**Stáhnout** always; rename/describe/move/delete/pin for `editor`+).
- The preview region renders **through safe viewers only** (D48): **PDF and Office→PDF** in a **sandboxed PDF viewer** (name it — e.g. `react-pdf` / PDF.js in a sandboxed iframe), **images** in an image viewer (pinch-zoom on mobile), **text/Markdown** as escaped text (reuse the Markdown renderer for `.md`). **Download-only types**, and the **preview-pending** / **preview-failed** cases, show a **type card** (icon + filename + Stáhnout) — never a broken frame.
- Build **DocumentView as a standalone component** — the dashboard overlay (§6) reuses it verbatim. **Mobile** is a full-screen **sheet**; the PDF/image preview must be pinch-zoomable and Stáhnout always reachable.

**4. Organisation affordances.** Create/rename folders; rename + edit **description** for documents; **move** via the **"Přesunout do…"** picker (drag on desktop, one-tap picker on mobile) into the documents tree. **Every delete requires an explicit confirmation before it happens** (like the series-edit / cascade warnings): distinguish **soft-delete** (archive, recoverable) from the **admin hard-delete** (permanent — **also removes the file from R2**) in the copy. A **non-empty folder** delete shows a **cascade warning** (removes subfolders + documents).

**5. Pinning — two scopes.** Reuse Poznámky's control: **"Připnout pro všechny"** (household) and **"Připnout jen pro mě"** (personal), current-state shown, a document can be both. A `reader` sees **only the personal** option (household pin is `editor`+, D47).

**6. Sharing — the copy-link affordance (note the difference from Poznámky).** **"Kopírovat odkaz"** yields the document's **permanent `/d/{id}` link**. Two cues to land: it is **household-only, not public** (opens only for logged-in members — D33/D42); and — **unlike Poznámky** — this link **is permanent** and does **not** change on rename/move (D42). Do **not** carry over Poznámky's "current address, changes on rename" caveat here; it would be wrong.

**7. The `documents.pripnute` widget + preview overlay.** A dashboard widget framed like the others — title **"Připnuté dokumenty"**, optional count, arrange affordance — listing household pins **∪** the caller's personal pins, de-duplicated (household precedence), each row = **file-type icon / thumbnail + title + size + a scope marker**. **Tapping a row opens the document in a preview overlay on Nástěnka — the user never navigates away to Dokumenty** (the explicit requirement). The overlay reuses **DocumentView** (preview + Stáhnout; rename/unpin for `editor`+); actions act in place. **No press-and-hold "done" gesture** — documents aren't completed; the row simply opens.

**8. Navigation — the sixth destination (the hard one, D49 — supersedes the v3 nav item).** With Dokumenty, **regular members now have five destinations** (Nástěnka · Úkoly · Okno · Poznámky · Dokumenty) and **also exceed the four-tab mobile ceiling** that only admins hit in v3. **Resolved:** a **four-slot bottom bar — Nástěnka · Úkoly · Okno · Poznámky — plus a "Více" sheet** that holds **Dokumenty** (and, for admins, **Log**). Design the **"Více" overflow**: a bottom sheet/menu of the overflowed destinations (icon + label + active state), reachable in one tap; and the **active-state handling** when the user is *inside* a Více destination (the "Více" tab reads active, and the sheet marks the current item). **Desktop side-nav lists all six**, no overflow. This **replaces the v3 addendum's nav design** (Log-only overflow) — now Dokumenty + Log share "Více".

### States — design each (loading, empty, error, reader)

- **Empty root** (no documents/folders yet) — an inviting first-run: **"Nahrajte první dokument"**, not a blank pane.
- **Empty folder** — reads as intentional, offers **upload-here**.
- **No search results** for a query (and a hint that search covers **name + description, not file contents** — D46).
- **Upload states:** queued, uploading (progress), success, and per-file error (too large / blocked type / network-retry).
- **Preview states:** pending, ready, failed (download-only), and inherently download-only types.
- **Reader view-only** — no upload/edit/move/delete/household-pin affordances anywhere; **preview + download + personal-pin remain**. Must look deliberate, not a disabled editor (same bar as the v1 `reader` board and the v3 `reader` notes).
- **Loading / error** for tree, document fetch, preview, search, and the widget.

### Czech UX copy (use `design:ux-copy`, in Czech)

Own the wording for: **"Nahrát dokument"**, the upload queue/progress + errors (**"Soubor je větší než 50 MB"**, blocked-type, network-retry), the **preview pending/failed** copy, **"Stáhnout"**, the two pin labels, **"Kopírovat odkaz"** + its **household-not-public** cue **and** the **permanent-link** reassurance (contrast with Poznámky), the **delete confirmations** (soft vs the admin hard-delete that **purges R2**) and the non-empty-folder **cascade warning**, the empty/first-run states, the **grid/list toggle** labels, and the **"Více"** overflow label. Count labels need the **three plural forms** (1 / 2–4 / 5+): *1 dokument · 2 dokumenty · 5 dokumentů*, *1 složka · 2 složky · 5 složek*.

### Tokens & theme

Dark-first, tokenised, light under `.light` — as established. New surfaces to tokenise if the existing scale doesn't cover them: the **document list rows** and **thumbnail grid cards** (hover/selected/pending/failed), the **preview viewer chrome** (a **dark PDF/image surface** — white pages on a dark canvas need a subtle border/inset so they don't halate on OLED), the **upload-queue rows** (progress/success/error), the **file-type icons/badges**, and the **document-in-overlay** dialog. Reuse elevation-by-lightness (not shadows) on dark. Verify AA contrast in **both** themes with real Czech strings and filenames (incl. diacritics).

### Hard problems — address each with a rationale

1. **Many formats, one viewer, safe + dark.** PDF / Office-as-PDF / image / text through safe viewers, a clean download-only fallback, no broken frames — and a legible **dark-mode document surface**.
2. **Async preview that arrives after upload.** The pending→ready (and →failed) swap driven by `/ws`, without layout jank.
3. **Uploading on a phone.** A multi-file queue with progress and per-file errors, one-handed; drag-drop on desktop, picker on mobile.
4. **A deep tree on a 375 px screen** — the documents analogue of Úkoly's "many columns" and Poznámky's deep tree.
5. **The 6th-destination nav / "Více" overflow** (D49) — and its active-state semantics; supersedes v3's admin-only overflow.
6. **A link that is both household-only *and* permanent.** Communicate both without clutter — and *without* Poznámky's "changes on rename" caveat, which is wrong here.
7. **The immutable / no-replace mental model.** The UI offers **upload** and **delete**, never "replace" or "edit file" — make it obvious that a changed file is a **new** document (D41), so no one hunts for an overwrite action.
8. **A preview overlay on Nástěnka that never navigates away**, reusing DocumentView without a second implementation.
9. **A `reader` that looks deliberate** — preview + download stay; upload/edit vanish.

### v4 Definition of done (addendum)

- [ ] Documents browser: desktop **tree + pane (list default + grid/thumbnail toggle)** and mobile **drill-down (list)**, breadcrumb, Czech collation; verified 375 px + 1440 px, both themes.
- [ ] Upload: **"Nahrát dokument"** + drag-drop (desktop) / picker (mobile), **multi-file queue** with progress + per-file errors (too large / blocked / network), client-side pre-check.
- [ ] DocumentView + preview: **PDF / Office-PDF** (named sandboxed viewer), **image**, **text/Markdown**; **download-only + preview-pending + preview-failed** fallbacks; **Stáhnout** always; standalone, reused by the overlay.
- [ ] Organise: create/rename folders, rename/describe documents, move (drag + "Přesunout do…"); **delete confirmation** (soft vs admin hard-delete that **purges R2**) and the **non-empty-folder cascade warning**.
- [ ] Two-scope pin control ("pro všechny" / "jen pro mě") with state; `reader` sees only personal.
- [ ] **"Kopírovat odkaz" = the permanent `/d/{id}` link**, communicated as **household-only, not public** and **permanent (does not change on rename/move)**.
- [ ] `documents.pripnute` widget frame + **preview overlay on Nástěnka (no navigation away)** reusing DocumentView; **no done gesture**.
- [ ] Mobile nav: **4-slot bar (Nástěnka · Úkoly · Okno · Poznámky) + "Více" sheet holding Dokumenty (+ Log for admins)**, with active-state handling; desktop side-nav lists all six. **Supersedes the v3 nav section.**
- [ ] Every screen: loading, empty (root + folder + no-results), **upload**, **preview (pending/failed/download-only)**, error, and `reader` view-only states.
- [ ] All copy in Czech incl. the three plural forms for count labels; AA contrast in both themes with real diacritics and filenames.
- [ ] Accessibility pass (`design:accessibility-review`): keyboard-operable tree, upload, move, pin, and preview controls; touch targets ≥44 px; `prefers-reduced-motion` on overlay/tree/upload transitions; the preview iframe + thumbnails carry labels.

### Do NOT design (v4 additions to the existing list)

- **A "replace file" / overwrite / edit-file-contents affordance** — documents are **immutable**; a changed file is a **new** document (D41). **No version-history / revisions UI** (the audit log is the history).
- **Public / share-link management UI** — household-only; **no "make public", no share tokens, no link-permissions panel** (D33/D42).
- **In-file full-text search, OCR, or content indexing UI** — search is **filename + metadata only** (D46); the search box searches names/description, not contents.
- **In-browser document editing, annotation, or e-signature.**
- **Third-party storage pickers** (Google Drive/Dropbox), **storage-usage / quota dashboards**, and **notification / delivery-preference settings** (there are no notifications — reminders are in-app, and documents don't notify). **[⚠ Reversed in v5 — Home now HAS Web Push notifications; the per-user *Nastavení → Oznámení* panel and the admin-only *Administrace* module are designed in the § v5 addendum below.]**
- **Preview for non-LibreOffice-convertible formats**, and **streaming media players** for large/video files (out of scope for v4).

*Backend for `documents` (`HANDOFF-6-documents.md`, to be written) can be built now from the PRD + `openapi.yaml` 0.5.0; the **Dokumenty frontend and the "Více" nav overflow** are reconciled against this addendum once approved — the same rule v2/v3 used. R2 backup (versioning + mirror bucket) and the Office→PDF worker are backend/ops concerns, not design.*


---

## v5 addendum (2026-08-16) — Administrace (`admin`), push notifications & PWA

> This is the v5 design addendum to `HANDOFF-design.md`. It is a **draft, pending Karel's approval**, written to fold into the addendum series after the v4 section. It covers *what to design and why*; `PRD-v5-admin.md` (decisions **D51–D73**, FRs **FR-P1–5 / FR-S1 / FR-ADM1–6**) governs *what it does*, and `openapi-v5-admin.yaml` (0.6.0) is the data you'll render.

v5 (see `PRD-v5-admin.md`) adds a **seventh module, Administrace (`admin`)** — admin-only, gated exactly like Log — that turns Home into a **Web-Push sender**, plus the app-wide **PWA** groundwork the push channel rides on (installable + **reads-only** offline). Extend the **existing** design system and tokens — same Czech, same dark-default, same function-first bar. Reuse heavily: **dialogs/sheets**, the **filter-bar + table density from the Log browser** (Doručení is its sibling), the **role-gated "reader vs editor vs admin" affordance discipline**, the **widget frame** vocabulary, and the **"Více" overflow** from v4. Where this addendum conflicts with the v1–v4 body, **v5 wins for Administrace / notification-settings / offline screens**, and the **v5 nav note (§8) extends the v4 "Více" overflow** (Administrace joins Log there, admins only).

**One-liner:** an admin composes push notifications — an instant **broadcast**, **trigger** rules that fire off an audited action, and **scheduled summaries** built from module metrics — over **one shared Web Push channel**; every member independently **subscribes and mutes per device** in Nastavení; and Home becomes an **installable app** that stays **readable offline** (writes disabled, no sync).

### What to design

**1. Administrace — the module home (admin-only, lives in "Více").** A single screen with **four tabs**; on mobile the tabs are a top segmented control, on desktop a left rail or top tabs consistent with the rest of the app. Tabs: **Rozeslat** · **Pravidla** · **Souhrny** · **Doručení**. The whole module is `admin`-only (same gate as Log) — a non-admin never sees the nav item; this is **not** a "reader view", it simply doesn't exist for them (unlike Úkoly/Poznámky, which have reader states). The one piece of notification UI that *is* for everyone lives elsewhere (§6, Nastavení).

**2. The notification composer — the centrepiece (shared by Rozeslat / Pravidla / Souhrny).** This is the hard, novel interaction; design it once as a reusable block and specialise per tab. It has three parts stacked (or side-by-side on desktop):
- **Text fields** — **Nadpis** (title) and **Text** (body), free-form Czech.
- **"Vložit údaj"** — an insert-token affordance offering a **catalog-driven** palette. The available tokens differ by context: **Rozeslat** offers only time tokens (`{{date}}`, `{{now}}`); **Pravidla** offers **event tokens** (`{{event.summary}}`, `{{event.actor_label}}`, …) grouped under the triggering action; **Souhrny** offers **metric tokens** (`{{metric.todo.pravedelam_count}}`, …) grouped **by module** with their Czech label + unit. Tokens are **picked from a list, never typed** — the admin should never see a raw key like `reminder.complete` as free text.
- **Živý náhled** — a **live preview card** that renders the title/body as the notification will look (icon + Nadpis + Text), with tokens **resolved to sample values** from the catalog so the admin sees real Czech, not `{{…}}`. Style it to read like an OS notification on the dark surface (it is *not* the real OS chrome — see Do NOT design).
- Below every composer: the **audience picker** (§5) and **"Poslat test"** (sends only to the current admin's own devices — FR-ADM6) next to the primary action.

**3. Rozeslat (broadcast) — FR-ADM1.** The simplest use of the composer: Nadpis + Text + audience + **"Odeslat"**, plus **"Poslat test"**. No persistence, no list — it's a send form. On send, a clear **result** ("Odesláno — dorazí *4 lidem* na *7 zařízení*", using plural forms) and the recipient/subscription counts the API returns. Design the **empty-audience** guard and the **nobody-subscribed** case (composing is allowed, but warn "Zatím nemá oznámení zapnuté nikdo").

**4. Pravidla (trigger rules) — FR-ADM2.** A **list + editor** (reuse the Poznámky/Úkoly list-then-edit pattern):
- **List:** each rule row = name, an **enable/disable** switch, the **human action** it reacts to (Czech, from the catalog — "Když někdo dokončí připomínku", not `reminder.complete`), and its audience. Empty state: **"Zatím žádná pravidla"** + **"Nové pravidlo"**.
- **Editor:** name; **the action picker** — a catalog dropdown grouped by module, each entry a **human Czech phrase** with the raw key shown quietly as secondary text; optional **filters** (module / entity type / level); the **composer** (§2) with event tokens, where the **default body is the event's own Czech summary** shown as placeholder so the admin can leave it blank; **"Sloučit opakování"** (coalescing) with the 60 s default explained in plain words ("stejné akce v krátkém sledu spojí do jednoho oznámení"); **"Upozornit i původce akce"** (the `exclude_actor` inverse — default **on**, since v5 notifies everyone incl. the actor, D66); audience; enable toggle.

**5. Souhrny (scheduled summaries) — FR-ADM3.** A **list + editor**, sibling to Pravidla:
- **List:** name, enable switch, a **human schedule** ("Každý den v 8:00", "Každý všední den ve 20:00", "1. v měsíci v 9:00"), audience. Empty state + **"Nový souhrn"**.
- **Editor:** name; a **schedule builder** — a **time picker** (`HH:MM`, Prague) + a **day pattern** control (**Každý den · Všední dny · Víkend · vybrané dny · N-tého v měsíci**) as a chip/segmented row that stays one-handed at 375 px; the **composer** (§2) with **metric tokens** from the catalog; audience; enable toggle. Make the two worked examples (08:00 and 20:00 from the PRD) trivial to build. **Day-of-month accepts 1–31, not a 28 cap** (PRD D74): a day the month lacks (29–31) clamps to the month's last day (matching events' D19), so surface a quiet "posune se na poslední den kratšího měsíce" hint when 29–31 is picked. *(Reconciliation: the current design file caps this input at 28 — change to 1–31 with the clamp hint.)*

**6. The audience picker (shared control).** A compact control used on every composer: **Všem** (default), **Podle role** (multi-select `admin`/`editor`/`reader`), **Vybraným lidem** (member multi-select). Show a **live recipient echo** ("Odejde *všem* · *4 lidem*"). Keep it a single affordance that expands, not a wizard.

**7. Doručení (delivery log) — FR-ADM5.** The **operational** log, an explicit sibling of the Log browser — **reuse its filter bar + dense table** wholesale. Rows: time, kind (**Rozeslané · Pravidlo · Souhrn · Test**), recipient, and a **status** chip (**Odesláno · Nedoručeno · Vypršelo**). Filters: kind, status, rule, date range, recipient. Keyset pagination, newest-first. This is debugging ("dorazila ranní osmá? komu?"), so favour **scannability over decoration**. Make clear in copy this is a **best-effort delivery record**, not the audit log.

**8. Nastavení → Oznámení — the per-user panel (EVERY role, incl. `reader`).** This is the only notification UI outside the admin module, and it is **per-device**. Design:
- **The subscribe control** — a primary **"Zapnout oznámení na tomto zařízení"**. Because the **browser permission prompt is OS-owned and one-shot**, design a **priming step** so the prompt fires only on intent (never on load). Handle every permission outcome (§States): granted → subscribed; dismissed → back to the offer; **blocked → a recovery callout** ("Oznámení jsou blokovaná v prohlížeči") that guides to browser settings, since Home **cannot** re-prompt a blocked device.
- **The mutes** — a **master** switch (**"Oznámení"**) and, under it, **three category** toggles: **Rozeslaná oznámení**, **Upozornění na akce**, **Souhrny** (D53a). Master off greys the categories.
- **"Poslat zkušební oznámení"** — a self-test so a member can confirm this device works.
- Make **"toto zařízení"** unmistakable — a member with a phone + laptop manages each separately. (Cross-device management is **not** in v5 — Do NOT design.)

**9. PWA install affordance — D67.** Home is now installable. Design an **unobtrusive "Nainstalovat aplikaci" / "Přidat na plochu"** entry (e.g. in Nastavení and/or a dismissible one-time hint) that triggers the platform install flow where available, and simply hides where it isn't. Specify the **manifest**-driven identity: app name, **icon set** (maskable + standard), **`display: standalone`**, and **dark theme/background** colours matching the token theme (so the splash/standalone chrome is dark, not white).

**10. App-wide offline, read-only — D71/D73.** This touches **every** screen designed in v1–v4, so define **one consistent, calm offline treatment**, not per-module hacks:
- A **persistent, quiet offline indicator** (a slim bar/badge — **neutral/muted, not error-red**; being offline is a state, not a failure) reading **"Jste offline — zobrazená data mohou být starší"**.
- **Every write control disables** with one consistent message on interaction: **"Změny nelze uložit offline"** (buttons for create/move/edit/complete/pin/upload go disabled, not hidden — the user should still see what they *could* do).
- **Online-required surfaces** show their own inline state: **login** ("Přihlášení vyžaduje připojení"), **document preview/download and upload** ("Náhled/stažení vyžaduje připojení" — documents are online-only, D73). Reads of boards/cards/events/notes/document-metadata render from cache normally.
- **No "queued changes", no sync status, no conflict UI** — offline is strictly read-only (Do NOT design).

**11. The notification envelope (light touch).** You don't design the OS notification chrome, but you **do** specify the **content conventions** every module reuses: the **icon/badge** (a Home mark that reads at notification size, mono badge for Android), and the click-target convention (a notification opens the in-app `url` for its `module`/`type`). Provide the icon assets and a one-line-per-module note on what a click should open.

### States — design each (loading, empty, error, reader, + push/offline-specific)

- **Administrace tabs:** loading; **empty** (no rules / no schedules — inviting "vytvořte první…", not blank); list; editor; **save error** and **validation** (unknown action/metric key or bad token → `422`, surfaced on the field).
- **Composer:** empty; with live preview; **default-body** placeholder (Pravidla) so blank ≠ broken; token-resolves-to-sample in preview.
- **Broadcast:** ready; sending; **sent** result with counts; **empty audience** guard; **nobody subscribed** warning.
- **Audience:** all / role-subset / specific people; recipient echo; empty-selection error.
- **Doručení:** loading; empty (nothing sent yet); rows with all three status chips; filtered-no-results.
- **Nastavení → Oznámení:** **not subscribed** (default offer); **subscribed on this device**; **permission dismissed**; **permission blocked** (recovery callout); **push unsupported** (browser without Push API → explain, no dead button); master-off; per-category off; **test sent** confirmation.
- **Offline (app-wide):** the offline indicator; disabled write controls with the standard message; online-required inline states for login, document preview/download, upload.
- **Reader:** **no Administrace** at all (nav item absent), **but the full Oznámení panel** — a reader subscribes and mutes like anyone. Make the split obvious and deliberate.

### Czech UX copy (use `design:ux-copy`, in Czech)

Own the wording for: module + tab names (**Administrace · Rozeslat · Pravidla · Souhrny · Doručení**); the composer (**Nadpis · Text · Vložit údaj · Živý náhled · Poslat test · Odeslat**); the **human action phrases** for each audit action key (this is real work — map `card.move`, `reminder.complete`, `document.create`, … to natural Czech triggers) and the **human schedule phrases**; **Sloučit opakování** and its plain-words explanation; **Upozornit i původce akce**; the audience labels (**Všem · Podle role · Vybraným lidem**) and the recipient echo; the delivery statuses (**Odesláno · Nedoručeno · Vypršelo**) and kinds (**Rozeslané · Pravidlo · Souhrn · Test**); the settings panel (**Zapnout oznámení na tomto zařízení · Oznámení · Rozeslaná oznámení · Upozornění na akce · Souhrny · Poslat zkušební oznámení**); the **permission-blocked** recovery copy and the **push-unsupported** copy; the **install** affordance (**Nainstalovat aplikaci / Přidat na plochu**); and the **offline** set (**Jste offline… · Změny nelze uložit offline · Náhled vyžaduje připojení · Přihlášení vyžaduje připojení**). Count labels need the **three plural forms** (1 / 2–4 / 5+): *1 příjemce · 2 příjemci · 5 příjemců*; *1 zařízení · 2 zařízení · 5 zařízení*; *1 oznámení · 2 oznámení · 5 oznámení*; *1 pravidlo · 2 pravidla · 5 pravidel*.

### Tokens & theme

Dark-first, tokenised, light under `.light` — as established; elevation-by-lightness, not shadows. New surfaces to tokenise if the existing scale doesn't cover them: the **composer** (the text area, the **token chips**, and the **notification preview card** — an OS-notification-like surface on dark, with a subtle border/inset so a light-ish preview doesn't halate on OLED); the **audience picker** and **role/member multi-selects**; the **schedule builder** (time + day-pattern chips); the **settings toggle rows** and the **permission-blocked callout** (a **warning**, not an error — distinct from destructive red); the **offline indicator** (a **calm neutral/muted** token, explicitly *not* the error/destructive colour); and the **delivery status chips** (Odesláno / Nedoručeno / Vypršelo — map to success / warning / muted, all AA on dark). Verify AA contrast in **both** themes with real Czech strings, including the resolved-token preview text.

### Hard problems — address each with a rationale

1. **The composer + live preview.** Free-form Czech interleaved with **catalog-picked tokens**, a **live preview that resolves tokens to sample values**, working one-handed at 375 px, with three different token palettes by context. This is the module's payoff screen — make inserting a metric feel like inserting a word, and make the preview trustworthy.
2. **Action keys → human choices.** An admin must pick **"Když někdo dokončí připomínku"**, never `reminder.complete`. Design the catalog grouping + phrasing so developer keys never surface as the primary label, while staying honest about what fires.
3. **The permission gauntlet.** The browser prompt is one-shot and OS-owned; a **blocked** device can't be re-prompted. Design the **priming step** (prompt only on intent) and a **blocked-recovery** path that points to browser settings — and the **unsupported** fallback — so no one hits a dead "Zapnout" button.
4. **Per-device, not per-account.** Subscriptions and mutes are **this device's**. Make that legible without a device-management console (which v5 doesn't have) — the panel speaks for the device you're on.
5. **App-wide offline that feels intentional, not broken.** One calm indicator, consistently **disabled** (not vanished) write controls, and honest online-required states for login / documents — across every existing module. The bar is the v1 `reader`/empty-state bar: deliberate, never a broken editor.
6. **Silent updates = nothing to design.** Per D72 there is **no update prompt**; call this out so no one builds a "nová verze" toast.
7. **Deliveries vs the audit log.** Doručení looks like Log but means something different (best-effort delivery, prune-able). Reuse the chrome, differentiate the meaning in copy so an admin doesn't read it as the source of truth.
8. **Reader has settings but no module.** The one notification surface a reader sees is Nastavení → Oznámení; Administrace is absent for them. Design the split so it reads as intentional, matching how `reader` is handled elsewhere.

### v5 Definition of done (addendum)

- [ ] **Administrace** shell: four tabs (Rozeslat · Pravidla · Souhrny · Doručení), admin-only (no reader state — absent for non-admins), verified 375 px + 1440 px, both themes.
- [ ] **Composer** block: title/body, **catalog-driven "Vložit údaj"** (time / event / metric palettes by context), and a **live preview resolving tokens to sample values**; reused across all three send tabs; **default-body-as-summary** placeholder in Pravidla.
- [ ] **Rozeslat**: send form + **Poslat test** + audience; result with plural-correct recipient/device counts; empty-audience and nobody-subscribed states.
- [ ] **Pravidla**: list + editor with the **human action picker**, filters, composer (event tokens), **Sloučit opakování** (60 s default explained), **Upozornit i původce akce** (default on), audience, enable switch, empty state.
- [ ] **Souhrny**: list + editor with the **schedule builder** (time + day-pattern, Prague), composer (metric tokens), audience, enable switch; the 08:00 and 20:00 examples are trivial to build.
- [ ] **Doručení**: Log-style filter bar + dense table, kind + status chips, keyset paging; framed as operational, not audit.
- [ ] **Nastavení → Oznámení** (all roles incl. reader): per-device subscribe with **priming + granted/dismissed/blocked/unsupported** states, **master + three category** mutes, **Poslat zkušební oznámení**; "toto zařízení" unmistakable.
- [ ] **PWA install** affordance + manifest identity (name, maskable icons, `standalone`, dark theme/splash).
- [ ] **App-wide offline** treatment: one calm indicator, consistently disabled write controls (**Změny nelze uložit offline**), online-required states for login + document preview/download/upload; no queued/sync/conflict UI.
- [ ] **Nav**: Administrace joins Log in the **"Více"** sheet **for admins only** (regular 4-slot bar unchanged); desktop side-nav lists it for admins; active-state handling as v4. **Extends the v4 nav section.**
- [ ] **Notification envelope** assets: Home notification icon/badge (maskable + mono) and a per-module click-target note.
- [ ] Every screen: loading, empty, error, and the push/offline-specific states above.
- [ ] All copy Czech incl. the three plural forms and the **action-key → Czech phrase** map; AA contrast in both themes with real diacritics.
- [ ] Accessibility pass (`design:accessibility-review`): keyboard-operable composer/token-insert, audience + schedule pickers, toggles; **touch targets ≥44 px**; `prefers-reduced-motion` on any composer/preview/sheet transitions; the preview card and status chips carry text labels (not colour-only).

### Do NOT design (v5 additions to the existing list)

- **The OS/browser permission dialog** and **the OS-rendered notification chrome** — platform-owned; we control only the **envelope content + icon** and the in-app preview card.
- **Any update prompt / "nová verze" toast** — updates are **silent** (D72).
- **Offline write UI** — no **queued-changes tray, sync status, or conflict-resolution** screens; offline is **read-only** (D71).
- **Offline document preview/download** — documents are **online-only** (D73); design the online-required state, not an offline viewer.
- **Cross-device / "manage my devices"** management — the settings panel is **this device only** in v5.
- **User-authored rules** — members only **subscribe + mute**; rule/schedule/broadcast authoring is **admin-only** (no member-facing rule editor).
- **Email / SMS / other channel settings** — **Web Push only**.
- **A superadmin UI or role management** — the gate is plain `admin` (**no new tier**, D62); no role-editing screens here.
- **Lock-screen redaction / sensitivity controls** — none (D70).
- **Per-user quiet-hours, snooze, or member-set schedules** — out of scope for v5.

*Backend for `admin` + `platform/push` + `platform/scheduler` + `platform/pwa` (`HANDOFF-7-admin.md`, to be written) can be built from `PRD-v5-admin.md` + `openapi-v5-admin.yaml` 0.6.0; the **Administrace frontend, the Nastavení → Oznámení panel, the app-wide offline treatment, and the "Více" overflow update** are reconciled against this addendum once approved — the same rule v2/v3/v4 used. VAPID keys, the outbox tailer, the scheduler, and PWA cache mechanics are backend/ops concerns, not design.*

---

## v6 addendum (2026-08-17) — Finance (`finance`), and the app it replaces

> This is the v6 design addendum to `HANDOFF-design.md`, written to fold into the addendum series after the v5 section. It covers *what to design and why*; `PRD.md` **§V6-1…§V6-12** (decisions **D81–D98**, **FR-F1–F10**) governs *what it does*, and `openapi.yaml` **0.8.0** (tag `finance`) is the data you'll render. Build brief: `HANDOFF-8-finance.md`.
>
> **This round is unusual: the screen already exists.** v6 clones a **running service** — `fin.tilcer.cz` — into Home as an eighth module, and then retires it. The `ws-tilcer-fin` repo (`frontend/src/`) holds a working, liked UI: `components/FlowViz.tsx`, `MonthRow.tsx`, `SummaryStrip.tsx`, `Legend.tsx`, `MonthFormModal.tsx`, and its own `styles.css` token set. **Look at it before you draw anything.** Your job is not to invent this module — it is to decide, screen by screen, what to carry across intact and what must change to become a Home screen (Czech, dark-first tokens, shadcn anatomy, one-handed at 375 px). Where this addendum conflicts with the v1–v5 body, **v6 wins for Finance**; the **v6 nav note extends the v4/v5 "Více" overflow** (Finance joins it **for everyone**, not admin-gated).

**One-liner:** once a month, two people enter two incomes and four percentages, and the app shows — legibly, at a glance — how that money splits into two personal accounts, one joint operational account ("Kandy"), and two pooled savings pots. The rest of the module exists to make sure a month never goes unrecorded.

### What to design

**1. Finance — the module home (`/finance`, all roles, lives in "Více").** One screen, no tabs: page header → **summary strip** (§7) → **legend** (§2) → **month list** (§2). This is a **once-a-month destination**, which changes the design brief in a specific way: nobody builds muscle memory here. Every control must be self-explanatory to someone who last saw the screen five weeks ago. Favour explicit labels over learned iconography, and put the primary action (**"Přidat měsíc"**) where it is impossible to miss.

**2. The month list + the allocation bar (FR-F1).** `fin`'s `MonthRow` pattern is good and should survive: a **collapsible row per month**, newest first, showing the month label, a **mini stacked allocation bar**, total income, and the amount that went to savings; tapping expands it to the flow visualisation (§3). Two things to solve that `fin` did not have to:

- **The list is long now.** After the migration the list opens with **every month `fin` ever held** — a couple of years of real rows, not the three of a fresh app. Decide the treatment: year group headers, a sticky year marker, a "load more", or simply a long scroll with a strong sticky header. Design it with ~30 realistic rows, not 3.
- **The bar is a chart.** It follows the chart rules in §*Tokens & theme* below: four segments in a **fixed order**, a **2 px surface gap between segments** (not a hairline border), rounded 4 px data-ends anchored to the bar's ends, a **legend always present**, and hover/press revealing the segment's Czech label and value. It is the one place all four buckets sit edge-to-edge, so it is the palette's hardest test.

**3. The flow visualisation — the payoff screen (FR-F3).** `fin`'s three-stage `FlowViz` is the reason this app is nicer than a spreadsheet, and it is the piece most worth carrying across faithfully in *structure* and re-dressing in *Home's* tokens. Three stages with connectors:

1. **Příjem** — Kája's and Andy's incomes.
2. **Osobní** — what each person keeps (`personal_*`), each card carrying a **"Zbytek → Kandy"** line (`to_operational_*`).
3. **Společné účty** — **Provozní účet (Kandy) · potřeby** (`needs`, annotated "přijato ⟨operational_received⟩, spoření odchází"), **Zábavné spoření** (`fun_savings`), **Nezábavné spoření** (`no_fun_savings`).

Below it, the reconciliation note in Czech: *"Potřeby pohlcují zaokrouhlení, takže součty za osobu i za účet sedí přesně na ⟨celkový příjem⟩ Kč."*

**The hard part is 375 px.** `fin`'s layout is three columns with arrows between them — desktop-shaped. Decide how it reflows: three stacked bands with vertical connectors, a horizontal scroll-snap of stages, or a genuinely different mobile composition. What must survive the reflow is the **sense of flow** — that money moves left-to-right (or top-to-bottom) through stages and arrives somewhere. A vertical list of nine numbers has lost the whole point.

**4. Přidat / Upravit měsíc (FR-F2 / FR-F4 / FR-F6).** A modal/sheet with: **month picker** (already-recorded months disabled when adding), **two income fields**, **four rate fields**, and a **live computed preview** of the resulting split. Rates pre-fill from the most recent month, else 20/60/10/10. The interesting constraint: **the four rates must total exactly 100**, and submit is blocked until they do. Design that as a *helpful running total*, not a validation slap — show the current sum and the remainder ("zbývá 5 %") continuously, and consider whether the last field should auto-balance. The preview is what makes the form worth using: someone changing a rate should see the split move.

**5. Smazat měsíc — and it is permanent (FR-F5, D87).** Unlike Poznámky and Dokumenty, which archive, **Finance deletes for real**. The confirm dialog must say so plainly and name the month. This is a deliberate exception to a convention the rest of the app has taught the user, so the copy carries the whole weight — design it to be read, not dismissed. *(The data is still recoverable from the Log by an admin, but do not offer that as an in-module affordance; it is not an undo.)*

**6. The `finance.rozpocet` Nástěnka widget (FR-F7, D88) — two states, and the second one matters more.** Narrow, all roles.

- **Month recorded** → the headline numbers: total income, what each person keeps, what stays as needs, what went to savings. Tapping opens that month in Finance.
- **Month NOT recorded** → **"Zadat ⟨srpen 2026⟩"**, linking to the add form. Design this as a **prompt, not an empty state**: the app's actual failure mode is a month nobody entered, and this widget is the only thing on the household's landing page that will notice. It should read as a small, friendly obligation — not as a widget with nothing to say.

**7. The summary strip — stat tiles, not charts (FR-F1).** `fin` carries four: latest month's total, saved to date, fun/no-fun split, average monthly income. Keep something in that spirit and design them as **stat tiles** — a big value, a quiet label, one line of context — with the value in the **mono figure** treatment and no decorative sparkline. A stat tile earns its place by answering one question instantly; four tiles that each answer half a question are worse than two that answer one each. Prune if you disagree with `fin`'s four.

**8. Kája and Andy need visual identity — and it must not collide with the buckets.** `fin` gave each person a colour (`--kaja` warm rose, `--andy` cool blue) and an avatar. Home has **no per-person colour token** and no avatar system. This is a real decision, and it is constrained: the four **bucket** colours already occupy blue, green, amber and violet, so a person colour taken from the same family will be read as a bucket. Options to weigh: initials-in-a-circle with a **neutral** surface (identity from the letter, not the hue), a colour drawn from a deliberately different token family (the label tokens `--l-*`), or no person colour at all — just the names, which in a two-person household may be enough. Whatever you choose, the same treatment must work in the flow viz stage 1, the stage 2 cards, and the form's two income fields.

**9. The negative `needs` case — show `0 Kč` with a footnote (PRD §V6-4 FR-F3).** When the `operational` rate is 0, `needs` can compute to −1 or −2 Kč from rounding. The value is correct and must not be clamped in the data; the **UI** shows `0 Kč` with a quiet footnote (*"zaokrouhlení: −2 Kč"* or similar). Design that footnote so it reads as precision, not as an error.

**10. Nav — Finance joins "Více", for everyone.** No new tab: the four daily thumb tabs (Nástěnka · Úkoly · Okno · Poznámky) are unchanged. Finance sits in the overflow sheet beside Dokumenty, **with no `adminOnly` gate** — unlike Log and Administrace. Icon: a wallet-family mark consistent with the existing lucide set. Desktop side-nav lists it for everyone. **Extends the v4/v5 nav sections.**

**11. Reader state.** A `reader` sees **the whole module** — list, flow viz, widget, summary strip — including both household incomes (PRD §V6-3, accepted). What they do not get is any write control. Use the established v1 reader discipline: controls **disabled and visible**, not hidden, so the screen reads the same for everyone.

### States — design each (loading, empty, error, reader, + finance-specific)

- **Month list:** loading (skeleton rows); **empty** — the genuinely fresh state, "Zatím žádné měsíce" + "Přidejte první měsíc a uvidíte rozdělení příjmů"; **long list** (~30 rows, the post-migration reality); row collapsed / expanded; error + retry.
- **Flow viz:** normal; a month where one income is **0** (one person had no income — the stage-1 card must not look broken); a month with **extreme rates** (100/0/0/0 — three buckets at zero, the bar has one segment); the **negative-`needs`** case (§9).
- **Form:** add vs edit; rates summing to ≠ 100 (blocked, with the running remainder); duplicate month (**"Tento měsíc už je zadaný."**); month picker with recorded months disabled; live preview updating; saving; save error.
- **Delete:** the permanent-delete confirm; deleting.
- **Widget:** recorded; **not recorded** ("Zadat ⟨měsíc⟩"); loading.
- **Reader:** full read, every write control disabled and visible.
- **Offline** (v5 D71): months render from cache read-only; write controls disabled with the standard **"Změny nelze uložit offline"**. Nothing module-specific — Finance has no bytes.

### Czech UX copy (use `design:ux-copy`, in Czech)

The vocabulary is **fixed in the PRD (§V6-7)** and must be used verbatim so the page, the widget, the metric labels and the notification tokens all say the same words: **Finance · Měsíce · Příjem · Osobní · Provozní účet (Kandy) · Potřeby · Zábavné spoření · Nezábavné spoření · Sazby · Celkový příjem · Do spoření · Zbytek → Kandy · Přidat měsíc · Upravit měsíc**. **Keep "Kandy"** — it is the household's own name for the joint account, and translating it away would make the app read as somebody else's.

Own the wording for everything else: the empty and prompt states; the rate-sum helper ("zbývá 5 %" / **"Sazby musí dát dohromady 100 %."**); the duplicate-month error; the **permanent-delete** confirm (the one piece of copy in this module that has to stop someone); the reconciliation footnote; the rounding footnote; the stat-tile labels and their one-line context; and the widget prompt. Count labels need the **three plural forms**: *1 měsíc · 2 měsíce · 5 měsíců*. Money is Czech-formatted throughout — space thousands separator, **" Kč"** suffix, no decimals (everything is whole koruny).

### Tokens & theme

Dark-first, tokenised, light under `.light` — as established; elevation by surface lightness, not shadows. `fin`'s own tokens (`--c-personal`, `--c-needs`, `--c-fun`, `--c-nofun`, `--kaja`, `--andy`, its radii and its Hanken Grotesk / JetBrains Mono pairing) **do not come across** — Home's system governs. What Finance needs from it:

- **Money figures** — a **tabular/mono numeric** treatment so columns of koruny align down the list and in the flow viz. Home has `--mono`; specify the figure style (tabular-nums) as a token-level rule, since this is the first module that shows columns of numbers.
- **The four bucket colours** — see the computed finding below. This is the module's one genuinely risky token decision.
- **The stacked bar** — a 2 px **surface-coloured** gap between segments (the gap does the separating work colour cannot), 4 px rounded ends, and a minimum segment width so a 2 % bucket is still visible and hoverable.
- **Stat tiles** and the **flow-viz account cards** — both are new card densities; check they sit correctly against `--s1`/`--s2` in both themes.

#### The four-bucket palette — a computed finding, decide this before you design

Home already ships a five-slot categorical palette (`--c1`…`--c5`) in `theme/globals.css`, currently consumed only by the Log's stats bars. Its hues line up almost exactly with `fin`'s bucket semantics (blue 256 / green 150 / amber 78 / violet 300 vs `fin`'s blue / teal / amber / violet), so the obvious move is `--c1` osobní · `--c2` potřeby · `--c3` zábavné · `--c4` nezábavné. **That obvious move fails validation.** Run against the data-viz palette validator (OKLab ΔE under Machado protan/deutan simulation), the current tokens give:

- **Order `c1,c2,c3,c4`, adjacent pairs (the stacked bar):** **FAIL** — `c2↔c3` (green↔amber) ΔE **4.4** protan, and in dark also **12.3** under *normal* vision, below the 15 floor. Green and amber sitting edge-to-edge are hard to tell apart for everyone, not just for dichromats.
- **All pairs (the flow viz and the legend, where all four are read together):** **FAIL in every ordering and every 4-of-5 subset** — `c1↔c4` (blue↔violet) is ΔE **0.8** under protanopia and **9.2** under normal vision. Reordering cannot fix this; the two hues are simply too close in lightness and chroma.

> **RESOLVED 2026-08-17 (Karel, during the v6 build): PATH A.** The shared `--c1`…`--c5` values are **unchanged** — the Log's stats bars keep their current colours — and Finance's four buckets are assigned `c1` osobní · `c2` potřeby · `c4` zábavné · `c3` nezábavné through four aliases in `theme/globals.css`:
> `--fin-personal: var(--c1)` · `--fin-needs: var(--c2)` · `--fin-fun: var(--c4)` · `--fin-nofun: var(--c3)`.
> They are aliases rather than values, so the `.light` block re-steps them for free and **no new hex/oklch value is introduced anywhere**. Because Path A's all-pairs weakness remains (`c1↔c4` ΔE 0.8 protan in every ordering), **secondary encoding is mandatory and is implemented as shipped**: the O/P/Z/N mono marks, a distinct swatch shape per bucket (square / circle / diamond / square), the 2 px surface-coloured gaps in the bar, an always-present legend, direct labels on every flow-viz card, and an `aria-label` naming each bucket and its value — colour never carries a bucket on its own. Path B stays documented below as the option not taken.

Two ways forward. **Decide which, and say why, in the design system doc:**

- **Path A — keep the tokens, constrain the usage.** Assign the buckets in the order **`c1` osobní · `c2` potřeby · `c4` zábavné · `c3` nezábavné**, which **passes** the adjacent-pair and normal-vision checks in **both** themes (worst adjacent ΔE 13.8 dark / 18.2 light). Then make **secondary encoding mandatory** everywhere the four appear together — direct labels on the flow-viz cards, the 2 px gaps in the bar, the always-present legend, and a distinct mark or icon per bucket — because the all-pairs weakness remains. Cheapest, and touches no shared token. Cost: it swaps `fin`'s amber-is-fun / violet-is-no-fun association.
- **Path B — re-step the palette.** Keep Home's four hues and move only lightness and chroma so all four separate. This candidate **passes every check, all pairs, in both themes**, and is offered as a starting point rather than a decree:
  - dark `--c1..--c4` = `oklch(0.55 0.11 256)` `oklch(0.67 0.11 150)` `oklch(0.65 0.15 78)` `oklch(0.67 0.13 300)` → `#4473b1 #60a871 #c08100 #a281d9`
  - light `--c1..--c4` = `oklch(0.45 0.11 256)` `oklch(0.62 0.11 150)` `oklch(0.50 0.11 78)` `oklch(0.62 0.11 300)` → `#275591 #519962 #855a00 #9076be`
  - **This is a design-system change, not a module change** — it repaints the Log's stats bars too, and it makes the dark marks less bright than Home's current character. Karel's call.

Whichever path: **run the validator on the final values before handing back** — `scripts/validate_palette.js` from the `dataviz` skill, `--mode dark --surface` Home's `--s1`, then `--mode light`. Do not settle this by eye. (Separately, and independent of v6: the current dark `--c2` sits at chroma **0.09**, below the 0.10 floor at which a hue starts reading as grey, and all five dark slots sit above the validator's dark lightness band — worth a note in the system doc even if nothing changes.)

### Hard problems — address each with a rationale

1. **The four-bucket palette.** The finding above is computed, not aesthetic. Pick a path, state the trade, and validate the result. This is the one decision that can make the whole module unreadable for a colourblind reader — and, at the current `c2↔c3` numbers, mildly unreadable for everyone.
2. **The flow visualisation at 375 px.** `fin`'s three-column composition is desktop-shaped. Reflow it so the *sense of money moving through stages* survives — that sense is the entire reason the screen exists, and a stacked list of nine labelled numbers has thrown it away.
3. **Two people, four buckets, one palette.** Give Kája and Andy identity that no one reads as a fifth category (§8).
4. **Four numbers that must total 100.** Make the constraint feel like assistance (running remainder, maybe auto-balance on the last field) rather than a form that refuses to submit and won't say why.
5. **A permanent delete in an app that archives.** Finance is the exception (D87). Design the confirm so the difference lands, without turning an ordinary monthly correction into a scary ritual.
6. **The "Zadat ⟨měsíc⟩" widget state.** The module's single most valuable pixel: it is the only place the household is told a month is missing. Design it as a prompt with a clear action, not as a polite empty state.
7. **A once-a-month screen has no muscle memory.** Every affordance must survive a five-week gap. This is the opposite of Úkoly's design problem and should visibly change your choices.
8. **Money density in Czech.** `1 234 567 Kč` with space separators, in a four-segment bar and a nine-value flow, at 375 px, with diacritics in every label. Tabular figures and honest truncation rules, tested with the real magnitudes this household uses — not `100 000`.
9. **A list that arrives full.** The migration seeds years of history on day one. The empty state will be seen once, ever; the long list is the real screen. Design in that order.

### v6 Definition of done (addendum)

- [ ] **Finance page**: header, summary strip, legend, month list (collapsed + expanded), verified at 375 px and 1440 px in both themes, with ~30 realistic Czech months.
- [ ] **Allocation bar**: fixed segment order, 2 px surface gaps, rounded ends, minimum segment width, hover/press value, legend always present, direct labels where they fit.
- [ ] **Flow visualisation**: all three stages with connectors, at both breakpoints, with the reflow rationale stated; reconciliation footnote in Czech.
- [ ] **Add/edit form**: month picker (recorded months disabled), two incomes, four rates with a **running remainder**, live split preview, blocked submit until the rates total 100, duplicate-month error.
- [ ] **Permanent-delete confirm**, naming the month and stating that it cannot be undone.
- [ ] **`finance.rozpocet` widget**: both states, with the "Zadat ⟨měsíc⟩" prompt designed as a call to action.
- [ ] **Summary stat tiles** with mono figures and no decorative sparklines.
- [ ] **Person identity** for Kája and Andy that does not read as a fifth bucket, working in the flow viz and the form.
- [ ] **Palette path chosen (A or B), justified, and validated with the script in both themes** — output pasted into the design system doc.
- [ ] **Nav**: Finance in the **"Více"** sheet **for everyone** (no admin gate), desktop side-nav for everyone, active state as v4/v5. **Extends the v4/v5 nav sections.**
- [ ] Every screen: loading, empty, long-list, error, reader, offline, and the edge cases above (zero income, 100/0/0/0, negative `needs`).
- [ ] All copy Czech, using the PRD §V6-7 vocabulary verbatim, with the three plural forms for month counts and Czech money formatting.
- [ ] Accessibility pass (`design:accessibility-review`): the bar and every bucket carry a **text label, never colour alone**; keyboard-operable list expansion, form and rate fields; touch targets ≥44 px; `prefers-reduced-motion` on the row expand and any flow animation; AA contrast in both themes with real diacritics and real magnitudes.

### Do NOT design (v6 additions to the existing list)

- **Charts beyond the allocation bar and the stat tiles** — no income trend line, no savings-over-time area, no month-vs-month comparison. v6 has **no reporting** (PRD §V6-2), and a trend chart would imply data the module does not offer.
- **Transactions, categories, budgets-vs-actual, bank sync, forecasting** — the module records **one row per month**, nothing finer.
- **A currency switcher or any non-CZK affordance** — whole koruny only.
- **Archive / trash / undo for a deleted month** — delete is **permanent** (D87). No restore UI, no "recently deleted".
- **An import, export, or migration screen** — the historic months arrive as a database seed (D91) and are simply *there* on first load. There is nothing for a user to import.
- **A "we've moved" interstitial, redirect page, or migration banner for `fin`** — there is **no redirect** (D96); `fin.tilcer.cz` is switched off and the two users are told directly. Nothing to design.
- **Editing any derived value** — only the two incomes and the four rates are inputs; the nine split values are computed and read-only everywhere.
- **Per-person savings attribution** — fun and no-fun savings are **pooled**, never split per person. Do not design a UI that implies otherwise.
- **Rate history, per-month rate comparison, or a "why did this change" view** — the Log answers that, for admins.
- **Any admin-only surface inside Finance** — the module has none (D84); there is no settings tab, no per-user configuration.

*Backend + frontend for `finance` (`HANDOFF-8-finance.md`) can be built from `PRD.md` §V6 + `openapi.yaml` 0.8.0; the **Finance page, the flow visualisation, the widget, and the "Více" nav update** are reconciled against this addendum once approved — the same rule v2–v5 used. The `fin` data migration, its verification, and the decommissioning runbook are backend/ops concerns, not design.*

---

## v7 addendum (2026-08-18) — Zahrada (`garden`)

> This is the v7 design addendum to `HANDOFF-design.md`, written to fold into the addendum series after the v6 section. It covers *what to design and why*; `PRD.md` **§V7-1…§V7-11** (decisions **D101–D132**, **FR-G1–G17**) governs *what it does*, and `openapi.yaml` **0.9.0** (tag `garden`) is the data you'll render. Build brief: `HANDOFF-9-garden.md`.
>
> **This is the biggest design round since v1.** Eight routes, eleven entities, and the first Home module whose primary screen is a *planning tool* rather than a list. There is no reference UI — unlike v6, nothing exists to look at. Where this addendum conflicts with the v1–v6 body, **v7 wins for Zahrada**; the **v7 nav note extends the v4/v5/v6 "Více" overflow** (Zahrada joins it **for everyone**, like Finance).
>
> **One context fact that should change your choices more than any other:** this module is used **outdoors, on a phone, in sunlight, with dirty hands, and often with no signal**. Home is dark-first by decision; a garden in June is the one place that default is actively wrong. Solve for the light theme here as a first-class case, not as a conversion of the dark one — and remember that writes are disabled offline (v5 D71), which is why **print (§11) is a designed deliverable in this round, not a stylesheet afterthought**.

### What to design

**1. Plán — the planner (`/zahrada/plan/{rok}`). The module's centre of gravity.** Up to **~15 beds** (PRD §V7-1 scale target), so this is a single grid of **bed cards**, not a two-pane workspace and not a virtualised table. Each card shows the bed's code and name, what is planted in it this season, how much of its area is used, and a **warning badge**. Design the card at three fullnesses: empty, two crops, six crops.

The interaction that matters is **putting a crop in a bed**, and it must work with a thumb. Drag-and-drop is desktop thinking and fails outdoors; design a **pick-then-place** or **card → "Přidat výsadbu" → crop picker sheet** flow as the primary path, and treat drag as an enhancement if you want it at all. The crop picker is used forty times in a planning session — it needs search, recently-used, and the crop's family visible at pick time, because family is what the warnings are about.

**2. Kontrola plánu — eleven checks that must not become wallpaper.** This is the hardest UX problem in the module, harder than any layout in it. The check returns up to eleven kinds of warning at four severities (`error` · `warn` · `info` · `tip`), each dismissible per season with a note. A panel of yellow triangles is read twice and ignored forever.

Design decisions to make explicitly:

- **Two surfaces, one truth.** A compact badge inline on each bed card (what's wrong *here*), and the full panel (what's wrong *across the garden* — C3 rotation, C5 workload, C6 concentration, C8 succession are invisible from inside a single bed). They must not disagree, and the panel must make clear which findings are garden-wide.
- **Severity has to be legible without colour** — see §*Tokens & theme*.
- **Dismissal is a first-class action, not a close button.** "Ignorovat" with an optional note (*"vím, letos to risknu"*), visibly recorded, restorable. A dismissed warning should remain findable — design the "dismissed" state, not just its absence.
- **`tip` is not a warning.** C8's "legume before a heavy feeder" is *praise*. If tips render in the same visual language as errors, the panel teaches people that everything in it is noise.

**3. The "chybí historie" state — design the check that cannot run.** Rotation (C3) and feeder succession (C8) read **closed seasons only**, and there is **no historical back-fill** (D120). On a fresh install, and through the whole first year, they have nothing to work with. The panel must say so — *"rotaci zatím nelze zkontrolovat, chybí historie"* — and must **not** render as a passing check. This is a small piece of UI carrying an honest message about the tool's own limits, and getting its tone right (matter-of-fact, not apologetic, not alarming) matters more than its pixels.

**4. Sezóna jako osa času — the occupancy problem.** A bed holds different crops at different *times*: špenát in March, pórek from July. Every warning in the module turns on **overlapping occupancy** (D107), so a design that shows a bed as a flat list of crops is hiding the concept the module runs on.

This is v7's equivalent of v6's flow-visualisation problem. Something timeline-shaped — a per-bed year strip with occupancy bars — is the honest representation and is **desktop-shaped by nature**. Decide how it reflows to 375 px: a horizontally scroll-snapped month strip, a vertical month axis, or a different mobile composition entirely. What must survive the reflow is the sense that **time passes through a bed** — a stacked list of crop names has thrown away the whole idea.

**5. Plodiny — a form with ~40 fields, and the thing that makes it bearable.** The knowledge-base editor is the largest form in Home by a wide margin: identity, agronomy, propagation, spacing, four timing windows, harvest, storage, pests, diseases, free notes. Design its **progressive disclosure** — sections, sensible collapse, what a minimal viable crop looks like (name, family, hardiness, sow method, harvest unit) versus a fully-filled one.

Then design the escape hatch, which is the actual feature: **"Vygenerovat prompt" → paste into any model → paste the answer back → preview → apply.** Three screens nobody in this app has designed before:

- the **prompt hand-off** (a copy button, and enough explanation that a person who has never done this understands what is about to happen);
- the **import preview with a field-level diff** — what will be created, what will be *changed* on an existing crop (old → new, per field), and the fields the importer **could not map**, which are reported and never silently dropped;
- the outcome for a **20-crop array** in one paste — a per-element result list where three failed and seventeen applied.

**6. "Neověřeno" — trust signalling for machine-written content.** A crop filled by a model and not yet checked by a human is badged (D114). Design the badge, its tooltip, the filter ("jen neověřené") and the act of verifying. The tone is the design problem: it must read as *bookkeeping*, not as a warning that the data is wrong — most of it will be fine, and a scary badge on 40 crops teaches people to ignore it.

**7. The timing-window control — the weirdest input in Home.** A window is `{anchor, from, to}` with three anchors: ISO **week**, days relative to the **last spring frost**, days relative to the **first autumn frost** (D102). Four of these per crop. It must be enterable by someone reading a Czech seed packet ("výsev březen–duben") *and* by someone thinking in frost offsets ("6–8 týdnů před posledním mrazem"), without a manual.

Design it as one control with a mode, and **show the resolved real dates for the current season underneath as you type** — that live echo is what makes the abstraction land. It is used four times per crop, forty crops deep; a control that takes six taps costs an evening.

**8. Kalendář — work as windows, not dates.** Every generated task has an **od–do window**, not a due date, because "vysaď rajčata mezi 15. a 25. květnem" is the truth. Most calendar UIs draw points. Design a month/week view where a job legitimately spans ten days, can be **overdue** (window passed, still open), and can be ticked off. Filters by bed, crop and kind. This is also the **print target** (§11).

**9. Sklizeň a sklad.** Two entry-heavy screens used with dirty hands: recording a harvest (quantity in the crop's own unit, pre-filled) and putting produce into storage (what, how, where, how much, best-before). Design the **quick-entry** path — from the widget or the planting, two taps and a number — separately from the full form. And design the **yield-versus-expected** comparison on the planting, which is the payoff of recording anything at all.

**10. Uzavřít sezónu — a once-a-year ritual worth doing (D120).** One flow: final yields per planting, what failed and why, the observed frost dates, then the season **locks** and becomes rotation history. Because it is the *only* thing that creates history, the screen has to be worth twenty minutes in November. Design it as a **review**, not a form — show the year (what was planted, what came out, what the plan said) and let the numbers be confirmed rather than typed. Include the moment where a planting is marked **`failed` with a reason**: design that as neutral record-keeping, because a failure is data and the UI should not make it feel like a confession.

**11. Print — a designed deliverable (D125).** Two targets: **this month's work** (real checkboxes, bed codes, windows, grouped by week) and **the season plan** on one page. This is the accepted answer to writes-being-disabled-offline: the phone reads, the paper gets ticked. Design it as a real artefact — black on white, no dark-theme tokens, no icons that only mean something in the app, sized for a folded A4 in a back pocket.

**12. Záhony — where drag order *means* something.** Bed adjacency is **inferred from drag order within a zone** (D117): consecutive beds are neighbours, and that is what check C11 reads. Everywhere else in Home a drag handle means cosmetic ordering; here it changes the warnings. Design so the reorder interaction **teaches** that — a hint, a neighbour indicator on the card, something that makes "put them in the order they actually stand in" the obvious thing to do. If a user drags for tidiness and silently changes their adjacency warnings, the model has failed at the interface.

**13. The `garden.prace` Nástěnka widget (D123) — wide, all roles.** The next 30 days of work: **overdue first**, then grouped by ISO week, each line carrying the job, the crop and the bed code. Ticked via the house **2000 ms hold**. Empty state: *"na zahradě je teď klid"* — and design it as a **calm, correct answer**, because from November to February that is the widget's normal state and it should not look broken. **There is no second widget**; harvest appears here as a Sklizeň task.

**14. Nav and the eight-route problem.** Zahrada joins the **"Více" overflow for everyone**, no admin gate — but unlike every other Home module it has **eight sub-pages**. The four thumb tabs stay untouched. Design the in-page sub-navigation (segmented control, scrollable tab strip, a Přehled hub that fans out?) and be aware this is a **shell pattern Home does not yet have** — whatever you choose sets the precedent for the next multi-screen module. **Extends the v4/v5/v6 nav sections.**

**15. Two gardens: January and July.** In February the app is a planner — beds empty, plan being drafted, warnings loud, tasks few. In July it is a tracker — everything occupied, harvest tasks daily, the plan frozen, the warnings long dismissed. Design **Přehled** for both, and check every screen against both. A dashboard tuned to July is dead for four months of the year, and a planner tuned to February is in the way for the other eight.

**16. Reader state.** A `reader` sees the entire module and can change nothing — including **ticking a task off**, which is an ordinary write (D124). Use the established reader discipline: controls **disabled and visible**, never hidden.

### States — design each (loading, empty, error, reader, + garden-specific)

- **Plán:** loading; **empty season** (no beds yet → the real first-run state, which must route to Záhony); beds with no plantings; a full season; a **closed** season (read-only, visibly archival); the copy-season **preview** with its before/after check comparison.
- **Kontrola plánu:** clean; a handful of mixed severities; **eleven checks all firing** (design the worst realistic case, not the tidy one); everything dismissed; **`no_history`** for C3/C8.
- **Plodiny:** list loading / empty / ~40 crops with search; crop detail; the big form collapsed and expanded; a crop with **no varieties** and one with six; **"neověřeno"** badge and the unverified filter.
- **Import:** prompt hand-off; preview creating a new crop; preview **updating** an existing one with a field diff; unmapped fields; **a 422 on an unmappable enum, naming field and value**; a 20-element array with mixed outcomes; applying; applied.
- **Timing control:** each of the three anchors; a window whose season anchor is **missing** (no frost date set → the control must say what it cannot resolve, and the date stays unset); an inverted range refused.
- **Kalendář:** a busy week (the C5 spike — thirteen sowings in week 12); a quiet week; **overdue** items; a task ticked; a task **skipped**; the print preview.
- **Výsadba detail:** planned only; planned + actual with **drift** (*"vyseto o 14 dní později, sklizeň v plánu beze změny"*) and the one-click **"posunout navazující práce"**; a `failed` planting; a **permanent** planting (no season, with podnož and planted-on).
- **Sklizeň / sklad:** quick entry; full form; yield above and below expectation; an item running to zero (**auto `consumed`**); something **spoiled**; best-before approaching.
- **Uzavřít sezónu:** the review, mid-flow, confirmed, and the resulting **locked** season.
- **Widget:** work due; overdue present; **quiet** (November); loading.
- **Reader:** everything readable, every write control disabled and visible.
- **Offline:** all pages read from cache; write controls disabled with the standard *"Změny nelze uložit offline"*; **the print action stays available** — it is the offline answer.
- **Weather absent:** no coordinates set, or the forecast failed. Frost info simply is not there, with **no error state** — it is not something the user can act on (D112).

### Czech UX copy (use `design:ux-copy`, in Czech)

The vocabulary is **fixed in the PRD (§V7-7)** and must be used verbatim so the pages, the widget, the metric labels and the notification tokens all say the same words: **Zahrada · Plodina · Odrůda · Čeleď · Záhon · Část zahrady · Sezóna · Výsadba · Trvalka / dřevina · Kontrola plánu · Varování · Ignorovat · Práce · Termín výsevu · Poslední jarní mráz · první podzimní mráz · citlivá / polootužilá / otužilá · Nárok na živiny · Sklizeň · Sklad · Uzavřít sezónu · Neověřeno.**

Own everything else — and note that **this module's most-read strings are its warnings**. Eleven checks, each needing a title and a detail that a person would actually say: not *"C3 rotation violation"* but *"Brukvovité tu rostly předloni — doporučená pauza jsou 4 roky."* Write them as sentences, with the bed and the crop named. Also yours: the **"chybí historie"** line; the drift sentence; the season-close review; the "neověřeno" badge and tooltip; the import preview's diff and error copy; the print header; and the widget's quiet state.

Counts need the **three plural forms** (*1 záhon · 2 záhony · 5 záhonů*; *1 práce · 2 práce · 5 prací*; *1 varování · 2 varování · 5 varování*). Quantities carry the crop's own unit (kg · ks · l · svazek), dates are Czech-formatted, and a **window** is written as a range (*"15.–25. 5."*), never as a single date.

### Tokens & theme

- **Do not colour by botanical family.** The palette has **five** categorical slots (`--c1…--c5`) under the **Path A** aliasing resolved in §v6; the enum has **fifteen** families. Fifteen hues cannot pass a CVD all-pairs check at any chroma, and a fifteen-item colour legend is unreadable regardless of vision. Design family as a **chip with a short label** (and optionally a pattern), and reserve colour for the handful of families actually present in a given season — always **with** the label, never instead of it. This is the v7 counterpart of v6's four-bucket finding, and it is the one decision that can make the planner illegible.
- **Severity must read without colour.** `error` · `warn` · `info` · `tip` need an icon and a word each, not four shades. The garden is looked at in sunlight on a phone at 40 % brightness; a hue difference that survives the studio does not survive June.
- **Light theme is a first-class case here.** Home is dark-first by decision (see the body), and this is the one module used outdoors in daylight. Check contrast, tap targets and warning legibility in **light** before dark, and state anything you had to compromise.
- **Occupancy bars are charts** and follow the §v6 chart rules: fixed order, 2 px surface gaps rather than hairline borders, rounded data-ends, a legend always present, hover/press revealing the Czech label and dates.
- **Print tokens are their own set:** black on white, no theme variables, no icon-only meaning, real checkbox glyphs, and bed codes large enough to read at arm's length on a windy day.
- **Run the validator on any new or re-used colour values before handing back** — `scripts/validate_palette.js` from the `dataviz` skill, `--mode dark --surface` Home's `--s1`, then `--mode light`. Do not settle this by eye.

### Hard problems — address each with a rationale

1. **Eleven checks that stay readable.** The module's value is the warning nobody has to look for; its failure mode is a panel people stop reading in April. Severity, grouping, dismissal-with-note, and a `tip` that does not look like an error.
2. **A check that cannot run.** C3/C8 on a fresh install must communicate *"no history yet"* without reading as either an error or a pass. Small surface, disproportionate honesty.
3. **Time inside a bed.** Occupancy is the concept every warning turns on, and it is the one thing a flat list of crops per bed cannot show. Solve the year-axis at 375 px without losing the sense that time passes through a bed.
4. **Assigning crops to beds with a thumb.** Forty picks in a planning session, outdoors, one-handed. Drag-and-drop is not the answer; design the pick-then-place path properly.
5. **`{anchor, from, to}`.** An input that makes "6–8 týdnů před posledním mrazem" and "týden 10–13" equally natural, with the resolved dates echoed live. Four per crop, forty crops.
6. **A 40-field form people will actually fill.** Progressive disclosure plus the LLM escape hatch — and a preview screen that makes pasting machine output feel safe rather than reckless.
7. **Machine-written data, marked without alarm.** "Neověřeno" as bookkeeping, not as a defect badge.
8. **Fifteen families, five colours.** Solve it by not solving it with colour (see tokens).
9. **A drag that changes meaning.** Reordering beds changes adjacency warnings (D117). Make that legible at the moment of the drag, not in a help text.
10. **Eight routes in one "Více" entry.** Home has no multi-screen module pattern yet; whatever you choose becomes the precedent.
11. **January and July are different apps.** Design Přehled for both, and check every screen against both.
12. **Sunlight, dirty hands, no signal.** Light-theme legibility, ≥44 px targets everywhere (larger for the tick), and print as the offline answer rather than an apology for it.

### v7 Definition of done (addendum)

- [ ] **Plán**: bed-card grid at empty / partial / full, at 375 px and 1440 px in **both** themes, with 15 realistic Czech beds; the assign-a-crop flow designed as a thumb-first interaction with its crop picker.
- [ ] **Kontrola plánu**: inline bed badge + full panel; all four severities distinguishable **without colour**; the eleven-checks-firing worst case; dismissal with note, and the dismissed state; `tip` visually distinct from `warn`.
- [ ] **`no_history`** state for C3/C8 designed and worded.
- [ ] **Occupancy / year axis** for a bed, at both breakpoints, with the reflow rationale stated.
- [ ] **Plodiny form**: minimal vs full, section disclosure, four **timing-window controls** with the live resolved-date echo and the missing-anchor case.
- [ ] **Import flow**: prompt hand-off, preview creating, preview updating **with a field-level diff**, unmapped fields, the enum `422`, and a 20-element array result.
- [ ] **"Neověřeno"** badge, tooltip, filter and the verify action.
- [ ] **Kalendář**: month and week, jobs as **windows**, overdue treatment, tick and skip, filters; the C5 busy-week case.
- [ ] **Výsadba detail** with the **drift** line and the "posunout navazující práce" action; a permanent planting; a `failed` planting.
- [ ] **Sklizeň/sklad**: quick entry (two taps and a number) and full form; yield vs expected; zero-remaining and spoiled.
- [ ] **Uzavřít sezónu** designed as a review, including marking a planting failed, and the resulting locked season.
- [ ] **Print**: both targets as real artefacts, black on white, with checkboxes and bed codes.
- [ ] **Záhony**: reorder interaction that makes the adjacency meaning visible.
- [ ] **`garden.prace` widget**: work due, overdue, and the quiet November state designed as correct rather than empty.
- [ ] **Nav**: Zahrada in "Více" for everyone; the eight-route sub-navigation pattern chosen and justified as a Home precedent. **Extends v4/v5/v6.**
- [ ] Every screen: loading, empty, error, reader, offline, plus the garden-specific cases above.
- [ ] **January and July** both checked on Přehled, Plán and the widget.
- [ ] All copy Czech, PRD §V7-7 vocabulary verbatim, three plural forms, windows written as ranges, and **eleven warning messages written as sentences a person would say**.
- [ ] **Family is never encoded by colour alone**; any colour used is validated with the script in both themes, output pasted into the design system doc.
- [ ] Accessibility pass (`design:accessibility-review`) with an explicit **light-theme-in-sunlight** note: AA contrast in both themes with real diacritics, keyboard-operable planner, picker, timing control and calendar; touch targets ≥44 px; `prefers-reduced-motion` on every expand and any occupancy animation.

### Do NOT design (v7 additions to the existing list)

- **A garden map** — no plan view, no drawn beds, no coordinates, no drag-on-canvas. Adjacency comes from list order (D117), and a map would imply spatial data the module does not hold.
- **Photos anywhere** — no crop photos, no variety photos, no bed journal, no upload control. The module has **no blob storage** (D122).
- **Seed inventory (osivo) or a shopping list** — out of scope; do not design an "osivo" tab, a stock badge, or a "chybí ti semínka" prompt.
- **A general pantry** — Sklad is **garden produce only** (D121). No bought goods, no barcode, no stock levels for the household.
- **Watering or weeding schedules** — `water` and `weed` are manual-only kinds (D118). Do not design a cadence setting, a watering plan, or a "kdy jsi naposledy zaléval" tracker.
- **Green manure as a crop** — not modelled in v7 (D127). No zelené hnojení picker, no cover-crop row in the planner.
- **A second Nástěnka widget** — one widget (D123). No "Ke sklizni" card.
- **An automatic planner or rotation solver** — v7 **warns** about an assignment you made; it never proposes one. No "navrhni plán" button, no optimiser, no auto-fill of empty beds.
- **Historical season entry** — there is no back-fill (D120). No "zadej co rostlo v roce 2025" screen, and no empty-state that invites one.
- **Offline write affordances** — no queue, no "uloží se později", no sync indicator. Writes are disabled offline and **print** is the answer (D125).
- **Frost-alert settings inside Zahrada** — audience, timing, quiet hours and channels all live in **Administrace** (D113). Nastavení zahrady holds the frost *threshold* and the location; it must not grow a recipient picker or a "poslat mi upozornění" toggle.
- **Weather as a feature** — no forecast screen, no rain chart, no weather widget. The forecast exists to answer one question (is tonight a frost risk) and is otherwise invisible.
- **Per-person assignment of garden work** — tasks belong to the garden, not to Kája or Andy. No assignee avatar, no "moje práce" filter.
- **Any admin-only surface inside Zahrada** — the module has exactly one admin route (re-opening a closed season) and it is a confirm dialog, not a settings area.

---

## v8 addendum (2026-08-20) — Elektřina (`electricity`)

> This is the v8 design addendum to `HANDOFF-design.md`, written to fold into the addendum series after the v7 section. It covers *what to design and why*; `PRD.md` **§V8-1…§V8-9** (decisions **D133–D162**) governs *what it does*, and `openapi.yaml` **0.10.0** (tag `electricity`) is the data you'll render. Build brief: `HANDOFF-10-electricity.md`. Where this addendum conflicts with the v1–v7 body, **v8 wins for Elektřina**; the **v8 nav note extends the v4/v5/v6/v7 "Více" overflow** (Elektřina joins it **for everyone**, like Finance and Zahrada).
>
> **This is the smallest module Home has ever shipped, and the hardest one to make anybody open.** Four routes, four entities, no drag, no board, no timeline. What makes it a real design round is the thing it *doesn't* have: **Elektřina is the first Home module that puts nothing on Nástěnka and nothing in the notification catalogs** (D147, D156). No widget, no metric, no list, no push, and — at Karel's explicit request — **no chase of any kind**. Every module before this one had a surface that reminded the household it existed. This one has none. Přehled is not "the module's main screen"; it is the *entire* module's reason to exist, and it has to be worth walking to. That is a design constraint, not a footnote.
>
> **Two context facts that should change your choices more than any layout rule.** First, **the screen Karel sees on day one is the screen where prediction is impossible** — one reading, no second, no average, no forecast (§4.6 of the brief). That state is not an edge case to sweep up at the end; it is the primary screen for the first weeks of the module's life, and if it renders as a blank panel the module is dead before it has a number to show. Second, **this app is used standing at a meter cupboard, on a phone, in bad light, reading six digits off a display.** Home is dark-first by decision; like Zahrada, this module needs the **light theme solved first and 375 px solved first**, and a reading has to be enterable in about **fifteen seconds** — because nothing anywhere will remind the user to do it.

### What to design

**1. Přehled (`/elektrina`) — the screen that carries the whole module.** One page, no tabs: headline → prediction basis → progress → VT/NT breakdown → zálohy → doporučená záloha → the odečet line. It answers one question — *vyjdou zálohy, nebo doplatím?* — and it has to answer it in the first screenful, above the fold at 375 px, because the user came here on purpose and will not scroll to find out whether the trip was worth it. Treat the fold as a hard budget: headline, hedge, and the odečet action. Everything else is below it.

**2. The headline — a big confident number that must never read as a fact.** *Nedoplatek* or *přeplatek* at the period end, red or green, in the mono figure treatment, with the projected-to date beside it and — while `ends_on_confirmed` is false — the words **předpokládaný konec** (D153). This is the module's central typographic problem: the number has to be legible at arm's length *and* visibly hedged, and those two goals pull against each other. Solve it with **structure, not with timidity** — do not shrink the number or grey it out to signal uncertainty, because then it stops being readable in a dim cupboard and the module loses its one payoff. Suggested shape to beat: full-size figure, a **kicker line above it** naming what the number is (*"Odhad k 23. 6. 2027"*) with the **předpokládaný konec** badge inline, and a **basis line below it** — *"predikce z průměru za posledních 122 dní"*. Hedge above and below, confidence in the middle. Design the transition too: when `ends_on_confirmed` flips true, the badge disappears and nothing else moves.

**3. "Zatím nelze předpovědět" — the first-class empty state, and the one to design first.** With one reading in the period there is no average, so the module says so and **names what is missing**: *"potřebuji druhý odečet"*. What makes this a designed screen rather than an apology is the **headroom** figure (§4.6), which is computable with zero consumption data: of the **1 500 Kč** záloha, **642,35 Kč** are poplatky, leaving **857,65 Kč/měs** for energy — about **176 kWh** all-VT, **213 kWh** all-NT, **~200 kWh** at a 30/70 mix. That is a genuinely useful answer to "will the zálohy cover it", and it should occupy the headline slot with the same weight the prediction will later take, so the screen does not visibly upgrade from *broken* to *working* — it upgrades from *one answer* to *a better one*. Below it, the things that *are* known: the ceník, the období with its **předpokládaný konec**, the záloha and its counted months. **No spinner, no zero, no blank panel** (acceptance criterion 8).

**4. The hard block — valid data and a blocked region on one screen (D137).** When a ceník change falls strictly inside a reading interval, the module refuses to price across it: *"Chybí odečet k 1. 1. 2027 (změna ceny). Bez něj nelze spočítat spotřebu po tomto datu."* with a **Zadat odečet** button that opens the form **pre-filled with that date and nothing else** — never the values. The design problem is that the screen must simultaneously show **numbers that are still true** (everything before the gap) and **a region that cannot be computed**, without the blocked part reading as an error and without the valid part reading as untrustworthy. Do not tint the whole page. Suggested handle: a **date-anchored divider** in the Přehled body — everything above it renders normally, the divider carries the missing-date line and its button, and the panels below it are shown as *not yet computable* in the same visual language as §3, not in the destructive one. This is a **blocked**, not a **failed**, state and its colour token must say so.

**5. Spotřeba a náklady, VT vs NT (D151).** kWh, Kč, and each tariff's share of the total, for the period to date. This is not decoration: at **4 858,65** vs **4 026,69 Kč/MWh**, roughly **830 Kč** rides on every MWh moved from VT to NT, and the split is the only place that becomes visible. The two parts **always sum exactly to the total** — VT is rounded and NT takes the remainder (D158), the `needs` pattern from the fin split — so never design a layout in which a rounding gap could plausibly appear and need explaining. Design it as a paired figure block with a **two-segment share bar** — chart rules from §v6 apply (fixed order, 2 px surface gaps, rounded ends, legend always present, direct labels).

**6. Zálohy, counted months, and the doporučená záloha.** Paid so far (by due day, D155) against the expected total, with the **counted months listed on demand** — the D145 rule (*a month counts iff the period contains its 1st*) is the module's most surprising piece of arithmetic, and a disclosure listing *červenec 2026 … červen 2027* with amounts turns folklore into something checkable. Then **doporučená záloha vs. the current one**, as a comparison, not a lone number: *"doporučeno 1 795 Kč/měs · nyní 1 500 Kč"*. When no months remain, it is not shown at all (D146).

**7. The odečet line — a nudge, not a notification (D156).** One plain line, *"poslední odečet před 47 dny"*, with a **Zadat odečet** button. This is a **tone problem before it is a layout problem.** The user refused notifications; a line that reads as a scold breaks the same promise a push would. It should read as *explanation* — it is the honest reason the prediction is stale — with the action attached because the user is already here. Design a **quiet register** (muted text, secondary button) and design its **escalation**, if any, deliberately: if 47 days and 200 days look identical, the line is useless; if 200 days turns red, the module has started chasing. Find the register between the two and state your rationale. **✅ RESOLVED BY THE BUILD — words only, at 15 and 90 days; colour and size never change. See §v8 — as built (D175).**

**8. Odečty (`/elektrina/odecty`) — and the fifteen-second form, which is the whole product.** The list shows one row per reading, each carrying **the interval that ends at it**: days, VT/NT kWh, Kč, and which ceník priced it. That is where a mistyped register becomes obvious, because one interval looks absurd beside its neighbours — so design the rows to be **compared**, not just read. The form is four numbers a few times a year and it is the only thing standing between this module and neglect: **date (defaulted to today), VT, NT, optional note.** Whole kWh, no decimal, `inputmode="numeric"`, fields big enough to hit while holding a phone at arm's length, and a save that does not require scrolling. Design the **pre-filled variant** arriving from the hard block (date locked to the missing date, values empty) and the **monotonicity refusal** (422) as a message that names the neighbouring reading and its value, because with výměna elektroměru out of scope (D150) a falling counter is always a typo.

**9. Ceníky a poplatky (`/elektrina/ceniky`).** The versions in date order, each showing its three numbers, its `effective_from`, its **derived** validity range, and *"platí pro 153 dní tohoto období"*. **A version with a future date is the normal case, not an edge case** — design the form and the list so entering next January's prices in August feels routine, and so the effect on the forecast is visible immediately (D142). Because a version's end is derived and never stored (D136), the list is the only place the user can see where one version stops and the next begins: make that boundary a visible property of the list, not something to be inferred from two dates. The **záloha schedule and its `due_day`** live on this screen too, versioned the same way.

**10. Historie a grafy (`/elektrina/historie`) — and the module's one approximation.** Consumption per month (VT/NT), cost per month, and past periods with **computed vs. invoiced in both Kč and kWh** (D154) — the second line is the one that tells you whether a discrepancy was a price surprise or an odhadnutý odečet on the supplier's side. Two design obligations here. First, the month columns are built from **interpolated** data (D138): an interval's kWh spread evenly over its days so months can be drawn at all. That caveat must be **visible without nagging** — carried by the marks themselves plus one footnote, not by a warning banner over a chart. Second, and more important: **this is one of very few places in Home where an approximation is displayed at all, and the Kč figures sitting next to it are exact to the haléř.** Those two kinds of number must not share a visual treatment. See *Tokens & theme*.

**11. Zúčtovací období — creating one, and correcting its end.** Period start, expected end (defaulting to `starts_on + 1 rok − 1 den`), and the `ends_on_confirmed` toggle. Two states worth designing properly: the **overlap refusal** (422, naming the period it collides with), and the moment the supplier's real end date arrives and one field changes — every number on Přehled re-projects and nothing about the actual figures moves (acceptance criterion 10). **The period also finishes, and that changes the page's tense.** Once the closing reading — dated `ends_on + 1`, the same one that opens the next period — exists, there is nothing left to forecast: the headline stops being a prediction and becomes a fact (D157). Design that flip deliberately: **predikce → skutečnost**, the hedge kicker and the basis line both disappear, and the number stays exactly where it was so the eye does not have to re-find it. It is the one moment in this module where a hedged figure earns the right to be unhedged, and it should feel like an arrival. Recording the vyúčtování is four optional fields and produces the comparison line: *"spočteno 21 560 Kč · vyúčtováno 21 890 Kč · rozdíl −330 Kč"* — plus, because the supplier's own final readings are stored (D154), a second line in **kWh**. **Nothing ever locks** (D139) — do not design a close/archive ritual.

**12. Nav — and the map that must *not* be edited.** Elektřina joins the **"Více" overflow for everyone**, no admin gate; the four thumb tabs are untouched. Icon: a lucide zap/plug-family mark. Four sub-routes, so reuse whatever sub-navigation pattern v7 established rather than inventing a second one. **Explicitly: there is no `platform/widgets/registry.tsx` entry and no Nástěnka surface of any kind** (D147). If a widget appears in a mock, it is wrong.

**13. Reader state.** Ordinary all-roles module, no admin-only route (D151). A `reader` sees every screen and every number and can write nothing. Established discipline: controls **disabled and visible**, never hidden — including **Zadat odečet**, which is the one button a reader will most want to press.

### States — design each (loading, empty, error, reader, + electricity-specific)

- **Přehled:** loading (skeleton, never a spinner in the headline slot); **no period at all** (true first run → route to creating one); **one reading — the headroom state** (§3, Karel's real day one); **predicting normally**; **blocked** by a missing price-change odečet (§4); **no opening reading on `starts_on`** (D140 — no money at all, only the missing-reading notice); **no ceník effective on or before the period start**; a period **finished** — the closing reading entered, so the page reads *skutečnost* rather than *predikce* and the hedges are gone (D157) — but with no vyúčtování yet; a period **with** the vyúčtování recorded and the two comparison lines; **`ends_on` confirmed** vs unconfirmed; reader; offline.
- **Odečty:** empty; **exactly one reading** (the day-one list — no interval to show on the only row); a normal year of readings; the row whose interval is **unpriceable** because of a straddled ceník change; add / edit / delete; the **date-pre-filled** variant from the hard block; the **monotonicity 422** naming its neighbour; the duplicate-date refusal.
- **Ceníky a poplatky:** one version (day one); several; a **future-dated** version and its effect echoed on the forecast; the delete refused with 409 because the version covers existing days; the záloha schedule with a `due_day` of 31 and its **únor** clamp made visible.
- **Historie a grafy:** **no history at all** — the honest first-year state, the counterpart of v7's *"chybí historie"*, matter-of-fact and not an error; one partial month; a full year; the **interpolated** month columns with their caveat; a month straddling a ceník change; past periods with computed-vs-invoiced in Kč **and** kWh, including a case where the kWh differ and the Kč nearly agree.
- **The nudge line:** last reading 4 days ago, 47 days ago, 200 days ago — three renderings of §7, and a defensible reason why they differ (or don't).
- **Reader:** every screen readable, every write control disabled and visible.
- **Offline** (v5 D71): everything renders from cache read-only; write controls disabled with the standard **"Změny nelze uložit offline"**. **No offline queue** — and note the irony worth designing around: the meter cupboard is exactly where signal is worst, and the answer is *"zapiš si to a zadej to doma"*, not a sync tray.

### Czech UX copy (use `design:ux-copy`, in Czech)

The vocabulary is **fixed in the PRD (§V8-7)** and must be used verbatim across the pages, the forms and the audit-action phrases: **Elektřina · Odečet · VT (vysoký tarif) · NT (nízký tarif) · Ceník · Cena VT · Cena NT · Měsíční poplatky · Záloha · Den splatnosti · Zúčtovací období · Předpokládaný konec · Vyúčtování · Spotřeba · Náklady · Predikce · Nedoplatek · Přeplatek · Doporučená záloha · Zadat odečet.**

Own everything else, and note that in this module **the copy is doing work the layout cannot**. The strings that carry the most weight: the **hedge line** (*"predikce z průměru za posledních 122 dní"*); *"zatím nelze předpovědět — potřebuji druhý odečet"*; the **headroom sentence**, which has to make 857,65 Kč/měs and ~200 kWh land as one idea in one line at 375 px; the **hard-block line** and its button; the **monotonicity error** naming the offending neighbour; the **nudge** (*"poslední odečet před 47 dny"*); the **interpolation footnote** on the history chart (approximate without being alarming); the **counted-months explainer** for D145; the computed-vs-invoiced comparison; and the *"platí pro 153 dní tohoto období"* line.

Counts need the **three plural forms**: *1 odečet · 2 odečty · 5 odečtů*; *1 den · 2 dny · 5 dní*; *1 měsíc · 2 měsíce · 5 měsíců*; *1 ceník · 2 ceníky · 5 ceníků*. **kWh, MWh and Kč do not inflect.** Numbers are Czech-formatted throughout — space thousands separator, comma decimal mark: **`21 560 Kč`**, **`4 858,65 Kč/MWh`**, **`1 234,5 kWh`**, dates **`24. 6. 2026`**. ⚠ **This is the first Home module with decimal koruny** — v6 fixed money at whole koruny, and here totals are whole (`21 560 Kč`) while unit prices and fees are not (`4 858,65 Kč/MWh`, `642,35 Kč/měs`). State the rule per figure type in the system doc; do not leave it to each screen.

### Tokens & theme

Dark-first, tokenised, light under `.light` — but **check light first here** (§ intro). Elevation by surface lightness, as established. What Elektřina needs:

- **Exact vs. approximate must be a token-level distinction, not a per-chart decision.** The history chart's month columns are interpolated (D138) and the Kč beside them are exact to the haléř — a month's cost is an **allocation of exact interval costs by day count, never a repricing of the approximate kWh** (D159), which is precisely why the two must not share a visual language. Give the approximate treatment its own vocabulary — a hatch/pattern fill or a defined opacity step, plus a consistent footnote — and make it a **rule that Kč figures never receive it**. This is the first place in Home where the two kinds of number appear together and it will not be the last.
- **VT and NT are two chart series and must differ by more than colour.** Path A aliasing from §v6 applies: propose **`--el-vt: var(--c1)` · `--el-nt: var(--c2)`** in `theme/globals.css` — aliases, not values, so `.light` re-steps them free and no new colour enters the system. Two series is the easiest palette case Home has had, but they sit **edge-to-edge in the share bar and the stacked columns**, so the §v6 adjacency finding is exactly the one that applies. **Run `scripts/validate_palette.js` (`--mode dark --surface` Home's `--s1`, then `--mode light`) before handing back, and paste the output into the system doc.** Secondary encoding is mandatory regardless: **direct VT/NT labels**, a distinct swatch shape, a pattern on one series, and an `aria-label` naming the tariff and its value.
- **Nedoplatek / přeplatek colour is semantic, and it is a *prediction*.** Red/green must carry a **word and a sign, never hue alone** (`−1 240 Kč nedoplatek` / `+40 Kč přeplatek`), must pass AA in both themes, and — following the v5 precedent that a warning is not a destructive action — the nedoplatek token must **not** be the destructive red used for delete. Nobody has done anything wrong; the forecast is simply above the zálohy.
- **Blocked is not error.** The hard-block treatment (§4) needs its own token pairing — informational/muted with a strong action, distinctly not the destructive palette — because the numbers around it are still correct.
- **The "předpokládaný konec" badge** is a neutral/muted chip, deliberately not a warning colour. It appears next to the largest figure on the screen and must not compete with it.
- **Mono figures, extended.** v6 established tabular-nums for columns of koruny; v8 adds a **display size** for the Přehled headline and **mixed-precision alignment** (`21 560 Kč` above `4 858,65 Kč/MWh` in the same column). Specify the alignment rule, not just the font.
- **Numeric entry at a meter cupboard:** oversized numeric fields, `inputmode="numeric"`, ≥44 px targets with generous spacing between VT and NT so the wrong register is hard to hit, and **light-theme contrast verified at low screen brightness**.

### Hard problems — address each with a rationale

1. **A module with no front door.** No widget, no metric, no list, no push (D147, D156). Nothing will ever tell the household this module exists. Design Přehled so that opening it deliberately, maybe monthly, is repaid within one screenful — and say in the system doc what you did to earn that trip.
2. **A confident number that must not read as a fact.** Big, legible at arm's length, and honestly hedged, with a **předpokládaný konec** badge sitting next to it. Solve it structurally; shrinking or greying the figure trades away the module's only payoff.
3. **The empty state is the primary screen.** For weeks, "zatím nelze předpovědět" *is* Elektřina. Design it first and give it the headroom figure at full weight, so the later upgrade to a real prediction is a change of answer, not a repair.
4. **Valid data and a blocked region, on the same screen, at the same time.** Neither contaminating the other. This is the subtlest state in the module and the one most likely to be drawn as a full-page error.
5. **Exact beside approximate.** Interpolated kWh columns next to haléř-exact koruny. Two number species, one screen, and the user must never wonder which kind they are reading.
6. **VT vs NT with more than colour** — and with a share bar where the two sit edge-to-edge. Validate; do not settle it by eye.
7. **Fifteen seconds at the meter cupboard.** Date, two whole numbers, save — one-handed, in bad light, possibly with no signal. If this form is slow, the module dies of neglect and no notification will rescue it, because there are none.
8. **A nudge that informs without nagging.** *"poslední odečet před 47 dny"* is the entire retention mechanism the user permitted. Get the register right, and decide explicitly whether it escalates. **✅ Decided: it escalates in words only (D175).**
9. **Czech money with two precisions.** `21 560 Kč` and `4 858,65 Kč/MWh` in the same column, at 375 px, with diacritics — a first for Home (v6 was whole koruny). Tabular figures and a stated per-type rule.
10. **A screen visited a few times a year has no muscle memory** — worse than v6's once-a-month case. Four versioned entities (odečet, ceník, záloha, období) that a user meets twice a year: every control must be self-explanatory to someone who last saw it in February. Favour explicit Czech labels over learned iconography, everywhere.

### v8 Definition of done (addendum)

- [ ] **Přehled** at 375 px and 1440 px, in **both** themes with **light checked first**, in all its states: headroom / predicting / blocked / missing opening reading / period ended / vyúčtování recorded.
- [ ] **The headline** designed as a hedged prediction: kicker with the projected-to date, **předpokládaný konec** badge, display figure, basis line — plus the confirmed variant, with nothing but the badge moving.
- [ ] **The day-one screen** rendering Karel's real state — one reading, ceník, období, **857,65 Kč/měs ≈ ~200 kWh** headroom — with no spinner, no zero and no blank panel (acceptance criterion 8).
- [ ] **Hard-block treatment** showing valid figures above the gap and the blocked region below it, with the Czech line and the **date-pre-filled** Zadat odečet path; blocked visually distinct from error.
- [ ] **VT/NT breakdown** with kWh, Kč and shares, and its two-segment bar following the §v6 chart rules.
- [ ] **Zálohy block**: paid-by-due-day vs expected, the **counted months** disclosure making D145 checkable, and **doporučená záloha vs. current** (absent when no months remain).
- [x] **The nudge line** at 4 / 47 / 200 days, with the escalation decision stated. **Shipped with five renderings, not three** — never · future-dated · fresh · ageing · stale; see §v8 — as built.
- [ ] **Odečet form** demonstrably enterable in ~15 s one-handed: whole kWh, numeric keypad, ≥44 px targets, no scroll to save; plus the pre-filled and 422-refusal variants.
- [ ] **Odečty list** with per-interval days / kWh / Kč / ceník, designed for row-to-row comparison, including an unpriceable interval.
- [ ] **Ceníky** list and form with derived validity ranges, *"platí pro 153 dní tohoto období"*, a **future-dated version as the normal case**, the 409 delete refusal, and the záloha schedule with `due_day` and its únor clamp.
- [ ] **Historie**: VT/NT month columns with the **approximate** treatment and footnote, cost per month, past periods with **computed vs. invoiced in Kč and kWh**, and the honest **no-history** first-year state.
- [ ] **Období**: create, expected-vs-confirmed end, the overlap 422, and the vyúčtování comparison lines.
- [ ] **Nav**: Elektřina in **"Více"** for everyone, desktop side-nav for everyone, sub-navigation reusing the v7 pattern. **Extends v4/v5/v6/v7.** **No widget anywhere** — assert it in the mocks as well as the code.
- [ ] Every screen: loading, empty, error, reader, offline, plus the electricity-specific states above.
- [ ] All copy Czech, PRD §V8-7 vocabulary verbatim, three plural forms, Czech number and date formatting, and the **decimal-koruna rule stated per figure type**.
- [ ] **`--el-vt` / `--el-nt` validated with the script in both themes**, output pasted into the design system doc; VT/NT never distinguished by colour alone.
- [ ] Accessibility pass (`design:accessibility-review`) with an explicit **light-theme-at-low-brightness** note: AA in both themes with real diacritics and real magnitudes; nedoplatek/přeplatek carrying word and sign, not hue; keyboard-operable forms and lists; touch targets ≥44 px; `prefers-reduced-motion` on any chart or disclosure animation.

### Do NOT design (v8 additions to the existing list)

- **A Nástěnka widget** — Elektřina contributes **nothing** to the dashboard (D147). No card, no stat, no "zadej odečet" tile. `platform/widgets/registry.tsx` is deliberately untouched; a widget in a mock will be built.
- **Any notification or push surface** — no rule, no summary token, no metric, no list, no catalog entry, no settings row under Nastavení → Oznámení (D147, D156). The **one** in-app line on Přehled is the entire permitted nudge.
- **Invoice itemization** — three price fields only: **cena VT · cena NT · měsíční poplatky**, all entered **s DPH a distribucí** and used as typed (D135). No silová/distribuce split, no jistič, no systémové služby, POZE, OTE, daň z elektřiny, and **no DPH arithmetic or VAT-rate field**.
- **Výměna elektroměru** (D150) — no meter-change flow, no counter reset, no "nový elektroměr" wizard. A falling register is a typo and is refused.
- **A second odběrné místo, FVE/solar, přetoky, plyn or voda** — one supply point, two registers, electricity only. No site switcher, no generation series, no fuel picker.
- **Invoice PDF upload or any attachment** — the module has **no blob storage**. No upload control, no document link, no "přilož fakturu" affordance.
- **A print view** — v7's print deliverable does not extend here; there is nothing to carry to a cupboard on paper.
- **Offline writes** — no queue, no "uloží se později", no sync indicator (v5 D71). Writes are disabled offline, full stop.
- **Seasonal, degree-day or weather-based forecasting** — the prediction is a **plain average since period start** by decision (D141). No winter/summer weighting, no temperature input, no "loni touhle dobou" comparison, no confidence interval.
- **Price-offer comparison, supplier switching, or an HDO schedule** — out of scope; do not design a tariff-shopping screen or an NT-hours timetable.
- **A back-fill importer or history-entry screen** — there is none (OQ-V8-8). Whatever history exists is typed in as ordinary readings, and the empty state must not invite an import.
- **A settings screen** — the module has no settings table and nothing to configure (§3.5). Ceníky holds the záloha schedule; that is all the configuration there is.
- **Closing, locking or archiving a period** — periods are never locked (D139). No close ritual, no read-only past period, no "uzavřít období" button.
- **Editing any derived value** — kWh per interval, Kč per interval, balance, doporučená záloha and the validity ranges are all computed on read (D152) and are read-only everywhere.

*Backend + frontend for `electricity` (`HANDOFF-10-electricity.md`) can be built from `PRD.md` §V8 + `openapi.yaml` **0.10.1**; the **four Elektřina screens and the "Více" nav update** are reconciled against this addendum once approved — the same rule v2–v7 used. The three non-registry host maps (`AppShell` OVERFLOW, the Log browser's `MODULES`, `admin/listener.go`'s `inAppURL`), the `compute.go` purity test and the no-widget assertion are build concerns, not design.*


---

## §v8 — as built (2026-08-21)

> The v8 addendum above is the brief. This section records what the build settled, so nobody re-opens a question that has an answer. Where the two differ, **the build wins for Elektřina**.

**The escalation question is CLOSED — words only (PRD §V8-12 D175).** The sub-line changes at **15** and **90** days; the colour and the size never do. Both halves of §v8's framing were real: if 47 days and 200 days rendered identically the line would stop informing, and if 200 days turned red the module would have started chasing, which is the one thing Karel refused. Escalating the *wording* while holding the *visual register* fixed is the seam between them. The register stays explanation, not reproach.

**One state the brief did not anticipate:** `days_since_last_reading` can be **zero or negative** — a reading dated today, or a closing odečet entered ahead of time. Both are legal. Neither goes through the counting branch, because *"před −42 dny"* is nonsense; the future case gets its own hint line. The required renderings are therefore **five**, not three: never · future-dated · fresh (<15) · ageing (15–89) · stale (≥90).

**Two AA failures were found in the delivered tokens and fixed.** `--el-approx` measured **3.91** dark / **3.38** light against `--s1` while carrying the interpolation footnote — a full sentence of body text, not a label — and `--el-over` measured **4.33** on its own soft chip in light, where the chip sets bold 13 px text that WCAG counts as body, not large. Both retuned; **every new v8 token now clears 4.5:1 in both themes** — approx 5.3/4.97, over-on-soft 6.47/5.35, blocked-on-soft 6.33/6.00, under 8.14/5.18. ⚠ **Method note for the next module:** `getComputedStyle` returns `oklch()` in this codebase, and naive parsing of that silently produces nonsense contrast numbers. Sample the rendered sRGB from a canvas instead. The brief's `scripts/validate_palette.js` is a design-side tool and is not in the app repo.

**Five copy bugs the running app revealed that no test would have caught:** an ungrouped `1460 Kč` beside a grouped `1 500 Kč`; *"dosud"* printed as the end of a **future** ceník's validity range; one month total rendered at two different precisions on the same screen; the headroom chips showing `200,6 / 176,5 kWh` where the spec pins whole `200 / 176`; and the negative day count above. All fixed. The lesson generalises: **this is home's first module with two money precisions** — totals in whole koruny, unit prices and fees with their haléře — and every place the two meet is a bug site. The rule is stated once in `format.ts` and must stay stated once.

**Confirmed as designed:** four first-class Přehled states (`ok`, `insufficient_data`, `blocked`, `complete`), none a fallback · **Zadat odečet in the module header**, not on a tab, because it is the one action anybody arrives to perform · the tab route is `/elektrina/cenik` (singular — the PRD's spelling won over the prototype's `ceniky`), while the tab **label** stays "Ceníky a poplatky" · zero horizontal overflow at 375 px · `platform/widgets/registry.tsx` verified absent from the diff.

---

## v9 addendum (2026-08-21) — Soukromé položky a Úložiště (`notes` · `documents` · `admin`)

> This is the v9 design addendum to `HANDOFF-design.md`, written to fold into the addendum series after the v8 section. It covers *what to design and why*; `PRD.md` **§V9-1…§V9-9** (decisions **D176–D215**) governs *what it does*, and `openapi.yaml` **0.11.0** is the data you'll render. Build brief: `HANDOFF-11-privacy-storage.md`; scope brief: `V9-privacy-storage-brief.md`. Where this addendum conflicts with the v1–v8 body, **v9 wins for the two trees and for Administrace**.
>
> **Every addendum before this one asked you to design a new module. This one asks you to change two that people already use every day** — and that is a harder brief, not an easier one. Poznámky and Dokumenty have been in the household's hands since v3 and v4; their trees, their search, their upload queue and their pins are muscle memory. v9 puts a **second root** beside each of them. The design problem is not *how do we show a private folder* — that part is a lock icon and an hour. It is **how a person always knows which of the two trees they are standing in**, at 375 px, in a hurry, one-handed, when the cost of being wrong is putting something private in front of the whole household and there is **no way to take it back** (D182: publish is one-way; there is no unpublish).
>
> **The second half of the brief is the opposite kind of screen.** Administrace gains **Úložiště** — a page of numbers about database tables and R2 buckets that will be opened perhaps four times a year, by one person, usually because a bill or a warning prompted it. It has to be readable cold, by someone who last saw it in February, and it has to be honest about what it could not measure rather than filling the gap with a zero.

### What to design

**1. The root switcher — the whole interaction design of the privacy half.** Above each tree, two roots: **Poznámky** / **Soukromé poznámky** (and Dokumenty likewise). Everything else on those pages is unchanged — same tree, same editor, same upload queue, same previews, same pins — and that is deliberate: **a member who never opens the private root should not be able to tell that v9 shipped.** What must never be ambiguous is which root is on screen. Two failure modes to design against, and they pull in opposite directions: a switcher so quiet that someone uploads a private document into the shared tree, and one so loud that the shared tree — the one used ninety per cent of the time — feels like it is being guarded. **Solve it with persistent state, not with a warning**: the current root should be legible from the page's *shape* (a header, a breadcrumb root segment, a tinted tree container), so it is readable at a glance rather than requiring a control to be re-read. Design both roots at 375 px side by side and ask whether a screenshot of one could be mistaken for the other.

**2. Lock language, defined once and spent nowhere else.** A lock mark means exactly *"only you can see this"*. It appears on the private root, on private rows in the tree, on a private hit in search results, and on a private row in a pinned widget. **It is never borrowed** for a disabled control, an admin-only route, or a locked settlement period. Home has an established discipline that disabled controls stay visible rather than hidden; a lock that sometimes means "not yours" and sometimes means "not visible to others" destroys both meanings. Also: **the lock must not be the only carrier** — a text label accompanies it everywhere, for the accessibility pass and for the person who has never seen the icon before.

**3. "Publikovat do sdílených" — the one irreversible action in either module.** Owner-only, in the item's own menu rather than on a toolbar, with a dialog that says plainly what changes: the household will see it, and **it cannot be undone**. Not a toast-and-undo, because there is no undo to offer. This is a **copy problem before it is a layout problem**: the sentence has to convey irreversibility without theatrics, in a household app where the consequence is usually mild and occasionally serious. Design the folder variant too, which publishes **every descendant** — the dialog should say how many items are about to become visible, because *"publish this folder"* reads much smaller than what it does.

**4. The state that has no screen: a private item that is 404 to you.** Another member's private item does not render as *"you don't have permission"* — it renders as **nenalezeno**, the ordinary not-found screen, because a permission message is itself the disclosure (D180). There is nothing new to draw here; what there is to design is the **absence of a special case**. If a mock anywhere shows a padlock-shaped "this is private" screen for a foreign item, it is wrong, and it undoes the decision.

**5. Search, scoped.** Searching inside Soukromé poznámky searches only that root; searching in Poznámky searches only the shared one (D184). The design work is making the scope of a search **visible in the search field itself** — a placeholder naming the tree, a scope chip in the input — so nobody concludes their private note has vanished because they searched from the wrong root. Design the empty result state to say which tree it searched.

**6. The pinned widgets on Nástěnka.** A private item can be pinned **"jen pro mě"** only; **"pro všechny" is refused** (D183). Two things to design: the pin control with the household option unavailable-and-explained rather than silently absent, and the widget row itself, which now mixes household-pinned and private-pinned items in one list where the difference genuinely matters. **A published item keeps its personal pin** — so a row can change from locked to unlocked in place, and that transition wants a moment of feedback rather than a silent swap.

**7. Úložiště (`/administrace` → Úložiště) — a page of numbers read four times a year.** The order is fixed by what a person came for: **totals first** (database and objects), then **per module**, then **per member split shared / soukromé**, then the two lines that belong to nobody — the **Litestream replika** and the **zálohovací bucket**. Those last two are a design problem in miniature: they are R2 space the household pays for, they are often *larger* than everything above them, and they are **not part of any per-module sum** (D214). Make that structurally obvious — a separate block below the breakdown, not two more rows in the same table — or the page reads as though its own arithmetic is broken. Two design obligations that are not decoration:

- **Unmeasured is not zero.** Where `dbstat` is unavailable, a table shows its row count and **no byte figure** (D193). Every byte field in the payload is nullable and null-never-zero, and the page must render that absence as *nezměřeno*, visibly distinct from a real small number. This is v8's exact-vs-approximate problem (`--el-approx`) returning as a platform-level concern — see *Tokens & theme*.
- **`Nezařazené` is a real row, not an error.** Objects in the bucket that resolve to no live row are the orphan backlog the mirror job reconciles (D194), surfaced for the first time. It belongs in the table as an ordinary bucket of usage — not in red, not as a warning — with one line of copy explaining what it is, because the number is meaningless and slightly alarming without it.

**8. The warning register.** One threshold on total R2 bytes (D196). Above it the page marks the largest contributors and says so. **Nothing is ever blocked** — no upload fails, no quota exists — so the register must be *informational*, following the v5 precedent that a warning is not a destructive action, and distinctly **not** the red used for delete. Nobody has done anything wrong; the bucket is simply larger than a number somebody chose.

**9. Soukromé položky — the purge screen, the most delicate screen in v9, and one that is on screen even when it is empty (D215).** It lists **id, owner, kind, size, dates** for every member's private items — and no title, no filename, no content type, no thumbnail, no download, no search box (D198). It exists because an admin's power to hard-delete is useless if nothing can name the thing to delete, and it is **uncomfortably close to being the private-file browser the whole feature exists to prevent.** Design it so that discomfort is legible rather than smoothed away: it should read as a **maintenance tool**, austere and slightly inconvenient, not as a file manager. Deliberately: no grid view, no preview column, no sorting by anything but size and recency, purge confirmed by **typing**, and a visible note that opening this list is itself recorded (`admin.private_items.view`). If it starts to feel pleasant to browse, the design has gone wrong.

**10. Nav.** No new destination. **Administrace grows to six tabs** — Rozeslat · Pravidla · Souhrny · Doručení · **Úložiště** · **Soukromé položky** — which is past what a tab row carries at 375 px, so **reuse v7's module tab-strip pattern** rather than inventing a second one. ⚠ **Explicitly: no new nav entry, no new widget, no Nástěnka surface of any kind.** v9 touches none of the four non-registry host maps (D202); `platform/widgets/registry.tsx` stays untouched for the second version running. A widget in a mock will be built.

**11. Reader state.** A `reader` sees both trees — their own private root included, since a reader can own nothing they did not create and therefore usually sees it empty — and can write nothing. Established discipline: controls **disabled and visible**, never hidden, **Publikovat do sdílených** included. Administrace stays admin-only, so neither new screen exists for a reader at all.

### States — design each (loading, empty, error, reader, + v9-specific)

- **Both trees:** shared root as today; **private root, empty** — the state every member sees on day one, and the one that has to explain what the tree is *for* without a tutorial; private root with folders; a private root belonging to a `reader`; the moment after a publish, where an item leaves the private tree in front of you.
- **The switcher:** shared selected · private selected · mid-transition · at 375 px with a long folder breadcrumb · in both themes.
- **Search:** scoped to shared · scoped to private · no results, naming which tree was searched · a private hit rendered in a result list (lock + label).
- **Publish:** the confirm dialog for a single item · for a folder with **N descendants** · the slug-collision outcome (`smlouva-2`) explained without alarming · the refusal a non-owner (including an admin) receives, which is the **ordinary nenalezeno screen** and not a "not yours" message. ⚠ *An earlier draft asked for a 403 state here; it is withdrawn (PRD D206) — a permission message is itself the disclosure, and there is no permission screen for a foreign private item anywhere in v9.*
- **Pins:** the pin control on a private item with **"pro všechny"** unavailable-and-explained · a widget row with a lock · a row that has just been published and lost its lock.
- **Foreign private item:** **the ordinary nenalezeno screen** — draw it exactly once, to prove there is no special case (§4).
- **Úložiště:** fully measured · **`dbstat` unavailable** (row counts, no bytes) · object storage unreachable (`blobs.available:false`, database figures intact, **not** an error page) · under the threshold · over it · a large `nezařazené` row · **a replica several times the size of the live database**, which is normal and must not read as a fault · no backup bucket and no replica configured.
- **Soukromé položky:** **empty (nobody has private items) — and the tab is present anyway (D215), so this is a designed screen, not a fallback**: it says what the tool is for, that it never shows titles, and that opening it is recorded · one member's items · two members' · the purge confirmation · after a purge.
- **Offline** (v5 D71): both trees render read-only from cache; write controls disabled with the standard **"Změny nelze uložit offline"**; Administrace's two new screens are online-only and say so.

### Czech UX copy (use `design:ux-copy`, in Czech)

Vocabulary is **fixed in the PRD (§V9-7)** and must be used verbatim: **Poznámky · Soukromé poznámky · Dokumenty · Soukromé dokumenty · Viditelnost · Sdílené · Soukromé · Publikovat do sdílených · Vlastník · Úložiště · Databáze · Objektové úložiště (R2) · Nezařazené · Zálohovací bucket · Varovný práh · Soukromé položky · Trvale smazat.** The redaction phrase is fixed too and appears in the log and in a push and nowhere else: **"Soukromá položka — podrobnosti skryty."**

Own everything else. The strings doing the most work: the **publish confirmation**, which must carry irreversibility in one sentence; the **folder publish** count line; the **"pro všechny" refusal** on a private item, which has to explain rather than scold; the **empty private root**, which is the only place the feature ever gets to say what it is for; the **scoped-search empty state**; the **`nezařazené` explainer**; the **unmeasured-bytes label** (*nezměřeno*, not *0 B*); the **threshold line**; and the **purge confirmation**, which should be plain to the point of dryness.

Plural forms: *1 položka · 2 položky · 5 položek*; *1 soubor · 2 soubory · 5 souborů*; *1 objekt · 2 objekty · 5 objektů*; *1 tabulka · 2 tabulky · 5 tabulek*. **MB, GB, kB and R2 do not inflect.** Byte sizes Czech-formatted throughout — space thousands separator, comma decimal: **`1,2 GB`**, **`847 MB`**, **`12 345 objektů`**.

⚠ **Do not let the copy promise more than the feature delivers.** Private here means *access-controlled*, not *encrypted*: an admin with the database file reads anything, and an admin can hard-delete a private item without being able to read it. Czech copy implying end-to-end secrecy is a bug, not a flourish.

### Tokens & theme

Dark-first, tokenised, light under `.light`, elevation by surface lightness — all unchanged. What v9 needs:

- **A private/lock treatment that is a token, not a per-screen decision.** Propose `--vis-private` as an **alias** over the existing scale (Path A from §v6 — aliases, not values, so `.light` re-steps free and no new colour enters the system). It must be **neutral, not alarming**: private is the normal state of a private tree, not a warning about one. Pair it with a mandatory text label; the lock is never the sole carrier.
- **Unmeasured vs measured, promoted from v8.** `--el-approx` was introduced for one module's interpolated columns; Úložiště needs the same distinction at platform level — a table cell that could not be measured must be visibly a *different kind of thing* from a small number, with one consistent footnote. Generalise the token rather than defining a second one, and **state the rule once**: a null byte figure renders as *nezměřeno* and never as `0 B`.
- **The warning register is informational, not destructive.** Following the v5 precedent, the threshold colour must not be the destructive red used for delete, must pass AA in both themes, and must carry a **word**, not hue alone.
- **Byte figures are mono and tabular**, like v6's koruny and v8's kWh — and this is a third magnitude family (kB / MB / GB) with its own alignment problem when `847 MB` sits above `1,2 GB` in a column. Specify the alignment rule, not just the font, exactly as v8 had to for mixed-precision koruny.
- **No new chart is required.** If a distribution bar is proposed for the per-module split, §v6's chart rules apply in full — fixed order, 2 px gaps, legend always present, direct labels, secondary encoding mandatory — and it must be **validated**, not settled by eye. ⚠ **Method note carried from §v8 as built:** `getComputedStyle` returns `oklch()` in this codebase and naive parsing of it silently produces nonsense contrast numbers. **Sample rendered sRGB from a canvas instead.**

### Hard problems — address each with a rationale

1. **Two trees, one page, no ambiguity — ever.** The switcher is the entire privacy UX. Getting it wrong puts something private in front of the household, and D182 provides no way back. Solve it with persistent page shape, not with a warning nobody reads twice.
2. **A private tree must feel ordinary.** It is the same tree with a different audience. If it looks like a vault, people will not use it for the mundane things it is actually for; if it looks identical to the shared tree, they will misfile into it. Find the register between those.
3. **An irreversible action in a household app.** *Publikovat do sdílených* has to convey "this cannot be undone" without theatrics, in an app where most actions are soft-deletes and forgiving.
4. **The absence of a special case.** A foreign private item is *nenalezeno*, full stop. Design it once and then design nothing, and say in the system doc why the obvious "this is private" screen is the wrong answer.
5. **A lock that means one thing.** One meaning, defined once, spent nowhere else — including not on disabled controls, which Home already renders visibly.
6. **A page of numbers read four times a year.** Úložiště has no muscle memory to lean on — worse than v8's few-times-a-year case, because the reader is not even the person who filed the data. Every label explicit, every unit stated, no learned iconography.
7. **Honest gaps.** *Nezměřeno* must be as readable as a number and must never be mistaken for zero. This is the second time Home has faced the exact-vs-unknown problem and the first time it has to be a platform token.
8. **A maintenance tool that must not become a browser.** Soukromé položky is the most delicate screen in the version. If browsing it feels good, it is wrong. Say in the system doc what you did to keep it uncomfortable.
9. **A warning that blocks nothing.** The threshold informs; it must not read as a failure, and it must not read as an error the user caused.
10. **Not overpromising.** Copy, iconography and tone must land on *"the household can't see this"* and not on *"this is encrypted"*, because the second is false and the first is the entire feature.

### v9 Definition of done (addendum)

- [ ] **The root switcher** at 375 px and 1440 px, in **both** themes, in both positions — with an explicit argument for why the two roots cannot be mistaken for one another at a glance.
- [ ] **Both trees** designed: shared as today; private empty, private populated, private for a `reader`.
- [ ] **Lock treatment** specified as a token with a mandatory text label, and a written rule for where it is **not** used.
- [ ] **Publikovat do sdílených**: menu placement, the single-item dialog, the folder dialog with its **N descendants** count, and the post-publish moment.
- [ ] **The 404** rendered as the ordinary nenalezeno screen, drawn once, with the rationale stated.
- [ ] **Scoped search**: scope visible in the field, both empty states naming the tree they searched, a private hit in a result list.
- [ ] **Pins**: the "pro všechny" refusal explained rather than hidden; a widget row with a lock; the lock disappearing on publish.
- [ ] **Úložiště** in all its states: fully measured · **dbstat unavailable** · **object storage unreachable** · under/over threshold · a large **nezařazené** row · a **replica larger than the database** · no backup bucket and no replica.
- [ ] The **replika and zálohovací bucket** sit visibly outside the per-module breakdown, so the page's sums read as correct.
- [ ] **Soukromé položky**: the **empty state as a designed screen** (the tab is always present — D215), carrying what the tool is for and that it never shows titles, populated for two members, the typed purge confirmation, and the visible note that opening it is recorded.
- [ ] **Administrace at six tabs** through v7's tab-strip pattern, no horizontal overflow at 375 px.
- [ ] All copy Czech, PRD §V9-7 vocabulary verbatim, three plural forms, Czech byte and date formatting, **and no copy implying encryption**.
- [ ] **`--vis-private` and the generalised unmeasured token validated in both themes**, output pasted into the design system doc; sampled from **canvas sRGB**, not `getComputedStyle`.
- [ ] Accessibility pass (`design:accessibility-review`): the lock is never the sole carrier of "private", the warning register passes AA in both themes, the purge confirmation is keyboard-operable, touch targets ≥44 px, `prefers-reduced-motion` on the switcher transition.
- [ ] Every screen: loading, empty, error, reader, offline, plus the v9-specific states above.

### Do NOT design (v9 additions to the existing list)

- **A per-person sharing UI** — there are two visibilities, not an ACL (brief §9). No share sheet, no member picker, no "share with Andy", no share links, no groups.
- **An unpublish / "make private again" control** — the route does not exist (D182), and a control for it would be a control that fails.
- **A permission-denied screen for a foreign private item** — it is **nenalezeno** (D180). A padlock screen undoes the decision.
- **Any encryption affordance** — no key icon implying end-to-end secrecy, no passphrase, no "zašifrováno" badge, no unlock gesture. Private is access-controlled and the copy must say only that.
- **A Nástěnka widget, a metric, a list, a push, a scheduled summary or a threshold notification** for storage (D199). The Úložiště page is the only surface.
- **Storage history, growth curves, forecasts or "you'll run out in March"** — the snapshot owns no state (D195). No sparkline, no trend arrow, no comparison to last month.
- **Quota UI of any kind** — no per-user allowance, no progress ring toward a limit, no upload blocked by size policy (D196). The only cap in the app remains the existing 50 MB per upload.
- **A cleanup wizard, a "delete all previews" button, or a bucket browser** — `nezařazené` is reported, and the mirror job owns reconciling it.
- **Titles, filenames, content types, thumbnails, previews or downloads anywhere in Administrace** — not on Úložiště, not on Soukromé položky, not in a tooltip, not in a detail drawer (D197, D198).
- **Privacy in any other module** — Úkoly, Okno, Finance, Zahrada and Elektřina are household-wide. No lock icons there.
- **A new nav destination or any change to the four thumb tabs** — v9 adds no route to the shell (D202).
- **Offline writes** — unchanged since v5 D71. No queue, no "uloží se později".

*Backend + frontend for v9 (`HANDOFF-11-privacy-storage.md`) can be built from `PRD.md` §V9 + `openapi.yaml` **0.11.0**; the **two root switchers, the publish flow, Úložiště and Soukromé položky** are reconciled against this addendum once approved — the same rule v2–v8 used. The leak table (PRD §V9-4a), the redaction seam, the storage catalog and the `sqlite_master` completeness test are build concerns, not design.*


---

## §v9 — as built (2026-08-25)

> The v9 addendum above is the brief. This section records what the build settled, so nobody re-opens a question that has an answer. Where the two differ, **the build wins for the two trees and for Administrace**.

**Administrace shipped a TWO-LEVEL tab strip, not one six-tab row.** `TAB_GROUPS` = **Notifikace** (Rozeslat · Pravidla · Souhrny · Doručení) and **Správa úložiště** (Úložiště · Soukromé položky). The reasoning in the code is the one the addendum should have reached: six pills overflow horizontally at 375 px, *and the six are not peers anyway* — four configure notifications, two are maintenance, and grouping them says which is which before anything is read. Level 1 still reuses **v7's tab-strip pattern**, so §12's "no second pattern" holds and D202 is intact.

**The root switcher is `nav` + `aria-current="page"`, not `role="tablist"`/`role="tab"`** (PR #22). The tab roles overrode the anchors' own and promised a screen reader arrow-key navigation and a tabpanel that do not exist — an accessibility regression introduced by reaching for the nearest-looking pattern. **The active private tab is tinted, not merely underlined**, because `--vis-private` sits at the same lightness as `--muted` and a 2 px rule in it read as decoration rather than as state. Both are worth carrying into any future switcher.

**⚠ The Litestream replica line does not exist (D214 declined).** The addendum's §7 asks for the *replika* and *zálohovací bucket* in a separate block below the breakdown; **only the bucket shipped**. The replica renders permanently as its designed "not configured" state — which the addendum had already listed among the states to draw, so no mock is wasted. The design argument survives intact and is the reason the decline was cheap: because the block always sat **outside** the per-module sums, contributing nothing changes no total and the page's arithmetic still reads as correct.

**Tokens landed as proposed, with the promotion the addendum asked for.** `--info`, `--unmeasured` and `--attention` (with `-soft` pairs) are now **platform** tokens; v8's `--el-approx` and `--el-over` become aliases of them; `--vis-private` aliases `--info`. Measured by **canvas-sampled sRGB**, as §v8's method note requires: `--vis-private` **8.48** dark / **6.83** light on `--s1`; text on `--vis-private-soft` **11.56** / **14.50**; `--unmeasured` **5.30** / **4.97**; `--attention` on `--attention-soft` **6.47** / **5.35**. Every one clears AA. The rule that made the palette easy is worth restating: **private is not a warning**, so `--vis-private` never enters the attention or danger family, and the lock is never the sole carrier — the word travels with it.

**Confirmed as designed:** the private tree tinted with a 3 px left rail and the root segment in the breadcrumb, so which tree you are in is carried by the **shape** of the page rather than by a dismissible message — five glanceable carriers in total · *Publikovat do sdílených* in the pin popover's new **Viditelnost** group, owner-only, irreversibility in a sentence rather than in red, the folder variant stating how many items become visible · a foreign private item as the **ordinary nenalezeno screen**, drawn exactly once, with no padlock screen anywhere in v9 · scope named in the search placeholder and in the empty state · *"pro všechny"* on a private item **unavailable and explained**, not hidden · **nezměřeno never rendered as 0 B**, set in proportional italic where a mono figure would sit · the purge screen as a maintenance tool with purge confirmed by **typing the full identifier** and a visible note that opening the list is recorded · and its **empty state as a designed screen** (D215), since the tab is always present.

**One thing the design could not carry, and the build had to.** No mock can show that a background job has no actor. The **preview worker** and the **image GC** both needed unscoped reads, and the failure mode — every private upload stuck at *zpracovává se* forever, with no error state to draw — would have looked like a design bug in a screen that was designed correctly. Worth remembering the next time a module gains both a private scope and an async worker.

---

## v10 addendum (2026-08-26) — Chat (`chat`)

> This is the v10 design addendum, written to fold into the addendum series after the v9 section. It covers *what to design and why*; `PRD.md` **§V10-1…§V10-11** (decisions **D216–D262**) governs *what it does*, and `openapi.yaml` **0.12.0** is the data you'll render. Build guide: `HANDOFF-12-chat.md`; scope brief: `V10-chat-brief.md`. Where this addendum conflicts with the v1–v9 body, **v10 wins for Chat and for the Úložiště page's chat block**.
>
> **This is the first module in Home that is not readable by the household.** v9 introduced *ownership* — a private note belongs to one person. v10 introduces **membership**: a conversation is readable by the people in it, which is neither "everyone" nor "one person". The consequence for design is that **a person can be looking at a room they are only partly inside.** They joined on Tuesday; the conversation started in March; the messages above their first one do not exist for them and never will (D218, D258).
>
> ⚠ **That is the hardest thing in this brief, and it is a copy problem before it is a layout problem.** A thread that simply *starts* somewhere, with no explanation, reads as data loss. A thread that apologises for it on every screen reads as broken. It has to be stated once, calmly, in a place the eye lands on naturally — and it has to be *true* rather than reassuring, because the history genuinely is not coming back.
>
> **The second half is a working screen, not a dashboard.** Attachments accumulate in R2 and two thresholds warn about it. **Úklid úložiště chatu** exists to be used under mild pressure — somebody has been told the chat is over its limit and wants it to stop being true. It is closer to v9's purge screen than to v9's Úložiště: austere, sortable by size, and honest that doing nothing is a legitimate outcome.

### What to design

**1. The thread, and three things about it that are requirements rather than polish.**

- **Which conversation is on screen must be unmistakable** at 375 px, in both themes, one-handed. The cost of getting it wrong is posting into the wrong room, and **there is no unsend** — edit and delete leave a tombstone that everybody has already seen. This is v9's root-switcher problem in a module where the mistake is louder.
- **Own versus others' bubbles must not rely on colour alone.** Alignment and an author label carry the distinction; colour reinforces it. The `--c1…--c5` CVD constraint recorded in §v6 applies here exactly as it does to a chart.
- **Images must not reflow the thread as they load.** The API returns intrinsic `width`/`height` for every image attachment precisely so the space can be reserved. A thread that jumps while somebody is reading it is the most-noticed bug in any chat, and it is a design instruction, not an implementation detail.

**2. The floor, made legible — the v10 equivalent of v9's "which tree am I in".** Three surfaces, one idea:

- **The top of a thread** for a member who joined later: a quiet, permanent line — not a dismissible banner — saying that earlier messages are not part of their history. It is the only place the feature gets to explain itself.
- **A reply whose parent is above the floor** renders **"Zpráva mimo vaši historii"** with no author, no date, no excerpt (D226). Design it as a *shape* that is clearly a quote and clearly empty, so it does not read as a failed load.
- **The members panel** shows each member's `effective_from`, which is what lets the app say plainly that somebody added yesterday cannot read last week. ⚠ And the **removal dialog** must say that re-adding leaves a permanent gap, because nothing afterwards will explain it.

⚠ **One exception to design against confusion, not for it:** **Všichni** — the household room — gives every member the *whole* history, including someone the app meets for the first time years later (D258). So the "you joined later" line appears in created groups and **never** in Všichni. If a mock shows it there, the decision has been misread.

**3. The nav — the first demotion in this app's history (D260).** Four thumb-reachable tabs plus *Více*; a fifth makes six slots at 375 px. **Chat takes a tab; Okno do budoucnosti moves into the overflow.** Okno is the least-daily of the four and the only one whose signal already arrives elsewhere — two Nástěnka widgets and four metrics — while chat is the one screen in the app carrying an **unread badge**, which is a reason to open something rather than a place to end up.

Design the badge: a count on a tab that is often zero and occasionally 40, in both themes, without becoming the loudest thing in the bar. ⚠ **Chat publishes no Nástěnka widget** (D252) — `platform/widgets/registry.tsx` is untouched for the third version running, and a widget in a mock will be built.

**4. Two-pane at ≥1024, stacked below (D262).** Conversation list left, thread right, both rendered at `/chat/{id}` — so a desktop member never loses the unread counts to read a message. Below 1024 the thread is a route push and back returns to the list. **Members are a panel or a sheet, not a third column**: the list is consulted when somebody is added or when the floor needs explaining, not read continuously.

**5. Three attachment states, three renderings, and two of them are text.**

- **`live`** — image inline with its thumbnail; video inline with controls; everything else an icon, a filename and a size. **PDF opens in the browser's own viewer** — there is no preview pipeline in chat (D227), so design the affordance for "opens elsewhere" rather than a preview pane.
- **`removed`** — the epitaph: *"📄 smlouva-2026.pdf · 2,4 MB — Soubor odstraněn při úklidu úložiště · Karel, 25. 8. 2026"* (D243). It keeps the filename and the size on purpose, so the thread still makes sense and somebody can ask for the file again knowing what it was. Design it as a **settled absence**, not as an error.
- **`moved`** — the file still renders, sourced from Dokumenty, with a quiet marker saying where it lives now (D246). Not a warning; a fact.

⚠ **A video that will not play needs a designed state.** There is no transcoding in v10, so an iPhone `.mov` stores fine and may not play in every browser. Draw the fallback — a download and one sentence — rather than leaving a broken player.

**6. The composer.** Drag-and-drop, paste and a file picker; a per-file progress row; **an over-cap file refused before it is uploaded**, naming the limit in MB. Up to ten files per message. Design the multi-file state and the partial-failure state — nine files up, one rejected — because that is what a phone photo roll produces.

**7. The koš, in the conversation list (D253).** A deleted conversation leaves every other surface entirely and appears in a **Koš** section with its name, size and **days remaining**, plus **Obnovit**. Two things to design carefully:

- ⚠ **Its bytes are still counted** against both thresholds until it is actually purged (D254), which is honest and looks wrong. **Smazat natrvalo** is the answer, and the copy has to make the relationship obvious: *deleting frees the space in seven days; purging frees it now.*
- **Deleting requires typing the conversation's name.** Any member can delete a room containing everyone else's files, and the koš is what makes that survivable — but the confirmation still has to convey that this is not an undo-able tap.

**8. The two warnings, and the page they lead to.** The module total appears in Administrace; the per-conversation limit appears **inside the chat, to that conversation's own members** (D237). Both are **informational, never destructive** — nothing is blocked, no upload fails, nobody has done anything wrong — so they reuse §v9's promoted `--attention` register and never the delete red.

⚠ **The link is not rendered for a member who cannot use it.** A `reader` in an over-limit conversation sees the warning and is not offered a button that will 403 them (D241).

**9. Úklid úložiště chatu — a working screen.** Live attachments from the caller's own conversations, grouped by conversation, over-limit rooms flagged, **sorted by size by default** because that is the order in which cleaning pays.

- ***Ponechat* is not a button.** It is what happens when nothing is clicked. Do not design a "review changes" step or a staged-actions tray — *"not every document has to be dealt with at that moment"* is a statement about **state**, not about a control (D242).
- **A moved or removed row is gone on the next load** (D246), not struck through and not greyed. The listing is *what still counts*.
- **Leaving while still over the limit** raises a confirmation naming which threshold and by how much (D244). A confirm, never a block.
- **Sorting by size is single-page.** Design the "that's all this view can show" state honestly rather than a Load-more that will not work.

**10. The move dialog — the one action in v10 that widens access.** ⚠ **A move is a publish** (D245): the file becomes readable by every household member, **including people who are not in this conversation**. The sentence is fixed and must appear before the confirm:

> *"Soubor bude viditelný pro všechny členy domácnosti, i pro ty, kteří nejsou v této konverzaci."*

The folder picker offers **shared folders only**; a private v9 folder is refused. Design the picker so the private roots are *absent and explained*, following the same discipline v9 used for the unavailable household pin — not silently missing, not present-and-failing.

**11. Administrace → Úložiště, chat block.** Total against threshold, then a table of every conversation: name, size, objects, member **count**, over-limit flag, koš state with days remaining. ⚠ **And no way in** — no thread, no attachment list, no clean-up link (D240). Design the absence deliberately: a table of names with nothing clickable reads as broken unless the page says why, so one line of copy explains that clean-up belongs to a conversation's own members. **`Nezálohováno`** is a normal row here, not a warning: chat blobs are deliberately not mirrored (D229).

**12. Reader state — and it is new.** ⚠ **Chat is the first module in Home where a `reader` writes.** They post, reply, edit and delete their own messages, create conversations and manage membership (D222). What they cannot do is clean up: `/chat/uklid` is **403 with the reason named**, and the warning does not offer them the link. This is a recorded asymmetry (D241) — a reader can fill storage they can never clean — and the copy should be straightforward about it rather than vague.

**13. Offline — a deliberate departure from every other module.** ⚠ **Chat is excluded from the PWA persister entirely.** Every other module renders read-only from cache; chat renders an **offline state**, not a stale thread. Message bodies and other members' names on a shared laptop's disk are worth less than the convenience, and v9 already established the threat model — *"a laptop in the kitchen gets used by more than one person"*. Design the offline state so it reads as a deliberate choice, not as a failure to load.

### States — design each (loading, empty, error, reader, + v10-specific)

- **Conversation list:** empty (a new household, only Všichni) · several rooms with unread counts · one room over its limit · a **Koš** section with days remaining · at 375 px and at 1440 two-pane.
- **Thread:** empty conversation · a member who joined later, with the floor line · a busy thread with mixed text, images and video · a thread containing all three attachment states · a tombstone · an edited message · a reply · **a reply to a message above the floor** · the *Nové zprávy* divider · the moment a new message arrives while scrolled up.
- **Composer:** idle · typing · one file attaching · ten files attaching · a file refused for size · a partial failure · offline (disabled, with the standard **"Změny nelze uložit offline"**).
- **Members:** the panel with `effective_from` shown · adding a member (the directory picker, ⚠ **display names only**) · the empty picker, when nobody else has ever logged in · the removal confirmation naming the permanent gap.
- **Deletion:** the typed confirmation · the room in the koš · restored · **Smazat natrvalo** and its confirmation · the koš empty.
- **Warnings:** under both thresholds · module total exceeded (Administrace + chat) · one conversation over its limit, seen by a member · the same, seen by a `reader` (no link) · leaving the clean-up page still over.
- **Úklid úložiště chatu:** with items, grouped and flagged · sorted by size, single-page · a member of no conversation (**an empty page with an explanation, not a 403**) · a `reader` (403, reason named) · immediately after a delete, with the figure already lower · after a move.
- **The move:** the dialog with its publish sentence · the folder picker with private roots absent-and-explained · a refusal · **no sink configured**, where the action is absent rather than failing.
- **Administrace → Úložiště:** the chat block under threshold · over · with a trashed conversation counted · **Nezálohováno** · a household where chat holds nothing.
- **Offline:** the chat route's offline state, at both widths.

### Czech UX copy (use `design:ux-copy`, in Czech)

Vocabulary is **fixed in the PRD (§V10-7)** and must be used verbatim: **Chat · Všichni · konverzace · Nová konverzace · Členové · Přidat člena · Odebrat z konverzace · Smazat konverzaci · Koš · Obnovit · Smazat natrvalo · Odpovědět · Upravit · Smazat zprávu · upraveno · Zpráva byla smazána · Zpráva mimo vaši historii · Nové zprávy · Ztlumit konverzaci · Úklid úložiště chatu · Ponechat · Odstranit · Přesunout do Dokumentů · Soubor odstraněn při úklidu úložiště · Limit pro chat celkem · Limit na jednu konverzaci · Nad limitem · Nezálohováno.** The publish sentence in the move dialog is fixed too (§10 above).

Own everything else. The strings doing the most work, in order of difficulty:

1. **The floor line** at the top of a thread — the hardest sentence in v10. It must say that earlier messages are not part of this person's history, without implying a fault, a permission problem, or something recoverable.
2. **The removal confirmation**, which has to mention that re-adding leaves a gap.
3. **The delete confirmation**, which has to convey that a room full of other people's files is about to go, while the koš makes it recoverable — two facts that pull against each other in one dialog.
4. **The koš / Smazat natrvalo relationship** — *frees the space in seven days* versus *frees it now*.
5. **The reader's 403** on the clean-up page, which should explain rather than scold.
6. **The empty Všichni** for a brand-new household — the only place chat introduces itself.
7. **The epitaph**, the **video fallback**, the **leaving-while-over confirm**, and the **Administrace "no way in" explainer**.

Plural forms: *1 zpráva · 2 zprávy · 5 zpráv*; *1 konverzace · 2 konverzace · 5 konverzací*; *1 člen · 2 členové · 5 členů*; *1 soubor · 2 soubory · 5 souborů*; *1 den · 2 dny · 5 dní*. **MB, GB and R2 do not inflect.** Byte sizes Czech-formatted as established: **`1,2 GB`**, **`847 MB`**.

⚠ **Do not let the copy promise more than the feature delivers.** Members-only means *access-controlled*, not encrypted — the same sentence §v9 wrote about private. An admin with the database file reads anything; an admin can purge a conversation they may never read. And **a push notification carries the sender's name and 140 characters of the message to a lock screen** (D249): copy that implies chat text never leaves the app is wrong.

### Tokens & theme

Dark-first, tokenised, light under `.light`, elevation by surface lightness — unchanged. **v10 should add no new colour values.** Everything it needs already exists or aliases something that does, following §v6's Path A discipline:

- **Own versus others' bubbles.** Propose an alias pair over the existing surface scale rather than a new hue, and **validate the pair by canvas-sampled sRGB** — `getComputedStyle` returns `oklch()` in this codebase and naive parsing of it silently produces nonsense contrast numbers (§v8's method note, restated because this is the third version to need it). ⚠ **Colour is never the sole carrier**: alignment and the author label do the work, and the pair must survive a greyscale screenshot.
- **The unread divider and the unread badge** reuse the accent, not the attention family. Unread is not a warning.
- **The two warning registers reuse `--attention`**, promoted to platform in v9. No new token, and never the destructive red — nothing is blocked and nobody has done anything wrong.
- **The `removed` attachment** wants the **`--unmeasured`** treatment promoted in v9 — a thing that is deliberately not there, visually distinct from a thing that failed. It is the same idea as *nezměřeno* rendered where a figure would sit, and reusing it keeps one meaning rather than inventing a second.
- **The `moved` marker** is neutral body-adjacent text. Not a badge, not a colour — a fact.
- **Byte figures** stay mono and tabular, as v6's koruny, v8's kWh and v9's kB/MB/GB do. The clean-up page is a fourth column of magnitudes and inherits the alignment rule rather than restating it.
- **No new chart.** If a per-conversation distribution bar is proposed for the Administrace block, §v6's chart rules apply in full — fixed order, 2 px gaps, legend always present, direct labels, mandatory secondary encoding — and it must be **validated, not settled by eye**.

⚠ **One thing no mock can carry, carried forward from §v9 as built:** a background job has no actor. v10's drain, koš purge and thumbnail generation all run without a viewer, and if any of them inherits a member-scoped read, the failure looks like a design bug in a screen that was designed correctly — a thumbnail that never appears, an attachment stuck *zpracovává se*, bytes that never fall. Worth remembering the second time a module gains both a scope and an async worker.

---

## §v10.1 — as built (2026-08-29)

The `design/v10_1` bundle against the shipped module. **Two labels erased and one feature added** — `design/v10/Home.dc.html` and `design/v10_1/Home.dc.html` differ by **92 diff lines once CR is stripped**, and `CardTile`, `DocumentView`, `NoteView`, `support.js` and `github.md` are byte-identical between the two snapshots.

**Reactions** (PRD D265 — ⚠ the bundle numbers this D264 and that number was taken on 2026-08-28). Seven emoji, a chip with a count under the bubble, who reacted under the cursor. Built as drawn, with three notes the mock could not carry:

- **The chips sit between the body and the timestamp**, which is the mock's placement and an argument rather than a preference: a reaction belongs to *what was said*, not to *when*.
- **The palette is a strip in the flow, not a floating popover.** The desktop mock draws it as an absolutely-positioned bar above the ☺; a popover anchored to a bubble inside a scroll box has to be repositioned on every scroll frame, which is the one thing a thread somebody is reading cannot afford. Below the chips it simply moves with them, and the phone's version was already that shape.
- **44 px under a thumb and 26–34 at the desk**, per the mock. ⚠ Seven 44 px targets do not fit a 298 px bubble at 375 px, so the bar scrolls horizontally inside its own `om-scroll` container — which is what the mock does too. The page itself does not scroll.

**The erased labels.** The list pane's subtitle explained the access model on a line read once and then scrolled past forever; the composer's hint parked a paragraph of documentation where the member types. ⚠ **Neither cap stopped being stated** — the over-cap refusal still names the MB beside the file it refused, which is where a limit is worth reading. The model still explains itself where it bites: the floor line, the members panel, and `celá domácnost` on Všichni's own row.

**The row's preview took the member count's line**, which the mock's `c.preview` already showed and the build had not implemented. That is a trade rather than an addition: the row has one line to spend there, *5 členů* changes twice a year, and what the column is scanned for is whether anything was said and by whom.

⚠ **One thing no mock could carry, and it is the design-side lesson of this pass.** A `truncate` line is `white-space: nowrap`, and a grid item's default `min-width: auto` resolves to its **min-content** width — so the moment a row's second line became a real sentence, the list pane's minimum width became that sentence's width. It measured **415 px inside a 375 px grid** and the pane clipped: the ＋ button lost its right half, every timestamp read *21* instead of *21:45*. The mock is right and the implementation of it was wrong in a way only a browser at 375 px could show. **Any mock that puts variable-length text on a fixed-width pane carries this hazard**, and the fix is a `min-w-0` on whichever box the width has to stop at — the width twin of the `min-h-0` v10 already needed twice on these same two panes.

---

## §v10.2 — as built (2026-09-05)

The `design/v10_2` bundle against the shipped shell. **Nothing here is a module** — it is the frame the eleven modules are drawn inside, and it is the first pass that ever looked at it on its own. `design/v10_1/Home.dc.html` and `design/v10_2/Home.dc.html` differ by **161 diff lines once CR is stripped**; most of them are v10.1's as-built corrections folded back into the bundle, and `CardTile`, `DocumentView`, `NoteView` and `support.js` are unchanged in content — the v10_2 snapshot was written with LF where v10_1 has CRLF, so those four differ by nothing but line endings.

**The phone's app header was never designed, and that is the finding of the pass.** A 61 px strip carrying *home*, a theme toggle and a sign-out button has been above every screen at 375 px since v1 and appears in **no artboard of any version**. It survived nine releases because every pass compares a MODULE to its mock, and the shell is what is left over when you have done that eleven times: **drift in shared chrome is invisible to a per-module review.** Deleted (PRD D272), and the first row under the status bar now belongs to the screen somebody is standing on.

⚠ **The deletion was the easy half.** `--chat-chrome-top` is that header measured, and it is subtracted from the viewport in the one height calc chat renders — so removing the header without zeroing the token leaves the thread 61 px above the tab bar with a dead strip under the composer, which is the same defect the old `8rem` produced from the other direction. **Any mock that deletes chrome carries this hazard**, and the answer is that a stylesheet which measures the shell has to be read whenever the shell changes shape, not only when it changes colour. Verified in a browser at 375 × 812: 755 px of box against a 57 px bar, and no page scroll.

**The side nav's foot became a person** (D273). Four stacked full-width buttons under a bare email are one row: the initial, the name, the role, and a ⚙ to Nastavení with the same active state the list rows carry. The theme toggle and *Odhlásit se* moved into Nastavení, where the theme already was — the shell had been carrying a second copy of each in the sidebar and a third in the phone's header.

⚠ **The bundle's prose and its artboards disagree once.** Hard problem 58 puts the theme toggle and *Odhlásit se* in the phone's "Více" sheet; the sheet drawn in the same file holds modules and Nastavení, and Nastavení has carried both controls since v5. The artboards were built, the prose was not, and the disagreement is recorded rather than reconciled — for the same reason the D264 collision was.

**The version is a label, not a notice** (D271) — `v10.2 · a3f9c2e`, mono and quiet, in the side nav's foot and at the bottom of the "Více" sheet, taking the line from a hint that described the sheet to somebody already looking at it. D26 still holds: this is not an update prompt, and there is nothing to press.

⚠ **One token was not followed, and it was measured rather than argued.** The artboards specify `--subtle` for the version label. `--subtle` on `--s1` is **4.29:1** in the light theme, under the 4.5:1 AA bar for text this size — the identical measurement `theme/globals.css` already recorded for the foreign bubble's author label, where it chose `--muted` (6.81:1). The same swap fixed three more usages the axe sweep found on the sidebar's `admin` tags and in Nastavení, **two of which had been failing the suite since before this pass**. The pairing is wider than four usages: the design system's own note records the audit lifting the DARK `--subtle` 0.560 → 0.660 while the light value stayed at 0.580, and that is where the general fix belongs.
