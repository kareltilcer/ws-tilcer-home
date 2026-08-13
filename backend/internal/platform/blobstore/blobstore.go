// Package blobstore is the object-storage client for bytes that do not belong in
// SQLite (HANDOFF-6 §2). It is platform infrastructure like db/ws — importable by
// anyone, owned by no feature module — and in v4 only `documents` uses it.
//
// The interface exposes exactly what the documents module needs and nothing that
// leaks R2 into feature logic. Two implementations ship: S3 (Cloudflare R2) for
// real deployments and a local filesystem store for development and tests, so
// `go test ./...` needs neither network nor Docker.
//
// Invariants callers rely on:
//   - Streaming only. An object may be up to HOME_DOCS_MAX_UPLOAD_MB (50 MB); no
//     implementation may buffer a whole object in memory on the way in or out.
//   - Immutability is the CALLER's invariant, not the store's: the documents
//     module writes a given {id}/original key exactly once and never overwrites
//     it (PRD D41). Nothing here enforces that, and there is no "replace" path in
//     the module that would need it.
//   - No presigned URLs. Content is always served THROUGH the backend so it stays
//     session-gated and same-origin (PRD D33/D42).
package blobstore

import (
	"context"
	"errors"
	"io"
	"time"
)

// ErrNotFound is returned by Get/Stat when the key does not exist. Callers map it
// to a 404 rather than a 500.
var ErrNotFound = errors.New("blobstore: object not found")

// ByteRange is an inclusive byte range for a partial read (HTTP Range support on
// the /raw endpoint). Length <= 0 means "to the end of the object".
type ByteRange struct {
	Offset int64
	Length int64
}

// ObjInfo is the metadata a caller needs about a stored object: its key, size,
// content type, and last modification time (used by the mirror and the orphan
// reconciliation pass, which compares object age against a grace window).
type ObjInfo struct {
	Key         string
	Size        int64
	ContentType string
	ModTime     time.Time
}

// BlobStore is the object-storage contract.
type BlobStore interface {
	// Put streams size bytes from r to key with the given content type. size must
	// be exact: the S3 implementation signs and sends a known-length body.
	Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
	// Get opens key for reading. rng nil reads the whole object; a non-nil rng
	// reads that window (used for HTTP Range). The returned ObjInfo always carries
	// the FULL object size, not the length of the returned window.
	Get(ctx context.Context, key string, rng *ByteRange) (io.ReadCloser, ObjInfo, error)
	// Stat reports an object's metadata without reading it. ErrNotFound when absent.
	Stat(ctx context.Context, key string) (ObjInfo, error)
	// Delete removes the given keys. Deleting an absent key is not an error, so a
	// repeated purge (or a purge racing the reconciliation pass) is idempotent.
	Delete(ctx context.Context, keys ...string) error
	// List returns every object under prefix. Household-scale data (a few thousand
	// objects at most) means a full listing is cheap and paging is unnecessary.
	List(ctx context.Context, prefix string) ([]ObjInfo, error)
	// Copy copies srcKey from this store to dstKey in dst. Used by the backup
	// mirror; dst may be the same store or a different bucket/account.
	Copy(ctx context.Context, srcKey, dstKey string, dst BlobStore) error
}

// copyThrough is the generic Copy fallback: stream the source object into the
// destination. Used whenever a server-side copy is unavailable (cross-account,
// cross-implementation, or the filesystem store).
func copyThrough(ctx context.Context, src BlobStore, srcKey, dstKey string, dst BlobStore) error {
	body, info, err := src.Get(ctx, srcKey, nil)
	if err != nil {
		return err
	}
	defer func() { _ = body.Close() }()
	return dst.Put(ctx, dstKey, body, info.Size, info.ContentType)
}
