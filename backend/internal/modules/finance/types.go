// Package finance is the Finance feature module (v6, PRD §V6): one row per
// calendar month holding two incomes and four rates, and a nine-value split
// DERIVED ON READ by a locked formula (split.go).
//
// It is a functional clone of the retiring standalone `fin` service. Two things
// crossed over verbatim — the split formula (D82) and the column vocabulary
// (D83, income_kaja/income_andy/rate_*) — everything else is home's: Czech UI,
// home's session and roles, the audit spine (which `fin` never had), snake_case
// + PATCH, live sync.
//
// Self-contained per HANDOFF §3: it imports only platform/ and registry, and the
// dashboard host, the metric/list catalogs and the log browser discover it purely
// through the contracts it registers.
package finance

// Rates are percentages OF TOTAL INCOME: whole integers, each >= 0, summing to
// exactly 100. Never a partial structure — all four always travel together,
// because a single rate is meaningless on its own.
//
// The field names are `fin`'s, deliberately (D83). Only the JSON is home's:
// snake_case, so `noFun` goes on the wire as `no_fun` (D92).
type Rates struct {
	Personal    int `json:"personal"`    // wants — the share each person keeps on their own account
	Operational int `json:"operational"` // needs — what stays in the joint "Kandy" account
	Fun         int `json:"fun"`         // fun savings (pooled, not per person)
	NoFun       int `json:"no_fun"`      // no-fun savings (pooled, not per person)
}

// Split is the derived breakdown — computed on read by Compute, NEVER stored
// (D82). Storing it would create a second source of truth for a formula that is
// deliberately locked. All values are whole CZK.
type Split struct {
	TotalIncome         int `json:"total_income"`
	PersonalKaja        int `json:"personal_kaja"`
	PersonalAndy        int `json:"personal_andy"`
	ToOperationalKaja   int `json:"to_operational_kaja"`
	ToOperationalAndy   int `json:"to_operational_andy"`
	OperationalReceived int `json:"operational_received"`
	FunSavings          int `json:"fun_savings"`
	NoFunSavings        int `json:"no_fun_savings"`
	// Needs is the remainder and absorbs all rounding; it can be negative by at
	// most 2 CZK and is deliberately not clamped (see Compute).
	Needs int `json:"needs"`
}

// Month is one recorded calendar month: the stored inputs plus the split
// composed onto them on the way out.
type Month struct {
	ID         string  `json:"id"`
	Month      string  `json:"month"` // YYYY-MM, unique
	IncomeKaja int     `json:"income_kaja"`
	IncomeAndy int     `json:"income_andy"`
	Rates      Rates   `json:"rates"`
	Split      Split   `json:"split"`
	CreatedBy  *string `json:"created_by"` // NULL for rows seeded from `fin`, which recorded no actor
	CreatedAt  string  `json:"created_at"`
	UpdatedAt  string  `json:"updated_at"`
}

// RatesInput is the wire form of the four rates. Every field is a POINTER so a
// PARTIAL block — one rate sent on its own — is detectable and rejected with a
// 422 (FR-F4), rather than silently zeroing the other three. The same type
// serves create (where all four are required) and update (where the whole block
// is optional but all-or-nothing), so one validator covers both.
type RatesInput struct {
	Personal    *int `json:"personal"`
	Operational *int `json:"operational"`
	Fun         *int `json:"fun"`
	NoFun       *int `json:"no_fun"`
}

// MonthCreate is POST /api/finance/months. Incomes are pointers because 0 is a
// legitimate income (one person can have no income in a month) and must not be
// confused with an omitted field.
type MonthCreate struct {
	Month      string      `json:"month"`
	IncomeKaja *int        `json:"income_kaja"`
	IncomeAndy *int        `json:"income_andy"`
	Rates      *RatesInput `json:"rates"`
}

// MonthUpdate is PATCH /api/finance/months/{id}. Omitted fields are left alone;
// `rates`, if present, must be the complete four-value block summing to 100.
type MonthUpdate struct {
	Month      *string     `json:"month"`
	IncomeKaja *int        `json:"income_kaja"`
	IncomeAndy *int        `json:"income_andy"`
	Rates      *RatesInput `json:"rates"`
}

// MonthPage is the paged list response. The dataset is a few dozen rows — paging
// exists for uniformity with the rest of the API, not out of need.
type MonthPage struct {
	Items      []Month `json:"items"`
	NextCursor *string `json:"next_cursor"`
}

// withSplit composes the derived block onto a row on its way out. Every read
// path goes through here, so there is exactly one place the split is attached.
func withSplit(m Month) Month {
	m.Split = Compute(m.IncomeKaja, m.IncomeAndy, m.Rates)
	return m
}
