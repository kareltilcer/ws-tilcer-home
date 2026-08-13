package blobstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

// S3Config points an S3 client at one bucket. For Cloudflare R2 the endpoint is
// the account's S3 API URL and the region is always "auto".
type S3Config struct {
	Bucket    string
	Endpoint  string
	AccessKey string
	SecretKey string
	Region    string // "" defaults to "auto" (R2)
}

// S3 is an S3-compatible BlobStore. Built for Cloudflare R2: path-style
// addressing (R2 does not do virtual-host style for arbitrary buckets), region
// "auto", and no server-side checksum trailers — the documents module computes
// and stores its own SHA-256, which is also the /raw ETag.
type S3 struct {
	client *s3.Client
	bucket string
	// endpoint/region/accessKey identify the ACCOUNT, not the client object. Copy
	// compares these to decide whether a server-side copy is possible: every store
	// builds its own *s3.Client, so comparing client pointers would never match even
	// for two buckets in the same account.
	endpoint  string
	region    string
	accessKey string
}

// NewS3 builds a client for cfg. It does no network I/O: a bad endpoint or a
// wrong secret surfaces on the first real call (and, at boot, on the readiness
// probe rather than as a crash).
func NewS3(cfg S3Config) (*S3, error) {
	if strings.TrimSpace(cfg.Bucket) == "" {
		return nil, errors.New("blobstore: s3 needs a bucket")
	}
	if strings.TrimSpace(cfg.Endpoint) == "" {
		return nil, errors.New("blobstore: s3 needs an endpoint")
	}
	region := cfg.Region
	if region == "" {
		region = "auto"
	}
	client := s3.New(s3.Options{
		Region:       region,
		BaseEndpoint: aws.String(cfg.Endpoint),
		UsePathStyle: true,
		Credentials: aws.NewCredentialsCache(
			credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
		),
		// R2 rejects the SDK's default streaming checksum trailers on some paths;
		// requesting no additional checksum keeps PutObject to a plain signed body.
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
		ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
	})
	return &S3{
		client:    client,
		bucket:    cfg.Bucket,
		endpoint:  cfg.Endpoint,
		region:    region,
		accessKey: cfg.AccessKey,
	}, nil
}

// Bucket reports the bucket this store writes to (used in log lines).
func (s *S3) Bucket() string { return s.bucket }

func (s *S3) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	in := &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(key),
		Body:          r,
		ContentLength: aws.Int64(size),
	}
	if contentType != "" {
		in.ContentType = aws.String(contentType)
	}
	if _, err := s.client.PutObject(ctx, in); err != nil {
		return fmt.Errorf("blobstore: put %s: %w", key, err)
	}
	return nil
}

func (s *S3) Get(ctx context.Context, key string, rng *ByteRange) (io.ReadCloser, ObjInfo, error) {
	in := &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)}
	if rng != nil {
		in.Range = aws.String(httpRangeHeader(*rng))
	}
	out, err := s.client.GetObject(ctx, in)
	if err != nil {
		if isNotFound(err) {
			return nil, ObjInfo{}, ErrNotFound
		}
		return nil, ObjInfo{}, fmt.Errorf("blobstore: get %s: %w", key, err)
	}
	info := ObjInfo{
		Key:         key,
		Size:        deref(out.ContentLength),
		ContentType: derefStr(out.ContentType),
		ModTime:     derefTime(out.LastModified),
	}
	// For a ranged read ContentLength is the WINDOW length; recover the full object
	// size from Content-Range ("bytes 0-99/12345") so callers always see the total.
	if rng != nil && out.ContentRange != nil {
		if total, ok := totalFromContentRange(*out.ContentRange); ok {
			info.Size = total
		}
	}
	return out.Body, info, nil
}

func (s *S3) Stat(ctx context.Context, key string) (ObjInfo, error) {
	out, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if isNotFound(err) {
			return ObjInfo{}, ErrNotFound
		}
		return ObjInfo{}, fmt.Errorf("blobstore: stat %s: %w", key, err)
	}
	return ObjInfo{
		Key:         key,
		Size:        deref(out.ContentLength),
		ContentType: derefStr(out.ContentType),
		ModTime:     derefTime(out.LastModified),
	}, nil
}

func (s *S3) Delete(ctx context.Context, keys ...string) error {
	for _, k := range keys {
		_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(s.bucket),
			Key:    aws.String(k),
		})
		if err != nil && !isNotFound(err) {
			return fmt.Errorf("blobstore: delete %s: %w", k, err)
		}
	}
	return nil
}

func (s *S3) List(ctx context.Context, prefix string) ([]ObjInfo, error) {
	var out []ObjInfo
	p := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(prefix),
	})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("blobstore: list %s: %w", prefix, err)
		}
		for _, o := range page.Contents {
			out = append(out, ObjInfo{
				Key:     derefStr(o.Key),
				Size:    deref(o.Size),
				ModTime: derefTime(o.LastModified),
			})
		}
	}
	return out, nil
}

// Copy prefers a server-side copy when the destination is another bucket in the
// SAME account (no bytes leave R2); otherwise it streams through this process.
func (s *S3) Copy(ctx context.Context, srcKey, dstKey string, dst BlobStore) error {
	if other, ok := dst.(*S3); ok && s.sameAccount(other) {
		_, err := s.client.CopyObject(ctx, &s3.CopyObjectInput{
			Bucket:     aws.String(other.bucket),
			Key:        aws.String(dstKey),
			CopySource: aws.String(s.bucket + "/" + srcKey),
		})
		if err != nil {
			return fmt.Errorf("blobstore: copy %s: %w", srcKey, err)
		}
		return nil
	}
	return copyThrough(ctx, s, srcKey, dstKey, dst)
}

// sameAccount reports whether other is reachable with this store's credentials, so
// CopyObject can name its bucket as the destination. Same endpoint + region + access
// key is exactly the condition R2/S3 require for a cross-bucket server-side copy.
func (s *S3) sameAccount(other *S3) bool {
	return other != nil &&
		s.endpoint == other.endpoint &&
		s.region == other.region &&
		s.accessKey == other.accessKey
}

// isNotFound recognises both the typed NoSuchKey/NotFound errors and the bare 404
// HeadObject returns (which carries no typed body).
func isNotFound(err error) bool {
	var nsk *types.NoSuchKey
	if errors.As(err, &nsk) {
		return true
	}
	var nf *types.NotFound
	if errors.As(err, &nf) {
		return true
	}
	var api smithy.APIError
	if errors.As(err, &api) {
		switch api.ErrorCode() {
		case "NoSuchKey", "NotFound", "404":
			return true
		}
	}
	return false
}

func httpRangeHeader(r ByteRange) string {
	if r.Length <= 0 {
		return fmt.Sprintf("bytes=%d-", r.Offset)
	}
	return fmt.Sprintf("bytes=%d-%d", r.Offset, r.Offset+r.Length-1)
}

// totalFromContentRange extracts the total size from "bytes 0-99/12345".
func totalFromContentRange(cr string) (int64, bool) {
	i := strings.LastIndex(cr, "/")
	if i < 0 || i == len(cr)-1 {
		return 0, false
	}
	var n int64
	if _, err := fmt.Sscanf(cr[i+1:], "%d", &n); err != nil {
		return 0, false
	}
	return n, true
}

func deref(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func derefTime(p *time.Time) time.Time {
	if p == nil {
		return time.Time{}
	}
	return *p
}
