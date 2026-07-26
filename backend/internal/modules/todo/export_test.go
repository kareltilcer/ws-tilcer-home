package todo

// Test seam: exposes the unexported widget-provider constructor to the external
// todo_test package (which uses testsupport → bootstrap and so cannot be an
// in-package test without an import cycle).
var NewPravedelamProviderForTest = newPravedelamProvider
