package notes_test

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/notes"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/blobstore"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/registry"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/testsupport"
)

// h bundles a migrated db + a filesystem object store + service + t so the
// must-helpers can consume the (value, error) return of a service call directly (Go
// spreads a two-value return only when it is the whole argument list).
type h struct {
	t    *testing.T
	svc  *notes.Service
	db   *sql.DB
	blob blobstore.BlobStore
}

func newH(t *testing.T) *h {
	t.Helper()
	db := testsupport.NewDB(t)
	blob, err := blobstore.NewFS(t.TempDir())
	if err != nil {
		t.Fatalf("blob store: %v", err)
	}
	svc := notes.NewService(db, audit.NewSink(), nil, blob,
		notes.ImageOptions{MaxUploadBytes: 1 << 20}, // 1 MB keeps the over-cap test cheap
		slog.New(slog.NewJSONHandler(io.Discard, nil)))
	return &h{t: t, svc: svc, db: db, blob: blob}
}

func (x *h) note(n *notes.NoteDetail, err error) *notes.NoteDetail {
	x.t.Helper()
	if err != nil {
		x.t.Fatalf("note op failed: %v", err)
	}
	return n
}

func (x *h) folder(f *notes.FolderDetail, err error) *notes.FolderDetail {
	x.t.Helper()
	if err != nil {
		x.t.Fatalf("folder op failed: %v", err)
	}
	return f
}

func editorCtx() context.Context { return testsupport.CtxUser("u-editor", "editor") }
func readerCtx() context.Context { return testsupport.CtxUser("u-reader", "reader") }

func status(t *testing.T, err error) int {
	t.Helper()
	var ae *httpx.APIError
	if errors.As(err, &ae) {
		return ae.Status
	}
	t.Fatalf("expected *httpx.APIError, got %v", err)
	return 0
}

// A root note (no folder) must return path as an empty array, not null: the client
// reads path.length, and a null would crash the note view. Regression guard for the
// nil-vs-empty-slice marshalling (nil → JSON null, empty slice → []).
func TestRootNotePathIsEmptyNotNil(t *testing.T) {
	x := newH(t)
	ctx := editorCtx()
	n := x.note(x.svc.CreateNote(ctx, notes.NoteCreate{Title: "Bez složky"}))
	if n.Path == nil {
		t.Fatal("CreateNote: root note path is nil (marshals as null, crashes the client); want empty slice")
	}
	if len(n.Path) != 0 {
		t.Fatalf("root note path: got %d segments, want 0", len(n.Path))
	}
	if d := x.note(x.svc.GetNoteDetail(ctx, n.ID)); d.Path == nil {
		t.Fatal("GetNoteDetail: root note path is nil; want empty slice")
	}
}

// ---- Slugs & the addressing invariant (D32) ----

func TestSlugCrossTableAndCollision(t *testing.T) {
	x := newH(t)
	ctx := editorCtx()

	f := x.folder(x.svc.CreateFolder(ctx, notes.FolderCreate{Name: "Recepty"}))
	if f.Slug != "recepty" {
		t.Fatalf("slug fold: got %q want recepty", f.Slug)
	}
	// A note at root sharing the folder's derived slug must be suffixed (cross-table).
	n := x.note(x.svc.CreateNote(ctx, notes.NoteCreate{Title: "Recepty"}))
	if n.Slug == f.Slug {
		t.Fatalf("cross-table slug collision not resolved: both %q", n.Slug)
	}
	if n.Slug != "recepty-2" {
		t.Fatalf("collision suffix: got %q want recepty-2", n.Slug)
	}
	g := x.note(x.svc.CreateNote(ctx, notes.NoteCreate{Title: "Guláš"}))
	if g.Slug != "gulas" {
		t.Fatalf("diacritic fold: got %q want gulas", g.Slug)
	}
}

func TestRootSiblingDedupe(t *testing.T) {
	x := newH(t)
	ctx := editorCtx()
	a := x.note(x.svc.CreateNote(ctx, notes.NoteCreate{Title: "Nákup"}))
	b := x.note(x.svc.CreateNote(ctx, notes.NoteCreate{Title: "Nákup"}))
	if a.Slug == b.Slug {
		t.Fatalf("root-level siblings not deduped (COALESCE index): both %q", a.Slug)
	}
}

// ---- Resolver (FR-P4) ----

func TestResolveAndNoRedirect(t *testing.T) {
	x := newH(t)
	ctx := editorCtx()
	recepty := x.folder(x.svc.CreateFolder(ctx, notes.FolderCreate{Name: "Recepty"}))
	gulas := x.note(x.svc.CreateNote(ctx, notes.NoteCreate{Title: "Guláš", FolderID: &recepty.ID}))

	res, err := x.svc.Resolve(ctx, "recepty/gulas")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if res.Type != "note" || res.ID != gulas.ID {
		t.Fatalf("resolve got %+v want note %s", res, gulas.ID)
	}
	newTitle := "Guláš segedínský"
	x.note(x.svc.UpdateNote(ctx, gulas.ID, notes.NoteUpdate{Title: &newTitle}, ""))
	if _, err := x.svc.Resolve(ctx, "recepty/gulas"); status(t, err) != 404 {
		t.Fatalf("old path should 404 after rename")
	}
	if _, err := x.svc.Resolve(ctx, "recepty/gulas-segedinsky/whatever"); status(t, err) != 404 {
		t.Fatalf("non-folder intermediate segment should 404")
	}
}

// ---- Move: descendants are not rewritten (HANDOFF-5 §2) ----

func TestMoveFolderDoesNotRewriteDescendants(t *testing.T) {
	x := newH(t)
	ctx := editorCtx()
	parent := x.folder(x.svc.CreateFolder(ctx, notes.FolderCreate{Name: "Domácnost"}))
	sub := x.folder(x.svc.CreateFolder(ctx, notes.FolderCreate{Name: "Kotel", ParentID: &parent.ID}))
	note := x.note(x.svc.CreateNote(ctx, notes.NoteCreate{Title: "Servis", FolderID: &sub.ID}))
	other := x.folder(x.svc.CreateFolder(ctx, notes.FolderCreate{Name: "Chata"}))

	moved := x.folder(x.svc.MoveFolder(ctx, sub.ID, notes.FolderMoveRequest{ParentID: &other.ID, Position: "m"}))

	after, err := x.svc.GetNoteDetail(ctx, note.ID)
	if err != nil {
		t.Fatalf("get note: %v", err)
	}
	if after.FolderID == nil || *after.FolderID != sub.ID || after.Slug != note.Slug {
		t.Fatalf("descendant note was rewritten: %+v", after.Note)
	}
	if _, err := x.svc.Resolve(ctx, "chata/"+moved.Slug+"/"+note.Slug); err != nil {
		t.Fatalf("descendant should resolve at new path: %v", err)
	}
}

func TestFolderMoveCycleGuard(t *testing.T) {
	x := newH(t)
	ctx := editorCtx()
	a := x.folder(x.svc.CreateFolder(ctx, notes.FolderCreate{Name: "A"}))
	b := x.folder(x.svc.CreateFolder(ctx, notes.FolderCreate{Name: "B", ParentID: &a.ID}))
	if _, err := x.svc.MoveFolder(ctx, a.ID, notes.FolderMoveRequest{ParentID: &b.ID, Position: "m"}); status(t, err) != 422 {
		t.Fatalf("cycle guard should 422")
	}
	after, _ := x.svc.GetFolderDetail(ctx, a.ID)
	if after.ParentID != nil {
		t.Fatalf("A must not have moved")
	}
}

// A move that re-sends the note's current folder/position/slug is a true no-op: it
// must not bump updated_at, log a note.move audit event, or broadcast — otherwise
// every other client refetches and raises a bogus "changed elsewhere" toast.
func TestNoOpMoveDoesNotBumpOrBroadcast(t *testing.T) {
	db := testsupport.NewDB(t)
	var broadcasts int
	svc := notes.NewService(db, audit.NewSink(), func(context.Context, string, any) { broadcasts++ }, nil, notes.ImageOptions{}, nil)
	ctx := editorCtx()

	n, err := svc.CreateNote(ctx, notes.NoteCreate{Title: "Seznam", BodyMD: "mléko"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	broadcasts = 0 // ignore the create's broadcast

	after, err := svc.MoveNote(ctx, n.ID, notes.NoteMoveRequest{FolderID: n.FolderID, Position: n.Position}, "")
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if after.UpdatedAt != n.UpdatedAt {
		t.Fatalf("no-op move bumped updated_at: %q → %q", n.UpdatedAt, after.UpdatedAt)
	}
	if broadcasts != 0 {
		t.Fatalf("no-op move should not broadcast, got %d", broadcasts)
	}
	var moves int
	if err := db.QueryRow(`SELECT COUNT(*) FROM audit_events WHERE action='note.move'`).Scan(&moves); err != nil {
		t.Fatal(err)
	}
	if moves != 0 {
		t.Fatalf("no-op move should not record an audit event, got %d", moves)
	}
}

// A real move updates the note row but touches neither title nor body, so the
// guarded notes_au trigger skips re-indexing — the pre-existing FTS entry must stay,
// leaving the note searchable at its new location.
func TestMovePreservesSearchIndex(t *testing.T) {
	x := newH(t)
	ctx := editorCtx()
	dest := x.folder(x.svc.CreateFolder(ctx, notes.FolderCreate{Name: "Cíl"}))
	n := x.note(x.svc.CreateNote(ctx, notes.NoteCreate{Title: "Recept", BodyMD: "zzunikaltoken"}))

	x.note(x.svc.MoveNote(ctx, n.ID, notes.NoteMoveRequest{FolderID: &dest.ID, Position: "z"}, ""))

	page, err := x.svc.List(ctx, "zzunikaltoken", nil, false)
	if err != nil {
		t.Fatalf("search after move: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("moved note should still be searchable, got %d", len(page.Items))
	}
}

// ---- Folder delete (FR-P2) ----

func TestFolderDeleteCascade(t *testing.T) {
	x := newH(t)
	ctx := editorCtx()
	f := x.folder(x.svc.CreateFolder(ctx, notes.FolderCreate{Name: "Staré"}))
	x.note(x.svc.CreateNote(ctx, notes.NoteCreate{Title: "Poznámka", FolderID: &f.ID}))

	if err := x.svc.DeleteFolder(ctx, f.ID, false, false); status(t, err) != 409 {
		t.Fatalf("non-empty delete should 409")
	}
	if err := x.svc.DeleteFolder(ctx, f.ID, true, false); err != nil {
		t.Fatalf("cascade delete: %v", err)
	}
	tree, _ := x.svc.Tree(ctx, false)
	if len(tree.Roots) != 0 {
		t.Fatalf("folder should be gone from the tree, got %d roots", len(tree.Roots))
	}
	var noteDeletes int
	if err := x.db.QueryRow(`SELECT COUNT(*) FROM audit_events WHERE action='note.delete'`).Scan(&noteDeletes); err != nil {
		t.Fatal(err)
	}
	if noteDeletes != 1 {
		t.Fatalf("cascade should log each child note delete, got %d", noteDeletes)
	}
}

// ---- Archive / restore invariants (addressing D32) ----

// Un-archiving a note whose slug was reused by a sibling while it was archived must
// re-derive a free slug rather than violate ux_notes_sibling_slug (would 500).
func TestUnarchiveNoteReFreesSlug(t *testing.T) {
	x := newH(t)
	ctx := editorCtx()
	arch, live := true, false

	a := x.note(x.svc.CreateNote(ctx, notes.NoteCreate{Title: "Plán"}))
	x.note(x.svc.UpdateNote(ctx, a.ID, notes.NoteUpdate{Archived: &arch}, "")) // archive → slug leaves live scope
	b := x.note(x.svc.CreateNote(ctx, notes.NoteCreate{Title: "Plán"}))        // sibling reuses the freed slug
	if b.Slug != a.Slug {
		t.Fatalf("B should reuse archived A's slug %q, got %q", a.Slug, b.Slug)
	}

	restored := x.note(x.svc.UpdateNote(ctx, a.ID, notes.NoteUpdate{Archived: &live}, ""))
	if restored.Archived {
		t.Fatalf("A should be live after restore")
	}
	if restored.Slug == b.Slug {
		t.Fatalf("restored A must get a fresh slug, still collides on %q", restored.Slug)
	}
	if _, err := x.svc.Resolve(ctx, restored.Slug); err != nil {
		t.Fatalf("restored note should resolve at its fresh slug: %v", err)
	}
}

// A live item may never sit under an archived ancestor (Resolve walks only live
// folders), so restoring a note under an archived parent is rejected.
func TestRestoreNoteUnderArchivedParentRejected(t *testing.T) {
	x := newH(t)
	ctx := editorCtx()
	f := x.folder(x.svc.CreateFolder(ctx, notes.FolderCreate{Name: "Složka"}))
	n := x.note(x.svc.CreateNote(ctx, notes.NoteCreate{Title: "Pozn", FolderID: &f.ID}))
	if err := x.svc.DeleteFolder(ctx, f.ID, true, false); err != nil { // cascade-archive folder + note
		t.Fatalf("cascade archive: %v", err)
	}
	live := false
	if _, err := x.svc.UpdateNote(ctx, n.ID, notes.NoteUpdate{Archived: &live}, ""); status(t, err) != 422 {
		t.Fatalf("restoring a note under an archived parent should 422")
	}
}

// Archiving a non-empty folder via PATCH would strand its live children under an
// archived ancestor; it is rejected (409) and the subtree stays reachable.
func TestArchiveNonEmptyFolderViaPatchRejected(t *testing.T) {
	x := newH(t)
	ctx := editorCtx()
	f := x.folder(x.svc.CreateFolder(ctx, notes.FolderCreate{Name: "Plná"}))
	x.note(x.svc.CreateNote(ctx, notes.NoteCreate{Title: "Dítě", FolderID: &f.ID}))

	arch := true
	if _, err := x.svc.UpdateFolder(ctx, f.ID, notes.FolderUpdate{Archived: &arch}); status(t, err) != 409 {
		t.Fatalf("archiving a non-empty folder via PATCH should 409")
	}
	tree, _ := x.svc.Tree(ctx, false)
	if len(tree.Roots) != 1 || len(tree.Roots[0].Notes) != 1 {
		t.Fatalf("folder + note must stay live and reachable, got %+v", tree.Roots)
	}
}

// ---- Folder icon (v4.1) ----

func TestFolderIcon(t *testing.T) {
	x := newH(t)
	ctx := editorCtx()

	// Created with an icon → round-trips.
	withIcon := x.folder(x.svc.CreateFolder(ctx, notes.FolderCreate{Name: "Domácnost", Icon: "🏠"}))
	if withIcon.Icon != "🏠" {
		t.Fatalf("create icon: got %q want 🏠", withIcon.Icon)
	}
	// Created without an icon → empty (client renders the 📁 default).
	noIcon := x.folder(x.svc.CreateFolder(ctx, notes.FolderCreate{Name: "Ostatní"}))
	if noIcon.Icon != "" {
		t.Fatalf("no icon: got %q want empty", noIcon.Icon)
	}

	// Icon-only PATCH updates it and persists (verify by re-reading the detail).
	newIcon := "🔧"
	x.folder(x.svc.UpdateFolder(ctx, withIcon.ID, notes.FolderUpdate{Icon: &newIcon}))
	got := x.folder(x.svc.GetFolderDetail(ctx, withIcon.ID))
	if got.Icon != "🔧" {
		t.Fatalf("patched icon not persisted: got %q want 🔧", got.Icon)
	}

	// Empty-string PATCH clears it.
	empty := ""
	x.folder(x.svc.UpdateFolder(ctx, withIcon.ID, notes.FolderUpdate{Icon: &empty}))
	if cleared := x.folder(x.svc.GetFolderDetail(ctx, withIcon.ID)); cleared.Icon != "" {
		t.Fatalf("clearing icon: got %q want empty", cleared.Icon)
	}

	// A surrounding-whitespace icon is trimmed by the service.
	trimmed := x.folder(x.svc.CreateFolder(ctx, notes.FolderCreate{Name: "Trim", Icon: "  🚗  "}))
	if trimmed.Icon != "🚗" {
		t.Fatalf("icon not trimmed: got %q want 🚗", trimmed.Icon)
	}
}

// ---- FTS stays consistent under FK-cascade hard delete ----

func TestHardFolderDeleteClearsFTS(t *testing.T) {
	x := newH(t)
	ctx := editorCtx()
	f := x.folder(x.svc.CreateFolder(ctx, notes.FolderCreate{Name: "Archiv"}))
	x.note(x.svc.CreateNote(ctx, notes.NoteCreate{Title: "Tajné", BodyMD: "zzqunikaltoken hovězí", FolderID: &f.ID}))

	// Sanity: the note is indexed while it exists.
	if got := ftsMatchCount(t, x.db, "zzqunikaltoken"); got != 1 {
		t.Fatalf("note should be indexed before delete, got %d fts rows", got)
	}
	// Hard delete purges the folder; the note goes via FK ON DELETE CASCADE. That
	// cascade is an ordinary child-table delete, so notes_ad fires and clears the
	// index — no orphan left to phantom-match a rowid-reusing note later.
	if err := x.svc.DeleteFolder(ctx, f.ID, true, true); err != nil {
		t.Fatalf("hard delete: %v", err)
	}
	if got := ftsMatchCount(t, x.db, "zzqunikaltoken"); got != 0 {
		t.Fatalf("hard folder delete left %d orphaned FTS row(s) for the cascaded note", got)
	}
}

func ftsMatchCount(t *testing.T, db *sql.DB, term string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM notes_fts WHERE notes_fts MATCH ?`, term).Scan(&n); err != nil {
		t.Fatalf("fts match query: %v", err)
	}
	return n
}

// ---- Pinning (FR-P7, D35) ----

func TestPinScopesAndReaderException(t *testing.T) {
	x := newH(t)
	ed := editorCtx()
	n := x.note(x.svc.CreateNote(ed, notes.NoteCreate{Title: "Wi-Fi heslo"}))

	if _, err := x.svc.Pin(readerCtx(), n.ID, "household", ""); status(t, err) != 403 {
		t.Fatalf("reader household pin should 403")
	}
	st, err := x.svc.Pin(readerCtx(), n.ID, "personal", "")
	if err != nil || !st.Personal {
		t.Fatalf("reader personal pin should succeed: %v %+v", err, st)
	}
	if _, err := x.svc.Pin(readerCtx(), n.ID, "personal", ""); err != nil {
		t.Fatalf("re-pin should be idempotent: %v", err)
	}
	if _, err := x.svc.Pin(ed, n.ID, "household", ""); err != nil {
		t.Fatalf("editor household pin: %v", err)
	}
}

func TestPinnedWidgetDedupAndPrecedence(t *testing.T) {
	x := newH(t)
	uid := "u-editor"
	ctx := testsupport.CtxUser(uid, "editor")

	both := x.note(x.svc.CreateNote(ctx, notes.NoteCreate{Title: "Sdílená", BodyMD: "# Nadpis\nnějaký *text*"}))
	personalOnly := x.note(x.svc.CreateNote(ctx, notes.NoteCreate{Title: "Osobní"}))
	if _, err := x.svc.Pin(ctx, both.ID, "household", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := x.svc.Pin(ctx, both.ID, "personal", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := x.svc.Pin(ctx, personalOnly.ID, "personal", ""); err != nil {
		t.Fatal(err)
	}

	provider := notes.NewModule(x.svc).Widgets()[0]
	data, err := provider.Data(ctx, registry.User{ID: uid, Roles: []string{"editor"}})
	if err != nil {
		t.Fatalf("widget data: %v", err)
	}
	w := data.(notes.PripnutePoznamkyWidget)
	if len(w.Notes) != 2 {
		t.Fatalf("widget should de-dup to 2 rows, got %d", len(w.Notes))
	}
	if w.Notes[0].NoteID != both.ID || w.Notes[0].Scope != "both" {
		t.Fatalf("row0 should be the both-scoped household note, got %+v", w.Notes[0])
	}
	if w.Notes[1].Scope != "personal" {
		t.Fatalf("row1 should be personal, got %+v", w.Notes[1])
	}
	if w.Notes[0].Excerpt == nil || *w.Notes[0].Excerpt == "" || containsMD(*w.Notes[0].Excerpt) {
		t.Fatalf("excerpt should be non-empty plain text, got %v", w.Notes[0].Excerpt)
	}
}

func containsMD(s string) bool {
	for _, r := range s {
		if r == '#' || r == '*' || r == '`' {
			return true
		}
	}
	return false
}

// ---- FTS5 diacritics (FR-P6) ----

func TestSearchDiacriticInsensitive(t *testing.T) {
	x := newH(t)
	ctx := editorCtx()
	x.note(x.svc.CreateNote(ctx, notes.NoteCreate{Title: "Guláš", BodyMD: "hovězí, cibule"}))
	page, err := x.svc.List(ctx, "gulas", nil, false)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("diacritic-insensitive search should find Guláš, got %d", len(page.Items))
	}
}

// A punctuation-only query tokenizes to nothing. It must short-circuit to an empty
// result rather than run `notes_fts MATCH ”` (unspecified empty-phrase behavior).
func TestPunctuationOnlySearchReturnsEmpty(t *testing.T) {
	x := newH(t)
	ctx := editorCtx()
	x.note(x.svc.CreateNote(ctx, notes.NoteCreate{Title: "Guláš", BodyMD: "hovězí"}))

	page, err := x.svc.List(ctx, "### !!!", nil, false)
	if err != nil {
		t.Fatalf("punctuation-only search should not error: %v", err)
	}
	if page.Items == nil {
		t.Fatalf("items should be a non-nil empty slice (JSON contract)")
	}
	if len(page.Items) != 0 {
		t.Fatalf("punctuation-only search should match nothing, got %d", len(page.Items))
	}
}

// The guarded notes_au trigger still re-indexes when title/body actually change:
// after a rename, the new title must match and the old must not.
func TestTitleEditReindexesFTS(t *testing.T) {
	x := newH(t)
	ctx := editorCtx()
	n := x.note(x.svc.CreateNote(ctx, notes.NoteCreate{Title: "Puvodninadpis"}))

	newTitle := "Zmenenynadpis"
	x.note(x.svc.UpdateNote(ctx, n.ID, notes.NoteUpdate{Title: &newTitle}, ""))

	if got := ftsMatchCount(t, x.db, "zmenenynadpis"); got != 1 {
		t.Fatalf("renamed note should be re-indexed under its new title, got %d", got)
	}
	if got := ftsMatchCount(t, x.db, "puvodninadpis"); got != 0 {
		t.Fatalf("old title should no longer match after re-index, got %d", got)
	}
}

// ---- Audit (D36) ----

func TestUpdateProducesFieldDiffs(t *testing.T) {
	x := newH(t)
	ctx := editorCtx()
	n := x.note(x.svc.CreateNote(ctx, notes.NoteCreate{Title: "Původní", BodyMD: "start"}))
	newBody := "úplně jiný a mnohem delší text poznámky"
	x.note(x.svc.UpdateNote(ctx, n.ID, notes.NoteUpdate{BodyMD: &newBody}, "dashboard"))

	var updates int
	if err := x.db.QueryRow(`SELECT COUNT(*) FROM audit_events WHERE action='note.update'`).Scan(&updates); err != nil {
		t.Fatal(err)
	}
	if updates != 1 {
		t.Fatalf("expected one note.update event, got %d", updates)
	}
	var bodyChanges int
	if err := x.db.QueryRow(
		`SELECT COUNT(*) FROM audit_changes c JOIN audit_events e ON e.id=c.event_id
		 WHERE e.action='note.update' AND c.field='body_md' AND c.new_value=?`, newBody).Scan(&bodyChanges); err != nil {
		t.Fatal(err)
	}
	if bodyChanges != 1 {
		t.Fatalf("body_md diff should be recorded full/untruncated, got %d", bodyChanges)
	}
	var viaCount int
	if err := x.db.QueryRow(
		`SELECT COUNT(*) FROM audit_events WHERE action='note.update' AND meta LIKE '%"via":"dashboard"%'`).Scan(&viaCount); err != nil {
		t.Fatal(err)
	}
	if viaCount != 1 {
		t.Fatalf("expected meta.via=dashboard on the overlay edit, got %d", viaCount)
	}
}

// A PATCH that re-sends an unchanged body_md must be a true no-op: it must not
// bump updated_at, log an audit event, or broadcast. Otherwise updated_at drifts
// silently and another open editor later raises a bogus "changed elsewhere"
// banner for an edit that never happened (D38 concurrency guard).
func TestNoOpBodyPatchDoesNotBumpOrBroadcast(t *testing.T) {
	db := testsupport.NewDB(t)
	var broadcasts int
	svc := notes.NewService(db, audit.NewSink(), func(context.Context, string, any) { broadcasts++ }, nil, notes.ImageOptions{}, nil)
	ctx := editorCtx()

	n, err := svc.CreateNote(ctx, notes.NoteCreate{Title: "Seznam", BodyMD: "mléko, chléb"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	broadcasts = 0 // ignore the create's broadcast

	same := "mléko, chléb"
	after, err := svc.UpdateNote(ctx, n.ID, notes.NoteUpdate{BodyMD: &same}, "")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if after.UpdatedAt != n.UpdatedAt {
		t.Fatalf("no-op body PATCH bumped updated_at: %q → %q", n.UpdatedAt, after.UpdatedAt)
	}
	if broadcasts != 0 {
		t.Fatalf("no-op body PATCH broadcast %d change(s), want 0", broadcasts)
	}
	var updates int
	if err := db.QueryRow(`SELECT COUNT(*) FROM audit_events WHERE action='note.update'`).Scan(&updates); err != nil {
		t.Fatal(err)
	}
	if updates != 0 {
		t.Fatalf("no-op body PATCH logged %d note.update event(s), want 0", updates)
	}
}

// ---- Personal pins are not audited (D35) ----

func TestPersonalPinNotAudited(t *testing.T) {
	x := newH(t)
	ctx := editorCtx()
	n := x.note(x.svc.CreateNote(ctx, notes.NoteCreate{Title: "X"}))
	if _, err := x.svc.Pin(ctx, n.ID, "personal", ""); err != nil {
		t.Fatal(err)
	}
	var pinEvents int
	if err := x.db.QueryRow(`SELECT COUNT(*) FROM audit_events WHERE action IN ('note.pin','note.unpin')`).Scan(&pinEvents); err != nil {
		t.Fatal(err)
	}
	if pinEvents != 0 {
		t.Fatalf("personal pin must not be audited, got %d pin events", pinEvents)
	}
}
