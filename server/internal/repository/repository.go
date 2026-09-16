package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// ErrNotFound means the requested row does not exist, independent of the backend.
var ErrNotFound = errors.New("repository: not found")

// ErrDuplicate indicates a uniqueness constraint violation.
var ErrDuplicate = errors.New("repository: duplicate")

// ErrNoMetadataObserved means an observation supplied neither title nor category.
var ErrNoMetadataObserved = errors.New("repository: no metadata observed")

// ErrStaleExecution means a recording or task writer does not own the execution.
var ErrStaleExecution = errors.New("repository: stale recording execution")

// ErrNoTransaction means a row-locking method was called outside WithTx,
// where the lock would be released before the caller could rely on it.
var ErrNoTransaction = errors.New("repository: requires a transaction")

// Repository provides the database operations shared by both adapters.
type Repository interface {
	// Ping checks whether the database can answer a query.
	Ping(ctx context.Context) error

	// WithTx commits the callback's writes together or rolls back on error,
	// cancellation, or panic. The callback must use only the supplied
	// repository, which expires when it returns. Compound repository methods
	// join that transaction; calling WithTx again inside it is unsupported.
	WithTx(ctx context.Context, fn func(Repository) error) error

	// GetVideoForUpdate locks the video until WithTx finishes and returns
	// ErrNoTransaction when called outside one.
	GetVideoForUpdate(ctx context.Context, id int64) (*Video, error)
	SetJobExecution(ctx context.Context, jobID, executionID string, acceptsMetadata bool) error
	StopJobMetadata(ctx context.Context, jobID, executionID string) error
	RequestJobStop(ctx context.Context, id string) error
	CheckpointAttempt(ctx context.Context, jobID, executionID string, state json.RawMessage) error
	ListRecoveryJobs(ctx context.Context, afterID string, limit int) ([]Job, error)
	ListStoppedJobs(ctx context.Context, afterID string, limit int) ([]Job, error)
	ListQueuedArchiveJobs(ctx context.Context, after time.Time, afterID int64, limit int) ([]ArchiveQueueCandidate, error)
	ClaimTask(ctx context.Context, name, executionID string) error
	SettleTask(ctx context.Context, name, executionID, status string, durationMs int64, message string) error
	ResetTaskAvailability(ctx context.Context) error
	RecoverInterruptedTasks(ctx context.Context) error
	CreateRecordingIntent(ctx context.Context, intent RecordingIntent) error
	GetRecordingIntent(ctx context.Context, id string) (*RecordingIntent, error)
	// LockRecordingIntent locks the intent until WithTx finishes and returns
	// ErrNoTransaction when called outside one.
	LockRecordingIntent(ctx context.Context, id string) (*RecordingIntent, error)
	GetRecordingIntentByJob(ctx context.Context, jobID string) (*RecordingIntent, error)
	ListRecoverableRecordingIntents(ctx context.Context, afterID string, limit int) ([]RecordingIntent, error)
	// SetRecordingIntentWaiting parks the intent until the deadline and
	// ActivateRecordingIntent admits the successor observed on or before it.
	// Backends keep the deadline to at least the millisecond, so an
	// observation a millisecond past it is stale everywhere.
	SetRecordingIntentWaiting(ctx context.Context, id, jobID string, until time.Time) error
	ActivateRecordingIntent(ctx context.Context, id, previousJobID, nextJobID, streamID string, observedAt time.Time) error
	CloseRecordingIntent(ctx context.Context, id, status string) error
	RequestRecordingIntentStop(ctx context.Context, id string) error
	LinkRecordingIntentVideo(ctx context.Context, intentID string, videoID int64, streamID *string) error
	ListRecordingIntentJobs(ctx context.Context, intentID, afterID string, limit int) ([]Job, error)
	ListRelatedRecordings(ctx context.Context, videoID int64) ([]RelatedRecording, error)
	GetMediaPublication(ctx context.Context, key string) (*MediaPublication, error)
	BeginMediaPublication(ctx context.Context, input MediaPublication) (*MediaPublication, error)
	ConfirmMediaPublication(ctx context.Context, key, digest string) error
	RequestMediaPublicationDelete(ctx context.Context, key string) error
	DeleteMediaPublication(ctx context.Context, key string) error
	ListMediaPublications(ctx context.Context, after string, limit int) ([]MediaPublication, error)
	ListRecordingPublications(ctx context.Context, videoID int64, after string, limit int) ([]MediaPublication, error)
	GetVideoWaveformKey(ctx context.Context, videoID int64) (string, error)
	SetVideoWaveformKey(ctx context.Context, videoID int64, key string) error
	DeleteVideoWaveformKey(ctx context.Context, videoID int64) error

	GetUser(ctx context.Context, id string) (*User, error)
	// GetUserForUpdate locks the user or its absence until WithTx finishes.
	// It returns ErrNotFound for a missing user while retaining the lock.
	// Call it on the transaction repository before other reads to avoid a
	// stale SQLite snapshot.
	GetUserForUpdate(ctx context.Context, id string) (*User, error)
	GetUserByLogin(ctx context.Context, login string) (*User, error)
	// UpsertUser refreshes profile fields and sets Role only on insert.
	UpsertUser(ctx context.Context, u *User) (*User, error)
	ListUsers(ctx context.Context) ([]User, error)
	ListUserDisplayNames(ctx context.Context, ids []string) (map[string]string, error)
	UpdateUserRole(ctx context.Context, id string, role string) error

	CreateSession(ctx context.Context, s *Session) error
	GetSession(ctx context.Context, hashedID string) (*Session, error)
	UpdateSessionTokens(ctx context.Context, hashedID string, encryptedTokens []byte) error
	UpdateSessionActivity(ctx context.Context, hashedID string) error
	DeleteSession(ctx context.Context, hashedID string) error
	DeleteUserSessions(ctx context.Context, userID string) error
	DeleteExpiredSessions(ctx context.Context) error
	ListUserSessions(ctx context.Context, userID string) ([]SessionInfo, error)

	// GetTwitchPlaybackSession returns the shared recorder credential, separate from
	// application login sessions.
	GetTwitchPlaybackSession(ctx context.Context) (*TwitchPlaybackSession, error)
	SaveTwitchPlaybackSession(ctx context.Context, session *TwitchPlaybackSession) error
	// UpdateTwitchPlaybackSessionValidation compares the encrypted token so stale
	// validation cannot invalidate a replacement; only Save can restore it.
	UpdateTwitchPlaybackSessionValidation(ctx context.Context, session *TwitchPlaybackSession) error
	DeleteTwitchPlaybackSession(ctx context.Context) error

	GetLatestAppToken(ctx context.Context) (*AppAccessToken, error)
	CreateAppToken(ctx context.Context, token string, expiresAt time.Time) (*AppAccessToken, error)
	DeleteExpiredAppTokens(ctx context.Context) error

	IsWhitelisted(ctx context.Context, twitchUserID string) (bool, error)
	AddToWhitelist(ctx context.Context, twitchUserID string) error
	RemoveFromWhitelist(ctx context.Context, twitchUserID string) error
	ListWhitelist(ctx context.Context) ([]WhitelistEntry, error)

	CreateInvite(ctx context.Context, input *InviteInput) (*Invite, error)
	GetInviteByTokenHash(ctx context.Context, tokenHash string) (*Invite, error)
	// RedeemInvite consumes a pending, unexpired invitation and reports
	// whether it matched.
	RedeemInvite(ctx context.Context, tokenHash, redeemedBy string) (bool, error)
	ListInvites(ctx context.Context) ([]Invite, error)
	// DeleteInvite revokes an unredeemed invitation and reports whether it
	// matched.
	DeleteInvite(ctx context.Context, id int64) (bool, error)
	// RotateInviteToken replaces the token hash of a pending, unexpired
	// invitation or returns ErrNotFound.
	RotateInviteToken(ctx context.Context, id int64, tokenHash string) (*Invite, error)

	GetChannel(ctx context.Context, broadcasterID string) (*Channel, error)
	GetChannelByLogin(ctx context.Context, login string) (*Channel, error)
	UpsertChannel(ctx context.Context, c *Channel) (*Channel, error)
	ListChannels(ctx context.Context) ([]Channel, error)
	ListChannelsPage(ctx context.Context, limit int, sort string, filter string, userID string, cursor *ChannelPageCursor) (*ChannelPage, error)
	// ListChannelsByIDs returns existing channels for the supplied IDs; empty input
	// returns no rows.
	ListChannelsByIDs(ctx context.Context, ids []string) ([]Channel, error)
	// SearchChannels matches login and name, ranking exact, prefix, then substring
	// matches; empty query returns up to limit channels alphabetically.
	SearchChannels(ctx context.Context, query string, limit int) ([]Channel, error)
	GetChannelUserState(ctx context.Context, userID string, broadcasterID string) (*ChannelUserState, error)
	ListChannelUserStatesForChannels(ctx context.Context, userID string, broadcasterIDs []string) ([]ChannelUserState, error)
	SetChannelFavorite(ctx context.Context, userID string, broadcasterID string, favorite bool) (*ChannelUserState, error)
	DeleteChannel(ctx context.Context, broadcasterID string) error

	UpsertUserFollow(ctx context.Context, f *UserFollow) error
	ListUserFollows(ctx context.Context, userID string) ([]Channel, error)
	UnfollowChannel(ctx context.Context, userID, broadcasterID string) error

	GetCategory(ctx context.Context, id string) (*Category, error)
	GetCategoryDetail(ctx context.Context, id string) (*CategoryDetail, error)
	GetCategoryByName(ctx context.Context, name string) (*Category, error)
	UpsertCategory(ctx context.Context, c *Category) (*Category, error)
	UpsertCategories(ctx context.Context, categories []Category) ([]Category, error)
	ListCategories(ctx context.Context) ([]Category, error)
	// ListCategoriesWithVideos returns categories linked to non-deleted recordings.
	ListCategoriesWithVideos(ctx context.Context) ([]Category, error)
	// ListCategoriesWithVideosPage returns the browse/library categories with
	// cursor pagination and a small sort allowlist.
	ListCategoriesWithVideosPage(ctx context.Context, limit int, sort string, cursor *CategoryPageCursor) (*CategoryPage, error)
	// ListCategoriesByIDs returns found categories in ids order. Missing IDs are
	// skipped and duplicate IDs are collapsed at their first occurrence.
	ListCategoriesByIDs(ctx context.Context, ids []string) ([]Category, error)
	// SearchCategories ranks case-insensitive name matches by exact, prefix, then
	// substring match; empty query returns up to limit categories alphabetically.
	SearchCategories(ctx context.Context, query string, limit int) ([]Category, error)
	// SearchCategoriesWithVideos is the library-only category search. It uses
	// the same ranking as SearchCategories, but restricts results to categories
	// linked to at least one non-deleted recording.
	SearchCategoriesWithVideos(ctx context.Context, query string, limit int) ([]Category, error)
	ListCategoriesMissingGameMetadata(ctx context.Context, checkedBefore time.Time) ([]Category, error)
	// UpdateCategoryGameMetadata writes the Twitch /games metadata used by the
	// category dashboard. Empty boxArtURL or igdbID inputs preserve the existing
	// value; if a non-empty igdbID changes, cached IGDB description state is
	// cleared so it can be re-enriched for the new game.
	UpdateCategoryGameMetadata(ctx context.Context, id, boxArtURL, igdbID string) error
	MarkCategoryGameMetadataChecked(ctx context.Context, id string) error
	ListCategoriesMissingDescription(ctx context.Context, checkedBefore time.Time) ([]Category, error)
	UpdateCategoryDescription(ctx context.Context, id, description string) error
	MarkCategoryDescriptionChecked(ctx context.Context, id string) error
	GetCategorySearchCache(ctx context.Context, normalizedQuery string) (*CategorySearchCache, error)
	UpsertCategorySearchCache(ctx context.Context, input CategorySearchCacheInput) (*CategorySearchCache, error)
	TouchCategorySearchCache(ctx context.Context, normalizedQuery string, at time.Time) error
	DeleteExpiredCategorySearchCache(ctx context.Context, before time.Time) error
	PruneCategorySearchCache(ctx context.Context, maxRows int) error

	GetTag(ctx context.Context, id int64) (*Tag, error)
	GetTagByName(ctx context.Context, name string) (*Tag, error)
	UpsertTag(ctx context.Context, name string) (*Tag, error)
	ListTags(ctx context.Context) ([]Tag, error)

	CreateFetchLog(ctx context.Context, input *FetchLogInput) error
	ListFetchLogs(ctx context.Context, limit, offset int) ([]FetchLog, error)
	ListFetchLogsByType(ctx context.Context, fetchType string, limit, offset int) ([]FetchLog, error)
	CountFetchLogs(ctx context.Context) (int64, error)
	CountFetchLogsByType(ctx context.Context, fetchType string) (int64, error)
	DeleteOldFetchLogs(ctx context.Context, before time.Time) error

	GetStream(ctx context.Context, id string) (*Stream, error)
	UpsertStream(ctx context.Context, s *StreamInput) (*Stream, error)
	EndStream(ctx context.Context, id string, endedAt time.Time) error
	UpdateStreamViewers(ctx context.Context, id string, viewerCount int64) error
	ListActiveStreams(ctx context.Context) ([]Stream, error)
	ListStreamsByBroadcaster(ctx context.Context, broadcasterID string, limit, offset int) ([]Stream, error)
	GetLastLiveStream(ctx context.Context, broadcasterID string) (*Stream, error)
	// ListLatestLivePerChannel returns the latest broadcast per channel with display
	// metadata, newest first.
	ListLatestLivePerChannel(ctx context.Context, limit int) ([]LatestLiveStream, error)

	GetVideo(ctx context.Context, id int64) (*Video, error)
	GetVideoByJobID(ctx context.Context, jobID string) (*Video, error)
	// ListVideosByJobIDs returns existing recordings for the supplied job IDs;
	// unknown IDs are omitted.
	ListVideosByJobIDs(ctx context.Context, jobIDs []string) ([]Video, error)
	CreateVideo(ctx context.Context, v *VideoInput) (*Video, error)
	UpdateVideoStatus(ctx context.Context, id int64, status string) error
	UpdateVideoSelectedVariant(ctx context.Context, id int64, quality string, fps *float64) error
	MarkVideoDone(ctx context.Context, id int64, durationSeconds float64, sizeBytes int64, thumbnail *string, completionKind string, truncated bool) error
	MarkVideoFailed(ctx context.Context, id int64, errMsg string, completionKind string, truncated bool) error
	MarkVideoDoneAndEnqueueRecordingWebhook(ctx context.Context, id int64, durationSeconds float64, sizeBytes int64, thumbnail *string, completionKind string, truncated bool, delivery *RecordingWebhookDeliveryInput) error
	MarkVideoFailedAndEnqueueRecordingWebhook(ctx context.Context, id int64, errMsg string, completionKind string, truncated bool, delivery *RecordingWebhookDeliveryInput) error
	SetVideoThumbnail(ctx context.Context, id int64, thumbnail string) error
	// SetVideoThumbnailIfMissing sets the thumbnail only when the row has none
	// and reports whether it did.
	SetVideoThumbnailIfMissing(ctx context.Context, id int64, thumbnail string) (bool, error)
	// GetOpenVideoByTwitchVideoID includes failed rows only while a retry is
	// scheduled, matching the unique open-VOD constraint.
	GetOpenVideoByTwitchVideoID(ctx context.Context, twitchVideoID string) (*Video, error)
	ListOpenVideosByTwitchVideoIDs(ctx context.Context, twitchVideoIDs []string) ([]Video, error)
	// ListOpenVideosByStreamIDs returns live recordings of the given
	// broadcasts that are neither removed nor failed.
	ListOpenVideosByStreamIDs(ctx context.Context, streamIDs []string) ([]Video, error)
	// ListArchiveQueue returns queued and running archives, oldest first.
	ListArchiveQueue(ctx context.Context) ([]Video, error)
	// ListRecentArchiveFailures returns failed archives that ended at or
	// after since, newest first, capped at limit rows.
	ListRecentArchiveFailures(ctx context.Context, since time.Time, limit int) ([]Video, error)
	// ListArchivesDueForRetry returns failed archives whose scheduled retry is
	// at or before now, earliest first.
	ListArchivesDueForRetry(ctx context.Context, now, after time.Time, afterID int64, limit int) ([]Video, error)
	// MarkArchiveFailedForRetry fails an archive like MarkVideoFailed and
	// schedules its next attempt in the same statement, so the row never
	// leaves the one-row-per-VOD rule.
	MarkArchiveFailedForRetry(ctx context.Context, id int64, errMsg string, completionKind string, truncated bool, nextRetryAt time.Time) error
	// RequeueArchiveVideo returns a failed archive to PENDING under jobID and
	// clears its failure. With scheduledOnly, only a row whose retry is still
	// scheduled qualifies. ErrNotFound when no row qualifies; ErrDuplicate when
	// another open row already holds the VOD.
	RequeueArchiveVideo(ctx context.Context, id int64, jobID string, scheduledOnly bool) error
	// ClearArchiveRetry cancels a scheduled retry; ErrNotFound when none is
	// scheduled.
	ClearArchiveRetry(ctx context.Context, id int64) error
	// ListArchivesMissingPoster returns at most limit archives queued since since
	// without posters, in ID order after afterID.
	ListArchivesMissingPoster(ctx context.Context, since time.Time, afterID int64, limit int) ([]Video, error)
	// DeleteQueuedArchiveVideo hard-deletes a PENDING archive and its job;
	// ErrNotFound when the row is missing, already started, or not an archive.
	DeleteQueuedArchiveVideo(ctx context.Context, id int64) error
	// ListVideos returns filtered recordings; empty Sort and Order default to
	// created_at descending.
	ListVideos(ctx context.Context, opts ListVideosOpts) ([]Video, error)
	ListVideosPage(ctx context.Context, opts ListVideosOpts, cursor *VideoListPageCursor) (*VideoListPage, error)
	// SearchVideos returns videos matching query across the recording title,
	// title history, broadcaster display metadata, and linked categories.
	// Empty query returns the latest non-deleted videos up to limit, matching
	// the "show all" behavior of channel/category search endpoints.
	SearchVideos(ctx context.Context, query string, limit int) ([]Video, error)
	ListVideosByBroadcaster(ctx context.Context, broadcasterID string, limit int, cursor *VideoPageCursor) (*VideoPage, error)
	ListVideosByCategory(ctx context.Context, categoryID string, limit int, cursor *VideoPageCursor) (*VideoPage, error)
	ListVideosMissingThumbnail(ctx context.Context) ([]Video, error)
	// RequestVideoDelete queues an operator-requested delete on a live terminal
	// recording. The background deletion task performs the object purge and
	// tombstone finalization.
	RequestVideoDelete(ctx context.Context, id int64) (*Video, error)
	// ListVideosPendingManualDelete returns queued manual deletes that are safe
	// to purge now, including the recording-webhook frozen-parts guard.
	ListVideosPendingManualDelete(ctx context.Context, afterID int64, limit int) ([]Video, error)
	// SoftDeleteVideo tombstones a video, recording why via kind
	// (DeletionKindRetention | DeletionKindManual).
	SoftDeleteVideo(ctx context.Context, id int64, kind string) error
	// ListRetentionCandidates returns the terminal, not-yet-tombstoned
	// recordings that own a snapshotted retention policy, can have reclaimable
	// objects, and are already due at now.
	ListRetentionCandidates(ctx context.Context, now time.Time, afterID int64, limit int) ([]RetentionVideo, error)
	ListVideosForStorageScan(ctx context.Context, afterID int64, limit int) ([]StorageScanVideo, error)
	// ListVideosForStorageWitness samples rows that may still own media,
	// including active attempts and reversible tombstones excluded from scans.
	ListVideosForStorageWitness(ctx context.Context, limit int) ([]StorageScanVideo, error)
	GetVideoForStorageScan(ctx context.Context, id int64) (*StorageScanVideo, error)
	// TombstoneMissingVideo conditionally reconciles a terminal row. It retains
	// its poster, parts and asset metadata and never authorizes object deletion.
	TombstoneMissingVideo(ctx context.Context, id int64) (bool, error)
	// RestoreMissingVideo brings a missing-media tombstone back into the library.
	// ErrNotFound when the row is live, another kind, or queued for a manual
	// delete; parts and objects were never touched, so nothing else changes.
	RestoreMissingVideo(ctx context.Context, id int64) error
	// ListMissingTombstones pages the reversible tombstones, oldest id first, for
	// the scan's restore phase; GetMissingTombstone is the exact lookup.
	ListMissingTombstones(ctx context.Context, afterID int64, limit int) ([]StorageScanVideo, error)
	GetMissingTombstone(ctx context.Context, id int64) (*StorageScanVideo, error)
	// FinalizeDelete is the DB commit marker after object purge: tombstone the
	// video (recording why via kind) and remove its parts in one transaction so
	// readers never see a visible row whose part rows were already deleted.
	FinalizeDelete(ctx context.Context, videoID int64, kind string) error
	CountVideosByStatus(ctx context.Context, status string) (int64, error)
	VideoStatsByStatus(ctx context.Context) ([]VideoStatsByStatus, error)
	// VideoStatsHistory returns the terminal recordings grouped by status,
	// completion kind and tombstone state, which is everything the download
	// history needs to count its outcome tabs under either media scope.
	VideoStatsHistory(ctx context.Context) ([]VideoStatsHistoryBucket, error)
	VideoStatsTotals(ctx context.Context, userID string) (*VideoStatsTotals, error)
	// VideoStatsTotalsByBroadcaster scopes the statistics totals to one broadcaster.
	VideoStatsTotalsByBroadcaster(ctx context.Context, broadcasterID string) (*VideoStatsTotals, error)
	GetVideoUserState(ctx context.Context, userID string, videoID int64) (*VideoUserState, error)
	ListVideoUserStatesForVideos(ctx context.Context, userID string, videoIDs []int64) ([]VideoUserState, error)
	SetVideoWatchLater(ctx context.Context, userID string, videoID int64, watchLater bool) (*VideoUserState, error)
	// UpdateVideoWatchProgress orders finished-recording progress writes by the
	// server time at; WatchStartedSeconds and WatchStartedFraction govern watched_at.
	UpdateVideoWatchProgress(ctx context.Context, userID string, videoID int64, positionSeconds float64, completed bool, at time.Time) (*VideoUserState, error)
	// ListContinueWatchingVideos returns started recordings whose saved position
	// is resumable under the viewer's preferences, most recently watched first.
	ListContinueWatchingVideos(ctx context.Context, userID string, limit int) ([]Video, error)

	CreateJob(ctx context.Context, input *JobInput) (*Job, error)
	GetJob(ctx context.Context, id string) (*Job, error)
	GetJobByVideoID(ctx context.Context, videoID int64) (*Job, error)
	// GetActiveLiveJobByBroadcaster is the live-recording idempotency check;
	// queued or running archives for the channel are ignored.
	GetActiveLiveJobByBroadcaster(ctx context.Context, broadcasterID string) (*Job, error)
	MarkJobDone(ctx context.Context, id string) error
	MarkJobFailed(ctx context.Context, id string, errMsg string) error
	ListRunningLiveBroadcasters(ctx context.Context) ([]string, error)

	CreateVideoPart(ctx context.Context, input *VideoPartInput) (*VideoPart, error)
	FinalizeVideoPart(ctx context.Context, input *VideoPartFinalize) error
	GetVideoPart(ctx context.Context, id int64) (*VideoPart, error)
	GetVideoPartByIndex(ctx context.Context, videoID int64, partIndex int32) (*VideoPart, error)
	ListVideoParts(ctx context.Context, videoID int64) ([]VideoPart, error)
	// ListVideoPartsForVideos returns parts ordered by video ID, then part index.
	ListVideoPartsForVideos(ctx context.Context, videoIDs []int64) ([]VideoPart, error)
	CountVideoParts(ctx context.Context, videoID int64) (int64, error)
	// HasFinalizedVideoParts reports whether any part has stored output bytes,
	// which permits classifying a failed recording as partial.
	HasFinalizedVideoParts(ctx context.Context, videoID int64) (bool, error)
	DeleteVideoParts(ctx context.Context, videoID int64) error

	GetVideoPlaybackAsset(ctx context.Context, videoID int64) (*VideoPlaybackAsset, error)
	UpsertVideoPlaybackAsset(ctx context.Context, input *VideoPlaybackAssetInput) (*VideoPlaybackAsset, error)
	TouchVideoPlaybackAsset(ctx context.Context, videoID int64) error
	SumReadyPlaybackBytes(ctx context.Context) (int64, error)
	ListReadyVideoPlaybackAssets(ctx context.Context, after PlaybackAssetCursor, limit int) ([]VideoPlaybackAsset, error)
	DeleteVideoPlaybackAsset(ctx context.Context, videoID int64) error

	UpsertTitle(ctx context.Context, name string) (*Title, error)
	LinkStreamTitle(ctx context.Context, streamID string, titleID int64) error
	LinkVideoTitle(ctx context.Context, videoID int64, titleID int64) error
	UpsertVideoTitleSpan(ctx context.Context, videoID int64, titleID int64, at time.Time) error
	ListTitlesForStream(ctx context.Context, streamID string) ([]Title, error)
	ListTitlesForVideo(ctx context.Context, videoID int64) ([]TitleSpan, error)

	LinkStreamCategory(ctx context.Context, streamID, categoryID string) error
	LinkVideoCategory(ctx context.Context, videoID int64, categoryID string) error
	UpsertVideoCategorySpan(ctx context.Context, videoID int64, categoryID string, at time.Time) error
	LinkStreamTag(ctx context.Context, streamID string, tagID int64) error
	LinkVideoTag(ctx context.Context, videoID, tagID int64) error
	ListPrimaryCategoriesForVideos(ctx context.Context, videoIDs []int64) (map[int64]Category, error)
	ListCategoriesForVideo(ctx context.Context, videoID int64) ([]CategorySpan, error)
	CloseOpenVideoMetadataSpans(ctx context.Context, videoID int64, at time.Time) error
	ResumeVideoMetadataSpans(ctx context.Context, videoID int64, at time.Time) error

	// RecordVideoMetadataChange atomically writes an owning execution's observation.
	// Empty title and category return ErrNoMetadataObserved; stopped or replaced
	// executions return ErrStaleExecution. The result supports effects after commit.
	RecordVideoMetadataChange(ctx context.Context, input VideoMetadataChangeInput) (*VideoMetadataChangeResult, error)
	// ListVideoMetadataChanges returns chronological observations with title and
	// category rows hydrated.
	ListVideoMetadataChanges(ctx context.Context, videoID int64) ([]VideoMetadataChange, error)
	ListTagsForVideo(ctx context.Context, videoID int64) ([]Tag, error)

	CreateScheduleRequest(ctx context.Context, broadcasterID, requestedBy string, note *string) (*ScheduleRequest, error)
	GetScheduleRequest(ctx context.Context, id int64) (*ScheduleRequest, error)
	ListScheduleRequests(ctx context.Context, limit int, cursor *ScheduleRequestCursor) ([]ScheduleRequestView, error)
	ListScheduleRequestsForUser(ctx context.Context, userID string, limit int, cursor *ScheduleRequestCursor) ([]ScheduleRequestView, error)
	// DecideScheduleRequest finalizes a pending request and reports whether
	// it matched.
	DecideScheduleRequest(ctx context.Context, id int64, status, decidedBy string, scheduleID *int64) (bool, error)
	// DeleteScheduleRequest cancels the requester's own pending request and
	// reports whether it matched.
	DeleteScheduleRequest(ctx context.Context, id int64, requestedBy string) (bool, error)
	// ApproveScheduleRequest creates the schedule and its filters and
	// approves the request atomically. It returns nil, false, nil without
	// creating a schedule if the request is no longer pending.
	ApproveScheduleRequest(ctx context.Context, requestID int64, decidedBy string, input *ScheduleInput, filters ScheduleFilterInput) (*DownloadSchedule, bool, error)

	CreateSchedule(ctx context.Context, input *ScheduleInput) (*DownloadSchedule, error)
	CreateScheduleWithFilters(ctx context.Context, input *ScheduleInput, filters ScheduleFilterInput) (*DownloadSchedule, error)
	GetSchedule(ctx context.Context, id int64) (*DownloadSchedule, error)
	GetScheduleForUserChannel(ctx context.Context, broadcasterID, userID string) (*DownloadSchedule, error)
	UpdateSchedule(ctx context.Context, id int64, input *ScheduleInput) (*DownloadSchedule, error)
	UpdateScheduleWithFilters(ctx context.Context, id int64, input *ScheduleInput, filters ScheduleFilterInput) (*DownloadSchedule, error)
	ToggleSchedule(ctx context.Context, id int64) (*DownloadSchedule, error)
	DeleteSchedule(ctx context.Context, id int64) error
	ListSchedules(ctx context.Context, limit, offset int) ([]DownloadSchedule, error)
	ListSchedulesForUser(ctx context.Context, userID string, limit, offset int) ([]DownloadSchedule, error)
	// ListActiveSchedulesForBroadcaster runs on every stream.online event and must
	// retain its indexed lookup of enabled schedules.
	ListActiveSchedulesForBroadcaster(ctx context.Context, broadcasterID string) ([]DownloadSchedule, error)
	RecordScheduleTrigger(ctx context.Context, id int64) error
	LinkScheduleCategory(ctx context.Context, scheduleID int64, categoryID string) error
	UnlinkScheduleCategory(ctx context.Context, scheduleID int64, categoryID string) error
	ClearScheduleCategories(ctx context.Context, scheduleID int64) error
	ListScheduleCategoriesByScheduleIDs(ctx context.Context, ids []int64) (map[int64][]Category, error)
	ListScheduleTagsByScheduleIDs(ctx context.Context, ids []int64) (map[int64][]Tag, error)
	ListScheduleCategories(ctx context.Context, scheduleID int64) ([]Category, error)
	LinkScheduleTag(ctx context.Context, scheduleID, tagID int64) error
	UnlinkScheduleTag(ctx context.Context, scheduleID, tagID int64) error
	ClearScheduleTags(ctx context.Context, scheduleID int64) error
	ListScheduleTags(ctx context.Context, scheduleID int64) ([]Tag, error)

	// CreateSubscription inserts a mirrored EventSub subscription; revocation is a
	// soft deletion via MarkSubscriptionRevoked.
	CreateSubscription(ctx context.Context, input *SubscriptionInput) (*Subscription, error)
	// UpsertSubscription mirrors Twitch-reported subscriptions even when the
	// original create was not recorded locally.
	UpsertSubscription(ctx context.Context, input *SubscriptionInput) (*Subscription, error)
	GetSubscription(ctx context.Context, id string) (*Subscription, error)
	GetActiveSubscriptionForBroadcasterType(ctx context.Context, broadcasterID, subType string) (*Subscription, error)
	ListActiveSubscriptions(ctx context.Context, limit, offset int) ([]Subscription, error)
	ListSubscriptionsByBroadcaster(ctx context.Context, broadcasterID string) ([]Subscription, error)
	ListSubscriptionsByType(ctx context.Context, subType string) ([]Subscription, error)
	UpdateSubscriptionStatus(ctx context.Context, id, status string) error
	MarkSubscriptionRevoked(ctx context.Context, id, reason string) error
	DeleteSubscription(ctx context.Context, id string) error
	CountActiveSubscriptions(ctx context.Context) (int64, error)

	CreateEventSubSnapshot(ctx context.Context, total, totalCost, maxTotalCost int64) (*EventSubSnapshot, error)
	GetLatestEventSubSnapshot(ctx context.Context) (*EventSubSnapshot, error)
	ListEventSubSnapshots(ctx context.Context, limit, offset int) ([]EventSubSnapshot, error)
	DeleteOldEventSubSnapshots(ctx context.Context, before time.Time) error
	LinkSnapshotSubscription(ctx context.Context, snapshotID int64, subscriptionID string, costAtSnapshot int64, statusAtSnapshot string) error

	UpsertTask(ctx context.Context, name, description string, intervalSeconds int64) (*Task, error)
	GetTask(ctx context.Context, name string) (*Task, error)
	ListTasks(ctx context.Context) ([]Task, error)
	ListDueTasks(ctx context.Context) ([]Task, error)
	ScheduleTaskIfEnabled(ctx context.Context, name string) error
	SetTaskEnabled(ctx context.Context, name string, enabled bool) (*Task, error)
	SetTaskNextRun(ctx context.Context, name string) error

	CreateEventLog(ctx context.Context, input *EventLogInput) (*EventLog, error)
	ListEventLogs(ctx context.Context, limit, offset int) ([]EventLog, error)
	ListEventLogsByDomain(ctx context.Context, domain string, limit, offset int) ([]EventLog, error)
	ListEventLogsBySeverity(ctx context.Context, severity string, limit, offset int) ([]EventLog, error)
	CountEventLogs(ctx context.Context) (int64, error)
	CountEventLogsByDomain(ctx context.Context, domain string) (int64, error)
	DeleteOldEventLogs(ctx context.Context, before time.Time) error

	GetSettings(ctx context.Context, userID string) (*Settings, error)
	// EnsureSettings creates defaults only when the user has no settings row.
	EnsureSettings(ctx context.Context, userID string) (*Settings, error)
	UpdatePlaybackSettings(ctx context.Context, s *Settings) (*Settings, error)
	UpsertSettings(ctx context.Context, s *Settings) (*Settings, error)

	GetServerSettings(ctx context.Context) (*ServerSettings, error)
	UpsertServerSettings(ctx context.Context, s *ServerSettings) (*ServerSettings, error)
	UpsertPlaybackCacheConfig(ctx context.Context, enabled bool, maxPercent int, autoGenerate bool) (*ServerSettings, error)
	// SetSchedulesPaused writes only the global auto-download pause flag,
	// leaving every other server setting and every schedule's is_disabled
	// untouched. Returns the persisted settings row.
	SetSchedulesPaused(ctx context.Context, paused bool) (*ServerSettings, error)
	// SetStorageID records the identity of the attached storage; every other
	// setting is left untouched.
	SetStorageID(ctx context.Context, id string) (*ServerSettings, error)
	// SetStorageScanCursor persists the storage scan's resume position; 0
	// restarts from the beginning of the library.
	SetStorageScanCursor(ctx context.Context, cursor int64) error
	// SetStorageRestoreCursor persists a separate restore pass: nil finishes
	// it, zero starts it, and positive ids record completed pages.
	SetStorageRestoreCursor(ctx context.Context, cursor *int64) error

	// UpsertRecordingWebhookConfig changes only enabled, url, and events, preserving
	// all secrets. EnsureRecordingWebhookSecret initializes an empty secret;
	// SetRecordingWebhookSecret replaces it unconditionally.
	UpsertRecordingWebhookConfig(ctx context.Context, enabled bool, url, events string) (*ServerSettings, error)
	EnsureRecordingWebhookSecret(ctx context.Context, secret string) error
	SetRecordingWebhookSecret(ctx context.Context, secret string) error

	// CreateRecordingWebhookDelivery queues an event for at-least-once delivery.
	// CreateClaimedRecordingWebhookDelivery reserves it for synchronous sending
	// so the poller cannot also claim it; retention prunes only terminal rows.
	CreateRecordingWebhookDelivery(ctx context.Context, input *RecordingWebhookDeliveryInput) (*RecordingWebhookDelivery, error)
	CreateClaimedRecordingWebhookDelivery(ctx context.Context, input *RecordingWebhookDeliveryInput) (*RecordingWebhookDelivery, error)
	ClaimDueRecordingWebhookDeliveries(ctx context.Context, now time.Time, limit int) ([]RecordingWebhookDelivery, error)
	MarkRecordingWebhookDeliveryDelivered(ctx context.Context, id int64, httpStatus int, now time.Time) error
	MarkRecordingWebhookDeliveryFinal(ctx context.Context, id int64, status string, httpStatus int, errMsg string, nextAttemptAt time.Time, now time.Time) error
	// SetRecordingWebhookDeliveryFrozenParts freezes the part metadata on the
	// first delivery build so a retry rebuilds the real part list after retention
	// deletes the video's parts. URLs are re-minted per attempt, capped by
	// retention, and not stored.
	SetRecordingWebhookDeliveryFrozenParts(ctx context.Context, id int64, frozenParts string) error
	ResetStaleRecordingWebhookDeliveries(ctx context.Context, before time.Time, now time.Time) error
	RetryRecordingWebhookDelivery(ctx context.Context, id int64, now time.Time) (*RecordingWebhookDelivery, error)
	ListRecordingWebhookDeliveries(ctx context.Context, limit int) ([]RecordingWebhookDelivery, error)
	DeleteOldRecordingWebhookDeliveries(ctx context.Context, before time.Time) error

	// GetServerHMACSecret returns the stored EventSub HMAC secret, or "" when
	// none has been generated yet. EnsureServerHMACSecret persists one only if
	// the slot is still empty (compare-and-swap), so it is safe to call from
	// concurrent boots.
	GetServerHMACSecret(ctx context.Context) (string, error)
	EnsureServerHMACSecret(ctx context.Context, secret string) error

	CreateWebhookEvent(ctx context.Context, input *WebhookEventInput) (*WebhookEvent, error)
	GetWebhookEvent(ctx context.Context, id int64) (*WebhookEvent, error)
	GetWebhookEventByEventID(ctx context.Context, eventID string) (*WebhookEvent, error)
	MarkWebhookEventProcessed(ctx context.Context, id int64) error
	MarkWebhookEventFailed(ctx context.Context, id int64, errMsg string) error
	ListWebhookEvents(ctx context.Context, limit, offset int) ([]WebhookEvent, error)
	ListWebhookEventsByBroadcaster(ctx context.Context, broadcasterID string, limit, offset int) ([]WebhookEvent, error)
	ListWebhookEventsByType(ctx context.Context, eventType string, limit, offset int) ([]WebhookEvent, error)
	ListStuckWebhookEvents(ctx context.Context, before time.Time, limit int) ([]WebhookEvent, error)
	ClearWebhookEventPayload(ctx context.Context, before time.Time) error
	CountWebhookEvents(ctx context.Context) (int64, error)
	CountWebhookEventsByType(ctx context.Context, eventType string) (int64, error)
}
