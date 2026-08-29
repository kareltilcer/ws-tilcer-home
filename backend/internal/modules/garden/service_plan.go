package garden

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/dates"
	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/db"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/idgen"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/lexorank"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
)

// The planning half of the service: seasons, plantings, the work calendar, the
// check, harvest and storage.

// =============================== seasons ===============================

func (s *Service) ListSeasons(ctx context.Context) ([]Season, error) {
	items, err := s.store.ListSeasons(ctx)
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = []Season{}
	}
	return items, nil
}

func (s *Service) GetSeason(ctx context.Context, year int) (Season, error) {
	se, err := s.store.SeasonByYear(ctx, nil, year)
	return se, mapNotFound(err)
}

// SeasonShift describes a rotation of bed assignments over the ordered active
// beds (FR-G5).
type SeasonShift struct {
	Offset  int   `json:"offset"`
	ByFamily *bool `json:"by_family"`
}

// SeasonCreateInput is POST /api/garden/seasons.
type SeasonCreateInput struct {
	Year         int          `json:"year"`
	LastFrostOn  *string      `json:"last_frost_on"`
	FirstFrostOn *string      `json:"first_frost_on"`
	NotesMD      *string      `json:"notes_md"`
	CopyFrom     *int         `json:"copy_from"`
	Shift        *SeasonShift `json:"shift"`
}

// SeasonPreview is the dry_run=true response (D129). NOTHING WAS PERSISTED.
type SeasonPreview struct {
	Season      Season      `json:"season"`
	Plantings   []Planting  `json:"plantings"`
	Check       CheckResult `json:"check"`
	CheckBefore *CheckResult `json:"check_before,omitempty"`
}

// CreateSeason creates a season, optionally copying and rotating a previous
// year's plan.
//
// dry_run returns the whole prospective season plus its check WITHOUT
// persisting, alongside the source season's check — so the UI can show what a
// rotation shift fixed and what it broke, side by side, BEFORE the season
// exists. A copy that silently reproduces last year's rotation error is worse
// than typing the year in by hand.
func (s *Service) CreateSeason(ctx context.Context, in SeasonCreateInput, dryRun bool) (Season, *SeasonPreview, error) {
	if in.Year < 1900 || in.Year > 2200 {
		return Season{}, nil, errUnprocessable("Rok sezóny musí být rozumné číslo.")
	}
	for _, d := range []*string{in.LastFrostOn, in.FirstFrostOn} {
		if d != nil && *d != "" {
			if _, err := dates.Parse(*d); err != nil {
				return Season{}, nil, errUnprocessable("Datum mrazu musí být ve tvaru RRRR-MM-DD.")
			}
		}
	}

	settings, err := s.store.Settings(ctx, nil)
	if err != nil {
		return Season{}, nil, err
	}
	se := Season{
		ID: idgen.New(), Year: in.Year, Status: SeasonPlanning,
		LastFrostOn: in.LastFrostOn, FirstFrostOn: in.FirstFrostOn, NotesMD: in.NotesMD,
	}
	// A COPY FILLS A SEASON THAT ALREADY EXISTS. "Zkopírovat z předchozí sezóny"
	// is offered from the planner of the year being filled, whose row necessarily
	// exists — so an insert would collide with ux_garden_seasons_year every single
	// time and the action could never once succeed. A copy therefore REUSES the
	// row and appends to its plan; a bare create on a taken year is still a 409,
	// because that one really is a mistake.
	reuse := false
	if in.CopyFrom != nil {
		existing, err := s.store.SeasonByYear(ctx, nil, in.Year)
		switch {
		case err == nil:
			if existing.Status == SeasonClosed {
				return Season{}, nil, httpx.ErrConflict("Sezóna je uzavřená. Nejdřív ji musí správce znovu otevřít.")
			}
			// A copy FILLS a season — it must not fill it twice. Re-applying the
			// dialog onto a season that already has plantings would silently
			// duplicate every one of them (plus their generated tasks), with no
			// undo, so a non-empty target is a hard stop rather than an append.
			already, _, err := s.store.ListPlantings(ctx, nil, PlantingFilter{Year: &in.Year}, 1, "")
			if err != nil {
				return Season{}, nil, err
			}
			if len(already) > 0 {
				return Season{}, nil, httpx.ErrConflict("Sezóna " + strconv.Itoa(in.Year) + " už výsadby má — kopírování by je zdvojilo.")
			}
			reuse = true
			se.ID, se.Status, se.CreatedAt = existing.ID, existing.Status, existing.CreatedAt
			se.LastFrostActualOn, se.FirstFrostActualOn = existing.LastFrostActualOn, existing.FirstFrostActualOn
			// Only what the caller actually sent overrides the season already on
			// record — a copy is not a licence to reset its frost dates.
			if in.LastFrostOn == nil {
				se.LastFrostOn = existing.LastFrostOn
			}
			if in.FirstFrostOn == nil {
				se.FirstFrostOn = existing.FirstFrostOn
			}
			if in.NotesMD == nil {
				se.NotesMD = existing.NotesMD
			}
		case err != errNotFound:
			return Season{}, nil, err
		}
	}
	// Fall back to the garden's default frost dates so a new season is usable
	// straight away — without them every frost-anchored window is unresolvable
	// and the plan comes out empty.
	if se.LastFrostOn == nil {
		se.LastFrostOn = frostDateFor(settings.DefaultLastFrost, in.Year)
	}
	if se.FirstFrostOn == nil {
		se.FirstFrostOn = frostDateFor(settings.DefaultFirstFrost, in.Year)
	}
	now := nowUTC()
	se.UpdatedAt = now
	if !reuse {
		se.CreatedAt = now
	}

	var copied []Planting
	var checkBefore *CheckResult
	if in.CopyFrom != nil {
		source, err := s.store.SeasonByYear(ctx, nil, *in.CopyFrom)
		if err != nil {
			return Season{}, nil, httpx.ErrUnprocessable("Sezóna, ze které se má kopírovat, neexistuje.")
		}
		before, err := s.CheckSeason(ctx, source.Year)
		if err != nil {
			return Season{}, nil, err
		}
		checkBefore = &before
		copied, err = s.buildCopy(ctx, source, se, in.Shift)
		if err != nil {
			return Season{}, nil, err
		}
	}

	if dryRun {
		preview, err := s.previewSeason(ctx, se, copied)
		if err != nil {
			return Season{}, nil, err
		}
		preview.CheckBefore = checkBefore
		return Season{}, &preview, nil
	}

	err = appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		if reuse {
			if err := s.store.UpdateSeason(ctx, tx, se); err != nil {
				return err
			}
		} else if err := s.store.InsertSeason(ctx, tx, se, reqctx.ActorID(ctx)); err != nil {
			if strings.Contains(err.Error(), "UNIQUE constraint failed") {
				return httpx.ErrConflict("Sezóna " + strconv.Itoa(in.Year) + " už existuje.")
			}
			return err
		}
		for i := range copied {
			copied[i].SeasonID = sp(se.ID)
			if err := s.store.InsertPlanting(ctx, tx, copied[i], reqctx.ActorID(ctx)); err != nil {
				return err
			}
			if _, err := s.regeneratePlanting(ctx, tx, copied[i]); err != nil {
				return err
			}
		}
		// The verb follows what actually happened to the row, so the Log does not
		// claim a season was founded when its plan was merely filled in.
		action, summary := "season.create", "Založena sezóna "+strconv.Itoa(in.Year)
		if reuse {
			action, summary = "season.update", "Doplněna sezóna "+strconv.Itoa(in.Year)
		}
		if len(copied) > 0 {
			summary += fmt.Sprintf(" — zkopírováno %d výsadeb z roku %d", len(copied), derefI(in.CopyFrom, 0))
		}
		return s.record(ctx, tx, action, "garden_season", se.ID, summary, nil,
			map[string]any{"copied": len(copied)})
	})
	if err != nil {
		return Season{}, nil, err
	}
	s.notify(ctx, "garden_season.changed", map[string]any{"year": in.Year})
	out, err := s.GetSeason(ctx, in.Year)
	return out, nil, err
}

// frostDateFor turns a settings "MM-DD" default into a real date in a year.
func frostDateFor(mmdd *string, year int) *string {
	if mmdd == nil || len(*mmdd) != 5 {
		return nil
	}
	full := strconv.Itoa(year) + "-" + *mmdd
	if _, err := dates.Parse(full); err != nil {
		return nil
	}
	return &full
}

// buildCopy reproduces a season's non-permanent plantings into a new season,
// optionally rotating their bed assignments, and RE-ANCHORS the planned dates
// against the new season's frost dates.
//
// Frost-anchored windows move and week-anchored ones do not — which is the
// intent in both cases (D102).
//
// A COPY DOES NOT CARRY manual_dates, and that is the one place the flag does
// not survive: "12 May 2027" is a decision about 2027, and a 2028 planting
// holding it would be a date nobody chose for that year. So every planned date
// on a copy is re-derived from the crop's own windows, and the household edits
// the ones it wants to pin again — which re-flags them. (reanchorSeason, which
// stays inside one season, does honour manual_dates.)
func (s *Service) buildCopy(ctx context.Context, source, target Season, shift *SeasonShift) ([]Planting, error) {
	// EXHAUSTIVE, not one page. A copy that stopped at the 200th planting would
	// reproduce a truncated plan with nothing saying so — and CreateSeason's
	// non-empty guard then refuses to re-run the copy for the remainder, so the
	// dropped rows could only be re-entered by hand.
	src, err := s.store.allPlantings(ctx, nil, PlantingFilter{Year: &source.Year})
	if err != nil {
		return nil, err
	}
	beds, err := s.store.ListBeds(ctx, nil, false)
	if err != nil {
		return nil, err
	}
	bedIndex := map[string]int{}
	for i, b := range beds {
		bedIndex[b.ID] = i
	}
	if shift != nil && len(beds) > 0 && (shift.Offset > len(beds) || shift.Offset < -len(beds)) {
		return nil, errUnprocessable("Posun je větší než počet záhonů.")
	}

	anchors := target.Anchors()
	out := make([]Planting, 0, len(src))
	// A shift moves a family as a BLOCK — the "posuň čeledi o záhon" move, which
	// is what a rotation actually is — and a block keeps its SHAPE. Every
	// planting therefore slides `offset` beds from its OWN bed, so a family
	// spread across A1 and A3 lands on A2 and A4.
	//
	// It used to measure the offset from one anchor bed per family, which sent
	// every planting of that family to a single destination: A1 and A3 both
	// became A2, over-booking one bed and emptying another. That is not a block
	// move, it is a collapse, and it silently destroyed the source layout.
	for _, p := range src {
		if p.IsPermanent() {
			continue // permanents are not copied; they simply continue
		}
		np := Planting{
			ID: idgen.New(), BedID: p.BedID, LocationLabel: p.LocationLabel,
			PlantID: p.PlantID, VarietyID: p.VarietyID,
			AreaM2: p.AreaM2, PlantCount: p.PlantCount, Rows: p.Rows,
			Status: PlantingPlanned, NotesMD: p.NotesMD,
			PlantName: p.PlantName, Family: p.Family, VarietyName: p.VarietyName,
			// Actual dates are emphatically NOT copied: last year's sowing date is
			// history, not next year's plan.
			ManualDates: nil,
		}
		if shift != nil && shift.Offset != 0 && np.BedID != nil && len(beds) > 0 {
			if idx, ok := bedIndex[*np.BedID]; ok {
				// by_family is still accepted (older clients send it) but no
				// longer changes the destination: measuring from anything other
				// than the planting's own bed is what collapsed a family onto one
				// bed. The offset wraps, so the last bed rotates back to the first.
				np.BedID = sp(beds[((idx+shift.Offset)%len(beds)+len(beds))%len(beds)].ID)
			}
		}
		eff, err := s.store.EffectiveFor(ctx, nil, np.PlantID, np.VarietyID)
		if err != nil {
			return nil, err
		}
		PlannedDates(&np, eff, anchors)
		now := nowUTC()
		np.CreatedAt, np.UpdatedAt = now, now
		np.Occupancy = np.OccupancyWire()
		out = append(out, np)
	}
	return out, nil
}

// previewSeason runs the check over a season that does not exist yet.
func (s *Service) previewSeason(ctx context.Context, se Season, plantings []Planting) (SeasonPreview, error) {
	snap := Snapshot{Season: se, Today: s.today(), Plantings: plantings, Effective: map[string]Effective{}}
	var err error
	if snap.Beds, err = s.store.ListBeds(ctx, nil, false); err != nil {
		return SeasonPreview{}, err
	}
	if err = s.store.resolveEffective(ctx, s.db, plantings, snap.Effective); err != nil {
		return SeasonPreview{}, err
	}
	if snap.Rules, err = s.store.rulesForMatching(ctx, s.db); err != nil {
		return SeasonPreview{}, err
	}
	if snap.History, err = s.store.allBedHistory(ctx, s.db); err != nil {
		return SeasonPreview{}, err
	}
	if snap.ClosedSeasons, err = s.store.ClosedSeasonCount(ctx, nil); err != nil {
		return SeasonPreview{}, err
	}
	if snap.Settings, err = s.store.Settings(ctx, nil); err != nil {
		return SeasonPreview{}, err
	}
	// No dismissals: the season does not exist, so nothing has been dismissed for
	// it — and the warning keys carry the new year anyway, so last year's
	// "letos to risknu" could not apply even if it did.
	snap.Dismissals = map[string]Dismissal{}
	if plantings == nil {
		plantings = []Planting{}
	}
	return SeasonPreview{Season: se, Plantings: plantings, Check: Check(snap)}, nil
}

// SeasonUpdateInput is PATCH /api/garden/seasons/{year}.
type SeasonUpdateInput struct {
	Status       *string `json:"status"`
	LastFrostOn  *string `json:"last_frost_on"`
	FirstFrostOn *string `json:"first_frost_on"`
	NotesMD      *string `json:"notes_md"`
}

// UpdateSeason changes a season and, when a frost date moved, RE-ANCHORS every
// frost-anchored planned date the user has not overridden.
func (s *Service) UpdateSeason(ctx context.Context, year int, in SeasonUpdateInput) (Season, error) {
	// The same guard CreateSeason applies: a garbage frost date would persist
	// with a 200 and silently un-anchor every frost-anchored window.
	for _, d := range []*string{in.LastFrostOn, in.FirstFrostOn} {
		if d != nil && *d != "" {
			if _, err := dates.Parse(*d); err != nil {
				return Season{}, errUnprocessable("Datum mrazu musí být ve tvaru RRRR-MM-DD.")
			}
		}
	}
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		before, err := s.store.SeasonByYear(ctx, tx, year)
		if err != nil {
			return mapNotFound(err)
		}
		if before.Status == SeasonClosed {
			return httpx.ErrConflict("Sezóna je uzavřená. Nejdřív ji musí správce znovu otevřít.")
		}
		after := before
		if in.Status != nil {
			if !Valid(EnumSeasonStatus, *in.Status) || *in.Status == SeasonClosed {
				return errUnprocessable("Stav sezóny lze změnit jen na „plánuje se“ nebo „probíhá“; k uzavření slouží vlastní akce.")
			}
			after.Status = *in.Status
		}
		if in.LastFrostOn != nil {
			after.LastFrostOn = in.LastFrostOn
		}
		if in.FirstFrostOn != nil {
			after.FirstFrostOn = in.FirstFrostOn
		}
		if in.NotesMD != nil {
			after.NotesMD = in.NotesMD
		}
		after.UpdatedAt = nowUTC()
		if err := s.store.UpdateSeason(ctx, tx, after); err != nil {
			return err
		}

		var changes []audit.Change
		diffStr(&changes, "last_frost_on", before.LastFrostOn, after.LastFrostOn)
		diffStr(&changes, "first_frost_on", before.FirstFrostOn, after.FirstFrostOn)
		diffPlain(&changes, "status", before.Status, after.Status)
		if err := s.record(ctx, tx, "season.update", "garden_season", after.ID,
			"Upravena sezóna "+strconv.Itoa(year), changes, nil); err != nil {
			return err
		}

		if !sameStrPtr(before.LastFrostOn, after.LastFrostOn) || !sameStrPtr(before.FirstFrostOn, after.FirstFrostOn) {
			return s.reanchorSeason(ctx, tx, after)
		}
		return nil
	})
	if err != nil {
		return Season{}, err
	}
	s.notify(ctx, "garden_season.changed", map[string]any{"year": year})
	return s.GetSeason(ctx, year)
}

// reanchorSeason recomputes planned dates for every planting in a season after
// its frost dates moved, then regenerates the work.
func (s *Service) reanchorSeason(ctx context.Context, tx *sql.Tx, se Season) error {
	// EXHAUSTIVE, not one page: a re-anchor that stopped at the 200th planting
	// would leave the season holding two generations of frost-anchored dates,
	// with nothing in the response or the audit event saying the pass was partial.
	plantings, err := s.store.allPlantings(ctx, tx, PlantingFilter{Year: &se.Year})
	if err != nil {
		return err
	}
	anchors := se.Anchors()
	for _, p := range plantings {
		eff, err := s.store.EffectiveFor(ctx, tx, p.PlantID, p.VarietyID)
		if err != nil {
			return err
		}
		updated := p
		PlannedDates(&updated, eff, anchors)
		updated.UpdatedAt = nowUTC()
		if err := s.store.UpdatePlanting(ctx, tx, updated); err != nil {
			return err
		}
		if _, err := s.regeneratePlanting(ctx, tx, updated); err != nil {
			return err
		}
	}
	return nil
}

// SeasonCloseOutcome is one planting's final word at season close.
type SeasonCloseOutcome struct {
	PlantingID     string   `json:"planting_id"`
	Status         string   `json:"status"`
	FailReason     *string  `json:"fail_reason"`
	FinalYield     *float64 `json:"final_yield"`
	FinalYieldUnit *string  `json:"final_yield_unit"`
}

// SeasonCloseInput is POST /api/garden/seasons/{year}/close.
type SeasonCloseInput struct {
	LastFrostActualOn  *string              `json:"last_frost_actual_on"`
	FirstFrostActualOn *string              `json:"first_frost_actual_on"`
	NotesMD            *string              `json:"notes_md"`
	Outcomes           []SeasonCloseOutcome `json:"outcomes"`
}

// CloseSeason locks a season and turns it into rotation history.
//
// This is the ONLY thing that creates history (D120), which is why it is a
// once-a-year ritual worth twenty minutes rather than a status toggle: a
// half-recorded season becomes half-trusted history, and C3/C8 read nothing else.
func (s *Service) CloseSeason(ctx context.Context, year int, in SeasonCloseInput) (Season, error) {
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		se, err := s.store.SeasonByYear(ctx, tx, year)
		if err != nil {
			return mapNotFound(err)
		}
		if se.Status == SeasonClosed {
			return httpx.ErrConflict("Sezóna už je uzavřená.")
		}
		for _, o := range in.Outcomes {
			if o.Status != PlantingDone && o.Status != PlantingFailed {
				return errUnprocessable("Výsledek výsadby musí být „hotovo“ nebo „nepovedlo se“.")
			}
			p, err := s.store.GetPlanting(ctx, tx, o.PlantingID)
			if err != nil {
				return mapNotFound(err)
			}
			// THE OUTCOME MUST BELONG TO THE SEASON BEING CLOSED. Without this
			// an outcome could hand a final status and a harvest row to a
			// planting in another season — an already-closed one included —
			// rewriting the history C3 and C8 read, past both requireOpenSeason
			// and the admin-only reopen gate that exists to protect it.
			if p.SeasonID == nil || *p.SeasonID != se.ID {
				return errUnprocessable("Výsadba nepatří do uzavírané sezóny.")
			}
			p.Status = o.Status
			p.FailReason = o.FailReason
			p.UpdatedAt = nowUTC()
			if err := s.store.UpdatePlanting(ctx, tx, p); err != nil {
				return err
			}
			// A recorded final yield becomes an ordinary harvest row, so the
			// season total and the per-planting yield read from one place.
			if o.FinalYield != nil && *o.FinalYield > 0 {
				// THE CROP'S OWN UNIT IS THE DEFAULT, not kg — the same default
				// CreateHarvest takes, and for a sharper reason here. A season
				// close is a review of numbers already on screen: "120" against
				// cibule means 120 kusů. Stored as kilograms it would land in
				// garden.harvest_season (which sums kg only), while the planting's
				// yield_actual — read in the crop's own unit — would stay empty,
				// so the very next close would offer the box again and add a
				// second phantom 120 kg. An explicit unit still wins.
				eff, err := s.store.EffectiveFor(ctx, tx, p.PlantID, p.VarietyID)
				if err != nil {
					return err
				}
				unit := eff.Unit()
				if o.FinalYieldUnit != nil && Valid(EnumHarvestUnit, *o.FinalYieldUnit) {
					unit = *o.FinalYieldUnit
				}
				h := Harvest{
					ID: idgen.New(), PlantingID: o.PlantingID, Quantity: *o.FinalYield, Unit: unit,
					HarvestedOn: derefS(firstStr(p.ClearedOn, p.HarvestTo, sp(s.today().String()))),
					Note:        sp("Zapsáno při uzavření sezóny"),
				}
				if err := s.store.InsertHarvest(ctx, tx, h, reqctx.ActorID(ctx), nowUTC()); err != nil {
					return err
				}
			}
		}
		// THE SEASON'S UNFINISHED WORK IS CLOSED OUT WITH IT. A closed season is
		// frozen by requireOpenSeason, so a task left `open` here can never be
		// completed, skipped or deleted again — it would sit in Zmeškané and in
		// garden.tasks_overdue forever, and the only way out would be the
		// admin-only reopen that rewrites rotation history. The season ending is
		// itself the answer to "was this done": whatever is still open was not,
		// so it is marked `skipped` rather than stranded.
		skipped, err := s.closeOutTasks(ctx, tx, year)
		if err != nil {
			return err
		}

		se.Status = SeasonClosed
		se.LastFrostActualOn = orKeep(in.LastFrostActualOn, se.LastFrostActualOn)
		se.FirstFrostActualOn = orKeep(in.FirstFrostActualOn, se.FirstFrostActualOn)
		if in.NotesMD != nil {
			se.NotesMD = in.NotesMD
		}
		se.ClosedAt, se.ClosedBy = sp(nowUTC()), sp(reqctx.ActorID(ctx))
		se.UpdatedAt = nowUTC()
		if err := s.store.UpdateSeason(ctx, tx, se); err != nil {
			return err
		}
		return s.record(ctx, tx, "season.close", "garden_season", se.ID,
			fmt.Sprintf("Uzavřena sezóna %d — od teď se z ní počítá střídání plodin", year),
			nil, map[string]any{"outcomes": len(in.Outcomes), "tasks_skipped": skipped})
	})
	if err != nil {
		return Season{}, err
	}
	s.notify(ctx, "garden_season.changed", map[string]any{"year": year})
	return s.GetSeason(ctx, year)
}

// closeOutTasks marks every still-open task of a season `skipped`, and returns
// how many. Called by CloseSeason INSIDE its transaction, before the season is
// frozen — after that, requireOpenSeason refuses every write to these rows.
//
// `skipped` rather than deleted: the work really was planned and really did not
// happen, and that is worth keeping. It simply stops being outstanding.
func (s *Service) closeOutTasks(ctx context.Context, tx *sql.Tx, year int) (int, error) {
	tasks, err := s.store.allTasks(ctx, tx, TaskFilter{Year: &year, Status: TaskOpen})
	if err != nil {
		return 0, err
	}
	at := nowUTC()
	for _, t := range tasks {
		t.Status = TaskSkipped
		if err := s.store.UpdateTask(ctx, tx, t, at); err != nil {
			return 0, err
		}
	}
	return len(tasks), nil
}

// ReopenSeason is the module's ONLY admin-gated action (FR-G10). Re-opening
// rewrites the record C3 and C8 depend on, so it is not an ordinary edit.
func (s *Service) ReopenSeason(ctx context.Context, year int) (Season, error) {
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		se, err := s.store.SeasonByYear(ctx, tx, year)
		if err != nil {
			return mapNotFound(err)
		}
		if se.Status != SeasonClosed {
			return httpx.ErrConflict("Sezóna není uzavřená.")
		}
		se.Status, se.ClosedAt, se.ClosedBy = SeasonActive, nil, nil
		se.UpdatedAt = nowUTC()
		if err := s.store.UpdateSeason(ctx, tx, se); err != nil {
			return err
		}
		return s.record(ctx, tx, "season.reopen", "garden_season", se.ID,
			fmt.Sprintf("Znovu otevřena sezóna %d — přepisuje se tím historie střídání plodin", year), nil, nil)
	})
	if err != nil {
		return Season{}, err
	}
	s.notify(ctx, "garden_season.changed", map[string]any{"year": year})
	return s.GetSeason(ctx, year)
}

// ================================ check ================================

// CheckSeason runs Kontrola plánu (FR-G9). Computed on every read rather than
// cached, which is what keeps it honest: a stale check is worse than no check,
// because it is a green tick that has stopped being true.
func (s *Service) CheckSeason(ctx context.Context, year int) (CheckResult, error) {
	snap, err := s.store.SeasonSnapshot(ctx, nil, year, s.today())
	if err != nil {
		return CheckResult{}, mapNotFound(err)
	}
	return Check(snap), nil
}

// DismissWarning silences one warning for one season.
//
// Dismissal is a first-class action, not a close button: without it you stop
// reading the panel by April, and the panel is the feature.
func (s *Service) DismissWarning(ctx context.Context, year int, key string, note *string) error {
	if strings.TrimSpace(key) == "" {
		return errUnprocessable("Chybí klíč varování.")
	}
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		se, err := s.store.SeasonByYear(ctx, tx, year)
		if err != nil {
			return mapNotFound(err)
		}
		if err := s.store.InsertDismissal(ctx, tx, idgen.New(), se.ID, key, note, reqctx.ActorID(ctx), nowUTC()); err != nil {
			return err
		}
		summary := "Ignorováno varování v plánu " + strconv.Itoa(year)
		if note != nil && *note != "" {
			summary += " — " + *note
		}
		return s.record(ctx, tx, "season.update", "garden_season", se.ID, summary, nil,
			map[string]any{"dismissed": key})
	})
	if err != nil {
		return err
	}
	s.notify(ctx, "garden_season.changed", map[string]any{"year": year})
	return nil
}

func (s *Service) RestoreWarning(ctx context.Context, year int, key string) error {
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		se, err := s.store.SeasonByYear(ctx, tx, year)
		if err != nil {
			return mapNotFound(err)
		}
		ok, err := s.store.DeleteDismissal(ctx, tx, se.ID, key)
		if err != nil {
			return err
		}
		if !ok {
			return httpx.ErrNotFound("Takové ignorované varování tu není.")
		}
		return s.record(ctx, tx, "season.update", "garden_season", se.ID,
			"Obnoveno varování v plánu "+strconv.Itoa(year), nil, map[string]any{"restored": key})
	})
	if err != nil {
		return err
	}
	s.notify(ctx, "garden_season.changed", map[string]any{"year": year})
	return nil
}

// ============================== plantings ==============================

type PlantingPage struct {
	Items      []Planting `json:"items"`
	NextCursor *string    `json:"next_cursor"`
}

func (s *Service) ListPlantings(ctx context.Context, f PlantingFilter, limit int, cursor string) (PlantingPage, error) {
	items, next, err := s.store.ListPlantings(ctx, nil, f, NormalizeLimit(limit), cursor)
	if err != nil {
		return PlantingPage{}, err
	}
	if err := s.attachYields(ctx, items); err != nil {
		return PlantingPage{}, err
	}
	for i := range items {
		items[i].Drift = ComputeDrift(items[i])
	}
	if items == nil {
		items = []Planting{}
	}
	return PlantingPage{Items: items, NextCursor: next}, nil
}

// attachYields fills the yield pair on a PAGE of plantings, in two queries: the
// crop records together, then every planting's harvest totals together.
//
// The list carries the same numbers as the detail on purpose. Season close reads
// yield_actual off list rows to decide whether a harvest was already recorded,
// so a list that always reported "nothing recorded" made it offer to enter a
// figure that then landed as a SECOND harvest row and doubled the season total.
func (s *Service) attachYields(ctx context.Context, items []Planting) error {
	if len(items) == 0 {
		return nil
	}
	eff := map[string]Effective{}
	if err := s.store.resolveEffective(ctx, s.db, items, eff); err != nil {
		return err
	}
	ids := make([]string, 0, len(items))
	for _, p := range items {
		ids = append(ids, p.ID)
	}
	yields, err := s.store.YieldsFor(ctx, s.db, ids)
	if err != nil {
		return err
	}
	for i := range items {
		e, ok := eff[items[i].ID]
		if !ok {
			continue // an unresolvable crop record simply reports no yield
		}
		items[i].HarvestUnit = sp(e.Unit())
		items[i].YieldExpected = e.ExpectedYield(items[i])
		// The crop's own unit ONLY, exactly as YieldFor does: adding kilograms to
		// bunches gives a number worse than no number.
		if byUnit, ok := yields[items[i].ID]; ok {
			if total, ok := byUnit[e.Unit()]; ok {
				items[i].YieldActual = fp(total)
			}
		}
	}
	return nil
}

func (s *Service) GetPlanting(ctx context.Context, id string) (Planting, error) {
	p, err := s.store.GetPlanting(ctx, nil, id)
	if err != nil {
		return Planting{}, mapNotFound(err)
	}
	return s.enrichPlanting(ctx, p)
}

// enrichPlanting attaches the derived read-model fields: the drift sentence and
// the yield comparison, which is the payoff of recording a harvest at all.
func (s *Service) enrichPlanting(ctx context.Context, p Planting) (Planting, error) {
	p.Drift = ComputeDrift(p)
	eff, err := s.store.EffectiveFor(ctx, nil, p.PlantID, p.VarietyID)
	if err != nil {
		// mapNotFound, not the bare sentinel: an unresolvable crop record must
		// not leave this package as a raw error and become a 500.
		return p, mapNotFound(err)
	}
	p.HarvestUnit = sp(eff.Unit())
	p.YieldExpected = eff.ExpectedYield(p)
	actual, err := s.store.YieldFor(ctx, nil, p.ID, eff.Unit())
	if err != nil {
		return p, err
	}
	p.YieldActual = actual
	return p, nil
}

// PlantingInput is the create/update body.
type PlantingInput struct {
	SeasonID      *string  `json:"season_id"`
	BedID         *string  `json:"bed_id"`
	LocationLabel *string  `json:"location_label"`
	PlantID       *string  `json:"plant_id"`
	VarietyID     *string  `json:"variety_id"`
	AreaM2        *float64 `json:"area_m2"`
	PlantCount    *int     `json:"plant_count"`
	Rows          *int     `json:"rows"`

	SowIndoorOn  *string `json:"sow_indoor_on"`
	SowDirectOn  *string `json:"sow_direct_on"`
	TransplantOn *string `json:"transplant_on"`
	HarvestFrom  *string `json:"harvest_from"`
	HarvestTo    *string `json:"harvest_to"`

	SowedOn        *string `json:"sowed_on"`
	TransplantedOn *string `json:"transplanted_on"`
	FirstHarvestOn *string `json:"first_harvest_on"`
	ClearedOn      *string `json:"cleared_on"`

	Status     *string `json:"status"`
	FailReason *string `json:"fail_reason"`
	PlantedOn  *string `json:"planted_on"`
	Rootstock  *string `json:"rootstock"`
	RemovedOn  *string `json:"removed_on"`
	NotesMD    *string `json:"notes_md"`

	// SeasonYear is the friendlier way in from the planner, which knows the year
	// from its URL and not the season's surrogate id.
	SeasonYear *int `json:"season_year"`
}

func (s *Service) CreatePlanting(ctx context.Context, in PlantingInput) (Planting, error) {
	if in.PlantID == nil || *in.PlantID == "" {
		return Planting{}, errUnprocessable("Plodina je povinná.")
	}
	p := Planting{
		ID: idgen.New(), BedID: in.BedID, LocationLabel: in.LocationLabel,
		PlantID: *in.PlantID, VarietyID: in.VarietyID,
		AreaM2: in.AreaM2, PlantCount: in.PlantCount, Rows: in.Rows,
		SowIndoorOn: in.SowIndoorOn, SowDirectOn: in.SowDirectOn, TransplantOn: in.TransplantOn,
		HarvestFrom: in.HarvestFrom, HarvestTo: in.HarvestTo,
		Status: PlantingPlanned, PlantedOn: in.PlantedOn, Rootstock: in.Rootstock, NotesMD: in.NotesMD,
	}
	// Any planned date the caller sent explicitly is flagged manual and survives
	// every later re-anchor. That flag is the user saying "I have decided this".
	p.ManualDates = manualDatesOf(in)

	normalizePlantingSize(&p)
	if err := validatePlantingShape(p); err != nil {
		return Planting{}, err
	}

	var out Planting
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		seasonID, season, err := s.resolveSeason(ctx, tx, in.SeasonID, in.SeasonYear)
		if err != nil {
			return err
		}
		p.SeasonID = seasonID
		if err := s.requireOpenSeason(ctx, tx, p.SeasonID); err != nil {
			return err
		}
		eff, err := s.store.EffectiveFor(ctx, tx, p.PlantID, p.VarietyID)
		if err != nil {
			return mapNotFound(err)
		}
		if p.BedID != nil {
			if _, err := s.store.GetBed(ctx, tx, *p.BedID); err != nil {
				return mapNotFound(err)
			}
		}
		// Planned dates default from the resolved windows; a window with no
		// anchor leaves its date unset rather than guessing.
		PlannedDates(&p, eff, season.Anchors())
		now := nowUTC()
		p.CreatedAt, p.UpdatedAt = now, now
		if err := s.store.InsertPlanting(ctx, tx, p, reqctx.ActorID(ctx)); err != nil {
			return err
		}
		counts, err := s.regeneratePlanting(ctx, tx, p)
		if err != nil {
			return err
		}
		out = p
		return s.record(ctx, tx, "planting.create", "garden_planting", p.ID,
			"Naplánována výsadba "+eff.PlantName+bedSuffix(ctx, s, tx, p.BedID),
			plantingChanges(nil, p), counts)
	})
	if err != nil {
		return Planting{}, err
	}
	s.notifyPlan(ctx, out)
	return s.GetPlanting(ctx, out.ID)
}

// resolveSeason accepts either the season id or the year, because the planner
// knows the year from its URL and the API keys seasons by it (D128).
func (s *Service) resolveSeason(ctx context.Context, tx DBTX, seasonID *string, year *int) (*string, Season, error) {
	if seasonID != nil && *seasonID != "" {
		var se Season
		row := tx.QueryRowContext(ctx, `SELECT `+seasonCols+` FROM garden_seasons WHERE id = ?`, *seasonID)
		se, err := scanSeason(row)
		if err == sql.ErrNoRows {
			return nil, Season{}, httpx.ErrNotFound("Sezóna nebyla nalezena.")
		}
		return seasonID, se, err
	}
	if year != nil {
		se, err := s.store.SeasonByYear(ctx, tx, *year)
		if err != nil {
			return nil, Season{}, mapNotFound(err)
		}
		return sp(se.ID), se, nil
	}
	// Neither ⇒ a PERMANENT planting (D106). Not an error, and not a second
	// table — a tree is a planting with no season.
	return nil, Season{}, nil
}

func manualDatesOf(in PlantingInput) []string {
	var out []string
	for _, f := range []struct {
		name  string
		value *string
	}{
		{"sow_indoor_on", in.SowIndoorOn}, {"sow_direct_on", in.SowDirectOn},
		{"transplant_on", in.TransplantOn}, {"harvest_from", in.HarvestFrom}, {"harvest_to", in.HarvestTo},
	} {
		if f.value != nil && *f.value != "" {
			out = append(out, f.name)
		}
	}
	return out
}

// normalizePlantingSize stores a non-positive size as NULL rather than as a
// literal zero.
//
// The table requires EXACTLY ONE of area_m2 / plant_count, and the check below
// tests both for `> 0` — so a zero sent alongside the other one would read as
// "not sized this way" here and as "sized both ways" to SQLite, turning an
// ordinary bad input into a CHECK-constraint 500 instead of the Czech 422 every
// other malformed body gets. Zero is not a size, so it is simply not stored.
func normalizePlantingSize(p *Planting) {
	if p.AreaM2 != nil && *p.AreaM2 <= 0 {
		p.AreaM2 = nil
	}
	if p.PlantCount != nil && *p.PlantCount <= 0 {
		p.PlantCount = nil
	}
}

func validatePlantingShape(p Planting) error {
	hasArea := p.AreaM2 != nil && *p.AreaM2 > 0
	hasCount := p.PlantCount != nil && *p.PlantCount > 0
	if hasArea == hasCount {
		return errUnprocessable("Zadejte buď plochu, nebo počet rostlin — právě jedno z toho.")
	}
	for _, d := range []*string{p.SowIndoorOn, p.SowDirectOn, p.TransplantOn, p.HarvestFrom, p.HarvestTo,
		p.SowedOn, p.TransplantedOn, p.FirstHarvestOn, p.ClearedOn, p.PlantedOn, p.RemovedOn} {
		if d != nil && *d != "" {
			if _, err := dates.Parse(*d); err != nil {
				return errUnprocessable("Datum musí být ve tvaru RRRR-MM-DD.")
			}
		}
	}
	if p.Status != "" && !Valid(EnumPlantingStatus, p.Status) {
		return errUnprocessable("Neznámý stav výsadby.")
	}
	return nil
}

func (s *Service) UpdatePlanting(ctx context.Context, id string, in PlantingInput) (Planting, error) {
	var out Planting
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		before, err := s.store.GetPlanting(ctx, tx, id)
		if err != nil {
			return mapNotFound(err)
		}
		if err := s.requireOpenSeason(ctx, tx, before.SeasonID); err != nil {
			return err
		}
		after := before
		applyPlantingInput(&after, in)
		if err := validatePlantingShape(after); err != nil {
			return err
		}
		// The same bed guard CreatePlanting applies: soft delete keeps the row, so
		// the FK alone would happily move a planting into a deleted bed — where it
		// vanishes from every bed-filtered view while the checks silently skip it.
		// Only a CHANGED bed is checked, so editing an unrelated field on a
		// planting whose bed was deleted underneath it still saves.
		if after.BedID != nil && (before.BedID == nil || *after.BedID != *before.BedID) {
			if _, err := s.store.GetBed(ctx, tx, *after.BedID); err != nil {
				return mapNotFound(err)
			}
		}
		after.UpdatedAt = nowUTC()
		if err := s.store.UpdatePlanting(ctx, tx, after); err != nil {
			return err
		}
		out = after

		// ACTUAL DATES NEVER RE-DRIVE PLANNED ONES (D119). Regeneration is
		// triggered by the PLAN changing — a planned date, the crop, the variety
		// or the quantity — not by reality catching up with it.
		var counts map[string]any
		if planChanged(before, after) {
			if counts, err = s.regeneratePlanting(ctx, tx, after); err != nil {
				return err
			}
		}
		return s.record(ctx, tx, "planting.update", "garden_planting", id,
			"Upravena výsadba "+after.PlantName, plantingChanges(&before, after), counts)
	})
	if err != nil {
		return Planting{}, err
	}
	s.notifyPlan(ctx, out)
	return s.GetPlanting(ctx, id)
}

func applyPlantingInput(p *Planting, in PlantingInput) {
	if in.BedID != nil {
		p.BedID = emptyToNil(in.BedID)
	}
	if in.LocationLabel != nil {
		p.LocationLabel = in.LocationLabel
	}
	if in.VarietyID != nil {
		p.VarietyID = emptyToNil(in.VarietyID)
	}
	if in.AreaM2 != nil {
		p.AreaM2 = in.AreaM2
		if *in.AreaM2 > 0 {
			p.PlantCount = nil
		}
	}
	if in.PlantCount != nil {
		p.PlantCount = in.PlantCount
		if *in.PlantCount > 0 {
			p.AreaM2 = nil
		}
	}
	normalizePlantingSize(p)
	if in.Rows != nil {
		p.Rows = in.Rows
	}
	// A planned date sent by hand joins manual_dates and is then skipped by every
	// later re-anchor.
	for _, f := range []struct {
		name   string
		value  *string
		target **string
	}{
		{"sow_indoor_on", in.SowIndoorOn, &p.SowIndoorOn},
		{"sow_direct_on", in.SowDirectOn, &p.SowDirectOn},
		{"transplant_on", in.TransplantOn, &p.TransplantOn},
		{"harvest_from", in.HarvestFrom, &p.HarvestFrom},
		{"harvest_to", in.HarvestTo, &p.HarvestTo},
	} {
		if f.value == nil {
			continue
		}
		*f.target = emptyToNil(f.value)
		// A DATE ONLY COUNTS AS MANUAL WHILE THERE IS ONE. Clearing the field is
		// the user handing the date back to the planner, so it also un-pins it —
		// otherwise "" would flag the field decided-forever and every later
		// re-anchor would skip it, leaving it unset for good with no request that
		// could restore it.
		if *f.target == nil {
			p.ManualDates = removeStr(p.ManualDates, f.name)
		} else if !containsStr(p.ManualDates, f.name) {
			p.ManualDates = append(p.ManualDates, f.name)
		}
	}
	for _, f := range []struct {
		value  *string
		target **string
	}{
		{in.SowedOn, &p.SowedOn}, {in.TransplantedOn, &p.TransplantedOn},
		{in.FirstHarvestOn, &p.FirstHarvestOn}, {in.ClearedOn, &p.ClearedOn},
		{in.PlantedOn, &p.PlantedOn}, {in.RemovedOn, &p.RemovedOn},
		{in.Rootstock, &p.Rootstock}, {in.NotesMD, &p.NotesMD}, {in.FailReason, &p.FailReason},
	} {
		if f.value != nil {
			*f.target = emptyToNil(f.value)
		}
	}
	if in.Status != nil && *in.Status != "" {
		p.Status = *in.Status
	}
	p.Occupancy = p.OccupancyWire()
}

// planChanged reports whether an edit touched anything the generator derives
// from.
//
// D119 says an actual date never re-drives a PLANNED one, and it still does not:
// regeneration re-runs GenerateFor, which reads the planting's own dates, and
// never calls PlannedDates. But ONE derived row — the monthly pest_check series
// — hangs off OccupancyWindow, and that window is field-aware: it prefers the
// actual sowing/transplant date at the front and the actual clearing date at the
// back. Leaving the actual dates out of this list therefore meant that pulling a
// crop in June left its pest checks scheduled into October, open, and on their
// way into Zmeškané for a bed that is empty — the calendar quietly offering work
// that cannot be done, which is exactly what D110 exists to prevent.
func planChanged(a, b Planting) bool {
	return !sameStrPtr(a.SowIndoorOn, b.SowIndoorOn) ||
		!sameStrPtr(a.SowDirectOn, b.SowDirectOn) ||
		!sameStrPtr(a.TransplantOn, b.TransplantOn) ||
		!sameStrPtr(a.HarvestFrom, b.HarvestFrom) ||
		!sameStrPtr(a.HarvestTo, b.HarvestTo) ||
		!sameStrPtr(a.VarietyID, b.VarietyID) ||
		!sameStrPtr(a.BedID, b.BedID) ||
		!sameFloatPtr(a.AreaM2, b.AreaM2) ||
		!sameIntPtr(a.PlantCount, b.PlantCount) ||
		// The occupancy-bearing actual dates, and only those.
		!sameStrPtr(a.SowedOn, b.SowedOn) ||
		!sameStrPtr(a.TransplantedOn, b.TransplantedOn) ||
		!sameStrPtr(a.PlantedOn, b.PlantedOn) ||
		!sameStrPtr(a.ClearedOn, b.ClearedOn) ||
		!sameStrPtr(a.RemovedOn, b.RemovedOn)
}

func (s *Service) DeletePlanting(ctx context.Context, id string) error {
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		before, err := s.store.GetPlanting(ctx, tx, id)
		if err != nil {
			return mapNotFound(err)
		}
		if err := s.requireOpenSeason(ctx, tx, before.SeasonID); err != nil {
			return err
		}
		at := nowUTC()
		if err := s.store.SoftDeletePlanting(ctx, tx, id, at); err != nil {
			return err
		}
		// The generated work goes with it: a task for a planting that no longer
		// exists is work nobody can do.
		tasks, err := s.store.TasksForPlanting(ctx, tx, id)
		if err != nil {
			return err
		}
		for _, t := range tasks {
			if t.IsGenerated && t.Status == TaskOpen {
				if err := s.store.SoftDeleteTask(ctx, tx, t.ID, at); err != nil {
					return err
				}
			}
		}
		return s.record(ctx, tx, "planting.delete", "garden_planting", id,
			"Smazána výsadba "+before.PlantName, nil, nil)
	})
	if err != nil {
		return err
	}
	s.notify(ctx, "garden_planting.changed", map[string]any{"id": id})
	return nil
}

// ShiftTasks moves a planting's REMAINING OPEN work by a number of days and
// marks each moved task is_edited.
//
// That flag is the point: it is the user saying "I have decided this", and D110
// then protects those tasks from regeneration permanently. It is the one action
// offered against the drift line, and the reason actual dates can safely leave
// the plan alone (D119).
func (s *Service) ShiftTasks(ctx context.Context, plantingID string, days int, kinds []string) ([]Task, error) {
	if days == 0 {
		return nil, errUnprocessable("Posun musí být nenulový počet dní.")
	}
	var moved []string
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		p, err := s.store.GetPlanting(ctx, tx, plantingID)
		if err != nil {
			return mapNotFound(err)
		}
		if err := s.requireOpenSeason(ctx, tx, p.SeasonID); err != nil {
			return err
		}
		tasks, err := s.store.TasksForPlanting(ctx, tx, plantingID)
		if err != nil {
			return err
		}
		at := nowUTC()
		for _, t := range tasks {
			if t.Status != TaskOpen || t.Suppressed {
				continue
			}
			if len(kinds) > 0 && !containsStr(kinds, t.Kind) {
				continue
			}
			from, err := dates.Parse(t.WindowFrom)
			if err != nil {
				continue
			}
			to, err := dates.Parse(t.WindowTo)
			if err != nil {
				continue
			}
			t.WindowFrom, t.WindowTo = from.AddDays(days).String(), to.AddDays(days).String()
			if t.DueHint != nil {
				if due, err := dates.Parse(*t.DueHint); err == nil {
					t.DueHint = sp(due.AddDays(days).String())
				}
			}
			t.IsEdited = true
			if err := s.store.UpdateTask(ctx, tx, t, at); err != nil {
				return err
			}
			moved = append(moved, t.ID)
		}
		return s.record(ctx, tx, "task.update", "garden_planting", plantingID,
			fmt.Sprintf("Posunuty navazující práce u %s o %s", p.PlantName, daysCS(abs(days))),
			nil, map[string]any{"moved": len(moved), "days": days})
	})
	if err != nil {
		return nil, err
	}
	s.notify(ctx, "garden_task.changed", map[string]any{"planting_id": plantingID})
	page, err := s.ListTasks(ctx, TaskFilter{PlantingID: plantingID}, maxLimit, "")
	return page.Items, err
}

// ============================ regeneration ============================

// regeneratePlanting applies the generator's plan for one planting, inside the
// caller's transaction, and returns the {created, moved, left} counts for the
// audit event's meta — so the Log can answer "what did that edit do to my
// calendar", including the part that is "nothing, on purpose".
func (s *Service) regeneratePlanting(ctx context.Context, tx *sql.Tx, p Planting) (map[string]any, error) {
	eff, err := s.store.EffectiveFor(ctx, tx, p.PlantID, p.VarietyID)
	if err != nil {
		return nil, err
	}
	var anchors SeasonAnchors
	if p.SeasonID != nil {
		row := tx.QueryRowContext(ctx, `SELECT `+seasonCols+` FROM garden_seasons WHERE id = ?`, *p.SeasonID)
		se, err := scanSeason(row)
		if err != nil {
			return nil, err
		}
		anchors = se.Anchors()
	} else if p.PlantedOn != nil {
		// A permanent has no season, so its yearly care anchors on the CURRENT
		// year rather than on nothing.
		anchors = SeasonAnchors{Year: s.today().Y}
	}

	bedCode := ""
	if p.BedID != nil {
		if bed, err := s.store.GetBed(ctx, tx, *p.BedID); err == nil {
			bedCode = bed.Code
		}
	}
	existing, err := s.store.TasksForPlanting(ctx, tx, p.ID)
	if err != nil {
		return nil, err
	}
	plan := GenerateFor(p, eff, anchors, bedCode, existing)
	at := nowUTC()
	pos := lexorank.First()
	for _, t := range plan.Create {
		t.ID = idgen.New()
		t.Position = pos
		pos = lexorank.Between(pos, "")
		if err := s.store.InsertTask(ctx, tx, t, reqctx.ActorID(ctx), at); err != nil {
			return nil, err
		}
	}
	for _, m := range plan.Move {
		if err := s.store.MoveTaskWindow(ctx, tx, m.ID, m.From.String(), m.To.String(), dateStr(m.DueHint), m.BedID, m.TitleCS, at); err != nil {
			return nil, err
		}
	}
	for _, id := range plan.Remove {
		if err := s.store.SoftDeleteTask(ctx, tx, id, at); err != nil {
			return nil, err
		}
	}
	return map[string]any{
		"created": len(plan.Create), "moved": len(plan.Move),
		"left": len(plan.Leave), "removed": len(plan.Remove),
	}, nil
}

// regenerateForPlant re-runs generation for every OPEN planting of one crop,
// after that crop's timing changed.
func (s *Service) regenerateForPlant(ctx context.Context, tx *sql.Tx, plantID string) error {
	// EXHAUSTIVE, not one page, and the cap here bites soonest of the three: this
	// filter spans EVERY season a crop was ever grown in, and ListPlantings orders
	// by the UUIDv7 id — so one page is the OLDEST 200, largely closed seasons the
	// loop skips below. The current season's plantings, the only ones a timing
	// change can still reach, would be exactly the rows past the cap.
	plantings, err := s.store.allPlantings(ctx, tx, PlantingFilter{PlantID: plantID})
	if err != nil {
		return err
	}
	for _, p := range plantings {
		if p.SeasonID != nil {
			var status string
			if err := tx.QueryRowContext(ctx, `SELECT status FROM garden_seasons WHERE id = ?`, *p.SeasonID).
				Scan(&status); err != nil {
				return err
			}
			if status == SeasonClosed {
				continue // history is not re-derived
			}
		}
		// Re-anchoring the planted dates first is what makes a timing change
		// actually reach the calendar; PlannedDates skips whatever the user typed.
		eff, err := s.store.EffectiveFor(ctx, tx, p.PlantID, p.VarietyID)
		if err != nil {
			return err
		}
		updated := p
		// ONLY A SEASONAL PLANTING IS RE-ANCHORED. A permanent has no season and
		// therefore no sowing cycle: CreatePlanting resolves it against the zero
		// Season and deliberately leaves every planned date unset (D106). Handing
		// it the current YEAR here instead would resolve the crop's week-anchored
		// windows and write a sow_direct_on onto a rhubarb crown that has been in
		// the ground for three years — and the generator would then produce a
		// sowing and a bed-prep task for it. Its yearly care still updates:
		// regeneratePlanting anchors permanents itself, below.
		if p.SeasonID != nil {
			row := tx.QueryRowContext(ctx, `SELECT `+seasonCols+` FROM garden_seasons WHERE id = ?`, *p.SeasonID)
			se, err := scanSeason(row)
			if err != nil {
				return err
			}
			PlannedDates(&updated, eff, se.Anchors())
			updated.UpdatedAt = nowUTC()
			if err := s.store.UpdatePlanting(ctx, tx, updated); err != nil {
				return err
			}
		}
		if _, err := s.regeneratePlanting(ctx, tx, updated); err != nil {
			return err
		}
	}
	return nil
}

// notifyPlan pushes the three caches a planting change invalidates together.
// A stale check is worse than no check, so the plan, its check and the tasks
// always move as one.
func (s *Service) notifyPlan(ctx context.Context, p Planting) {
	payload := map[string]any{"id": p.ID}
	if p.SeasonYear != nil {
		payload["year"] = *p.SeasonYear
	}
	s.notify(ctx, "garden_planting.changed", payload)
}

// ============================== helpers ==============================

func emptyToNil(v *string) *string {
	if v == nil || *v == "" {
		return nil
	}
	return v
}

// removeStr returns list without v, preserving order.
func removeStr(list []string, v string) []string {
	out := list[:0:0]
	for _, s := range list {
		if s != v {
			out = append(out, s)
		}
	}
	return out
}

func firstStr(candidates ...*string) *string {
	for _, c := range candidates {
		if c != nil && *c != "" {
			return c
		}
	}
	return nil
}

func orKeep(in, current *string) *string {
	if in != nil && *in != "" {
		return in
	}
	return current
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func bedSuffix(ctx context.Context, s *Service, tx DBTX, bedID *string) string {
	if bedID == nil {
		return ""
	}
	bed, err := s.store.GetBed(ctx, tx, *bedID)
	if err != nil {
		return ""
	}
	return " (" + bed.Code + ")"
}

// plantingChanges is the field diff for garden_planting. It is in the diff set
// because "who moved the tomato transplant date and to what" is exactly the
// question the Log exists to answer here.
func plantingChanges(before *Planting, after Planting) []audit.Change {
	var changes []audit.Change
	var b Planting
	if before != nil {
		b = *before
	}
	diffStr(&changes, "bed_id", b.BedID, after.BedID)
	diffStr(&changes, "variety_id", b.VarietyID, after.VarietyID)
	diffFloat(&changes, "area_m2", b.AreaM2, after.AreaM2)
	if !sameIntPtr(b.PlantCount, after.PlantCount) {
		changes = append(changes, audit.Change{Field: "plant_count", Old: fmtInt(b.PlantCount), New: fmtInt(after.PlantCount)})
	}
	for _, f := range []struct {
		field string
		o, n  *string
	}{
		{"sow_indoor_on", b.SowIndoorOn, after.SowIndoorOn},
		{"sow_direct_on", b.SowDirectOn, after.SowDirectOn},
		{"transplant_on", b.TransplantOn, after.TransplantOn},
		{"harvest_from", b.HarvestFrom, after.HarvestFrom},
		{"harvest_to", b.HarvestTo, after.HarvestTo},
		{"sowed_on", b.SowedOn, after.SowedOn},
		{"transplanted_on", b.TransplantedOn, after.TransplantedOn},
		{"first_harvest_on", b.FirstHarvestOn, after.FirstHarvestOn},
		{"cleared_on", b.ClearedOn, after.ClearedOn},
		{"fail_reason", b.FailReason, after.FailReason},
	} {
		diffStr(&changes, f.field, f.o, f.n)
	}
	diffPlain(&changes, "status", b.Status, after.Status)
	return changes
}

// sortTasksForDisplay orders a task list the way every surface shows it:
// overdue first, then by window start. Kept here so the widget, the calendar and
// the print sheet cannot disagree.
func sortTasksForDisplay(tasks []Task, today dates.Date) {
	sort.SliceStable(tasks, func(i, j int) bool {
		oi, oj := tasks[i].Overdue, tasks[j].Overdue
		if oi != oj {
			return oi
		}
		if tasks[i].WindowFrom != tasks[j].WindowFrom {
			return tasks[i].WindowFrom < tasks[j].WindowFrom
		}
		return tasks[i].Position < tasks[j].Position
	})
}
