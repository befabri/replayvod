package hls

import "slices"

type sequenceRange struct {
	from, to int64
}

// sequenceRanges tracks resolved media and loss so overlapping reports cannot count it twice.
type sequenceRanges []sequenceRange

func (ranges sequenceRanges) additional(from, to int64) int64 {
	remaining := to - from + 1
	for _, known := range ranges {
		if known.to < from {
			continue
		}
		if known.from > to {
			break
		}
		remaining -= min(to, known.to) - max(from, known.from) + 1
	}
	return remaining
}

func (ranges *sequenceRanges) add(from, to int64) {
	*ranges = append(*ranges, sequenceRange{from, to})
	slices.SortFunc(*ranges, func(a, b sequenceRange) int {
		if a.from < b.from {
			return -1
		}
		if a.from > b.from {
			return 1
		}
		return 0
	})
	out := (*ranges)[:0]
	for _, next := range *ranges {
		if len(out) > 0 && (next.from <= out[len(out)-1].to || next.from-1 == out[len(out)-1].to) {
			out[len(out)-1].to = max(out[len(out)-1].to, next.to)
		} else {
			out = append(out, next)
		}
	}
	*ranges = out
}
