package downloader

import "slices"

// resolvedSeqs excludes saved outcomes above the cursor from acquisition and new loss.
func (r *ResumeState) resolvedSeqs() []int64 {
	if !r.PartStarted {
		return nil
	}
	resolved := make(map[int64]bool, len(r.CompletedAboveFrontier))
	for _, seq := range r.CompletedAboveFrontier {
		if seq > r.AccountedFrontierMediaSeq {
			resolved[seq] = true
		}
	}
	for _, gap := range r.Gaps {
		if gap.Reason == GapReasonAuth {
			continue
		}
		end := max(gap.MediaSeq, gap.EndMediaSeq)
		for seq := max(gap.MediaSeq, r.AccountedFrontierMediaSeq+1); seq <= end; seq++ {
			resolved[seq] = true
		}
	}
	seqs := make([]int64, 0, len(resolved))
	for seq := range resolved {
		seqs = append(seqs, seq)
	}
	slices.Sort(seqs)
	return seqs
}

type policyGapRange struct {
	from, to int64
}

// policyCounts restores per-part loss policy without seeding attempt bytes or progress.
func (r *ResumeState) policyCounts() (done, lost int64) {
	if !r.PartStarted {
		return 0, 0
	}
	last := r.AccountedFrontierMediaSeq
	for _, gap := range r.Gaps {
		last = max(last, gap.MediaSeq, gap.EndMediaSeq)
	}
	all := policyGapRanges(r.Gaps, r.PartStartMediaSequence, r.AccountedFrontierMediaSeq, false)
	permanent := policyGapRanges(r.Gaps, r.PartStartMediaSequence, last, true)
	done = max(r.AccountedFrontierMediaSeq-r.PartStartMediaSequence+1, 0)
	for _, gap := range all {
		done -= gap.to - gap.from + 1
	}
	for _, gap := range permanent {
		lost += gap.to - gap.from + 1
	}
	completed := make(map[int64]bool, len(r.CompletedAboveFrontier))
	for _, seq := range r.CompletedAboveFrontier {
		if seq < r.PartStartMediaSequence || seq <= r.AccountedFrontierMediaSeq || completed[seq] {
			continue
		}
		completed[seq] = true
		done++
		// Explicit saved media wins over an overlapping range reported by the playlist.
		for _, gap := range permanent {
			if seq >= gap.from && seq <= gap.to {
				lost--
				break
			}
		}
	}
	return done, lost
}

func policyGapRanges(gaps []Gap, first, last int64, permanentOnly bool) []policyGapRange {
	var ranges []policyGapRange
	for _, gap := range gaps {
		// Legacy window gaps do not identify whether restoration exempted them from policy.
		if permanentOnly && (gap.Reason == GapReasonAuth || gap.Reason == GapReasonStitchedAd || gap.Reason == GapReasonRestartWindowRolled) {
			continue
		}
		from, to := max(first, gap.MediaSeq), min(last, max(gap.MediaSeq, gap.EndMediaSeq))
		if from <= to {
			ranges = append(ranges, policyGapRange{from, to})
		}
	}
	slices.SortFunc(ranges, func(a, b policyGapRange) int {
		if a.from < b.from {
			return -1
		}
		if a.from > b.from {
			return 1
		}
		return 0
	})
	merged := ranges[:0]
	for _, gap := range ranges {
		if len(merged) > 0 && gap.from <= merged[len(merged)-1].to {
			merged[len(merged)-1].to = max(merged[len(merged)-1].to, gap.to)
		} else {
			merged = append(merged, gap)
		}
	}
	return merged
}
