package documents

import (
	"context"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/registry"
)

// pripnuteProvider implements FR-DOC11: the documents.pripnute widget = household
// pins ∪ the caller's personal pins, de-duplicated (household precedence),
// household block first then personal, each ordered by pin position.
//
// It reaches document data only through the store it was handed — the dashboard
// host never touches these tables (D28). Rows carry everything the widget needs to
// render a type icon or thumbnail and, on tap, to open the document in a preview
// overlay ON Nástěnka without navigating away.
type pripnuteProvider struct{ store *Store }

func newPripnuteProvider(store *Store) *pripnuteProvider { return &pripnuteProvider{store: store} }

func (p *pripnuteProvider) Key() string    { return "documents.pripnute" }
func (p *pripnuteProvider) Title() string  { return "Připnuté dokumenty" }
func (p *pripnuteProvider) Module() string { return "documents" }
func (p *pripnuteProvider) Description() string {
	return "Dokumenty připnuté pro celou domácnost i jen pro tebe."
}
func (p *pripnuteProvider) DefaultSize() string { return "wide" }
func (p *pripnuteProvider) AdminOnly() bool     { return false }

func (p *pripnuteProvider) Data(ctx context.Context, u registry.User) (any, error) {
	// One bounded query for the pins (join document_pins → documents) and one for the
	// folder map used to build slug paths in memory — never a query per pin.
	rows, err := p.store.PinnedRowsFor(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	folders, err := p.store.AllFolders(ctx, false)
	if err != nil {
		return nil, err
	}
	fmap := make(map[string]DocFolder, len(folders))
	for _, f := range folders {
		fmap[f.ID] = f
	}

	householdIDs := map[string]bool{}
	personalIDs := map[string]bool{}
	for _, r := range rows {
		if r.Scope == scopeHousehold {
			householdIDs[r.DocumentID] = true
		} else {
			personalIDs[r.DocumentID] = true
		}
	}

	toRow := func(r pinRow, scope string) PinnedDocument {
		row := PinnedDocument{
			DocumentID:    r.DocumentID,
			Title:         r.Title,
			SlugPath:      slugPathInMap(fmap, r.FolderID, r.Slug),
			Scope:         scope,
			ContentType:   r.ContentType,
			ByteSize:      r.ByteSize,
			PreviewKind:   r.PreviewKind,
			PreviewStatus: r.PreviewStatus,
			UpdatedAt:     r.UpdatedAt,
			Position:      r.Position,
		}
		if r.ThumbnailKey != nil && *r.ThumbnailKey != "" {
			u := thumbnailURL(r.DocumentID)
			row.ThumbnailURL = &u
		}
		return row
	}

	out := []PinnedDocument{}
	// Household block first; a document pinned both ways appears once, here, as "both".
	for _, r := range rows {
		if r.Scope != scopeHousehold {
			continue
		}
		scope := scopeHousehold
		if personalIDs[r.DocumentID] {
			scope = scopeBoth
		}
		out = append(out, toRow(r, scope))
	}
	// Personal block: only documents not already shown by a household pin.
	for _, r := range rows {
		if r.Scope != scopePersonal || householdIDs[r.DocumentID] {
			continue
		}
		out = append(out, toRow(r, scopePersonal))
	}
	return PripnuteDokumentyWidget{Documents: out}, nil
}

// slugPathInMap builds a document's slug path from an in-memory folder map (no N+1,
// which is why it cannot reuse the DB-walking service.ancestors). The final assembly
// funnels through slugPathFrom so the path format (D32) lives in one place shared
// with the DB-backed detail assembly.
func slugPathInMap(fmap map[string]DocFolder, folderID *string, ownSlug string) string {
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
