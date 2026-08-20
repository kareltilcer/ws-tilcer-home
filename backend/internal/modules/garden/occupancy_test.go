package garden

import "testing"

// planted is a planting shorthand: planned direct sowing and harvest end.
func planted(id string, bed string, sowDirect, harvestTo string) Planting {
	p := Planting{ID: id, BedID: sp(bed)}
	if sowDirect != "" {
		p.SowDirectOn = sp(sowDirect)
	}
	if harvestTo != "" {
		p.HarvestTo = sp(harvestTo)
	}
	return p
}

// THE CASE THIS WHOLE FILE EXISTS FOR.
//
// Spring špenát is out of the bed before autumn pórek goes in. They share a bed
// on the calendar and never share it in reality, so no bed-level rule may join
// them. A companion warning here is the one that teaches the household to stop
// reading the panel.
func TestOverlapSpenatPorek(t *testing.T) {
	spenat := planted("spenat", "A1", "2027-03-15", "2027-05-30")
	porek := planted("porek", "A1", "2027-07-01", "2027-11-15")

	if Overlap(spenat, porek) {
		t.Error("spring špenát and autumn pórek in one bed must NOT overlap — they never meet")
	}
	if Overlap(porek, spenat) {
		t.Error("overlap must be symmetric")
	}
}

func TestOverlapCases(t *testing.T) {
	cases := []struct {
		name string
		a, b Planting
		want bool
	}{
		{
			"same bed, same summer",
			planted("a", "A1", "2027-05-01", "2027-09-01"),
			planted("b", "A1", "2027-06-01", "2027-08-01"),
			true,
		},
		{
			"back to back — cleared the day the next is sown",
			planted("a", "A1", "2027-03-01", "2027-06-01"),
			planted("b", "A1", "2027-06-01", "2027-09-01"),
			false, // a succession, not a conflict
		},
		{
			"one day of overlap",
			planted("a", "A1", "2027-03-01", "2027-06-02"),
			planted("b", "A1", "2027-06-01", "2027-09-01"),
			true,
		},
		{
			"no dates at all is open-ended and overlaps everything",
			Planting{ID: "a", BedID: sp("A1")},
			planted("b", "A1", "2027-06-01", "2027-09-01"),
			true, // conservative: an unplanned planting should provoke a question
		},
		{
			"no end date is open-ended forwards",
			planted("a", "A1", "2027-03-01", ""),
			planted("b", "A1", "2027-09-01", "2027-11-01"),
			true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Overlap(tc.a, tc.b); got != tc.want {
				t.Errorf("Overlap = %v, want %v", got, tc.want)
			}
			if got := Overlap(tc.b, tc.a); got != tc.want {
				t.Errorf("reversed Overlap = %v, want %v (must be symmetric)", got, tc.want)
			}
		})
	}
}

// The actual date wins over the planned one: once a crop is really in the
// ground, that is when it started occupying the bed, whatever February thought.
func TestOccupancyPrefersActualDates(t *testing.T) {
	p := planted("a", "A1", "2027-04-01", "2027-08-01")
	p.SowedOn = sp("2027-04-20") // two and a half weeks late

	from, to := p.OccupancyWindow()
	if from == nil || from.String() != "2027-04-20" {
		t.Errorf("occupancy start = %v, want the ACTUAL sowing date 2027-04-20", from)
	}
	if to == nil || to.String() != "2027-08-01" {
		t.Errorf("occupancy end = %v, want the planned harvest end", to)
	}

	p.ClearedOn = sp("2027-07-10") // cleared early
	if _, to := p.OccupancyWindow(); to == nil || to.String() != "2027-07-10" {
		t.Errorf("occupancy end = %v, want the ACTUAL clearing date", to)
	}
}

// A permanent (D106) is a planting with no season — one table, one code path —
// and it occupies its spot from the day it was planted.
func TestOccupancyPermanent(t *testing.T) {
	tree := Planting{ID: "jabloň", BedID: sp("sad"), PlantedOn: sp("2019-11-02")}
	if !tree.IsPermanent() {
		t.Error("a planting with no season is a permanent")
	}
	from, to := tree.OccupancyWindow()
	if from == nil || from.String() != "2019-11-02" {
		t.Errorf("permanent start = %v, want its planted_on", from)
	}
	if to != nil {
		t.Errorf("a permanent that has not been removed has no occupancy end, got %v", to)
	}
	if !tree.OccupiesOn(d("2027-06-01")) {
		t.Error("a permanent occupies its spot in every later season")
	}
}

// OccupiesOn is the frost evaluation's question, and it answers it differently
// from Overlap on purpose: a planting with no dates is NOT "in the ground
// tonight". Open-ended is right where a question is cheap and wrong where it
// would cry wolf.
func TestOccupiesOnRequiresARealStart(t *testing.T) {
	undated := Planting{ID: "a", BedID: sp("A1")}
	if undated.OccupiesOn(d("2027-06-01")) {
		t.Error("an undated planting must not count as being in the ground tonight")
	}

	real := planted("b", "A1", "2027-05-01", "2027-09-01")
	for _, tc := range []struct {
		day  string
		want bool
	}{
		{"2027-04-30", false},
		{"2027-05-01", true},
		{"2027-07-15", true},
		{"2027-09-01", true},
		{"2027-09-02", false},
	} {
		if got := real.OccupiesOn(d(tc.day)); got != tc.want {
			t.Errorf("OccupiesOn(%s) = %v, want %v", tc.day, got, tc.want)
		}
	}
}

func TestOccupancyWire(t *testing.T) {
	p := planted("a", "A1", "2027-05-01", "2027-09-01")
	w := p.OccupancyWire()
	if w.From == nil || *w.From != "2027-05-01" || w.To == nil || *w.To != "2027-09-01" {
		t.Errorf("wire occupancy = %+v", w)
	}
	empty := Planting{ID: "b"}.OccupancyWire()
	if empty.From != nil || empty.To != nil {
		t.Errorf("an undated planting serialises both bounds as null, got %+v", empty)
	}
}
