package hls

import (
	"context"
	"io"
)

// RateLimiter bounds how many body bytes a job may pull from the edge per
// second. *rate.Limiter from golang.org/x/time/rate satisfies it; nil means
// unlimited.
type RateLimiter interface {
	WaitN(ctx context.Context, n int) error
	Burst() int
}

// throttledReader charges every read against the limiter after it lands.
// Reads are capped at the limiter's burst so one call never asks for more
// tokens than the bucket can hold, which WaitN would refuse outright.
type throttledReader struct {
	ctx     context.Context
	r       io.Reader
	limiter RateLimiter
	chunk   int
}

func newThrottledReader(ctx context.Context, r io.Reader, limiter RateLimiter) io.Reader {
	if limiter == nil {
		return r
	}
	return &throttledReader{ctx: ctx, r: r, limiter: limiter, chunk: max(1, limiter.Burst())}
}

func (t *throttledReader) Read(p []byte) (int, error) {
	if len(p) > t.chunk {
		p = p[:t.chunk]
	}
	n, err := t.r.Read(p)
	if n > 0 {
		if werr := t.limiter.WaitN(t.ctx, n); werr != nil {
			return n, werr
		}
	}
	return n, err
}
