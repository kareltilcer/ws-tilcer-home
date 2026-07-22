// Package todo implements the Úkoly board: boards → columns → cards, with
// links, checklists and labels. Every mutation writes an audit event in the same
// transaction as the change (via the spine) and publishes a change to the
// websocket hub after commit.
package todo

// ---- Wire types (match openapi.yaml schemas) ----

type Board struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
	Position    string  `json:"position"`
	Archived    bool    `json:"archived"`
	CreatedBy   *string `json:"created_by"`
	CreatedAt   string  `json:"created_at"`
}

type Column struct {
	ID        string `json:"id"`
	BoardID   string `json:"board_id"`
	Name      string `json:"name"`
	Priority  int    `json:"priority"`
	Position  string `json:"position"`
	Kind      string `json:"kind"`
	CreatedAt string `json:"created_at"`
}

type ChecklistProgress struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}

type Card struct {
	ID                string            `json:"id"`
	ColumnID          string            `json:"column_id"`
	Title             string            `json:"title"`
	Notes             *string           `json:"notes"`
	Position          string            `json:"position"`
	Archived          bool              `json:"archived"`
	DoneAt            *string           `json:"done_at"`
	LabelIDs          []string          `json:"label_ids"`
	ChecklistProgress ChecklistProgress `json:"checklist_progress"`
	CreatedBy         *string           `json:"created_by"`
	CreatedAt         string            `json:"created_at"`
	UpdatedAt         string            `json:"updated_at"`
}

type CardDetail struct {
	Card
	Links     []CardLink      `json:"links"`
	Checklist []ChecklistItem `json:"checklist"`
	Labels    []Label         `json:"labels"`
}

type CardLink struct {
	ID       string  `json:"id"`
	CardID   string  `json:"card_id"`
	URL      string  `json:"url"`
	Title    *string `json:"title"`
	Position string  `json:"position"`
}

type ChecklistItem struct {
	ID       string `json:"id"`
	CardID   string `json:"card_id"`
	Text     string `json:"text"`
	Done     bool   `json:"done"`
	Position string `json:"position"`
}

type Label struct {
	ID      string `json:"id"`
	BoardID string `json:"board_id"`
	Name    string `json:"name"`
	Color   string `json:"color"`
}

type BoardTreeColumn struct {
	Column Column `json:"column"`
	Cards  []Card `json:"cards"`
}

type BoardTree struct {
	Board   Board             `json:"board"`
	Columns []BoardTreeColumn `json:"columns"`
}

// ---- Request DTOs. Pointer fields on *Update mean "unset ⇒ leave unchanged". ----

type BoardCreate struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type BoardUpdate struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Archived    *bool   `json:"archived"`
}

type ColumnCreate struct {
	Name     string `json:"name"`
	Priority int    `json:"priority"`
	Kind     string `json:"kind"`
}

type ColumnUpdate struct {
	Name     *string `json:"name"`
	Priority *int    `json:"priority"`
	Kind     *string `json:"kind"`
}

type CardCreate struct {
	Title string `json:"title"`
	Notes string `json:"notes"`
}

type CardUpdate struct {
	Title    *string `json:"title"`
	Notes    *string `json:"notes"`
	Archived *bool   `json:"archived"`
}

type CardMoveRequest struct {
	ColumnID string `json:"column_id"`
	Position string `json:"position"`
}

type MoveRequest struct {
	Position string `json:"position"`
}

type CardLinkCreate struct {
	URL   string `json:"url"`
	Title string `json:"title"`
}

type ChecklistItemCreate struct {
	Text string `json:"text"`
}

type ChecklistItemUpdate struct {
	Text     *string `json:"text"`
	Done     *bool   `json:"done"`
	Position *string `json:"position"`
}

type LabelCreate struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

// Column kinds.
const (
	KindNormal = "normal"
	KindNow    = "now"
	KindDone   = "done"
)
