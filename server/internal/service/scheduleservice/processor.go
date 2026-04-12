package scheduleservice

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/befabri/replayvod/server/internal/downloader"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/twitch"
)

// EventProcessor implements routes/webhook.EventProcessor. On a
// stream.online webhook it enriches the event with full stream data
// from Helix, runs every active schedule through MatchSchedule, picks
// the highest-quality winner, and kicks off exactly one download. All
// matching schedules get trigger_count bumped so the dashboard shows
// every schedule that fired, even non-winners.
type EventProcessor struct {
	repo       repository.Repository
	dl         *downloader.Service
	twitch     *twitch.Client
	log        *slog.Logger
	defaultLng string
	// fetchRetries / fetchRetryDelay configure the GetStreams retry
	// loop for stream enrichment. stream.online fires before Helix
	// necessarily reflects the live state, so per the spec we retry a
	// few times before giving up and processing without signals.
	fetchRetries    int
	fetchRetryDelay time.Duration
}

// NewEventProcessor builds the webhook dispatcher. twitchClient is
// used to enrich stream.online events with viewer_count / category /
// tags via GET /helix/streams. Pass nil to skip enrichment (tests, or
// a degraded mode where we want schedule matching on raw webhook data
// only — filtered schedules then never match, see matcher invariant).
func NewEventProcessor(repo repository.Repository, dl *downloader.Service, tc *twitch.Client, log *slog.Logger) *EventProcessor {
	return &EventProcessor{
		repo:            repo,
		dl:              dl,
		twitch:          tc,
		log:             log.With("domain", "schedule"),
		defaultLng:      "en",
		fetchRetries:    3,
		fetchRetryDelay: time.Second,
	}
}

// Process dispatches the decoded notification to the per-event handler.
// Events we don't act on (e.g. channel.update v1, automod, etc.) are
// audit-logged by the webhook handler; here we return nil so the
// webhook returns 204 cleanly.
func (p *EventProcessor) Process(ctx context.Context, n *twitch.EventSubNotification) error {
	switch ev := n.Event.(type) {
	case twitch.StreamOnlineEvent:
		if ev.BroadcasterUserID == "" {
			p.log.Warn("stream.online event missing broadcaster_user_id", "event_id", ev.ID)
			return nil
		}
		return p.dispatchStreamOnline(ctx, ev)
	case twitch.StreamOfflineEvent:
		if ev.BroadcasterUserID == "" {
			p.log.Warn("stream.offline event missing broadcaster_user_id")
			return nil
		}
		return p.dispatchStreamOffline(ctx, ev)
	default:
		return nil
	}
}

// dispatchStreamOffline stamps ended_at on the most recent active
// stream for the broadcaster. The live downloader (if running) keeps
// its own end-detection, so this doesn't cancel in-flight downloads —
// it just closes the stream row for reporting.
func (p *EventProcessor) dispatchStreamOffline(ctx context.Context, event twitch.StreamOfflineEvent) error {
	// WithoutCancel so webhook timeouts don't leave ended_at unset.
	persistCtx := context.WithoutCancel(ctx)

	stream, err := p.repo.GetLastLiveStream(persistCtx, event.BroadcasterUserID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			p.log.Info("stream.offline with no active stream row; ignoring",
				"broadcaster_id", event.BroadcasterUserID)
			return nil
		}
		return fmt.Errorf("get last live stream: %w", err)
	}
	if stream.EndedAt != nil {
		// Already ended — idempotent, happens when Twitch retries the
		// same offline event or we processed one earlier.
		return nil
	}
	if err := p.repo.EndStream(persistCtx, stream.ID, time.Now().UTC()); err != nil {
		return fmt.Errorf("end stream: %w", err)
	}
	p.log.Info("stream ended",
		"stream_id", stream.ID,
		"broadcaster_id", event.BroadcasterUserID)
	return nil
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

	// Enrich from Helix per spec: retry GetStreams a few times because
	// stream.online races ahead of the live listing by a few hundred ms.
	// On failure we proceed with empty signals — filtered schedules won't
	// match (that's the invariant), but unfiltered ones still fire.
	signals, language := p.enrichStreamSignals(ctx, event)
	if language != "" {
		// Override the Video row's language hint when we actually have one.
		// The struct assignment below picks this up in Start params.
		_ = language
	}

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

	dlLanguage := p.defaultLng
	if language != "" {
		dlLanguage = language
	}
	jobID, startErr := p.dl.Start(ctx, downloader.Params{
		BroadcasterID:    event.BroadcasterUserID,
		BroadcasterLogin: login,
		DisplayName:      displayName,
		Quality:          winner.Quality,
		Language:         dlLanguage,
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

// enrichStreamSignals fetches the live stream from Helix (with retry),
// persists the stream row + category/tag links, and returns the
// StreamSignals for the matcher plus the stream language (used as the
// Video row's Language hint). On any failure we return empty signals
// and defaultLng — filtered schedules won't match, which is the
// documented degradation per spec § stream.online.
func (p *EventProcessor) enrichStreamSignals(ctx context.Context, event twitch.StreamOnlineEvent) (StreamSignals, string) {
	if p.twitch == nil {
		return StreamSignals{}, ""
	}
	stream, err := p.fetchStreamWithRetry(ctx, event.BroadcasterUserID)
	if err != nil {
		p.log.Warn("stream enrichment failed; processing with empty signals",
			"broadcaster_id", event.BroadcasterUserID, "error", err)
		return StreamSignals{}, ""
	}

	// Persist the stream row. ctx.WithoutCancel so a client drop
	// mid-handler doesn't strand a partial write.
	persistCtx := context.WithoutCancel(ctx)
	isMature := stream.IsMature
	viewerCount := int64(stream.ViewerCount)
	thumb := stream.ThumbnailURL
	streamPtr, err := p.repo.UpsertStream(persistCtx, &repository.StreamInput{
		ID:            stream.ID,
		BroadcasterID: event.BroadcasterUserID,
		Type:          stream.Type,
		Language:      stream.Language,
		ThumbnailURL:  stringOrNil(thumb),
		ViewerCount:   viewerCount,
		IsMature:      &isMature,
		StartedAt:     stream.StartedAt,
	})
	if err != nil {
		p.log.Warn("upsert stream row", "stream_id", stream.ID, "error", err)
	}

	// Link category (upsert first so the category row exists).
	var catIDs []string
	if stream.GameID != "" {
		if _, err := p.repo.UpsertCategory(persistCtx, &repository.Category{
			ID:   stream.GameID,
			Name: stream.GameName,
		}); err != nil {
			p.log.Warn("upsert stream category", "game_id", stream.GameID, "error", err)
		} else if streamPtr != nil {
			if err := p.repo.LinkStreamCategory(persistCtx, streamPtr.ID, stream.GameID); err != nil {
				p.log.Warn("link stream category", "stream_id", streamPtr.ID, "error", err)
			}
			catIDs = append(catIDs, stream.GameID)
		}
	}

	// Link tags. Helix returns tag TEXT, not IDs — upsert each name and
	// collect the resulting int64 IDs so the matcher can compare against
	// download_schedule_tags rows (which are FK to tags.id).
	var tagIDs []int64
	for _, name := range stream.Tags {
		if name == "" {
			continue
		}
		tag, err := p.repo.UpsertTag(persistCtx, name)
		if err != nil {
			p.log.Warn("upsert tag", "name", name, "error", err)
			continue
		}
		tagIDs = append(tagIDs, tag.ID)
		if streamPtr != nil {
			if err := p.repo.LinkStreamTag(persistCtx, streamPtr.ID, tag.ID); err != nil {
				p.log.Warn("link stream tag", "tag_id", tag.ID, "error", err)
			}
		}
	}

	return StreamSignals{
		ViewerCount: viewerCount,
		CategoryIDs: catIDs,
		TagIDs:      tagIDs,
	}, stream.Language
}

// fetchStreamWithRetry wraps GetStreams with the spec-mandated retry
// loop. Returns ErrNotFound-shaped error if the stream still isn't
// visible after fetchRetries attempts — caller degrades to empty
// signals.
func (p *EventProcessor) fetchStreamWithRetry(ctx context.Context, broadcasterID string) (*twitch.Stream, error) {
	var lastErr error
	for attempt := 0; attempt < p.fetchRetries; attempt++ {
		if attempt > 0 {
			timer := time.NewTimer(p.fetchRetryDelay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
		}
		streams, _, err := p.twitch.GetStreams(ctx, &twitch.GetStreamsParams{
			UserID: []string{broadcasterID},
		})
		if err != nil {
			// Rate-limit error or network issue — retry.
			lastErr = err
			continue
		}
		if len(streams) > 0 {
			return &streams[0], nil
		}
		lastErr = errors.New("twitch returned no streams for broadcaster")
	}
	if lastErr == nil {
		lastErr = errors.New("stream enrichment retries exhausted")
	}
	return nil, lastErr
}

func stringOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
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
