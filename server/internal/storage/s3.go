package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

// S3Storage is an S3-compatible backend. Works with AWS S3, MinIO,
// Backblaze B2, Cloudflare R2, and Ceph — anything the aws-sdk-go-v2
// client speaks.
//
// Design notes:
//   - Uploads go through s3manager.Uploader, which handles multipart
//     transparently (5 MB parts, 5 concurrent parts). Video files run
//     into gigabytes; a single PutObject would OOM or time out.
//   - Open returns a seekable io.ReadSeekCloser backed by ranged
//     GetObject calls, so http.ServeContent can serve byte-range
//     requests without pulling the full object each time.
type S3Storage struct {
	client   *s3.Client
	uploader *manager.Uploader
	bucket   string
}

// S3Options configures the client. Endpoint is optional; empty means
// AWS default resolution. Region is required even for MinIO (pass any
// value — "us-east-1" works).
//
// AccessKey + SecretKey are optional. When either is empty, NewS3
// skips the static credentials provider so the AWS SDK walks its
// default chain (env vars, shared credentials file, EC2/ECS
// metadata, SSO, etc.). Set both only when you explicitly want to
// pin credentials in config — production IAM-role deployments should
// leave them empty.
type S3Options struct {
	Endpoint  string
	Bucket    string
	Region    string
	AccessKey string
	SecretKey string
	// UsePathStyle forces path-style addressing (bucket in the URL
	// path instead of the subdomain). Required for MinIO and most
	// self-hosted S3 implementations; AWS S3 accepts both.
	UsePathStyle bool
}

// NewS3 builds a backend. Returns an error when required options are
// missing; the caller (main.go) surfaces that at startup so a bad
// config fails loudly rather than silently degrading.
func NewS3(ctx context.Context, opts S3Options) (*S3Storage, error) {
	if opts.Bucket == "" {
		return nil, fmt.Errorf("s3 storage: bucket required")
	}
	if opts.Region == "" {
		return nil, fmt.Errorf("s3 storage: region required")
	}

	// Enforce all-or-nothing credentials: either both AccessKey and
	// SecretKey are set (explicit static provider) or both are empty
	// (delegate to the AWS SDK default chain). Asymmetric config
	// would silently skip the static provider and fall through to
	// whatever IAM role / env var happens to be available — operator
	// thinks their TOML took effect, auth quietly uses something else.
	if (opts.AccessKey == "") != (opts.SecretKey == "") {
		return nil, fmt.Errorf("s3 storage: AccessKey and SecretKey must both be set or both be empty")
	}

	loadOpts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(opts.Region),
	}
	if opts.AccessKey != "" && opts.SecretKey != "" {
		loadOpts = append(loadOpts,
			awsconfig.WithCredentialsProvider(
				credentials.NewStaticCredentialsProvider(opts.AccessKey, opts.SecretKey, ""),
			),
		)
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return nil, fmt.Errorf("s3 storage: load aws config: %w", err)
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		if opts.Endpoint != "" {
			o.BaseEndpoint = aws.String(opts.Endpoint)
		}
		o.UsePathStyle = opts.UsePathStyle
	})

	// TODO: swap for github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager
	// when it exits the feature/ prefix and reaches v1 stable. The
	// deprecation warnings here are aspirational — SDK still supports
	// this path and the successor is in pre-release territory.
	uploader := manager.NewUploader(client, func(u *manager.Uploader) {
		// 5 MiB parts — S3 minimum. Videos that fit in a single part
		// skip multipart entirely via the uploader's internal check.
		u.PartSize = 5 * 1024 * 1024
	})

	return &S3Storage{
		client:   client,
		uploader: uploader,
		bucket:   opts.Bucket,
	}, nil
}

// objectKey normalizes a Storage path into an S3 object key. Leading
// slashes and "./" are trimmed; ".." is rejected (keys can legally
// contain "..", but our path contract forbids it for parity with
// LocalStorage's resolve()).
func objectKey(p string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("empty path")
	}
	// Normalize forward-slash; reject double-slashes / parent refs.
	cleaned := strings.TrimLeft(p, "/")
	if cleaned == "" {
		return "", fmt.Errorf("empty path")
	}
	for _, part := range strings.Split(cleaned, "/") {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("invalid path segment in %q", p)
		}
	}
	return cleaned, nil
}

func (s *S3Storage) Save(ctx context.Context, path string, r io.Reader) error {
	key, err := objectKey(path)
	if err != nil {
		return err
	}
	_, err = s.uploader.Upload(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
		Body:   r,
	})
	if err != nil {
		return fmt.Errorf("s3 put %s: %w", key, err)
	}
	return nil
}

func (s *S3Storage) Open(ctx context.Context, path string) (io.ReadSeekCloser, error) {
	key, err := objectKey(path)
	if err != nil {
		return nil, err
	}
	// Probe size with HEAD — the ReadSeekCloser caller (http.ServeContent)
	// seeks relative to the end before reading, so we need size up front.
	head, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if is404(err) {
			return nil, errNotFound{key: key}
		}
		return nil, fmt.Errorf("s3 head %s: %w", key, err)
	}
	if head.ContentLength == nil {
		return nil, fmt.Errorf("s3 head %s: missing Content-Length", key)
	}
	return &s3ReadSeeker{
		ctx:    ctx,
		client: s.client,
		bucket: s.bucket,
		key:    key,
		size:   *head.ContentLength,
	}, nil
}

func (s *S3Storage) Delete(ctx context.Context, path string) error {
	key, err := objectKey(path)
	if err != nil {
		return err
	}
	_, err = s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil && !is404(err) {
		return fmt.Errorf("s3 delete %s: %w", key, err)
	}
	return nil
}

func (s *S3Storage) Exists(ctx context.Context, path string) (bool, error) {
	key, err := objectKey(path)
	if err != nil {
		return false, err
	}
	_, err = s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if is404(err) {
			return false, nil
		}
		return false, fmt.Errorf("s3 head %s: %w", key, err)
	}
	return true, nil
}

func (s *S3Storage) Stat(ctx context.Context, path string) (FileInfo, error) {
	key, err := objectKey(path)
	if err != nil {
		return FileInfo{}, err
	}
	head, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if is404(err) {
			return FileInfo{}, errNotFound{key: key}
		}
		return FileInfo{}, fmt.Errorf("s3 head %s: %w", key, err)
	}
	var size int64
	if head.ContentLength != nil {
		size = *head.ContentLength
	}
	var mod time.Time
	if head.LastModified != nil {
		mod = *head.LastModified
	}
	return FileInfo{Size: size, ModTime: mod}, nil
}

// is404 detects the "key not found" case across both AWS S3 (NoSuchKey
// error code) and other S3-compatible servers (404 from the HTTP layer).
func is404(err error) bool {
	var nsk *types.NoSuchKey
	if errors.As(err, &nsk) {
		return true
	}
	var nf *types.NotFound
	if errors.As(err, &nf) {
		return true
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		code := apiErr.ErrorCode()
		if code == "NoSuchKey" || code == "NotFound" || code == "404" {
			return true
		}
	}
	var respErr interface{ HTTPStatusCode() int }
	if errors.As(err, &respErr) && respErr.HTTPStatusCode() == http.StatusNotFound {
		return true
	}
	return false
}

// errNotFound lets Stat/Open signal a missing object in a way callers
// can detect with errors.Is(err, os.ErrNotExist)-style checks. We
// don't import "os" here to keep the type local; callers that want
// os.ErrNotExist semantics can wrap with errors.Join or adopt the
// storage-specific check.
type errNotFound struct {
	key string
}

func (e errNotFound) Error() string {
	return fmt.Sprintf("s3: key not found: %s", e.key)
}

// s3ReadSeeker issues ranged GetObject calls on Seek/Read so
// http.ServeContent can serve byte-range requests without downloading
// the full object. Not efficient for many small random reads — video
// streaming does large sequential reads with occasional seeks, which
// matches the cost profile well.
type s3ReadSeeker struct {
	ctx    context.Context
	client *s3.Client
	bucket string
	key    string
	size   int64
	offset int64
	// Current GET body is held open until the next Seek or Close; a
	// sequence of sequential Reads reuses this body instead of opening
	// a new ranged GET per call.
	body io.ReadCloser
	// bodyStart tracks the absolute offset the current body was
	// opened at so sequential reads can match offset == bodyStart+read.
	bodyStart int64
	bodyRead  int64
}

func (r *s3ReadSeeker) Read(p []byte) (int, error) {
	if r.offset >= r.size {
		return 0, io.EOF
	}
	if r.body == nil || r.bodyStart+r.bodyRead != r.offset {
		if r.body != nil {
			_ = r.body.Close()
			r.body = nil
		}
		if err := r.openRange(r.offset); err != nil {
			return 0, err
		}
	}
	n, err := r.body.Read(p)
	r.offset += int64(n)
	r.bodyRead += int64(n)
	if errors.Is(err, io.EOF) && r.offset < r.size {
		// Server closed the range stream but we haven't read the
		// whole object yet — pretend it's fine; the next Read opens
		// a fresh range. Matters for servers that split a single
		// response across multiple TCP sessions.
		_ = r.body.Close()
		r.body = nil
		return n, nil
	}
	return n, err
}

func (r *s3ReadSeeker) Seek(offset int64, whence int) (int64, error) {
	var abs int64
	switch whence {
	case io.SeekStart:
		abs = offset
	case io.SeekCurrent:
		abs = r.offset + offset
	case io.SeekEnd:
		abs = r.size + offset
	default:
		return 0, fmt.Errorf("invalid whence %d", whence)
	}
	if abs < 0 {
		return 0, fmt.Errorf("negative offset %d", abs)
	}
	r.offset = abs
	return abs, nil
}

func (r *s3ReadSeeker) Close() error {
	if r.body != nil {
		err := r.body.Close()
		r.body = nil
		return err
	}
	return nil
}

// openRange issues a GetObject with Range: bytes=off- starting at abs.
// The body is held on r.body until the next seek or close.
func (r *s3ReadSeeker) openRange(abs int64) error {
	rangeHeader := fmt.Sprintf("bytes=%d-", abs)
	out, err := r.client.GetObject(r.ctx, &s3.GetObjectInput{
		Bucket: aws.String(r.bucket),
		Key:    aws.String(r.key),
		Range:  aws.String(rangeHeader),
	})
	if err != nil {
		return fmt.Errorf("s3 get %s range %s: %w", r.key, rangeHeader, err)
	}
	r.body = out.Body
	r.bodyStart = abs
	r.bodyRead = 0
	return nil
}

// assert interface satisfaction at compile time.
var (
	_ Storage         = (*S3Storage)(nil)
	_ io.ReadSeekCloser = (*s3ReadSeeker)(nil)
	_                 = bytes.NewReader // avoid unused import if above patterns change
)
