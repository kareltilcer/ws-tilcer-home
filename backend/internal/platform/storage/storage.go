// Package storage is the FOURTH registered catalog, beside the widget catalog,
// the audit-action catalog, and the metric/list pair (v9, PRD §V9-1 D191).
//
// Administrace needs to say how big each module is — in SQLite and in R2 — and it
// may not import a single one of them: `internal/arch`'s import-lint fails the
// build on a cross-module import, and rightly. Home has answered this shape of
// problem three times already, so v9 answers it the same way a fourth time.
//
// The split of responsibility is the whole design:
//
//	the PLATFORM sizes tables. It needs no idea what they mean — a table name and
//	`dbstat` are enough, so a module declares a plain []string and nothing else.
//
//	the MODULE attributes bytes. Only `documents` knows that documents/{id}/original
//	maps to documents.created_by, and only `notes` knows that note-images/{id} maps
//	through note_images.note_id to its note's owner. Neither fact belongs in the
//	platform, and neither belongs in `admin`.
//
// ⚠ Built as §V5-12 CORRECTED metrics and lists to be built: an optional Source
// interface plus a *Registry assembled at composition — NEVER a package-level
// Register global (D191). A global works until two tests run in parallel, and then
// it works differently.
package storage

import (
	"context"
	"fmt"
	"sort"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/blobstore"
)

// Source is what a module implements to declare the SQLite tables it owns.
//
// It is deliberately NOT part of registry.Module: adding a catalog must not change
// the contract every module implements (the D56 rule applied a fourth time).
// Collect type-asserts for it, exactly as metrics.Collect does.
//
// ⚠ THE LIST MUST INCLUDE FTS5 SHADOW TABLES. An external-content FTS5 table
// materialises FIVE `type='table'` rows — X, X_config, X_data, X_docsize, X_idx —
// of which only X appears in a migration. They are frequently among the LARGEST
// b-trees in the file (notes_fts_data routinely outweighs `notes`), so a module
// that declares only its real tables produces a storage page whose per-module
// totals do not add up to the database total — and the arithmetic is the page's
// entire premise. FTSShadows below expands one for you.
type Source interface {
	StorageTables() []string
}

// BlobSource is the optional second interface: only `notes` and `documents` hold
// bytes outside SQLite, and only they can attribute them.
type BlobSource interface {
	StorageBlobs(ctx context.Context) ([]BlobUsage, error)
}

// PrivateInventory is the optional third: the purge screen's listing (D198).
//
// ⚠ What it returns is bounded by what the screen is ALLOWED to show — id, kind,
// owner, size, dates and nothing else. There is no Title field on Item and there
// must not be one: an admin can name the thing well enough to delete it and not
// well enough to know what it is (D197). Anything added to Item has to be
// justified against that sentence.
//
// ⚠ IT TAKES AN OWNER, NOT THE FILTER, and that narrowing is load-bearing. A
// module returns EVERY private item matching ownerUserID, unsorted and unpaged,
// plus the complete byte total; SORTING AND PAGING ARE THE REGISTRY'S, because the
// collection is one list across two modules and a cursor has to mean the same
// thing in both.
//
// Handing modules the whole ItemFilter said otherwise. Sort, Limit and Cursor were
// on it, every implementation ignored them, and the registry's merge was correct
// only BECAUSE they did — the first module to honour Limit would have made the
// keyset cursor start skipping items, silently and without a failing test. An
// argument list that cannot express the wrong thing is better than a comment
// asking for the right one.
type PrivateInventory interface {
	PrivateItems(ctx context.Context, ownerUserID string) ([]Item, int64, error)
}

// BlobUsage is one bucket of a module's object-storage consumption, already
// attributed by the module that owns the prefix.
type BlobUsage struct {
	// Prefix is the bucket prefix the module owns, e.g. "documents/".
	Prefix string
	// Kind is "shared", "private" or "unattributed".
	//
	// `unattributed` is NOT an error state and not padding: it is objects that
	// resolve to no live row — the orphan backlog the mirror job already
	// reconciles, surfaced on a screen for the first time (D194). Reporting it is
	// the reason the figures come from LISTING the bucket rather than from summing
	// documents.byte_size, which would make those objects silently invisible.
	Kind string
	// OwnerID is set iff Kind == KindPrivate.
	OwnerID string
	Objects int64
	Bytes   int64
}

// Usage kinds.
const (
	KindShared       = "shared"
	KindPrivate      = "private"
	KindUnattributed = "unattributed"
)

// ItemFilter narrows the purge listing. There is deliberately no free-text search
// field: the screen has nothing to search (D198).
//
// ⚠ This is the REGISTRY's and the handler's type, not a module's. Only
// OwnerUserID ever reaches a module (see PrivateInventory); Module selects which
// modules are asked, and Sort/Limit/Cursor are applied here, over the merged list.
type ItemFilter struct {
	OwnerUserID string
	Module      string
	// Sort is "recent" (newest-first by id, resumable with Cursor) or "size"
	// (largest-first, SINGLE PAGE — a keyset cursor is an id, and an id does not
	// locate a position in a size ordering). TotalBytes still covers every matching
	// item either way, so the figure the screen acts on is complete even when the
	// list is truncated.
	Sort   string
	Limit  int
	Cursor string
}

// Sort orders.
const (
	SortRecent = "recent"
	SortSize   = "size"
)

// Item is one private item as the purge screen sees it.
//
// ⚠ THE FIELDS THAT ARE ABSENT ARE THE SPECIFICATION (D198). No title, no
// filename, no description, no content type, no preview, no download URL.
type Item struct {
	ID        string
	Module    string
	Kind      string // note | document | note_folder | document_folder | note_image
	OwnerID   string
	ByteSize  int64
	CreatedAt string
	UpdatedAt string
}

// Item kinds.
const (
	ItemNote           = "note"
	ItemDocument       = "document"
	ItemNoteFolder     = "note_folder"
	ItemDocumentFolder = "document_folder"
	// ItemNoteImage is INFORMATIONAL and not deletable. There is no delete route
	// for a note image and there should not be — an image belongs to its note and
	// goes when the note does (D204/D212). The screen says so rather than offering
	// a control that 405s.
	ItemNoteImage = "note_image"
)

// Registry is the assembled catalog: built once at composition, read-only after.
//
// Every read is nil-receiver safe, so a host that wires no catalog degrades to
// "nothing is declared" rather than panicking inside a page render.
type Registry struct {
	tables    map[string][]string // module → tables
	blobs     map[string]BlobSource
	inventory map[string]PrivateInventory
	groups    map[string]GroupSource // v10 — see groups.go
	order     []string               // module registration order, for stable output
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		tables:    map[string][]string{},
		blobs:     map[string]BlobSource{},
		inventory: map[string]PrivateInventory{},
		groups:    map[string]GroupSource{},
	}
}

// Named is the minimum a contributor must expose so the registry can label it.
// registry.Module satisfies it; so does the platform's own pseudo-module.
type Named interface{ Name() string }

// Register adds one module's declarations. A table claimed twice is a programming
// error — two modules cannot both own a b-tree, and the per-module totals would
// double-count it — so it fails the build of the registry rather than silently
// shadowing.
func (r *Registry) Register(m any) error {
	named, ok := m.(Named)
	if !ok {
		return nil // not a module; nothing to attribute it to
	}
	name := named.Name()
	if src, ok := m.(Source); ok {
		// ⚠ APPENDED ONE AT A TIME, so the check below sees this module's OWN earlier
		// entries as well as every other module's. Scanning the whole list first and
		// appending it wholesale let a name repeated inside one StorageTables() —
		// a copy-paste, or an FTS shadow list expanded by hand beside FTSShadows —
		// through cleanly, and measureDatabase then added that b-tree's bytes and
		// index bytes to the module twice. The per-module figures stop summing to the
		// database total, which is the page's entire premise, and TableOwners() is a
		// map so the completeness test cannot see it either.
		for _, t := range src.StorageTables() {
			if owner, dup := r.ownerOf(t); dup {
				if owner == name {
					return fmt.Errorf("storage: module %q declares table %q twice", name, t)
				}
				return fmt.Errorf("storage: table %q is declared by both %q and %q", t, owner, name)
			}
			r.tables[name] = append(r.tables[name], t)
		}
	}
	if bs, ok := m.(BlobSource); ok {
		r.blobs[name] = bs
	}
	if inv, ok := m.(PrivateInventory); ok {
		r.inventory[name] = inv
	}
	// v10: the group projection, duck-typed exactly like the three above it. A
	// module that does not implement it is skipped rather than declared empty —
	// which is what keeps ten modules from having to grow a method that returns nil.
	if gs, ok := m.(GroupSource); ok {
		r.groups[name] = gs
	}
	if _, seen := indexOf(r.order, name); !seen {
		r.order = append(r.order, name)
	}
	return nil
}

// Collect builds a registry from anything that implements the interfaces above —
// in practice the module set, passed straight from the composition root.
func Collect(modules ...any) (*Registry, error) {
	r := NewRegistry()
	for _, m := range modules {
		if err := r.Register(m); err != nil {
			return nil, err
		}
	}
	return r, nil
}

func (r *Registry) ownerOf(table string) (string, bool) {
	for module, tables := range r.tables {
		for _, t := range tables {
			if t == table {
				return module, true
			}
		}
	}
	return "", false
}

// Modules returns the declaring modules in registration order.
func (r *Registry) Modules() []string {
	if r == nil {
		return nil
	}
	return append([]string(nil), r.order...)
}

// Tables returns one module's declared tables, sorted.
func (r *Registry) Tables(module string) []string {
	if r == nil {
		return nil
	}
	out := append([]string(nil), r.tables[module]...)
	sort.Strings(out)
	return out
}

// InventoryModules returns, in registration order, the modules that actually
// implement PrivateInventory — i.e. the only values `?module=` on the purge
// listing can meaningfully take.
//
// ⚠ It exists so the handler does not carry a hand-written `"notes" | "documents"`
// allow-list, which is a fifth place that has to be remembered when a third module
// gains a private root — and the one place nothing would fail to compile. The
// registry already knows; asking it is the same declare-once/ask-the-catalog shape
// the rest of this package argues for (D191).
func (r *Registry) InventoryModules() []string {
	if r == nil {
		return nil
	}
	out := make([]string, 0, len(r.inventory))
	for _, module := range r.order {
		if _, ok := r.inventory[module]; ok {
			out = append(out, module)
		}
	}
	return out
}

// TableOwners returns table → module for every declared table. This is what the
// completeness test enumerates sqlite_master against.
func (r *Registry) TableOwners() map[string]string {
	out := map[string]string{}
	if r == nil {
		return out
	}
	for module, tables := range r.tables {
		for _, t := range tables {
			out[t] = module
		}
	}
	return out
}

// Blobs resolves every blob-holding module's attributed usage. A module's failure
// is returned rather than swallowed: the storage page reports `blobs.available:
// false` with the reason, which is more useful than a total that is quietly short.
func (r *Registry) Blobs(ctx context.Context) (map[string][]BlobUsage, error) {
	out := map[string][]BlobUsage{}
	if r == nil {
		return out, nil
	}
	for _, module := range r.order {
		bs, ok := r.blobs[module]
		if !ok {
			continue
		}
		usage, err := bs.StorageBlobs(ctx)
		if err != nil {
			return nil, fmt.Errorf("storage: %s blob usage: %w", module, err)
		}
		out[module] = usage
	}
	return out, nil
}

// PrivateItems merges every module's private inventory.
//
// Sorting and paging happen HERE rather than per module, because the collection is
// one list across two modules and a cursor has to mean the same thing in both.
//
// ⚠ THE LIMIT BOUNDS THE RESPONSE, NOT THE WORK, and the distinction is worth
// stating plainly because an earlier draft of this comment claimed otherwise.
//
// Every call asks each module for its COMPLETE matching set — five unbounded
// queries across the two of them — then merges, sorts, applies the cursor, and
// only then truncates. So "Načíst další" costs the same as page one, every time.
// The Limit+1 clamp below is what makes the CURSOR correct; it is not, and cannot
// be, a bound on the scan.
//
// That is a deliberate trade at this scale, not an oversight. Pushing the page
// down would mean handing modules the cursor and the limit, which PrivateInventory
// refuses on purpose (see its comment: the first module to honour Limit WITHOUT
// the cursor makes the keyset silently skip items), and it would need a second
// aggregate query per table to keep `total` covering everything. For a household
// private tree — hundreds of rows, behind an admin-only route that writes an audit
// event per load — that is machinery bought for a problem nobody has.
//
// ⚠ If this page ever gets slow, THIS is the place to look, and the fix is a
// keyset pushdown: each module returns its own top Limit+1 by id-desc below the
// cursor plus its complete byte total, and the merge of two sorted prefixes is
// still the global prefix. Do not add a bare Limit to PrivateInventory without the
// cursor beside it.
func (r *Registry) PrivateItems(ctx context.Context, f ItemFilter) ([]Item, int64, error) {
	if r == nil {
		return []Item{}, 0, nil
	}
	var all []Item
	var total int64
	for _, module := range r.order {
		inv, ok := r.inventory[module]
		if !ok {
			continue
		}
		if f.Module != "" && f.Module != module {
			continue
		}
		// Only the owner narrows what a module returns — everything else is applied
		// below, once, over the merged list. See PrivateInventory.
		items, bytes, err := inv.PrivateItems(ctx, f.OwnerUserID)
		if err != nil {
			return nil, 0, fmt.Errorf("storage: %s private items: %w", module, err)
		}
		all = append(all, items...)
		total += bytes
	}
	if f.Sort == SortSize {
		sort.SliceStable(all, func(i, j int) bool { return all[i].ByteSize > all[j].ByteSize })
	} else {
		// UUIDv7 ids sort chronologically, so newest-first is a plain reverse id sort
		// — and it is the ordering the keyset cursor below can actually resume.
		sort.SliceStable(all, func(i, j int) bool { return all[i].ID > all[j].ID })
		if f.Cursor != "" {
			for i, it := range all {
				if it.ID < f.Cursor {
					all = all[i:]
					break
				}
				if i == len(all)-1 {
					all = nil
				}
			}
		}
	}
	// ⚠ LIMIT+1, not Limit. The caller detects "there is another page" by comparing
	// the returned length against its own limit, so handing back exactly Limit rows
	// would make the last full page look like the end of the list and lose the
	// cursor. One spare row answers the question without a second query.
	//
	// The truncation is the LAST thing that happens, after the merge, the sort and
	// the cursor — and `total` was summed from every module's complete figure well
	// before it, so the byte total the purge screen acts on still covers everything
	// this filter matches, not just the page.
	if f.Limit > 0 && len(all) > f.Limit+1 {
		all = all[:f.Limit+1]
	}
	if all == nil {
		all = []Item{}
	}
	return all, total, nil
}

// Attribute buckets one prefix's objects into shared / per-member / unattributed
// usage — the arithmetic every BlobSource performs, in one place.
//
// The MODULE still supplies the two facts that are genuinely its own: which prefix
// it owns, how a key maps back to a row id (idOf), and the id → owner index
// (owners, where "" means a shared row and a MISSING key means no live row at all).
// Everything after that — the bucketing, the ordering, which rows are emitted — is
// the same rule on both sides of the page and belongs here.
//
// ⚠ IT WAS THE SAME FORTY LINES IN BOTH MODULES, carrying the same two invariants
// below in the same two comments. The storage page shows notes and documents
// beside each other, so a fix applied to one copy and not the other does not
// produce a bug you can see — it produces two modules reporting under different
// rules in one table.
//
// ⚠ THE TWO FIXED KINDS ARE ALWAYS EMITTED, empty or not. An absent row has two
// possible readings — "this kind is zero" or "this module did not report this
// kind" — and on a maintenance page that is the difference between "no orphan
// backlog" (good news, worth stating) and "nobody looked". Per-MEMBER rows stay
// data-driven: a member with no objects has no row, because a module has no member
// directory to enumerate.
//
// ⚠ THE PER-MEMBER ROWS ARE SORTED BY OWNER. Go randomises map iteration order and
// admin.measureBlobs sorts these rows by KIND only (stably), so an unsorted slice
// reshuffles the per-member lines on every snapshot.
func Attribute(objects []blobstore.ObjInfo, prefix string, idOf func(key string) string, owners map[string]string) []BlobUsage {
	shared := BlobUsage{Prefix: prefix, Kind: KindShared}
	orphans := BlobUsage{Prefix: prefix, Kind: KindUnattributed}
	private := map[string]*BlobUsage{}

	for _, obj := range objects {
		owner, ok := owners[idOf(obj.Key)]
		switch {
		case !ok:
			// No live row: a failed upload, a purge that lost its objects, or a
			// mirror that ran ahead of a delete. Reported, never cleaned from here.
			orphans.Objects++
			orphans.Bytes += obj.Size
		case owner == "":
			shared.Objects++
			shared.Bytes += obj.Size
		default:
			u, seen := private[owner]
			if !seen {
				u = &BlobUsage{Prefix: prefix, Kind: KindPrivate, OwnerID: owner}
				private[owner] = u
			}
			u.Objects++
			u.Bytes += obj.Size
		}
	}

	out := make([]BlobUsage, 0, len(private)+2)
	out = append(out, shared)
	ownerIDs := make([]string, 0, len(private))
	for owner := range private {
		ownerIDs = append(ownerIDs, owner)
	}
	sort.Strings(ownerIDs)
	for _, owner := range ownerIDs {
		out = append(out, *private[owner])
	}
	return append(out, orphans)
}

// FTSShadows expands an external-content FTS5 table into the five `type='table'`
// rows it actually materialises.
//
// It exists so a module declares `storage.FTSShadows("notes_fts")` instead of
// hand-listing four suffixes it will get wrong once. See Source's comment for why
// leaving them out breaks the page's arithmetic rather than merely under-reporting.
func FTSShadows(name string) []string {
	return []string{name, name + "_config", name + "_data", name + "_docsize", name + "_idx"}
}

func indexOf(ss []string, want string) (int, bool) {
	for i, s := range ss {
		if s == want {
			return i, true
		}
	}
	return 0, false
}
