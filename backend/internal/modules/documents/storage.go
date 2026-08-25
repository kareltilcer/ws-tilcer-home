package documents

import (
	"context"
	"strings"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/storage"
)

// The documents module's contribution to the v9 storage catalog (D191/D194/D198).
//
// Two things live here, and both exist because ONLY THIS MODULE CAN DO THEM.
// `admin` may not import `documents` (the import-lint enforces D28), and the
// platform has no idea that `documents/{id}/original` has anything to do with a
// row in `documents` — so the attribution has to be computed on this side of the
// boundary and handed over as plain numbers.

// StorageBlobs attributes every object under this module's prefix to the household,
// to a member, or to nobody (D194).
//
// ⚠ IT LISTS THE BUCKET RATHER THAN SUMMING documents.byte_size, and that is
// deliberate twice over:
//
//   - Summing the column would miss the DERIVED objects — preview.pdf and
//     thumb.webp — whose sizes are recorded in no table at all. On a household
//     with a lot of scanned PDFs those are a large fraction of the bill.
//   - Summing the column would make objects that resolve to NO LIVE ROW silently
//     invisible. Listing surfaces them as `unattributed`: the orphan backlog the
//     mirror job already reconciles, on a screen for the first time.
//
// `unattributed` is not an error state and not padding. It is a real number with a
// real meaning, and the page says what it is rather than hiding it.
func (s *Service) StorageBlobs(ctx context.Context) ([]storage.BlobUsage, error) {
	if s.blob == nil {
		return nil, nil
	}
	objects, err := s.blob.List(ctx, keyPrefix)
	if err != nil {
		return nil, err
	}
	owners, err := s.store.DocumentOwners(ctx)
	if err != nil {
		return nil, err
	}

	// The bucketing itself is the PLATFORM's: it is the same arithmetic on both
	// sides of the storage page, and the two copies of it had to be kept in step by
	// hand. What stays here is the pair of facts only this module holds — the
	// prefix it owns, and how a key maps back to a document id. See
	// storage.Attribute for the invariants (both fixed kinds always emitted,
	// per-member rows sorted).
	return storage.Attribute(objects, keyPrefix, documentIDFromKey, owners), nil
}

// documentIDFromKey extracts the id from `documents/{id}/original` and friends.
// A key that does not have that shape yields "", which buckets it as unattributed
// — the honest answer for an object this module does not recognise.
func documentIDFromKey(key string) string {
	rest, ok := strings.CutPrefix(key, keyPrefix)
	if !ok {
		return ""
	}
	id, _, _ := strings.Cut(rest, "/")
	return id
}

// PrivateItems lists this module's private items for the purge screen (D198).
//
// ⚠ WHAT IS ABSENT IS THE SPECIFICATION. No title, no filename, no description, no
// content type, no preview, no download. An admin can name the thing well enough
// to delete it and not well enough to know what it is (D197). If a field is ever
// added here, it has to be justified against that sentence.
//
// Folders are included (D212): `DELETE …/folders/{id}?hard=true&cascade=true` is
// what actually reclaims a private subtree, and a screen that cannot name a folder
// cannot do the job it exists for.
// It returns the COMPLETE matching set, unsorted and unpaged: sorting and paging
// belong to the registry, which merges this module's items with the other's.
//
// ⚠ THE SIZES COME FROM THE BUCKET, not from documents.byte_size, for exactly the
// reason StorageBlobs above lists rather than sums: `byte_size` is the ORIGINAL
// object alone, while a hard delete purges preview.pdf and thumb.webp with it
// (objectKeysOf), and those two are recorded in no table. On a private tree of
// scanned PDFs the derived objects are a large fraction of the bytes, so summing
// the column told an admin a purge would reclaim roughly half what it does — on the
// one screen whose whole purpose is deciding whether the purge is worth doing.
//
// One LIST of the module's prefix, the same call StorageBlobs makes, keyed back to
// the row by documentIDFromKey. A bucket that will not answer costs the figures
// their derived half rather than the whole listing: the row sizes still stand, they
// are simply the low bound they always were.
func (s *Service) PrivateItems(ctx context.Context, ownerUserID string) ([]storage.Item, int64, error) {
	items, total, err := s.store.PrivateInventory(ctx, ownerUserID)
	if err != nil || s.blob == nil {
		return items, total, err
	}
	objects, err := s.blob.List(ctx, keyPrefix)
	if err != nil {
		return items, total, nil
	}
	bytesByID := map[string]int64{}
	for _, o := range objects {
		bytesByID[documentIDFromKey(o.Key)] += o.Size
	}
	total = 0
	for i := range items {
		// A document whose objects are missing from the bucket keeps its row figure:
		// an orphaned row is not a zero-byte one.
		if items[i].Kind == storage.ItemDocument {
			if b, ok := bytesByID[items[i].ID]; ok {
				items[i].ByteSize = b
			}
		}
		total += items[i].ByteSize
	}
	return items, total, nil
}
