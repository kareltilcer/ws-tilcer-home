# Home — Design Handoff (Claude Design)

> **Read first:** root `CLAUDE.md` (project conventions), then `PRD.md` (source of truth for behaviour), `openapi.yaml` (data shapes you'll be rendering), and `notes.md` (resolved decisions). This brief covers *what to design and why*; the PRD governs *what it does*.
>
> Status: v1 brief issued 2026-07-19; **v2 addendum — 2026-07-21** (login + widget host); **v3 addendum — 2026-07-29** (Poznámky / `notes`, § v3 at the end). Owner: Karel · Precedes implementation (Claude Code) — design informs the build, not the other way round.
>
> **The body of this brief describes the v1 four-module design (delivered and approved). v2 changes two things — login is now home-hosted, and Nástěnka is a widget host. v3 adds a fifth module, Poznámky. Read the `## v2 addendum` and `## v3 addendum` sections at the end before starting; where an addendum conflicts with the v1 body, the addendum wins for its own screens.**

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
