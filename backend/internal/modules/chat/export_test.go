package chat

// Test seams for the custody transfer's fault-injection matrix (D238, FR-V10-14).
//
// ⚠ THE MOVE IS THE ONLY PATH IN v10 THAT CAN DESTROY DATA SILENTLY, and the
// acceptance criteria ask for a failure injected at EACH of its five steps, with the
// resulting state asserted and the move re-run from it. Steps 2 and 3 live inside
// `storage.BlobSink` and a fake sink covers them honestly. Steps 4 and 5 are chat's
// own SQLite write and object delete — and there is no way to make either fail from
// outside the process without breaking the database for everything else.
//
// So the injection point is a package-private hook, nil in production, compiled
// into the test binary through this file. The alternative is shipping the matrix
// untested on the one path that can lose a file, which is not a trade worth making
// for a cleaner struct.

// MoveStep names a step of the transfer, for a test's injector.
type MoveStep = moveStep

// The two steps a fake sink cannot reach.
const (
	// StepMark is step 4: chat marks its attachment `moved`. A failure here leaves
	// the document existing and the attachment still `live` — over-counted, visible
	// and re-runnable.
	StepMark = stepMark
	// StepDelete is step 5: chat deletes its own object, LAST. A failure here leaves
	// the move complete and the bytes queued for the drain.
	StepDelete = stepDelete
	// StepValidate is chat's own guard, before the sink is called at all.
	StepValidate = stepValidate
)

// InjectMoveFault installs a fault injector. Passing nil clears it, which is how a
// test re-runs the same move from the state the injected failure left behind.
func (s *Service) InjectMoveFault(fn func(step MoveStep) error) { s.moveFault = fn }

// QueuedKeysForTest returns how many object keys are waiting for the drain.
//
// ⚠ The queue is where "the move failed at step 5" and "the message was deleted"
// both end up, and neither is observable from the API: the attachment reads as
// `moved` either way and the bytes are gone either way. The count is the only thing
// that separates "the drain will collect it" from "the object is an orphan nothing
// will ever come back for".
func (s *Service) QueuedKeysForTest() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM chat_deleted_keys`).Scan(&n)
	return n, err
}
