package downloader

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"testing/synctest"
	"time"

	"github.com/befabri/replayvod/server/internal/downloader/twitch"
	"github.com/befabri/replayvod/server/internal/playbackauth"
)

func TestPlaybackResolutionRetriesTransientFailures(t *testing.T) {
	for _, failure := range []error{playbackauth.ErrUnavailable, playbackauth.ErrChanged, &twitch.AuthError{Status: 429}, &twitch.AuthError{Status: 503}} {
		t.Run(failure.Error(), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				calls := 0
				variant, err := retryPlaybackResolution(context.Background(), func(context.Context) (twitch.SelectedVariant, error) {
					calls++
					if calls < 3 {
						return twitch.SelectedVariant{}, failure
					}
					return twitch.SelectedVariant{Quality: "1440"}, nil
				})
				if err != nil || variant.Quality != "1440" || calls != 3 {
					t.Fatalf("quality=%s calls=%d err=%v", variant.Quality, calls, err)
				}
			})
		})
	}
}
func TestPlaybackResolutionLimitsAndCancellation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		failure error
		calls   int
		elapsed time.Duration
	}{
		{"outage budget", playbackauth.ErrUnavailable, 5, 20 * time.Second},
		{"revoked", playbackauth.ErrRejected, 1, 0},
		{"channel restriction", &twitch.AuthError{Status: http.StatusForbidden}, 1, 0},
		{"retry after beyond deadline", &playbackauth.UnavailableError{RetryAfter: time.Hour}, 1, time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				started := time.Now()
				calls := 0
				_, err := retryPlaybackResolution(context.Background(), func(context.Context) (twitch.SelectedVariant, error) {
					calls++
					return twitch.SelectedVariant{}, tc.failure
				})
				if !errors.Is(err, tc.failure) || !isPlaybackResolutionFailure(err) || calls != tc.calls || time.Since(started) > tc.elapsed {
					t.Fatalf("calls=%d elapsed=%s err=%v", calls, time.Since(started), err)
				}
			})
		})
	}
	for _, cancelBefore := range []bool{false, true} {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if cancelBefore {
				cancel()
			} else {
				go func() { time.Sleep(500 * time.Millisecond); cancel() }()
			}
			calls := 0
			_, err := retryPlaybackResolution(ctx, func(context.Context) (twitch.SelectedVariant, error) {
				calls++
				return twitch.SelectedVariant{}, playbackauth.ErrUnavailable
			})
			if !errors.Is(err, context.Canceled) || isPlaybackResolutionFailure(err) || calls > 1 {
				t.Fatalf("cancellation: calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestPlaybackResolutionDeadlineIncludesRequestsAndHonorsRetryAfter(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		started := time.Now()
		_, err := retryPlaybackResolution(context.Background(), func(ctx context.Context) (twitch.SelectedVariant, error) {
			<-ctx.Done()
			return twitch.SelectedVariant{}, ctx.Err()
		})
		if !errors.Is(err, context.DeadlineExceeded) || !isPlaybackResolutionFailure(err) || time.Since(started) != time.Minute {
			t.Fatalf("request deadline: elapsed=%s err=%v", time.Since(started), err)
		}
	})
	synctest.Test(t, func(t *testing.T) {
		started := time.Now()
		calls := 0
		_, err := retryPlaybackResolution(context.Background(), func(context.Context) (twitch.SelectedVariant, error) {
			calls++
			if calls == 1 {
				return twitch.SelectedVariant{}, &playbackauth.UnavailableError{RetryAfter: 12 * time.Second}
			}
			return twitch.SelectedVariant{Quality: "1440"}, nil
		})
		if err != nil || calls != 2 || time.Since(started) != 12*time.Second {
			t.Fatalf("Retry-After: calls=%d elapsed=%s err=%v", calls, time.Since(started), err)
		}
	})
}
