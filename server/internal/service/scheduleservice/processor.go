package scheduleservice

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/befabri/replayvod/server/internal/downloader"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/twitch"
)

// EventProcessor implements routes/webhook.EventProcessor. It fans a
// stream.online webhook out to every active schedule on the affected
// broadcaster, runs each through MatchSchedule, and kicks off a download
// for every match.
type EventProcessor struct {
	repo       repository.Repository
	dl         *downloader.Service
	log        *slog.Logger
	defaultLng string
}

// NewEventProcessor builds the webhook dispatcher. defaultLanguage is the
// fallback language stored on Video rows when the incoming stream.online
// payload has none (Twitch omits language in the event — we'd normally
// fetch it separately; until then "en" is a safe default).
func NewEventProcessor(repo repository.Repository, dl *downloader.Service, log *slog.Logger) *EventProcessor {
	return &EventProcessor{
		repo:       repo,
		dl:         dl,
		log:        log.With("domain", "schedule"),
		defaultLng: "en",
	}
}

// Process dispatches the decoded notification. Only stream.online drives
// auto-download in Phase 5 — other event types are recorded in the audit
// log by the webhook handler but otherwise ignored here.
func (p *EventProcessor) Process(ctx context.Context, n *twitch.EventSubNotification) error {
	event, ok := n.Event.(twitch.StreamOnlineEvent)
	if !ok {
		// The webhook handler recorded the event in the audit log; no
		// auto-download means no work, not an error.
		return nil
	}
	if event.BroadcasterUserID == "" {
		p.log.Warn("stream.online event missing broadcaster_user_id", "event_id", event.ID)
		return nil
	}
	return p.dispatchStreamOnline(ctx, event)
}

func (p *EventProcessor) dispatchStreamOnline(ctx context.Context, event twitch.StreamOnlineEvent) error {
	schedules, err := p.repo.ListActiveSchedulesForBroadcaster(ctx, event.BroadcasterUserID)
	if err != nil {
		return fmt.Errorf("list schedules for broadcaster: %w", err)
	}
	if len(schedules) == 0 {
		return nil
	}

	// Pull display name from the channels mirror — the event payload has
	// broadcaster_user_name, which is good enough for the Video row's
	// display_name, but lazy-loading from the repo keeps auto-download
	// and manual download consistent.
	channel, err := p.repo.GetChannel(ctx, event.BroadcasterUserID)
	if err != nil {
		p.log.Warn("channel mirror missing for live broadcaster; using event payload",
			"broadcaster_id", event.BroadcasterUserID, "error", err)
		channel = nil
	}

	displayName := event.BroadcasterUserName
	login := event.BroadcasterUserLogin
	if channel != nil {
		displayName = channel.BroadcasterName
		login = channel.BroadcasterLogin
	}

	// Current stream signals: we don't have viewer_count or categories/tags
	// from the stream.online event itself. The matcher has defensive
	// branches for empty filters, so schedules with those toggles disabled
	// still match. Fully-filtered schedules will match on the next poll
	// once we enrich from GetStreams (Phase 6).
	signals := StreamSignals{}

	var anyErr error
	for i := range schedules {
		schedule := &schedules[i]
		filters, err := p.loadFilters(ctx, schedule)
		if err != nil {
			p.log.Error("load schedule filters", "schedule_id", schedule.ID, "error", err)
			anyErr = err
			continue
		}
		if !MatchSchedule(schedule, filters, signals) {
			continue
		}

		// Start the download. The downloader handles its own dedup (refuses
		// a second concurrent download for the same broadcaster), so two
		// matching schedules on the same channel won't spawn two ffmpeg
		// pipelines — only the first records trigger_count.
		jobID, startErr := p.dl.Start(ctx, downloader.Params{
			BroadcasterID:    event.BroadcasterUserID,
			BroadcasterLogin: login,
			DisplayName:      displayName,
			Quality:          schedule.Quality,
			Language:         p.defaultLng,
			ViewerCount:      signals.ViewerCount,
		})
		if startErr != nil {
			p.log.Warn("auto-download start failed",
				"schedule_id", schedule.ID, "broadcaster_id", event.BroadcasterUserID,
				"error", startErr)
			anyErr = startErr
			continue
		}

		// Use WithoutCancel: if the webhook handler's request context is
		// cancelled (client timeout, upstream reset) after Start returns,
		// we still want trigger_count/last_triggered_at recorded so the
		// dashboard reflects that the schedule did fire.
		recordCtx := context.WithoutCancel(ctx)
		if err := p.repo.RecordScheduleTrigger(recordCtx, schedule.ID); err != nil {
			p.log.Error("record schedule trigger", "schedule_id", schedule.ID, "error", err)
		}
		p.log.Info("schedule triggered auto-download",
			"schedule_id", schedule.ID,
			"broadcaster_id", event.BroadcasterUserID,
			"job_id", jobID,
			"quality", schedule.Quality)
	}
	return anyErr
}

func (p *EventProcessor) loadFilters(ctx context.Context, schedule *repository.DownloadSchedule) (ScheduleFilters, error) {
	var f ScheduleFilters
	if schedule.HasCategories {
		cats, err := p.repo.ListScheduleCategories(ctx, schedule.ID)
		if err != nil {
			return f, fmt.Errorf("list categories: %w", err)
		}
		f.Categories = cats
	}
	if schedule.HasTags {
		tags, err := p.repo.ListScheduleTags(ctx, schedule.ID)
		if err != nil {
			return f, fmt.Errorf("list tags: %w", err)
		}
		f.Tags = tags
	}
	return f, nil
}
