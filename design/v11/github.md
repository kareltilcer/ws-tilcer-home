repo: kareltilcer/ws-tilcer-home
branch: main
path: design, frontend/src, handoff

## Last sync
date: 2026-09-06T16:20:00Z
<!-- commit sha omitted: only a tree hash was resolved (bdd6831bb70c), not a commit -->

### Updated in this project
- **v11 (Asistenti / MCP) is designed and folded into `Home.dc.html`.** Built from `handoff/v11/V11-HANDOFF-design-addendum.md` and `handoff/v11/HANDOFF-13-mcp.md` (openapi 0.15.0, decisions D275–D323). v11 adds **no module, no route and no nav entry** — two surfaces, one Log chip and one credential.
- **Nastavení → Asistenti (MCP)** is the fourth panel and sits last (desktop + 375). Empty state is one Czech paragraph and the button; the list carries name · prefix · rozsah · platnost do · **naposledy použito** · Odvolat, sorted by `last_used_at` descending with nulls last. Five list states are switchable from a demo strip (prázdný · jeden aktivní · nepoužitý · vyprší brzy · s odvolaným · plný · 10 = maximum).
- **The reveal dialog is the one screen that cannot be re-opened.** No backdrop click, no Escape, `Hotovo` the only exit, warning **above** the value, focus on *Kopírovat konfiguraci*, `aria-live` result. The secret is rendered **once, inside the connect snippet** (`mcpServers` + `https://home.tilcer.cz/mcp`); the quieter *Kopírovat jen token* is a second way to copy one rendering. 48 chars scroll inside their own container at 375 and never wrap. Third state: the max-tokens refusal, which is a sentence and not a greyed button.
- **Rozsah is not a security control** (D288): collapsed *Všechny moduly* summary expanding to the nine module checkboxes, helper *"Omezí, co asistent uvidí. Nemění vaše oprávnění."*, no lock, shield or privacy signal.
- **Administrace → Asistenti → Tokeny** is a third level-1 group with one tab — the strip goes from seven tabs to **eight** (counted from `GROUPS` in the code, not from D202's stale text). Two members' tokens with the owner column doing real work, last IP, `Odvolat`, and **no mint affordance** (D316) with the reason written on the page.
- **Log**: filter `Vše · V aplikaci · Přes asistenta` (eighth filter dimension), and a **neutral** chip on `via='mcp'` rows. Resolved once: the actor line carries the token name (`Karel · Claude (notebook)`, D291), so the chip reads only *Přes asistenta*; a null join renders the prefix in the actor line. Seeded rows include both audited reads (`chat.read`, `notes.private.read` — container, never content) and a null-join row.
- **`--subtle` finally fixed** (open since v10.2). Measured off an sRGB canvas on this bundle's own surfaces: light 0.580 → **0.515** (s1 5,60 · s2 5,26 · s3 4,91 · s4 4,56), dark 0.660 → **0.700** (6,77 · 6,30 · 5,65 · 4,92). Stated rule: metadata is **never drawn on `--accent-soft`** — no value that keeps its distance from `--muted` passes there (0.700 gives 4,05); use `--muted`.
- **Doc side:** a *Nové v v11* card, a v11 token section (states as words, the secret's rendering rules, the three registers), hard problems **82–85**, three new a11y notes, six new state-matrix rows, version label **v0.11.0** and the app stamp **v11.0 · dev** (no invented build hash — the backend has no build stamp).
- **Not designed, deliberately:** admin mint, any way to see a token again, per-tool consent, an OAuth screen (v12 / D322), a live feed of assistant activity, a Nástěnka widget (registry untouched a fourth version running), a nav entry.

## Screen map
| Design (Home.dc.html) | Repo source |
|---|---|
| Design tokens / `:root` · `.light`, `--subtle` (v11 AA fix) | frontend/src/theme/globals.css |
| APLIKACE · Nastavení → Asistenti (MCP) — seznam, prázdný stav, stavy | frontend/src/platform/settings/NastaveniPage.tsx (fourth `<section>`, last) |
| APLIKACE · Nový token — název · platnost · rozsah | platform/settings/McpTokenDialog.tsx (new) → `POST /api/mcp/tokens` |
| APLIKACE · Token zobrazený jednou + connect snippet | platform/settings/McpRevealDialog.tsx (new) — `McpTokenCreated.secret`, never persisted, excluded from the TanStack persister (platform/pwa/persist.ts) |
| APLIKACE · Odvolat (vlastní i cizí) | `DELETE /api/mcp/tokens/{id}` · `DELETE /api/admin/mcp/tokens/{id}` |
| APLIKACE · Administrace → Asistenti → Tokeny | frontend/src/modules/admin/AdministracePage.tsx (`GROUPS`), McpTokensTab.tsx (new) → `GET /api/admin/mcp/tokens` |
| APLIKACE · Log — filtr původu, čip „Přes asistenta" | frontend/src/modules/logging/LogPage.tsx, query.go `eventCols` + `via`/`via_token_id`, LEFT JOIN `mcp_tokens` |
| Backend (kontext, ne návrh) | platform/mcp (host, dispatcher, routes), platform/auth/tokenstore.go, migrations `02005_mcp_tokens.sql` + `01003_audit_via.sql` |
| APLIKACE · nav shell — bez změny (v11 nepřidává položku) | frontend/src/app/AppShell.tsx, app/routes.ts |
| APLIKACE · Chat — seznam, vlákno, reakce, gesta, skladatel | frontend/src/modules/chat/* |
| APLIKACE · Administrace → Úložiště · Soukromé položky · Limity | frontend/src/modules/admin/{StorageTab,ChatStorageBlock,LimitsTab}.tsx |
| APLIKACE · Nástěnka | frontend/src/modules/dashboard/NastenkaPage.tsx, platform/widgets/registry.tsx — **untouched, four releases running** (D252) |
| APLIKACE · Poznámky · Dokumenty · Úkoly · Okno · Finance · Zahrada · Elektřina | frontend/src/modules/{notes,documents,todo,events,finance,garden,electricity}/ |
| Data model / copy | frontend/src/api/types.ts, api/keys.ts (`['mcp','tokens']` · `['admin','mcp-tokens']`), i18n/cs.ts |

## Sync history
- 2026-09-05 — v10.1 as built (Chat): reactions renumbered **D265**, the reaction palette became an overlay hung under the message row, chip labels corrected to `❤️ vy, Petr`, three touch gestures (D268), the conversation row's last-message preview bounded by the floor (D266), `/chat` opens the last-opened conversation at ≥1024 (D269), hard problems 78–81, version label v0.10.1. Design doc matched `design/v10_1` plus those corrections; a `design/v10_2` snapshot was to be cut from it.
- 2026-08-26 — v10 (Chat): the eleventh module and the first the household does not read in full. Membership as the third axis of access; the history floor drawn in three places; Všichni never shows the floor line (D258); the first demotion in the app's history (D260); no new colour; three attachment states; Úklid úložiště chatu; 7-day koš; reader as the first writer; editable thresholds (D263).
- 2026-08-23 — v9 (Soukromé položky a Úložiště): first version with no new module; two roots per module, publish as the only irreversible action, foreign private item = ordinary 404, Úložiště with replica + backup bucket outside the breakdown (D214).
- 2026-08-20 — v8 (Elektřina). 2026-08-19 — v7 (Zahrada). 2026-08-17 — v6 (Finance). 2026-08-16 — v5 (Administrace, Web Push, PWA). 2026-08-12 — v4 (Dokumenty). 2026-07-30 — v3 (Poznámky). 2026-07-29 — v2 a11y token fixes.

## Notes
- `Home.dc.html` is the single evolving design doc (v1 → v11); `NoteView.dc.html`, `DocumentView.dc.html`, `CardTile.dc.html` are the reusable pieces. Versioned snapshots live in the repo under `design/v1 … design/v10_2`; the project file now carries **v11**, so a `design/v11` snapshot should be cut from here.
- `handoff/v10/*` in this project are the v10 copies. **v11's own bundle was read from the repo, not copied in** — `handoff/v11/V11-HANDOFF-design-addendum.md` governs the two Asistenti surfaces and the Log chip, and where it conflicts with the v1–v10.2 body, v11 wins for those.
- **Resolved in the mock, as the addendum asked:** the Log chip does **not** repeat the token name (the actor line carries it) — the chip is `⌁ Přes asistenta`, neutral register, and falls back to the prefix in the actor line on a null join.
- **Contrast flag closed:** `--subtle` was below AA in the light theme everywhere it is drawn; fixed here with measured values. The new rule (`--subtle` never on `--accent-soft`) is written into the token comments in both theme blocks.
- **Open decision for Karel:** still Path A vs Path B for the four-bucket palette (v6).
- **Open from the v10 build, not fixed:** the published `reply_to` is a per-recipient field on a frame marshalled once.
- Deliberately not folded in: the Mode B **Login** screen and the **redirect / signed-out** shell.
