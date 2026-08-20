package garden

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/dates"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
)

// DBTX is satisfied by *sql.DB and *sql.Tx. Reads use the store's *sql.DB;
// mutations take the explicit tx so the audit write commits atomically with them.
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Store is the garden module's SQL layer.
type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// DB exposes the pool for the read paths that need it directly (the widget and
// the metric providers, which never write).
func (s *Store) DB() *sql.DB { return s.db }

const tsFormat = "2006-01-02T15:04:05.000Z07:00"

func nowUTC() string { return time.Now().UTC().Format(tsFormat) }

const (
	defaultLimit = 50
	maxLimit     = 200
)

// NormalizeLimit clamps a caller-supplied page size.
func NormalizeLimit(n int) int {
	if n <= 0 {
		return defaultLimit
	}
	if n > maxLimit {
		return maxLimit
	}
	return n
}

type scanner interface{ Scan(dest ...any) error }

var errNotFound = errors.New("garden: not found")

// ============================== plants ==============================

const plantIdentCols = `id, name_cs, family, plant_type, hardiness,
	source, source_model, source_at, verified_by, verified_at,
	created_by, created_at, updated_at`

func scanPlant(sc scanner) (Plant, error) {
	var p Plant
	var core coreRow
	var sourceModel, sourceAt, verifiedBy, verifiedAt, createdBy sql.NullString
	targets := []any{
		&p.ID, &p.NameCS, &p.Family, &p.PlantType, &p.Hardiness,
		&p.Provenance.Source, &sourceModel, &sourceAt, &verifiedBy, &verifiedAt,
		&createdBy, &p.CreatedAt, &p.UpdatedAt,
	}
	targets = append(targets, core.scanTargets()...)
	if err := sc.Scan(targets...); err != nil {
		return Plant{}, err
	}
	p.PlantCore = core.toCore()
	p.Provenance.SourceModel = nsp(sourceModel)
	p.Provenance.SourceAt = nsp(sourceAt)
	p.Provenance.VerifiedBy = nsp(verifiedBy)
	p.Provenance.VerifiedAt = nsp(verifiedAt)
	p.CreatedBy = nsp(createdBy)
	return p, nil
}

// PlantFilter is what GET /api/garden/plants supports.
type PlantFilter struct {
	Query      string
	Family     string
	PlantType  string
	Unverified bool
}

// ListPlants returns crops keyset-paginated by UUIDv7 id.
//
// The FTS branch and the plain branch return the same shape deliberately: search
// is a filter on the list, not a different endpoint, so the Plodiny page has one
// rendering path.
func (s *Store) ListPlants(ctx context.Context, f PlantFilter, limit int, cursor string) ([]Plant, *string, error) {
	q := `SELECT ` + plantIdentCols + `, ` + coreColumns + ` FROM garden_plants WHERE deleted_at IS NULL`
	var args []any
	if f.Query != "" {
		// MATCH over the external-content index; `seq` is the shared rowid.
		q += ` AND seq IN (SELECT rowid FROM garden_plants_fts WHERE garden_plants_fts MATCH ?)`
		args = append(args, ftsQuery(f.Query))
	}
	if f.Family != "" {
		q += ` AND family = ?`
		args = append(args, f.Family)
	}
	if f.PlantType != "" {
		q += ` AND plant_type = ?`
		args = append(args, f.PlantType)
	}
	if f.Unverified {
		q += ` AND verified_at IS NULL`
	}
	if cursor != "" {
		q += ` AND id > ?`
		args = append(args, cursor)
	}
	q += ` ORDER BY id LIMIT ?`
	args = append(args, limit+1)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []Plant
	for rows.Next() {
		p, err := scanPlant(rows)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	items, next := page(out, limit, func(p Plant) string { return p.ID })
	if err := s.attachVarietyCounts(ctx, items); err != nil {
		return nil, nil, err
	}
	return items, next, nil
}

// attachVarietyCounts fills VarietyCount for a page in ONE query — the Plodiny
// list shows it on every row, and a per-row count is the classic N+1 here.
func (s *Store) attachVarietyCounts(ctx context.Context, plants []Plant) error {
	if len(plants) == 0 {
		return nil
	}
	ids := make([]any, 0, len(plants))
	for _, p := range plants {
		ids = append(ids, p.ID)
	}
	q := `SELECT plant_id, COUNT(*) FROM garden_varieties
	      WHERE deleted_at IS NULL AND plant_id IN (` + placeholders(len(ids)) + `)
	      GROUP BY plant_id`
	rows, err := s.db.QueryContext(ctx, q, ids...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	counts := map[string]int{}
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return err
		}
		counts[id] = n
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range plants {
		plants[i].VarietyCount = counts[plants[i].ID]
	}
	return nil
}

func (s *Store) GetPlant(ctx context.Context, db DBTX, id string) (Plant, error) {
	if db == nil {
		db = s.db
	}
	row := db.QueryRowContext(ctx,
		`SELECT `+plantIdentCols+`, `+coreColumns+` FROM garden_plants WHERE id = ? AND deleted_at IS NULL`, id)
	p, err := scanPlant(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Plant{}, errNotFound
	}
	return p, err
}

// PlantByName resolves a crop by its Czech name. Used by the importer (which
// matches on name) and by the rule resolver (built-in pairs reference crops by
// name — see the seed migration's header).
func (s *Store) PlantByName(ctx context.Context, db DBTX, name string) (Plant, error) {
	if db == nil {
		db = s.db
	}
	row := db.QueryRowContext(ctx,
		`SELECT `+plantIdentCols+`, `+coreColumns+` FROM garden_plants WHERE name_cs = ? AND deleted_at IS NULL`, name)
	p, err := scanPlant(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Plant{}, errNotFound
	}
	return p, err
}

func (s *Store) InsertPlant(ctx context.Context, tx DBTX, p Plant) error {
	args := []any{p.ID, p.NameCS, p.Family, p.PlantType, p.Hardiness,
		p.Provenance.Source, p.Provenance.SourceModel, p.Provenance.SourceAt,
		p.Provenance.VerifiedBy, p.Provenance.VerifiedAt,
		p.CreatedBy, p.CreatedAt, p.UpdatedAt, searchBlob(p.PlantCore)}
	args = append(args, coreValues(p.PlantCore)...)
	_, err := tx.ExecContext(ctx,
		`INSERT INTO garden_plants (`+plantIdentCols+`, search_blob, `+coreColumns+`)
		 VALUES (`+placeholders(14+coreColumnCount)+`)`, args...)
	return err
}

func (s *Store) UpdatePlant(ctx context.Context, tx DBTX, p Plant) error {
	args := []any{p.NameCS, p.Family, p.PlantType, p.Hardiness,
		p.Provenance.Source, p.Provenance.SourceModel, p.Provenance.SourceAt,
		p.Provenance.VerifiedBy, p.Provenance.VerifiedAt,
		p.UpdatedAt, searchBlob(p.PlantCore)}
	args = append(args, coreValues(p.PlantCore)...)
	args = append(args, p.ID)
	_, err := tx.ExecContext(ctx,
		`UPDATE garden_plants SET name_cs = ?, family = ?, plant_type = ?, hardiness = ?,
		    source = ?, source_model = ?, source_at = ?, verified_by = ?, verified_at = ?,
		    updated_at = ?, search_blob = ?, `+assignments(coreColumns)+`
		 WHERE id = ? AND deleted_at IS NULL`, args...)
	return err
}

func (s *Store) SoftDeletePlant(ctx context.Context, tx DBTX, id, at string) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE garden_plants SET deleted_at = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL`, at, at, id)
	return err
}

// PlantingsUsingPlant counts live plantings of a crop, so a delete that would
// orphan a plan is refused rather than cascading.
func (s *Store) PlantingsUsingPlant(ctx context.Context, db DBTX, plantID string) (int, error) {
	var n int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM garden_plantings WHERE plant_id = ? AND deleted_at IS NULL`, plantID).Scan(&n)
	return n, err
}

// PlantingsUsingVariety is PlantingsUsingPlant's twin, and exists for the same
// reason: EffectiveFor resolves a planting's variety through GetVariety, which
// filters soft-deleted rows, so a variety deleted out from under a live planting
// makes that planting unreadable and unsavable rather than merely un-varietal.
func (s *Store) PlantingsUsingVariety(ctx context.Context, db DBTX, varietyID string) (int, error) {
	var n int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM garden_plantings WHERE variety_id = ? AND deleted_at IS NULL`, varietyID).Scan(&n)
	return n, err
}

// ============================= varieties =============================

const varietyIdentCols = `id, plant_id, name, supplier, description_md, is_favourite, retired,
	source, source_model, source_at, verified_by, verified_at, created_at, updated_at`

func scanVariety(sc scanner) (Variety, error) {
	var v Variety
	var core coreRow
	var supplier, descr, sourceModel, sourceAt, verifiedBy, verifiedAt sql.NullString
	var fav, retired int
	targets := []any{
		&v.ID, &v.PlantID, &v.Name, &supplier, &descr, &fav, &retired,
		&v.Provenance.Source, &sourceModel, &sourceAt, &verifiedBy, &verifiedAt,
		&v.CreatedAt, &v.UpdatedAt,
	}
	targets = append(targets, core.scanTargets()...)
	if err := sc.Scan(targets...); err != nil {
		return Variety{}, err
	}
	v.PlantCore = core.toCore()
	v.Supplier, v.DescriptionMD = nsp(supplier), nsp(descr)
	v.IsFavourite, v.Retired = fav != 0, retired != 0
	v.Provenance.SourceModel = nsp(sourceModel)
	v.Provenance.SourceAt = nsp(sourceAt)
	v.Provenance.VerifiedBy = nsp(verifiedBy)
	v.Provenance.VerifiedAt = nsp(verifiedAt)
	return v, nil
}

func (s *Store) ListVarieties(ctx context.Context, plantID string, limit int, cursor string) ([]Variety, *string, error) {
	q := `SELECT ` + varietyIdentCols + `, ` + coreColumns + `
	      FROM garden_varieties WHERE plant_id = ? AND deleted_at IS NULL`
	args := []any{plantID}
	if cursor != "" {
		q += ` AND id > ?`
		args = append(args, cursor)
	}
	q += ` ORDER BY id LIMIT ?`
	args = append(args, limit+1)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Variety
	for rows.Next() {
		v, err := scanVariety(rows)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	items, next := page(out, limit, func(v Variety) string { return v.ID })
	return items, next, nil
}

func (s *Store) GetVariety(ctx context.Context, db DBTX, id string) (Variety, error) {
	if db == nil {
		db = s.db
	}
	row := db.QueryRowContext(ctx,
		`SELECT `+varietyIdentCols+`, `+coreColumns+` FROM garden_varieties WHERE id = ? AND deleted_at IS NULL`, id)
	v, err := scanVariety(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Variety{}, errNotFound
	}
	return v, err
}

func (s *Store) InsertVariety(ctx context.Context, tx DBTX, v Variety, createdBy, at string) error {
	args := []any{v.ID, v.PlantID, v.Name, v.Supplier, v.DescriptionMD, b2i(v.IsFavourite), b2i(v.Retired),
		v.Provenance.Source, v.Provenance.SourceModel, v.Provenance.SourceAt,
		v.Provenance.VerifiedBy, v.Provenance.VerifiedAt, at, at, createdBy}
	args = append(args, coreValues(v.PlantCore)...)
	_, err := tx.ExecContext(ctx,
		`INSERT INTO garden_varieties (`+varietyIdentCols+`, created_by, `+coreColumns+`)
		 VALUES (`+placeholders(15+coreColumnCount)+`)`, args...)
	return err
}

func (s *Store) UpdateVariety(ctx context.Context, tx DBTX, v Variety, at string) error {
	args := []any{v.Name, v.Supplier, v.DescriptionMD, b2i(v.IsFavourite), b2i(v.Retired),
		v.Provenance.Source, v.Provenance.SourceModel, v.Provenance.SourceAt,
		v.Provenance.VerifiedBy, v.Provenance.VerifiedAt, at}
	args = append(args, coreValues(v.PlantCore)...)
	args = append(args, v.ID)
	_, err := tx.ExecContext(ctx,
		`UPDATE garden_varieties SET name = ?, supplier = ?, description_md = ?, is_favourite = ?, retired = ?,
		    source = ?, source_model = ?, source_at = ?, verified_by = ?, verified_at = ?, updated_at = ?,
		    `+assignments(coreColumns)+`
		 WHERE id = ? AND deleted_at IS NULL`, args...)
	return err
}

func (s *Store) SoftDeleteVariety(ctx context.Context, tx DBTX, id, at string) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE garden_varieties SET deleted_at = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL`, at, at, id)
	return err
}

// ================================ beds ================================

const bedCols = `id, name, code, type, length_cm, width_cm, area_m2,
	sun_exposure, zone, soil_notes_md, is_active, position, created_at, updated_at`

func scanBed(sc scanner) (Bed, error) {
	var b Bed
	var length, width sql.NullFloat64
	var sun, zone, notes sql.NullString
	var active int
	if err := sc.Scan(&b.ID, &b.Name, &b.Code, &b.Type, &length, &width, &b.AreaM2,
		&sun, &zone, &notes, &active, &b.Position, &b.CreatedAt, &b.UpdatedAt); err != nil {
		return Bed{}, err
	}
	b.LengthCM, b.WidthCM = nfp(length), nfp(width)
	b.SunExposure, b.Zone, b.SoilNotesMD = nsp(sun), nsp(zone), nsp(notes)
	b.IsActive = active != 0
	return b, nil
}

// ListBeds returns every live bed in (zone, position) order — WHICH IS THE
// ADJACENCY ORDER (D117), not a cosmetic sort. Beds are a couple of dozen rows,
// so this is deliberately unpaginated: the planner needs all of them to compute
// neighbours, and a page boundary would silently cut an adjacency pair in half.
func (s *Store) ListBeds(ctx context.Context, db DBTX, includeInactive bool) ([]Bed, error) {
	if db == nil {
		db = s.db
	}
	q := `SELECT ` + bedCols + ` FROM garden_beds WHERE deleted_at IS NULL`
	if !includeInactive {
		q += ` AND is_active = 1`
	}
	q += ` ORDER BY COALESCE(zone, ''), position, id`
	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Bed
	for rows.Next() {
		b, err := scanBed(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *Store) GetBed(ctx context.Context, db DBTX, id string) (Bed, error) {
	if db == nil {
		db = s.db
	}
	row := db.QueryRowContext(ctx, `SELECT `+bedCols+` FROM garden_beds WHERE id = ? AND deleted_at IS NULL`, id)
	b, err := scanBed(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Bed{}, errNotFound
	}
	return b, err
}

func (s *Store) InsertBed(ctx context.Context, tx DBTX, b Bed, createdBy string) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO garden_beds (`+bedCols+`, created_by) VALUES (`+placeholders(15)+`)`,
		b.ID, b.Name, b.Code, b.Type, b.LengthCM, b.WidthCM, b.AreaM2,
		b.SunExposure, b.Zone, b.SoilNotesMD, b2i(b.IsActive), b.Position,
		b.CreatedAt, b.UpdatedAt, createdBy)
	return err
}

func (s *Store) UpdateBed(ctx context.Context, tx DBTX, b Bed) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE garden_beds SET name = ?, code = ?, type = ?, length_cm = ?, width_cm = ?, area_m2 = ?,
		    sun_exposure = ?, zone = ?, soil_notes_md = ?, is_active = ?, position = ?, updated_at = ?
		 WHERE id = ? AND deleted_at IS NULL`,
		b.Name, b.Code, b.Type, b.LengthCM, b.WidthCM, b.AreaM2,
		b.SunExposure, b.Zone, b.SoilNotesMD, b2i(b.IsActive), b.Position, b.UpdatedAt, b.ID)
	return err
}

func (s *Store) SoftDeleteBed(ctx context.Context, tx DBTX, id, at string) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE garden_beds SET deleted_at = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL`, at, at, id)
	return err
}

// BedHasOpenPlantings reports whether a bed holds plantings in a season that is
// not closed. Deleting such a bed would orphan a live plan, so it is a 409.
func (s *Store) BedHasOpenPlantings(ctx context.Context, db DBTX, bedID string) (bool, error) {
	var n int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM garden_plantings p
		 LEFT JOIN garden_seasons se ON se.id = p.season_id
		 WHERE p.bed_id = ? AND p.deleted_at IS NULL
		   AND (p.season_id IS NULL OR se.status <> 'closed')`, bedID).Scan(&n)
	return n > 0, err
}

// BedHistory returns what occupied a bed, per CLOSED season only (D120).
//
// An open season is not history — it is a plan that may still change, and
// rotation history that moves when you edit next year's plan is not history at
// all. This is why C3 and C8 are structurally silent in the first year.
func (s *Store) BedHistory(ctx context.Context, db DBTX, bedID string) ([]BedSeasonHistory, error) {
	if db == nil {
		db = s.db
	}
	rows, err := db.QueryContext(ctx,
		`SELECT se.year, pl.family, pl.name_cs, v.name, pl.feeder_class,
		        (SELECT SUM(h.quantity) FROM garden_harvests h
		          WHERE h.planting_id = p.id AND h.deleted_at IS NULL AND h.unit = 'kg')
		 FROM garden_plantings p
		 JOIN garden_seasons se ON se.id = p.season_id
		 JOIN garden_plants pl ON pl.id = p.plant_id
		 LEFT JOIN garden_varieties v ON v.id = p.variety_id
		 WHERE p.bed_id = ? AND p.deleted_at IS NULL AND se.status = 'closed'
		 ORDER BY se.year DESC`, bedID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	byYear := map[int]*BedSeasonHistory{}
	var order []int
	for rows.Next() {
		var year int
		var family, plantName string
		var varietyName sql.NullString
		var feeder sql.NullString
		var yieldKg sql.NullFloat64
		if err := rows.Scan(&year, &family, &plantName, &varietyName, &feeder, &yieldKg); err != nil {
			return nil, err
		}
		h := byYear[year]
		if h == nil {
			h = &BedSeasonHistory{Year: year}
			byYear[year] = h
			order = append(order, year)
		}
		if !containsStr(h.Families, family) {
			h.Families = append(h.Families, family)
		}
		h.Plantings = append(h.Plantings, BedHistoryPlanting{
			PlantName: plantName, VarietyName: nsp(varietyName),
			YieldKg: nfp(yieldKg), FeederClass: feeder.String, Family: family,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]BedSeasonHistory, 0, len(order))
	for _, y := range order {
		out = append(out, *byYear[y])
	}
	return out, nil
}

// allBedHistory loads the closed-season record for EVERY bed in one query, for
// the snapshot. Per-bed calls would be the check engine's N+1.
func (s *Store) allBedHistory(ctx context.Context, db DBTX) (map[string][]BedSeasonHistory, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT p.bed_id, se.year, pl.family, pl.name_cs, pl.feeder_class
		 FROM garden_plantings p
		 JOIN garden_seasons se ON se.id = p.season_id
		 JOIN garden_plants pl ON pl.id = p.plant_id
		 WHERE p.bed_id IS NOT NULL AND p.deleted_at IS NULL AND se.status = 'closed'
		 ORDER BY p.bed_id, se.year DESC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := map[string][]BedSeasonHistory{}
	for rows.Next() {
		var bedID, family, plantName string
		var year int
		var feeder sql.NullString
		if err := rows.Scan(&bedID, &year, &family, &plantName, &feeder); err != nil {
			return nil, err
		}
		list := out[bedID]
		idx := -1
		for i := range list {
			if list[i].Year == year {
				idx = i
				break
			}
		}
		if idx < 0 {
			list = append(list, BedSeasonHistory{Year: year})
			idx = len(list) - 1
		}
		if !containsStr(list[idx].Families, family) {
			list[idx].Families = append(list[idx].Families, family)
		}
		list[idx].Plantings = append(list[idx].Plantings, BedHistoryPlanting{
			PlantName: plantName, FeederClass: feeder.String, Family: family,
		})
		out[bedID] = list
	}
	return out, rows.Err()
}

// =============================== seasons ===============================

const seasonCols = `id, year, status, last_frost_on, first_frost_on,
	last_frost_actual_on, first_frost_actual_on, notes_md, closed_at, closed_by, created_at, updated_at`

func scanSeason(sc scanner) (Season, error) {
	var s Season
	var lf, ff, lfa, ffa, notes, closedAt, closedBy sql.NullString
	if err := sc.Scan(&s.ID, &s.Year, &s.Status, &lf, &ff, &lfa, &ffa, &notes,
		&closedAt, &closedBy, &s.CreatedAt, &s.UpdatedAt); err != nil {
		return Season{}, err
	}
	s.LastFrostOn, s.FirstFrostOn = nsp(lf), nsp(ff)
	s.LastFrostActualOn, s.FirstFrostActualOn = nsp(lfa), nsp(ffa)
	s.NotesMD, s.ClosedAt, s.ClosedBy = nsp(notes), nsp(closedAt), nsp(closedBy)
	return s, nil
}

// SeasonByYear resolves the {year} path parameter to a row (D128).
func (s *Store) SeasonByYear(ctx context.Context, db DBTX, year int) (Season, error) {
	if db == nil {
		db = s.db
	}
	row := db.QueryRowContext(ctx, `SELECT `+seasonCols+` FROM garden_seasons WHERE year = ?`, year)
	se, err := scanSeason(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Season{}, errNotFound
	}
	if err != nil {
		return Season{}, err
	}
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM garden_plantings WHERE season_id = ? AND deleted_at IS NULL`,
		se.ID).Scan(&se.PlantingCount); err != nil {
		return Season{}, err
	}
	return se, nil
}

func (s *Store) ListSeasons(ctx context.Context) ([]Season, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+seasonCols+` FROM garden_seasons ORDER BY year DESC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Season
	for rows.Next() {
		se, err := scanSeason(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, se)
	}
	return out, rows.Err()
}

// ClosedSeasonCount is what decides whether C3 and C8 can run at all (D120).
func (s *Store) ClosedSeasonCount(ctx context.Context, db DBTX) (int, error) {
	if db == nil {
		db = s.db
	}
	var n int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM garden_seasons WHERE status = 'closed'`).Scan(&n)
	return n, err
}

func (s *Store) InsertSeason(ctx context.Context, tx DBTX, se Season, createdBy string) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO garden_seasons (`+seasonCols+`, created_by) VALUES (`+placeholders(13)+`)`,
		se.ID, se.Year, se.Status, se.LastFrostOn, se.FirstFrostOn,
		se.LastFrostActualOn, se.FirstFrostActualOn, se.NotesMD, se.ClosedAt, se.ClosedBy,
		se.CreatedAt, se.UpdatedAt, createdBy)
	return err
}

func (s *Store) UpdateSeason(ctx context.Context, tx DBTX, se Season) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE garden_seasons SET status = ?, last_frost_on = ?, first_frost_on = ?,
		    last_frost_actual_on = ?, first_frost_actual_on = ?, notes_md = ?,
		    closed_at = ?, closed_by = ?, updated_at = ?
		 WHERE id = ?`,
		se.Status, se.LastFrostOn, se.FirstFrostOn, se.LastFrostActualOn, se.FirstFrostActualOn,
		se.NotesMD, se.ClosedAt, se.ClosedBy, se.UpdatedAt, se.ID)
	return err
}

// ============================== plantings ==============================

const plantingCols = `p.id, p.season_id, p.bed_id, p.location_label, p.plant_id, p.variety_id,
	p.area_m2, p.plant_count, p.rows,
	p.sow_indoor_on, p.sow_direct_on, p.transplant_on, p.harvest_from, p.harvest_to, p.manual_dates,
	p.sowed_on, p.transplanted_on, p.first_harvest_on, p.cleared_on,
	p.status, p.fail_reason, p.planted_on, p.rootstock, p.removed_on, p.notes_md,
	p.created_at, p.updated_at`

// plantingJoinCols are the denormalised labels every planting read carries, so a
// bed code or a crop name never costs a second query.
const plantingJoinCols = `se.year, b.code, pl.name_cs, pl.family, v.name`

const plantingFrom = `FROM garden_plantings p
	LEFT JOIN garden_seasons se ON se.id = p.season_id
	LEFT JOIN garden_beds b ON b.id = p.bed_id
	JOIN garden_plants pl ON pl.id = p.plant_id
	LEFT JOIN garden_varieties v ON v.id = p.variety_id`

func scanPlanting(sc scanner) (Planting, error) {
	var p Planting
	var seasonID, bedID, locationLabel, varietyID sql.NullString
	var areaM2 sql.NullFloat64
	var plantCount, rowsN sql.NullInt64
	var sowIndoor, sowDirect, transplant, harvestFrom, harvestTo sql.NullString
	var manualDates string
	var sowed, transplanted, firstHarvest, cleared sql.NullString
	var failReason, plantedOn, rootstock, removedOn, notes sql.NullString
	var seasonYear sql.NullInt64
	var bedCode, varietyName sql.NullString

	if err := sc.Scan(&p.ID, &seasonID, &bedID, &locationLabel, &p.PlantID, &varietyID,
		&areaM2, &plantCount, &rowsN,
		&sowIndoor, &sowDirect, &transplant, &harvestFrom, &harvestTo, &manualDates,
		&sowed, &transplanted, &firstHarvest, &cleared,
		&p.Status, &failReason, &plantedOn, &rootstock, &removedOn, &notes,
		&p.CreatedAt, &p.UpdatedAt,
		&seasonYear, &bedCode, &p.PlantName, &p.Family, &varietyName); err != nil {
		return Planting{}, err
	}
	p.SeasonID, p.BedID, p.LocationLabel, p.VarietyID = nsp(seasonID), nsp(bedID), nsp(locationLabel), nsp(varietyID)
	p.AreaM2, p.PlantCount, p.Rows = nfp(areaM2), nip(plantCount), nip(rowsN)
	p.SowIndoorOn, p.SowDirectOn, p.TransplantOn = nsp(sowIndoor), nsp(sowDirect), nsp(transplant)
	p.HarvestFrom, p.HarvestTo = nsp(harvestFrom), nsp(harvestTo)
	p.ManualDates = decodeStrings(manualDates)
	p.SowedOn, p.TransplantedOn = nsp(sowed), nsp(transplanted)
	p.FirstHarvestOn, p.ClearedOn = nsp(firstHarvest), nsp(cleared)
	p.FailReason, p.PlantedOn, p.Rootstock, p.RemovedOn, p.NotesMD =
		nsp(failReason), nsp(plantedOn), nsp(rootstock), nsp(removedOn), nsp(notes)
	p.SeasonYear, p.BedCode, p.VarietyName = nip(seasonYear), nsp(bedCode), nsp(varietyName)
	p.Occupancy = p.OccupancyWire()
	return p, nil
}

// PlantingFilter is what GET /api/garden/plantings supports.
type PlantingFilter struct {
	Year      *int
	BedID     string
	PlantID   string
	Status    string
	Permanent *bool // true ⇒ season_id IS NULL (Trvalky), false ⇒ seasonal only
}

func (s *Store) ListPlantings(ctx context.Context, db DBTX, f PlantingFilter, limit int, cursor string) ([]Planting, *string, error) {
	if db == nil {
		db = s.db
	}
	q := `SELECT ` + plantingCols + `, ` + plantingJoinCols + ` ` + plantingFrom + ` WHERE p.deleted_at IS NULL`
	var args []any
	if f.Year != nil {
		q += ` AND se.year = ?`
		args = append(args, *f.Year)
	}
	if f.BedID != "" {
		q += ` AND p.bed_id = ?`
		args = append(args, f.BedID)
	}
	if f.PlantID != "" {
		q += ` AND p.plant_id = ?`
		args = append(args, f.PlantID)
	}
	if f.Status != "" {
		q += ` AND p.status = ?`
		args = append(args, f.Status)
	}
	if f.Permanent != nil {
		if *f.Permanent {
			q += ` AND p.season_id IS NULL`
		} else {
			q += ` AND p.season_id IS NOT NULL`
		}
	}
	if cursor != "" {
		q += ` AND p.id > ?`
		args = append(args, cursor)
	}
	q += ` ORDER BY p.id LIMIT ?`
	args = append(args, limit+1)

	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Planting
	for rows.Next() {
		p, err := scanPlanting(rows)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	items, next := page(out, limit, func(p Planting) string { return p.ID })
	return items, next, nil
}

func (s *Store) GetPlanting(ctx context.Context, db DBTX, id string) (Planting, error) {
	if db == nil {
		db = s.db
	}
	row := db.QueryRowContext(ctx,
		`SELECT `+plantingCols+`, `+plantingJoinCols+` `+plantingFrom+` WHERE p.id = ? AND p.deleted_at IS NULL`, id)
	p, err := scanPlanting(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Planting{}, errNotFound
	}
	return p, err
}

func (s *Store) InsertPlanting(ctx context.Context, tx DBTX, p Planting, createdBy string) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO garden_plantings (id, season_id, bed_id, location_label, plant_id, variety_id,
		    area_m2, plant_count, rows, sow_indoor_on, sow_direct_on, transplant_on, harvest_from, harvest_to,
		    manual_dates, sowed_on, transplanted_on, first_harvest_on, cleared_on, status, fail_reason,
		    planted_on, rootstock, removed_on, notes_md, created_by, created_at, updated_at)
		 VALUES (`+placeholders(28)+`)`,
		p.ID, p.SeasonID, p.BedID, p.LocationLabel, p.PlantID, p.VarietyID,
		p.AreaM2, p.PlantCount, p.Rows, p.SowIndoorOn, p.SowDirectOn, p.TransplantOn, p.HarvestFrom, p.HarvestTo,
		encodeStrings(p.ManualDates), p.SowedOn, p.TransplantedOn, p.FirstHarvestOn, p.ClearedOn,
		p.Status, p.FailReason, p.PlantedOn, p.Rootstock, p.RemovedOn, p.NotesMD,
		createdBy, p.CreatedAt, p.UpdatedAt)
	return err
}

func (s *Store) UpdatePlanting(ctx context.Context, tx DBTX, p Planting) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE garden_plantings SET bed_id = ?, location_label = ?, variety_id = ?,
		    area_m2 = ?, plant_count = ?, rows = ?,
		    sow_indoor_on = ?, sow_direct_on = ?, transplant_on = ?, harvest_from = ?, harvest_to = ?,
		    manual_dates = ?, sowed_on = ?, transplanted_on = ?, first_harvest_on = ?, cleared_on = ?,
		    status = ?, fail_reason = ?, planted_on = ?, rootstock = ?, removed_on = ?, notes_md = ?,
		    updated_at = ?
		 WHERE id = ? AND deleted_at IS NULL`,
		p.BedID, p.LocationLabel, p.VarietyID, p.AreaM2, p.PlantCount, p.Rows,
		p.SowIndoorOn, p.SowDirectOn, p.TransplantOn, p.HarvestFrom, p.HarvestTo,
		encodeStrings(p.ManualDates), p.SowedOn, p.TransplantedOn, p.FirstHarvestOn, p.ClearedOn,
		p.Status, p.FailReason, p.PlantedOn, p.Rootstock, p.RemovedOn, p.NotesMD,
		p.UpdatedAt, p.ID)
	return err
}

func (s *Store) SoftDeletePlanting(ctx context.Context, tx DBTX, id, at string) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE garden_plantings SET deleted_at = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL`, at, at, id)
	return err
}

// YieldFor sums a planting's harvests in the crop's own unit.
func (s *Store) YieldFor(ctx context.Context, db DBTX, plantingID, unit string) (*float64, error) {
	if db == nil {
		db = s.db
	}
	var total sql.NullFloat64
	err := db.QueryRowContext(ctx,
		`SELECT SUM(quantity) FROM garden_harvests
		 WHERE planting_id = ? AND deleted_at IS NULL AND unit = ?`, plantingID, unit).Scan(&total)
	if err != nil {
		return nil, err
	}
	return nfp(total), nil
}

// YieldsFor is YieldFor for a whole page, in ONE query: harvest totals per
// planting and unit. The per-row form would be the list endpoint's N+1, which is
// why the list used to carry no yields at all — and a list that shows nothing
// where the detail shows 12 kg is what let the season-close review offer to
// record a figure the household had already entered.
func (s *Store) YieldsFor(ctx context.Context, db DBTX, plantingIDs []string) (map[string]map[string]float64, error) {
	out := map[string]map[string]float64{}
	if len(plantingIDs) == 0 {
		return out, nil
	}
	if db == nil {
		db = s.db
	}
	args := toAny(plantingIDs)
	rows, err := db.QueryContext(ctx,
		`SELECT planting_id, unit, SUM(quantity) FROM garden_harvests
		 WHERE deleted_at IS NULL AND planting_id IN (`+placeholders(len(args))+`)
		 GROUP BY planting_id, unit`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id, unit string
		var total sql.NullFloat64
		if err := rows.Scan(&id, &unit, &total); err != nil {
			return nil, err
		}
		if !total.Valid {
			continue
		}
		if out[id] == nil {
			out[id] = map[string]float64{}
		}
		out[id][unit] = total.Float64
	}
	return out, rows.Err()
}

// ================================ tasks ================================

const taskCols = `t.id, t.kind, t.season_id, t.planting_id, t.bed_id, t.title_cs,
	t.window_from, t.window_to, t.due_hint, t.status, t.completed_by, t.completed_at,
	t.is_generated, t.is_edited, t.generation_key, t.suppressed, t.notes_md, t.position`

const taskJoinCols = `b.code, pl.name_cs, v.name`

const taskFrom = `FROM garden_tasks t
	LEFT JOIN garden_beds b ON b.id = t.bed_id
	LEFT JOIN garden_plantings p ON p.id = t.planting_id
	LEFT JOIN garden_plants pl ON pl.id = p.plant_id
	LEFT JOIN garden_varieties v ON v.id = p.variety_id`

func scanTask(sc scanner) (Task, error) {
	var t Task
	var seasonID, plantingID, bedID, dueHint, completedBy, completedAt, genKey, notes sql.NullString
	var bedCode, plantName, varietyName sql.NullString
	var isGen, isEdited, suppressed int
	if err := sc.Scan(&t.ID, &t.Kind, &seasonID, &plantingID, &bedID, &t.TitleCS,
		&t.WindowFrom, &t.WindowTo, &dueHint, &t.Status, &completedBy, &completedAt,
		&isGen, &isEdited, &genKey, &suppressed, &notes, &t.Position,
		&bedCode, &plantName, &varietyName); err != nil {
		return Task{}, err
	}
	t.SeasonID, t.PlantingID, t.BedID = nsp(seasonID), nsp(plantingID), nsp(bedID)
	t.DueHint, t.CompletedBy, t.CompletedAt = nsp(dueHint), nsp(completedBy), nsp(completedAt)
	t.GenerationKey, t.NotesMD = nsp(genKey), nsp(notes)
	t.IsGenerated, t.IsEdited, t.Suppressed = isGen != 0, isEdited != 0, suppressed != 0
	t.BedCode, t.PlantName, t.VarietyName = nsp(bedCode), nsp(plantName), nsp(varietyName)
	return t, nil
}

// TaskFilter is what GET /api/garden/tasks and the widget both go through.
type TaskFilter struct {
	From, To string // window overlap bounds, inclusive
	// ToEnd matches only work whose WHOLE window is over (window_to <= ToEnd).
	// The overlap bound (To) would count a job still mid-window as overdue.
	ToEnd      string
	Status     string
	Kind       string
	BedID      string
	PlantingID string
	Year       *int
	// IncludeSuppressed is for the generator only. Tombstones are never a
	// user-visible task.
	IncludeSuppressed bool
}

func (s *Store) ListTasks(ctx context.Context, db DBTX, f TaskFilter, limit int, cursor string) ([]Task, *string, error) {
	if db == nil {
		db = s.db
	}
	q := `SELECT ` + taskCols + `, ` + taskJoinCols + ` ` + taskFrom + ` WHERE t.deleted_at IS NULL`
	var args []any
	if !f.IncludeSuppressed {
		q += ` AND t.suppressed = 0`
	}
	// OVERLAP, not containment: a job already under way must not vanish from the
	// list on the day it becomes urgent.
	if f.From != "" {
		q += ` AND t.window_to >= ?`
		args = append(args, f.From)
	}
	if f.To != "" {
		q += ` AND t.window_from <= ?`
		args = append(args, f.To)
	}
	if f.ToEnd != "" {
		q += ` AND t.window_to <= ?`
		args = append(args, f.ToEnd)
	}
	if f.Status != "" {
		q += ` AND t.status = ?`
		args = append(args, f.Status)
	}
	if f.Kind != "" {
		q += ` AND t.kind = ?`
		args = append(args, f.Kind)
	}
	if f.BedID != "" {
		q += ` AND t.bed_id = ?`
		args = append(args, f.BedID)
	}
	if f.PlantingID != "" {
		q += ` AND t.planting_id = ?`
		args = append(args, f.PlantingID)
	}
	if f.Year != nil {
		q += ` AND t.season_id = (SELECT id FROM garden_seasons WHERE year = ?)`
		args = append(args, *f.Year)
	}
	// THE CURSOR MUST MATCH THE SORT. Tasks are the one list here not ordered by
	// id, so an `id > ?` cursor would compare against a column the ORDER BY does
	// not lead with: page two would drop every later-windowed row whose id sorts
	// low and re-serve rows page one already returned. The cursor therefore
	// carries the whole sort tuple and the predicate compares the whole tuple.
	if cursor != "" {
		from, pos, id, ok := decodeTaskCursor(cursor)
		if !ok {
			return nil, nil, errBadCursor
		}
		q += ` AND (t.window_from, t.position, t.id) > (?, ?, ?)`
		args = append(args, from, pos, id)
	}
	q += ` ORDER BY t.window_from, t.position, t.id LIMIT ?`
	args = append(args, limit+1)

	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	items, next := page(out, limit, encodeTaskCursor)
	return items, next, nil
}

// errBadCursor is what a malformed page cursor becomes. It is a client error,
// not a server one: the caller sent something no page of ours ever minted.
var errBadCursor = httpx.ErrUnprocessable("Neplatný stránkovací kurzor.")

// encodeTaskCursor packs the task sort tuple (window_from, position, id) into
// one opaque, URL-safe token. Opaque on purpose: it is the ORDER BY's business
// and no caller should be building one by hand.
func encodeTaskCursor(t Task) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strings.Join([]string{t.WindowFrom, t.Position, t.ID}, "\x1f")))
}

func decodeTaskCursor(cursor string) (windowFrom, position, id string, ok bool) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return "", "", "", false
	}
	parts := strings.Split(string(raw), "\x1f")
	if len(parts) != 3 {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[2], true
}

func (s *Store) GetTask(ctx context.Context, db DBTX, id string) (Task, error) {
	if db == nil {
		db = s.db
	}
	row := db.QueryRowContext(ctx,
		`SELECT `+taskCols+`, `+taskJoinCols+` `+taskFrom+` WHERE t.id = ? AND t.deleted_at IS NULL`, id)
	t, err := scanTask(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, errNotFound
	}
	return t, err
}

// TasksForPlanting returns every task of a planting INCLUDING tombstones — the
// generator needs to see a suppressed key to know not to recreate it (D110).
func (s *Store) TasksForPlanting(ctx context.Context, db DBTX, plantingID string) ([]Task, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT `+taskCols+`, `+taskJoinCols+` `+taskFrom+`
		 WHERE t.planting_id = ? AND t.deleted_at IS NULL ORDER BY t.window_from, t.id`, plantingID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) InsertTask(ctx context.Context, tx DBTX, t Task, createdBy, at string) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO garden_tasks (id, kind, season_id, planting_id, bed_id, title_cs,
		    window_from, window_to, due_hint, status, completed_by, completed_at,
		    is_generated, is_edited, generation_key, suppressed, notes_md, position,
		    created_by, created_at, updated_at)
		 VALUES (`+placeholders(21)+`)`,
		t.ID, t.Kind, t.SeasonID, t.PlantingID, t.BedID, t.TitleCS,
		t.WindowFrom, t.WindowTo, t.DueHint, t.Status, t.CompletedBy, t.CompletedAt,
		b2i(t.IsGenerated), b2i(t.IsEdited), t.GenerationKey, b2i(t.Suppressed), t.NotesMD, t.Position,
		createdBy, at, at)
	return err
}

func (s *Store) UpdateTask(ctx context.Context, tx DBTX, t Task, at string) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE garden_tasks SET kind = ?, title_cs = ?, window_from = ?, window_to = ?, due_hint = ?,
		    status = ?, completed_by = ?, completed_at = ?, is_edited = ?, suppressed = ?,
		    notes_md = ?, position = ?, updated_at = ?
		 WHERE id = ? AND deleted_at IS NULL`,
		t.Kind, t.TitleCS, t.WindowFrom, t.WindowTo, t.DueHint,
		t.Status, t.CompletedBy, t.CompletedAt, b2i(t.IsEdited), b2i(t.Suppressed),
		t.NotesMD, t.Position, at, t.ID)
	return err
}

// MoveTaskWindow is the generator's only write to an existing task, kept
// separate from UpdateTask so a regeneration can never touch a field it has no
// business touching. The bed and title ride along because they are DERIVED the
// same way the window is — from the planting — and a task left in the bed the
// crop moved out of is as wrong as one left in last month's window.
func (s *Store) MoveTaskWindow(ctx context.Context, tx DBTX, id, from, to string, dueHint *string, bedID *string, titleCS, at string) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE garden_tasks SET window_from = ?, window_to = ?, due_hint = ?, bed_id = ?, title_cs = ?, updated_at = ?
		 WHERE id = ? AND deleted_at IS NULL`, from, to, dueHint, bedID, titleCS, at, id)
	return err
}

// SuppressTask writes the TOMBSTONE: the row stays so its generation key remains
// taken and the task cannot resurrect on the next recompute (D110).
func (s *Store) SuppressTask(ctx context.Context, tx DBTX, id, at string) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE garden_tasks SET suppressed = 1, updated_at = ? WHERE id = ? AND deleted_at IS NULL`, at, id)
	return err
}

func (s *Store) SoftDeleteTask(ctx context.Context, tx DBTX, id, at string) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE garden_tasks SET deleted_at = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL`, at, at, id)
	return err
}

// CountTasks powers the two task metrics without loading rows.
func (s *Store) CountTasks(ctx context.Context, f TaskFilter) (int, error) {
	q := `SELECT COUNT(*) FROM garden_tasks t WHERE t.deleted_at IS NULL AND t.suppressed = 0`
	var args []any
	if f.From != "" {
		q += ` AND t.window_to >= ?`
		args = append(args, f.From)
	}
	if f.To != "" {
		q += ` AND t.window_from <= ?`
		args = append(args, f.To)
	}
	if f.ToEnd != "" {
		q += ` AND t.window_to <= ?`
		args = append(args, f.ToEnd)
	}
	if f.Status != "" {
		q += ` AND t.status = ?`
		args = append(args, f.Status)
	}
	var n int
	err := s.db.QueryRowContext(ctx, q, args...).Scan(&n)
	return n, err
}

// ============================== harvests ==============================

const harvestCols = `h.id, h.planting_id, h.harvested_on, h.quantity, h.unit,
	h.destination, h.quality, h.note, h.created_by, h.created_at`

func scanHarvest(sc scanner) (Harvest, error) {
	var h Harvest
	var dest, quality, note, createdBy, bedCode sql.NullString
	if err := sc.Scan(&h.ID, &h.PlantingID, &h.HarvestedOn, &h.Quantity, &h.Unit,
		&dest, &quality, &note, &createdBy, &h.CreatedAt, &h.PlantName, &bedCode); err != nil {
		return Harvest{}, err
	}
	h.Destination, h.Quality, h.Note, h.CreatedBy = nsp(dest), nsp(quality), nsp(note), nsp(createdBy)
	h.BedCode = nsp(bedCode)
	return h, nil
}

// ListHarvests pages the harvest rows of LIVE plantings. Deleting a planting
// soft-deletes it and leaves its harvests behind, so the planting filter is not
// optional: SeasonHarvestKg (the garden.harvest_season metric) already excludes
// them, and without the same filter here the Sklizeň page's own total would
// disagree with the dashboard's with nothing on either screen explaining it.
func (s *Store) ListHarvests(ctx context.Context, plantingID string, year *int, limit int, cursor string) ([]Harvest, *string, error) {
	q := `SELECT ` + harvestCols + `, pl.name_cs, b.code
	      FROM garden_harvests h
	      JOIN garden_plantings p ON p.id = h.planting_id
	      JOIN garden_plants pl ON pl.id = p.plant_id
	      LEFT JOIN garden_beds b ON b.id = p.bed_id
	      WHERE h.deleted_at IS NULL AND p.deleted_at IS NULL`
	var args []any
	if plantingID != "" {
		q += ` AND h.planting_id = ?`
		args = append(args, plantingID)
	}
	if year != nil {
		// The same clause the metric sums, so the page's own total and the
		// dashboard's cannot disagree — permanents included, by the calendar year
		// they were picked in.
		q += ` AND ` + harvestYearClause
		args = append(args, *year, itoaYear(*year))
	}
	if cursor != "" {
		q += ` AND h.id > ?`
		args = append(args, cursor)
	}
	q += ` ORDER BY h.id LIMIT ?`
	args = append(args, limit+1)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Harvest
	for rows.Next() {
		h, err := scanHarvest(rows)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, h)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	items, next := page(out, limit, func(h Harvest) string { return h.ID })
	return items, next, nil
}

func (s *Store) GetHarvest(ctx context.Context, db DBTX, id string) (Harvest, error) {
	if db == nil {
		db = s.db
	}
	row := db.QueryRowContext(ctx,
		`SELECT `+harvestCols+`, pl.name_cs, b.code
		 FROM garden_harvests h
		 JOIN garden_plantings p ON p.id = h.planting_id
		 JOIN garden_plants pl ON pl.id = p.plant_id
		 LEFT JOIN garden_beds b ON b.id = p.bed_id
		 WHERE h.id = ? AND h.deleted_at IS NULL`, id)
	h, err := scanHarvest(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Harvest{}, errNotFound
	}
	return h, err
}

func (s *Store) InsertHarvest(ctx context.Context, tx DBTX, h Harvest, createdBy, at string) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO garden_harvests (id, planting_id, harvested_on, quantity, unit,
		    destination, quality, note, created_by, created_at, updated_at)
		 VALUES (`+placeholders(11)+`)`,
		h.ID, h.PlantingID, h.HarvestedOn, h.Quantity, h.Unit,
		h.Destination, h.Quality, h.Note, createdBy, at, at)
	return err
}

func (s *Store) UpdateHarvest(ctx context.Context, tx DBTX, h Harvest, at string) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE garden_harvests SET harvested_on = ?, quantity = ?, unit = ?,
		    destination = ?, quality = ?, note = ?, updated_at = ?
		 WHERE id = ? AND deleted_at IS NULL`,
		h.HarvestedOn, h.Quantity, h.Unit, h.Destination, h.Quality, h.Note, at, h.ID)
	return err
}

func (s *Store) SoftDeleteHarvest(ctx context.Context, tx DBTX, id, at string) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE garden_harvests SET deleted_at = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL`, at, at, id)
	return err
}

// SeasonHarvestKg sums a season's harvest IN KG ONLY. Rows in ks/l/svazek are
// deliberately excluded rather than coerced — adding apples to bunches produces
// a number that is worse than no number, and the UI says so.
//
// A PERMANENT HAS NO SEASON (D106), so the season join alone would drop every
// apple and every rhubarb stalk — while garden.harvest_ready happily names those
// same crops as ready to pick. The two must agree, so a permanent's harvest
// counts toward the CALENDAR year it was picked in, which is the only year a
// tree has.
func (s *Store) SeasonHarvestKg(ctx context.Context, year int) (float64, error) {
	var total sql.NullFloat64
	err := s.db.QueryRowContext(ctx,
		`SELECT SUM(h.quantity) FROM garden_harvests h
		 JOIN garden_plantings p ON p.id = h.planting_id
		 WHERE h.deleted_at IS NULL AND h.unit = 'kg' AND p.deleted_at IS NULL
		   AND `+harvestYearClause, year, itoaYear(year)).Scan(&total)
	if err != nil {
		return 0, err
	}
	if !total.Valid {
		return 0, nil
	}
	return total.Float64, nil
}

// harvestYearClause selects the harvests belonging to one year: a seasonal
// planting's by its season, a permanent's by the calendar year it was picked in.
// Takes TWO binds — the year as an int, then as the four-character prefix of
// `harvested_on`. Shared so the Sklizeň page's total and the
// garden.harvest_season metric cannot answer differently.
const harvestYearClause = `(p.season_id = (SELECT id FROM garden_seasons WHERE year = ?)
	    OR (p.season_id IS NULL AND substr(h.harvested_on, 1, 4) = ?))`

// =============================== storage ===============================

const storageCols = `id, harvest_id, planting_id, product_name, method, location,
	quantity_initial, quantity_remaining, unit, stored_on, best_before, status, note,
	created_at, updated_at`

func scanStorage(sc scanner) (StorageItem, error) {
	var it StorageItem
	var harvestID, plantingID, location, bestBefore, note sql.NullString
	if err := sc.Scan(&it.ID, &harvestID, &plantingID, &it.ProductName, &it.Method, &location,
		&it.QuantityInitial, &it.QuantityRemaining, &it.Unit, &it.StoredOn, &bestBefore,
		&it.Status, &note, &it.CreatedAt, &it.UpdatedAt); err != nil {
		return StorageItem{}, err
	}
	it.HarvestID, it.PlantingID = nsp(harvestID), nsp(plantingID)
	it.Location, it.BestBefore, it.Note = nsp(location), nsp(bestBefore), nsp(note)
	return it, nil
}

func (s *Store) ListStorage(ctx context.Context, status string, limit int, cursor string) ([]StorageItem, *string, error) {
	q := `SELECT ` + storageCols + ` FROM garden_storage_items WHERE deleted_at IS NULL`
	var args []any
	if status != "" {
		q += ` AND status = ?`
		args = append(args, status)
	}
	if cursor != "" {
		q += ` AND id > ?`
		args = append(args, cursor)
	}
	q += ` ORDER BY id LIMIT ?`
	args = append(args, limit+1)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []StorageItem
	for rows.Next() {
		it, err := scanStorage(rows)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	items, next := page(out, limit, func(it StorageItem) string { return it.ID })
	return items, next, nil
}

func (s *Store) GetStorage(ctx context.Context, db DBTX, id string) (StorageItem, error) {
	if db == nil {
		db = s.db
	}
	row := db.QueryRowContext(ctx, `SELECT `+storageCols+` FROM garden_storage_items WHERE id = ? AND deleted_at IS NULL`, id)
	it, err := scanStorage(row)
	if errors.Is(err, sql.ErrNoRows) {
		return StorageItem{}, errNotFound
	}
	return it, err
}

func (s *Store) InsertStorage(ctx context.Context, tx DBTX, it StorageItem, createdBy, at string) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO garden_storage_items (`+storageCols+`, created_by) VALUES (`+placeholders(16)+`)`,
		it.ID, it.HarvestID, it.PlantingID, it.ProductName, it.Method, it.Location,
		it.QuantityInitial, it.QuantityRemaining, it.Unit, it.StoredOn, it.BestBefore,
		it.Status, it.Note, at, at, createdBy)
	return err
}

func (s *Store) UpdateStorage(ctx context.Context, tx DBTX, it StorageItem, at string) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE garden_storage_items SET product_name = ?, method = ?, location = ?,
		    quantity_remaining = ?, best_before = ?, status = ?, note = ?, updated_at = ?
		 WHERE id = ? AND deleted_at IS NULL`,
		it.ProductName, it.Method, it.Location, it.QuantityRemaining,
		it.BestBefore, it.Status, it.Note, at, it.ID)
	return err
}

func (s *Store) SoftDeleteStorage(ctx context.Context, tx DBTX, id, at string) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE garden_storage_items SET deleted_at = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL`, at, at, id)
	return err
}

// ================================ rules ================================

const ruleCols = `id, scope, a_ref, b_ref, verdict, severity, min_years_gap,
	reason_cs, source, is_builtin, is_disabled`

func scanRule(sc scanner) (Rule, error) {
	var r Rule
	var gap sql.NullInt64
	var reason, source sql.NullString
	var builtin, disabled int
	if err := sc.Scan(&r.ID, &r.Scope, &r.ARef, &r.BRef, &r.Verdict, &r.Severity, &gap,
		&reason, &source, &builtin, &disabled); err != nil {
		return Rule{}, err
	}
	r.MinYearsGap = nip(gap)
	r.ReasonCS, r.Source = nsp(reason), nsp(source)
	r.IsBuiltin, r.IsDisabled = builtin != 0, disabled != 0
	return r, nil
}

func (s *Store) ListRules(ctx context.Context, db DBTX, scope string, includeDisabled bool, limit int, cursor string) ([]Rule, *string, error) {
	if db == nil {
		db = s.db
	}
	q := `SELECT ` + ruleCols + ` FROM garden_rules WHERE deleted_at IS NULL`
	var args []any
	if scope != "" {
		q += ` AND scope = ?`
		args = append(args, scope)
	}
	if !includeDisabled {
		q += ` AND is_disabled = 0`
	}
	if cursor != "" {
		q += ` AND id > ?`
		args = append(args, cursor)
	}
	q += ` ORDER BY id LIMIT ?`
	args = append(args, limit+1)

	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Rule
	for rows.Next() {
		r, err := scanRule(rows)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	items, next := page(out, limit, func(r Rule) string { return r.ID })
	return items, next, nil
}

func (s *Store) GetRule(ctx context.Context, db DBTX, id string) (Rule, error) {
	if db == nil {
		db = s.db
	}
	row := db.QueryRowContext(ctx, `SELECT `+ruleCols+` FROM garden_rules WHERE id = ? AND deleted_at IS NULL`, id)
	r, err := scanRule(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Rule{}, errNotFound
	}
	return r, err
}

func (s *Store) InsertRule(ctx context.Context, tx DBTX, r Rule, createdBy, at string) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO garden_rules (`+ruleCols+`, created_by, created_at, updated_at)
		 VALUES (`+placeholders(14)+`)`,
		r.ID, r.Scope, r.ARef, r.BRef, r.Verdict, r.Severity, r.MinYearsGap,
		r.ReasonCS, r.Source, b2i(r.IsBuiltin), b2i(r.IsDisabled), createdBy, at, at)
	return err
}

func (s *Store) UpdateRule(ctx context.Context, tx DBTX, r Rule, at string) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE garden_rules SET verdict = ?, severity = ?, min_years_gap = ?,
		    reason_cs = ?, source = ?, is_disabled = ?, updated_at = ?
		 WHERE id = ? AND deleted_at IS NULL`,
		r.Verdict, r.Severity, r.MinYearsGap, r.ReasonCS, r.Source, b2i(r.IsDisabled), at, r.ID)
	return err
}

func (s *Store) SoftDeleteRule(ctx context.Context, tx DBTX, id, at string) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE garden_rules SET deleted_at = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL`, at, at, id)
	return err
}

// rulesForMatching loads the ENABLED rules with built-in plant pairs resolved
// from crop NAMES to crop IDS.
//
// Built-in plant_pair rules ship before any crop exists, so they name crops
// rather than pointing at them (see the seed migration's header). Resolving here
// keeps check.go's matcher simple — it only ever compares ids — and makes a rule
// about a crop this garden does not grow silently inert rather than a special
// case somebody has to remember.
func (s *Store) rulesForMatching(ctx context.Context, db DBTX) ([]Rule, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT `+ruleCols+` FROM garden_rules WHERE deleted_at IS NULL AND is_disabled = 0`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var raw []Rule
	for rows.Next() {
		r, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		raw = append(raw, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	byName, err := s.plantIDsByName(ctx, db)
	if err != nil {
		return nil, err
	}
	out := make([]Rule, 0, len(raw))
	for _, r := range raw {
		if r.Scope == ScopePlantPair {
			a, aok := resolvePlantRef(r.ARef, byName)
			b, bok := resolvePlantRef(r.BRef, byName)
			if !aok || !bok {
				continue // names a crop this garden does not grow — inert, not an error
			}
			r.ARef, r.BRef = canonicalPair(a, b)
		}
		out = append(out, r)
	}
	return out, nil
}

// plantIDsByName maps both the id and the normalised Czech name of every live
// crop onto its id, so resolvePlantRef needs one lookup either way.
func (s *Store) plantIDsByName(ctx context.Context, db DBTX) (map[string]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, name_cs FROM garden_plants WHERE deleted_at IS NULL`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]string{}
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out[id] = id
		out[normalize(name)] = id
	}
	return out, rows.Err()
}

func resolvePlantRef(ref string, byName map[string]string) (string, bool) {
	if id, ok := byName[ref]; ok {
		return id, true
	}
	id, ok := byName[normalize(ref)]
	return id, ok
}

// ============================= dismissals =============================

func (s *Store) Dismissals(ctx context.Context, db DBTX, seasonID string) (map[string]Dismissal, error) {
	if db == nil {
		db = s.db
	}
	rows, err := db.QueryContext(ctx,
		`SELECT warning_key, note, dismissed_by, dismissed_at FROM garden_warning_dismissals WHERE season_id = ?`, seasonID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]Dismissal{}
	for rows.Next() {
		var d Dismissal
		var note, by sql.NullString
		if err := rows.Scan(&d.Key, &note, &by, &d.DismissedAt); err != nil {
			return nil, err
		}
		d.Note, d.DismissedBy = nsp(note), nsp(by)
		out[d.Key] = d
	}
	return out, rows.Err()
}

func (s *Store) InsertDismissal(ctx context.Context, tx DBTX, id, seasonID, key string, note *string, by, at string) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO garden_warning_dismissals (id, season_id, warning_key, note, dismissed_by, dismissed_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT (season_id, warning_key) DO UPDATE SET note = excluded.note,
		     dismissed_by = excluded.dismissed_by, dismissed_at = excluded.dismissed_at`,
		id, seasonID, key, note, by, at)
	return err
}

func (s *Store) DeleteDismissal(ctx context.Context, tx DBTX, seasonID, key string) (bool, error) {
	res, err := tx.ExecContext(ctx,
		`DELETE FROM garden_warning_dismissals WHERE season_id = ? AND warning_key = ?`, seasonID, key)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// =============================== settings ===============================

func (s *Store) Settings(ctx context.Context, db DBTX) (Settings, error) {
	if db == nil {
		db = s.db
	}
	var st Settings
	var lastFrost, firstFrost sql.NullString
	var lat, lon sql.NullFloat64
	var alt sql.NullInt64
	var checksJSON string
	err := db.QueryRowContext(ctx,
		`SELECT default_last_frost, default_first_frost, latitude, longitude, altitude_m,
		        rotation_break_default, frost_threshold_c, frost_lookahead_days,
		        workload_week_threshold, checks_config, updated_at
		 FROM garden_settings WHERE id = 1`).
		Scan(&lastFrost, &firstFrost, &lat, &lon, &alt,
			&st.RotationBreakDefault, &st.FrostThresholdC, &st.FrostLookaheadDays,
			&st.WorkloadWeekThreshold, &checksJSON, &st.UpdatedAt)
	if err != nil {
		return Settings{}, err
	}
	st.DefaultLastFrost, st.DefaultFirstFrost = nsp(lastFrost), nsp(firstFrost)
	st.Latitude, st.Longitude, st.AltitudeM = nfp(lat), nfp(lon), nip(alt)
	st.Checks = ChecksConfig{}
	if checksJSON != "" {
		_ = json.Unmarshal([]byte(checksJSON), &st.Checks)
	}
	return st, nil
}

func (s *Store) UpdateSettings(ctx context.Context, tx DBTX, st Settings, at string) error {
	checks, err := json.Marshal(st.Checks)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx,
		`UPDATE garden_settings SET default_last_frost = ?, default_first_frost = ?,
		    latitude = ?, longitude = ?, altitude_m = ?, rotation_break_default = ?,
		    frost_threshold_c = ?, frost_lookahead_days = ?, workload_week_threshold = ?,
		    checks_config = ?, updated_at = ?
		 WHERE id = 1`,
		st.DefaultLastFrost, st.DefaultFirstFrost, st.Latitude, st.Longitude, st.AltitudeM,
		st.RotationBreakDefault, st.FrostThresholdC, st.FrostLookaheadDays,
		st.WorkloadWeekThreshold, string(checks), at)
	return err
}

// ================================ weather ================================

func (s *Store) UpsertWeather(ctx context.Context, tx DBTX, days []WeatherDay, source, at string) error {
	for _, d := range days {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO garden_weather_days (day, temp_min, temp_max, precip_mm, fetched_at, source)
			 VALUES (?, ?, ?, ?, ?, ?)
			 ON CONFLICT (day) DO UPDATE SET temp_min = excluded.temp_min, temp_max = excluded.temp_max,
			     precip_mm = excluded.precip_mm, fetched_at = excluded.fetched_at, source = excluded.source`,
			d.Day, d.TempMin, d.TempMax, d.PrecipMM, at, source); err != nil {
			return err
		}
	}
	return nil
}

// PruneWeather drops rows older than the retention window. The cache exists to
// answer "is tonight a frost risk"; last spring's forecast answers nothing.
func (s *Store) PruneWeather(ctx context.Context, before string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM garden_weather_days WHERE day < ?`, before)
	return err
}

func (s *Store) WeatherRange(ctx context.Context, from, to string) ([]WeatherDay, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT day, temp_min, temp_max, precip_mm, fetched_at, source
		 FROM garden_weather_days WHERE day >= ? AND day <= ? ORDER BY day`, from, to)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []WeatherDay
	for rows.Next() {
		var d WeatherDay
		var min, max, precip sql.NullFloat64
		var fetched, source sql.NullString
		if err := rows.Scan(&d.Day, &min, &max, &precip, &fetched, &source); err != nil {
			return nil, err
		}
		d.TempMin, d.TempMax, d.PrecipMM = nfp(min), nfp(max), nfp(precip)
		d.FetchedAt, d.Source = nsp(fetched), nsp(source)
		out = append(out, d)
	}
	return out, rows.Err()
}

// WeatherDayFor returns one cached day, or ok=false when nothing is cached.
// A missing forecast is not an error — the metric resolves null and every
// condition gating on it stays silent (D112).
func (s *Store) WeatherDayFor(ctx context.Context, day string) (WeatherDay, bool, error) {
	days, err := s.WeatherRange(ctx, day, day)
	if err != nil || len(days) == 0 {
		return WeatherDay{}, false, err
	}
	return days[0], true, nil
}

// ======================= exhaustive (uncapped) reads =======================

// The all* readers follow the cursor to exhaustion. The paged readers exist for
// the API, where a page size is a contract; the check engine and the export are
// the opposite case — they need the WHOLE set or their answer is a guess, and a
// cap that quietly drops the 201st row is a wrong answer nobody can see.
//
// Bounded by maxExhaustivePages so a cursor bug can never spin here forever.
const maxExhaustivePages = 200

func (s *Store) allPlantings(ctx context.Context, db DBTX, f PlantingFilter) ([]Planting, error) {
	var out []Planting
	cursor := ""
	for i := 0; i < maxExhaustivePages; i++ {
		items, next, err := s.ListPlantings(ctx, db, f, maxLimit, cursor)
		if err != nil {
			return nil, err
		}
		out = append(out, items...)
		if next == nil {
			return out, nil
		}
		cursor = *next
	}
	return out, nil
}

func (s *Store) allTasks(ctx context.Context, db DBTX, f TaskFilter) ([]Task, error) {
	var out []Task
	cursor := ""
	for i := 0; i < maxExhaustivePages; i++ {
		items, next, err := s.ListTasks(ctx, db, f, maxLimit, cursor)
		if err != nil {
			return nil, err
		}
		out = append(out, items...)
		if next == nil {
			return out, nil
		}
		cursor = *next
	}
	return out, nil
}

// allPlants, allVarieties and allRules serve the knowledge-base EXPORT, which is
// a backup and a way to move the knowledge base between installs — a file that
// silently stops at the 200th crop restores to a different garden than the one
// it came from.
func (s *Store) allPlants(ctx context.Context, f PlantFilter) ([]Plant, error) {
	var out []Plant
	cursor := ""
	for i := 0; i < maxExhaustivePages; i++ {
		items, next, err := s.ListPlants(ctx, f, maxLimit, cursor)
		if err != nil {
			return nil, err
		}
		out = append(out, items...)
		if next == nil {
			return out, nil
		}
		cursor = *next
	}
	return out, nil
}

func (s *Store) allVarieties(ctx context.Context, plantID string) ([]Variety, error) {
	var out []Variety
	cursor := ""
	for i := 0; i < maxExhaustivePages; i++ {
		items, next, err := s.ListVarieties(ctx, plantID, maxLimit, cursor)
		if err != nil {
			return nil, err
		}
		out = append(out, items...)
		if next == nil {
			return out, nil
		}
		cursor = *next
	}
	return out, nil
}

func (s *Store) allRules(ctx context.Context, db DBTX, scope string, includeDisabled bool) ([]Rule, error) {
	var out []Rule
	cursor := ""
	for i := 0; i < maxExhaustivePages; i++ {
		items, next, err := s.ListRules(ctx, db, scope, includeDisabled, maxLimit, cursor)
		if err != nil {
			return nil, err
		}
		out = append(out, items...)
		if next == nil {
			return out, nil
		}
		cursor = *next
	}
	return out, nil
}

// SeasonSnapshot loads EVERYTHING the check engine needs in one call.
//
// This exists so check.go can be pure: it takes this struct and touches no
// database, which is what makes each of the eleven checks a table-driven test.
// Five queries, not N+1 — the crop record for every planting is resolved once
// here rather than per check.
func (s *Store) SeasonSnapshot(ctx context.Context, db DBTX, year int, today dates.Date) (Snapshot, error) {
	if db == nil {
		db = s.db
	}
	season, err := s.SeasonByYear(ctx, db, year)
	if err != nil {
		return Snapshot{}, err
	}
	snap := Snapshot{Season: season, Today: today, Effective: map[string]Effective{}}

	if snap.Beds, err = s.ListBeds(ctx, db, false); err != nil {
		return Snapshot{}, err
	}
	// Permanents occupy their beds in every season, so the snapshot carries them
	// alongside the year's own plantings — a fruit tree still shades the bed next
	// to it, and C11 must see that.
	//
	// THE SNAPSHOT PAGES TO EXHAUSTION. A single maxLimit read would silently cap
	// the check engine's input at 200 rows — and a check that ran on two thirds
	// of the garden and reported nothing is a green tick that has not been
	// earned, which is precisely what this module refuses to show.
	seasonal, err := s.allPlantings(ctx, db, PlantingFilter{Year: &year})
	if err != nil {
		return Snapshot{}, err
	}
	permanent, err := s.allPlantings(ctx, db, PlantingFilter{Permanent: bp(true)})
	if err != nil {
		return Snapshot{}, err
	}
	snap.Plantings = append(seasonal, permanent...)

	if err := s.resolveEffective(ctx, db, snap.Plantings, snap.Effective); err != nil {
		return Snapshot{}, err
	}
	if snap.Rules, err = s.rulesForMatching(ctx, db); err != nil {
		return Snapshot{}, err
	}
	if snap.History, err = s.allBedHistory(ctx, db); err != nil {
		return Snapshot{}, err
	}
	if snap.ClosedSeasons, err = s.ClosedSeasonCount(ctx, db); err != nil {
		return Snapshot{}, err
	}
	if snap.Dismissals, err = s.Dismissals(ctx, db, season.ID); err != nil {
		return Snapshot{}, err
	}
	if snap.Settings, err = s.Settings(ctx, db); err != nil {
		return Snapshot{}, err
	}
	if snap.Tasks, err = s.allTasks(ctx, db, TaskFilter{Year: &year}); err != nil {
		return Snapshot{}, err
	}
	return snap, nil
}

// resolveEffective loads the crops and varieties for a set of plantings in two
// queries and resolves each pair ONCE, through the one shared Resolve function.
func (s *Store) resolveEffective(ctx context.Context, db DBTX, plantings []Planting, out map[string]Effective) error {
	if len(plantings) == 0 {
		return nil
	}
	plantIDs := map[string]bool{}
	varietyIDs := map[string]bool{}
	for _, p := range plantings {
		plantIDs[p.PlantID] = true
		if p.VarietyID != nil {
			varietyIDs[*p.VarietyID] = true
		}
	}
	plants, err := s.plantsByID(ctx, db, keysOf(plantIDs))
	if err != nil {
		return err
	}
	varieties, err := s.varietiesByID(ctx, db, keysOf(varietyIDs))
	if err != nil {
		return err
	}
	for _, p := range plantings {
		plant, ok := plants[p.PlantID]
		if !ok {
			continue
		}
		var v *Variety
		if p.VarietyID != nil {
			if found, ok := varieties[*p.VarietyID]; ok {
				v = &found
			}
		}
		out[p.ID] = Resolve(plant, v)
	}
	return nil
}

func (s *Store) plantsByID(ctx context.Context, db DBTX, ids []string) (map[string]Plant, error) {
	out := map[string]Plant{}
	if len(ids) == 0 {
		return out, nil
	}
	args := toAny(ids)
	rows, err := db.QueryContext(ctx,
		`SELECT `+plantIdentCols+`, `+coreColumns+` FROM garden_plants
		 WHERE id IN (`+placeholders(len(args))+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		p, err := scanPlant(rows)
		if err != nil {
			return nil, err
		}
		out[p.ID] = p
	}
	return out, rows.Err()
}

func (s *Store) varietiesByID(ctx context.Context, db DBTX, ids []string) (map[string]Variety, error) {
	out := map[string]Variety{}
	if len(ids) == 0 {
		return out, nil
	}
	args := toAny(ids)
	rows, err := db.QueryContext(ctx,
		`SELECT `+varietyIdentCols+`, `+coreColumns+` FROM garden_varieties
		 WHERE id IN (`+placeholders(len(args))+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		v, err := scanVariety(rows)
		if err != nil {
			return nil, err
		}
		out[v.ID] = v
	}
	return out, rows.Err()
}

// EffectiveFor resolves one planting's crop record, for the single-planting
// paths (create, update, generate) that do not need a whole snapshot.
func (s *Store) EffectiveFor(ctx context.Context, db DBTX, plantID string, varietyID *string) (Effective, error) {
	if db == nil {
		db = s.db
	}
	plant, err := s.GetPlant(ctx, db, plantID)
	if err != nil {
		return Effective{}, err
	}
	var v *Variety
	if varietyID != nil && *varietyID != "" {
		found, err := s.GetVariety(ctx, db, *varietyID)
		if err != nil {
			return Effective{}, err
		}
		v = &found
	}
	return Resolve(plant, v), nil
}

// ============================== helpers ==============================

// page trims the limit+1 probe row and returns the next cursor.
func page[T any](items []T, limit int, id func(T) string) ([]T, *string) {
	if len(items) <= limit {
		return items, nil
	}
	items = items[:limit]
	next := id(items[len(items)-1])
	return items, &next
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func toAny(in []string) []any {
	out := make([]any, len(in))
	for i, v := range in {
		out[i] = v
	}
	return out
}

func containsStr(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

func encodeStrings(v []string) string {
	if len(v) == 0 {
		return "[]"
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "[]"
	}
	return string(b)
}

func decodeStrings(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil
	}
	return out
}

// ftsQuery turns free text into a safe FTS5 prefix query. Every term is quoted,
// so a stray `"` or a bare `NEAR` from a search box cannot become syntax — the
// same defensive shape notes and documents use.
func ftsQuery(q string) string {
	fields := strings.Fields(q)
	parts := make([]string, 0, len(fields))
	for _, f := range fields {
		f = strings.ReplaceAll(f, `"`, "")
		if f == "" {
			continue
		}
		parts = append(parts, `"`+f+`"*`)
	}
	if len(parts) == 0 {
		return `""`
	}
	return strings.Join(parts, " ")
}

