// Package foldericon normalizes the optional emoji icon a folder can carry.
// The notes and documents modules both store folder icons and must apply the
// exact same policy, so the trim + length cap lives here rather than being
// duplicated in each service (it lives in platform/ like slug and idgen because
// it is a generic, module-agnostic utility).
package foldericon

import "strings"

// maxRunes bounds an icon's length. An emoji can be several code points (ZWJ
// family sequences, regional-indicator flags), so the cap is on runes and
// generous enough for any single glyph while refusing a pasted essay.
const maxRunes = 8

// Normalize trims a folder icon and bounds its length.
// Empty → "" (stored NULL; the client renders the 📁 default).
func Normalize(s string) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > maxRunes {
		s = string(r[:maxRunes])
	}
	return s
}
