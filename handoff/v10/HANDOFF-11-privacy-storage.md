# Home — v9: Soukromé položky a Úložiště (`notes` · `documents` · `admin` · `platform/storage`)

> **Read first:** root `CLAUDE.md`, then `PRD.md` **§V9-1…§V9-11** (decisions **D176–D215**), `openapi.yaml` **0.11.0**, the scope brief `V9-privacy-storage-brief.md`, and the design addendum `HANDOFF-design.md` **§v9**. Build guide for v9. Owner: Karel. Issued 2026-08-21.
>
> ⚠ **This is not a module build, and building it like one will produce a broken app.** `HANDOFF-5` through `HANDOFF-10` all had the same shape: a new package, a new migration block, a registry entry, four host maps, done. **v9 has no new module and no new migration block.** It alters four tables that have carried real household data since v3 and v4, and it invalidates an assumption that roughly forty existing call sites were written against:
>
> > **Until v9, every row in `notes` and `documents` was visible to every member.**
>
> **The new code is the easy half.** `platform/storage` and the Úložiště page are ordinary work — a day, maybe two. The risk is entirely in the seams, and PRD **§V9-4a — the leak table** — is the enumeration of them. **Twenty-three surfaces. Twenty-two deny, one grants.** Treat that table as the build checklist and as the test plan; it is written as a table rather than as prose precisely so it can be ticked off.
>
> ⚠ **It had eighteen rows until a review pass read the spec against the shipped code, which found three real holes in it** — the log's entity timeline returning full diffs for any id, the trigger template's `{{change.*}}` tokens rendering a private filename to every lock screen, and a `Cache-Control: immutable, max-age=31536000` that keeps a private document readable on a shared laptop for a year after the 404 ships. Those became D206–D213. **Assume the table is still short and add to it.**

> ---
>
> ## ⚠ BUILT AND DEPLOYED 2026-08-25 — this guide is now history, and it is wrong in nine places
>
> v9 shipped as PRs **#21 `bcd3c2f`** and **#22 `a7d7e17`**. **`PRD.md` §V9-12 is the as-built reconciliation and wins over everything below.** The guide is kept as issued, because a build guide rewritten after the fact stops being evidence of what was known when. Where it now misleads:
>
> - **D214 (the Litestream replica line) was DECLINED**, not built — §6.4's instruction to measure it is void. The app does not hold `LITESTREAM_*` credentials and deliberately still does not.
> - **`dbstat` IS available** (v1.54.0), probed **on first use** rather than at boot, caching only the positive answer.
> - **There are FOUR FTS5 indexes, not three** — `garden_plants_fts` was never counted — so **twenty** shadow rows, not fifteen. §6.2's numbers are stale.
> - **`/ws` was fixed, not named**: a private mutation publishes `{"private":"1"}` and drops the id. §3 row 15 understates what shipped.
> - **The log's entity timeline excludes rather than redacts** (§5's "two rules" is now three doors with two behaviours).
> - **One redaction phrase became four**, chosen on `entity_type`.
> - **The private-image rule gained a widening** — an image its owner embedded in a shared note they authored is readable. §3 row 7's flat "404 for a non-owner" is no longer the whole rule.
> - **Administrace shipped a two-level tab strip**, not §8's single six-tab row.
> - **`StorageBackup.last_mirrored_at` was dropped from the contract** with no stated reason.
>
> ⚠ **And §10's definition of done is NOT met.** Five criteria stand unticked, two by design (D214) and three by omission — the largest being that **the trigger-listener redaction has no test**, on the surface §3 row 15b calls out as the one the first draft missed. §V9-12 lists all five.
>
> ---

## The model in one paragraph

Four tables — `folders`, `notes`, `document_folders`, `documents` — gain `visibility` (`shared` | `private`) and `owner_id` (NULL ⇔ shared). A tree is addressed by its **root scope**, the pair `(visibility, owner_id)`, of which there are `1 + N` per module for `N` members. Reads of a private item are refused to everyone but the owner — **admins included, always 404, never 403**. The one asymmetry: an admin may **hard-delete** a foreign private item and may never read one. Publishing is **one-way and owner-only**; there is no unpublish. Audit events are written in full and **redacted at read time** by one function. Administrace gains a computed-on-read storage snapshot fed by a new registered catalog, `platform/storage`, and a purge screen that lists ids and sizes and nothing else.

## Build order

**Do not build this in the order the PRD reads.** The order below is chosen so that each step is testable before the next one can hide its bugs.

| # | Step | Why here |
|---|---|---|
| 1 | **The migrations, the four slug indexes, and the store's collision query** (§1) | Everything else is downstream of the index. Get it wrong and the bug surfaces months later, silently, when two people happen to pick the same name. |
| 2 | **The scope-aware store layer** in both modules (§2) | Every read path takes a scope before any handler knows what a scope is. |
| 3 | **The leak table, row by row** (§3) | With adversarial tests written **first** — see §7.1. This is the bulk of the work. |
| 4 | **Publish** (§4) | Needs the store and the slug re-derivation from steps 1–2. |
| 5 | **Redaction** in `platform/audit`, the log query and the listener (§5) | One function, three call sites, two rules. |
| 6 | **`platform/storage` + the Úložiště page** (§6) | Independent of 1–5; do it last because it is the part that cannot leak anything. |
| 7 | **The purge screen** (§6.5) | Needs the catalog from step 6 and the admin hard-delete asymmetry from step 3. |
| 8 | **Frontend** (§8) | |
| 9 | ⚠ **Update `backend/openapi.yaml` to 0.11.0 in the same PR** | The v7/v8 process lesson. Neither of those builds did it and the served contract sat three versions behind. |

---

## 1. Migrations — three files, no new block

`01002_private_meta.sql` (logging) · `06004_notes_private_scope.sql` (notes) · `07004_documents_private_scope.sql` (documents). **No new migration block**, because there is no new module — the schema-level statement of D176.

### 1.1 The two columns, four times

```sql
ALTER TABLE notes ADD COLUMN visibility TEXT NOT NULL DEFAULT 'shared';
ALTER TABLE notes ADD COLUMN owner_id   TEXT;
```

and the same on `folders`, `document_folders`, `documents`. The `DEFAULT 'shared'` **is** the migration of existing data (D200) — there is no backfill and no seed, and every row that exists on deploy day stays exactly as visible as it was.

⚠ **No `CHECK (visibility IN ('shared','private'))` on the added column, and no table-level pairing constraint.** SQLite cannot `ALTER TABLE … ADD CONSTRAINT`, and rebuilding these tables is not a routine migration: `notes` and `documents` carry an explicit `seq INTEGER PRIMARY KEY` **because** their FTS5 indexes are external-content and keyed on the rowid — `06001` says so in a comment. A rebuild renumbers rowids and desynchronises the search index, and the symptom is that search silently returns the wrong rows. The pairing (`shared ⇒ owner_id IS NULL`, `private ⇒ owner_id IS NOT NULL`) is a **service-level invariant** (D179), exactly as v8's meter monotonicity is (D148).

### 1.2 The four slug indexes — the most important lines in v9

Each module today dedupes root-level siblings with a `COALESCE(parent, '')` sentinel, because SQLite treats NULLs as distinct and a plain `UNIQUE(parent_id, slug)` would not constrain the root at all. **That sentinel now collides with itself.** Two members who each keep a private note called *Recepty* at their own root both key on `('', 'recepty')` — and the second one does **not** 409, which is worse: see the warning below §1.2.

```sql
DROP INDEX ux_notes_sibling_slug;
CREATE UNIQUE INDEX ux_notes_sibling_slug ON notes (
    COALESCE(folder_id, 'root:' || visibility || ':' || COALESCE(owner_id, '')), slug
) WHERE archived = 0;
```

Repeat for `ux_folders_sibling_slug` (on `parent_id`), `ux_docfolders_sibling_slug` (on `parent_id`) and `ux_documents_sibling_slug` (on `folder_id`). **Four indexes, not one.** The `'root:'` literal keeps the composite from colliding with a real UUIDv7 parent id, which cannot begin with those characters.

The cross-table half of the invariant — a folder and a note under one parent may not share a slug — has no single-index form and stays where it already is, in the write transaction. **Make it scope-aware there too**, or a private note will refuse a slug because a shared folder in a tree it has nothing to do with already uses it.

⚠ **And the indexes alone do not finish the job** (D210). `Store.SiblingSlugTaken` in both modules queries `COALESCE(parent_id,'') = ?` with **no visibility term**, and `freeSlug` loops on it appending `-2`, `-3`… So an un-scoped collision query does not raise a 409 — it hands the second member **`recepty-2`**, a slug that discloses a sibling they cannot see, and both requests succeed. **Scope `SiblingSlugTaken` in the same commit as the indexes**, and assert on the resulting *slug*: the diagnostic you would otherwise be watching for (an error) never appears.

⚠ **One more `COALESCE(…, '')` in the same family** (D203). `notes` has no `folder_id=root` sentinel at all — the handler passes the parameter straight through, and `Store.NoteSummariesInFolder` is `WHERE COALESCE(folder_id,'') = ?` with a nil pointer dereferencing to `''`, so *omitting* `folder_id` on `/api/notes` already means "root notes only". After v9 that predicate collapses **every scope's root into one bucket**. Add the scope term there, and add the sentinel `documents` already has.

### 1.3 Two lookup indexes and one expression index

```sql
CREATE INDEX idx_notes_owner_scope     ON notes     (owner_id, visibility) WHERE visibility = 'private';
CREATE INDEX idx_documents_owner_scope ON documents (owner_id, visibility) WHERE visibility = 'private';
```

and, in the logging block:

```sql
CREATE INDEX idx_events_private_owner
  ON audit_events (json_extract(meta, '$.owner_id'))
  WHERE json_extract(meta, '$.visibility') = 'private';
```

An **expression index** needs no rebuild, which is exactly why the redaction marker lives in the existing `meta` JSON instead of in two new columns on a table with an external-content FTS5 index of its own.

### 1.4 Down migrations

**Drop the indexes before the columns.** SQLite refuses to drop a column an index references, so the reverse order fails halfway and leaves the table wedged (D200).

### 1.5 Verify on restored production, not on an empty database

`ALTER TABLE ADD COLUMN` is cheap and safe; the risk is the FTS5 pairing. Restore a copy from Litestream, migrate it, and then assert that **a full-text search returns the same rows it returned before the migration**. An empty database cannot fail this test, which is why it must not be the only one you run.

---

## 2. The store layer takes a scope before anything else does

Add a small value type and thread it through, rather than passing two loose parameters that some call site will eventually swap:

```go
// Scope names one root of one module's tree. The zero value is the shared root,
// which is what makes every pre-v9 call site keep working unchanged.
type Scope struct {
    Private bool
    OwnerID string // set iff Private
}
```

**Every** store method that reads or writes a note, folder, document or document folder takes one. Resist the temptation to default it inside the store: a store that guesses a scope is a store that returns another member's rows when a handler forgets to set one, and it will fail open rather than closed.

The predicate is always the same pair of terms and belongs in one helper:

```sql
AND visibility = ? AND (owner_id IS ? OR owner_id = ?)   -- shared: 'shared', NULL; private: 'private', :uid
```

⚠ **For search it goes INSIDE the FTS join, never after it** (D184). A post-filter over an FTS result set still leaks: the caller learns how many rows matched before filtering, through `next_cursor` behaviour and short pages, even when the rows themselves are gone.

`owner_id` is set from `reqctx` at creation and is **never** read from a request body — the discipline v5's audience resolution already follows for roles.

---

## 3. The leak table, row by row

PRD §V9-4a lists all eighteen. Notes on the ones that are more than a `WHERE` clause:

**Rows 4–7 — 404, never 403, on GET *and* HEAD.** The four content endpoints are registered as `content(pattern, fn)` pairs precisely so HEAD is not a 405, which means the HEAD branch is live code and a HEAD-only oracle is a real oracle. `/d/{id}` renders the ordinary "nenalezeno" screen, not a permission error — a screen that says *"you may not see this"* is itself the disclosure.

**Row 7 — note images.** `note_images` gains no column (D204). `GET /api/notes/images/{id}` joins to the owning note and 404s for a non-owner. `idx_note_images_note` already supports the join.

**Row 8 — pins.** A household pin on a private item is a **422** with a Czech reason, not a 403: the caller has the role, the operation is meaningless. A personal pin by a non-owner never gets that far — the item 404s first.

**Row 9 — the widgets' `AllFolders`.** Both `pripnute.go` providers call `store.AllFolders(ctx, false)` to build breadcrumbs. The **pins** are per-caller, so nothing leaks today — but the call loads every member's private folders into memory next to a response, and the next person to put a folder name on a widget row will not know that. Scope it.

**Row 14 — the log query has more than two doors** (D209). `Browse` and `Get` are the obvious ones. Also: `GET /api/logs/entity/{type}/{id}` (`Store.Timeline`) returns `AuditEventDetail` **with full `changes`** for any id — and the purge screen hands admins ids by design; the `?entity_id=` filter matches the raw column, so N redacted rows still confirm the id exists (the D188 argument, and stronger, because an exact match beats a lexical one — **exclude, do not redact**); and `/api/logs/stats` counts private events into admin-visible dimension and bucket totals.

**Row 15b — the push carries more than a summary** (D207). `listener.send` builds `RenderContext{Event: &e, Changes: changes}` from the **raw** entry and renders **once, before `ResolveAudience`**. Two consequences: `{{change.<field>.new}}` — whitelisted by *shape*, so you cannot fix this by listing fields — renders a private filename or title into an envelope that goes to the whole household; and `URL: inAppURL(e)` returns `/d/{entity_id}`, naming the private id even when the text is clean. For a private event pass **empty `Changes`** and fall the URL back to `/dokumenty` / `/poznamky`. The v9 acceptance criteria test this on the **rendered envelope**, not on the summary.

**Row 19 — the HTTP cache header** (D208). `httpx.ImmutableContentCache` (`private, immutable, max-age=31536000`) is applied by `documents/content.go` to all four streams and by `notes/images.go` to the image endpoint. `private` excludes shared *proxies* — **not the other person using the same laptop** — and `immutable` suppresses revalidation entirely, so a private document stays readable from disk cache for a year after the 404 ships. The repo already names this threat model in `frontend/src/platform/pwa/persist.ts`; it had simply never reached the HTTP layer.

**The header becomes visibility-dependent: shared keeps `immutable`; private gets `private, no-cache, must-revalidate`.** Note that `no-cache` does **not** mean "do not cache" — it means "revalidate before every reuse", which is exactly what is wanted here: the ownership check runs on every view, a second member gets 404, and the owner's repeat view of a 30 MB PDF is a **304 rather than a full re-download**. `no-store` was considered and rejected as the sort of tax that gets a header quietly removed six months later. **Keep the `ETag` on both paths** — it is what makes the 304 possible. This failure is invisible from inside the app, so the test asserts the **header**, and asserts that a conditional re-request returns 304 to the owner and 404 to anyone else.

**Row 21 — `POST /api/notes/{id}/images`.** The write side of row 7: uploading *into* a foreign private note. 404, like everything else.

**Row 10 — the catalogs.** `notes.pinned_count` and `documents.pinned_count` are already `ScopePersonal`, so they should follow the pin rules with no change. **Assert it under two members with different private content rather than reasoning about it** — this is exactly the kind of "already correct" that turns out not to be.

**Row 15 — `/ws`.** Payloads are already id-only (`{"id": "..."}`) and the hub broadcasts one marshalled message to every client with no user scoping. Leave both alone (D190) — and **add a test asserting no title, name, slug or path is ever published to the hub from these two modules**, because D190 is only defensible while that property holds. If a future change puts a title in a ws payload, that test is what stops it.

**Row 16 — the jobs.** The documents mirror, the orphan reconciliation and the notes image GC are visibility-blind by design; they operate on keys and bytes. **Do not "improve" their log lines with titles.** A test asserts they read no title column.

**Row 18 — the asymmetry.** In the hard-delete handlers, the read check and the delete check are deliberately different: **reads require ownership; `?hard=true` requires `admin`, exactly as it does today.** Ownership grants no hard delete — that is unchanged from v3/v4 and v9 does not widen it. Write it as two explicit branches with a comment naming D181, because it reads like a bug otherwise and somebody will "fix" it. The same applies to the **folder** routes, which are what actually reclaim a private subtree (D212).

---

## 4. Publish

`POST /api/{notes,documents}/{id}/publish` and the two folder equivalents. In **one transaction**:

1. Load the item **as its owner**. ⚠ **A non-owner gets 404, not 403 — an admin included — byte-identical to an unknown id** (D206). This route is `RequireWrite`, so every `editor` can call it: a 403-for-private / 404-for-unknown pair is an existence oracle over the whole private tree, which is exactly what D180 closes on the read side. It is still true that this is the one place an admin has less power than the role implies; it simply must not be *observable* that way.
2. Set `visibility='shared'`, `owner_id=NULL`.
3. Reparent to `folder_id` (null ⇒ shared root); a destination outside the shared tree is a 422.
4. **Re-derive the slug** if it now collides in the new scope — the destination's siblings are different siblings.
5. For a **folder**, recurse over every descendant, folders and items alike. A partial publish is the one outcome this endpoint must never produce.
6. Write `*.publish` with the `visibility` field diff.

**The personal pin survives** (D183) — it is keyed on the item id and the item id does not change. **`/d/{id}` does not change either**: the R2 key is id-based and independent of folder, slug and scope (D42), so a published document keeps the URL it was shared with, which is the whole reason that URL was specified as permanent.

There is **no unpublish route**, and the absence is a decision (D182), not a gap. Do not add one because it "obviously pairs".

A `move` whose destination sits in the other scope is **422** (D186), not a silent publish.

---

## 5. Redaction — one function, three call sites, two rules

```go
// In platform/audit. The ONLY place a summary is withheld.
func Redact(e Entry, viewerUserID string) Entry
```

If `e.Meta["visibility"] == "private"` and `viewerUserID != e.Meta["owner_id"]`: replace `Summary` with the module's fixed Czech phrase, blank `EntityID`, set `Redacted = true`. The caller drops `changes`.

**Write side:** every existing `notes.*` and `documents.*` audit call gains `meta.visibility` and, when private, `meta.owner_id`. The summary itself is written **in full** — redacting at write time would redact the record for the person it belongs to, permanently, and the spine would stop being the owner's own history (D187).

**Read side, and the part that is easy to get wrong.** There are **two** rules, not one (D188):

- **Browsing** (`GET /api/logs`, `GET /api/logs/{id}`): private events are returned, **redacted**, with `redacted: true` and `changes: []` — `[]`, never null; D174 still holds.
- **Searching** (`?q=`): private events are **excluded from FTS matching entirely** for a non-owner. Redacting a hit is not enough — the hit itself tells the searcher that their term occurs in a private title, which is precisely the thing being protected.

**Push:** `admin/listener.go` renders `{{event.summary}}` from `Redact(e, "")` — the anonymous viewer — **once**, for the whole audience, the owner included (D189). A coalescing window builds one envelope per rule, not one per recipient; a second owner-only rendering would exist solely to carry the real title and would be one audience-resolution bug away from delivering it to the household.

A test asserts that **both** the log query and the listener route through `Redact`. Two implementations of a privacy rule is one implementation and one bug.

---

## 6. `platform/storage` — the fourth registered catalog

### 6.1 Shape

Build it the way §V5-12 **corrected** metrics and lists to be built: an optional `Source` interface plus a `*Registry` assembled at composition. **Never a package-level `Register` global** (D191).

```go
package storage

type Source interface {
    StorageTables() []string // the SQLite tables this module owns
}

// Optional second interface: only notes and documents implement it.
type BlobSource interface {
    StorageBlobs(ctx context.Context) ([]BlobUsage, error)
}

type BlobUsage struct {
    Prefix     string // "documents/" | "note-images/"
    Kind       string // "shared" | "private" | "unattributed"
    OwnerID    string // set iff Kind == "private"
    Objects    int64
    Bytes      int64
}

// Optional third: the purge screen's listing.
type PrivateInventory interface {
    PrivateItems(ctx context.Context, f ItemFilter) ([]Item, int64, error)
}
```

The platform sizes tables (it needs no idea what they mean); the **module** attributes bytes, because only `documents` knows that `documents/{id}/original` maps to `documents.created_by`, and only `notes` knows that `note-images/{id}` maps through `note_images.note_id` to its note's owner. `admin` reads the registry and **imports no module** — `internal/arch`'s `TestModulesDoNotImportEachOther` stays green.

### 6.2 The completeness test — the new trap, closed by machine

v9 touches **none** of the four non-registry host maps (D202). It opens a fifth surface instead: a module that ships a table and forgets to declare it becomes invisible on the storage page and the totals quietly stop adding up.

```go
// Enumerate sqlite_master; every user table must be declared by exactly one module
// or listed in platform's own set. A table with no home FAILS THE BUILD.
```

⚠ **Ship the allow-list with it or the test red-lights on day one** (D211). Each external-content FTS5 table materialises **five** `type='table'` rows — `X`, `X_config`, `X_data`, `X_docsize`, `X_idx` — so `notes_fts`, `documents_fts` and `audit_events_fts` account for **fifteen**, of which three appear in a migration. Add `goose_db_version` too. **Attribute each shadow row to the module owning its parent** rather than dumping them into `platform`: they are often the largest b-trees in the file, and a storage page whose per-module totals do not sum to the exact database total has failed at its one job. A test that fails on the shipped schema gets deleted rather than fixed, which is how a guard like this dies.

Verify the test by **adding a throwaway table and watching the build go red**. A guard nobody has seen fail is a guard nobody knows works.

### 6.3 Measuring the database

- **Total** — `PRAGMA page_count` × `PRAGMA page_size`, plus the WAL file's size from `os.Stat`. Exact, always available, and the only figure on the page checkable against `ls`. `PRAGMA freelist_count` × page size gives `free_bytes` — what a `VACUUM` would reclaim.
- **Per table** — `SELECT name, SUM(pgsize), SUM(payload) FROM dbstat GROUP BY name`.

⚠ **`dbstat` is a compile-time option (`SQLITE_ENABLE_DBSTAT_VTAB`) and whether `modernc.org/sqlite` exposes it MUST BE PROBED AT BOOT, not assumed** (D193). Probe once with `SELECT 1 FROM dbstat LIMIT 1`, cache the boolean, and set `bytes_available` from it.

**Expect the probe to succeed.** *(Evidence gathered 2026-08-21: `modernc.org/libsqlite3` publishes the constant `DBSTAT_PAGE_PADDING_BYTES`, which in the SQLite amalgamation sits **inside** the `#if defined(SQLITE_ENABLE_DBSTAT_VTAB)` guard and is therefore only transpiled when that flag is set; the `DBPAGE_COLUMN_*` constants beside it point the same way. So `dbstat` is **very likely available** and the fallback is a safety net, not the expected path. It is inference from a package index rather than a read of the source, so **keep the probe** — and settle it in thirty seconds anywhere with Go and network: `SELECT count(*) FROM dbstat` against an in-memory DB.)* The fallback still has to be written and **exercised** — a failure branch that has never run is not a fallback — but do not design the page around the assumption that bytes will be missing.

**Where it is absent: row counts, `bytes: null`, and the page says so. NEVER an estimate** — a guessed byte figure on a page whose whole job is reporting byte figures is worse than an honest gap. **Record the answer in `PRD.md` §V9-12** when you find it; the next module needs it too.

### 6.4 Measuring object storage

`blobstore.List(ctx, prefix)` returns `[]ObjInfo` with sizes. Sum per prefix, then join back to SQLite by the id parsed out of the key, to bucket each object as **shared**, **private (owner)** or **unattributed** (D194).

Listing rather than summing `documents.byte_size` is deliberate twice over: it counts the **derived** objects (`preview.pdf`, `thumb.webp`) whose sizes are in no table, and it makes objects resolving to no live row **visible** rather than silently missing. That third bucket is the orphan backlog the mirror job already reconciles, surfaced for the first time.

**Two more prefixes, neither of them per-module.** The **Litestream replica** under `home/` (D214 — objects, bytes, `generations`, and the newest object's timestamp, which is the first time the app can answer *"is replication actually running?"* from inside itself) and the **backup mirror bucket** (D205). Both come from the same `blobstore.List`. ⚠ **Keep both out of the per-module sums.** Litestream replicates the whole file, so a replica attributed to a module is an invented number — and the page's arithmetic is its premise. Expect the replica to be **several times the size of the live database**: it holds many generations where `page_count × page_size` reports one. That is normal, and the API says so rather than leaving the reader to guess.

**The whole snapshot is computed on read behind a 60-second in-process cache** (`HOME_STORAGE_CACHE_SECONDS`; `?refresh=true` bypasses). No table, no scheduler job, no history (D195) — assert that against the migration set and the job registry, not just by reading the diff.

**A bucket outage returns 200 with `blobs.available:false` and the database figures intact.** It must never 500 and must never blank the page.

### 6.5 The purge screen

`GET /api/admin/storage/private-items` lists **id, module, kind, owner, size, dates** and nothing else — no title, filename, description, content type, preview, download or free-text search (D198). It is deliberately uncomfortable to look at; that discomfort is the design constraint, not an objection to it.

**The tab is always present, empty or not** (D215), and the empty state is written copy rather than a dash: what the screen is for, that it never shows titles, and that opening it is recorded. Hiding the tab until a foreign private item exists would hide the *screen*, not the *capability*.

**Deletion is not implemented here.** The SPA calls the owning module's existing route — `DELETE /api/documents/{id}?hard=true`, `DELETE /api/notes/{id}?hard=true` — so the audit action stays the module's and `admin` gains no delete path of its own.

**Opening the listing writes `admin.private_items.view`** — the only READ in Home that writes an audit event. It is the answer to "who looked", and it is not optional.

### 6.6 What `admin` still does not get

No metric, no list, no widget, no push, no scheduled summary, no threshold notification (D199). The v7 frost pattern would fit in an afternoon and is out of scope anyway, because it needs a daily job and a fired-today marker — the stored state §6.4 declines. **Considered, costed, deferred.**

---

## 7. Tests

### 7.1 Write the adversarial tests first

Every row of the leak table gets at least one test written **from the attacker's side**: a second member, and an admin who is not the owner. Assert **404 (not 403)**, an empty result, or a redacted field. A feature whose failure mode is silent needs tests that fail loudly, and tests written after the handler tend to assert what the handler does rather than what the rule says.

### 7.2 The named cases

- **The colliding name.** Two members, one name (*Recepty*), two private roots, all four tables. Both succeed, both slugs are `recepty`, neither 409s. **This is the case that fails if §1.2's index is copied unchanged**, and no single-user test reaches it.
- **The pairing invariant.** No write can produce shared-with-owner or private-without-owner; both are 422.
- **The publish.** Into a shared folder that already holds `smlouva` ⇒ `smlouva-2`, **`/d/{id}` unchanged**, personal pin intact, one `publish` event with the `visibility` diff. A folder publish is atomic; an induced mid-way failure publishes nothing.
- **The blind admin.** GET 404 · HEAD 404 · `/raw` 404 · `/d/{id}` 404 · `DELETE ?hard=true` 204, objects gone, audit carries `owner_id` + `by_admin`.
- **The log.** Non-owner sees the phrase, `redacted:true`, `changes: []`. Owner sees the real summary and both values. A `?q=` for a term occurring **only** in a private title: **no hits** for the non-owner, one for the owner.
- **The push.** Trigger rule, audience *all*, private upload ⇒ everyone including the owner receives the redacted text.
- **The migration.** Applied to a **restored** production copy: every pre-existing row `shared`/NULL, and FTS returns the same rows as before.
- **The forgotten table.** Add one, watch the build go red, remove it.
- **The catalogs.** No widget, no metric, no list, no push added by v9 — asserted against all four, exactly as v8 asserts it.

### 7.3 What a green suite still does not prove

The v8 build found six frontend bugs by **looking at the running app** that no test would have caught. The equivalent here is worse, because the failure is invisible rather than ugly: **log in as the second member and click through both trees, both searches, the pinned widgets, the log browser and a real push.** A leak looks exactly like a working app to the person leaking.

---

## 8. Frontend

New code stays out of the legacy `src/routes/` placement where it can — but Poznámky and Dokumenty **live** there, and relocating them is still not this version's job (open since v6).

- **The root switcher** above both trees: *Poznámky* / *Soukromé poznámky*. It sets `?scope=` for every query the page makes. Which tree is on screen must be unmistakable at 375 px in both themes — the cost of getting it wrong is uploading something private into the household's tree, and there is no way back.
- **Route literals** `/poznamky/soukrome/…` and `/dokumenty/soukrome/…`, which makes `soukrome` a **reserved slug at both shared roots** (D185): a shared folder named *Soukromé* takes `soukrome-2`.
- **Lock language** for private items and **nothing else** — never for a disabled control or an admin gate.
- **Publikovat do sdílených** in the item's own menu, owner-only, with a dialog naming what changes and that it cannot be undone.
- **Administrace grows to six tabs** — Rozeslat · Pravidla · Souhrny · Doručení · **Úložiště** · **Soukromé položky** — past the point where a tab row works at 375 px, so adopt **v7's module tab-strip pattern** rather than inventing a second one.
- **Czech byte formatting** (`1,2 GB`, `847 MB`, `12 345 objektů`), with unmeasured figures visually distinct from measured ones — the v8 exact-vs-approximate treatment (`--el-approx`) promoted to a platform-level token.
- **Verify** that the persisted TanStack Query cache is per-user namespaced and cleared on logout (v5 D71/D73). It is now the one place a private title can outlive a session.

Query keys: add the scope to the notes/documents keys (`['notes','tree',scope]`, …) — **a shared cache key across two scopes is a leak that survives a logout**, and it is the single most likely frontend bug in this version.

**Two caches are already clean and need verifying, not fixing:** the persisted TanStack cache *is* per-user namespaced and cleared on logout, and the service worker deliberately never caches `/api`. **The third was not clean** — the HTTP response cache (§3, row 19), which is a backend fix.

---

## 9. Audit and security

- Five new actions: `notes.note.publish`, `notes.folder.publish`, `documents.document.publish`, `documents.document_folder.publish`, `admin.private_items.view`. ⚠ **Add all five to `admin/labels.go`'s `actionLabels`** (D213) — a hand-maintained map that falls back to the raw key, so without entries they show as `notes.note.publish` in the rule composer. It is not one of "the four" and never was, which is why it has been quietly degrading since v6.
- Every existing `notes.*` / `documents.*` action gains `meta.visibility` and, when private, `meta.owner_id`.
- **No new env secret.** Two new plain vars: `HOME_STORAGE_WARN_TOTAL_MB` (default **1024** — a change detector, not a bill detector: D196) and `HOME_STORAGE_CACHE_SECONDS` (default 60).
- **There is no configuration for privacy at all, and there must not be.** A privacy feature with a kill switch is a privacy feature whose guarantee depends on an environment variable nobody re-reads.

---

## 10. Definition of done

Everything in PRD **§V9-11**, plus:

- [ ] The leak table is ticked off row by row, each with a test written from the attacker's side.
- [ ] The colliding-name case passes on all four tables.
- [ ] The migration is verified on a **restored** production copy, with FTS asserted intact.
- [ ] `Redact` is the only place a summary is withheld, and a test proves both call sites route through it.
- [ ] The `sqlite_master` completeness test has been **seen to fail** on a throwaway table **and to pass on the shipped schema**, with FTS shadow rows attributed to their parents' modules.
- [ ] Per-module byte totals **sum to the exact database total**.
- [ ] Publish returns **404, not 403**, to a non-owner including an admin (D206).
- [ ] A trigger rule bodied `{{change.original_filename.new}}` leaks nothing, and its push URL is the module route (D207).
- [ ] A private document's `/raw` carries **`private, no-cache, must-revalidate`**; a shared one still carries the immutable header; a conditional re-request is **304 to the owner, 404 to anyone else** (D208).
- [ ] The storage snapshot carries the **Litestream replica** line, excluded from the per-module sums (D214).
- [ ] The log's entity timeline, `?entity_id=` filter and `/stats` obey the redaction rules (D209).
- [ ] `admin/labels.go` has Czech phrases for all five new actions (D213).
- [ ] `dbstat` availability under `modernc.org/sqlite` is **probed, answered, and recorded in `PRD.md` §V9-12** — and the fallback branch has been **exercised**, not merely written.
- [ ] **Soukromé položky is in the tab strip when empty**, with its explaining empty state (D215).
- [ ] The four non-registry host maps are untouched and the diff proves it.
- [ ] ⚠ **`backend/openapi.yaml` is at 0.11.0 in the same PR.**
- [ ] A second member's session has been clicked through by hand (§7.3).

---

## 11. Known limitations, taken knowingly

- **Two visibilities, not an ACL.** No per-person sharing, no share links, no groups. Adding one later means a join table and a scope on every read — the model here does not block it, but nothing here anticipates it either.
- **No unpublish** (D182).
- **Private is access-controlled, not encrypted.** An admin with the database file or the R2 credentials reads anything. The UI must not imply otherwise, and Czech copy that promises more than this is a bug.
- **The `/ws` residue** (D190): a UUID and a timing signal cross to every connected client. Named, not fixed — **and this decision expires the moment a `/ws` payload grows a title.**
- **`unattributed` objects are reported, not cleaned.** The page shows the orphan backlog; the mirror job still owns reconciling it.

## 12. Module packaging

Unchanged rules: each module owns its routes, migrations, audit actions and providers, and reaches another module only through a registered catalog — now four of them. `admin` imports neither `notes` nor `documents`, and the import-lint proves it. `platform/storage` is infra, like `db` and `ws`: it is not a feature module and owns no table of its own.
