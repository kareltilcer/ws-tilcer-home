package notes

import (
	"context"
	"strings"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/storage"
)

// The notes module's contribution to the v9 storage catalog (D191/D194/D198).
//
// Only this module can do the attribution: an object under `note-images/` maps to
// a note through `note_images.note_id`, and neither `admin` (which may not import
// this package) nor the platform (which has never heard of note_images) can make
// that hop. See documents/storage.go for the same argument on the other prefix.

// StorageBlobs attributes every object under `note-images/` (D194).
//
// The join is one hop longer than the documents one — object → note_images → notes
// — because an image inherits its note's visibility rather than carrying its own
// (D204). That is the same decision that makes the image endpoint's 404 a join
// rather than a column check, and keeping one source of truth for an image's
// audience is worth the extra hop here.
func (s *Service) StorageBlobs(ctx context.Context) ([]storage.BlobUsage, error) {
	if s.blob == nil {
		return nil, nil
	}
	objects, err := s.blob.List(ctx, noteImageKeyPrefix)
	if err != nil {
		return nil, err
	}
	owners, err := s.store.NoteImageOwners(ctx)
	if err != nil {
		return nil, err
	}

	// The bucketing itself is the PLATFORM's — see storage.Attribute, and the
	// documents twin, which now calls the same function. What is genuinely this
	// module's is the pair below: the prefix it owns, and the fact that a
	// note-image key is the prefix plus the row id and nothing else.
	//
	// ⚠ An unrecognised key yields an id that matches no row, which buckets it as
	// unattributed — the honest answer for an object this module cannot place, and
	// the reason `owners` distinguishes a MISSING key from a key mapping to "".
	idOf := func(key string) string { return strings.TrimPrefix(key, noteImageKeyPrefix) }
	return storage.Attribute(objects, noteImageKeyPrefix, idOf, owners), nil
}

// PrivateItems lists this module's private items for the purge screen (D198).
//
// ⚠ WHAT IS ABSENT IS THE SPECIFICATION: no title, no body, no excerpt. An admin
// can name the thing well enough to delete it and not well enough to know what it
// is (D197).
//
// Note images are listed for ACCOUNTING and are marked non-deletable (D212): there
// is no delete route for an image and there should not be — an image belongs to its
// note and goes when the note does. The screen says so rather than offering a
// control that 405s.
// It returns the COMPLETE matching set, unsorted and unpaged: sorting and paging
// belong to the registry, which merges this module's items with the other's.
func (s *Service) PrivateItems(ctx context.Context, ownerUserID string) ([]storage.Item, int64, error) {
	return s.store.PrivateInventory(ctx, ownerUserID)
}
