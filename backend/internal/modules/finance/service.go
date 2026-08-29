package finance

import (
	"context"
	"database/sql"
	"strconv"
	"strings"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/db"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/idgen"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
)

// Notifier publishes a websocket change after commit. The ctx carries the
// request's client id (reqctx), which the hub stamps as the push's origin.
type Notifier func(ctx context.Context, typ string, payload any)

// Service orchestrates finance mutations: validate → WithTx (row change + audit
// event in ONE transaction) → notify after commit. The same shape every other
// module uses.
//
// Finance joins the audit spine, which `fin` never had (D86). That is not
// decoration here: delete is permanent (D87), so the month.delete event's
// full-row diff is the only surviving record of what a deleted month held.
type Service struct {
	db     *sql.DB
	store  *Store
	sink   audit.Sink
	notify Notifier
}

func NewService(db *sql.DB, sink audit.Sink, notify Notifier) *Service {
	if notify == nil {
		notify = func(context.Context, string, any) {}
	}
	return &Service{db: db, store: NewStore(db), sink: sink, notify: notify}
}

// Store exposes the read store to this module's widget, metric and list
// providers.
func (s *Service) Store() *Store { return s.store }

func itoa(v int) string { return strconv.Itoa(v) }

// monthTaken maps a UNIQUE constraint failure onto the SAME 409 the in-tx
// MonthTaken check produces. `month` is the table's only unique column, so a
// violation can mean nothing else.
//
// MonthTaken catches the ordinary duplicate and gives the better message; this
// catches the one two concurrent writers can still slip past it — both read
// "free", the first commits, the second's write hits
// ux_finance_months_month. Returned raw, that error has no *httpx.APIError to
// match and would surface as a 500, which is neither what openapi.yaml
// documents nor what the frontend can turn into "Tento měsíc už je zadaný."
func monthTaken(err error) error {
	if err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed") {
		return httpx.ErrConflict("Tento měsíc už je zadaný.")
	}
	return err
}

// record writes one audit event through the spine, inside the caller's tx. The
// error is returned unchanged so the transaction rolls back: an action that
// succeeds unlogged is the bug the spine exists to prevent.
func (s *Service) record(ctx context.Context, tx *sql.Tx, action, entityID, summary string, changes []audit.Change) error {
	_, err := s.sink.Record(ctx, tx, audit.Event{
		Module:     audit.ModuleFinance,
		Action:     action,
		EntityType: "finance_month",
		EntityID:   entityID,
		Summary:    summary,
		Changes:    changes,
	})
	return err
}

// ---- Validation (PRD FR-F2 / FR-F4; every failure is a 422 with one Czech message) ----

func validateMonthKey(ym string) error {
	if !monthPattern.MatchString(ym) {
		return httpx.ErrUnprocessable("Měsíc musí být ve tvaru RRRR-MM.")
	}
	return nil
}

func validateIncome(v int) error {
	if v < 0 {
		return httpx.ErrUnprocessable("Příjem nemůže být záporný.")
	}
	return nil
}

// resolveRates turns the all-or-nothing wire block into Rates. A PARTIAL block —
// one rate sent without the others — is a 422 rather than a silent zeroing of
// the rest: a single rate is meaningless on its own (FR-F4).
func resolveRates(in *RatesInput) (Rates, error) {
	if in == nil {
		return Rates{}, httpx.ErrUnprocessable("Sazby jsou povinné.")
	}
	if in.Personal == nil || in.Operational == nil || in.Fun == nil || in.NoFun == nil {
		return Rates{}, httpx.ErrUnprocessable("Sazby je nutné poslat všechny čtyři najednou.")
	}
	r := Rates{Personal: *in.Personal, Operational: *in.Operational, Fun: *in.Fun, NoFun: *in.NoFun}
	if r.Personal < 0 || r.Operational < 0 || r.Fun < 0 || r.NoFun < 0 {
		return Rates{}, httpx.ErrUnprocessable("Sazba nemůže být záporná.")
	}
	if r.Sum() != 100 {
		return Rates{}, httpx.ErrUnprocessable("Sazby musí dát dohromady 100 %.")
	}
	return r, nil
}

// ---- Reads ----

// List returns months newest first (FR-F1). Reads are open to every member,
// `reader` included (D84) — the route carries no write gate.
func (s *Service) List(ctx context.Context, limit int, cursor string) (MonthPage, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	// The cursor is a MONTH key, not a UUIDv7 (openapi.yaml). Unchecked it is
	// compared lexically against `month`, so anything sorting above a digit — a
	// UUIDv7 copied from the other paged collections, a corrupted value — would
	// silently return the newest page again instead of the documented 422.
	if cursor != "" && !monthPattern.MatchString(cursor) {
		return MonthPage{}, httpx.ErrUnprocessable("Kurzor musí být měsíc ve tvaru RRRR-MM.")
	}
	items, next, err := s.store.List(ctx, limit, cursor)
	if err != nil {
		return MonthPage{}, err
	}
	if items == nil {
		items = []Month{}
	}
	return MonthPage{Items: items, NextCursor: next}, nil
}

func (s *Service) Get(ctx context.Context, id string) (*Month, error) {
	m, ok, err := s.store.Get(ctx, nil, id)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, httpx.ErrNotFound("month not found")
	}
	return &m, nil
}

// ---- Mutations ----

// Create records a month (FR-F2). It persists the INPUTS only and returns the
// split computed on the way out.
func (s *Service) Create(ctx context.Context, in MonthCreate) (*Month, error) {
	if err := validateMonthKey(in.Month); err != nil {
		return nil, err
	}
	if in.IncomeKaja == nil || in.IncomeAndy == nil {
		return nil, httpx.ErrUnprocessable("Oba příjmy jsou povinné.")
	}
	if err := validateIncome(*in.IncomeKaja); err != nil {
		return nil, err
	}
	if err := validateIncome(*in.IncomeAndy); err != nil {
		return nil, err
	}
	rates, err := resolveRates(in.Rates)
	if err != nil {
		return nil, err
	}

	m := Month{
		ID:         idgen.New(),
		Month:      in.Month,
		IncomeKaja: *in.IncomeKaja,
		IncomeAndy: *in.IncomeAndy,
		Rates:      rates,
	}

	var out Month
	err = appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		// Checked inside the transaction so the message is useful; the unique
		// index is still the authority if two writes race.
		taken, err := s.store.MonthTaken(ctx, tx, m.Month, "")
		if err != nil {
			return err
		}
		if taken {
			return httpx.ErrConflict("Tento měsíc už je zadaný.")
		}
		out, err = s.store.Insert(ctx, tx, m, reqctx.ActorID(ctx))
		if err != nil {
			return monthTaken(err)
		}
		return s.record(ctx, tx, "month.create", out.ID,
			"Přidán měsíc "+monthLabel(out.Month), rowDiff(nil, &out))
	})
	if err != nil {
		return nil, err
	}
	s.notify(ctx, "finance.changed", out)
	return &out, nil
}

// Update corrects a month (FR-F4). Omitted fields are left alone; `rates`, if
// present, must be the complete four-value block. The audit event carries a
// field-level diff, so "who changed May's rates and to what" is answerable —
// something `fin` never could do.
func (s *Service) Update(ctx context.Context, id string, in MonthUpdate) (*Month, error) {
	if in.Month != nil {
		if err := validateMonthKey(*in.Month); err != nil {
			return nil, err
		}
	}
	if in.IncomeKaja != nil {
		if err := validateIncome(*in.IncomeKaja); err != nil {
			return nil, err
		}
	}
	if in.IncomeAndy != nil {
		if err := validateIncome(*in.IncomeAndy); err != nil {
			return nil, err
		}
	}
	var rates *Rates
	if in.Rates != nil {
		r, err := resolveRates(in.Rates)
		if err != nil {
			return nil, err
		}
		rates = &r
	}

	var out Month
	var changed bool
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		before, ok, err := s.store.Get(ctx, tx, id)
		if err != nil {
			return err
		}
		if !ok {
			return httpx.ErrNotFound("month not found")
		}

		after := before
		if in.Month != nil {
			after.Month = *in.Month
		}
		if in.IncomeKaja != nil {
			after.IncomeKaja = *in.IncomeKaja
		}
		if in.IncomeAndy != nil {
			after.IncomeAndy = *in.IncomeAndy
		}
		if rates != nil {
			after.Rates = *rates
		}

		if after.Month != before.Month {
			taken, err := s.store.MonthTaken(ctx, tx, after.Month, id)
			if err != nil {
				return err
			}
			if taken {
				return httpx.ErrConflict("Tento měsíc už je zadaný.")
			}
		}

		// A patch that changes nothing writes nothing — no updated_at bump, no
		// audit row, no broadcast. rowDiff covers exactly the seven input fields,
		// so an empty diff means the stored row already is what was asked for.
		// The same guard documents/notes apply, and it matters more here: the
		// form always PATCHes all four values, so re-saving an untouched month
		// would otherwise log an empty-diff month.update and push "Upraven měsíc
		// …" to the household through a v5 trigger rule.
		changes := rowDiff(&before, &after)
		if len(changes) == 0 {
			out = before
			return nil
		}

		out, err = s.store.Update(ctx, tx, after)
		if err != nil {
			return monthTaken(err)
		}
		changed = true
		return s.record(ctx, tx, "month.update", out.ID,
			"Upraven měsíc "+monthLabel(out.Month), changes)
	})
	if err != nil {
		return nil, err
	}
	if changed {
		s.notify(ctx, "finance.changed", out)
	}
	return &out, nil
}

// Delete removes a month PERMANENTLY (FR-F5, D87). No soft delete, no ?hard
// flag, no admin tier — an ordinary write, exactly as in `fin`.
//
// The row is read INSIDE the transaction before it is deleted so the audit event
// can carry a FULL-ROW diff. That is not decoration: with the row gone, this
// event is the only surviving record of what the month held, and it is what
// makes an unrecoverable delete acceptable in a module whose predecessor had no
// audit trail at all. A month.delete that recorded only the id would have thrown
// the data away.
func (s *Service) Delete(ctx context.Context, id string) error {
	var deleted Month
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		before, ok, err := s.store.Get(ctx, tx, id)
		if err != nil {
			return err
		}
		if !ok {
			return httpx.ErrNotFound("month not found")
		}
		if err := s.store.Delete(ctx, tx, id); err != nil {
			return err
		}
		deleted = before
		return s.record(ctx, tx, "month.delete", id,
			"Smazán měsíc "+monthLabel(before.Month), rowDiff(&before, nil))
	})
	if err != nil {
		return err
	}
	s.notify(ctx, "finance.changed", map[string]string{"id": id, "month": deleted.Month})
	return nil
}

// rowDiff builds the audit field diff over the seven INPUT fields — month, both
// incomes and the four rates. The derived split is never diffed: it is a
// function of these, so recording it would log the same fact twice and invite
// the two copies to disagree.
//
//	before == nil → a create: every field as a new value.
//	after  == nil → a delete: every field as an old value (the full-row diff).
//	both set      → an update: only the fields that actually changed.
func rowDiff(before, after *Month) []audit.Change {
	field := func(name string, oldV, newV *string) audit.Change {
		return audit.Change{Field: name, Old: oldV, New: newV}
	}
	vals := func(m *Month) map[string]string {
		if m == nil {
			return nil
		}
		return map[string]string{
			"month":            m.Month,
			"income_kaja":      itoa(m.IncomeKaja),
			"income_andy":      itoa(m.IncomeAndy),
			"rate_personal":    itoa(m.Rates.Personal),
			"rate_operational": itoa(m.Rates.Operational),
			"rate_fun":         itoa(m.Rates.Fun),
			"rate_nofun":       itoa(m.Rates.NoFun),
		}
	}
	// Fixed order so a diff reads the same way every time it is shown.
	order := []string{"month", "income_kaja", "income_andy",
		"rate_personal", "rate_operational", "rate_fun", "rate_nofun"}

	oldVals, newVals := vals(before), vals(after)
	changes := make([]audit.Change, 0, len(order))
	for _, name := range order {
		o, hasOld := oldVals[name]
		n, hasNew := newVals[name]
		switch {
		case hasOld && hasNew:
			if o != n {
				changes = append(changes, field(name, audit.Ptr(o), audit.Ptr(n)))
			}
		case hasNew:
			changes = append(changes, field(name, nil, audit.Ptr(n)))
		case hasOld:
			changes = append(changes, field(name, audit.Ptr(o), nil))
		}
	}
	return changes
}
