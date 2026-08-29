# Refactor candidates

A survey of the whole repository for **pure refactors** — changes that reduce duplication,
shorten functions and delete dead code **without altering behaviour**, and without
weakening the module boundaries the arch tests enforce.

## Ground rules this list obeys

- **No behaviour change.** Every item is a same-output transformation. Where an item
  *cannot* be done without a behaviour change, it says so explicitly and stops at the
  safe part.
- **Module division is preserved.** `internal/arch/arch_test.go` forbids
  cross-module imports (D28), so every "share this" item lands in `internal/platform/…`,
  never in a sibling module. Nothing here asks a module to import another.
- **Adopt, don't just extract.** The precedent in `platform/db/sql.go` is explicit:
  *"an extraction nobody adopts is a seventh copy with a doc comment claiming
  otherwise."* Each item below means migrating every call site, not adding a helper
  beside the copies.
- **Comments travel with the code.** This codebase carries a lot of load-bearing
  rationale in comments. Merging two copies means merging their comments, not picking
  one.

## Baseline (verified 2026-08-28, `main` @ `f676473`)

| Check | Result |
|---|---|
| `go build ./...` | clean |
| `go vet ./...` | clean |
| `go test ./...` | 28 packages ok |
| `tsc -b --noEmit` | clean |
| `vitest run` | 21 files, 151 tests pass |

Scale: **58.8k** LOC Go (non-test) + **32.4k** LOC Go tests; **38.7k** LOC TS/TSX.

---

# Part 1 — Backend: helpers that exist 4–8 times over

These are the safest items in the document. Each is a set of byte-identical (or
identical-modulo-a-noun) functions that should live once in `internal/platform/`.

### 1. `respond` / `respondNoContent` — 8 identical copies

`chat`, `documents`, `electricity`, `events`, `finance`, `garden`, `notes`, `todo` each
carry a byte-identical pair:

```go
func respond(w http.ResponseWriter, status int, v any, err error) {
	if err != nil { httpx.WriteError(w, err); return }
	httpx.JSON(w, status, v)
}
```

**Do:** move to `httpx.Respond` / `httpx.NoContent` beside `httpx.JSON` and
`httpx.WriteError`, delete all 16 copies. Note `garden` spells the second one
`noContent` and the rest `respondNoContent` — pick one name.

**Effort:** trivial · **Risk:** none · **Removes:** ~110 lines

### 2. `DBTX` interface — 7 identical copies

`documents`, `electricity`, `events`, `finance`, `garden`, `notes`, `todo` each declare
the same three-method interface. It is the seam onto `platform/db`, which every one of
them already imports.

**Do:** `appdb.DBTX`; keep a per-module `type DBTX = appdb.DBTX` alias only if it keeps
diffs small, otherwise migrate the signatures outright.

**Effort:** trivial · **Risk:** none · **Removes:** ~35 lines

### 3. The row-scan loop — **137 occurrences**

The same eight lines appear 137 times (84 with a `defer rows.Close()`), heaviest in
`notes/store.go` (16), `documents/store.go` (14), `electricity/store.go` (11):

```go
defer rows.Close()
var out []T
for rows.Next() {
	v, err := scanT(rows)
	if err != nil { return nil, err }
	out = append(out, v)
}
return out, rows.Err()
```

Every module already has a `scanX(scanner) (X, error)` to pass in.

**Do:** `func Collect[T any](rows *sql.Rows, scan func(Scanner) (T, error)) ([]T, error)`
in `platform/db`. Go is on 1.26, so generics are available.

**Effort:** medium (many call sites, each mechanical) · **Risk:** low · **Removes:** ~700 lines

> ⚠ A handful of these loops do more than append (they build a map, or count as they
> go). Migrate only the plain ones; leave the rest and say so in the helper's doc.

### 4. Audit-diff helpers `ap` / `eqp` / `diff` — 4–6 copies each

`eqp` and `diff` are byte-identical in `documents`, `events`, `notes`, `todo`; `ap` also
appears in `chat/upload.go` and `finance`. All four modules already import
`platform/audit` — this is that package's own vocabulary.

**Do:** `audit.Ptr`, `audit.EqualPtr`, `audit.Diff(&changes, field, old, new)`.

**Effort:** trivial · **Risk:** none · **Removes:** ~70 lines

### 5. `record` — 8 wrappers that have drifted into 4 signatures

Every module wraps `sink.Record` to stamp its own `audit.Module*` constant. They now
disagree about the parameter list:

| Module | Signature tail |
|---|---|
| `todo`, `garden` | `…, changes, meta` |
| `events` | `…, changes, meta` (entity type hard-coded) |
| `electricity` | `…, changes` |
| `finance`, `chat` | `…, changes` (entity type hard-coded) |
| `notes`, `documents` | `…, changes, meta, Scope` |

The divergence is the cost: four call conventions for one act, so a reader moving
between modules re-learns it each time.

**Do:** `audit.Sink.For(audit.ModuleX) *audit.ModuleSink` returning a sink with the
module pre-bound, and call `s.rec.Record(ctx, tx, audit.Event{…})` everywhere — the
`Event` struct already carries every field these wrappers spread across positional
arguments.

**Effort:** medium · **Risk:** low · **Removes:** ~90 lines, and one whole
inconsistency

### 6. Page-limit clamp — 5 implementations under 3 names

| Location | Name | Bounds |
|---|---|---|
| `admin/http.go:232` | `limitOf` | 50 / 200 |
| `chat/store.go:30` | `NormalizeLimit` | 50 / 200 |
| `garden/store.go:43` | `NormalizeLimit` | 50 / 200 |
| `electricity/http.go:71` | `limitOf` | 100 / **500, and out-of-range falls back to the default rather than clamping** |
| `logging/query.go:491` | `clampLimit` | own |

**Do:** `httpx.Limit(r *http.Request, def, max int) int` plus
`appdb.ClampLimit(n, def, max int) int` for the store-side callers. Keep each module's
bounds as arguments.

> ⚠ `chat/store.go:28` explicitly documents electricity's 100/500-with-no-clamp as *"a
> known defect and not a precedent to copy."* Sharing the helper does not fix that, and
> fixing it here would be a behaviour change — leave electricity's numbers exactly as
> they are and open it separately.

**Effort:** small · **Risk:** low (given the caveat)

### 7. List cursors — 4 mutually incompatible encodings

| Location | Encoding |
|---|---|
| `logging/query.go:626` | base64url of `ts \x00 id` |
| `garden/store.go:1018` | base64url of `from \x1f pos \x1f id` |
| `chat/store.go:193` | plain `updatedAt \| id` |
| `documents/service.go:1797` | plain `ts \| id`, `LastIndex` split |

Four spellings of "an opaque keyset cursor over a sort tuple", with four different
malformed-input behaviours (garden returns `ok=false`, documents silently restarts at
page one, logging returns an error).

**Do:** `platform/cursor` with `Encode(parts ...string) string` /
`Decode(cur string, n int) ([]string, bool)`, base64url over a `\x1f` separator.

> ⚠ The wire format changes for `chat` and `documents` unless you keep their plain
> encodings. A cursor is short-lived, but a client mid-pagination across a deploy gets
> one bad page. If that is unacceptable, ship the shared package and migrate only
> `logging` + `garden` (which already agree on base64), and leave the two plain ones
> with a doc comment pointing at it.

**Effort:** small · **Risk:** medium (wire-visible — read the caveat)

### 8. `nowUTC` — 8 copies over **5 different timestamp formats**

`chat`, `documents`, `electricity`, `events`, `finance`, `garden`, `notes`, `todo`
(+ `admin`, `platform/push` for the constant):

| Format | Modules |
|---|---|
| `time.RFC3339` | `events`, `todo` |
| `time.RFC3339Nano` | `admin`, `platform/push` |
| `…05.000Z07:00` (ms) | `chat`, `electricity`, `finance`, `garden` |
| `…05.000000Z07:00` (µs) | `documents`, `notes` |

**Do:** share only the *function* — `appdb.NowUTC(format string) string` — and keep each
module's `tsFormat` constant where it is.

> ⚠ **Do not unify the formats.** Stored timestamps are compared lexically by SQLite in
> ORDER BY and in keyset cursors. Changing a module's format changes how its existing
> rows sort against its new ones. That is a data migration, not a refactor.

**Effort:** trivial · **Risk:** none (given the caveat)

### 9. Leftover shims after earlier extractions

The `Placeholders` and `FTSQuery` extractions landed, but the one-line forwarders were
left behind:

- `placeholders(n)` → `appdb.Placeholders(n)` in `documents/store.go:1196`,
  `garden/corecols.go:282`, `notes/store.go:1006`, `todo/tree.go:213`,
  `platform/push/store.go:571`
- `ftsQuery(q)` → `appdb.FTSQuery(q)` in `chat/search.go:46`,
  `documents/service.go:1818`, `notes/service.go:1821`

**Do:** call `appdb.*` directly and delete the eight shims. (`garden` and `logging` keep
their own `ftsQuery` — those genuinely differ, and `platform/db/sql.go` already records
why.)

**Effort:** trivial · **Risk:** none

### 10. `garden`'s eight identical soft-deletes

`SoftDeletePlant`, `SoftDeleteVariety`, `SoftDeleteBed`, `SoftDeletePlanting`,
`SoftDeleteTask`, `SoftDeleteHarvest`, `SoftDeleteStorage`, `SoftDeleteRule` are
byte-identical apart from the table name:

```go
`UPDATE <table> SET deleted_at = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL`
```

**Do:** one private `func (s *Store) softDelete(ctx, tx, table, id, at) error` plus eight
one-line wrappers (keep the wrappers — they are the module's vocabulary and the
service calls them by name). Table names are compile-time constants, so there is no
injection surface.

**Effort:** trivial · **Risk:** none · **Removes:** ~35 lines

### 11. Two mechanisms for PATCH "omitted vs explicit null"

- `garden/service.go:181` — `decodePatch` returns a `presentFields` key set, each input
  type carries an unexported `present` field and a custom `UnmarshalJSON`.
- `electricity/http.go:268` — `decode[T]` decodes twice (typed + `map[string]json.RawMessage`),
  each input type carries `NoteSet` / `InvoicedTotalSet` / … booleans.

Same problem, two solutions, neither reusable by the other.

**Do:** `httpx.DecodePatch(r, dst) (Present, error)` in the platform layer, returning the
key set. Migrate `electricity` onto it (its `*Set` booleans become `present["note"]`
lookups) and `garden`'s `decodePatch` to a thin call.

**Effort:** medium · **Risk:** low — both mechanisms are covered by tests that pin
null-vs-omitted, which is exactly the property to preserve

### 12. `electricity`'s eight repeated pointer-date blocks

`http.go` repeats this shape eight times across `readingBody`, `tariffBody`,
`advanceBody`, `paymentBody`, `periodBody`:

```go
if b.EffectiveFrom != nil {
	d, err := parseDate(*b.EffectiveFrom, "effective_from")
	if err != nil { return in, err }
	in.EffectiveFrom = &d
}
```

**Do:** `func assignDate(src *string, field string, dst **dates.Date) error`.

**Effort:** trivial · **Risk:** none · **Removes:** ~40 lines

### 13. `folderCols` — identical constant in two modules

`notes/store.go:69` and `documents/store.go:76` hold the same string. The *columns* are
genuinely the same because the two folder tables are the same shape.

**Do:** roll into item 16 rather than sharing the bare string on its own — a shared
column list without a shared scanner is a trap.

---

# Part 2 — The `notes` ⇄ `documents` twin

This is the single largest opportunity in the repository and also the one that
contradicts a recorded decision. **Read the whole section before acting on any of it.**

> ### The recorded decision
>
> `documents/scope.go` states: *"This is deliberately duplicated from the `notes` module
> rather than shared with it: the v4 D40 precedent is that Dokumenty MIRRORS Poznámky's
> folder model — one behaviour, two implementations… The two copies must be kept in
> step by hand, and the tests on both sides are what catch it when they are not."*
> `notes/mirror.go` says the same of the mirror jobs.
>
> **That decision is Karel's to revisit, not mine to overturn.** What follows is the
> evidence for reconsidering it, plus a smaller subset that is worth doing *even if the
> decision stands*.

### The evidence

`notes` + `documents` are **12,057 LOC** of non-test Go and **6,459 LOC** of tests —
roughly a third of the backend.

- **`service.go`:** 41 function names appear in both files. **24 are identical modulo
  the noun** (`Note`↔`Document`), including `wouldCycle`, `ancestors`,
  `assertParentLive`, `freeSlug`, `pinState`, `Unpin`. **17 more are byte-identical
  with no renaming at all:** `actorID`, `writeAllowed`, `ap`, `eqp`, `diff`, `boolPtr`,
  `metaHard`, `metaVia`, `metaByAdmin`, `parsePinScope`, `shortID`, `slugPathFrom`,
  `splitPath`, `isReservedRootSlug`, `assertLiveForMutation`, `publishChanges`,
  `publishMeta`.
- **`scope.go`:** 260 and 262 lines. Normalising the noun leaves a **64-line diff, all
  of it comment prose.** The code is functionally identical.
- **`store.go`:** the entire folder half (`GetFolder`, `GetFolderAnyScope`,
  `InsertFolder`, `RenameFolder`, `SetFolderIcon`, `MoveFolderRow`,
  `SetFolderArchived`, `DeleteFolder`, `ChildFolderBySlug`, `lastFolderPosition`,
  `FolderChildCounts`, `FolderMetaByIDs`, `DescendantFolderIDs`, `AllFolders`,
  `AllFoldersForViewer`, `ChildFolders`) and the entire pin half (`PinSets`,
  `GetPinState`, `lastPinPosition`, `InsertPin`, `DeletePin`, `PinnedRowsFor`,
  `CountPinnedFor`) differ **only in the table name** — `folders`/`document_folders`,
  `note_pins`/`document_pins`.
- **`mirror.go`:** 346 + 318 lines. Same five methods (`Run`, `RunOnce`, `mirror`,
  `reconcile`, `safeToDelete`), same algorithm, same guards.
- **`pripnute.go`:** 159 + 132 lines, same provider shape.

### The drift the decision predicted has already happened

The two copies **are** out of step, in ways nothing catches:

- `notes/mirror.go`'s `safeToDelete` is missing the six-line comment that explains *why*
  the floor carries a second bound — the comment `documents` learned the hard way.
- `DeleteFolder` is **186 lines** in `documents` and **141** in `notes` for the same
  algorithm.
- On the frontend twin, `DocumentsDialogs.tsx` has `min-h-11` and `aria-hidden` where
  `NotesDialogs.tsx` has neither — an accessibility and touch-target fix that reached
  one copy.

That is the cost side of the ledger, and it is worth putting in front of the decision.

---

### 14. `scope.go` — extract to `platform/treescope`

The 522 lines across the two files reduce to one ~270-line package: `Scope`,
`Visibility`, `scopeCond`, `viewerCond`, `siblingKeyExpr`, `ParseScope`,
`ParseCreateScope`, `callerScopeFor`, `assertPairing`, `isAdminCtx`,
`errCrossScopeMove`.

Every one of these is table-agnostic already — `siblingKeyExpr` even takes the parent
column name as a parameter. Nothing here needs to know what a note or a document is.

**Effort:** medium · **Risk:** low — this is the *most* mechanical item in Part 2, and
`v9_privacy_test.go` on both sides pins the behaviour
**Removes:** ~250 lines

### 15. The 17 byte-identical service helpers — extract to `platform/`

`actorID`, `writeAllowed`, `ap`, `eqp`, `diff`, `boolPtr`, `metaHard`, `metaVia`,
`metaByAdmin`, `parsePinScope`, `shortID`, `slugPathFrom`, `splitPath`,
`isReservedRootSlug`, `assertLiveForMutation`, `publishChanges`, `publishMeta`.

Several belong with item 4 (`audit`), several with item 14 (`treescope`), and
`splitPath`/`slugPathFrom` want a small `platform/slugpath`.

**Effort:** small · **Risk:** none · **Removes:** ~140 lines

### 16. The folder half of the store — one table-parameterised implementation

A `platform/foldertree.Store` holding the table name, returning a shared `Folder` row
type, with `notes` and `documents` keeping their own thin wrappers for naming.

`folderCols` (item 13) becomes that package's constant, so the column list and the
scanner can never disagree.

**Effort:** large · **Risk:** medium — this is real surgery on the data layer, and
`nplusone_test.go` in `documents` pins query counts that must not regress
**Removes:** ~400 lines

### 17. The pin half of the store — same treatment

`PinSets`, `GetPinState`, `lastPinPosition`, `InsertPin`, `DeletePin`, `PinnedRowsFor`,
`CountPinnedFor`, over a table-name parameter. Smaller and more self-contained than
item 16 — a good place to prove the pattern before attempting the folder half.

**Effort:** medium · **Risk:** low · **Removes:** ~150 lines

### 18. `DeleteFolder` — 186 + 141 lines of one algorithm

Same cascade guard, same archived-descendant refusal, same `ErrConflictCode`
escalation, same purge enumeration. The 45-line gap between them is the drift.

**Do:** only after 14/16/17 land — it depends on all three.

**Effort:** large · **Risk:** medium-high — this is the most consequential code in either
module (it destroys rows and objects). It has good test coverage on both sides; keep
*both* suites running against the merged implementation.

### 19. The remaining structural twins

`MoveFolder`, `PublishFolder`, `PublishNote`/`PublishDocument`, `Tree`, `Resolve`,
`wouldCycle`, `ancestors`, `assertFolder`, `folderDetail`, `freeSlug`, `Pin`, `Unpin`.
All identical-modulo-noun or nearly so.

**Do:** with 14–18, not before.

### 20. The two mirror jobs — `blobstore.MirrorJob`

`Run`, `RunOnce`, `mirror`, `reconcile`, `safeToDelete` are one job over two parameters:
a key prefix, and a source of "objects the rows claim". The second is a one-method
interface each module already satisfies (`ExpectedObjects` / `ExpectedImageObjects`).

`main.go` already configures the two with **field-identical** `MirrorConfig`/
`ImageMirrorConfig` literals, which is itself the tell.

> ⚠ `notes/mirror.go` states the twinning is deliberate *"so a change to one module's
> storage can never quietly alter the other's"* — and the destructive act here is
> deleting bytes from a bucket with no versioning. That is the strongest case in this
> document for leaving a duplicate alone. If it stays duplicated, at minimum
> **backport the missing `safeToDelete` comment from `documents` to `notes`**, since the
> reasoning it records applies identically to both.

**Effort:** medium · **Risk:** high (destructive path) · **Removes:** ~280 lines

### 21. The two `pripnute.go` providers

Same query shape, same viewer-scoped folder map, same household ∪ personal
de-duplication. Differs in the row type and the breadcrumb payload.

**Effort:** medium · **Risk:** low

---

# Part 3 — Backend structure

### 22. `cmd/home/main.go` — `run()` is **574 lines**

It is well-organised (numbered sections 1–7) and heavily commented, but it is one
function doing configuration, migration, auth, push, websockets, ten module
constructions, four catalogs, background workers, the server and a two-phase shutdown.

**Do:** split along the section comments it already has, into
`loadAndMigrate`, `buildAuth`, `buildPush`, `buildWS`, `buildModules`,
`startBackground`, `serve` — each returning a small struct. **Move the comments with
the code**; a section comment that loses its section is worse than the long function.

**Effort:** medium · **Risk:** low — no logic moves, only braces
**Note:** this also makes the shutdown sequence (which has a genuinely subtle
`notifier.Wait()` / `FlushPending` / `Shutdown` ordering) readable on one screen.

### 23. The same six-module list, written three times

```go
registry.CollectWidgets([]registry.Module{todoMod, eventsMod, notesMod, docsMod, financeMod, gardenMod})
metrics.Collect(todoMod, eventsMod, notesMod, docsMod, financeMod, gardenMod)
lists.Collect(todoMod, eventsMod, notesMod, docsMod, financeMod, gardenMod)
```

Three literals that must stay in step, with nothing checking that they do. The comments
on `elecMod` and `chatMod` explain at length that those two are *deliberately* absent
from all three — which is precisely the invariant a single named slice would express.

**Do:** `contributingModules := []registry.Module{…}` once, used three times.

**Effort:** trivial · **Risk:** none

### 24. Two field-identical mirror configs in `main.go`

`documents.MirrorConfig` and `notes.ImageMirrorConfig` are built from the same five
`cfg.Docs.*` values. Even without item 20, extract the shared literal.

**Effort:** trivial · **Risk:** none

### 25. Other long functions worth splitting

| Function | Lines |
|---|---|
| `ws/handler.go:160` `(*Hub).Handler` | 222 |
| `documents/service.go:811` `DeleteFolder` | 186 (item 18) |
| `electricity/compute.go:755` `Summarize` | 157 |
| `garden/service_plan.go:73` `CreateSeason` | 146 |
| `documents/upload.go:56` `Upload` | 144 |
| `notes/service.go:1912` `gcNoteImages` | 143 |
| `documents/content.go:60` `serveContent` | 134 |

`Summarize` and `serveContent` are the two best candidates: both are straight-line
pipelines with clear stages (`serveContent` = resolve → authorise → range → headers →
stream), so splitting them is mechanical. `(*Hub).Handler` is a closure factory whose
length is mostly the pump loop — lower priority, higher care.

**Effort:** small each · **Risk:** low

---

# Part 4 — Backend dead code

Found with `golang.org/x/tools/cmd/deadcode -test ./...`, each verified by hand.

### 26. Unreachable functions

| Symbol | Note |
|---|---|
| `documents.ParseTS` (`store.go:41`) | **exported**, zero callers. Its doc comment claims *"used by the reconciliation pass"* — it is not, any more. The lying comment is the real defect. |
| `db.Ping` (`platform/db/db.go:45`) | zero callers. Doc says *"used by the readiness probe"*; the probe takes `httpx.Pinger` and calls `PingContext` directly. |
| `push.Disabled` + its 3 methods (`push.go:452-454`) | the inert-channel type is never constructed. `pushSvc.Enabled()` carries the real behaviour. |
| `garden.Enum` (`enums.go:379`) | only `Enums()` is used. |
| `garden.bedsCS`, `garden.warningsCS` (`cs.go:43-44`) | zero callers. |
| `garden.coreFieldNames` (`resolve.go:118`) | comment says *"Exported for the resolve test"* — no test references it. |
| `garden.sortTasksForDisplay` (`service_plan.go:1415`) | zero callers, despite a comment claiming it is how *"every surface"* orders tasks. |
| `lexorank.Rebalance` (`lexorank.go:96`) | zero callers. Documented as a *"rare, explicit renormalisation"* — plausibly a deliberate stand-by API. **Ask before deleting**; if it stays, say in the comment that nothing calls it yet. |
| `testsupport.NewSeededDB` (`testdb.go:38`) | zero callers. |
| `admin.editorCtx` (`admin_test.go:225`), `documents.otherCtx` (`documents_test.go:119`) | unused test helpers. |

**Effort:** trivial · **Risk:** none, except `Rebalance` (a judgement call)

---

# Part 5 — Frontend dead code and dependencies

### 27. Nine unused npm dependencies

`@dnd-kit/modifiers`, `@hookform/resolvers`, `@radix-ui/react-checkbox`,
`@radix-ui/react-select`, `@radix-ui/react-switch`, `@radix-ui/react-tooltip`,
`date-fns`, `react-hook-form`, `zod`.

Verified: **zero references** across `src/`, `e2e/`, `scripts/` and `vite.config.ts`.
`react-hook-form` + `@hookform/resolvers` + `zod` is a whole form stack that was never
adopted — every form in the app is hand-rolled `useState`.

**Do:** `npm uninstall` all nine.

**Effort:** trivial · **Risk:** none (rebuild to confirm) · **Wins:** smaller lockfile,
faster installs, and nobody reaches for a half-installed form library

### 28. Vite scaffold leftovers

`src/App.css` (184 lines) and `src/assets/react.svg`, `src/assets/vite.svg`,
`src/assets/hero.png` — all unreferenced.

**Effort:** trivial · **Risk:** none

### 29. Unused exports (~43 functions)

Notable ones: `gardenError` (`garden/api/hooks.ts` — ironic given item 33),
`fileKind`, `pushSupported`, `deburr`, `rememberCrop`, `kwhNum`, `pct`,
`EmptyState` (electricity), `Placeholder`/`EmptySuccess`/`Card` (`states.tsx`),
`Badge` (`ui.tsx`), `first`/`head` (`lexorank.ts`), `isPrivate` (`scope.ts`).

Plus ~11 endpoint wrappers with no caller: `listColumns`, `deleteCard`, `listCardLinks`,
`listEvents`, `uncompleteReminder`, `getWidget`, `listDocuments`, and garden's
`getPlant`, `createTask`, `deleteTask`, `updateHarvest`, the whole `*Rule` set,
`getWeather`.

> ⚠ The endpoint wrappers map to **real backend routes**. Deleting them shrinks the
> client's coverage of an API that still exists. Triage each: dead experiment → delete;
> route the UI ought to use → leave with a `// not yet wired` note.

**Effort:** small · **Risk:** low (given the triage)

### 30. ~52 types exported without an external consumer

`Visibility`, `Note`, `PathSegment`, `DocumentUrls`, `StorageDatabase`, `StorageTable`,
`ElBlocking`, `GardenOccupancy`, … Almost all are used *within* their own file as field
types, so this is `export` noise rather than dead code.

**Do:** treat as a low-priority tidy — drop `export` where nothing outside the file
names the type. Several deliberately mirror OpenAPI schema names and are worth keeping
exported for that reason alone; decide per type, not in bulk.

**Effort:** small · **Risk:** none · **Priority:** low

---

# Part 6 — Frontend duplication

### 31. `Field` — 5 identical copies, plus 14 hand-written repeats of its markup

Byte-identical in all five electricity dialogs (`AdvanceDialog`, `PaymentDialog`,
`PeriodDialog`, `ReadingDialog`, `TariffDialog`):

```tsx
<label className="block">
  <span className="mb-1.5 block text-[12.5px] font-semibold text-muted">{label}</span>
  {children}
</label>
```

The same label markup is then hand-written **14 times** across 9 files —
`AdministracePage` (×5), `Composer` (×2), `ConditionsBuilder`, `ScheduleBuilder`, and
the five dialogs.

**Do:** export `Field` from `components/ui/ui.tsx`, adopt at all 14 sites.

> `EventForm.tsx` and `ColumnMenu.tsx` also declare a `Field`, but theirs render
> **different markup** (a `div`, different type scale). Leave those two alone —
> merging them would be a visual change.

**Effort:** small · **Risk:** none · **Removes:** ~70 lines

### 32. The inline error box — 5 identical copies

```tsx
<p role="alert" className="rounded-lg border border-danger/40 bg-danger/10 px-3 py-2 text-[13px] text-pretty">
```

Same five electricity dialogs. `LoginScreen.tsx` has two near-variants with a different
radius and colour role.

**Do:** `<FormError>{message}</FormError>` in `components/ui/ui.tsx`; migrate the five
identical ones. Leave `LoginScreen`'s two unless you want the visual change.

**Effort:** trivial · **Risk:** none

### 33. `qs()` — 4 implementations, 4 different skip rules

| File | Drops |
|---|---|
| `api/endpoints.ts:67` | `undefined` only (handles arrays) |
| `garden/api/endpoints.ts:51` | `undefined`, `null`, `''` |
| `electricity/api/endpoints.ts:34` | every falsy value |
| `chat/api/endpoints.ts:22` | `undefined`, `''` |

Four query-string builders that disagree about what an empty value means — the exact
shape of a bug that only shows up on one filter, in one module.

**Do:** one `src/api/qs.ts` taking the widest union and dropping **`undefined` only**
(the `api/endpoints.ts` semantics). Then audit the garden/electricity/chat call sites
that were relying on `''`/`null` being dropped and pass `undefined` instead.

> ⚠ The audit is the work here, and skipping it *is* a behaviour change. Do the sites
> before deleting the local copies.

**Effort:** medium · **Risk:** medium (read the caveat) · **Value:** high

### 34. `e instanceof ApiError ? (e.detail ?? fallback) : fallback` — ~20 sites

Written out longhand in `PlantingDialog` (×3), `PlanTab` (×3), `SeasonCloseDialog`,
`KalendarTab`, `PlodinyTab`, `ZahonyTab` (×2), `CenikTab` (×2), `NastaveniPage`,
`AdministracePage`, and more.

`garden/api/hooks.ts` already exports `gardenError()` and `toastGardenError()` for
exactly this — **and its own module's call sites don't use them** (which is why knip
reports `gardenError` as unused, item 29).

**Do:** `apiErrorMessage(e, fallback)` in `api/client.ts` beside `ApiError` itself;
adopt everywhere; delete `gardenError`.

**Effort:** small · **Risk:** none · **Value:** high — this is the pattern most likely
to be copy-pasted wrong next

### 35. The in-page tab strip — byte-identical in two modules

`GardenPage.tsx:160-180` and `ElectricityPage.tsx:205-223` are the same `<nav>` +
`NavLink` + `cn(...)` block, character for character apart from the route base and the
`aria-label`. `AdministracePage.tsx` has two more with `role="tablist"`.

`RootSwitcher.tsx`'s doc comment already warns about exactly this: *"four
hand-assembled copies had already drifted apart."* The precedent for extracting is in
the file that documents the last time it bit.

**Do:** `<TabStrip base={routes.zahrada} tabs={TABS} label={cs.garden.title} />` in
`components/common/`.

> The `AdministracePage` pair use `role="tablist"` because they switch panels in place
> rather than navigating. Keep them separate, or give `TabStrip` an explicit `as="nav" |
> "tablist"` — do not silently unify the two, since `RootSwitcher`'s comment records
> that the roles were wrong the last time somebody did.

**Effort:** small · **Risk:** low · **Removes:** ~40 lines

### 36. The pin-scope badge in the two `Pripnute*` widgets

Same three-way `personal | household | both` label resolution and the same badge chip
in `PripnuteWidget.tsx` and `PripnuteDokumentyWidget.tsx`, differing only in the `cs.*`
namespace.

**Do:** `<PinScopeBadge scope labels />` in `platform/widgets/shared.tsx`, which already
hosts `WidgetRow` and `WidgetEmpty`.

**Effort:** trivial · **Risk:** none

### 37. `indexTree` and friends — the frontend twin of Part 2

`PoznamkyPage.tsx` (1,120 lines) and `DokumentyPage.tsx` (1,350 lines) carry the same
function set: `indexTree`, `tailOf`, `folderCountLabel`, `rootLabel`, `findNode`,
`subtreeCounts`, `DesktopView`, `TreeNodes`, `FolderPane`, `MobileView`, `SearchBox`,
`SearchResults`, `EmptyState`.

`tailOf` is byte-identical. `indexTree` is identical apart from the item field name and
one extra map (`ancestorsById`) on the documents side.

**Do:** a generic `indexTree<TItem>(tree, { items, itemsAtRoot })` in
`src/lib/foldertree.ts` is the tractable half. The view components are a much larger
job and should follow the backend decision in Part 2, not lead it.

**Effort:** medium (helpers) / large (views) · **Risk:** low / medium

### 38. `NotesDialogs.tsx` ⇄ `DocumentsDialogs.tsx`

`MoveDialog` and `DeleteDialog` are ~90% identical. The differences are: the `cs.*`
namespace, a hard-delete checkbox in documents, and — **as pure drift** — `min-h-11` on
the move-target row and `aria-hidden` on two decorative spans, present in documents and
missing in notes.

**Do:** shared `MoveDialog` / `DeleteDialog` in `components/common/` taking a label
bundle and an optional `hardDelete` slot.

> ⚠ Adopting the shared version **gives notes the missing `min-h-11` and `aria-hidden`**
> — a touch-target and screen-reader improvement, but a change. Either take it
> deliberately (recommended) or replicate notes' current markup exactly. Do not let it
> happen by accident.

**Effort:** medium · **Risk:** low (given the note)

### 39. Two ISO-week implementations inside one module

`garden/components/TimingWindowInput.tsx` (`isoWeekMonday`, `weeksInISOYear`, `addDays`,
`toISO`) and `garden/pages/KalendarTab.tsx` (`isoWeekKey`, its own Thursday arithmetic).
Both hand-roll ISO-8601 week maths; both must agree with the backend's
`time.Time.ISOWeek()`.

**Do:** one `src/modules/garden/isoWeek.ts`. Test it against the backend's answers for
the week-53 years (2020, 2026, 2032) — `TimingWindowInput` already documents a
deliberate week-53 clamp that must survive.

**Effort:** small · **Risk:** low · **Value:** high — duplicated calendar arithmetic is
duplicated off-by-one bugs

### 40. `window.confirm` in 10 places, `ResponsiveModal` everywhere else

`AdministracePage` (×2), `CenikTab` (×4), `OdectyTab`, `LabelManager`, `UkolyPage` (×2)
use the native browser confirm; the rest of the app uses a styled `ResponsiveModal`
confirm.

**Do:** a shared `useConfirm()` / `<ConfirmDialog>`.

> ⚠ This **is** a visible change (native chrome → in-app modal), so it is not a pure
> refactor. Listed for completeness; treat as a small UX consistency task rather than
> part of this pass.

**Effort:** small · **Risk:** low, but **not behaviour-neutral**

---

# Part 7 — Frontend structure

### 41. `src/routes/*` and `src/modules/*` are two conventions for one thing

| Convention | Features |
|---|---|
| `src/modules/<x>/` owns pages + `api/` + `widgets/` | `garden`, `electricity`, `chat`, `admin` |
| `src/routes/<x>/` holds pages, API lives in the shared `src/api/` | `ukoly`, `poznamky`, `dokumenty`, `finance`, `log`, `nastenka`, `okno` |
| **Split across both** | `notes` (widget in `modules/`, pages in `routes/poznamky/`), `documents` (widget in `modules/`, pages in `routes/dokumenty/`), `todo`, `events`, `finance` |

The `modules/` shape is the newer one and matches the backend's `internal/modules/`
layout one-to-one. The split cases are the confusing ones: a reader looking for
Dokumenty finds half of it in each tree.

**Do:** converge on `src/modules/<x>/` — moving `routes/poznamky/` → `modules/notes/`,
`routes/dokumenty/` → `modules/documents/`, and so on, with `src/routes/` reduced to
the route table.

**This strengthens the module division rather than compromising it**, and it makes the
frontend mirror the backend's `internal/modules/` layout name for name.

**Effort:** large (mostly `git mv` + import rewrites) · **Risk:** low — no logic
changes. Do it as one mechanical commit with no other edits in it, so review is a
diff of paths.

### 42. `src/api/endpoints.ts` (524 lines) and `src/api/types.ts` (1,087 lines) are shared monoliths

They hold todo + events + notes + documents + finance + dashboard + admin + push, while
`garden`, `electricity` and `chat` each keep their own `api/endpoints.ts` + `types.ts`.

Same inconsistency as item 41, one layer down: `api/types.ts` imports nothing
module-specific, so any module's type change touches a 1,087-line shared file.

**Do:** split per module alongside item 41. Keep `src/api/client.ts`, `ws.ts`, `keys.ts`
and the shared envelope types where they are — those genuinely are platform.

> `keys.ts` should stay central regardless. Its comments explain that the shared key
> *prefixes* are what make cross-module invalidation correct, and splitting it would
> put that invariant in six files.

**Effort:** medium · **Risk:** low

### 43. Hardcoded Czech in 81 of ~120 `.tsx` files, beside a 1,846-line `cs.ts`

There are 848 distinct `cs.*.*` references, and also literal Czech strings in 81 files —
`CardTile.tsx`'s `title="Přesunout do…"`, `EventForm.tsx`'s multi-sentence warning,
`AppShell.tsx`'s `Přepnout na tmavý motiv`, and so on. Two conventions, coexisting.

**Do:** move the literals into `cs.ts`. Moving a string **verbatim** is behaviour-neutral.

> ⚠ Scope this. All 81 files at once is a huge, unreviewable diff. Suggested order:
> (1) `components/common/` + `components/ui/` — shared components leaking strings is
> the worst case; (2) `routes/ukoly/` — the densest; (3) the rest, module by module,
> folded into whatever other work touches them.
>
> ⚠ And **only move strings, never reword them**. A reworded string is a product change
> wearing a refactor's clothes.

**Effort:** large · **Risk:** low, if strings are moved character-for-character

---

# Part 8 — Test-code duplication

32.4k lines of Go tests carry their own copies.

### 44. `send` and `router` — 4 copies each

`send(t, h, method, path, body)` is **byte-identical** in `finance`, `events` and
`electricity` (and `todo` has a variant returning `int` instead of the recorder).
`router(t, roles...)` is the same 11-line `httpx.NewRouter(httpx.Deps{…})` literal in
`todo`, `events`, `finance`, `electricity`, differing only in which handler it mounts.

**Do:** `testsupport.Router(t, mount func(chi.Router), roles ...string)` and
`testsupport.Send(t, h, method, path, body) *httptest.ResponseRecorder`. All four are
external `_test` packages, so importing `testsupport` costs nothing.

**Effort:** small · **Risk:** none · **Removes:** ~90 lines

### 45. `countRows` and `auditCount`

`countRows` is byte-identical in `finance`, `electricity`, `documents`. `auditCount` is
identical in `todo` and `events`, with scoped variants in `electricity` and `chat`.

**Do:** `testsupport.CountRows(t, db, table)` and
`testsupport.CountAudit(t, db, opts…)` covering the scoped variants.

**Effort:** trivial · **Risk:** none

### 46. Unused test helpers

`admin.editorCtx`, `documents.otherCtx`, `testsupport.NewSeededDB` — see item 26.

---

# Suggested order

Sequenced so each step is independently shippable and reviewable.

| Wave | Items | Why here |
|---|---|---|
| **1 — free wins** | 1, 2, 4, 8, 9, 10, 12, 13, 23, 24, 26, 27, 28, 45, 46 | Mechanical, zero-risk, no design decisions. Clears noise before the real work. |
| **2 — frontend hygiene** | 31, 32, 34, 36, 39, 44 | Small, self-contained, each one commit. Item 34 first: it is the pattern most likely to be copied wrong next. |
| **3 — platform seams** | 3, 5, 6, 11, 22, 25 | Bigger, still behaviour-neutral. Item 3 (`appdb.Collect`) is the largest single line-count win in the repo. |
| **4 — decide, then act** | 7, 33, 41, 42, 43 | Each needs a call from Karel first — wire format (7), skip semantics (33), directory layout (41/42), scope (43). |
| **5 — the twin** | 14, 15, 17, 16, 21, 37, 38, 19, 18, 20 | Only after **§Part 2's recorded decision is explicitly revisited.** Ordered by ascending risk: `scope.go` proves the pattern, `DeleteFolder` and the mirror jobs come last. |
| **not in this pass** | 30, 40 | Item 30 is low value; item 40 is a UX change, not a refactor. |

## Guard rails for every wave

- The baseline at the top of this file is the contract: `go test ./...` (28 packages),
  `tsc -b`, `vitest run` (151 tests) must all still pass after each commit.
- `internal/arch` must stay green — it is the thing that keeps a "share this helper"
  from becoming a cross-module import.
- One concern per commit. Item 41 in particular must be a pure move with no other edits.
- When two copies merge, **merge their comments too.** Item 20's missing `safeToDelete`
  rationale is what a lost comment looks like six months on.
