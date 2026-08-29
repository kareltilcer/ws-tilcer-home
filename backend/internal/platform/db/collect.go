package db

import "database/sql"

// Scanner is the one method a row-scanning helper needs. Both *sql.Rows and
// *sql.Row satisfy it, so a module's `scanX(Scanner) (X, error)` serves the
// single-row read and the list read alike.
//
// ⚠ IT IS A NAMED TYPE BECAUSE THE ANONYMOUS ONE DOES NOT COMPOSE. Nine files
// declared this interface for themselves — five as a module-local `scanner`
// (`admin`, `electricity`, `finance`, `garden`, `platform/push`), one as
// `rowScanner` (`todo`), one as `scannable` (`logging`), and the rest inline as
// `interface{ Scan(...any) error }` on the parameter (`chat`, `documents`,
// `events`, `notes`). A defined interface type and its literal are not identical
// types in Go, so `func(interface{ Scan(...any) error }) (T, error)` is NOT
// assignable to `func(Scanner) (T, error)` — every one of those spellings had to
// become this one before Collect below could take any of them.
type Scanner interface {
	Scan(dest ...any) error
}

// Collect drains rows through scan into a slice, closes rows, and folds
// rows.Err() into the returned error. It replaces the eight-line loop that
// appeared once per list query in every module that speaks SQL.
//
// ⚠ IT CLOSES rows. The caller's `defer rows.Close()` goes away with the loop —
// a Collect that returned with rows still open would leave the pool's single
// connection pinned, which at MaxOpenConns(1) is the whole application.
//
// ⚠ IT RETURNS A NIL SLICE FOR ZERO ROWS, matching the `var out []T` the loops
// it replaces overwhelmingly used. Nine call sites wanted the other thing —
// `out := []T{}`, so an empty page serialises as `[]` and not `null` — and those
// wrap this in OrEmpty rather than letting the distinction go quiet. It is a
// wire-visible property, so it stays visible in the code.
//
// ⚠ IT IS FOR THE PLAIN APPEND ONLY, which is what 50 of the repository's 127
// row loops are. The rest either scan straight into locals with no named scanX
// (46 of them — a closure there is longer than the loop it replaces) or do more
// than append: build a map (`chat`'s attachments-by-message), count as they go,
// or stop early. Those keep their loops.
func Collect[T any](rows *sql.Rows, scan func(Scanner) (T, error)) ([]T, error) {
	defer func() { _ = rows.Close() }()
	var out []T
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// OrEmpty turns a nil slice into an empty one, so a JSON response declaring an
// array never serialises `null`. It is Collect's companion: the loops that read
// `out := []T{}` said this at the declaration, and saying it at the return keeps
// the promise where a reader can still see it.
func OrEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
