package todo

import (
	"context"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/registry"
)

// DashboardTask is a card sitting in a kind='now' column, shaped for the
// todo.pravedelam widget (matches openapi DashboardTask). DoneColumnID is
// additive: the board's first kind='done' column so the widget can mark-done via
// the card-move endpoint (else null → archive, D15).
type DashboardTask struct {
	CardID            string            `json:"card_id"`
	Title             string            `json:"title"`
	BoardID           string            `json:"board_id"`
	BoardName         string            `json:"board_name"`
	ColumnID          string            `json:"column_id"`
	ColumnName        string            `json:"column_name"`
	LabelIDs          []string          `json:"label_ids"`
	ChecklistProgress ChecklistProgress `json:"checklist_progress"`
	DoneColumnID      *string           `json:"done_column_id"`
}

// PravedelamWidget is the todo.pravedelam payload (openapi PravedelamWidget).
type PravedelamWidget struct {
	Tasks []DashboardTask `json:"tasks"`
}

// pravedelamProvider implements registry.WidgetProvider for FR-T8. It reads only
// this module's store; the dashboard host calls Data and never touches todo
// tables directly (D28).
type pravedelamProvider struct{ store *Store }

func newPravedelamProvider(store *Store) registry.WidgetProvider {
	return &pravedelamProvider{store: store}
}

func (p *pravedelamProvider) Key() string    { return "todo.pravedelam" }
func (p *pravedelamProvider) Title() string  { return "Právě dělám" }
func (p *pravedelamProvider) Module() string { return "todo" }
func (p *pravedelamProvider) Description() string {
	return "Úkoly ve sloupcích „Právě dělám“ napříč všemi nástěnkami."
}
func (p *pravedelamProvider) DefaultSize() string { return "wide" }
func (p *pravedelamProvider) AdminOnly() bool     { return false }

// Data returns every non-archived card in a kind='now' column across all boards
// (one indexed query + batched fills, no N+1), each carrying its board's first
// done column for the in-widget mark-done gesture.
func (p *pravedelamProvider) Data(ctx context.Context, _ registry.User) (any, error) {
	cards, err := p.store.NowCards(ctx)
	if err != nil {
		return nil, err
	}
	doneCols, err := p.store.FirstDoneColumns(ctx)
	if err != nil {
		return nil, err
	}
	tasks := make([]DashboardTask, 0, len(cards))
	for _, c := range cards {
		var doneCol *string
		if id, ok := doneCols[c.BoardID]; ok {
			d := id
			doneCol = &d
		}
		tasks = append(tasks, DashboardTask{
			CardID:            c.CardID,
			Title:             c.Title,
			BoardID:           c.BoardID,
			BoardName:         c.BoardName,
			ColumnID:          c.ColumnID,
			ColumnName:        c.ColumnName,
			LabelIDs:          c.LabelIDs,
			ChecklistProgress: c.ChecklistProgress,
			DoneColumnID:      doneCol,
		})
	}
	return PravedelamWidget{Tasks: tasks}, nil
}
