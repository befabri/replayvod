// Package contracttest runs the repository behavior contract against both
// adapters; backend-specific fixture operations belong to Harness.
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
	// ConcurrentRepo returns an independent connection so Go pool limits
	// cannot hide missing database locks.
	ConcurrentRepo(t *testing.T) repository.Repository
	BackdateScheduleRequests(t *testing.T, at time.Time)

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
	run("Recording_TerminalTransaction", testRecordingTerminalTransaction)
	run("Recording_TerminalOutboxRollback", testRecordingTerminalOutboxRollback)
	run("Transaction_NestedRejected", testNestedTransactionRejected)

	run("CreateAttemptAtomic", testCreateAttemptAtomic)
	run("RecordingIntentAtomicity", testRecordingIntentAtomicity)
	run("RecordingIntentConstraints", testRecordingIntentConstraints)
	run("ExecutionRejectsStaleTransitions", testExecutionRejectsStaleTransitions)
	run("AttemptStopSurvivesCheckpointsAndFencesWriters", testAttemptStopSurvivesCheckpointsAndFencesWriters)
	run("StoppedAttemptDiscovery", testStoppedAttemptDiscovery)
	run("StoppedAdmissionRejectsInitialMetadata", testStoppedAdmissionRejectsInitialMetadata)
	run("PlaybackAsset_PaginationWithTiedTimestamps", testPlaybackAssetPaginationWithTiedTimestamps)
	run("AttemptCommitConfirmationLost", testAttemptCommitConfirmationLost)
	run("MetadataEligibilityIsTransactional", testMetadataEligibilityIsTransactional)

	run("Schedule_UpsertPreservesTriggerCount", testScheduleUpsertPreservesTriggerCount)
	run("Schedule_FilterLinkFailureRollsBack", testScheduleFilterLinkFailureRollsBack)

	run("ScheduleRequest_DecideOnce", testScheduleRequestDecideOnce)
	run("ScheduleRequest_DuplicatePendingIsErrDuplicate", testScheduleRequestDuplicatePendingIsErrDuplicate)
	run("ScheduleRequest_ApproveAtomic", testScheduleRequestApproveAtomic)
	run("ScheduleRequest_ApproveBlocksActiveDuplicate", testScheduleRequestApproveBlocksActiveDuplicate)
	run("ScheduleRequest_CancelOwnPendingOnly", testScheduleRequestCancelOwnPendingOnly)
	run("ScheduleRequest_ApprovalFailureRollsBack", testScheduleRequestApprovalFailureRollsBack)
	run("ScheduleRequest_ConcurrentApprovals", testScheduleRequestConcurrentApprovals)
	run("ScheduleRequest_ListScopeAndHistory", testScheduleRequestListScopeAndHistory)
	run("ScheduleRequest_Pagination", testScheduleRequestPagination)
	run("Schedule_BatchMetadata", testScheduleBatchMetadata)

	// subscriptions
	run("Subscription_RevokeKeepsRowForAudit", testSubscriptionRevokeKeepsRowForAudit)
	run("Subscription_ListActiveStableWithTiedCreatedAt", testSubscriptionListActiveStableWithTiedCreatedAt)
	run("Subscription_ActiveUniquePerBroadcasterType", testSubscriptionActiveUniquePerBroadcasterType)

	run("WebhookEvent_DedupOnConflict", testWebhookEventDedupOnConflict)
	run("WebhookEvent_PayloadRoundTrip", testWebhookEventPayloadRoundTrip)

	// tasks
	run("Task_InterruptedRetriesImmediately", testTaskInterruptedRetriesImmediately)
	run("Task_AutomaticRunRespectsDisabledState", testTaskAutomaticRunRespectsDisabledState)
	run("Archive_RunningJobsOnlyResumeCurrentActiveAttempt", testRunningJobsOnlyResumeCurrentActiveAttempt)
	run("Task_UpsertPreservesRuntimeState", testTaskUpsertPreservesRuntimeState)
	run("Task_MarkSuccessRearmsNextRun", testTaskMarkSuccessRearmsNextRun)
	run("Task_QueuedRunSurvivesMarkSuccess", testTaskQueuedRunSurvivesMarkSuccess)
	run("Task_SetNextRunMissingReturnsNotFound", testTaskSetNextRunMissingReturnsNotFound)

	run("Invite_RedeemSingleUse", testInviteRedeemSingleUse)
	run("Invite_RedeemExpiredFailsClosed", testInviteRedeemExpiredFailsClosed)
	run("Invite_RevokeOnlyPending", testInviteRevokeOnlyPending)
	run("Invite_RotateOnlyPending", testInviteRotateOnlyPending)
	run("Invite_ConcurrentRedemption", testInviteConcurrentRedemption)
	run("Invite_ConcurrentRotateAndRedeem", testInviteConcurrentRotateAndRedeem)
	run("Transaction_CommitAndRollback", testTransactionCommitAndRollback)
	run("UserLock_SerializesRoleChanges", testUserLockSerializesRoleChanges)

	// settings + event logs
	run("Settings_UpsertInsertThenUpdate", testSettingsUpsertInsertThenUpdate)
	run("TwitchPlaybackSession", testTwitchPlaybackSession)
	run("EventLog_DeleteOldSkipsWarnAndError", testEventLogDeleteOldSkipsWarnAndError)

	run("VideoMetadataChange_RoundTripsMediaOffset", testVideoMetadataChangeRoundTripsMediaOffset)

	// errors
	run("NotFound_OnMissingGet", testNotFoundOnMissingGet)

	// videos
	run("Video_ListForStorageScan", testListVideosForStorageScan)
	run("Video_SoftDeleteThumbnail", testSoftDeleteVideoThumbnail)
	run("Video_MissingTombstoneRestoreAndPermanentRemoval", testMissingTombstoneRestoreAndPermanentRemoval)

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

	run("Archive_VideoRoundTrip", testArchiveVideoRoundTrip)
	run("Archive_OpenRowPerVOD", testArchiveOpenRowPerVOD)
	run("Archive_QueueOrderAndDequeue", testArchiveQueueOrderAndDequeue)
	run("Archive_RetryLifecycle", testArchiveRetryLifecycle)
	run("Archive_StreamMatchAndMissingPoster", testArchiveStreamMatchAndMissingPoster)
	run("Archive_RetryYieldsToQueuedDelete", testArchiveRetryYieldsToQueuedDelete)
	run("Archive_SourceFilterAndBroadcastSort", testArchiveSourceFilterAndBroadcastSort)
	run("Video_StreamLinkRequiresKnownStream", testVideoStreamLinkRequiresKnownStream)
	run("Video_MarkDoneKeepsPosterWithoutFrame", testMarkVideoDoneKeepsPosterWithoutFrame)

	run("Settings_SetSchedulesPausedRoundTripAndIsolation", testSetSchedulesPausedRoundTripAndIsolation)
	run("Settings_StorageIdentityRoundTripAndIsolation", testStorageIdentityRoundTripAndIsolation)
	run("Settings_StorageRestoreCursorRoundTripAndIsolation", testStorageRestoreCursorRoundTripAndIsolation)
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

	run("Video_ListPageScope", testListVideosPageScope)
	run("Video_ListSortDimensions", testListVideosSortDimensions)
	run("Video_ListPageTerminalOnlyHistoryWhen", testListVideosPageTerminalOnlyHistoryWhen)
	run("Video_UserStateFiltersAndStatistics", testVideoUserStateFiltersAndStatistics)
	run("Video_HistoryOutcomeCounts", testVideoHistoryOutcomeCounts)
}
