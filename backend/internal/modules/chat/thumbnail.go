package chat

import (
	"context"
	"fmt"
	"image"
	_ "image/gif"  // register GIF for image.Decode
	_ "image/jpeg" // register JPEG
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	xdraw "golang.org/x/image/draw" // scaling kernels the stdlib lacks
	_ "golang.org/x/image/webp"     // register WebP decoding — a pasted WebP gets a thumbnail too
)

// Image thumbnails and intrinsic dimensions (D227, HANDOFF-design §v10).
//
// ⚠ THIS IS A SECOND COPY OF documents/preview.go's SCALING, AND IT IS DELIBERATE.
// D227 declines to create `platform/preview`: extracting the preview worker out of
// a live module inside an already-large version is a refactor with its own failure
// mode, and internal/arch forbids `chat` from importing `documents`. So chat carries
// the forty lines it needs, WITHOUT the half it does not — no gotenberg, no PDF
// rastering, no retry column, no worker pool. If a third module ever wants this, the
// extraction becomes worth doing; two copies is the cheaper answer today.
//
// ⚠ IT RUNS INLINE, IN THE UPLOAD REQUEST, and that is a departure from Dokumenty
// worth stating. §V10-4a warns that a background job has no actor and must load
// through an explicit any-membership variant — thumbnail generation is one of the
// three it names. Doing the work in the request removes that hazard entirely rather
// than guarding against it: there is an actor, the membership is already resolved,
// and there is no `zpracovává se` state to design, no pending column to sweep at
// boot and no thumbnail that silently never appears. The cost is bounded — a
// household message carries at most ten files, each capped at HOME_DOCS_MAX_UPLOAD_MB
// — and a failure is non-fatal: the attachment simply has no thumbnail and the UI
// falls back to the full image, exactly as it does for one whose generation failed.

// dimensionsOf reads an image's intrinsic size from its HEADER without decoding it.
//
// ⚠ THE DIMENSIONS ARE THE POINT OF THIS FILE, more than the thumbnail is. They are
// what lets the bubble reserve the right box before the bytes arrive, and a thread
// that reflows while somebody is reading it is the most-noticed bug in any chat
// (HANDOFF-design §v10). They are recorded even when the thumbnail fails.
func dimensionsOf(path string) (width, height int, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = f.Close() }()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return 0, 0, err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return 0, 0, fmt.Errorf("image has no pixels")
	}
	return cfg.Width, cfg.Height, nil
}

// ThumbOptions is what the module was configured with — all of it borrowed from
// Dokumenty's configuration (D228's principle applied past the upload cap): one
// cwebp path, one thumbnail size, one decode ceiling for the whole application.
type ThumbOptions struct {
	CwebpPath      string
	MaxPx          int
	MaxImagePixels int
	TempDir        string
}

// makeThumbnail scales an image down and encodes it as WebP, returning the path of
// the encoded file.
//
// ⚠ THE PIXEL CEILING IS CHECKED BEFORE ANY DECODE, and it is not a nicety.
// MaxUploadBytes bounds the FILE, which bounds nothing here: compression ratio is
// unlimited, so a ~5 MB PNG can be 30000×30000 and decode into ~3.6 GB of RGBA.
// This runs in the app process, so that allocation does not "fail the thumbnail" —
// it OOM-kills the backend and takes every module in Home with it. A 100 MP
// panorama reaches this honestly; it does not have to be an attack.
func makeThumbnail(ctx context.Context, opts ThumbOptions, src, dir string) (string, error) {
	if strings.TrimSpace(opts.CwebpPath) == "" {
		return "", fmt.Errorf("cwebp is not configured")
	}
	pngPath := filepath.Join(dir, "thumb.png")
	if err := scaleImageFile(opts, src, pngPath); err != nil {
		return "", err
	}
	webpPath := filepath.Join(dir, keyThumb)
	cmd := exec.CommandContext(ctx, opts.CwebpPath, "-quiet", "-q", "80", pngPath, "-o", webpPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("cwebp: %w (%s)", err, trimOutput(out))
	}
	return webpPath, nil
}

// scaleImageFile decodes, scales so the longest edge is MaxPx (never upscaling),
// and writes a PNG for cwebp to encode.
func scaleImageFile(opts ThumbOptions, src, dst string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	hdr, _, err := image.DecodeConfig(f)
	if err != nil {
		return err
	}
	if hdr.Width <= 0 || hdr.Height <= 0 {
		return fmt.Errorf("image has no pixels")
	}
	if px := int64(hdr.Width) * int64(hdr.Height); opts.MaxImagePixels > 0 && px > int64(opts.MaxImagePixels) {
		return fmt.Errorf("image is %dx%d, over the %d MP decode limit",
			hdr.Width, hdr.Height, opts.MaxImagePixels/1e6)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	img, _, err := image.Decode(f)
	if err != nil {
		return err
	}
	b := img.Bounds()
	width, height := b.Dx(), b.Dy()
	if width <= 0 || height <= 0 {
		return fmt.Errorf("image has no pixels")
	}
	maxPx := opts.MaxPx
	if maxPx < 32 {
		maxPx = 480
	}
	if width > maxPx || height > maxPx {
		if width >= height {
			height = max(1, height*maxPx/width)
			width = maxPx
		} else {
			width = max(1, width*maxPx/height)
			height = maxPx
		}
	}
	dstImg := image.NewRGBA(image.Rect(0, 0, width, height))
	xdraw.CatmullRom.Scale(dstImg, dstImg.Bounds(), img, b, xdraw.Over, nil)
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	if err := png.Encode(out, dstImg); err != nil {
		return err
	}
	return out.Sync()
}

// trimOutput bounds a subprocess's output before it reaches a log line.
func trimOutput(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 400 {
		return s[:400] + "…"
	}
	return s
}

