package garden

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/dates"
	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/db"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/idgen"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/lexorank"
)

// The work half of the service: tasks, harvest, storage, rules and settings.

// ================================ tasks ================================

type TaskPage struct {
	Items      []Task  `json:"items"`
	NextCursor *string `json:"next_cursor"`
}

func (s *Service) ListTasks(ctx context.Context, f TaskFilter, limit int, cursor string) (TaskPage, error) {
	items, next, err := s.store.ListTasks(ctx, nil, f, NormalizeLimit(limit), cursor)
	if err != nil {
		return TaskPage{}, err
	}
	today := s.today()
	for i := range items {
		items[i].Overdue = isOverdue(items[i], today)
	}
	if items == nil {
		items = []Task{}
	}
	return TaskPage{Items: items, NextCursor: next}, nil
}

// isOverdue is derived, never stored: open, and the window has passed.
func isOverdue(t Task, today dates.Date) bool {
	if t.Status != TaskOpen {
		return false
	}
	to, err := dates.Parse(t.WindowTo)
	if err != nil {
		return false
	}
	return to.Before(today)
}

func (s *Service) GetTask(ctx context.Context, id string) (Task, error) {
	t, err := s.store.GetTask(ctx, nil, id)
	if err != nil {
		return Task{}, mapNotFound(err)
	}
	t.Overdue = isOverdue(t, s.today())
	return t, nil
}

// TaskInput is the create/update body for MANUAL tasks — and for editing a
// generated one, which is what sets is_edited.
type TaskInput struct {
	Kind       *string `json:"kind"`
	TitleCS    *string `json:"title_cs"`
	WindowFrom *string `json:"window_from"`
	WindowTo   *string `json:"window_to"`
	DueHint    *string `json:"due_hint"`
	Status     *string `json:"status"`
	SeasonID   *string `json:"season_id"`
	PlantingID *string `json:"planting_id"`
	BedID      *string `json:"bed_id"`
	NotesMD    *string `json:"notes_md"`
	SeasonYear *int    `json:"season_year"`
}

func (s *Service) CreateTask(ctx context.Context, in TaskInput) (Task, error) {
	t := Task{
		Kind: derefS(in.Kind), TitleCS: strings.TrimSpace(derefS(in.TitleCS)),
		WindowFrom: derefS(in.WindowFrom), WindowTo: derefS(in.WindowTo),
		DueHint: in.DueHint, Status: TaskOpen, PlantingID: emptyToNil(in.PlantingID),
		BedID: emptyToNil(in.BedID), NotesMD: in.NotesMD,
	}
	if err := validateTask(t); err != nil {
		return Task{}, err
	}
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		seasonID, _, err := s.resolveSeason(ctx, tx, in.SeasonID, in.SeasonYear)
		if err != nil {
			return err
		}
		t.SeasonID = seasonID
		if err := s.requireOpenSeason(ctx, tx, t.SeasonID); err != nil {
			return err
		}
		t.ID = idgen.New()
		t.Position = lexorank.First()
		if err := s.store.InsertTask(ctx, tx, t, actorID(ctx), nowUTC()); err != nil {
			return err
		}
		return s.record(ctx, tx, "task.create", "garden_task", t.ID, "Přidána práce: "+t.TitleCS, nil, nil)
	})
	if err != nil {
		return Task{}, err
	}
	s.notify(ctx, "garden_task.changed", map[string]any{"id": t.ID})
	return s.GetTask(ctx, t.ID)
}

func validateTask(t Task) error {
	if t.TitleCS == "" {
		return errUnprocessable("Název práce je povinný.")
	}
	if !Valid(EnumTaskKind, t.Kind) {
		return errUnprocessable("Neznámý druh práce.")
	}
	from, err := dates.Parse(t.WindowFrom)
	if err != nil {
		return errUnprocessable("Začátek termínu musí být ve tvaru RRRR-MM-DD.")
	}
	to, err := dates.Parse(t.WindowTo)
	if err != nil {
		return errUnprocessable("Konec termínu musí být ve tvaru RRRR-MM-DD.")
	}
	if to.Before(from) {
		return errUnprocessable("Konec termínu nemůže být dřív než začátek.")
	}
	return nil
}

// UpdateTask edits a task. Editing a GENERATED task's content sets
// is_edited = true, permanently exempting it from regeneration (D110) — the
// user has taken this one over, and the generator does not argue. A pure
// status change (the calendar skips via PATCH) is not an edit: it says what
// happened to the task, not what the task is, so skip→reopen stays as
// re-generable as done→reopen through the dedicated endpoints.
func (s *Service) UpdateTask(ctx context.Context, id string, in TaskInput) (Task, error) {
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		before, err := s.store.GetTask(ctx, tx, id)
		if err != nil {
			return mapNotFound(err)
		}
		if err := s.requireOpenSeason(ctx, tx, before.SeasonID); err != nil {
			return err
		}
		after := before
		if in.Kind != nil {
			after.Kind = *in.Kind
		}
		if in.TitleCS != nil {
			after.TitleCS = strings.TrimSpace(*in.TitleCS)
		}
		if in.WindowFrom != nil {
			after.WindowFrom = *in.WindowFrom
		}
		if in.WindowTo != nil {
			after.WindowTo = *in.WindowTo
		}
		if in.DueHint != nil {
			after.DueHint = emptyToNil(in.DueHint)
		}
		if in.NotesMD != nil {
			after.NotesMD = in.NotesMD
		}
		if in.Status != nil {
			if !Valid(EnumTaskStatus, *in.Status) {
				return errUnprocessable("Neznámý stav práce.")
			}
			after.Status = *in.Status
			if after.Status == TaskDone && after.CompletedAt == nil {
				after.CompletedBy, after.CompletedAt = sp(actorID(ctx)), sp(nowUTC())
			}
			if after.Status == TaskOpen {
				after.CompletedBy, after.CompletedAt = nil, nil
			}
		}
		if err := validateTask(after); err != nil {
			return err
		}
		contentChanged := in.Kind != nil || in.TitleCS != nil || in.WindowFrom != nil ||
			in.WindowTo != nil || in.DueHint != nil || in.NotesMD != nil
		if after.IsGenerated && contentChanged {
			after.IsEdited = true
		}
		if err := s.store.UpdateTask(ctx, tx, after, nowUTC()); err != nil {
			return err
		}
		var changes []audit.Change
		diffPlain(&changes, "title_cs", before.TitleCS, after.TitleCS)
		diffPlain(&changes, "window_from", before.WindowFrom, after.WindowFrom)
		diffPlain(&changes, "window_to", before.WindowTo, after.WindowTo)
		diffPlain(&changes, "status", before.Status, after.Status)
		return s.record(ctx, tx, "task.update", "garden_task", id, "Upravena práce: "+after.TitleCS, changes, nil)
	})
	if err != nil {
		return Task{}, err
	}
	s.notify(ctx, "garden_task.changed", map[string]any{"id": id})
	return s.GetTask(ctx, id)
}

// CompleteTask ticks a job off. IDEMPOTENT (D131), mirroring events' completion:
// completing an already-complete task is a 200, not a 409, because the
// dashboard's 2000 ms hold can fire twice on a bad connection and the second
// send must not look like an error.
func (s *Service) CompleteTask(ctx context.Context, id string, meta map[string]any) (Task, error) {
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		t, err := s.store.GetTask(ctx, tx, id)
		if err != nil {
			return mapNotFound(err)
		}
		if err := s.requireOpenSeason(ctx, tx, t.SeasonID); err != nil {
			return err
		}
		if t.Status == TaskDone {
			return nil // already done; say so with a 200 and write nothing
		}
		t.Status = TaskDone
		t.CompletedBy, t.CompletedAt = sp(actorID(ctx)), sp(nowUTC())
		if err := s.store.UpdateTask(ctx, tx, t, nowUTC()); err != nil {
			return err
		}
		return s.record(ctx, tx, "task.update", "garden_task", id, "Hotovo: "+t.TitleCS, nil, meta)
	})
	if err != nil {
		return Task{}, err
	}
	s.notify(ctx, "garden_task.changed", map[string]any{"id": id})
	return s.GetTask(ctx, id)
}

// ReopenTask is completion's inverse and equally idempotent.
func (s *Service) ReopenTask(ctx context.Context, id string) (Task, error) {
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		t, err := s.store.GetTask(ctx, tx, id)
		if err != nil {
			return mapNotFound(err)
		}
		if err := s.requireOpenSeason(ctx, tx, t.SeasonID); err != nil {
			return err
		}
		if t.Status == TaskOpen {
			return nil
		}
		t.Status, t.CompletedBy, t.CompletedAt = TaskOpen, nil, nil
		if err := s.store.UpdateTask(ctx, tx, t, nowUTC()); err != nil {
			return err
		}
		return s.record(ctx, tx, "task.update", "garden_task", id, "Znovu otevřeno: "+t.TitleCS, nil, nil)
	})
	if err != nil {
		return Task{}, err
	}
	s.notify(ctx, "garden_task.changed", map[string]any{"id": id})
	return s.GetTask(ctx, id)
}

// DeleteTask removes a task. A GENERATED one leaves a TOMBSTONE rather than
// vanishing: the row stays with suppressed = 1 so its generation key remains
// taken and the next recompute cannot resurrect it (D110). A calendar that
// re-adds work you threw away is one you stop trusting.
func (s *Service) DeleteTask(ctx context.Context, id string) error {
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		t, err := s.store.GetTask(ctx, tx, id)
		if err != nil {
			return mapNotFound(err)
		}
		if err := s.requireOpenSeason(ctx, tx, t.SeasonID); err != nil {
			return err
		}
		at := nowUTC()
		if t.IsGenerated {
			if err := s.store.SuppressTask(ctx, tx, id, at); err != nil {
				return err
			}
		} else if err := s.store.SoftDeleteTask(ctx, tx, id, at); err != nil {
			return err
		}
		return s.record(ctx, tx, "task.delete", "garden_task", id, "Smazána práce: "+t.TitleCS, nil, nil)
	})
	if err != nil {
		return err
	}
	s.notify(ctx, "garden_task.changed", map[string]any{"id": id})
	return nil
}

// =============================== harvests ===============================

type HarvestPage struct {
	Items      []Harvest `json:"items"`
	NextCursor *string   `json:"next_cursor"`
}

func (s *Service) ListHarvests(ctx context.Context, plantingID string, year *int, limit int, cursor string) (HarvestPage, error) {
	items, next, err := s.store.ListHarvests(ctx, plantingID, year, NormalizeLimit(limit), cursor)
	if err != nil {
		return HarvestPage{}, err
	}
	if items == nil {
		items = []Harvest{}
	}
	return HarvestPage{Items: items, NextCursor: next}, nil
}

// HarvestInput is the create/update body.
type HarvestInput struct {
	PlantingID  *string  `json:"planting_id"`
	HarvestedOn *string  `json:"harvested_on"`
	Quantity    *float64 `json:"quantity"`
	Unit        *string  `json:"unit"`
	Destination *string  `json:"destination"`
	Quality     *string  `json:"quality"`
	Note        *string  `json:"note"`
}

func (s *Service) CreateHarvest(ctx context.Context, in HarvestInput) (Harvest, error) {
	if in.PlantingID == nil || *in.PlantingID == "" {
		return Harvest{}, errUnprocessable("Výsadba je povinná.")
	}
	// STRICTLY POSITIVE, which is what the message has always said. A zero row is
	// not a harvest: it still stamps first_harvest_on and flips the planting to
	// `harvesting`, and it makes yield_actual non-null — so the season-close
	// review reads "už zapsáno" and never offers the box for the real figure,
	// which then cannot be entered at all once the season is frozen.
	if in.Quantity == nil || *in.Quantity <= 0 {
		return Harvest{}, errUnprocessable("Množství musí být kladné.")
	}
	h := Harvest{
		ID: idgen.New(), PlantingID: *in.PlantingID, Quantity: *in.Quantity,
		Destination: in.Destination, Quality: in.Quality, Note: in.Note,
	}
	h.HarvestedOn = derefS(firstStr(in.HarvestedOn, sp(s.today().String())))
	if _, err := dates.Parse(h.HarvestedOn); err != nil {
		return Harvest{}, errUnprocessable("Datum sklizně musí být ve tvaru RRRR-MM-DD.")
	}
	if h.Destination != nil && !Valid(EnumDestination, *h.Destination) {
		return Harvest{}, errUnprocessable("Neznámé určení sklizně.")
	}

	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		p, err := s.store.GetPlanting(ctx, tx, h.PlantingID)
		if err != nil {
			return mapNotFound(err)
		}
		if err := s.requireOpenSeason(ctx, tx, p.SeasonID); err != nil {
			return err
		}
		eff, err := s.store.EffectiveFor(ctx, tx, p.PlantID, p.VarietyID)
		if err != nil {
			return err
		}
		// The unit defaults from the crop, so the quick-entry path really is two
		// taps and a number.
		h.Unit = eff.Unit()
		if in.Unit != nil && *in.Unit != "" {
			if !Valid(EnumHarvestUnit, *in.Unit) {
				return errUnprocessable("Neznámá jednotka sklizně.")
			}
			h.Unit = *in.Unit
		}
		// A harvest before the crop went in the ground is a typo, not a record.
		if from, _ := p.OccupancyWindow(); from != nil {
			if d, err := dates.Parse(h.HarvestedOn); err == nil && d.Before(*from) {
				return errUnprocessable("Datum sklizně je dřív, než byla plodina zaseta.")
			}
		}
		now := nowUTC()
		h.CreatedAt, h.CreatedBy = now, sp(actorID(ctx))
		if err := s.store.InsertHarvest(ctx, tx, h, actorID(ctx), now); err != nil {
			return err
		}
		// The first harvest is also an actual date — recorded once, here, so the
		// drift line does not need the household to type it twice.
		if p.FirstHarvestOn == nil {
			p.FirstHarvestOn = sp(h.HarvestedOn)
			p.Status = PlantingHarvesting
			p.UpdatedAt = now
			if err := s.store.UpdatePlanting(ctx, tx, p); err != nil {
				return err
			}
		}
		return s.record(ctx, tx, "harvest.create", "garden_harvest", h.ID,
			"Sklizeno: "+p.PlantName+" "+fmtQty(h.Quantity)+" "+h.Unit, nil, nil)
	})
	if err != nil {
		return Harvest{}, err
	}
	s.notify(ctx, "garden_harvest.changed", map[string]any{"id": h.ID})
	return s.store.GetHarvest(ctx, nil, h.ID)
}

// requireOpenSeasonOfHarvest applies the closed-season guard to a harvest, which
// reaches its season through its planting.
//
// A harvest is season history as much as the planting is: the closed season's
// yield_kg, the season total and the planting's yield_actual all read these
// rows, so editing or deleting one after the close would rewrite the record C3
// and C8 depend on — past the admin-only reopen gate that exists to protect it.
// CreateHarvest has always taken this guard; update and delete take it too.
func (s *Service) requireOpenSeasonOfHarvest(ctx context.Context, tx DBTX, plantingID string) error {
	p, err := s.store.GetPlanting(ctx, tx, plantingID)
	if err != nil {
		return mapNotFound(err)
	}
	return s.requireOpenSeason(ctx, tx, p.SeasonID)
}

func (s *Service) UpdateHarvest(ctx context.Context, id string, in HarvestInput) (Harvest, error) {
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		before, err := s.store.GetHarvest(ctx, tx, id)
		if err != nil {
			return mapNotFound(err)
		}
		if err := s.requireOpenSeasonOfHarvest(ctx, tx, before.PlantingID); err != nil {
			return err
		}
		after := before
		if in.HarvestedOn != nil {
			// The same guard CreateHarvest applies: a malformed date would persist
			// with a 200 and silently fall out of every date comparison.
			if _, err := dates.Parse(*in.HarvestedOn); err != nil {
				return errUnprocessable("Datum sklizně musí být ve tvaru RRRR-MM-DD.")
			}
			after.HarvestedOn = *in.HarvestedOn
		}
		if in.Quantity != nil {
			if *in.Quantity < 0 {
				return errUnprocessable("Množství nemůže být záporné.")
			}
			after.Quantity = *in.Quantity
		}
		if in.Unit != nil && *in.Unit != "" {
			if !Valid(EnumHarvestUnit, *in.Unit) {
				return errUnprocessable("Neznámá jednotka sklizně.")
			}
			after.Unit = *in.Unit
		}
		if in.Destination != nil {
			after.Destination = emptyToNil(in.Destination)
		}
		if in.Quality != nil {
			after.Quality = emptyToNil(in.Quality)
		}
		if in.Note != nil {
			after.Note = emptyToNil(in.Note)
		}
		if err := s.store.UpdateHarvest(ctx, tx, after, nowUTC()); err != nil {
			return err
		}
		var changes []audit.Change
		diffPlain(&changes, "harvested_on", before.HarvestedOn, after.HarvestedOn)
		diffFloat(&changes, "quantity", &before.Quantity, &after.Quantity)
		diffPlain(&changes, "unit", before.Unit, after.Unit)
		return s.record(ctx, tx, "harvest.update", "garden_harvest", id, "Upravena sklizeň", changes, nil)
	})
	if err != nil {
		return Harvest{}, err
	}
	s.notify(ctx, "garden_harvest.changed", map[string]any{"id": id})
	return s.store.GetHarvest(ctx, nil, id)
}

func (s *Service) DeleteHarvest(ctx context.Context, id string) error {
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		before, err := s.store.GetHarvest(ctx, tx, id)
		if err != nil {
			return mapNotFound(err)
		}
		if err := s.requireOpenSeasonOfHarvest(ctx, tx, before.PlantingID); err != nil {
			return err
		}
		at := nowUTC()
		if err := s.store.SoftDeleteHarvest(ctx, tx, id, at); err != nil {
			return err
		}
		return s.record(ctx, tx, "harvest.delete", "garden_harvest", id,
			"Smazána sklizeň "+fmtQty(before.Quantity)+" "+before.Unit, nil, nil)
	})
	if err != nil {
		return err
	}
	s.notify(ctx, "garden_harvest.changed", map[string]any{"id": id})
	return nil
}

// ================================ storage ================================

type StoragePage struct {
	Items      []StorageItem `json:"items"`
	NextCursor *string       `json:"next_cursor"`
}

func (s *Service) ListStorage(ctx context.Context, status string, limit int, cursor string) (StoragePage, error) {
	items, next, err := s.store.ListStorage(ctx, status, NormalizeLimit(limit), cursor)
	if err != nil {
		return StoragePage{}, err
	}
	if items == nil {
		items = []StorageItem{}
	}
	return StoragePage{Items: items, NextCursor: next}, nil
}

// StorageInput is the create/update body.
type StorageInput struct {
	HarvestID         *string  `json:"harvest_id"`
	PlantingID        *string  `json:"planting_id"`
	ProductName       *string  `json:"product_name"`
	Method            *string  `json:"method"`
	Location          *string  `json:"location"`
	QuantityInitial   *float64 `json:"quantity_initial"`
	QuantityRemaining *float64 `json:"quantity_remaining"`
	Unit              *string  `json:"unit"`
	StoredOn          *string  `json:"stored_on"`
	BestBefore        *string  `json:"best_before"`
	Status            *string  `json:"status"`
	Note              *string  `json:"note"`
}

func (s *Service) CreateStorage(ctx context.Context, in StorageInput) (StorageItem, error) {
	it := StorageItem{
		ID: idgen.New(), HarvestID: emptyToNil(in.HarvestID), PlantingID: emptyToNil(in.PlantingID),
		ProductName: strings.TrimSpace(derefS(in.ProductName)), Method: derefS(in.Method),
		Location: in.Location, Unit: derefS(in.Unit), Note: in.Note, Status: StorageStored,
		BestBefore: emptyToNil(in.BestBefore),
	}
	if in.QuantityInitial != nil {
		it.QuantityInitial = *in.QuantityInitial
	}
	it.QuantityRemaining = it.QuantityInitial
	it.StoredOn = derefS(firstStr(in.StoredOn, sp(s.today().String())))
	if err := validateStorage(it); err != nil {
		return StorageItem{}, err
	}

	now := nowUTC()
	it.CreatedAt, it.UpdatedAt = now, now
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		if err := s.store.InsertStorage(ctx, tx, it, actorID(ctx), now); err != nil {
			return err
		}
		return s.record(ctx, tx, "storage.create", "garden_storage_item", it.ID,
			"Uskladněno: "+it.ProductName+" "+fmtQty(it.QuantityInitial)+" "+it.Unit, nil, nil)
	})
	if err != nil {
		return StorageItem{}, err
	}
	s.notify(ctx, "garden_storage.changed", map[string]any{"id": it.ID})
	return it, nil
}

func validateStorage(it StorageItem) error {
	if it.ProductName == "" {
		return errUnprocessable("Název produktu je povinný.")
	}
	if !Valid(EnumStorageMethod, it.Method) {
		return errUnprocessable("Způsob uskladnění je povinný.")
	}
	if !Valid(EnumHarvestUnit, it.Unit) {
		return errUnprocessable("Jednotka je povinná.")
	}
	if it.QuantityInitial < 0 {
		return errUnprocessable("Množství nemůže být záporné.")
	}
	if _, err := dates.Parse(it.StoredOn); err != nil {
		return errUnprocessable("Datum uskladnění musí být ve tvaru RRRR-MM-DD.")
	}
	if it.BestBefore != nil {
		if _, err := dates.Parse(*it.BestBefore); err != nil {
			return errUnprocessable("Datum spotřeby musí být ve tvaru RRRR-MM-DD.")
		}
	}
	return nil
}

// UpdateStorage records consumption by editing quantity_remaining IN PLACE
// (D121). There is no movements table: the audit spine's field diffs already
// answer "when did we eat the last jar", and a second table would buy only a
// chart.
func (s *Service) UpdateStorage(ctx context.Context, id string, in StorageInput) (StorageItem, error) {
	var out StorageItem
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		before, err := s.store.GetStorage(ctx, tx, id)
		if err != nil {
			return mapNotFound(err)
		}
		after := before
		if in.ProductName != nil {
			after.ProductName = strings.TrimSpace(*in.ProductName)
		}
		if in.Method != nil {
			after.Method = *in.Method
		}
		if in.Location != nil {
			after.Location = emptyToNil(in.Location)
		}
		if in.BestBefore != nil {
			after.BestBefore = emptyToNil(in.BestBefore)
		}
		if in.Note != nil {
			after.Note = emptyToNil(in.Note)
		}
		if in.QuantityRemaining != nil {
			if *in.QuantityRemaining < 0 {
				return errUnprocessable("Zbývající množství nemůže být záporné.")
			}
			if *in.QuantityRemaining > after.QuantityInitial {
				return errUnprocessable("Zbývající množství nemůže být větší než uskladněné.")
			}
			after.QuantityRemaining = *in.QuantityRemaining
		}
		if in.Status != nil {
			if !Valid(EnumStorageStatus, *in.Status) {
				return errUnprocessable("Neznámý stav zásoby.")
			}
			after.Status = *in.Status
		}
		// Reaching zero means it is eaten — recorded automatically, because
		// asking someone to also change a dropdown is how the list goes stale.
		if after.QuantityRemaining == 0 && after.Status == StorageStored {
			after.Status = StorageConsumed
		}
		if err := validateStorage(after); err != nil {
			return err
		}
		if err := s.store.UpdateStorage(ctx, tx, after, nowUTC()); err != nil {
			return err
		}
		out = after
		var changes []audit.Change
		diffFloat(&changes, "quantity_remaining", &before.QuantityRemaining, &after.QuantityRemaining)
		diffPlain(&changes, "status", before.Status, after.Status)
		return s.record(ctx, tx, "storage.update", "garden_storage_item", id,
			"Upraveno ve skladu: "+after.ProductName, changes, nil)
	})
	if err != nil {
		return StorageItem{}, err
	}
	s.notify(ctx, "garden_storage.changed", map[string]any{"id": id})
	return out, nil
}

func (s *Service) DeleteStorage(ctx context.Context, id string) error {
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		before, err := s.store.GetStorage(ctx, tx, id)
		if err != nil {
			return mapNotFound(err)
		}
		at := nowUTC()
		if err := s.store.SoftDeleteStorage(ctx, tx, id, at); err != nil {
			return err
		}
		return s.record(ctx, tx, "storage.delete", "garden_storage_item", id,
			"Smazáno ze skladu: "+before.ProductName, nil, nil)
	})
	if err != nil {
		return err
	}
	s.notify(ctx, "garden_storage.changed", map[string]any{"id": id})
	return nil
}

// ================================= rules =================================

type RulePage struct {
	Items      []Rule  `json:"items"`
	NextCursor *string `json:"next_cursor"`
}

func (s *Service) ListRules(ctx context.Context, scope string, includeDisabled bool, limit int, cursor string) (RulePage, error) {
	items, next, err := s.store.ListRules(ctx, nil, scope, includeDisabled, NormalizeLimit(limit), cursor)
	if err != nil {
		return RulePage{}, err
	}
	if err := s.labelRules(ctx, items); err != nil {
		return RulePage{}, err
	}
	if items == nil {
		items = []Rule{}
	}
	return RulePage{Items: items, NextCursor: next}, nil
}

// labelRules fills the human-readable a_label/b_label. Built-in plant pairs
// reference crops by NAME (see the seed migration), so the label is the ref
// itself; user rules reference ids and need a lookup.
func (s *Service) labelRules(ctx context.Context, rules []Rule) error {
	ids := map[string]bool{}
	for _, r := range rules {
		if r.Scope == ScopePlantPair {
			ids[r.ARef], ids[r.BRef] = true, true
		}
	}
	names := map[string]string{}
	if len(ids) > 0 {
		plants, err := s.store.plantsByID(ctx, s.db, keysOf(ids))
		if err != nil {
			return err
		}
		for id, p := range plants {
			names[id] = p.NameCS
		}
	}
	label := func(scope, ref string) string {
		if scope == ScopePlantPair {
			if n, ok := names[ref]; ok {
				return n
			}
			return ref // a built-in naming a crop this garden does not grow
		}
		return Label(EnumFamily, ref)
	}
	for i := range rules {
		rules[i].ALabel = label(rules[i].Scope, rules[i].ARef)
		rules[i].BLabel = label(rules[i].Scope, rules[i].BRef)
	}
	return nil
}

// RuleInput is the create/update body.
type RuleInput struct {
	Scope       *string `json:"scope"`
	ARef        *string `json:"a_ref"`
	BRef        *string `json:"b_ref"`
	Verdict     *string `json:"verdict"`
	Severity    *string `json:"severity"`
	MinYearsGap *int    `json:"min_years_gap"`
	ReasonCS    *string `json:"reason_cs"`
	Source      *string `json:"source"`
	IsDisabled  *bool   `json:"is_disabled"`
}

func (s *Service) CreateRule(ctx context.Context, in RuleInput) (Rule, error) {
	r := Rule{
		ID: idgen.New(), Scope: derefS(in.Scope), Verdict: derefS(in.Verdict),
		Severity: SeverityWarn, MinYearsGap: in.MinYearsGap, ReasonCS: in.ReasonCS, Source: in.Source,
	}
	if in.Severity != nil && *in.Severity != "" {
		r.Severity = *in.Severity
	}
	// Canonical (sorted) order makes symmetry structural: the matcher looks up
	// one row, not two, and nobody has to remember to enter both directions.
	r.ARef, r.BRef = canonicalPair(derefS(in.ARef), derefS(in.BRef))
	if err := validateRule(r); err != nil {
		return Rule{}, err
	}
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		if err := s.store.InsertRule(ctx, tx, r, actorID(ctx), nowUTC()); err != nil {
			if strings.Contains(err.Error(), "UNIQUE constraint failed") {
				return httpx.ErrConflict("Pravidlo pro tuhle dvojici už existuje.")
			}
			return err
		}
		return s.record(ctx, tx, "rule.create", "garden_rule", r.ID, "Přidáno pravidlo", nil, nil)
	})
	if err != nil {
		return Rule{}, err
	}
	// A rule IS the check engine's input, so a second screen still showing the
	// warning this rule just changed is stale in the way this module cares about
	// most — a green tick, or a red one, that has stopped being true.
	s.notify(ctx, "garden_rule.changed", map[string]any{"id": r.ID})
	out := []Rule{r}
	_ = s.labelRules(ctx, out)
	return out[0], nil
}

func validateRule(r Rule) error {
	if !Valid(EnumRuleScope, r.Scope) {
		return errUnprocessable("Neznámý rozsah pravidla.")
	}
	if r.ARef == "" || r.BRef == "" {
		return errUnprocessable("Pravidlo musí určit obě strany.")
	}
	if r.Verdict != VerdictGood && r.Verdict != VerdictBad {
		return errUnprocessable("Pravidlo musí říct, jestli je dvojice vhodná, nebo ne.")
	}
	if !Valid(EnumSeverity, r.Severity) {
		return errUnprocessable("Neznámá závažnost.")
	}
	if r.Scope != ScopePlantPair {
		for _, ref := range []string{r.ARef, r.BRef} {
			if !Valid(EnumFamily, ref) {
				return errUnprocessable("Neznámá čeleď „" + ref + "“.")
			}
		}
	}
	if r.MinYearsGap != nil && (*r.MinYearsGap < 0 || *r.MinYearsGap > 10) {
		return errUnprocessable("Pauza musí být 0–10 let.")
	}
	return nil
}

// UpdateRule edits a rule. On a BUILT-IN only is_disabled and severity are
// accepted (D130): the built-ins carry their `source`, and letting them be
// rewritten in place would destroy the one thing that tells agronomy and
// folklore apart.
func (s *Service) UpdateRule(ctx context.Context, id string, in RuleInput) (Rule, error) {
	var out Rule
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		before, err := s.store.GetRule(ctx, tx, id)
		if err != nil {
			return mapNotFound(err)
		}
		after := before
		if before.IsBuiltin {
			if in.Verdict != nil || in.MinYearsGap != nil || in.ReasonCS != nil || in.Source != nil {
				return errUnprocessable("U vestavěného pravidla lze změnit jen závažnost a jestli je vypnuté.")
			}
		} else {
			if in.Verdict != nil {
				after.Verdict = *in.Verdict
			}
			if in.MinYearsGap != nil {
				after.MinYearsGap = in.MinYearsGap
			}
			if in.ReasonCS != nil {
				after.ReasonCS = emptyToNil(in.ReasonCS)
			}
			if in.Source != nil {
				after.Source = emptyToNil(in.Source)
			}
		}
		if in.Severity != nil {
			after.Severity = *in.Severity
		}
		if in.IsDisabled != nil {
			after.IsDisabled = *in.IsDisabled
		}
		if err := validateRule(after); err != nil {
			return err
		}
		if err := s.store.UpdateRule(ctx, tx, after, nowUTC()); err != nil {
			return err
		}
		out = after
		var changes []audit.Change
		diffPlain(&changes, "severity", before.Severity, after.Severity)
		diffPlain(&changes, "is_disabled", boolCS(before.IsDisabled), boolCS(after.IsDisabled))
		return s.record(ctx, tx, "rule.update", "garden_rule", id, "Upraveno pravidlo", changes, nil)
	})
	if err != nil {
		return Rule{}, err
	}
	s.notify(ctx, "garden_rule.changed", map[string]any{"id": id})
	list := []Rule{out}
	_ = s.labelRules(ctx, list)
	return list[0], nil
}

// DeleteRule removes a USER rule. A built-in is a 409 (D130) — you can turn it
// off, but you can always see what you did not type.
func (s *Service) DeleteRule(ctx context.Context, id string) error {
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		before, err := s.store.GetRule(ctx, tx, id)
		if err != nil {
			return mapNotFound(err)
		}
		if before.IsBuiltin {
			return httpx.ErrConflict("Vestavěné pravidlo nelze smazat — můžete ho vypnout.")
		}
		at := nowUTC()
		if err := s.store.SoftDeleteRule(ctx, tx, id, at); err != nil {
			return err
		}
		return s.record(ctx, tx, "rule.delete", "garden_rule", id, "Smazáno pravidlo", nil, nil)
	})
	if err != nil {
		return err
	}
	s.notify(ctx, "garden_rule.changed", map[string]any{"id": id})
	return nil
}

// =============================== settings ===============================

func (s *Service) GetSettings(ctx context.Context) (Settings, error) {
	st, err := s.store.Settings(ctx, nil)
	if err != nil {
		return Settings{}, err
	}
	st.WeatherEnabled = s.opts.WeatherEnabled
	return st, nil
}

// SettingsInput is PATCH /api/garden/settings. There are deliberately no
// audience or notification fields here — that is Administrace's (D113).
type SettingsInput struct {
	DefaultLastFrost      *string      `json:"default_last_frost"`
	DefaultFirstFrost     *string      `json:"default_first_frost"`
	Latitude              *float64     `json:"latitude"`
	Longitude             *float64     `json:"longitude"`
	AltitudeM             *int         `json:"altitude_m"`
	RotationBreakDefault  *int         `json:"rotation_break_default"`
	FrostThresholdC       *float64     `json:"frost_threshold_c"`
	FrostLookaheadDays    *int         `json:"frost_lookahead_days"`
	WorkloadWeekThreshold *int         `json:"workload_week_threshold"`
	Checks                ChecksConfig `json:"checks"`

	// present is the decoded body's key set, for the same reason PlantInput
	// carries one: the NULLABLE settings can only be CLEARED if "omitted" and
	// "explicitly null" are distinguishable, and both decode to a nil pointer.
	// Without it a mistyped latitude could be corrected but never removed — and
	// removing the coordinates is how the household stops the forecast being
	// fetched at all (D112).
	present presentFields
}

func (in *SettingsInput) UnmarshalJSON(b []byte) error {
	type alias SettingsInput
	var a alias
	present, err := decodePatch(b, &a)
	if err != nil {
		return err
	}
	*in = SettingsInput(a)
	in.present = present
	return nil
}

// carried reports whether the body actually contained `key`. An input built in
// Go (a test) has no key set, so it falls back to "a non-nil value wins", which
// is how this behaved before the distinction existed.
func (in SettingsInput) carried(key string, nonNil bool) bool {
	if in.present == nil {
		return nonNil
	}
	return in.present[key]
}

func (s *Service) UpdateSettings(ctx context.Context, in SettingsInput) (Settings, error) {
	var out Settings
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		st, err := s.store.Settings(ctx, tx)
		if err != nil {
			return err
		}
		before := st
		// The five NULLABLE settings honour an explicit null as "clear this",
		// which is what the form sends when a field is emptied. The non-nullable
		// ones below keep the plain nil check: there is no "no threshold".
		if in.carried("default_last_frost", in.DefaultLastFrost != nil) {
			st.DefaultLastFrost = emptyToNil(in.DefaultLastFrost)
		}
		if in.carried("default_first_frost", in.DefaultFirstFrost != nil) {
			st.DefaultFirstFrost = emptyToNil(in.DefaultFirstFrost)
		}
		if in.carried("latitude", in.Latitude != nil) {
			st.Latitude = in.Latitude
		}
		if in.carried("longitude", in.Longitude != nil) {
			st.Longitude = in.Longitude
		}
		if in.carried("altitude_m", in.AltitudeM != nil) {
			st.AltitudeM = in.AltitudeM
		}
		if in.RotationBreakDefault != nil {
			st.RotationBreakDefault = *in.RotationBreakDefault
		}
		if in.FrostThresholdC != nil {
			st.FrostThresholdC = *in.FrostThresholdC
		}
		if in.FrostLookaheadDays != nil {
			st.FrostLookaheadDays = *in.FrostLookaheadDays
		}
		if in.WorkloadWeekThreshold != nil {
			st.WorkloadWeekThreshold = *in.WorkloadWeekThreshold
		}
		if in.Checks != nil {
			st.Checks = in.Checks
		}
		if err := validateSettings(st); err != nil {
			return err
		}
		if err := s.store.UpdateSettings(ctx, tx, st, nowUTC()); err != nil {
			return err
		}
		out = st
		var changes []audit.Change
		diffStr(&changes, "default_last_frost", before.DefaultLastFrost, st.DefaultLastFrost)
		diffStr(&changes, "default_first_frost", before.DefaultFirstFrost, st.DefaultFirstFrost)
		diffFloat(&changes, "latitude", before.Latitude, st.Latitude)
		diffFloat(&changes, "longitude", before.Longitude, st.Longitude)
		return s.record(ctx, tx, "settings.update", "garden_settings", "1", "Upraveno nastavení zahrady", changes, nil)
	})
	if err != nil {
		return Settings{}, err
	}
	// The frost threshold, the rotation default and the per-check config are all
	// snapshot inputs, so this moves the whole module's answer — not just this
	// screen's form.
	s.notify(ctx, "garden_settings.changed", map[string]any{})
	out.WeatherEnabled = s.opts.WeatherEnabled
	return out, nil
}

func validateSettings(st Settings) error {
	for _, f := range []struct {
		label string
		value *string
	}{{"Poslední jarní mráz", st.DefaultLastFrost}, {"První podzimní mráz", st.DefaultFirstFrost}} {
		if f.value == nil || *f.value == "" {
			continue
		}
		if len(*f.value) != 5 || (*f.value)[2] != '-' {
			return errUnprocessable(f.label + ": zadejte ve tvaru MM-DD.")
		}
		if _, err := dates.Parse("2000-" + *f.value); err != nil {
			return errUnprocessable(f.label + ": neplatné datum.")
		}
	}
	if st.Latitude != nil && (*st.Latitude < -90 || *st.Latitude > 90) {
		return errUnprocessable("Zeměpisná šířka musí být mezi −90 a 90.")
	}
	if st.Longitude != nil && (*st.Longitude < -180 || *st.Longitude > 180) {
		return errUnprocessable("Zeměpisná délka musí být mezi −180 a 180.")
	}
	if st.RotationBreakDefault < 0 || st.RotationBreakDefault > 10 {
		return errUnprocessable("Výchozí pauza pro střídání musí být 0–10 let.")
	}
	if st.FrostLookaheadDays < 1 || st.FrostLookaheadDays > 14 {
		return errUnprocessable("Výhled na mráz musí být 1–14 dní.")
	}
	if st.WorkloadWeekThreshold < 1 {
		return errUnprocessable("Práh nabitého týdne musí být aspoň 1.")
	}
	for key, cfg := range st.Checks {
		if !strings.HasPrefix(key, "C") {
			return errUnprocessable("Neznámá kontrola „" + key + "“.")
		}
		if cfg.Severity != nil && !Valid(EnumSeverity, *cfg.Severity) {
			return errUnprocessable("Kontrola " + key + ": neznámá závažnost.")
		}
	}
	return nil
}

// =============================== weather ===============================

// WeatherPage is GET /api/garden/weather.
type WeatherPage struct {
	Items []WeatherDay `json:"items"`
	// Stale is true when the newest row is older than two poll intervals. The UI
	// degrades quietly on it; it does not raise an error, because a forecast that
	// did not load is not something anyone can act on (D112).
	Stale bool `json:"stale"`
}

func (s *Service) Weather(ctx context.Context, from, to string) (WeatherPage, error) {
	today := s.today()
	if from == "" {
		from = today.String()
	}
	if to == "" {
		to = today.AddDays(7).String()
	}
	items, err := s.store.WeatherRange(ctx, from, to)
	if err != nil {
		return WeatherPage{}, err
	}
	if items == nil {
		items = []WeatherDay{}
	}
	return WeatherPage{Items: items, Stale: s.weatherStale(items)}, nil
}

func (s *Service) weatherStale(items []WeatherDay) bool {
	if len(items) == 0 {
		return true
	}
	newest := ""
	for _, d := range items {
		if d.FetchedAt != nil && *d.FetchedAt > newest {
			newest = *d.FetchedAt
		}
	}
	if newest == "" {
		return true
	}
	cutoff := nowMinus(2 * s.opts.WeatherPoll)
	return newest < cutoff
}

func fmtQty(v float64) string {
	if v == float64(int64(v)) {
		return strconvFormatInt(int64(v))
	}
	return strconvFormatFloat(v)
}

func boolCS(v bool) string {
	if v {
		return "ano"
	}
	return "ne"
}

func strconvFormatInt(v int64) string     { return strconv.FormatInt(v, 10) }
func strconvFormatFloat(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// nowMinus returns the UTC timestamp d ago, in the store's format.
func nowMinus(d time.Duration) string { return time.Now().UTC().Add(-d).Format(tsFormat) }
