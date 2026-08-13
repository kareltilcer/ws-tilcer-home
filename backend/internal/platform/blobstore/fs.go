package blobstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// FS is a local-directory BlobStore used in development and by every test, so the
// suite runs with no network and no Docker (the S3 path is exercised separately
// against MinIO when HOME_TEST_S3_ENDPOINT is set). Config refuses it in
// production — real deployments must point at R2.
//
// Keys are slash-separated and mapped onto nested directories. Content types are
// not persisted: Get and Stat re-derive them from the stored bytes, which is
// enough for the two consumers (the documents module keeps the sniffed type in
// SQLite, and the mirror only needs it to re-Put). List deliberately leaves the
// field empty — filling it would mean opening every object in the bucket, and no
// caller reads it there.
type FS struct{ root string }

// NewFS creates (if needed) and returns a filesystem-backed store rooted at dir.
func NewFS(dir string) (*FS, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("blobstore: filesystem store needs a directory")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("blobstore: create %q: %w", dir, err)
	}
	return &FS{root: dir}, nil
}

// path maps a key to a filesystem path, refusing anything that could escape root.
func (f *FS) path(key string) (string, error) {
	clean := strings.TrimPrefix(filepath.ToSlash(key), "/")
	if clean == "" || strings.Contains(clean, "..") {
		return "", fmt.Errorf("blobstore: invalid key %q", key)
	}
	return filepath.Join(f.root, filepath.FromSlash(clean)), nil
}

func (f *FS) Put(_ context.Context, key string, r io.Reader, size int64, _ string) error {
	p, err := f.path(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	// Write to a temp file in the same directory and rename, so a crash mid-write
	// never leaves a truncated object that would look durable to the caller.
	tmp, err := os.CreateTemp(filepath.Dir(p), ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName) // no-op once renamed
	}()
	n, err := io.Copy(tmp, r)
	if err != nil {
		return err
	}
	if size >= 0 && n != size {
		return fmt.Errorf("blobstore: %s wrote %d bytes, expected %d", key, n, size)
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, p)
}

func (f *FS) Get(_ context.Context, key string, rng *ByteRange) (io.ReadCloser, ObjInfo, error) {
	p, err := f.path(key)
	if err != nil {
		return nil, ObjInfo{}, err
	}
	file, err := os.Open(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ObjInfo{}, ErrNotFound
		}
		return nil, ObjInfo{}, err
	}
	st, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, ObjInfo{}, err
	}
	info := ObjInfo{Key: key, Size: st.Size(), ModTime: st.ModTime(), ContentType: sniffAt(file)}
	if rng == nil {
		return file, info, nil
	}
	if _, err := file.Seek(rng.Offset, io.SeekStart); err != nil {
		_ = file.Close()
		return nil, ObjInfo{}, err
	}
	length := rng.Length
	if length <= 0 {
		length = st.Size() - rng.Offset
	}
	// LimitReader caps the window; the file handle still owns the Close.
	return readCloser{Reader: io.LimitReader(file, length), Closer: file}, info, nil
}

func (f *FS) Stat(_ context.Context, key string) (ObjInfo, error) {
	p, err := f.path(key)
	if err != nil {
		return ObjInfo{}, err
	}
	// Opened rather than os.Stat'ed: the content type is not persisted, so reporting
	// one means reading the head. It costs a single 512-byte read on a local file.
	file, err := os.Open(p)
	if err != nil {
		if os.IsNotExist(err) {
			return ObjInfo{}, ErrNotFound
		}
		return ObjInfo{}, err
	}
	defer func() { _ = file.Close() }()
	st, err := file.Stat()
	if err != nil {
		return ObjInfo{}, err
	}
	return ObjInfo{Key: key, Size: st.Size(), ModTime: st.ModTime(), ContentType: sniffAt(file)}, nil
}

// sniffAt re-derives an object's content type from its leading bytes — the FS store
// persists no metadata, so the bytes are the only source. ReadAt leaves the file
// offset alone, so a Get can hand the very same handle straight to the caller.
//
// It is not cosmetic: the mirror copies objects with `dst.Put(…, info.ContentType)`,
// so an empty value here writes every mirrored object to the backup bucket with no
// Content-Type, and a restore from that bucket would serve the household's whole
// archive as application/octet-stream. An unreadable head degrades to the same
// generic type a sniff of unrecognisable bytes yields rather than failing the read.
func sniffAt(file *os.File) string {
	var head [512]byte
	n, err := file.ReadAt(head[:], 0)
	if n == 0 && err != nil && !errors.Is(err, io.EOF) {
		return "application/octet-stream"
	}
	return http.DetectContentType(head[:n])
}

func (f *FS) Delete(_ context.Context, keys ...string) error {
	for _, k := range keys {
		p, err := f.path(k)
		if err != nil {
			return err
		}
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
		// Object stores have no directories, so a purged document should leave nothing
		// behind. Remove the now-empty parent to match; a failure means it still holds
		// siblings (or a concurrent Put just recreated one), which is fine to ignore.
		if dir := filepath.Dir(p); dir != f.root {
			_ = os.Remove(dir)
		}
	}
	return nil
}

func (f *FS) List(_ context.Context, prefix string) ([]ObjInfo, error) {
	var out []ObjInfo
	err := filepath.WalkDir(f.root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || strings.HasPrefix(d.Name(), ".tmp-") {
			return nil
		}
		rel, rerr := filepath.Rel(f.root, path)
		if rerr != nil {
			return rerr
		}
		key := filepath.ToSlash(rel)
		if !strings.HasPrefix(key, prefix) {
			return nil
		}
		st, serr := d.Info()
		if serr != nil {
			return serr
		}
		out = append(out, ObjInfo{Key: key, Size: st.Size(), ModTime: st.ModTime()})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

func (f *FS) Copy(ctx context.Context, srcKey, dstKey string, dst BlobStore) error {
	return copyThrough(ctx, f, srcKey, dstKey, dst)
}

// readCloser pairs a windowed reader with the underlying file's Closer.
type readCloser struct {
	io.Reader
	io.Closer
}
