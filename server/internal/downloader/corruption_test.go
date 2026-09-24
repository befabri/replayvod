package downloader

import (
	"testing"

	"github.com/befabri/replayvod/server/internal/downloader/probe"
	"github.com/befabri/replayvod/server/internal/downloader/remux"
)

// TestIsCorruptFlagsOnlyMeasurableMismatchesPastTheThreshold pins which
// remuxed parts go through the stream-copy heal pass. A false positive
// rewrites a healthy file; a false negative keeps a part whose container and
// stream disagree about its length. The threshold is spelled out rather than
// read from remux.CorruptionThreshold so that moving it fails here.
func TestIsCorruptFlagsOnlyMeasurableMismatchesPastTheThreshold(t *testing.T) {
	const length = 3600.0
	const limit = 50.0
	stream := func(seconds float64) *probe.Stream { return &probe.Stream{Duration: seconds} }
	cases := []struct {
		name   string
		result *probe.Result
		kind   remux.Kind
		want   bool
	}{
		{"no probe result", nil, remux.KindVideo, false},
		{"format duration unmeasured", &probe.Result{VideoStream: stream(length)}, remux.KindVideo, false},
		{"stream duration unmeasured", &probe.Result{Duration: length + 2*limit, VideoStream: stream(0)}, remux.KindVideo, false},
		{"no video stream", &probe.Result{Duration: length + 2*limit}, remux.KindVideo, false},
		{"mismatch at the threshold", &probe.Result{Duration: length + limit, VideoStream: stream(length)}, remux.KindVideo, false},
		{"format past the threshold", &probe.Result{Duration: length + limit + 0.5, VideoStream: stream(length)}, remux.KindVideo, true},
		{"stream past the threshold", &probe.Result{Duration: length, VideoStream: stream(length + limit + 0.5)}, remux.KindVideo, true},
		{"audio judged by its audio stream", &probe.Result{Duration: length + 2*limit, VideoStream: stream(length + 2*limit), AudioStream: stream(length)}, remux.KindAudio, true},
		{"video ignores a short audio stream", &probe.Result{Duration: length + 2*limit, VideoStream: stream(length + 2*limit), AudioStream: stream(length)}, remux.KindVideo, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isCorrupt(tc.result, tc.kind); got != tc.want {
				t.Fatalf("isCorrupt = %v, want %v", got, tc.want)
			}
		})
	}
}
