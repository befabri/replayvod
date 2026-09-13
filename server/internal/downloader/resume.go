package downloader

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"
)

// Stage is a persisted pipeline position; new values require matching recovery handling.
type Stage string

// Persisted pipeline stages.
const (
	StageAuth            Stage = "AUTH"
	StagePlaylist        Stage = "PLAYLIST"
	StageSegments        Stage = "SEGMENTS"
	StagePrepareInput    Stage = "PREPARE_INPUT"
	StageRemux           Stage = "REMUX"
	StageProbe           Stage = "PROBE"
	StageThumbnail       Stage = "THUMBNAIL"
	StageCorruptionCheck Stage = "CORRUPTION_CHECK"
	StageStore           Stage = "STORE"
)

// stageOrder follows pipeline execution because alphabetical stage order differs.
var stageOrder = map[Stage]int{
	StageAuth:            0,
	StagePlaylist:        1,
	StageSegments:        2,
	StagePrepareInput:    3,
	StageRemux:           4,
	StageProbe:           5,
	StageCorruptionCheck: 6,
	StageThumbnail:       7,
	StageStore:           8,
}

// AtOrAfter reports whether s reaches target in pipeline order.
func (s Stage) AtOrAfter(target Stage) bool {
	return stageOrder[s] >= stageOrder[target]
}

// GapReason is persisted in checkpoints and must remain stable across upgrades.
type GapReason string

const (
	// GapReasonStitchedAd excludes Twitch stitched-ad segments from content loss.
	GapReasonStitchedAd GapReason = "stitched-ad"

	// GapReasonFetchFailure records a fetch failure accepted by gap policy.
	GapReasonFetchFailure GapReason = "fetch_failure_after_retries"

	// GapReasonAuth remains refetchable after playback-token renewal.
	GapReasonAuth GapReason = "auth_error"

	// GapReasonRestartWindowRolled preserves restoration gaps exempt from capture policy.
	GapReasonRestartWindowRolled GapReason = "restart_window_rolled"

	// GapReasonWindowRolled records playlist-window loss accepted by capture policy.
	GapReasonWindowRolled GapReason = "window_rolled"

	// GapReasonRefetchExpired records requested retries that permanently left the playlist.
	GapReasonRefetchExpired GapReason = "refetch_expired"

	// GapReasonMalformed records invalid manifest segments that cannot be refetched.
	GapReasonMalformed GapReason = "malformed"
)

// Gap describes an inclusive lost-media sequence range; equal endpoints mean one segment.
type Gap struct {
	MediaSeq    int64     `json:"media_seq"`
	EndMediaSeq int64     `json:"end_media_seq"`
	Reason      GapReason `json:"reason"`
}

// CompletedSegmentAccounting delays byte and duration totals until a completed segment reaches
// the frontier.
type CompletedSegmentAccounting struct {
	MediaSeq        int64   `json:"media_seq"`
	Bytes           int64   `json:"bytes"`
	DurationSeconds float64 `json:"duration_seconds,omitempty"`
}

// ResumeState is the execution's durable recording checkpoint.
// Only the attempt goroutine may mutate it; persistence must verify execution ownership.
type ResumeState struct {
	CaptureStoppedAt *time.Time    `json:"capture_stopped_at,omitempty"`
	PreparedPart     *PreparedPart `json:"prepared_part,omitempty"`
	Stage            Stage         `json:"stage"`

	// PosterURL is fetched only after admission to avoid objects orphaned by queue removal.
	PosterURL string `json:"poster_url,omitempty"`

	// CaptureError seals an interrupted capture for remux/store on restart.
	// Once set, no new segments are acquired and completion remains FAILED/partial.
	CaptureError string `json:"capture_error,omitempty"`
	// CaptureRetryable preserves the archive retry decision after the typed cause is lost.
	CaptureRetryable bool `json:"capture_retryable,omitempty"`

	// CurrentPartIndex starts at 1 and advances when a media part is finalized.
	CurrentPartIndex int32 `json:"current_part_index"`

	// EmptySplitReanchors counts splits without media toward the discontinuity cap;
	// those attempts consume no part number.
	EmptySplitReanchors int32 `json:"empty_split_reanchors,omitempty"`

	// SelectedQuality, SelectedFPS, and SelectedCodec stay fixed within a part.
	SelectedQuality string   `json:"selected_quality,omitempty"`
	SelectedFPS     *float64 `json:"selected_fps,omitempty"`
	SelectedCodec   string   `json:"selected_codec,omitempty"`
	SegmentFormat   string   `json:"segment_format,omitempty"`

	// MaxHeight preserves the exact rendition ceiling because the video stores only its tier.
	MaxHeight int `json:"max_height,omitempty"`

	// PartStartMediaSequence is the playlist sequence at which this part begins.
	PartStartMediaSequence int64 `json:"part_start_media_sequence"`

	// PartStarted distinguishes a part anchored at sequence zero from an uninitialized part.
	PartStarted bool `json:"part_started,omitempty"`

	// AccountedFrontierMediaSeq covers contiguous committed or explicitly gapped sequences
	// from PartStartMediaSequence; it never decreases within a part.
	AccountedFrontierMediaSeq int64 `json:"accounted_frontier_media_sequence"`

	// CompletedAboveFrontier contains sorted committed sequences awaiting lower outcomes.
	CompletedAboveFrontier []int64 `json:"completed_above_frontier,omitempty"`

	// CompletedAboveFrontierAccounting carries buffered byte/duration metrics during JSON
	// serialization; absent metrics in older checkpoints contribute zero on recovery.
	CompletedAboveFrontierAccounting []CompletedSegmentAccounting `json:"completed_above_frontier_accounting,omitempty"`

	// EndListSeen records durable completion of acquisition; a pending threshold split
	// also requires proof that ENDLIST fell at its sealed boundary.
	EndListSeen bool `json:"endlist_seen,omitempty"`

	// Gaps records accepted losses and refetchable authorization failures; successful
	// refetches remove their single gaps, and terminal outcomes replace authorization reasons.
	Gaps []Gap `json:"gaps,omitempty"`

	// Path fields are relative to WorkDir where possible and support recovery verification.
	InitSegmentPath   string `json:"init_segment_path,omitempty"`
	PreparedInputPath string `json:"prepared_input_path,omitempty"`
	RemuxOutputPath   string `json:"remux_output_path,omitempty"`
	FinalVideoPath    string `json:"final_video_path,omitempty"`

	// PendingSplit persists the intent to continue in another part after publication.
	PendingSplit bool `json:"pending_split,omitempty"`

	// HadWindowRoll preserves content-loss classification across parts whose gap logs reset.
	HadWindowRoll bool `json:"had_window_roll,omitempty"`

	// PartBytes and PartDurationSeconds count contiguous committed bytes and EXTINF seconds
	// for split thresholds; recovery retains them and each new part resets them.
	PartBytes           int64   `json:"part_bytes,omitempty"`
	PartDurationSeconds float64 `json:"part_duration_seconds,omitempty"`

	// PendingThresholdSplit keeps the same rendition and continues at the sealed boundary;
	// other split types re-anchor because rendition sequence spaces can differ.
	PendingThresholdSplit bool `json:"pending_threshold_split,omitempty"`

	// PendingSplitBoundaryMediaSeq is the last sequence owned by a threshold-split part;
	// recovery prunes higher files and resumes the continuation at boundary+1.
	PendingSplitBoundaryMediaSeq int64 `json:"pending_split_boundary_media_seq,omitempty"`
	PendingSplitBoundarySet      bool  `json:"pending_split_boundary_set,omitempty"`

	// PendingSplitEndListAtBoundary proves ENDLIST did not extend past the sealed boundary;
	// EndListSeen alone cannot exclude unresolved continuation media.
	PendingSplitEndListAtBoundary bool `json:"pending_split_endlist_at_boundary,omitempty"`

	CheckpointAt time.Time `json:"checkpoint_at"`

	// resolvedAbove is rebuilt by Init and contains committed or gapped sequences above the frontier.
	resolvedAbove map[int64]bool

	// completedAccounting stores buffered commit metrics; Init restores it and MarshalJSON
	// serializes it without maintaining a second sorted slice during capture.
	completedAccounting map[int64]CompletedSegmentAccounting
}

// NewResumeState returns an initialized checkpoint at AUTH for part 1.
func NewResumeState() *ResumeState {
	return &ResumeState{
		Stage:               StageAuth,
		CurrentPartIndex:    1,
		CheckpointAt:        time.Now().UTC(),
		resolvedAbove:       map[int64]bool{},
		completedAccounting: map[int64]CompletedSegmentAccounting{},
	}
}

// Init rebuilds accounting after decoding and is safe to call repeatedly.
// Call it before recording outcomes unless using UnmarshalResumeState.
func (r *ResumeState) Init() {
	if r.resolvedAbove == nil {
		r.resolvedAbove = map[int64]bool{}
	} else {
		clear(r.resolvedAbove)
	}
	for _, s := range r.CompletedAboveFrontier {
		if s > r.AccountedFrontierMediaSeq {
			r.resolvedAbove[s] = true
		}
	}
	if r.completedAccounting == nil {
		r.completedAccounting = map[int64]CompletedSegmentAccounting{}
	} else {
		clear(r.completedAccounting)
	}
	for _, m := range r.CompletedAboveFrontierAccounting {
		if m.MediaSeq <= r.AccountedFrontierMediaSeq {
			continue
		}
		if _, found := slices.BinarySearch(r.CompletedAboveFrontier, m.MediaSeq); !found {
			continue
		}
		r.completedAccounting[m.MediaSeq] = m
	}
	r.CompletedAboveFrontierAccounting = nil
	for _, g := range r.Gaps {
		end := max(g.EndMediaSeq, g.MediaSeq)
		for s := g.MediaSeq; s <= end; s++ {
			if s > r.AccountedFrontierMediaSeq {
				r.resolvedAbove[s] = true
			}
		}
	}
}

// SetStage updates the in-memory checkpoint; the caller must persist it.
func (r *ResumeState) SetStage(s Stage) {
	r.Stage = s
	r.CheckpointAt = time.Now().UTC()
}

// StartPart anchors a fresh part at partStart and clears its per-part accounting.
func (r *ResumeState) StartPart(partStart int64) {
	r.PartStartMediaSequence = partStart
	r.PartStarted = true
	r.AccountedFrontierMediaSeq = partStart - 1
	r.resetPerPartAccounting()
	r.Gaps = nil
	r.PendingSplitBoundaryMediaSeq = 0
	r.PendingSplitBoundarySet = false
	r.PendingSplitEndListAtBoundary = false
}

// MaxDiscontinuityPartsPerVideo bounds repeated rendition changes that could create
// unbounded part rows; intentional threshold splits use a separate cap.
const MaxDiscontinuityPartsPerVideo int32 = 32

// DefaultMaxThresholdPartsPerVideo bounds intentional size/duration splitting.
const DefaultMaxThresholdPartsPerVideo int32 = 1024

// ShouldOpenNextPart reports whether pending continuation fits its split cap.
// Nonpositive caps are unlimited; exceeding a cap returns an error.
func (r *ResumeState) ShouldOpenNextPart(maxDiscontinuityParts, maxThresholdParts int32) (bool, error) {
	if !r.PendingSplit {
		return false, nil
	}
	maxParts := maxDiscontinuityParts
	if r.PendingThresholdSplit {
		maxParts = maxThresholdParts
	}
	if maxParts <= 0 {
		return true, nil
	}
	attemptIndex := r.CurrentPartIndex
	if !r.PendingThresholdSplit {
		attemptIndex += r.EmptySplitReanchors
	}
	if attemptIndex >= maxParts {
		return false, fmt.Errorf("split loop exceeded %d part attempts; aborting to prevent runaway", maxParts)
	}
	return true, nil
}

// BeginNewPart consumes a discontinuity split and clears the rendition and sequence anchor.
// Twitch renditions have independent sequence spaces; HadWindowRoll survives the change.
func (r *ResumeState) BeginNewPart() {
	r.CurrentPartIndex++
	r.PartStartMediaSequence = 0
	r.PartStarted = false
	r.AccountedFrontierMediaSeq = 0
	r.resetPerPartAccounting()
	r.SelectedQuality = ""
	r.SelectedFPS = nil
	r.SelectedCodec = ""
	r.SegmentFormat = ""
	// A new rendition/window means capture continues, so the previous ENDLIST is stale.
	r.EndListSeen = false
	r.ClearPendingSplit()
	r.SetStage(StageAuth)
}

// ReanchorCurrentPartAfterEmptySplit consumes a split without incrementing the part number;
// only attempts with committed media own part rows.
func (r *ResumeState) ReanchorCurrentPartAfterEmptySplit() {
	r.EmptySplitReanchors++
	r.PartStartMediaSequence = 0
	r.PartStarted = false
	r.AccountedFrontierMediaSeq = 0
	r.resetPerPartAccounting()
	r.Gaps = nil
	r.SelectedQuality = ""
	r.SelectedFPS = nil
	r.SelectedCodec = ""
	r.SegmentFormat = ""
	// The new window must establish its own ENDLIST before capture can finish.
	r.EndListSeen = false
	r.ClearPendingSplit()
	r.SetStage(StageAuth)
}

// ContinuePart consumes a threshold split and retains the rendition and sequence space.
// The next part begins at the sealed boundary+1 to avoid duplicating earlier media.
func (r *ResumeState) ContinuePart() {
	end := r.AccountedFrontierMediaSeq
	if r.PendingThresholdSplit && r.PendingSplitBoundarySet {
		end = r.PendingSplitBoundaryMediaSeq
	}
	r.CurrentPartIndex++
	r.PartStartMediaSequence = end + 1
	r.PartStarted = true
	r.AccountedFrontierMediaSeq = end
	r.resetPerPartAccounting()
	r.Gaps = nil
	if r.PendingThresholdSplit && !r.PendingSplitEndListAtBoundary {
		r.EndListSeen = false
	}
	r.ClearPendingSplit()
	r.SetStage(StageAuth)
}

func (r *ResumeState) resetPerPartAccounting() {
	r.PreparedPart = nil
	r.CompletedAboveFrontier = nil
	r.CompletedAboveFrontierAccounting = nil
	r.PartBytes = 0
	r.PartDurationSeconds = 0
	if r.resolvedAbove == nil {
		r.resolvedAbove = map[int64]bool{}
	} else {
		clear(r.resolvedAbove)
	}
	if r.completedAccounting == nil {
		r.completedAccounting = map[int64]CompletedSegmentAccounting{}
	} else {
		clear(r.completedAccounting)
	}
}

// NoteCommitted records a saved sequence and removes its matching single gap.
// It preserves range gaps and advances only through contiguous resolved outcomes.
func (r *ResumeState) NoteCommitted(seq int64) {
	r.NoteCommittedSegment(seq, 0, 0)
}

// NoteCommittedSegment records saved bytes and EXTINF seconds when their sequence reaches
// the frontier; refetched single gaps already below the frontier count immediately.
func (r *ResumeState) NoteCommittedSegment(seq int64, bytes int64, durationSeconds float64) {
	r.noteCommittedSegmentUntilThreshold(seq, bytes, durationSeconds, 0, 0)
}

// NoteCommittedSegmentUntilThreshold stops at the first contiguous sequence reaching
// a size/duration ceiling and returns its boundary; higher commits remain buffered.
func (r *ResumeState) NoteCommittedSegmentUntilThreshold(seq int64, bytes int64, durationSeconds float64, maxBytes int64, maxSeconds int) (int64, bool) {
	return r.noteCommittedSegmentUntilThreshold(seq, bytes, durationSeconds, maxBytes, maxSeconds)
}

func (r *ResumeState) noteCommittedSegmentUntilThreshold(seq int64, bytes int64, durationSeconds float64, maxBytes int64, maxSeconds int) (int64, bool) {
	removedGap := r.deleteSingleGap(seq)
	if seq <= r.AccountedFrontierMediaSeq {
		if removedGap {
			r.PartBytes += bytes
			r.PartDurationSeconds += durationSeconds
			if thresholdLimitReached(r.PartBytes, r.PartDurationSeconds, maxBytes, maxSeconds) {
				return r.AccountedFrontierMediaSeq, true
			}
		}
		return 0, false
	}
	r.resolvedAbove[seq] = true
	r.insertCompleted(seq)
	r.recordCompletedAccounting(CompletedSegmentAccounting{
		MediaSeq:        seq,
		Bytes:           bytes,
		DurationSeconds: durationSeconds,
	})
	return r.advanceUntilThreshold(maxBytes, maxSeconds)
}

// NoteGap records an accepted single-sequence gap and advances the frontier.
// A terminal reason replaces an earlier authorization gap; committed history is unchanged.
func (r *ResumeState) NoteGap(seq int64, reason GapReason) {
	r.noteGapUntilThreshold(seq, reason, 0, 0)
}

// NoteGapUntilThreshold returns the first threshold crossing made contiguous by the gap.
// Resolving an authorization gap also checks metrics already folded below the frontier.
func (r *ResumeState) NoteGapUntilThreshold(seq int64, reason GapReason, maxBytes int64, maxSeconds int) (int64, bool) {
	return r.noteGapUntilThreshold(seq, reason, maxBytes, maxSeconds)
}

func (r *ResumeState) noteGapUntilThreshold(seq int64, reason GapReason, maxBytes int64, maxSeconds int) (int64, bool) {
	resolvedAuth := false
	if reason != GapReasonAuth {
		for i := range r.Gaps {
			gap := &r.Gaps[i]
			if gap.MediaSeq == seq && gap.EndMediaSeq == seq && gap.Reason == GapReasonAuth {
				gap.Reason = reason
				resolvedAuth = true
			}
		}
	}
	if seq <= r.AccountedFrontierMediaSeq {
		if resolvedAuth {
			// Authentication advanced the frontier without sealing a refetchable hole.
			return r.AccountedFrontierMediaSeq, thresholdLimitReached(r.PartBytes, r.PartDurationSeconds, maxBytes, maxSeconds)
		}
		return 0, false
	}
	if !r.resolvedAbove[seq] {
		r.resolvedAbove[seq] = true
		r.Gaps = append(r.Gaps, Gap{
			MediaSeq:    seq,
			EndMediaSeq: seq,
			Reason:      reason,
		})
	}
	return r.advanceUntilThreshold(maxBytes, maxSeconds)
}

// NoteRangeGap records inclusive loss and advances the frontier.
// It ignores inverted ranges and trims overlap with already-accounted history.
func (r *ResumeState) NoteRangeGap(start, end int64, reason GapReason) {
	r.noteRangeGapUntilThreshold(start, end, reason, 0, 0)
}

// NoteRangeGapUntilThreshold returns the first threshold crossing made contiguous by a
// range gap, allowing recovery to seal before consuming every buffered commit.
func (r *ResumeState) NoteRangeGapUntilThreshold(start, end int64, reason GapReason, maxBytes int64, maxSeconds int) (int64, bool) {
	return r.noteRangeGapUntilThreshold(start, end, reason, maxBytes, maxSeconds)
}

func (r *ResumeState) noteRangeGapUntilThreshold(start, end int64, reason GapReason, maxBytes int64, maxSeconds int) (int64, bool) {
	if end < start {
		return 0, false
	}
	if start <= r.AccountedFrontierMediaSeq {
		start = r.AccountedFrontierMediaSeq + 1
	}
	if end < start {
		return 0, false
	}
	// Preserve saved outcomes before frontier advancement discards their individual records.
	gapStart := start
	for s := start; s <= end; s++ {
		if r.resolvedAbove[s] {
			if gapStart < s {
				r.Gaps = append(r.Gaps, Gap{MediaSeq: gapStart, EndMediaSeq: s - 1, Reason: reason})
			}
			gapStart = s + 1
			continue
		}
		r.resolvedAbove[s] = true
	}
	if gapStart <= end {
		r.Gaps = append(r.Gaps, Gap{MediaSeq: gapStart, EndMediaSeq: end, Reason: reason})
	}
	return r.advanceUntilThreshold(maxBytes, maxSeconds)
}

// advanceUntilThreshold folds buffered commit metrics through contiguous outcomes,
// stopping at the first threshold crossing; disabled limits permit the entire run.
func (r *ResumeState) advanceUntilThreshold(maxBytes int64, maxSeconds int) (int64, bool) {
	for {
		next := r.AccountedFrontierMediaSeq + 1
		if !r.resolvedAbove[next] {
			return 0, false
		}
		delete(r.resolvedAbove, next)
		if i, found := slices.BinarySearch(r.CompletedAboveFrontier, next); found {
			if m, ok := r.completedAccounting[next]; ok {
				r.PartBytes += m.Bytes
				r.PartDurationSeconds += m.DurationSeconds
			}
			r.deleteCompletedAccounting(next)
			r.CompletedAboveFrontier = slices.Delete(r.CompletedAboveFrontier, i, i+1)
		}
		r.AccountedFrontierMediaSeq = next
		if thresholdLimitReached(r.PartBytes, r.PartDurationSeconds, maxBytes, maxSeconds) {
			return next, true
		}
	}
}

func thresholdLimitReached(bytes int64, seconds float64, maxBytes int64, maxSeconds int) bool {
	if maxBytes > 0 && bytes >= maxBytes {
		return true
	}
	if maxSeconds > 0 && seconds >= float64(maxSeconds) {
		return true
	}
	return false
}

func (r *ResumeState) insertCompleted(seq int64) {
	i, found := slices.BinarySearch(r.CompletedAboveFrontier, seq)
	if found {
		return
	}
	r.CompletedAboveFrontier = slices.Insert(r.CompletedAboveFrontier, i, seq)
}

func (r *ResumeState) deleteSingleGap(seq int64) bool {
	removed := false
	r.Gaps = slices.DeleteFunc(r.Gaps, func(g Gap) bool {
		if g.MediaSeq == seq && g.EndMediaSeq == seq {
			removed = true
			return true
		}
		return false
	})
	return removed
}

func (r *ResumeState) recordCompletedAccounting(m CompletedSegmentAccounting) {
	if r.completedAccounting == nil {
		r.completedAccounting = map[int64]CompletedSegmentAccounting{}
	}
	r.completedAccounting[m.MediaSeq] = m
}

func (r *ResumeState) deleteCompletedAccounting(seq int64) {
	if r.completedAccounting != nil {
		delete(r.completedAccounting, seq)
	}
}

// SealThresholdSplitBoundary preserves the last sequence owned by this part and discards
// higher checkpoint outcomes so the continuation can refetch them.
func (r *ResumeState) SealThresholdSplitBoundary(boundary int64) {
	r.PendingSplitBoundaryMediaSeq = boundary
	r.PendingSplitBoundarySet = true
	r.CompletedAboveFrontier = slices.DeleteFunc(r.CompletedAboveFrontier, func(seq int64) bool {
		if seq > boundary {
			delete(r.resolvedAbove, seq)
			r.deleteCompletedAccounting(seq)
			return true
		}
		return false
	})
	r.Gaps = trimGapsToBoundary(r.Gaps, boundary)
	for seq := range r.resolvedAbove {
		if seq > boundary {
			delete(r.resolvedAbove, seq)
			r.deleteCompletedAccounting(seq)
		}
	}
}

// ClearPendingSplit consumes split intent while preserving capture accounting.
func (r *ResumeState) ClearPendingSplit() {
	r.PendingSplit = false
	r.PendingThresholdSplit = false
	r.PendingSplitBoundaryMediaSeq = 0
	r.PendingSplitBoundarySet = false
	r.PendingSplitEndListAtBoundary = false
}

func trimGapsToBoundary(gaps []Gap, boundary int64) []Gap {
	out := gaps[:0]
	for _, g := range gaps {
		if g.MediaSeq > boundary {
			continue
		}
		if g.EndMediaSeq > boundary {
			g.EndMediaSeq = boundary
		}
		out = append(out, g)
	}
	return out
}

// AuthGapSeqs returns single authorization gaps still eligible for refetch.
// Successful refetches remove them; accepted permanent loss replaces their reason.
func (r *ResumeState) AuthGapSeqs() []int64 {
	if len(r.Gaps) == 0 {
		return nil
	}
	var out []int64
	for _, g := range r.Gaps {
		if g.Reason == GapReasonAuth && g.MediaSeq == g.EndMediaSeq {
			out = append(out, g.MediaSeq)
		}
	}
	return out
}

// SkipSet returns a mutable copy of completed-above-frontier and gap sequences.
// Callers must separately exclude sequences at or below AccountedFrontierMediaSeq.
func (r *ResumeState) SkipSet() map[int64]bool {
	skip := make(map[int64]bool, len(r.CompletedAboveFrontier)+len(r.Gaps))
	for _, s := range r.CompletedAboveFrontier {
		skip[s] = true
	}
	for _, g := range r.Gaps {
		end := max(g.EndMediaSeq, g.MediaSeq)
		for s := g.MediaSeq; s <= end; s++ {
			skip[s] = true
		}
	}
	return skip
}

// ShouldSkip reports whether a sequence is committed, recorded as a gap, or below the frontier.
func (r *ResumeState) ShouldSkip(seq int64) bool {
	if seq <= r.AccountedFrontierMediaSeq {
		return true
	}
	if _, found := slices.BinarySearch(r.CompletedAboveFrontier, seq); found {
		return true
	}
	for _, g := range r.Gaps {
		end := max(g.EndMediaSeq, g.MediaSeq)
		if seq >= g.MediaSeq && seq <= end {
			return true
		}
	}
	return false
}

// MarshalJSON refreshes CheckpointAt and serializes buffered accounting with the checkpoint.
func (r *ResumeState) MarshalJSON() ([]byte, error) {
	r.CheckpointAt = time.Now().UTC()
	type shadow ResumeState
	out := shadow(*r)
	out.CompletedAboveFrontierAccounting = nil
	if len(r.completedAccounting) > 0 {
		for _, seq := range r.CompletedAboveFrontier {
			if m, ok := r.completedAccounting[seq]; ok {
				out.CompletedAboveFrontierAccounting = append(out.CompletedAboveFrontierAccounting, m)
			}
		}
	}
	return json.Marshal(out)
}

// UnmarshalResumeState reads the current persisted shape. SQL migrations own
// upgrades; malformed or missing checkpoint structure is a domain failure.
func UnmarshalResumeState(data []byte) (*ResumeState, error) {
	var r ResumeState
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("resume state unmarshal: %w", err)
	}
	if _, ok := stageOrder[r.Stage]; !ok || r.CurrentPartIndex < 1 {
		return nil, fmt.Errorf("invalid recording checkpoint stage or part index")
	}
	r.Init()
	return &r, nil
}
