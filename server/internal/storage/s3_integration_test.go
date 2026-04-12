//go:build integration

package storage_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"io"
	"testing"

	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/testdb"
)

// These tests pin contracts a unit test can't observe: the S3 wire
// protocol (range-based GetObject, NoSuchKey translation), multipart
// upload behavior on the s3manager path, and the end-to-end handshake
// through aws-sdk-go-v2 against a real-enough S3 endpoint. Run with
//
//	go test -tags integration ./internal/storage/
//
// which pulls the Garage image once (~100 MB) and caches it locally.

func newS3(t *testing.T) *storage.S3Storage {
	t.Helper()
	g := testdb.NewGarageBucket(t)
	s, err := storage.NewS3(context.Background(), storage.S3Options{
		Endpoint:     g.Endpoint,
		Bucket:       g.BucketName,
		Region:       g.Region,
		AccessKey:    g.AccessKey,
		SecretKey:    g.SecretKey,
		UsePathStyle: true, // Garage requires path-style.
	})
	if err != nil {
		t.Fatalf("NewS3: %v", err)
	}
	return s
}

// TestS3_RoundTrip_SaveOpenReadAllBytes guards the core data path:
// Save a multi-MB blob, Open, read to EOF, compare bytes. Multi-MB
// so s3manager.Uploader takes the multipart path (5 MiB part size —
// a 6 MiB blob triggers it). Plain PutObject would paper over
// multipart-specific bugs.
func TestS3_RoundTrip_SaveOpenReadAllBytes(t *testing.T) {
	ctx := context.Background()
	s := newS3(t)

	src := make([]byte, 6*1024*1024+123) // 6 MiB + change, forces multipart
	if _, err := rand.Read(src); err != nil {
		t.Fatalf("rand: %v", err)
	}

	if err := s.Save(ctx, "videos/round-trip.bin", bytes.NewReader(src)); err != nil {
		t.Fatalf("Save: %v", err)
	}

	rc, err := s.Open(ctx, "videos/round-trip.bin")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer rc.Close()

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, src) {
		t.Fatalf("round-trip mismatch: got %d bytes, want %d", len(got), len(src))
	}
}

// TestS3_Range_MidFileSeekAndRead guards the s3ReadSeeker: seek to
// an arbitrary offset, read a slice, verify it matches the same
// slice from the source. This is the hot path for
// http.ServeContent — if range-based GetObject is broken, byte-range
// video playback silently serves wrong bytes instead of erroring,
// which is the worst failure mode.
func TestS3_Range_MidFileSeekAndRead(t *testing.T) {
	ctx := context.Background()
	s := newS3(t)

	const size = 1 * 1024 * 1024 // 1 MiB — big enough for interesting offsets, small enough to be fast
	src := make([]byte, size)
	if _, err := rand.Read(src); err != nil {
		t.Fatalf("rand: %v", err)
	}
	if err := s.Save(ctx, "videos/range.bin", bytes.NewReader(src)); err != nil {
		t.Fatalf("Save: %v", err)
	}

	rc, err := s.Open(ctx, "videos/range.bin")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer rc.Close()

	// Seek into the middle, read 4 KiB, compare.
	const (
		offset = 500_000
		length = 4096
	)
	if _, err := rc.Seek(offset, io.SeekStart); err != nil {
		t.Fatalf("Seek: %v", err)
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(rc, buf); err != nil {
		t.Fatalf("ReadFull: %v", err)
	}
	if !bytes.Equal(buf, src[offset:offset+length]) {
		t.Fatalf("range mismatch at offset %d", offset)
	}

	// Seek backwards — must re-issue a ranged GET, not reuse the body.
	if _, err := rc.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("Seek back: %v", err)
	}
	first4k := make([]byte, length)
	if _, err := io.ReadFull(rc, first4k); err != nil {
		t.Fatalf("ReadFull after back-seek: %v", err)
	}
	if !bytes.Equal(first4k, src[:length]) {
		t.Fatal("back-seek range mismatch")
	}

	// SeekEnd: offset relative to size. http.ServeContent uses this
	// to learn the total size before serving.
	end, err := rc.Seek(0, io.SeekEnd)
	if err != nil {
		t.Fatalf("SeekEnd: %v", err)
	}
	if end != int64(size) {
		t.Errorf("SeekEnd = %d, want %d", end, size)
	}
}

// TestS3_Delete_IsIdempotent pins the Storage contract. Downloader
// retry paths and cleanup tasks call Delete without knowing whether
// the object exists; a NoSuchKey error on the second call would
// surface as spurious retry noise in the logs.
func TestS3_Delete_IsIdempotent(t *testing.T) {
	ctx := context.Background()
	s := newS3(t)

	if err := s.Save(ctx, "videos/doomed.bin", bytes.NewReader([]byte("hello"))); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := s.Delete(ctx, "videos/doomed.bin"); err != nil {
		t.Fatalf("first Delete: %v", err)
	}
	if err := s.Delete(ctx, "videos/doomed.bin"); err != nil {
		t.Errorf("second Delete must be idempotent; got %v", err)
	}
	// Delete on a key that was never created also must not error.
	if err := s.Delete(ctx, "videos/never-existed.bin"); err != nil {
		t.Errorf("Delete on missing key must not error; got %v", err)
	}
}

// TestS3_Open_MissingKey_ReturnsNotFound pins the error-translation
// contract. The video streaming handler uses this to emit a clean
// 404 instead of a raw AWS error; without the S3 NoSuchKey →
// errNotFound translation, missing-thumbnail requests would 500.
func TestS3_Open_MissingKey_ReturnsNotFound(t *testing.T) {
	ctx := context.Background()
	s := newS3(t)

	_, err := s.Open(ctx, "videos/does-not-exist.bin")
	if err == nil {
		t.Fatal("Open on missing key must return an error")
	}
	// errNotFound is unexported; we can't type-assert from outside
	// the package, but the error message pins the shape. Loose
	// check — upgrade to a typed sentinel when storage exports one.
	if !containsAny(err.Error(), "key not found", "not found", "NoSuchKey") {
		t.Errorf("error should look like NotFound; got %v", err)
	}

	// Stat on the same missing key takes the same translation path.
	if _, err := s.Stat(ctx, "videos/does-not-exist.bin"); err == nil {
		t.Error("Stat on missing key must return an error")
	}

	// Exists is the friendly wrapper — returns false, nil.
	ok, err := s.Exists(ctx, "videos/does-not-exist.bin")
	if err != nil {
		t.Errorf("Exists must not error for missing key; got %v", err)
	}
	if ok {
		t.Error("Exists must report false for missing key")
	}
}

// containsAny is a tiny test helper to check loose error-message
// shapes without pulling in a regex dependency.
func containsAny(s string, needles ...string) bool {
	for _, n := range needles {
		if bytes.Contains([]byte(s), []byte(n)) {
			return true
		}
	}
	return false
}

// Compile-time escape-hatch check: errors package is used to keep
// `errors.Is` around even when no test currently calls it — the
// ErrNotFound export would use this path once it lands.
var _ = errors.Is
