# Home — v11: MCP (`platform/mcp` · `platform/auth` · `logging` · every module)

> **Read first:** root `CLAUDE.md`, then `PRD.md` **§V11-1…§V11-11** (decisions **D275–D323**), `openapi.yaml` **0.15.0**, the scope brief `V11-MCP-brief.md`, and the design addendum `HANDOFF-design.md` **§v11**. Build guide for v11. Owner: Karel. Issued 2026-09-06.
>
> ⚠ **This is not a module build, and it is the second one that is not.** `HANDOFF-5` through `HANDOFF-10`, and `HANDOFF-12`, were all the same shape — a new package, a new migration block, a registry entry, four host maps, done. `HANDOFF-11` (v9) opened with this same warning and it is worth reusing the phrasing: **v11 adds no module, no migration block and no nav entry.** It adds a **second front door to all eleven** — a new protocol, a new credential, a fifth registered catalog, and a change to what the Log records — none of which any module owns.
>
> ⚠ **It ships as THREE pull requests (D323).** PR 1 is the whole risk in one review. See the build order below and do not reorder it.
>
> **The new rule, stated once:**
>
> > **A token is a second credential for the same identity.** It carries the member's id and the member's roles, re-minted on the same threshold as a session, and it is refused everything the member would be refused. What makes it different is not what it may see — it is that **nobody is watching the screen**, so every verb that cannot be taken back is absent rather than confirmed.
>
> PRD **§V11-4 FR-M11 — the leak table** — enumerates **seventeen** surfaces where that rule can be read around. §12 below is one test per row, written from the attacker's side. ⚠ v9's equivalent table went from eighteen rows to twenty-three under review and the build still found **two more that no review had listed** — the preview worker and the image GC, background jobs with no actor. **v11's equivalent blind spot is any code path that reads household data before the token has resolved**, because until it does there is no actor in the ctx. Assume the table is short.

## The model in one paragraph

`mcp_tokens` holds one row per credential: a SHA-256 of a secret shown once, its owner, a name, an 8-character prefix, an optional module allowlist, an expiry written at mint and never touched again, and a revocation column. `POST /mcp` speaks JSON-RPC 2.0 over Streamable HTTP, **outside the `/api` group**, bearer-only — it does not read the session cookie at all. The bearer resolves to a `reqctx.Actor` carrying the member's real id and roles, `Type: "user"`, and a label naming the token; every audit event written on that path additionally carries `via='mcp'` and `via_token_id`. Nine of the eleven modules publish tools through **`mcp.Source`**, an optional interface type-asserted at composition exactly as `metrics.Source` and `lists.Source` are (`logging` implements it for search alone; `dashboard` not at all) — the host reaches module data through the contract and never through a table. Thirty-nine tools: seven cross-cutting, thirty-two per module, none of them destructive, because delete, publish, purge and upload are **absent from the list rather than gated**. Three resource templates, five Czech prompts. A semaphore of two per token keeps a chatty agent from queueing the household's dashboard behind it on the **one** database connection. Two reads — a chat thread and a private note — write an audit event naming the container and never the content, which is the only way to answer *"what has the assistant seen?"*.

---

## Build order — three PRs (D323)

**Do not build this in the order the PRD reads.** Each PR is green on its own and independently deployable.

### PR 1 — the front door, and three modules to prove it

`platform/mcp` (contract, host, dispatcher) · `02005_mcp_tokens.sql` + `01003_audit_via.sql` · the token store and its four REST routes · bearer auth, Origin check, the SPA exclusion, the semaphore, the deadline, the rate limiter · attribution end to end · **`todo`, `events` and `notes` providers only**.

⚠ **`notes` is here on purpose.** It is the module with private roots, so leak rows 7, 8, 10 and 11 are testable in the *first* PR rather than the last. Three modules is enough to prove the contract and not enough to hide a flaw in it.

⚠ **The contract bumps here** — 0.14.0 → **0.15.0** — because the four token routes ship here. *"Bump `openapi.yaml` in the PR that earns it"* has been on the checklist since v8's failure.

⚠ **PR 1 is deployable and useless, and that is the point.** With three providers and no frontend, the only way to mint a token is a hand-written `INSERT` on the droplet. **Do exactly that**, point a real client at it, and find out what is wrong while the diff is still small enough to read.

### PR 2 — the rest of the surface

The remaining six providers (`documents`, `finance`, `garden`, `electricity`, `chat`, `admin`) · `logging`'s **search-only** provider · the cross-module search and its merge · resources · the two read-audit actions.

Mechanical once PR 1 lands: each provider is the same shape. The one genuinely new thing is the search merge (§8), and it is easier to get right against six real indexes than against three.

### PR 3 — the surfaces people touch

The two frontend screens · prompts · `backend/mcp-manifest.json` + `internal/arch/mcp_completeness_test.go` · the load test · README, `REGISTRY.md`, CHANGELOG, and the `HANDOFF-design` §v11 sign-off.

⚠ **The manifest test is last deliberately.** It is a golden file; generating it before the tool list has stopped moving means regenerating it three times and reviewing it none.

### ⚠ Before PR 1: the doc debt

`#46` and `#47`/`#48` still have **no PRD or CHANGELOG entry**, so 0.14.0 has no changelog entry at all and 0.15.0 would stack on the gap. Write those two entries and re-sync the Nextcloud masters **first**. It is one sitting and it is the difference between a changelog and a list.

---

## 1. `platform/mcp` — the contract, and why it is shaped like `lists`

### 1.1 The optional Source

```go
// platform/mcp — the FIFTH registered catalog (D276).
//
// mcp lives in platform/ and is imported BY modules; it must never import a
// module. The host reaches feature data through Provider, never through a
// module's tables — the same rule registry.Catalog, metrics.Registry and
// lists.Registry all follow.

// Source is the optional interface a feature module implements to publish an
// MCP provider. It is deliberately NOT part of registry.Module: adding a
// capability must not change the contract every module implements (D56).
// Collect type-asserts for it, exactly as metrics and lists do.
type Source interface{ MCPProvider() Provider }
```

⚠ **Copy `platform/lists`'s assembly verbatim, and copy the real signatures** — they are not the obvious ones:

```go
func Collect(modules ...any) (*Registry, error)   // variadic any; type-asserts for Source
func NewRegistry() *Registry                      // NO arguments
func (r *Registry) Register(p Provider, d []Descriptor) error   // duplicate rejection lives HERE,
                                                  // delegated to platform/catalog
```

`metrics` is identical. Both registries are built at composition in `buildModules` (`cmd/home/main.go:648`, `:656`). **No package-level `Register` global**, for the reason the other three catalogs do not have one: a global makes registration order depend on import order, and the import graph is the thing `internal/arch` polices.

### 1.2 The Provider

```go
type Provider interface {
    // Module is the English code identifier, matching registry.Module.Name().
    Module() string

    // Tools this module publishes. MAY be empty — see logging.
    Tools() []Tool

    // Call executes one of them. name is the FULL tool name ("home_todo_card_create").
    // ctx carries a resolved reqctx.Actor; a ctx without one is an error (D302).
    Call(ctx context.Context, name string, args json.RawMessage) (Result, error)

    // Search contributes this module's rows to the one cross-module search.
    // Return (nil, nil) for a module with nothing to find.
    Search(ctx context.Context, q Query) ([]Hit, error)

    // Resources this module addresses by URI. MAY be empty.
    Resources() []ResourceTemplate
    // Read resolves one URI. Access is re-checked here, from scratch (D308).
    Read(ctx context.Context, uri string) (Content, error)
}
```

```go
type Tool struct {
    Name        string          // "home_todo_card_create", unique across the registry
    Title       string          // short English label
    Description string          // ONE English sentence: what it returns and what it costs
    InputSchema json.RawMessage // JSON Schema, draft 2020-12
    ReadOnly    bool            // → annotations.readOnlyHint
}

type Result struct {
    Text    string          // human-readable, Czech data verbatim
    JSON    json.RawMessage // structuredContent; nil when there is nothing structured
    IsError bool            // a tool that RAN and refused — never a protocol failure
}

type Query struct {
    Text  string // the user's terms, unparsed
    Limit int    // PER MODULE (D301), already clamped by the host
}

type Hit struct {
    Kind      string    // "notes.note", "todo.card", "garden.plant" …
    ID        string
    Title     string    // Czech, verbatim
    Snippet   string    // Czech, verbatim; may be ""
    UpdatedAt time.Time // for the merge order — NOT a score
    URI       string    // resource URI when the hit is addressable; "" otherwise
    ExactHit  bool      // the module's own judgement that the title matched exactly
}
```

⚠ **`Hit` carries no score, and the type is where that decision is enforced.** A provider that wants to rank globally has nowhere to put the number. See §8.

⚠ **`Module()` is on `Provider`, not derived from the tool-name prefix.** Deriving it would make `home_admin_status` look like it belongs to a module called `admin_status`, and the completeness test needs the real answer to check the `McpModule` enum against the registry.

### 1.3 Who implements what

| Module | `Tools()` | `Search()` | `Resources()` |
|---|---|---|---|
| todo, events, finance, electricity | ✅ | title scan | — |
| notes, documents | ✅ | FTS5 | ✅ |
| garden | ✅ | `garden_plants_fts` | — |
| chat | ✅ (2, read-only) | `chat_messages_fts` | ✅ (attachments) |
| admin | ✅ (1, read-only) | — | — |
| **logging** | **empty** | `audit_events_fts` | — |
| **dashboard** | *does not implement `Source` at all* | | |

⚠ **A provider with no tools is not an empty provider.** `logging` implements `Source` for `Search` alone. If `Tools()` returning empty makes something skip the provider entirely, search loses a fifth of its corpus silently.

---

## 2. The two migrations

### 2.1 `02005_mcp_tokens.sql` — platform block, beside `02001_sessions`

```sql
-- +goose Up
-- +goose StatementBegin
CREATE TABLE mcp_tokens (
    id            TEXT PRIMARY KEY,               -- UUIDv7
    user_id       TEXT NOT NULL,                  -- auth user id (sub)
    name          TEXT NOT NULL,
    token_hash    TEXT NOT NULL UNIQUE,           -- SHA-256 hex of the secret
    prefix        TEXT NOT NULL,                  -- first 8 chars of the secret
    modules       TEXT NOT NULL DEFAULT '[]',     -- JSON array; [] ⇒ all the owner can see
    created_at    TEXT NOT NULL,                  -- RFC3339 UTC
    expires_at    TEXT,                           -- NULL ⇒ never (D285)
    last_used_at  TEXT,
    last_used_ip  TEXT,
    revoked_at    TEXT
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_mcp_tokens_user ON mcp_tokens (user_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS mcp_tokens;
-- +goose StatementEnd
```

⚠ **`token_hash` is `UNIQUE` and that is the lookup index.** Resolution is one seek on it, per call, with no cache (D287) — which is what makes a revoke take effect on the next call rather than on the next restart.

⚠ **There is no `roles` column, and there must not be one.** `sessions` caches roles because a session is minted at login and lives for weeks; a token reads its owner's roles live. A `roles` column here would be a second, staler answer to a question that already has one, and it is exactly how a token comes to outlive a demotion.

### 2.2 `01003_audit_via.sql` — logging block

```sql
-- +goose Up
-- +goose StatementBegin
ALTER TABLE audit_events ADD COLUMN via TEXT;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE audit_events ADD COLUMN via_token_id TEXT;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_events_via ON audit_events (via, ts DESC) WHERE via IS NOT NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_events_via;
-- +goose StatementEnd
-- ⚠ The columns are NOT dropped on the way down. See below.
```

⚠⚠ **This migration must contain no `CREATE TABLE`, no `DROP TABLE` and no rename, and a test asserts it.**

The reason is D290 and it is worth restating at the code. `actor_type` is `CHECK (actor_type IN ('user','system','service'))`, SQLite cannot ALTER a CHECK, and `audit_changes` declares `REFERENCES audit_events (id) ON DELETE CASCADE`. So a rebuild of `audit_events` would — with `foreign_keys=ON` — perform an implicit DELETE on the parent, **fire that cascade, and destroy the household's entire change history while reporting success.** `#46`'s `04002` needed 229 lines and a rename-never-drop dance to avoid exactly this on a far smaller table. **v11 does not rebuild `audit_events` at all**, which is why the fourth `actor_type` was declined rather than deferred.

⚠ **The down migration drops the index and keeps the columns**, deliberately. `ALTER TABLE ... DROP COLUMN` on SQLite rewrites the table, which is the operation this migration exists to avoid; and two nullable columns nobody reads cost nothing. Say so in the file, or the next reader will "finish" it.

⚠ **`01003` is the SIXTH out-of-order goose version below the applied `11001`/`12003`** — `01002`, `06004` and `07004` shipped in v9, `02004` and `08003` in v10, and `02005` here makes seven. The shape this repo has shipped with since `01002`. Expected, not a problem, and **do not "tidy" the numbering**: renumbering an applied migration is how a restored copy stops matching `goose_db_version`.

---

## 3. The token

### 3.1 Mint

```go
// platform/auth (beside SessionStore) or platform/mcp — see §3.5.
const tokenPrefix = "hmcp_"

func newSecret() (secret, hash, prefix string, err error) {
    b := make([]byte, 32)
    if _, err = rand.Read(b); err != nil { return }          // crypto/rand
    secret = tokenPrefix + base64.RawURLEncoding.EncodeToString(b)
    sum := sha256.Sum256([]byte(secret))
    return secret, hex.EncodeToString(sum[:]), secret[:8], nil
}
```

- **The secret is returned once**, by `POST /api/mcp/tokens`, and never again. There is no route that re-reads it because only the hash is stored.
- **`prefix` is the first 8 characters of the whole string** — i.e. `hmcp_` plus three — so it is a literal substring of what is in the config file and can be matched by eye.
- ⚠ **The literal `hmcp_` is not decoration.** It makes a leaked token greppable in a paste, a config file and a log, which is the only cheap mitigation that exists for a credential living on a laptop.
- `expires_at = now + expires_in_days` where the days are one of **30 / 90 / 365 / null**, and it is **written once**. Nothing anywhere updates it.
- Audited as `mcp.token.create`, carrying the **name and prefix** in `meta`, never the secret and never the hash.

### 3.2 Resolve — the per-call path

```
Authorization: Bearer hmcp_…
   ↓ constant-time compare is NOT needed — we look up by hash, not by scan
   ↓ SELECT … FROM mcp_tokens WHERE token_hash = ?        (one seek, no cache)
   ↓ revoked_at IS NULL           else 401
   ↓ expires_at IS NULL OR > now  else 401
   ↓ refreshIdentity if past HOME_ROLE_REFRESH_MINUTES    (§3.3)
   ↓ reqctx.Actor{UserID: t.user_id, Roles: <live>, Type: "user",
                  Label: displayName + " · " + t.name}
   ↓ reqctx.RequestInfo{RequestID, IP, UserAgent, Site}   — ClientID stays ""
   ↓ mcpctx.WithToken(ctx, t.id)                          — for the audit hook, §6
   ↓ touch last_used_at / last_used_ip under touchGranularity  (§3.4)
```

⚠ **The lookup is by hash, so there is no timing side channel to protect against** and no `subtle.ConstantTimeCompare` is needed. If somebody later adds a prefix-then-compare path, that stops being true — don't.

⚠ **`ClientID` stays empty.** It is the per-tab id the browser sends on mutations, echoed back as the `origin` of the resulting websocket push so a tab can recognise its own change. An MCP client has no tab; leaving it empty means every open browser treats an agent's write as somebody else's, which is exactly right.

### 3.3 Re-mint, and the fail-closed decision

**Call `auth`'s existing `refreshIdentity` — do not write a second one.** It is the one place the fail-closed decision is taken, shared today by the request middleware and by `RevalidateSession` precisely so that an open websocket and an HTTP request cannot disagree about whether an account is still open. A third caller that disagreed with both would be worse than either.

The MCP path differs in one thing only: **on `ErrUserClosed` it stamps `revoked_at` on the token row** and returns 401, where the session path revokes the session. There is no socket to tear down (contrast v10's `OnSessionRevoked` → `hub.DisconnectSession`), which is the one place a token is genuinely simpler than a session.

⚠ **A transient auth outage must not revoke anything.** `refreshIdentity` already distinguishes *"auth says this account is closed"* from *"auth did not answer"*; only the first revokes. Getting this wrong turns a five-minute outage into every household token needing to be re-minted by hand.

### 3.4 `last_used_at`

Reuse `auth`'s `touchGranularity` (one hour) exactly. Without it, a read-only conversation of forty tool calls does forty writes to one row **on a pool of one connection** — every read tool becomes a writer, and the browser waits behind it.

### 3.5 ⚠ Where this code lives

The token store is **platform**, not a module: `auth` writes it, the MCP host reads it, `admin` lists it, and none of the three may import a feature module.

Put the store in **`platform/auth`** (`tokenstore.go`, beside `session.go`) and the HTTP handlers for `/api/mcp/tokens` in **`platform/mcp`**, mounted onto the gated `/api` group from the composition root the way `pushp.handler.Mount(api)` already is. Rationale: the store is a credential store and belongs with the other one; the routes are the MCP feature's own surface. `platform/mcp` imports `platform/auth`; nothing imports back.

---

## 4. The endpoint, and the two ways it silently becomes HTML

### 4.1 Mounting

```go
// httpx.Deps gains one field:
//   MountMCP func(r chi.Router)   // mounted OUTSIDE the /api group. May be nil.
```

In `NewRouter`, after the `/api` block and before the websocket:

```go
if d.MountMCP != nil {
    r.Route("/mcp", d.MountMCP)   // its own chain: bearer, Origin, semaphore, deadline
}
```

⚠ **It must not be inside `r.Route("/api", …)`.** Inside, it inherits `SessionMW` and `CSRFMW`; the first would make a cookie sufficient (leak row 1) and the second would reject every call, since an MCP client sends no `X-CSRF-Token`.

### 4.2 ⚠⚠ The SPA exclusion — leak row 2

`NewRouter`'s `NotFound` currently reads:

```go
if spa != nil && !strings.HasPrefix(req.URL.Path, "/api/") && req.URL.Path != "/ws" {
    spa.ServeHTTP(w, req)
    return
}
```

**`/mcp` must join that exclusion**, or a mistyped MCP path returns `index.html` **with a 200** and the client reports a protocol error against a perfectly healthy server.

```go
if spa != nil && !strings.HasPrefix(req.URL.Path, "/api/") &&
    req.URL.Path != "/ws" && !strings.HasPrefix(req.URL.Path, "/mcp") {
```

⚠ **This has an identical twin that is not code** (§17.2): if the Coolify path route for `/mcp` is missing, the request never reaches the Go binary at all and the *frontend's* Nginx returns `index.html` with a 200. **Same symptom, different layer.** When `/mcp` returns HTML, check both, in that order: `curl -sI https://home.tilcer.cz/mcp` against the container directly, then through the origin.

### 4.3 The middleware chain on `/mcp`

1. **Enabled** — `HOME_MCP_ENABLED` false ⇒ 404 (not 403: a disabled feature should look absent).
2. **Method** — `POST` only; `GET` ⇒ **405** with `Allow: POST` (D277).
3. **Origin** — if the header is present it must be in `HOME_ALLOWED_ORIGINS`; if absent, allow (a CLI has none) but only with a valid bearer. This is the DNS-rebinding defence the MCP spec requires of HTTP servers. **Reuse `auth`'s existing origin helper** — ⚠ **`originAllowed(r, allowed)` at `platform/auth/middleware.go:526` is UNEXPORTED**, so this is a one-line export (`OriginAllowed`) plus its call site, not a free reuse. Do it anyway: a second allowlist parser is how the two lists come to disagree about a trailing slash.
4. **Bearer** — §3.2. ⚠ **Do not read the session cookie here at all**, not even as a fallback.
5. **Rate limit** — per token id (§11.3).
6. **Semaphore** — per token id, 2 (§11.1).
7. **Deadline** — `context.WithTimeout(ctx, HOME_MCP_CALL_TIMEOUT_SEC)` (§11.2).
8. `RequestID`, `Logger`, `Recover` come from the root chain and are already there. ⚠ **Strip `Authorization` before the logger and before `statusreport`** — see §12 row 14.

### 4.4 `Content-Type` and the body

`application/json` in, `application/json` out. **Reject a batch** (a JSON array at the top level) with a JSON-RPC error: the 2025-06-18 revision removed batching, home does not implement it, and silently processing the first element of an array is worse than refusing.

---

## 5. The dispatcher, and the error mapping that leak row 9 rests on

### 5.1 Methods

| Method | Bearer? | Notes |
|---|---|---|
| `initialize` | **no** | Returns `protocolVersion`, `serverInfo{name:"home", version:…}`, `capabilities{tools:{}, resources:{}, prompts:{}}`. **No tool names** (D312). ⚠ **There is no build-time version in the Go binary** — `VITE_APP_COMMIT` is a Vite arg with no backend twin, and the only backend release identifier is the free-form `STATUS_RELEASE`, which defaults to empty. Report `cfg.Status.Release` when set and the string `"dev"` otherwise; **do not invent a build stamp** for this one field. |
| `ping` | no | `{}`. |
| `tools/list` | yes | Filtered by the token's `modules` allowlist. |
| `tools/call` | yes | §5.2. |
| `resources/list`, `resources/templates/list`, `resources/read` | yes | §9. |
| `prompts/list`, `prompts/get` | yes | §10. |

`MCP-Protocol-Version` is echoed on the response. An unsupported version is refused at `initialize`, **naming the version home speaks** — a client that cannot tell whether it is too old or too new retries forever.

⚠ **Protocol-version drift is now ours** (D278). The `initialize` handler is the one place it surfaces; put the supported-version constant there, alone, with a comment saying what to check when it changes.

### 5.2 ⚠ The two kinds of failure, and why a model treats them differently

```go
// A PROTOCOL failure — JSON-RPC error object, no result:
//   unknown method, malformed params, unknown tool, unparseable JSON.
//   The model cannot fix these by trying again with different arguments.
//
// A TOOL failure — a normal result with isError: true:
//   validation refused it, the caller may not see it, the thing is not there.
//   The model reads the text and adjusts.
//
// Getting these backwards is not cosmetic: a model RETRIES a protocol error
// and READS a tool error.
```

### 5.3 The mapper

```go
func toResult(err error) Result {
    var ae *httpx.APIError
    if errors.As(err, &ae) {
        switch ae.Status {
        case 403:
            // ⚠ LEAK ROW 9. On an ownership- or membership-scoped surface a 403
            // and a 404 must be INDISTINGUISHABLE. 404-never-403 is a property of
            // the ANSWER, not of the status line — a tool that says "forbidden"
            // leaks exactly what "not found" hides.
            return notFoundResult()
        case 404:
            return notFoundResult()
        default:
            return Result{Text: ae.Detail, IsError: true}
        }
    }
    …
}

// ONE function, ONE string, so the two paths cannot drift.
func notFoundResult() Result {
    return Result{Text: "Nenalezeno.", IsError: true}
}
```

⚠ **`notFoundResult` returns a value, and the test compares the two paths byte for byte.** A refusal that differs by a trailing space is still an oracle.

⚠ **A 500 must never reach the client as a 500-shaped result.** An agent retries a 500 and gives up on a 422 (D311), so an internal error is logged with its request id and returned as *"Došlo k chybě, zkuste to prosím znovu."* with `IsError: true` and **no detail** — the detail is in the Log, where a person can read it.

### 5.4 The panic guard

`httpx.Recover` is on the root chain and already logs `"stack", string(debug.Stack())` (`httpx/middleware.go:200`) — **keep that attr**, `statusreport` lifts it into the crash report's own stack field and **the crash board groups on the first frame**. A panic inside a tool call must produce a JSON-RPC internal error, not a dropped connection: an MCP client that loses the socket mid-turn has no way to report what it was doing.

---

## 6. Attribution — three touch points, and one of them is not obvious

1. **`Actor.Label`** — `displayName + " · " + token.name`. `displayName` comes from the session projection (D230 as amended by D270); when it is null, use the email local part, never the raw user id.
2. **The audit write** — `audit.Sink.Record` takes actor and request metadata *"from ctx, never from `e`"* (`audit/sink.go:35`; the *"so a handler cannot forge who did what"* line is `platform/reqctx`'s package doc, not `Record`'s — quote it to the right file). `via` and `via_token_id` follow the same rule: **read them from the ctx in `sink.go`**, from `mcpctx.TokenFrom(ctx)`, and add them to the INSERT. Do **not** add parameters to `Record` — there are **28 non-test call sites**: 21 across ten feature modules (`admin` 10, `chat` 2, `garden` 2, one each in documents / electricity / events / finance / logging / notes / todo) **and 7 in platform itself** (`auth/http.go` ×2, `push/http.go` ×5), none of which knows anything about a token.
3. ⚠ **The third touch point is `admin/listener.go`.** The audit outbox tails `audit_events` and renders notifications from `Entry`. If `Entry` does not carry `via`, a notification rule can never mention it — which is fine for v11 (nothing asks) — but **the `Entry` struct and the notifier's `SELECT` must stay in sync**, and the notifier's column list is hand-written (`notifier.go:223`). Add the columns there in the same commit or the tail silently reads stale shapes the first time somebody widens it again.

**The Log browser** (`modules/logging`): one filter, `via=mcp|ui`, translated to `via IS NOT NULL` / `via IS NULL`; `eventCols` (`query.go:118`) gains the two columns; the row renders a chip reading the token's name, resolved by joining `mcp_tokens` on `via_token_id`.

⚠ **The join must be a LEFT JOIN and the chip must survive a null.** A token row is never deleted, only revoked — but an event from a token minted on a database later restored from a different point could still find nothing. Render the prefix in that case, never a blank chip.

⚠ **`TestActorTypeStaysThree`** — assert the CHECK constraint still names exactly `user | system | service`. This is v9's `TestReplicaIsDeclinedNotUnimplemented` pattern: **a declined thing needs a test, or the next reader repairs it** (D290).

---

## 7. The tool surface — 39, and the shape of an argument

### 7.1 Naming and annotations

`home_<module>_<verb>`, English, snake_case; core tools drop the module. Every description is **one English sentence** saying what it returns and what it costs — *"Lists to-do boards; pass `board` for one board's full tree of columns and cards."*

`readOnlyHint` is set truthfully on every tool. ⚠ **Nothing carries `destructiveHint: true`, and `TestNoDestructiveTools` asserts it** (D294) — the absence is the mechanism, because a gated destructive tool is one the model still proposes and a person still approves at 23:40.

### 7.2 Core (7)

| Tool | Args | Returns |
|---|---|---|
| `home_whoami` | — | member id, display name, roles, `modules` this token exposes, `HOME_TIMEZONE`, today's date in Prague. **The first call of every session.** |
| `home_search` | `query`, `in[]` (modules), `limit` | merged hits (§8) |
| `home_get` | `kind`, `id` | one entity, the module's own detail shape |
| `home_today` | `as_of?` | reminders due + overdue, tasks due, garden tasks — **from the metrics and lists catalogs only** (D310) |
| `home_metrics` | `keys[]?` | no args ⇒ the 19 descriptors; with keys ⇒ values for this caller |
| `home_lists` | `keys[]?` | the same for the 16 lists |
| `home_activity` | `since`, `module?`, `action?`, `actor?`, `entity?`, `limit` | audit digest, **redacted** (§13.2). ⚠ **ADMIN ONLY**, because its HTTP twin is: `/api/logs/**` has been behind `httpx.RequireAdmin` since D5, so a reader who is refused the Log in the browser must be refused it through a token — *roles gate exactly as they do over HTTP* (PRD §V11-3). Read-only is not the same question as ungated, and redaction is the SECOND rule here rather than the first: it keeps another member’s private items from an **admin**, which was never what kept the spine away from a reader. `Tool.AdminOnly` carries it, the host hides such a tool from a non-admin’s `tools/list` and refuses it in words if called anyway, and `TestActivityHonoursTheHTTPRoleGate` is what stops the next reader repairing it. |

⚠ **`home_today` carries no unread count.** D252 kept chat out of both catalogs; an unread figure here would make the one tool that touches no module table reach into one. Unread rides on `home_chat_conversations`, where it is free.

⚠ **`home_metrics` and `home_lists` are two tools doing two things each** (catalog with no args, values with keys). That is deliberate and documented in the description; it saves two tools against the ceiling and models handle it reliably when the description says so.

### 7.3 Per module (32)

| Module | Tools |
|---|---|
| **todo** (5) | `boards` · `card_create` · `card_update` · `card_move` · `checklist` |
| **events** (4) | `upcoming` · `create` · `update` · `complete` |
| **notes** (4) | `tree` · `create` · `update` · `pin` |
| **documents** (3) | `tree` · `update` · `pin` |
| **finance** (3) | `months` · `month_create` · `month_update` |
| **garden** (6) | `tasks` · `task_create` · `task_complete` · `plan` · `planting_create` · `harvest_log` |
| **electricity** (4) | `readings` · `reading_add` · `advance_add` · `summary` |
| **chat** (2, read-only) | `conversations` · `messages` |
| **admin** (1, read-only, admin role) | `status` |

**What is ABSENT, by name, from every module** (D295): every `DELETE`; `publish` (notes, documents); folder delete; `finance` month delete; garden `season_close` / `season_reopen`; all chat writes; all admin writes; every upload; `dashboard/layout`.

⚠ **Season close is the one that reads as harmless and is not.** `CloseSeason` runs `closeOutTasks`, which marks every still-open task `TaskSkipped` — **it destroys work rather than regenerating it** — and the closed year becomes the rotation history C3 and C8 read. `ReopenSeason` is garden's **only** admin-gated action (`garden/http.go:65` is the module's single `RequireAdmin`), and *"přepisuje se tím historie střídání plodin"* is its **audit-event summary** — ⚠ not UI copy, and not a confirmation string anyone has ever seen. `check.go:59` states the dependency outright: **C3 and C8 read CLOSED seasons only.**

### 7.4 How a write tool is built

**Every write tool calls the module's existing service. There is no second write path anywhere in v11** — that is G2, and it is what makes the audit event, the websocket publish and the role gate come for free.

```go
func (p *provider) Call(ctx context.Context, name string, args json.RawMessage) (mcp.Result, error) {
    switch name {
    case "home_todo_card_create":
        var in struct{ ColumnID, Title string; Description, DueOn *string }
        if err := json.Unmarshal(args, &in); err != nil {
            return mcp.Result{}, httpx.ErrUnprocessable("…")   // → isError, never a 500
        }
        if strings.TrimSpace(in.Title) == "" {
            return mcp.Result{}, httpx.ErrUnprocessable("Název karty nesmí být prázdný.")
        }
        card, err := p.svc.CreateCard(ctx, in.ColumnID, …)      // the SAME service the handler calls
        …
    }
}
```

⚠ **Validate before you call** (D311). An agent retries a 500 and gives up on a 422, so a household typo returning the wrong code turns a mistake into a loop. **No write tool may reach a service with an input it could have refused itself** — one negative-number, one empty-string and one out-of-range test per writing module.

⚠ **Two gates, as over HTTP.** The host checks `reqctx.CanWrite` before dispatching any tool with `ReadOnly: false`; the service checks again. `httpx.RequireWrite` has no equivalent here, which is precisely the case `reqctx.CanWrite`'s doc comment was written for.

⚠ **`appdb.Collect` everywhere.** A leaked `*sql.Rows` in a new provider deadlocks **the next query in the whole process**, the browser's included. `internal/arch` cannot check this; it is a review item on the whole diff.

---

## 8. The cross-module search — the one thing with no precedent

Metrics return an `int`. Lists return `[]string`. Widgets return `any` shaped for one host. **Search returns rows from five different FTS5 indexes that have to be ordered against each other**, and nothing in home has ever needed that.

### 8.1 Fan out, never join

The host calls each provider's `Search` with the same `Query`, in module order, and merges. **There is no cross-module SQL and there cannot be** — `internal/arch` forbids it, and the FTS tables are external-content anyway.

### 8.2 The order is a documented merge, not a score

```
1. ExactHit true, most recent first
2. everything else, most recent first
3. ties broken by module registration order
```

**bm25 values from five separate indexes are not comparable**, and a global "relevance" assembled from them is confident nonsense. The tool description says out loud that the result is a merge ordered by recency — so the model knows it is reading a merge and does not treat position as authority. `Hit` has nowhere to put a score (§1.2), which is how this stays true.

### 8.3 The budget is per module

`HOME_MCP_SEARCH_LIMIT` (10) is applied **to each provider**, and the response reports each module's count. A search that returns 40 chat messages and no notes because chat is chattier is a worse answer than 8 of each.

### 8.4 ⚠⚠ The actor-less context — leak row 8, and v9's exact trap

```go
func (p *provider) Search(ctx context.Context, q mcp.Query) ([]mcp.Hit, error) {
    actor, ok := reqctx.ActorFrom(ctx)
    if !ok {
        // ⚠ NOT an empty result. NOT …AnyScope. An ERROR.
        return nil, errors.New("notes: search without an actor")
    }
    return p.store.SearchScoped(ctx, actor.UserID, q.Text, q.Limit)
}
```

v9's build found two surfaces no review had listed — the preview worker and the image GC — because **a background job has no actor, a viewer-scoped read returns nothing, and the next person reaches for the unscoped load**. An MCP tool call has an actor only *after* the token resolves, so every provider is one refactor away from being called without one.

**One test per provider, with a deliberately actor-less ctx**, asserting an error and not an empty slice. An empty slice is what an `AnyScope` bug looks like on the day somebody "fixes" the nil.

---

## 9. Resources

### 9.1 The three templates

| URI | Owner | Content |
|---|---|---|
| `home://notes/{path}` | notes | markdown body |
| `home://documents/{path}` | documents | the file |
| `home://chat/{conversation}/attachments/{id}` | chat | the attachment |

⚠ **`{path}` is the SPA's own splat, verbatim — there is no scope segment.** A shared item has **no prefix** (`home://notes/recepty/gulas`) and a private one is prefixed `soukrome` (`home://notes/soukrome/denik`), exactly as `frontend/src/lib/scope.ts`'s `parseScopedPath` reads a URL. ⚠ **There is no `sdilene` segment and inventing one would be the only place in the app where the shared root is named.** The parse is unambiguous for the same reason the SPA's is: **`soukrome` is a reserved slug at both shared roots** (D185), backfilled by `06004`/`07004`, so a folder can never shadow the private tree.

The payoff is that the tail of a URI is a route: paste `notes/soukrome/denik` after `/poznamky/` and you are looking at the thing the model just read.

### 9.2 What is served as what

- A file that **is** text — `text/plain`, `text/markdown`, `text/csv` per `documents/mime.go` — is served as text.
- A **PDF or image** is served as content the model can look at.
- ⚠ **Everything else is served as its bytes, and nothing extracts text from it.** `home-gotenberg` converts Office files to **PDF, never to text** (`documents/preview.go` — `deriveOfficePreview` writes `preview.pdf`). A `.docx` reaches the model as a rendered PDF or not at all. **No new conversion path and no `platform/preview`** — D227 stands.

### 9.3 ⚠ A URI is not a capability (leak rows 10 and 11)

- **`resources/list` is viewer-scoped.** A private root appears only for its owner; a conversation appears only for its members. This is the *existence* leak v9 spent a whole version closing, arriving in a new surface.
- **`resources/read` re-checks access from scratch, every call.** A URI member B obtained out of band returns **the same result as a URI that does not exist** — same string, via `notFoundResult`.
- Results are capped at `HOME_MCP_MAX_RESULT_KB` **by the host**, not by each provider (D307): a cap eleven providers must remember is a cap that is wrong in at least one of them. Truncation is **stated in the result**, never silent. ⚠ **An oversized BLOB is refused in words rather than cut** — half a PDF is not a smaller PDF, it is a file that will not open, handed over with nothing the model can read saying so. Text is truncated because a shorter text is still a text; bytes are replaced by a sentence naming the cap.
- ⚠ **`ListResources` takes `resourceListLimit` (200), NOT `HOME_MCP_SEARCH_LIMIT`, and PR 2 must not swap them back.** PR 1 shipped it wired to the search budget and a household with forty notes was shown ten, silently — the model reasons about the tree it was handed as though it were the tree. The two are different questions: a SEARCH wants the best few from each module (D301, ten), a LISTING answers *what is there*. The count bound exists only to protect the one connection, and it is set where this household cannot reach it; `resources/list` has nowhere on the wire to say it was cut short of the cursor pagination v11 does not implement, so the host logs a Warn naming the module when a provider fills the page. `TestResourceListIsNotCappedAtTheSearchBudget` is what goes red.

---

## 10. Prompts

Five, Czech-named and Czech-bodied, English tool calls underneath:

`/co-mě-čeká` · `/nákup` · `/večeře-z-toho-co-máme` · `/měsíční-uzávěrka` · `/zahrada-týden`

They are the cheapest thing in v11 and the place household knowledge actually lives — `/večeře-z-toho-co-máme` is `garden.storage` + `garden.harvests` and a sentence about what this family eats, and it is worth more than any three tools. Keep them as data (a Go slice of literals), not as files: five short strings do not need a loader.

---

## 11. The pool

**`SetMaxOpenConns(1)`** (`platform/db/db.go:36`), deliberately, and the whole backend is written around it. A browser makes one request per view; **an MCP client fans out parallel tool calls by design** and Claude does it on most turns. Six concurrent calls are six-plus queries with nothing pacing them, and the household's dashboard is behind them in the same queue.

### 11.1 Semaphore — 2 per token

`chan struct{}` of capacity 2, keyed by token id, in the middleware. The third concurrent call **waits**; it does not fail. This is back-pressure, not rate limiting, and it is what keeps a chatty agent from being a self-inflicted outage.

⚠ **Acquire before the deadline is set, or a queued call spends its whole budget waiting.** Set the timeout after the semaphore is held.

### 11.2 Deadline — 20 s on the ctx

So a pathological query cannot hold the one connection after the client has given up. The cancellation must reach the DB — which it does, since every store call takes the ctx.

### 11.3 Rate limit — 120/min per token

⚠ **`auth.rateLimiter` cannot be reused, and this is the note most likely to be acted on without being read.** It is a **fixed-window** counter, it is **unexported**, and its `allowed(key)` deliberately *records no attempt* — only `fail()` counts, because it exists to throttle failed logins. A call-rate limiter counts successes. **Write a small one in `platform/mcp`** (D306). Do not promote and generalise `auth`'s: two callers with opposite counting semantics behind one type is how the login limiter stops working.

### 11.4 ⚠ The acceptance criterion is a load test

*"Twenty concurrent tool calls from one token, and a browser request served throughout."* This is the shape of thing v10.2 proved a green suite does not tell you. ⚠ And **`go test -race` is unavailable on the windows/arm64 dev host**, so the concurrency review is by reading, on the one surface where that is least comfortable.

---

## 12. The leak table, row by row

**Write these tests first.** Each is written from the attacker's side; each names the row it closes.

| # | Test | Asserts |
|---|---|---|
| 1 | `TestMCPRejectsCookieOnly` | A request with a **valid session cookie and no bearer** is 401. Not 200, not "authenticated as the cookie's owner". |
| 2 | `TestMCPPathNeverServesSPA` | `GET /mcp/nonsense` returns `application/json`, not `text/html`. **Assert on the Content-Type** — the failure looks like a working server. |
| 3 | `TestMCPOriginAllowlist` | A foreign `Origin` is refused; an absent `Origin` with a valid bearer succeeds. |
| 4 | `TestRevokedTokenFailsNextCall` | Revoke between two calls in one test; the second is 401 with no restart. |
| 5 | `TestClosedAccountRevokesToken` | `ErrUserClosed` ⇒ `revoked_at` stamped; the **next** call is a clean 401 against a revoked token, **not a second re-mint**. And: a transient auth error revokes **nothing**. |
| 6 | `TestTokenCannotExceedOwner` | A `reader`'s token is refused every write tool; the `modules` allowlist narrows and cannot widen. |
| 7 | `TestActivityRedactsOtherMembersPrivateItems` | ⚠ **Written against a SECOND member's private item**, not the caller's. `home_activity` returns the redacted phrase for that `entity_type` with the id blanked. |
| 8 | `TestSearchIsViewerScoped` + `TestSearchWithoutActorErrors` | B searching for a term only in A's private note gets nothing; A gets one. **And every provider errors on an actor-less ctx** — one test per provider. |
| 9 | `TestRefusalIsIndistinguishable` | Ownership- and membership-scoped tools return a result **byte-identical** to the not-found result. Compare the structs, not the sentiment. |
| 10 | `TestResourceURIIsNotACapability` | B reading A's URI gets the same result as reading a URI that does not exist. |
| 11 | `TestResourceListIsViewerScoped` | `resources/list` for B enumerates no private root of A's and no conversation B is not in. |
| 12 | `TestChatReadIsAuditedWithoutBodies` | One `chat.read` event per thread read, carrying the conversation id and a count and **no body, snippet or message id** — asserted against `audit_events` **and** `audit_changes`. |
| 12b | `TestChatMessagesAreNotAudited` | ⚠ **The existing v10 test still passes.** D231 is not reversed by D297. |
| 13 | `TestUnauthenticatedInitializeLeaksNothing` | `initialize` without a bearer returns capabilities and **no tool names**; `tools/list` without one is refused. |
| 14 | `TestNoTokenInCrashReport` | A forced panic inside a tool call produces a status report with **no `Authorization` header and no token**, while **still carrying the `stack` attr** the crash board groups on. |
| 15 | `TestResultCapEnforcedByHost` | An oversized resource read is truncated by the host and **says so** in the result. |
| 16 | — | Push is not suppressed; the notification's audit trail carries `via='mcp'` (D321). Covered by the attribution test. |
| 17 | — | No provider leaks `*sql.Rows`. **`internal/arch` cannot check this** — review item + the load test. |

---

## 13. The two audited reads (D297)

### 13.1 What is written

| Action | When | `entity_id` | `meta` |
|---|---|---|---|
| `chat.read` | a token reads a thread | conversation id | `{token_id, message_count}` |
| `notes.private.read` | a token reads a private note | note id | `{token_id}` |

**The container, never the content.** No body, no snippet, no message id. The Log is not a second copy of the chat, which is the entire reason D231 exists.

⚠ **These fire only when the reader is a token** — `mcpctx.TokenFrom(ctx)` is present. A browser read writes nothing, as it always has.

Two Czech `actionLabels` entries, or they surface as `chat.read` in the rule composer — `TestActionLabelsCoverEveryAction` will catch it, and `admin/labels.go` is the **sixth host map** (D213) precisely because it degraded silently for three versions.

### 13.2 `home_activity` must redact what the page redacts

⚠ **Get the two functions the right way round — they are not interchangeable and only one of them is the page's.**

- **`audit.Redact(entry, viewerID)`** is what the **Log page** calls, in `logging/query.go:176`'s `redactEvent`. ⚠ It redacts the *summary* and **cannot** redact changes, so `logging` **drops `Changes` itself** right after (`query.go:259` says so in as many words). A consumer that calls `Redact` and then returns the changes has redacted nothing that matters.
- **`audit.RedactRendered(entry, changes, viewerID)`** has exactly **one** non-test caller in the whole backend — `admin/listener.go:384` — and it is the notification path, not the page.

`home_activity` reads the raw table, so it must do **both halves of what the page does**:

```go
e = audit.Redact(e, actor.UserID)
if redacted(e) { changes = nil }        // ⚠ the half logging/query.go does by hand
```

**Extract that pair into one helper and have `logging` and `platform/mcp` both call it**, or the two consumers will drift — which is precisely how the raw table came to have a second reader with no rule.

⚠ **This is the v9 trigger-listener gap — *the code is correct and nothing tests it* — arriving in a brand-new consumer.** v9's is still untested (the largest v9 gap); v11 adds the second consumer and the first test. Write both.

---

## 14. The manifest, and the arch test

`internal/arch/mcp_completeness_test.go` — the guard for the **new host map**:

- tool names unique across the registry;
- every tool's `Module()` is a registered module;
- every member of `openapi.yaml`'s `McpModule` enum is a module with a non-empty `Tools()`;
- every declared `ResourceTemplate` is readable by its own provider;
- **no tool carries `destructiveHint`**;
- tool count ≤ **45**;
- the serialised manifest **equals `backend/mcp-manifest.json`**, and the test writes the file when `-update` is passed.

**`TestRealModulesImplementTheStorageCatalog`** — in `internal/arch/storage_completeness_test.go`, *not* in `platform/storage` — exists because a real bug shipped green: `StorageBlobs`/`PrivateItems` were implemented on the *Service* rather than the *Module*, everything compiled, every test passed, and the Úložiště page reported **0 B**. *"It was found by opening the page."* **There is no page to open here**, which is the whole argument for a golden file.

⚠ **`mcp-manifest.json` is generated, never hand-written.** The **seventh** thing on the host-map list — the PRD's own count is six (D213) — is not also going to be hand-maintained.

---

## 15. What the frontend needs from the backend

Design detail is in `HANDOFF-design.md` §v11. The contract:

- `GET/POST /api/mcp/tokens`, `PATCH/DELETE /api/mcp/tokens/{id}`, `GET /api/admin/mcp/tokens`, `DELETE /api/admin/mcp/tokens/{id}` — openapi **0.15.0**.
- `McpTokenCreated.secret` arrives **once**. The client must not persist it anywhere — not `localStorage`, not a query cache. ⚠ Render it from the mutation's response object and let it fall out of memory with the dialog.
- ⚠ **The token list must be excluded from the TanStack persister**, the way chat is. The file is `frontend/src/platform/pwa/persist.ts` — `mayPersistKey()` at line 43, with `if (key === 'chat') return false` at :59 and `isPrivateItemsKey` (D198) right beside it. **Exclude the admin list at minimum**: a rehydrated list of another member's token names sitting on a shared laptop's disk is the v9 purge-listing reasoning exactly.
- ⚠ **Query keys are ARRAYS and the hazard is segment nesting, not string prefixes.** `frontend/src/api/keys.ts:128–143` records the rule from v10: `['chat','conversation',id]` being a prefix of `['chat','conversation',id,'messages']` invalidates both, and the fix was putting the resource segment **before** the id. Two keys are needed here — the member's own list and the admin list — so give them distinct heads (`['mcp','tokens']`, `['admin','mcp-tokens']`) rather than nesting one under the other.
- `GET /api/logs?via=mcp` for the Log filter.

---

## 16. Tests

### 16.1 Write the adversarial ones first

§12, all seventeen, before any provider exists. They are cheap against three modules and expensive against nine.

### 16.2 The named cases

- **Migration:** `01003` and `02005` apply cleanly on an empty DB **and on a restored copy of production**; both downs run. ⚠ **`TestAuditViaMigrationDoesNotRebuild`** — the file contains no `CREATE TABLE`, no `DROP TABLE`, no rename, and `audit_changes`'s row count is identical before and after.
- **`TestActorTypeStaysThree`** — the CHECK still names exactly three values (D290).
- **`TestNoDestructiveTools`** — no tool carries `destructiveHint`, and none of the absent verbs appears in the manifest by name.
- **`TestUnknownToolIsProtocolError`** — calling `home_notes_delete` returns *unknown tool* at the protocol level. ⚠ **The criterion is the absence, not a polite refusal.**
- **`TestWriteToolsValidateBeforeService`** — one negative-number, one empty-string and one out-of-range case per writing module, each a 422-shaped result and never a 500 (D311).
- **`TestExpiryNeverSlides`** — `expires_at` is byte-identical after 50 calls; `last_used_at` advances at most once in an hour.
- **`TestMCPWriteIsAuditedInTheSameTx`** — the house invariant, at least one tool per writing module.
- **`TestViaFilterUsesPartialIndex`** — assert the query plan, or the filter is a scan on the largest table in the database.
- **Regression:** `POST /api/electricity/periods` with a negative `invoiced_vt_dkwh` **still returns 500**, and electricity's `limit` still falls back where the shared helper clamps. ⚠ Both defects are **recorded as unfixed** (D311); a test that pins them is how they stay recorded rather than quietly repaired and undocumented.

### 16.3 What a green suite still does not prove

v10 wrote this lesson three times and v10.2 wrote it a fourth: a fully green suite coexisted with a search that 500'd on an apostrophe, a thread capped at fifty messages, a blank clean-up page, a 415 px pane in a 375 px grid, and **an axe suite red at baseline for five releases because nobody had ever run it green**.

For v11 specifically, the suite will not tell you:

1. **whether a real MCP client can complete `initialize`** — protocol negotiation is the one thing a Go test asserts against its own assumptions;
2. **whether twenty parallel calls starve the browser** (§11.4);
3. **whether the Coolify path route exists** (§17.2);
4. **whether the tool descriptions are any good** — a model choosing the wrong tool is a documentation defect with no failing test. ⚠ Budget a session of actually *using* it, in Czech, on real household data, and fix the descriptions the model got wrong.
5. ⚠ **a click-through with a second member's session** — owed since v8, and v11 is the version where "another member's view" is the whole risk.

---

## 17. Config and deploy

### 17.1 Six variables, all defaulted, none secret

`HOME_MCP_ENABLED` (**true**) · `HOME_MCP_RATE_PER_MIN` (120) · `HOME_MCP_CALL_TIMEOUT_SEC` (20) · `HOME_MCP_MAX_RESULT_KB` (256) · `HOME_MCP_MAX_TOKENS_PER_USER` (10) · `HOME_MCP_SEARCH_LIMIT` (10, **per module**).

⚠ **All six refuse at both ends** rather than clamping. v10's two older windows clamp with a loud `CONFIGURATION CORRECTED` warning because aborting `Load` would crash-loop the container on the deploy that lands them; **nothing is upgrading into these**, so refusal is correct.

⚠ **`HOME_MCP_ENABLED` says its state ONCE at boot**, the way `statusreport` does and for the same reason: a disabled server and a misconfigured client look identical from the client.

⚠ Add all six to `config.Redacted()`'s coverage and to **README's env tables** — and while you are in that table, `HOME_CHAT_TRASH_DAYS` is **still missing**, unrecorded since v10.

### 17.2 ⚠ The Coolify route

`home.tilcer.cz` is **two public apps sharing one origin**, routed by path — `home-backend` (`/api`, `/ws`) and `home-frontend` (catch-all). `home-gotenberg` is a third app with **no public domain** and is not path-routed.

`/mcp` is therefore the **backend's third path route**, to port **7999**, and **Strip Prefix stays OFF** as it has since v1.

⚠ **Without the route, `/mcp` reaches the frontend's Nginx and returns `index.html` with a 200** — indistinguishable at the client from §4.2's bug. Verify with `curl -si https://home.tilcer.cz/mcp -X POST -d '{}'` and expect JSON.

---

## 18. Audit and security, in one place

- The token is **never logged**. `Authorization` is stripped before `httpx.Logger` and before the `statusreport` handler (which lifts attrs into the crash report). `config.Redacted()` covers the new variables.
- **No new secret and no new outbound dependency.** Nothing is added to `LITESTREAM_*`, nothing to the `STATUS_*` set, no third-party endpoint.
- **`mcp_tokens` is replicated by Litestream like every other table.** ⚠ It contains hashes, never secrets, so a restored replica grants nothing — which is the property that makes storing only the hash worth the "you can't see it again" complaint.
- **Every mutation still audits in the same tx.** The only additions to that rule are two *read* actions (D297), and chat messages remain unaudited (D231/D256).
- ⚠ **The largest residual risk is not in this document.** It is that the surface was deliberately opened onto private notes and chat bodies (PRD §V11-3), so a leaked token reads them. The mitigations are: a greppable prefix, a hash-only store, an expiry chosen at mint, a one-call revoke, `last_used_at` on a page its owner reads, an admin who can revoke without minting, and two audited reads that make *"what has it seen?"* answerable. **None of them prevents the first hour.** That is the trade Karel took, knowingly, and it belongs written down here rather than discovered later.
