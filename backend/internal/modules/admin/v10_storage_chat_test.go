package admin_test

// v10 — the Úložiště chat block and the two thresholds (FR-V10-16/17, D236/D240/D254).
//
// The tests here are about ABSENCE as much as presence: the block's specification
// is that an admin sees which room is heavy and has NO WAY IN (D240), so what must
// be asserted is that the response carries names and sizes and carries no route,
// no message and no attachment.

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/admin"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/storage"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/testsupport"
)

// fakeGroupModule is a module that reports named sub-buckets — chat's shape,
// without chat.
type fakeGroupModule struct {
	name   string
	tables []string
	groups []storage.GroupUsage
}

func (m *fakeGroupModule) Name() string            { return m.name }
func (m *fakeGroupModule) StorageTables() []string { return m.tables }
func (m *fakeGroupModule) StorageGroups(context.Context) ([]storage.GroupUsage, error) {
	return m.groups, nil
}

// TestChatBlockCarriesNamesSizesAndNoWayIn is D240, asserted against the CONTRACT
// rather than against the UI.
//
// ⚠ "NO WAY IN" IS A CLAIM ABOUT THE RESPONSE, and the acceptance criterion says so
// explicitly: no route in that payload leads into a conversation. A test that only
// opened the page would pass on a build whose JSON carried a thread URL nobody had
// linked yet.
func TestChatBlockCarriesNamesSizesAndNoWayIn(t *testing.T) {
	db := testsupport.NewDB(t)
	chatish := &fakeGroupModule{
		name:   "chat",
		tables: []string{"chat_conversations"},
		groups: []storage.GroupUsage{
			{ID: "c-1", Name: "Dovolená s Petrou", Members: 3, Objects: 4, Bytes: 200 << 20},
			{ID: "c-2", Name: "Všichni", Members: 5, Objects: 1, Bytes: 1 << 20,
				TrashedAt: "2026-08-20T10:00:00.000Z", PurgeAfter: "2026-08-27T10:00:00.000Z"},
		},
	}
	svc := newStorageService(t, db, []any{chatish}, nil)

	snap, err := svc.Snapshot(context.Background(), true)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snap.Chat == nil {
		t.Fatal("the chat block is absent although a module reports storage groups")
	}
	if len(snap.Chat.Conversations) != 2 {
		t.Fatalf("the block lists %d rooms, want 2", len(snap.Chat.Conversations))
	}
	// ⚠ THE TRASHED ROOM IS COUNTED (D254). An admin chasing an overrun needs to know
	// that a chunk is spoken for but not yet gone.
	want := int64(200<<20) + int64(1<<20)
	if snap.Chat.TotalBytes == nil || *snap.Chat.TotalBytes != want {
		t.Errorf("total_bytes = %v, want %d — a trashed room's bytes are still in R2",
			snap.Chat.TotalBytes, want)
	}
	if snap.Chat.Conversations[1].TrashedAt == nil {
		t.Error("the trashed room is not flagged — *v koši* with days remaining is the point")
	}
	// ⚠ Nezálohováno, always (D229). Chat blobs are the one category in Home
	// deliberately excluded from the mirror.
	if snap.Chat.Mirrored {
		t.Error("the block claims chat is mirrored — chat blobs are deliberately not backed up (D229)")
	}
	// Over-limit against the SEEDED 128 MB per-conversation threshold.
	if !snap.Chat.Conversations[0].OverLimit {
		t.Error("a 200 MB room is not flagged over the 128 MB per-conversation limit")
	}
	if snap.Chat.Conversations[1].OverLimit {
		t.Error("a 1 MB room is flagged over the limit")
	}

	// The absence, asserted on the serialised payload: no message, no attachment, no
	// thread, no clean-up link.
	body, err := json.Marshal(snap.Chat)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, forbidden := range []string{"/chat/", "message", "attachment", "uklid", "cleanup", "thread"} {
		if strings.Contains(strings.ToLower(string(body)), forbidden) {
			t.Errorf("the chat block's payload contains %q — there is NO WAY IN from Administrace "+
				"(D240): an admin sees which room is heavy and asks its members.\n%s", forbidden, body)
		}
	}
	// And member COUNTS, never names.
	if strings.Contains(string(body), "Kája") || strings.Contains(string(body), "user_id") {
		t.Errorf("the block names members; it reports a count (D240)\n%s", body)
	}
}

// TestChatBlockIsAbsentWithoutAGroupSource — a build with no chat module gets no
// empty block rather than one full of zeroes it cannot explain.
func TestChatBlockIsAbsentWithoutAGroupSource(t *testing.T) {
	db := testsupport.NewDB(t)
	plain := &fakeModule{name: "todo", tables: []string{"cards"}}
	svc := newStorageService(t, db, []any{plain}, nil)

	snap, err := svc.Snapshot(context.Background(), true)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snap.Chat != nil {
		t.Errorf("a household with no group source got a chat block: %+v", snap.Chat)
	}
}

// ---- the thresholds ----

// TestSetThresholdsWritesAuditsAndInvalidates is FR-V10-17.
func TestSetThresholdsWritesAuditsAndInvalidates(t *testing.T) {
	f := newFixture(t)
	chatish := &fakeGroupModule{name: "chat", tables: []string{"chat_conversations"},
		groups: []storage.GroupUsage{{ID: "c-1", Name: "Dovolená", Members: 2, Bytes: 700 << 20}}}
	f.svc.SetStorage(newStorageService(t, f.db, []any{chatish}, nil))

	// The seeded pair, read through the snapshot — there is deliberately no GET.
	first, err := f.svc.Storage().Snapshot(context.Background(), true)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if first.Chat.ThresholdTotalMB != 512 {
		t.Fatalf("seeded chat.total = %d MB, want 512", first.Chat.ThresholdTotalMB)
	}
	if !first.Chat.Exceeded {
		t.Fatal("700 MB against a 512 MB limit is not flagged as exceeded")
	}

	out, err := f.svc.SetThresholds(testsupport.CtxUser("u-admin", "admin"),
		admin.StorageThresholdsUpdate{ChatTotalMB: intp(1024)})
	if err != nil {
		t.Fatalf("SetThresholds: %v", err)
	}
	if out.ChatTotalMB != 1024 || out.ChatConversationMB != 128 {
		t.Errorf("saved %d/%d, want 1024/128 — an absent field must not reset its threshold",
			out.ChatTotalMB, out.ChatConversationMB)
	}

	// ⚠ THE SNAPSHOT MUST NOT SERVE THE OLD NUMBER BACK. The fields autosave on blur
	// and there is no Save button to press again, so a stale cache reads as "it did
	// not take".
	second, err := f.svc.Storage().Snapshot(context.Background(), false)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if second.Chat.ThresholdTotalMB != 1024 {
		t.Errorf("the cached snapshot still reports %d MB after a save", second.Chat.ThresholdTotalMB)
	}
	// ⚠ AND A VALUE BELOW CURRENT USAGE IS SAVED, NOT REFUSED (D237/D244). Nothing in
	// v10 is ever blocked by a threshold; the screen says what it just switched on.
	if second.Chat.Exceeded {
		t.Error("700 MB against a raised 1024 MB limit is still flagged exceeded")
	}

	var action, module string
	if err := f.db.QueryRow(
		`SELECT module, action FROM audit_events ORDER BY id DESC LIMIT 1`).Scan(&module, &action); err != nil {
		t.Fatalf("read the event: %v", err)
	}
	if module != "chat" || action != "threshold.update" {
		t.Errorf("the event is %s.%s, want chat.threshold.update (D263) — the setting is an "+
			"admin's to change and the subject is chat's", module, action)
	}
}

// TestSetThresholdsWritesNothingWhenNothingChanged — autosave-on-blur fires on every
// focus loss, so a Log row per blur would bury the changes that mattered.
func TestSetThresholdsWritesNothingWhenNothingChanged(t *testing.T) {
	f := newFixture(t)
	ctx := testsupport.CtxUser("u-admin", "admin")

	before := auditRows(t, f)
	if _, err := f.svc.SetThresholds(ctx, admin.StorageThresholdsUpdate{ChatTotalMB: intp(512)}); err != nil {
		t.Fatalf("SetThresholds: %v", err)
	}
	if got := auditRows(t, f) - before; got != 0 {
		t.Errorf("saving the value that was already there wrote %d event(s)", got)
	}
}

// TestSetThresholdsRefusesNonsense keeps the one invariant SQLite can hold.
func TestSetThresholdsRefusesNonsense(t *testing.T) {
	f := newFixture(t)
	ctx := testsupport.CtxUser("u-admin", "admin")

	if _, err := f.svc.SetThresholds(ctx, admin.StorageThresholdsUpdate{}); err == nil {
		t.Error("an empty body was accepted")
	}
	if _, err := f.svc.SetThresholds(ctx, admin.StorageThresholdsUpdate{ChatTotalMB: intp(0)}); err == nil {
		t.Error("0 MB was accepted")
	}
	if _, err := f.svc.SetThresholds(ctx, admin.StorageThresholdsUpdate{ChatConversationMB: intp(-5)}); err == nil {
		t.Error("a negative limit was accepted")
	}
}

// TestThresholdsRouteIsAdminOnly — the route sits behind RequireAdmin like the rest
// of /admin/storage.
func TestThresholdsRouteIsAdminOnly(t *testing.T) {
	for _, tc := range []struct {
		role string
		want int
	}{
		{"editor", http.StatusForbidden},
		{"reader", http.StatusForbidden},
		{"admin", http.StatusOK},
	} {
		f := newFixture(t)
		f.svc.SetStorage(newStorageService(t, f.db, []any{
			&fakeGroupModule{name: "chat", tables: []string{"chat_conversations"}},
		}, nil))
		rr := do(t, f.router(tc.role), http.MethodPut, "/api/admin/storage/thresholds",
			`{"chat_total_mb":600}`)
		if rr.Code != tc.want {
			t.Errorf("PUT thresholds as %s answered %d, want %d: %s",
				tc.role, rr.Code, tc.want, rr.Body.String())
		}
	}
}

// auditRows counts EVERY event, which is what "no change writes no row" needs — a
// filter on the action would pass over an event written under the wrong one.
func auditRows(t *testing.T, f *fixture) int {
	t.Helper()
	var n int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM audit_events`).Scan(&n); err != nil {
		t.Fatalf("count audit events: %v", err)
	}
	return n
}

func intp(n int) *int { return &n }

// TestSetThresholdsRefusesAnOverflowingValue is the upper bound.
//
// ⚠ `storage.MB` SHIFTS BY 20, so an unbounded value saved cleanly and then became a
// limit of ZERO — after which every comparison read `total > 0` as exceeded and the
// warning fired on every screen for a household holding a few bytes, beside a figure
// claiming the limit was millions of terabytes. `value_mb > 0` is the only invariant
// SQLite can hold, so the bound has to be here.
func TestSetThresholdsRefusesAnOverflowingValue(t *testing.T) {
	f := newFixture(t)
	f.svc.SetStorage(newStorageService(t, f.db, []any{
		&fakeGroupModule{name: "chat", tables: []string{"chat_conversations"}},
	}, nil))
	ctx := testsupport.CtxUser("u-admin", "admin")

	huge := 9007199254740992 // well inside int64, and zero after <<20
	if _, err := f.svc.SetThresholds(ctx, admin.StorageThresholdsUpdate{ChatTotalMB: &huge}); err == nil {
		t.Fatal("a value that overflows MB() was accepted")
	}
	// The seeded value survives, so the refusal did not half-apply.
	snap, err := f.svc.Storage().Snapshot(context.Background(), true)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snap.Chat == nil || snap.Chat.ThresholdTotalMB != 512 {
		t.Errorf("the refused save changed the threshold: %+v", snap.Chat)
	}
	// And the bound itself is still a legal value, so the refusal is a bound and not
	// an off-by-one that rules out everything large.
	atMax := storage.MaxThresholdMB
	if _, err := f.svc.SetThresholds(ctx, admin.StorageThresholdsUpdate{ChatTotalMB: &atMax}); err != nil {
		t.Errorf("the maximum itself was refused: %v", err)
	}
}
