package db

import "strings"

// LikeEscapeChar is the character SQL `ESCAPE` clauses in this repository use.
// It must appear in the query as `ESCAPE '\'` beside every `LIKE ?` bound with
// LikeContains.
const LikeEscapeChar = `\`

// LikeContains turns free text into a bound `%…%` LIKE pattern with the two
// metacharacters neutralised.
//
// ⚠ IT IS THE SUBSTRING TWIN OF FTSQuery, AND IT EXISTS FOR THE SAME REASON. A
// household search term is written by a person, not by somebody composing SQL:
// `100%` and `snake_case` contain LIKE wildcards that nobody meant to type, and
// unescaped they turn a search for "100%" into a search for "100 followed by
// anything". Not an injection — the term is still bound — but a wrong answer,
// which is worse than an error because nothing reports it.
//
// ⚠ THE QUERY MUST CARRY `ESCAPE '\'`, or the backslashes this inserts are
// matched literally and the term stops matching itself. SQLite has no default
// escape character; there is nothing to inherit.
//
// ⚠ EXISTING CALLERS ARE NOT MIGRATED, deliberately. `todo/tree.go`'s board
// filter builds a bare `%q%` and has since v1: routing it through here would
// change what that filter matches, which is a behaviour change belonging to
// todo's own release rather than to v11 (NG5). This is used by the code v11 adds.
func LikeContains(s string) string {
	s = strings.ReplaceAll(s, LikeEscapeChar, LikeEscapeChar+LikeEscapeChar)
	s = strings.ReplaceAll(s, "%", LikeEscapeChar+"%")
	s = strings.ReplaceAll(s, "_", LikeEscapeChar+"_")
	return "%" + s + "%"
}
