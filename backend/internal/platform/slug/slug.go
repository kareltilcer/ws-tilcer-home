// Package slug derives human-readable URL slugs from Czech titles (PRD D32,
// HANDOFF-5 §2). It folds Czech diacritics to ASCII so a title like "Guláš"
// yields "gulas", lowercases, turns whitespace into '-', drops other
// punctuation, and collapses repeated dashes. Collision suffixing (-2, -3, …)
// is the caller's job (the notes service, against the live sibling scope) —
// this package only produces the base slug.
//
// It lives in platform/ (like idgen, lexorank, dates) because it is a generic,
// module-agnostic utility; the notes module is its only caller today.
package slug

import (
	"strings"
	"unicode"
)

// fold maps the lowercase Czech accented letters (and a few common neighbours)
// to their ASCII equivalents. Uppercase forms are handled by lowercasing first.
var fold = map[rune]string{
	'á': "a", 'č': "c", 'ď': "d", 'é': "e", 'ě': "e", 'í': "i", 'ň': "n",
	'ó': "o", 'ř': "r", 'š': "s", 'ť': "t", 'ú': "u", 'ů': "u", 'ý': "y", 'ž': "z",
	// common non-Czech accents that may still appear in household content
	'à': "a", 'ä': "a", 'â': "a", 'ö': "o", 'ô': "o", 'ü': "u", 'û': "u",
	'ç': "c", 'ñ': "n", 'ľ': "l", 'ĺ': "l", 'ŕ': "r", 'ß': "ss",
}

// Make returns the base slug for title, or "" when nothing usable remains (e.g.
// the title was only punctuation) — the caller then falls back to the short id.
func Make(title string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(title)) {
		if rep, ok := fold[r]; ok {
			b.WriteString(rep)
			prevDash = false
			continue
		}
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevDash = false
		case unicode.IsSpace(r) || r == '-' || r == '_':
			// separators collapse to a single dash; suppress a leading dash
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		default:
			// any other punctuation / unmapped rune is dropped (not a separator)
		}
	}
	return strings.Trim(b.String(), "-")
}
