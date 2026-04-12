package scheduleservice

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/befabri/replayvod/server/internal/downloader"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/twitch"
)

// EventProcessor implements routes/webhook.EventProcessor. On a
// stream.online webhook it runs every active schedule on the affected
// broadcaster through MatchSchedule, picks the highest-quality winner
// from the matching set, and kicks off exactly one download. Other
// matching schedules still get trigger_count bumped so operators can
// see they fired, even though only the winner's quality is used.
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

	// First pass: collect matching schedules. We need them all to pick
	// the highest-quality one per spec (eventsub.md § stream.online). The
	// webhook processor must trigger exactly ONE download regardless of
	// how many schedules match — relying on the downloader's busy-check
	// would work today but races on cold-start (first-caller wins might
	// be the lowest quality).
	var matches []*repository.DownloadSchedule
	var anyErr error
	for i := range schedules {
		schedule := &schedules[i]
		filters, err := p.loadFilters(ctx, schedule)
		if err != nil {
			p.log.Error("load schedule filters", "schedule_id", schedule.ID, "error", err)
			anyErr = err
			continue
		}
		if MatchSchedule(schedule, filters, signals) {
			matches = append(matches, schedule)
		}
	}
	if len(matches) == 0 {
		return anyErr
	}

	// Pick highest-quality match deterministically. Ties break by
	// schedule ID so repeated firings of the same event converge on the
	// same winner.
	winner := highestQuality(matches)

	jobID, startErr := p.dl.Start(ctx, downloader.Params{
		BroadcasterID:    event.BroadcasterUserID,
		BroadcasterLogin: login,
		DisplayName:      displayName,
		Quality:          winner.Quality,
		Language:         p.defaultLng,
		ViewerCount:      signals.ViewerCount,
	})
	if startErr != nil {
		p.log.Warn("auto-download start failed",
			"schedule_id", winner.ID, "broadcaster_id", event.BroadcasterUserID,
			"error", startErr)
		return startErr
	}

	// Bump trigger_count / last_triggered_at on every matching schedule —
	// operators need to see "this schedule fired" in the dashboard even
	// if it wasn't the quality winner. context.WithoutCancel so a client
	// timeout mid-record doesn't desync the counters.
	recordCtx := context.WithoutCancel(ctx)
	for _, s := range matches {
		if err := p.repo.RecordScheduleTrigger(recordCtx, s.ID); err != nil {
			p.log.Error("record schedule trigger", "schedule_id", s.ID, "error", err)
		}
	}
	p.log.Info("schedule triggered auto-download",
		"winner_schedule_id", winner.ID,
		"match_count", len(matches),
		"broadcaster_id", event.BroadcasterUserID,
		"job_id", jobID,
		"quality", winner.Quality)
	return anyErr
}

// qualityRank orders the three legal values so HIGH wins ties over
// MEDIUM and LOW. Using a map keeps this a pure function of the string;
// future quality additions only need an entry here.
var qualityRank = map[string]int{
	repository.QualityLow:    1,
	repository.QualityMedium: 2,
	repository.QualityHigh:   3,
}

// highestQuality returns the schedule with the highest quality rank.
// Ties break by lowest ID — deterministic across repeated invocations
// so retry / replay of the same event always picks the same winner.
func highestQuality(matches []*repository.DownloadSchedule) *repository.DownloadSchedule {
	winner := matches[0]
	winRank := qualityRank[winner.Quality]
	for _, s := range matches[1:] {
		r := qualityRank[s.Quality]
		if r > winRank || (r == winRank && s.ID < winner.ID) {
			winner = s
			winRank = r
		}
	}
	return winner
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
