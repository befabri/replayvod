package downloader

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/befabri/replayvod/server/internal/downloader/hls"
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

// archiveFailureMessage is the error stored on an archive row: readable text
// for the refusals an operator can act on, and for everything else a summary
// built from the typed error, never its raw text. Upstream response bodies
// and signed URLs never reach the row, whichever layer produced the error.
func archiveFailureMessage(err error) string {
	if errors.Is(err, twitch.ErrPlaybackTokenEmpty) || twitch.IsPermanent(err) || errors.Is(err, hls.ErrPlaylistAuthPermanent) {
		return "Twitch refused playback; a subscriber session may be required (System > Twitch downloads)"
	}
	var auth *twitch.AuthError
	if errors.As(err, &auth) {
		if auth.Status == http.StatusNotFound {
			return "Twitch has no playable VOD with this id; it may have been deleted"
		}
		if auth.Code != "" {
			return fmt.Sprintf("Twitch playback answered HTTP %d (%s)", auth.Status, auth.Code)
		}
		return fmt.Sprintf("Twitch playback answered HTTP %d", auth.Status)
	}
	switch {
	case errors.Is(err, hls.ErrPlaylistGone):
		return "the VOD playlist is gone from Twitch"
	case errors.Is(err, hls.ErrUnsupportedManifest):
		return "Twitch served a playlist this recorder cannot read"
	case errors.Is(err, hls.ErrPlaylistAuth):
		return "Twitch rejected the playlist credentials; they could not be renewed"
	case errors.Is(err, playbackauth.ErrRejected):
		return "Twitch session expired or was revoked"
	case errors.Is(err, playbackauth.ErrUnavailable):
		return "Twitch playback could not be renewed"
	}
	var fetch *hls.FetchError
	if errors.As(err, &fetch) {
		if fetch.Status != 0 {
			return fmt.Sprintf("download interrupted: %s, HTTP %d after %d attempts", fetch.Kind, fetch.Status, fetch.Attempts)
		}
		return fmt.Sprintf("download interrupted: %s after %d attempts", fetch.Kind, fetch.Attempts)
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		host := "Twitch"
		if u, perr := url.Parse(urlErr.URL); perr == nil && u.Host != "" {
			host = u.Host
		}
		return fmt.Sprintf("network error reaching %s during %s", host, strings.ToLower(urlErr.Op))
	}
	return redactUpstream(err.Error())
}

// redactUpstream is the last line for an error no type explains: it drops
// URLs (which may carry signed tokens), anything that follows an HTTP status
// (a body preview), and JSON or HTML fragments, then bounds the length.
func redactUpstream(msg string) string {
	msg = upstreamURL.ReplaceAllString(msg, "[url]")
	msg = upstreamStatusTail.ReplaceAllString(msg, "status ${1}")
	msg = upstreamBlob.ReplaceAllString(msg, "[body]")
	msg = strings.TrimSpace(msg)
	if len(msg) > maxStoredFailureMessage {
		msg = msg[:maxStoredFailureMessage]
	}
	return msg
}

const maxStoredFailureMessage = 300

var (
	upstreamURL        = regexp.MustCompile(`https?://[^\s"'<>]+`)
	upstreamStatusTail = regexp.MustCompile(`status (\d{3}):.*`)
	upstreamBlob       = regexp.MustCompile(`(?s)(\{.*\}|<[a-zA-Z!/][^>]*>.*)`)
)

// sealedCaptureError is the failure of a capture that was sealed and stored
// before the job failed. Its text is the readable reason already persisted on
// the checkpoint; its retryability is the classification made when the typed
// cause was still at hand, so a restart between seal and failure keeps it.
type sealedCaptureError struct {
	msg       string
	retryable bool
}

func (e *sealedCaptureError) Error() string { return e.msg }

func sealedCaptureFailure(resume *ResumeState) error {
	return &sealedCaptureError{msg: resume.CaptureError, retryable: resume.CaptureRetryable}
}
