package todo

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/mcp"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
)

// The todo module's MCP provider (v11, PRD §V11-4 FR-M4).
//
// ⚠ EVERY WRITE TOOL CALLS THE MODULE'S EXISTING SERVICE. There is no second
// write path anywhere in v11 — that is G2, and it is what makes the audit event,
// the websocket publish and the role gate come for free. A provider that reached
// the store directly would have to reproduce all three, and would get one wrong.
//
// ⚠ WHAT IS ABSENT, BY NAME: every delete (board, column, card, label, checklist
// item, link), every label mutation, every column mutation, and the board create.
// They are not gated and not confirmed — they do not exist (D295), so the model
// cannot propose them. `home_todo_card_update` can archive a card, which is what
// "delete" means to a household anyway and is reversible.

// ⚠ mcp.NoResources IS EMBEDDED RATHER THAN RE-IMPLEMENTED. todo addresses
// nothing by URI — a card is not a document — and the embedded default refuses
// a read exactly the way an unknown URI is refused, never with a
// distinguishable "this module has no resources" (D303).
type mcpProvider struct {
	mcp.NoResources
	svc *Service
}

// MCPProvider implements mcp.Source. Type-asserted at composition, exactly as
// metrics.Source and lists.Source are — adding it changed no module contract.
func (m *Module) MCPProvider() mcp.Provider { return &mcpProvider{svc: m.svc} }

func (p *mcpProvider) Module() string { return "todo" }

const (
	toolBoards     = "home_todo_boards"
	toolCardCreate = "home_todo_card_create"
	toolCardUpdate = "home_todo_card_update"
	toolCardMove   = "home_todo_card_move"
	toolChecklist  = "home_todo_checklist"
)

func (p *mcpProvider) Tools() []mcp.Tool {
	return []mcp.Tool{
		{
			Name:  toolBoards,
			Title: "To-do boards",
			// ⚠ ONE READ TOOL FOR THE WHOLE MODULE, and that is what keeps
			// home_todo_checklist honestly ReadOnly:false. Splitting the checklist's
			// READ out into the write tool would have made a `reader` unable to see a
			// checklist at all, because the host refuses a non-read-only tool to
			// anyone without editor or admin.
			Description: "Lists the to-do boards; pass board for one board's full tree of columns and cards, or card for one card with its checklist, links and labels.",
			InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "board": {"type": "string", "description": "Board id. Returns that board's columns and their cards."},
    "card": {"type": "string", "description": "Card id. Returns that card in full, including its checklist."},
    "query": {"type": "string", "description": "Optional text filter applied to the board tree's card titles."},
    "include_archived": {"type": "boolean"}
  },
  "additionalProperties": false
}`),
			ReadOnly: true,
		},
		{
			Name:        toolCardCreate,
			Title:       "Create a card",
			Description: "Adds a card to a column; call home_todo_boards first to get the column id, because a column id is not guessable from a board id.",
			InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "column_id": {"type": "string"},
    "title": {"type": "string", "minLength": 1},
    "notes": {"type": "string", "description": "Optional Markdown body."}
  },
  "required": ["column_id", "title"],
  "additionalProperties": false
}`),
		},
		{
			Name:        toolCardUpdate,
			Title:       "Update a card",
			Description: "Changes a card's title, notes or archived flag; archiving is how a household removes a card, and it is reversible where a delete would not be.",
			InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "id": {"type": "string"},
    "title": {"type": "string", "minLength": 1},
    "notes": {"type": "string"},
    "archived": {"type": "boolean"}
  },
  "required": ["id"],
  "additionalProperties": false
}`),
		},
		{
			Name:        toolCardMove,
			Title:       "Move a card",
			Description: "Moves a card to another column — moving it into a column of kind \"done\" is how a task is completed — optionally anchored before a named card.",
			InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "id": {"type": "string"},
    "column_id": {"type": "string"},
    "before_card_id": {"type": "string", "description": "Optional: place it immediately before this card. Omit to append at the end."}
  },
  "required": ["id", "column_id"],
  "additionalProperties": false
}`),
		},
		{
			Name:  toolChecklist,
			Title: "Change a card's checklist",
			// ⚠ TWO VERBS IN ONE TOOL, said out loud in the description because a
			// model handles that reliably when it is told and badly when it is not.
			// Reading a checklist is home_todo_boards's job — see toolBoards.
			Description: "Adds an item to a card's checklist, or ticks and unticks an existing one, and returns the whole checklist afterwards; reading a checklist without changing it is home_todo_boards with card.",
			InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "card_id": {"type": "string"},
    "add": {"type": "string", "description": "Text of a new checklist item to append."},
    "item_id": {"type": "string", "description": "Existing item to tick or untick; requires done."},
    "done": {"type": "boolean", "description": "The state to set item_id to."}
  },
  "required": ["card_id"],
  "additionalProperties": false
}`),
		},
	}
}

func (p *mcpProvider) Call(ctx context.Context, name string, args json.RawMessage) (mcp.Result, error) {
	switch name {
	case toolBoards:
		return p.boards(ctx, args)
	case toolCardCreate:
		return p.cardCreate(ctx, args)
	case toolCardUpdate:
		return p.cardUpdate(ctx, args)
	case toolCardMove:
		return p.cardMove(ctx, args)
	case toolChecklist:
		return p.checklist(ctx, args)
	default:
		return mcp.Result{}, mcp.UnknownToolError(name)
	}
}

type boardsArgs struct {
	Board           string `json:"board"`
	Card            string `json:"card"`
	Query           string `json:"query"`
	IncludeArchived bool   `json:"include_archived"`
}

func (p *mcpProvider) boards(ctx context.Context, args json.RawMessage) (mcp.Result, error) {
	var in boardsArgs
	if err := mcp.DecodeArgs(args, &in); err != nil {
		return mcp.Result{}, err
	}
	switch {
	case in.Card != "":
		card, err := p.svc.GetCardDetail(ctx, in.Card)
		if err != nil {
			return mcp.Result{}, err
		}
		if card == nil {
			return mcp.NotFoundResult(), nil
		}
		return mcp.TextResult(renderCard(*card), card)
	case in.Board != "":
		tree, err := p.svc.Tree(ctx, in.Board, nil, in.Query, in.IncludeArchived)
		if err != nil {
			return mcp.Result{}, err
		}
		if tree == nil {
			return mcp.NotFoundResult(), nil
		}
		return mcp.TextResult(renderTree(*tree), tree)
	default:
		boards, err := p.svc.ListBoards(ctx)
		if err != nil {
			return mcp.Result{}, err
		}
		var b strings.Builder
		fmt.Fprintf(&b, "%d nástěnek:\n", len(boards))
		for _, board := range boards {
			fmt.Fprintf(&b, "\n• %s (id %s)", board.Name, board.ID)
			if board.Archived {
				b.WriteString(" [archivováno]")
			}
		}
		return mcp.TextResult(b.String(), boards)
	}
}

type cardCreateArgs struct {
	ColumnID string `json:"column_id"`
	Title    string `json:"title"`
	Notes    string `json:"notes"`
}

// cardCreate validates BEFORE it reaches the service (D311).
//
// ⚠ THE RULE EXISTS BECAUSE OF electricity: a negative invoiced value surfaces
// there as a 500 instead of a 422, and AN AGENT RETRIES A 500 AND GIVES UP ON A
// 422 — so a household typo returning the wrong code turns a mistake into a loop.
// No write tool in v11 may reach a service with an input it could have refused
// itself.
func (p *mcpProvider) cardCreate(ctx context.Context, args json.RawMessage) (mcp.Result, error) {
	var in cardCreateArgs
	if err := mcp.DecodeArgs(args, &in); err != nil {
		return mcp.Result{}, err
	}
	if strings.TrimSpace(in.ColumnID) == "" {
		return mcp.Result{}, httpx.ErrUnprocessable("Chybí column_id.")
	}
	if strings.TrimSpace(in.Title) == "" {
		return mcp.Result{}, httpx.ErrUnprocessable("Název karty nesmí být prázdný.")
	}
	card, err := p.svc.CreateCard(ctx, in.ColumnID, CardCreate{Title: in.Title, Notes: in.Notes})
	if err != nil {
		return mcp.Result{}, err
	}
	return mcp.TextResult(fmt.Sprintf("Karta „%s“ vytvořena (id %s).", card.Title, card.ID), card)
}

type cardUpdateArgs struct {
	ID       string  `json:"id"`
	Title    *string `json:"title"`
	Notes    *string `json:"notes"`
	Archived *bool   `json:"archived"`
}

func (p *mcpProvider) cardUpdate(ctx context.Context, args json.RawMessage) (mcp.Result, error) {
	var in cardUpdateArgs
	if err := mcp.DecodeArgs(args, &in); err != nil {
		return mcp.Result{}, err
	}
	if strings.TrimSpace(in.ID) == "" {
		return mcp.Result{}, httpx.ErrUnprocessable("Chybí id karty.")
	}
	if in.Title != nil && strings.TrimSpace(*in.Title) == "" {
		return mcp.Result{}, httpx.ErrUnprocessable("Název karty nesmí být prázdný.")
	}
	if in.Title == nil && in.Notes == nil && in.Archived == nil {
		return mcp.Result{}, httpx.ErrUnprocessable("Zadejte alespoň jednu změnu.")
	}
	card, err := p.svc.UpdateCard(ctx, in.ID, CardUpdate{Title: in.Title, Notes: in.Notes, Archived: in.Archived})
	if err != nil {
		return mcp.Result{}, err
	}
	if card == nil {
		return mcp.NotFoundResult(), nil
	}
	return mcp.TextResult(fmt.Sprintf("Karta „%s“ upravena.", card.Title), card)
}

type cardMoveArgs struct {
	ID           string `json:"id"`
	ColumnID     string `json:"column_id"`
	BeforeCardID string `json:"before_card_id"`
}

func (p *mcpProvider) cardMove(ctx context.Context, args json.RawMessage) (mcp.Result, error) {
	var in cardMoveArgs
	if err := mcp.DecodeArgs(args, &in); err != nil {
		return mcp.Result{}, err
	}
	if strings.TrimSpace(in.ID) == "" || strings.TrimSpace(in.ColumnID) == "" {
		return mcp.Result{}, httpx.ErrUnprocessable("Zadejte id karty i cílový sloupec.")
	}
	// The empty `via` is what the HTTP handler passes for an ordinary move; it
	// selects the audit summary's wording and has nothing to do with v11's `via`
	// column, which the sink reads from the context.
	card, err := p.svc.MoveCard(ctx, in.ID, CardMoveRequest{ColumnID: in.ColumnID, BeforeCardID: in.BeforeCardID}, "")
	if err != nil {
		return mcp.Result{}, err
	}
	if card == nil {
		return mcp.NotFoundResult(), nil
	}
	return mcp.TextResult(fmt.Sprintf("Karta „%s“ přesunuta.", card.Title), card)
}

type checklistArgs struct {
	CardID string `json:"card_id"`
	Add    string `json:"add"`
	ItemID string `json:"item_id"`
	Done   *bool  `json:"done"`
}

func (p *mcpProvider) checklist(ctx context.Context, args json.RawMessage) (mcp.Result, error) {
	var in checklistArgs
	if err := mcp.DecodeArgs(args, &in); err != nil {
		return mcp.Result{}, err
	}
	if strings.TrimSpace(in.CardID) == "" {
		return mcp.Result{}, httpx.ErrUnprocessable("Chybí card_id.")
	}
	if in.Add == "" && in.ItemID == "" {
		return mcp.Result{}, httpx.ErrUnprocessable("Zadejte add, nebo item_id spolu s done.")
	}
	if in.ItemID != "" && in.Done == nil {
		return mcp.Result{}, httpx.ErrUnprocessable("K item_id je potřeba i done.")
	}
	// ⚠ THE ITEM MUST BELONG TO THE CARD THE CALLER NAMED, and nothing under this
	// checks it: Service.UpdateChecklistItem resolves an item by id ALONE, which is
	// right for its HTTP twin (that route is item-addressed and carries no card) and
	// wrong here, where the tool takes both and then renders card_id's list. A
	// mismatched pair ticked a box on a DIFFERENT card, audited it, and answered
	// with a checklist the change was not in — so the model read its own write as
	// having failed and sent it again.
	//
	// ⚠ AND IT RUNS BEFORE THE ADD, because this tool can do two things in one
	// call: checking after would append the new item and then refuse, leaving a
	// half-applied write, which is worse than either whole outcome. It costs one
	// read, only when item_id is given, and it is the same read that makes an
	// unknown card_id a clean refusal rather than an empty list.
	if in.ItemID != "" {
		before, err := p.svc.ListChecklist(ctx, in.CardID)
		if err != nil {
			return mcp.Result{}, err
		}
		if !hasChecklistItem(before, in.ItemID) {
			return mcp.NotFoundResult(), nil
		}
	}
	if in.Add != "" {
		if strings.TrimSpace(in.Add) == "" {
			return mcp.Result{}, httpx.ErrUnprocessable("Text položky nesmí být prázdný.")
		}
		if _, err := p.svc.CreateChecklistItem(ctx, in.CardID, ChecklistItemCreate{Text: in.Add}); err != nil {
			return mcp.Result{}, err
		}
	}
	if in.ItemID != "" {
		item, err := p.svc.UpdateChecklistItem(ctx, in.ItemID, ChecklistItemUpdate{Done: in.Done})
		if err != nil {
			return mcp.Result{}, err
		}
		if item == nil {
			return mcp.NotFoundResult(), nil
		}
	}
	items, err := p.svc.ListChecklist(ctx, in.CardID)
	if err != nil {
		return mcp.Result{}, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Checklist (%d položek):\n", len(items))
	for _, it := range items {
		mark := "☐"
		if it.Done {
			mark = "☑"
		}
		fmt.Fprintf(&b, "\n%s %s (id %s)", mark, it.Text, it.ID)
	}
	return mcp.TextResult(b.String(), items)
}

// hasChecklistItem reports whether id is one of this card's items.
func hasChecklistItem(items []ChecklistItem, id string) bool {
	for _, it := range items {
		if it.ID == id {
			return true
		}
	}
	return false
}

// Search scans card titles across every board.
//
// ⚠ THE ACTOR CHECK IS NOT DEFENSIVE POLISH — IT IS D302, AND IT IS THE ONE THING
// EVERY PROVIDER MUST GET RIGHT. An MCP tool call has an actor only AFTER the
// token resolves, so every provider is one refactor away from being called
// without one. v9's build found two surfaces no review had listed — the preview
// worker and the image GC — because a background job has no actor, a
// viewer-scoped read returns nothing, and the next person reaches for the
// unscoped load. An ERROR, never an empty slice: an empty slice is what an
// unscoped-load bug looks like on the day somebody "fixes" the nil.
//
// ⚠ Todo itself is household-visible — there is no per-member scope to apply
// here — which is exactly why the check has to be written down rather than left
// implicit. The rule is the same in every provider whether or not this one's data
// happens to be shared.
func (p *mcpProvider) Search(ctx context.Context, q mcp.Query) ([]mcp.Hit, error) {
	if _, ok := reqctx.ActorFrom(ctx); !ok {
		return nil, fmt.Errorf("todo: search without an actor")
	}
	rows, err := p.svc.Store().SearchCards(ctx, q.Text, q.Limit)
	if err != nil {
		return nil, err
	}
	hits := make([]mcp.Hit, 0, len(rows))
	for _, r := range rows {
		updated, _ := time.Parse(time.RFC3339, r.UpdatedAt)
		hits = append(hits, mcp.Hit{
			Kind:      "todo.card",
			ID:        r.ID,
			Title:     r.Title,
			Snippet:   r.BoardName + " · " + r.ColumnName,
			UpdatedAt: updated,
			ExactHit:  strings.EqualFold(strings.TrimSpace(r.Title), strings.TrimSpace(q.Text)),
		})
	}
	return hits, nil
}

// Get answers home_get for this module's kinds.
func (p *mcpProvider) Get(ctx context.Context, kind, id string) (mcp.Result, error) {
	if kind != "todo.card" {
		return mcp.NotFoundResult(), nil
	}
	card, err := p.svc.GetCardDetail(ctx, id)
	if err != nil {
		return mcp.Result{}, err
	}
	if card == nil {
		return mcp.NotFoundResult(), nil
	}
	return mcp.TextResult(renderCard(*card), card)
}

// ---- rendering ----

func renderTree(t BoardTree) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Nástěnka „%s“ (id %s)", t.Board.Name, t.Board.ID)
	for _, col := range t.Columns {
		fmt.Fprintf(&b, "\n\n%s [%s] — %d karet (id %s)", col.Column.Name, col.Column.Kind, col.CardCount, col.Column.ID)
		for _, c := range col.Cards {
			fmt.Fprintf(&b, "\n  • %s (id %s)", c.Title, c.ID)
			if c.ChecklistProgress.Total > 0 {
				fmt.Fprintf(&b, " [%d/%d]", c.ChecklistProgress.Done, c.ChecklistProgress.Total)
			}
		}
	}
	return b.String()
}

func renderCard(c CardDetail) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s (id %s, sloupec %s)", c.Title, c.ID, c.ColumnID)
	if c.Notes != nil && *c.Notes != "" {
		fmt.Fprintf(&b, "\n\n%s", *c.Notes)
	}
	if len(c.Checklist) > 0 {
		b.WriteString("\n\nChecklist:")
		for _, it := range c.Checklist {
			mark := "☐"
			if it.Done {
				mark = "☑"
			}
			fmt.Fprintf(&b, "\n  %s %s (id %s)", mark, it.Text, it.ID)
		}
	}
	if len(c.Labels) > 0 {
		names := make([]string, 0, len(c.Labels))
		for _, l := range c.Labels {
			names = append(names, l.Name)
		}
		fmt.Fprintf(&b, "\n\nŠtítky: %s", strings.Join(names, ", "))
	}
	return b.String()
}
