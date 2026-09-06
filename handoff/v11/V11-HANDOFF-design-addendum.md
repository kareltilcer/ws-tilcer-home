<!--
  HANDOFF-design.md §v11 — append after "## §v10.2 — as built (2026-09-05)".
  Written 2026-09-06. Governs v11's two new surfaces and the Log's new chip.
-->

## v11 addendum (2026-09-06) — Asistenti (MCP) (`platform/settings` · `admin` · `logging`)

> This is the v11 design addendum, written to fold into the addendum series after §v10.2. It covers *what to design and why*; `PRD.md` **§V11-1…§V11-11** (decisions **D275–D323**) governs *what it does*, and `openapi.yaml` **0.15.0** is the data you will render. Build guide: `HANDOFF-13-mcp.md`; scope brief: `V11-MCP-brief.md`. Where this addendum conflicts with the v1–v10.2 body, **v11 wins for the two Asistenti surfaces and for the Log's `via` chip**.
>
> **v11 adds no module, no route and no nav entry.** It adds a **credential** — the first one this application has ever asked a person to hold. Every screen in Home so far has shown the household its own data. These two show a member something *about themselves*: what they have connected, when it last did anything, and how to make it stop.
>
> ⚠ **The hardest thing here is a pattern Home has never drawn: a value shown once and never again.** Every other screen can be re-opened. This one cannot — only the SHA-256 is stored, so a member who dismisses the dialog without copying has lost the token and must mint another. **That is not a failure state to apologise for; it is the feature.** But it means the dialog carries a weight nothing else in this app carries, and a dialog that looks like every other dialog will be dismissed like every other dialog.
>
> **The second half is quieter and matters more over time.** A token list is only useful if a person can look at it in eighteen months and answer *"do I still need this?"* — which means **naposledy použito** is the most important column on the screen and should not be the smallest text on it.

### What to design

**1. Nastavení → Asistenti (MCP) — a fourth section.**

`platform/settings/NastaveniPage.tsx` already carries three `<section>` panels: **Oznámení**, *Vzhled a účet* (D273) and *Aplikace*. ⚠ **The first one is "Oznámení", not "Notifikace"** — *Notifikace* is the **Administrace** level-1 group's label. The two screens use different words for the same subject and both are correct; copying the wrong one into a mock is the exact cross-screen swap this note exists to prevent. Asistenti is the **fourth**, and it sits **last** — it is the least-visited of the four and the only one a member may never open at all.

⚠ **The empty state is the only place this feature ever explains itself.** There is no onboarding, no tour, no nav entry and no widget. One short Czech paragraph — what a token is, that it acts as you, that it can be revoked at any moment — and the button. Write it as something a person reads once and does not need again, not as marketing.

**2. The mint dialog — three fields, and one of them is a scope.**

- **Název** — free text, ≤ 60 chars. Placeholder that teaches by example: `Claude na notebooku`. The name is what appears in the Log next to the member's own, so the copy should hint that it will be read later by somebody trying to remember what this was.
- **Platnost** — four choices, not a date picker: **30 dní · 90 dní · 1 rok · bez omezení** (D285). A picker invites a date nobody can justify; four choices are a decision.
- **Rozsah** — which modules this token may reach. Default **all**, presented as a collapsed summary (*Všechny moduly*) that expands to a checkbox list of the nine.

⚠ **Rozsah must not look like a security control**, because it is not one (D288). It narrows what the assistant is offered; it does not narrow what the member can see. The helper text says so plainly — *"Omezí, co asistent uvidí. Nemění vaše oprávnění."* — and the section is not styled with any lock, shield or privacy signal.

**3. ⚠ The reveal — the one screen in this app that cannot be re-opened.**

Requirements, not polish:

- **It does not dismiss on outside-click or Escape.** Only an explicit **Hotovo** (D315). This is the single deviation from every other dialog in Home, and it is deliberate: the standard dismissals are muscle memory, and muscle memory here costs the token.
- **One secret, rendered once, in one place.** See item 4 — the connect snippet *contains* the token, so do not also render the bare token above it. Two copies of a secret on one screen doubles the chance one of them ends up somewhere it should not, and doubles the vertical space at 375 px where there is none.
- **A copy button with a confirmed result.** *Zkopírováno* announced to assistive technology (`aria-live="polite"`), not only shown. A copy that silently failed is indistinguishable from one that worked until the paste.
- **The warning line is above the value, not below it.** *"Token se zobrazí jen jednou. Zkopírujte si ho teď."* A caution read after the thing it cautions about is decoration.
- **Focus lands on the copy button** when the dialog opens, and is trapped. **Hotovo** is the only exit and it is not the initially focused control.

⚠ **A 48-character monospace string is the worst thing yet asked to fit 375 px.** It scrolls **inside its own container** (`overflow-x: auto`), never wrapping mid-string and never widening the page. ⚠ Do not break it across lines for looks: a wrapped secret invites a partial selection, and a partial token fails with an error that says nothing useful.

**4. The connect snippet — new, and worth the space.**

The token on its own is not usable; it has to reach a client's config. Render, **inside the reveal dialog**, a ready-to-paste JSON block with the token already in it, plus the server URL. That is what the member actually needs, and it makes the "copy" action produce a working thing rather than a string they must then look up how to use.

- The **primary** action copies the whole snippet. A **secondary, quieter** action copies just the token, for a client that wants it in an env var.
- The snippet is a code block in the same monospace, scrolling in its own container.
- ⚠ **This does not make the secret appear twice** — it appears once, inside the snippet. The two buttons are two ways to copy one rendering.
- One line under it points at where this goes, without becoming documentation: *"Vložte do konfigurace svého MCP klienta."*

**5. The token list — six things per row, and one of them decides everything.**

| Field | Notes |
|---|---|
| **Název** | the member's own words; the strongest thing in the row |
| **prefix** | `hmcp_9tK…` — monospace, dimmed. It exists so a member can match a row to a config file **by eye** |
| **Rozsah** | *Všechny moduly*, or a count with the names on hover / tap |
| **Platnost do** | a date, or **bez omezení** |
| **Naposledy použito** | ⚠ **the most important column** — a relative date (*před 3 dny*), with the absolute one available. **nepoužito** when null |
| **Odvolat** | see item 7 |

**Four states, and they are not four colours.**

- **Aktivní** — ordinary.
- **Nepoužito** — active and never called. Not an error; a fact, and a useful one: it usually means the config never landed.
- **Vyprší brzy** (≤ 14 days) — a quiet marker. ⚠ **Not the `--attention` register and never the danger family.** An expiry a member chose is not a problem; it is the thing working. §v9's rule that *private is not a warning* applies here to *expiring*.
- **Vypršel / Odvolán** — dimmed, kept in the list, with the date. **Never removed**, because a token named in a Log row must still be identifiable years later (§V11-6).

⚠ **Sort by `last_used_at` descending, nulls last.** The row a member is looking for is almost always the one that has just done something, or the one that has never done anything.

**6. Administrace → Asistenti → Tokeny — a third level-1 group.**

The two-level tab strip currently carries **Notifikace (4** — send, rules, summaries, deliveries**)** and **Správa úložiště (3** — `storage`, `private`, `limits`**)**. v11 adds a **third group with one tab**, taking the strip from seven tabs to eight. (⚠ `AdministracePage.tsx:45` cites *D263* for the third storage tab; the PRD's D263 is about the threshold verb. Trust the code's tab list, not that citation — and the file's own header comments say "four tabs" and "at six tabs", both stale.)

⚠ **D202's own text says "six tabs" and is already stale by one. Do not copy the number out of it — count the code.**

The tab lists **every member's** tokens: owner, name, prefix, rozsah, created, expiry, last used, last IP, and **Odvolat**.

⚠ **There is no admin mint, and there must be no affordance that looks like one** (D316). This is D240's shape — *names and sizes, and no way in* — applied to credentials: an admin may see that a key exists and take it away, and may not make one in someone else's name. If a mock shows a "Vytvořit pro…" control, the decision has been misread.

**7. Odvolat — the one destructive action on either screen.**

Revoking is immediate, irreversible and takes effect on the assistant's very next call. It gets the **danger** register and a confirmation naming the token — but **not** a type-the-name confirmation (v10's conversation-delete pattern): the cost of revoking by accident is minting another one, which is a minute, and the cost of friction here is a member who does not revoke a token they are unsure about.

The admin's confirmation additionally names the **owner**, because revoking someone else's credential is a different act from revoking your own.

**8. The Log chip — Přes asistenta.**

One filter in the Log browser (**Vše · V aplikaci · Přes asistenta**) and one chip on rows where `via='mcp'`, reading the **token's name**.

- The chip is **neutral**, not a warning. An assistant's write is an ordinary change with a known origin; styling it as an alert would make the Log's most common future row look like an incident.
- ⚠ **The actor line already reads `Karel · Claude (notebook)`** (D291). The chip must not repeat the token name a second time in the same row — if the label carries it, the chip is an icon plus *Asistent*. Resolve this once, in the mock, and say which.
- ⚠ **The chip must survive a null join.** A token row is never deleted, but an event restored from a different point could find nothing; render the prefix, never a blank chip.

### Copy (Czech, fixed)

| Where | Text |
|---|---|
| Section title | **Asistenti (MCP)** |
| Empty state | *"Zatím nemáte žádný token. Token umožní AI asistentovi číst vaše data a zapisovat do nich vaším jménem. Kdykoli ho můžete odvolat."* |
| Mint button | **Vytvořit token** |
| Expiry options | **30 dní · 90 dní · 1 rok · bez omezení** |
| Rozsah default | **Všechny moduly** |
| Rozsah helper | *"Omezí, co asistent uvidí. Nemění vaše oprávnění."* |
| Reveal warning | **Token se zobrazí jen jednou. Zkopírujte si ho teď.** |
| Snippet hint | *"Vložte do konfigurace svého MCP klienta."* |
| Copy actions | **Kopírovat konfiguraci** · *Kopírovat jen token* |
| Copy result | *Zkopírováno* |
| Reveal exit | **Hotovo** |
| Last used | **naposledy použito** · **nepoužito** |
| Expiry | **platnost do** · **bez omezení** · **vyprší brzy** |
| Dead states | **vypršel** · **odvolán** |
| Revoke | **Odvolat** |
| Revoke confirm | *"Odvolat token „{název}"? Asistent ztratí přístup okamžitě."* |
| Admin revoke confirm | *"Odvolat token „{název}" člena {jméno}? Ztratí přístup okamžitě."* |
| Admin tab | **Asistenti** → **Tokeny** |
| Log filter | **Vše · V aplikaci · Přes asistenta** |
| Log chip | **Přes asistenta** |
| Max tokens | *"Máte maximální počet tokenů (10). Odvolejte některý starý."* |

### Tokens (colour)

**No new colour token.** Reuse: ordinary text and `--subtle` for the metadata columns; the **danger** family for *Odvolat* only; **neutral** for the Log chip and the *vyprší brzy* marker.

⚠ **`--subtle` on `--s1` / `--s2` / `--accent-soft` fails AA in the light theme wherever it is drawn** (open since v10.2, five usages fixed and the rest unmeasured). These screens are **metadata-dense** — prefix, dates, last-used, scope are all secondary text — so they are the worst possible place to inherit that defect. **Measure the contrast on this bundle's own surfaces** rather than assuming the platform token is safe, and if it is not, this is the version that finally fixes it.

### The phone

⚠ **v10.2's lesson applies before a line is drawn: drift lives in shared chrome, and no per-module review sees it.** The phone's app header existed for nine releases in **no artboard of any version**.

So: **the 375 px artboards ship in this bundle, not after it.** Specifically —

- Nastavení → Asistenti at 375 px, with two tokens listed. Nastavení keeps its full row in the *Více* sheet (D273), so the phone reaches this screen exactly as it always has.
- **The reveal dialog at 375 px**, with the snippet block scrolling inside itself and both copy buttons reachable one-handed. This is the artboard most likely to be wrong and least likely to be checked.
- The Administrace strip at 375 px with **three groups and eight tabs**, showing it does not overflow.

### Accessibility

- The reveal dialog is a **modal** — focus trapped, focus restored to *Vytvořit token* on close, `aria-modal`, labelled by its heading.
- The copy result is announced (`aria-live="polite"`), not only shown.
- The four token states are distinguished by **text**, never by colour alone.
- Touch targets ≥ 44 px, including *Odvolat* in a dense row.
- **The axe sweep is green for the first time as of v10.2** — cover both new surfaces and keep it green. It is worth more now than it has ever been, precisely because it was red for five releases and nobody knew.

### Do NOT design

- **An admin mint**, or anything shaped like one (D316).
- **Any way to see a token again.** No "reveal", no "show", no masked field with an eye. Only the hash exists.
- **A per-tool consent screen, an approval queue, or a "the assistant is asking to…" prompt.** A token is trusted the moment it is minted (NG4); the control is which verbs exist at all.
- **An OAuth consent screen.** That is v12 (D322), and drawing it now sets an expectation v11 cannot meet.
- **A live feed of what the assistant is doing.** The Log answers this after the fact; there is no socket on the MCP path and none is planned (D277).
- **A Nástěnka widget.** `platform/widgets/registry.tsx` is untouched for the **fourth** version running — and a widget in a mock will get built.
- **A nav entry.** v11 adds no route.

### Definition of done

- Both surfaces at **1440, 768 and 375**, in **both themes**.
- The reveal dialog in **three states**: fresh, copied, and the max-tokens refusal.
- The token list in **five states**: empty, one active, never-used, expiring-soon, and a list containing a revoked row.
- The Administrace tab with **two members' tokens**, showing the owner column doing real work.
- The Log row **with** the chip and **without** it, side by side, so the difference is legible at a glance rather than in theory.
- Contrast measured on the actual surfaces, `--subtle` included.
