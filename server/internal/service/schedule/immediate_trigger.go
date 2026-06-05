package schedule

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/befabri/replayvod/server/internal/twitch"
)

// LiveTrigger is the post-write hook used by the schedule CRUD service. It is
// intentionally narrower than EventProcessor so schedule writes do not know
// whether live state came from EventSub, polling, or an explicit Twitch probe.
type LiveTrigger interface {
	TriggerScheduleIfLive(ctx context.Context, scheduleID int64, broadcasterID string) error
}

type liveStreamFetcher interface {
	GetStreams(ctx context.Context, params *twitch.GetStreamsParams) ([]twitch.Stream, twitch.Pagination, error)
}

type targetedOnlineDispatcher interface {
	DispatchStreamOnlineFromStreamForSchedule(ctx context.Context, stream twitch.Stream, scheduleID int64) error
}

// ImmediateTrigger probes Helix after a schedule write. If the broadcaster is
// live, it hands the prefetched stream to the normal schedule processor with a
// required schedule ID, so a non-matching new rule cannot accidentally trigger a
// different user's existing matching rule.
type ImmediateTrigger struct {
	streams    liveStreamFetcher
	dispatcher targetedOnlineDispatcher
	log        *slog.Logger
}

func NewImmediateTrigger(streams liveStreamFetcher, dispatcher targetedOnlineDispatcher, log *slog.Logger) *ImmediateTrigger {
	if log == nil {
		log = slog.Default()
	}
	return &ImmediateTrigger{
		streams:    streams,
		dispatcher: dispatcher,
		log:        log.With("domain", "schedule"),
	}
}

func (t *ImmediateTrigger) TriggerScheduleIfLive(ctx context.Context, scheduleID int64, broadcasterID string) error {
	if t == nil || t.streams == nil || t.dispatcher == nil || scheduleID <= 0 || broadcasterID == "" {
		return nil
	}
	streams, _, err := t.streams.GetStreams(ctx, &twitch.GetStreamsParams{
		UserID: []string{broadcasterID},
		First:  1,
	})
	if err != nil {
		return fmt.Errorf("probe live stream: %w", err)
	}
	if len(streams) == 0 {
		return nil
	}
	if err := t.dispatcher.DispatchStreamOnlineFromStreamForSchedule(ctx, streams[0], scheduleID); err != nil {
		return fmt.Errorf("dispatch live schedule: %w", err)
	}
	t.log.Debug("immediate schedule live probe completed",
		"schedule_id", scheduleID,
		"broadcaster_id", broadcasterID,
		"stream_id", streams[0].ID)
	return nil
}
