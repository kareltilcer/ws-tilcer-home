package documents

import "testing"

// The one internal test in this package, and it is a TRIPWIRE rather than a
// behaviour test: it asserts a pair of constants against literals, which is
// normally a test that only restates the code.
//
// It earns its place because the invariant it guards is not checkable any other
// way and has already been broken in production. pdfSandbox is enforced by the
// browser; previewSandboxVersion is what makes a browser ASK for it again. A SHARED
// /preview response is `private, immutable, max-age=31536000` (a private one
// revalidates on every view under D208 and takes the policy off the 304, which is the
// one path that needs no bump), so a policy changed without a bump reaches nobody who
// has already opened a shared document — for a year, silently, with every test on
// both sides still green. That is precisely what
// happened when `allow-popups` was found to be the token the Android placeholder
// actually wanted: without the bump the second fix would have looked as ineffective
// as the first.
//
// So changing pdfSandbox alone fails here, and the failure says what to do.
func TestPreviewSandboxVersionIsBumpedWithThePolicy(t *testing.T) {
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
