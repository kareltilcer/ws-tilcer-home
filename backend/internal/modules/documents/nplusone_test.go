package documents_test

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/bootstrap"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/documents"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/blobstore"
	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/db"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/registry"
	_ "modernc.org/sqlite"
)

// The tree and the dashboard widget are both required to load with NO N+1: their
// cost must not grow with the number of folders or pins (FR-DOC11, §12). Asserting
// that needs a real statement counter, so this file registers a driver that wraps
// the SQLite one and counts every statement database/sql executes — on both the
// QueryerContext fast path and the Prepare+Stmt fallback.

var statements atomic.Int64

func init() {
	// sql.Open is lazy, so this never touches a file: it just hands us the registered
	// SQLite driver to wrap.
	base, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		panic("counting driver: " + err.Error())
	}
	sql.Register("sqlite-counting", &countingDriver{inner: base.Driver()})
	_ = base.Close()
}

// countingDB opens a migrated database through the counting driver, mirroring the
// pragmas platform/db uses (single connection, WAL, immediate transactions).
func countingDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "counting.db")
	dsn := "file:" + url.PathEscape(path) +
		"?_txlock=immediate&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)" +
		"&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite-counting", dsn)
	if err != nil {
		t.Fatalf("open counting db: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	migFS, err := bootstrap.MigrationFS()
	if err != nil {
		t.Fatalf("migrations: %v", err)
	}
	if err := appdb.Migrate(db, migFS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestTreeAndWidget_CostDoesNotGrowWithTheTree(t *testing.T) {
	db := countingDB(t)
	store, err := blobstore.NewFS(t.TempDir())
	if err != nil {
		t.Fatalf("blob store: %v", err)
	}
	svc := documents.NewService(db, audit.NewSink(), nil, store, documents.Options{
		MaxUploadBytes: 1 << 20,
		TempDir:        t.TempDir(),
	}, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	ctx := editorCtx()

	// Build a small tree, then a much larger one, and compare the statement counts of
	// the same two reads. Equal counts prove the reads are bounded; an N+1 would grow
	// roughly with the number of folders.
	small := measure(t, svc, ctx, 3)
	large := measure(t, svc, ctx, 25)

	if large.tree != small.tree {
		t.Errorf("Tree ran %d statements for 25 folders vs %d for 3 — that is an N+1",
			large.tree, small.tree)
	}
	if large.widget != small.widget {
		t.Errorf("the widget ran %d statements with 25 pins vs %d with 3 — that is an N+1",
			large.widget, small.widget)
	}
	// Sanity: the reads are a handful of statements, not zero (which would mean the
	// counter is not wired up).
	if small.tree == 0 || small.widget == 0 {
		t.Fatal("the statement counter recorded nothing — the counting driver is not in use")
	}
	t.Logf("tree=%d statements, widget=%d statements (constant across tree sizes)", small.tree, small.widget)
}

type costs struct{ tree, widget int64 }

// measure grows the tree to n folders/documents/pins and counts the statements the
// tree read and the widget provider each execute.
func measure(t *testing.T, svc *documents.Service, ctx context.Context, n int) costs {
	t.Helper()
	root, err := svc.CreateFolder(ctx, documents.DocFolderCreate{Name: fmt.Sprintf("Kořen %d", n)})
	if err != nil {
		t.Fatalf("create folder: %v", err)
	}
	for i := 0; i < n; i++ {
		sub, err := svc.CreateFolder(ctx, documents.DocFolderCreate{
			Name: fmt.Sprintf("Složka %d-%d", n, i), ParentID: &root.ID,
		})
		if err != nil {
			t.Fatalf("create subfolder: %v", err)
		}
		d, err := svc.Upload(ctx, documents.UploadInput{
			Filename: fmt.Sprintf("dok-%d-%d.pdf", n, i),
			File:     bytes.NewReader(pdfBytes()),
			FolderID: &sub.ID,
		})
		if err != nil {
			t.Fatalf("upload: %v", err)
		}
		if _, err := svc.Pin(ctx, d.ID, "household", ""); err != nil {
			t.Fatalf("pin: %v", err)
		}
	}

	provider := documents.NewModule(svc).Widgets()[0]

	before := statements.Load()
	if _, err := svc.Tree(ctx, false, documents.Scope{}); err != nil {
		t.Fatalf("tree: %v", err)
	}
	treeCost := statements.Load() - before

	before = statements.Load()
	if _, err := provider.Data(ctx, registry.User{ID: "u-editor", Roles: []string{"editor"}}); err != nil {
		t.Fatalf("widget: %v", err)
	}
	widgetCost := statements.Load() - before

	return costs{tree: treeCost, widget: widgetCost}
}

// ---- the counting driver ----

type countingDriver struct{ inner driver.Driver }

func (d *countingDriver) Open(name string) (driver.Conn, error) {
	c, err := d.inner.Open(name)
	if err != nil {
		return nil, err
	}
	return &countingConn{inner: c}, nil
}

type countingConn struct{ inner driver.Conn }

func (c *countingConn) Prepare(q string) (driver.Stmt, error) {
	s, err := c.inner.Prepare(q)
	if err != nil {
		return nil, err
	}
	return &countingStmt{inner: s}, nil
}

func (c *countingConn) PrepareContext(ctx context.Context, q string) (driver.Stmt, error) {
	if p, ok := c.inner.(driver.ConnPrepareContext); ok {
		s, err := p.PrepareContext(ctx, q)
		if err != nil {
			return nil, err
		}
		return &countingStmt{inner: s}, nil
	}
	return c.Prepare(q)
}

func (c *countingConn) Close() error              { return c.inner.Close() }
func (c *countingConn) Begin() (driver.Tx, error) { return c.inner.Begin() } //nolint:staticcheck // required by driver.Conn

func (c *countingConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if b, ok := c.inner.(driver.ConnBeginTx); ok {
		return b.BeginTx(ctx, opts)
	}
	return c.inner.Begin() //nolint:staticcheck // fallback for drivers without BeginTx
}

// QueryContext/ExecContext are the paths database/sql prefers when the driver
// supports them, which modernc's does — so this is where most statements land.
func (c *countingConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	qr, ok := c.inner.(driver.QueryerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	statements.Add(1)
	return qr.QueryContext(ctx, q, args)
}

func (c *countingConn) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	ex, ok := c.inner.(driver.ExecerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	statements.Add(1)
	return ex.ExecContext(ctx, q, args)
}

type countingStmt struct{ inner driver.Stmt }

func (s *countingStmt) Close() error  { return s.inner.Close() }
func (s *countingStmt) NumInput() int { return s.inner.NumInput() }

func (s *countingStmt) Exec(args []driver.Value) (driver.Result, error) { //nolint:staticcheck // required by driver.Stmt
	statements.Add(1)
	return s.inner.Exec(args)
}

func (s *countingStmt) Query(args []driver.Value) (driver.Rows, error) { //nolint:staticcheck // required by driver.Stmt
	statements.Add(1)
	return s.inner.Query(args)
}

func (s *countingStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	if e, ok := s.inner.(driver.StmtExecContext); ok {
		statements.Add(1)
		return e.ExecContext(ctx, args)
	}
	return nil, driver.ErrSkip
}

func (s *countingStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	if q, ok := s.inner.(driver.StmtQueryContext); ok {
		statements.Add(1)
		return q.QueryContext(ctx, args)
	}
	return nil, driver.ErrSkip
}
