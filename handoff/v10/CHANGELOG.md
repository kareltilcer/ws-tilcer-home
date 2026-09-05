# Home — Changelog

Version history for the `home` service. Full detail lives in `PRD.md` (§10 Decisions) and the `HANDOFF*.md` set. OpenAPI versions track the API contract in `openapi.yaml`.

---

## v10.2 — 2026-09-05 · The shell's own chrome (`app shell` · `settings`)

> **No API change** — OpenAPI stays at **0.14.0** and no migration ships. (0.13.0 was v10.1; `backend/openapi.yaml` has carried **0.14.0** since the same-day reminder lead `"0d"` was added to the event surfaces, which this file never got an entry for.) Decisions **D271–D273** (`PRD.md` §V10-10), as-built record in **§V10-15**. From the `design/v10_2` bundle plus three requests from Karel, all of them about the frame around the modules rather than about any module.

**The phone had a header the design never drew** (D272). A 61 px strip carrying the word *home*, a theme toggle and a sign-out button sat above every screen at 375 px from v1 onwards, and it appears in **no artboard of any version** — drift that outlived nine releases because nobody had put the mock and the screen side by side. The name is on the icon that was tapped to get here and the other two are settings; the first row under the status bar now belongs to the screen somebody is standing on. ⚠ **The arithmetic had to follow it out**: `--chat-chrome-top` was that header measured, and chat's one height calc subtracts it — left at 61 px the thread would have ended 61 px above the tab bar with a dead strip under the composer. Measured in a browser at 375 × 812: the box is 755 px against a 57 px tab bar, and the page does not scroll.

**The side nav's foot is a person, not a stack of buttons** (D273). Four full-width controls under a bare email became one row — the initial, the name, what that member may do — with a ⚙ carrying the same active state the list rows carry. Nastavení leaves `DESKTOP_NAV_ORDER` for a new `DESKTOP_FOOTER_ROUTES`, so the guard that every nav entry is drawn *somewhere* still holds and the phone's sheet keeps its full row. **Odhlásit se moves into Nastavení** beside the theme toggle already there, under a section renamed *Vzhled a účet*: that deletes the shell's second and third copies of both controls.

**The deployed version is visible** (D271), in the two places the artboards put it — the foot of the side nav and the bottom of the *Více* sheet, where a hint describing the sheet to somebody already looking at it used to be. `v10.2` is a constant in the repo bumped with this file; the commit is a build arg defaulting to Coolify's `SOURCE_COMMIT`. ⚠ **Coolify excludes that variable from the build by default** to keep Docker's layer cache warm — *Include Source Commit in Build* is the toggle — and without it the label reads `v10.2` alone, which still names the release. A value that is not a sha is dropped rather than printed.

⚠ **Four contrast fixes, and the e2e sweep is how they were found.** Adding `/nastaveni` to the axe run turned up two serious violations on that screen — and then two more on the sidebar's `admin` tags that had been failing the suite at light/1440 **for releases**, unnoticed because the suite had never been run green. `--subtle` on `--s1` measures **4.29:1** in the light theme, under the 4.5:1 AA bar; `--muted` is 6.81:1. Same swap `theme/globals.css` already made once for the foreign bubble's author label. The pairing is wider than these four usages and belongs to the design bundle's tokens, not to a per-usage fix — but the suite is green for the first time, which closes an item that has been outstanding since v5.

---

## v10.1 — 2026-08-29 · Chat's second pass (`chat`)

> OpenAPI **0.12.2 → 0.13.0**. Decisions **D265–D269** (`PRD.md` §V10-10), as-built record in **§V10-14**. Migration **`12003`**, the third file in block 12. One pull request, against the shipped module — from the `design/v10_1` bundle plus four requests from Karel. ⚠ **The design bundle numbers its reactions decision D264 and that number was taken three days earlier**; the numbering here starts at D265, and the collision is recorded rather than quietly resolved.

**Reactions** (D265) — seven emoji, a chip per emoji under the bubble, `PUT /api/chat/messages/{id}/reactions`. It takes the **desired state** rather than toggling, and the reason is the double tap: a gesture fires twice far more easily than a button does, and a toggle applied twice lands on the opposite of what the member meant with a vanished chip as the only evidence. The wire carries **no `mine` and no `count`** — `ws.PublishTo` marshals one frame for the whole audience (D233), so a per-recipient field is right for at most one recipient; the reactors ride as `(user_id, label)` pairs and every client answers both questions locally. No audit event, no push: D231 accepted that for messages, and a reaction is strictly less than one. The `/ws` audience is `MemberIDsAbove` — the frame carries the whole message.

**Three touch gestures** (D268) — double tap hearts, swipe right replies, long press opens the reaction bar — and **every one has a visible control doing the same thing**, because this is a household app and a gesture nobody was told about cannot be the only way in. ⚠ The hard part is not firing during a scroll: a thread is a vertical scroller and a bubble fills most of its width, so *every* scroll starts as a press on one of them.

**The row previews its last message** (D266), bounded by the floor — `MAX(id)` over a conversation is the newest message the ROOM has, which for a member added yesterday is a body they may not read, printed on the row they see before they open anything. **`/chat` opens the last room at ≥1024** (D269), gated on the viewport and not the data: below 1024 the list IS the screen, and redirecting there would make it unreachable. **The koš is drawn only when it has something in it** (D267) — what §V10-7's route table already said and the design already drew; `trashed_count` rides the active listing so knowing costs no request.

⚠ **One defect found by opening the page**, and the third release running for that sentence: the preview line made the list pane's min-content width the sentence's width, so the pane measured 415 px inside a 375 px grid and the ＋ button and every timestamp were clipped. `min-w-0` on the aside — the width twin of a `min-h-0` note that has been in that file since v10.

⚠ **Two `xhigh` review rounds, seventeen findings** (§V10-14). The second round is the one worth the name: it found that D266 had quietly given `chat_message.updated` a second consumer, so fixing a typo left every sidebar quoting the typo — and that the first round's own control guard had created a two-finger swipe that replied to a message nobody swiped. **One finding is recorded and NOT fixed**: the published `reply_to` is a per-recipient field on a frame marshalled once, which is v10's defect on the send path and not this pass's — every quote a reaction could leak has already been leaked by the reply's own creation frame. It is on the outstanding list with the shape of the fix.

---

## v10 — 2026-08-26 (spec) · **PR 1 merged, PR 2 built, PR 3 outstanding** · Chat (`chat`)

> OpenAPI **0.11.0 → 0.12.0** (spec; the served contract is still 0.11.0 until v10 ships). Decisions **D216–D258** (`PRD.md` §V10-10), plus **D259–D262 taken by a build-guide pass against the spec** before a line was written. Migration block **12**, the first new block since v8. Triggered by a short brief from Karel: a default group chat for all members, plus groups anybody can create and name, carrying images, video and files — and, because that will fill R2, a storage threshold and a clean-up page in Administrace. Scope was frozen the same day after an interview of nineteen questions, **then four decisions were deliberately re-opened before the PRD** and two of them changed. Resolved brief: `V10-chat-brief.md`. Build guide: `HANDOFF-12-chat.md`. **⚠ Ships as three pull requests, not one (D261).**
>
> **Build status (2026-08-27).** **PR 1** — `platform/ws` grows an identity — merged as [#23](https://github.com/kareltilcer/ws-tilcer-home/pull/23); it landed larger than planned, adding `NotifyTo`, `DisconnectSession` and a session-revalidation pump so a socket cannot outlive the session that opened it. **PR 2** — chat core, and the served contract reaches **0.12.0** with it. **PR 3** — attachments, the two thresholds, `/chat/uklid`, the move to Dokumenty, the drain and the Administrace storage block — is unbuilt, and the **nav entry rides with it** (D260), so the household does not meet a chat that cannot yet send a file. ⚠ **`PRD.md` §V10-12 is the PR 2 as-built reconciliation — read it before trusting §V10-1…§V10-11.** Six things shipped differently, and two of them correct facts the spec asserts: the read floor is anchored on a **message id** rather than on a timestamp converted into one (the timestamp form leaks by up to a millisecond, and the first adversarial test found it), and `notification_deliveries` has **four** indexes, not three. ⚠ **A review pass then found fifteen more, all fixed in PR 2** — among them a search that returned **500 on an apostrophe**, a thread that stopped at fifty messages with no way back, a conversation name reaching every lock screen through a trigger's default body, and an email address reachable through the member directory. Four of the fifteen were UI absent from the build *and* from the "deliberately does not ship" list, which is to say nobody had decided to defer them; that list is now exhaustive.

### Headline

The eleventh module, and **the first one that is not readable by the household.** v1–v8 published data every member could see. v9 added a second access axis, *ownership*, and made a private item invisible to everyone but its owner. v10 adds a **third: membership.** A conversation is readable by the people in it — neither "everyone" nor "one person" — and almost every platform surface built so far assumes it is one of those two.

The refusal is v9's, unchanged in shape: **404, never 403**, byte-identical to an unknown id, admins included (D217). A 403 confirms a conversation exists, which turns a guessed id into an oracle over who in the household talks to whom.

### The floor, which is the model's sharp edge (D218, D258)

`chat_members.effective_from` is *the instant from which a conversation is a member's to read*, and it is pushed into the SQL of every read — thread, search, attachments, clean-up, unread, and the reply-quote resolver. A floor applied after the rows are fetched is not a floor; it is a place where a cursor, a count or a snippet still describes what it removed.

⚠ **It is not "when they joined", and the difference is D258.** A group add writes `now()`, because being added to a group is one person's decision about another person's access to a third person's history. **Všichni — the household room — seeds it with the conversation's own `created_at`**, so a member the app meets for the first time in 2028 reads the whole household history. **The exemption is a value, not a branch**: no read path anywhere tests `kind == "default"`, and `TestDefaultConversationHasNoHistoryBranch` enforces it.

*(This was re-opened. The first pass applied the floor everywhere, which meant a new member opened the household room and found it permanently empty.)*

### The two changes that are larger than the chat

**`platform/ws` grows an identity (D232, D233).** The hub broadcasts to every connected client and `ws.client` holds a socket, a channel and a cancel func — **no user id at all**. The upgrade handler already resolves an actor from the session cookie and throws it away. v9 worked around this rather than through it, publishing `{"private":"1"}` with the id dropped (D190); that does not scale to a module whose whole payload is the thing that must not leak. So the hub keeps a per-user index and gains `PublishTo`, **`Publish` is untouched**, and chat's payload becomes the first in Home to carry content. ⚠ Its one non-obvious hazard is removal: a client sits in two maps, and a `remove` that cleans one leaks dead sockets per user — visible only as memory, months later.

**The move to Dokumenty is a custody transfer, not a copy (D238).** `chat` may not import `documents`, so *"move this file into Dokumenty and keep it visible in the thread"* goes through a new **`BlobSink`** in `platform/storage` — the first **verb** in a catalog that has so far carried only projections. It spans two SQLite transactions and two object-store calls with **no transaction over all four**, so the ordering is fixed — validate, copy, insert, mark, **delete last** — chosen so that every crash point over-counts rather than loses the file. The PRD's acceptance criteria demand fault injection at each of the four points.

⚠ **And a move is a PUBLISH** (D245). Shared Dokumenty has no membership, so a file from a three-person conversation becomes readable by the whole household. That is inherent in *"still visible inside the chats"*: the thread's members must be able to read it, and the only place in Home where that is true without inventing a third `visibility` is the shared tree. Three alternatives were put to Karel and declined — Všichni-only moves, no move at all, and conversation-scoped documents. **The leak was kept and the cleaning power bought**, with the consequence in the dialog.

### The storage half (D236, D237, D240–D247, D253, D254)

Two thresholds, **both DB-backed and editable in Administrace** — `chat.total` (512 MB) and `chat.conversation` (128 MB, one number applied to every room, no per-room override that a member could raise to silence a warning). **Warn only**: no upload is ever refused, there is no quota and no new 413 — §V9's D196 restated for a second register. They live in a platform-owned `storage_thresholds` table because `admin` writes them and `chat` reads them and neither may import the other. ⚠ v9's `HOME_STORAGE_WARN_TOTAL_MB` **stays an env var**; Home now has two threshold mechanisms and the inconsistency is recorded rather than hidden.

**Úklid úložiště chatu** is member-scoped — you clean what you can already see — with an `editor`/`admin` gate on top. ⚠ That intersection has a consequence stated rather than resolved: **a `reader` can fill storage they can never clean.** Three actions, one of which is the absence of a button: *Ponechat* stages nothing and queues nothing, because *"not every document has to be dealt with at that moment"* is a statement about state, not about a control.

**Deleting a conversation is a 7-day koš** (D253) — re-opened and changed. The first pass cascaded immediately, which let any member, a reader included, permanently destroy every other member's files with one confirmation. Now it is recoverable, restore is the state before the delete rather than a reconstruction, and ⚠ **trashed bytes keep counting** toward both thresholds because the storage page's figures have to sum — with **Smazat natrvalo** so that never traps somebody cleaning up under pressure. An **admin may restore or purge a conversation they may never read** (D255), v9's D181 asymmetry repeated exactly, through chat's own routes so the audit action stays chat's.

### The invariant v10 breaks (D231, D256)

⚠ **Messages write no audit event.** Only structural changes do — conversations, membership, attachments, settings. **Chat is the first module in Home whose primary mutation breaks the every-mutation-audits invariant**, and it does so deliberately.

*This one moved twice, and the round trip is the record.* Draft 1 was structural-only. Draft 2 audited every message content-free, restoring the invariant. **Final: back to draft 1**, because pricing draft 2 produced two costs that together were worse than the breach — it turns the Log into a **traffic-analysis record** of who talked to whom, how often and at what hour, for conversations an admin may never open; and with `HOME_LOG_RETENTION_DAYS` defaulting to `0` (**the Log has never pruned**) it grows `audit_events` and `audit_events_fts` by a row per message with nothing removing them, forcing a whole-Log retention decision on a chat feature.

**What it costs is stated and not softened:** an edited message leaves no record of what it said before, and a deleted one leaves nothing beyond its tombstone. `TestChatMessagesAreNotAudited` guards the breach so it cannot be "fixed" by accident. ⚠ If it ever bites, the fix is **not** full auditing — it is `chat.message.edited` and `chat.message.deleted` only, content-free: a handful of rows a month and neither cost. Offered, priced, declined in favour of a clean line.

⚠ **Attachments ARE audited although the messages carrying them are not.** That looks inconsistent and is not: the bytes are what the thresholds, the clean-up page and the storage register exist for, and *"who uploaded that 40 MB video, and when"* is the question the whole storage half answers.

### The build-guide pass (D259–D262)

Four decisions came out of writing `HANDOFF-12` against the spec, the same kind of pass that produced §V9's D206–D214. **One is a hole in the PRD's own first draft.**

⚠ **D259 — a dropped websocket frame is a missing message.** `ws.Hub` drops on a full send buffer by design and has no replay. Every module so far is fine with that: a dropped *"something changed"* is repaired by refetch-on-focus and nothing was lost meanwhile. **A chat message is different — the loss IS the content, in a thread somebody is reading, with nothing on screen to say so.** Closed twice: every payload carries `prev_message_id` and a client whose held latest does not match refetches the tail (UUIDv7 ordering, so no sequence column and no schema change), **and** the tail is refetched unconditionally on reconnect, because a socket that dies delivering nothing leaves no frame to check against. The check is **one-shot per received message**, which is what makes it terminate — a client that re-checked its own refetch would loop on every message.

**D260 — chat takes a mobile thumb tab and Okno do budoucnosti moves to the overflow.** The first demotion in the nav. Four tabs plus *Více* is the shape that works at 375 px; a fifth makes six. Okno is the least-daily of the four and the only one whose signal already arrives elsewhere — two Nástěnka widgets and four metrics — while chat is the one screen carrying an unread count.

**D261 — three PRs, not one.** `platform/ws` alone, then chat core, then attachments and storage. v7, v8 and v9 each shipped as one PR; v10 is roughly twice the size of any of them with its riskiest change at the bottom of the stack.

**D262 — two-pane at ≥1024**, stacked below; the members list is a panel, not a third column.

### What v10 declines

> ⚠ **Reactions are no longer on this list — v10.1 built them** (D265, above). The rest of it stands, and the entry is left in place rather than edited out: what a version declined and later changed its mind about is worth more as a record than as a tidy sentence.

~~No reactions~~, threads, typing indicators, presence or read receipts · no forwarding, pinned messages or voice messages · **no transcoding and no video poster frames** · **no preview pipeline** — PDFs open in the browser's own viewer, `home-gotenberg` is not involved and `platform/preview` is not created · no message retention or auto-pruning (the koš is for conversations) · **no widget, no metric, no list** (the electricity precedent, enforced by `forbiddenImports`) · no third `visibility` for conversation-scoped documents · no per-conversation threshold override · **no blocking** · no `platform/members` strand · and neither the rewrite of v9's D190 fan-out nor the migration of `HOME_STORAGE_WARN_TOTAL_MB` into the new table, both of which v10 makes possible and neither of which is v10's.

### Data model (§V10-5)

Migration block **12** — `chat_conversations`, `chat_members`, `chat_messages`, `chat_attachments`, `chat_messages_fts`, `chat_deleted_keys`. ⚠ `chat_messages` carries an explicit `seq INTEGER PRIMARY KEY` **for the same reason `notes` and `documents` do** — external-content FTS5 keys on the rowid, and a TEXT-PK table's implicit rowid is renumbered by `VACUUM` — so that table must never be rebuilt, and the body/attachment invariant lives in the write transaction. `chat_messages_fts` is the **fifth** external-content FTS5 index, taking the shadow count from twenty to **twenty-five**.

Two migrations land **outside** the block: `02004` adds `cat_chat` to `notification_preferences` and creates the platform-owned `storage_thresholds`; **`08003` rebuilds `notification_deliveries`** to widen two CHECKs for `'chat'` — the only v10 migration touching live data, and the one that wants a restored-production run with its down exercised. ⚠ Both are out-of-order goose versions below the applied `11001`; v9 shipped that shape with `01002`/`06004`/`07004`, so verify the runner tolerates it before writing them.

**Eleven audit actions**, all structural. **One new env var**, `HOME_CHAT_TRASH_DAYS` (7). The per-file upload cap is **`HOME_DOCS_MAX_UPLOAD_MB`**, shared with Dokumenty on purpose — a file above Dokumenty's cap could never be moved there, so the clean-up page's headline action would fail on exactly the files that caused the overrun.

**Contract:** 125 → **143 paths** (18 new, carrying 24 operations), 249 → **279 schemas**.

### Open — and the one thing to watch

- ⚠ **The leak table has twenty-three rows and should be treated as a floor.** v9's went from eighteen to twenty-three under review and the build still found **two more that no review had listed** — background jobs with no actor. v10's equivalents are the drain, the koš purge and thumbnail generation.
- **A `reader` can fill storage they can never clean** (D241) — the one place two of Karel's answers still pull against each other.
- **`HOME_LOG_RETENTION_DAYS` is `0` and the Log has never pruned.** Surfaced while pricing D231; **pre-existing, not v10's doing**, and worth a decision one day.
- Still not this version's job: relocating the legacy `routes/` screens into `src/modules/*` (open since v6), and the `fin` switch-off (§V6-12 step 5).

---

## v9 — 2026-08-21 (spec) · **deployed 2026-08-25** · Soukromé položky a Úložiště (`notes` · `documents` · `admin`)

> OpenAPI **0.10.1 → 0.11.0** (spec **and** as built — the served contract shipped in the same PR). Decisions **D176–D205** (`PRD.md` §V9-10), plus **D206–D213 forced back into the spec by a review pass against the shipped code**, and **D214/D215 added — and D196/D208 revised — when its findings went back to Karel**. Built 2026-08-24/25 as PRs **#21 `bcd3c2f`** and **#22 `a7d7e17`**; the as-built reconciliation is **`PRD.md` §V9-12**. Triggered by three sentences from Karel: give Dokumenty and Poznámky a second, private root, and give Administrace a storage-statistics page. Scope was frozen the same day after an interview of twelve questions; the resolved brief is `V9-privacy-storage-brief.md`.


### As built — the v9 build (2026-08-24/25, deployed 2026-08-25)

> Built from this spec in two PRs — **#21 `bcd3c2f`** (the module) and **#22 `a7d7e17`** (the root switcher restyle) — 88 files, ~13 500 insertions, seven new test files at ~3 600 lines. Everything in §V9-1…§V9-11 still holds except where recorded in **`PRD.md` §V9-12**, which is the full reconciliation.

**The v7/v8 process failure did not repeat.** `backend/openapi.yaml` shipped at **0.11.0 in the same PR** — 125 paths, 249 schemas — and this folder's `openapi.yaml` is now a copy of it rather than a parallel document. That is the one thing the last two releases got wrong and this one got right.

**Four findings the build recorded itself.** ⚠ **`modernc.org/sqlite` v1.54.0 DOES expose `dbstat`** — probed rather than assumed, and `pgsize` sums to exactly `page_count × page_size`, so the spec's inference from the package index was right and the fallback is a safety net rather than a path. ⚠ **There are FOUR external-content FTS5 indexes, not three**: `garden_plants_fts` arrived with v7 and was never counted, so **twenty** shadow rows and not fifteen — each now declared through `storage.FTSShadows("x_fts")` so a fifth index is one call rather than four chances to mistype a suffix. **Two tables belong to nobody and are created by no migration** — `goose_db_version` and `sqlite_sequence` — both allow-listed, the second found by running the guard. And **the completeness guard was verified the way D192 asks**: a throwaway table was added, the build went red naming it, and the table was removed.

**⚠ D214 was DECLINED, not left unbuilt.** The Litestream replica line needs `LITESTREAM_*` credentials that only `docker-entrypoint.sh` and the litestream process hold. Wiring them into the Go binary would have introduced no *new* secret while meaningfully widening what the application process can reach — the credentials for the household's entire database backup. Karel declined that trade on 2026-08-24, so `StorageReplica.configured` is permanently `false` and the other fields stay null. **The distinction matters because D214 reads persuasively**: anyone who finds `configured:false`, reads D214 and wires the keys in as a bug fix has reversed a decision, and `TestReplicaIsDeclinedNotUnimplemented` fails if the field is ever populated. What it costs is precise and small — the app cannot report replica size or confirm replication liveness, which returns to `litestream snapshots` on the droplet. Database figures, per-module R2, the mirror-bucket line and the warning threshold are all unaffected, and the page's arithmetic never included the replica anyway.

**Nine more decisions shipped differently, none accidentally** (all in §V9-12): `/ws` was **fixed rather than named** — a private mutation now publishes `{"private":"1"}` and drops the id, going further than D190 promised · the log's **entity timeline excludes** where §V9-11 asked it to redact, stricter but a different response shape than any client would expect, and still undocumented on the wire · **one redaction phrase became four**, chosen on `entity_type`, because *a private item happened* is more useful when it says which module · the **private-image rule gained a documented widening** so an image its owner copied into a shared note does not break for everyone else — and a weaker first version of that term was a real hole, since an admin could paste an id from the purge screen into a note they wrote themselves · `dbstat` is probed **on first use rather than at boot**, caching only the positive answer, because a cancelled request returns the same error as an absent virtual table · Administrace shipped a **two-level tab strip** rather than one six-tab row · the backup line **measures module prefixes rather than the bucket** · **`StorageBackup.last_mirrored_at` was dropped from the contract with no stated reason anywhere** — the one v9 change nothing explains · and **D179's pairing invariant is structurally unreachable through HTTP**, which is arguably correct but leaves its criterion unproven.

**The build also found two leak surfaces the 23-row table never listed**, both of the same class: the **preview worker** and the **notes image GC** are background jobs with **no actor**, so a viewer-scoped read would have left every private upload at `pending` forever with no error. And **the 304 ordering** — the viewer-scoped load had to move *before* the `If-None-Match` branch, or a second member holding a stale ETag gets a 304 (*"yes, and it hasn't changed"*) for something they may not read. Neither is a new rule; both are places the existing rules had to reach and the table did not point at. **Assume the table is still short.**

**⚠ Five acceptance criteria are unmet, and one of them matters.** The **trigger-listener redaction has no test at all** — the code is right, but `admin_test.go` is untouched and nothing calls `RedactRendered` from a test, so both criteria for leak rows 15a and 15b stand unguarded **on the exact surface the review pass identified as the half the first draft missed**. The others: `documents.pinned_count` and the notes list provider are untested where §V9-11 said "asserted, not assumed"; nothing asserts the jobs read no title; D199's catalog assertions were never written (`arch_test.go` is untouched, so the claim is true but unguarded); and the restored-production migration test **skips** unless an env var points at a copy.

**One bug worth carrying forward, because it is the v8 lesson repeating.** `StorageBlobs` and `PrivateItems` were implemented on the *Service* rather than the *Module*: everything compiled, every test passed, and the Úložiště page reported **0 B with an empty purge listing**. *"It was found by opening the page."* `TestRealModulesImplementTheStorageCatalog` exists because of it — and the running app still wants clicking through **from a second member's session**, which is the only way a privacy leak looks like anything at all.

---

### Headline

**Poznámky and Dokumenty each gain a second root** — *Soukromé poznámky* and *Soukromé dokumenty* — visible only to the member who put something there. And **Administrace gains Úložiště**, a page that finally answers *what is using our space*: database per module per table, R2 per module split shared versus per member, the orphan backlog, the backup bucket, and one warning threshold.

**v9 is the first version of Home that adds no module.** Every release from v3 to v8 appended one through the registry and left everything before it alone, which is why each could be specified as a self-contained delta and built in one pass. v9 cannot be. It alters four tables that have carried real household data since v3 and v4, and it invalidates an assumption roughly forty call sites were written against:

> Until v9, every row in `notes` and `documents` was visible to every member.

The trees, both searches, the resolver, the four content endpoints, the permalink, the two pinned widgets, the two metric providers, the one list provider, the audit spine, the log browser and its entity timeline, the trigger listener and its templates, the websocket fan-out, the HTTP cache headers and the mirror/GC jobs all rest on it. **§V9-4a — the leak table — enumerates twenty-three surfaces**, twenty-two of which deny and one of which grants. It is written as a table rather than as prose because it is simultaneously the requirement, the build checklist and the test plan. The build's risk profile inverts accordingly: **the new code is the easy half**, and the difficulty is entirely in the seams.

### A second root, not a checkbox (D177, D178)

Four tables gain `visibility` (`shared` | `private`) and `owner_id` (NULL ⇔ shared), and a tree is addressed by its **root scope** — the pair — of which there are `1 + N` per module for `N` members. Each private root is a **full tree**: arbitrarily deep subfolders, the same folder CRUD, the same emoji icons, the same slugs, the same move. The per-item-flag alternative was specified and rejected: it puts folders of mixed visibility into a tree the household shares, so members see folders whose contents differ from what the folder claims to hold, and *"why is this folder empty for me?"* becomes a permanent question with no good answer.

**The model is really enforced by four indexes — and by one query beside them.** Both modules dedupe root-level siblings with a `COALESCE(parent, '')` sentinel, because SQLite treats NULLs as distinct and a plain `UNIQUE(parent_id, slug)` would not constrain the root at all. That sentinel now collides with itself once there are `1 + N` roots, so it carries the root scope — `COALESCE(parent_id, 'root:' || visibility || ':' || COALESCE(owner_id, ''))` — in **four** indexes, not one. ⚠ **And the symptom of leaving it alone is not the 409 an earlier draft claimed four times over** (D210): `freeSlug` loops on the store's own un-scoped collision query and appends a suffix, so the second member silently gets **`recepty-2`** — a slug that discloses a sibling they cannot see — and both requests succeed. Four indexes and one query, in the same commit. The bug surfaces only when two people happen to pick the same name, which in a household is often, and it announces itself as a slightly odd slug rather than as an error.

**No table was rebuilt** (D179). SQLite cannot add a constraint in place, and `notes`/`documents` carry an explicit `seq INTEGER PRIMARY KEY` *because* their FTS5 indexes are external-content and rowid-keyed — a rebuild renumbers rowids and desynchronises search, with the symptom that search silently returns wrong rows. The visibility/owner pairing is therefore a **service-level invariant**, exactly as v8's meter monotonicity is (D148).

### Private means private, with exactly one asymmetry (D180, D181)

Reads are refused to everyone but the owner, **admins included**, and refused with **404 rather than 403** — on GET **and** HEAD, because a 403 confirms an id exists and turns `/d/{id}` into an existence oracle over the whole private tree. Against that stands one power: an `admin` may **hard-delete** a foreign private item and may never read one. Somebody has to be able to reclaim space and remove a departed member's files; nobody has to be able to read them.

**Publishing is one-way and owner-only** (D182). `POST …/publish` sets `visibility='shared'`, clears `owner_id`, reparents, re-derives the slug and cascades to a folder's descendants in one transaction. There is **no unpublish route** — a document the household has relied on for months must not vanish into one member's tree — and an admin cannot publish someone else's item, because publishing what you cannot read is not a power that should exist. The permanent `/d/{id}` URL is **unchanged** by a publish: the R2 key is id-based and independent of folder, slug and scope, which is the whole reason it was specified as permanent.

### The audit spine, written in full and redacted on the way out (D187, D188, D189)

A private mutation writes the event it always did — real summary, real diffs — plus `meta.visibility` and `meta.owner_id`. Redaction happens **at read time** in one function: for anyone but the owner the summary becomes *"Soukromá položka — podrobnosti skryty"*, `entity_id` is dropped, `changes` comes back empty and `redacted` is true. Redacting at write time would redact the record for the person it belongs to, permanently, and the spine would stop being their own history.

**Two rules, not one.** Private events are redacted in unfiltered browsing and **excluded from the log's `?q=` FTS matching entirely** for non-owners — redacting a hit still reveals that the term occurs in a private title. One rule leaks; that is why there are two.

**The push renders from the redacted entry once, for the whole audience, the owner included.** A coalescing window builds one envelope per rule, not one per recipient; an owner-only second rendering would exist solely to carry the real title and would be one audience-resolution bug away from delivering it to the household. The owner loses a lock-screen title and gains a guarantee.

**`/ws` is left alone, and the residual leak is named** (D190). Payloads are already id-only and the hub has no per-user routing, so what crosses is a UUID and the timing of a change. Per-user routing is a platform change with its own failure modes, for one opaque identifier. ⚠ **This decision expires the moment a `/ws` payload grows a title** — and a test asserts none ever does.

### Úložiště, and the fourth registered catalog (D191–D197, D205)

`admin` may not ask `documents` how big it is — the import-lint fails the build on a cross-module import. Home has answered this shape of question three times (`widgets`, `metrics`, `lists`), so v9 adds the fourth: **`platform/storage`**, built the way §V5-12 corrected the other two — an optional `Source` interface plus a `*Registry` assembled at composition, never a package-level global. Modules declare the tables they own and their attributed blob usage; `admin` reads the registry and imports nothing.

⚠ **v9 touches none of the four non-registry host maps** — the first version that touches none. It opens a **fifth** registration surface instead, and this one is closed by machine: a test enumerates `sqlite_master` and asserts every user table is declared by exactly one module or named in the platform's own list. **A table with no home fails the build.**

The numbers are honest by construction. **The database total is exact** (`page_count × page_size` + the WAL file) — the only figure checkable against `ls`. **Per-table bytes come from `dbstat`**, whose availability under `modernc.org/sqlite` is **probed at boot, not assumed**; where it is absent the page shows row counts and says the bytes are unavailable. **It never estimates** — a guessed byte figure on a page whose whole job is reporting byte figures is worse than an honest gap. **R2 figures come from listing the bucket** and joining back to SQLite, not from summing `documents.byte_size`: listing counts the derived `preview.pdf` and `thumb.webp` objects whose sizes are in no table, and it makes objects resolving to no live row visible as **`nezařazené`** — the orphan backlog the mirror job already reconciles, surfaced for the first time. The **backup mirror bucket** gets one line, because it is half the R2 bill and no screen has ever shown it.

**Snapshot only** — computed on read behind a 60-second in-process cache, no sample table, no scheduler job, no history. One **warning threshold** on total bucket bytes (default 8 GB, since R2's free allowance is 10 GB), and **nothing is ever blocked**: no quota, no new 413, no upload that fails because of a number somebody chose.

### The review pass, and the three holes in the spec's own first draft (D206–D213)

The spec was read adversarially against the shipped code before a line of v9 was written, and it found **three real privacy holes in its own leak table** — which is the argument for the table existing, and for treating its length as a floor rather than a total.

- **`publish` was specified as 403 in five documents at once** (D206). On a route open to every `editor`, a 403-for-private / 404-for-unknown pair answers *"does this id exist, and is it private?"* for any id — the permalink oracle D180 closes, reopened with a different verb. It is now **404**, byte-identical to an unknown id, and the design addendum's "not yours" publish state is withdrawn with it.
- **Redacting the summary does not redact the push** (D207). The trigger template's `{{change.<field>.new}}` tokens render from the raw entry, once, before the audience resolves — so an existing rule bodied `{{change.original_filename.new}}` puts a private filename on every household lock screen with `summary` untouched — and `inAppURL` names the private id in `/d/{id}`. The whitelist is by *shape*, so this cannot be fixed by listing fields.
- **A year-long `immutable` cache header outlives the 404** (D208). All five content endpoints send `private, immutable, max-age=31536000`; `private` excludes shared *proxies*, not the second person on the same laptop, and `immutable` suppresses revalidation entirely, so the new 404 never executes. The repo already names this threat model in the PWA persister — it had simply never reached the HTTP layer. The header is now **visibility-dependent**: shared keeps `immutable`, private gets **`private, no-cache, must-revalidate`** — which does not mean *do not cache* but *revalidate before every reuse*, so the ownership check runs on every view while the owner's repeat view of a 30 MB PDF stays a 304 rather than a re-download.

Five smaller corrections came with them: the log's **entity timeline, `?entity_id=` filter and `/stats`** are three more doors into the same query layer, and the timeline returns full diffs for any id — which the purge screen hands out by design (D209) · the slug collision **does not 409** (D210, above) · the completeness test needs an allow-list, because each external-content FTS5 table materialises **five** `sqlite_master` rows and Home has three of them (D211) · the purge screen must list **folders**, which are what actually reclaim a private subtree (D212) · and ⚠ **"the four non-registry host maps" were never four** — `admin/labels.go`'s `actionLabels` is a sixth, hand-maintained, falling back to the raw key and silently degrading since v6; v9 edits it, and edits `inAppURL` in the opposite direction from usual: not a new case, but a private-event fallback (D213).

**The leak table went from eighteen rows to twenty-three.** Read the number as a floor.

Two of the review's questions went back to Karel and one changed the page: the storage snapshot now also reports the **Litestream replica** under the `home/` prefix (**D214**) — objects, bytes, generations and the newest object's timestamp. It is R2 space the household is billed for that no screen has ever shown, the same argument that put the mirror bucket there (D205), and a larger number, since a replica holds many generations where the database figure reports one. It sits **beside** the per-module breakdown and never inside it: Litestream replicates the whole file, so a replica attributed to a module would be an invented number. `newest_at` is the first time the app can answer *"is replication actually running?"* from inside itself. The other two were re-confirmed unchanged — publish stays one-way (so nothing already shared can ever be made private, which is accepted), and private events keep firing trigger rules even though D207 leaves the message saying very little.

### The purge screen, and why it is uncomfortable (D198)

**Soukromé položky** lists every member's private items by **id, module, kind, owner, size, dates** — and nothing else. No title, no filename, no content type, no thumbnail, no download, no search box. It exists because "an admin may hard-delete" is useless if nothing in the app can name the thing to delete, and it is uncomfortably close to being the private-file browser the whole feature exists to prevent. That discomfort is the design constraint rather than an objection to it: the screen should read as a maintenance tool, not a file manager. **Deletion is not implemented there** — it calls the owning module's existing hard-delete route, so `admin` gains no delete path of its own and the audit action stays the module's. **Opening the listing is itself audited** (`admin.private_items.view`), the only read in Home that writes an event.

### What v9 declines

No per-person sharing — two visibilities, not an ACL. No unpublish. No encryption beyond what R2 and the droplet already provide, and no copy implying otherwise. No privacy in any other module. No storage history, growth curve, forecast, quota or cleanup wizard. **And no storage metric, list, widget or push** (D199) — the v7 frost pattern (D113) would fit in an afternoon and is out of scope anyway, because it needs a daily job and a fired-today marker, which is exactly the stored state the snapshot declines. Considered, costed, deferred, so the next person to think of it finds it already thought of.

### Data model (§V9-5)

**No new table and no new migration block** — the schema-level statement of "v9 adds no module". Three migration files: `01002_private_meta.sql` (one expression index on `audit_events`, so the redaction marker can live in the existing `meta` JSON rather than in two new columns), `06004_notes_private_scope.sql` and `07004_documents_private_scope.sql` (two columns × four tables, four replaced slug indexes, two owner-scope lookup indexes). Existing rows become `shared` by column default; **no backfill, no seed**. Down migrations drop the indexes **before** the columns, because SQLite refuses to drop a column an index references and the reverse order wedges the table halfway.

**Five new audit actions**, and every existing `notes.*` / `documents.*` action gains `meta.visibility`.

### Open questions — resolved 2026-08-21

Twelve, all closed before a line of PRD was written. Nobody but the owner reads a private item, admins included · one power for admins, hard delete · publish is one-way and owner-only · a full private tree per member, not a flat list · personal pins only, household refused · search scoped to the tree you are in · the audit event written in full and redacted at read time · the push carries the redacted text to the whole audience · snapshot only, plus one warning threshold · named per-member totals split shared/private · a dedicated purge screen · nothing enforced, no quota. **None open** — and the three blanks that were left for Karel closed the same day: the threshold is **1024 MB**, a *change* detector rather than a bill detector (D196 — household usage sits well under a gigabyte, so a threshold at R2's 10 GB billing cliff would stay silent for years and teach nobody anything) · all existing content **stays shared**, with the consequence accepted explicitly, since there is no unpublish · and **Soukromé položky is always visible with an explaining empty state** (D215), because hiding the tab would hide the screen and not the capability.

One item remains and it belongs to the build rather than to Karel: whether `modernc.org/sqlite` exposes `dbstat`. Evidence gathered 2026-08-21 says it very likely does — `modernc.org/libsqlite3` publishes `DBSTAT_PAGE_PADDING_BYTES`, a constant that in the SQLite amalgamation sits inside the `#if defined(SQLITE_ENABLE_DBSTAT_VTAB)` guard and is therefore only transpiled when that flag is set. That is inference from a package index rather than a read of the source, so the boot probe stays and the fallback must be **exercised** rather than merely written.

### One thing the spec could not settle outright

⚠ **Whether `modernc.org/sqlite` exposes `dbstat`** — a compile-time SQLite option (`SQLITE_ENABLE_DBSTAT_VTAB`) — could not be verified directly while drafting: the Go module proxy is unreachable from the drafting environment, GitLab blocks robots and the GitHub mirrors need auth. It is specified as a **boot-time probe with an honest fallback** rather than as an assumption, and the build records the answer in `PRD.md` §V9-12.

The indirect evidence is good, though: `modernc.org/libsqlite3` publishes the constant **`DBSTAT_PAGE_PADDING_BYTES`**, which in the SQLite amalgamation lives **inside** the `#if defined(SQLITE_ENABLE_DBSTAT_VTAB)` guard and is only transpiled when that flag is set; the `DBPAGE_COLUMN_*` constants beside it point the same way. So the expected answer is **yes**, and the fallback is a safety net rather than the likely path — which is exactly why it must be **exercised** rather than merely written. This is the one figure on the storage page whose method is conditional, and it is stated as conditional rather than guessed.

---

## v8 — 2026-08-20 (spec) · **deployed 2026-08-21** · Elektřina (electricity)

> OpenAPI **0.9.0 → 0.10.0** (spec) → **0.10.1** (as built). Decisions **D133–D162** (`PRD.md` §V8-10), plus **D169–D175 taken during the build** (`PRD.md` §V8-12). Triggered by one product addition from Karel: tracking the household's electricity against its zálohy. Scope was frozen the same day after an interview of eleven questions; the resolved brief is `V8-electricity-brief.md`.

### Headline

A **tenth module, Elektřina (`electricity`)** — the household enters its meter readings whenever it happens to think of it, two registers (**VT** / **NT**) at arbitrary dates, and the module answers the one question that matters: **will the monthly zálohy cover the bill, or is a nedoplatek coming?**

It is the mirror image of v7. Zahrada was the largest module home has gained — eleven tables, an LLM round trip, an external forecast; Elektřina is the **smallest**: five tables, thirteen paths, one pure function, no external dependency, no env var, and — for the first time in home — **no widget, no metric, no list and no push**. Almost all of its difficulty sits in one formula and in three questions about honesty: what counts as measured, what may be estimated, and what the module refuses to say.

### The three answers that shape everything else

- **A reading is an instant, not a day (D134).** The meter state at 00:00 of `read_on`; consumption of day *d* is `reading(d+1) − reading(d)`. Every boundary in the module — reading intervals, ceník changes, the start and end of a settlement period — is a corollary of that one sentence, which is why none of them needed a rule of its own. A period `[starts_on, ends_on]` is closed by the reading dated `ends_on + 1`, the same reading that opens the next one, exactly as the distributor treats it.
- **Money is never interpolated; pictures may be (D137, D138, D159).** Karel took the strict option: a ceník change falling *inside* a reading interval **blocks** that interval rather than pro-rating it. The module names the missing odečet, computes nothing past it, and leaves everything before the gap on screen and valid. The counterpart is stated as loudly as the rule, because otherwise the history chart is impossible: the chart *does* spread kWh evenly over an interval's days, labelled approximate — and a month's Kč column is an **allocation of already-exact interval costs by day count**, never a repricing of the invented kWh.
- **A prediction must never read as a fact.** The headline number is hedged three ways: the basis is printed under it (*"predikce z průměru za posledních 122 dní"*), the period's end date carries a **předpokládaný konec** badge while it is only expected (D153), and the actual/forecast boundary is the **last reading rather than today** (D141) — so "actual" always means measured, and the fortnight since the last odečet is honestly counted as forecast.

### The module that contributes nothing (D147, D152, D156)

Karel's answers were *"no widget for Nástěnka"* and *"no chase"*, and taking them literally produced the leanest module in the app. `electricity` implements no `Source` interface and imports **none** of `platform/metrics`, `platform/lists`, `platform/push`, `platform/scheduler`, `platform/blobstore` — a test asserts the absent imports, so a later refactor cannot quietly add one. Everything is computed on read: no derived column, no cache table, no job, no seed.

Two consequences. First, **the module has to be worth opening on purpose**, since nothing will remind anyone it exists — which is why Přehled carries the module's entire value and why the one concession is a plain in-app line, *"poslední odečet před 47 dny"* with a **Zadat odečet** button. Text on a page you already opened is not a notification. Second, the **four non-registry host maps** that v5, v6 and v7 each tripped over become **three** — and the fourth inverts into a trap in the opposite direction: `platform/widgets/registry.tsx` must **not** be touched.

### Three numbers, not thirty (D135)

The itemized model was specified in full — silová elektřina and distribuce split per tariff, stálý plat za jistič, systémové služby, POZE, činnost OTE, daň z elektřiny, DPH computed — and then rejected: *"No, just VT, NT, poplatky, nothing more."* A ceník version is **cena VT**, **cena NT** (Kč/MWh) and **měsíční poplatky** (Kč/měs), all **including DPH and distribuce**, used exactly as typed. Ten fields that must be re-typed every January would be ten chances to mistype, in exchange for an itemization the household never reads.

What survives from the itemized idea is the thing Karel actually asked for: prices are **versioned by effective date**, a version governs all days from its date until the next version starts, its end is derived and never stored (D136), and editing one moves **only its own days**. That is the structural form of "effective to some date, which will not affect numbers before that date" — a property of the schema rather than a rule someone has to remember. Entering next January's prices in August is the normal case, and the forecast honours them immediately (D142).

### Zálohy, and the arithmetic of "how many"

A schedule `{effective_from, amount, due_day}` versioned like the ceník, plus optional rows recording what was really paid; a recorded payment **wins** for its month (D144), and attribution is by the month key rather than the payment date, so a March záloha paid on 2. dubna still belongs to March. How many months a period counts is **its own rule**: a calendar month counts iff the period contains that month's **first day** (D145), which makes a year-long period exactly twelve whatever day it starts on — Karel's 24. 6. 2026 – 23. 6. 2027 counts červenec 2026 … červen 2027. `due_day` (D155) is read in exactly **one** place, whether a counted month is already paid, so it moves the *doporučená záloha* and nothing else.

### Data model (§V8-5)

Five tables in block **11** — `electricity_readings`, `_tariffs`, `_advances`, `_payments`, `_periods` — with `11001_electricity.sql` the module's only migration and **no seed source**, unlike v6 and v7. No settings table either: the three things one would hold are all date-versioned entities, because in this module "the current value" is never the whole truth.

Units are enforced by the column names: energy is **INTEGER tenths of kWh** (`*_dkwh`), money is **INTEGER haléře** (`*_haler`). Neither alternative works — a ceník price of 4 858,65 Kč/MWh would lose money as `finance`'s whole koruny, and a float would lose determinism in a formula whose entire point is that two people can reproduce it (D148). The form takes **whole kWh**, because Karel's meter has no decimal, while the storage keeps the tenth so a future meter needs no migration.

Two things the schema refuses. A register that would **decrease** is a 422 naming the offending neighbour, checked against both sides so a back-filled reading is validated too — with výměna elektroměru out of scope (D150) a falling counter is always a typo. And a settlement period never **locks** (D139): the vyúčtování is four recorded figures, including the supplier's own final meter values (D154), so a discrepancy can be attributed to kWh rather than only to Kč.

### Open questions — resolved 2026-08-20

Eleven, all closed before a line of PRD was written. Whole kWh, no decimal · a month counts if the period contains its 1st · the real ceník (4 858,65 / 4 026,69 Kč/MWh, 642,35 Kč/měs, záloha 1 500) · **the period end is unknown, so it is an expected date** · a period requires a reading on its start date · the vyúčtování stores the supplier's readings too · an in-app line is allowed, notifications are not · Přehled splits VT vs NT · no history to back-fill · `Elektřina` / `/elektrina` / `electricity` · record a due day. **None open** — but two blanks remain for the implementer to collect: the záloha's actual due day, and the supplier's real end date once stated.

### Four corrections the handoff pass forced back (D157–D160)

Writing `HANDOFF-10-electricity.md` against the frozen brief surfaced four places where the spec was silent or self-contradictory, recorded rather than fixed quietly: **a period with its closing reading is entirely actual** (the spec described only the mid-period case, so a finished period would have gone on forecasting an answer it already had) · **a displayed VT/NT split rounds VT and gives NT the remainder**, since D148 rounds once on the sum and two independent roundings would occasionally miss the headline by a haléř — the `needs` pattern from the fin split, reused · **"cost per month" is an allocation of exact interval costs**, without which D137 and the month-cost chart contradict each other outright · and **ceník delete is 409 only when it would leave a day inside a period unpriced**, where the earlier wording would have frozen every version ever used.

Two smaller gaps closed in place: a counted month is paid **at equality** (due on the 15th ⇒ paid on the 15th), and the worked example now pins **splatnost 15.**, because its doporučená záloha is 1 795 Kč at splatnost ≤ 20 and 1 796 Kč at splatnost 25 — a fixture that doesn't state the due day isn't reproducible.

### The day-one state is the primary screen

Karel's meter was installed on the period's first day and reads **32 kWh VT / 70 kWh NT**, with no second reading yet. So on day one there is no average, no forecast and no balance — and there will not be for weeks. Rather than a spinner or a zero, Přehled shows the figure that *is* computable with no consumption data at all: of the 1 500 Kč záloha, 642,35 Kč is poplatky, leaving **857,65 Kč/měsíc for energy — about 176 kWh if it were all VT, 213 kWh if all NT**. The empty state is specified as a designed screen and has its own acceptance criterion.

### Access

Ordinary all-roles module in the "více" overflow: `reader` reads, `editor`/`admin` write with CSRF, soft delete, and — unlike v7 — **no admin-only route at all**, since nothing here is irreversible enough to warrant one.

### Contract

OpenAPI **0.10.0**: **13 new paths** (106 → **119**) and **32 new schemas** (203 → **235**), one new shared parameter (`DateCursor`), one new tag. Validates against OpenAPI 3.1 with every `$ref` resolving.

**`DateCursor` generalises the `finance` month-key precedent (D149).** These collections are ordered by a natural chronological key — `read_on`, `effective_from`, `starts_on` — so a UUIDv7 cursor would misplace a back-filled row; a malformed cursor 422s rather than silently re-serving page one. Anything that "tidies" a `DateCursor` back into `$ref: Cursor` has broken paging.

**Amended 2026-08-20 by the implementation-planning pass (D161, D162)** — two places where 0.10.0 contradicted the brief it was written from, both fixed in place with no new path and no new schema. `ElectricitySummary.cost_total_haler` and `.balance_haler` are now **nullable and dropped from `required`**: as first written they forced a `0` onto the wire in exactly the state where the spec's own rule — *the module never shows a number it hasn't earned, not a zero* — matters most, leaving the honesty of the headline dependent on every screen remembering to gate on `status`. And `ElectricityHeadroom` gains **`kwh_mix_dkwh`**, the 30/70 figure Přehled leads with: the brief, the build guide and the design all pin ~200 kWh, but only the all-VT and all-NT numbers were published, and a client cannot recover the mix from those without reconstructing the prices. Both are recorded in the brief's new §7b rather than fixed quietly, on the D157–D160 precedent.

**Fixed in passing:** two enum lists in `openapi.yaml` had been stale since v6. `NotificationRule.filter_module` omitted **`finance` and `garden`**, so an admin composing a trigger rule could not qualify a finance or garden action key; `WidgetCatalogEntry.module` omitted the same two despite both shipping widgets. Both now list them — and `filter_module` gains `electricity`, while `WidgetCatalogEntry.module` deliberately does **not**, because v8 publishes no widget. Also observed and avoided: the v7 comma rule caught four fresh unquoted flow-mapping descriptions in the new schemas before they landed.

### As built — 2026-08-21, OpenAPI **0.10.1** (PR #17 `4d217c1`, plus #18 `afce38e` and #19 `81102af`)

Built in one pass and deployed with v7. `compute.go` was written and pinned by both worked fixtures **before a line of SQL existed**, and its purity is a test rather than a comment: no `database/sql`, no `net/http`, no `time.Now`. Mutation-checked twice — turning the forecast's single division into a rounded-average-times-*n* drifted the total by 19 haléře and failed four assertions; a real `float64` declaration failed the purity test. The five bans (widget, metric, list, push, blob) are enforced by `internal/arch`, and `platform/widgets/registry.tsx` is verifiably **absent from the diff** — an entry there for a module with no widget provider produces a dashboard tile that resolves to nothing: no compile error, no runtime error, an empty card.

**Seven decisions the build took (D169–D175).** A counted month's `due_on` always comes from the schedule's `due_day`, never from a payment's `paid_on` — the prototype's rule made an undated payment never count as due and quietly skewed the doporučená záloha (D169). `no_tariff` turned out not to be a blocking kind at all but `insufficient_data` plus a **`reason` enum on the wire** (D170). `forecast` is an all-zero span when `complete` and null when `blocked`, the opposite of what 0.10.0 said (D171). The summary serves `energy_total_haler`, `fee_total_haler` and `recommended_advance_kc`, and counted months carry `paid_on` + `due_clamped`, because a client re-deriving them lands on a different number than the server's own tests (D172). And the nudge line **escalates in words only** — at 15 and 90 days, never in colour or size — which closes the design addendum's one open question (D175).

**Two fixes shipped straight after the module.** PR #19 (D173): the log summary is the **one place the server formats money and energy**, because a summary is prose frozen at write time — a wrong unit is not a rendering bug a later deploy repairs, it is a wrong sentence sitting in the log forever. `kc()` now prints whole koruny with Czech non-breaking separators and a new `kc2()` the two-decimal form for unit prices and fees, mirroring the split already stated in the frontend's `format.ts`; `kwh()` gets the same grouping, so a five-digit register reads `12 345,6 kWh` in the log exactly as on Odečty. PR #18 (D174): **empty collections are arrays, and a failed request is not an empty log** — Go marshalled a nil slice as `null`, crashing a client that indexes into the log detail, while every view's `?? []` fallback rendered a 500 as "no records for this filter", the opposite of what happened. `AuditEventDetail.changes` is now required, and a Vitest file asserts each view's error branch beats its empty state, so reordering the ternary fails the build.

**The contract, reconciled.** 0.10.1 = **119 paths, 236 schemas, 6 parameters**. It also closes a defect predating v8: **`backend/openapi.yaml` in the repo had never left 0.8.0**, because neither the v7 nor the v8 build updated it — 0.9.0 and 0.10.0 lived only in `handoff/v7/` and `handoff/v8/`, so the served contract described neither `garden` nor `electricity`. Add "update `backend/openapi.yaml`" to the module checklist, beside the non-registry host maps.

**Two known defects, recorded not fixed.** The electricity collections use `limit` default **100** / ceiling **500** and do **not clamp**, where every other module uses 50/200 and clamps — documented through a module-local `ElectricityLimit` parameter marked ⚠ AS BUILT so nobody reads it as design. And a negative `invoiced_vt_dkwh` / `invoiced_nt_dkwh` reaches the table `CHECK` unvalidated and surfaces as a **500 instead of the 422 every sibling field returns**.

### Still to do

Karel owes three things the module cannot invent: the záloha's **`due_day`** (1–31, entered through the UI — there is no seed), the supplier's **real period end date** when it arrives, and a decision on **when the first záloha was paid** — červen 2026 does not count toward the period, so a June payment for this supply belongs to the record as `2026-07`. Do not "fix" that by changing the counted-months rule.

---

## v7 — 2026-08-18 (spec) · **deployed 2026-08-21** · Zahrada (garden)

> OpenAPI **0.8.0 → 0.9.0** (spec), shipped inside **0.10.1**. Decisions **D101–D132** (`PRD.md` §V7-10), plus **D163–D168 taken during the build** (`PRD.md` §V7-12). Triggered by one product addition from Karel: the kitchen garden. Scope was frozen the same day after a sixteen-question interview; the resolved brief is `V7-garden-brief.md`.

### Headline

A **ninth module, Zahrada (`garden`)** — the household's crop knowledge, its bed plan, and the work both imply. Four capabilities: a **two-level knowledge base** (druh → odrůda) with an LLM round trip to fill it, **beds and a per-season plan**, **checks that fire while you plan** rather than in July, and a **work calendar** generated from the plan and ticked off from Nástěnka.

Unlike Finance, the interesting part *is* the feature. This is the largest module home has gained — eleven tables against Finance's one — and almost all of its difficulty is in three questions that look small: what "sharing a bed" means, what happens to generated work when the plan changes, and what a check should say when it cannot run.

### The three answers that shape everything else

- **Sharing a bed is overlapping occupancy, not the calendar year (D107).** Spring špenát and autumn pórek in the same bed never meet, and a companion check that flags them is a check nobody reads by April. Occupancy is derived — first of (sowed, sown direct, transplanted) through cleared-or-harvest-end — and it is what every bed-level rule joins on.
- **Regeneration is conservative (D110).** Generated tasks carry a `generation_key`; a plan change may move an **open, unedited, generated** task and nothing else. `done`, `skipped` and `is_edited` are untouchable, and a generated task you delete leaves a tombstone so it cannot resurrect. A calendar that quietly undoes your work is a calendar you stop opening.
- **A check that cannot run must not look like one that passed (D120).** Rotation reads closed seasons only, and there is **no historical back-fill** — so on a fresh install C3/C8 return the explicit status `no_history` and the panel says *"rotaci zatím nelze zkontrolovat, chybí historie"*. The flagship warning does nothing in year one, and says so.

### The task engine stays in the module (D101)

The obvious idea — make garden work into `todo` cards or `events` occurrences, so everything lands in one list — was raised and rejected, and the reasoning is recorded because it will come back. §10 **D25/D28** forbid the import outright (`internal/arch` fails the build), so reuse would mean a new platform-level task contract, `source_module`/`source_key` columns on `cards`, and two-way completion sync: three packages changed to avoid changing one. Beyond the boundary, the **shape** doesn't fit — garden work is a *window* bound to a planting, and a card has neither — and the **volume** doesn't either, at 100–200 items per season. What is shared is the surfacing: one widget and the catalogs.

### The module sends no push (D113)

Karel's instruction was *"configurable in notifications settings in Admin module, do not reinvent the wheel"*, and taking it literally produced a better architecture than the one that was drafted. `garden` imports **no `platform/push`** and stores **no audience**. It publishes three things and stops: the metric `garden.frost_risk_tonight`, the list `garden.frost_sensitive_now`, and one idempotent `garden.frost_warning` audit event per night whose Czech summary already reads as a finished notification.

That leaves **both** v5 delivery mechanisms available, chosen in Administrace at runtime rather than in the spec: a **scheduled summary** conditioned `garden.frost_risk_tonight lte 2` — silent on every night there is nothing to say, exactly the `finance.missing_months gt 0` shape — or a **trigger rule** on the audit event, which fires the moment the poll flips. The module is agnostic; both work on day one.

### Timing is anchored, not dated (D102)

Every knowledge-base window is `{anchor, from, to}` with three anchors, mixable per window and per crop: `week` (ISO week, how Czech garden literature states it), `last_frost` and `first_frost` (days relative to the season's frost dates). When a season's frost dates move, frost-anchored windows move and week-anchored ones don't — which is the intent in both cases. Resolution runs against the season, never against "today", so a planting resolves identically whenever the page is loaded.

### The plan is a plan, not a tracker (D119)

Planned and actual dates are both recorded and **actuals never re-drive the plan**: sow two weeks late and the harvest window stays where February put it. Chosen over recompute-from-actuals because a self-reshuffling calendar is untrustworthy — with the compensating control that the drift is never silent. The planting detail states it in Czech (*"vyseto o 14 dní později, sklizeň v plánu beze změny"*) and offers one action, `POST …/shift-tasks`, which moves the remaining open work and marks it `is_edited` — after which regeneration leaves it alone permanently.

### Citizenship: what the module contributes

- **One widget**, `garden.prace` (all roles) — the next 30 days of work, overdue first then by week, each line carrying crop and bed code, ticked via the house 2000 ms hold. Empty state *"na zahradě je teď klid"*. **No second widget** (D123): harvest surfaces as a `harvest` task rather than as a card that is dead weight from November to May.
- **Six household metrics** (13 → 19) and **six lists** (10 → 16), of which two are **list-only** on the D100 precedent — `garden.harvest_ready` and `garden.frost_sensitive_now`. The four countable keys mirror their metrics by construction (D77).
- **Twelve audit actions**, with `garden_plant`, `garden_planting` and `garden_task` joining the field-diff set — "who moved the tomato transplant date and to what" is the question the Log exists to answer here.
- The metrics exist to be **conditions**, not decoration: `garden.plan_warnings gt 0` gates a February planning nudge that goes quiet once the plan is clean; `garden.beds_unplanned` is the March version of `finance.missing_months`.

### Access

An **ordinary all-roles module** in the "více" overflow, like Finance: reads for every member including `reader`, writes `editor`/`admin`. **Ticking a task off is an ordinary write (D124)** — no `reader` exception was created for it, which was considered and declined. Exactly **one admin-only route** exists in the module: re-opening a closed season, because that rewrites the rotation history the checks depend on.

### Data model (§V7-5)

**Eleven tables, migration block 10**, no change to any existing table, and — for the second time after Finance — **no blob storage** (D122: photos are out of scope, so the module holds no bytes). `garden_plants` · `garden_varieties` (sparse overrides, `NULL` = inherit) · `garden_beds` · `garden_seasons` · `garden_plantings` · `garden_tasks` · `garden_harvests` · `garden_storage_items` · `garden_rules` · `garden_warning_dismissals` · `garden_settings` · plus a `garden_weather_days` cache.

Two shapes worth flagging. **Permanents are plantings with `season_id IS NULL` (D106)** rather than a second table, so occupancy, warnings, tasks and harvests keep one code path and Trvalky is a filtered view. And **bed adjacency is inferred from lexorank order within a zone (D117)** — no neighbour table, no coordinates, no drawing surface: you drag beds into the order they physically stand in, and the adjacent-bed check becomes possible for free.

Built-in compatibility rules — the botanical families with their break years plus ~50–80 sourced Czech companion pairs — ship as **`10900_garden_seed.sql` in a separate embedded source, excluded from `testsupport`** (D115), the v6 seed pattern for the same reason: a module test whose database is pre-loaded with rules would let a check fixture pass for the wrong reason. Built-ins are marked, carry their `source`, and can be **disabled but not deleted** (D130).

### The one external dependency

A public forecast (Open-Meteo — free, keyless, no account) polled twice daily by v5's `platform/scheduler`, through **the version's only platform edit: a generic `RegisterJob(name, every, fn)` hook** on the existing ticker. Additive, and the alternative — a second ad-hoc ticker inside a feature module — is exactly what v5 created that package to avoid. It is **soft**: every failure is logged and swallowed, and the module degrades to manual frost dates with no user-visible error, because a forecast that didn't load is not something anyone can act on. Three env vars, all with working defaults — `HOME_GARDEN_WEATHER_ENABLED` / `_URL` / `_POLL_HOURS`. **Coordinates are not env vars** (D112): latitude/longitude/altitude live in `garden_settings` next to the frost dates they serve, because they are user data rather than secrets and a typo should not need a redeploy.

### Filling the knowledge base (D114, D126)

Forty crops of structured agronomy is a lot of typing, so the module generates a Czech LLM prompt embedding **the JSON schema produced by the importer's own validator** — one registry feeds the validator, the prompt's schema and `/api/garden/enums`, so a prompt cannot ask a model for a field the importer would reject. The answer is pasted back and **previewed** (`dry_run=true`): the resulting record, a field-level diff when it updates an existing crop, and an explicit list of fields that couldn't be mapped. Enum matching is lenient with Czech words but an unmappable enum is a `422` naming field and value, never a silent default. Applied rows record `source=llm` plus the model and are badged **"neověřeno"** until a human confirms them. `GET /api/garden/export` emits the importer's own shape, so an export re-imports.

### Open questions — resolved 2026-08-18

Sixteen, all closed before a line of PRD was written. **In:** harvest log · frost dates + live forecast · perennials and fruit trees · rotation history + copy-season · a produce-only storage log · a printable month of work. **Out:** seed inventory · a drawn garden map · photos of any kind · a general pantry · offline writes · bed sub-sections · auto-generated watering and weeding · green manure as a modelled crop · a second widget · historical back-fill. Model depth: **druh → odrůda**. Task engine: **garden-owned**. Frost delivery: **Administrace's**. Actuals: **planned windows stay put**. Scale target: **~15 beds, ~40 crops** — which is why the planner is one grid of bed cards with no virtualisation and no pagination controls. **None open.**

### Spec-time decisions beyond the frozen brief (D128–D132)

Writing the contract forced five: **seasons are addressed by `{year}`**, not a surrogate id — a knowing single-entity deviation, since the year is unique, immutable and user-visible and makes `/zahrada/plan/2027` map 1:1 onto the API · **`dry_run` is a shared preview idiom** across the import and season-copy, returning the real response shape plus a diff and persisting nothing · **built-in rules disable, never delete** · **task completion is idempotent**, mirroring `events`, because the 2000 ms hold can fire twice on a bad connection · and the **Czech UI vocabulary** is pinned in §V7-7 so pages, widget, metric labels and notification tokens say the same words.

### Contract

OpenAPI **0.9.0**: **34 new paths** (72 → **106**) and **79 new schemas** (124 → **203**), one new shared parameter (`GardenYear`), one new tag. Validates against OpenAPI 3.1 with every `$ref` resolving and no unused schema.

**Fixed in passing:** four inline flow-mapping descriptions in `openapi.yaml` contained **unquoted commas**, so YAML was parsing the tail of each as a stray null key rather than as description text — `FinanceRates.fun` / `.no_fun` ("(pooled, not per person)") and two `key:` descriptions mentioning `events.pripominky_today`. Latent rather than fatal, since JSON Schema tolerates unknown keywords and 0.8.0 still validated, but the text was silently truncated. All four are now quoted. **Rule going forward: any inline `{ … }` description containing a comma must be quoted.**

### As built — 2026-08-19/21, deployed 2026-08-21 (PR #16 `fd79fed`)

Eleven tables at block **10**, **34 routes / 61 operations**, the `garden.prace` widget, six metrics, six lists, **31 audit actions** (not the twelve the spec bullet claimed) and the 82-rule seed at block `10900`. Built in the mandated order — the four pure functions first, no DB and no HTTP: `timing.go` (anchored windows and the ISO week-53 clamp) → `resolve.go` (species+variety through one shared `PlantCore`, so the mirror is structural rather than hand-maintained) → `occupancy.go` → `check.go` (C1–C11, pure over a loaded snapshot). `TestForbiddenPlatformImports` was proved by a deliberate violation: the module cannot import `platform/push` or `platform/blobstore` without failing the build, so the frost alert really is composed entirely in Administrace over the published metric, list and nightly audit event.

**Six decisions the build took (D163–D168).** `by_family` on the season copy is **accepted but inert** — the family-anchored shift collapsed A1 and A3 onto A2, because a shift is only meaningful relative to where a planting actually was (D163). A planting's **crop cannot be changed** by `PATCH`; that is a delete-and-recreate, and the spec's "the crop" among the regeneration triggers was wrong (D164). Dismissing a warning returns **200 + the recomputed check**, not 201 + the dismissal row, because a dismissal can change sibling warnings and the caller always re-renders anyway (D165). A harvest quantity is **strictly positive** — a zero row reads as "harvested, nothing came" and poisons `yield_actual` (D166). A season is closed **only by its own action** (D167). And the garden collections do not all follow the house paging contract: beds and seasons are **unpaged**, `/tasks` keysets on an opaque composite, and the filter is `year`, not `season` (D168).

**What the spec had wrong, now corrected in place.** Seven endpoints documented filters that were never implemented, `GardenVariety.effective` is its own shape (`GardenEffective`, not a `GardenPlant`), fourteen `GardenPlantCore` properties serialise as explicit `null` rather than being absent, occupancy reads more fields than §V7-5 listed — including suppressing `sowed_on` when the sowing was indoors — and `/tasks` is ordered `window_from, position, id` with no overdue-first term. The **catalog keys are not the ones the prose claimed** either: `garden.harvest_season` and `garden.frost_risk_tonight` are metric-only, `garden.harvest_ready` and `garden.frost_sensitive_now` list-only.

**One gap, recorded not fixed: the D126 export↔import round trip did not ship.** The export emits plants, varieties and rules; the importer accepts crops only, so feeding an export back is refused. The export is a **superset** of what import understands, and both the spec prose and the acceptance criterion now say so instead of promising the round trip.

**`backend/openapi.yaml` was never updated by this build** — it stayed at 0.8.0, with no `garden` paths at all, until the 2026-08-21 as-built pass folded 0.9.0 and 0.10.0 into **0.10.1**.

### Still to do

The **Playwright/axe pass at 375/1440 in both themes**, outstanding since v5. Teaching the importer varieties and rules would close D126 — roughly a day's work, not a line.

---

## v6 — 2026-08-17 (spec) · **deployed 2026-08-18** · Finance (finance) + the `fin` migration & retirement

> OpenAPI **0.7.0 → 0.8.0** — and **0.8.0 is also what shipped**, the first version of home whose spec and build agree on the contract. Decisions **D81–D98** in the spec round, **D99–D100** during the build (`PRD.md` §V6-13). Triggered by one product decision from Karel: the standalone `fin` service becomes a module of home, and then stops existing. **Deployed 2026-08-18 with `fin` still running** — the retirement is gated on a verification that has not been run yet.

### Headline

An **eighth module, Finance (`finance`)** — a functional clone of `fin.tilcer.cz`, the two-person household budget-split app that has been running as its own fe/be pair since June. v6 is the first version of home that **removes a live service**: it absorbs `fin`'s behaviour, migrates its months, verifies them row-for-row, and only then retires the original.

Unlike v3/v4/v5, the interesting part is not the feature. Finance is the simplest module home has — one table, one form, no scheduler, no blob store, no new platform strand, **no new environment variable**. The work is in the two things that must not go wrong: **the calculation must be identical**, and **the data must be provably intact before anything is switched off**.

### What crosses over, and what doesn't (D81–D83)

`fin` is a full service: its own Mode B session store, its own auth client, its own JWT plumbing, its own English React SPA. Almost none of that is worth carrying — home has a better version of each. **Two** things cross over:

- **The locked split formula (D82)**, verbatim, including the rounding order and the four reconciliation invariants, with `fin`'s worked-example test. It is the one thing in `fin` that took real work, and only because the *old* app it replaced had two contradictory implementations. The split stays **derived on read, never stored**.
- **The column vocabulary (D83)** — `income_kaja`, `income_andy`, four `rate_*` — literally. Only the table is namespaced (`finance_months`), because home is one database holding eight modules' tables. No generalisation to person A/B or N people: it would put a translation layer between the formula, the seed and the tests for no behavioural gain in a two-person household.

Everything else is home's: Czech UI (D85), home's session and roles (D84), **the audit spine** (D86 — `fin` had no audit trail at all, and it is what makes keeping `fin`'s hard delete safe, D87), `snake_case` + `PATCH` + `/api/finance/months` (D92), live sync (D94), PWA read caching (D95). `fin`'s import endpoint is **dropped** rather than ported — the seed replaces it, so no import API outlives the one import it existed for.

### Citizenship: what the module contributes (D88–D90)

- **One widget**, `finance.rozpocet` (narrow, all roles), with **two states**: the current month's headline split, or **"Zadat ⟨měsíc⟩"** when the current month has no row. The second state is the point — the app's real failure mode is a month nobody entered, and `fin` had no way to say so.
- **Four household metrics** (9 → 13) and **one list** (8 → 9, and 10 once D100 lands below), both including `finance.missing_months`, which mirror each other by construction (D77's rule).
- Composed with v5's conditions (D75), those turn the failure mode into a notification: a summary on day 1, conditioned on `finance.missing_months gt 0`, listing exactly what is missing — and silent in every month where nothing is.

### Access (D84)

Finance is an **ordinary module, not an admin one**: reads for every member including `reader`, writes `editor`/`admin` — **including delete, which has no separate admin tier** because there is no soft/hard distinction to gate. It sits in the **"více" overflow for everyone**, beside Dokumenty — a once-a-month destination does not earn one of the four thumb tabs. Stated plainly in the PRD: a `reader` therefore sees both household incomes, which is accepted for this household and is the first thing to reconsider if a `reader` account ever goes to somebody outside it.

### Data model (§V6-5)

One table, `finance_months`, migration block **09** — inputs only, no stored split, plain `UNIQUE(month)`. Delete is **hard**, as in `fin` (D87): no `deleted_at`, no `?hard=true`, no admin tier. A month is seven numbers that take twenty seconds to re-enter, and carrying a nullable column plus a filter on every read to protect it is not a trade worth making twelve times a year — **the audit spine is the compensating control**, with `month.delete` writing a full-row diff so the deleted numbers stay readable in the Log. `fin`'s table-level rate-sum CHECK is kept deliberately: it makes a bad *seed* row fail loudly at migration time instead of quietly at read time.

### The migration (D91)

The historic months arrive as a **one-off Goose seed** with `fin`'s ids and timestamps preserved — shipped in its **own migration source** (`finance/seed`, block `09900`) that the server entrypoint includes and **`testsupport` excludes**. Without that split every module test would run against a database pre-loaded with thirty months of real household finances. `bootstrap.MigrationFS()` stays schema-only; `MigrationFSWithSeed()` is the opt-in — the default is the safe one on purpose.

### The retirement (D96–D98)

Gated, ordered, and **not** collapsed into the deploy:

1. Home v6 goes live **with `fin` still running**.
2. **Verification** — row-for-row against `fin`'s live output, **including all nine recomputed split values**. Comparing inputs alone would not catch a mis-ported formula, which is the one mistake this migration can actually make. Any mismatch stops everything.
3. Retire in order, with **no redirect** (D96): tell both users the app moved → stop the backend and frontend after a final snapshot → **retain** the `fin/` R2 prefix as provenance → archive the repo → deprovision the auth site last. `fin.tilcer.cz` simply goes away; with two users who both know where it went, a redirect app running indefinitely was not worth the standing infrastructure. The trade-off to respect: that redirect would also have been the post-cutover fallback, so the verification in step 2 is now the only gate.

**D98** catches something easy to miss: `services/fin/` in the project folder is **empty**. `fin`'s PRD, OpenAPI spec and handoff exist only inside the repo about to become read-only, so they are recovered into the project record *before* archiving.

### Open questions — resolved 2026-08-17

OQ-V6-1 → **hard delete, as in `fin`** (D87 rewritten; the audit full-row diff is the compensating control) · OQ-V6-2 → **"více" overflow**, the four thumb tabs untouched · OQ-V6-3 → **no redirect**, `fin.tilcer.cz` switched off outright (D96 rewritten) · OQ-V6-4 → **accepted**, a `reader` sees both household incomes. **None open.**

### Design

The **v6 design addendum** was drafted the same day into `HANDOFF-design.md` §v6 (**approved, with the palette question resolved as Path A — see below**). Unusually for this project it briefs against a **working reference UI** — `fin`'s own React frontend — so the round is about what to carry across intact versus what must change to become a Home screen. It also carries one **computed** finding: Home's existing `--c1`…`--c5` categorical palette **fails** the data-viz CVD checks for this module's four buckets (`c2`↔`c3` green/amber at ΔE 4.4 protan and 12.3 normal-vision; `c1`↔`c4` blue/violet at ΔE 0.8 protan in every possible ordering). Two paths were specified — reorder to `c1,c2,c4,c3` plus mandatory secondary encoding, or re-step the tokens (a validated candidate is given, which also repaints the Log's stats bars). **Karel chose Path A on 2026-08-17.**

### As built (2026-08-17/18) — `PRD.md` §V6-13

Built in one pass (PR #14 `4f8a719`) and deployed 2026-08-18, with a follow-up (PR #15 `87cccdf`) carrying the version's only product change outside Finance.

- **D99 — the `pripominky` summary tokens follow the reminder's window, not the event's date.** `events.pripominky_today` / `_today_open` — metric **and** list, which must not part ways over a shared key — now resolve through the Připomínky widget's **own** selection: a "připomínka na dnešek" is a **current widget row** (lead open, day not yet passed), and every line is dated because the occurrence is no longer today's by construction. The old event-date reading answered a question nobody asks a reminder app — the rent due next Wednesday, whose 1w lead opened this morning, was exactly what the 08:00 summary left out. `events.due_within_7d` is deliberately untouched: a look-ahead is a question about the calendar.
- **D100 — `events.pripominky_active`**: the whole widget in words, overdue included, one line per event. **List-only** — the first key without a metric twin, because "how many rows are on the dashboard" is a number nobody asked for. Catalog totals after v6: **13 metrics, 10 lists**.
- **Two spec corrections.** D84/§V6-6's "hard delete `admin`" predated OQ-V6-1's resolution — with a hard delete there is nothing left to gate, so delete is an ordinary `editor`/`admin` write, as D87/FR-F5/the OpenAPI already said. And the finance keyset cursor is a **`YYYY-MM` month key, not a UUIDv7**: the collection orders by `month`, so the shared `Cursor` parameter would be compared lexically and silently return a wrong page; a bad cursor now 422s.
- **Four hardenings from a review pass:** `finance.missing_months` floored at 36 months · the in-row allocation bar `aria-hidden` (the row button's accessible name was four labels and four amounts) · the widget's "Zadat ⟨měsíc⟩" opens the add form · and the four **non-registry host maps** updated for `finance` (`inAppURL`, the widget registry, the "více" overflow, the Log's module filter — which also gained the `admin` and `platform` entries missing since v5).
- **Palette: Path A.** `--c1`…`--c5` unchanged; Finance's buckets are aliases (`--fin-personal/needs/fun/nofun` → `c1/c2/c4/c3`), so no new colour value enters the codebase. The all-pairs CVD weakness remains, so secondary encoding ships as mandatory: O/P/Z/N marks, per-bucket swatch shapes, 2 px gaps, an always-present legend, direct labels, and an `aria-label` per bucket.
- **The formula was verified before deployment, not after:** `TestComputeMatchesFinLiveExport` runs the port over the committed 15-row `fin` export and matches all nine split values for every month.
- **Retirement:** steps 1–3 done (export, seed, deploy), step 6 done (document recovery). **Steps 4–5 outstanding** — the live re-export through `v6-seed/verify_migration.py`, then the ordered switch-off with no redirect.

---

## v5 — 2026-08-16 (spec) · **deployed 2026-08-17** · Administrace (admin) + Web Push + PWA

> OpenAPI **0.5.0 → 0.6.0** (spec) **→ 0.7.0** (as built). Decisions **D51–D74** in the spec round, **D75–D80** during the build (`PRD.md` §V5-12). Triggered by one product addition from Karel: notifications.

### Headline

A **seventh module, Administrace (`admin`)** — admin-only, gated exactly like the Log browser and reached through the **"více"** overflow — that turns Home into a **Web Push sender**, plus the app-wide PWA groundwork the channel rides on. Unlike v3/v4 this is not only a feature module: it adds **five platform strands** (`push`, `scheduler`, `metrics`, `lists`, `pwa`) and an **outbox tailer** inside `platform/audit`. Auth, the dashboard-host contract, and the six existing modules are unchanged — `events` explicitly so (D68).

### What Administrace is (§V5-4, FR-P1–P5 / FR-S1 / FR-ADM1–6)

- **One shared push channel** (D52) — one service worker ⇒ one subscription per device, VAPID, every module sending through `platform/push.Send(envelope{module,type,title,body,url,tag,category,data})`. No per-module channel, no notification bus.
- **Subscription and consent are per-user, platform-owned** (D53) — **Nastavení → Oznámení** for every role including `reader`: permission + subscribe this device, a master switch and `broadcast`/`triggers`/`summaries` mutes (D53a), and a **self-test** (D78). An admin configures what is sent; an admin **cannot force-subscribe** anybody.
- **Three ways to send.** **(1) Broadcast** — ad-hoc to an audience, audited, recorded, not a persisted rule (D54). **(2) Trigger rules** — bind an **audit action key** to a push, default body = the event's Czech `summary`, overridable with a fixed safe token palette (D55/D61), with per-rule **coalescing** (default 60 s, `0` = every event, D57) and `exclude_actor` (default false, D66). **(3) Scheduled summaries** — wall-clock pushes composed from the **metrics catalog**, resolved **per recipient** (D59/D60).
- **`audit_events` is the transactional outbox** (D56) — a platform tailer reads it by UUIDv7 keyset with a persisted cursor (`audit_notify_cursor`), at-least-once and idempotent, fanning out to registered listeners; the `admin` listener reacts to todo/events/notes/documents alike **without importing any of them**, so the import-lint acceptance criterion (D28) stays true.
- **A scheduler exists** (D58) — a deliberate, scoped reversal of v1's "no scheduler": an in-process minute ticker, `Europe/Prague` and DST-correct, `last_fired` idempotency, catch-up only within 120 minutes (D58a), and a **day-of-month 1–31 with a clamp to the month's last day** in short months (D74, matching events' D19).

### PWA (§V5-1a, D67/D71/D72/D73)

Home becomes **installable** (manifest: `standalone`, dark, maskable icons) and **reads-only offline**: app-shell precache plus a **persisted TanStack Query cache** in IndexedDB, namespaced per user and cleared on logout. Deliberately **no service-worker `/api` cache** (it would leak across users), **no offline mutation queue, background sync, or conflict handling** — write controls simply disable offline. Login and document bytes stay online-only (D73). New builds activate **silently** (D72) — safe precisely because there is no write queue to skew.

### Data model (§V5-5)

- **Platform:** `push_subscriptions`, `notification_preferences`, `audit_notify_cursor`.
- **Admin:** `notification_rules`, `notification_schedules`, `notification_deliveries` — deliveries are **operational, not audit** (D64): 30-day default retention, dead endpoints (404/410) self-delete, continuously-failing subscriptions pruned after `HOME_NOTIF_MAX_FAILDAYS`.
- Migrations: platform gains `02002_push` + `02003_audit_cursor`; `admin` is a new `08001` block, applied last.
- **No new blob store**; the tables ride the existing Litestream `home/` replication (D65).

### API (openapi 0.5.0 → 0.6.0 → **0.7.0**, §V5-6)

- **Spec (0.6.0):** `/api/push/*` (vapid key, subscribe/unsubscribe, preferences) and `/api/admin/notifications/*` (broadcast, rules CRUD + test, schedules CRUD + test, deliveries, catalog); tags `push`, `admin-notifications`.
- **As built (0.7.0), the six post-spec additions (D75–D80):** **conditions** on rules *and* schedules (count-vs-number clauses, all/any, evaluated at send/fire time, **failing open**); **active hours** on trigger rules (wrapping `HH:MM` window, drops rather than queues); a **lists catalog** — the fourth registered catalog — with `{{list.…}}` body tokens naming the items behind a metric's number; **`POST /api/push/test`**; the **household member directory** on the catalog (with per-member device counts) and `user_label` on deliveries; and a **server-rendered Czech schedule `description`**, plus `url` path validation and `filter_module` qualification.

### Audit / security

Every admin configuration mutation is audited through the spine as usual; `admin` declares its `AuditActions()`. VAPID keys are **Coolify secrets** and only the public half is ever served (D65) — they are generated once with `cmd/vapidgen` and **never rotated**, since rotation invalidates every existing subscription. Subscription **endpoints are allowlisted** against known push services rather than trusted (D78). Audience resolution needs no new table: `all` = every user with a subscription, `roles` from home's **session role cache** (never client input), `users` by id; labels come from `sessions.display_name`.

### Frontend & nav (§V5-7)

New **Administrace** page (4 tabs: broadcast, triggers, schedules, deliveries) with the composer, audience picker and schedule builder; **Nastavení → Oznámení** for every role. Nav is unchanged in shape — Administrace joins Log in the admin-only part of **"více"**.

### Non-goals (v5)

No cross-service notification bus; no per-module push channels or subscriptions; no offline writes, sync queue or conflict resolution; no email/SMS fallback; no per-user reminder completion in `events` (D68 reverts OQ-7); no new "superadmin" role tier (D62); no lock-screen content restriction (D70).

### Relation to v4

v4 (Dokumenty) was the last spec-only version. **v5 closed the gap: v3, v4 and v5 were all built and deployed together**, so the live app went from v2's four modules to seven. The written masters lagged the build by four merged branches — `PRD.md` §V5-12 and `openapi.yaml` 0.7.0 are the reconciliation.

---

## v4 — 2026-08-11 (spec) · Dokumenty (documents) module

> OpenAPI **0.4.0 → 0.5.0**. Decisions **D39–D50** added; **D6** extended again, **D33** reaffirmed. Triggered by one product addition from Karel: a documents/file-storage module.

### Headline

A **sixth module, Dokumenty (`documents`)**, added on top of v3 as a self-contained package registered through the existing module registry — **no change** to Mode B auth, the dashboard-host contract, or the todo/events/logging/notes modules. It is the **first module with blob storage**: it owns a dedicated **Cloudflare R2 bucket** for file bytes alongside the Litestream-backed SQLite (which now holds only document *metadata*).

### What Dokumenty is (§1, §4 FR-DOC1–DOC11)

- **Upload / preview / download / delete files** — PDFs, images, and Office/other docs — in a **single-parent folder tree** (its own tree, isolated from Poznámky — D40), reusing Poznámky's slug/resolver/cycle-guard pattern (D31/D32) over separate data.
- **Blobs in a dedicated R2 bucket; metadata in SQLite** (D41). A document's bytes are **immutable / upload-once** — never replaced or overwritten; a changed file is a **new** document, and the old one can be deleted after confirmation (D50). No version history.
- **Permanent, id-based, household-only URL** (D42) — the permanent link is derived from the document's **immutable UUID** (`/d/{id}` → `/api/documents/{id}/raw`), **not** the slug path (which changes on rename/move, D32). Served by the backend, session-gated — **no public access, no share tokens** (D33 upheld).
- **Uploads through the backend** (D43) — multipart `POST /api/documents` streams to R2, sniffs MIME server-side, checksums (SHA-256), records metadata + audit in one transaction. Cap **50 MB**.
- **Preview** (D44) — PDFs/images/text preview natively; **Office → PDF** (spec said headless LibreOffice in-image; **shipped as a `home-gotenberg` sidecar** — see v5 §Relation), generated **once** async (immutable ⇒ derive-once, cache-forever) with `preview_status` transitions and a `/ws` push; failures degrade to download-only without losing the upload.
- **Search is filename + metadata** (FTS5 over title + filename + description, D46) — **not** file contents.
- **Two pin scopes** (D47, mirrors D35) — "pro všechny" (household, audited, editor+) and "jen pro mě" (personal, per-user, not audited).

### Dashboard (§4 FR-DOC11)

- New widget **`documents.pripnute`** (Připnuté dokumenty): household pins **∪** the caller's personal pins, de-duplicated (household precedence). A row **opens the document in a preview overlay on Nástěnka — the user never navigates away**; overlay actions reuse the documents endpoints with `meta.via="dashboard"`. No done gesture. No change to the dashboard-host contract — it's just another provider.

### Data model (§5)

- **New:** `document_folders` (self-ref parent, slug, lexorank — its own tree), `documents` (folder_id null=root, title/slug, original_filename, sniffed `content_type`, `byte_size`, `checksum`, `storage_key`, `preview_kind`/`preview_status`/`preview_key`/`thumbnail_key` — **no version columns**), `documents_fts` (FTS5 over title+filename+description), `document_pins` (partial unique indexes: one household per document, one personal per document per user).
- **Slug invariant:** unique across sibling document-folders+documents under one parent (`COALESCE` partial unique indexes + an app-level cross-check), exactly like Poznámky.
- **R2 object layout:** `documents/{id}/original` (write-once), `documents/{id}/preview.pdf`, `documents/{id}/thumb.webp` — **id-based keys** (renames/moves never touch R2).
- Per-module migrations extend the one Goose sequence: logging → platform → todo → events → notes → **documents**.

### Backup — the open architecture question, resolved (§8, D45)

- **Litestream cannot back up the R2 blob bucket** (it only replicates the SQLite WAL). So: document **metadata** rides Litestream (`home/`) as usual, and the **bucket** gets its own strategy — **object versioning** on the primary bucket (recovers deletes; bytes are immutable so there are no overwrites) **plus a scheduled server-side mirror to a second R2 bucket** (`HOME_DOCS_R2_BACKUP_BUCKET`). Periodic reconciliation flags orphaned objects / dangling rows. Fresh build: metadata from Litestream, bytes from R2. *(Answer: not Litestream — mirror + versioning.)*

### API (openapi 0.4.0 → 0.5.0, §6)

- **Added** under `/api/documents`: `tree`, `resolve?path=`, search `?q=`, **multipart upload**, document CRUD (PATCH = metadata only) + `move` + `pin`/unpin, the four content endpoints **`raw` / `download` / `preview` / `thumbnail`** (permanent, household-only, Range + checksum ETag + immutable cache), and `folders` CRUD + `move`. New `documents` tag and schemas (DocFolder(s), Document(s), DocumentDetail, DocumentsTree, DocResolveResult, DocumentUpload/Update/Move, DocumentPin, DocumentUrls, PripnuteDokumentyWidget/PinnedDocument). `documents.pripnute` added to `WidgetInstance.data` and the catalog `module` enum.
- **Unchanged:** auth, dashboard host, todo, events, logging, notes paths.

### Audit / real-time / security

- `document` and `document_folder` **join D6's key diff entities** — **metadata diffs only** (bytes are immutable, never diffed). Household pin/unpin audited; personal pins not. `/ws` now also pushes document/document-folder/household-pin changes and `document.preview_ready`/`_failed`.
- **Untrusted-content isolation** (D48): MIME sniffed server-side; originals default to attachment + `nosniff` + sandboxed CSP; inline previews only through safe viewers (PDF.js sandboxed iframe, `<img>`, escaped text); active types download-only.

### Frontend & nav (§7, D49)

- New **Dokumenty** destination (desktop tree+grid / mobile drill-down; upload + drag-drop; a standalone `DocumentView` reused by the dashboard overlay; pin UI; "Kopírovat odkaz" copies the permanent `/d/{id}` household link). **Regular members now have five destinations and also exceed the four-tab mobile ceiling** that only admins hit in v3, so the overflow/"více" pattern **generalizes to everyone**. Top item for the **v4 design addendum** (`HANDOFF-design.md`).

### Non-goals (v4)

No public/unauthenticated access or share tokens; no content replace/overwrite/version history (immutable, D41); no in-file full-text search or OCR (filename+metadata only, D46); no in-browser editing/annotation/e-sign; no third-party storage integrations; no per-document ACLs; Office preview limited to LibreOffice-convertible formats; >50 MB and streaming media out of scope — candidate future work.

### Relation to v3

v3 (Poznámky) is a spec addition on the live v2; v4 layers `documents` on top. Because the module is self-contained and additive, nothing in v1/v2/v3 changes to build it — the one new operational dependency is the R2 documents bucket(s) + headless LibreOffice in the runtime image.

---

## v3 — 2026-07-29 (spec) · Poznámky (notes) module

> OpenAPI **0.3.0 → 0.4.0**. Decisions **D30–D38** added; **D6** extended. Triggered by one product addition from Karel: a notes module.

### Headline

A **fifth module, Poznámky (`notes`)**, added on top of the live v2 as a self-contained package registered through the existing module registry — **no change** to Mode B auth, the dashboard-host contract, or the todo/events/logging modules. It's the first real exercise of "adding a module is adding a package that registers itself" (D25).

### What Poznámky is (§1, §4 FR-P1–P8)

- **Create / edit / view / delete Markdown notes.** The body is stored **once, as Markdown** (D30); the editor offers **WYSIWYG (default)** and a **raw-Markdown** toggle as two round-tripping views over that one source — no HTML or second copy is persisted.
- **Folders + subfolders**, a **single-parent tree** of arbitrary depth; a note lives at the **root or in exactly one folder** (D31). Lexorank sibling order.
- **Human-readable slug-path URLs** — every note and folder is addressable at `/poznamky/<folder>/…/<slug>`, slugs unique across sibling folders+notes so a path resolves to one item; canonical ops by stable id + a path→id resolver; **rename/move changes the URL with no redirects** (D32).
- **Sharing is in-app / household-only** (D33) — a link opens for logged-in members only; **no public access, share tokens, or public routes.**
- **Text + external links only** (D34) — no uploads/attachments, **no blob storage added.**
- **Two pin scopes** (D35) — **"pro všechny"** (household: shared, audited, editor+) and **"jen pro mě"** (personal: per-user view preference, any member incl. `reader`, not audited).
- **FTS5 search** over note title + body (D6-style, FR-P6).

### Dashboard (§4 FR-P8)

- New widget **`notes.pripnute`** (Připnuté poznámky): household pins **∪** the caller's personal pins, de-duplicated (household precedence). A row **opens the note in an overlay dialog on Nástěnka — the user never navigates away**; edits/unpin reuse the notes endpoints with `meta.via="dashboard"` (FR-D5). No change to the dashboard-host contract (FR-M2/D28) — it's just another provider.

### Data model (§5)

- **New:** `folders` (self-ref parent, slug, lexorank), `notes` (folder_id null=root, canonical `body_md`, slug), `notes_fts` (FTS5 over title+body), `note_pins` (scope household|personal, partial unique indexes: one household pin per note, one personal per note per user).
- **Slug invariant:** unique across sibling folders+notes under one parent (partial unique indexes + an app-level cross-check).
- Per-module migrations extend the one Goose sequence: logging → platform → todo → events → **notes**.

### API (openapi 0.3.0 → 0.4.0, §6)

- **Added** under `/api/notes`: `tree`, `resolve?path=`, search `?q=`, note CRUD + `move` + `pin`/unpin, and `folders` CRUD + `move`. New `notes` tag and schemas (Folder(s), Note(s), NotesTree, ResolveResult, PinState/PinRequest/NotePin, PripnutePoznamkyWidget/PinnedNote). `notes.pripnute` added to `WidgetInstance.data` and the catalog `module` enum.
- **Unchanged:** auth, dashboard host, todo, events, logging paths.

### Audit / real-time

- `note` and `folder` **join D6's key diff entities** (full-value diffs, truncate-with-expand). Household pin/unpin is audited; **personal pins are not** (a personal view preference). The audit log **is** the note's history — no separate versioning (D36). `/ws` now also pushes note/folder/household-pin changes.

### Frontend & nav (§7, D37)

- New **Poznámky** destination (desktop tree+pane / mobile drill-down; WYSIWYG↔Markdown editor; pin UI; "Kopírovat odkaz" household link). Regular members keep four thumb-reachable tabs; **admins now have five destinations and exceed the mobile ceiling**, so the app shell needs an **overflow / "více"** pattern for the admin-only Log. Top item for the **v3 design addendum** (`HANDOFF-design.md`).

### Non-goals (v3)

No public/unauthenticated sharing; no uploads/embedded media/blob storage; no slug redirects; no collaborative/simultaneous editing (last-write-wins, D38); no note tags/labels, no export (md/zip), no note-level ACLs — candidate future work.

### Relation to v2

v2 is **live** (deployed 2026-07-29). v3 is a **spec addition** layered on it; because the module is self-contained and additive, v2 continues to run unchanged until v3 is built.

---

## v2 — 2026-07-21 (spec) · self-hosted login, widget dashboard, modular architecture

> OpenAPI **0.2.0 → 0.3.0**. Decisions **D23–D29** added; **D2** and **D16** adjusted. Triggered by two product changes from Karel.

### Headline

Two structural shifts on top of the approved four-module v1:

1. **Auth: Mode A → Mode B.** Home now hosts its **own login UI** and owns its **own session**, verifying credentials against auth BE→BE instead of redirecting to `auth.tilcer.cz`.
2. **Nástěnka becomes a widget host.** Instead of two hardcoded lists, the dashboard is a per-user arrangement of **widgets** that modules provide; each user shows/hides, reorders, and resizes them. This forces the module boundaries to become real, so the whole codebase is restructured into a **compile-time modular monolith** with each module self-contained in `backend/` and `frontend/`.

### Auth (was Mode A, §3)

- **Home hosts login + logout only** (D23). The login form posts to home; home calls auth **`POST /internal/login`** as a service client, then creates its **own session** (cookie on `home.tilcer.cz`) and caches the user's identity + roles.
- **No JWT in the browser** — the earlier per-request `/introspect` verification (v1 D2) is replaced: home authorizes each request from its **own session**, and refreshes identity/roles periodically via auth **`POST /internal/token/mint`**. `/introspect` is no longer on home's hot path. *(D2 adjusted.)*
- **Password-only** in v1 (D23): no TOTP, no Google on home. If auth returns an MFA challenge, home degrades gracefully and points the user at auth-hosted login rather than building an MFA UI.
- **Users are admin-provisioned in auth** — no self-signup on home. **Password reset stays auth-hosted** (home links out). Google OAuth, if ever used, stays auth-hosted (redirect).
- **Home is now a Mode B consumer** of auth, so it needs its own **service client bound to site `home`** (as `fin` does) — used for `/internal/login` and `/internal/token/mint`, not just introspection.
- New session concerns: home owns a **session store** + cookie, **CSRF** (double-submit) on cookie-authenticated state-changing routes, and its **own revocation** (a disabled user keeps working until the next role refresh, ≤ the refresh interval).

### Dashboard → widget host (was FR-N1–N3, §4/§7)

- **Nástěnka is a widget host** (D24). It owns no feature data; it renders **widgets** contributed by modules and stores each user's **layout**.
- **Per-user layout is server-side** (D24): which widgets are visible, their order, and their size — persisted in home's DB and synced across devices. A user can **show/hide, reorder (drag), and resize** widgets (narrow/wide).
- **v1 widget catalog** (D27): **Právě dělám** (todo — cards in `kind=now` columns across boards), **Připomínky** (events — active reminders), **Tento měsíc** (events — look-ahead upcoming events). *(No admin log widget in v1.)*
- The old FR-N1 aggregation logic **moves into module-provided widget providers**; the host fans out to visible widgets in one request (D28). Mark-done still reuses the owning module's endpoints with `meta.via="dashboard"`, and the **2000 ms press-and-hold** (D22) is preserved inside the task/reminder widgets.
- **"Active items only" (v1 D16) relaxed:** widgets define their own scope, so *Tento měsíc* legitimately looks ahead. *(D16 adjusted.)*

### Architecture: compile-time modular monolith (D25, D28)

- **Strict module boundaries.** Each module is a **self-contained backend package** (its own routes, migrations, audit actions, and widget providers) and a **frontend folder** (its page + its widgets), wired through a **central registry**. One binary, one deploy — but modules don't import each other's internals.
- **Module code identifiers stay English** (D26, per the earlier D17): `logging`, `todo`, `events`, `dashboard`, `platform`/core. UI shows Czech (Úkoly, Okno, Nástěnka).
- **Per-module migrations** (D25): each module owns its migration files, run in one Goose sequence — replaces v1's single `0001_init`.
- **Cross-module data flows through the widget-provider contract only** (D28): the dashboard host calls a registered provider interface, never a module's tables. Modules communicate through defined interfaces, not shared DB access.

### Data model (§5)

- **New:** `sessions` (home's own session store — token hash, user, UA/IP, sliding expiry, revocation) and `user_dashboard_layout` (`user_id`, `widget_key`, `visible`, `position`, `size`).
- Events/todo/logging tables unchanged in shape; now created by **per-module migrations** rather than one init.

### API (openapi 0.2.0 → 0.3.0, §6)

- **Added:** `POST /api/auth/login`, `POST /api/auth/logout`, `GET /api/auth/session`.
- **Changed:** `GET /api/dashboard` now returns `{ layout, widgets[] }` (host fan-out), not two fixed lists. Added `GET /api/dashboard/catalog`, `PUT /api/dashboard/layout`, `GET /api/dashboard/widgets/{key}` (single-widget refresh).
- **Security schemes:** home **session cookie** + **CSRF header** on cookie-authenticated mutations; the browser no longer carries a bearer JWT.
- Todo/events/logging paths unchanged.

### Config (§9)

- **New/changed:** `HOME_AUTH_SERVICE_SECRET` now authenticates `/internal/login` + `/internal/token/mint` (not just introspect); `HOME_SESSION_TTL_DAYS` (own session sliding window, default 90); `HOME_ROLE_REFRESH_MINUTES` (how often home re-mints to refresh roles, default 15); `HOME_CSRF_*`. `AUTH_BASE_URL` now also the target of the "reset password" / MFA-fallback links.

### Design

- **New screens require a design addendum** (flagged in `HANDOFF-design.md` §v2): home-hosted **login/logout** screens, the **widget host** (grid, drag-reorder, resize, add/remove from a catalog), and the **empty/first-run** dashboard. The v1 prototype covered neither. Not blocking the backend build, but blocks the dashboard/login frontend.

### Migration from v1

v1 was a spec, never built — so there is **no data migration**. v2 supersedes v1 as the build target. Where v1 handoffs conflict with v2, **v2 wins**.

---

## v1 — 2026-07-21 (spec, superseded) · four modules, Mode A

Initial approved spec + design. Four modules (`logging`, `todo`, `events`, `dashboard`), Mode A auth (redirect to auth-hosted login), Nástěnka as a hardcoded two-list aggregator, single `0001_init`. Decisions D1–D22. Design prototype delivered and reviewed (five deviations; D22 press-and-hold adopted). Never implemented.
