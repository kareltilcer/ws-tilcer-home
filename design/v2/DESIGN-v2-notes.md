# Home — v2 design addendum (notes)

Companion to `home-v2-prototype.html`. Covers only the **new v2 screens** — home-hosted login and the widget host. The v1 design system (tokens, type, motion, Czech rules, the Úkoly/Okno/Log screens) is unchanged and remains the source of truth; this addendum **extends** it, reusing the same tokens verbatim.

> Brief: `../HANDOFF-design.md` §v2. Behaviour: `../PRD.md` (D23–D29). Contract: `../openapi.yaml` 0.3.0.

## What's in the prototype

Open `home-v2-prototype.html` and use the top bar: **Obraz** (screen), **Stav přihl.** (login states), **Nástěnka** (Uspořádat / Prázdná / Reset), **Motiv** (dark default / light), **Šířka** (both / 1440 / 375).

**1. Přihlášení (login, Mode B — D23)**
- Email + password + "Přihlásit"; brand mark; dark-default, Czech.
- States: idle, submitting (spinner), **bad credentials** (generic — never says which field), **disabled account**, **MFA→auth** (message + "Pokračovat na auth.tilcer.cz", no MFA UI built).
- Links: "Zapomněli jste heslo?" → auth-hosted reset; "Nemáte účet? Požádejte správce." **No signup / reset / TOTP / Google screens** (all auth-hosted).

**2. Přesměrování / signed-out** — minimal branded frame with a spinner while the SPA checks the session (`GET /api/auth/session`). This was also the v1 gap.

**3. Nástěnka (widget host — D24/D27/D28)**
- **Grid:** 2 columns on desktop (a widget is **narrow** = 1 col or **wide** = 2 col), single column on mobile.
- **Three widgets, contents reused from v1:** Právě dělám (wide; tasks in `kind=now` across boards, grouped by board), Připomínky (narrow; active reminders, overdue-first with the danger tint), Tento měsíc (narrow; read-only look-ahead).
- **Arrange mode** ("Uspořádat"): each widget gets a dashed action bar — **grip** (drag; also keyboard `↑/↓`), **Úzký/Široký** size toggle, **Skrýt**. An "＋ Přidat widget" button opens the **catalog** sheet to toggle widgets on/off.
- **Empty / first-run** state ("Nástěnka je prázdná" + add affordance) — a deliberate calm state, not a broken page.
- **Done gesture (D22):** the circular control on task/reminder rows is **press-and-hold 2 s** (visible fill); **keyboard `Enter`/`Space` completes immediately** (the mandatory accessible path). Short tap does nothing.

## shadcn/ui mapping (new elements)

| Element | shadcn/ui | States |
|---|---|---|
| Login form card | `Card` + `Input` + `Button` | idle, submitting, error (Alert) |
| Login error / MFA banner | `Alert` (destructive / warning) | credentials, disabled, mfa |
| Widget frame | `Card` | default, arrange (accented ring) |
| Arrange size toggle | `ToggleGroup` | narrow / wide |
| Hide / Add | `Button` (ghost / outline) | — |
| Catalog sheet | `Dialog` (desktop) / `Sheet` (mobile) | — |
| Widget on/off | `Switch` | on / off |
| Drag handle | custom + `dnd-kit` (keyboard sensor) | idle, dragging, focus |
| Hold-done control | custom (pointer + `Progress`-style fill) | idle, holding, done, focus |
| Redirect frame | `Skeleton`/spinner | — |

Nothing beyond shadcn/ui + Radix + Tailwind except **dnd-kit** (widget reorder — with its keyboard sensor, so reorder is operable without a mouse).

## Decisions reflected

- **D23** login is home-hosted, password-only; no signup/reset/MFA/Google screens.
- **D24** per-user layout: show/hide + drag-reorder + narrow/wide; responsive grid.
- **D27** exactly the three widgets; catalog is that fixed set (no user-authored widgets).
- **D28** widgets are framed as module-provided; the host chrome is generic (the design implies the boundary — the host never styles feature internals, it renders a widget body the module owns).
- **D22** press-and-hold 2 s + immediate keyboard path, carried into the widgets.
- **D20/D21** Czech throughout; dark default with light under `.light`.

## Accessibility notes

- Reorder and resize are **keyboard-operable** (grip is focusable; `↑/↓` move; size toggle is a normal button group). Drag is an enhancement, not the only path.
- The hold-done control exposes a normal action label to assistive tech and **completes immediately on keyboard activation** — the 2 s hold is a pointer-only guard, never a barrier.
- `prefers-reduced-motion` disables the fill/spinner animations (inherited from the v1 system).
- Contrast: same token pairs as v1 (already AA in both themes); the only new colour use is the overdue danger tint on reminder rows, which matches v1's reminder treatment.

## Reconciliation / open points for Karel

- **This is a fresh mockup in the v1 visual language, not a Claude Design export.** If you'd rather Claude Design produce the addendum in the same tooling/bundle format as v1, this doc + prototype are a complete brief to hand it.
- **Widget frame chrome** (how much header each widget shows, whether counts belong in the frame or the body) is proposed, not prescribed — easy to adjust.
- **Mobile arrange**: the prototype uses the same grip + buttons on mobile; if drag-on-phone feels awkward in testing, the up/down keyboard move doubles as an explicit control (as the board's "Přesunout do…" does for cards).
- Not yet designed because they're unchanged from v1 and out of this addendum's scope: the Úkoly board, Okno form, Log browser.

## v2 addendum — definition of done (from the brief)

- [x] Login + signed-out states, all error cases incl. MFA-redirect; no signup/reset/TOTP/Google.
- [x] Widget host: grid at 375 px (1 col) and 1440 px (2 col), narrow vs wide, arrange (add/hide/reorder/resize) with a keyboard path, empty + first-run — both themes.
- [x] v1 widget contents reused in consistent widget frames.
- [x] Nothing from the Do-NOT-design list; no signup/reset/MFA/Google.
