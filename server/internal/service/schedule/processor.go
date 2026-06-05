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

// StreamDownloader is the slice of the downloader the processor needs to start
// a recording. Narrowing it to an interface keeps the dispatch path unit-
// testable with a fake that returns downloader.ErrBusy. *downloader.Service
// satisfies it.
type StreamDownloader interface {
	Start(ctx context.Context, p downloader.Params) (string, error)
}

var errDownloaderUnavailable = errors.New("schedule: downloader unavailable")

// EventProcessor implements routes/webhook.EventProcessor. On a
// stream.online webhook it enriches the event with full stream data
// from Helix, runs every active schedule through Match, picks the
// highest-quality winner, and kicks off exactly one download. All
// matching schedules get trigger_count bumped so the dashboard shows
// every schedule that fired, even non-winners.
type EventProcessor struct {
	repo       repository.Repository
	dl         StreamDownloader
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
func NewEventProcessor(repo repository.Repository, dl StreamDownloader, tc *twitch.Client, hydrator *streammeta.Hydrator, bus *eventbus.Buses, log *slog.Logger) *EventProcessor {
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
		return p.processStreamOnlineEvent(ctx, ev)
	case *twitch.StreamOnlineEvent:
		if ev == nil {
			return nil
		}
		return p.processStreamOnlineEvent(ctx, *ev)
	case twitch.StreamOfflineEvent:
		return p.processStreamOfflineEvent(ctx, ev)
	case *twitch.StreamOfflineEvent:
		if ev == nil {
			return nil
		}
		return p.processStreamOfflineEvent(ctx, *ev)
	case twitch.ChannelUpdateEvent:
		return p.processChannelUpdateEvent(ctx, ev)
	case *twitch.ChannelUpdateEvent:
		if ev == nil {
			return nil
		}
		return p.processChannelUpdateEvent(ctx, *ev)
	default:
		return nil
	}
}

func (p *EventProcessor) processStreamOnlineEvent(ctx context.Context, ev twitch.StreamOnlineEvent) error {
	return p.DispatchStreamOnline(ctx, ev)
}

func (p *EventProcessor) processStreamOfflineEvent(ctx context.Context, ev twitch.StreamOfflineEvent) error {
	return p.DispatchStreamOffline(ctx, ev)
}

func (p *EventProcessor) processChannelUpdateEvent(ctx context.Context, ev twitch.ChannelUpdateEvent) error {
	// Skip only when nothing useful is attached. Gating on `ev.Title == ""`
	// alone would drop category-only changes (streamer flips game but keeps the
	// title), and those are the exact events /dashboard/categories/$id depends
	// on to list every category the recording passed through.
	if ev.BroadcasterUserID == "" || (ev.Title == "" && ev.CategoryID == "") {
		return nil
	}
	return p.dispatchChannelUpdate(ctx, ev)
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

// DispatchStreamOffline stamps ended_at on the most recent active
// stream for the broadcaster. The live downloader (if running) keeps
// its own end-detection, so this doesn't cancel in-flight downloads —
// it just closes the stream row for reporting. Also publishes a
// StreamStatusEvent so SSE subscribers watching the delta feed can
// drop this broadcaster from their live-set without polling.
func (p *EventProcessor) DispatchStreamOffline(ctx context.Context, event twitch.StreamOfflineEvent) error {
	if event.BroadcasterUserID == "" {
		p.log.Warn("stream.offline event missing broadcaster_user_id")
		return nil
	}

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

// CloseStaleStream stamps ended_at on the broadcaster's currently-open stream
// row WITHOUT publishing an SSE offline. The live poller calls it when a
// broadcaster stays live under a new stream ID (a rerun, or an offline/online
// blip that spanned a poll interval): the superseded streams row must be closed
// so it doesn't leak as perpetually live, but the channel never actually left
// the live set, so emitting offline would make the dashboard live-dot flicker
// off and back on. Idempotent: a missing or already-ended row is a no-op.
func (p *EventProcessor) CloseStaleStream(ctx context.Context, broadcasterID string) error {
	if broadcasterID == "" {
		return nil
	}
	persistCtx := context.WithoutCancel(ctx)
	stream, err := p.repo.GetLastLiveStream(persistCtx, broadcasterID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("get last live stream: %w", err)
	}
	if stream.EndedAt != nil {
		return nil
	}
	if err := p.repo.EndStream(persistCtx, stream.ID, time.Now().UTC()); err != nil {
		return fmt.Errorf("end stale stream: %w", err)
	}
	p.log.Info("closed superseded stream", "stream_id", stream.ID, "broadcaster_id", broadcasterID)
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

func (p *EventProcessor) DispatchStreamOnline(ctx context.Context, event twitch.StreamOnlineEvent) error {
	return p.dispatchStreamOnline(ctx, event, nil, nil)
}

// DispatchStreamOnlineFromStream is the poll-mode entry point. The caller has
// already fetched the live stream from Helix, so enrichment reuses it instead
// of issuing a second GetStreams. The webhook entry (DispatchStreamOnline) only
// has the EventSub payload, which carries no title/category/viewer data, so it
// must re-fetch.
func (p *EventProcessor) DispatchStreamOnlineFromStream(ctx context.Context, stream twitch.Stream) error {
	return p.dispatchStreamOnline(ctx, streamOnlineEventFromStream(stream), &stream, nil)
}

// DispatchStreamOnlineFromStreamForSchedule is the schedule-write entry point:
// the broadcaster is already live, but we should only start recording if the
// schedule that was just created/updated is one of the matching schedules. Once
// that gate passes, the normal winner selection still considers every matching
// active schedule for the broadcaster so the one-download-per-stream rule stays
// identical to real stream.online handling.
func (p *EventProcessor) DispatchStreamOnlineFromStreamForSchedule(ctx context.Context, stream twitch.Stream, scheduleID int64) error {
	if scheduleID <= 0 {
		return nil
	}
	return p.dispatchStreamOnline(ctx, streamOnlineEventFromStream(stream), &stream, &scheduleID)
}

func streamOnlineEventFromStream(s twitch.Stream) twitch.StreamOnlineEvent {
	return twitch.StreamOnlineEvent{
		ID:                   s.ID,
		BroadcasterUserID:    s.UserID,
		BroadcasterUserLogin: s.UserLogin,
		BroadcasterUserName:  s.UserName,
		Type:                 s.Type,
		StartedAt:            s.StartedAt,
	}
}

// dispatchStreamOnline is the shared path. prefetched is the already-polled live
// stream in poll/immediate mode, or nil in webhook mode (where hydrate
// re-fetches from Helix). requiredScheduleID gates the immediate schedule-write
// path: nil means normal stream.online semantics, non-nil means "do nothing
// unless this schedule matched the current stream".
func (p *EventProcessor) dispatchStreamOnline(ctx context.Context, event twitch.StreamOnlineEvent, prefetched *twitch.Stream, requiredScheduleID *int64) error {
	if event.BroadcasterUserID == "" {
		p.log.Warn("stream.online event missing broadcaster_user_id", "event_id", event.ID)
		return nil
	}

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

	// Global pause kill switch: when the owner has paused all schedules, skip
	// auto-downloads entirely. Individual schedule is_disabled flags are left
	// untouched, so resuming restores each schedule's prior behavior. The live
	// status fan-out above still runs so the dashboard indicator stays correct.
	// Read after the no-schedule early return so broadcasters without schedules
	// (the common case) don't pay a settings lookup on every stream.online.
	paused, err := readSchedulesPaused(ctx, p.repo)
	if err != nil {
		return fmt.Errorf("read schedules paused flag: %w", err)
	}
	if paused {
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
	signals, language, streamTitle, categoryID, categoryName := p.hydrate(ctx, event.BroadcasterUserID, prefetched)

	// First pass: collect matching schedules. We need them all to pick
	// the highest-quality one per spec (eventsub.md § stream.online). The
	// webhook processor must trigger exactly ONE download regardless of
	// how many schedules match — relying on the downloader's busy-check
	// would work today but races on cold-start (first-caller wins might
	// be the lowest quality).
	var matches []*repository.DownloadSchedule
	var filterErrs []error
	requiredMatched := requiredScheduleID == nil
	for i := range schedules {
		schedule := &schedules[i]
		filters, err := p.loadFilters(ctx, schedule)
		if err != nil {
			p.log.Error("load schedule filters", "schedule_id", schedule.ID, "error", err)
			filterErrs = append(filterErrs, fmt.Errorf("schedule %d filters: %w", schedule.ID, err))
			continue
		}
		if Match(schedule, filters, signals) {
			matches = append(matches, schedule)
			if requiredScheduleID != nil && schedule.ID == *requiredScheduleID {
				requiredMatched = true
			}
		}
	}
	if !requiredMatched {
		return errors.Join(filterErrs...)
	}
	if len(matches) == 0 {
		return errors.Join(filterErrs...)
	}

	// Pick the best match deterministically. Video schedules win over audio
	// when both match because video contains the audio track too; within the
	// same mode, higher quality wins and ties break by schedule ID.
	winner := bestSchedule(matches)
	winnerID := winner.ID
	retention := effectiveRetentionPolicy(matches)

	if p.dl == nil {
		p.log.Warn("auto-download skipped; downloader unavailable",
			"schedule_id", winner.ID, "broadcaster_id", event.BroadcasterUserID)
		return errDownloaderUnavailable
	}

	dlLanguage := p.defaultLng
	if language != "" {
		dlLanguage = language
	}
	settings := repository.NormalizeRecordingSettings(repository.RecordingSettingsInput{
		RecordingType: winner.RecordingType,
		Quality:       winner.Quality,
		ForceH264:     winner.ForceH264,
	})
	jobID, startErr := p.dl.Start(ctx, downloader.Params{
		BroadcasterID:             event.BroadcasterUserID,
		BroadcasterLogin:          login,
		DisplayName:               displayName,
		Title:                     streamTitle,
		CategoryID:                categoryID,
		CategoryName:              categoryName,
		RecordingType:             settings.RecordingType,
		Quality:                   settings.Quality,
		ForceH264:                 settings.ForceH264,
		Language:                  dlLanguage,
		ViewerCount:               signals.ViewerCount,
		TriggerScheduleID:         &winnerID,
		RetentionSourceScheduleID: retention.SourceScheduleID,
		RetentionWindowHours:      retention.WindowHours,
	})
	if startErr != nil {
		if errors.Is(startErr, downloader.ErrBusy) {
			// The broadcaster already has an active download, so the online
			// intent is already satisfied. Treat as an idempotent no-op:
			// repeated signals (a poll re-detect, a Twitch retry, or a manual
			// record already in flight) must not surface as an error, or the
			// live poller would re-dispatch this broadcaster every tick.
			return nil
		}
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
		"recording_type", settings.RecordingType,
		"force_h264", settings.ForceH264,
		"quality", settings.Quality)
	return nil
}

// qualityRank orders the three legal values so HIGH wins ties over
// MEDIUM and LOW. Using a map keeps this a pure function of the string;
// future quality additions only need an entry here.
var qualityRank = map[string]int{
	repository.QualityLow:    1,
	repository.QualityMedium: 2,
	repository.QualityHigh:   3,
}

func recordingTypeRank(recordingType string) int {
	if repository.NormalizeRecordingType(recordingType) == repository.RecordingTypeAudio {
		return 1
	}
	return 2
}

type retentionPolicySnapshot struct {
	SourceScheduleID *int64
	WindowHours      *int64
}

// effectiveRetentionPolicy snapshots the delete policy that applies to this
// recording from the schedules that actually matched it. The shortest enabled
// window wins; ties pick the lower schedule ID so retries converge. Manual
// recordings do not call this path, and schedule recordings with no matched
// delete policy return nil fields.
func effectiveRetentionPolicy(matches []*repository.DownloadSchedule) retentionPolicySnapshot {
	var out retentionPolicySnapshot
	for _, s := range matches {
		if s == nil || !s.IsDeleteRediff || s.IsDisabled || s.TimeBeforeDelete == nil {
			continue
		}
		hours := *s.TimeBeforeDelete
		if hours <= 0 || hours > repository.MaxRetentionWindowHours {
			continue
		}
		if out.WindowHours == nil || hours < *out.WindowHours || (hours == *out.WindowHours && s.ID < *out.SourceScheduleID) {
			id := s.ID
			window := hours
			out.SourceScheduleID = &id
			out.WindowHours = &window
		}
	}
	return out
}

// bestSchedule returns the schedule that should own the single auto-download.
// Video wins over audio, then higher quality wins within the same recording
// mode. Ties break by lowest ID so retry / replay of the same event always
// picks the same winner.
func bestSchedule(matches []*repository.DownloadSchedule) *repository.DownloadSchedule {
	winner := matches[0]
	winModeRank := recordingTypeRank(winner.RecordingType)
	winQualityRank := qualityRank[winner.Quality]
	for _, s := range matches[1:] {
		modeRank := recordingTypeRank(s.RecordingType)
		candidateQualityRank := qualityRank[s.Quality]
		if modeRank > winModeRank ||
			(modeRank == winModeRank && candidateQualityRank > winQualityRank) ||
			(modeRank == winModeRank && candidateQualityRank == winQualityRank && s.ID < winner.ID) {
			winner = s
			winModeRank = modeRank
			winQualityRank = candidateQualityRank
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
func (p *EventProcessor) hydrate(ctx context.Context, broadcasterID string, prefetched *twitch.Stream) (StreamSignals, string, string, string, string) {
	if p.hydrator == nil {
		return StreamSignals{}, "", "", "", ""
	}
	// WithoutCancel so a client drop mid-handler doesn't strand a
	// partial write. The hydrator's internal retries + upserts all
	// run under this context.
	persistCtx := context.WithoutCancel(ctx)
	// Poll mode already polled the live stream, so enrich from it directly;
	// the webhook path has no stream data and must fetch from Helix.
	var snap *streammeta.Snapshot
	if prefetched != nil {
		snap = p.hydrator.HydrateFromStream(persistCtx, prefetched)
	} else {
		snap = p.hydrator.Hydrate(persistCtx, broadcasterID)
	}
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
