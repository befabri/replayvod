package hls

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

type recordingLimiter struct {
	burst int
	waits []int
	err   error
}

func (l *recordingLimiter) WaitN(_ context.Context, n int) error {
	l.waits = append(l.waits, n)
	return l.err
}

func (l *recordingLimiter) Burst() int { return l.burst }

func TestThrottledReader_CapsReadsAtBurstAndChargesBytes(t *testing.T) {
	limiter := &recordingLimiter{burst: 4}
	r := newThrottledReader(context.Background(), strings.NewReader("0123456789"), limiter)
	got, err := io.ReadAll(r)
	if err != nil || string(got) != "0123456789" {
		t.Fatalf("read = %q, %v", got, err)
	}
	for _, n := range limiter.waits {
		if n > 4 {
			t.Fatalf("a read exceeded the burst: %v", limiter.waits)
		}
	}
	var total int
	for _, n := range limiter.waits {
		total += n
	}
	if total != 10 {
		t.Fatalf("charged %d bytes, want 10 (%v)", total, limiter.waits)
	}
	if newThrottledReader(context.Background(), strings.NewReader("x"), nil).(*strings.Reader) == nil {
		t.Fatal("nil limiter must return the reader unchanged")
	}
}

func TestThrottledReader_LimiterErrorStopsTheCopy(t *testing.T) {
	limiter := &recordingLimiter{burst: 8, err: context.Canceled}
	r := newThrottledReader(context.Background(), strings.NewReader("payload"), limiter)
	if _, err := io.Copy(io.Discard, r); err != context.Canceled {
		t.Fatalf("copy err = %v, want the limiter's error", err)
	}
}

func TestFetchLimited_PacesTheBody(t *testing.T) {
	payload := bytes.Repeat([]byte("x"), 40<<10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(payload)))
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	f := newTestFetcher(FetcherConfig{})
	dir := t.TempDir()
	w, err := NewPartWriter(dir, "seg.ts")
	if err != nil {
		t.Fatal(err)
	}
	defer w.Abort()
	// 40 KiB/s with a 10 KiB bucket: the first bucket is free, the remaining
	// 30 KiB must take at least three quarters of a second.
	limiter := rate.NewLimiter(rate.Limit(40<<10), 10<<10)
	start := time.Now()
	n, err := f.FetchLimited(context.Background(), srv.URL, w, 0, limiter)
	if err != nil || n != int64(len(payload)) {
		t.Fatalf("fetch = %d, %v", n, err)
	}
	if elapsed := time.Since(start); elapsed < 600*time.Millisecond {
		t.Fatalf("throttled fetch took %v, want at least 600ms", elapsed)
	}
}
