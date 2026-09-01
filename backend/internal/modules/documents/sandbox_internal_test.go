package documents

import "testing"

// The one internal test in this package, and a TRIPWIRE rather than a behaviour test:
// it asserts two constants against literals, which normally only restates the code.
//
// It earns that because the invariant is not checkable any other way. pdfSandbox is
// enforced by the browser; previewSandboxVersion is what makes a browser ASK for it
// again. A SHARED /preview response is `private, immutable, max-age=31536000`, so a
// policy changed without a bump reaches nobody who has already opened a shared document
// — for a year, silently, with every test on both sides still green. (A private one
// revalidates on every view under D208 and takes the policy off the 304: that is the one
// path that needs no bump.)
//
// ⚠ It cannot verify that a bump HAPPENED — both literals live here, so editing
// pdfSandbox and `policy` below together leaves this green. What it does is make a
// policy edit impossible to make SILENTLY, and the failure message is where the reader
// is told to bump. A stop sign, not a lock; the lock would be deriving the key from a
// hash of the policy, at the cost of a value nobody can grep, read out of a URL, or
// recognise in a log line.
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
