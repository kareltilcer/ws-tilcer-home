package garden

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/dates"
	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/db"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/idgen"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/lexorank"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
)

// Notifier publishes a websocket change after commit.
type Notifier func(ctx context.Context, typ string, payload any)

// Options carries the module's configuration.
type Options struct {
	Location       *time.Location
	WeatherEnabled bool
	WeatherURL     string
	WeatherPoll    time.Duration
	ImportMaxBytes int64
	Logger         *slog.Logger
}

// Service orchestrates garden mutations: validate → WithTx (row change + audit
// event in ONE transaction) → notify after commit. The same shape every other
// module uses.
//
// Two things are specific to this module and both are enforced here rather than
// per handler: CLOSED SEASONS ARE FROZEN (one guard, called from every write),
// and a planting change RE-RUNS TASK GENERATION under the D110 rules.
type Service struct {
	db     *sql.DB
	store  *Store
	sink   audit.Sink
	notify Notifier
	opts   Options
	logger *slog.Logger
}

func NewService(db *sql.DB, sink audit.Sink, notify Notifier, opts Options) *Service {
	if notify == nil {
		notify = func(context.Context, string, any) {}
	}
	if opts.Location == nil {
		opts.Location = time.UTC
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.ImportMaxBytes <= 0 {
		opts.ImportMaxBytes = 1 << 20
	}
	return &Service{db: db, store: NewStore(db), sink: sink, notify: notify, opts: opts, logger: opts.Logger}
}

// Store exposes the read store to this module's widget, metric and list
// providers.
func (s *Service) Store() *Store { return s.store }

// Options exposes the configuration the weather job and the settings read model
// need.
func (s *Service) Options() Options { return s.opts }

func (s *Service) today() dates.Date { return dates.Today(s.opts.Location) }

func actorID(ctx context.Context) string {
	if a, ok := reqctx.ActorFrom(ctx); ok {
		return a.UserID
	}
	return ""
}

// record writes one audit event through the spine, inside the caller's tx. The
// error is returned unchanged so the transaction rolls back: an action that
// succeeds unlogged is the bug the spine exists to prevent.
func (s *Service) record(ctx context.Context, tx *sql.Tx, action, entityType, entityID, summary string, changes []audit.Change, meta map[string]any) error {
	_, err := s.sink.Record(ctx, tx, audit.Event{
		Module:     audit.ModuleGarden,
		Action:     action,
		EntityType: entityType,
		EntityID:   entityID,
		Summary:    summary,
		Meta:       meta,
		Changes:    changes,
	})
	return err
}

func mapNotFound(err error) error {
	if err == errNotFound {
		return httpx.ErrNotFound("Záznam nebyl nalezen.")
	}
	return err
}

// ================= the closed-season guard (one helper) =================

// requireOpenSeason refuses a write against a closed season (FR-G10).
//
// It is ONE helper called from every mutating path rather than a check
// remembered per handler, because the failure mode of the other approach is
// silent: a route added later simply would not have it, and the rotation history
// the checks depend on would start drifting under them.
func (s *Service) requireOpenSeason(ctx context.Context, db DBTX, seasonID *string) error {
	if seasonID == nil {
		return nil // a permanent belongs to no season and is never frozen
	}
	var status string
	err := db.QueryRowContext(ctx, `SELECT status FROM garden_seasons WHERE id = ?`, *seasonID).Scan(&status)
	if err == sql.ErrNoRows {
		return httpx.ErrNotFound("Sezóna nebyla nalezena.")
	}
	if err != nil {
		return err
	}
	if status == SeasonClosed {
		return httpx.ErrConflict("Sezóna je uzavřená. Nejdřív ji musí správce znovu otevřít.")
	}
	return nil
}

// ============================== plants ==============================

// PlantPage is the paged read model.
type PlantPage struct {
	Items      []Plant `json:"items"`
	NextCursor *string `json:"next_cursor"`
}

func (s *Service) ListPlants(ctx context.Context, f PlantFilter, limit int, cursor string) (PlantPage, error) {
	items, next, err := s.store.ListPlants(ctx, f, NormalizeLimit(limit), cursor)
	if err != nil {
		return PlantPage{}, err
	}
	if items == nil {
		items = []Plant{}
	}
	return PlantPage{Items: items, NextCursor: next}, nil
}

func (s *Service) GetPlant(ctx context.Context, id string) (Plant, error) {
	p, err := s.store.GetPlant(ctx, nil, id)
	if err != nil {
		return Plant{}, mapNotFound(err)
	}
	var n int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM garden_varieties WHERE plant_id = ? AND deleted_at IS NULL`, id).Scan(&n); err != nil {
		return Plant{}, err
	}
	p.VarietyCount = n
	return p, nil
}

// presentFields records which top-level JSON keys a body actually carried.
//
// It is what makes "omitted" and "explicitly null" distinguishable at all: both
// decode to a nil pointer, so without the key set a PATCH can set and change a
// knowledge-base field but never CLEAR one — and a rotation_break_years or
// hardening_days entered by mistake drives check C3 and the harden-off task with
// no way back short of deleting the crop.
type presentFields map[string]bool

// decodePatch unmarshals a body into dst while recording its top-level keys.
//
// It re-applies DisallowUnknownFields itself: a custom UnmarshalJSON is handed
// the raw value and would otherwise switch off the unknown-field rejection
// httpx.DecodeJSON asked for one level up.
func decodePatch(b []byte, dst any) (presentFields, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return nil, err
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(b, &keys); err != nil {
		return nil, err
	}
	present := make(presentFields, len(keys))
	for k := range keys {
		present[k] = true
	}
	return present, nil
}

// PlantInput is the create/update body. Pointers distinguish "omitted" from
// "explicitly null" on update — see `present`, which is the half that actually
// carries the distinction.
type PlantInput struct {
	PlantCore
	NameCS    *string `json:"name_cs"`
	Family    *string `json:"family"`
	PlantType *string `json:"plant_type"`
	Hardiness *string `json:"hardiness"`
	Verified  *bool   `json:"verified"`

	// present is the decoded body's key set. Unexported, so it is invisible to
	// the encoder and to DisallowUnknownFields, and nil for an input built in Go
	// (a test, the importer) — which then merges by inheritance, as before.
	present presentFields
}

func (in *PlantInput) UnmarshalJSON(b []byte) error {
	type alias PlantInput
	var a alias
	present, err := decodePatch(b, &a)
	if err != nil {
		return err
	}
	*in = PlantInput(a)
	in.present = present
	return nil
}

func (s *Service) CreatePlant(ctx context.Context, in PlantInput) (Plant, error) {
	p := Plant{PlantCore: in.PlantCore}
	if in.NameCS != nil {
		p.NameCS = strings.TrimSpace(*in.NameCS)
	}
	p.Family, p.PlantType, p.Hardiness = derefS(in.Family), derefS(in.PlantType), derefS(in.Hardiness)
	if err := validatePlant(p); err != nil {
		return Plant{}, err
	}

	now := nowUTC()
	p.ID = idgen.New()
	p.CreatedAt, p.UpdatedAt = now, now
	p.CreatedBy = sp(actorID(ctx))
	if p.Provenance.Source == "" {
		p.Provenance.Source = SourceManual
	}

	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		if err := s.store.InsertPlant(ctx, tx, p); err != nil {
			return duplicateName(err)
		}
		return s.record(ctx, tx, "plant.create", "garden_plant", p.ID,
			"Přidána plodina "+p.NameCS, plantChanges(nil, p), nil)
	})
	if err != nil {
		return Plant{}, err
	}
	s.notify(ctx, "garden_plant.changed", map[string]any{"id": p.ID})
	return p, nil
}

func (s *Service) UpdatePlant(ctx context.Context, id string, in PlantInput) (Plant, error) {
	var out Plant
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		before, err := s.store.GetPlant(ctx, tx, id)
		if err != nil {
			return mapNotFound(err)
		}
		after := before
		after.PlantCore = mergeCorePatch(before.PlantCore, &in.PlantCore, in.present)
		if in.NameCS != nil {
			after.NameCS = strings.TrimSpace(*in.NameCS)
		}
		if in.Family != nil {
			after.Family = *in.Family
		}
		if in.PlantType != nil {
			after.PlantType = *in.PlantType
		}
		if in.Hardiness != nil {
			after.Hardiness = *in.Hardiness
		}
		// Verifying is what clears the "neověřeno" badge (D114). It is a distinct
		// act from editing: a member is saying they checked the numbers, so it
		// stamps who and when rather than just touching updated_at.
		if in.Verified != nil {
			if *in.Verified {
				after.Provenance.VerifiedBy = sp(actorID(ctx))
				after.Provenance.VerifiedAt = sp(nowUTC())
			} else {
				after.Provenance.VerifiedBy, after.Provenance.VerifiedAt = nil, nil
			}
		}
		if err := validatePlant(after); err != nil {
			return err
		}
		after.UpdatedAt = nowUTC()
		if err := s.store.UpdatePlant(ctx, tx, after); err != nil {
			return duplicateName(err)
		}
		out = after

		changes := plantChanges(&before, after)
		if err := s.record(ctx, tx, "plant.update", "garden_plant", id,
			"Upravena plodina "+after.NameCS, changes, nil); err != nil {
			return err
		}
		// A timing change re-anchors every open planting that uses this crop
		// (FR-G7's third trigger): the plan was derived from these windows, so
		// leaving it alone would make the calendar quietly wrong.
		if timingChanged(before.PlantCore, after.PlantCore) {
			return s.regenerateForPlant(ctx, tx, id)
		}
		return nil
	})
	if err != nil {
		return Plant{}, err
	}
	s.notify(ctx, "garden_plant.changed", map[string]any{"id": id})
	return out, nil
}

func (s *Service) DeletePlant(ctx context.Context, id string) error {
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		before, err := s.store.GetPlant(ctx, tx, id)
		if err != nil {
			return mapNotFound(err)
		}
		n, err := s.store.PlantingsUsingPlant(ctx, tx, id)
		if err != nil {
			return err
		}
		if n > 0 {
			return httpx.ErrConflict("Plodina je použitá ve " + strconv.Itoa(n) + " výsadbách. Nejdřív je smažte.")
		}
		at := nowUTC()
		if err := s.store.SoftDeletePlant(ctx, tx, id, at); err != nil {
			return err
		}
		return s.record(ctx, tx, "plant.delete", "garden_plant", id, "Smazána plodina "+before.NameCS, nil, nil)
	})
	if err != nil {
		return err
	}
	s.notify(ctx, "garden_plant.changed", map[string]any{"id": id})
	return nil
}

func validatePlant(p Plant) error {
	if strings.TrimSpace(p.NameCS) == "" {
		return errUnprocessable("Název plodiny je povinný.")
	}
	// family and hardiness are required because the rotation engine joins on the
	// first and the frost logic reads the second (D104, D111). A crop missing
	// either is one this module cannot reason about, so it is refused rather than
	// defaulted to something plausible.
	if !Valid(EnumFamily, p.Family) {
		return errUnprocessable("Čeleď je povinná — bez ní nelze kontrolovat střídání plodin.")
	}
	if !Valid(EnumHardiness, p.Hardiness) {
		return errUnprocessable("Odolnost proti mrazu je povinná — bez ní nelze upozornit na mráz.")
	}
	if !Valid(EnumPlantType, p.PlantType) {
		return errUnprocessable("Typ plodiny je povinný.")
	}
	return validateCore(p.PlantCore, true)
}

// validateCore checks the shared knowledge-base fields. requireUnit is true for
// a species (which must state one) and false for a variety (which inherits).
func validateCore(c PlantCore, requireUnit bool) error {
	for _, e := range []struct {
		name, enum string
		value      *string
	}{
		{"Nárok na živiny", EnumFeederClass, c.FeederClass},
		{"Hloubka kořenů", EnumRootDepth, c.RootDepth},
		{"Osvit", EnumSun, c.Sun},
		{"Nárok na vodu", EnumWaterNeed, c.WaterNeed},
		{"Způsob výsevu", EnumSowMethod, c.SowMethod},
		{"Jednotka sklizně", EnumHarvestUnit, c.HarvestUnit},
	} {
		if e.value != nil && !Valid(e.enum, *e.value) {
			return errUnprocessable(e.name + ": neznámá hodnota „" + *e.value + "“.")
		}
	}
	if requireUnit && (c.HarvestUnit == nil || *c.HarvestUnit == "") {
		return errUnprocessable("Jednotka sklizně je povinná.")
	}
	for _, m := range c.StorageMethods {
		if !Valid(EnumStorageMethod, m) {
			return errUnprocessable("Způsob uskladnění: neznámá hodnota „" + m + "“.")
		}
	}
	for _, w := range []struct {
		label string
		win   *Window
	}{
		{"Termín výsevu do sadbovačů", c.WinSowIndoor},
		{"Termín přímého výsevu", c.WinSowDirect},
		{"Termín výsadby", c.WinTransplant},
		{"Termín sklizně", c.WinHarvest},
	} {
		if w.win == nil {
			continue
		}
		if err := w.win.Validate(w.label); err != nil {
			return err
		}
	}
	for _, m := range []struct {
		label string
		value *float64
	}{
		{"Hloubka výsevu", c.SowDepthCM}, {"Rozestup řádků", c.SpacingRowCM},
		{"Rozestup rostlin", c.SpacingPlantCM}, {"Rostlin na m²", c.PlantsPerM2},
		{"Výnos na m²", c.YieldPerM2}, {"Výnos na rostlinu", c.YieldPerPlant},
	} {
		if m.value != nil && *m.value < 0 {
			return errUnprocessable(m.label + " nemůže být záporná.")
		}
	}
	if c.DaysToGerminateMin != nil && c.DaysToGerminateMax != nil && *c.DaysToGerminateMin > *c.DaysToGerminateMax {
		return errUnprocessable("Doba klíčení: dolní mez nemůže být větší než horní.")
	}
	if c.DaysToMaturityMin != nil && c.DaysToMaturityMax != nil && *c.DaysToMaturityMin > *c.DaysToMaturityMax {
		return errUnprocessable("Doba do sklizně: dolní mez nemůže být větší než horní.")
	}
	return nil
}

func duplicateName(err error) error {
	if err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed") {
		return httpx.ErrConflict("Plodina nebo odrůda s tímto názvem už existuje.")
	}
	return err
}

// timingChanged reports whether a crop edit moved anything a plan was derived
// from — the four windows plus the two spans the derived tasks hang off.
func timingChanged(a, b PlantCore) bool {
	if !sameWindow(a.WinSowIndoor, b.WinSowIndoor) ||
		!sameWindow(a.WinSowDirect, b.WinSowDirect) ||
		!sameWindow(a.WinTransplant, b.WinTransplant) ||
		!sameWindow(a.WinHarvest, b.WinHarvest) {
		return true
	}
	return !sameIntPtr(a.HardeningDays, b.HardeningDays) ||
		!sameIntPtr(a.DaysToGerminateMin, b.DaysToGerminateMin) ||
		!sameIntPtr(a.DaysToGerminateMax, b.DaysToGerminateMax) ||
		!sameStrPtr(a.FeederClass, b.FeederClass) ||
		!sameBoolPtr(a.NeedsPrickingOut, b.NeedsPrickingOut) ||
		!sameBoolPtr(a.NeedsSupport, b.NeedsSupport) ||
		!sameBoolPtr(a.WantsMulch, b.WantsMulch) ||
		!sameBoolPtr(a.WantsPestCheck, b.WantsPestCheck)
}

func sameWindow(a, b *Window) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// ============================= varieties =============================

type VarietyPage struct {
	Items      []Variety `json:"items"`
	NextCursor *string   `json:"next_cursor"`
}

func (s *Service) ListVarieties(ctx context.Context, plantID string, limit int, cursor string) (VarietyPage, error) {
	plant, err := s.store.GetPlant(ctx, nil, plantID)
	if err != nil {
		return VarietyPage{}, mapNotFound(err)
	}
	items, next, err := s.store.ListVarieties(ctx, plantID, NormalizeLimit(limit), cursor)
	if err != nil {
		return VarietyPage{}, err
	}
	for i := range items {
		eff := Resolve(plant, &items[i])
		items[i].Effective = &eff
	}
	if items == nil {
		items = []Variety{}
	}
	return VarietyPage{Items: items, NextCursor: next}, nil
}

func (s *Service) GetVariety(ctx context.Context, id string) (Variety, error) {
	v, err := s.store.GetVariety(ctx, nil, id)
	if err != nil {
		return Variety{}, mapNotFound(err)
	}
	plant, err := s.store.GetPlant(ctx, nil, v.PlantID)
	if err != nil {
		return Variety{}, mapNotFound(err)
	}
	eff := Resolve(plant, &v)
	v.Effective = &eff
	return v, nil
}

// VarietyInput is the create/update body. `present` carries the same
// null-vs-omitted distinction as PlantInput's, and for the same reason — a
// variety override entered by mistake must be removable, not just changeable.
type VarietyInput struct {
	PlantCore
	Name          *string `json:"name"`
	Supplier      *string `json:"supplier"`
	DescriptionMD *string `json:"description_md"`
	IsFavourite   *bool   `json:"is_favourite"`
	Retired       *bool   `json:"retired"`
	Verified      *bool   `json:"verified"`

	present presentFields
}

func (in *VarietyInput) UnmarshalJSON(b []byte) error {
	type alias VarietyInput
	var a alias
	present, err := decodePatch(b, &a)
	if err != nil {
		return err
	}
	*in = VarietyInput(a)
	in.present = present
	return nil
}

func (s *Service) CreateVariety(ctx context.Context, plantID string, in VarietyInput) (Variety, error) {
	v := Variety{PlantCore: in.PlantCore, PlantID: plantID}
	if in.Name != nil {
		v.Name = strings.TrimSpace(*in.Name)
	}
	if v.Name == "" {
		return Variety{}, errUnprocessable("Název odrůdy je povinný.")
	}
	v.Supplier, v.DescriptionMD = in.Supplier, in.DescriptionMD
	v.IsFavourite, v.Retired = derefB(in.IsFavourite), derefB(in.Retired)
	// requireUnit=false: a variety INHERITS everything it does not state, which
	// is the whole of D103.
	if err := validateCore(v.PlantCore, false); err != nil {
		return Variety{}, err
	}

	now := nowUTC()
	v.ID = idgen.New()
	v.CreatedAt, v.UpdatedAt = now, now
	if v.Provenance.Source == "" {
		v.Provenance.Source = SourceManual
	}

	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		plant, err := s.store.GetPlant(ctx, tx, plantID)
		if err != nil {
			return mapNotFound(err)
		}
		if err := s.store.InsertVariety(ctx, tx, v, actorID(ctx), now); err != nil {
			return duplicateName(err)
		}
		return s.record(ctx, tx, "variety.create", "garden_variety", v.ID,
			"Přidána odrůda "+v.Name+" ("+plant.NameCS+")", nil, nil)
	})
	if err != nil {
		return Variety{}, err
	}
	s.notify(ctx, "garden_plant.changed", map[string]any{"id": plantID})
	return s.GetVariety(ctx, v.ID)
}

func (s *Service) UpdateVariety(ctx context.Context, id string, in VarietyInput) (Variety, error) {
	var plantID string
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		before, err := s.store.GetVariety(ctx, tx, id)
		if err != nil {
			return mapNotFound(err)
		}
		after := before
		after.PlantCore = mergeCorePatch(before.PlantCore, &in.PlantCore, in.present)
		if in.Name != nil {
			after.Name = strings.TrimSpace(*in.Name)
		}
		if in.Supplier != nil {
			after.Supplier = in.Supplier
		}
		if in.DescriptionMD != nil {
			after.DescriptionMD = in.DescriptionMD
		}
		if in.IsFavourite != nil {
			after.IsFavourite = *in.IsFavourite
		}
		if in.Retired != nil {
			after.Retired = *in.Retired
		}
		// Un-verifying matters as much as verifying (D114): the badge is a claim
		// that a human checked these numbers, and a claim you cannot withdraw is
		// one the next reader cannot trust. Same both ways as UpdatePlant.
		if in.Verified != nil {
			if *in.Verified {
				after.Provenance.VerifiedBy = sp(actorID(ctx))
				after.Provenance.VerifiedAt = sp(nowUTC())
			} else {
				after.Provenance.VerifiedBy, after.Provenance.VerifiedAt = nil, nil
			}
		}
		if after.Name == "" {
			return errUnprocessable("Název odrůdy je povinný.")
		}
		if err := validateCore(after.PlantCore, false); err != nil {
			return err
		}
		if err := s.store.UpdateVariety(ctx, tx, after, nowUTC()); err != nil {
			return duplicateName(err)
		}
		plantID = before.PlantID
		if err := s.record(ctx, tx, "variety.update", "garden_variety", id, "Upravena odrůda "+after.Name, nil, nil); err != nil {
			return err
		}
		// A variety-level timing change re-anchors open plantings exactly like a
		// plant-level one (FR-G7): a planting resolves its windows THROUGH the
		// variety overlay, so leaving the plan alone would make the calendar
		// quietly wrong — the same reason UpdatePlant regenerates.
		if timingChanged(before.PlantCore, after.PlantCore) {
			return s.regenerateForPlant(ctx, tx, plantID)
		}
		return nil
	})
	if err != nil {
		return Variety{}, err
	}
	s.notify(ctx, "garden_plant.changed", map[string]any{"id": plantID})
	return s.GetVariety(ctx, id)
}

func (s *Service) DeleteVariety(ctx context.Context, id string) error {
	var plantID string
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		before, err := s.store.GetVariety(ctx, tx, id)
		if err != nil {
			return mapNotFound(err)
		}
		plantID = before.PlantID
		// The same guard DeletePlant applies, for a sharper reason: a planting
		// resolves its variety through GetVariety, which skips soft-deleted rows,
		// so deleting one still in use does not merely drop the variety name —
		// it makes every planting that named it unreadable and unsavable.
		n, err := s.store.PlantingsUsingVariety(ctx, tx, id)
		if err != nil {
			return err
		}
		if n > 0 {
			return httpx.ErrConflict("Odrůda je použitá ve " + strconv.Itoa(n) + " výsadbách. Nejdřív je smažte nebo u nich odrůdu odeberte.")
		}
		at := nowUTC()
		if err := s.store.SoftDeleteVariety(ctx, tx, id, at); err != nil {
			return err
		}
		return s.record(ctx, tx, "variety.delete", "garden_variety", id, "Smazána odrůda "+before.Name, nil, nil)
	})
	if err != nil {
		return err
	}
	s.notify(ctx, "garden_plant.changed", map[string]any{"id": plantID})
	return nil
}

// ================================ beds ================================

func (s *Service) ListBeds(ctx context.Context, includeInactive bool) ([]Bed, error) {
	beds, err := s.store.ListBeds(ctx, nil, includeInactive)
	if err != nil {
		return nil, err
	}
	withNeighbours(beds)
	if beds == nil {
		beds = []Bed{}
	}
	return beds, nil
}

// withNeighbours fills the derived neighbour list — the beds immediately before
// and after each one in its zone (D117). Derived on read, never stored: the
// order IS the model, so a stored copy could only ever disagree with it.
//
// INACTIVE BEDS ARE SKIPPED, exactly as check C11's adjacentBedPairs skips them.
// The two must agree or the Záhony screen teaches the household one adjacency
// while the warning panel reports another: with A1 active, A2 resting and A3
// active, C11 pairs A1–A3, so that is the pair the cards have to name too — and
// a resting bed is nobody's neighbour.
func withNeighbours(beds []Bed) {
	byZone := map[string][]int{}
	for i, b := range beds {
		if !b.IsActive {
			continue
		}
		byZone[derefS(b.Zone)] = append(byZone[derefS(b.Zone)], i)
	}
	for _, idxs := range byZone {
		for pos, i := range idxs {
			var n []string
			if pos > 0 {
				n = append(n, beds[idxs[pos-1]].ID)
			}
			if pos+1 < len(idxs) {
				n = append(n, beds[idxs[pos+1]].ID)
			}
			beds[i].Neighbours = n
		}
	}
}

func (s *Service) GetBed(ctx context.Context, id string) (Bed, error) {
	b, err := s.store.GetBed(ctx, nil, id)
	return b, mapNotFound(err)
}

// BedInput is the create/update body.
type BedInput struct {
	Name        *string  `json:"name"`
	Code        *string  `json:"code"`
	Type        *string  `json:"type"`
	LengthCM    *float64 `json:"length_cm"`
	WidthCM     *float64 `json:"width_cm"`
	AreaM2      *float64 `json:"area_m2"`
	SunExposure *string  `json:"sun_exposure"`
	Zone        *string  `json:"zone"`
	SoilNotesMD *string  `json:"soil_notes_md"`
	IsActive    *bool    `json:"is_active"`

	// present is the decoded body's key set, for the same reason PlantInput and
	// SettingsInput carry one: the nullable numbers can only be CLEARED if
	// "omitted" and "explicitly null" are distinguishable, and both decode to a
	// nil pointer. Without it a bed measured 400×100 that turns out to be
	// irregular could be given an area directly but never have its dimensions
	// removed — and the next edit to either one would re-derive the area from the
	// ghost value. Nil for an input built in Go (a test), which then falls back to
	// "a non-nil value wins", as before the distinction existed.
	present presentFields
}

func (in *BedInput) UnmarshalJSON(b []byte) error {
	type alias BedInput
	var a alias
	present, err := decodePatch(b, &a)
	if err != nil {
		return err
	}
	*in = BedInput(a)
	in.present = present
	return nil
}

// carried reports whether the body actually contained `key`.
func (in BedInput) carried(key string, nonNil bool) bool {
	if in.present == nil {
		return nonNil
	}
	return in.present[key]
}

func (s *Service) CreateBed(ctx context.Context, in BedInput) (Bed, error) {
	b := Bed{
		Name: strings.TrimSpace(derefS(in.Name)), Code: strings.TrimSpace(derefS(in.Code)),
		Type: derefS(in.Type), LengthCM: in.LengthCM, WidthCM: in.WidthCM,
		SunExposure: in.SunExposure, Zone: in.Zone, SoilNotesMD: in.SoilNotesMD, IsActive: true,
	}
	if in.IsActive != nil {
		b.IsActive = *in.IsActive
	}
	b.AreaM2 = resolveArea(in)
	if err := validateBed(b); err != nil {
		return Bed{}, err
	}

	now := nowUTC()
	b.ID = idgen.New()
	b.CreatedAt, b.UpdatedAt = now, now

	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		pos, err := s.lastBedPosition(ctx, tx, b.Zone)
		if err != nil {
			return err
		}
		b.Position = lexorank.Between(pos, "")
		if err := s.store.InsertBed(ctx, tx, b, actorID(ctx)); err != nil {
			return err
		}
		return s.record(ctx, tx, "bed.create", "garden_bed", b.ID,
			"Založen záhon "+b.Code+" ("+b.Name+")", nil, nil)
	})
	if err != nil {
		return Bed{}, err
	}
	s.notify(ctx, "garden_bed.changed", map[string]any{"id": b.ID})
	return b, nil
}

// resolveArea derives the area from the dimensions when it was not given
// directly. Settable directly for irregular shapes, which is why it is not
// simply computed.
func resolveArea(in BedInput) float64 {
	if in.AreaM2 != nil && *in.AreaM2 > 0 {
		return *in.AreaM2
	}
	if in.LengthCM != nil && in.WidthCM != nil {
		return round2(*in.LengthCM * *in.WidthCM / 10000)
	}
	return 0
}

func validateBed(b Bed) error {
	if b.Name == "" {
		return errUnprocessable("Název záhonu je povinný.")
	}
	if b.Code == "" || len([]rune(b.Code)) > 8 {
		return errUnprocessable("Značka záhonu je povinná a nejvýš osm znaků.")
	}
	if !Valid(EnumBedType, b.Type) {
		return errUnprocessable("Typ záhonu je povinný.")
	}
	if b.AreaM2 <= 0 {
		return errUnprocessable("Plocha musí být kladná — zadejte rozměry, nebo plochu přímo.")
	}
	if b.SunExposure != nil && !Valid(EnumSun, *b.SunExposure) {
		return errUnprocessable("Osvit: neznámá hodnota.")
	}
	return nil
}

func (s *Service) lastBedPosition(ctx context.Context, db DBTX, zone *string) (string, error) {
	var pos sql.NullString
	err := db.QueryRowContext(ctx,
		`SELECT MAX(position) FROM garden_beds WHERE deleted_at IS NULL AND COALESCE(zone,'') = COALESCE(?,'')`,
		zone).Scan(&pos)
	if err != nil {
		return "", err
	}
	return pos.String, nil
}

func (s *Service) UpdateBed(ctx context.Context, id string, in BedInput) (Bed, error) {
	var out Bed
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		before, err := s.store.GetBed(ctx, tx, id)
		if err != nil {
			return mapNotFound(err)
		}
		after := before
		if in.Name != nil {
			after.Name = strings.TrimSpace(*in.Name)
		}
		if in.Code != nil {
			after.Code = strings.TrimSpace(*in.Code)
		}
		if in.Type != nil {
			after.Type = *in.Type
		}
		// The three size fields honour an explicit null as "clear this", which is
		// what the form sends when a box is emptied — see BedInput.present.
		if in.carried("length_cm", in.LengthCM != nil) {
			after.LengthCM = in.LengthCM
		}
		if in.carried("width_cm", in.WidthCM != nil) {
			after.WidthCM = in.WidthCM
		}
		if in.SunExposure != nil {
			after.SunExposure = in.SunExposure
		}
		if in.Zone != nil {
			after.Zone = in.Zone
		}
		if in.SoilNotesMD != nil {
			after.SoilNotesMD = in.SoilNotesMD
		}
		if in.IsActive != nil {
			after.IsActive = *in.IsActive
		}
		// An area sent directly wins; an area the body CLEARED (or never carried)
		// falls back to the dimensions, which is what makes "Plocha se dopočítá z
		// rozměrů" true. A bed left with no usable dimensions keeps the area it
		// had — area_m2 is NOT NULL and must stay positive.
		if in.AreaM2 != nil {
			after.AreaM2 = *in.AreaM2
		} else if in.carried("length_cm", in.LengthCM != nil) ||
			in.carried("width_cm", in.WidthCM != nil) ||
			in.carried("area_m2", false) {
			if after.LengthCM != nil && after.WidthCM != nil {
				after.AreaM2 = round2(*after.LengthCM * *after.WidthCM / 10000)
			}
		}
		if err := validateBed(after); err != nil {
			return err
		}
		// A ZONE CHANGE IS A MOVE. Positions are minted per zone
		// (lastBedPosition), so they interleave arbitrarily across zones — a bed
		// carrying its old position into a new zone would land at an index nobody
		// chose, and consecutive beds ARE the adjacency check C11 reads (D117).
		// Re-mint it at the end of the destination zone, which is where a bed the
		// household has not yet ordered belongs; MoveBed is how it is placed.
		if !sameStrPtr(before.Zone, after.Zone) {
			pos, err := s.lastBedPosition(ctx, tx, after.Zone)
			if err != nil {
				return err
			}
			after.Position = lexorank.Between(pos, "")
		}
		after.UpdatedAt = nowUTC()
		if err := s.store.UpdateBed(ctx, tx, after); err != nil {
			return err
		}
		out = after
		return s.record(ctx, tx, "bed.update", "garden_bed", id, "Upraven záhon "+after.Code,
			bedChanges(before, after), nil)
	})
	if err != nil {
		return Bed{}, err
	}
	s.notify(ctx, "garden_bed.changed", map[string]any{"id": id})
	return out, nil
}

// MoveBed reorders a bed within (or into) a zone. THIS CHANGES THE WARNINGS
// (D117): consecutive beds are neighbours, so the drag is not cosmetic, and the
// audit summary says so in as many words.
func (s *Service) MoveBed(ctx context.Context, id string, zone *string, afterID, beforeID string) (Bed, error) {
	var out Bed
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		bed, err := s.store.GetBed(ctx, tx, id)
		if err != nil {
			return mapNotFound(err)
		}
		before := bed
		if zone != nil {
			bed.Zone = zone
		}
		prev, next := "", ""
		if afterID != "" {
			nb, err := s.store.GetBed(ctx, tx, afterID)
			if err != nil {
				return mapNotFound(err)
			}
			prev = nb.Position
		}
		if beforeID != "" {
			nb, err := s.store.GetBed(ctx, tx, beforeID)
			if err != nil {
				return mapNotFound(err)
			}
			next = nb.Position
		}
		bed.Position = lexorank.Between(prev, next)
		bed.UpdatedAt = nowUTC()
		if err := s.store.UpdateBed(ctx, tx, bed); err != nil {
			return err
		}
		out = bed
		return s.record(ctx, tx, "bed.move", "garden_bed", id,
			"Přesunut záhon "+bed.Code+" — změnilo se sousedství, které čte kontrola plánu",
			bedChanges(before, bed), nil)
	})
	if err != nil {
		return Bed{}, err
	}
	s.notify(ctx, "garden_bed.changed", map[string]any{"id": id})
	return out, nil
}

func (s *Service) DeleteBed(ctx context.Context, id string) error {
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		before, err := s.store.GetBed(ctx, tx, id)
		if err != nil {
			return mapNotFound(err)
		}
		has, err := s.store.BedHasOpenPlantings(ctx, tx, id)
		if err != nil {
			return err
		}
		if has {
			return httpx.ErrConflict("V záhonu jsou výsadby v neuzavřené sezóně. Smažte je, nebo sezónu uzavřete.")
		}
		at := nowUTC()
		if err := s.store.SoftDeleteBed(ctx, tx, id, at); err != nil {
			return err
		}
		return s.record(ctx, tx, "bed.delete", "garden_bed", id, "Smazán záhon "+before.Code, nil, nil)
	})
	if err != nil {
		return err
	}
	s.notify(ctx, "garden_bed.changed", map[string]any{"id": id})
	return nil
}

func (s *Service) BedHistory(ctx context.Context, id string) (BedHistory, error) {
	if _, err := s.store.GetBed(ctx, nil, id); err != nil {
		return BedHistory{}, mapNotFound(err)
	}
	seasons, err := s.store.BedHistory(ctx, nil, id)
	if err != nil {
		return BedHistory{}, err
	}
	if seasons == nil {
		seasons = []BedSeasonHistory{}
	}
	return BedHistory{BedID: id, Seasons: seasons}, nil
}

// ============================== helpers ==============================

func sameStrPtr(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func sameIntPtr(a, b *int) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func sameBoolPtr(a, b *bool) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func sameFloatPtr(a, b *float64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// diffStr appends a field change when the value actually moved.
func diffStr(changes *[]audit.Change, field string, old, new *string) {
	if sameStrPtr(old, new) {
		return
	}
	*changes = append(*changes, audit.Change{Field: field, Old: old, New: new})
}

func diffPlain(changes *[]audit.Change, field, old, new string) {
	if old == new {
		return
	}
	*changes = append(*changes, audit.Change{Field: field, Old: sp(old), New: sp(new)})
}

func diffFloat(changes *[]audit.Change, field string, old, new *float64) {
	if sameFloatPtr(old, new) {
		return
	}
	*changes = append(*changes, audit.Change{Field: field, Old: fmtFloat(old), New: fmtFloat(new)})
}

func fmtFloat(v *float64) *string {
	if v == nil {
		return nil
	}
	return sp(strconv.FormatFloat(*v, 'f', -1, 64))
}

func fmtInt(v *int) *string {
	if v == nil {
		return nil
	}
	return sp(strconv.Itoa(*v))
}

func fmtWindow(w *Window) *string {
	if w == nil {
		return nil
	}
	return sp(fmt.Sprintf("%s %d…%d", w.Anchor, w.From, w.To))
}

// plantChanges is the field diff for garden_plant, which joins the diff set
// because "who moved the tomato transplant date and to what" is the question the
// Log exists to answer here (D-arch).
func plantChanges(before *Plant, after Plant) []audit.Change {
	var changes []audit.Change
	var b Plant
	if before != nil {
		b = *before
	}
	diffPlain(&changes, "name_cs", b.NameCS, after.NameCS)
	diffPlain(&changes, "family", b.Family, after.Family)
	diffPlain(&changes, "plant_type", b.PlantType, after.PlantType)
	diffPlain(&changes, "hardiness", b.Hardiness, after.Hardiness)
	diffStr(&changes, "feeder_class", b.FeederClass, after.FeederClass)
	diffStr(&changes, "sow_method", b.SowMethod, after.SowMethod)
	diffStr(&changes, "harvest_unit", b.HarvestUnit, after.HarvestUnit)
	for _, w := range []struct {
		field string
		o, n  *Window
	}{
		{"win_sow_indoor", b.WinSowIndoor, after.WinSowIndoor},
		{"win_sow_direct", b.WinSowDirect, after.WinSowDirect},
		{"win_transplant", b.WinTransplant, after.WinTransplant},
		{"win_harvest", b.WinHarvest, after.WinHarvest},
	} {
		if !sameWindow(w.o, w.n) {
			changes = append(changes, audit.Change{Field: w.field, Old: fmtWindow(w.o), New: fmtWindow(w.n)})
		}
	}
	if !sameIntPtr(b.RotationBreakYears, after.RotationBreakYears) {
		changes = append(changes, audit.Change{Field: "rotation_break_years",
			Old: fmtInt(b.RotationBreakYears), New: fmtInt(after.RotationBreakYears)})
	}
	diffStr(&changes, "notes_md", b.NotesMD, after.NotesMD)
	return changes
}

func bedChanges(before, after Bed) []audit.Change {
	var changes []audit.Change
	diffPlain(&changes, "name", before.Name, after.Name)
	diffPlain(&changes, "code", before.Code, after.Code)
	diffPlain(&changes, "type", before.Type, after.Type)
	diffFloat(&changes, "area_m2", &before.AreaM2, &after.AreaM2)
	diffStr(&changes, "zone", before.Zone, after.Zone)
	diffPlain(&changes, "position", before.Position, after.Position)
	diffPlain(&changes, "is_active", strconv.FormatBool(before.IsActive), strconv.FormatBool(after.IsActive))
	return changes
}
