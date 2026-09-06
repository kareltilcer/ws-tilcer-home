# v11 brief — MCP (`platform/mcp` · `platform/auth` · `logging` · every module)

> **Scope frozen 2026-09-06.** Twelve questions asked and answered in one sitting. This is the brief that `PRD.md` §V11, `openapi.yaml` **0.15.0**, `HANDOFF-13-mcp.md` and the `HANDOFF-design.md` **§v11** addendum are written against. Where a later document contradicts this one, the later document wins and says so.
>
> **v11 adds no module. It adds a second FRONT DOOR to all eleven.** Every version from v1 to v10.2 added a room to the house; v11 hands out a key. The work is not in any module — it is in the four things the house has never had to answer: who is calling when there is no session cookie, what a caller may see when it is a program rather than a person, how forty-odd verbs stay legible to a reader that has no screen, and what the Log says afterwards.
>
> ⚠ **The single largest piece of work in v11 is not the protocol.** MCP over Streamable HTTP is a POST handler, a JSON-RPC dispatcher and about six hundred lines. The large piece is the **fifth registered catalog** — the contract through which eleven modules publish tools without importing each other — and the **seventh host map** it creates. `internal/arch` already fails the build on a cross-module import and on a module importing a platform strand it disclaimed; it does not yet fail on a module that publishes a tool naming an entity it does not own, and it will need to.
>
> ⚠ **The second largest is not the protocol either. It is the pool.** `home` runs on **ONE** database connection. A browser makes one request at a time per view; an MCP client fans out parallel tool calls by design, and Claude does it on almost every turn. Six concurrent `home_search` calls do not run in parallel — they queue behind one connection, and the household's dashboard queues behind them. §8 is the answer and it is not optional.
>
> ⚠ **§6's leak table has seventeen rows. Treat that as a floor.** v9's grew from eighteen to twenty-three under review and the build found two more that no review had listed. The equivalent blind spot here is anything that reads household data **without an actor in hand** — and an MCP tool call is exactly that shape until the token is resolved.

---

## 1. What Karel asked for

In his words: **"Let's plan v11 of the home app, the whole v11 will be MCP server for the whole app."**

One sentence. Everything below is the resolution of what it leaves open.

---

## 2. The questions, and the answers

| # | Question | Answer |
|---|---|---|
| 1 | Where does the MCP server live? | **Inside the home binary, as the fifth registered catalog.** A new `platform/mcp`, mounted at `home.tilcer.cz/mcp`. No new Coolify app, no re-implemented auth, no second copy of any response shape. |
| 2 | Who is the client, and how does it authenticate? | **A personal access token.** Home mints it, hashes it, and never shows it twice. `Authorization: Bearer`. No OAuth, no dynamic client registration, no consent screen — and therefore **not addable in claude.ai as a remote connector**; this is a desktop-client surface. |
| 3 | Does it write? | **Read, create and update. No delete, no publish, no purge, no admin mutation, no membership change.** The irreversible things stay in the browser. |
| 4 | What may a token see of the access-controlled surfaces? | **Everything the member can see** — private (`soukromé`) roots and chat included. The token *is* them. |
| 5 | How is the tool surface shaped? | **Core + per-module toolsets, 39 tools.** Seven cross-cutting, one to six per module, each a real verb with a real schema. Which modules a token exposes is chosen at mint time. |
| 6 | Which MCP surfaces ship? | **Tools + resources + prompts.** All three. Resource *subscriptions* stay out. |
| 7 | What does the Log show for an agent's write? | **You, with the token named beside it.** ⚠ **The implementation of this answer changed under the code — see §4.3.** The intent stands; `actor_type` does not gain a fourth value. |
| 8 | What language does the contract speak? | **English contract, Czech data.** Tool names, parameters and descriptions in English; every household value stays exactly as stored. |
| 9 | May an agent post to chat? | **No. Chat is read-only over MCP.** A chat write is not a row in your data — it is a message another person receives believing you typed it, and there is no undo that unsends a push notification. |
| 10 | Who mints tokens, and where? | **Every member, in Nastavení — plus an Administrace tab** listing every member's tokens with a revoke button, and never a secret. |
| 11 | How long does a token live? | **Chosen at mint: 30 / 90 / 365 days, or never.** |
| 12 | What comes out of this session? | **Brief + PRD §V11 + the openapi delta, in one pass.** |

⚠ **Answers 3 and 4 pull in opposite directions and both were chosen deliberately.** The surface is *narrow on verbs* and *wide on reads*: an agent cannot destroy anything, and it can read the private notes. That is a coherent posture — it is the posture of a very good assistant with no authority — but it means **the whole risk of v11 is a read risk**, and the leak table, not the write list, is where the review effort belongs.

---

## 3. The model

### 3.1 The fifth catalog, and why it is not `registry.Module`

Home has four registered catalogs: **widgets** (in `platform/registry`), **metrics**, **lists**, **storage**. Three of the four share one shape, and v11 copies it exactly:

```go
// platform/lists
type Source interface { ListProvider() Provider }
// platform/storage
type Source interface { StorageTables() []string }
```

An **optional** interface, type-asserted by the host at composition, assembled into a `*Registry`, and **deliberately not part of `registry.Module`** — D56's rule, restated at every catalog since: *adding a capability must not change the contract every module implements.* v11 obeys it:

```go
// platform/mcp
type Source interface { MCPProvider() Provider }

type Provider interface {
    // Tools this module publishes, with their JSON Schemas.
    Tools() []Tool
    // Call executes one of them for one caller at one moment. The ctx already
    // carries the resolved actor; the provider does its own role gate exactly
    // as its HTTP handler does.
    Call(ctx context.Context, name string, args json.RawMessage) (Result, error)
    // Search contributes this module's rows to the one cross-module search.
    // Nil for a module with nothing to find.
    Search(ctx context.Context, q Query) ([]Hit, error)
    // Resources this module addresses by URI, and the reader for them.
    Resources() []ResourceTemplate
    Read(ctx context.Context, uri string) (Content, error)
}
```

**This is the single most important structural claim in v11, and it is what keeps `internal/arch` green.** The MCP host is the fifth thing in the codebase that needs data from every module and may import none of them. It reaches them the way the dashboard reaches widgets and the scheduler reaches metrics: through a contract, never through a table.

⚠ **`Search` is the part that has no precedent.** Metrics return an `int`; lists return `[]string`; widgets return `any` shaped for one host. Search returns rows from five different FTS5 indexes that must be *ranked against each other*. There is no cross-module ranking today because nothing has ever needed one. §5 is the answer.

### 3.2 The token is not a session, and the difference matters twice

`sessions` and `mcp_tokens` look alike — a hashed secret, a user id, cached roles, an expiry, a revocation column — and the resemblance is a trap in two places.

**A session slides; a token does not.** `SessionStore` extends `expires_at` on use (bounded by `touchGranularity`, one hour). A token must **not** slide: a 90-day token that renews itself on every call is a token that never expires, which is the option Karel did not pick. `expires_at` is written once, at mint.

**A session re-mints; a token must too, or it outlives the account.** `RefreshIdentity` is home's only question to auth after login — every 15 minutes it re-mints roles, name and email, and **fails closed**: a closed account drops the session. A bearer token that skips this is a key to a house whose lock was changed. **The MCP path runs the same `refreshIdentity`, against the same threshold, with the same fail-closed decision** — and on failure it does not drop a cookie, it revokes the token row and returns a JSON-RPC error the client can only fix by minting a new one. This is leak row 6 and it is the one most likely to be skipped, because it looks like an optimisation to leave out.

⚠ **`last_used_at` is a write on every call, on a one-connection pool.** It gets the same `touchGranularity` treatment sessions get — at most one write per token per hour. Without it, every read tool becomes a writer, and a read-only conversation with forty tool calls does forty writes to the same row.

### 3.3 Attribution — the answer stands, the implementation changed

The answer chosen was *"you, with the token named beside it: a fourth `reqctx.Actor.Type` — `agent`."* **The intent is right and is built. The fourth `actor_type` is not, and here is why.**

```sql
actor_type TEXT NOT NULL CHECK (actor_type IN ('user', 'system', 'service')),
```

`reqctx.Actor.Type` is written straight into that column by `audit.Sink` (`sink.go:105`). **SQLite cannot ALTER a CHECK**, so a fourth value means rebuilding `audit_events` — and `audit_events` is:

- the table carrying **every mutation the household has ever made**, since v1 — by construction the one whose rebuild costs the most and can afford it least;
- the parent of `audit_changes`, which `REFERENCES audit_events (id) **ON DELETE CASCADE**`.

That is precisely the `04002` hazard from `#46`, at a much worse scale: with `foreign_keys=ON`, dropping the parent performs an implicit DELETE first, fires the cascade, and takes the household's entire change history with it **while reporting success**. `#46` needed 229 lines and a rename-never-drop dance to widen one enum on a small table. Doing it to `audit_events` — on a droplet, over Litestream, on one connection — to record a fact that has a cheaper home, is not a trade worth making.

**What v11 builds instead, which satisfies the answer exactly:**

- `reqctx.Actor.Type` stays `"user"`. The actor **is** the member: `UserID` is theirs, `Roles` are theirs, and **every ownership check, membership check and `created_by` stamp in eleven modules keeps working with no change at all** — which was the reason a separate identity per token was rejected in the first place.
- `Actor.Label` becomes `"Karel · Claude (notebook)"` — the member's display name, then the token's name. This is already how the Log renders an actor (`COALESCE(actor_user_id, actor_label, actor_type)`, `query.go:503`).
- Two **new nullable columns** on `audit_events`, added by `ALTER TABLE ... ADD COLUMN` — cheap, no rebuild, no cascade, no risk:

```sql
ALTER TABLE audit_events ADD COLUMN via          TEXT;  -- 'mcp' or NULL
ALTER TABLE audit_events ADD COLUMN via_token_id TEXT;  -- mcp_tokens.id or NULL
CREATE INDEX idx_events_via ON audit_events (via, ts DESC) WHERE via IS NOT NULL;
```

- The Log browser gains one filter — *Přes asistenta* — and one chip on the row. A partial index means the filter is a seek, not a scan, and costs nothing on the 99% of rows where `via IS NULL`.

⚠ **This is a deviation from the answer as given, and it is flagged for confirmation rather than assumed.** If the fourth `actor_type` is wanted for its own sake, it is buildable — as its own release, with `#46`'s rename-never-drop procedure, a restored-copy migration test, and a Litestream snapshot taken first. It is not v11's.

### 3.4 What "read + safe writes" means, concretely

**Absent from the tool list entirely** — not gated, not confirmed, *absent*, so the model cannot propose them:

| Not exposed | Because |
|---|---|
| every `DELETE` in all eleven modules | soft-delete is a household courtesy, not an undo the agent understands |
| `notes`/`documents` **publish** | one-way, no unpublish (D182), and it moves an item from a private root to a shared one |
| `notes`/`documents` **folder delete** | cascades a whole subtree in one transaction — and the two confirmations do not even count the same thing (see [[home-refactor-waves]]) |
| `finance` month delete | finance has **no soft-delete at all** (D87) |
| chat: create / edit / react / members / move / cleanup | §4.1 |
| admin: broadcast, rules, schedules, thresholds | household-wide side effects with a push at the end of them |
| `garden` season close / reopen | closing a season **skips every still-open task** (`closeOutTasks` → `TaskSkipped`) and turns the year into rotation history that C3 and C8 then read; reopening it *"přepisuje se tím historie střídání plodin"* and is the module's **only admin-gated action** |
| `dashboard` layout | it is the member's screen, not their data |
| document and image **upload** | bytes over JSON-RPC, against a 50 MB cap, for no use case anyone asked for |

**Present, and safe:** create and update of a todo card, a checklist item, an event, a note, a folder *rename*, a finance month, a garden task / planting / harvest, an electricity reading / advance / payment; pin and unpin; complete and reopen; move a card between columns. Every one of them goes through the module's existing **service layer**, so it audits in the same transaction, publishes to the websocket with the same `origin`, and is gated twice — `httpx.RequireWrite` has no equivalent here, so `reqctx.CanWrite` is the gate, which is exactly what it was built for (*"a service reached from a job or another module is still gated once"*).

⚠ **A reader's token is a reader.** Roles come from the token's owner, re-minted. There is no way to mint a token with more than you have.

---

## 4. Three things v11 must refuse, and the reasons are not symmetrical

### 4.1 Chat writes — refused because the blast radius is another person

Chat is readable and not writable, and the asymmetry is deliberate. Every other refusal in §3.4 protects **data**. This one protects **the other members**: a message is delivered to a phone, with a push, under your name, and there is no operation that unsends it.

⚠ **It is also the one refusal that could not have been implemented cleanly anyway.** v10 decided that **chat messages are not audited** (D231/D256, guarded by `TestChatMessagesAreNotAudited`) — so a message posted through MCP would be the only write in the entire system with **nowhere to put the `via` marker**. Honouring "you, with the token named beside it" for chat would have meant reversing D231. Two independent reasons, one answer.

### 4.2 Reading chat, however, IS audited — and this is new

Home audits mutations, never reads. v9 made one exception (`admin.private_items.view`), and v11 makes the second: **`chat.read` and `notes.private.read` write an audit event when the reader is a token.**

The rule is precise: **the event records that a token read a conversation, never what was in it.** `entity_id` is the conversation, `summary` names it, `meta` carries the token id and the message count. No body, no snippet — the Log is not a second copy of the chat, which is the whole reason D231 exists.

Without this, the honest answer to *"what has the assistant seen?"* is *"there is no way to know"* — and for a surface that was explicitly opened onto private notes and message bodies, that is not an acceptable answer. It costs two audit actions and two Czech labels.

### 4.3 `home_activity` must redact what the Log page redacts

`home_activity` reads `audit_events` directly. **The Log page does not** — it renders through `audit.Redact` / `RedactRendered`, so a private item's event reaches a non-owner as *"a private note happened"* with the id blanked (D201, four phrases by `entity_type`).

⚠ **An MCP tool that reads the raw table bypasses every one of those rules, for every member's private items, in one call.** This is the v9 trigger-listener gap — *the code is correct and nothing tests it* — repeating in a brand-new consumer that nobody has written a test for yet. `home_activity` calls the same redaction the page calls, and **the acceptance criteria assert it against a second member's private item**, not against a fixture the caller owns.

---

## 5. The cross-module search, which has no precedent

`home_search` is the most useful tool on the list and the only one that needs machinery that does not exist.

**Five external-content FTS5 indexes:** `notes`, `documents`, `logging`, `garden_plants_fts`, `chat_messages_fts` — twenty-five shadow rows, and **five different notions of a match**. Plus the things with no FTS at all: a todo card title, an event title, a garden bed name, an electricity period.

The design, in four rules:

1. **Fan out, never join.** Each module answers `Search(ctx, Query) ([]Hit, error)` over its own index, viewer-scoped, with its own `LIMIT`. The host merges. There is no cross-module SQL and there cannot be — `internal/arch` forbids it and the FTS tables are external-content anyway.
2. **Rank by a shape, not by a score.** bm25 from five separate indexes is not comparable and pretending it is produces confident nonsense. The host orders by **(exact title match, then recency, then module order)** and says so in the tool description, so the model knows it is reading a merge and not a relevance ranking. A module may return its own rows pre-ordered; it may not claim a global score.
3. **Every module gets the same budget.** `limit` is per module, defaulted low (10), and the response says how many each module had. A search that returns 40 chat messages and no notes because chat is chattier is a worse answer than a search that returns 8 of each.
4. ⚠ **The viewer scope is the whole game.** v9's build found two surfaces the 23-row leak table never listed — the preview worker and the image GC — both because *a background job has no actor and a viewer-scoped read would have returned nothing, so someone reached for `…AnyScope`*. An MCP tool call has an actor **only after the token resolves**, and the resolve happens in the host, not the module. **A provider that receives a ctx with no actor must return an error, never `AnyScope`, never empty.** This gets a test with a deliberately actor-less context, per module.

---

## 6. Where v11 can leak — the list, which is not "the whole list"

| # | Surface | Closed by |
|---|---|---|
| 1 | `/mcp` accepts the session cookie ⇒ any website can drive the whole tool surface cross-origin, with no CSRF check, because `/mcp` is outside the `/api` group that has one | **Bearer only.** The cookie is not read on this path at all; a request with a cookie and no bearer is 401, not "authenticated as the cookie's owner" |
| 2 | `/mcp` unmatched falls through to the SPA — `httpx.NewRouter`'s `NotFound` sends every non-`/api/`, non-`/ws` path to `index.html` — so a mistyped MCP path returns **HTML with a 200** and the client reports a protocol error against a working server | `/mcp` joins `/ws` in that exclusion. One line, and it is the first thing that will be got wrong |
| 3 | DNS rebinding: a local page resolving `home.tilcer.cz` to itself and POSTing JSON-RPC | `Origin` validated against `HOME_ALLOWED_ORIGINS`, the list CSRF already uses; a request with **no** `Origin` is allowed (a CLI has none) but only with a valid bearer |
| 4 | A revoked token keeps working while a cached lookup lives | No positive cache on revocation state. The token row is read per call (one indexed lookup on a hashed unique key, on the same connection every other query uses) |
| 5 | A token outlives its owner's account being closed in auth | The MCP path runs `refreshIdentity` on the same threshold and **fails closed**, revoking the token row (§3.2) |
| 6 | A token grants more than its owner has | Roles are the owner's, re-minted, never stored on the token; the `modules` allowlist can only narrow |
| 7 | `home_activity` returns raw `audit_events` including other members' private-item summaries and ids | `audit.Redact` / `RedactRendered` on the MCP path, asserted against a **second member's** private item (§4.3) |
| 8 | `home_search` returns another member's private note because a provider reached for `…AnyScope` when the ctx had no actor | Actor-less ctx is an error in every provider; per-module test (§5.4) |
| 9 | 404-never-403 breaks in the error mapping — a tool that answers *"forbidden"* leaks exactly what *"not found"* hides | One mapper, `httpx.APIError` → JSON-RPC, and it maps 403 on an ownership/membership surface to the same result as 404. Tested per axis |
| 10 | A resource URI is treated as a capability: member B reads `home://notes/soukrome/…` pasted from member A | Every `Read` re-checks access from scratch; the URI carries no grant |
| 11 | `resources/list` enumerates what the caller may not read — the *existence* leak v9 spent a version closing | The listing is viewer-scoped, and a private root appears only for its owner |
| 12 | A chat read leaves no trace, so "what has the assistant seen" is unanswerable | `chat.read` audit event — the conversation, never the body (§4.2) |
| 13 | Tool *descriptions* leak household structure to a client that has not authenticated | `tools/list` requires the bearer, like every other method; an unauthenticated `initialize` returns capabilities and nothing else |
| 14 | The token is logged — a bearer in a request log, an error message, or a crash report to status.tilcer.cz | The token never leaves the auth path; `config.Redacted()` gains it; **the `statusreport` slog handler must not carry `Authorization`** — it lifts attrs into the crash report and the crash board **groups on the first frame** |
| 15 | A tool result carries an image or PDF large enough to be a denial-of-service against the model's context | Per-result byte cap (`HOME_MCP_MAX_RESULT_KB`, 256), and resource reads are ranged |
| 16 | Push: an MCP-created event or task fires a notification to every member, and nobody knows why | Nothing is suppressed — a created task *should* notify — but the notification's audit trail carries `via='mcp'`, so the Log answers it |
| 17 | The one-connection pool: a leaked `*sql.Rows` in a new provider deadlocks the **next query in the whole process**, browser included | `appdb.Collect` closes rows; every provider uses it; `internal/arch` cannot check this, so it is a review item and a load test (§8) |

⚠ Rows **7, 8 and 11** are the same failure wearing three hats: *a read that forgot who was asking*. They are the ones to review together.

---

## 7. The tool surface — 39 tools

**Naming:** `home_<module>_<verb>`, English, snake_case. Core tools drop the module. Every description is one English sentence that says what it returns and what it costs; every Czech value inside a result is verbatim as stored.

**Annotations:** every tool carries `readOnlyHint` truthfully. Nothing carries `destructiveHint: true`, because nothing destructive is exposed — which is a property worth asserting in a test rather than trusting.

### Core (7)

| Tool | Read | What |
|---|---|---|
| `home_whoami` | ✅ | Identity, roles, which modules this token exposes, `HOME_TIMEZONE`, today's date in Prague. The first call of every session |
| `home_search` | ✅ | Cross-module full-text (§5). Returns `kind`, `id`, `title`, `snippet`, `module`, `resource_uri` |
| `home_get` | ✅ | One entity by `kind` + `id`. The universal expansion of a search hit |
| `home_today` | ✅ | What the dashboard answers: reminders due and overdue, tasks due, garden tasks — composed from the **metrics and lists catalogs**, never from module tables. ⚠ **No unread count**: D252 kept chat out of both catalogs, so an unread figure here would mean the one tool that touches no module table reaching into one. Unread lives on `home_chat_conversations`, where it costs nothing extra |
| `home_metrics` | ✅ | No args ⇒ the 19 descriptors; `keys` ⇒ their values for this caller, `asOf` in Prague |
| `home_lists` | ✅ | The same for the 16 lists |
| `home_activity` | ✅ | The audit spine as a digest: `since`, `module`, `action`, `actor`, `entity`. **Redacted** (§4.3). Admin-only filters stay admin-only |

### Per module (32)

| Module | Tools |
|---|---|
| **todo** (5) | `boards` · `card_create` · `card_update` · `card_move` · `checklist` |
| **events** (4) | `upcoming` (occurrence expansion) · `create` · `update` · `complete` |
| **notes** (4) | `tree` · `create` · `update` · `pin` |
| **documents** (3) | `tree` · `update` (rename / describe) · `pin` |
| **finance** (3) | `months` · `month_create` · `month_update` |
| **garden** (6) | `tasks` · `task_create` · `task_complete` · `plan` (season, beds, occupancy, checks C1–C11) · `planting_create` · `harvest_log` |
| **electricity** (4) | `readings` · `reading_add` · `advance_add` · `summary` (nedoplatek / přeplatek) |
| **chat** (2, read-only) | `conversations` (with the unread counts) · `messages` |
| **admin** (1, read-only, admin role) | `status` — storage snapshot, thresholds, rule counts, private-items inventory |
| **dashboard, logging** (0 tools) | Covered by `home_today` and `home_activity`. A module contributing nothing to a catalog is normal — **five of the eleven publish no widget at all** (`admin`, `logging`, `chat`, `electricity`, `dashboard`). ⚠ `logging` still implements **`Search`** over `audit_events_fts` (§5); a provider with no tools is not an empty provider. `dashboard` implements neither |

⚠ **Garden gets six of thirty-two because it has 34 of the 144 paths**, and it is still the module most likely to want a seventh. The ceiling is **45**; every tool past it must retire one.

⚠ **Every write tool validates its own inputs, and the reason is electricity.** A negative `invoiced_vt_dkwh` / `invoiced_nt_dkwh` surfaces as a **500 instead of a 422** — a known defect, recorded and unfixed. Those fields are on **`electricity_periods`**, and v11 exposes **no periods tool**, so the loop is avoided by absence rather than by care. The rule it teaches applies to all sixteen write tools anyway: **an agent retries a 500 and gives up on a 422**, so a household typo returning the wrong code turns a mistake into a loop. No write tool may reach a service with an input it could have refused itself. ⚠ **This fixes nothing in the REST surface** — the periods 500 and electricity's non-clamping 100/500 `limit` are both still v8's to repay.

### Resources

`home://notes/{path}` · `home://documents/{path}` · `home://chat/{conversation}/attachments/{id}`

⚠ **`{path}` is the SPA's own splat and there is no scope segment**: shared has no prefix (`home://notes/recepty/gulas`), private is prefixed `soukrome` (`home://notes/soukrome/denik`), exactly as `lib/scope.ts` reads a URL. There is no `sdilene`, and inventing one would be the only place in the app that names the shared root. It parses unambiguously because `soukrome` is a reserved slug at both shared roots (D185).

Listed viewer-scoped, read with a fresh access check, ranged, capped. A file that **is** text (`text/plain`, `text/markdown`, `text/csv`) is served as text; a PDF or image is served as content the model can look at. ⚠ **Everything else is served as its bytes and nothing extracts text from it.** The `home-gotenberg` sidecar converts Office files to **PDF**, never to text (`documents/preview.go`), so a .docx reaches the model as a rendered PDF or not at all. No new conversion path, no `platform/preview` refactor — D227 stands.

### Prompts (5)

`/co-mě-čeká` · `/nákup` · `/večeře-z-toho-co-máme` · `/měsíční-uzávěrka` · `/zahrada-týden`

Czech names, Czech text, English tool calls underneath. Prompts are the cheapest thing in v11 and the place household knowledge actually lives — `/večeře-z-toho-co-máme` is `garden.storage` + `garden.harvests` + a sentence about what the family eats, and it is worth more than any three tools.

---

## 8. The pool, and the concurrency nobody has needed until now

**One connection.** `appdb` caps it, deliberately, and the whole backend is written around it — `appdb.Collect` closes rows precisely because *"a leaked `*sql.Rows` deadlocks the next query"*.

An MCP client breaks the assumption that produced that cap. Claude issues parallel tool calls routinely; six at once is ordinary. Six tool calls are six-plus queries with no browser pacing them, and **the household's dashboard is behind them in the same queue**.

v11's answer, in three parts:

1. **A semaphore of 2 per token**, in the MCP handler. The third concurrent call waits. This is not a rate limit — it is back-pressure, and it keeps a chatty agent from being a self-inflicted outage.
2. **A hard per-call deadline** — `HOME_MCP_CALL_TIMEOUT_SEC`, default 20 — set on the ctx, so a pathological search cannot hold the connection while the client has already given up.
3. **A rate limit per token** — `HOME_MCP_RATE_PER_MIN`, default 120. ⚠ **`auth`'s `rateLimiter` cannot be reused as it stands**, and the brief says so rather than discovering it in the build: it is a **fixed-window** counter, it is **unexported**, and it counts **failures only** (`allowed` deliberately does not record an attempt). A call-rate limit counts successes. Either promote it to `platform/` with a counting mode, or write a small one in `platform/mcp` — §13 item 7.

⚠ **This wants a load test, not a unit test.** The acceptance criterion is *"twenty concurrent tool calls, and a browser request served throughout"* — the kind of thing v10.2 learned the hard way that a green suite does not tell you. ⚠ And `go test -race` is unavailable on Karel's windows/arm64 host, so the concurrency review is by reading, on a surface where that is least comfortable.

---

## 9. Deployment — three things that are not code

1. ⚠ **A new Coolify path route.** `home.tilcer.cz` is **two apps sharing one origin** — `home-backend` on `/api` and `/ws`, `home-frontend` on the catch-all (`home-gotenberg` is a third app but internal-only, no public domain). `/mcp` is therefore the **backend's third path route**, to **7999**, and **Strip Prefix stays off** as it has since v1. Without the route, `/mcp` reaches the Nginx SPA and returns `index.html`.
2. ⚠ **`/mcp` in the router's SPA exclusion** (leak row 2) — the code half of the same problem, and it fails in the identical way, which is what makes it hard to diagnose.
3. **Six new env vars, all defaulted, none secret:** `HOME_MCP_ENABLED` (**true**) · `HOME_MCP_RATE_PER_MIN` (120) · `HOME_MCP_CALL_TIMEOUT_SEC` (20) · `HOME_MCP_MAX_RESULT_KB` (256) · `HOME_MCP_MAX_TOKENS_PER_USER` (10) · `HOME_MCP_SEARCH_LIMIT` (10).

   ⚠ `HOME_MCP_ENABLED` defaults **true** on purpose: the real gate is that a human must mint a token in the UI, and an env flag defaulting to off is a second thing to forget on a surface whose failure mode is silence. It says its state **once at boot**, the way `statusreport` does, and for the same reason — a disabled server and a misconfigured client look identical from the client.

---

## 10. Czech UI vocabulary (fixed)

| English | Czech |
|---|---|
| MCP access / assistants | **Asistenti** |
| access token | **token** |
| mint a token | **Vytvořit token** |
| revoke | **Odvolat** |
| last used | **naposledy použito** · never used ⇒ **nepoužito** |
| expires | **platnost do** · never ⇒ **bez omezení** |
| scope / modules | **Rozsah** |
| via an assistant (Log filter + chip) | **Přes asistenta** |
| Nastavení section | **Asistenti (MCP)** |
| Administrace group → tab | **Asistenti** → **Tokeny** |
| the one-time reveal | **Token se zobrazí jen jednou. Zkopírujte si ho teď.** |

---

## 11. Worked cases the implementation must reproduce

1. **"Přidej mléko na nákupní seznam."** `home_search("nákup")` → a board hit → `home_todo_card_create`. The card appears in the browser **live** (the module's own service publishes to the ws hub as always), and the Log row reads *Karel · Claude (notebook)* with the **Přes asistenta** chip.
2. **"Co mám dnes udělat?"** One `home_today`. No module table is touched by the host; the answer is composed from 19 metrics and 16 lists, personal ones resolved per recipient.
3. **"Najdi tu smlouvu o pojištění."** `home_search` → a `documents` hit → `resources/read` on `home://documents/…` → the PDF as content. Three round trips, one access check each.
4. **"Shrň, co se dnes psalo v chatu."** `home_chat_conversations` → `home_chat_messages`. **Two `chat.read` audit events, no bodies.** No write is offered anywhere in the flow.
5. **"Smaž ten úkol."** There is no delete tool. The model says so and offers to mark it done. ⚠ **The acceptance criterion is that it cannot be talked into one** — not that it refuses politely.
6. **A second member's private note** appears in neither `home_search`, nor `resources/list`, nor `home_activity` — three separate assertions, one bug.
7. **The owner's account is closed in auth.** The next tool call re-mints, fails closed, revokes the token row, and returns an error. The *following* call is a clean 401 against a revoked token, not a second re-mint.
8. **The token is revoked from the Administrace tab while a conversation is mid-turn.** The in-flight call finishes; the next one 401s. (Contrast with a session, where v10 also had to tear down the websocket — there is no socket here, which is the one place a token is *simpler* than a session.)
9. **Twenty concurrent tool calls**, and the browser stays responsive (§8).

---

## 12. Out of scope, explicitly

- **OAuth 2.1, dynamic client registration, and therefore claude.ai / mobile.** A PAT cannot be a claude.ai remote connector. If that is wanted it is **v12**, and it is most of a version by itself.
- **Server-initiated notifications and resource subscriptions.** `GET /mcp` returns 405. The hub exists and could feed them; nothing asked for it, and it would be the first thing in home that pushes to a non-browser.
- **MCP sampling and elicitation.** Home never asks the client to run a model, and never asks the user a question mid-call.
- **stdio transport / a shipped bridge binary.** `mcp-remote` or the client's own HTTP support.
- **Uploads of any kind.**
- **Any chat write.**
- **Anything destructive** (§3.4).
- **The two-threshold merge.** Home still carries v9's `HOME_STORAGE_WARN_TOTAL_MB` **and** v10's DB-backed `storage_thresholds`. It is still owed, and it is still not this version's.
- **Item 43** (the 81 `.tsx` files hardcoding Czech). v11 adds one page and one tab; they use `cs.ts` and do not repay the debt.

---

## 13. Settled on 2026-09-06, after the PRD — and what is still owed

**Five follow-up questions were put to Karel once the PRD was written. All five are now answered, and the answers are recorded as decisions rather than left in this list.**

- ✅ **§3.3 / D290 CONFIRMED** — `via` + `via_token_id` + the label. `actor_type` gains no fourth value; the rebuild is not deferred to a later release, it is **not wanted**. Anyone who finds `actor_type` lacking an `agent` value has found a decision, not an oversight.
- ✅ **The SDK question is SETTLED: hand-rolled** (D278, no longer provisional). ~600 lines in `platform/mcp`, full control of the error mapping leak row 9 depends on, and Home stays at 13 pinned dependencies. The cost accepted with it: **protocol-version drift is ours to track**.
- ✅ **D297 CONFIRMED, both** — `chat.read` **and** `notes.private.read`. The container, never the body.
- ✅ **The rate limiter: a new one in `platform/mcp`** (D306). `auth.rateLimiter` is not promoted.
- ✅ **The build is THREE PRs** (D323), and §15 is the plan.

**Still owed, and none of it blocks the handoffs:**

1. **Settle the host-map count in the same pass as D319.** The PRD's own arithmetic (D213) says **six**, so the tool catalog is the **seventh**; v10.2's `DESKTOP_FOOTER_ROUTES` split is an eighth candidate the PRD never numbered. *"The four were never four"* is now a recurring sentence and deserves one line that ends it.
2. **Name the new host map's guard.** `arch/mcp_completeness_test.go`: unique tool names, every tool's module registered, every declared resource template readable, **no tool carrying `destructiveHint`**, and a **committed golden manifest** so a tool change is visible in review — the treatment `platform/storage`'s table declarations got, for the same reason. ⚠ **Settle the count while you are there:** the PRD's own arithmetic (D213) says **six** host maps, so the tool catalog is the **seventh**; v10.2's `DESKTOP_FOOTER_ROUTES` split is a candidate for a seventh that the PRD never numbered. Pick one and write it down, because "the four were never four" is now a recurring sentence.
4. **Where the manifest lives.** The MCP surface is not OpenAPI and `openapi.yaml` cannot describe it. Proposal: `backend/mcp-manifest.json`, generated by the golden test, committed, and **`HANDOFF-13-mcp.md`** as the prose.
5. **Ask whether `chat.read` auditing is wanted** (§4.2). It is the second read ever audited in this system, and the first that fires on an ordinary flow.
3. **The design bundle** — now issued as `HANDOFF-design.md` **§v11**. ⚠ Two things in it are new to this app and want Karel's eye before they are drawn: the **one-time secret reveal**, and the **connect snippet** (§15.4).

---

## 15. The build plan — three PRs (D323)

**Do not build this in the order the PRD reads.** Each PR is green on its own and independently deployable, and the risky half is reviewed alone.

| PR | What | Why it is this shape |
|---|---|---|
| **1** | `platform/mcp` (contract, dispatcher, host) · `02005_mcp_tokens` + `01003_audit_via` · the token store and its four REST routes · bearer auth, Origin check, SPA exclusion, semaphore, deadline, rate limiter · attribution end to end · **three providers only — `todo`, `events`, `notes`** | It is the whole risk in one review. Every leak-table test lands here, the credential is exercised against a real client before nine more providers exist, and three modules are enough to prove the contract without being enough to hide a flaw in it. ⚠ **`notes` is in PR 1 on purpose** — it is the module with private roots, so rows 7, 8, 10 and 11 are testable from the first PR rather than the last. |
| **2** | The remaining six providers (`documents`, `finance`, `garden`, `electricity`, `chat`, `admin`) · `logging`'s search-only provider · cross-module search and its merge · resources · `chat.read` / `notes.private.read` auditing | Mechanical once PR 1 lands: each provider is the same shape. The one genuinely new thing is the search merge, and it is easier to get right against six real indexes than against three. |
| **3** | The two frontend surfaces · prompts · `mcp-manifest.json` + `arch/mcp_completeness_test.go` · the load test · README, REGISTRY, CHANGELOG, the `HANDOFF-design` §v11 sign-off | The manifest test is deliberately **last**: it is a golden file, and generating it before the tool list has stopped moving means regenerating it three times and reviewing it none. |

⚠ **The contract bumps in PR 1** (0.14.0 → 0.15.0), not in PR 3 — the four token routes ship there, and *"bump `openapi.yaml` in the PR that earns it"* has been on the checklist since v8's failure.

⚠ **PR 1 is deployable and useless, and that is the point.** With three providers and no frontend, the only way to mint a token is a hand-written SQL insert on the droplet — do exactly that, connect a client, and find out what is wrong while the diff is still small enough to read.

---

## 14. Before any of it — the doc debt this version inherits

⚠ Three passes have landed since the 2026-09-02 Nextcloud sync and **two of them have no entry in any document**: `#46`'s `reminder_lead: "0d"` — which moved the contract to **0.14.0 with no CHANGELOG entry**, a gap the v10.2 entry already **names out loud** without closing — and `#47`/`#48`'s status.tilcer.cz integration, whose only written record anywhere is a README. (Verified: `statusreport` and `status.tilcer.cz` appear nowhere in the PRD or the CHANGELOG.) v11 makes the contract **0.15.0**.

**Writing 0.15.0's entry on top of a 0.14.0 that has none is how a changelog stops being readable.** The two missing entries and the Nextcloud re-sync are v11's first commit, not its last — and `handoff/v10/openapi.yaml` is still sitting at **0.12.0**, two minors behind the file it is supposed to be a copy of.
