# Home — Module 6: `documents` (Dokumenty)

> **Read first:** `HANDOFF.md` (foundation — module registry, Mode B auth/session, CSRF, `platform/{db,ws,audit}`, conventions), then PRD §1 (Architecture — Dokumenty), §4 **FR-DOC1–DOC11**, §5 documents tables + R2 layout, §6 documents endpoints, §7 Dokumenty, §8 (backup / preview / isolation NFRs), §9 config, and decisions **D39–D50** (D6 extended, D33 reaffirmed).
> **Depends on:** foundation (F1–F6) and the audit spine (`HANDOFF-1-logging.md`). **Blocks:** nothing — the live dashboard host picks up the `documents.pripnute` widget automatically through the registry (`HANDOFF-4`), no host change needed.
> **Scope:** file storage (PDF/image/Office/other) in a folder tree, blobs in a **dedicated R2 bucket** with metadata in SQLite, a **permanent id-based household-only URL**, in-browser **preview** (native + Office→PDF) and **download**, two-scope pinning, and the `documents.pripnute` Nástěnka widget.
> **v4:** a self-contained module per `HANDOFF.md` §3 (own routes/migrations/audit actions/widget, registered via the core; **no cross-module imports**). Auth is Mode B — authorization is from the home **session**; writes = `editor`/`admin`, with two documented exceptions (personal pin; §9 below). This is the **first module with blob storage** — it owns a dedicated R2 bucket alongside the Litestream-backed SQLite. The **v4 design is delivered** (`design/Home.dc.html`, `design/DocumentView.dc.html`) — build the UI against it, not from tokens alone.

## The model in one paragraph

A **document folder** holds subfolders and documents; folders nest arbitrarily, each with exactly one parent (or none = root) — its **own** tree, isolated from Poznámky (D40). A **document** lives at the root or in exactly one folder and represents **one uploaded file** whose **bytes are immutable** (D41): the file is written **once** to R2 and never replaced — to change content you upload a new document; the old one can be deleted after confirmation. SQLite stores only metadata (title, slug, filename, MIME, size, checksum, storage keys, preview state); the bytes live in R2. Every folder and document is addressable by a **human-readable slug path** (`/dokumenty/<folder>/…/<slug>`, D32), but the **permanent** link is **id-based** (`/d/{id}` → `/api/documents/{id}/raw`) because it never changes on rename/move (D42). All content endpoints are backend-served and **session-gated** — household-only, no public access (D33). A document can be **pinned** "pro všechny" (household — audited) or "jen pro mě" (personal — a per-user preference, not audited), and pinned documents surface in the `documents.pripnute` widget, whose rows open a **preview overlay** on Nástěnka.

**Every mutation below writes an audit event in the same transaction** (see `HANDOFF-1`) — **except personal pins** (§9/§10). Not repeated per requirement.

## 1. Data model (PRD §5)

All ids UUIDv7; `position` lexorank strings; soft delete default (`?hard=true`); timestamps UTC. **No version columns** — bytes are immutable (D41).

**document_folders** — `id` · `parent_id` NULL (self-ref FK→`document_folders.id`; NULL = root) · `name` · `slug` · `position` · `archived` BOOL DEFAULT false · `created_by` · `created_at` · `updated_at`. Index `(parent_id, position)`. Its **own** tree — do **not** share Poznámky's `folders` (D40).

**documents** — `id` · `folder_id` NULL (FK→`document_folders.id`; NULL = root/unfiled) · `title` (display, editable) · `slug` · `description` NULL (indexed for search) · `original_filename` · `content_type` (**server-sniffed** MIME, D48) · `byte_size` INTEGER · `checksum` TEXT (SHA-256 hex; the `/raw` `ETag`) · `storage_key` TEXT (R2 key of the original) · `preview_kind` TEXT CHECK(`native`,`pdf`,`none`) · `preview_status` TEXT CHECK(`pending`,`ready`,`failed`,`none`) DEFAULT `pending` · `preview_key` TEXT NULL (derived preview PDF) · `thumbnail_key` TEXT NULL · `position` · `archived` BOOL DEFAULT false · `created_by` · `created_at` · `updated_at`. Indexes `(folder_id, position)`, `(updated_at)`, `(checksum)`.

**document_pins** — `document_id` FK→`documents.id` CASCADE · `scope` CHECK(`household`,`personal`) · `user_id` NULL (NULL for household; the auth user id for personal) · `pinned_by` (actor) · `position` (lexorank, per scope/user) · `created_at`.

**documents_fts** — FTS5 virtual table over `title` + `original_filename` + `description`, kept in sync by triggers (mirror `notes_fts` in `HANDOFF-5`). Use `unicode61` + `remove_diacritics=2` so `smlouva` finds *Smlouva*. **File contents are not indexed** (D46).

### The uniqueness indexes — same as Poznámky, over the documents tables

**Sibling slug uniqueness (per table)** — SQLite treats `NULL` as distinct, so key on a coalesced parent over non-archived rows:

```sql
CREATE UNIQUE INDEX ux_docfolders_sibling_slug ON document_folders (COALESCE(parent_id,''), slug) WHERE archived = 0;
CREATE UNIQUE INDEX ux_documents_sibling_slug  ON documents        (COALESCE(folder_id,''), slug) WHERE archived = 0;
```

**Cross-table sibling uniqueness (the addressing invariant, D32/D40)** — a folder and a document under the **same parent** must not share a slug. No index spans two tables, so enforce it **in the write transaction**: before insert/rename/move, check *both* tables for a live row with the same parent scope and slug. This check + the two indexes together are the invariant — cover it with a test.

**Pin scopes (partial unique).**

```sql
CREATE UNIQUE INDEX ux_document_pins_household ON document_pins (document_id)          WHERE scope = 'household';
CREATE UNIQUE INDEX ux_document_pins_personal  ON document_pins (document_id, user_id) WHERE scope = 'personal';
```

### R2 object layout — id-based keys

Per document, in the primary documents bucket (`HOME_DOCS_R2_BUCKET`):

```
documents/{id}/original        # write-once, the uploaded bytes
documents/{id}/preview.pdf      # derived (only when preview_kind = "pdf")
documents/{id}/thumb.webp       # derived thumbnail (images + PDFs/Office first page)
```

Keys are **id-based** — independent of folder/slug — so **renames and moves never touch R2**. This bucket is **not** covered by Litestream; it is backed up by **object versioning + a scheduled mirror** (§13).

### Migrations & registration

This module ships its own Goose files. Insert `documents` into the core's one sequence **after `notes`, before `dashboard`**: `logging → platform → todo → events → notes → documents → dashboard`. **Nothing is seeded.** Must apply cleanly on an empty DB and after a Litestream restore (metadata restores from Litestream; bytes live in R2 — do not re-derive previews on restore, the `preview_key`/`thumbnail_key` already point at existing R2 objects). Register via `registry.Module`: routes, migrations, `AuditActions()` (§10), one `Widgets()` entry (§14).

## 2. Object storage — the `platform/blobstore` client

Add a small **S3-compatible object-store client to `platform/`** (infra, like `db`/`ws` — importable by anyone; only `documents` uses it in v4). Configure it from `HOME_DOCS_R2_*` (§16). It exposes exactly what the module needs, nothing that leaks R2 into feature logic:

```go
type BlobStore interface {
    Put(ctx, key string, r io.Reader, size int64, contentType string) error
    Get(ctx, key string, rng *ByteRange) (io.ReadCloser, ObjInfo, error)  // rng nil = whole object
    Delete(ctx, keys ...string) error
    Copy(ctx, srcKey, dstKey string, dst *BlobStore) error                // used by the mirror job
}
```

- Use the AWS SDK v2 S3 client pointed at the R2 endpoint (path-style, region `auto`). **Stream** — never buffer a whole object (up to 50 MB) in memory on the way in or out.
- **Immutability is a module invariant, not an R2 feature:** the module only ever `Put`s a given `{id}/original` key **once** and never overwrites it. There is no "replace" code path. (Bucket versioning is a backup safety net for deletes, not a replace mechanism — §13.)
- **Never hand a presigned URL to the browser on the permanent path.** Content is served *through* the backend (§5) so it stays session-gated and same-origin. (Presigned GET is allowed only as an internal optimization behind the backend if you ever need it — not in v4.)

## 3. Upload flow (FR-DOC1) — get the ordering right

`POST /api/documents` (`multipart/form-data`: `file`, optional `folder_id`, `title`, `description`). `editor`/`admin`, CSRF required. Steps, in order:

1. **Stream + limit.** Read the file part through a hard `MaxBytes` guard (`HOME_DOCS_MAX_UPLOAD_MB`, 50). Over the cap → `413`. Simultaneously **tee** into a SHA-256 hasher and a size counter.
2. **Sniff the MIME** from the leading bytes (`http.DetectContentType` / a sniffer) — **do not trust** the client's `Content-Type` (D48). If an allowlist is configured (`HOME_DOCS_ALLOWED_MIME`) and the sniffed type isn't in it → `415`.
3. **Put to R2 first.** `blob.Put("documents/{id}/original", …)`. If R2 fails, return `502` and **write nothing to the DB** — no half-committed document.
4. **Then the DB transaction:** insert the `documents` row (`content_type`=sniffed, `byte_size`, `checksum`, `storage_key`, derived `slug` via §6, `preview_kind`/`preview_status` per the type — see §4, `preview_status="pending"` for anything that needs a derived preview, else `"none"`/`"ready"` for natively-previewable types) **and** the `document.create` audit event, in **one** `*sql.Tx`. On slug conflict that can't auto-resolve → roll back, `409` (and best-effort delete the just-uploaded object to avoid an orphan).
5. **Enqueue preview generation** (§4) *after* commit. Respond `201` with the `Document` (permanent URLs; `preview_status:"pending"`).

**Orphan discipline:** the object is written before the row, so a crash between steps 3 and 4 can leave an orphan object with no row — the reconciliation job (§13) sweeps these. The inverse (row without object) must never happen; only commit the row after `Put` returns.

## 4. Preview & thumbnail worker (FR-DOC9) — async, once, cached

Preview generation runs **out of the request path** in an **in-process worker** (a bounded goroutine pool consuming a channel; enqueue = the document id). No external scheduler — this is event-driven (triggered by upload), consistent with the "no cron for domain logic" stance (D9); the only periodic job is the backup mirror (§13). Because bytes are immutable, **derive once and cache in R2 forever**.

Per `content_type`:

- **image/**\*** → `preview_kind="native"`; generate a scaled `thumb.webp` (e.g. `disintegration/imaging` or libvips). `preview_status="ready"` (native preview uses `/raw`).
- **application/pdf** → `preview_kind="native"`; generate a first-page `thumb.webp` (`pdftoppm` from poppler-utils, or pdfium). `preview_status="ready"`.
- **text/**\*** and **.md** → `preview_kind="native"`, no derived object, `preview_status="ready"` (rendered client-side).
- **Office (docx/xlsx/pptx/odt/…)** → convert to `preview.pdf` via **headless LibreOffice** (`soffice --headless --convert-to pdf --outdir <tmp> <in>`, bounded by `HOME_DOCS_PREVIEW_TIMEOUT_SEC`), upload it as `{id}/preview.pdf`, and thumbnail its first page. On success `preview_kind="pdf"`, `preview_status="ready"`; on timeout/failure `preview_status="failed"` (document stays **download-only**, never lost).
- **Everything else** → `preview_kind="none"`, `preview_status="none"` (download-only).

After each job, update the row's `preview_*` fields and **publish `/ws`** `document.preview_ready` (or `_failed`) so open views and the widget swap the skeleton for a thumbnail. `HOME_DOCS_PREVIEW_ENABLED=false` skips conversion (everything Office becomes download-only). Make the worker **idempotent** (re-running for an id is safe) and **crash-safe** (on boot, re-enqueue documents left in `preview_status="pending"`). Run LibreOffice in a temp dir per job; clean up.

## 5. Serving content (FR-DOC8) — the permanent, household-only endpoints

Four **read** endpoints stream from R2 through the backend, all **session-gated** (any authenticated member). These are the permanent URLs (D42) — stable for the document's life because the id and the bytes never change.

- **`GET /api/documents/{id}/raw`** — the original. `Content-Type` = the **sniffed** `content_type`. `Content-Disposition: inline` **only for safe types** (PDF, image/\*, text/plain, text/markdown); **`attachment` for everything else** (D48). Set `ETag: "<checksum>"`, `Cache-Control: private, immutable, max-age=31536000`, `Accept-Ranges: bytes`, `X-Content-Type-Options: nosniff`, and a restrictive `Content-Security-Policy: sandbox` (defence-in-depth for anything rendered in an `<iframe>`/`<embed>`). Honour **`Range`** (206) and **`If-None-Match`** (304).
- **`GET /api/documents/{id}/download`** — same bytes, always `Content-Disposition: attachment; filename="<original_filename>"` (RFC 5987-encode the Czech filename).
- **`GET /api/documents/{id}/preview`** — the best previewable representation: `raw` bytes for `preview_kind="native"`; the `preview.pdf` object for `preview_kind="pdf"`. `409` while `preview_status` is `pending`/`failed`; `204` for `preview_kind="none"`.
- **`GET /api/documents/{id}/thumbnail`** — `thumb.webp` (image/webp) when present, else `404`.

**Isolation (D48):** these routes serve untrusted user files from home's own origin, so the `nosniff` + `attachment`-for-active-types + sandboxed-CSP rules above are **security-critical, not cosmetic** — an uploaded `.html`/`.svg` must never execute in `home.tilcer.cz`. Comment them as such. (Hardening option deferred: a separate cookieless content subdomain with short-lived signed links.) Range streaming must not buffer the whole object; copy R2's body through with the requested range.

## 6. Slugs, URLs & the permanent link (FR-DOC5)

- **Derivation & collisions:** identical to Poznámky (`HANDOFF-5` §2) — fold Czech diacritics to ASCII, lowercase, spaces→`-`, drop punctuation, collapse `-`; empty → short id; on collision in the parent scope (either table) append `-2`, `-3`, … inside the same transaction as the cross-table check. For documents, derive the slug from `title` (which defaults to the filename sans extension).
- **The full path is computed, not stored** — a folder/document stores only its own slug; the path is built by walking `parent_id` to the root. **Moving a folder does NOT rewrite descendants** — only the moved item may need a fresh slug in its new parent. (Same rule and same test as `HANDOFF-5` §2.)
- **Resolver (`GET /api/documents/resolve?path=`):** split on `/`; walk from root matching each segment's slug among child **folders** for intermediate segments, and among child folders **or** documents for the final segment. Return `{type:"folder"|"document", id, slug_path}`; unmatched → `404`. **No redirects** (D32).
- **The permanent link is id-based (D42).** The SPA short route `/d/{id}` and the four `/api/documents/{id}/…` content endpoints are **stable for the document's life**. The slug path is navigation only — **do not** treat it as permanent, and **do not** build a slug-history/redirect table. "Kopírovat odkaz" copies `/d/{id}` (not the slug path).

## 7. Folders, move, ordering (FR-DOC3/4)

- **Folders CRUD** over `document_folders`: create under a parent (null=root); rename→new slug (URL changes); soft-delete default; a **non-empty** folder returns `409` + child count unless `?cascade=true` (soft-deletes the subtree, **each child logged**); `?hard=true` purges and is `admin`-gated **and purges every descendant document's R2 objects** (§8/D50).
- **Move** (`POST /api/documents/{id}/move`, `POST /api/documents/folders/{id}/move`): reparent (`folder_id`/`parent_id`, null=root) and/or reorder (`position`) in one call; re-derive the slug only if needed to stay unique in the new parent. **Folder-move cycle guard:** reject moving a folder into itself or a descendant → `422`, checked **before** any write (walk up from the target parent to root; if you pass the moved folder, reject). Moving never touches R2 (id-based keys).
- **Ordering** is lexorank (D4), exactly as `todo`/`notes` — insert between neighbours = one row; a move rewrites one row; handle the "200 inserts at one spot" degenerate case. Folders and documents order **independently** within a parent (separate tables); the browser interleaves them for display (a UI choice — see the design bundle: **list default, grid toggle**).

## 8. Endpoints (see `openapi.yaml` 0.5.0)

- **Documents:** `GET /api/documents` (list; `?q=` → FTS5 filename+metadata, `?folder_id=`, `?include_archived=`), `POST /api/documents` (multipart upload), `GET/PATCH/DELETE /api/documents/{id}` (**PATCH = metadata only**, never bytes), `POST /api/documents/{id}/move`, `POST /api/documents/{id}/pin` + `DELETE …/pin?scope=`.
- **Content (read, permanent, household-only):** `GET /api/documents/{id}/raw`, `/download`, `/preview`, `/thumbnail`.
- **Folders:** `POST /api/documents/folders`, `GET/PATCH/DELETE /api/documents/folders/{id}`, `POST /api/documents/folders/{id}/move`.
- **Tree & resolve:** `GET /api/documents/tree`, `GET /api/documents/resolve?path=`.

**Routing order:** register the static `/api/documents/tree`, `/api/documents/resolve`, `/api/documents/folders*` (and the `?q=` branch of `GET /api/documents`) **before** the parameterised `/api/documents/{id}`; the `{id}/raw|download|preview|thumbnail|move|pin` sub-routes register under it. Reads: any authenticated member. Writes: `editor`/`admin` (F4 middleware) — **except** a **personal** pin/unpin, allowed for any member incl. `reader` (§9). Every mutation needs the CSRF header. The content GETs are reads (no CSRF) but still session-gated; they're used as `<img>`/iframe/anchor targets, so they must accept the cookie (same-origin — fine).

### Behaviours worth calling out

- **`GET /api/documents/tree`** — the nav read model: the folder tree with each folder's child folders + documents as lightweight `DocumentSummary` nodes (id, title, slug, position, archived, `content_type`, `byte_size`, `preview_kind`/`preview_status`, `thumbnail_url`, and the caller's `pinned` `{household, personal}`) — **no bytes**. One bounded query set, **no N+1** over folders (load all folders + all document summaries, assemble in memory). `?include_archived` off by default.
- **`GET /api/documents/{id}`** — `DocumentDetail`: metadata incl. the breadcrumb `path[]`, `slug_path`, `preview_kind`/`preview_status`, the caller's `pinned` state, and the `urls` block (`permalink /d/{id}`, `raw`, `download`, `preview`, `thumbnail`).
- **`PATCH /api/documents/{id}`** — `title` (re-derives slug), `description`, `archived`. **Never the bytes** (D41) — there is no field for file content and no replace endpoint.
- **Delete** — soft by default (needs an explicit UI confirmation, §15/D50); `?hard=true` is `admin` and purges the row **and** its R2 objects (`original` + `preview.pdf` + `thumb.webp`).

## 9. Pinning — two scopes (FR-DOC10, D47) — identical semantics to notes

- **`POST /api/documents/{id}/pin { scope }`**, **`DELETE …/pin?scope=`**.
- **household ("pro všechny")** — shared mutation: `editor`/`admin`, **audited** (`document.pin`/`document.unpin`, entity `document`, `meta.scope="household"`), one per document, `/ws`-broadcast.
- **personal ("jen pro mě")** — per-user **view preference**: any authenticated member incl. `reader`, **not audited**, one per document per user, **no** `/ws` broadcast (invalidate that user's widget client-side only).
- Idempotent: re-pinning a scope is a no-op `200`; the partial indexes prevent duplicates. Enforce the reader exception **narrowly**: a personal pin/unpin is the *only* documents write a `reader` may make; every other documents mutation is `403` for a reader.

## 10. Audit (spine, `HANDOFF-1`)

- **Actions** (`AuditActions()` returns them for the log filter): `document.create`, `document.update`, `document.move`, `document.delete`, `document.pin`, `document.unpin`, `document_folder.create`, `document_folder.update`, `document_folder.move`, `document_folder.delete`. **Personal pins emit nothing.**
- **Entity types** `document` and `document_folder` **join D6's key-diff set** (PRD §10 D50). Diffs are **metadata only — bytes are immutable, never diffed.** For `document.update`: `title`, `slug`, `description`, `folder_id`, `position`, `archived`. `document.create` records `{original_filename, content_type, byte_size, checksum, folder_id, title}` (`null → new`). For `document_folder.update`: `name`, `slug`, `parent_id`, `position`, `archived`.
- Use distinct entity type names (`document`, `document_folder`) so the shared log browser doesn't conflate them with Poznámky's `note`/`folder`. Actor/request-id come from context, never from arguments. Cross-module: edits/unpin invoked from the dashboard overlay log under **`documents`** with `meta.via="dashboard"`.

## 11. Websocket (F5)

Publish `document`/`document_folder` changes and **household** pin/unpin so open document views, the tree, and the `documents.pripnute` widget update live. Publish **`document.preview_ready`** / **`document.preview_failed`** (payload: document id + new `preview_kind`/`preview_status`/`thumbnail_url`) so a just-uploaded item swaps its skeleton for a thumbnail without a manual refresh. **Personal pins are not broadcast.** Frontend applies via `setQueryData`/invalidation with refetch-on-focus fallback.

## 12. Search (FR-DOC7, D46)

`GET /api/documents?q=` runs `documents_fts MATCH` over **title + original_filename + description**, returns `DocumentSummary` items (with folder path for display), capped + keyset-paged, ordered by relevance or `updated_at` desc. Reads only. **File contents are not indexed** — no extraction/OCR in v4. Every query path (search + tree) must hit an index or the FTS table — no full scans.

## 13. Backup — versioning + mirror + reconciliation (D45) — the one Litestream can't do

Litestream replicates the **SQLite WAL** only; it **cannot** back up the R2 blob bucket. So:

1. **Metadata** — rides Litestream (`home/`) exactly like every other table. Nothing special.
2. **Object versioning** — enabled on the **primary** documents bucket (an **ops prerequisite**, set on the bucket, not in app code) so accidental deletes/hard-deletes are recoverable. (Bytes are immutable, so there are no overwrites to recover — versioning here is a delete safety net.)
3. **Mirror job** — a periodic, in-process worker (guarded by `HOME_DOCS_MIRROR_CRON`, default hourly) that syncs the primary bucket into `HOME_DOCS_R2_BACKUP_BUCKET` via `BlobStore.Copy` (list new/changed keys since the last run, copy the missing ones; objects are immutable so it's copy-if-absent, cheap). Acceptable alternative: an **`rclone sync` in a Coolify scheduled task** if you'd rather keep it out of the process — either is fine; document which.
4. **Reconciliation** — a low-frequency pass that flags **orphaned objects** (an `{id}/…` key with no live `documents` row — from an upload that crashed before commit, §3) and **dangling rows** (a row whose `original` object is missing). Orphans older than a grace window can be deleted; dangling rows are logged for investigation. Expose the counts on a log/health line.

**Fresh build:** Litestream restores the DB; the bytes are read from R2 (primary; fail over to the mirror if the primary is lost). **Do not** re-derive previews on restore — `preview_key`/`thumbnail_key` already point at surviving R2 objects.

## 14. The `documents.pripnute` widget provider (FR-DOC11)

This module contributes one dashboard widget through the `WidgetProvider` contract (`HANDOFF.md` §3). The host calls it; it never reads this module's tables from outside.

- **Key** `documents.pripnute`, title **"Připnuté dokumenty"**, default size **wide**, not admin-only.
- **`Data(ctx, user)`** returns **household pins ∪ the caller's personal pins**, **de-duplicated** (a document pinned both ways appears once, `scope="both"`, household precedence), household block first then personal, each ordered by pin `position`. Each row = `document_id`, `title`, `slug_path`, `scope` (`household|personal|both`), `content_type`, `byte_size`, `preview_kind`/`preview_status`, `thumbnail_url`, `updated_at`, `position`. Shape = `PinnedDocument` in `openapi.yaml`. One bounded query (join `document_pins`→`documents`, filter to household + this user's personal), no N+1.
- **Frontend widget component** (registered in the frontend widget registry by key): renders the rows (type icon / thumbnail + title + size + scope marker); **a row tap opens the document in a preview overlay on Nástěnka — it does NOT navigate to Dokumenty** (the explicit requirement). The overlay reuses `DocumentView` (preview + Stáhnout; rename/unpin for `editor`+). Overlay actions call the documents endpoints with `meta.via="dashboard"`. **No press-and-hold done gesture** — documents aren't completed.
- Publishes/consumes `/ws`: a household pin, a document change, or a `preview_ready` refreshes the widget via `['dashboard','widget','documents.pripnute']`.

## 15. Frontend — Dokumenty (design **delivered**: `design/Home.dc.html`, `design/DocumentView.dc.html`)

Build against the v4 design bundle (it covers Dokumenty in full). From the PRD + the bundle:

- **Route** `/dokumenty/*` = slug paths; resolve path→id on navigation, then work by id. **"Kopírovat odkaz"** copies the **permanent `/d/{id}`** link (not the slug path) and its toast/tooltip says **household-only + permanent (does not change on rename/move)** — do **not** reuse Poznámky's "changes on rename" caveat.
- **Layout:** desktop **folder-tree sidebar + documents pane**, pane **defaults to a list** (type icon, title, size, modified date, a `preview_status` chip, pin marker) with a **grid/thumbnail toggle**; mobile **drill-down** (folder → contents → document), list default, breadcrumb everywhere. Driven by `['documents','tree']`.
- **Upload:** a **"Nahrát dokument"** action + **drag-and-drop** (desktop) / picker (mobile); a **multi-file upload queue** (per-file progress, success, error); a client-side size (>50 MB) / type pre-check before `POST /api/documents`; on success the item appears `preview_status:"pending"` and swaps to a thumbnail on the `/ws` `preview_ready` push.
- **Viewer:** a **standalone `DocumentView`** (reused verbatim by the dashboard overlay) — preview through **safe viewers only** (D48): **PDF and Office→PDF** via a sandboxed PDF viewer (e.g. `react-pdf`/PDF.js in a sandboxed `<iframe>`), **images** via `<img>` (pinch-zoom on mobile), **text/Markdown** via the existing Markdown renderer / escaped text. **Download-only** types and the **preview-pending** / **preview-failed** states show a **type card** (icon + filename + **Stáhnout**), never a broken frame. **Stáhnout** always present.
- **Organise:** create/rename folders, rename/describe documents, move via **"Přesunout do…"** (drag + one-tap picker) into the documents tree; **every delete confirms first** (distinguish soft vs the admin hard-delete that purges R2); non-empty folder delete shows the cascade warning.
- **Pin control:** two scopes; `reader` sees only personal.
- **Query keys:** `['documents','tree']`, `['documents','detail',id]`, `['documents','resolve',path]`, `['documents','search',q]`, `['dashboard','widget','documents.pripnute']`. Content endpoints (`raw`/`preview`/`thumbnail`) are addressed as **URLs**, not query-cached. A document/folder/move mutation invalidates `['documents','tree']`; a **household** pin also invalidates `['dashboard']` + the `documents.pripnute` widget.
- **Nav (D49):** the sixth destination. Mobile app shell = **4-slot bottom bar (Nástěnka · Úkoly · Okno · Poznámky) + a "Více" sheet** holding **Dokumenty** (and, for admins, **Log**); desktop side-nav lists all six. (This supersedes the v3 admin-only overflow — build per the v4 design bundle.)
- **States:** loading, empty root, empty folder, no-results, **upload (queued/uploading/error)**, **preview (pending/failed/download-only)**, error, and `reader` view-only (no upload/edit/move/household-pin; preview + download + personal-pin remain).
- **Accessibility:** keyboard-operable tree, upload, move, pin, and preview controls; touch targets ≥44 px; `prefers-reduced-motion` on tree/overlay/upload transitions; the preview iframe + thumbnails carry labels.

## 16. Config (PRD §9)

New env for this module (Coolify only; nothing secret in the repo):

- **Primary bucket:** `HOME_DOCS_R2_BUCKET`, `HOME_DOCS_R2_ENDPOINT`, `HOME_DOCS_R2_ACCESS_KEY_ID`, `HOME_DOCS_R2_SECRET_ACCESS_KEY`.
- **Backup:** `HOME_DOCS_R2_BACKUP_BUCKET` (+ its endpoint/keys if a distinct account); `HOME_DOCS_MIRROR_CRON` (default hourly).
- **Limits/preview:** `HOME_DOCS_MAX_UPLOAD_MB` (50); `HOME_DOCS_ALLOWED_MIME` (empty = allow all, still sniffed); `HOME_DOCS_PREVIEW_ENABLED` (true); `HOME_DOCS_SOFFICE_PATH` (headless LibreOffice binary); `HOME_DOCS_PREVIEW_TIMEOUT_SEC` (60).
- **Links:** `HOME_DOCS_PUBLIC_BASE_URL` (base for `/d/{id}`; defaults to the app origin).

**Prerequisites (Karel / ops, before build):** create the **primary + backup R2 buckets**; **enable object versioning** on the primary; put credentials in the `HOME_DOCS_R2_*` vars; ensure the **runtime image includes headless LibreOffice** (Office→PDF) and **poppler-utils** (`pdftoppm`, PDF thumbnails).

## 17. Tests

- **Upload ordering:** the row is committed **only after** the object is durable; an injected R2 failure yields `502` and **no** row; an over-cap file is `413` with nothing written; the sniffed `content_type` (not the client's) is stored.
- **Immutability:** there is **no** replace/overwrite path; `PATCH` cannot change bytes/`checksum`/`storage_key`; re-uploading the same file makes a **new** document (new id, new permanent URL).
- **Permanent URL:** `/api/documents/{id}/raw` is stable across a rename and a move (id + bytes unchanged); `ETag` = checksum; `If-None-Match` → 304; a `Range` request → 206 with the right bytes; the slug path resolves via `resolve` and **404s after rename/move** (no redirect).
- **Serving/isolation:** a `.html`/`.svg` upload is served `attachment` + `nosniff` (never inline-executed); a PDF/image/text is `inline`; `preview` returns 409 while pending, the derived PDF once ready, 204 for download-only types.
- **Preview worker:** an Office file flips `pending`→`ready` with a `preview.pdf` + thumbnail and a `/ws` push; a conversion timeout flips `pending`→`failed` and leaves the document download-only (not lost); pending documents are re-enqueued on boot; the worker is idempotent.
- **Slug/resolver/move:** cross-table sibling uniqueness (a folder and a document can't share a slug under one parent); root-level dedupe via the `COALESCE` index; collision suffixing; **moving a folder rewrites one row, not the subtree**; cycle guard rejects self/descendant → `422` with no write.
- **Folder delete:** non-empty blocks `409` + count; `?cascade=true` soft-deletes the subtree (each child logged); `?hard=true` is admin-only **and deletes the descendants' R2 objects**.
- **Pinning:** household pin requires `editor`+ and is audited; a `reader` can set/remove a **personal** pin (`200`) but gets `403` on a household pin and every other documents mutation; partial indexes prevent duplicates.
- **Widget:** de-dup with household precedence; household block then personal; one bounded query; a household pin refreshes the widget for another user, a personal pin does not.
- **Search:** FTS matches title/filename/description (diacritic-insensitive) and **does not** match file contents; triggers keep `documents_fts` in sync on insert/update/delete.
- **Audit:** every mutation except personal pins produces an event; `document.update`/`document_folder.update` produce **metadata** diffs (never bytes); an overlay edit logs under `documents` with `meta.via="dashboard"` and appears in that document's entity timeline.
- **Backup/reconciliation:** the mirror copies new objects into the backup bucket; a hard-deleted document's objects are gone from the primary; the reconciliation pass flags an orphaned object and a dangling row.
- **Isolation:** the import/arch test asserts `modules/documents` imports no other feature module, and no other module imports it (it may import `platform/blobstore`, `platform/db`, `platform/ws`, `platform/audit`).
- **Role gating:** `reader` gets `200` on all reads (tree, detail, resolve, search, raw/download/preview/thumbnail) and `403` on all writes except a personal pin.

## 18. Definition of done

- [ ] `document_folders`, `documents`, `document_pins`, `documents_fts` (+ triggers) created by this module's migrations, inserted **after `notes`/before `dashboard`**; clean on empty DB and after Litestream restore; nothing seeded; previews not re-derived on restore.
- [ ] `platform/blobstore` S3/R2 client streams (never buffers 50 MB); the module only ever `Put`s `{id}/original` once (no replace path).
- [ ] Upload: size cap (`413`), server-side MIME sniff, checksum; **object durable before the row**; row + `document.create` audit in one tx; slug conflict `409`; orphan avoided/best-effort-cleaned.
- [ ] Preview worker: images/PDF thumbnails; **Office→PDF via headless LibreOffice** (bounded), async, `preview_status` transitions + `/ws` push; failures degrade to download-only without losing the upload; pending re-enqueued on boot; idempotent.
- [ ] Content endpoints: `raw` (inline-safe/attachment-else, `nosniff`, sandboxed CSP, `ETag`=checksum, `immutable` cache, **Range**, 304), `download` (attachment), `preview` (native/PDF, 409 pending/failed, 204 none), `thumbnail`.
- [ ] Permanent **id-based** URL stable across rename/move; slug path resolves and **404s after rename/move** (no redirects); "Kopírovat odkaz" copies `/d/{id}`.
- [ ] Folders/move: cycle guard before write; move rewrites one row, not the subtree; non-empty delete cascade; hard-delete admin + purges R2.
- [ ] Two-scope pinning: household audited + `/ws` + one-per-doc; personal not audited, not broadcast, one-per-doc-per-user, reader-allowed.
- [ ] `document`/`document_folder` join the audit diff set; **metadata diffs only**; dashboard-originated edits carry `meta.via="dashboard"`.
- [ ] FTS search is diacritic-insensitive, filename+metadata only, index-backed; tree loads with no N+1.
- [ ] `documents.pripnute` widget provider registered; de-dup + ordering correct; **row opens a preview overlay on Nástěnka without navigating away**; reuses `DocumentView`; no done gesture.
- [ ] Backup: object **versioning on** (ops), **mirror to the backup bucket** running, **reconciliation** flags orphans/dangling; fresh-build reads bytes from R2.
- [ ] Dokumenty frontend built against `design/Home.dc.html` + `design/DocumentView.dc.html` (list default + grid toggle, upload queue, DocumentView, pins, **4-slot + "Více" mobile nav**); loading/empty/upload/preview/error/`reader` states; verified 375 px + 1440 px, both themes.
- [ ] Import/arch test covers `documents`; every mutation (except personal pins) audited in-transaction.
- [ ] Config wired (PRD §9 + §16); runtime image has LibreOffice + poppler-utils; `REGISTRY.md` reflects `documents` live once deployed.

## 19. Module packaging & build order

`documents` is **module 6**, built after the foundation and the spine (it does not depend on todo/events/notes/dashboard). Package it exactly like the others (`HANDOFF.md` §3): own `module.go` implementing `registry.Module` (routes, migrations, `AuditActions()`, one `Widgets()` entry), own `migrations/*.sql`, own frontend folder `src/modules/documents/`. It may import only `platform/*` (incl. the new `platform/blobstore`), never another feature module. Adding it to the live app is **adding a package that registers itself** — the dashboard host discovers `documents.pripnute` through the registry with **no host edit**. Migrations slot in **after `notes`, before `dashboard`**.
