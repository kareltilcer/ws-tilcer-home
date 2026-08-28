package storage

import (
	"context"
	"fmt"
)

// The v10 additions to the catalog: a second projection and the catalog's FIRST
// VERB (PRD D235/D238).
//
// Until now every interface in this package was read-only — a module declared what
// it owned and the platform measured it. `BlobSink` is different in kind: it is a
// module accepting CUSTODY of another module's object, and it exists because
// `chat` may not import `documents` and the move to Dokumenty has to cross that
// line somewhere. The storage catalog is the seam the two already share.

// GroupSource is a module reporting NAMED SUB-BUCKETS of its own storage.
//
// ⚠ IT IS NOT NAMED FOR CHAT'S WORD FOR IT, and that is deliberate (D235).
// `documents` could report per-root and `garden` per-bed through this same
// interface without a second one being invented; a `ConversationSource` would have
// been the third catalog interface in this package written for exactly one caller.
//
// ⚠ THE THRESHOLD COMPARISON LIVES IN THE CONSUMER. A source reports bytes and
// nothing else — no `over_limit`, no threshold value — because the two consumers
// apply DIFFERENT thresholds to the same figures: Administrace compares the module
// total against `chat.total`, and the chat module compares each conversation
// against `chat.conversation`. A source that pre-computed a verdict would have to
// know which of them was asking.
type GroupSource interface {
	StorageGroups(ctx context.Context) ([]GroupUsage, error)
}

// GroupUsage is one named sub-bucket of a module's storage.
type GroupUsage struct {
	ID   string
	Name string
	// Members is a COUNT and never a list. The Úložiště page shows an admin that a
	// room is heavy, not who is in it (D240).
	Members int
	Objects int64
	Bytes   int64
	// TrashedAt is "" unless the group is in a koš. ⚠ A trashed group's bytes are
	// still reported (D254): they are still in R2, and a page whose premise is that
	// its figures sum cannot drop them for a week.
	TrashedAt string
	// PurgeAfter is when those bytes are actually destroyed, "" when nothing is
	// queued. It is what makes "200 MB is spoken for but not yet gone" readable.
	PurgeAfter string
}

// BlobSink is a module that will accept custody of another module's object (D238).
//
// ⚠ IT IS THE FIRST VERB IN A CATALOG THAT HAS SO FAR CARRIED ONLY PROJECTIONS,
// and the asymmetry is worth stating: everything else here answers a question,
// while this one WRITES — a document row, an audit event and an object under
// another module's prefix. It is implemented by `documents` and handed to `chat`
// at composition as an OPTIONAL dependency.
//
// ⚠ THE SINK OWNS STEPS 1–3 OF THE MOVE AND NOTHING ELSE (FR-V10-14):
//
//	1. validate the target folder is shared and writable
//	2. copy the source object to its own prefix — server-side, nothing streams
//	   through the app
//	3. insert its row, in its own transaction, with its own audit event
//
// Steps 4 and 5 — marking the source `moved`, then deleting the source object
// LAST — belong to the calling module, because only it knows what it is giving
// away. No transaction covers all five and none can: two SQLite writes and two
// object-store calls. The ordering is what guarantees every crash point
// OVER-COUNTS rather than loses.
//
// ⚠ A SINK MUST NEVER DELETE THE SOURCE. That is the caller's step 5, and a sink
// that helpfully tidied up would be deleting bytes before its own row was known to
// have committed.
type BlobSink interface {
	AcceptBlob(ctx context.Context, req AcceptRequest) (AcceptResult, error)
}

// AcceptRequest is one custody transfer, described by the module giving the bytes
// away.
//
// ⚠ IT CARRIES NO DESTINATION KEY. The sink mints its own id and builds its own
// key, because the layout under its prefix is its business and a caller that named
// the destination would be writing into another module's namespace.
type AcceptRequest struct {
	// SourceKey is the object as it exists today, in the caller's prefix.
	SourceKey string
	// FolderID is the target folder. ⚠ A PRIVATE folder must be refused with 422
	// (D245): a private target would make the file unreadable to the people the
	// move exists to keep it readable for.
	FolderID string
	// Filename, ContentType, ByteSize and Checksum describe the bytes. They are the
	// caller's already-sniffed values (D48 happened at upload); the sink trusts
	// them because they came from a module, not from a client.
	Filename    string
	ContentType string
	ByteSize    int64
	Checksum    string
	// Title is what the accepting module should call the item; "" lets it derive
	// one from the filename.
	Title string
	// Via names the originating module for the audit event's `meta.via`, so a
	// document that arrived by transfer is distinguishable from one somebody
	// uploaded (FR-V10-14 step 3).
	Via string
}

// AcceptResult is what the caller renders afterwards.
type AcceptResult struct {
	// DocumentID identifies the row that now owns the bytes.
	DocumentID string
	// Path is the accepting module's content URL — what the chat bubble renders in
	// place of its own `/raw` once the attachment is `moved` (D246).
	Path string
}

// Groups resolves every group-reporting module's usage.
//
// A module's failure is returned rather than swallowed, exactly as Blobs does:
// the page reports the failure with its reason, which is more useful than a table
// that is quietly short a room.
func (r *Registry) Groups(ctx context.Context) (map[string][]GroupUsage, error) {
	out := map[string][]GroupUsage{}
	if r == nil {
		return out, nil
	}
	for _, module := range r.order {
		gs, ok := r.groups[module]
		if !ok {
			continue
		}
		usage, err := gs.StorageGroups(ctx)
		if err != nil {
			return nil, fmt.Errorf("storage: %s group usage: %w", module, err)
		}
		out[module] = usage
	}
	return out, nil
}

// GroupsOf resolves ONE module's groups.
//
// ⚠ IT EXISTS SO A CONSUMER THAT WANTS ONE BLOCK DOES NOT RUN EVERY SOURCE. Groups
// above resolves them all, which is right for a caller rendering all of them and
// wrong for Administrace's chat block: today chat is the only implementer so the
// difference is nil, but the interface is deliberately generic (D235 — `documents`
// could report per-root, `garden` per-bed), and the moment a second one exists its
// full per-group query would run on every uncached snapshot and be discarded.
//
// An unknown or non-implementing module is (nil, nil) — the caller renders nothing,
// which is the same answer it gives for a household without that module at all.
func (r *Registry) GroupsOf(ctx context.Context, module string) ([]GroupUsage, error) {
	if r == nil {
		return nil, nil
	}
	gs, ok := r.groups[module]
	if !ok {
		return nil, nil
	}
	usage, err := gs.StorageGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: %s group usage: %w", module, err)
	}
	return usage, nil
}

// GroupModules returns, in registration order, the modules that implement
// GroupSource — the same declare-once/ask-the-catalog shape InventoryModules has,
// and for the same reason: a consumer must not carry a hand-written module name.
func (r *Registry) GroupModules() []string {
	if r == nil {
		return nil
	}
	out := make([]string, 0, len(r.groups))
	for _, module := range r.order {
		if _, ok := r.groups[module]; ok {
			out = append(out, module)
		}
	}
	return out
}
