# v10 brief — Chat (`chat` · `admin` · `platform/ws` · `platform/storage`)

> **Scope frozen 2026-08-26.** Nineteen questions asked and answered, then **four decisions deliberately re-opened before the PRD** — §9 records what changed and why. This is the brief that `PRD.md` §V10, `openapi.yaml` **0.12.0**, `HANDOFF-12-chat.md` and the `HANDOFF-design.md` **§v10** addendum will be written against. Where a later document contradicts this one, the later document wins and says so.
>
> **v10 adds the eleventh module — and it is the first one that is not readable by the household.** Every module from v1 to v8 published data every member could see; v9 introduced a second axis, *ownership*, and made a private item invisible to everyone but its owner. v10 introduces a **third**: *membership*. A conversation is readable by the people in it, which is neither "everyone" nor "one person", and almost every platform surface built so far assumes it is one of those two.
>
> ⚠ **The single largest piece of work in v10 is not the chat.** It is `platform/ws`. The hub broadcasts every message to every connected client and keeps **no identity per connection at all** — `ws.client` has a socket, a send channel and a cancel func, and nothing else. v9 worked around this by having private mutations publish `{"private":"1"}` with the id dropped (§V9-12, D190). That workaround does not scale to a module whose entire payload is the thing that must not leak. The hub grows an identity in v10, and every other module keeps its existing broadcast unchanged.
>
> ⚠ **The second largest is not the chat either.** It is the move to Dokumenty. `chat` may not import `documents` — the import-lint forbids it — so "move this file into Dokumenty and keep it visible in the thread" is a **custody transfer across a catalog**, spanning two modules, two SQLite writes and two object-store operations, with no transaction that covers all four. §7.3 is the ordering, and it is the part of v10 most likely to be got wrong quietly.
>
> ⚠ **§5's leak table has twenty-three rows. Treat that as a floor.** v9's table grew from eighteen to twenty-three under review and the build then found two more that no review had listed — background jobs with no actor. The equivalent blind spot here is anything that reads a message without an actor in hand.

---

## 1. What Karel asked for

In his words, condensed:

1. The next module is **chat**. There is always a **default group chat for all members**; new group chats can be **created, named and managed**.
2. **Any image, video or file can be added to chats.** That will add up quite a lot in R2.
3. In **Administrace → Správa úložiště**, special settings for the storage taken by chat documents, including an **MB threshold for a chat storage warning**.
4. The warning is visible **in Administrace and in the chat module** when chat storage exceeds the threshold.
5. Clicking the warning opens a **special chat storage clean-up page**.
6. On that page each image / video / document can be **deleted** (the thread then shows a message that the document was removed by clean-up), **moved to Dokumenty** (still visible inside the chats, no longer counted against the chat threshold, gone from the clean-up page on the next reload), or **left as is** — not every document has to be dealt with at that moment.

Everything below is the resolution of what those six sentences leave open.

---

## 2. The questions, and the answers

| # | Question | Answer |
|---|---|---|
| 1 | Are group chats readable only by their members? | **Members only — real access control.** A non-member gets **404**, admins included. v9's rule, moved from ownership to membership. |
| 2 | How do new messages reach open browsers? | **Scope the hub.** `platform/ws` learns who each connection belongs to; chat publishes to member connections only, payload included. |
| 3 | Where does a moved attachment land in Dokumenty? | **The mover picks the folder** in the move dialog. |
| 4 | What does chat do with attachments? | **Images inline with thumbnails · video inline, no transcoding · PDFs rendered natively by the browser · everything else download-only.** No gotenberg, no derived preview, no `platform/preview` refactor. |
| 5 | Who sees what on the clean-up page? | **Scoped to conversations you belong to.** Nobody triages blind, and nobody sees a chat they are not in. |
| 6 | What does the MB threshold measure? | **Two thresholds** — one for chat in total, one applied to every conversation. Both raise warnings. **Warn only; nothing is ever blocked.** |
| 7 | Which chat behaviours are in scope? | **Edit and delete your own messages · unread badge per conversation · reply to a message · full-text search across your chats.** |
| 8 | Push on a new message? | **Yes, with sender and message preview.** |
| 9 | Who creates conversations and manages membership? | **Everyone posts and creates; any member manages.** No role gate inside chat — a reader can start a group and add people. |
| 10 | What does a newly added member see? | **Only messages from the moment they joined** — ⚠ **amended, see 20.** |
| 11 | Per-file upload cap? | **Reuse Dokumenty's 50 MB.** No new env var. |
| 12 | What does Administrace show for chats the admin is not in? | **Names and sizes — and no way in.** |
| 13 | Previews for chat files? | **Images and video inline, PDFs native, nothing else.** |
| 14 | The default chat for a first-seen member? | ⚠ **Amended, see 20.** |
| 15 | Can a conversation be deleted? | **Yes, cascading everything** — ⚠ **amended, see 21: a 7-day koš first.** |
| 16 | Naming and the dashboard? | **"Chat", route `/chat`. No widget** — the nav badge carries the signal. |
| 17 | Per-chat threshold: one number or per conversation? | **One number, applied to every conversation.** |
| 18 | How much of a chat shows up in the Log? | **Structural events only.** Individual messages are never audited. |
| 19 | Retention? | **Never — chat is permanent.** |
| 19a | Default conversation name? | **"Všichni"** — renameable, never deletable. |
| 19b | Threshold defaults? | **512 MB total, 128 MB per conversation.** |
| 19c | Clean-up gate — membership, or membership *and* editor? | **Member AND editor/admin.** Both conditions. |
| **20** | **Re-opened: the floor on Všichni** | **A column, not a branch.** `effective_from` — Všichni is seeded with the conversation's own creation time (full history), a group add writes `now()`. **No `kind='default'` special case anywhere in the code.** |
| **21** | **Re-opened: any member can destroy a conversation** | **Kept — any member — but delete is recoverable for 7 days.** A koš, then a hard purge. |
| **22** | **Re-opened: messages are not audited** | **Reversed, then reverted — structural events only, final.** Auditing every message content-free was adopted for one round and withdrawn once its cost was priced: it makes the Log a traffic-analysis record and grows the audit spine by one row per message with nothing pruning it. §9.3 keeps the trail. |
| **23** | **Re-opened: the move publishes to the household** | **Kept as is.** The move publishes and the dialog says so in words. |

Four of these deserve their reasoning written down, because all four will look wrong to someone reading only the outcome.

**Q19c — the clean-up gate is an intersection.** Karel's first message said the page was for "any editor or admin user"; his later answer scoped it to conversations you belong to. Both survive: **member AND (editor OR admin)**. The consequence is asymmetric and needs saying out loud: **a reader can upload files into a chat and can never clean them up.** An editor in the same conversation has to do it for them, and if a conversation happens to contain only readers, its attachments can only be removed by deleting the messages that carry them. This follows directly from Q9's "any member manages" applying to *membership* but not to *storage*, which is defensible — filling storage and emptying it are different powers — but it is the one place in v10 where two of Karel's answers pull in opposite directions.

**Q23 — the move publishes, and that was re-confirmed rather than assumed.** Moving an attachment into shared Dokumenty makes it readable by the whole household, including people who are not in that conversation. Three alternatives were put and all three declined: restricting the move to Všichni attachments (no leak, but the heaviest files live in the small groups and those would become delete-or-keep); dropping the move entirely; and building a third `visibility` — conversation-scoped documents — which re-walks v9's entire leak table for a third value and makes `documents` ask `chat` about membership. **Karel kept the leak and bought the cleaning power.** The dialog carries the sentence; §7.4 has the wording.

**Q3 + Q4 — no preview pipeline, on purpose.** Karel first asked for Dokumenty-style previews, then chose against them once the cost surfaced: the preview worker lives inside `documents` and the import-lint forbids `chat` from reaching it, so previews would mean extracting `platform/preview` — a refactor of a live module inside a version that is already the largest since v5. PDFs open in the browser's own viewer, which covers the common case at zero cost, and the answer to "I want to read this properly" is the move to Dokumenty, which is the action the clean-up page is built around anyway.

**Q11 — one upload cap, and the reason is not thrift.** Chat reads `HOME_DOCS_MAX_UPLOAD_MB` rather than owning a cap of its own. **An attachment larger than Dokumenty's cap could never be moved into Dokumenty**, so the clean-up page's headline action would fail on precisely the files heavy enough to have caused the overrun. One cap keeps every attachment movable by construction, and 50 MB against a 128 MB per-conversation threshold means no single file can eat a conversation's whole budget.

---

## 3. The model

### 3.1 A conversation is a room with a floor

Four tables in migration block **12** (`12001_chat.sql`), plus one FTS index and one queue table.

```
chat_conversations   id · kind('default'|'group') · name · created_by
                     · created_at · updated_at · deleted_at · deleted_by
chat_members         (conversation_id, user_id) PK · effective_from · added_by
                     · muted · last_read_at
chat_messages        seq INTEGER PK · id UNIQUE · conversation_id · author_id · body
                     · reply_to_id · created_at · edited_at · deleted_at
chat_attachments     id · message_id · conversation_id · kind('image'|'video'|'file')
                     · original_filename · content_type · byte_size · checksum
                     · storage_key · thumbnail_key · width · height
                     · state('live'|'moved'|'removed') · document_id
                     · uploaded_by · created_at · cleaned_by · cleaned_at
chat_messages_fts    external-content FTS5 over chat_messages(body), content_rowid = seq
chat_deleted_keys    key TEXT PK · queued_at · purge_after      -- the drain, §7.5
```

**D216** — the module id is `chat`, the Czech UI name is **"Chat"**, the route is `/chat`, the migration block is **12**, the contract goes to **OpenAPI 0.12.0**, and the decisions are **D216–D258**.

**D217 — membership is the access boundary, and a non-member gets 404, never 403.** This is v9's rule (§V9-2) with ownership swapped for membership, and it applies on every conversation-scoped route including `HEAD`, including the attachment bytes, including a `304` (see leak row 5). A 403 tells you the conversation exists; 404 tells you nothing.

**D218 — `chat_members.effective_from` is a read floor, not a display filter.** It is pushed into the SQL of the thread query, the search query, the attachment listing, the clean-up listing, the unread count and the reply-quote resolver. A floor applied after the rows are fetched is not a floor — it is a place where a snippet, a rank, a count or a `Link` header still carries what it filtered out.

⚠ **The column is called `effective_from`, not `joined_at`, and the difference is the whole of D258.** It is not "when you joined"; it is "the instant from which this conversation is yours to read". For a group add they are the same value. For Všichni they are not.

⚠ **Removing a member deletes the row.** Re-adding writes a new one with a new `effective_from`, so a removed-and-re-added member has a **permanent gap** in the middle of a conversation they can otherwise read in full. This is a consequence of D218, not a bug, and the members screen says so before the removal is confirmed.

**D219 — the default conversation is "Všichni".** `kind='default'`, created by the migration, exactly one row (a partial unique index on `kind` WHERE `kind='default'`). Every member `home` has ever seen a session for is auto-joined **at first sight** — the first request that resolves them to an actor, not at boot, because the member directory is projected from `sessions` and a member who has never logged in does not exist yet (§3.4). It is **renameable** and **never deletable, never leaveable**; both refusals are 422 with a reason, not a hidden button.

**D258 — Všichni's membership rows are seeded with the conversation's own `created_at`, and no code path branches on `kind='default'` for history.** ⚠ **This is the re-opened decision (§9.1) and it is one line of insert, not an exception.** A member first seen in 2028 gets `effective_from` = the day Chat shipped, and therefore the whole household history — because nobody *decided* to add them to Všichni, so there is no retroactive disclosure for the floor to prevent. A group add is somebody's decision about somebody else's history, and that is what the floor exists for.

A test asserts it: `TestDefaultConversationHasNoHistoryBranch` greps the module for `kind == "default"` outside the create/delete/leave guards. The value is data; the moment it becomes a `CASE`, it becomes an exception somebody will get wrong.

**D220 — conversations are addressed by id. There is no slug.** The house pattern is `freeSlug`, which loops on a sibling collision and appends `-2`, `-3`… (§V9 slugs). Applied here it becomes an oracle: creating a conversation called *Dovolená* and being handed *dovolena-2* tells you there is already a *Dovolená* you cannot see. UUIDv7 in the URL, no collision, no oracle, no `soukrome`-style reserved-word backfill to write.

**D221 — there are no system messages.** Nobody is announced into or out of a conversation in the thread. Adds, removes and renames are audit events (D231) and the members panel is the live truth. The alternative — a `kind='system'` row — was rejected because it would be the one message type with no author, no edit path, no delete path and no floor semantics, i.e. four special cases in every query, to say something a panel already says.

**D222 — no role gate inside chat.** Any authenticated member may post, may create a conversation, and — as a member of it — may rename it, add members, remove members and delete it. Readers included. The **only** role gate in the whole module is the clean-up page (D241).

### 3.2 Deletion, and the koš

**D223 — nothing in chat is destroyed by the request that asks for it.** A **message** is soft-deleted, leaving a tombstone that reads *"Zpráva byla smazána"*. A **conversation** is soft-deleted into a koš and hard-purged after **seven days**.

⚠ **This is the re-opened decision (§9.2).** The first pass had conversation deletion cascade immediately, which meant any member — a reader included — could permanently destroy every other member's files with one confirmation. Karel kept "any member may delete" and made it recoverable instead, which is the better trade: the power stays symmetric with the rest of the module, and the irreversibility goes away.

**D253 — the koš.** `chat_conversations.deleted_at` + `deleted_by`; retention `HOME_CHAT_TRASH_DAYS`, default **7**. While a conversation is in the koš:

- it disappears from every member's conversation list, thread, search, unread count and clean-up listing — a soft-deleted conversation is invisible to reads exactly as if it were gone;
- it appears in a **Koš** section of the chat conversation list for its members, showing name, size and days remaining, with **Obnovit**;
- its `chat_deleted_keys` rows carry `purge_after = deleted_at + 7 days`, so the drain (§7.5) does not touch the bytes until then;
- restoring clears `deleted_at` and the queued keys in one transaction. Membership, messages and attachments were never touched, so a restore is genuinely the state before the delete — not a reconstruction.

**D254 — ⚠ a conversation in the koš still counts toward both thresholds, and *Smazat natrvalo* exists so that can never trap anyone.** The bytes are still in R2; reporting them as freed would make the storage page lie for a week, and the storage page's entire premise is that its figures sum. But a member who deletes a 200 MB conversation *to fix an overrun* must not be told to come back in seven days — so the koš row carries an immediate purge, available to the deleter and to admins, with its own typed confirmation. **Deletion is recoverable by default and irreversible on request.**

The admin's conversation table (D240) lists trashed conversations flagged *v koši* with their days remaining, for the same reason: an admin chasing an overrun needs to know that 200 MB is spoken for but not yet gone.

**D255 — an admin may restore or purge a conversation they may never read.** v9's asymmetry, repeated exactly: *"an admin may hard-delete a foreign private item and may never read one."* Restore and purge are the only two chat verbs an admin has over a conversation they are not in, and neither of them opens it.

**D224** — a message carries a body, zero or more attachments and an optional `reply_to_id`. A `CHECK` refuses a row that has neither body nor attachment. Body cap: **8 000 characters** (well past any real message, short of a pasted document).

**D225** — an edit sets `edited_at` and the bubble shows *upraveno*. **No edit history is kept anywhere.** There is exactly one place in Home that keeps before-and-after values — the audit spine — and D231 keeps message text out of it entirely by not writing message events at all. ⚠ **An edited message therefore leaves no record of what it said before, and a deleted one leaves nothing beyond its tombstone.** That is the price of D231 and it is paid knowingly.

**D226 — a reply quote is resolved through the floor.** A message can reply to a message written before your `effective_from`. The quoted parent is loaded with the same membership-and-floor predicate as everything else; below your floor it renders as *"Zpráva mimo vaši historii"* with no author, no date and no excerpt. ⚠ **This is the leak the floor almost misses**, because the quote is denormalised-looking data that feels like part of the child message rather than a read of the parent.

⚠ **Soft-delete BLANKS the body in place.** `deleted_at IS NOT NULL` is not enough: `chat_messages_fts` is external-content, so a body left in the table is a body still returned by search. The delete sets `body = ''` in the same statement and the FTS row goes with it. The same applies to `original_filename` on a message-delete that takes attachments with it.

### 3.3 Attachments

**D227** — three kinds, decided by **server-sniffed MIME** and never the client's claim (D48's rule, unchanged):

| kind | types | stored | rendered |
|---|---|---|---|
| `image` | png · jpeg · gif · webp | original + `thumb.webp` | inline, dimensions recorded so the thread does not reflow |
| `video` | mp4 · webm · quicktime | original only | inline `<video>` with byte-range support, **no transcoding, no poster frame** |
| `file` | everything else | original only | icon · filename · size · download. **PDF opens in the browser's own viewer** |

⚠ **An iPhone `.mov` (HEVC) will store and fail to play in some browsers.** There is no transcoding in v10 and no plan for one — the fallback is the download button, and the failed-playback state says so rather than showing a broken player.

**D228 — chat reuses Dokumenty's per-file cap**, `HOME_DOCS_MAX_UPLOAD_MB`, default **50 MB**. No new env var, for the reason in §2's Q11 note. ⚠ Raising it for Dokumenty raises it for chat, and the config comment says so.

**D229** — keys are `chat/{attachment_id}/original` and `chat/{attachment_id}/thumb.webp` in the same primary bucket, under a `chat/` prefix so `platform/storage`'s per-module attribution works unchanged. **Chat blobs are not mirrored** to the backup bucket: they are the most disposable bytes in the application, the module exists under a storage warning, and doubling them into the mirror would be the one place in Home where a warning threshold is undermined by a background job. The Úložiště backup line measures module prefixes (§V9-12, D205), so chat simply does not appear there — and the storage page says *nezálohováno* against chat rather than leaving a gap that reads as zero.

**Cache-Control on chat bytes is `private, no-cache, must-revalidate`, with an ETag — never `immutable`.** v9 gave shared items a year-long `immutable` header and private items a revalidating one; a chat attachment is neither. **Membership can be revoked**, and a year-long cache entry is a copy that outlives the revocation on a device nobody can reach. Same header as v9's private branch, different reason.

### 3.4 There is no users table, and chat needs one

⚠ The member directory in Home is **projected from `sessions`** by `push.Store.Members()` — "everyone home has ever seen a session for", newest identity per user. There is no `users` table and never has been.

For chat this stops being a curiosity:

- **You cannot add someone who has never logged in.** The member picker offers the directory, and the directory is a login history.
- **A member's display name is whatever their newest session carried.** A rename in the auth service reaches chat on their next login, not before.
- **Auto-joining Všichni happens at first sight** (D219), not at account creation, because account creation is not an event this service can observe.

**D230** — chat takes the same narrow `MemberDirectory` interface `admin` already uses (`Members(ctx) ([]push.Member, error)`), satisfied by `*push.Store` at composition. No new table, no new dependency, and the same staleness as everywhere else. ⚠ The picker renders **display names only** — never emails, never roles. `push.Member` carries all three and the member picker is the first surface in Home that shows the directory to a non-admin.

A proper `platform/members` strand is v11's if anyone wants it; it is not v10's to invent.

---

## 4. The Log, and the invariant v10 breaks

**D231 — messages are not audited. Sending, editing and deleting a message write no audit event.** Only structural events do:

```
chat.conversation.created   chat.conversation.renamed   chat.conversation.deleted
chat.conversation.restored  chat.conversation.purged
chat.member.added           chat.member.removed
chat.attachment.uploaded    chat.attachment.removed     chat.attachment.moved
chat.settings.updated
```

Eleven actions, eleven Czech labels in `admin/labels.go`, guarded by the existing `TestActionLabelsCoverEveryAction`.

⚠ **This decision was re-opened, reversed, and reverted — see §9.3 for the full trail, because the reasoning matters more than the outcome.** For one round v10 audited every message content-free, which restored the house invariant. It was withdrawn once the cost was priced: it makes the Log a traffic-analysis record of the household, and it grows `audit_events` and `audit_events_fts` by one row per message with **`HOME_LOG_RETENTION_DAYS` defaulting to `0` — keep forever**. Structural-only is final.

**Chat is therefore the first module in Home whose primary mutation writes no audit event.** Every handoff since v1 states the invariant — *every mutation writes an audit event in the same transaction* — and v10 breaks it deliberately, for the one module where the audit row would carry nothing but metadata and there would be one per message.

**D256 — the breach is a test, not a comment.** `TestChatMessagesAreNotAudited` asserts that a send, an edit and a delete each leave `audit_events` unchanged. An invariant relaxed without a test is an invariant that comes back by accident, and "the missing audit coverage" is exactly what a later reader will try to fix.

⚠ **The cost, stated plainly: a deleted message leaves no trace beyond its tombstone, and an edited one leaves none at all.** There is no answer to *who changed this, and what did it say before*. If that ever bites, the smallest honest fix is not full auditing — it is **`chat.message.edited` and `chat.message.deleted` only, content-free**, which closes the forensic gap for a handful of rows a month rather than one per message. It was offered, and declined in favour of a clean line. §9.5 keeps it on the list.

**D257 — WITHDRAWN.** *(It recorded the Log as a traffic-analysis record, which only followed from the message-auditing version. Recorded as withdrawn rather than reused, the way §V9-12 records D214 as declined — a gap in the numbering is cheaper than a number that means two things.)*

**Attachments are audited even though the messages carrying them are not.** ⚠ This looks inconsistent and is not: the bytes are the thing the clean-up page, the thresholds and the storage register all care about, and *"who uploaded that 40 MB video, and when"* is the question the whole storage half of v10 exists to answer. `chat.attachment.uploaded` carries the **filename** and the conversation name.

The Log is **admin-only** (D5), so structural events go **unredacted**. An admin sees conversation names, who created them, who was added — and attachment filenames — for conversations they may never open. That is consistent with D240, which already shows an admin every conversation's name and size. ⚠ v9's redaction machinery (`audit.Redact` / `RedactRendered`) is therefore **not extended by v10**; chat writes nothing that needs it.

⚠ **A trigger rule on the `chat.` prefix is possible and harmless**, because the only events that exist are structural. An admin who writes one gets *"Karel vytvořil konverzaci Dovolená"* pushed to the household, which is exactly what the event says.

---

## 5. Where membership can leak — the list, which is not "the whole list"

| # | Surface | The leak | Closed by |
|---|---|---|---|
| 1 | `GET /api/chat/conversations/{id}` | reading a conversation you are not in | D217 — membership predicate, 404 |
| 2 | The thread query | messages before your `effective_from` | D218 — floor in SQL |
| 3 | **Search (FTS5)** | an unfiltered `MATCH` returns every household message, with a snippet | D218 + D251 — membership **and** floor joined inside the query, before rank and snippet |
| 4 | **The reply quote** | quoting a parent below your floor | D226 |
| 5 | **`If-None-Match` on attachment bytes** | a stale ETag earns a non-member a `304` — *"yes, and it hasn't changed"* — for something they may not read | the membership load moves **before** the conditional branch. ⚠ v9 found this exact bug in documents; it is repeated here because the shape repeats |
| 6 | `Cache-Control` on attachment bytes | a year-long `immutable` copy outliving a revoked membership | §3.3 — `no-cache, must-revalidate`, always |
| 7 | **`/ws` fan-out** | the hub broadcasts to every connected client | D232 — the scoped hub, §6 |
| 8 | The push payload | carries sender and message text | **accepted by design** (D249). Mitigated only by targeting: members minus author, mute honoured |
| 9 | `notification_deliveries` | an admin-visible log of what was pushed | ✅ already closed — the table stores kind, category, user, status, error and **no title or body**. Verified in `08001_admin_notifications.sql` |
| 10 | Unread counts | a count for a conversation you were removed from | the badge query joins `chat_members`; a removed member has no row |
| 11 | The nav badge | a total is not a name | ✅ by construction — one integer, no per-conversation breakdown outside your own list |
| 12 | **The Log — structural events** | conversation names, who created them, who was added, **and attachment filenames** — for conversations the admin may never open | **accepted by design** — admin-only (D5), consistent with D240. ⚠ **Message metadata is NOT here**: D231 writes no message events, so the Log says nothing about who talked to whom or when |
| 13 | The Log's entity timeline | an exact-id lookup on a `chat_conversation` entity | ✅ admin-only; and §V9-12's D209 already made the timeline an exact-id match rather than a lexical one |
| 14 | Administrace → Úložiště | conversation names, sizes, member counts, koš state | **accepted by design** (D240, D254). No way in: no thread, no attachment list, no clean-up |
| 15 | The clean-up page | attachments from conversations you are not in | D241 — member ∧ editor/admin, and the floor applies to the listing too |
| 16 | **Move to Dokumenty** | a members-only file becomes household-readable | **accepted by design** (D245), re-confirmed against three alternatives — and the dialog says so in words before it happens |
| 17 | The move dialog's folder picker | a private v9 folder as a target would make the file unreadable to the other members | D245 — **shared folders only**, 422 on a private target |
| 18 | **The koš** | a trashed conversation still readable through a stale id | D253 — soft-delete is invisible to **every** read; the Koš section is a separate listing, members only |
| 19 | The member picker | emails and roles from `push.Member`, shown to a non-admin for the first time | D230 — **display names only** |
| 20 | PWA persisted cache | message bodies and other members' names on a shared laptop's disk | **chat is excluded from the persister entirely** — v9 excluded one listing; v10 excludes a module |
| 21 | `chat_deleted_keys` | queued object keys | ✅ keys are `chat/{uuid}/original` — no names, no filenames |
| 22 | Membership revoked mid-session | an open tab keeps rendering what it already fetched | **accepted, bounded** — the hub resolves membership at publish time so nothing new arrives, and the next request 404s. No socket is force-closed |
| 23 | Message ids are UUIDv7 | a leaked id carries its own timestamp | **accepted** — the same property every id in Home has had since v1 |

⚠ **Treat twenty-three as a floor.** v9's table was reviewed to twenty-three and the build still found two the review had not: the preview worker and the image GC, **background jobs that have no actor and therefore no membership**. v10's equivalents are the orphan drain (§7.5), the koš purge and thumbnail generation — all three run without a request, all three touch attachment rows, and all three must load with an explicit *any-membership* variant rather than accidentally inheriting a viewer-scoped one.

---

## 6. The scoped hub — `platform/ws` grows an identity

Today: `Hub.Publish(m Message)` marshals once and writes to every client. `client` is `{conn, send, cancel}`. The handler authenticates the upgrade from the session cookie, **discards the resulting actor**, and adds an anonymous client to a `map[*client]struct{}`.

**D232 — the hub learns who is connected.**

```go
type client struct { conn *websocket.Conn; send chan []byte; cancel context.CancelFunc; userID string }

type Hub struct {
    mu      sync.Mutex
    clients map[*client]struct{}            // unchanged — broadcast still walks this
    byUser  map[string]map[*client]struct{} // v10
}

func (h *Hub) Publish(m Message)                     // unchanged, every module keeps working
func (h *Hub) PublishTo(userIDs []string, m Message)  // v10 — marshals once, fans out to the union
```

- `Config.Authenticate` already returns `(reqctx.Actor, bool)` and the handler already calls it. v10 keeps the actor instead of throwing it away.
- One user, many clients: phone, laptop, two tabs. `byUser` is a set per id, and `remove` must clean **both** maps or the hub leaks a growing set of dead sockets per user — the kind of leak that shows up as memory, months later.
- **The dev bypass has no actor.** Under `HOME_DEV_AUTH_BYPASS` the client registers under `Config.BypassActor.UserID`; with no bypass actor configured the connection is anonymous and `PublishTo` never reaches it. Refused in production anyway.

**D233 — chat resolves the member list at publish time and sends the message body over the socket.** Not an id-and-refetch: the round trip is what makes a chat feel slow, and the hub now knows exactly who may have it. Membership is read inside the same transaction that wrote the message, so a member removed a second earlier does not receive it.

**D234 — v9's D190 workaround is left alone.** Private notes and documents still publish `{"private":"1"}` with the id dropped to everybody. With `PublishTo` in place that becomes unnecessary, and un-necessary is not the same as wrong: rewriting v9's fan-out is a change to a live privacy path for a latency benefit nobody has asked for. ⚠ **v11 may take it. v10 does not.** The leak-table row that D190 closed stays closed either way.

---

## 7. Storage, the clean-up page, and the move

### 7.1 Registration

Chat registers with `platform/storage` — the fourth catalog, v9's (D191) — as:

- **`Source`** — `StorageTables()` returns the four tables, `chat_messages_fts`, and its five FTS shadows via `storage.FTSShadows("chat_messages_fts")`, plus `chat_deleted_keys`. ⚠ **`chat_messages_fts` is the FIFTH external-content FTS5 index in Home, which makes twenty-five shadow rows, not twenty.** §V9-12 records that `garden_plants_fts` went uncounted for two versions; the completeness test (`arch/storage_completeness_test.go`) is what stops it happening a third time, and it fails loudly on an unclaimed table.
- **`BlobSource`** — `StorageBlobs()` walks the `chat/` prefix and attributes each object to its **uploader**, with kind `shared`. ⚠ **The word is wrong and it is kept anyway.** In `platform/storage`, `shared` means "not a v9 private item"; a chat attachment is member-restricted, which is a third thing. Introducing a fourth `Kind` would change a wire enum and every consumer of it for a distinction the storage page does not make. The `BlobUsage` comment says so explicitly.
- **`GroupSource`** — new in v10, and generic:

```go
// GroupSource is a module reporting NAMED SUB-BUCKETS of its own storage.
type GroupSource interface { StorageGroups(ctx context.Context) ([]GroupUsage, error) }

type GroupUsage struct {
    ID, Name  string
    Members   int
    Objects   int64
    Bytes     int64
    TrashedAt string // "" unless in a koš — see D254
}
```

**D235** — the interface is named for what it is (a module's own named partitions), not for chat's word for it. `documents` could report per-root and `garden` per-bed without a new interface. The threshold comparison lives in the **consumer**, not the source: chat reports bytes, Administrace and the chat module each apply the threshold they read.

### 7.2 Two thresholds, DB-backed

**D236** — one platform-owned table, in the platform migration block:

```sql
CREATE TABLE storage_thresholds (
    key        TEXT PRIMARY KEY,   -- 'chat.total' | 'chat.conversation'
    value_mb   INTEGER NOT NULL CHECK (value_mb > 0),
    updated_at TEXT NOT NULL,
    updated_by TEXT
);
```

It is platform-owned because **admin writes it and chat reads it**, and neither may import the other. Keyed rather than columned so v11 adds a threshold with an INSERT, not a migration. Claimed by `PlatformTables` in the completeness test.

**D237 — defaults are 512 MB total and 128 MB per conversation**, seeded by the migration. Both are **warn-only**: no upload is refused, there is no quota, there is no new 413. This is v9's D196 rule, unchanged — a register that blocks things stops being a register.

⚠ **v9's `HOME_STORAGE_WARN_TOTAL_MB` stays an env var.** Home now has two threshold mechanisms: one in Coolify for the whole application, two in the database for chat. That is an inconsistency, it is recorded rather than fixed, and the reason is that migrating a live operator setting into a table is a separate change with its own failure mode. **v11's to tidy.**

### 7.3 The custody transfer — the hard part

`chat` may not import `documents`. The move is therefore brokered through the catalog, exactly as `admin` reaches ten modules without importing one:

```go
// BlobSink — a module that will ACCEPT custody of another module's object.
type BlobSink interface {
    AcceptBlob(ctx context.Context, req AcceptRequest) (AcceptResult, error)
}

type AcceptRequest struct {
    SourceModule string   // "chat"
    SourceKey    string   // chat/{attachment_id}/original
    Filename     string
    ContentType  string
    ByteSize     int64
    Checksum     string   // sha-256 hex, already computed at upload
    FolderID     string   // the picked Dokumenty folder — SHARED only (D245)
    ActorID      string
}

type AcceptResult struct {
    DocumentID string
    PublicPath string   // /api/documents/{id}/raw — what the bubble renders
}
```

**D238 — the ordering, and it is not negotiable:**

1. **Validate** the target folder is shared and the actor may write to it. *(A private target is a 422 — see D245.)*
2. **Copy** `chat/{aid}/original` → `documents/{did}/original` via `blobstore.Copy`. Same account, so R2 does it server-side; nothing streams through the app.
3. **Insert** the `documents` row in its own transaction, with its own `document.created` audit event and `meta.via: "chat"`.
4. **Update** the `chat_attachments` row in its own transaction: `state='moved'`, `document_id`, `cleaned_by`, `cleaned_at`, and `chat.attachment.moved` audited.
5. **Delete** the chat object — **last**, and enqueued into `chat_deleted_keys` if it fails.

⚠ **There is no transaction covering all five, and there cannot be** — two SQLite writes and two object-store calls. The ordering is chosen so that every crash point leaves a state that is *over*-counted rather than *lost*:

| crash after | state | consequence |
|---|---|---|
| 2 | bytes in both places, no document row | an unattributed object under `documents/` — reported by v9's machinery, never auto-cleaned. Re-running the move is safe (`idx_documents_checksum` finds it) |
| 3 | document exists, attachment still `live` | the file is in Dokumenty **and** still counted against chat. Visible, fixable, re-runnable |
| 4 | attachment `moved`, chat object still present | chat still counts bytes it no longer owns — the drain (§7.5) removes it on its next pass |

**Never** delete before the copy is confirmed, and never mark `moved` before the document row exists. Both inversions lose the file.

**D239 — with no sink configured, the move action does not exist.** `AcceptBlob` is an optional dependency assembled in `bootstrap`. Nil ⇒ the API returns 501 and the UI hides the button. It does **not** silently fall back to delete. ⚠ §V9-12 records `TestRealModulesImplementTheStorageCatalog`, which exists because `StorageBlobs` was implemented on the wrong type, everything compiled, every test passed, and the page reported 0 B — *"it was found by opening the page"*. The equivalent here is `TestDocumentsImplementsBlobSink`, and it is written before the move is.

### 7.4 The clean-up page

**D240 — Administrace shows names and sizes and no way in.** The Úložiště page gains a chat block: total chat bytes against `chat.total`, and a table of every conversation — name, size, object count, member count, over-limit flag, and **koš state with days remaining** (D254). There is no link into a thread, no attachment list and no clean-up button for a conversation the admin is not in. The nudge is social: an admin sees which conversation is heavy and asks its members to clean it. The only two verbs an admin has are restore and purge (D255).

**D241 — the clean-up page is at `/chat/uklid`, gated on member ∧ (editor | admin).** It lists attachments from **your** conversations only, floor applied, koš excluded, grouped by conversation, with conversations over `chat.conversation` flagged. Sort by size or recency. Each row: thumbnail (images), filename, size, uploader, date, conversation.

**D242 — three actions, and "leave as is" is the absence of one.** *Ponechat* is not a button; nothing is staged, nothing is queued, and closing the page is a valid outcome. *Odstranit* and *Přesunout do Dokumentů* both act immediately.

**D243 — the removed placeholder keeps the filename and the size.** The bubble reads:

> 📄 *smlouva-2026.pdf* · 2,4 MB — **Soubor odstraněn při úklidu úložiště** · Karel, 25. 8. 2026

The thread stays legible, a member can ask for the file again knowing exactly what it was, and the clean-up is attributed. `state='removed'`, bytes gone, `byte_size` retained for display and **excluded from every sum** (the sums walk the bucket prefix, so this is by construction, not by a `WHERE`).

**D244 — leaving while still over threshold warns.** Navigating away from `/chat/uklid` with either threshold still exceeded raises a confirmation naming which: *"Chat je stále nad limitem (612 MB / 512 MB). Opravdu odejít?"* ⚠ **A `beforeunload` handler is not enough** — most exits are client-side route changes. It hooks the router's navigation blocker, and `beforeunload` is the secondary guard.

**D245 — a move is a publish, and the dialog says so.** Moving a chat attachment into shared Dokumenty makes it readable by **every household member, including people who are not in that conversation.** This is inherent in Karel's requirement that a moved file stay visible in the thread — the thread's members must be able to read it, and the only place in Home where that is true without a second ACL is the shared tree. Re-confirmed against three alternatives (§2, Q23). The dialog carries the sentence:

> *"Soubor bude viditelný pro všechny členy domácnosti, i pro ty, kteří nejsou v této konverzaci."*

⚠ **A private v9 folder is refused with a 422**, not offered and greyed out. A private target would make the file unreadable to the conversation's other members — the exact opposite of what the move is for.

**D246 — after the move the attachment stays in the thread**, rendered from `AcceptResult.PublicPath`. It **no longer counts** toward either chat threshold (the bytes left the `chat/` prefix), and it is **gone from the clean-up listing on the next reload** (`state='moved'` is excluded from the query). All three are Karel's stated requirements and all three fall out of the key move rather than being maintained separately.

⚠ **Purging the conversation later does not delete the moved document.** The purge takes `chat/` objects; `documents/` objects belong to another module now. The thread's reference dies with the thread; the document does not. Stated in the delete confirmation.

### 7.5 The drain

**D247** — every path that destroys bytes writes to `chat_deleted_keys` with a `purge_after`, and a scheduler job every **15 minutes** drains everything due: batch-delete via `blobstore.Delete(keys...)`, clear the rows, log a count.

- A **message** delete and a **clean-up** delete set `purge_after = now` — nothing to recover, nothing to wait for.
- A **conversation** delete sets `purge_after = deleted_at + HOME_CHAT_TRASH_DAYS` (D253). **Restore deletes the queued rows**; purge-now rewrites them to `now`.

Except one. ⚠ **The clean-up page deletes inline**, then enqueues only on failure — because the entire workflow is *clean until the number goes down*, and a number that lags fifteen minutes behind the button makes the page unusable. One or two objects per click is cheap; a conversation with four hundred attachments is not, which is why conversation purge and message delete enqueue instead of blocking a request on bulk object I/O.

**This is why `chat` takes `platform/scheduler`**, and the drain is its only job. ⚠ A job has **no actor**: its loads must use explicit any-membership variants. See the floor of §5.

---

## 8. Push, unread, search

**D248 — a new message pushes to every member except the author**, honouring the per-member `chat_members.muted` flag and a new `cat_chat` column in `notification_preferences`. Off the request path, as `push.Sender` requires: *"no mutation may be slowed or failed by a push service."*

**D249 — the payload carries the sender's name and a preview of the message**, truncated at 140 characters, deep-linking to `/chat/{id}`. Karel's explicit choice, over the content-free alternative. It is the **one place in v10 where chat text leaves the application**, it appears on a lock screen, and it is leak row 8. Attachment-only messages read *"Karel poslal soubor"*. The `Tag` collapses to one notification per conversation, so twenty messages do not become twenty banners.

⚠ **`notification_deliveries` needs a migration.** Its `kind` and `category` columns are `CHECK`-constrained to `('broadcast','trigger','schedule','test')` and `('broadcast','triggers','summaries')`. SQLite cannot alter a CHECK — this is a **table rebuild** in the admin block (`08003_chat_delivery_kind.sql`), adding `'chat'` to both. Small, operational, non-audit data; and the alternative (recording chat pushes as `trigger`) would let a member silence chat by muting Administrace's trigger rules.

⚠ Two more migrations sit outside chat's own block: `02004` adds `cat_chat` to `notification_preferences` (a plain ADD COLUMN) and creates `storage_thresholds` (D236). Both are **out-of-order goose versions** — numerically below `11001`, which is already applied. v9 shipped exactly this shape with `01002`, `06004` and `07004`, so the runner tolerates it; **verify before writing them, not after.**

**D250 — unread is `chat_members.last_read_at`.** Unread = `created_at > MAX(last_read_at, effective_from)` AND `author_id != me` AND `deleted_at IS NULL`. `POST /api/chat/conversations/{id}/read` with `{"until_message_id": …}` advances it, idempotently and never backwards. The nav badge is the sum across your conversations; the thread shows a *Nové zprávy* divider.

**D251 — search is `chat_messages_fts`, membership and floor pushed into the query.** One `MATCH`, joined to `chat_members` on the actor, `created_at >= effective_from`, `deleted_at IS NULL`, conversation not in the koš, ranked and snippeted **after** those predicates. ⚠ FTS5 ranks over what it matched; filtering the result set afterwards leaves a rank computed over other people's messages, and a snippet is a message body by another name.

**D252 — chat publishes no metric, no list and no widget.** `forbiddenImports["chat"]` bans `platform/metrics` and `platform/lists`, with the reason recorded in the test as v7 and v8 do. ⚠ **`platform/widgets/registry.tsx` must be absent from the v10 diff** — v8's trap: an entry there for a module with no widget provider is a dashboard tile that resolves to nothing, with no compile error and no runtime error. Third version running with that file untouched.

Chat **does** take `platform/audit`, `platform/blobstore`, `platform/push`, `platform/scheduler`, `platform/storage` and `platform/ws`.

---

## 9. Re-opened before the PRD — what changed, and what it cost

All four of the decisions flagged as "most likely to be re-opened" were re-opened on 2026-08-26, before a line of the PRD was written. Three changed.

### 9.1 The floor on Všichni — changed (D258)

**Was:** the join floor applied everywhere, so a member first seen in 2028 opened the household room and found it empty, permanently.

**Now:** the column is `effective_from`, and Všichni seeds it with the conversation's own `created_at`. **The exemption is a value, not a branch** — no `if kind == "default"` anywhere in the read paths, and a test that says so.

The principle survives intact, and it is worth stating because it is *why* this works rather than being a fudge: **the floor exists to stop one person retroactively handing another person's history to somebody.** Being added to a group is exactly that decision. Being auto-joined to Všichni is nobody's decision — it is what the household room means. Different situations, one predicate, two values.

### 9.2 Destroying a conversation — changed (D253, D254, D255)

**Was:** an immediate cascade. Any member, a reader included, could permanently destroy every other member's files with one typed confirmation.

**Now:** a **7-day koš**, then a hard purge. "Any member may delete" survives — the symmetry Karel wanted — and the irreversibility does not.

⚠ **The cost is the one I flagged when offering it, and it is resolved rather than accepted:** trashed bytes keep counting toward both thresholds for the week, because the storage page's figures must sum and the bytes really are still there. **Smazat natrvalo** (D254) is what stops that from trapping somebody mid-clean-up. So: recoverable by default, irreversible on request, honest in the numbers either way.

New env var: `HOME_CHAT_TRASH_DAYS`, default **7**. Chat's only one.

### 9.3 Auditing messages — reversed, then reverted (D231, D256; D257 withdrawn)

**This one moved twice, and the round trip is worth keeping rather than tidying away.**

**Draft 1:** structural events only. Chat becomes the first module in Home whose primary mutation writes no audit event — a deliberate breach of the house invariant, defended by a test that exists to explain the absence.

**Draft 2:** every message audited, content-free. The invariant is intact, there is no breach to explain, and the forensic gap around edits and deletes closes.

**Final: back to draft 1.** Two costs came out of pricing draft 2, and together they were worse than the breach:

1. **The Log becomes a traffic-analysis record of the household.** One row per message means an admin reading the Log sees who talked to whom, in which conversation, how often and at what hour — for conversations they may never open. Not the words. Everything else. For a feature whose entire premise is that a members-only conversation is members-only, that is a strange thing to build into the one screen an admin always has.
2. **Unbounded growth, because the mitigation is switched off.** `logging.Prune` honours `HOME_LOG_RETENTION_DAYS`, and `defaultLogRetentionDays` is **`0` — keep forever**. `audit_events` and `audit_events_fts` would have grown by one row per message with nothing pruning them, and `audit_events_fts` is already "usually the largest thing in the file". Fixing that means choosing a retention value for the *whole* Log — a household decision, forced by a chat feature.

So the invariant stays broken, deliberately and visibly, guarded by `TestChatMessagesAreNotAudited`. **What it costs is real and is not hidden:** an edited message leaves no record of what it said before, and a deleted one leaves nothing beyond its tombstone.

⚠ **The middle was offered and declined, and it stays on the table** (§9.5): auditing **only `chat.message.edited` and `chat.message.deleted`**, content-free. Edits and deletes in a four-person household are a handful of rows a month, not one per message — so it closes the forensic gap without either cost above. It lost to a clean line, which is a defensible preference; it is recorded here because the *reason* for reverting was volume, and this option has none.

**One consequence of the revert worth noting:** `HOME_LOG_RETENTION_DAYS = 0` is still true, and the Log still never prunes. That is now a **pre-existing condition rather than something v10 causes** — worth knowing, no longer urgent. §13.4 says so.

### 9.4 The move as a publish — kept (D245)

**Was and is:** moving an attachment into shared Dokumenty publishes it to the whole household, including non-members of the conversation.

Three alternatives were offered and all three declined: Všichni-only moves (no leak, but the heaviest files live in small groups and those would become delete-or-keep); no move at all; and a third `visibility` for conversation-scoped documents, which re-walks v9's leak table for a third value and makes `documents` ask `chat` about membership — a version of its own, not a v10 line item.

**Karel kept the leak and bought the cleaning power**, with the consequence spelled out in the dialog. This is the one place in v10 where a member can widen access with a single click, and it is deliberate.

### 9.5 What is still most likely to be re-opened

1. **Messages are not audited (D231).** The one broken invariant in the module, and the one thing every future reader will take for an oversight. If the missing forensic trail ever bites — an argument about who deleted what — the fix is **not** full auditing, it is `chat.message.edited` and `chat.message.deleted` only, content-free: a handful of rows a month, no traffic-analysis record, no growth problem. That option is already priced (§9.3) and can be added without changing anything else.
2. **A reader can fill storage they cannot clean (D241).** The intersection of Karel's two answers, and the only place two of them still pull against each other. If it bites, the fix is to drop the role gate, not to remove readers from chat.
3. **Any member can still start the destruction (D222 + D253).** The koš makes it recoverable, not impossible. If it ever bites, narrowing *delete* to the creator and admins leaves rename and membership untouched.

---

## 10. Czech UI vocabulary (fixed)

| Concept | Czech |
|---|---|
| module | **Chat** |
| default conversation | **Všichni** |
| conversation | konverzace · **Nová konverzace** |
| members | Členové · Přidat člena · Odebrat z konverzace |
| delete / restore / purge | Smazat konverzaci · **Koš** · Obnovit · **Smazat natrvalo** |
| in the koš | v koši · zbývá 5 dní |
| message actions | Odpovědět · Upravit · Smazat zprávu |
| edited marker | upraveno |
| deleted message | Zpráva byla smazána |
| below the floor | Zpráva mimo vaši historii |
| unread divider | Nové zprávy |
| mute | Ztlumit konverzaci |
| clean-up page | **Úklid úložiště chatu** |
| clean-up actions | Ponechat · Odstranit · Přesunout do Dokumentů |
| removed placeholder | Soubor odstraněn při úklidu úložiště |
| thresholds | Limit pro chat celkem · Limit na jednu konverzaci |
| over threshold | Nad limitem |
| not backed up | Nezálohováno |
| warning | Chat zabírá 612 MB z limitu 512 MB |

---

## 11. Worked cases the implementation must reproduce

1. **Jana is added to *Dovolená* on 26. 8. 2026 at 14:00.** The thread shows nothing before that instant. Search finds nothing before it. The clean-up page lists no attachment uploaded before it. A message posted at 13:59 that replies to one from 13:00 is invisible; a message posted at 14:01 replying to the 13:00 one shows *Zpráva mimo vaši historii*.
2. **Jana is removed at 16:00 and re-added at 18:00.** She sees 14:00–16:00 and 18:00 onward. 16:00–18:00 is gone for good. Her unread count on re-add is zero, not two hours' worth.
3. **A new member logs in for the first time in 2028.** They are auto-joined to **Všichni** with `effective_from` = Všichni's `created_at`, and see **the entire household history**. They are in no other conversation. *(Contrast with 1 — same column, different value, no branch.)*
4. **Karel moves `video.mp4` (38 MB) from *Dovolená* into Dokumenty → Rodina.** Chat's total falls by 38 MB immediately (the object left the prefix), Dokumenty's rises by 38 MB, the bubble still plays the video, the clean-up page no longer lists it after a reload, and **Jana — not in *Dovolená* — can now open it in Dokumenty.**
5. **Karel tries to move it into Soukromé dokumenty.** 422, named reason, no copy attempted.
6. **Chat total reaches 612 MB.** Banner in Administrace → Úložiště. Banner in the chat module for **every** member. *Dovolená* at 140 MB is flagged over the per-conversation limit to its members, and appears in the admin's conversation table by name and size — with no link.
7. **Jana deletes *Dovolená* (140 MB) at 10:00.** It vanishes from every member's list, thread, search and clean-up page. It appears in the **Koš** for its members with *zbývá 7 dní*. **Chat's total does not move.** The admin's table shows it as *v koši*. At 10:00 seven days later the drain deletes 140 MB of objects and the rows go.
8. **Jana instead clicks *Smazat natrvalo*.** Typed confirmation, `purge_after = now`, the drain removes it within 15 minutes, the total falls by 140 MB. The file moved to Dokumenty in case 4 **survives** either way.
9. **Karel restores it on day 3.** Membership, messages and attachments are exactly as they were; the queued keys are gone; nothing was reconstructed.
10. **An admin who is in no conversation at all opens `/chat/uklid`.** An empty page with an explanation, not a 403 — the gate passed, there is simply nothing they may clean.
11. **A reader in *Dovolená* opens the warning.** They reach `/chat/uklid` and get 403 with the reason; the warning banner does not offer them the link in the first place.
12. **An admin opens the Log.** They see conversations created and renamed, members added and removed, and attachments uploaded, removed and moved — with filenames. They see **no message activity at all**: not the text, not the sender, not the hour, not the fact that anything was said. Asserted by `TestChatMessagesAreNotAudited`.

---

## 12. Out of scope, explicitly

Reactions · threads (replies are flat back-references) · typing indicators · presence and online status · read receipts naming who read what · message forwarding · pinned messages · voice messages · automatic 1:1 direct messages · end-to-end encryption (member-restricted is access control, not cryptography — the same sentence v9 wrote about private) · message retention and auto-pruning (the koš is for conversations, not messages) · transcoding · video poster frames · gotenberg previews inside chat · a Nástěnka widget · chat metrics and lists · editing or deleting another member's message · un-publishing a moved document · a koš for individual messages · per-conversation threshold overrides · conversation-scoped documents (a third `visibility`) · a `platform/members` strand · migrating v9's env threshold into `storage_thresholds` · rewriting v9's D190 fan-out on top of the new `PublishTo`.

---

## 13. Owed before the PRD is written

1. **Confirm the module's place in the nav** — eleven items now. `AppShell`'s `OVERFLOW`, the Log's `MODULES` and `admin/listener.go`'s `inAppURL` all need a `chat` entry; `platform/widgets/registry.tsx` needs **none** (D252). That is three of the six host maps, and the sixth — `backend/openapi.yaml` — goes to 0.12.0 **in the same PR** (the v7/v8 failure that v9 did not repeat).
2. **The `08003` rebuild of `notification_deliveries`** is the only migration in v10 that touches an existing table with live data. It wants the same care §V9-12 gave the `soukrome` backfill: a down-migration that survives, and a test that runs it against a restored copy.
3. **The design pass** — bubble colours for own vs others, against the `--c1…--c5` CVD constraint recorded in the palette notes; the unread divider; the three attachment states (live / removed / moved); the Koš section; the two warning banners; and the clean-up page at 375 px. The Playwright/axe pass at 375 and 1440 in both themes has been outstanding since v5 and v10 adds the densest screen in the app.
4. **`HOME_LOG_RETENTION_DAYS` is `0` — keep forever — and nothing has ever pruned the Log.** Noted while pricing the audit decision (§9.3); it is a **pre-existing condition, not something v10 causes**, because D231 keeps message events out of the spine. Chat adds structural rows at roughly the rate the other ten modules already do. Worth a decision one day; not a v10 blocker.
