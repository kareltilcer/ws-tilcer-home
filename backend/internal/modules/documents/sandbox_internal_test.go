package documents

import "testing"

// The one internal test in this package, and it is a TRIPWIRE rather than a
// behaviour test: it asserts a pair of constants against literals, which is
// normally a test that only restates the code.
//
// It earns its place because the invariant it guards is not checkable any other way.
// pdfSandbox is enforced by the browser; previewSandboxVersion is what makes a browser
// ASK for it again. A SHARED /preview response is `private, immutable, max-age=31536000`
// (a private one revalidates on every view under D208 and takes the policy off the 304,
// which is the one path that needs no bump), so a policy changed without a bump reaches
// nobody who has already opened a shared document — for a year, silently, with every
// test on both sides still green. That is what the `allow-popups` fix would have run
// into had the bump been forgotten: the second attempt would have looked as ineffective
// as the first, for the opposite reason. The bump was not forgotten, and this test is
// what makes the next one just as hard to forget.
//
// ⚠ WHAT IT CAN AND CANNOT DO. It cannot verify that a bump HAPPENED — the version is a
// literal on both sides, so someone who edits pdfSandbox and updates only `policy` below
// leaves this green. What it can do is make a policy edit impossible to make SILENTLY:
// the first assertion fails on any change to the policy, and its message is where the
// reader is told to bump. The second is the same forcing move pointed the other way, so
// a bump cannot be made without reading this file either. It is a stop sign, not a lock
// — the lock would be deriving the key from a hash of the policy, at the cost of a value
// that can no longer be grepped, read out of a URL, or recognised in a log line.
func TestPdfSandboxAndItsCacheKeyArePinnedTogether(t *testing.T) {
	const (
		policy  = "sandbox allow-scripts allow-downloads allow-popups"
		version = "3"
	)
	if pdfSandbox != policy {
		t.Errorf("pdfSandbox = %q, want %q.\n"+
			"If this change is intended: BUMP previewSandboxVersion (currently %q) in the same edit, "+
			"then update both literals here. Without the bump, every browser that has already framed a "+
			"PDF keeps enforcing the old policy for a year.", pdfSandbox, policy, previewSandboxVersion)
	}
	if previewSandboxVersion != version {
		t.Errorf("previewSandboxVersion = %q, want %q — update the literal here in the edit that bumps it",
			previewSandboxVersion, version)
	}
}
