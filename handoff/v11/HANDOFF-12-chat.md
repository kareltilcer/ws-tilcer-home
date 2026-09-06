# Home — v10: Chat (`chat` · `admin` · `platform/ws` · `platform/storage`)

> **Read first:** root `CLAUDE.md`, then `PRD.md` **§V10-1…§V10-11** (decisions **D216–D262**), `openapi.yaml` **0.12.0**, the scope brief `V10-chat-brief.md`, and the design addendum `HANDOFF-design.md` **§v10**. Build guide for v10. Owner: Karel. Issued 2026-08-26.
>
> ⚠ **THIS IS THE v10 BUILD GUIDE AND v10 IS BUILT.** The served contract is now **0.13.0**, not the 0.12.0 this document keeps naming, and three as-built sections correct it: `PRD.md` **§V10-12** (PR 2), **§V10-13** (PR 3) and **§V10-14** (v10.1 — reactions, the row preview, the koš, the gestures, decisions **D265–D269**). Read those before trusting a version number or a table below. The `openapi.yaml` in this folder is the frozen **0.12.0 spec snapshot** and has not been updated since it was issued; `backend/openapi.yaml` is the contract the server actually serves.
>
> ⚠ **This is a module build with two platform changes underneath it, and the platform changes are the risky half.** `HANDOFF-5` through `HANDOFF-10` were all the same shape — a new package, a new migration block, a registry entry, three or four host maps, done. v10 has all of that **plus** a change to `platform/ws`, which ten modules already publish through, **plus** a new verb in `platform/storage` that spans two modules and two object-store calls with no transaction over them.
>
> ⚠ **It ships as THREE pull requests (D261), not one.** v7, v8 and v9 each shipped as one PR; v10 is roughly twice the size of any of them and its riskiest change sits at the bottom of the stack. Building it as one PR means the `ws` change is first written and last verified.
>
> **The new access rule, stated once:**
>
> > **A conversation is readable by the people in it, from their `effective_from` onward.** Not "everyone" (v1–v8), not "one person" (v9). A third axis.
>
> PRD **§V10-4a — the leak table** — enumerates twenty-three surfaces where that rule can be read around. **Twenty-one deny; four are accepted disclosures with the reason recorded.** Treat it as the build checklist and the test plan. ⚠ v9's equivalent table went from eighteen rows to twenty-three under review and the build still found **two more that no review had listed** — the preview worker and the image GC, background jobs with no actor. v10's equivalents are **the drain, the koš purge and thumbnail generation**. Assume the table is short.

## The model in one paragraph

`chat_conversations` holds one row per room, exactly one of them `kind='default'` — **Všichni**, everyone auto-joined. `chat_members` is `(conversation_id, user_id)` plus **`effective_from`**, the instant from which the conversation is that member's to read; every read path joins it and filters on it **in SQL**. Messages are soft-deleted to a tombstone; conversations are soft-deleted into a **7-day koš** and purged by a scheduler job. Attachments live under a `chat/` prefix in the primary bucket, are never mirrored, and are `live`, `moved` or `removed`. Two DB-backed thresholds warn — never block — and a member-scoped clean-up page offers three actions, one of which is doing nothing. **Moving a file to Dokumenty is a custody transfer across the storage catalog and it publishes the file to the household.** Messages write **no audit event**; structural changes write eleven kinds. `/ws` grows a per-user index so a message reaches only members, and carries `prev_message_id` so a dropped frame is detectable.

---

## Build order — three PRs (D261)

**Do not build this in the order the PRD reads.** Each PR below is green on its own, and nothing in PR 1 mentions chat.

### PR 1 — `platform/ws` grows an identity

| # | Step |
|---|---|
| 1 | `client.userID`, `Hub.byUser`, `PublishTo` (§1) |
| 2 | Tests: fan-out to a subset, removal from **both** maps, multi-tab per user, the dev-bypass actor |
| 3 | Nothing else. `Publish` is untouched; every existing module's fan-out must be byte-identical |

⚠ **Land this alone and watch it.** It is a live strand with ten publishers, its failure mode is "a board stopped updating", and a bug in it that arrives inside a 90-file chat PR will be found by bisecting the chat PR.

### PR 2 — chat core

| # | Step | Why here |
|---|---|---|
| 1 | **Migration `12001`** (§2) | Everything is downstream of the schema, and `chat_messages` carries an FTS5 rowid contract that must be right on the first try |
| 2 | **The store layer takes `(actor, conversation)` before any handler knows what a conversation is** (§3) | The membership join and the floor belong in one place. Two implementations of an access rule is one implementation and one bug |
| 3 | **The leak table, rows 1–4 and 10–13, with adversarial tests written FIRST** (§4) | This is the bulk of PR 2 |
| 4 | Conversations, membership, the koš (§5) | |
| 5 | Messages, replies, edit, delete, unread (§6) | |
| 6 | Search (§7) | Needs the FTS triggers from step 1 and the floor from step 2 |
| 7 | Realtime — `PublishTo` + `prev_message_id` (§8) | Needs PR 1 merged |
| 8 | Push + migrations `02004` / `08003` (§9) | ⚠ `08003` rebuilds a live table |
| 9 | Frontend: list, thread, members, nav (§12) | |
| 10 | ⚠ **`backend/openapi.yaml` → 0.12.0 in this PR** | The v7/v8 lesson. v9 did not repeat it; neither does this |

### PR 3 — attachments, storage, clean-up, the move

| # | Step | Why here |
|---|---|---|
| 1 | Attachment upload, kinds, thumbnails, delivery (§10) | |
| 2 | `platform/storage`: `GroupSource`, `BlobSink`, `storage_thresholds` (§11) | |
| 3 | The two thresholds and both warnings (§11.3) | |
| 4 | The clean-up page and *Odstranit* (§11.4) | |
| 5 | **The move — and its fault-injection matrix** (§11.5) | Last, because it is the only thing here that can lose a file |
| 6 | The drain, and the koš purge path (§11.6) | |
| 7 | Administrace → Úložiště chat block (§11.7) | |
| 8 | Frontend: clean-up page, attachment states, the two banners (§12) | |

---

## 1. `platform/ws` — the hub learns who is connected

Today the hub is deliberately anonymous:

```go
type client struct { conn *websocket.Conn; send chan []byte; cancel context.CancelFunc }
type Hub struct { mu sync.Mutex; clients map[*client]struct{} }
```

The handler already resolves an actor from the session cookie and **throws it away**. Keep it.

```go
type client struct { conn *websocket.Conn; send chan []byte; cancel context.CancelFunc; userID string }

type Hub struct {
    mu      sync.Mutex
    clients map[*client]struct{}            // unchanged — Publish still walks this
    byUser  map[string]map[*client]struct{} // v10
}

func (h *Hub) Publish(m Message)                       // UNCHANGED
func (h *Hub) PublishTo(userIDs []string, m Message)    // v10 — marshal once, fan out to the union
```

**Rules:**

- **`Publish` must not change at all.** Ten modules depend on it and none of them should need a line touched. A test asserts the existing broadcast still reaches an anonymous client.
- **`PublishTo` marshals once**, then walks the union of `byUser[id]` for the given ids. Not once per user — that is the whole reason `prev_message_id` cannot be per-recipient (§8).
- ⚠ **`remove` must delete from BOTH maps.** A `remove` that clears `clients` and forgets `byUser` leaves a growing set of dead `*client` pointers under every user id, holding their channels and their contexts. It never errors, never logs, and shows up as memory months later. **Write the test that connects and disconnects the same user fifty times and asserts `len(h.byUser["u"]) == 0`.**
- **The dev bypass has no actor.** Register under `cfg.BypassActor.UserID` when one is configured; otherwise the client is anonymous and `PublishTo` never reaches it. `HOME_DEV_AUTH_BYPASS` is refused in production anyway.
- **A user with no connections is not an error.** `PublishTo` with ids nobody is connected under is a no-op, not a failure — a phone that is asleep is the normal case.

**Do not** take this opportunity to rewrite v9's D190 fan-out. Private notes and documents keep publishing `{"private":"1"}` with the id dropped, to everybody. `PublishTo` makes that unnecessary; unnecessary is not wrong, and changing a live privacy path for a latency benefit nobody asked for is v11's decision to make (D234).

---

## 2. Migration `12001_chat.sql` — block 12

Six declared tables plus five FTS shadows. **A new block, because there is a new module** — the schema-level statement of D216.

### 2.1 `chat_messages` carries an FTS5 rowid contract

```sql
CREATE TABLE chat_messages (
    seq             INTEGER PRIMARY KEY,          -- explicit rowid alias — see below
    id              TEXT NOT NULL UNIQUE,         -- UUIDv7; the logical key and the ordering key
    conversation_id TEXT NOT NULL REFERENCES chat_conversations (id) ON DELETE CASCADE,
    author_id       TEXT NOT NULL,
    body            TEXT NOT NULL,
    reply_to_id     TEXT REFERENCES chat_messages (id),
    created_at      TEXT NOT NULL,
    edited_at       TEXT,
    deleted_at      TEXT
);
```

⚠ **`seq INTEGER PRIMARY KEY` is not decoration.** `chat_messages_fts` is external-content and keyed on the rowid; a TEXT-PK table has only an *implicit* rowid, which `VACUUM` renumbers — desynchronising the index so that search quietly returns the wrong rows. This is the same comment `06001` and `07001` carry, for the same reason, and it is the single line in this migration most likely to be "simplified".

**Consequence, and it is not optional: this table must never be rebuilt.** So the "body or attachment" invariant is a **service-level check in the write transaction, not a table CHECK** — the v9 D179 precedent exactly. Do not add it later with an `ALTER TABLE`; there is no such statement in SQLite and the rebuild that would implement it breaks search.

### 2.2 The rest

```sql
CREATE TABLE chat_conversations (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL CHECK (kind IN ('default','group')),
    name TEXT NOT NULL,
    created_by TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
    deleted_at TEXT, deleted_by TEXT
);
CREATE UNIQUE INDEX ux_chat_default ON chat_conversations (kind) WHERE kind = 'default';

CREATE TABLE chat_members (
    conversation_id TEXT NOT NULL REFERENCES chat_conversations (id) ON DELETE CASCADE,
    user_id         TEXT NOT NULL,
    effective_from  TEXT NOT NULL,
    added_by        TEXT,
    muted           INTEGER NOT NULL DEFAULT 0,
    last_read_at    TEXT,
    PRIMARY KEY (conversation_id, user_id)
);

CREATE TABLE chat_attachments (
    id TEXT PRIMARY KEY,
    message_id      TEXT NOT NULL REFERENCES chat_messages (id) ON DELETE CASCADE,
    conversation_id TEXT NOT NULL,          -- DENORMALISED, deliberately — see below
    kind            TEXT NOT NULL CHECK (kind IN ('image','video','file')),
    original_filename TEXT NOT NULL, content_type TEXT NOT NULL,
    byte_size INTEGER NOT NULL, checksum TEXT NOT NULL,
    storage_key TEXT NOT NULL, thumbnail_key TEXT,
    width INTEGER, height INTEGER,
    state TEXT NOT NULL DEFAULT 'live' CHECK (state IN ('live','moved','removed')),
    document_id TEXT,
    uploaded_by TEXT NOT NULL, created_at TEXT NOT NULL,
    cleaned_by TEXT, cleaned_at TEXT
);

CREATE TABLE chat_deleted_keys (key TEXT PRIMARY KEY, queued_at TEXT NOT NULL, purge_after TEXT NOT NULL);
```

**`chat_attachments.conversation_id` is denormalised on purpose.** The per-conversation byte sums and the clean-up listing are the two hottest queries in the storage half, and both would otherwise join through `chat_messages` for a column that never changes. It is written once at upload and is never updated.

**Indexes:**

```sql
CREATE INDEX idx_chat_conv_active    ON chat_conversations (deleted_at);
CREATE INDEX idx_chat_members_user   ON chat_members (user_id);
CREATE INDEX idx_chat_messages_conv  ON chat_messages (conversation_id, id);
CREATE INDEX idx_chat_att_conv_state ON chat_attachments (conversation_id, state);
CREATE INDEX idx_chat_att_message    ON chat_attachments (message_id);
CREATE INDEX idx_chat_deleted_due    ON chat_deleted_keys (purge_after);
```

`idx_chat_messages_conv (conversation_id, id)` carries the thread, the floor and the cursor in one index — the floor is a range on `id`, because UUIDv7 ids sort chronologically. Do not add a separate index on `created_at`; it would be a second ordering that disagrees with the cursor under clock skew.

### 2.3 FTS5

```sql
CREATE VIRTUAL TABLE chat_messages_fts USING fts5(body, content='chat_messages', content_rowid='seq');
```

plus the three triggers (insert / delete / update) in the shape `06001` and `07001` already use. ⚠ **The update trigger must fire on the delete path too**, because a soft delete blanks `body` — see §6.3.

⚠ **This is the FIFTH external-content FTS5 index in Home, which takes the shadow count from twenty to twenty-five.** Declare them with `storage.FTSShadows("chat_messages_fts")`; §V9-12 records that `garden_plants_fts` went uncounted for two versions, and `arch/storage_completeness_test.go` is what stops it a third time.

### 2.4 Seed

One row: `kind='default'`, name **"Všichni"**. **No membership is seeded** — the directory is projected from `sessions` and a member who has never logged in does not exist yet (§5.1).

### 2.5 Down migration

Drop the triggers, then the virtual table, then the tables. A down that drops `chat_messages` before its triggers leaves orphaned triggers referencing a missing table and the next `up` fails.

---

## 3. The store layer takes membership and the floor before anything else does

**One function, used by every read:**

```go
// scopeFor returns the predicate fragment and args for "messages this actor may read
// in this conversation". Every read path in the module goes through it.
func (s *Store) memberScope(ctx context.Context, actor, conversationID string) (Scope, error)
```

It resolves membership **and** `effective_from` in one query, returns `ErrNotMember` (which the handler renders as **404**, never 403), and hands back the floor as a bound on `id`. Then:

- **The thread** filters `conversation_id = ? AND id >= ? AND ...`
- **Search** joins `chat_members` inside the `MATCH` query (§7)
- **The attachment listing, the clean-up listing, the unread count and the reply-quote resolver** all take the same bound

⚠ **A floor applied after the rows are fetched is not a floor.** It is a place where `next_cursor`, `has_more` and any total still describe rows the caller may not read. The acceptance criteria assert those three fields precisely because that is the failure a hand test does not see.

⚠ **Background jobs have no actor.** The drain, the koš purge and thumbnail generation must load through an explicit `...AnyScope` variant, named so that it is a decision rather than an accident. §V9-12 records that this exact shape — the preview worker and the image GC — was the pair the v9 leak table did not list, and that a viewer-scoped read there would have left every private upload `pending` forever, silently.

---

## 4. The leak table, row by row

PRD §V10-4a. Build it as a checklist; the numbering below is that table's.

| Row | What to do |
|---|---|
| 1 | Every conversation-scoped handler resolves membership first and returns **404** — the same body an unknown id returns. Assert byte-identical responses in a test. |
| 2 | The thread's floor is in the SQL. Assert `next_cursor` / `has_more` too. |
| 3 | Search joins membership and the floor **inside** the `MATCH` (§7). |
| 4 | The reply quote goes through `memberScope`; below the floor it returns `available:false` **with every other field absent**. |
| 5 | ⚠ **Move the membership load ABOVE the `If-None-Match` branch.** A non-member with a valid ETag must get 404, not 304. v9 shipped this bug in `documents` and caught it; do not re-derive it. |
| 6 | `Cache-Control: private, no-cache, must-revalidate` on `/raw` and `/thumbnail`, **always**. Never `immutable` — membership is revocable in a way v9's `shared` was not. Assert on the header. |
| 7 | `PublishTo` with the member set from the writing transaction. |
| 8 | Push carries sender + 140 chars. **Accepted** — target it correctly (members minus author, mutes honoured) and change nothing else. |
| 9 | ✅ already closed — `notification_deliveries` stores no title and no body. Verify, do not extend. |
| 10–11 | Unread and the conversation list join `chat_members`; a removed member has no row and therefore no count. |
| 12–13 | The Log is admin-only and chat writes **no message events**. Nothing to redact. Do not extend `audit.Redact`. |
| 14 | The admin storage block carries names and sizes and **no route into a conversation**. Assert against the contract. |
| 15 | The clean-up listing is member-scoped, floor-applied, koš-excluded, `state='live'`. |
| 16–17 | The move publishes (accepted, dialogued); a private target folder is **422 with no copy attempted**. |
| 18 | A trashed conversation is invisible to **every** read, not merely absent from the list. |
| 19 | `GET /api/chat/directory` serialises `user_id` and `display_name`. **Not** `email`, **not** `roles`. |
| 20 | Chat query keys are excluded from the PWA persister. Assert against its configuration. |
| 21–23 | Nothing to build — verify and move on. |

---

## 5. Conversations, membership, the koš

### 5.1 Auto-join and the floor

`chat_members.effective_from` is **not** "when they joined". It is *the instant from which this conversation is theirs to read*.

- **A group add writes `now()`.** Being added to a group is one person's decision about another person's access to a third person's history, and the floor exists to bound it.
- **Auto-joining Všichni writes the conversation's own `created_at`** (D258), so a member the app sees for the first time in 2028 reads the household room in full.

⚠ **The exemption is a VALUE, not a branch.** Write `TestDefaultConversationHasNoHistoryBranch`, which walks the module's Go files and fails on `kind == "default"` outside the create / delete / leave guards. The moment history depends on a `CASE`, it becomes an exception somebody gets wrong in the fourth query that needs it.

**Auto-join happens at first sight** — the first request that resolves the caller to an actor, in the same transaction, idempotently. Not at boot: the directory is `push.Store.Members()` projected from `sessions`, and a member who has never logged in does not exist yet.

### 5.2 Removal leaves a gap

Removing deletes the row. Re-adding writes a new one with a new `effective_from`, so a removed-and-re-added member has a **permanent hole** in the middle of a conversation they otherwise read in full. That is a consequence of D218, not a bug — but the members screen must say so before the removal is confirmed, because nothing else in the UI would explain it afterwards.

Their messages stay. Authorship does not depend on membership.

### 5.3 The koš

`DELETE` sets `deleted_at` / `deleted_by` and queues the conversation's object keys with `purge_after = deleted_at + HOME_CHAT_TRASH_DAYS`.

- **Invisible to every read.** Not "absent from the list" — the thread, search, unread, attachments and the clean-up listing all exclude it. One predicate in `memberScope`.
- **`?state=trash`** lists it for its members with days remaining.
- **`POST .../restore`** clears `deleted_at` and **deletes the queued keys**, in one transaction. Nothing is reconstructed; nothing was ever removed.
- ⚠ **`DELETE ?hard=true` rewrites `purge_after` to `now`.** It exists so that somebody deleting a heavy conversation *to fix an overrun* is never told to come back in seven days.
- **An `admin` who is not a member may restore and purge** (D255) — and `GET` of that same conversation must still 404 for them. Write both assertions in the same test so the asymmetry is visible.
- **Trashed bytes keep counting** toward both thresholds. Do not "fix" this: the storage page's premise is that its figures sum, and the bytes really are still in R2.
- ⚠ **A `moved` attachment's document survives the purge.** The cascade takes `chat/` keys; `documents/` keys belong to another module. Say so in the confirmation dialog.

---

## 6. Messages

### 6.1 Shape

Body up to **8 000 characters**; up to **10 attachments**; optional `reply_to_id` in the same conversation. A row with neither body nor attachment is refused **in the write transaction** (§2.1).

### 6.2 Edit

Own messages only, no time limit, sets `edited_at`. **No history is kept anywhere.** Do not add a `chat_message_versions` table "while we're here": it would be a private, unsearchable, un-redactable copy of every message ever revised, and D231 deliberately keeps message text out of the one store Home has for before-and-after values.

### 6.3 Delete

⚠ **Blank the body in the same statement.**

```sql
UPDATE chat_messages SET deleted_at = ?, body = '' WHERE id = ? AND author_id = ?;
```

`deleted_at IS NOT NULL` alone is **not** enough: `chat_messages_fts` is external-content, so a body left in the table is a body still returned by search. Blank `original_filename` on any attachments the delete takes with it, and queue their keys with `purge_after = now`.

The tombstone stays. Removing the row would leave replies pointing at nothing and silently reflow a thread somebody is reading.

⚠ **No audit event on send, edit or delete** (D231). Write `TestChatMessagesAreNotAudited` — it asserts each of the three leaves `audit_events` unchanged. It exists so that "the missing audit coverage" is not fixed by accident: the reasoning is in D231 and it is not an oversight.

### 6.4 Replies

Flat back-references, never threads. The quote is a **read of the parent** through `memberScope`, and below the floor it returns `available:false` with no author, no date, no excerpt. It is the leak the floor most easily misses, because a quote *looks* like data belonging to the child message.

### 6.5 Unread

`last_read_at` on the membership row. Unread = `created_at > MAX(last_read_at, effective_from)` AND `author_id <> caller` AND `deleted_at IS NULL`. The read endpoint is **idempotent and never moves backwards** — a replayed older marker must not un-read a conversation.

---

## 7. Search

One `MATCH`, with membership and the floor **inside** it:

```sql
SELECT m.id, m.conversation_id, snippet(chat_messages_fts, ...)
  FROM chat_messages_fts f
  JOIN chat_messages m       ON m.seq = f.rowid
  JOIN chat_members  mem     ON mem.conversation_id = m.conversation_id AND mem.user_id = ?
  JOIN chat_conversations c  ON c.id = m.conversation_id AND c.deleted_at IS NULL
 WHERE chat_messages_fts MATCH ?
   AND m.id >= mem.effective_from_id
   AND m.deleted_at IS NULL
 ORDER BY rank
```

⚠ **The placement is the requirement.** FTS5 ranks over what it matched, so filtering the result set afterwards leaves an ordering computed from other people's messages — and a snippet **is** a message body under another name.

---

## 8. Realtime, and the gap

After the writing transaction commits, publish with `PublishTo` and the member set **resolved inside that transaction** (D233). The payload carries the message — body, author, attachments. It is the first `/ws` payload in Home with content, and it is safe only because of §1.

### 8.1 `prev_message_id` — D259, and the reasoning that makes it terminate

⚠ **The hub drops on a full `sendBuffer` by design, and there is no replay.** Every module so far is fine with that: a dropped *"something changed"* is repaired by refetch-on-focus and nothing was lost in the meantime. **A chat message is different — the loss IS the content, in a thread somebody is reading, with nothing on screen to say so.**

Two mechanisms, because they cover different failures:

1. **Every payload carries `prev_message_id`** — the id of the message before it in that conversation. A client whose held latest does not match refetches the tail. UUIDv7 ordering means no sequence column and no schema change.
2. **The tail is refetched unconditionally on every reconnect**, because a socket that dies and delivers nothing afterwards leaves a client silently stale with no frame to check against.

⚠ **The check is ONE-SHOT per received message, and that is what makes it terminate.** `prev_message_id` is computed **once for the whole audience** — `PublishTo` marshals once, and a per-recipient value would mean one marshal per member and defeat the point. So a member whose `effective_from` sits above that id can never hold it, and their **first** message after joining always looks like a gap. One refetch later they hold message *N*, the next payload carries `prev = N`, and it matches from then on.

**A client that re-checked after its own refetch would loop on every message.** Write the test: a member added to a busy conversation performs **exactly one** refetch and none afterwards.

### 8.2 Membership revoked mid-session

Publish `chat.membership.changed` **to the removed member specifically** so their client can leave the thread rather than sitting on a view that has quietly become forbidden. No socket is force-closed and their already-fetched page is not scrubbed; the next request 404s. That is the accepted bound in leak row 22.

---

## 9. Push, and two migrations outside block 12

Every member except the author, honouring `chat_members.muted` and the new `cat_chat` preference, **off the request path** — `push.Sender` is explicit that no mutation may be slowed or failed by a push service.

Payload: sender's display name + up to 140 characters, deep link `/chat/{id}`, `Tag` collapsing per conversation so twenty messages are not twenty banners. Attachment-only messages read *"Karel poslal soubor"*.

### 9.1 `02004_chat_platform.sql`

`ALTER TABLE notification_preferences ADD COLUMN cat_chat INTEGER NOT NULL DEFAULT 1;` plus `CREATE TABLE storage_thresholds (...)` and its two seed rows (512 / 128). Additive, trivial.

### 9.2 `08003_chat_delivery.sql` — ⚠ the only v10 migration that touches live data

`notification_deliveries.kind` and `.category` are `CHECK`-constrained. **SQLite cannot alter a CHECK**, so this is a table rebuild: create the widened table, copy, drop, rename, re-create the **four** indexes. ⚠ **FOUR, not three** — `_ts`, `_kind_ts`, `_rule_ts` and `_status_ts` have all existed since `08001`; this line said three until the v10 build counted them.

- It is small, operational, non-audit data. That makes it low-risk, **not** no-risk.
- **The down migration must delete `'chat'` rows before rebuilding narrow**, or the restore fails on its own copy step.
- **Run it against a restored copy of production**, with the down exercised, the way §V9-12 handled the `soukrome` backfill.
- Do **not** dodge this by recording chat pushes as `kind='trigger'`: that would let a member silence chat by muting Administrace's trigger rules.

### 9.3 Out-of-order goose versions

⚠ `02004` and `08003` are numerically **below** `11001`, which is already applied on the live database. v9 shipped exactly this shape with `01002`, `06004` and `07004`, so the runner tolerates it — **verify that before writing them, not after.**

---

## 10. Attachments

### 10.1 Kinds and caps

Sniff the MIME **server-side**; never trust the client's claim (D48's rule).

| kind | accepted | stored | notes |
|---|---|---|---|
| `image` | png · jpeg · gif · webp | original + `thumb.webp` | **record intrinsic dimensions** — the thread reflowing as images load is the most-noticed bug in any chat |
| `video` | mp4 · webm · quicktime | original only | inline `<video>`, byte ranges. **No transcoding, no poster frame** |
| `file` | anything else | original only | PDF opens in the browser's own viewer. **No gotenberg, no `platform/preview`** |

**The per-file cap is `HOME_DOCS_MAX_UPLOAD_MB`** — read the same config field Dokumenty reads. Do **not** add `HOME_CHAT_MAX_UPLOAD_MB`. ⚠ The reason is not thrift: **a file above Dokumenty's cap could never be moved into Dokumenty**, so the clean-up page's headline action would fail on exactly the files heavy enough to have caused the overrun.

⚠ An iPhone `.mov` (HEVC) will store and may not play. Build the failed-playback state — download button plus a sentence — rather than leaving a broken player.

### 10.2 Delivery

Keys `chat/{attachment_id}/original` and `/thumb.webp`, primary bucket, `chat/` prefix.

- **Membership load before the conditional branch** (leak row 5).
- **`Cache-Control: private, no-cache, must-revalidate` with an ETag, always.** Never `immutable`.
- `?download=true` sets an attachment disposition; there is no `/download` path.
- **Not mirrored.** Chat blobs are excluded from the backup-bucket job. `StorageChat.mirrored` is permanently false and the page renders *Nezálohováno* rather than a gap that reads as zero.

---

## 11. Storage, clean-up, the move

### 11.1 `platform/storage` — two additions

```go
type GroupSource interface { StorageGroups(ctx context.Context) ([]GroupUsage, error) }
type BlobSink   interface { AcceptBlob(ctx context.Context, req AcceptRequest) (AcceptResult, error) }
```

`GroupSource` is named for what it is — **a module reporting named sub-buckets of its own storage** — not for chat's word for it. `documents` could report per-root and `garden` per-bed without a new interface. **The threshold comparison lives in the consumer**; the source reports bytes.

`BlobSink` is implemented by `documents` and handed to `chat` at composition. It is **the first verb in a catalog that has so far carried only projections**, and it is what makes the move possible without a cross-module import.

### 11.2 Chat's catalog registration

`StorageTables()` returns the six tables plus `storage.FTSShadows("chat_messages_fts")`. `StorageBlobs()` walks the `chat/` prefix and attributes each object to its **uploader**, with `Kind: shared`.

⚠ **`shared` is the wrong word here and it is kept deliberately.** In `platform/storage`, `shared` means *not a v9 private item*; a chat attachment is member-restricted, which is a third thing. A fourth `Kind` would change a wire enum and every consumer of it for a distinction the storage page does not draw. **Put that sentence in the code as a comment**, or somebody will "fix" it.

### 11.3 The two thresholds

`storage_thresholds(key, value_mb, updated_at, updated_by)`, platform-owned because `admin` writes and `chat` reads and neither may import the other. Keyed rather than columned so v11 adds a threshold with an INSERT.

**Warn only.** No upload is refused, there is no quota, there is no new 413 anywhere in the app. Two warnings: Administrace for the module total, and the chat module for a member of a conversation over `chat.conversation`. Both link to `/chat/uklid` — **and the link is not rendered for a member who cannot pass §11.4's gate.**

### 11.4 The clean-up page

Gated **member ∧ (`editor` | `admin`)**. Lists `state='live'` attachments from the caller's own conversations, floor applied, koš excluded, grouped by conversation, over-limit rooms flagged, `sort=size` by default.

- ⚠ **A member of no conversation gets an empty page with an explanation, not a 403.** The gate passed; there is nothing to clean.
- ⚠ **A `reader` gets 403 with the reason named**, and the warning banner does not offer them the link in the first place. This is the accepted asymmetry in D241 — a reader can fill storage they can never clean — and the copy should not pretend otherwise.
- **`sort=size` is single-page** (a keyset cursor is an id, and an id does not locate a position in a size ordering). A cursor with it is **422**, refused rather than ignored — the §V9 `private-items` precedent.
- ***Ponechat* is not a button.** Nothing is staged, nothing is queued, closing the page is a valid outcome. Do not build a "review changes" step.
- ***Odstranit* deletes the object INLINE**, falling back to the queue on failure. It is the only path that does. The workflow is *clean until the number goes down*, and a figure lagging fifteen minutes behind the button makes the page unusable.
- **Leaving while over threshold** raises a confirmation via the **router's navigation blocker**. `beforeunload` alone misses client-side route changes, which is most exits.

### 11.5 The move — the ordering, and the fault-injection matrix

**The ordering is not negotiable:**

1. **Validate** the target folder is shared and writable. *(A private v9 folder is 422 with no copy attempted.)*
2. **Copy** `chat/{aid}/original` → `documents/{did}/original` via `blobstore.Copy` — same account, server-side, nothing streams through the app.
3. **Insert** the `documents` row in its own transaction, with its own audit event and `meta.via: "chat"`.
4. **Mark** the attachment `moved` in its own transaction, audited.
5. **Delete** the chat object — **last**, enqueued on failure.

⚠ **No transaction covers all five and none can.** Two SQLite writes and two object-store calls. The order is chosen so every crash point **over-counts rather than loses**:

| crash after | state | recovery |
|---|---|---|
| 2 | bytes in both places, no document row | unattributed object under `documents/` — reported by v9's machinery, never auto-cleaned. Re-running the move is safe; `idx_documents_checksum` finds it |
| 3 | document exists, attachment still `live` | in Dokumenty **and** still counted against chat. Visible, fixable, re-runnable |
| 4 | attachment `moved`, chat object present | chat counts bytes it no longer owns — the drain removes it next pass |

**Never delete before the copy is confirmed. Never mark `moved` before the document row exists.** Both inversions lose the file.

**Write the fault-injection tests.** Inject a failure at each of steps 2, 3, 4 and 5; assert the state above; assert that re-running the move from each state succeeds; assert that **no injected failure loses the file**. This is the acceptance criterion the PRD spends a table on, and it is the only part of v10 that can destroy data silently.

**With no sink configured:** `501`, and the UI renders no button. ⚠ It does **not** fall back to delete. A capability that silently becomes a different, destructive capability is worse than one that is plainly absent. `TestDocumentsImplementsBlobSink` is written **before** the move is — §V9-12 records `TestRealModulesImplementTheStorageCatalog` existing because a catalog method landed on the wrong type, everything compiled, every test passed, and the page reported 0 B. *"It was found by opening the page."*

⚠ **A move is a publish.** The dialog carries the sentence before the confirm:

> *"Soubor bude viditelný pro všechny členy domácnosti, i pro ty, kteří nejsou v této konverzaci."*

### 11.6 The drain

`chat_deleted_keys(key, queued_at, purge_after)`, drained every **15 minutes** by chat's **only** scheduler job: batch-delete everything due, clear the rows, log one structured line with the count deleted and the count deferred.

⚠ **The job has no actor.** Every load it makes uses an explicit any-membership variant. See §3.

### 11.7 Administrace → Úložiště

A `chat` block: module total against `chat.total`, then a table of every conversation — name, bytes, objects, member **count**, over-limit flag, koš state with days remaining — sorted by size.

⚠ **No way in.** No thread, no message, no attachment list, no clean-up page, no link. The admin's two verbs over a room they are not in are restore and purge, and neither opens it. Assert the absence against the contract, not against the UI.

The two threshold fields are editable in place, in MB, audited as `chat.settings.updated`.

---

## 12. Frontend

**Nav — the first demotion in the nav** (D260). `AppShell`'s `PRIMARY` gains chat; **Okno do budoucnosti moves into `OVERFLOW`**. Four thumb tabs plus *Více* is the shape that works at 375 px and a fifth makes six. Nothing about Okno changes except where its link lives. The chat tab carries the unread badge.

**Three of the six host maps gain a `chat` entry** — `AppShell`, the Log browser's `MODULES`, `admin/listener.go`'s `inAppURL` → `/chat`. ⚠ **`platform/widgets/registry.tsx` gains NONE**, and the diff must prove it: an entry there for a module with no widget provider is a dashboard tile that resolves to nothing, with no compile error and no runtime error (v8's trap). Third version running with that file untouched.

**Layout: two-pane at ≥1024, stacked below** (D262). List left, thread right; `/chat/{id}` renders both. Below 1024 the thread is a route push and browser-back returns to the list. Members are a panel or sheet, not a third column.

**Three requirements that are not polish:**

- **Which conversation is on screen must be unmistakable** at 375 px in both themes. The cost of getting it wrong is posting into the wrong room, and there is no unsend.
- **Own versus others' bubbles must not rely on colour alone** — alignment and label carry it, colour reinforces. The `--c1…--c5` CVD constraint applies here as much as to a chart.
- **Images must not reflow the thread as they load.** That is what the recorded dimensions are for.

**Three attachment states, three renderings:** live shows the file; `removed` shows the epitaph — filename, size, who, when; `moved` shows the file from Dokumenty with a marker saying where it lives now.

**The composer** takes drag-and-drop, paste and a picker, shows a per-file progress row, and **refuses an over-cap file before uploading it**, naming the limit in MB.

⚠ **Chat is excluded from the PWA persister entirely.** The route renders an offline state, not a stale thread. This is a deliberate departure from every other module: message bodies and other members' names on a shared laptop's disk are worth less than the offline convenience, and v9 already established the threat model — *"a laptop in the kitchen gets used by more than one person"*.

**Every chat query key carries the conversation id as a segment.** A key shared across two conversations is the single most likely frontend bug in this version — v9's note about `scope` in query keys, in a module where the payload is the content.

---

## 13. Tests

### 13.1 Write the adversarial ones first

Every leak-table row gets at least one test written from **the attacker's side**: a second member who is not in the conversation, a member below their floor, and an admin who is neither. Assert 404 (not 403), an empty result, or an absent delivery.

### 13.2 The named cases

- **The 304 ordering** (row 5) — a conditional request from a non-member returns 404.
- **The floor's page metadata** — `next_cursor` and `has_more` agree with the rows, for a member added midway.
- **`TestChatMessagesAreNotAudited`** — send, edit, delete each leave `audit_events` unchanged.
- **`TestDefaultConversationHasNoHistoryBranch`** — no `kind == "default"` in a read path.
- **`TestDocumentsImplementsBlobSink`** — written before the move.
- **The hub's two maps** — connect/disconnect the same user fifty times, assert `byUser` empties.
- **The gap check terminates** — a member added to a busy conversation refetches exactly once.
- **The move's fault-injection matrix** — four injection points, four asserted states, four successful re-runs.
- **The soft delete blanks the body** — asserted through the API **and** against `chat_messages_fts` directly.
- **`08003` up and down** on a restored copy of production.
- **The completeness test** green on the shipped schema with twenty-five shadow rows, **and verified to fail** on a throwaway table.

### 13.3 What a green suite still does not prove

⚠ **Click through it with a second member's session.** §V9-12 records six frontend bugs no test caught and one backend bug found only by opening a page. The v10 equivalents are: a conversation you are not in appearing in a list, a thread that reflows as images load, a badge that does not clear, and a moved file the other members cannot open.

---

## 14. Audit and security

Eleven structural actions, eleven Czech phrases in `admin/labels.go`, covered by `TestActionLabelsCoverEveryAction`:

```
chat.conversation.created   .renamed   .deleted   .restored   .purged
chat.member.added           .removed
chat.attachment.uploaded    .removed   .moved
chat.settings.updated
```

⚠ **Attachments are audited although the messages carrying them are not.** This looks inconsistent and is not: the bytes are what the thresholds, the clean-up page and the storage register exist for, and *"who uploaded that 40 MB video, and when"* is the question the whole storage half answers. `chat.attachment.uploaded` carries the filename and the conversation name.

The Log is admin-only, so structural events go unredacted. **`audit.Redact` is not extended by v10** — chat writes nothing that needs it.

`forbiddenImports["chat"]` bans `platform/metrics` and `platform/lists`, with the reasons recorded in the test as v7 and v8 do. Chat takes `audit`, `blobstore`, `push`, `scheduler`, `storage` and `ws`.

---

## 15. Definition of done

- [ ] Three PRs, each green on its own; nothing in PR 1 mentions chat.
- [ ] `backend/openapi.yaml` at **0.12.0** in PR 2, matching the spec copy.
- [ ] Every leak-table row has an adversarial test.
- [ ] The move's fault-injection matrix passes at all four injection points.
- [ ] `08003` verified up **and down** on restored production data.
- [ ] `arch` green: no cross-module imports, chat's two bans enforced, the completeness test green on the schema and verified red on a throwaway table.
- [ ] `platform/widgets/registry.tsx` **absent from the diff**.
- [ ] Clicked through by a **second member's session**, on a phone, at 375 px, in both themes.
- [ ] Playwright/axe at 375 and 1440 in both themes. ⚠ Outstanding since v5, and v10 adds the densest screen in the app.

---

## 16. Known limitations, taken knowingly

- **An edited message leaves no record of what it said before; a deleted one leaves nothing beyond its tombstone** (D231). If it ever bites, the fix is **not** full auditing — it is `chat.message.edited` and `chat.message.deleted` only, content-free: a handful of rows a month, neither of D231's two costs. Priced and declined.
- **A `reader` can fill storage they can never clean** (D241).
- **A move publishes to the household** (D245). The alternative is a third `visibility`, which is a version of its own.
- **Any member can trash a conversation**; the koš makes it recoverable, not impossible.
- **A removed-and-re-added member has a permanent gap** (D218).
- **No transcoding**: a `.mov` may not play.
- **Chat blobs are not backed up** (D229).
- **`HOME_STORAGE_WARN_TOTAL_MB` remains an env var** while chat's two thresholds are DB rows (D236). Two mechanisms; v11's to reconcile.
- **`HOME_LOG_RETENTION_DAYS` is `0` — the Log has never pruned.** Pre-existing, not v10's doing, and worth a decision one day.

---

## 17. Module packaging

`internal/modules/chat/` — `module.go` `http.go` `service.go` `store.go` `types.go` `scope.go` `messages.go` `attachments.go` `upload.go` `cleanup.go` `move.go` `search.go` `storage.go` `push.go` `drain.go` `migrations/12001_chat.sql`.

Frontend in `src/modules/chat/` — **properly, from the start**. v7 and v8 did; v6's finance and v3/v4's notes and documents are still in the legacy `src/routes/` placement and relocating them has been open since v6. Do not add to that.
