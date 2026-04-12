package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// ErrNotFound is returned when a requested entity does not exist.
// Both PG and SQLite adapters translate their driver-specific "no rows"
// errors to this sentinel so services can branch on it portably.
var ErrNotFound = errors.New("repository: not found")

// Repository is the common interface for database access.
// Both PG and SQLite adapters implement this.
type Repository interface {
	// Users
	GetUser(ctx context.Context, id string) (*User, error)
	GetUserByLogin(ctx context.Context, login string) (*User, error)
	UpsertUser(ctx context.Context, u *User) (*User, error)
	ListUsers(ctx context.Context) ([]User, error)
	UpdateUserRole(ctx context.Context, id string, role string) error

	// Sessions
	CreateSession(ctx context.Context, s *Session) error
	GetSession(ctx context.Context, hashedID string) (*Session, error)
	UpdateSessionTokens(ctx context.Context, hashedID string, encryptedTokens []byte) error
	UpdateSessionActivity(ctx context.Context, hashedID string) error
	DeleteSession(ctx context.Context, hashedID string) error
	DeleteUserSessions(ctx context.Context, userID string) error
	DeleteExpiredSessions(ctx context.Context) error
	ListUserSessions(ctx context.Context, userID string) ([]SessionInfo, error)

	// App Access Tokens
	GetLatestAppToken(ctx context.Context) (*AppAccessToken, error)
	CreateAppToken(ctx context.Context, token string, expiresAt time.Time) (*AppAccessToken, error)
	DeleteExpiredAppTokens(ctx context.Context) error

	// Whitelist
	IsWhitelisted(ctx context.Context, twitchUserID string) (bool, error)
	AddToWhitelist(ctx context.Context, twitchUserID string) error
	RemoveFromWhitelist(ctx context.Context, twitchUserID string) error
	ListWhitelist(ctx context.Context) ([]WhitelistEntry, error)

	// Channels
	GetChannel(ctx context.Context, broadcasterID string) (*Channel, error)
	GetChannelByLogin(ctx context.Context, login string) (*Channel, error)
	UpsertChannel(ctx context.Context, c *Channel) (*Channel, error)
	ListChannels(ctx context.Context) ([]Channel, error)
	DeleteChannel(ctx context.Context, broadcasterID string) error

	// User follows
	UpsertUserFollow(ctx context.Context, f *UserFollow) error
	ListUserFollows(ctx context.Context, userID string) ([]Channel, error)
	UnfollowChannel(ctx context.Context, userID, broadcasterID string) error

	// Categories
	GetCategory(ctx context.Context, id string) (*Category, error)
	GetCategoryByName(ctx context.Context, name string) (*Category, error)
	UpsertCategory(ctx context.Context, c *Category) (*Category, error)
	ListCategories(ctx context.Context) ([]Category, error)
	ListCategoriesMissingBoxArt(ctx context.Context) ([]Category, error)

	// Tags
	GetTag(ctx context.Context, id int64) (*Tag, error)
	GetTagByName(ctx context.Context, name string) (*Tag, error)
	UpsertTag(ctx context.Context, name string) (*Tag, error)
	ListTags(ctx context.Context) ([]Tag, error)

	// Fetch logs
	CreateFetchLog(ctx context.Context, input *FetchLogInput) error
	ListFetchLogs(ctx context.Context, limit, offset int) ([]FetchLog, error)
	ListFetchLogsByType(ctx context.Context, fetchType string, limit, offset int) ([]FetchLog, error)
	CountFetchLogs(ctx context.Context) (int64, error)
	CountFetchLogsByType(ctx context.Context, fetchType string) (int64, error)
	DeleteOldFetchLogs(ctx context.Context, before time.Time) error

	// Streams
	GetStream(ctx context.Context, id string) (*Stream, error)
	UpsertStream(ctx context.Context, s *StreamInput) (*Stream, error)
	EndStream(ctx context.Context, id string, endedAt time.Time) error
	UpdateStreamViewers(ctx context.Context, id string, viewerCount int64) error
	ListActiveStreams(ctx context.Context) ([]Stream, error)
	ListStreamsByBroadcaster(ctx context.Context, broadcasterID string, limit, offset int) ([]Stream, error)
	GetLastLiveStream(ctx context.Context, broadcasterID string) (*Stream, error)

	// Videos
	GetVideo(ctx context.Context, id int64) (*Video, error)
	GetVideoByJobID(ctx context.Context, jobID string) (*Video, error)
	CreateVideo(ctx context.Context, v *VideoInput) (*Video, error)
	UpdateVideoStatus(ctx context.Context, id int64, status string) error
	MarkVideoDone(ctx context.Context, id int64, durationSeconds float64, sizeBytes int64, thumbnail *string) error
	MarkVideoFailed(ctx context.Context, id int64, errMsg string) error
	SetVideoThumbnail(ctx context.Context, id int64, thumbnail string) error
	ListVideos(ctx context.Context, limit, offset int) ([]Video, error)
	ListVideosByStatus(ctx context.Context, status string, limit, offset int) ([]Video, error)
	ListVideosByBroadcaster(ctx context.Context, broadcasterID string, limit, offset int) ([]Video, error)
	ListVideosByCategory(ctx context.Context, categoryID string, limit, offset int) ([]Video, error)
	ListVideosMissingThumbnail(ctx context.Context) ([]Video, error)
	SoftDeleteVideo(ctx context.Context, id int64) error
	CountVideosByStatus(ctx context.Context, status string) (int64, error)
	VideoStatsByStatus(ctx context.Context) ([]VideoStatsByStatus, error)
	VideoStatsTotals(ctx context.Context) (*VideoStatsTotals, error)

	// Jobs — durable record of a download execution. Broadcaster-level
	// idempotency + resume-on-restart live here. See models.go Job for
	// schema and .docs/spec/download-pipeline.md for the resume-state
	// JSON shape.
	CreateJob(ctx context.Context, input *JobInput) (*Job, error)
	GetJob(ctx context.Context, id string) (*Job, error)
	GetJobByVideoID(ctx context.Context, videoID int64) (*Job, error)
	GetActiveJobByBroadcaster(ctx context.Context, broadcasterID string) (*Job, error)
	MarkJobRunning(ctx context.Context, id string) error
	MarkJobDone(ctx context.Context, id string) error
	MarkJobFailed(ctx context.Context, id string, errMsg string) error
	UpdateJobResumeState(ctx context.Context, id string, resumeState json.RawMessage) error
	ListRunningJobs(ctx context.Context) ([]Job, error)
	ListFailedJobsForRetry(ctx context.Context, before time.Time, limit int) ([]Job, error)

	// Video parts — one row per output segment. A single-part VOD has
	// one row; a VOD that split on variant change, codec change, or
	// restart-gap threshold has 2..N rows ordered by part_index.
	CreateVideoPart(ctx context.Context, input *VideoPartInput) (*VideoPart, error)
	FinalizeVideoPart(ctx context.Context, input *VideoPartFinalize) error
	GetVideoPart(ctx context.Context, id int64) (*VideoPart, error)
	GetVideoPartByIndex(ctx context.Context, videoID int64, partIndex int32) (*VideoPart, error)
	ListVideoParts(ctx context.Context, videoID int64) ([]VideoPart, error)
	CountVideoParts(ctx context.Context, videoID int64) (int64, error)
	DeleteVideoParts(ctx context.Context, videoID int64) error

	// Titles
	UpsertTitle(ctx context.Context, name string) (*Title, error)
	LinkStreamTitle(ctx context.Context, streamID string, titleID int64) error
	LinkVideoTitle(ctx context.Context, videoID int64, titleID int64) error
	ListTitlesForStream(ctx context.Context, streamID string) ([]Title, error)
	ListTitlesForVideo(ctx context.Context, videoID int64) ([]Title, error)

	// Junctions (categories, tags, requests)
	LinkStreamCategory(ctx context.Context, streamID, categoryID string) error
	LinkVideoCategory(ctx context.Context, videoID int64, categoryID string) error
	LinkStreamTag(ctx context.Context, streamID string, tagID int64) error
	LinkVideoTag(ctx context.Context, videoID, tagID int64) error
	ListCategoriesForVideo(ctx context.Context, videoID int64) ([]Category, error)
	ListTagsForVideo(ctx context.Context, videoID int64) ([]Tag, error)
	AddVideoRequest(ctx context.Context, videoID int64, userID string) error
	ListVideoRequestsForUser(ctx context.Context, userID string, limit, offset int) ([]Video, error)

	// Download schedules — auto-record rules matched on stream.online.
	CreateSchedule(ctx context.Context, input *ScheduleInput) (*DownloadSchedule, error)
	GetSchedule(ctx context.Context, id int64) (*DownloadSchedule, error)
	GetScheduleForUserChannel(ctx context.Context, broadcasterID, userID string) (*DownloadSchedule, error)
	UpdateSchedule(ctx context.Context, id int64, input *ScheduleInput) (*DownloadSchedule, error)
	ToggleSchedule(ctx context.Context, id int64) (*DownloadSchedule, error)
	DeleteSchedule(ctx context.Context, id int64) error
	ListSchedules(ctx context.Context, limit, offset int) ([]DownloadSchedule, error)
	ListSchedulesForUser(ctx context.Context, userID string, limit, offset int) ([]DownloadSchedule, error)
	// ListActiveSchedulesForBroadcaster is the hot path: called on every
	// stream.online webhook. Must be fast (partial index on is_disabled).
	ListActiveSchedulesForBroadcaster(ctx context.Context, broadcasterID string) ([]DownloadSchedule, error)
	RecordScheduleTrigger(ctx context.Context, id int64) error
	LinkScheduleCategory(ctx context.Context, scheduleID int64, categoryID string) error
	UnlinkScheduleCategory(ctx context.Context, scheduleID int64, categoryID string) error
	ClearScheduleCategories(ctx context.Context, scheduleID int64) error
	ListScheduleCategories(ctx context.Context, scheduleID int64) ([]Category, error)
	LinkScheduleTag(ctx context.Context, scheduleID, tagID int64) error
	UnlinkScheduleTag(ctx context.Context, scheduleID, tagID int64) error
	ClearScheduleTags(ctx context.Context, scheduleID int64) error
	ListScheduleTags(ctx context.Context, scheduleID int64) ([]Tag, error)

	// EventSub subscriptions (soft-delete via MarkSubscriptionRevoked).
	CreateSubscription(ctx context.Context, input *SubscriptionInput) (*Subscription, error)
	// UpsertSubscription mirrors a Twitch-reported sub into the local
	// table. Used by the snapshot self-heal path when Twitch returns
	// a sub we didn't create (or whose create-mirror failed) — lets
	// the snapshot junction link to a real subscriptions row.
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

	// EventSub snapshots + junction for historical state reconstruction.
	CreateEventSubSnapshot(ctx context.Context, total, totalCost, maxTotalCost int64) (*EventSubSnapshot, error)
	GetLatestEventSubSnapshot(ctx context.Context) (*EventSubSnapshot, error)
	ListEventSubSnapshots(ctx context.Context, limit, offset int) ([]EventSubSnapshot, error)
	DeleteOldEventSubSnapshots(ctx context.Context, before time.Time) error
	LinkSnapshotSubscription(ctx context.Context, snapshotID int64, subscriptionID string, costAtSnapshot int64, statusAtSnapshot string) error

	// Scheduled tasks — registered on startup, runtime state mutated
	// by the scheduler. See queries/*/tasks.sql for the state-machine.
	UpsertTask(ctx context.Context, name, description string, intervalSeconds int64) (*Task, error)
	GetTask(ctx context.Context, name string) (*Task, error)
	ListTasks(ctx context.Context) ([]Task, error)
	ListDueTasks(ctx context.Context) ([]Task, error)
	MarkTaskRunning(ctx context.Context, name string) error
	MarkTaskSuccess(ctx context.Context, name string, durationMs int64) error
	MarkTaskFailed(ctx context.Context, name string, durationMs int64, errMsg string) error
	SetTaskEnabled(ctx context.Context, name string, enabled bool) (*Task, error)
	SetTaskNextRun(ctx context.Context, name string) error

	// Event logs — append-only app-side audit trail.
	CreateEventLog(ctx context.Context, input *EventLogInput) (*EventLog, error)
	ListEventLogs(ctx context.Context, limit, offset int) ([]EventLog, error)
	ListEventLogsByDomain(ctx context.Context, domain string, limit, offset int) ([]EventLog, error)
	ListEventLogsBySeverity(ctx context.Context, severity string, limit, offset int) ([]EventLog, error)
	CountEventLogs(ctx context.Context) (int64, error)
	CountEventLogsByDomain(ctx context.Context, domain string) (int64, error)
	DeleteOldEventLogs(ctx context.Context, before time.Time) error

	// Settings — per-user preferences.
	GetSettings(ctx context.Context, userID string) (*Settings, error)
	UpsertSettings(ctx context.Context, s *Settings) (*Settings, error)

	// Webhook events — audit log with state machine + retention.
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
