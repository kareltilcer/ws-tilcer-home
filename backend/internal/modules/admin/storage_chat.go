package admin

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/db"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/storage"
)

// Úložiště's chat block and the two thresholds (v10, FR-V10-16/17, D236/D240/D254).
//
// ⚠ THERE IS NO WAY IN FROM HERE, AND THE ABSENCE IS THE SPECIFICATION (D240). No
// thread, no message, no attachment list, no clean-up page, no link. An admin sees
// which room is heavy and asks its members, because the clean-up page is
// member-scoped (D241) and the only two chat verbs an admin has over a conversation
// they are not in are restore and purge — neither of which opens it (D255).
//
// Conversation names and rough membership therefore become admin-visible. That is
// the accepted disclosure in leak row 14: an actionable module-level warning needs a
// name to act on, and the alternative is an admin watching a number rise with no way
// to say whose it is.
//
// ⚠ `admin` STILL IMPORTS NO FEATURE MODULE. The rows arrive through the storage
// catalog's `GroupSource` (D235) exactly as the byte figures arrive through
// `BlobSource`, and the module name is asked of the registry rather than written
// here — the same discipline InventoryModules enforces on the purge screen.

// chatModule is the catalog key whose groups this block renders.
//
// ⚠ IT IS THE ONE HAND-WRITTEN MODULE NAME IN THIS FILE, and it is unavoidable: the
// block is chat-shaped — its thresholds are `chat.total` and `chat.conversation`,
// its copy says *Nezálohováno* because chat blobs are not mirrored. A second module
// implementing GroupSource gets its own block or none; it does not silently land in
// this one.
const chatModule = "chat"

// StorageChat is the chat block of the snapshot.
type StorageChat struct {
	TotalBytes              *int64  `json:"total_bytes"`
	ThresholdTotalMB        int     `json:"threshold_total_mb"`
	Exceeded                bool    `json:"exceeded"`
	ThresholdConversationMB int     `json:"threshold_conversation_mb"`
	ThresholdsUpdatedAt     *string `json:"thresholds_updated_at"`
	ThresholdsUpdatedBy     *string `json:"thresholds_updated_by"`
	// Mirrored is ALWAYS false, and deliberately so (D229). Chat blobs are not
	// copied to the backup bucket: they are the most disposable bytes in the
	// application and the module exists under a storage warning, so doubling them
	// into the mirror would be the one place in Home where a background job
	// undermines a threshold. The page renders *Nezálohováno* rather than leaving a
	// gap that reads as zero.
	Mirrored      bool                      `json:"mirrored"`
	Conversations []StorageChatConversation `json:"conversations"`
}

// StorageChatConversation is one room's row.
type StorageChatConversation struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Members is a COUNT. Never the names — an admin sees that a room is heavy, not
	// who is in it.
	Members   int    `json:"members"`
	Bytes     *int64 `json:"bytes"`
	Objects   *int64 `json:"objects"`
	OverLimit bool   `json:"over_limit"`
	// TrashedAt non-null ⇒ in the koš, and ⚠ STILL COUNTED in TotalBytes (D254). An
	// admin chasing an overrun needs to know that 200 MB is spoken for but not gone.
	TrashedAt  *string `json:"trashed_at"`
	PurgeAfter *string `json:"purge_after"`
}

// StorageThresholds is the pair, as the API renders them.
type StorageThresholds struct {
	ChatTotalMB        int     `json:"chat_total_mb"`
	ChatConversationMB int     `json:"chat_conversation_mb"`
	UpdatedAt          *string `json:"updated_at"`
	UpdatedBy          *string `json:"updated_by"`
}

// StorageThresholdsUpdate is the PUT body. Both fields are optional; at least one
// must be present.
type StorageThresholdsUpdate struct {
	ChatTotalMB        *int `json:"chat_total_mb"`
	ChatConversationMB *int `json:"chat_conversation_mb"`
}

// measureChat builds the block.
//
// ⚠ A FAILURE HERE LEAVES THE BLOCK NIL RATHER THAN FAILING THE SNAPSHOT, the same
// rule measureBlobs follows for a bucket outage: the database figures are still
// worth rendering, and a 5xx carrying partial results is a shape no client handles.
func (s *StorageService) measureChat(ctx context.Context) *StorageChat {
	if s.deps.Catalog == nil || s.deps.DB == nil {
		return nil
	}
	if !containsString(s.deps.Catalog.GroupModules(), chatModule) {
		return nil
	}
	th, err := storage.LoadThresholds(ctx, s.deps.DB)
	if err != nil {
		s.logf("admin: reading the chat thresholds failed", err)
		return nil
	}
	rooms, err := s.deps.Catalog.GroupsOf(ctx, chatModule)
	if err != nil {
		s.logf("admin: reading the chat storage groups failed", err)
		return nil
	}

	limit := storage.MB(th.Conversation.ValueMB)
	out := &StorageChat{
		ThresholdTotalMB:        th.Total.ValueMB,
		ThresholdConversationMB: th.Conversation.ValueMB,
		Mirrored:                false,
		Conversations:           make([]StorageChatConversation, 0, len(rooms)),
	}
	// ⚠ THE LATER OF THE TWO ROWS, NOT `chat.total`'S. They are separate rows with
	// separate stamps, and reading only the total's meant that changing only *Limit
	// na jednu konverzaci* left the page reporting "Zatím nikdo neměnil — platí
	// výchozí hodnoty" over a value somebody had just chosen. That sentence is the
	// exact distinction the null-for-a-seeded-default design exists to draw, so
	// getting it wrong is worse than showing nothing.
	latest := th.Total
	if th.Conversation.UpdatedAt > latest.UpdatedAt {
		latest = th.Conversation
	}
	out.ThresholdsUpdatedBy = latest.UpdatedBy
	if latest.UpdatedAt != "" {
		at := latest.UpdatedAt
		out.ThresholdsUpdatedAt = &at
	}
	var total int64
	for _, g := range rooms {
		bytes, objects := g.Bytes, g.Objects
		row := StorageChatConversation{
			ID: g.ID, Name: g.Name, Members: g.Members,
			Bytes: &bytes, Objects: &objects,
			OverLimit: bytes > limit,
		}
		if g.TrashedAt != "" {
			t := g.TrashedAt
			row.TrashedAt = &t
		}
		if g.PurgeAfter != "" {
			p := g.PurgeAfter
			row.PurgeAfter = &p
		}
		// ⚠ A TRASHED ROOM IS ADDED TO THE TOTAL LIKE ANY OTHER (D254). Its bytes are
		// still in R2, and reporting them as freed would make this page lie for a
		// week — the page's premise is that its figures sum.
		total += bytes
		out.Conversations = append(out.Conversations, row)
	}
	out.TotalBytes = &total
	out.Exceeded = total > storage.MB(th.Total.ValueMB)
	return out
}

// SetThresholds writes the two chat thresholds. Admin only, audited.
//
// ⚠ THE EVENT IS `chat.threshold.update`, WRITTEN WITH audit.ModuleChat FROM THE
// admin MODULE. The setting is an admin's to change and the subject is chat's, so
// the row belongs on chat's timeline — the same shape D255 uses when an admin
// restores a conversation they may not read. `chat.Module.AuditActions()` declares
// the verb and this writes it; the catalog is what makes them agree.
//
// ⚠ A VALUE BELOW CURRENT USAGE IS SAVED, NOT REFUSED (D244/D237). Nothing in v10 is
// ever blocked by a threshold — the whole register is warn-only — so lowering a limit
// below what is already stored is a legitimate thing to do, and the screen says what
// it just switched on rather than arguing about it.
func (s *Service) SetThresholds(ctx context.Context, in StorageThresholdsUpdate) (StorageThresholds, error) {
	if in.ChatTotalMB == nil && in.ChatConversationMB == nil {
		return StorageThresholds{}, httpx.ErrUnprocessable("Zadejte alespoň jeden limit.")
	}
	// The same bounds SetThreshold enforces, restated here so a refusal is a 422 with
	// a Czech sentence rather than a 500 carrying the storage layer's own error text.
	//
	// ⚠ THE UPPER BOUND IS NOT DECORATION. storage.MB shifts by 20, so an unbounded
	// value saved cleanly and then overflowed into a limit of ZERO — a warning that
	// fired on every screen and could not be turned off except by saving again.
	for _, v := range []*int{in.ChatTotalMB, in.ChatConversationMB} {
		if v != nil && (*v < 1 || *v > storage.MaxThresholdMB) {
			return StorageThresholds{}, httpx.ErrUnprocessable(
				fmt.Sprintf("Limit musí být mezi 1 a %d MB.", storage.MaxThresholdMB))
		}
	}
	actor := reqctx.ActorID(ctx)
	// ⚠ THE SEED'S LAYOUT, NOT admin's tsFormat. `storage_thresholds` is a PLATFORM
	// table written by this module and read by `chat`, and 02004 seeds it with a
	// fixed-millisecond RFC 3339 string — which is also what every chat timestamp
	// uses. Writing RFC3339Nano here would put two widths in one column and make a
	// seeded row and an edited one sort and render differently.
	now := s.now().UTC().Format("2006-01-02T15:04:05.000Z07:00")
	var out StorageThresholds
	err := appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		before, err := storage.LoadThresholds(ctx, tx)
		if err != nil {
			return err
		}
		var changes []audit.Change
		if in.ChatTotalMB != nil && *in.ChatTotalMB != before.Total.ValueMB {
			if err := storage.SetThreshold(ctx, tx, storage.ThresholdChatTotal, *in.ChatTotalMB, actor, now); err != nil {
				return err
			}
			changes = append(changes, audit.Change{
				Field: "chat_total_mb",
				Old:   audit.Ptr(fmt.Sprint(before.Total.ValueMB)),
				New:   audit.Ptr(fmt.Sprint(*in.ChatTotalMB)),
			})
		}
		if in.ChatConversationMB != nil && *in.ChatConversationMB != before.Conversation.ValueMB {
			if err := storage.SetThreshold(ctx, tx, storage.ThresholdChatConversation, *in.ChatConversationMB, actor, now); err != nil {
				return err
			}
			changes = append(changes, audit.Change{
				Field: "chat_conversation_mb",
				Old:   audit.Ptr(fmt.Sprint(before.Conversation.ValueMB)),
				New:   audit.Ptr(fmt.Sprint(*in.ChatConversationMB)),
			})
		}
		// ⚠ NO CHANGE WRITES NO EVENT. Autosave-on-blur fires on every focus loss,
		// so a Log row per blur would bury the changes that mattered under a dozen
		// that changed nothing — the same reason UpdateDocument's all-nil patch
		// leaves updated_at alone.
		if len(changes) > 0 {
			if _, err := s.sink.Record(ctx, tx, audit.Event{
				Module:     audit.ModuleChat,
				Action:     "threshold.update",
				EntityType: "storage_threshold",
				EntityID:   "chat",
				Summary:    thresholdSummary(changes),
				Changes:    changes,
			}); err != nil {
				return err
			}
		}
		after, err := storage.LoadThresholds(ctx, tx)
		if err != nil {
			return err
		}
		out = StorageThresholds{
			ChatTotalMB:        after.Total.ValueMB,
			ChatConversationMB: after.Conversation.ValueMB,
			UpdatedBy:          after.Total.UpdatedBy,
		}
		if after.Total.UpdatedAt != "" {
			at := after.Total.UpdatedAt
			out.UpdatedAt = &at
		}
		return nil
	})
	if err != nil {
		return StorageThresholds{}, err
	}
	// The snapshot carries the thresholds, so a saved value that the page re-reads
	// from a 60-second-old cache would look like the save had not taken.
	if s.storage != nil {
		s.storage.Invalidate()
	}
	return out, nil
}

// thresholdSummary renders the Czech phrase the Log shows.
func thresholdSummary(changes []audit.Change) string {
	if len(changes) == 1 {
		return fmt.Sprintf("Limit úložiště chatu změněn (%s: %s → %s MB)",
			thresholdLabel(changes[0].Field), derefStr(changes[0].Old), derefStr(changes[0].New))
	}
	return "Limity úložiště chatu změněny"
}

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func thresholdLabel(field string) string {
	if field == "chat_conversation_mb" {
		return "limit na jednu konverzaci"
	}
	return "limit pro chat celkem"
}

func containsString(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

// logf reports a half-measured block. It writes through slog's default rather than
// taking a logger dependency: StorageDeps carries the four things the snapshot
// MEASURES and nothing else, and a logger threaded in for two warn lines would be
// the first field on it that is not a measurement.
func (s *StorageService) logf(msg string, err error) {
	slog.Default().Warn(msg, "err", err)
}
