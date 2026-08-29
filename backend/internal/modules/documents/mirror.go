package documents

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/blobstore"
)

// Litestream replicates the SQLite WAL; it cannot back up a blob bucket. So the
// document BYTES get their own strategy (D45), part ops and part this file:
//
//  1. Metadata rides Litestream like every other table. Nothing to do here.
//  2. This job: a MIRROR into a second bucket plus a RECONCILIATION pass.
//
// D45 originally had a third leg — object VERSIONING on the primary bucket, a
// per-object undo for a bad delete — but Cloudflare R2 does not support bucket
// versioning (PutBucketVersioning/GetBucketVersioning are unimplemented), so there
// is no version history to roll back to. The MIRROR bucket is therefore the only
// second copy of the bytes, which makes HOME_DOCS_R2_BACKUP_BUCKET the real delete
// safety net rather than an optional extra — and the safeToDelete guard below the
// only thing standing between a short database and an erased archive.
//
// It runs daily (Karel's choice; HOME_DOCS_MIRROR_CRON is a Go duration, default
// 24h). Because objects are immutable, mirroring is copy-if-absent — cheap, and
// safe to run as often as you like.
//
// Reconciliation compares "objects the rows claim" against "objects that exist":
//   - ORPHANED OBJECT (in the bucket, no row): an upload that crashed between the
//     Put and the commit, or a purge whose delete failed. Deleted once older than
//     the grace window — young orphans may be in-flight uploads.
//   - DANGLING ROW (row claims an object that is gone): only ever LOGGED. Deleting
//     a user's document row because a storage read hiccuped would be far worse than
//     leaving a broken row to investigate.
//
// The orphan delete is the ONLY destructive act in this file, and it infers "orphan"
// from the ABSENCE of a row — so it is only ever as trustworthy as the database it
// reads. safeToDelete below bounds its blast radius accordingly.

// MirrorConfig configures the daily job.
type MirrorConfig struct {
	Interval    time.Duration
	OrphanGrace time.Duration
	// MaxOrphanShare is the fraction of the bucket one pass may delete before it
	// refuses and logs instead (see safeToDelete). <= 0 takes the default; 1 lets a
	// pass delete everything it considers orphaned.
	MaxOrphanShare float64
	// Backup is the mirror target; nil disables mirroring (reconciliation still runs).
	Backup blobstore.BlobStore
	Logger *slog.Logger
}

const (
	// defaultMaxOrphanShare: a healthy bucket's orphan set is a handful of crashed
	// uploads, never a quarter of the archive.
	defaultMaxOrphanShare = 0.25
	// orphanDeleteFloor is the count a pass may delete even when the share guard would
	// trip, so a small bucket can still reclaim a crashed upload (1 orphan out of 3
	// objects is 33%). It stays deliberately small: an upload that crashes between the
	// Put and the commit leaves exactly ONE object behind and a purge whose delete
	// failed leaves at most three, so a handful covers every genuine case — while a
	// bigger orphan set in a bucket too small to trip the share is the signature of a
	// database that came back short.
	orphanDeleteFloor = 5
)

// MirrorJob mirrors the primary bucket and reconciles rows against objects.
type MirrorJob struct {
	store   *Store
	primary blobstore.BlobStore
	cfg     MirrorConfig
	logger  *slog.Logger
}

func NewMirrorJob(store *Store, primary blobstore.BlobStore, cfg MirrorConfig) *MirrorJob {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.OrphanGrace <= 0 {
		cfg.OrphanGrace = 24 * time.Hour
	}
	if cfg.MaxOrphanShare <= 0 {
		cfg.MaxOrphanShare = defaultMaxOrphanShare
	}
	return &MirrorJob{store: store, primary: primary, cfg: cfg, logger: cfg.Logger}
}

// MirrorReport is one pass's outcome, logged as a single structured line so the
// counts are visible in Coolify's log without an extra endpoint.
type MirrorReport struct {
	Copied         int
	AlreadyThere   int
	CopyFailed     int
	Orphans        int
	OrphansDeleted int
	// OrphansBlocked counts orphans the safety guard refused to delete this pass.
	// Non-zero means a human should look before the next pass.
	OrphansBlocked int
	Dangling       int
	Duration       time.Duration
}

// Run starts the daily loop. It returns immediately; the loop stops with ctx.
func (j *MirrorJob) Run(ctx context.Context) {
	if j.cfg.Interval <= 0 {
		j.logger.Info("documents: blob mirror disabled (HOME_DOCS_MIRROR_CRON=0)")
		return
	}
	go func() {
		// A short delay keeps the first pass out of the boot path, where it would
		// compete with migrations and the preview re-enqueue for the single DB
		// connection.
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

// RunOnce performs one mirror + reconciliation pass. Exported so it can be driven
// directly from a test (and, if ever needed, from an admin trigger).
func (j *MirrorJob) RunOnce(ctx context.Context) MirrorReport {
	started := time.Now()
	report := MirrorReport{}

	objects, err := j.primary.List(ctx, keyPrefix)
	if err != nil {
		j.logger.Error("documents: mirror pass cannot list the primary bucket", "err", err)
		return report
	}
	expected, err := j.store.ExpectedObjects(ctx)
	if err != nil {
		j.logger.Error("documents: mirror pass cannot list expected objects", "err", err)
		return report
	}

	present := make(map[string]blobstore.ObjInfo, len(objects))
	for _, o := range objects {
		present[o.Key] = o
	}
	claimed := make(map[string]ObjectRef, len(expected))
	for _, e := range expected {
		claimed[e.Key] = e
	}

	// Reconcile FIRST, then mirror. The other order copies an orphan into the backup
	// and only afterwards removes it from the primary — and nothing ever removes it
	// from the backup, so the backup bucket would slowly fill with bytes no row claims
	// (including the bytes of documents an admin explicitly purged).
	j.reconcile(ctx, present, claimed, &report)
	j.mirror(ctx, objects, claimed, &report)

	report.Duration = time.Since(started)
	j.logger.Info("documents: blob mirror + reconciliation pass",
		"copied", report.Copied,
		"already_present", report.AlreadyThere,
		"copy_failed", report.CopyFailed,
		"orphans", report.Orphans,
		"orphans_deleted", report.OrphansDeleted,
		"orphans_blocked", report.OrphansBlocked,
		"dangling_rows", report.Dangling,
		"duration", report.Duration,
	)
	return report
}

// mirror copies into the backup bucket every CLAIMED object the backup does not
// already have. Objects are immutable, so "present" always means "identical" — there
// is no need to compare sizes or checksums.
func (j *MirrorJob) mirror(ctx context.Context, objects []blobstore.ObjInfo, claimed map[string]ObjectRef, report *MirrorReport) {
	if j.cfg.Backup == nil {
		j.logger.Debug("documents: no backup bucket configured, skipping the mirror")
		return
	}
	backupObjects, err := j.cfg.Backup.List(ctx, keyPrefix)
	if err != nil {
		j.logger.Error("documents: mirror pass cannot list the backup bucket", "err", err)
		return
	}
	inBackup := make(map[string]bool, len(backupObjects))
	for _, o := range backupObjects {
		inBackup[o.Key] = true
	}
	for _, o := range objects {
		if _, ok := claimed[o.Key]; !ok {
			// Unclaimed: either an upload still in flight (its row appears in a moment and
			// the next pass mirrors it) or an orphan reconcile has just deleted. Neither
			// belongs in the backup, which has no sweep of its own.
			continue
		}
		if inBackup[o.Key] {
			report.AlreadyThere++
			continue
		}
		if err := j.primary.Copy(ctx, o.Key, o.Key, j.cfg.Backup); err != nil {
			report.CopyFailed++
			j.logger.Warn("documents: mirroring an object failed", "key", o.Key, "err", err)
			continue
		}
		report.Copied++
	}
}

// reconcile flags (and, for aged orphans, removes) the two ways rows and objects can
// disagree.
func (j *MirrorJob) reconcile(ctx context.Context, present map[string]blobstore.ObjInfo, claimed map[string]ObjectRef, report *MirrorReport) {
	cutoff := time.Now().Add(-j.cfg.OrphanGrace)

	ours := 0
	claimedPresent := 0
	var deletable []string
	for key, info := range present {
		if !strings.HasPrefix(key, keyPrefix) {
			continue // not ours; leave it alone
		}
		ours++
		if _, ok := claimed[key]; ok {
			claimedPresent++
			continue
		}
		report.Orphans++
		// A young orphan may be an upload in flight right now — the object is written
		// before the row exists, which is exactly the window this grace period covers.
		if info.ModTime.IsZero() || info.ModTime.After(cutoff) {
			j.logger.Info("documents: orphaned object within the grace window, leaving it",
				"key", key, "modified", info.ModTime)
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
			j.logger.Warn("documents: deleting orphaned objects failed", "count", len(deletable), "err", err)
		} else {
			report.OrphansDeleted = len(deletable)
			j.logger.Info("documents: deleted orphaned objects past the grace window",
				"count", len(deletable), "keys", deletable)
		}
	}

	for key, ref := range claimed {
		if _, ok := present[key]; ok {
			continue
		}
		report.Dangling++
		// Logged, never auto-repaired: a missing object is either a real data loss to
		// restore from the backup bucket or a listing anomaly, and both need a human.
		j.logger.Error("documents: DANGLING ROW — a document claims an object that does not exist",
			"document_id", ref.DocumentID, "kind", ref.Kind, "key", key)
	}
}

// safeToDelete bounds the blast radius of the orphan sweep.
//
// "Orphan" is inferred from the ABSENCE of a row, so a pass is only as trustworthy
// as the database it just read. A boot against an empty or half-restored `documents`
// table — a Litestream restore that was skipped or failed, a rebuilt volume that
// re-ran the migrations from scratch — makes every object in the bucket look
// orphaned, and two minutes later the first pass would delete the household's entire
// archive. Nothing else in D45 reliably covers that: the mirror bucket is OPTIONAL
// (empty by default) and, on a first-ever pass, holds nothing yet; and R2 has no
// object versioning to undo a delete. So when the database reads as empty or
// half-restored, refusing HERE is the only thing between it and an erased archive.
//
// A genuine orphan set is tiny — an upload that crashed between the Put and the
// commit, or a purge whose delete failed. Anything that looks like "most of the
// bucket" is a bug in the caller rather than 900 crashed uploads, so refuse, log, and
// make a human decide. The cost of refusing wrongly is some wasted storage; the cost
// of deleting wrongly is the documents.
//
// Mirrors NoteImageMirrorJob.safeToDelete in `notes`, deliberately (D40): the two
// jobs are one behaviour in two implementations, so a change to this reasoning
// belongs in both. It went out of step once already — the floor's second bound was
// explained here and nowhere else.
func (j *MirrorJob) safeToDelete(orphans, ours, claimed, claimedPresent int) bool {
	if claimed == 0 {
		j.logger.Error("documents: reconciliation REFUSED to delete — no live row claims any object at all, "+
			"which reads as an empty or unrestored database rather than an empty bucket",
			"orphans", orphans, "objects", ours)
		return false
	}
	// The floor lets a small bucket out of the share guard, so it carries a bound of its
	// own: a handful of objects AND never more than the surviving rows still account for.
	// Without that second half the escape swallows the guard whole — a household of 3
	// documents restored down to 1 row leaves 6 orphans among 9 objects, which is 67%,
	// far past any share limit and yet under any floor worth having. Half a bucket
	// looking orphaned is the same signal in 9 objects as it is in 900.
	if orphans <= orphanDeleteFloor && orphans <= claimedPresent {
		return true
	}
	share := float64(orphans) / float64(ours)
	if share > j.cfg.MaxOrphanShare {
		j.logger.Error("documents: reconciliation REFUSED to delete — the orphan share exceeds the safety limit; "+
			"check that the database is fully restored before clearing this",
			"orphans", orphans, "objects", ours, "claimed", claimed,
			"share", share, "limit", j.cfg.MaxOrphanShare)
		return false
	}
	return true
}

// ErrNoBackup reports a misconfigured mirror to a caller that wants to fail loudly.
var ErrNoBackup = errors.New("documents: no backup bucket configured")
