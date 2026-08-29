// Package optional builds the pointers that mean "this field is present".
//
// A patch struct, an update struct and a paginated response all use a nil pointer
// for "absent" and a non-nil one for "set to this" — and Go has no address-of for
// a literal, so naming a present value takes a helper. Six of them existed.
package optional

// Of returns a pointer to v.
//
// ⚠ IT REPLACES SIX COPIES: `boolPtr` in `documents`, `events`, `notes`, `todo`
// and todo's test package, all five spelled `func boolPtr(b bool) *bool` and every
// call site of all five reading `Archived: boolPtr(true)`; plus `chat`'s already
// generic `ptr[T any](v T) *T`, which is this function under a name the four
// modules above cannot use.
//
// ⚠ THE PACKAGE IS NOT CALLED `ptr`, and that is not a style preference.
// `documents`, `events`, `notes` and `todo` each declare a package-level
// `ptr(sql.NullString) *string` — a scan helper, a genuinely different function —
// so an import named `ptr` does not compile in the four packages that need this
// one most.
//
// `audit.Ptr` is deliberately NOT folded in here either. It is the string-only
// spelling for building audit.Change values, it carries 103 call sites, and its
// doc comment records why six modules converged on it after each spelling it `ap`.
// Replacing it is a rename across every module in the repo and belongs in its own
// commit rather than smuggled into this one.
func Of[T any](v T) *T { return &v }
