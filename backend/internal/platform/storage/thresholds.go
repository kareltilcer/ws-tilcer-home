package storage

import (
	"context"
	"database/sql"
	"fmt"
)

// `storage_thresholds` — the two chat limits, read here rather than in either
// module that uses them (PRD D236, migration 02004).
//
// ⚠ THE TABLE BELONGS TO NEITHER MODULE THAT TOUCHES IT. `admin` WRITES these
// values and `chat` READS them, and internal/arch forbids the two from importing
// each other — the same constraint that put `notification_preferences` in the
// platform block. This package is the seam they already share, so the accessor
// lives here and there is exactly one spelling of the key strings.
//
// ⚠ WARN ONLY, EVERYWHERE (D237). Nothing in Home is blocked by a threshold: no
// upload is refused, there is no quota and there is no new 413. A value below
// current usage is a legitimate thing to save — the UI says what it just switched
// on rather than refusing it — so the only invariant is `value_mb > 0`, and the
// table's own CHECK holds that.
//
// ⚠ v9's HOME_STORAGE_WARN_TOTAL_MB STAYS AN ENVIRONMENT VARIABLE and stays out of
// this table. Home now has two threshold mechanisms; the inconsistency is recorded
// rather than hidden, because migrating a live operator setting is a change with
// its own failure mode and it is v11's.

// Threshold keys. Keyed rather than columned, so a later threshold is an INSERT
// and not a migration.
const (
	ThresholdChatTotal        = "chat.total"
	ThresholdChatConversation = "chat.conversation"
)

// Threshold defaults, matching 02004's seed. They exist so a caller reading a
// table that somehow has no row still renders a number rather than a 0 MB limit
// every conversation in the house instantly exceeds.
const (
	DefaultChatTotalMB        = 512
	DefaultChatConversationMB = 128
)

// MaxThresholdMB is the upper bound — 1 PB, which is four orders of magnitude past
// anything a household bucket will hold and still nowhere near where the arithmetic
// breaks.
//
// ⚠ IT EXISTS BECAUSE MB() SHIFTS BY 20, AND AN UNBOUNDED VALUE OVERFLOWS INTO A
// WARNING THAT CANNOT BE TURNED OFF. `value_mb > 0` is the only thing SQLite can
// hold, so a fat-fingered 9007199254740992 saved cleanly, answered 200, and then
// became a limit of ZERO — after which every comparison read `total > 0` as
// exceeded and the banner fired on every screen for a household holding 43 bytes,
// beside a figure claiming the limit was 8 589 934 592 TB. Recoverable by saving a
// sane value, and nothing on the way in said the number had been refused, because
// it had not been.
const MaxThresholdMB = 1 << 30

// Threshold is one row.
type Threshold struct {
	Key       string
	ValueMB   int
	UpdatedAt string
	// UpdatedBy is nil for a seeded default. ⚠ The distinction is rendered: the
	// Administrace screen tells a value somebody chose from one nobody has touched,
	// and a fake actor in the seed would make every fresh install look edited.
	UpdatedBy *string
}

// ThresholdQuerier is satisfied by *sql.DB and *sql.Tx alike.
//
// ⚠ It is not a convenience. The pool is capped at a single connection because
// SQLite is single-writer (platform/db), so a read issued on *sql.DB while a
// transaction is open waits for the connection that transaction holds — a
// guaranteed deadlock. A write path reading a threshold inside its own tx has to
// be able to pass the tx.
type ThresholdQuerier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// Thresholds is the pair the whole feature reads, already defaulted.
type Thresholds struct {
	Total        Threshold
	Conversation Threshold
}

// LoadThresholds reads both values.
//
// A missing row falls back to its default rather than erroring: the figures are a
// WARNING register, and a page that 500s because somebody deleted a settings row
// is worse than one that warns at the documented default.
func LoadThresholds(ctx context.Context, q ThresholdQuerier) (Thresholds, error) {
	out := Thresholds{
		Total:        Threshold{Key: ThresholdChatTotal, ValueMB: DefaultChatTotalMB},
		Conversation: Threshold{Key: ThresholdChatConversation, ValueMB: DefaultChatConversationMB},
	}
	rows, err := q.QueryContext(ctx,
		`SELECT key, value_mb, updated_at, updated_by FROM storage_thresholds`)
	if err != nil {
		return out, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			t         Threshold
			updatedBy sql.NullString
		)
		if err := rows.Scan(&t.Key, &t.ValueMB, &t.UpdatedAt, &updatedBy); err != nil {
			return out, err
		}
		if updatedBy.Valid {
			v := updatedBy.String
			t.UpdatedBy = &v
		}
		switch t.Key {
		case ThresholdChatTotal:
			out.Total = t
		case ThresholdChatConversation:
			out.Conversation = t
		}
	}
	return out, rows.Err()
}

// SetThreshold writes one value.
//
// An UPSERT rather than an UPDATE, so a row deleted by hand comes back on the next
// save instead of silently failing to take.
func SetThreshold(ctx context.Context, q ThresholdQuerier, key string, valueMB int, actor, now string) error {
	switch key {
	case ThresholdChatTotal, ThresholdChatConversation:
	default:
		return fmt.Errorf("storage: unknown threshold %q", key)
	}
	if valueMB < 1 || valueMB > MaxThresholdMB {
		return fmt.Errorf("storage: threshold %q must be between 1 and %d MB", key, MaxThresholdMB)
	}
	_, err := q.ExecContext(ctx, `
		INSERT INTO storage_thresholds (key, value_mb, updated_at, updated_by)
		VALUES (?, ?, ?, ?)
		    ON CONFLICT (key) DO UPDATE SET
		        value_mb   = excluded.value_mb,
		        updated_at = excluded.updated_at,
		        updated_by = excluded.updated_by`,
		key, valueMB, now, nullableActor(actor))
	return err
}

// MB converts a threshold to bytes for comparison against a measured figure.
//
// ⚠ ONE CONVERSION, IN ONE PLACE. The threshold is stored, edited and displayed in
// MB while every measurement in Home is in bytes, and the comparison happens on
// three screens — Administrace, the chat module's banner and the clean-up page.
// Three hand-written `<<20`s is three chances for one of them to be `*1000`.
func MB(valueMB int) int64 { return int64(valueMB) << 20 }

func nullableActor(actor string) any {
	if actor == "" {
		return nil
	}
	return actor
}
