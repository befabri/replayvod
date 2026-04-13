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

	// CurrentPartIndex starts at 1 and increments when a
	// variant/codec/container switch forces a split. Phase 6g
	// ships single-part recordings; this field is structural
	// headroom for 6f.
	CurrentPartIndex int32 `json:"current_part_index"`

	// Selected* are sticky within a part. Tracked here so a
	// restart knows which ffmpeg input shape to build without
	// re-walking Stage 3's variant-selection logic.
	SelectedQuality string `json:"selected_quality,omitempty"`
	SelectedCodec   string `json:"selected_codec,omitempty"`
	SegmentFormat   string `json:"segment_format,omitempty"`

	// PartStartMediaSequence is the first MediaSeq of the
	// current part. Part 1 starts at whatever the playlist's
	// EXT-X-MEDIA-SEQUENCE base was on the first poll. A new
	// part (Phase 6f) starts from the new variant's first seq.
	PartStartMediaSequence int64 `json:"part_start_media_sequence"`

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

	CheckpointAt time.Time `json:"checkpoint_at"`

	// resolvedAbove is an in-memory acceleration structure for
	// frontier-advance. Holds every seq > frontier that is
	// resolved (committed or gapped). Rebuilt from the
	// serialized fields by Init() after Unmarshal.
	resolvedAbove map[int64]bool
}

// NewResumeState returns a fresh checkpoint ready for a Stage-1
// start. Callers set Stage via SetStage once pipeline work begins.
func NewResumeState() *ResumeState {
	return &ResumeState{
		Stage:            StageAuth,
		CurrentPartIndex: 1,
		CheckpointAt:     time.Now().UTC(),
		resolvedAbove:    map[int64]bool{},
	}
}

// Init rebuilds the in-memory aux structure after a JSON
// unmarshal. Callers deserializing from jobs.resume_state MUST
// call Init before using the Note* or Advance methods.
//
// Idempotent: safe to call repeatedly, safe to call on a
// fresh-constructed state.
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

// StartPart initializes the frontier for a new part starting at
// the given mediaSeq. Sets PartStartMediaSequence and seeds
// AccountedFrontierMediaSeq to partStart-1 so the first
// NoteCommitted(partStart) advances cleanly.
//
// Part 1 is bootstrapped from the orchestrator's observed
// EXT-X-MEDIA-SEQUENCE base. Subsequent parts (Phase 6f) start
// from the new variant's first seq.
func (r *ResumeState) StartPart(partStart int64) {
	r.PartStartMediaSequence = partStart
	r.AccountedFrontierMediaSeq = partStart - 1
	r.CompletedAboveFrontier = nil
	r.Gaps = nil
	if r.resolvedAbove == nil {
		r.resolvedAbove = map[int64]bool{}
	} else {
		clear(r.resolvedAbove)
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
	r.Gaps = slices.DeleteFunc(r.Gaps, func(g Gap) bool {
		return g.MediaSeq == seq && g.EndMediaSeq == seq
	})
	if seq <= r.AccountedFrontierMediaSeq {
		return
	}
	r.resolvedAbove[seq] = true
	r.insertCompleted(seq)
	r.advance()
}

// NoteGap records an accepted gap at a single mediaSeq, advancing
// the frontier. reason is persisted verbatim in the Gap entry.
func (r *ResumeState) NoteGap(seq int64, reason GapReason) {
	if seq <= r.AccountedFrontierMediaSeq {
		return
	}
	if !r.resolvedAbove[seq] {
		r.resolvedAbove[seq] = true
		r.Gaps = append(r.Gaps, Gap{
			MediaSeq:    seq,
			EndMediaSeq: seq,
			Reason:      reason,
		})
	}
	r.advance()
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
	if end < start {
		return
	}
	if start <= r.AccountedFrontierMediaSeq {
		start = r.AccountedFrontierMediaSeq + 1
	}
	if end < start {
		return
	}
	r.Gaps = append(r.Gaps, Gap{
		MediaSeq:    start,
		EndMediaSeq: end,
		Reason:      reason,
	})
	for s := start; s <= end; s++ {
		r.resolvedAbove[s] = true
	}
	r.advance()
}

// advance moves AccountedFrontierMediaSeq forward as far as the
// resolved-above set allows, trimming CompletedAboveFrontier as
// the frontier consumes entries.
func (r *ResumeState) advance() {
	for {
		next := r.AccountedFrontierMediaSeq + 1
		if !r.resolvedAbove[next] {
			return
		}
		delete(r.resolvedAbove, next)
		if i, found := slices.BinarySearch(r.CompletedAboveFrontier, next); found {
			r.CompletedAboveFrontier = slices.Delete(r.CompletedAboveFrontier, i, i+1)
		}
		r.AccountedFrontierMediaSeq = next
	}
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
// the serialized state. Wraps the default marshal rather than
// replacing it — custom logic is limited to the timestamp refresh.
func (r *ResumeState) MarshalJSON() ([]byte, error) {
	r.CheckpointAt = time.Now().UTC()
	type shadow ResumeState
	return json.Marshal((*shadow)(r))
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
