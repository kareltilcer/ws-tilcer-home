# v9 brief — Soukromé položky a Úložiště (`notes` · `documents` · `admin`)

> **Status: RESOLVED, scope frozen 2026-08-21.** Twelve questions asked, twelve answered; nothing open. This is the brief the PRD's §V9 sections, `openapi.yaml` **0.11.0**, `HANDOFF-11-privacy-storage.md` and the `HANDOFF-design.md` **§v9** addendum are all written against. Where a later document contradicts this one, the later document wins and says so.
>
> **v9 is the first version of Home that adds no module.** Every release from v3 to v8 appended a module through the registry and left everything before it alone; that is why each of them could be specified as a self-contained delta. v9 cannot. It changes **three existing modules** (`notes`, `documents`, `admin`), adds **one platform strand** (`platform/storage`), and — for the first time — alters tables that have been carrying real household data since v3 and v4. The risk profile is inverted: almost none of the difficulty is in the new code, and almost all of it is in the seams where old code assumed something that stops being true.
>
> That assumption, stated once: **until v9, every row in `notes` and `documents` was visible to every member.** Roughly forty places in the backend and the frontend rely on it — the trees, the two searches, the resolver, the four content endpoints, the permalink, the two pinned widgets, the two metric providers, the one list provider, the audit spine, the log browser and its entity timeline, the trigger listener and its templates, the websocket fan-out, the HTTP cache headers and the mirror/GC jobs. Every one of them is a place where "private" can leak. The brief is organised around that fact.
>
> ⚠ **§4's leak table grew from eighteen rows to twenty-three under review**, and the five it gained are the ones nobody thinks of — the log's entity timeline, the trigger template's `change.*` tokens, a year-long `immutable` cache header, image upload into a foreign note, and the slug-collision suffix. **Treat twenty-three as a floor.**

---

## 1. What Karel asked for

Three sentences, in his words:

1. **Dokumenty** now has one root, "Dokumenty"; it should have two — **"Dokumenty"** and **"Soukromé dokumenty"**, where the private ones are visible only to the member who uploaded them.
2. **Poznámky** likewise: **"Poznámky"** and **"Soukromé poznámky"**.
3. **Administrace** should gain, beside the notification settings, **a storage-statistics page**: database statistics per module, and S3/R2 statistics per module and per user/shared.

Everything below is the resolution of what those three sentences leave open.

---

## 2. The twelve questions, and the answers

| # | Question | Answer |
|---|---|---|
| 1 | Who can see a private item besides its owner? | **Nobody — admins included.** An admin keeps exactly one power: **hard-delete**. Never a read. |
| 2 | Can an item move between the shared and private trees? | **Private → shared only.** A one-way *publish*. Nothing can be retracted from the household once shared. **Re-confirmed 2026-08-21** after the consequence was put plainly: nothing *currently* in either shared tree can ever be made private either, so anything already there that should be one person's is re-uploaded and deleted by hand. Karel: leave it one-way. |
| 3 | What does the admin-only Log browser show for a private mutation? | **The event is written in full and redacted at read time.** The owner sees everything; everyone else sees a fixed phrase and no field diffs. |
| 4 | What should the storage page do? | **Snapshot only** — computed on read, no history table, no job — **plus one warning threshold**. |
| 5 | What lives under the private root? | **A full private tree, per user** — arbitrarily deep subfolders, same folder CRUD, same emoji icons, same slugs, same move. |
| 6 | Can a private item be pinned to Nástěnka? | **Personal pin only.** A household pin is refused. The widget row carries a lock mark. |
| 7 | Who may publish a private item into the shared tree? | **The owner only** — including against admins, who cannot publish what they cannot read. |
| 8 | Does the storage page enforce anything? | **No.** One configurable warning threshold, nothing blocked, no quota, no new 413. |
| 9 | A private mutation can match an Administrace trigger rule. What is pushed? | **The redacted summary, to the whole resolved audience.** Nothing leaks; the household may learn that *something private happened*. **Re-confirmed 2026-08-21**, after D207 reduced the message further still — the tokens render empty and the URL falls back to the module route, so the push says very little indeed. Kept anyway: a rule should mean the same thing regardless of data the admin cannot see. |
| 10 | How much does an admin see of another member's private usage? | **Named totals, split shared vs private** — bytes and object counts only. |
| 11 | Where does the admin's hard-delete of a foreign private item live? | **A dedicated purge screen** under Administrace. |
| 12 | Search — one box or two? | **Scoped to the tree you are in.** A `scope` parameter can widen it deliberately; nothing widens it by accident. |

Two of these deserve their reasoning written down, because both were close calls and both will look wrong to someone reading only the outcome.

**Q9 — the push.** The alternatives were narrowing a private event's audience to its owner, or excluding private events from trigger rules altogether. Narrowing loses on a subtle point: a coalescing window builds **one envelope per rule**, not one per recipient, so an audience-narrowed private event would need a second rendering path that exists solely to carry the real title — and the day someone reorders the audience resolution, that path delivers it to the household. Excluding is safe but silently changes what a rule means depending on data the admin cannot see. The chosen answer keeps one rendering, one envelope and one audience, and pays for it with a message that says less than it could. **The owner is not exempt** — they too get the redacted text, and open the app to see the detail (§6.3).

**Q11 — the purge screen.** It is uncomfortably close to being the private-file browser this whole feature exists to prevent, and that discomfort is the design constraint rather than an objection to it. It lists **id, owner, kind, size, dates** and nothing else: no title, no filename, no content type, no preview, no download, no search. Opening it is itself an audited event. It exists because "an admin can hard-delete" is useless if nothing in the app can name the thing to delete.

---

## 3. The model

### 3.1 Two roots, not a flag

A private item is not a shared item with a checkbox. `folders`, `notes`, `document_folders` and `documents` each gain **two** columns:

- `visibility` — `'shared'` (default) or `'private'`
- `owner_id` — `NULL` when shared, the auth user id when private

and the invariant that ties them: **shared ⇒ `owner_id IS NULL`, private ⇒ `owner_id IS NOT NULL`.** A tree is then addressed by a *root scope* — `(visibility, owner_id)` — of which there are exactly `1 + N` per module for `N` members.

The per-item-flag alternative was specified and rejected: it puts folders of mixed visibility into a tree everyone shares, and every member then sees folders whose contents differ from what the folder says it holds. "Why is this folder empty for me?" is a question with no good answer, and it recurs forever.

### 3.2 The slug index is where the model is really enforced

Today both modules enforce sibling-slug uniqueness with the same shape:

```sql
CREATE UNIQUE INDEX ux_notes_sibling_slug
  ON notes (COALESCE(folder_id, ''), slug) WHERE archived = 0;
```

The `COALESCE(…, '')` exists because SQLite treats `NULL`s as distinct, so a plain `UNIQUE(folder_id, slug)` would **not** dedupe root-level siblings. That sentinel now collides with itself: two members each with a private note called *Recepty* at their own root both key on `('', 'recepty')`. ⚠ **The symptom is not the 409 you would expect** — `freeSlug` loops on `Store.SiblingSlugTaken` and appends a suffix, so the second member silently receives **`recepty-2`**, a slug that discloses a sibling they cannot see, and both requests succeed. So the sentinel has to carry the root scope:

```sql
COALESCE(parent_id, 'root:' || visibility || ':' || COALESCE(owner_id, ''))
```

This is the single most important line in the whole version, and it is four indexes, not one — `folders`, `notes`, `document_folders`, `documents`. **And four indexes are still not enough**: `Store.SiblingSlugTaken` carries its own un-scoped `COALESCE(parent_id,'') = ?`, so it moves in the same commit. Getting either wrong produces a bug that only appears when two different people happen to choose the same name — which in a household is *frequently*, which no single-user test will ever reach, and which announces itself as a slightly odd slug rather than as an error.

### 3.3 The pairing invariant is checked in the service, not by the table

SQLite cannot `ALTER TABLE … ADD CONSTRAINT`; a table-level CHECK requires rebuilding the table. Rebuilding these four is not a routine migration: `notes` and `documents` both carry an explicit `seq INTEGER PRIMARY KEY` **precisely because** their FTS5 indexes are external-content and keyed on the rowid, and the 06001 migration says so in a comment. A rebuild renumbers rowids and desynchronises the search index — the failure mode being that search silently returns the wrong rows.

So the pairing is a **service-level invariant**, the way v8's meter monotonicity is (D148 precedent), plus an expression index that makes the private rows cheap to find. Stated, tested, not rebuilt.

### 3.4 Publishing is one-way, and it is a move

`POST /api/notes/{id}/publish` and `POST /api/documents/{id}/publish` (and the two folder equivalents) take an optional destination `folder_id` in the shared tree, set `visibility='shared'`, clear `owner_id`, re-derive the slug if it now collides, and — for a folder — do the same for **every descendant** in one transaction. It is audited as its own action with a field diff, because "this became visible to the household" is the single most consequential thing that can happen to a private item.

⚠ **A non-owner — an admin included — gets 404, not 403**, byte-identical to an unknown id. The route is open to every `editor`, so a 403-for-private / 404-for-unknown pair would answer *"does this id exist, and is it private?"* for any id: §6.1's oracle with a different verb. This was specified as 403 in the brief's first draft and in four other documents, and it is the single worst defect the review pass found.

There is no `unpublish`. Not "not yet" — there is no route, and the absence is a decision: a document the household has been relying on for six months should not be able to vanish into one member's private tree, and a member who wants it back can re-upload it and delete the shared copy, leaving both facts in the audit log.

An ordinary `move` whose destination sits in the other scope is a **422**, not a silent publish.

---

## 4. Where private can leak — the list, which is not "the whole list"

This is the section to build from. Each row is a place where code written before v9 assumed universal visibility. It is the authoritative copy of PRD §V9-4a; the two must not drift.

| # | Surface | v9 rule |
|---|---|---|
| 1 | `GET /api/{notes,documents}/tree` | Returns one root scope, chosen by `?scope=`, default `shared`. Never both. |
| 2 | `GET /api/{notes,documents}` (list **and** `?q=` search) | Same scoping. FTS joins the base table, so the filter rides in the same query — not applied afterwards. |
| 3 | `GET /api/{notes,documents}/resolve` | Takes `?scope=`; a path is meaningless without one. |
| 4 | Detail by id (`/notes/{id}`, `/documents/{id}`, both folder gets) | **404** for a non-owner. Never 403 — a 403 confirms the id exists. |
| 5 | The four documents content endpoints (`/raw`, `/download`, `/preview`, `/thumbnail`) | 404 for a non-owner, on GET **and** HEAD. |
| 6 | `/d/{id}` permalink | Same 404, rendered as the ordinary "nenalezeno" screen. |
| 7 | `/api/notes/images/{id}` | An image inherits its owning note's visibility. 404 for a non-owner. |
| 8 | Pins | Household pin on a private item ⇒ **422**. Personal pin allowed for the owner only. |
| 9 | The two pinned widgets | Already per-caller; a private row carries a lock mark. A published item **keeps** its personal pin. |
| 10 | `notes.pinned_count` / `documents.pinned_count` metrics + lists | Already `ScopePersonal`, so they follow the pin rules with no change — verify, don't assume. |
| 11 | Audit write | Full summary and full diffs, plus `meta.visibility="private"` and `meta.owner_id`. |
| 12 | Log browser browsing | Redacted at read time for non-owners: fixed phrase, no `entity_id`, no `changes`. |
| 13 | Log browser `?q=` | Private events are **excluded from FTS matching** for non-owners — redacting a hit still reveals that the search term occurs in a private title. |
| 14 | Log **entity timeline**, the `?entity_id=` filter, and `/api/logs/stats` | The three doors the first draft missed. The timeline returns full `changes` for any id — and the purge screen hands admins ids by design. The filter's exact match confirms an id exists even when every row is redacted. `stats` counts private events into admin-visible totals. |
| 15a | The audit outbox → trigger rules → push **text** | Rendered **once**, from the redacted entry, for the whole audience including the owner. |
| 15b | The trigger template's `{{change.<field>}}` tokens and the push **URL** | Redacting `summary` is not enough: the render context is built from the raw entry, so a rule bodied `{{change.original_filename.new}}` delivers a private filename to every lock screen, and `inAppURL` names the private id in `/d/{id}`. Changes empty, URL falls back to the module route. |
| 16 | `/ws` fan-out | Payloads are already id-only and the hub has no per-user routing. Kept as is; the residual leak is named in §6.4 rather than fixed. |
| 17 | Documents mirror + orphan reconciliation, notes image GC | Visibility-blind by design — they operate on keys and bytes. Must stay that way, and must not start reading titles for a log line. |
| 18 | Hard delete | The one asymmetry: an `admin` may hard-delete a foreign private item and may never read one. |
| 19 | **HTTP response caching** | The four documents streams and `/api/notes/images/{id}` send `private, immutable, max-age=31536000` today. `private` excludes shared *proxies*, not the second person on the same laptop, and `immutable` skips revalidation for a year — so the 404 never runs. The header now depends on visibility: shared keeps `immutable`, private gets **`private, no-cache, must-revalidate`** — cached but revalidated, so the check runs every view and a repeat view is a 304, not a re-download. |
| 20 | Storage statistics + purge screen | Sizes, counts and owners. Never a title, filename or content type. |
| 21 | `POST /api/notes/{id}/images` | Uploading *into* a foreign private note — the write side of row 7. 404. |
| 22 | `idx_documents_checksum` | Dormant today; the anticipated "this file is already here" UI would be a cross-scope duplicate oracle. Named so it is scoped when built. |
| 23 | The slug-collision **suffix** | `freeSlug` silently appends `-2`, so an un-scoped collision query hands the second member `recepty-2` — a slug disclosing an invisible sibling. Four indexes are not enough; the store's query takes the scope too. |

**Twenty-two of the twenty-three rows are "deny".** Row 18 is the only place an admin gains anything, and it is the answer to Q1. ⚠ **The table was eighteen rows until a review pass read it against the shipped code.** Five more surfaced, three of them real holes. The lesson is in the number, not in the rows: **add to this table rather than trusting its length.**

---

## 5. Úložiště — the storage page

### 5.1 The catalog, because `admin` may not import a module

`admin` cannot ask `documents` how big it is: the import-lint in `internal/arch` fails the build on a cross-module import, and rightly. Home already has the answer to this shape of problem three times over — the widget catalog in `platform/registry`, plus `platform/metrics` and `platform/lists` (there is no `platform/widgets` Go package; `platform/widgets/registry.tsx` is a frontend file) — so v9 adds the fourth: **`platform/storage`**, an optional `Source` interface plus a `*Registry` built at composition, exactly like metrics and lists (never a package-level `Register` global — §V5-12's correction).

A module declares two things:

- **the SQLite tables it owns** — a plain `[]string`, so the platform can size them without knowing what they mean, **including the FTS5 shadow tables** (`X_config`, `X_data`, `X_docsize`, `X_idx`), which are five `sqlite_master` rows per virtual table and are typically among the largest b-trees in the file;
- **its attributed blob usage** — rows of `{prefix, owner_id, visibility, objects, bytes}`, which only the module can compute, because only the module knows that `documents/{id}/original` maps to `documents.created_by` and that `note-images/{id}` maps through `note_images.note_id` to the note's owner.

All ten feature modules declare tables (plus `platform`'s own four). **Two of them** also declare blobs. `admin` reads the registry and imports nothing.

### 5.2 The new trap, and the test that closes it

Every version so far has tripped on one of the **four non-registry host maps**. v9 does not touch any of them — no new module, no new nav entry, no new log-browser filter, no new `inAppURL` case, and the widget registry stays untouched for the second version running. Instead it creates a **fifth registration surface**: a module that ships a table and forgets to declare it becomes invisible on the storage page, and the page's totals quietly stop adding up.

That one is closable by machine, so it gets closed by machine: a test enumerates `sqlite_master` and asserts that **every user table is either declared by exactly one module or named in the platform's own list**. A new table with no home fails the build — and the allow-list must cover `goose_db_version` and the fifteen FTS5 shadow rows, or the test red-lights on day one and gets deleted rather than fixed.

⚠ **And "the four host maps" were never four.** `admin/labels.go`'s `actionLabels` is a hand-maintained `module.action → Czech phrase` table that falls back to the raw key, so v9's five new actions would appear as `notes.note.publish` in the rule composer; it has been silently degrading since v6. Counting it makes **six**, of which v9 edits two — `actionLabels`, and `inAppURL` in the opposite direction from usual: not a new case but a private-event fallback.

### 5.3 What the numbers actually are

- **Database total** — `page_count × page_size` plus the WAL file's size on disk. Exact, always available, and the only figure that can be checked against `ls`.
- **Per table** — from `dbstat`, which reports real page usage per b-tree. `dbstat` is a compile-time option (`SQLITE_ENABLE_DBSTAT_VTAB`) and **whether `modernc.org/sqlite` exposes it must be probed at boot, not assumed.** If it is absent, the page shows **row counts and no bytes**, and says so. It never estimates: a guessed byte figure on a page whose entire job is to report byte figures is worse than an honest gap. *(Evidence gathered 2026-08-21: `modernc.org/libsqlite3` publishes the constant `DBSTAT_PAGE_PADDING_BYTES`, which in the SQLite amalgamation sits **inside** the `#if defined(SQLITE_ENABLE_DBSTAT_VTAB)` guard and is therefore only transpiled when that flag is set; the `DBPAGE_COLUMN_*` constants beside it point the same way. So `dbstat` is **very likely available** and the fallback is a safety net, not the expected path. It is inference from a package index rather than a read of the source, so **keep the probe** — and settle it in thirty seconds anywhere with Go and network: `SELECT count(*) FROM dbstat` against an in-memory DB.)*
- **R2 per module** — from `blobstore.List(prefix)`, summed, then joined back to SQLite for attribution. Listing rather than summing `documents.byte_size` is deliberate: it counts the **derived** objects too (`preview.pdf`, `thumb.webp`), whose sizes are in no table, and it makes objects that resolve to no live row visible as **`nezařazené`** instead of silently missing. That number is the orphan backlog the mirror job already reconciles, surfaced for the first time.
- **The Litestream replica** — one line under the `home/` prefix: objects, bytes, generations, newest-object timestamp. *(Added with Karel 2026-08-21.)* Not per module and never will be — Litestream replicates the whole file — so it sits beside the breakdown rather than inside it. It is also the honest counterweight to the database figure, which reports one generation where the replica holds many, and `newest_at` is the first time the app can answer *"is replication actually running?"* from inside itself.
- **The backup mirror bucket** — one line, objects and bytes, when configured. Between it and the replica, the page finally accounts for the whole R2 bill.

### 5.4 Snapshot, cached, never stored

Everything above is computed **on read**. No sample table, no scheduler job, no history — Q4's answer, and consistent with v8's "everything is computed on read". A full bucket `List` on every page load is wasteful, though, so the snapshot sits behind a **60-second in-process cache** (`HOME_STORAGE_CACHE_SECONDS`, `0` disables it) with `?refresh=true` to bypass. A cache with a one-minute TTL is not state: nothing survives a restart, nothing needs a migration, and nothing can be wrong for longer than a minute.

### 5.5 The threshold

One environment variable, `HOME_STORAGE_WARN_TOTAL_MB`, default **1024** (1 GB). Above it the page enters a warning register and marks the largest contributors. Nothing is blocked, no upload ever fails because of it, and there is no per-user quota — Q8.

**The default is a change detector, not a bill detector** (settled with Karel 2026-08-21). R2's free allowance is 10 GB and household usage is expected to sit well under a gigabyte, so a threshold parked at the billing cliff would stay silent for years and teach nobody anything. At 1 GB the line fires when something has *changed* — a runaway preview job, an unusually large upload, a private tree growing faster than anyone expected — with nine-tenths of the free allowance still in hand. A smoke alarm, not an invoice.

### 5.6 What the page does **not** do

No history, no growth curve, no forecast, no per-user quota, no cleanup wizard, no "delete all previews", no bucket browser, no per-file listing on the statistics page itself. And, deliberately, **no metric and no scheduled summary** — see §6.5.

---

## 6. The five decisions most likely to be re-opened

**6.1 — Why 404 and not 403.** A 403 on `/api/documents/{id}` tells the caller the id exists, which turns the permalink route into an existence oracle over the whole private tree. 404 is the only answer that leaks nothing, and it must be the answer on **HEAD** as well, or the same oracle returns through the back door.

**6.2 — Why redact at read time rather than write a redacted summary.** Because the owner is entitled to their own history. A summary redacted at write time is redacted forever, including for the person it belongs to, and the audit spine stops being a record for them. The cost is that redaction must be applied at *every* read path, which is why it is exactly one function and why the log's `q=` gets its own rule (§4 rows 12–13).

**6.3 — Why the owner also gets a redacted push.** One envelope per rule, per coalescing window, for the whole audience. A second, owner-only rendering is one refactor away from being delivered to the wrong list. The owner loses a title on a lock screen and gains a guarantee.

**6.4 — Why `/ws` is not fixed.** The hub broadcasts one marshalled message to every connected client and has no user scoping at all. Payloads are already id-only (`{"id": "..."}`), so what crosses is a UUID and the timing of a change. Adding per-user routing to the hub is a platform change with its own failure modes, for a leak of one opaque identifier. **Named, not fixed** — and if `/ws` payloads ever grow a title, this decision expires with them.

**6.5 — Why no storage metric.** The house pattern for "warn me about a number" is v7's frost handling (D113): the module publishes a metric plus one idempotent audit event, and Administrace decides at runtime whether that becomes a scheduled summary or a trigger rule. It would fit here in an afternoon. It is out of scope anyway, because it needs a daily job and a "did I already fire today" marker — which is precisely the stored state §5.4 says this feature does not have. **Considered, costed, deferred**, so that whoever thinks of it next finds it already thought of.

---

## 7. Czech UI vocabulary (fixed)

| Concept (code, English) | Czech UI |
|---|---|
| Shared root — notes / documents | **Poznámky** / **Dokumenty** |
| Private root — notes / documents | **Soukromé poznámky** / **Soukromé dokumenty** |
| Visibility | **Viditelnost** |
| Shared item | **Sdílené** |
| Private item | **Soukromé** |
| Publish to the shared tree | **Publikovat do sdílených** |
| Owner | **Vlastník** |
| Redacted log entry | **Soukromá položka — podrobnosti skryty** |
| Storage page | **Úložiště** |
| Database | **Databáze** · per module = **podle modulu** |
| Object storage | **Objektové úložiště (R2)** |
| Shared vs private split | **Sdílené** / **Soukromé** |
| Unattributed objects | **Nezařazené** |
| Backup bucket | **Zálohovací bucket** |
| Warning threshold | **Varovný práh** |
| Purge screen | **Soukromé položky** |
| Hard delete | **Trvale smazat** |

Plural forms are needed for: *1 položka · 2 položky · 5 položek*; *1 soubor · 2 soubory · 5 souborů*; *1 objekt · 2 objekty · 5 objektů*; *1 tabulka · 2 tabulky · 5 tabulek*. **MB, GB, kB and R2 do not inflect.** Byte sizes are Czech-formatted with a space thousands separator and a comma decimal — `1,2 GB`, `847 MB`, `12 345 objektů`.

---

## 8. Worked cases the implementation must reproduce

1. **The colliding name.** Kaja creates a private note *Recepty* at her private root; Andy creates a private note *Recepty* at his. Both succeed, **both slugs are `recepty`**, and neither sees the other. ⚠ *This is the §3.2 index — but the symptom of getting it wrong is **silent, not a 409**: `freeSlug` loops and appends a suffix, so Andy quietly gets `recepty-2`, a slug that discloses a sibling he cannot see. Assert on the resulting slug, and scope the store's collision query alongside the four indexes — fixing the indexes alone does not fix this.*
2. **The publish.** Kaja publishes her private document *Smlouva* into shared `Dokumenty/Smlouvy`, where a document with slug `smlouva` already exists. It lands as `smlouva-2`, its permanent `/d/{id}` URL is **unchanged**, its personal pin survives, and one `documents.document.publish` event carries the `visibility` diff.
3. **The blind admin.** Andy is `admin`. `GET /api/documents/{kaja's private id}` → **404**. `GET /api/documents/{id}/raw` → **404**. `HEAD` → **404**. `DELETE …?hard=true` → **204**, the R2 objects go, and the audit event records the purge with the owner id and `by_admin`.
4. **The log.** Kaja edits a private note's title. Andy opens the Log: one row, *"Soukromá poznámka — podrobnosti skryty"*, no entity link, no diffs. Kaja opens the same row: the real title, both values of the change. Andy searches `q=` for a word that occurs only in that title: **no hits**. Kaja searches the same word: one hit.
5. **The push.** A trigger rule on `documents.document.create`, audience *all*. Kaja uploads a private document. Everyone including Kaja receives *"Soukromý dokument — podrobnosti skryty"*.
6. **The pin.** A household pin on a private note → **422**, naming the reason. A personal pin → 200, and the Nástěnka widget shows the row with a lock mark.
7. **The storage page.** Two members, one shared tree, two private trees, some previews and thumbnails, and one object left behind by a failed upload. The page reports: database total matching the file on disk; per-module tables; R2 per module split shared / Kaja / Andy; the orphan under **nezařazené**; the mirror bucket on its own line; and — with `HOME_STORAGE_WARN_TOTAL_MB` set below the total — the warning register with the largest contributor marked.
8. **The forgotten table.** A new migration adds a table and no module declares it. `go test ./...` fails, naming the table.

---

## 9. Out of scope, explicitly

- **Sharing with a specific person.** There are two visibilities, not an ACL. "Soukromé" means *mine*; "sdílené" means *the household*. No per-member grants, no share links, no groups.
- **Unpublishing** (§3.4).
- **Encryption at rest beyond what R2 and the droplet already provide.** Private means access-controlled, not encrypted-to-the-user; an admin with the database file can read anything, and the feature does not pretend otherwise.
- **Private items in any other module.** Úkoly, Okno, Finance, Zahrada and Elektřina are household-wide and stay that way.
- **A private-items browser for admins** beyond the purge screen's id/owner/kind/size listing.
- **Storage history, growth curves, forecasts, quotas or automatic cleanup** (§5.6).
- **A storage metric, list, widget or push** (§6.5).
- **Per-user routing on `/ws`** (§6.4).
- **Moving the legacy `routes/` screens into `src/modules/*`** — still open from v6, still not this version's job.

---

## 10. Settled — and the one item that belongs to the build

**All three are answered** (2026-08-21), recorded here so the reasoning survives the decision:

1. ~~A value for `HOME_STORAGE_WARN_TOTAL_MB`~~ → **1024 (1 GB)**, as a *change* detector rather than a bill detector (§5.5).
2. ~~Confirmation that existing content stays shared~~ → **yes, and the consequence is accepted.** v9 migrates every existing note, document and folder to `visibility='shared'` by column default and backfills nothing, and §3.4 provides no unpublish. Anything currently in Dokumenty or Poznámky that was only ever meant for one person must be **published in reverse by hand** — re-uploaded privately and deleted from the shared tree, leaving both facts in the audit log.
3. ~~Whether the purge screen shows while empty~~ → **always visible, with an explaining empty state** (D215). Hiding the tab would hide the *screen*, not the *capability*.

**One item remains, and it belongs to the build rather than to Karel:** whether `modernc.org/sqlite` exposes `dbstat` (§5.3). *(Evidence gathered 2026-08-21: `modernc.org/libsqlite3` publishes the constant `DBSTAT_PAGE_PADDING_BYTES`, which in the SQLite amalgamation sits **inside** the `#if defined(SQLITE_ENABLE_DBSTAT_VTAB)` guard and is therefore only transpiled when that flag is set; the `DBPAGE_COLUMN_*` constants beside it point the same way. So `dbstat` is **very likely available** and the fallback is a safety net, not the expected path. It is inference from a package index rather than a read of the source, so **keep the probe** — and settle it in thirty seconds anywhere with Go and network: `SELECT count(*) FROM dbstat` against an in-memory DB.)*
