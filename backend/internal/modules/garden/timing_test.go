package garden

import (
	"testing"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/dates"
)

func d(s string) dates.Date {
	v, err := dates.Parse(s)
	if err != nil {
		panic(err)
	}
	return v
}

func dp(s string) *dates.Date { v := d(s); return &v }

func TestWindowResolveAnchors(t *testing.T) {
	// 2027 starts on a Friday, so ISO week 1 is 4–10 January.
	anchors := SeasonAnchors{Year: 2027, LastFrost: dp("2027-05-15"), FirstFrost: dp("2027-10-05")}

	cases := []struct {
		name       string
		win        Window
		from, to   string
		wantOK     bool
	}{
		{"week 10 to 13", Window{AnchorWeek, 10, 13}, "2027-03-08", "2027-04-04", true},
		{"single week", Window{AnchorWeek, 1, 1}, "2027-01-04", "2027-01-10", true},
		{"six to eight weeks before last frost", Window{AnchorLastFrost, -56, -42}, "2027-03-20", "2027-04-03", true},
		{"after last frost", Window{AnchorLastFrost, 0, 14}, "2027-05-15", "2027-05-29", true},
		{"before first frost", Window{AnchorFirstFrost, -30, -10}, "2027-09-05", "2027-09-25", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			from, to, ok := tc.win.Resolve(anchors)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if from.String() != tc.from || to.String() != tc.to {
				t.Errorf("got %s…%s, want %s…%s", from, to, tc.from, tc.to)
			}
		})
	}
}

// The whole point of anchors: when a season's frost dates move, frost-anchored
// windows move WITH them and week-anchored ones stay exactly where they were.
// Both halves matter, and both are the intent.
func TestWindowResolveFrostAnchorMovesWeekAnchorDoesNot(t *testing.T) {
	early := SeasonAnchors{Year: 2027, LastFrost: dp("2027-05-15")}
	late := SeasonAnchors{Year: 2027, LastFrost: dp("2027-05-25")} // ten days later

	frostWin := Window{AnchorLastFrost, -14, 0}
	weekWin := Window{AnchorWeek, 20, 21}

	f1, t1, _ := frostWin.Resolve(early)
	f2, t2, _ := frostWin.Resolve(late)
	if f1.DaysUntil(f2) != 10 || t1.DaysUntil(t2) != 10 {
		t.Errorf("frost-anchored window did not follow the frost date: %s…%s then %s…%s", f1, t1, f2, t2)
	}

	w1f, w1t, _ := weekWin.Resolve(early)
	w2f, w2t, _ := weekWin.Resolve(late)
	if !w1f.Equal(w2f) || !w1t.Equal(w2t) {
		t.Errorf("week-anchored window moved with the frost date: %s…%s then %s…%s", w1f, w1t, w2f, w2t)
	}
}

// A year with no ISO week 53 clamps to week 52 rather than letting the window
// slide into the following January — the same species of deviation as D19's
// short-month clamp, and for the same reason.
func TestWindowResolveWeek53Clamp(t *testing.T) {
	if got := weeksInISOYear(2026); got != 53 {
		t.Fatalf("2026 should have 53 ISO weeks, got %d", got)
	}
	if got := weeksInISOYear(2027); got != 52 {
		t.Fatalf("2027 should have 52 ISO weeks, got %d", got)
	}

	// 2027 has no week 53: asking for it must land on week 52's Monday.
	from, _, ok := Window{AnchorWeek, 53, 53}.Resolve(SeasonAnchors{Year: 2027})
	if !ok {
		t.Fatal("week 53 in a 52-week year must still resolve, clamped")
	}
	want, _, _ := Window{AnchorWeek, 52, 52}.Resolve(SeasonAnchors{Year: 2027})
	if !from.Equal(want) {
		t.Errorf("week 53 clamped to %s, want week 52's Monday %s", from, want)
	}

	// 2026 does have week 53, so nothing is clamped there.
	got53, _, _ := Window{AnchorWeek, 53, 53}.Resolve(SeasonAnchors{Year: 2026})
	got52, _, _ := Window{AnchorWeek, 52, 52}.Resolve(SeasonAnchors{Year: 2026})
	if got53.Equal(got52) {
		t.Errorf("2026 has a week 53; it must not clamp to week 52 (%s)", got53)
	}
}

// A missing anchor is not an error and not a guess — the window simply does not
// resolve, and the caller leaves the date unset.
func TestWindowResolveMissingAnchorLeavesDateUnset(t *testing.T) {
	noFrost := SeasonAnchors{Year: 2027} // frost dates not set on this season

	for _, anchor := range []Anchor{AnchorLastFrost, AnchorFirstFrost} {
		if _, _, ok := (Window{anchor, -14, 0}).Resolve(noFrost); ok {
			t.Errorf("%s window resolved without a frost date", anchor)
		}
	}
	// The consumer's contract: ok=false ⇒ nothing is written.
	p := Planting{}
	if from, _, ok := (Window{AnchorLastFrost, -14, 0}).Resolve(noFrost); ok {
		p.TransplantOn = sp(from.String())
	}
	if p.TransplantOn != nil {
		t.Error("an unresolvable window must leave the planned date unset, not guess one")
	}
}

func TestWindowValidate(t *testing.T) {
	cases := []struct {
		name    string
		win     Window
		wantErr bool
	}{
		{"good week window", Window{AnchorWeek, 10, 13}, false},
		{"good frost window", Window{AnchorLastFrost, -56, -42}, false},
		{"inverted range", Window{AnchorWeek, 13, 10}, true},
		{"week zero", Window{AnchorWeek, 0, 5}, true},
		{"week 54", Window{AnchorWeek, 50, 54}, true},
		{"absurd offset", Window{AnchorLastFrost, -400, 0}, true},
		{"unknown anchor", Window{"moon_phase", 1, 2}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.win.Validate("termín výsevu")
			if (err != nil) != tc.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestISOWeekOfRoundTrips(t *testing.T) {
	monday := isoWeekMonday(2027, 12)
	y, w := isoWeekOf(monday)
	if y != 2027 || w != 12 {
		t.Errorf("isoWeekOf(%s) = %d-W%d, want 2027-W12", monday, y, w)
	}
	sunday := isoWeekSunday(2027, 12)
	if monday.DaysUntil(sunday) != 6 {
		t.Errorf("week %s…%s is not seven days", monday, sunday)
	}
}
