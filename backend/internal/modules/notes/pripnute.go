package notes

import (
	"context"
	"regexp"
	"strings"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/registry"
)

// excerptLen is the plain-text preview length for a pinned note row.
const excerptLen = 140

// pripnuteProvider implements FR-P8: the notes.pripnute widget = household pins ∪
// the caller's personal pins, de-duplicated (household precedence), household
// block then personal block, each ordered by pin position. It reaches note data
// only through the store it was handed — the dashboard host never touches these
// tables (D28).
type pripnuteProvider struct{ store *Store }

func newPripnuteProvider(store *Store) *pripnuteProvider { return &pripnuteProvider{store: store} }

func (p *pripnuteProvider) Key() string    { return "notes.pripnute" }
func (p *pripnuteProvider) Title() string  { return "Připnuté poznámky" }
func (p *pripnuteProvider) Module() string { return "notes" }
func (p *pripnuteProvider) Description() string {
	return "Poznámky připnuté pro celou domácnost i jen pro tebe."
}
func (p *pripnuteProvider) DefaultSize() string { return "wide" }
func (p *pripnuteProvider) AdminOnly() bool     { return false }

func (p *pripnuteProvider) Data(ctx context.Context, u registry.User) (any, error) {
	rows, err := p.store.PinnedRowsFor(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	// Breadcrumbs for the rows above. The folder map has to span BOTH roots the
	// caller can reach — a private pinned note's breadcrumb lives in their private
	// tree — which is the VIEWER question, not the scope one, so it is one
	// viewer-scoped read (AllFoldersForViewer) rather than a scan per root.
	//
	// ⚠ Before v9 this was a single `AllFolders(ctx, false)` and it would have kept
	// compiling and kept working: the PINS are per-caller, so nothing leaked into
	// the response. What it did was load every member's private folder NAMES into
	// memory next to a payload, and leave no sign of it (leak table row 9). The
	// next person to put a folder name on a widget row would have inherited a leak
	// they had no way to notice. Bounded to the caller here so that day never comes.
	folders, err := p.store.AllFoldersForViewer(ctx, false, u.ID)
	if err != nil {
		return nil, err
	}
	fmap := make(map[string]Folder, len(folders))
	for _, f := range folders {
		fmap[f.ID] = f
	}

	householdIDs := map[string]bool{}
	personalIDs := map[string]bool{}
	for _, r := range rows {
		if r.Scope == scopeHousehold {
			householdIDs[r.NoteID] = true
		} else {
			personalIDs[r.NoteID] = true
		}
	}

	toRow := func(r pinRow, scope string) PinnedNote {
		return PinnedNote{
			NoteID:   r.NoteID,
			Title:    r.Title,
			SlugPath: slugPathInMap(fmap, r.FolderID, r.Slug),
			Scope:    scope,
			// v9: the widget row carries a lock mark for a private note (D183). It
			// can only ever be the caller's own — a private note takes personal pins
			// only, and PinnedRowsFor filters personal pins to this user.
			Visibility: r.Visibility,
			Excerpt:    excerpt(deref(r.BodyMD), excerptLen),
			UpdatedAt:  r.UpdatedAt,
			Position:   r.Position,
		}
	}

	out := []PinnedNote{}
	// household block first; a note pinned both ways shows once here as "both".
	for _, r := range rows {
		if r.Scope != scopeHousehold {
			continue
		}
		scope := scopeHousehold
		if personalIDs[r.NoteID] {
			scope = scopeBoth
		}
		out = append(out, toRow(r, scope))
	}
	// personal block: only notes not already shown by a household pin.
	for _, r := range rows {
		if r.Scope != scopePersonal || householdIDs[r.NoteID] {
			continue
		}
		out = append(out, toRow(r, scopePersonal))
	}
	return PripnutePoznamkyWidget{Notes: out}, nil
}

// slugPathInMap builds a note's slug path from an in-memory folder map (no N+1,
// which is why it can't reuse the DB-walking service.ancestors). It funnels the
// final assembly through slugPathFrom, so the on-disk path format (D32) lives in
// one place shared with the DB-backed noteDetail/folderDetail.
func slugPathInMap(fmap map[string]Folder, folderID *string, ownSlug string) string {
	var chain []PathSegment
	cur := folderID
	for depth := 0; cur != nil && depth < 1000; depth++ {
		f, ok := fmap[*cur]
		if !ok {
			break
		}
		chain = append(chain, PathSegment{ID: f.ID, Name: f.Name, Slug: f.Slug})
		cur = f.ParentID
	}
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return slugPathFrom(chain, ownSlug)
}

var (
	mdLinkRe  = regexp.MustCompile(`!?\[([^\]]*)\]\([^)]*\)`)
	mdLeadRe  = regexp.MustCompile("^[#>*_`~]+") // block + emphasis markers at a token's start
	mdTrailRe = regexp.MustCompile("[*_`~]+$")   // emphasis/code markers at a token's end
)

// excerpt strips Markdown to a short plain-text preview (~max chars). Returns nil
// for an empty body.
func excerpt(md string, max int) *string {
	if strings.TrimSpace(md) == "" {
		return nil
	}
	s := mdLinkRe.ReplaceAllString(md, "$1") // links/images → their text
	// Strip markers only where they act as markup — the leading/trailing edges of a
	// whitespace-delimited token — so intra-word punctuation survives (snake_case,
	// a*b*c, C#). '#'/'>' are block markers (leading only); a trailing '#' is text.
	fields := strings.Fields(s)
	kept := fields[:0]
	for _, tok := range fields {
		tok = mdTrailRe.ReplaceAllString(mdLeadRe.ReplaceAllString(tok, ""), "")
		if tok != "" {
			kept = append(kept, tok)
		}
	}
	s = strings.Join(kept, " ")
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if r := []rune(s); len(r) > max {
		s = strings.TrimSpace(string(r[:max])) + "…"
	}
	return &s
}
