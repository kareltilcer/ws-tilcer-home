package chat

import (
	"context"
	"database/sql"
	"strconv"
	"time"

	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/db"
)

// The drain — chat's ONLY scheduler job (FR-V10-22, D247).
//
// ⚠ IT HAS NO ACTOR, AND THAT IS THE HAZARD §V10-4a NAMES. Every other read in this
// module resolves membership and a floor from a session; a job has neither, so every
// load it makes is an explicit any-membership variant with `AnyMembership` or
// `Due` in its name. §V9-12 records that v9's equivalents — the preview worker and
// the image GC — were the two leak-table rows no review had listed, because a
// background job has no viewer and therefore does not look like a read path.
//
// ⚠ AND IT NEVER READS A MESSAGE, A NAME OR A MEMBER. It reads `chat_deleted_keys`,
// which is deliberately shaped so there is nothing in it to leak (leak row 21):
// keys are `chat/{uuid}/original`, with no filename and no conversation in them.
// That is the whole reason the queue is a table of KEYS rather than of attachment
// ids — an id would need a join back into the module's content to be useful.
//
// ⚠ AND IT IS THE ONLY THING chat TAKES platform/scheduler FOR. One job, one
// registration, asserted against the job registry by the acceptance criteria.

// DrainInterval is the fifteen minutes FR-V10-22 specifies.
const DrainInterval = 15 * time.Minute

// drainBatch bounds one pass.
//
// ⚠ THE BOUND IS NOT ABOUT MEMORY, IT IS ABOUT THE SINGLE WRITER. The pool is
// capped at one connection (platform/db), so the DELETE that clears these rows
// blocks every write in the application for as long as it holds it — and a
// conversation purge can queue four hundred keys at once. A bounded pass that runs
// again in fifteen minutes costs a delay nobody can perceive; an unbounded one
// costs the household's whole app a pause it can.
const drainBatch = 500

// Drain deletes every object whose purge_after has passed, then clears its rows.
//
// ⚠ THE ROWS GO ONLY AFTER THE OBJECTS DO, and a key whose delete failed is LEFT IN
// THE QUEUE. That is the direction that cannot lose bytes: a row that outlives its
// object is retried and blobstore.Delete is idempotent (deleting an absent key is
// not an error), while a row cleared before a failed delete is an object nothing
// will ever come back for — an orphan the storage page reports forever.
//
// It logs one structured line per pass with the count deleted and the count
// deferred, which is the only visibility this job has.
func (s *Service) Drain(ctx context.Context, now time.Time) error {
	if s.blob == nil {
		return nil
	}
	due := now.UTC().Format(tsFormat)
	keys, err := s.store.DueKeys(ctx, s.db, due, drainBatch)
	if err != nil {
		return err
	}
	if len(keys) == 0 {
		return nil
	}
	if err := s.blob.Delete(ctx, keys...); err != nil {
		// Every key stays queued. The next pass tries again; nothing is lost and
		// nothing is double-counted, because the rows were never cleared.
		s.logger.Warn("chat: the drain could not delete its batch — every key stays queued",
			"queued", len(keys), "err", err)
		return err
	}
	if err := s.store.ClearKeys(ctx, s.db, keys); err != nil {
		// The objects are gone and the rows are not. The next pass re-deletes absent
		// keys, which blobstore treats as success, and clears them then.
		s.logger.Warn("chat: the drain deleted its batch but could not clear the queue",
			"deleted", len(keys), "err", err)
		return err
	}
	deferred, err := s.store.QueuedCount(ctx, s.db)
	if err != nil {
		deferred = -1
	}
	s.logger.Info("chat: drain pass", "deleted", len(keys), "deferred", deferred)
	return nil
}

// DueKeys reads the keys whose purge_after has passed.
//
// ⚠ AN ANY-MEMBERSHIP READ, DELIBERATELY AND BY NAME. There is no actor here and
// therefore no floor: this is the queue, not the content, and the table holds
// nothing a floor would protect.
func (s *Store) DueKeys(ctx context.Context, q querier, due string, limit int) ([]string, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT key FROM chat_deleted_keys WHERE purge_after <= ?
		 ORDER BY purge_after LIMIT ?`, due, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []string{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// ClearKeys removes drained rows.
func (s *Store) ClearKeys(ctx context.Context, q querier, keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	args := make([]any, 0, len(keys))
	for _, k := range keys {
		args = append(args, k)
	}
	_, err := q.ExecContext(ctx,
		`DELETE FROM chat_deleted_keys WHERE key IN (`+appdb.Placeholders(len(keys))+`)`, args...)
	return err
}

// QueuedCount is what is still waiting, for the log line.
func (s *Store) QueuedCount(ctx context.Context, q querier) (int, error) {
	var n int
	err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM chat_deleted_keys`).Scan(&n)
	return n, err
}

// PurgeExpiredConversations destroys the rows of every conversation whose koš
// window has elapsed (D253).
//
// ⚠ THE ROWS GO HERE; THE BYTES WENT INTO THE QUEUE WHEN THE ROOM WAS TRASHED. The
// delete already enqueued every live key with `purge_after = deleted_at +
// HOME_CHAT_TRASH_DAYS`, so by the time this runs the same pass has usually already
// deleted them — and the ORDER of the two halves in Run below is what makes that
// true rather than a race.
//
// ⚠ A `moved` ATTACHMENT'S DOCUMENT SURVIVES, and it survives by construction: the
// move took the bytes out of the `chat/` prefix and the document row belongs to
// another module, so a cascade here cannot reach it. The acceptance criteria assert
// it because "the purge deleted the file I had saved into Dokumenty" is the failure
// this ordering exists to make impossible.
func (s *Service) PurgeExpiredConversations(ctx context.Context, now time.Time) error {
	cutoff := now.UTC().Format(tsFormat)
	ids, err := s.store.ExpiredConversations(ctx, s.db, cutoff, s.trashDays)
	if err != nil || len(ids) == 0 {
		return err
	}
	for _, id := range ids {
		if err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
			return s.store.PurgeConversationRows(ctx, tx, id)
		}); err != nil {
			s.logger.Warn("chat: could not purge an expired conversation", "conversation", id, "err", err)
			continue
		}
		s.logger.Info("chat: koš window elapsed — conversation purged", "conversation", id)
	}
	return nil
}

// ExpiredConversations lists trashed rooms whose window has run out.
//
// ⚠ ANOTHER ANY-MEMBERSHIP READ, and the second of the three §V10-4a warns about.
// It returns ids only — no name, no member, no message — so there is nothing here a
// missing floor could disclose even in principle.
func (s *Store) ExpiredConversations(ctx context.Context, q querier, nowTS string, trashDays int) ([]string, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT id FROM chat_conversations
		 WHERE deleted_at IS NOT NULL
		   AND datetime(deleted_at) <= datetime(?, ?)`,
		nowTS, "-"+strconv.Itoa(trashDays)+" days")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// DrainJob is what the composition root registers with the scheduler.
//
// ⚠ ONE JOB, NOT TWO. The koš purge runs inside it rather than beside it, and the
// order matters: the objects are drained FIRST, then the rows go. Registering them
// as two jobs would let the row purge run in a pass where the object drain had
// failed — cascading `chat_attachments` away while its keys were still queued is
// harmless (the queue holds keys, not ids), but the reverse reading is not obvious,
// and one job with a fixed order needs no reader to work it out.
func (s *Service) DrainJob(ctx context.Context, now time.Time) error {
	if err := s.Drain(ctx, now); err != nil {
		return err
	}
	return s.PurgeExpiredConversations(ctx, now)
}
