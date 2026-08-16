package push

// Test seam. The push tests live in the EXTERNAL push_test package because they
// use testsupport, which builds the full migration set through bootstrap — and
// bootstrap imports the admin module, which imports this package. An in-package
// test would therefore be an import cycle; an external one is not.
//
// These two are what the external tests still need from the inside.
var (
	// NormalizeForTest exposes the envelope defaulting + truncation step.
	NormalizeForTest = Envelope.normalized
	// MaxBodyRunesForTest is the body truncation bound.
	MaxBodyRunesForTest = maxBodyRunes
)
