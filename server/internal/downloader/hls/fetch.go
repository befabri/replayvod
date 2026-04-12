package hls

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// FetcherConfig carries the per-class retry budgets documented in
// .docs/spec/download-pipeline.md Stage 4. Each class counts
// independently — a 404-heavy segment won't exhaust the transport
// budget just because the orchestrator is also seeing network
// flakes on other segments.
//
// BaseBackoff + MaxBackoff are the full-jitter window bounds.
// TargetDuration is the media playlist's EXT-X-TARGETDURATION,
// used to space CDN-lag retries at half-targetDuration intervals
// (live segments propagate within a few seconds or not at all).
type FetcherConfig struct {
	TransportAttempts   int
	ServerErrorAttempts int
	CDNLagAttempts      int

	BaseBackoff    time.Duration
	MaxBackoff     time.Duration
	TargetDuration time.Duration
}

func (c FetcherConfig) normalize() FetcherConfig {
	if c.TransportAttempts <= 0 {
		c.TransportAttempts = 5
	}
	if c.ServerErrorAttempts <= 0 {
		c.ServerErrorAttempts = 5
	}
	if c.CDNLagAttempts <= 0 {
		c.CDNLagAttempts = 3
	}
	if c.BaseBackoff <= 0 {
		c.BaseBackoff = 500 * time.Millisecond
	}
	if c.MaxBackoff <= 0 {
		c.MaxBackoff = 30 * time.Second
	}
	if c.TargetDuration <= 0 {
		c.TargetDuration = 2 * time.Second
	}
	return c
}

// Fetcher fetches a single segment to disk with the per-class
// retry loop. One Fetcher is shared across all worker goroutines
// in a job — it carries no per-segment state.
//
// Auth failures (401/403) surface as FetchError{Kind: FetchKindAuth}
// without consuming any retry budget here. The orchestrator owns
// auth refresh: it re-runs Stages 1-2, gets a new signed media-
// playlist URL with fresh segment URLs, and re-queues the
// segment. The authRefreshes budget from the spec lives in the
// orchestrator, not the Fetcher.
type Fetcher struct {
	client *http.Client
	log    *slog.Logger
	cfg    FetcherConfig

	// bufPool is a reusable 256 KB copy buffer shared across
	// concurrent Fetch calls. Spec Stage 4: io.Copy allocates a
	// fresh 32 KB buffer each call, which matters at segment-
	// per-second rates. ReadFrom on os.File bypasses this
	// (sendfile fast path on Linux); the buffer is the fallback
	// for transports that don't expose a direct file fd.
	bufPool sync.Pool
}

// NewFetcher builds a Fetcher. The http.Client is taken
// by-reference so callers can share one transport across all
// jobs in the process.
func NewFetcher(client *http.Client, cfg FetcherConfig, log *slog.Logger) *Fetcher {
	f := &Fetcher{
		client: client,
		log:    log.With("domain", "hls.fetch"),
		cfg:    cfg.normalize(),
	}
	f.bufPool.New = func() any {
		b := make([]byte, 256<<10)
		return &b
	}
	return f
}

// FetchKind identifies the failure class. Orchestrator switches
// on this to decide: retry with new URL (auth), record a gap
// (transport/server/cdn_lag after budget exhausted), fail the
// job (body/malformed).
type FetchKind int

const (
	FetchKindOK FetchKind = iota
	FetchKindTransport
	FetchKindServer
	FetchKindCDNLag
	FetchKindAuth
	FetchKindBody
	FetchKindMalformed
)

// String is purely for logs/errors. Stable format so log scrapers
// can match on it.
func (k FetchKind) String() string {
	switch k {
	case FetchKindOK:
		return "ok"
	case FetchKindTransport:
		return "transport"
	case FetchKindServer:
		return "server"
	case FetchKindCDNLag:
		return "cdn_lag"
	case FetchKindAuth:
		return "auth"
	case FetchKindBody:
		return "body"
	case FetchKindMalformed:
		return "malformed"
	}
	return "unknown"
}

// FetchError is the typed failure returned by Fetch. Permanent
// means "budget exhausted or unrecoverable"; the orchestrator
// should not re-queue the segment with the same URL.
type FetchError struct {
	Kind      FetchKind
	Status    int // 0 for transport errors
	Attempts  int // attempts burned before returning
	Cause     error
	Permanent bool
}

func (e *FetchError) Error() string {
	if e.Status == 0 {
		return fmt.Sprintf("hls fetch: %s (attempts=%d): %v", e.Kind, e.Attempts, e.Cause)
	}
	return fmt.Sprintf("hls fetch: %s status=%d (attempts=%d): %v", e.Kind, e.Status, e.Attempts, e.Cause)
}

func (e *FetchError) Unwrap() error { return e.Cause }

// Fetch runs the per-segment retry loop. On success returns the
// bytes written and nil. Caller must Commit or Abort the writer
// based on the return value.
//
// The writer is driven directly by the HTTP body copy — no
// buffering in Fetcher itself (beyond the sync.Pool buffer used
// when ReadFrom isn't available). Content-Length mismatches are
// treated as transport-class truncation errors and burn the
// transport budget.
//
//nolint:gocyclo // State machine; splitting hurts readability.
func (f *Fetcher) Fetch(ctx context.Context, url string, w *PartWriter) (int64, error) {
	var (
		transportAttempts = 0
		serverAttempts    = 0
		cdnLagAttempts    = 0
	)

	for {
		select {
		case <-ctx.Done():
			return 0, &FetchError{Kind: FetchKindTransport, Attempts: transportAttempts, Cause: ctx.Err(), Permanent: true}
		default:
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return 0, &FetchError{Kind: FetchKindMalformed, Attempts: 0, Cause: err, Permanent: true}
		}

		resp, err := f.client.Do(req)
		if err != nil {
			transportAttempts++
			if transportAttempts >= f.cfg.TransportAttempts {
				return 0, &FetchError{Kind: FetchKindTransport, Attempts: transportAttempts, Cause: err, Permanent: true}
			}
			if sleepErr := f.sleep(ctx, Backoff(transportAttempts-1, f.cfg.BaseBackoff, f.cfg.MaxBackoff)); sleepErr != nil {
				return 0, &FetchError{Kind: FetchKindTransport, Attempts: transportAttempts, Cause: sleepErr, Permanent: true}
			}
			continue
		}

		switch {
		case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
			drainAndClose(resp)
			return 0, &FetchError{Kind: FetchKindAuth, Status: resp.StatusCode, Attempts: 1, Permanent: false}

		case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
			// CDN lag path — spec Stage 4: live segments propagate
			// within a few seconds or they never will. Sleep half
			// targetDuration so we hit the next CDN refresh tick
			// rather than waiting a full window.
			cdnLagAttempts++
			drainAndClose(resp)
			if cdnLagAttempts >= f.cfg.CDNLagAttempts {
				return 0, &FetchError{Kind: FetchKindCDNLag, Status: resp.StatusCode, Attempts: cdnLagAttempts, Permanent: true}
			}
			if sleepErr := f.sleep(ctx, f.cfg.TargetDuration/2); sleepErr != nil {
				return 0, &FetchError{Kind: FetchKindCDNLag, Status: resp.StatusCode, Attempts: cdnLagAttempts, Cause: sleepErr, Permanent: true}
			}
			continue

		case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
			serverAttempts++
			// Honor Retry-After first; fall back to full-jitter
			// when absent or unparseable.
			wait, ok := RetryAfter(resp)
			drainAndClose(resp)
			if serverAttempts >= f.cfg.ServerErrorAttempts {
				return 0, &FetchError{Kind: FetchKindServer, Status: resp.StatusCode, Attempts: serverAttempts, Permanent: true}
			}
			if !ok {
				wait = Backoff(serverAttempts-1, f.cfg.BaseBackoff, f.cfg.MaxBackoff)
			}
			if sleepErr := f.sleep(ctx, wait); sleepErr != nil {
				return 0, &FetchError{Kind: FetchKindServer, Status: resp.StatusCode, Attempts: serverAttempts, Cause: sleepErr, Permanent: true}
			}
			continue

		case resp.StatusCode >= 200 && resp.StatusCode < 300:
			// Fallthrough to body copy below.

		default:
			// 3xx unexpected here (http.Client follows redirects
			// by default), 4xx other than 401/403/404/410, etc.
			// Treat as permanent — neither retry nor auth refresh
			// will fix them.
			drainAndClose(resp)
			return 0, &FetchError{
				Kind:      FetchKindMalformed,
				Status:    resp.StatusCode,
				Attempts:  1,
				Cause:     fmt.Errorf("unexpected status %d", resp.StatusCode),
				Permanent: true,
			}
		}

		// 2xx body copy.
		n, copyErr := f.copyBody(w, resp.Body)
		// Always drain — if copyErr hit mid-body, remaining bytes
		// would break keep-alive.
		drainAndClose(resp)

		if copyErr != nil {
			transportAttempts++
			if transportAttempts >= f.cfg.TransportAttempts {
				return 0, &FetchError{Kind: FetchKindTransport, Attempts: transportAttempts, Cause: copyErr, Permanent: true}
			}
			if sleepErr := f.sleep(ctx, Backoff(transportAttempts-1, f.cfg.BaseBackoff, f.cfg.MaxBackoff)); sleepErr != nil {
				return 0, &FetchError{Kind: FetchKindTransport, Attempts: transportAttempts, Cause: sleepErr, Permanent: true}
			}
			continue
		}

		// Content-Length sanity check. Mismatch → transport-
		// class (silent truncation). We only check when the
		// server declared a length; chunked responses without
		// a length are accepted as-is.
		if resp.ContentLength >= 0 && n != resp.ContentLength {
			transportAttempts++
			if transportAttempts >= f.cfg.TransportAttempts {
				return 0, &FetchError{
					Kind:      FetchKindTransport,
					Attempts:  transportAttempts,
					Cause:     fmt.Errorf("short read: got %d, want %d", n, resp.ContentLength),
					Permanent: true,
				}
			}
			if sleepErr := f.sleep(ctx, Backoff(transportAttempts-1, f.cfg.BaseBackoff, f.cfg.MaxBackoff)); sleepErr != nil {
				return 0, &FetchError{Kind: FetchKindTransport, Attempts: transportAttempts, Cause: sleepErr, Permanent: true}
			}
			continue
		}

		return n, nil
	}
}

// copyBody streams the body into the writer. When the writer
// implements io.ReaderFrom (PartWriter does via os.File), net/http
// short-circuits to a direct copy that skips the buffer pool.
// Otherwise we lean on the pooled 256 KB buffer to cut allocations
// at segment-per-second rates.
func (f *Fetcher) copyBody(w io.Writer, r io.Reader) (int64, error) {
	if rf, ok := w.(io.ReaderFrom); ok {
		return rf.ReadFrom(r)
	}
	buf := f.bufPool.Get().(*[]byte)
	defer f.bufPool.Put(buf)
	return io.CopyBuffer(w, r, *buf)
}

// sleep honors ctx cancellation so a long backoff doesn't delay
// job shutdown. Returns ctx.Err() on cancel; nil otherwise.
func (f *Fetcher) sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// drainAndClose eats any unread body bytes and closes the
// response body. Failing to drain breaks keep-alive; the 64 KB
// cap means a broken server feeding megabytes of error HTML
// doesn't stall the goroutine.
func drainAndClose(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_, _ = io.CopyN(io.Discard, resp.Body, 64<<10)
	_ = resp.Body.Close()
}

// IsAuth reports whether err is a FetchError with FetchKindAuth.
// Convenience for orchestrator code that branches on auth vs
// other failures.
func IsAuth(err error) bool {
	var fe *FetchError
	return errors.As(err, &fe) && fe.Kind == FetchKindAuth
}
