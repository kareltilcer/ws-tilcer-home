package admin

import (
	"context"
	"database/sql"
	"sort"
	"sync"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/blobstore"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/push"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/storage"
)

// Úložiště — the storage snapshot (v9, PRD FR-V9-11…FR-V9-13, D193–D196, D205).
//
// A PROJECTION, not a resource. It owns no state, is never written, and has no
// table and no scheduler job — the whole page is computed on read behind a short
// in-process cache (D195). A one-minute TTL is not state: nothing survives a
// restart, nothing needs a migration, and nothing can be wrong for longer than a
// minute.
//
// ⚠ EVERY BYTE FIGURE IN THIS TREE IS NULLABLE, and null — never 0 — when it could
// not be measured. That is D161 generalised: a `0 B` table on a page whose whole
// job is reporting bytes is a lie that looks like good news. The frontend renders
// null as *nezměřeno*, in a different type family, so the absence is legible
// before it is read.
//
// `admin` reaches every module through platform/storage and imports none of them;
// internal/arch's TestModulesDoNotImportEachOther stays green.

// StorageSnapshot is the whole payload of GET /api/admin/storage.
type StorageSnapshot struct {
	GeneratedAt  string          `json:"generated_at"`
	Cached       bool            `json:"cached"`
	CacheSeconds int             `json:"cache_seconds"`
	Database     StorageDatabase `json:"database"`
	Blobs        StorageBlobs    `json:"blobs"`
	Replica      StorageReplica  `json:"replica"`
	Backup       StorageBackup   `json:"backup"`
	Warning      StorageWarning  `json:"warning"`
	// Chat is v10's block (storage_chat.go). Omitted entirely when no module
	// reports storage groups — a household running a build without chat gets no
	// empty block rather than one full of zeroes it cannot explain.
	Chat *StorageChat `json:"chat,omitempty"`
}

type StorageDatabase struct {
	TotalBytes int64  `json:"total_bytes"`
	WALBytes   int64  `json:"wal_bytes"`
	FreeBytes  *int64 `json:"free_bytes"`
	// BytesAvailable reports whether PER-TABLE bytes could be measured — i.e.
	// whether the SQLite build exposes `dbstat`. Probed on first use and never
	// assumed; a probe that fails is retried on the next snapshot rather than
	// latched, so one cancelled request cannot blank this for the process
	// (storage.Prober). TotalBytes above is exact either way (D193).
	BytesAvailable bool              `json:"bytes_available"`
	Modules        []StorageModuleDB `json:"modules"`
}

type StorageModuleDB struct {
	Module string             `json:"module"`
	Bytes  *int64             `json:"bytes"`
	Tables []StorageTableInfo `json:"tables"`
}

type StorageTableInfo struct {
	Name     string `json:"name"`
	RowCount int64  `json:"row_count"`
	// Virtual marks a VIRTUAL table (Home's four external-content FTS5 indexes).
	// It owns no b-tree, so its bytes are a measured ZERO rather than an unmeasured
	// null — the shadow tables listed beside it carry every page. The flag is what
	// lets the page say so instead of showing a 0 B that reads as an empty index,
	// and it is why row_count is 0 here (see storage.TableStats.Virtual).
	Virtual    bool   `json:"virtual"`
	Bytes      *int64 `json:"bytes"`
	IndexBytes *int64 `json:"index_bytes"`
}

type StorageBlobs struct {
	// Available is false when object storage could not be reached. The endpoint
	// still returns 200 with the database figures intact — a bucket outage must
	// never blank the page, and a 5xx carrying partial results is a shape no
	// client handles (FR-V9-11).
	Available    bool                 `json:"available"`
	Error        *string              `json:"error"`
	Bucket       *string              `json:"bucket"`
	TotalBytes   *int64               `json:"total_bytes"`
	TotalObjects *int64               `json:"total_objects"`
	Modules      []StorageModuleBlobs `json:"modules"`
}

type StorageModuleBlobs struct {
	Module  string              `json:"module"`
	Prefix  string              `json:"prefix"`
	Bytes   *int64              `json:"bytes"`
	Objects *int64              `json:"objects"`
	Owners  []StorageOwnerUsage `json:"owners"`
}

type StorageOwnerUsage struct {
	// Kind is shared | private | unattributed.
	//
	// `unattributed` is objects in the prefix that resolve to NO live row. It is
	// not an error state and not padding: it is the orphan backlog the mirror job
	// already reconciles, and this is the first screen in Home that has ever shown
	// it (D194).
	Kind        string  `json:"kind"`
	OwnerUserID *string `json:"owner_user_id"`
	OwnerLabel  *string `json:"owner_label"`
	Bytes       int64   `json:"bytes"`
	Objects     int64   `json:"objects"`
	// Module is set only on warning.largest_contributors rows, where every
	// module's usage is flattened into one list and two same-kind rows would
	// otherwise be indistinguishable. Inside StorageModuleBlobs.Owners the
	// parent block already names the module, so it stays empty there.
	Module string `json:"module,omitempty"`
}

// StorageReplica is the Litestream replica line.
//
// ⚠ IT IS PERMANENTLY `configured: false`, and that is a DECISION rather than an
// unfinished feature (PRD §V9-12, settled with Karel 2026-08-24).
//
// D214 specified this line — objects, bytes, generations, and `newest_at`, the
// last being the practical answer to "is replication actually running?". Reading
// it needs Litestream's bucket and keys, which exist in the environment but are
// consumed only by docker-entrypoint.sh and the litestream process. Handing them
// to the application would introduce no NEW secret while widening what the app
// process can reach to the credentials for the household's entire database backup.
// Karel declined that trade.
//
// ⚠ ANYONE WHO FINDS THIS FALSE, READS D214, AND WIRES THE CREDENTIALS IN HAS
// REVERSED A DECISION, NOT FIXED AN OVERSIGHT. The question it answered goes back
// to the droplet (`litestream snapshots`). If the liveness answer is ever wanted
// inside the app, the route is Litestream's own metrics endpoint bound to
// localhost — no secret, no bucket access — not these keys.
//
// The struct stays in the payload so the shape does not change if that ever
// happens, and so the frontend has a designed state to render (HANDOFF-design §v9
// lists "no backup bucket and no replica configured" among the states to draw).
type StorageReplica struct {
	Configured  bool    `json:"configured"`
	Prefix      *string `json:"prefix"`
	Bytes       *int64  `json:"bytes"`
	Objects     *int64  `json:"objects"`
	Generations *int64  `json:"generations"`
	NewestAt    *string `json:"newest_at"`
}

// StorageBackup is the mirror bucket as ONE line (D205). It is half the R2 bill
// and no screen has ever shown it.
//
// Unlike the replica above this one IS populated: the mirror's credentials
// (HOME_DOCS_R2_BACKUP_*) are already the app's own — it is the app that writes
// the mirror — so reading it widens nothing.
type StorageBackup struct {
	Configured bool    `json:"configured"`
	Bucket     *string `json:"bucket"`
	Bytes      *int64  `json:"bytes"`
	Objects    *int64  `json:"objects"`
}

// StorageWarning is one threshold on the MODULES' primary-bucket total (D196).
//
// ⚠ Nothing is ever blocked by it: no upload fails, there is no per-user quota and
// no new 413. And it is measured against the per-module total ONLY — the replica
// and the mirror are derived copies and sit outside the breakdown, so folding them
// in would make the 1 GB default fire permanently and the register would stop
// meaning anything.
type StorageWarning struct {
	ThresholdMB         int                 `json:"threshold_mb"`
	MeasuredBytes       *int64              `json:"measured_bytes"`
	Exceeded            bool                `json:"exceeded"`
	LargestContributors []StorageOwnerUsage `json:"largest_contributors"`
}

// StorageDeps is what the snapshot needs from the composition root.
type StorageDeps struct {
	DB     *sql.DB
	DBPath string
	// Catalog is the storage registry — how `admin` reaches ten modules without
	// importing one (D191).
	Catalog *storage.Registry
	// Primary and Backup are the object stores. Backup may be nil.
	Primary       blobstore.BlobStore
	PrimaryBucket string
	Backup        blobstore.BlobStore
	BackupBucket  string
	// Members labels the per-member rows. Satisfied by *push.Store, which projects
	// the directory from sessions — no new table and no new dependency.
	Members      MemberDirectory
	WarnTotalMB  int
	CacheSeconds int
}

// MemberDirectory is the slice of platform/push's store this page needs. Narrow on
// purpose: the storage page wants names for ids, not a push channel.
type MemberDirectory interface {
	Members(ctx context.Context) ([]push.Member, error)
}

// StorageService computes the snapshot and holds the short-lived cache.
type StorageService struct {
	deps   StorageDeps
	prober storage.Prober
	now    func() time.Time

	mu       sync.Mutex
	cached   *StorageSnapshot
	cachedAt time.Time
	// generation counts SUCCESSFUL computes, and it is what makes a waiter able to
	// tell "the pass I joined landed" from "something landed at some point".
	//
	// ⚠ `cached != nil` cannot answer that question. It stays non-nil after a
	// FAILED compute — holding whatever succeeded last, possibly hours ago — so a
	// waiter that only checked for nil was handed a stale snapshot as though it
	// were the answer to its own request, with the failure reported to nobody. The
	// counter moves only when a compute actually produces figures.
	generation uint64
	// computing is the SINGLE-FLIGHT gate: non-nil while one compute is in progress,
	// closed when it finishes. See Snapshot for why a snapshot is worth not
	// computing twice.
	computing chan struct{}
}

// NewStorageService builds the snapshot service.
func NewStorageService(deps StorageDeps) *StorageService {
	return &StorageService{deps: deps, now: time.Now}
}

// Snapshot returns the storage picture, from cache unless refresh is set.
//
// The cached copy is returned with Cached=true so a stale figure is VISIBLY stale
// rather than silently so — the same reason GeneratedAt records when the snapshot
// was computed rather than when it was asked for.
//
// ⚠ IT IS ALSO SINGLE-FLIGHT, because a cache miss is expensive in a way the
// one-line call hides: compute() runs a COUNT(*) per declared table, a full dbstat
// scan, a complete LIST of the primary bucket and a LIST per module prefix against
// the mirror. Releasing the lock before computing meant every concurrent miss ran
// ALL of that — an admin double-clicking Obnovit, or two admins opening Úložiště in
// the same second after a restart or a TTL expiry, multiplied the whole thing
// against one SQLite file and one pair of buckets. Callers that arrive during a
// compute now WAIT for it and read its answer, which is the answer they would have
// computed anyway.
//
// The wait is on a channel rather than under the mutex so the mutex never spans a
// query: a caller blocked here still respects its own ctx and gives up if the admin
// closes the tab, while the in-flight compute carries on for whoever is left.
func (s *StorageService) Snapshot(ctx context.Context, refresh bool) (*StorageSnapshot, error) {
	ttl := time.Duration(s.deps.CacheSeconds) * time.Second
	for {
		s.mu.Lock()
		if !refresh && ttl > 0 && s.cached != nil && s.now().Sub(s.cachedAt) < ttl {
			out := *s.cached
			out.Cached = true
			s.mu.Unlock()
			return &out, nil
		}
		// ttl > 0 gates the coalescing as well as the cache: CacheSeconds 0 means
		// "every request gets its own figures", and handing such a caller somebody
		// else's snapshot marked Cached would quietly re-enable what they turned off.
		if wait := s.computing; wait != nil && ttl > 0 {
			// Somebody is already computing exactly this. Wait for them and take their
			// answer rather than starting a second identical pass.
			//
			// A `refresh` caller coalesces here too, deliberately: the double-clicked
			// Obnovit IS two refreshes arriving together, so refusing to join would
			// leave the case this exists for unfixed. Joining costs at most the few
			// seconds the in-flight pass had already run — against a page with a
			// 60-second TTL whose generated_at states exactly when it was computed.
			gen := s.generation
			s.mu.Unlock()
			select {
			case <-wait:
				// Take the result of the pass we waited on, not the TTL cache: a
				// refresh caller must not fall back through the TTL branch and be
				// handed something older than what just landed.
				//
				// ⚠ WHICH IS A QUESTION ABOUT THE GENERATION, NOT ABOUT nil. A failed
				// compute leaves `cached` exactly as it was, so a nil check passes on
				// a snapshot from any point in the past: the bucket goes down at 11:00,
				// the pass fails, and every caller waiting on it is handed 09:00's
				// figures marked Cached with a 200 — the refresh silently not landing,
				// the error reported only to the one caller that ran the pass. Comparing
				// the counter asks whether THIS pass produced anything.
				s.mu.Lock()
				got, landed := s.cached, s.generation != gen
				s.mu.Unlock()
				if !landed || got == nil {
					// That pass failed. Compute our own rather than report its error,
					// which belongs to another request.
					refresh = true
					continue
				}
				out := *got
				out.Cached = true
				return &out, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		done := make(chan struct{})
		s.computing = done
		s.mu.Unlock()

		snap, err := s.compute(ctx)

		s.mu.Lock()
		s.computing = nil
		if err == nil {
			s.cached, s.cachedAt = snap, s.now()
			// Only a pass that produced figures moves the counter — see the field.
			s.generation++
		}
		s.mu.Unlock()
		close(done)

		if err != nil {
			return nil, err
		}
		out := *snap
		return &out, nil
	}
}

func (s *StorageService) compute(ctx context.Context) (*StorageSnapshot, error) {
	out := &StorageSnapshot{
		GeneratedAt:  s.now().UTC().Format(time.RFC3339),
		CacheSeconds: s.deps.CacheSeconds,
		// See the type comment: declined, not unimplemented (PRD §V9-12).
		Replica: StorageReplica{Configured: false},
	}

	db, err := s.measureDatabase(ctx)
	if err != nil {
		return nil, err
	}
	out.Database = db
	out.Blobs = s.measureBlobs(ctx)
	// After Blobs, and dependent on it: the mirror's prefixes are the modules'
	// prefixes, resolved once above rather than restated here.
	out.Backup = s.measureBackup(ctx, out.Blobs)
	out.Warning = s.warning(out.Blobs)
	// v10: the chat block, from the catalog's GroupSource plus the two DB-backed
	// thresholds. Nil when no module reports groups, so a household without chat
	// gets no empty block rather than one full of zeroes.
	out.Chat = s.measureChat(ctx)
	return out, nil
}

// Invalidate drops the cached snapshot.
//
// ⚠ IT EXISTS FOR ONE CALLER: saving a threshold (SetThresholds). The two limits
// ride the snapshot rather than having a GET of their own, so a save followed by
// the page re-reading a 60-second-old cache shows the OLD number back — which reads
// as "it did not take", on a field that autosaves on blur and has no Save button to
// press again.
func (s *StorageService) Invalidate() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.cached, s.cachedAt = nil, time.Time{}
	s.mu.Unlock()
}

func (s *StorageService) measureDatabase(ctx context.Context) (StorageDatabase, error) {
	// Asked of the catalog directly rather than by inverting TableOwners() by
	// hand: Tables() IS the module → tables accessor, and it returns the list
	// already sorted, so the hand-rolled inversion was a second spelling of a
	// question the registry answers. TableOwners() stays what it was built for —
	// the completeness test's table → module direction.
	byModule := map[string][]string{}
	for _, module := range s.deps.Catalog.Modules() {
		// A module that declares nothing gets no row, exactly as it did when this
		// map was built from the owners index: a page listing `events — 0 tables`
		// for a module that simply owns no b-tree is noise, not information.
		if tables := s.deps.Catalog.Tables(module); len(tables) > 0 {
			byModule[module] = tables
		}
	}
	// The platform's own tables are a module on this page like any other, so the
	// per-module figures sum to the exact database total (D211).
	byModule["platform"] = append(byModule["platform"], storage.PlatformTables...)

	var all []string
	for _, tables := range byModule {
		all = append(all, tables...)
	}
	stats, err := storage.MeasureDatabase(ctx, s.deps.DB, s.deps.DBPath, &s.prober, all)
	if err != nil {
		return StorageDatabase{}, err
	}
	byName := map[string]storage.TableStats{}
	for _, t := range stats.Tables {
		byName[t.Name] = t
	}

	out := StorageDatabase{
		TotalBytes:     stats.TotalBytes,
		WALBytes:       stats.WALBytes,
		FreeBytes:      stats.FreeBytes,
		BytesAvailable: stats.BytesAvailable,
		Modules:        []StorageModuleDB{},
	}
	names := make([]string, 0, len(byModule))
	for m := range byModule {
		names = append(names, m)
	}
	sort.Strings(names)
	for _, module := range names {
		row := StorageModuleDB{Module: module, Tables: []StorageTableInfo{}}
		var sum int64
		var measured bool
		tables := byModule[module]
		sort.Strings(tables)
		for _, name := range tables {
			t, ok := byName[name]
			if !ok {
				continue
			}
			info := StorageTableInfo{
				Name: t.Name, RowCount: t.RowCount, Virtual: t.Virtual,
				Bytes: t.Bytes, IndexBytes: t.IndexBytes,
			}
			if t.Bytes != nil {
				measured = true
				sum += *t.Bytes
			}
			if t.IndexBytes != nil {
				sum += *t.IndexBytes
			}
			row.Tables = append(row.Tables, info)
		}
		if measured {
			total := sum
			row.Bytes = &total
		}
		out.Modules = append(out.Modules, row)
	}
	return out, nil
}

func (s *StorageService) measureBlobs(ctx context.Context) StorageBlobs {
	out := StorageBlobs{Modules: []StorageModuleBlobs{}}
	if s.deps.Primary == nil {
		msg := "object storage is not configured"
		out.Error = &msg
		return out
	}
	usage, err := s.deps.Catalog.Blobs(ctx)
	if err != nil {
		// A bucket outage is a 200 with the reason, never a 500 and never a blank
		// page: blanking loses the half of the answer that WAS measurable.
		msg := err.Error()
		out.Error = &msg
		return out
	}
	out.Available = true
	if s.deps.PrimaryBucket != "" {
		b := s.deps.PrimaryBucket
		out.Bucket = &b
	}
	labels := s.memberLabels(ctx)

	var grandBytes, grandObjects int64
	modules := make([]string, 0, len(usage))
	for m := range usage {
		modules = append(modules, m)
	}
	sort.Strings(modules)
	for _, module := range modules {
		rows := usage[module]
		mod := StorageModuleBlobs{Module: module, Owners: []StorageOwnerUsage{}}
		var bytes, objects int64
		for _, u := range rows {
			if mod.Prefix == "" {
				mod.Prefix = u.Prefix
			}
			bytes += u.Bytes
			objects += u.Objects
			row := StorageOwnerUsage{Kind: u.Kind, Bytes: u.Bytes, Objects: u.Objects}
			if u.Kind == storage.KindPrivate {
				id := u.OwnerID
				row.OwnerUserID = &id
				if label, ok := labels[id]; ok && label != "" {
					l := label
					row.OwnerLabel = &l
				}
			}
			mod.Owners = append(mod.Owners, row)
		}
		// shared, then each member, then whatever resolved to nothing — the order
		// the page reads in.
		sort.SliceStable(mod.Owners, func(i, j int) bool {
			return kindRank(mod.Owners[i].Kind) < kindRank(mod.Owners[j].Kind)
		})
		mod.Bytes, mod.Objects = &bytes, &objects
		grandBytes += bytes
		grandObjects += objects
		out.Modules = append(out.Modules, mod)
	}
	out.TotalBytes, out.TotalObjects = &grandBytes, &grandObjects
	return out
}

func kindRank(kind string) int {
	switch kind {
	case storage.KindShared:
		return 0
	case storage.KindPrivate:
		return 1
	default:
		return 2
	}
}

// memberLabels projects the member directory for the per-member rows. `admin`
// already imports platform/push, whose store derives the directory from sessions,
// so this needs no new dependency and no new table.
//
// A missing label is left null rather than filled with the raw user id: the page
// then says "an id we cannot name" instead of pretending the id is a name.
func (s *StorageService) memberLabels(ctx context.Context) map[string]string {
	out := map[string]string{}
	if s.deps.Members == nil {
		return out
	}
	members, err := s.deps.Members.Members(ctx)
	if err != nil {
		// A directory that will not load costs the page its NAMES, not its numbers.
		// Degrading to unlabelled rows is strictly better than failing the request.
		return out
	}
	for _, m := range members {
		label := m.DisplayName
		if label == "" {
			label = m.Email
		}
		out[m.UserID] = label
	}
	return out
}

// measureBackup sizes the mirror bucket (D205).
//
// ⚠ THE PREFIXES COME FROM THE CATALOG, never from a literal here. Which prefixes
// exist in the docs bucket is a fact only the owning modules hold — that is the
// whole argument of platform/storage's package doc — and `admin` re-deriving it by
// hand is how this line silently falls behind a module that adds one, with no test
// to catch it: the completeness guard covers SQLite tables, not blob prefixes.
//
// `blobs` is the snapshot's own primary-bucket measurement, already computed a
// line earlier, so the prefixes are reused rather than resolved twice.
//
// When the primary could not be listed there are no prefixes to mirror, so the
// figures stay NULL rather than reporting a zero nobody measured (D161/D193).
func (s *StorageService) measureBackup(ctx context.Context, blobs StorageBlobs) StorageBackup {
	if s.deps.Backup == nil || s.deps.BackupBucket == "" {
		return StorageBackup{Configured: false}
	}
	bucket := s.deps.BackupBucket
	out := StorageBackup{Configured: true, Bucket: &bucket}
	if !blobs.Available {
		return out
	}
	var bytes, objects int64
	for _, m := range blobs.Modules {
		if m.Prefix == "" {
			continue
		}
		objs, err := s.deps.Backup.List(ctx, m.Prefix)
		if err != nil {
			// Same rule as the primary: report what is measurable, leave the rest
			// null rather than zero.
			return StorageBackup{Configured: true, Bucket: &bucket}
		}
		for _, o := range objs {
			bytes += o.Size
			objects++
		}
	}
	out.Bytes, out.Objects = &bytes, &objects
	return out
}

// warning evaluates the threshold against the MODULES' total only (D196).
func (s *StorageService) warning(blobs StorageBlobs) StorageWarning {
	out := StorageWarning{ThresholdMB: s.deps.WarnTotalMB, LargestContributors: []StorageOwnerUsage{}}
	if !blobs.Available || blobs.TotalBytes == nil {
		return out
	}
	measured := *blobs.TotalBytes
	out.MeasuredBytes = &measured
	if s.deps.WarnTotalMB <= 0 {
		return out
	}
	out.Exceeded = measured > int64(s.deps.WarnTotalMB)<<20
	if !out.Exceeded {
		return out
	}
	var all []StorageOwnerUsage
	for _, m := range blobs.Modules {
		for _, o := range m.Owners {
			// Stamp the module: flattened same-kind rows from different modules
			// are indistinguishable without it (the spec promises "module + owner
			// bucket").
			o.Module = m.Module
			all = append(all, o)
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].Bytes > all[j].Bytes })
	if len(all) > 5 {
		all = all[:5]
	}
	out.LargestContributors = all
	return out
}

// PrivateItemPage is the purge screen's listing (D198).
type PrivateItemPage struct {
	Items      []PrivateItem `json:"items"`
	NextCursor *string       `json:"next_cursor"`
	// TotalBytes covers ALL matching items, not only this page — it is the figure
	// the screen exists to act on, and it stays complete even when `sort=size`
	// truncates the list.
	TotalBytes *int64 `json:"total_bytes"`
}

// PrivateItem is one private item as an admin may see it.
//
// ⚠ THE FIELDS THAT ARE ABSENT ARE THE SPECIFICATION (D198): no title, no
// filename, no description, no content type, no preview, no download URL. An admin
// can name the thing well enough to delete it and not well enough to know what it
// is. Anything added here has to be justified against that sentence.
type PrivateItem struct {
	ID          string  `json:"id"`
	Module      string  `json:"module"`
	Kind        string  `json:"kind"`
	OwnerUserID string  `json:"owner_user_id"`
	OwnerLabel  *string `json:"owner_label"`
	ByteSize    int64   `json:"byte_size"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   *string `json:"updated_at"`
}

// InventoryModules is the set of module names `?module=` may name on the purge
// listing — the modules that actually implement storage.PrivateInventory, asked of
// the catalog rather than listed by hand in the handler. See the registry's copy.
func (s *StorageService) InventoryModules() []string {
	return s.deps.Catalog.InventoryModules()
}

// PrivateItems assembles the purge listing across modules, through the catalog.
//
// ⚠ DELETION IS NOT HERE, deliberately (D198). The SPA calls the owning module's
// existing hard-delete route — DELETE /api/documents/{id}?hard=true, DELETE
// /api/notes/{id}?hard=true, and for a subtree the folder route with
// cascade=true — so the audit action stays the MODULE's and `admin` gains no
// delete path of its own.
func (s *StorageService) PrivateItems(ctx context.Context, f storage.ItemFilter) (PrivateItemPage, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	f.Limit = limit

	items, total, err := s.deps.Catalog.PrivateItems(ctx, f)
	if err != nil {
		return PrivateItemPage{}, err
	}
	labels := s.memberLabels(ctx)

	page := PrivateItemPage{Items: []PrivateItem{}, TotalBytes: &total}
	var next *string
	if f.Sort != storage.SortSize && len(items) > limit {
		last := items[limit-1].ID
		items = items[:limit]
		next = &last
	} else if len(items) > limit {
		// ⚠ `sort=size` is SINGLE-PAGE on purpose: a keyset cursor is an id, and an
		// id does not locate a position in a size ordering. Paging it would need a
		// composite (bytes|id) cursor, and this collection is not big enough to earn
		// one. TotalBytes above still covers everything, so the figure the screen
		// acts on is complete even though the list is truncated.
		items = items[:limit]
	}
	for _, it := range items {
		row := PrivateItem{
			ID: it.ID, Module: it.Module, Kind: it.Kind,
			OwnerUserID: it.OwnerID, ByteSize: it.ByteSize, CreatedAt: it.CreatedAt,
		}
		if label, ok := labels[it.OwnerID]; ok && label != "" {
			l := label
			row.OwnerLabel = &l
		}
		if it.UpdatedAt != "" {
			u := it.UpdatedAt
			row.UpdatedAt = &u
		}
		page.Items = append(page.Items, row)
	}
	page.NextCursor = next
	return page, nil
}
