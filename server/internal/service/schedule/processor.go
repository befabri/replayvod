package schedule

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/befabri/replayvod/server/internal/downloader"
	"github.com/befabri/replayvod/server/internal/eventbus"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/service/streammeta"
	"github.com/befabri/replayvod/server/internal/twitch"
)

// EventProcessor implements routes/webhook.EventProcessor. On a
// stream.online webhook it enriches the event with full stream data
// from Helix, runs every active schedule through Match, picks the
// highest-quality winner, and kicks off exactly one download. All
// matching schedules get trigger_count bumped so the dashboard shows
// every schedule that fired, even non-winners.
type EventProcessor struct {
	repo       repository.Repository
	dl         *downloader.Service
	twitch     *twitch.Client
	hydrator   *streammeta.Hydrator
	bus        *eventbus.Buses
	log        *slog.Logger
	defaultLng string
}

// NewEventProcessor builds the webhook dispatcher. twitchClient is
// used to enrich stream.online events with viewer_count / category /
// tags via GET /helix/streams. Pass nil to skip enrichment (tests, or
// a degraded mode where we want schedule matching on raw webhook data
// only — filtered schedules then never match, see matcher invariant).
// bus is optional: when set, stream.live fires on every stream.online
// dispatch so SSE subscribers see channels going live in real time.
func NewEventProcessor(repo repository.Repository, dl *downloader.Service, tc *twitch.Client, hydrator *streammeta.Hydrator, bus *eventbus.Buses, log *slog.Logger) *EventProcessor {
	return &EventProcessor{
		repo:       repo,
		dl:         dl,
		twitch:     tc,
		hydrator:   hydrator,
		bus:        bus,
		log:        log.With("domain", "schedule"),
		defaultLng: "en",
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
	case twitch.ChannelUpdateEvent:
		// Skip only when nothing useful is attached. Gating on
		// `ev.Title == ""` alone would drop category-only changes
		// (streamer flips game but keeps the title) — and those
		// are the exact events /dashboard/categories/$id depends
		// on to list every category the recording passed through.
		if ev.BroadcasterUserID == "" || (ev.Title == "" && ev.CategoryID == "") {
			return nil
		}
		return p.dispatchChannelUpdate(ctx, ev)
	default:
		return nil
	}
}

// dispatchChannelUpdate writes mid-stream title AND category changes
// into video_titles + video_categories via the hydrator. Only runs
// for broadcasters the downloader subscribed to on record start
// (webhook mode); for any other broadcaster the lookup returns no
// active recording and the call is a no-op. WithoutCancel so a
// handler-timeout mid-write doesn't strand a partial link.
func (p *EventProcessor) dispatchChannelUpdate(ctx context.Context, ev twitch.ChannelUpdateEvent) error {
	if p.hydrator == nil {
		return nil
	}
	persistCtx := context.WithoutCancel(ctx)
	if err := p.hydrator.RecordChannelUpdate(persistCtx, ev.BroadcasterUserID, streammeta.ChannelUpdateMeta{
		Title:        ev.Title,
		CategoryID:   ev.CategoryID,
		CategoryName: ev.CategoryName,
	}); err != nil {
		return fmt.Errorf("record channel.update: %w", err)
	}
	return nil
}

// dispatchStreamOffline stamps ended_at on the most recent active
// stream for the broadcaster. The live downloader (if running) keeps
// its own end-detection, so this doesn't cancel in-flight downloads —
// it just closes the stream row for reporting. Also publishes a
// StreamStatusEvent so SSE subscribers watching the delta feed can
// drop this broadcaster from their live-set without polling.
func (p *EventProcessor) dispatchStreamOffline(ctx context.Context, event twitch.StreamOfflineEvent) error {
	// WithoutCancel so webhook timeouts don't leave ended_at unset.
	persistCtx := context.WithoutCancel(ctx)

	stream, err := p.repo.GetLastLiveStream(persistCtx, event.BroadcasterUserID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			p.log.Info("stream.offline with no active stream row; ignoring",
				"broadcaster_id", event.BroadcasterUserID)
			// Still fire the SSE delta — frontend may have learned of
			// this channel being live via Helix poll (stream.liveIds)
			// and needs to drop it from the Set even if we never saw
			// the online event.
			p.publishStatus(eventbus.StreamStatusOffline, event.BroadcasterUserID, event.BroadcasterUserLogin, event.BroadcasterUserName, "")
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
	p.publishStatus(eventbus.StreamStatusOffline, event.BroadcasterUserID, event.BroadcasterUserLogin, event.BroadcasterUserName, stream.ID)
	return nil
}

// publishStatus fans a stream.online/offline transition out to the
// SSE delta topic. Non-blocking (the bus drops when subscribers fall
// behind); safe to call with bus == nil (tests, degraded mode).
func (p *EventProcessor) publishStatus(kind eventbus.StreamStatusKind, broadcasterID, login, displayName, streamID string) {
	if p.bus == nil {
		return
	}
	p.bus.StreamStatus.Publish(eventbus.StreamStatusEvent{
		Kind:             kind,
		BroadcasterID:    broadcasterID,
		BroadcasterLogin: login,
		DisplayName:      displayName,
		StreamID:         streamID,
		At:               time.Now().UTC(),
	})
}

func (p *EventProcessor) dispatchStreamOnline(ctx context.Context, event twitch.StreamOnlineEvent) error {
	// Fan out the raw online signal first — StreamStatus is the delta
	// feed for the dashboard live-indicator, independent of whether we
	// end up triggering a schedule. Subscribers need to add this
	// broadcaster to their live Set regardless.
	p.publishStatus(eventbus.StreamStatusOnline, event.BroadcasterUserID, event.BroadcasterUserLogin, event.BroadcasterUserName, event.ID)

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

	// Enrich from Helix per spec: streammeta.Hydrate retries GetStreams
	// a few times because stream.online races ahead of the live listing
	// by a few hundred ms. On failure we proceed with empty signals —
	// filtered schedules won't match (that's the invariant), but
	// unfiltered ones still fire. Persist uses context.WithoutCancel so
	// a client drop mid-handler doesn't strand a partial write.
	signals, language, streamTitle, categoryID, categoryName := p.hydrate(ctx, event.BroadcasterUserID)

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
		if Match(schedule, filters, signals) {
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
		Title:            streamTitle,
		CategoryID:       categoryID,
		CategoryName:     categoryName,
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

	// Fan out to SSE subscribers. Non-blocking; the bus drops when a
	// subscriber falls behind (see eventbus docs).
	if p.bus != nil {
		p.bus.StreamLive.Publish(eventbus.StreamLiveEvent{
			BroadcasterID:    event.BroadcasterUserID,
			BroadcasterLogin: login,
			DisplayName:      displayName,
			StartedAt:        time.Now().UTC(),
			MatchedSchedules: len(matches),
			JobID:            jobID,
		})
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

// hydrate delegates to streammeta.Hydrator and pulls the pieces the
// schedule path cares about out of the resulting Snapshot. Returns
// (signals, language, title, categoryID, categoryName). The
// category fields are threaded through downloader.Params so the
// post-CreateVideo LinkInitialVideoMetadata call has everything it
// needs for the opening video_categories link.
func (p *EventProcessor) hydrate(ctx context.Context, broadcasterID string) (StreamSignals, string, string, string, string) {
	if p.hydrator == nil {
		return StreamSignals{}, "", "", "", ""
	}
	// WithoutCancel so a client drop mid-handler doesn't strand a
	// partial write. The hydrator's internal retries + upserts all
	// run under this context.
	persistCtx := context.WithoutCancel(ctx)
	snap := p.hydrator.Hydrate(persistCtx, broadcasterID)
	if snap == nil {
		return StreamSignals{}, "", "", "", ""
	}
	return StreamSignals{
		ViewerCount: snap.ViewerCount,
		CategoryIDs: snap.CategoryIDs,
		TagIDs:      snap.TagIDs,
	}, snap.Language, snap.Title, snap.GameID, snap.GameName
}

func (p *EventProcessor) loadFilters(ctx context.Context, schedule *repository.DownloadSchedule) (Filters, error) {
	var f Filters
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
