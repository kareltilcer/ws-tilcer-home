package chat

import (
	"context"
	"embed"
	"io/fs"

	"github.com/go-chi/chi/v5"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/registry"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/storage"
)

//go:embed migrations/*.sql
var MigrationsFS embed.FS

// Module is the chat (Chat) feature module: one household room everybody is in,
// plus groups anybody can create, carrying text now and images, video and files
// from PR 3.
//
// ⚠ THE ELEVENTH MODULE, AND THE FIRST ONE THE HOUSEHOLD DOES NOT READ IN FULL.
// v1–v8 published data every member could see. v9 added a second access axis,
// OWNERSHIP, and made a private item invisible to everyone but its owner. v10 adds
// a third: MEMBERSHIP. A conversation is readable by the people in it, from their
// effective_from onward — neither "everyone" nor "one person" — and nearly every
// platform surface built so far assumes it is one of those two. scope.go is where
// that rule lives, and it is the only place it lives.
//
// TWO IMPORTS IT MUST NEVER GAIN, asserted by internal/arch: platform/metrics and
// platform/lists (D252). Chat publishes no widget, no metric and no list, and the
// electricity precedent applies for the same reason it did there — a metric exists
// to be a CONDITION in Administrace, which is the first step toward a notification
// nobody could silence per conversation. Chat DOES take audit, blobstore, push,
// scheduler, storage and ws.
//
// ⚠ platform/widgets/registry.tsx GAINS NOTHING and must be absent from the diff.
// An entry there for a module with no widget provider produces a dashboard tile
// that resolves to nothing — no compile error, no runtime error, an empty card
// (v8's trap). Third version running with that file untouched.
type Module struct {
	handler *Handler
	svc     *Service
}

// NewModule builds the chat module over svc.
func NewModule(svc *Service) *Module {
	return &Module{handler: NewHandler(svc), svc: svc}
}

// StorageBlobs and StorageGroups forward to the service for the storage catalog.
//
// ⚠ THEIR ABSENCE IS SILENT — the registry duck-types both interfaces, so a module
// that stops implementing one is simply skipped and its bytes vanish from the page
// with nothing failing to compile. That is the §V9-12 trap
// (TestRealModulesImplementTheStorageCatalog) and it is why the wiring is asserted
// by a test rather than trusted.
//
// ⚠ THE NIL GUARDS ARE FOR bootstrap.StorageSourcesForTest, which builds a
// ZERO-VALUED module to read StorageTables() — a static list that touches nothing.
// The guards mean a future caller that reaches further gets an empty answer rather
// than a nil dereference inside the completeness test.
func (m *Module) StorageBlobs(ctx context.Context) ([]storage.BlobUsage, error) {
	if m.svc == nil {
		return nil, nil
	}
	return m.svc.StorageBlobs(ctx)
}

func (m *Module) StorageGroups(ctx context.Context) ([]storage.GroupUsage, error) {
	if m.svc == nil {
		return nil, nil
	}
	return m.svc.StorageGroups(ctx)
}

func (m *Module) Name() string { return "chat" }

func (m *Module) RegisterRoutes(r chi.Router) { m.handler.Mount(r) }

func (m *Module) Migrations() fs.FS { return MigrationsFS }

// AuditActions are the verbs this module emits.
//
// ⚠ THERE IS NO chat.message.* ACTION, AND THAT IS THE POINT (D231). Sending,
// editing and deleting a message write NOTHING to the Log — chat is the first
// module in Home whose primary mutation is invisible there, deliberately, because
// audit_events would otherwise become a second, admin-readable copy of every
// conversation in the house. TestChatMessagesAreNotAudited asserts it so the gap is
// never closed by somebody who reads it as an oversight.
//
// ⚠ ELEVEN NOW: PR 3 adds the three chat.attachment.* verbs and the threshold one.
// PR 2 declared seven deliberately, because a declared action that can never fire is
// a dead entry in the trigger composer's picker — an admin offered a rule that will
// never run. These four are declared in the PR that makes them fire.
//
// ⚠ ATTACHMENTS ARE AUDITED ALTHOUGH THE MESSAGES CARRYING THEM ARE NOT. That looks
// inconsistent and is not: the BYTES are what the two thresholds, the clean-up page
// and the storage register exist for, and "who uploaded that 40 MB video, and when"
// is the question the whole storage half of v10 answers. `chat.attachment.uploaded`
// carries the filename and the conversation name; no event in this module ever
// carries message text.
//
// ⚠ `threshold.update` IS DECLARED HERE AND EMITTED BY `admin`, and that is not a
// mistake. The write happens on an admin route (PUT /api/admin/storage/thresholds)
// because an admin owns the setting, but the ACTION is chat's — the same shape D255
// uses for restore and purge, where an admin's verb over a chat room stays a chat
// event. `admin` writes it with audit.ModuleChat and the catalog is what makes the
// two agree.
//
// ⚠ AND IT IS `threshold.update`, NOT `settings.updated` (D263, from the design
// bundle, which is later than the PRD and wins on this point).
//
// Structural changes ARE audited, and the asymmetry with messages is deliberate
// rather than inconsistent: who created, renamed, trashed or purged a room, and who
// added or removed whom, are the questions a household actually asks a year later.
func (m *Module) AuditActions() []string {
	return []string{
		"conversation.created", "conversation.renamed", "conversation.deleted",
		"conversation.restored", "conversation.purged",
		"member.added", "member.removed",
		"attachment.uploaded", "attachment.removed", "attachment.moved",
		"threshold.update",
	}
}

// Widgets returns NIL, not an empty non-nil slice (the electricity precedent).
//
// An empty non-nil slice would put an empty section into the admin composer, which
// is worse than the module simply not appearing there. Chat publishes nothing to
// Nástěnka (D252) and this is the method that says so.
//
// There is deliberately no MetricProvider() and no ListProvider(): metrics.Collect
// and lists.Collect are duck-typed on their Source interfaces and skip a module that
// does not implement them, so NOT WRITING THEM is the mechanism.
func (m *Module) Widgets() []registry.WidgetProvider { return nil }

// StorageTables declares this module's tables for the storage catalog (D191/D211).
//
// ⚠ chat_messages_fts IS THE FIFTH EXTERNAL-CONTENT FTS5 INDEX IN HOME, and each
// one materialises FIVE `type='table'` rows — so this takes the shadow count from
// twenty to twenty-five. storage.FTSShadows is what keeps that from being five
// hand-written strings; §V9-12 records that garden_plants_fts went uncounted for two
// versions, and arch/storage_completeness_test.go is what stops it a third time.
//
// chat_attachments and chat_deleted_keys are declared here although PR 2 writes
// neither: the tables exist from 12001, and a table in the schema that no module
// declares fails the completeness guard immediately — which is exactly what that
// guard is for.
func (m *Module) StorageTables() []string {
	return append([]string{
		"chat_conversations", "chat_members", "chat_messages",
		"chat_attachments", "chat_deleted_keys",
	}, storage.FTSShadows("chat_messages_fts")...)
}
