// Package contracttest is the backend-agnostic behavioral contract for
// repository.Repository. It runs one suite of subtests against any adapter via
// a Factory, so the Postgres and SQLite adapters are held to the same behavior
// by a single suite instead of two hand-maintained mirror test trees.
//
// Like net/http/httptest, this is a normal (non _test.go) package that imports
// testing on purpose: each adapter package wires it up from its own
// contract_test.go by passing a Factory.
//
// It depends only on the repository package. It does not import the adapters,
// their sqlc-generated packages, testdb, or any backend glue. Anything a test
// needs that the repository interface deliberately does not expose (e.g.
// backdating server-managed timestamps) goes through the Harness, whose
// dialect-specific implementation lives in each adapter's test file.
package contracttest

import (
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

// Harness provides the repository under test plus the backend-specific,
// test-only setup operations the interface does not expose. Each adapter
// package implements it in its contract_test.go.
type Harness interface {
	// Repo returns the repository under test. It returns the same instance
	// for the life of the harness.
	Repo() repository.Repository

	// BackdateAllSubscriptionsCreated sets created_at on every subscriptions
	// row to at. Used to force a created_at tie so pagination tie-breaking can
	// be exercised deterministically.
	BackdateAllSubscriptionsCreated(t *testing.T, at time.Time)

	// BackdateVideoStartDownload sets a single video's start_download_at. The
	// repository interface doesn't expose this server-managed timestamp, but
	// recency-ordered list/page queries need deterministic values to assert on.
	BackdateVideoStartDownload(t *testing.T, videoID int64, at time.Time)

	// BackdateVideoDownloadedAt sets a video's downloaded_at.
	BackdateVideoDownloadedAt(t *testing.T, videoID int64, at time.Time)

	// BackdateVideoDeletedAt sets a soft-deleted video's deleted_at.
	BackdateVideoDeletedAt(t *testing.T, videoID int64, at time.Time)

	// BackdateVideoUserStateWatched sets a (user, video) state's watched_at.
	BackdateVideoUserStateWatched(t *testing.T, userID string, videoID int64, at time.Time)

	// BackdateRecordingWebhookDelivery sets created_at/updated_at/delivered_at
	// on a delivery row; nil arguments leave that column unchanged. Used to age
	// deliveries for retention-pruning tests.
	BackdateRecordingWebhookDelivery(t *testing.T, id int64, createdAt, updatedAt, deliveredAt *time.Time)
}

// Factory builds a fresh Harness backed by a fresh, migrated, empty database
// for a single subtest.
type Factory func(t *testing.T) Harness

// Run executes the full backend-agnostic repository contract as subtests, each
// against its own fresh Harness from newHarness.
func Run(t *testing.T, newHarness Factory) {
	t.Helper()
	run := func(name string, fn func(*testing.T, Harness)) {
		t.Run(name, func(t *testing.T) { fn(t, newHarness(t)) })
	}

	// schedules
	run("Schedule_UpsertPreservesTriggerCount", testScheduleUpsertPreservesTriggerCount)
	run("Schedule_FilterLinkFailureRollsBack", testScheduleFilterLinkFailureRollsBack)

	// subscriptions
	run("Subscription_RevokeKeepsRowForAudit", testSubscriptionRevokeKeepsRowForAudit)
	run("Subscription_ListActiveStableWithTiedCreatedAt", testSubscriptionListActiveStableWithTiedCreatedAt)
	run("Subscription_ActiveUniquePerBroadcasterType", testSubscriptionActiveUniquePerBroadcasterType)

	// webhook events
	run("WebhookEvent_DedupOnConflict", testWebhookEventDedupOnConflict)
	run("WebhookEvent_PayloadRoundTrip", testWebhookEventPayloadRoundTrip)

	// tasks
	run("Task_UpsertPreservesRuntimeState", testTaskUpsertPreservesRuntimeState)
	run("Task_MarkSuccessRearmsNextRun", testTaskMarkSuccessRearmsNextRun)
	run("Task_QueuedRunSurvivesMarkSuccess", testTaskQueuedRunSurvivesMarkSuccess)
	run("Task_SetNextRunMissingReturnsNotFound", testTaskSetNextRunMissingReturnsNotFound)

	// settings + event logs
	run("Settings_UpsertInsertThenUpdate", testSettingsUpsertInsertThenUpdate)
	run("EventLog_DeleteOldSkipsWarnAndError", testEventLogDeleteOldSkipsWarnAndError)

	// video metadata changes
	run("VideoMetadataChange_RoundTripsMediaOffset", testVideoMetadataChangeRoundTripsMediaOffset)

	// errors
	run("NotFound_OnMissingGet", testNotFoundOnMissingGet)

	// playback assets
	run("PlaybackAsset_ReadyToFailedTransition", testPlaybackAssetReadyToFailedTransition)
	run("PlaybackAsset_ListReadyLRUOrder", testPlaybackAssetListReadyLRUOrder)
	run("PlaybackAsset_TouchMovesToBackOfLRU", testPlaybackAssetTouchMovesToBackOfLRU)

	// categories
	run("Category_DescriptionMethods", testCategoryDescriptionMethods)
	run("Category_GetDetail", testGetCategoryDetail)
	run("Category_ListByIDsReturnsInputOrder", testListCategoriesByIDsReturnsInputOrder)
	run("Category_ListMissingGameMetadata", testListCategoriesMissingGameMetadata)
	run("Category_UpdateGameMetadata", testUpdateCategoryGameMetadata)
	run("Category_UpsertBatchPreservesBoxArtAndOrder", testUpsertCategoriesPreservesBoxArtAndReturnsInputOrder)
	run("Category_UpsertPreservesBoxArt", testUpsertCategoryPreservesBoxArt)
	run("Category_ListWithVideos", testListCategoriesWithVideos)
	run("Category_ListWithVideosPageSortAndCursor", testListCategoriesWithVideosPageSortAndCursor)

	// channels
	run("Channel_ListPageCursorPagination", testListChannelsPageCursorPagination)
	run("Channel_ListLatestLivePerChannel", testListLatestLivePerChannelOnePerBroadcaster)
	run("Channel_ListByIDs", testListChannelsByIDs)

	// videos
	run("Video_CreateNormalizesRecordingSettings", testCreateVideoNormalizesRecordingSettings)
	run("Video_ListByJobIDs", testListVideosByJobIDs)
	run("Video_ListPageCursorPagination", testListVideosPageCursorPagination)
	run("Video_ListPageFiltersAndNullCursor", testListVideosPageFiltersAndNullCursor)
	run("Video_ListByBroadcasterAndCategoryPage", testListVideosByBroadcasterAndCategoryPage)
	run("Video_MetadataDurationsTracksHistory", testVideoMetadataDurationsTracksHistoryAndPrimaryCategory)
	run("Video_ManualDeleteQueueWaitsForWebhookFrozenParts", testManualDeleteQueueWaitsForWebhookFrozenParts)

	// server settings + recording webhook + schedules pause
	run("Settings_SetSchedulesPausedRoundTripAndIsolation", testSetSchedulesPausedRoundTripAndIsolation)
	run("Settings_ServerHMACSecretPreservedAcrossUpsert", testServerHMACSecretPreservedAcrossUpsert)
	run("RecordingWebhook_SecretEnsureCASSetUnconditional", testRecordingWebhookSecretEnsureIsCASSetIsUnconditional)
	run("RecordingWebhook_ConfigRoundTrip", testRecordingWebhookConfigRoundTrip)
	run("RecordingWebhook_ConfigPreservedAcrossServerModeUpsert", testRecordingWebhookConfigPreservedAcrossServerModeUpsert)
	run("RecordingWebhook_CreateClaimedNotClaimable", testCreateClaimedRecordingWebhookDeliveryNotClaimable)
	run("RecordingWebhook_MarkDoneEnqueueConditionalDedupe", testMarkVideoDoneAndEnqueueRecordingWebhookConditionalAndDedupe)
	run("RecordingWebhook_DeliveryOutboxLifecycle", testRecordingWebhookDeliveryOutboxLifecycle)
	run("RecordingWebhook_RetryOnlyFailedOrRejected", testRetryRecordingWebhookDeliveryOnlyFailedOrRejected)
	run("RecordingWebhook_ResetStaleDeliveries", testResetStaleRecordingWebhookDeliveries)
	run("RecordingWebhook_DeleteOldPrunesTerminalKeepsActive", testDeleteOldRecordingWebhookDeliveriesPrunesTerminalKeepsActive)

	// videos: scope / history / user-state (need backdate hooks)
	run("Video_ListPageScope", testListVideosPageScope)
	run("Video_ListSortDimensions", testListVideosSortDimensions)
	run("Video_ListPageTerminalOnlyHistoryWhen", testListVideosPageTerminalOnlyHistoryWhen)
	run("Video_UserStateFiltersAndStatistics", testVideoUserStateFiltersAndStatistics)
}
