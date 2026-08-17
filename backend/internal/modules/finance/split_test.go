package finance

import (
	"encoding/json"
	"math/rand"
	"os"
	"testing"
)

// The locked formula's test suite (PRD §V6-11, HANDOFF-8 §12). The worked
// example pins the numbers; the invariants pin the SHAPE of the arithmetic, and
// they are what actually catches a mis-ordered rounding — a formula can produce
// the right answer for 60000/40000 at 20/60/10/10 and still be wrong everywhere
// else.

type fixture struct {
	name       string
	kaja, andy int
	rates      Rates
}

// fixtures covers the eleven cases `fin`'s TestInvariants ran: ordinary months,
// a single earner, zero income, extreme rate distributions, odd money that lands
// rounding on a .5 boundary, and both forms of the negative-needs edge case.
var fixtures = []fixture{
	{"worked example", 60000, 40000, Rates{20, 60, 10, 10}},
	{"live rates", 67418, 52299, Rates{20, 60, 10, 10}},
	{"single earner", 83000, 0, Rates{20, 60, 10, 10}},
	{"no income at all", 0, 0, Rates{25, 25, 25, 25}},
	{"everything personal", 51234, 48766, Rates{100, 0, 0, 0}},
	{"nothing personal", 51234, 48766, Rates{0, 100, 0, 0}},
	{"all to savings", 40000, 35000, Rates{0, 0, 50, 50}},
	{"odd money, halves", 12345, 6789, Rates{50, 20, 20, 10}},
	{"odd money, thirds-ish", 33333, 66667, Rates{33, 33, 17, 17}},
	{"negative needs, operational 0", 6887385, 2030905, Rates{90, 0, 5, 5}},
	{"negative needs, tiny total", 0, 50, Rates{1, 1, 1, 97}},
}

// TestWorkedExample is the number-for-number example from PRD §V6-4 FR-F3.
func TestWorkedExample(t *testing.T) {
	got := Compute(60000, 40000, Rates{Personal: 20, Operational: 60, Fun: 10, NoFun: 10})
	want := Split{
		TotalIncome:         100000,
		PersonalKaja:        12000,
		PersonalAndy:        8000,
		ToOperationalKaja:   48000,
		ToOperationalAndy:   32000,
		OperationalReceived: 80000,
		FunSavings:          10000,
		NoFunSavings:        10000,
		Needs:               60000,
	}
	if got != want {
		t.Fatalf("split mismatch\n got: %+v\nwant: %+v", got, want)
	}
}

// TestInvariants asserts the six conditions `fin` asserted, over every fixture.
// The four the PRD names, plus operational_received == the sum of the transfers
// and total_income == the sum of the incomes.
func TestInvariants(t *testing.T) {
	for _, f := range fixtures {
		t.Run(f.name, func(t *testing.T) {
			assertInvariants(t, f.kaja, f.andy, f.rates)
		})
	}
}

func assertInvariants(t *testing.T, kaja, andy int, r Rates) {
	t.Helper()
	s := Compute(kaja, andy, r)
	if s.TotalIncome != kaja+andy {
		t.Errorf("total_income %d != %d + %d", s.TotalIncome, kaja, andy)
	}
	if s.PersonalKaja+s.ToOperationalKaja != kaja {
		t.Errorf("Kája: personal %d + to_operational %d != income %d", s.PersonalKaja, s.ToOperationalKaja, kaja)
	}
	if s.PersonalAndy+s.ToOperationalAndy != andy {
		t.Errorf("Andy: personal %d + to_operational %d != income %d", s.PersonalAndy, s.ToOperationalAndy, andy)
	}
	if s.OperationalReceived != s.ToOperationalKaja+s.ToOperationalAndy {
		t.Errorf("operational_received %d != %d + %d", s.OperationalReceived, s.ToOperationalKaja, s.ToOperationalAndy)
	}
	if s.FunSavings+s.NoFunSavings+s.Needs != s.OperationalReceived {
		t.Errorf("fun %d + no_fun %d + needs %d != operational_received %d",
			s.FunSavings, s.NoFunSavings, s.Needs, s.OperationalReceived)
	}
	if s.PersonalKaja+s.PersonalAndy+s.OperationalReceived != s.TotalIncome {
		t.Errorf("personal %d + %d + operational_received %d != total %d",
			s.PersonalKaja, s.PersonalAndy, s.OperationalReceived, s.TotalIncome)
	}
}

// TestRatesSum guards the input contract the service enforces and the table
// CHECK backs up: valid rates sum to exactly 100.
func TestRatesSum(t *testing.T) {
	for _, f := range fixtures {
		if got := f.rates.Sum(); got != 100 {
			t.Errorf("%s: rates sum to %d, not 100", f.name, got)
		}
	}
}

// TestInvariantsProperty runs the same six assertions over randomised incomes
// and rate quadruples. The worked example alone would not catch a mis-ordered
// rounding; the invariants are what "the totals reconcile" actually means, and
// they must hold for every input, not for one.
func TestInvariantsProperty(t *testing.T) {
	rng := rand.New(rand.NewSource(20260817))
	for i := 0; i < 20000; i++ {
		kaja := rng.Intn(10_000_001)
		andy := rng.Intn(10_000_001)
		assertInvariants(t, kaja, andy, randomRates(rng))
		if t.Failed() {
			t.Fatalf("failed at iteration %d (kaja=%d andy=%d)", i, kaja, andy)
		}
	}
}

// randomRates returns four non-negative integers summing to exactly 100, biased
// towards neither uniform splits nor extremes: it partitions 100 at three random
// cut points, so zero-valued buckets occur naturally.
func randomRates(rng *rand.Rand) Rates {
	cuts := []int{rng.Intn(101), rng.Intn(101), rng.Intn(101)}
	for i := range cuts {
		for j := i + 1; j < len(cuts); j++ {
			if cuts[j] < cuts[i] {
				cuts[i], cuts[j] = cuts[j], cuts[i]
			}
		}
	}
	return Rates{
		Personal:    cuts[0],
		Operational: cuts[1] - cuts[0],
		Fun:         cuts[2] - cuts[1],
		NoFun:       100 - cuts[2],
	}
}

// TestNeedsCanGoNegativeAndIsNotClamped covers the inherited edge case in BOTH
// its forms (PRD §V6-4, HANDOFF-8 §2). needs >= -2 is a provable bound, not an
// observation: needs >= T×o/100 − 2, so needs < 0 requires T × o < 200 — which
// happens at operational = 0 for any income, and at operational >= 1 only below
// 200 Kč of total income.
//
// The point of the test is that NOTHING CLAMPS IT. Clamping would break
// fun + no_fun + needs == operational_received, which assertInvariants checks
// here alongside the sign.
func TestNeedsCanGoNegativeAndIsNotClamped(t *testing.T) {
	cases := []struct {
		name       string
		kaja, andy int
		rates      Rates
		wantNeeds  int
	}{
		// operational = 0, at a realistic household income — the reachable form.
		{"operational zero", 6887385, 2030905, Rates{Personal: 90, Operational: 0, Fun: 5, NoFun: 5}, -2},
		// operational >= 1, only reachable with toy inputs (T × o < 200).
		{"tiny total income", 0, 50, Rates{Personal: 1, Operational: 1, Fun: 1, NoFun: 97}, -1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := Compute(c.kaja, c.andy, c.rates)
			if s.Needs != c.wantNeeds {
				t.Errorf("needs = %d, want %d (clamping or a reordered rounding would show up here)", s.Needs, c.wantNeeds)
			}
			if s.Needs < -2 {
				t.Errorf("needs = %d, below the provable −2 bound", s.Needs)
			}
			assertInvariants(t, c.kaja, c.andy, c.rates)
		})
	}
}

// finExportRow is one row of `fin`'s live GET /months, in fin's camelCase wire
// format (PRD §V6-12 step 1). testdata/fin-months-export.json is the unedited
// export taken 2026-08-17 and is the migration's provenance.
type finExportRow struct {
	ID         string `json:"id"`
	Month      string `json:"month"`
	IncomeKaja int    `json:"incomeKaja"`
	IncomeAndy int    `json:"incomeAndy"`
	Rates      struct {
		Personal    int `json:"personal"`
		Operational int `json:"operational"`
		Fun         int `json:"fun"`
		NoFun       int `json:"noFun"`
	} `json:"rates"`
	Split struct {
		TotalIncome         int `json:"totalIncome"`
		PersonalKaja        int `json:"personalKaja"`
		PersonalAndy        int `json:"personalAndy"`
		ToOperationalKaja   int `json:"toOperationalKaja"`
		ToOperationalAndy   int `json:"toOperationalAndy"`
		OperationalReceived int `json:"operationalReceived"`
		FunSavings          int `json:"funSavings"`
		NoFunSavings        int `json:"noFunSavings"`
		Needs               int `json:"needs"`
	} `json:"split"`
}

// TestComputeMatchesFinLiveExport is the D97 comparison run at BUILD time: every
// one of the fifteen migrated months carries the split the live `fin` service
// returned, and this port must reproduce all nine values to the koruna.
//
// The retirement gate (v6-seed/verify_migration.py) makes the same comparison
// against the two running services. This test is the cheap version of it: if the
// Go port ever disagrees with these rows, the port is wrong — the rows are what
// the household has been looking at for over a year.
func TestComputeMatchesFinLiveExport(t *testing.T) {
	raw, err := os.ReadFile("testdata/fin-months-export.json")
	if err != nil {
		t.Fatalf("read fin export: %v", err)
	}
	var rows []finExportRow
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatalf("decode fin export: %v", err)
	}
	if len(rows) != 15 {
		t.Fatalf("fin export holds %d rows, expected the 15 taken on 2026-08-17", len(rows))
	}
	for _, r := range rows {
		got := Compute(r.IncomeKaja, r.IncomeAndy, Rates{
			Personal:    r.Rates.Personal,
			Operational: r.Rates.Operational,
			Fun:         r.Rates.Fun,
			NoFun:       r.Rates.NoFun,
		})
		want := Split{
			TotalIncome:         r.Split.TotalIncome,
			PersonalKaja:        r.Split.PersonalKaja,
			PersonalAndy:        r.Split.PersonalAndy,
			ToOperationalKaja:   r.Split.ToOperationalKaja,
			ToOperationalAndy:   r.Split.ToOperationalAndy,
			OperationalReceived: r.Split.OperationalReceived,
			FunSavings:          r.Split.FunSavings,
			NoFunSavings:        r.Split.NoFunSavings,
			Needs:               r.Split.Needs,
		}
		if got != want {
			t.Errorf("%s: split differs from live fin\n got: %+v\nwant: %+v", r.Month, got, want)
		}
	}
}
