package schedule

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/befabri/replayvod/server/internal/ptr"

	"github.com/befabri/replayvod/server/internal/downloader"
	"github.com/befabri/replayvod/server/internal/eventbus"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/service/streammeta"
	"github.com/befabri/replayvod/server/internal/twitch"
)

// StreamDownloader accepts schedule recordings and observes manual broadcast continuations.
type StreamDownloader interface {
	ObserveStreamOnline(twitch.Stream)
	ObserveStreamOffline(string)
	Start(ctx context.Context, p downloader.Params) (string, error)
}

var errDownloaderUnavailable = errors.New("schedule: downloader unavailable")

// EventProcessor selects one recording from matching schedules and records every matched trigger.
type EventProcessor struct {
	repo       repository.Repository
	dl         StreamDownloader
	twitch     *twitch.Client
	hydrator   *streammeta.Hydrator
	bus        *eventbus.Buses
	log        *slog.Logger
	defaultLng string
	// storagePaused latches the storage refusal so it is logged once per
	// outage rather than on every re-detected live stream.
	storagePaused atomic.Bool
}

// NewEventProcessor connects stream observations to recording schedules.
// Nil hydrator disables enrichment, and nil bus disables notifications.
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

// Process handles supported stream notifications and ignores unrelated events.
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
	// Category-only updates remain observations even when no title accompanies them.
	if ev.BroadcasterUserID == "" || (ev.Title == "" && ev.CategoryID == "") {
		return nil
	}
	return p.dispatchChannelUpdate(ctx, ev)
}

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

// DispatchStreamOffline closes the latest stream and notifies live-status subscribers.
// Recording acquisition still drains HLS independently.
func (p *EventProcessor) DispatchStreamOffline(ctx context.Context, event twitch.StreamOfflineEvent) error {
	if p.dl != nil {
		p.dl.ObserveStreamOffline(event.BroadcasterUserID)
	}
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
			// Subscribers may have learned of the live channel through polling without a stored stream row.
			p.publishStatus(eventbus.StreamStatusOffline, event.BroadcasterUserID, event.BroadcasterUserLogin, event.BroadcasterUserName, "")
			return nil
		}
		return fmt.Errorf("get last live stream: %w", err)
	}
	if stream.EndedAt != nil {
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

// CloseStaleStream closes a superseded broadcast without announcing the channel offline.
// Missing or already-ended rows are harmless.
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

// DispatchStreamOnline records the observed broadcast and evaluates its recording schedules.
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

	if p.dl != nil {
		observed := twitch.Stream{ID: event.ID, UserID: event.BroadcasterUserID, UserLogin: event.BroadcasterUserLogin, UserName: event.BroadcasterUserName, StartedAt: event.StartedAt}
		if prefetched != nil && prefetched.ID == event.ID {
			observed = *prefetched
		}
		p.dl.ObserveStreamOnline(observed)
	}

	schedules, err := p.repo.ListActiveSchedulesForBroadcaster(ctx, event.BroadcasterUserID)
	if err != nil {
		return fmt.Errorf("list schedules for broadcaster: %w", err)
	}
	if len(schedules) == 0 {
		return nil
	}

	// Pausing schedules must leave live-status notifications and manual continuations active.
	paused, err := readSchedulesPaused(ctx, p.repo)
	if err != nil {
		return fmt.Errorf("read schedules paused flag: %w", err)
	}
	if paused {
		return nil
	}

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

	// Missing enrichment prevents filtered matches while unfiltered schedules remain eligible.
	signals, language, streamTitle, categoryID, categoryName := p.hydrate(ctx, event.BroadcasterUserID, prefetched)

	// Compare every match before admission so a lower-quality first match cannot occupy the slot.
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
		StreamID:                  ptr.StringOrNil(event.ID),
		StreamStartedAt:           event.StartedAt,
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
		if errors.Is(startErr, downloader.ErrStorageUnavailable) {
			if p.storagePaused.CompareAndSwap(false, true) {
				p.log.Warn("auto-download paused until storage is attached",
					"schedule_id", winner.ID, "broadcaster_id", event.BroadcasterUserID, "error", startErr)
			}
			return startErr
		}
		p.storageRecovered()
		if errors.Is(startErr, downloader.ErrBusy) {
			// Returning an error for an already-satisfied intent would make the live poller redispatch
			// every tick.
			return nil
		}
		p.log.Warn("auto-download start failed",
			"schedule_id", winner.ID, "broadcaster_id", event.BroadcasterUserID,
			"error", startErr)
		return startErr
	}
	p.storageRecovered()

	// Record every matching schedule, including those that did not determine the winning quality.
	recordCtx := context.WithoutCancel(ctx)
	for _, s := range matches {
		if err := p.repo.RecordScheduleTrigger(recordCtx, s.ID); err != nil {
			p.log.Error("record schedule trigger", "schedule_id", s.ID, "error", err)
		}
	}

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

// qualityRank prefers the least restrictive limit among matching schedules.
var qualityRank = map[string]int{
	repository.QualityLow:    1,
	repository.QualityMedium: 2,
	repository.QualityHigh:   3,
	repository.Quality1440:   4,
	repository.QualityBest:   5,
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

// storageRecovered re-arms the once-per-outage storage warning.
func (p *EventProcessor) storageRecovered() {
	if p.storagePaused.CompareAndSwap(true, false) {
		p.log.Info("auto-download resumed; storage is attached again")
	}
}
