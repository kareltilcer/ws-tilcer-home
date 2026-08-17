package finance

import (
	"context"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/registry"
)

// Widget states (FR-F7, D88).
const (
	stateRecorded = "recorded"
	stateMissing  = "missing"
)

// RozpocetWidget is the finance.rozpocet payload. It carries ONE of two states
// and the frontend renders a different body for each:
//
//   - "recorded" — the current month's headline numbers;
//   - "missing"  — a prompt naming the month, linking to the add form.
//
// The missing state is the reason the widget exists. The app's actual failure
// mode is a month nobody entered, and this widget is the only thing on the
// household's landing page that will notice — so it is a prompt, not an empty
// state to be styled quietly.
type RozpocetWidget struct {
	State string `json:"state"`
	Month string `json:"month"` // YYYY-MM, always present

	// Present only in the "recorded" state.
	TotalIncome  *int `json:"total_income,omitempty"`
	PersonalKaja *int `json:"personal_kaja,omitempty"`
	PersonalAndy *int `json:"personal_andy,omitempty"`
	Needs        *int `json:"needs,omitempty"`
	Savings      *int `json:"savings,omitempty"` // fun + no-fun, pooled
}

// rozpocetProvider implements the widget contract. It reaches month data only
// through the store it was handed — the dashboard host never touches these
// tables (D28).
//
// The key is finance.rozpocet, NOT finance.tento-mesic: `events` already
// publishes "Tento měsíc", and two identically-titled catalog rows would be a
// usability bug.
type rozpocetProvider struct {
	store *Store
	loc   *time.Location
}

func newRozpocetProvider(store *Store, loc *time.Location) *rozpocetProvider {
	return &rozpocetProvider{store: store, loc: loc}
}

func (p *rozpocetProvider) Key() string    { return "finance.rozpocet" }
func (p *rozpocetProvider) Title() string  { return "Rozpočet měsíce" }
func (p *rozpocetProvider) Module() string { return "finance" }
func (p *rozpocetProvider) Description() string {
	return "Rozdělení příjmů aktuálního měsíce"
}
func (p *rozpocetProvider) DefaultSize() string { return "narrow" }

// AdminOnly is false: Finance is an ordinary all-roles module (D84), so every
// member — `reader` included — may add this widget.
func (p *rozpocetProvider) AdminOnly() bool { return false }

func (p *rozpocetProvider) Data(ctx context.Context, _ registry.User) (any, error) {
	// The payload is household-wide: a budget has no per-recipient value, so the
	// caller's identity does not scope it.
	ym := currentMonth(time.Now(), p.loc)
	m, ok, err := p.store.ForMonth(ctx, ym)
	if err != nil {
		return nil, err
	}
	if !ok {
		return RozpocetWidget{State: stateMissing, Month: ym}, nil
	}
	savings := m.Split.Savings()
	return RozpocetWidget{
		State:        stateRecorded,
		Month:        m.Month,
		TotalIncome:  &m.Split.TotalIncome,
		PersonalKaja: &m.Split.PersonalKaja,
		PersonalAndy: &m.Split.PersonalAndy,
		Needs:        &m.Split.Needs,
		Savings:      &savings,
	}, nil
}
