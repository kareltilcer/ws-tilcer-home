package todo

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/audit"
	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/db"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/httpx"
)

// ---- Cards ----

func (s *Service) GetCardDetail(ctx context.Context, id string) (*CardDetail, error) {
	d, err := s.store.GetCardDetail(ctx, id)
	if err != nil {
		return nil, err
	}
	if d == nil {
		return nil, httpx.ErrNotFound("card not found")
	}
	return d, nil
}

func (s *Service) CreateCard(ctx context.Context, columnID string, in CardCreate) (*Card, error) {
	if strings.TrimSpace(in.Title) == "" {
		return nil, httpx.ErrUnprocessable("title is required")
	}
	var out *Card
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		col, err := s.store.GetColumn(ctx, tx, columnID)
		if err != nil {
			return err
		}
		if col == nil {
			return httpx.ErrNotFound("column not found")
		}
		pos, err := s.store.lastCardPosition(ctx, tx, columnID)
		if err != nil {
			return err
		}
		out, err = s.store.InsertCard(ctx, tx, columnID, in.Title, in.Notes, pos, actorID(ctx))
		if err != nil {
			return err
		}
		return s.record(ctx, tx, "card.create", "card", out.ID,
			fmt.Sprintf("Vytvořena karta „%s“", out.Title),
			[]audit.Change{{Field: "title", New: ap(out.Title)}}, nil)
	})
	if err != nil {
		return nil, err
	}
	s.notify("card.changed", out)
	return out, nil
}

func (s *Service) UpdateCard(ctx context.Context, id string, in CardUpdate) (*Card, error) {
	if in.Title != nil && strings.TrimSpace(*in.Title) == "" {
		return nil, httpx.ErrUnprocessable("title cannot be empty")
	}
	var out *Card
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		before, err := s.store.GetCard(ctx, tx, id)
		if err != nil {
			return err
		}
		if before == nil {
			return httpx.ErrNotFound("card not found")
		}
		if err := s.store.UpdateCard(ctx, tx, id, in); err != nil {
			return err
		}
		out, err = s.store.GetCard(ctx, tx, id)
		if err != nil {
			return err
		}
		var changes []audit.Change
		diff(&changes, "title", ap(before.Title), ap(out.Title))
		diff(&changes, "notes", before.Notes, out.Notes)
		diff(&changes, "archived", ap(fmt.Sprint(before.Archived)), ap(fmt.Sprint(out.Archived)))
		return s.record(ctx, tx, "card.update", "card", id,
			fmt.Sprintf("Upravena karta „%s“", out.Title), changes, nil)
	})
	if err != nil {
		return nil, err
	}
	s.notify("card.changed", out)
	return out, nil
}

// MoveCard moves a card to a column and/or position. Entering a kind=done column
// stamps done_at; leaving clears it. via records where the move was triggered
// (e.g. "dashboard") for cross-module attribution — this is the single move
// implementation the dashboard reuses.
func (s *Service) MoveCard(ctx context.Context, id string, in CardMoveRequest, via string) (*Card, error) {
	if strings.TrimSpace(in.ColumnID) == "" {
		return nil, httpx.ErrUnprocessable("column_id is required")
	}
	var out *Card
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		before, err := s.store.GetCard(ctx, tx, id)
		if err != nil {
			return err
		}
		if before == nil {
			return httpx.ErrNotFound("card not found")
		}
		target, err := s.store.GetColumn(ctx, tx, in.ColumnID)
		if err != nil {
			return err
		}
		if target == nil {
			return httpx.ErrUnprocessable("target column does not exist")
		}
		pos := in.Position
		if pos == "" {
			pos, err = s.store.lastCardPosition(ctx, tx, in.ColumnID)
			if err != nil {
				return err
			}
		}
		if err := s.store.MoveCard(ctx, tx, id, in.ColumnID, pos, target.Kind == KindDone); err != nil {
			return err
		}
		out, err = s.store.GetCard(ctx, tx, id)
		if err != nil {
			return err
		}
		var changes []audit.Change
		diff(&changes, "column_id", ap(before.ColumnID), ap(out.ColumnID))
		diff(&changes, "done_at", before.DoneAt, out.DoneAt)
		var meta map[string]any
		if via != "" {
			meta = map[string]any{"via": via}
		}
		return s.record(ctx, tx, "card.move", "card", id,
			fmt.Sprintf("Přesunuta karta „%s“ do „%s“", out.Title, target.Name), changes, meta)
	})
	if err != nil {
		return nil, err
	}
	s.notify("card.changed", out)
	return out, nil
}

func (s *Service) DeleteCard(ctx context.Context, id string, hard bool) error {
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		before, err := s.store.GetCard(ctx, tx, id)
		if err != nil {
			return err
		}
		if before == nil {
			return httpx.ErrNotFound("card not found")
		}
		var changes []audit.Change
		if hard {
			if err := s.store.DeleteCard(ctx, tx, id); err != nil {
				return err
			}
		} else {
			if err := s.store.UpdateCard(ctx, tx, id, CardUpdate{Archived: boolPtr(true)}); err != nil {
				return err
			}
			changes = []audit.Change{{Field: "archived", Old: ap("false"), New: ap("true")}}
		}
		return s.record(ctx, tx, "card.delete", "card", id,
			fmt.Sprintf("Smazána karta „%s“", before.Title), changes, metaHard(hard))
	})
	if err != nil {
		return err
	}
	s.notify("card.changed", map[string]string{"id": id})
	return nil
}

// ---- Card links ----

func (s *Service) ListCardLinks(ctx context.Context, cardID string) ([]CardLink, error) {
	card, err := s.store.GetCard(ctx, s.db, cardID)
	if err != nil {
		return nil, err
	}
	if card == nil {
		return nil, httpx.ErrNotFound("card not found")
	}
	return s.store.ListCardLinks(ctx, s.db, cardID)
}

func (s *Service) CreateCardLink(ctx context.Context, cardID string, in CardLinkCreate) (*CardLink, error) {
	if err := validateURL(in.URL); err != nil {
		return nil, err
	}
	var out *CardLink
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		card, err := s.store.GetCard(ctx, tx, cardID)
		if err != nil {
			return err
		}
		if card == nil {
			return httpx.ErrNotFound("card not found")
		}
		pos, err := s.store.lastLinkPosition(ctx, tx, cardID)
		if err != nil {
			return err
		}
		out, err = s.store.InsertCardLink(ctx, tx, cardID, in.URL, in.Title, pos)
		if err != nil {
			return err
		}
		return s.record(ctx, tx, "card.link.add", "card", cardID,
			fmt.Sprintf("Přidán odkaz ke kartě: %s", in.URL), nil, nil)
	})
	if err != nil {
		return nil, err
	}
	s.notify("card.changed", map[string]string{"id": cardID})
	return out, nil
}

func (s *Service) DeleteCardLink(ctx context.Context, linkID string) error {
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		cardID, err := s.store.LinkCardID(ctx, tx, linkID)
		if err != nil {
			return err
		}
		if cardID == "" {
			return httpx.ErrNotFound("link not found")
		}
		if err := s.store.DeleteCardLink(ctx, tx, linkID); err != nil {
			return err
		}
		return s.record(ctx, tx, "card.link.remove", "card", cardID, "Odebrán odkaz z karty", nil, nil)
	})
	if err != nil {
		return err
	}
	return nil
}

// ---- Checklist ----

func (s *Service) ListChecklist(ctx context.Context, cardID string) ([]ChecklistItem, error) {
	card, err := s.store.GetCard(ctx, s.db, cardID)
	if err != nil {
		return nil, err
	}
	if card == nil {
		return nil, httpx.ErrNotFound("card not found")
	}
	return s.store.ListChecklist(ctx, s.db, cardID)
}

func (s *Service) CreateChecklistItem(ctx context.Context, cardID string, in ChecklistItemCreate) (*ChecklistItem, error) {
	if strings.TrimSpace(in.Text) == "" {
		return nil, httpx.ErrUnprocessable("text is required")
	}
	var out *ChecklistItem
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		card, err := s.store.GetCard(ctx, tx, cardID)
		if err != nil {
			return err
		}
		if card == nil {
			return httpx.ErrNotFound("card not found")
		}
		pos, err := s.store.lastChecklistPosition(ctx, tx, cardID)
		if err != nil {
			return err
		}
		out, err = s.store.InsertChecklistItem(ctx, tx, cardID, in.Text, pos)
		if err != nil {
			return err
		}
		return s.record(ctx, tx, "checklist_item.create", "checklist_item", out.ID,
			fmt.Sprintf("Přidán bod checklistu „%s“", out.Text),
			[]audit.Change{{Field: "text", New: ap(out.Text)}}, map[string]any{"card_id": cardID})
	})
	if err != nil {
		return nil, err
	}
	s.notify("card.changed", map[string]string{"id": cardID})
	return out, nil
}

func (s *Service) UpdateChecklistItem(ctx context.Context, id string, in ChecklistItemUpdate) (*ChecklistItem, error) {
	if in.Text != nil && strings.TrimSpace(*in.Text) == "" {
		return nil, httpx.ErrUnprocessable("text cannot be empty")
	}
	var out *ChecklistItem
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		before, err := s.store.GetChecklistItem(ctx, tx, id)
		if err != nil {
			return err
		}
		if before == nil {
			return httpx.ErrNotFound("checklist item not found")
		}
		if err := s.store.UpdateChecklistItem(ctx, tx, id, in); err != nil {
			return err
		}
		out, err = s.store.GetChecklistItem(ctx, tx, id)
		if err != nil {
			return err
		}
		var changes []audit.Change
		diff(&changes, "text", ap(before.Text), ap(out.Text))
		diff(&changes, "done", ap(fmt.Sprint(before.Done)), ap(fmt.Sprint(out.Done)))
		return s.record(ctx, tx, "checklist_item.update", "checklist_item", id,
			fmt.Sprintf("Upraven bod checklistu „%s“", out.Text), changes, map[string]any{"card_id": out.CardID})
	})
	if err != nil {
		return nil, err
	}
	s.notify("card.changed", map[string]string{"id": out.CardID})
	return out, nil
}

func (s *Service) DeleteChecklistItem(ctx context.Context, id string) error {
	var cardID string
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		before, err := s.store.GetChecklistItem(ctx, tx, id)
		if err != nil {
			return err
		}
		if before == nil {
			return httpx.ErrNotFound("checklist item not found")
		}
		cardID = before.CardID
		if err := s.store.DeleteChecklistItem(ctx, tx, id); err != nil {
			return err
		}
		return s.record(ctx, tx, "checklist_item.delete", "checklist_item", id,
			fmt.Sprintf("Smazán bod checklistu „%s“", before.Text), nil, map[string]any{"card_id": cardID})
	})
	if err != nil {
		return err
	}
	s.notify("card.changed", map[string]string{"id": cardID})
	return nil
}

// ---- Labels ----

func (s *Service) ListLabels(ctx context.Context, boardID string) ([]Label, error) {
	board, err := s.store.GetBoard(ctx, s.db, boardID)
	if err != nil {
		return nil, err
	}
	if board == nil {
		return nil, httpx.ErrNotFound("board not found")
	}
	return s.store.ListLabels(ctx, s.db, boardID)
}

func (s *Service) CreateLabel(ctx context.Context, boardID string, in LabelCreate) (*Label, error) {
	if strings.TrimSpace(in.Name) == "" || strings.TrimSpace(in.Color) == "" {
		return nil, httpx.ErrUnprocessable("name and color are required")
	}
	var out *Label
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		board, err := s.store.GetBoard(ctx, tx, boardID)
		if err != nil {
			return err
		}
		if board == nil {
			return httpx.ErrNotFound("board not found")
		}
		out, err = s.store.InsertLabel(ctx, tx, boardID, in.Name, in.Color)
		if err != nil {
			if isUniqueViolation(err) {
				return httpx.ErrConflict("a label with this name already exists on the board")
			}
			return err
		}
		return s.record(ctx, tx, "label.create", "label", out.ID,
			fmt.Sprintf("Vytvořen štítek „%s“", out.Name),
			[]audit.Change{{Field: "name", New: ap(out.Name)}, {Field: "color", New: ap(out.Color)}}, nil)
	})
	if err != nil {
		return nil, err
	}
	s.notify("label.changed", out)
	return out, nil
}

func (s *Service) UpdateLabel(ctx context.Context, id string, in LabelCreate) (*Label, error) {
	if strings.TrimSpace(in.Name) == "" || strings.TrimSpace(in.Color) == "" {
		return nil, httpx.ErrUnprocessable("name and color are required")
	}
	var out *Label
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		before, err := s.store.GetLabel(ctx, tx, id)
		if err != nil {
			return err
		}
		if before == nil {
			return httpx.ErrNotFound("label not found")
		}
		if err := s.store.UpdateLabel(ctx, tx, id, in.Name, in.Color); err != nil {
			if isUniqueViolation(err) {
				return httpx.ErrConflict("a label with this name already exists on the board")
			}
			return err
		}
		out, err = s.store.GetLabel(ctx, tx, id)
		if err != nil {
			return err
		}
		var changes []audit.Change
		diff(&changes, "name", ap(before.Name), ap(out.Name))
		diff(&changes, "color", ap(before.Color), ap(out.Color))
		return s.record(ctx, tx, "label.update", "label", id,
			fmt.Sprintf("Upraven štítek „%s“", out.Name), changes, nil)
	})
	if err != nil {
		return nil, err
	}
	s.notify("label.changed", out)
	return out, nil
}

func (s *Service) DeleteLabel(ctx context.Context, id string) error {
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		before, err := s.store.GetLabel(ctx, tx, id)
		if err != nil {
			return err
		}
		if before == nil {
			return httpx.ErrNotFound("label not found")
		}
		if err := s.store.DeleteLabel(ctx, tx, id); err != nil {
			return err
		}
		return s.record(ctx, tx, "label.delete", "label", id,
			fmt.Sprintf("Smazán štítek „%s“ (odebrán ze všech karet)", before.Name), nil, nil)
	})
	if err != nil {
		return err
	}
	s.notify("label.changed", map[string]string{"id": id})
	return nil
}

func (s *Service) AttachLabel(ctx context.Context, cardID, labelID string) error {
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		card, err := s.store.GetCard(ctx, tx, cardID)
		if err != nil {
			return err
		}
		if card == nil {
			return httpx.ErrNotFound("card not found")
		}
		label, err := s.store.GetLabel(ctx, tx, labelID)
		if err != nil {
			return err
		}
		if label == nil {
			return httpx.ErrNotFound("label not found")
		}
		if err := s.store.AttachLabel(ctx, tx, cardID, labelID); err != nil {
			return err
		}
		return s.record(ctx, tx, "card.label.attach", "card", cardID,
			fmt.Sprintf("Přidán štítek „%s“ na kartu", label.Name), nil, map[string]any{"label_id": labelID})
	})
	if err != nil {
		return err
	}
	s.notify("card.changed", map[string]string{"id": cardID})
	return nil
}

func (s *Service) DetachLabel(ctx context.Context, cardID, labelID string) error {
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		if err := s.store.DetachLabel(ctx, tx, cardID, labelID); err != nil {
			return err
		}
		return s.record(ctx, tx, "card.label.detach", "card", cardID,
			"Odebrán štítek z karty", nil, map[string]any{"label_id": labelID})
	})
	if err != nil {
		return err
	}
	s.notify("card.changed", map[string]string{"id": cardID})
	return nil
}
