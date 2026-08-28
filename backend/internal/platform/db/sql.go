package db

import "strings"

// Placeholders builds "?,?,?" for an IN clause of n arguments.
//
// ⚠ IT LIVES HERE BECAUSE IT WAS ABOUT TO EXIST FOR THE FOURTH TIME. `notes`,
// `documents` and `garden` each grew their own copy, and v10's `chat` was writing a
// fourth — at which point the question stops being "is this worth sharing" and
// becomes "which of the four does a fix reach". platform/db is the seam every
// module that speaks SQL already depends on.
//
// n <= 0 returns "", which is not a valid IN list: a caller with nothing to match
// must skip the clause rather than emit `IN ()`.
func Placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}
