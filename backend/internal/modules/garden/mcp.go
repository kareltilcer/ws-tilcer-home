package garden

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

// The garden module's MCP provider (v11, PRD §V11-4 FR-M4).
//
// ⚠ `season_close` AND `season_reopen` ARE ABSENT, AND THIS IS THE ABSENCE MOST
// EASILY MISREAD AS OVER-CAUTION (D295). `CloseSeason` runs `closeOutTasks`,
// which marks every still-open task `TaskSkipped` — it DESTROYS work rather than
// regenerating it — and the closed year then becomes the rotation history checks
// C3 and C8 read (`check.go` states that dependency outright: they read CLOSED
// seasons only). `ReopenSeason` is this module's ONLY admin-gated action, and
// *"přepisuje se tím historie střídání plodin"* is its audit summary — not UI
// copy, and not a confirmation string anybody has ever seen. Neither belongs on
// a surface where nobody is watching the screen.
//
// ⚠ ALSO ABSENT: every delete, every plant and variety mutation, every bed verb,
// `ShiftTasks`, the LLM import, and the storage (sklad) verbs. The six that ship
// are the ones a person does standing in the garden with a phone.

type mcpProvider struct {
	mcp.NoResources
	svc *Service
}

// MCPProvider implements mcp.Source.
func (m *Module) MCPProvider() mcp.Provider { return &mcpProvider{svc: m.svc} }

func (p *mcpProvider) Module() string { return "garden" }

const (
	toolTasks          = "home_garden_tasks"
	toolTaskCreate     = "home_garden_task_create"
	toolTaskComplete   = "home_garden_task_complete"
	toolPlan           = "home_garden_plan"
	toolPlantingCreate = "home_garden_planting_create"
	toolHarvestLog     = "home_garden_harvest_log"
)

func (p *mcpProvider) Tools() []mcp.Tool {
	return []mcp.Tool{
		{
			Name:        toolTasks,
			Title:       "Garden work",
			Description: "Returns the garden jobs whose window overlaps a date range — sowing, transplanting, watering, harvesting — with which are overdue; defaults to the next fourteen days.",
			InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "from": {"type": "string", "description": "ISO date (YYYY-MM-DD). Defaults to today."},
    "to": {"type": "string", "description": "ISO date (YYYY-MM-DD). Defaults to 14 days after from."},
    "status": {"type": "string", "description": "Filter by status, e.g. \"open\"."},
    "bed_id": {"type": "string"},
    "limit": {"type": "integer", "minimum": 1}
  },
  "additionalProperties": false
}`),
			ReadOnly: true,
		},
		{
			Name:        toolTaskCreate,
			Title:       "Add a garden job",
			Description: "Adds a manual job to the current season with a Czech title and a date window; the season must be open, because a closed year is the rotation history the plan checks read.",
			InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "title_cs": {"type": "string", "minLength": 1, "description": "What to do, in Czech — this is what appears on the work list."},
    "window_from": {"type": "string", "description": "ISO date (YYYY-MM-DD)."},
    "window_to": {"type": "string", "description": "ISO date (YYYY-MM-DD), on or after window_from."},
    "kind": {"type": "string", "description": "Optional job kind."},
    "season_year": {"type": "integer", "description": "Defaults to the season the window falls in."},
    "planting_id": {"type": "string"},
    "bed_id": {"type": "string"},
    "notes_md": {"type": "string"}
  },
  "required": ["title_cs", "window_from", "window_to"],
  "additionalProperties": false
}`),
		},
		{
			Name:        toolTaskComplete,
			Title:       "Tick off a garden job",
			Description: "Marks one garden job done; reopening one is deliberately not available here, because undoing somebody else's record of work they did is not an assistant's call.",
			InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {"id": {"type": "string"}},
  "required": ["id"],
  "additionalProperties": false
}`),
		},
		{
			Name:        toolPlan,
			Title:       "Season plan",
			Description: "Returns one season's plantings — what is in which bed, with planned and actual dates — plus the season's frost anchors and status; defaults to the current year.",
			InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "year": {"type": "integer", "description": "Defaults to the current year."},
    "bed_id": {"type": "string"},
    "limit": {"type": "integer", "minimum": 1}
  },
  "additionalProperties": false
}`),
			ReadOnly: true,
		},
		{
			Name:        toolPlantingCreate,
			Title:       "Plant something",
			Description: "Records a planting of one crop in one bed for a season, which is what generates its sowing, transplanting and harvest jobs; call home_garden_plan first for the bed and crop ids.",
			InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "plant_id": {"type": "string", "description": "The crop, from the knowledge base."},
    "variety_id": {"type": "string"},
    "bed_id": {"type": "string"},
    "location_label": {"type": "string", "description": "For a planting that is not in a numbered bed."},
    "season_year": {"type": "integer", "description": "Defaults to the current year."},
    "area_m2": {"type": "number", "minimum": 0},
    "plant_count": {"type": "integer", "minimum": 0},
    "notes_md": {"type": "string"}
  },
  "required": ["plant_id"],
  "additionalProperties": false
}`),
		},
		{
			Name:        toolHarvestLog,
			Title:       "Record a harvest",
			Description: "Records what was picked from one planting, on a date, in a unit — the quantity must be strictly positive, because a zero row still flips the planting to harvesting and makes the real figure un-enterable later.",
			InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "planting_id": {"type": "string"},
    "quantity": {"type": "number", "exclusiveMinimum": 0},
    "unit": {"type": "string", "description": "e.g. \"kg\" or \"ks\"."},
    "harvested_on": {"type": "string", "description": "ISO date (YYYY-MM-DD). Defaults to today."},
    "destination": {"type": "string"},
    "quality": {"type": "string"},
    "note": {"type": "string"}
  },
  "required": ["planting_id", "quantity"],
  "additionalProperties": false
}`),
		},
	}
}

func (p *mcpProvider) Call(ctx context.Context, name string, args json.RawMessage) (mcp.Result, error) {
	switch name {
	case toolTasks:
		return p.tasks(ctx, args)
	case toolTaskCreate:
		return p.taskCreate(ctx, args)
	case toolTaskComplete:
		return p.taskComplete(ctx, args)
	case toolPlan:
		return p.plan(ctx, args)
	case toolPlantingCreate:
		return p.plantingCreate(ctx, args)
	case toolHarvestLog:
		return p.harvestLog(ctx, args)
	default:
		return mcp.Result{}, mcp.UnknownToolError(name)
	}
}

type tasksArgs struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Status string `json:"status"`
	BedID  string `json:"bed_id"`
	Limit  int    `json:"limit"`
}

func (p *mcpProvider) tasks(ctx context.Context, args json.RawMessage) (mcp.Result, error) {
	var in tasksArgs
	if err := mcp.DecodeArgs(args, &in); err != nil {
		return mcp.Result{}, err
	}
	from := in.From
	if from == "" {
		from = p.svc.today().String()
	} else if err := validGardenDate(from, "from"); err != nil {
		return mcp.Result{}, err
	}
	to := in.To
	if to == "" {
		start, err := time.Parse("2006-01-02", from)
		if err != nil {
			return mcp.Result{}, httpx.ErrUnprocessable("from musí být ve tvaru RRRR-MM-DD.")
		}
		to = start.AddDate(0, 0, 14).Format("2006-01-02")
	} else if err := validGardenDate(to, "to"); err != nil {
		return mcp.Result{}, err
	}
	page, err := p.svc.ListTasks(ctx, TaskFilter{
		From: from, To: to, Status: in.Status, BedID: in.BedID,
	}, in.Limit, "")
	if err != nil {
		return mcp.Result{}, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Práce %s – %s (%d):\n", from, to, len(page.Items))
	for _, t := range page.Items {
		fmt.Fprintf(&b, "\n• %s (id %s) — %s až %s", t.TitleCS, t.ID, t.WindowFrom, t.WindowTo)
		if t.BedCode != nil {
			fmt.Fprintf(&b, ", záhon %s", *t.BedCode)
		}
		if t.PlantName != nil {
			fmt.Fprintf(&b, ", %s", *t.PlantName)
		}
		if t.Overdue {
			b.WriteString(" [po termínu]")
		}
		if t.Status != TaskOpen {
			fmt.Fprintf(&b, " [%s]", t.Status)
		}
	}
	if len(page.Items) == 0 {
		b.WriteString("\n(nic v tomto okně)")
	}
	return mcp.TextResult(b.String(), page)
}

type taskCreateArgs struct {
	TitleCS    string  `json:"title_cs"`
	WindowFrom string  `json:"window_from"`
	WindowTo   string  `json:"window_to"`
	Kind       string  `json:"kind"`
	SeasonYear *int    `json:"season_year"`
	PlantingID *string `json:"planting_id"`
	BedID      *string `json:"bed_id"`
	NotesMD    *string `json:"notes_md"`
}

// taskCreate validates before the service (D311).
func (p *mcpProvider) taskCreate(ctx context.Context, args json.RawMessage) (mcp.Result, error) {
	var in taskCreateArgs
	if err := mcp.DecodeArgs(args, &in); err != nil {
		return mcp.Result{}, err
	}
	title := strings.TrimSpace(in.TitleCS)
	if title == "" {
		return mcp.Result{}, httpx.ErrUnprocessable("Název práce nesmí být prázdný.")
	}
	if err := validGardenDate(in.WindowFrom, "window_from"); err != nil {
		return mcp.Result{}, err
	}
	if err := validGardenDate(in.WindowTo, "window_to"); err != nil {
		return mcp.Result{}, err
	}
	if in.WindowTo < in.WindowFrom {
		return mcp.Result{}, httpx.ErrUnprocessable("window_to nesmí být dřív než window_from.")
	}
	year := in.SeasonYear
	if year == nil {
		y, err := time.Parse("2006-01-02", in.WindowFrom)
		if err != nil {
			return mcp.Result{}, httpx.ErrUnprocessable("window_from musí být ve tvaru RRRR-MM-DD.")
		}
		v := y.Year()
		year = &v
	}
	kind := in.Kind
	input := TaskInput{
		TitleCS: &title, WindowFrom: &in.WindowFrom, WindowTo: &in.WindowTo,
		PlantingID: in.PlantingID, BedID: in.BedID, NotesMD: in.NotesMD, SeasonYear: year,
	}
	if kind != "" {
		input.Kind = &kind
	}
	t, err := p.svc.CreateTask(ctx, input)
	if err != nil {
		return mcp.Result{}, err
	}
	return mcp.TextResult(fmt.Sprintf("Práce „%s“ přidána na %s–%s (id %s).",
		t.TitleCS, t.WindowFrom, t.WindowTo, t.ID), t)
}

type idArgs struct {
	ID string `json:"id"`
}

func (p *mcpProvider) taskComplete(ctx context.Context, args json.RawMessage) (mcp.Result, error) {
	var in idArgs
	if err := mcp.DecodeArgs(args, &in); err != nil {
		return mcp.Result{}, err
	}
	if strings.TrimSpace(in.ID) == "" {
		return mcp.Result{}, httpx.ErrUnprocessable("Chybí id práce.")
	}
	t, err := p.svc.CompleteTask(ctx, in.ID, nil)
	if err != nil {
		return mcp.Result{}, err
	}
	return mcp.TextResult(fmt.Sprintf("Práce „%s“ hotová.", t.TitleCS), t)
}

type planArgs struct {
	Year  *int   `json:"year"`
	BedID string `json:"bed_id"`
	Limit int    `json:"limit"`
}

func (p *mcpProvider) plan(ctx context.Context, args json.RawMessage) (mcp.Result, error) {
	var in planArgs
	if err := mcp.DecodeArgs(args, &in); err != nil {
		return mcp.Result{}, err
	}
	year := in.Year
	if year == nil {
		y := p.svc.today().Y
		year = &y
	}
	page, err := p.svc.ListPlantings(ctx, PlantingFilter{Year: year, BedID: in.BedID}, in.Limit, "")
	if err != nil {
		return mcp.Result{}, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Plán %d (%d výsadeb):\n", *year, len(page.Items))
	// The season's own row carries the frost anchors every planned date is
	// derived from — a plan without them is a list of dates nobody can check.
	if season, err := p.svc.GetSeason(ctx, *year); err == nil {
		fmt.Fprintf(&b, "Sezóna %d: %s", season.Year, season.Status)
		if season.LastFrostOn != nil {
			fmt.Fprintf(&b, ", poslední mráz %s", *season.LastFrostOn)
		}
		if season.FirstFrostOn != nil {
			fmt.Fprintf(&b, ", první mráz %s", *season.FirstFrostOn)
		}
		b.WriteString("\n")
	}
	for _, pl := range page.Items {
		fmt.Fprintf(&b, "\n• %s", pl.PlantName)
		if pl.VarietyName != nil {
			fmt.Fprintf(&b, " (%s)", *pl.VarietyName)
		}
		if pl.BedCode != nil {
			fmt.Fprintf(&b, " — záhon %s", *pl.BedCode)
		} else if pl.LocationLabel != nil {
			fmt.Fprintf(&b, " — %s", *pl.LocationLabel)
		}
		fmt.Fprintf(&b, " (id %s)", pl.ID)
	}
	return mcp.TextResult(b.String(), page)
}

type plantingCreateArgs struct {
	PlantID       string   `json:"plant_id"`
	VarietyID     *string  `json:"variety_id"`
	BedID         *string  `json:"bed_id"`
	LocationLabel *string  `json:"location_label"`
	SeasonYear    *int     `json:"season_year"`
	AreaM2        *float64 `json:"area_m2"`
	PlantCount    *int     `json:"plant_count"`
	NotesMD       *string  `json:"notes_md"`
}

func (p *mcpProvider) plantingCreate(ctx context.Context, args json.RawMessage) (mcp.Result, error) {
	var in plantingCreateArgs
	if err := mcp.DecodeArgs(args, &in); err != nil {
		return mcp.Result{}, err
	}
	if strings.TrimSpace(in.PlantID) == "" {
		return mcp.Result{}, httpx.ErrUnprocessable("Plodina je povinná.")
	}
	if in.AreaM2 != nil && *in.AreaM2 < 0 {
		return mcp.Result{}, httpx.ErrUnprocessable("Plocha nesmí být záporná.")
	}
	if in.PlantCount != nil && *in.PlantCount < 0 {
		return mcp.Result{}, httpx.ErrUnprocessable("Počet rostlin nesmí být záporný.")
	}
	year := in.SeasonYear
	if year == nil {
		y := p.svc.today().Y
		year = &y
	}
	pl, err := p.svc.CreatePlanting(ctx, PlantingInput{
		PlantID: &in.PlantID, VarietyID: in.VarietyID, BedID: in.BedID,
		LocationLabel: in.LocationLabel, AreaM2: in.AreaM2, PlantCount: in.PlantCount,
		NotesMD: in.NotesMD, SeasonYear: year,
	})
	if err != nil {
		return mcp.Result{}, err
	}
	return mcp.TextResult(fmt.Sprintf("Výsadba %s zapsána (id %s).", pl.PlantName, pl.ID), pl)
}

type harvestArgs struct {
	PlantingID  string   `json:"planting_id"`
	Quantity    *float64 `json:"quantity"`
	Unit        string   `json:"unit"`
	HarvestedOn string   `json:"harvested_on"`
	Destination *string  `json:"destination"`
	Quality     *string  `json:"quality"`
	Note        *string  `json:"note"`
}

func (p *mcpProvider) harvestLog(ctx context.Context, args json.RawMessage) (mcp.Result, error) {
	var in harvestArgs
	if err := mcp.DecodeArgs(args, &in); err != nil {
		return mcp.Result{}, err
	}
	if strings.TrimSpace(in.PlantingID) == "" {
		return mcp.Result{}, httpx.ErrUnprocessable("Výsadba je povinná.")
	}
	// ⚠ STRICTLY POSITIVE, which is what the service says too and what the
	// description explains: a zero row still stamps first_harvest_on, flips the
	// planting to `harvesting` and makes yield_actual non-null, so the season
	// review reads "už zapsáno" and never offers the box for the real figure.
	if in.Quantity == nil || *in.Quantity <= 0 {
		return mcp.Result{}, httpx.ErrUnprocessable("Množství musí být kladné.")
	}
	if in.HarvestedOn != "" {
		if err := validGardenDate(in.HarvestedOn, "harvested_on"); err != nil {
			return mcp.Result{}, err
		}
	}
	input := HarvestInput{
		PlantingID: &in.PlantingID, Quantity: in.Quantity,
		Destination: in.Destination, Quality: in.Quality, Note: in.Note,
	}
	if in.Unit != "" {
		input.Unit = &in.Unit
	}
	if in.HarvestedOn != "" {
		input.HarvestedOn = &in.HarvestedOn
	}
	h, err := p.svc.CreateHarvest(ctx, input)
	if err != nil {
		return mcp.Result{}, err
	}
	return mcp.TextResult(fmt.Sprintf("Sklizeň %s: %g %s (id %s).",
		h.HarvestedOn, h.Quantity, h.Unit, h.ID), h)
}

// Search reads the crop knowledge base over `garden_plants_fts`.
//
// ⚠ D302: an actor-less ctx is an ERROR, never an empty slice.
func (p *mcpProvider) Search(ctx context.Context, q mcp.Query) ([]mcp.Hit, error) {
	if _, ok := reqctx.ActorFrom(ctx); !ok {
		return nil, fmt.Errorf("garden: search without an actor")
	}
	page, err := p.svc.ListPlants(ctx, PlantFilter{Query: q.Text}, q.Limit, "")
	if err != nil {
		return nil, err
	}
	hits := make([]mcp.Hit, 0, len(page.Items))
	for _, pl := range page.Items {
		updated, _ := time.Parse(tsFormat, pl.UpdatedAt)
		hits = append(hits, mcp.Hit{
			Kind:      "garden.plant",
			ID:        pl.ID,
			Title:     pl.NameCS,
			Snippet:   pl.Family,
			UpdatedAt: updated,
			ExactHit:  strings.EqualFold(strings.TrimSpace(pl.NameCS), strings.TrimSpace(q.Text)),
		})
	}
	return hits, nil
}

// Get answers home_get for this module's kinds.
func (p *mcpProvider) Get(ctx context.Context, kind, id string) (mcp.Result, error) {
	switch kind {
	case "garden.plant":
		pl, err := p.svc.GetPlant(ctx, id)
		if err != nil {
			if mcp.IsNotFound(err) {
				return mcp.NotFoundResult(), nil
			}
			return mcp.Result{}, err
		}
		return mcp.TextResult(fmt.Sprintf("%s (%s, id %s)", pl.NameCS, pl.Family, pl.ID), pl)
	case "garden.task":
		t, err := p.svc.GetTask(ctx, id)
		if err != nil {
			if mcp.IsNotFound(err) {
				return mcp.NotFoundResult(), nil
			}
			return mcp.Result{}, err
		}
		return mcp.TextResult(fmt.Sprintf("%s — %s až %s (id %s)", t.TitleCS, t.WindowFrom, t.WindowTo, t.ID), t)
	default:
		return mcp.NotFoundResult(), nil
	}
}

func validGardenDate(s, field string) error {
	if strings.TrimSpace(s) == "" {
		return httpx.ErrUnprocessable("Chybí " + field + ".")
	}
	if _, err := time.Parse("2006-01-02", s); err != nil {
		return httpx.ErrUnprocessable(field + " musí být ve tvaru RRRR-MM-DD.")
	}
	return nil
}
