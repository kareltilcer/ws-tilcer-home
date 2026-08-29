package notes

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/blobstore"
)

// Durability for note-image bytes (full parity with documents, D45). Litestream
// replicates the SQLite WAL — including note_images — but cannot back up a blob
// bucket, so the objects get their own job: a MIRROR into a second bucket plus a
// RECONCILIATION pass. It is deliberately a near-twin of documents' MirrorJob rather
// than a shared abstraction, so a change to one module's storage can never quietly
// alter the other's; the only differences are the key prefix and the "expected
// objects" source. R2 has no bucket versioning, so the mirror bucket is the real
// delete safety net and safeToDelete below bounds the one destructive act.
//
// Reconciliation compares "objects the rows claim" against "objects that exist":
//   - ORPHANED OBJECT (in the bucket, no row): an upload that crashed between the Put
//     and the row insert, or a GC/purge whose delete failed. Deleted once older than
//     the grace window — young orphans may be uploads still in flight.
//   - DANGLING ROW (row claims a missing object): only ever LOGGED — a human decides.

// ImageMirrorConfig configures the daily note-image job.
type ImageMirrorConfig struct {
	Interval    time.Duration
	OrphanGrace time.Duration
	// MaxOrphanShare is the fraction of the (note-image) bucket one pass may delete
	// before it refuses and logs instead. <= 0 takes the default.
	MaxOrphanShare float64
	// Backup is the mirror target; nil disables mirroring (reconciliation still runs).
	Backup blobstore.BlobStore
	Logger *slog.Logger
}

const (
	imageDefaultMaxOrphanShare = 0.25
	imageOrphanDeleteFloor     = 5
)

// NoteImageMirrorJob mirrors the note-image objects and reconciles rows against them.
type NoteImageMirrorJob struct {
	store   *Store
	primary blobstore.BlobStore
	cfg     ImageMirrorConfig
	logger  *slog.Logger
}

// NewImageMirrorJob builds the job. primary is the store note images live in.
func NewImageMirrorJob(store *Store, primary blobstore.BlobStore, cfg ImageMirrorConfig) *NoteImageMirrorJob {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.OrphanGrace <= 0 {
		cfg.OrphanGrace = 24 * time.Hour
	}
	if cfg.MaxOrphanShare <= 0 {
		cfg.MaxOrphanShare = imageDefaultMaxOrphanShare
	}
	return &NoteImageMirrorJob{store: store, primary: primary, cfg: cfg, logger: cfg.Logger}
}

// ImageMirrorReport is one pass's outcome, logged as a single structured line.
type ImageMirrorReport struct {
	Copied              int
	AlreadyThere        int
	CopyFailed          int
	Orphans             int
	OrphansDeleted      int
	OrphansBlocked      int
	Dangling            int
	Unreferenced        int
	UnreferencedDeleted int
	UnreferencedBlocked int
	Duration            time.Duration
}

// Run starts the daily loop; it returns immediately and stops with ctx.
func (j *NoteImageMirrorJob) Run(ctx context.Context) {
	if j.primary == nil {
		return // no object store configured (e.g. images disabled)
	}
	if j.cfg.Interval <= 0 {
		// primary != nil here, so note images ARE being stored but would get no backup and
		// no reconciliation. The interval is inherited from the documents mirror config
		// (the objects share that bucket), so a documents-scoped setting silently governs
		// note-image durability — surface it as a Warn, naming the coupling, rather than as
		// a routine Info line an operator reads as intentional.
		j.logger.Warn("notes: image mirror + reconciliation DISABLED (interval<=0) while note images are " +
			"enabled — uploaded images get no backup and orphaned objects are never reclaimed; this follows " +
			"the documents mirror interval (HOME_DOCS_*), which also governs note images")
		return
	}
	go func() {
		// A short delay keeps the first pass out of the boot path.
		firstRun := time.NewTimer(2 * time.Minute)
		defer firstRun.Stop()
		select {
		case <-ctx.Done():
			return
		case <-firstRun.C:
		}
		ticker := time.NewTicker(j.cfg.Interval)
		defer ticker.Stop()
		for {
			j.RunOnce(ctx)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

// RunOnce performs one mirror + reconciliation pass. Exported so a test can drive it.
func (j *NoteImageMirrorJob) RunOnce(ctx context.Context) ImageMirrorReport {
	started := time.Now()
	report := ImageMirrorReport{}

	expected, err := j.store.ExpectedImageObjects(ctx)
	if err != nil {
		j.logger.Error("notes: image mirror cannot list expected objects", "err", err)
		return report
	}

	// Reclaim leaked rows (an image no note references) BEFORE listing/mirroring, so a
	// row+object we're about to delete isn't first copied into the backup and then
	// orphaned there. Removing the swept ids from `expected` keeps `claimed` consistent
	// with what the sweep left behind.
	if swept := j.sweepUnreferencedRows(ctx, len(expected), &report); len(swept) > 0 {
		gone := make(map[string]bool, len(swept))
		for _, id := range swept {
			gone[id] = true
		}
		kept := expected[:0]
		for _, e := range expected {
			if !gone[e.ImageID] {
				kept = append(kept, e)
			}
		}
		expected = kept
	}

	objects, err := j.primary.List(ctx, noteImageKeyPrefix)
	if err != nil {
		j.logger.Error("notes: image mirror cannot list the primary bucket", "err", err)
		return report
	}

	present := make(map[string]blobstore.ObjInfo, len(objects))
	for _, o := range objects {
		present[o.Key] = o
	}
	claimed := make(map[string]ImageObjectRef, len(expected))
	for _, e := range expected {
		claimed[e.Key] = e
	}

	// Reconcile FIRST, then mirror — otherwise an orphan is copied into the backup and
	// only afterwards removed from the primary, and nothing sweeps the backup.
	j.reconcile(ctx, present, claimed, &report)
	j.mirror(ctx, objects, claimed, &report)

	report.Duration = time.Since(started)
	j.logger.Info("notes: image mirror + reconciliation pass",
		"copied", report.Copied,
		"already_present", report.AlreadyThere,
		"copy_failed", report.CopyFailed,
		"orphans", report.Orphans,
		"orphans_deleted", report.OrphansDeleted,
		"orphans_blocked", report.OrphansBlocked,
		"dangling_rows", report.Dangling,
		"unreferenced", report.Unreferenced,
		"unreferenced_deleted", report.UnreferencedDeleted,
		"unreferenced_blocked", report.UnreferencedBlocked,
		"duration", report.Duration,
	)
	return report
}

// sweepUnreferencedRows reclaims note_images rows that NO note references anymore — an
// upload whose embedding body-save never landed, or a stray duplicate. Such a row is
// invisible to the orphan-object sweep (its object HAS a row) and to edit-time GC
// (which only runs when a body changes), so without this pass it leaks forever. The
// row and its object are deleted together, past the same grace window that spares an
// in-flight upload. Returns the reclaimed ids so the caller can drop them from the
// expected set. Guarded like the orphan sweep: an empty/half-restored notes table
// makes every image look unreferenced, so it refuses on those shapes.
func (j *NoteImageMirrorJob) sweepUnreferencedRows(ctx context.Context, totalImages int, report *ImageMirrorReport) []string {
	if totalImages == 0 {
		return nil
	}
	notesCount, err := j.store.CountNotes(ctx)
	if err != nil {
		j.logger.Error("notes: image sweep cannot count notes", "err", err)
		return nil
	}
	if notesCount == 0 {
		j.logger.Error("notes: image row sweep REFUSED — the notes table is empty, which reads as an "+
			"unrestored database rather than a bucket of leaks", "images", totalImages)
		return nil
	}
	cutoff := time.Now().Add(-j.cfg.OrphanGrace).UTC().Format(tsFormat)
	ids, err := j.store.UnreferencedImageIDs(ctx, cutoff)
	if err != nil {
		j.logger.Error("notes: image sweep cannot list unreferenced rows", "err", err)
		return nil
	}
	if len(ids) == 0 {
		return nil
	}
	report.Unreferenced = len(ids)
	// Blast-radius bound: a partially-restored notes table makes many images look
	// unreferenced. A small handful passes (real leaks); a large share refuses.
	share := float64(len(ids)) / float64(totalImages)
	if len(ids) > imageOrphanDeleteFloor && share > j.cfg.MaxOrphanShare {
		report.UnreferencedBlocked = len(ids)
		j.logger.Error("notes: image row sweep REFUSED — unreferenced share exceeds the safety limit; "+
			"check that the notes table is fully restored before clearing this",
			"unreferenced", len(ids), "images", totalImages, "share", share, "limit", j.cfg.MaxOrphanShare)
		return nil
	}
	if err := j.store.DeleteNoteImages(ctx, j.store.db, ids); err != nil {
		j.logger.Warn("notes: deleting unreferenced image rows failed", "count", len(ids), "err", err)
		return nil
	}
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = NoteImageKey(id)
	}
	if err := j.primary.Delete(ctx, keys...); err != nil {
		// The rows are gone; the now-orphaned objects fall to the orphan sweep.
		j.logger.Warn("notes: deleting unreferenced image objects failed; left for the orphan sweep", "count", len(keys), "err", err)
	}
	report.UnreferencedDeleted = len(ids)
	j.logger.Info("notes: reclaimed unreferenced image rows", "count", len(ids), "keys", keys)
	return ids
}

// mirror copies into the backup bucket every CLAIMED object it does not already have.
// Objects are immutable, so "present" always means "identical".
func (j *NoteImageMirrorJob) mirror(ctx context.Context, objects []blobstore.ObjInfo, claimed map[string]ImageObjectRef, report *ImageMirrorReport) {
	if j.cfg.Backup == nil {
		return
	}
	backupObjects, err := j.cfg.Backup.List(ctx, noteImageKeyPrefix)
	if err != nil {
		j.logger.Error("notes: image mirror cannot list the backup bucket", "err", err)
		return
	}
	inBackup := make(map[string]bool, len(backupObjects))
	for _, o := range backupObjects {
		inBackup[o.Key] = true
	}
	for _, o := range objects {
		if _, ok := claimed[o.Key]; !ok {
			continue // unclaimed: in-flight upload or just-reconciled orphan; not the backup's job
		}
		if inBackup[o.Key] {
			report.AlreadyThere++
			continue
		}
		if err := j.primary.Copy(ctx, o.Key, o.Key, j.cfg.Backup); err != nil {
			report.CopyFailed++
			j.logger.Warn("notes: mirroring an image failed", "key", o.Key, "err", err)
			continue
		}
		report.Copied++
	}
}

// reconcile flags (and, for aged orphans, removes) the two ways rows and objects can
// disagree.
func (j *NoteImageMirrorJob) reconcile(ctx context.Context, present map[string]blobstore.ObjInfo, claimed map[string]ImageObjectRef, report *ImageMirrorReport) {
	cutoff := time.Now().Add(-j.cfg.OrphanGrace)

	ours := 0
	claimedPresent := 0
	var deletable []string
	for key, info := range present {
		if !strings.HasPrefix(key, noteImageKeyPrefix) {
			continue // not ours; leave it alone (this bucket is shared with documents)
		}
		ours++
		if _, ok := claimed[key]; ok {
			claimedPresent++
			continue
		}
		report.Orphans++
		if info.ModTime.IsZero() || info.ModTime.After(cutoff) {
			j.logger.Info("notes: orphaned image within the grace window, leaving it", "key", key, "modified", info.ModTime)
			continue
		}
		deletable = append(deletable, key)
	}
	if len(deletable) > 0 && !j.safeToDelete(len(deletable), ours, len(claimed), claimedPresent) {
		report.OrphansBlocked = len(deletable)
		deletable = nil
	}
	if len(deletable) > 0 {
		if err := j.primary.Delete(ctx, deletable...); err != nil {
			j.logger.Warn("notes: deleting orphaned images failed", "count", len(deletable), "err", err)
		} else {
			report.OrphansDeleted = len(deletable)
			j.logger.Info("notes: deleted orphaned images past the grace window", "count", len(deletable), "keys", deletable)
		}
	}

	for key, ref := range claimed {
		if _, ok := present[key]; ok {
			continue
		}
		report.Dangling++
		j.logger.Error("notes: DANGLING ROW — a note_images row claims an object that does not exist",
			"image_id", ref.ImageID, "key", key)
	}
}

// safeToDelete bounds the blast radius of the orphan sweep.
//
// "Orphan" is inferred from the ABSENCE of a row, so a pass is only as trustworthy
// as the database it just read. A boot against an empty or half-restored
// `note_images` table — a Litestream restore that was skipped or failed, a rebuilt
// volume that re-ran the migrations from scratch — makes every object in the bucket
// look orphaned, and two minutes later the first pass would delete every image
// pasted into every note. Nothing else reliably covers that: the mirror bucket is
// OPTIONAL (empty by default) and, on a first-ever pass, holds nothing yet; and R2
// has no object versioning to undo a delete. So when the database reads as empty or
// half-restored, refusing HERE is the only thing between it and an erased archive.
//
// A genuine orphan set is tiny — an upload that crashed between the Put and the
// commit, or a purge whose delete failed. Anything that looks like "most of the
// bucket" is a bug in the caller rather than 900 crashed uploads, so refuse, log,
// and make a human decide. The cost of refusing wrongly is some wasted storage; the
// cost of deleting wrongly is the images.
//
// Mirrors documents' MirrorJob.safeToDelete, deliberately (D40): the two jobs are
// one behaviour in two implementations, so a change to this reasoning belongs in
// both.
func (j *NoteImageMirrorJob) safeToDelete(orphans, ours, claimed, claimedPresent int) bool {
	if claimed == 0 {
		j.logger.Error("notes: image reconciliation REFUSED to delete — no row claims any object, "+
			"which reads as an empty or unrestored database rather than an empty bucket",
			"orphans", orphans, "objects", ours)
		return false
	}
	// The floor lets a small bucket out of the share guard, so it carries a bound of
	// its own: a handful of objects AND never more than the surviving rows still
	// account for. Without that second half the escape swallows the guard whole — a
	// household of 9 note images restored down to 4 rows leaves 5 orphans among 9
	// objects, which is 56%, far past any share limit and yet exactly ON a floor of 5.
	// Half a bucket looking orphaned is the same signal in 9 objects as it is in 900.
	if orphans <= imageOrphanDeleteFloor && orphans <= claimedPresent {
		return true
	}
	share := float64(orphans) / float64(ours)
	if share > j.cfg.MaxOrphanShare {
		j.logger.Error("notes: image reconciliation REFUSED to delete — orphan share exceeds the safety limit; "+
			"check that the database is fully restored before clearing this",
			"orphans", orphans, "objects", ours, "claimed", claimed, "share", share, "limit", j.cfg.MaxOrphanShare)
		return false
	}
	return true
}
