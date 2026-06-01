package downloader

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"
)

// Stage is the durable pipeline stage recorded in ResumeState so a
// server restart knows where to pick up. Values match the spec's
// "Resume on restart" section — do not invent new stages without
// also updating the startup dispatch in Service.Resume.
type Stage string

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

// stageOrder maps each stage to its forward position in the
// pipeline. Used for ordering comparisons; string comparison on
// Stage values is alphabetical and doesn't reflect pipeline order
// ("PREPARE_INPUT" < "SEGMENTS" alphabetically but runs after).
//
// Unknown stages (future additions or corrupted JSON) map to 0
// via the zero-value default, which conservatively re-runs from
// the start of the pipeline.
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

// AtOrAfter reports whether s is at or past `target` in the
// pipeline. Resume paths use this to decide whether a stored
// checkpoint is already past a given stage boundary (e.g. "can
// we skip Stages 1-4 because we're already past SEGMENTS?").
func (s Stage) AtOrAfter(target Stage) bool {
	return stageOrder[s] >= stageOrder[target]
}

// GapReason is a stable identifier for why a segment entered gaps[].
// The resume logic never interprets reasons — they're for operator
// log review and UI surfacing. New reasons are additive; don't
// rename existing ones (they're on-disk strings in the JSONB column).
type GapReason string

const (
	// GapReasonStitchedAd: segment was inside a Twitch stitched-ad
	// pod (EXT-X-DATERANGE CLASS="twitch-stitched-ad"). Not a
	// fetch failure — the segment was never enqueued.
	GapReasonStitchedAd GapReason = "stitched-ad"

	// GapReasonFetchFailure: the worker exhausted its retry budget
	// on a transport / CDN error and the gap policy accepted the
	// loss rather than aborting the job.
	GapReasonFetchFailure GapReason = "fetch_failure_after_retries"

	// GapReasonAuth: the worker hit a 401/403 on a segment fetch.
	// The job is terminating via auth-refresh escalation; the seq
	// is marked gapped in the current attempt's state so the next
	// attempt's StartMediaSeq skips past it.
	GapReasonAuth GapReason = "auth_error"

	// GapReasonRestartWindowRolled: on resume, the playlist head
	// was already past the saved frontier. Covers a range
	// [frontier+1, playlistHead-1] in a single gap entry.
	GapReasonRestartWindowRolled GapReason = "restart_window_rolled"

	// GapReasonMalformed: poller filtered a segment with
	// invariant-violating metadata (EXTINF <= 0) before any
	// fetch attempt. Distinct from GapReasonFetchFailure —
	// no CDN or transport involvement; the defect is in the
	// manifest itself. Not refetched.
	GapReasonMalformed GapReason = "malformed"
)

// Gap is one entry in ResumeState.Gaps. Covers either a single
// MediaSeq (MediaSeq == EndMediaSeq) or an inclusive range
// (MediaSeq < EndMediaSeq) for restart-window-rolled losses.
//
// Wire format deviates from the spec's mixed {media_seq} /
// {media_seq_range:[a,b]} union: this package always emits both
// fields so reader code has one shape to parse. Spec example JSON
// is illustrative; no external consumer reads this blob except the
// same code that wrote it.
type Gap struct {
	MediaSeq    int64     `json:"media_seq"`
	EndMediaSeq int64     `json:"end_media_seq"`
	Reason      GapReason `json:"reason"`
}

// CompletedSegmentAccounting carries byte/duration metadata for a
// committed segment that finished above the contiguous frontier. The
// seq itself is still listed in CompletedAboveFrontier for the
// historical resume contract; this parallel field lets threshold
// accounting add bytes/seconds only when that seq becomes contiguous.
type CompletedSegmentAccounting struct {
	MediaSeq        int64   `json:"media_seq"`
	Bytes           int64   `json:"bytes"`
	DurationSeconds float64 `json:"duration_seconds,omitempty"`
}

// ResumeState is the durable per-job checkpoint stored in
// jobs.resume_state as JSON. Serialized verbatim; on restart the
// downloader reads one row, unmarshals into ResumeState, calls
// Init(), and dispatches on Stage.
//
// Mutation contract: only the run goroutine writes. Writes are
// through repository.UpdateJobResumeState, which is a single
// UPDATE — no history, no CAS. Writes after material state
// transitions: stage change, segment outcome, gap accepted.
type ResumeState struct {
	Stage Stage `json:"stage"`

	// CurrentPartIndex starts at 1 and increments on each
	// variant/codec/container split.
	CurrentPartIndex int32 `json:"current_part_index"`

	// EmptySplitReanchors counts split signals that occurred after a
	// prior real part but before the current part committed any media.
	// Those intervals do not get video_parts rows, so CurrentPartIndex
	// intentionally stays dense; this counter still makes repeated
	// no-output discontinuities advance toward the runaway split cap.
	EmptySplitReanchors int32 `json:"empty_split_reanchors,omitempty"`

	// Selected* are sticky within a part. Tracked so a restart
	// rebuilds the right ffmpeg input shape without re-walking
	// Stage 3.
	SelectedQuality string   `json:"selected_quality,omitempty"`
	SelectedFPS     *float64 `json:"selected_fps,omitempty"`
	SelectedCodec   string   `json:"selected_codec,omitempty"`
	SegmentFormat   string   `json:"segment_format,omitempty"`

	// PartStartMediaSequence is the first MediaSeq of the
	// current part — anchored from the playlist's
	// EXT-X-MEDIA-SEQUENCE base on the first poll.
	PartStartMediaSequence int64 `json:"part_start_media_sequence"`

	// PartStarted distinguishes a real HLS part anchored at media
	// sequence 0 from the zero-valued post-BeginNewPart state. HLS
	// playlists commonly start at 0, so PartStartMediaSequence alone
	// cannot be used as a presence bit.
	PartStarted bool `json:"part_started,omitempty"`

	// AccountedFrontierMediaSeq: every seq in
	// [PartStartMediaSequence, this] is resolved — either on
	// disk (in CompletedAboveFrontier's consumed history) or
	// explicitly recorded in Gaps. Never decreases.
	AccountedFrontierMediaSeq int64 `json:"accounted_frontier_media_sequence"`

	// CompletedAboveFrontier holds successfully-committed seqs
	// that arrived out-of-order (above the current frontier).
	// Bounded by concurrent-worker count in practice — as the
	// frontier advances past them, they get trimmed. Kept
	// sorted ascending.
	CompletedAboveFrontier []int64 `json:"completed_above_frontier,omitempty"`

	// CompletedAboveFrontierAccounting is the serialized form of
	// completedAccounting. It is rebuilt from the map during
	// MarshalJSON and is not maintained on the segment hot path.
	// Older checkpoints may have CompletedAboveFrontier without this
	// detail; those entries resume safely with zero metric contribution
	// instead of re-triggering a split at a non-contiguous boundary.
	CompletedAboveFrontierAccounting []CompletedSegmentAccounting `json:"completed_above_frontier_accounting,omitempty"`

	// EndListSeen is true once the playlist returned
	// EXT-X-ENDLIST. On resume with EndListSeen=true + stage=
	// SEGMENTS, the orchestrator knows segment fetching is
	// complete and can skip ahead to PrepareInput.
	EndListSeen bool `json:"endlist_seen,omitempty"`

	// Gaps is the append-only record of tolerated losses for
	// operator/UI review. The resume path uses this set to build
	// the "do not re-fetch" filter: anything in Gaps was already
	// decided, retrying turns a tolerant success into a hard
	// failure.
	Gaps []Gap `json:"gaps,omitempty"`

	// Path fields are WorkDir-relative where possible; Restart
	// uses them to verify on-disk state matches the checkpoint.
	InitSegmentPath   string `json:"init_segment_path,omitempty"`
	PreparedInputPath string `json:"prepared_input_path,omitempty"`
	RemuxOutputPath   string `json:"remux_output_path,omitempty"`
	FinalVideoPath    string `json:"final_video_path,omitempty"`

	// PendingSplit is true between fetchWithAuthRefresh signalling
	// a variant-loss split and BeginNewPart consuming it.
	// Persisted so a crash mid-runPart still drives part N+1 on
	// resume — without it the loop would complete the in-flight
	// part and exit, leaving the next part unrun.
	PendingSplit bool `json:"pending_split,omitempty"`

	// HadWindowRoll tracks whether ANY part observed a CDN window-
	// roll across the recording. The terminal completion_kind
	// classifier reads this; reading Gaps directly only sees the
	// LAST part because BeginNewPart wipes per-part aux state.
	// Persisted so a crash mid-part-N preserves the signal from
	// parts 1..N-1.
	HadWindowRoll bool `json:"had_window_roll,omitempty"`

	// PartBytes and PartDurationSeconds accumulate the current
	// part's committed segment bytes and EXTINF seconds. They drive
	// the size/duration part-split threshold (Download.MaxPartBytes /
	// MaxPartSeconds). Durable so a crash mid-part resumes with the
	// ceiling intact rather than restarting the count from zero and
	// overshooting it — committed work before the crash still counts
	// toward the boundary. Reset to zero on every part boundary
	// (StartPart, BeginNewPart, ContinuePart).
	PartBytes           int64   `json:"part_bytes,omitempty"`
	PartDurationSeconds float64 `json:"part_duration_seconds,omitempty"`

	// PendingThresholdSplit qualifies PendingSplit: it marks that the
	// pending split was triggered by the size/duration ceiling rather
	// than a variant loss. The two open the next part differently —
	// a threshold split is a clean cut in ONE continuous stream, so
	// ContinuePart carries the frontier forward (part N+1 starts at
	// endSeq+1, no gap, no re-fetch, variant lock retained); a
	// variant / playlist-gone / window-roll split re-anchors from
	// scratch (BeginNewPart) because the new variant owns an
	// independent MEDIA-SEQUENCE counter. Durable so a crash between
	// the split checkpoint and the next part's open resumes down the
	// right path.
	PendingThresholdSplit bool `json:"pending_threshold_split,omitempty"`

	// PendingSplitBoundaryMediaSeq is set for PendingThresholdSplit
	// and records the exact final media sequence that belongs to the
	// current part. It is persisted at the same checkpoint as
	// PendingSplit so a crash while still in SEGMENTS can skip fetch,
	// prune any above-boundary in-flight segment files, finalize this
	// part, and continue at boundary+1.
	PendingSplitBoundaryMediaSeq int64 `json:"pending_split_boundary_media_seq,omitempty"`
	PendingSplitBoundarySet      bool  `json:"pending_split_boundary_set,omitempty"`

	// PendingSplitEndListAtBoundary is set only when an actual HLS run
	// observed EXT-X-ENDLIST and its final observed media sequence was
	// not beyond PendingSplitBoundaryMediaSeq. It is the durable proof
	// that a pending threshold split ended exactly at the boundary. A
	// bare EndListSeen=true is not enough for threshold resumes because
	// older checkpoints could have recorded ENDLIST while post-boundary
	// tail segments still needed to be refetched by the continuation
	// part.
	PendingSplitEndListAtBoundary bool `json:"pending_split_endlist_at_boundary,omitempty"`

	CheckpointAt time.Time `json:"checkpoint_at"`

	// resolvedAbove is an in-memory acceleration structure for
	// frontier-advance. Holds every seq > frontier that is
	// resolved (committed or gapped). Rebuilt from the
	// serialized fields by Init() after Unmarshal.
	resolvedAbove map[int64]bool

	// completedAccounting is the in-memory map form of
	// CompletedAboveFrontierAccounting. Rebuilt by Init and serialized
	// by MarshalJSON; kept off the exported slice while running so
	// segment events avoid a second sorted-slice insert/delete.
	completedAccounting map[int64]CompletedSegmentAccounting
}

// NewResumeState returns a fresh checkpoint ready for a Stage-1
// start. Callers set Stage via SetStage once pipeline work begins.
func NewResumeState() *ResumeState {
	return &ResumeState{
		Stage:               StageAuth,
		CurrentPartIndex:    1,
		CheckpointAt:        time.Now().UTC(),
		resolvedAbove:       map[int64]bool{},
		completedAccounting: map[int64]CompletedSegmentAccounting{},
	}
}

// Init rebuilds the in-memory aux structure after a JSON
// unmarshal. Callers deserializing from jobs.resume_state MUST
// call Init before using the Note* or Advance methods.
//
// Idempotent: safe to call repeatedly, safe to call on a
// fresh-constructed state.
func (r *ResumeState) Init() {
	if !r.PartStarted &&
		(r.PartStartMediaSequence != 0 ||
			r.AccountedFrontierMediaSeq != 0 ||
			len(r.CompletedAboveFrontier) > 0 ||
			len(r.Gaps) > 0 ||
			r.Stage.AtOrAfter(StagePrepareInput)) {
		r.PartStarted = true
	}
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

// SetStage updates the durable stage + touches CheckpointAt.
// The caller persists via repository.UpdateJobResumeState.
func (r *ResumeState) SetStage(s Stage) {
	r.Stage = s
	r.CheckpointAt = time.Now().UTC()
}

// StartPart anchors the frontier for a new part. Frontier seeded
// to partStart-1 so the first NoteCommitted(partStart) advances
// cleanly. Called from OnFirstPoll for the first observation of
// any part — fresh job, post-BeginNewPart re-anchor, etc.
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

// MaxDiscontinuityPartsPerVideo bounds a runaway discontinuity split
// loop. Real Twitch streams essentially never flip variants this
// often, but a pathological broadcaster/CDN loop could otherwise
// produce unbounded video_parts rows. Threshold splits use their own
// higher operator-facing cap because max_part_seconds/max_part_bytes
// intentionally create many parts for long recordings.
const MaxDiscontinuityPartsPerVideo int32 = 32

// DefaultMaxThresholdPartsPerVideo is the default cap for intentional
// size/duration splitting. It is high enough for documented
// hour-sized chunks across multi-week recordings while still bounding
// accidental one-segment-per-part configurations.
const DefaultMaxThresholdPartsPerVideo int32 = 1024

// ShouldOpenNextPart drives the outer part loop's continue/exit
// decision off PendingSplit. Reading the durable flag (not a local)
// is the contract that lets a process crash between PendingSplit's
// checkpoint and the next BeginNewPart still re-enter the loop.
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

// BeginNewPart prepares the resume state for the next part after a
// split. Zeroes the per-part anchor (PartStart + frontier) so the
// new variant's OnFirstPoll → StartPart re-anchors from scratch,
// clears the variant lock so Stage 3 picks freely, and consumes
// PendingSplit. Preserves Gaps + HadWindowRoll for cross-part
// completion_kind classification.
//
// Carrying the prior part's last seq forward instead of zeroing
// would make fetchWithAuthRefresh's bootstrapped check stay true,
// the new variant's OnFirstPoll wouldn't re-anchor, and the poller
// would filter out every new-variant segment whose MediaSeq is
// below the carried-over threshold (Twitch doesn't share
// MEDIA-SEQUENCE counters across variants).
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
	// Opening a fresh discontinuity part means the broadcast is still
	// going (a new variant/window), so a stale ENDLIST observation from
	// the part just sealed must not leak forward and mark the recording
	// complete. Mirrors ContinuePart's conditional reset (which keeps it
	// only when the threshold split provably ended at the boundary).
	r.EndListSeen = false
	r.ClearPendingSplit()
	r.SetStage(StageAuth)
}

// ReanchorCurrentPartAfterEmptySplit prepares the current part for a
// fresh variant/window after a split signal produced no committed
// media and therefore no video_parts row. It is BeginNewPart's
// "no output was persisted" twin: reset the HLS anchor and variant
// lock, count one skipped no-output attempt, consume the pending split,
// but do NOT increment CurrentPartIndex. The next successful run owns
// the same part number.
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
	// Same as BeginNewPart: re-anchoring for a fresh variant/window
	// means the broadcast continues, so drop any stale ENDLIST marker.
	r.EndListSeen = false
	r.ClearPendingSplit()
	r.SetStage(StageAuth)
}

// ContinuePart opens the next part after a size/duration-threshold
// split. Unlike BeginNewPart, the stream itself is unchanged: the
// same variant keeps publishing into one continuous MEDIA-SEQUENCE
// space, so part N+1 must pick up at part N's last accounted seq + 1
// — no re-anchor, no gap, no re-fetch.
//
// Carrying the frontier forward (rather than zeroing it like
// BeginNewPart) is load-bearing twice over: it keeps
// fetchWithAuthRefresh's `bootstrapped` check true so the next
// hls.Run resumes at frontier+1 instead of re-emitting the whole
// CDN window (which would duplicate the segments part N already
// committed), and it makes part N+1's StartMediaSeq land exactly one
// past part N's EndMediaSeq — contiguous, no hole.
//
// The variant lock (SelectedQuality/FPS/Codec/SegmentFormat) is
// retained on purpose: the re-resolved signed URL must resolve to the
// same variant, and a genuine variant change at the same instant
// surfaces as ErrVariantChanged on the next resolve. Per-part
// accounting (Gaps, the byte/duration accumulators) resets;
// cross-part signals (HadWindowRoll) are left untouched.
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

// NoteCommitted records a successfully-written segment, advancing
// the frontier when possible. O(log n) insert on the sorted
// CompletedAboveFrontier slice; amortized O(1) advance.
//
// Refetch path: if a prior NoteGap had already recorded this seq
// as a single-seq gap (e.g. GapReasonAuth, pending refetch), the
// gap entry is removed — the segment is on disk now, it's not a
// gap anymore. Range gaps (restart_window_rolled) are NOT touched;
// those aren't individually refetched and their range stays
// documented. If seq is at or below the frontier we only clear
// the gap entry and return, since frontier math already consumed
// the seq as part of the gap-accepted advance.
func (r *ResumeState) NoteCommitted(seq int64) {
	r.NoteCommittedSegment(seq, 0, 0)
}

// NoteCommittedSegment is NoteCommitted plus byte/duration accounting
// for threshold splits. Bytes and duration are only added to
// PartBytes/PartDurationSeconds when the committed seq becomes part of
// the contiguous frontier. This prevents an out-of-order worker from
// tripping a split at seq N+k while lower seqs are still unresolved.
func (r *ResumeState) NoteCommittedSegment(seq int64, bytes int64, durationSeconds float64) {
	r.noteCommittedSegmentUntilThreshold(seq, bytes, durationSeconds, 0, 0)
}

// NoteCommittedSegmentUntilThreshold is NoteCommittedSegment with an
// additional threshold-aware frontier stop. If consuming this commit
// makes one or more previously out-of-order commits contiguous, it
// advances only through the first sequence whose cumulative
// byte/duration total reaches the configured ceiling and returns that
// media sequence as the boundary. Higher contiguous commits remain
// above-frontier so SealThresholdSplitBoundary can drop them from this
// part and the continuation can refetch them.
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

// NoteGap records an accepted gap at a single mediaSeq, advancing
// the frontier. reason is persisted verbatim in the Gap entry.
func (r *ResumeState) NoteGap(seq int64, reason GapReason) {
	r.noteGapUntilThreshold(seq, reason, 0, 0)
}

// NoteGapUntilThreshold is NoteGap with threshold-aware frontier
// advancement. This matters when a lower sequence is skipped after
// higher sequences have already committed out-of-order: consuming the
// gap can make those higher commits contiguous, and the split boundary
// must be the first contiguous commit that reaches the ceiling rather
// than the end of the buffered run.
func (r *ResumeState) NoteGapUntilThreshold(seq int64, reason GapReason, maxBytes int64, maxSeconds int) (int64, bool) {
	return r.noteGapUntilThreshold(seq, reason, maxBytes, maxSeconds)
}

func (r *ResumeState) noteGapUntilThreshold(seq int64, reason GapReason, maxBytes int64, maxSeconds int) (int64, bool) {
	if seq <= r.AccountedFrontierMediaSeq {
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

// NoteRangeGap records an inclusive [start, end] gap and advances
// the frontier. Used by the restart-window-rolled path where a
// whole window of segments is lost in one go; recording per-seq
// gaps would balloon the JSON blob for no gain.
//
// start > end is treated as empty (no-op). start > frontier is
// required; a range that overlaps the already-accounted history
// has its overlap trimmed.
func (r *ResumeState) NoteRangeGap(start, end int64, reason GapReason) {
	r.noteRangeGapUntilThreshold(start, end, reason, 0, 0)
}

// NoteRangeGapUntilThreshold is NoteRangeGap with threshold-aware
// frontier advancement. A resume window roll fills a whole lost range
// in one shot; if that fill makes buffered above-frontier commits
// (carried across a crash) contiguous, their bytes/duration fold into
// the part totals and can cross the size/duration ceiling. Returning
// the first crossing sequence lets OnWindowRoll seal a clean threshold
// boundary here instead of silently overshooting max_part_* until the
// next committed segment trips it (or never, if the part ends first).
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
	r.Gaps = append(r.Gaps, Gap{
		MediaSeq:    start,
		EndMediaSeq: end,
		Reason:      reason,
	})
	for s := start; s <= end; s++ {
		r.resolvedAbove[s] = true
	}
	return r.advanceUntilThreshold(maxBytes, maxSeconds)
}

// advanceUntilThreshold moves AccountedFrontierMediaSeq forward as far
// as the resolved-above set allows, trimming CompletedAboveFrontier and
// folding each newly-contiguous commit's bytes/duration into the part
// totals. It stops and returns (boundary, true) at the first sequence
// whose cumulative total reaches the size/duration ceiling; with both
// maxes 0 (disabled) it advances the whole contiguous run and returns
// (0, false).
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

// insertCompleted adds seq to CompletedAboveFrontier while
// maintaining ascending sort order. No-op on duplicates — the
// caller's resolvedAbove map guards against re-entry.
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

// SealThresholdSplitBoundary records the exact boundary selected by
// a size/duration split and drops any above-boundary accounting from
// the current part. Segment files above this boundary are handled by
// the downloader before remux; the next part refetches them from
// boundary+1 instead of treating them as already resolved.
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

// ClearPendingSplit consumes a pending split intent without otherwise
// changing part accounting. Used when ENDLIST proves there is no
// follow-up part to open and by BeginNewPart/ContinuePart after they
// have applied the split.
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

// AuthGapSeqs returns the MediaSeq values currently in Gaps with
// GapReasonAuth and EndMediaSeq == MediaSeq (single-seq entries
// only). Callers seed refetchSeqs from this on the first auth-
// refresh iteration after a process restart — the prior lifetime
// may have auth-errored on segments whose refetch intent didn't
// survive the crash. A successful refetch clears the entry via
// NoteCommitted; a rolled-off refetch leaves it in place (the
// next restart will attempt it once more, at the cost of one
// wasted poll per gap).
//
// Returns nil when no auth gaps are pending — fresh jobs and
// clean resumes both hit this common path.
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

// SkipSet reports the set of MediaSeq values the resume path must
// NOT re-enqueue. Anything ≤ frontier, anything in
// CompletedAboveFrontier, anything inside a Gap range — all
// already resolved and retrying turns a tolerated outcome into a
// hard failure.
//
// Returned map is a fresh copy; callers can mutate without
// affecting state.
func (r *ResumeState) SkipSet() map[int64]bool {
	skip := make(map[int64]bool, len(r.CompletedAboveFrontier)+len(r.Gaps))
	// The frontier range [PartStart, frontier] is compact and
	// isn't materialized into the map — caller checks seq <=
	// frontier separately. This keeps the map bounded by
	// concurrent-worker count + accepted-gap count rather than
	// by total segment count.
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

// ShouldSkip reports whether the resume path should skip mediaSeq
// — already committed on disk, already accepted as a gap, or
// simply below the accounted frontier.
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

// MarshalJSON emits the fresh CheckpointAt timestamp along with
// the serialized state. CompletedAboveFrontierAccounting is derived
// from the hot-path map here, keeping checkpoint JSON complete without
// maintaining a parallel sorted slice on every segment event.
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

// UnmarshalResumeState decodes a resume_state JSONB blob and
// rebuilds the in-memory aux structures. Empty or `{}` input
// returns a zero-valued state with Init already applied — a
// fresh job with no checkpoint yet.
func UnmarshalResumeState(data []byte) (*ResumeState, error) {
	if len(data) == 0 || string(data) == "{}" {
		return NewResumeState(), nil
	}
	var r ResumeState
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("resume state unmarshal: %w", err)
	}
	r.Init()
	return &r, nil
}
