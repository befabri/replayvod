package hls

import (
	"math"
	"slices"
	"testing"
)

func TestLostRangesCountsUnionAndCompactsAdjacentSequences(t *testing.T) {
	var ranges sequenceRanges
	for _, span := range []sequenceRange{{50, 99}, {10, 10}, {99, 99}, {70, 70}, {100, 109}, {20, 20}, {0, 9}, {11, 19}} {
		ranges.add(span.from, span.to)
	}
	if want := (sequenceRanges{{0, 20}, {50, 109}}); !slices.Equal(ranges, want) {
		t.Fatalf("loss ranges=%v, want %v", ranges, want)
	}
	if got := ranges.additional(10, 120); got != 40 {
		t.Fatalf("unseen loss=%d, want disjoint spans [21,49] + [110,120]", got)
	}
	for seq := int64(1000); seq < 2000; seq++ {
		ranges.add(seq, seq)
	}
	if len(ranges) != 3 || ranges[2] != (sequenceRange{1000, 1999}) {
		t.Fatalf("consecutive refetch losses did not compact: %v", ranges)
	}
	ranges.add(math.MaxInt64, math.MaxInt64)
	ranges.add(math.MaxInt64-1, math.MaxInt64-1)
	if len(ranges) != 4 || ranges[3] != (sequenceRange{math.MaxInt64 - 1, math.MaxInt64}) || ranges.additional(math.MaxInt64-2, math.MaxInt64) != 1 {
		t.Fatalf("sequence boundary overflowed loss accounting: %v", ranges)
	}
}
