package downloader

import (
	"context"
	"errors"
	"fmt"
	"time"

	provider "github.com/befabri/replayvod/server/internal/twitch"
)

var errBroadcastEnded = errors.New("selected broadcast is no longer live")
var errBroadcastLookup = errors.New("broadcast identity lookup unavailable")

func (s *Service) currentBroadcast(ctx context.Context, broadcasterID, streamID string) (*provider.Stream, error) {
	if s.observe == nil {
		return nil, errBroadcastLookup
	}
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	stream, err := s.observe(probeCtx, broadcasterID)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errBroadcastLookup, err)
	}
	if stream == nil || streamID == "" || stream.ID != streamID {
		return nil, errBroadcastEnded
	}
	return stream, nil
}

func (s *Service) verifyPlaybackIdentity(ctx context.Context, p Params) error {
	if p.isVOD() || p.StreamID == nil || *p.StreamID == "" {
		return nil
	}
	_, err := s.currentBroadcast(ctx, p.BroadcasterID, *p.StreamID)
	return err
}

// recoverCaptureIdentity prevents recovery from appending a different broadcast to saved media.
// Unknown identities remain unknown, while saved output still proceeds to finalization.
func (s *Service) recoverCaptureIdentity(ctx context.Context, d *download, p Params) error {
	if !d.recovered || d.vod || d.captureIdentityVerified || shouldSkipSegmentFetch(d.resume) {
		return nil
	}
	known := p.StreamID != nil && *p.StreamID != ""
	if known {
		_, err := s.currentBroadcast(ctx, p.BroadcasterID, *p.StreamID)
		if err != nil && !errors.Is(err, errBroadcastEnded) {
			return err
		}
		if err == nil {
			d.captureIdentityVerified = true
			return nil
		}
	}
	// Only an unidentified, empty admission may acquire the current channel.
	// A known obsolete identity must never receive a newer broadcast's bytes.
	if !known && !d.resume.PartStarted && d.resume.CurrentPartIndex == 1 && d.resume.PartBytes == 0 {
		d.captureIdentityVerified = true
		return nil
	}
	d.resume.SetStage(StagePrepareInput)
	d.resume.ClearPendingSplit()
	d.resume.HadWindowRoll = true
	at := s.now()
	d.resume.CaptureStoppedAt = &at
	return nil
}

// liveCaptureEnded requires provider confirmation because a vanished playlist may be a
// rendition change.
func (s *Service) liveCaptureEnded(ctx context.Context, d *download, p Params, cause error) bool {
	if !d.vod && errors.Is(cause, errBroadcastEnded) {
		return true
	}
	if d.vod || s.observe == nil || ctx.Err() != nil || (!isSplitSignal(cause) && !isPlaybackResolutionFailure(cause)) {
		return false
	}
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	stream, err := s.observe(probeCtx, p.BroadcasterID)
	if err != nil {
		return false
	}
	if stream == nil {
		return true
	}
	if p.StreamID != nil && *p.StreamID != "" {
		return stream.ID != *p.StreamID
	}
	return stream.StartedAt.After(d.startedAt)
}
