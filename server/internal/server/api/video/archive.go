package video

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/befabri/replayvod/server/internal/downloader"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/server/api/channel"
	"github.com/befabri/replayvod/server/internal/twitch"
)

type archiveRepo interface {
	GetChannel(ctx context.Context, broadcasterID string) (*repository.Channel, error)
	GetStream(ctx context.Context, id string) (*repository.Stream, error)
	GetVideoByJobID(ctx context.Context, jobID string) (*repository.Video, error)
	GetOpenVideoByTwitchVideoID(ctx context.Context, twitchVideoID string) (*repository.Video, error)
	ListOpenVideosByTwitchVideoIDs(ctx context.Context, twitchVideoIDs []string) ([]repository.Video, error)
	ListOpenVideosByStreamIDs(ctx context.Context, streamIDs []string) ([]repository.Video, error)
	ListArchiveQueue(ctx context.Context) ([]repository.Video, error)
	ListRecentArchiveFailures(ctx context.Context, since time.Time, limit int) ([]repository.Video, error)
}

type archiveRunner interface {
	EnqueueVOD(ctx context.Context, p downloader.Params) (string, error)
	DequeueArchive(ctx context.Context, videoID int64) error
	RetryArchive(ctx context.Context, videoID int64) error
	CancelArchiveRetry(ctx context.Context, videoID int64) error
	PumpArchiveQueue(ctx context.Context)
}

type helixVideos interface {
	GetUsers(ctx context.Context, params *twitch.GetUsersParams) ([]twitch.User, error)
	GetVideos(ctx context.Context, params *twitch.GetVideosParams) ([]twitch.Video, twitch.Pagination, error)
	GetStreams(ctx context.Context, params *twitch.GetStreamsParams) ([]twitch.Stream, twitch.Pagination, error)
}

type channelSyncer interface {
	SyncFromTwitch(ctx context.Context, input channel.SyncInput) (*repository.Channel, error)
}

// ErrChannelNotFound is returned by ListChannelVODs when Twitch knows no user
// by the given login.
var ErrChannelNotFound = errors.New("archive: channel not found on Twitch")

// RecentFailureWindow bounds the failed archives the queue page lists.
const RecentFailureWindow = 7 * 24 * time.Hour

const recentFailureLimit = 100

// ArchiveService is the control plane for VOD back-archiving: it turns pasted
// links or a channel's catalogue into queued archive jobs. Twitch is the source
// of truth for what a VOD is (title, owner, air date); the downloader owns the
// queue. Unknown channels are synced on the fly so any VOD can be archived, not
// only ones from channels already in the library.
type ArchiveService struct {
	repo       archiveRepo
	downloader archiveRunner
	twitch     helixVideos
	channels   channelSyncer
	log        *slog.Logger
	now        func() time.Time
}

func NewArchive(repo repository.Repository, dl *downloader.Service, tc *twitch.Client, channels *channel.Service, log *slog.Logger) *ArchiveService {
	return &ArchiveService{
		repo:       repo,
		downloader: dl,
		twitch:     tc,
		channels:   channels,
		log:        log.With("domain", "archive"),
		now:        time.Now,
	}
}

// vodPathPattern matches the VOD id in the URL forms Twitch has used:
// /videos/<id>, /<login>/video/<id> and the legacy /<login>/v/<id>.
var vodPathPattern = regexp.MustCompile(`(?i)^/(?:videos|[A-Za-z0-9_]+/(?:video|v))/(\d+)/?$`)

var loginPattern = regexp.MustCompile(`^[A-Za-z0-9_]{2,25}$`)

func isTwitchHost(host string) bool {
	host = strings.ToLower(host)
	return host == "twitch.tv" || strings.HasSuffix(host, ".twitch.tv")
}

// ParseVODID extracts a Twitch VOD id from a bare id or any twitch.tv VOD
// url, ignoring query and fragment (a "?t=1h2m" timestamp is common).
func ParseVODID(input string) (string, bool) {
	s := strings.TrimSpace(input)
	if s == "" {
		return "", false
	}
	if isDigits(s) {
		return s, true
	}
	u, ok := parseTwitchURL(s)
	if !ok {
		return "", false
	}
	m := vodPathPattern.FindStringSubmatch(u.Path)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// ParseChannelLogin accepts a login or a twitch.tv channel url (with or
// without a trailing section such as /videos) and returns the lowercase
// login. VOD urls are not channels and are rejected.
func ParseChannelLogin(input string) (string, bool) {
	s := strings.TrimSpace(input)
	s = strings.TrimPrefix(s, "@")
	if s == "" {
		return "", false
	}
	if loginPattern.MatchString(s) {
		return strings.ToLower(s), true
	}
	u, ok := parseTwitchURL(s)
	if !ok {
		return "", false
	}
	segments := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(segments) == 0 || segments[0] == "" || segments[0] == "videos" || segments[0] == "directory" {
		return "", false
	}
	if len(segments) >= 2 && (segments[1] == "video" || segments[1] == "v") {
		return "", false
	}
	if !loginPattern.MatchString(segments[0]) {
		return "", false
	}
	return strings.ToLower(segments[0]), true
}

func parseTwitchURL(s string) (*url.URL, bool) {
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil || !isTwitchHost(u.Hostname()) {
		return nil, false
	}
	return u, true
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

var durationPattern = regexp.MustCompile(`^(?:(\d+)h)?(?:(\d+)m)?(?:(\d+)s)?$`)

// ParseTwitchDuration converts Helix's "3h20m5s" style duration to seconds.
// Unparseable input yields 0 rather than an error: the field is display
// only and Twitch has changed its shape before.
func ParseTwitchDuration(s string) int {
	m := durationPattern.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil || s == "" {
		return 0
	}
	h, _ := strconv.Atoi(m[1])
	mi, _ := strconv.Atoi(m[2])
	sec, _ := strconv.Atoi(m[3])
	return h*3600 + mi*60 + sec
}

// HeldReason says why the library already holds a VOD: an archive of it, or
// the live recording of the same broadcast.
type HeldReason string

const (
	HeldByArchive       HeldReason = "archive"
	HeldByLiveRecording HeldReason = "live_recording"
)

// TwitchVOD is a Twitch VOD as the dashboard needs it, plus whether the
// library already holds (or is fetching) it and whether it can be archived
// at all right now.
type TwitchVOD struct {
	ID              string
	StreamID        string
	Title           string
	URL             string
	Type            string
	Viewable        string
	CreatedAt       time.Time
	DurationSeconds int
	ThumbnailURL    string
	ViewCount       int
	Language        string
	// Live is set while the broadcast the VOD belongs to is still on air:
	// its playlist has no end yet, so it cannot be archived until it ends.
	Live bool
	// Held is the open library row for this VOD, nil when none, and
	// HeldReason says whether that row is an archive or a live recording.
	Held       *repository.Video
	HeldReason HeldReason
}

type ChannelVODs struct {
	Channel    twitch.User
	VODs       []TwitchVOD
	NextCursor string
}

// ListChannelVODs resolves a channel login and returns one page of its VODs,
// newest first, marking the ones the library already holds (as an archive or
// a complete live recording) and the one still being broadcast.
func (s *ArchiveService) ListChannelVODs(ctx context.Context, userID, login, cursor string, limit int) (*ChannelVODs, error) {
	ctx = twitch.WithUserID(ctx, userID)
	users, err := s.twitch.GetUsers(ctx, &twitch.GetUsersParams{Login: []string{login}})
	if err != nil {
		return nil, fmt.Errorf("lookup channel: %w", err)
	}
	if len(users) == 0 {
		return nil, ErrChannelNotFound
	}
	user := users[0]
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	videos, page, err := s.twitch.GetVideos(ctx, &twitch.GetVideosParams{
		UserID: user.ID,
		Type:   "all",
		Sort:   "time",
		First:  strconv.Itoa(limit),
		After:  cursor,
	})
	if err != nil {
		return nil, fmt.Errorf("list channel vods: %w", err)
	}
	live, err := s.liveStreams(ctx, []string{user.ID})
	if err != nil {
		return nil, err
	}
	holds, _, err := s.libraryHolds(ctx, videos)
	if err != nil {
		return nil, err
	}
	out := &ChannelVODs{Channel: user, VODs: make([]TwitchVOD, 0, len(videos)), NextCursor: page.Cursor}
	for _, v := range videos {
		vod := toTwitchVOD(v)
		vod.Live = v.StreamID != "" && live[v.StreamID]
		if h, ok := holds[v.ID]; ok {
			vod.Held, vod.HeldReason = h.video, h.reason
		}
		out.VODs = append(out.VODs, vod)
	}
	return out, nil
}

// liveStreams returns the ids of the streams currently on air for the given
// broadcasters. Helix takes up to a hundred ids per call.
func (s *ArchiveService) liveStreams(ctx context.Context, userIDs []string) (map[string]bool, error) {
	out := map[string]bool{}
	const batch = 100
	for start := 0; start < len(userIDs); start += batch {
		chunk := userIDs[start:min(start+batch, len(userIDs))]
		streams, _, err := s.twitch.GetStreams(ctx, &twitch.GetStreamsParams{UserID: chunk, First: len(chunk)})
		if err != nil {
			return nil, fmt.Errorf("lookup live streams: %w", err)
		}
		for _, st := range streams {
			if st.Type == "live" {
				out[st.ID] = true
			}
		}
	}
	return out, nil
}

type libraryHold struct {
	video  *repository.Video
	reason HeldReason
}

// libraryHolds maps VOD ids to the open library row that already holds them:
// an open archive of the VOD, else a live recording of the same broadcast
// that captured it whole. A truncated live recording does not hold the VOD,
// since archiving it would add what the recording missed.
// libraryHolds reports, per VOD id, the library row that keeps it from being
// archived. Truncated live recordings hold nothing (the full VOD is worth
// having) but are returned in the second map so the result can say why the
// archive was accepted despite a recording of the same broadcast.
func (s *ArchiveService) libraryHolds(ctx context.Context, videos []twitch.Video) (map[string]libraryHold, map[string]*repository.Video, error) {
	ids := make([]string, 0, len(videos))
	streamIDs := make([]string, 0, len(videos))
	for _, v := range videos {
		ids = append(ids, v.ID)
		if v.StreamID != "" {
			streamIDs = append(streamIDs, v.StreamID)
		}
	}
	out := make(map[string]libraryHold, len(videos))
	truncated := make(map[string]*repository.Video)
	archives, err := s.openArchivesByVOD(ctx, ids)
	if err != nil {
		return nil, nil, err
	}
	for id, row := range archives {
		out[id] = libraryHold{video: row, reason: HeldByArchive}
	}
	recordings, err := s.repo.ListOpenVideosByStreamIDs(ctx, streamIDs)
	if err != nil {
		return nil, nil, fmt.Errorf("list live recordings: %w", err)
	}
	byStream := make(map[string]*repository.Video, len(recordings))
	truncatedByStream := make(map[string]*repository.Video)
	for i := range recordings {
		if recordings[i].StreamID == nil {
			continue
		}
		if recordings[i].Truncated {
			truncatedByStream[*recordings[i].StreamID] = &recordings[i]
			continue
		}
		byStream[*recordings[i].StreamID] = &recordings[i]
	}
	for _, v := range videos {
		if _, held := out[v.ID]; held || v.StreamID == "" {
			continue
		}
		if row, ok := byStream[v.StreamID]; ok {
			out[v.ID] = libraryHold{video: row, reason: HeldByLiveRecording}
			continue
		}
		if row, ok := truncatedByStream[v.StreamID]; ok {
			truncated[v.ID] = row
		}
	}
	return out, truncated, nil
}

func (s *ArchiveService) openArchivesByVOD(ctx context.Context, ids []string) (map[string]*repository.Video, error) {
	rows, err := s.repo.ListOpenVideosByTwitchVideoIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("list archived vods: %w", err)
	}
	out := make(map[string]*repository.Video, len(rows))
	for i := range rows {
		if rows[i].TwitchVideoID != nil {
			out[*rows[i].TwitchVideoID] = &rows[i]
		}
	}
	return out, nil
}

func toTwitchVOD(v twitch.Video) TwitchVOD {
	return TwitchVOD{
		ID:              v.ID,
		StreamID:        v.StreamID,
		Title:           v.Title,
		URL:             v.URL,
		Type:            v.Type,
		Viewable:        v.Viewable,
		CreatedAt:       v.CreatedAt,
		DurationSeconds: ParseTwitchDuration(v.Duration),
		ThumbnailURL:    twitch.VideoThumbnailURL(v.ThumbnailURL, 320, 180),
		ViewCount:       v.ViewCount,
		Language:        v.Language,
	}
}

// EnqueueStatus is the per-input outcome of Enqueue.
type EnqueueStatus string

const (
	EnqueueQueued   EnqueueStatus = "queued"
	EnqueueExists   EnqueueStatus = "exists"
	EnqueueNotFound EnqueueStatus = "not_found"
	EnqueueInvalid  EnqueueStatus = "invalid"
	// EnqueueLive means the VOD's broadcast is still on air; the VOD can be
	// archived once the stream ends.
	EnqueueLive EnqueueStatus = "live"
	// EnqueuePrivate means Twitch lists the VOD as private, which no playback
	// token unlocks.
	EnqueuePrivate EnqueueStatus = "private"
	EnqueueError   EnqueueStatus = "error"
)

type EnqueueItem struct {
	Input   string
	VODID   string
	Status  EnqueueStatus
	Title   string
	VideoID int64
	JobID   string
	Message string
}

type EnqueueInput struct {
	// Inputs are VOD urls or ids. Duplicates within one call collapse onto
	// the first occurrence.
	Inputs        []string
	RecordingType string
	Quality       string
	ForceH264     bool
	UserID        string
}

// Enqueue resolves every input against Twitch and queues each VOD that is
// not already held. It never fails the whole batch for one bad input: each
// item reports its own status so the dashboard can show exactly what
// happened per link.
func (s *ArchiveService) Enqueue(ctx context.Context, input EnqueueInput) ([]EnqueueItem, error) {
	ctx = twitch.WithUserID(ctx, input.UserID)
	settings := repository.NormalizeRecordingSettings(repository.RecordingSettingsInput{
		RecordingType: input.RecordingType,
		Quality:       input.Quality,
		ForceH264:     input.ForceH264,
	})

	items := make([]EnqueueItem, len(input.Inputs))
	seen := map[string]int{}
	var lookup []string
	for i, raw := range input.Inputs {
		items[i] = EnqueueItem{Input: raw}
		id, ok := ParseVODID(raw)
		if !ok {
			items[i].Status = EnqueueInvalid
			items[i].Message = "not a Twitch VOD link or id"
			continue
		}
		items[i].VODID = id
		if first, dup := seen[id]; dup {
			items[i].Status = EnqueueInvalid
			items[i].Message = fmt.Sprintf("same VOD as line %d", first+1)
			continue
		}
		seen[id] = i
		lookup = append(lookup, id)
	}

	// Already-held VODs are answered from the library without a Helix call.
	held, err := s.openArchivesByVOD(ctx, lookup)
	if err != nil {
		return nil, err
	}
	var fetch []string
	for _, id := range lookup {
		if existing, ok := held[id]; ok {
			items[seen[id]].markExists(existing)
			continue
		}
		fetch = append(fetch, id)
	}

	found, err := twitch.LookupVideosByID(ctx, s.twitch, fetch)
	if err != nil {
		return nil, err
	}
	videos := make([]twitch.Video, 0, len(found))
	users := map[string]bool{}
	for _, id := range fetch {
		v, ok := found[id]
		if !ok {
			items[seen[id]].Status = EnqueueNotFound
			items[seen[id]].Message = "Twitch has no VOD with this id (expired or deleted)"
			continue
		}
		videos = append(videos, v)
		users[v.UserID] = true
	}
	live, err := s.liveStreams(ctx, sortedKeys(users))
	if err != nil {
		return nil, err
	}
	holds, truncated, err := s.libraryHolds(ctx, videos)
	if err != nil {
		return nil, err
	}
	for _, v := range videos {
		item := &items[seen[v.ID]]
		item.Title = v.Title
		switch {
		case v.Viewable == "private":
			item.Status = EnqueuePrivate
			item.Message = "this VOD is private on Twitch"
		case v.StreamID != "" && live[v.StreamID]:
			item.Status = EnqueueLive
			item.Message = "still streaming, archive it once the stream ends"
		default:
			if h, ok := holds[v.ID]; ok && h.reason == HeldByLiveRecording {
				item.markExists(h.video)
				item.Message = "recorded live"
				continue
			}
			s.enqueueOne(ctx, item, v, settings, input.UserID)
			if _, cut := truncated[v.ID]; cut && item.Status == EnqueueQueued {
				item.Message = "the live recording of this broadcast was cut short; archiving the full VOD"
			}
		}
	}
	pumpCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	s.downloader.PumpArchiveQueue(pumpCtx)
	return items, nil
}

func (it *EnqueueItem) markExists(existing *repository.Video) {
	it.Status = EnqueueExists
	it.Title = existing.Title
	it.VideoID = existing.ID
	it.JobID = existing.JobID
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func (s *ArchiveService) enqueueOne(ctx context.Context, item *EnqueueItem, v twitch.Video, settings repository.RecordingSettings, userID string) {
	ch, err := s.ensureChannel(ctx, v.UserID, userID)
	if err != nil {
		s.log.Warn("archive: sync channel", "broadcaster_id", v.UserID, "vod_id", v.ID, "error", err)
		item.Status = EnqueueError
		item.Message = "could not sync the channel from Twitch"
		return
	}
	aired := v.CreatedAt
	streamID, err := s.linkedStream(ctx, v.StreamID)
	if err != nil {
		s.log.Warn("archive: look up broadcast", "stream_id", v.StreamID, "vod_id", v.ID, "error", err)
		item.Status = EnqueueError
		item.Message = "could not check the library for this broadcast"
		return
	}
	jobID, err := s.downloader.EnqueueVOD(ctx, downloader.Params{
		BroadcasterID:    ch.BroadcasterID,
		BroadcasterLogin: ch.BroadcasterLogin,
		DisplayName:      ch.BroadcasterName,
		Title:            v.Title,
		Quality:          settings.Quality,
		Language:         v.Language,
		StreamID:         streamID,
		RecordingType:    settings.RecordingType,
		ForceH264:        settings.ForceH264,
		VODID:            v.ID,
		BroadcastAt:      &aired,
		PosterURL:        twitch.VideoThumbnailURL(v.ThumbnailURL, 640, 360),
	})
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrDuplicate):
			item.Status = EnqueueExists
			if existing, gerr := s.repo.GetOpenVideoByTwitchVideoID(ctx, v.ID); gerr == nil {
				item.VideoID = existing.ID
				item.JobID = existing.JobID
			}
		case errors.Is(err, downloader.ErrShuttingDown):
			item.Status = EnqueueError
			item.Message = "the server is restarting; try again in a moment"
		default:
			s.log.Error("archive: enqueue vod", "vod_id", v.ID, "error", err)
			item.Status = EnqueueError
			item.Message = "could not queue the VOD"
		}
		return
	}
	item.Status = EnqueueQueued
	item.JobID = jobID
	if row, err := s.repo.GetVideoByJobID(ctx, jobID); err == nil {
		item.VideoID = row.ID
	}
}

// linkedStream returns the VOD's stream id when the library knows that
// broadcast, so the archive joins its recorded metadata. videos.stream_id
// references streams, and most back-archived VODs predate the install, so an
// unknown broadcast leaves the archive unlinked rather than failing the insert.
func (s *ArchiveService) linkedStream(ctx context.Context, streamID string) (*string, error) {
	if streamID == "" {
		return nil, nil
	}
	if _, err := s.repo.GetStream(ctx, streamID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &streamID, nil
}

func (s *ArchiveService) ensureChannel(ctx context.Context, broadcasterID, userID string) (*repository.Channel, error) {
	ch, err := s.repo.GetChannel(ctx, broadcasterID)
	if err == nil {
		return ch, nil
	}
	if !errors.Is(err, repository.ErrNotFound) {
		return nil, err
	}
	return s.channels.SyncFromTwitch(ctx, channel.SyncInput{BroadcasterID: broadcasterID, UserID: userID})
}

// ArchiveQueue is what the Archive page shows: what is waiting or running,
// oldest first, and what failed recently, newest first.
type ArchiveQueue struct {
	Queue    []repository.Video
	Failures []repository.Video
}

// Queue lists queued and running archives plus the failures of the last
// RecentFailureWindow, so a failed archive does not vanish from the page.
func (s *ArchiveService) Queue(ctx context.Context) (*ArchiveQueue, error) {
	queue, err := s.repo.ListArchiveQueue(ctx)
	if err != nil {
		return nil, err
	}
	failures, err := s.repo.ListRecentArchiveFailures(ctx, s.now().UTC().Add(-RecentFailureWindow), recentFailureLimit)
	if err != nil {
		return nil, err
	}
	return &ArchiveQueue{Queue: queue, Failures: failures}, nil
}

// Dequeue removes an archive that has not started. downloader.ErrBusy means
// it is already downloading and must be cancelled instead.
func (s *ArchiveService) Dequeue(ctx context.Context, videoID int64) error {
	return s.downloader.DequeueArchive(ctx, videoID)
}

// Retry queues a new attempt of a failed archive right away.
func (s *ArchiveService) Retry(ctx context.Context, videoID int64) error {
	return s.downloader.RetryArchive(ctx, videoID)
}

// CancelRetry drops the scheduled retry of a failed archive.
func (s *ArchiveService) CancelRetry(ctx context.Context, videoID int64) error {
	return s.downloader.CancelArchiveRetry(ctx, videoID)
}
