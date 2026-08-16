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
- **Editor:** name; a **schedule builder** — a **time picker** (`HH:MM`, Prague) + a **day pattern** control (**Každý den · Všední dny · Víkend · vybrané dny · N-tého v měsíci**) as a chip/segmented row that stays one-handed at 375 px; the **composer** (§2) with **metric tokens** from the catalog; audience; enable toggle. Make the two worked examples (08:00 and 20:00 from the PRD) trivial to build.

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
