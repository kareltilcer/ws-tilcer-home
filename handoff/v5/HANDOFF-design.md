# Home — Design Handoff (Claude Design)

> **Read first:** root `CLAUDE.md` (project conventions), then `PRD.md` (source of truth for behaviour), `openapi.yaml` (data shapes you'll be rendering), and `notes.md` (resolved decisions). This brief covers *what to design and why*; the PRD governs *what it does*.
>
> Status: v1 brief issued 2026-07-19; **v2 addendum — 2026-07-21** (login + widget host); **v3 addendum — 2026-07-29** (Poznámky / `notes`); **v4 addendum — 2026-08-11** (Dokumenty / `documents`); **v5 addendum — 2026-08-16** (Administrace / `admin` + push notifications + PWA, § v5 at the end). Owner: Karel · Precedes implementation (Claude Code) — design informs the build, not the other way round.
>
> **The body of this brief describes the v1 four-module design (delivered and approved). v2 changes two things — login is now home-hosted, and Nástěnka is a widget host. v3 adds a fifth module, Poznámky. v4 adds a sixth module, Dokumenty (file storage), and re-solves the mobile nav for six destinations. Read the `## v2 addendum`, `## v3 addendum`, and `## v4 addendum` sections at the end before starting; where an addendum conflicts with the v1 body, the addendum wins for its own screens. The v4 nav section supersedes the v3 nav item.**

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
- **Push or email notifications** — reminders are **in-app only**, seen when Nástěnka is opened. Design no notification settings, no delivery preferences, no notification centre.
- **Time of day on events** — events are all-day. No time pickers anywhere.
- **Per-occurrence editing of a recurring event** — there is no "this occurrence only" option. Series-only, with the warning described above.
- **Card assignee** — deliberately absent; accountability comes from the audit log, not per-card assignment.
- **Collaborative cursors / simultaneous co-editing** — changes sync, but editing is last-write-wins.
- **External calendar sync, iCal import/export.**
- **Offline / PWA install.**
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
