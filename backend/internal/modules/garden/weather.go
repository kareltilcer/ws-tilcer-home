package garden

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/dates"
	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/db"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
)

// WEATHER AND FROST PUBLICATION (PRD D112, D113, FR-G15).
//
// THE MODULE SENDS NO PUSH. It imports no platform/push, stores no audience and
// has no notification settings. It publishes exactly three things and stops:
//
//   - the metric  garden.frost_risk_tonight
//   - the list    garden.frost_sensitive_now
//   - one idempotent `garden.frost_warning` audit event per night, whose Czech
//     summary already reads as a finished notification.
//
// Delivery is then chosen in Administrace AT RUNTIME rather than in this file:
// either a scheduled summary conditioned `garden.frost_risk_tonight lte 2`, or a
// trigger rule on the audit event. Both work on day one because the module
// publishes for both. Do not add a third path.
//
// The whole thing is SOFT. Every failure is logged and swallowed: the page
// renders from cache or without weather, the metric resolves null, and any
// condition gating on it stays silent. A forecast that did not load is not
// something anyone can act on, so there is nothing to show them.

const weatherSource = "open-meteo"

// weatherRetentionDays is how far back the cache is kept. The forecast answers
// one question — is tonight a frost risk — and last spring's answer is not worth
// a row.
const weatherRetentionDays = 90

// WeatherJob is the scheduler job registered by cmd/home. It is a method value
// rather than a type so the module exposes exactly one thing to wire.
//
// A failure is logged here with its garden context and RETURNED so the
// scheduler retries sooner than the full poll interval — nothing downstream
// ever sees it (the page still renders from cache, the metric still resolves
// null), so the package note's "swallowed" still holds for everyone but the
// retry pacing.
func (s *Service) WeatherJob(ctx context.Context, now time.Time) error {
	if !s.opts.WeatherEnabled {
		return nil
	}
	if err := s.pollWeather(ctx); err != nil {
		s.logger.Warn("garden: weather poll failed", "err", err)
		return err
	}
	if err := s.evaluateFrost(ctx); err != nil {
		s.logger.Warn("garden: frost evaluation failed", "err", err)
		return err
	}
	return nil
}

// pollWeather fetches and caches the forecast for the garden's own coordinates.
func (s *Service) pollWeather(ctx context.Context) error {
	settings, err := s.store.Settings(ctx, nil)
	if err != nil {
		return err
	}
	// NO COORDINATES ⇒ DO NOTHING. Not an error: a garden that has not set its
	// location simply has no forecast, and saying so in a log line every twelve
	// hours would be noise about a state the household chose.
	if settings.Latitude == nil || settings.Longitude == nil {
		s.logger.Debug("garden: weather skipped — no coordinates set")
		return nil
	}

	days, err := s.fetchForecast(ctx, *settings.Latitude, *settings.Longitude)
	if err != nil {
		return err
	}
	at := nowUTC()
	if err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		return s.store.UpsertWeather(ctx, tx, days, weatherSource, at)
	}); err != nil {
		return err
	}
	if err := s.store.PruneWeather(ctx, s.today().AddDays(-weatherRetentionDays).String()); err != nil {
		return err
	}
	s.logger.Info("garden: weather fetched", "days", len(days), "source", weatherSource)
	return nil
}

// openMeteoResponse is the slice of the provider's payload we read.
//
// The daily arrays are POINTERS because the provider writes `null` for a day it
// has no value for, and encoding/json unmarshals a null into a plain float64 as
// a silent 0 — which is a real temperature. A missing minimum stored as 0 °C
// would sit below the default frost threshold and fire a frost warning for a
// night with no forecast at all, so "absent" has to stay distinguishable from
// zero here exactly as it is in FrostRiskTonight.
type openMeteoResponse struct {
	Daily struct {
		Time             []string   `json:"time"`
		TemperatureMin   []*float64 `json:"temperature_2m_min"`
		TemperatureMax   []*float64 `json:"temperature_2m_max"`
		PrecipitationSum []*float64 `json:"precipitation_sum"`
	} `json:"daily"`
}

// fetchForecast is the module's ONLY outbound HTTP call, and the only new egress
// v7 adds: one fixed host, no credentials, no user data in the query beyond the
// coordinates, and a hard timeout so a hanging forecast cannot hold a scheduler
// tick.
func (s *Service) fetchForecast(ctx context.Context, lat, lon float64) ([]WeatherDay, error) {
	base := s.opts.WeatherURL
	if base == "" {
		return nil, fmt.Errorf("garden: no weather URL configured")
	}
	u, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("latitude", strconv.FormatFloat(lat, 'f', 4, 64))
	q.Set("longitude", strconv.FormatFloat(lon, 'f', 4, 64))
	q.Set("daily", "temperature_2m_min,temperature_2m_max,precipitation_sum")
	q.Set("timezone", s.opts.Location.String())
	u.RawQuery = q.Encode()

	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("garden: forecast returned %s", resp.Status)
	}

	var payload openMeteoResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	out := make([]WeatherDay, 0, len(payload.Daily.Time))
	for i, day := range payload.Daily.Time {
		d := WeatherDay{Day: day}
		// A null reading stays null all the way to the column: the metric then
		// resolves to ErrNoData rather than to a number nobody measured.
		if i < len(payload.Daily.TemperatureMin) && payload.Daily.TemperatureMin[i] != nil {
			d.TempMin = fp(*payload.Daily.TemperatureMin[i])
		}
		if i < len(payload.Daily.TemperatureMax) && payload.Daily.TemperatureMax[i] != nil {
			d.TempMax = fp(*payload.Daily.TemperatureMax[i])
		}
		if i < len(payload.Daily.PrecipitationSum) && payload.Daily.PrecipitationSum[i] != nil {
			d.PrecipMM = fp(*payload.Daily.PrecipitationSum[i])
		}
		out = append(out, d)
	}
	return out, nil
}

// evaluateFrost writes at most ONE garden.frost_warning per frost date.
//
// The event's Czech summary is already a finished notification — that is the
// whole design (D113): Administrace's trigger rule defaults its body to the
// event's summary, so a frost alert reaches a phone with no notification code in
// this module at all.
func (s *Service) evaluateFrost(ctx context.Context) error {
	settings, err := s.store.Settings(ctx, nil)
	if err != nil {
		return err
	}
	// The scan STARTS AT TONIGHT'S ROW, not at today's. A night spans midnight,
	// so by the time an afternoon poll runs, today's daily minimum was reached
	// before dawn and has already passed — warning about it would tell the
	// household to cover up for a night that is actually mild. nightRow picks the
	// row that still lies ahead.
	tonight := nightRow(time.Now().In(s.opts.Location))
	// The household's frost_lookahead_days decides how far into the cached
	// forecast to look: a frost three nights out is still tonight's job —
	// covering up, or a weekend away. The FIRST frosty night wins.
	lookahead := settings.FrostLookaheadDays
	if lookahead < 1 {
		lookahead = 1
	}
	frostDay := tonight
	var tempMin float64
	found := false
	for i := 0; i < lookahead && !found; i++ {
		d := tonight.AddDays(i)
		day, ok, err := s.store.WeatherDayFor(ctx, d.String())
		if err != nil {
			return err
		}
		if !ok || day.TempMin == nil {
			continue // beyond the cached forecast ⇒ nothing to say for that night
		}
		if *day.TempMin <= settings.FrostThresholdC {
			frostDay, tempMin, found = d, *day.TempMin, true
		}
	}
	if !found {
		return nil
	}

	// Asked about frostDay, not about today: with a lookahead the summary names a
	// future night, and the crops it lists have to be the ones in the ground THAT
	// night. The event is idempotent per frost date, so a list built for the wrong
	// day is the only one that night ever gets.
	sensitive, err := s.frostSensitiveNow(ctx, frostDay)
	if err != nil {
		return err
	}
	if len(sensitive) == 0 {
		return nil // cold, but nothing outside that minds
	}

	// Idempotent on the frost date: a catch-up tick, a restart, a second poll
	// the same evening or an earlier lookahead hit on the same night must not
	// produce a second warning. Checked inside the transaction that writes it,
	// so two ticks racing cannot both pass.
	return appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		already, err := audit.Exists(ctx, tx, audit.ModuleGarden, "frost_warning", frostDay.String())
		if err != nil {
			return err
		}
		if already {
			return nil
		}
		summary := frostSummaryCS(frostDay, tonight, tempMin, sensitive)
		// actor_type "system": nobody did this, the weather did. The precedent is
		// logging.prune.
		sysCtx := reqctx.WithActor(ctx, reqctx.Actor{Type: "system", Label: "zahrada"})
		_, err = s.sink.Record(sysCtx, tx, audit.Event{
			Module:     audit.ModuleGarden,
			Action:     "frost_warning",
			EntityType: "garden_season",
			EntityID:   frostDay.String(),
			Summary:    summary,
			Level:      audit.LevelWarn,
			Meta:       map[string]any{"temp_min": tempMin, "count": len(sensitive)},
		})
		return err
	})
}

// frostSummaryCS builds the notification sentence: "Dnes v noci −2 °C. Citlivé:
// rajčata (A1), papriky (A2), cukety (B1)." A frost further out (the
// frost_lookahead_days setting) names its night: "V noci na 24. září −2 °C. …"
func frostSummaryCS(day, tonight dates.Date, tempMin float64, sensitive []string) string {
	temp := strconv.FormatFloat(tempMin, 'f', -1, 64)
	// Czech uses the minus sign, not a hyphen, and the difference is visible at
	// notification size.
	temp = strings.Replace(temp, "-", "−", 1)
	when := "Dnes v noci"
	// `tonight` is already the row that carries tonight's low (see nightRow), so
	// anything past it is a night further out and names its own date.
	if !day.Equal(tonight) {
		when = "V noci na " + czDateWords(day)
	}
	return fmt.Sprintf("%s %s °C. Citlivé: %s.", when, temp, strings.Join(sensitive, ", "))
}

// frostSensitiveNow lists the tender and half-hardy plantings that are ACTUALLY
// IN THE GROUND on day `on`, formatted "rajčata (A1)".
//
// The day is a PARAMETER rather than s.today() because the frost evaluation may
// be talking about a night up to frost_lookahead_days away: a message that names
// 24 September must list what is outside on 24 September, not what happens to be
// outside as the job runs.
//
// Shared by the frost evaluation and by the garden.frost_sensitive_now list, so
// the notification and the list behind it can never name different crops.
func (s *Service) frostSensitiveNow(ctx context.Context, on dates.Date) ([]string, error) {
	// EXHAUSTIVE, not one page — the same reason SeasonSnapshot pages to
	// exhaustion. A single maxLimit read would drop everything past the 200th
	// planting by id, and a frost warning that names two thirds of the tender
	// crops standing outside is worse than one that names none: nothing says it
	// was cut, so the missing rows read as "nothing to cover up".
	year := on.Y
	plantings, err := s.store.allPlantings(ctx, nil, PlantingFilter{Year: &year})
	if err != nil {
		return nil, err
	}
	// Overwintering crops belong to the PREVIOUS season's plan but are still in
	// the ground on a January night (the state even has a name: přezimování), so
	// the year filter alone would miss exactly the plantings a cross-year frost
	// threatens. OccupiesOn drops whatever was cleared, and dedupe absorbs any
	// label the two season queries both return.
	prevYear := year - 1
	previous, err := s.store.allPlantings(ctx, nil, PlantingFilter{Year: &prevYear})
	if err != nil {
		return nil, err
	}
	plantings = append(plantings, previous...)
	permanent, err := s.store.allPlantings(ctx, nil, PlantingFilter{Permanent: bp(true)})
	if err != nil {
		return nil, err
	}
	plantings = append(plantings, permanent...)

	eff := map[string]Effective{}
	if err := s.store.resolveEffective(ctx, s.db, plantings, eff); err != nil {
		return nil, err
	}
	var out []string
	for _, p := range plantings {
		e, ok := eff[p.ID]
		if !ok || !e.IsFrostSensitive() {
			continue
		}
		if !p.OccupiesOn(on) {
			continue
		}
		label := e.PlantName
		if p.BedCode != nil && *p.BedCode != "" {
			label += " (" + *p.BedCode + ")"
		}
		out = append(out, label)
	}
	sort.Strings(out)
	return dedupe(out), nil
}

// preDawnHour is when the coldest hour of a night is taken to be past. Before
// it, the night in progress is still today's daily row; from it onward, the next
// low a household can act on is tomorrow's.
//
// It is a constant rather than a setting because the only thing that turns on it
// is which of two cached rows answers "tonight", and no household has an opinion
// about that.
const preDawnHour = 6

// nightRow returns the daily row that carries TONIGHT's minimum — the one night
// somebody can still do something about.
//
// A night SPANS MIDNIGHT and the provider's minimum for date D covers 00:00–24:00
// of D, so the coldest hour of the night that starts this evening lands in
// TOMORROW's row. Today's row holds a minimum that was reached before dawn and
// has already passed by the time an afternoon tick runs — reporting it as
// tonight's risk is how a mild evening gets a frost warning.
func nightRow(local time.Time) dates.Date {
	d := dates.New(local.Year(), local.Month(), local.Day())
	if local.Hour() >= preDawnHour {
		return d.AddDays(1)
	}
	return d
}

// FrostRiskTonight returns tonight's cached forecast minimum, or ok=false when
// nothing is cached. NULL rather than zero is the point: 0 °C is a real
// temperature, and a metric that reported it for "no data" would make every
// condition gating on it fire on the coldest possible reading.
//
// "Tonight" spans midnight: for an afternoon or evening asOf the coming pre-dawn
// low sits in TOMORROW's daily row, for a small-hours asOf in today's. EXACTLY
// ONE of them is read — see nightRow — so the metric never presents a minimum
// that already passed this morning as tonight's risk.
func (s *Service) FrostRiskTonight(ctx context.Context, asOf time.Time) (float64, bool, error) {
	local := asOf.In(s.opts.Location)
	row, found, err := s.store.WeatherDayFor(ctx, nightRow(local).String())
	if err != nil || !found || row.TempMin == nil {
		return 0, false, err
	}
	return *row.TempMin, true, nil
}
