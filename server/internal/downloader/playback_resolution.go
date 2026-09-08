package downloader

import (
	"context"
	"errors"
	"math/rand/v2"
	"net"
	"net/http"
	"time"

	"github.com/befabri/replayvod/server/internal/downloader/twitch"
	"github.com/befabri/replayvod/server/internal/playbackauth"
)

// playbackResolutionError marks acquisition failures for which already captured
// media can still be finalized. Cancellation deliberately does not use this type.
type playbackResolutionError struct{ cause error }

func (e *playbackResolutionError) Error() string { return e.cause.Error() }
func (e *playbackResolutionError) Unwrap() error { return e.cause }

// retryPlaybackResolution has a separate budget from HLS URL renewals. Each
// attempt reloads the connection, so replacing/disconnecting it takes effect
// during recovery. Bound both attempts and elapsed time, including HTTP calls.
func retryPlaybackResolution(ctx context.Context, resolve func(context.Context) (twitch.SelectedVariant, error)) (twitch.SelectedVariant, error) {
	retryCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return twitch.SelectedVariant{}, err
		}
		variant, err := resolve(retryCtx)
		if err == nil {
			return variant, nil
		}
		if ctx.Err() != nil {
			return twitch.SelectedVariant{}, ctx.Err()
		}
		if attempt == 4 || retryCtx.Err() != nil || !retryablePlaybackResolution(err) {
			return twitch.SelectedVariant{}, &playbackResolutionError{cause: err}
		}
		delay := time.Second << attempt
		delay += time.Duration(rand.Int64N(int64(delay / 4)))
		if errors.Is(err, playbackauth.ErrChanged) {
			delay = 0
		}
		var unavailable *playbackauth.UnavailableError
		if errors.As(err, &unavailable) {
			delay = max(delay, unavailable.RetryAfter)
		}
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-retryCtx.Done():
			timer.Stop()
			if ctx.Err() != nil {
				return twitch.SelectedVariant{}, ctx.Err()
			}
			return twitch.SelectedVariant{}, &playbackResolutionError{cause: err}
		}
	}
}

func retryablePlaybackResolution(err error) bool {
	if errors.Is(err, playbackauth.ErrUnavailable) || errors.Is(err, playbackauth.ErrChanged) {
		return true
	}
	var auth *twitch.AuthError
	if errors.As(err, &auth) {
		return auth.Status == http.StatusTooManyRequests || auth.Status >= 500
	}
	var network net.Error
	return errors.As(err, &network)
}

// playbackCaptureFailure returns the reason persisted on the video row: never
// an upstream response body or a signed CDN URL.
func playbackCaptureFailure(err error) string {
	if errors.Is(err, playbackauth.ErrRejected) {
		return "Twitch session expired or was revoked; saved the captured portion"
	}
	return "Twitch playback could not be renewed; saved the captured portion"
}

func isPlaybackResolutionFailure(err error) bool {
	var failure *playbackResolutionError
	return errors.As(err, &failure)
}
