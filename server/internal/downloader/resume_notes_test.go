package downloader

// The unbounded note forms below drive ResumeState in tests. Production always
// records through the UntilThreshold variants, which also report where a size
// or duration ceiling is crossed.

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

// NoteRangeGap records inclusive loss and advances the frontier.
// It ignores inverted ranges and trims overlap with already-accounted history.
func (r *ResumeState) NoteRangeGap(start, end int64, reason GapReason) {
	r.noteRangeGapUntilThreshold(start, end, reason, 0, 0)
}
