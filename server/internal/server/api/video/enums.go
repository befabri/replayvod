package video

import "github.com/befabri/replayvod/server/internal/repository"

// Wire enums: named string types whose typed const groups make trpcgo emit
// narrowed TypeScript unions (VideoStatus = "PENDING" | "RUNNING" | ...) on the
// response DTOs instead of a bare `string`. The values alias the repository
// constants, so the DB CHECK constraints stay the single source of truth for
// the value set; these declarations exist only so the code generator can see
// it. TestWireEnumParity guards against the alias drifting.
//
// The domain layer (repository.Video.Status etc.) keeps plain `string` — the
// named type only appears on the JSON boundary, converted in the mappers below.

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
