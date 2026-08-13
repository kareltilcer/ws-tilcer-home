package blobstore_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/blobstore"
)

// The contract table below is the definition of "a BlobStore works". It runs
// against the filesystem store on every `go test` (hermetic: no network, no
// Docker) and against a real S3/R2 endpoint when HOME_TEST_S3_ENDPOINT is set,
// so both implementations are held to identical semantics.

type factory func(t *testing.T) blobstore.BlobStore

func fsFactory(t *testing.T) blobstore.BlobStore {
	t.Helper()
	s, err := blobstore.NewFS(t.TempDir())
	if err != nil {
		t.Fatalf("new fs store: %v", err)
	}
	return s
}

// s3Factory builds an S3 store against a MinIO/R2 endpoint, or skips.
func s3Factory(t *testing.T) blobstore.BlobStore {
	t.Helper()
	endpoint := os.Getenv("HOME_TEST_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("HOME_TEST_S3_ENDPOINT not set — skipping the S3 contract run")
	}
	s, err := blobstore.NewS3(blobstore.S3Config{
		Bucket:    envOr("HOME_TEST_S3_BUCKET", "home-docs-test"),
		Endpoint:  endpoint,
		AccessKey: os.Getenv("HOME_TEST_S3_ACCESS_KEY_ID"),
		SecretKey: os.Getenv("HOME_TEST_S3_SECRET_ACCESS_KEY"),
		Region:    envOr("HOME_TEST_S3_REGION", "auto"),
	})
	if err != nil {
		t.Fatalf("new s3 store: %v", err)
	}
	return s
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func TestBlobStoreContract(t *testing.T) {
	impls := map[string]factory{"fs": fsFactory, "s3": s3Factory}
	for name, newStore := range impls {
		t.Run(name, func(t *testing.T) {
			runContract(t, newStore)
		})
	}
}

func runContract(t *testing.T, newStore factory) {
	t.Helper()
	ctx := context.Background()

	t.Run("put then get round-trips bytes and content type", func(t *testing.T) {
		s := newStore(t)
		body := []byte("Smlouva ČEZ — elektřina")
		put(t, s, "documents/d1/original", body, "text/plain; charset=utf-8")

		r, info, err := s.Get(ctx, "documents/d1/original", nil)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		defer func() { _ = r.Close() }()
		got, err := io.ReadAll(r)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if !bytes.Equal(got, body) {
			t.Fatalf("bytes mismatch: got %q want %q", got, body)
		}
		if info.Size != int64(len(body)) {
			t.Fatalf("size = %d, want %d", info.Size, len(body))
		}
	})

	t.Run("ranged get returns the window but the full size", func(t *testing.T) {
		s := newStore(t)
		body := []byte("0123456789abcdef")
		put(t, s, "documents/d2/original", body, "application/octet-stream")

		r, info, err := s.Get(ctx, "documents/d2/original", &blobstore.ByteRange{Offset: 4, Length: 6})
		if err != nil {
			t.Fatalf("ranged get: %v", err)
		}
		defer func() { _ = r.Close() }()
		got, err := io.ReadAll(r)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if string(got) != "456789" {
			t.Fatalf("window = %q, want %q", got, "456789")
		}
		// The /raw handler needs the TOTAL size to build Content-Range.
		if info.Size != int64(len(body)) {
			t.Fatalf("info.Size = %d, want the full %d", info.Size, len(body))
		}
	})

	t.Run("open-ended range reads to the end", func(t *testing.T) {
		s := newStore(t)
		put(t, s, "documents/d3/original", []byte("abcdefgh"), "")
		r, _, err := s.Get(ctx, "documents/d3/original", &blobstore.ByteRange{Offset: 6})
		if err != nil {
			t.Fatalf("ranged get: %v", err)
		}
		defer func() { _ = r.Close() }()
		got, _ := io.ReadAll(r)
		if string(got) != "gh" {
			t.Fatalf("tail = %q, want %q", got, "gh")
		}
	})

	// The mirror re-Puts what Get reports (`dst.Put(…, info.ContentType)`), so an
	// implementation that leaves the field empty fills the backup bucket with objects
	// that have no Content-Type — and a restore from it serves the household's whole
	// archive as application/octet-stream. Both stores must answer, whether the type
	// was stored alongside the object (S3) or re-derived from the bytes (FS).
	t.Run("get and stat report a content type", func(t *testing.T) {
		s := newStore(t)
		body := []byte("%PDF-1.7\n1 0 obj\n<<>>\nendobj\ntrailer\n%%EOF\n")
		put(t, s, "documents/d9/original", body, "application/pdf")

		r, info, err := s.Get(ctx, "documents/d9/original", nil)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		defer func() { _ = r.Close() }()
		if info.ContentType != "application/pdf" {
			t.Errorf("Get content type = %q, want application/pdf", info.ContentType)
		}
		st, err := s.Stat(ctx, "documents/d9/original")
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if st.ContentType != "application/pdf" {
			t.Errorf("Stat content type = %q, want application/pdf", st.ContentType)
		}
	})

	t.Run("missing key is ErrNotFound, not a generic failure", func(t *testing.T) {
		s := newStore(t)
		if _, _, err := s.Get(ctx, "documents/nope/original", nil); !errors.Is(err, blobstore.ErrNotFound) {
			t.Fatalf("get missing: got %v, want ErrNotFound", err)
		}
		if _, err := s.Stat(ctx, "documents/nope/original"); !errors.Is(err, blobstore.ErrNotFound) {
			t.Fatalf("stat missing: got %v, want ErrNotFound", err)
		}
	})

	t.Run("delete is idempotent", func(t *testing.T) {
		s := newStore(t)
		put(t, s, "documents/d4/original", []byte("x"), "")
		if err := s.Delete(ctx, "documents/d4/original"); err != nil {
			t.Fatalf("delete: %v", err)
		}
		// A repeated purge (or one racing the reconciliation pass) must not error.
		if err := s.Delete(ctx, "documents/d4/original"); err != nil {
			t.Fatalf("second delete: %v", err)
		}
		if _, err := s.Stat(ctx, "documents/d4/original"); !errors.Is(err, blobstore.ErrNotFound) {
			t.Fatalf("stat after delete: got %v, want ErrNotFound", err)
		}
	})

	t.Run("list filters by prefix", func(t *testing.T) {
		s := newStore(t)
		put(t, s, "documents/d5/original", []byte("a"), "")
		put(t, s, "documents/d5/thumb.webp", []byte("b"), "")
		put(t, s, "documents/d6/original", []byte("c"), "")

		got, err := s.List(ctx, "documents/d5/")
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("list returned %d objects, want 2: %+v", len(got), got)
		}
		for _, o := range got {
			if !strings.HasPrefix(o.Key, "documents/d5/") {
				t.Fatalf("unexpected key %q outside the prefix", o.Key)
			}
		}
	})

	t.Run("copy duplicates an object into another store", func(t *testing.T) {
		src := newStore(t)
		dst := fsFactory(t) // cross-implementation copy exercises the streaming path
		put(t, src, "documents/d7/original", []byte("mirror me"), "application/pdf")

		if err := src.Copy(ctx, "documents/d7/original", "documents/d7/original", dst); err != nil {
			t.Fatalf("copy: %v", err)
		}
		r, _, err := dst.Get(ctx, "documents/d7/original", nil)
		if err != nil {
			t.Fatalf("get copy: %v", err)
		}
		defer func() { _ = r.Close() }()
		got, _ := io.ReadAll(r)
		if string(got) != "mirror me" {
			t.Fatalf("copied bytes = %q", got)
		}
	})

	t.Run("put rejects a size that disagrees with the stream", func(t *testing.T) {
		s, ok := newStore(t).(*blobstore.FS)
		if !ok {
			t.Skip("size-mismatch guard is filesystem-specific (S3 rejects server-side)")
		}
		err := s.Put(ctx, "documents/d8/original", strings.NewReader("abc"), 99, "")
		if err == nil {
			t.Fatal("expected an error when the declared size does not match the bytes")
		}
	})
}

func put(t *testing.T, s blobstore.BlobStore, key string, body []byte, ct string) {
	t.Helper()
	if err := s.Put(context.Background(), key, bytes.NewReader(body), int64(len(body)), ct); err != nil {
		t.Fatalf("put %s: %v", key, err)
	}
}
