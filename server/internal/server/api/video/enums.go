package video

import "github.com/befabri/replayvod/server/internal/repository"

// VideoStatus and the other wire enums use typed const groups so trpcgo emits
// TypeScript unions. Keep the wire values tied to repository constants.

type VideoStatus string

const (
	VideoStatusPending VideoStatus = repository.VideoStatusPending
	VideoStatusRunning VideoStatus = repository.VideoStatusRunning
	VideoStatusDone    VideoStatus = repository.VideoStatusDone
	VideoStatusFailed  VideoStatus = repository.VideoStatusFailed
)

type CompletionKind string

const (
	CompletionKindComplete  CompletionKind = repository.CompletionKindComplete
	CompletionKindPartial   CompletionKind = repository.CompletionKindPartial
	CompletionKindCancelled CompletionKind = repository.CompletionKindCancelled
)

type PlaybackAssetStatus string

const (
	PlaybackAssetStatusBuilding    PlaybackAssetStatus = repository.PlaybackAssetStatusBuilding
	PlaybackAssetStatusReady       PlaybackAssetStatus = repository.PlaybackAssetStatusReady
	PlaybackAssetStatusFailed      PlaybackAssetStatus = repository.PlaybackAssetStatusFailed
	PlaybackAssetStatusUnavailable PlaybackAssetStatus = repository.PlaybackAssetStatusUnavailable
)

type VideoSource string

const (
	VideoSourceLive VideoSource = repository.VideoSourceLive
	VideoSourceVOD  VideoSource = repository.VideoSourceVOD
)

// RecordingIntentStatus is omitted when a recording has no continuation intent.
type RecordingIntentStatus string

const (
	RecordingIntentStatusActive  RecordingIntentStatus = repository.RecordingIntentStatusActive
	RecordingIntentStatusWaiting RecordingIntentStatus = repository.RecordingIntentStatusWaiting
	RecordingIntentStatusStopped RecordingIntentStatus = repository.RecordingIntentStatusStopped
	RecordingIntentStatusExpired RecordingIntentStatus = repository.RecordingIntentStatusExpired
)
