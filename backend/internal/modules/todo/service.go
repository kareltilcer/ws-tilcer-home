package todo

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/db"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
)

// Notifier publishes a change event to the websocket hub after commit. Kept as a
// function so this package does not depend on the ws package.
type Notifier func(typ string, payload any)

// Service orchestrates todo mutations: each opens a transaction, performs the
// change, records an audit event through the spine in the SAME transaction, and
// (after commit) publishes a websocket change. Reads go straight to the store.
type Service struct {
	db     *sql.DB
	store  *Store
	sink   audit.Sink
	notify Notifier
}

// NewService builds a todo service. notify may be nil (no websocket).
func NewService(db *sql.DB, sink audit.Sink, notify Notifier) *Service {
	if notify == nil {
		notify = func(string, any) {}
	}
	return &Service{db: db, store: NewStore(db), sink: sink, notify: notify}
}

// Store exposes the underlying read store (used by the dashboard module).
func (s *Service) Store() *Store { return s.store }

func actorID(ctx context.Context) string {
	if a, ok := reqctx.ActorFrom(ctx); ok {
		return a.UserID
	}
	return ""
}

func ap(s string) *string { return &s }

// eqp reports whether two optional strings are equal.
func eqp(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// diff appends a change when old != new.
func diff(changes *[]audit.Change, field string, old, newVal *string) {
	if !eqp(old, newVal) {
		*changes = append(*changes, audit.Change{Field: field, Old: old, New: newVal})
	}
}

// ---- Boards ----

func (s *Service) ListBoards(ctx context.Context) ([]Board, error) { return s.store.ListBoards(ctx) }

func (s *Service) GetBoard(ctx context.Context, id string) (*Board, error) {
	b, err := s.store.GetBoard(ctx, s.db, id)
	if err != nil {
		return nil, err
	}
	if b == nil {
		return nil, httpx.ErrNotFound("board not found")
	}
	return b, nil
}

func (s *Service) Tree(ctx context.Context, boardID string, labels []string, q string, includeArchived bool) (*BoardTree, error) {
	t, err := s.store.Tree(ctx, boardID, labels, q, includeArchived)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, httpx.ErrNotFound("board not found")
	}
	return t, nil
}

func (s *Service) CreateBoard(ctx context.Context, in BoardCreate) (*Board, error) {
	if strings.TrimSpace(in.Name) == "" {
		return nil, httpx.ErrUnprocessable("name is required")
	}
	var out *Board
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		pos, err := s.store.lastBoardPosition(ctx, tx)
		if err != nil {
			return err
		}
		out, err = s.store.InsertBoard(ctx, tx, in.Name, in.Description, pos, actorID(ctx))
		if err != nil {
			return err
		}
		return s.record(ctx, tx, "board.create", "board", out.ID,
			fmt.Sprintf("Vytvořena nástěnka „%s“", out.Name),
			[]audit.Change{{Field: "name", New: ap(out.Name)}}, nil)
	})
	if err != nil {
		return nil, err
	}
	s.notify("board.changed", out)
	return out, nil
}

func (s *Service) UpdateBoard(ctx context.Context, id string, in BoardUpdate) (*Board, error) {
	if in.Name != nil && strings.TrimSpace(*in.Name) == "" {
		return nil, httpx.ErrUnprocessable("name cannot be empty")
	}
	var out *Board
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		before, err := s.store.GetBoard(ctx, tx, id)
		if err != nil {
			return err
		}
		if before == nil {
			return httpx.ErrNotFound("board not found")
		}
		if err := s.store.UpdateBoard(ctx, tx, id, in); err != nil {
			return err
		}
		out, err = s.store.GetBoard(ctx, tx, id)
		if err != nil {
			return err
		}
		var changes []audit.Change
		diff(&changes, "name", ap(before.Name), ap(out.Name))
		diff(&changes, "description", before.Description, out.Description)
		diff(&changes, "archived", ap(fmt.Sprint(before.Archived)), ap(fmt.Sprint(out.Archived)))
		return s.record(ctx, tx, "board.update", "board", id,
			fmt.Sprintf("Upravena nástěnka „%s“", out.Name), changes, nil)
	})
	if err != nil {
		return nil, err
	}
	s.notify("board.changed", out)
	return out, nil
}

func (s *Service) DeleteBoard(ctx context.Context, id string, hard bool) error {
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		before, err := s.store.GetBoard(ctx, tx, id)
		if err != nil {
			return err
		}
		if before == nil {
			return httpx.ErrNotFound("board not found")
		}
		var changes []audit.Change
		if hard {
			if err := s.store.DeleteBoard(ctx, tx, id); err != nil {
				return err
			}
		} else {
			if err := s.store.UpdateBoard(ctx, tx, id, BoardUpdate{Archived: boolPtr(true)}); err != nil {
				return err
			}
			changes = []audit.Change{{Field: "archived", Old: ap("false"), New: ap("true")}}
		}
		return s.record(ctx, tx, "board.delete", "board", id,
			fmt.Sprintf("Smazána nástěnka „%s“", before.Name), changes, metaHard(hard))
	})
	if err != nil {
		return err
	}
	s.notify("board.changed", map[string]string{"id": id})
	return nil
}

// ---- Columns ----

func (s *Service) ListColumns(ctx context.Context, boardID string) ([]Column, error) {
	return s.store.ListColumns(ctx, s.db, boardID)
}

func (s *Service) CreateColumn(ctx context.Context, boardID string, in ColumnCreate) (*Column, error) {
	if strings.TrimSpace(in.Name) == "" {
		return nil, httpx.ErrUnprocessable("name is required")
	}
	kind := in.Kind
	if kind == "" {
		kind = KindNormal
	}
	if !validKind(kind) {
		return nil, httpx.ErrUnprocessable("kind must be normal|now|done")
	}
	var out *Column
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		board, err := s.store.GetBoard(ctx, tx, boardID)
		if err != nil {
			return err
		}
		if board == nil {
			return httpx.ErrNotFound("board not found")
		}
		pos, err := s.store.lastColumnPosition(ctx, tx, boardID)
		if err != nil {
			return err
		}
		out, err = s.store.InsertColumn(ctx, tx, boardID, in.Name, in.Priority, pos, kind)
		if err != nil {
			return err
		}
		return s.record(ctx, tx, "column.create", "column", out.ID,
			fmt.Sprintf("Vytvořen sloupec „%s“", out.Name),
			[]audit.Change{{Field: "name", New: ap(out.Name)}, {Field: "kind", New: ap(kind)}}, nil)
	})
	if err != nil {
		return nil, err
	}
	s.notify("column.changed", out)
	return out, nil
}

func (s *Service) UpdateColumn(ctx context.Context, id string, in ColumnUpdate) (*Column, error) {
	if in.Name != nil && strings.TrimSpace(*in.Name) == "" {
		return nil, httpx.ErrUnprocessable("name cannot be empty")
	}
	if in.Kind != nil && !validKind(*in.Kind) {
		return nil, httpx.ErrUnprocessable("kind must be normal|now|done")
	}
	var out *Column
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		before, err := s.store.GetColumn(ctx, tx, id)
		if err != nil {
			return err
		}
		if before == nil {
			return httpx.ErrNotFound("column not found")
		}
		if err := s.store.UpdateColumn(ctx, tx, id, in); err != nil {
			return err
		}
		out, err = s.store.GetColumn(ctx, tx, id)
		if err != nil {
			return err
		}
		var changes []audit.Change
		diff(&changes, "name", ap(before.Name), ap(out.Name))
		diff(&changes, "priority", ap(fmt.Sprint(before.Priority)), ap(fmt.Sprint(out.Priority)))
		diff(&changes, "kind", ap(before.Kind), ap(out.Kind))
		return s.record(ctx, tx, "column.update", "column", id,
			fmt.Sprintf("Upraven sloupec „%s“", out.Name), changes, nil)
	})
	if err != nil {
		return nil, err
	}
	s.notify("column.changed", out)
	return out, nil
}

func (s *Service) MoveColumn(ctx context.Context, id, position string) (*Column, error) {
	if position == "" {
		return nil, httpx.ErrUnprocessable("position is required")
	}
	var out *Column
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		before, err := s.store.GetColumn(ctx, tx, id)
		if err != nil {
			return err
		}
		if before == nil {
			return httpx.ErrNotFound("column not found")
		}
		if err := s.store.MoveColumn(ctx, tx, id, position); err != nil {
			return err
		}
		out, err = s.store.GetColumn(ctx, tx, id)
		if err != nil {
			return err
		}
		changes := []audit.Change{{Field: "position", Old: ap(before.Position), New: ap(position)}}
		return s.record(ctx, tx, "column.move", "column", id,
			fmt.Sprintf("Přesunut sloupec „%s“", out.Name), changes, nil)
	})
	if err != nil {
		return nil, err
	}
	s.notify("column.changed", out)
	return out, nil
}

// ReorderColumns rewrites several column positions on one board in a single
// transaction, so a multi-column renumber (the UI's priority→manual conversion)
// applies all-or-nothing — no half-applied order if one write fails. Every column
// must belong to boardID.
func (s *Service) ReorderColumns(ctx context.Context, boardID string, items []ColumnPositionInput) ([]Column, error) {
	if len(items) == 0 {
		return nil, httpx.ErrUnprocessable("columns is required")
	}
	// Validate the batch shape before opening a transaction: every column needs a
	// position, and neither ids nor positions may repeat within the batch. A
	// duplicate position would leave two columns sharing one order key (an
	// ambiguous, unstable ListColumns order); a duplicate id would write the same
	// column twice. The UI's renumber always sends distinct keys, so this only
	// catches a malformed or buggy caller.
	seenID := make(map[string]bool, len(items))
	seenPos := make(map[string]bool, len(items))
	for _, it := range items {
		if it.Position == "" {
			return nil, httpx.ErrUnprocessable("position is required")
		}
		if seenID[it.ID] {
			return nil, httpx.ErrUnprocessable("duplicate column id in batch")
		}
		if seenPos[it.Position] {
			return nil, httpx.ErrUnprocessable("duplicate position in batch")
		}
		seenID[it.ID] = true
		seenPos[it.Position] = true
	}
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		// Global uniqueness: a target position must not collide with a column that is
		// not part of this batch. The within-batch check above only proves the listed
		// columns are distinct; a partial batch could still drop a column onto an order
		// key an untouched column already holds, leaving two columns sharing one key —
		// the ambiguous, unstable ListColumns order this endpoint must prevent.
		existing, err := s.store.ListColumns(ctx, tx, boardID)
		if err != nil {
			return err
		}
		for _, c := range existing {
			if !seenID[c.ID] && seenPos[c.Position] {
				return httpx.ErrUnprocessable("position collides with a column outside the batch")
			}
		}
		for _, it := range items {
			before, err := s.store.GetColumn(ctx, tx, it.ID)
			if err != nil {
				return err
			}
			if before == nil {
				return httpx.ErrNotFound("column not found")
			}
			if before.BoardID != boardID {
				return httpx.ErrUnprocessable("column does not belong to this board")
			}
			if before.Position == it.Position {
				continue
			}
			if err := s.store.MoveColumn(ctx, tx, it.ID, it.Position); err != nil {
				return err
			}
			if err := s.record(ctx, tx, "column.move", "column", it.ID,
				fmt.Sprintf("Přesunut sloupec „%s“", before.Name),
				[]audit.Change{{Field: "position", Old: ap(before.Position), New: ap(it.Position)}}, nil); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	out, err := s.store.ListColumns(ctx, s.db, boardID)
	if err != nil {
		return nil, err
	}
	s.notify("column.changed", map[string]string{"board_id": boardID})
	return out, nil
}

func (s *Service) DeleteColumn(ctx context.Context, id string, cascade bool) error {
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		before, err := s.store.GetColumn(ctx, tx, id)
		if err != nil {
			return err
		}
		if before == nil {
			return httpx.ErrNotFound("column not found")
		}
		n, err := s.store.ColumnCardCount(ctx, tx, id)
		if err != nil {
			return err
		}
		if n > 0 && !cascade {
			return httpx.ErrConflict(fmt.Sprintf("column has %d cards; pass ?cascade=true to delete", n))
		}
		if n > 0 {
			// Log each card deletion before the cascade removes them. Query through
			// tx (not s.db) — a pool read here would deadlock the single connection.
			cards, err := s.store.ColumnCards(ctx, tx, id)
			if err != nil {
				return err
			}
			for _, c := range cards {
				if err := s.record(ctx, tx, "card.delete", "card", c.ID,
					fmt.Sprintf("Smazána karta „%s“ (kaskádou)", c.Title), nil, map[string]any{"cascade": true}); err != nil {
					return err
				}
			}
		}
		if err := s.store.DeleteColumn(ctx, tx, id); err != nil {
			return err
		}
		return s.record(ctx, tx, "column.delete", "column", id,
			fmt.Sprintf("Smazán sloupec „%s“", before.Name), nil, nil)
	})
	if err != nil {
		return err
	}
	s.notify("column.changed", map[string]string{"id": id})
	return nil
}

// ---- record helper ----

func (s *Service) record(ctx context.Context, tx *sql.Tx, action, entityType, entityID, summary string, changes []audit.Change, meta map[string]any) error {
	_, err := s.sink.Record(ctx, tx, audit.Event{
		Module:     audit.ModuleTodo,
		Action:     action,
		EntityType: entityType,
		EntityID:   entityID,
		Summary:    summary,
		Meta:       meta,
		Changes:    changes,
	})
	return err
}

func boolPtr(b bool) *bool { return &b }

func metaHard(hard bool) map[string]any {
	if hard {
		return map[string]any{"hard": true}
	}
	return nil
}

func validKind(k string) bool { return k == KindNormal || k == KindNow || k == KindDone }

// isUniqueViolation reports whether err is a SQLite UNIQUE constraint failure.
func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// validateURL enforces the http/https scheme allowlist (FR-T4).
func validateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return httpx.ErrUnprocessable("url must be a valid http(s) URL")
	}
	return nil
}
